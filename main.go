// machine-probe: fingerprint a machine, benchmark it, and write a JSON result plus a self-contained HTML report.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"

	"github.com/trickleai/machine-probe/internal/bench"
	"github.com/trickleai/machine-probe/internal/compare"
	"github.com/trickleai/machine-probe/internal/fingerprint"
	"github.com/trickleai/machine-probe/internal/report"
	"github.com/trickleai/machine-probe/internal/util"
)

var version = "dev"

const usage = `machine-probe — environment fingerprint + benchmarks → JSON + HTML report

usage:
  machine-probe run     [--profile quick|full] [--groups cpu,memory,disk,workload,network]
                        [--label NAME] [--provider NAME] [--price USD_PER_HOUR]
                        [--out FILE.json] [--html FILE.html] [--no-html] [--no-egress] [--redact]
  machine-probe info    [--json] [--no-egress] [--redact]
  machine-probe report  RESULT.json [--out FILE.html]
  machine-probe compare A.json B.json [...]
  machine-probe version

run writes FILE.json and, unless --no-html, FILE.html next to it (or at --html).
--no-egress skips the network group and the egress-IP lookup; --redact strips hostname, IPs and MACs.
`

var boolFlags = map[string]bool{"no-egress": true, "redact": true, "json": true, "no-html": true, "help": true}

func parseArgs(args []string) (pos []string, flags map[string]string, err error) {
	flags = map[string]string{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "--") {
			pos = append(pos, a)
			continue
		}
		k, v, hasV := strings.Cut(a[2:], "=")
		if boolFlags[k] {
			flags[k] = "true"
			continue
		}
		if !hasV {
			if i+1 >= len(args) {
				return nil, nil, fmt.Errorf("--%s needs a value", k)
			}
			i++
			v = args[i]
		}
		flags[k] = v
	}
	return
}

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		return
	}
	pos, flags, err := parseArgs(os.Args[2:])
	if err != nil {
		fail(2, err)
	}
	if flags["help"] != "" {
		fmt.Print(usage)
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	switch os.Args[1] {
	case "run":
		err = cmdRun(ctx, flags)
	case "info":
		err = cmdInfo(ctx, flags)
	case "report":
		err = cmdReport(pos, flags)
	case "compare":
		err = cmdCompare(pos)
	case "version", "--version":
		fmt.Println("machine-probe", version)
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if errors.Is(err, context.Canceled) {
		fail(130, errors.New("cancelled"))
	}
	if err != nil {
		fail(1, err)
	}
}

func fail(code int, err error) {
	fmt.Fprintln(os.Stderr, "machine-probe:", err)
	os.Exit(code)
}

var unsafeName = regexp.MustCompile(`[^\w.-]+`)

func cmdRun(ctx context.Context, f map[string]string) error {
	pname := or(f["profile"], "quick")
	prof, ok := bench.Profiles[pname]
	if !ok {
		return fmt.Errorf("unknown profile %q (quick|full)", pname)
	}
	groups := bench.Groups
	if g := f["groups"]; g != "" {
		groups = nil
		valid := strings.Join(bench.Groups, ",")
		for _, x := range strings.Split(g, ",") {
			if x = strings.TrimSpace(x); !strings.Contains(","+valid+",", ","+x+",") {
				return fmt.Errorf("unknown group %q (valid: %s)", x, valid)
			}
			groups = append(groups, x)
		}
	}
	var price *float64
	if p := f["price"]; p != "" {
		v, err := strconv.ParseFloat(p, 64)
		if err != nil {
			return fmt.Errorf("--price: %w", err)
		}
		price = &v
	}
	label := f["label"]
	if label == "" {
		label, _ = os.Hostname()
	}
	egress := f["no-egress"] == ""
	if !egress {
		groups = without(groups, "network")
	}

	tty := isTTY(os.Stderr)
	run, runErr := bench.RunSuite(ctx, bench.Options{
		Profile: prof, Groups: groups, Label: label, Provider: f["provider"], Price: price,
		Egress: egress, Redact: f["redact"] != "", Version: version,
		Progress: func(p bench.Progress) {
			line := p.Title
			if p.Total > 0 {
				line = fmt.Sprintf("[%d/%d] %s", p.Index, p.Total, p.Title)
			}
			if p.Detail != "" {
				line += "  (" + p.Detail + ")"
			}
			if tty {
				fmt.Fprint(os.Stderr, "\r\x1b[K"+line)
			} else {
				fmt.Fprintln(os.Stderr, line)
			}
		},
	})
	if tty {
		fmt.Fprint(os.Stderr, "\r\x1b[K")
	}
	if runErr != nil && len(run.Results) == 0 {
		return runErr
	}

	out := or(f["out"], fmt.Sprintf("machine-probe-%s-%s.json", unsafeName.ReplaceAllString(label, "_"), prof.Name))
	if runErr != nil {
		out = strings.TrimSuffix(out, ".json") + ".partial.json"
	}
	if err := writeJSON(out, run); err != nil {
		return err
	}
	htmlPath := ""
	if f["no-html"] == "" {
		htmlPath = or(f["html"], strings.TrimSuffix(out, filepath.Ext(out))+".html")
		if err := writeReport(htmlPath, run); err != nil {
			return err
		}
	}

	fp := run.Fingerprint
	fmt.Printf("%s · %s · %d vCPU\n", fp.Virtualization.Label, fp.CPU.Model, fp.CPU.VCPUs)
	printResults(run)
	printScore(run.Score)
	abs, _ := filepath.Abs(out)
	fmt.Printf("\nfinished in %.0f s\n  json → %s\n", float64(run.DurationMs)/1000, abs)
	if htmlPath != "" {
		abs, _ = filepath.Abs(htmlPath)
		fmt.Printf("  html → %s\n", abs)
	}
	return runErr
}

func printResults(run *bench.Run) {
	for _, r := range run.Results {
		fmt.Printf("\n%s", r.Title)
		if r.Error != "" {
			fmt.Printf("  ✗ %s", r.Error)
		}
		fmt.Println()
		for _, kv := range r.Metrics {
			m := kv.M
			noisy := ""
			if m.CV > compare.NoisyCV {
				noisy = "  ⚠ noisy"
			}
			fmt.Printf("  %-32s %12s %-8s ±%.1f%%%s\n", kv.Key, fmtNum(m.Median), m.Unit, m.CV, noisy)
		}
	}
}

func printScore(sc *bench.Scorecard) {
	if sc == nil {
		return
	}
	fmt.Printf("\nscore  %d / 100  %s   (reference %s)\n", sc.Overall, sc.RatingLabel, sc.Reference)
	for _, g := range sc.Groups {
		fmt.Printf("  %-10s %3d  %s\n", g.ID, g.Score, g.RatingLabel)
	}
	for _, n := range sc.Notes {
		fmt.Println("  ·", n)
	}
}

func cmdInfo(ctx context.Context, f map[string]string) error {
	fp := fingerprint.Collect(ctx, f["no-egress"] == "")
	if f["redact"] != "" {
		fp = fingerprint.Redact(fp)
	}
	if f["json"] != "" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(fp)
	}
	section := ""
	for _, r := range fingerprint.Summarize(fp) {
		if r.Section != section {
			section = r.Section
			fmt.Printf("\n[%s]\n", section)
		}
		fmt.Printf("  %-36s %s\n", r.Label, r.Value)
	}
	fmt.Println("\nclassification evidence:")
	for _, e := range fp.Virtualization.Evidence {
		fmt.Println("  -", e)
	}
	return nil
}

