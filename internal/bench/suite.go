package bench

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math"
	"runtime"
	"time"

	"github.com/trickleai/machine-probe/internal/fingerprint"
	"github.com/trickleai/machine-probe/internal/util"
)

type Options struct {
	Profile  Profile
	Groups   []string
	Label    string
	Provider string
	Price    *float64
	Egress   bool
	Redact   bool
	Version  string
	Progress func(Progress)
}

type Progress struct {
	Index, Total int
	Title        string
	Detail       string
}

// Plan lists the tests a run would execute, in order.
func Plan(p Profile, groups []string, egress bool) []Test {
	want := map[string]bool{}
	for _, g := range groups {
		want[g] = true
	}
	var ts []Test
	if want["cpu"] {
		ts = append(ts, cpuTests(p)...)
	}
	if want["memory"] {
		ts = append(ts, memTests(p)...)
	}
	if want["disk"] {
		ts = append(ts, diskTests(p, candidateDirs(p.MultiDir))...)
	}
	if want["workload"] {
		ts = append(ts, workloadTests(p)...)
	}
	if want["network"] && egress {
		ts = append(ts, networkTests(p)...)
	}
	return ts
}

func newID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return time.Now().UTC().Format("20060102-150405") + "-" + hex.EncodeToString(b)
}

// RunSuite collects the fingerprint and runs every planned test. On cancellation it returns the partial run and ctx.Err().
func RunSuite(ctx context.Context, o Options) (*Run, error) {
	start := time.Now()
	report := func(p Progress) {
		if o.Progress != nil {
			o.Progress(p)
		}
	}
	report(Progress{Title: "Collecting fingerprint"})
	fp := fingerprint.Collect(ctx, o.Egress)
	if o.Redact {
		fp = fingerprint.Redact(fp)
	}
	run := &Run{
		Schema: 1, ID: newID(), Label: o.Label, Provider: o.Provider, PricePerHour: o.Price,
		Profile: o.Profile.Name, Groups: o.Groups, StartedAt: start.UnixMilli(),
		Probe:       &ProbeInfo{Version: o.Version, GoVersion: runtime.Version()},
		Fingerprint: fp, Results: []Result{},
	}
	tests := Plan(o.Profile, o.Groups, o.Egress)
	for i, t := range tests {
		if ctx.Err() != nil {
			break
		}
		res := runTest(ctx, t, o.Profile, func(phase, detail string) {
			report(Progress{Index: i + 1, Total: len(tests), Title: t.Title, Detail: joinNonEmpty(phase, detail)})
		})
		if ctx.Err() != nil && len(res.Metrics) == 0 {
			break
		}
		run.Results = append(run.Results, res)
	}
	run.DurationMs = time.Since(start).Milliseconds()
	run.Score = Score(run)
	return run, ctx.Err()
}

func joinNonEmpty(a, b string) string {
	if a == "" || b == "" {
		return a + b
	}
	return a + " · " + b
}

func runTest(ctx context.Context, t Test, p Profile, progress func(phase, detail string)) Result {
	repeats, warmup := p.Repeats, p.Warmup
	if t.Once {
		repeats, warmup = 1, 0
	}
	res := Result{ID: t.ID, Group: t.Group, Title: t.Title, Tool: t.Tool, Repeats: repeats, Warmup: warmup, Metrics: Metrics{}}
	samples := map[string][]float64{}
	var order []Measure
	var last Rep
	for i := 0; i < warmup+repeats; i++ {
		phase := fmt.Sprintf("run %d/%d", i-warmup+1, repeats)
		if i < warmup {
			phase = "warmup"
		}
		progress(phase, "")
		env := &Env{P: p, Progress: func(d string) { progress(phase, d) }}
		rep, err := t.Run(ctx, env)
		if err != nil {
			res.Error = err.Error()
			break
		}
		last = rep
		if i < warmup {
			continue
		}
		for _, m := range rep.Measures {
			if _, ok := samples[m.Key]; !ok {
				order = append(order, m)
			}
			samples[m.Key] = append(samples[m.Key], round(m.Value))
		}
	}
	for _, m := range order {
		st, ok := util.ComputeStats(samples[m.Key])
		if !ok {
			continue
		}
		res.Metrics = append(res.Metrics, KV{m.Key, Metric{Unit: m.Unit, Better: m.Better, Median: round(st.Median), Mean: round(st.Mean),
			Min: st.Min, Max: st.Max, CV: round(st.CV), N: st.N, Samples: samples[m.Key]}})
	}
	res.Series, res.Notes = last.Series, last.Notes
	return res
}

// round keeps a few decimals so the JSON stays readable.
func round(v float64) float64 {
	switch a := math.Abs(v); {
	case a >= 1000:
		return math.Round(v)
	case a >= 1:
		return math.Round(v*100) / 100
	}
	return math.Round(v*10000) / 10000
}