func cmdReport(pos []string, f map[string]string) error {
	if len(pos) != 1 {
		return errors.New("usage: machine-probe report RESULT.json [--out FILE.html]")
	}
	run, err := bench.Load(pos[0])
	if err != nil {
		return err
	}
	out := or(f["out"], strings.TrimSuffix(pos[0], filepath.Ext(pos[0]))+".html")
	if err := writeReport(out, run); err != nil {
		return err
	}
	abs, _ := filepath.Abs(out)
	fmt.Println("html →", abs)
	return nil
}

func cmdCompare(files []string) error {
	if len(files) < 2 {
		return errors.New("compare needs at least two result files")
	}
	var runs []*bench.Run
	for _, p := range files {
		r, err := bench.Load(p)
		if err != nil {
			return err
		}
		runs = append(runs, r)
	}
	c := compare.Runs(runs)
	w := 12
	for _, m := range c.Machines {
		w = max(w, len(m.Name)+2)
	}
	row := func(label string, vals []string) {
		fmt.Printf("%-40s", label)
		for _, v := range vals {
			fmt.Printf("%*s", w, v)
		}
		fmt.Println()
	}
	names := make([]string, len(c.Machines))
	for i, m := range c.Machines {
		names[i] = m.Name
	}
	row("", names)
	abs := make([]string, len(runs))
	for i, r := range runs {
		abs[i] = "–"
		if sc := bench.Score(r); sc != nil {
			abs[i] = fmt.Sprintf("%d %s", sc.Overall, sc.Rating)
		}
	}
	row("absolute score 0-100 (ref "+bench.RefVersion+")", abs)
	row("relative score (100 = best)", scores(c.Overall))
	for _, g := range c.Groups {
		fmt.Println()
		row("== "+g.ID+"   group score:", scores(g.Scores))
		for _, t := range g.Tests {
			for _, m := range t.Metrics {
				vals := make([]string, len(m.Values))
				for i, v := range m.Values {
					if v == nil {
						vals[i] = "–"
						continue
					}
					vals[i] = fmtNum(v.V)
					if i == m.Best {
						vals[i] += " ★"
					}
					if v.CV > compare.NoisyCV {
						vals[i] += " ⚠"
					}
				}
				row(fmt.Sprintf("  %s (%s)", m.Key, m.Unit), vals)
			}
		}
	}
	for _, x := range c.Warnings {
		fmt.Println("\n⚠", x)
	}
	fmt.Println("\n" + compare.ScoreNote)
	return nil
}

func scores(xs []*float64) []string {
	out := make([]string, len(xs))
	for i, x := range xs {
		out[i] = "–"
		if x != nil {
			out[i] = strconv.Itoa(int(*x))
		}
	}
	return out
}

func writeJSON(path string, run *bench.Run) error {
	b, err := json.MarshalIndent(run, "", "  ")
	if err != nil {
		return err
	}
	return util.WriteFileAtomic(path, append(b, '\n'))
}

func writeReport(path string, run *bench.Run) error {
	b, err := report.Render(run)
	if err != nil {
		return err
	}
	return util.WriteFileAtomic(path, b)
}

func fmtNum(v float64) string {
	a := v
	if a < 0 {
		a = -a
	}
	switch {
	case a >= 1000:
		return strconv.FormatFloat(v, 'f', 0, 64)
	case a >= 10:
		return strconv.FormatFloat(v, 'f', 1, 64)
	}
	return strconv.FormatFloat(v, 'f', 2, 64)
}

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func without(xs []string, drop string) []string {
	var out []string
	for _, x := range xs {
		if x != drop {
			out = append(out, x)
		}
	}
	return out
}

func isTTY(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}
