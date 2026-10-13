// Package compare builds a side-by-side view of several result documents with per-group scores.
package compare

import (
	"fmt"
	"math"
	"sort"

	"github.com/SamdyChen2023/machine-probe/internal/bench"
)

const NoisyCV = 25 // percent; above this a median is flagged as unreliable

type Machine struct {
	Name, Profile string
}

type Value struct {
	V, CV float64
}

type Metric struct {
	Key, Unit, Better string
	Values            []*Value // nil when the machine lacks the metric
	Best              int      // index of the best machine, -1 when fewer than 2 machines share it
}

type Test struct {
	ID, Title string
	Metrics   []*Metric
}

type Group struct {
	ID     string
	Tests  []*Test
	Scores []*float64
}

type Comparison struct {
	Machines []Machine
	Groups   []*Group
	Overall  []*float64
	Warnings []string
}

const ScoreNote = "100 = best machine on every metric of that group; scores are geometric means of per-metric ratios to the best, over metrics that ≥2 machines share."

func geomean(xs []float64) *float64 {
	if len(xs) == 0 {
		return nil
	}
	s := 0.0
	for _, x := range xs {
		s += math.Log(x)
	}
	g := math.Exp(s / float64(len(xs)))
	return &g
}

var groupOrder = map[string]int{"cpu": 0, "memory": 1, "disk": 2, "network": 3, "workload": 4}

func Runs(runs []*bench.Run) *Comparison {
	c := &Comparison{}
	seen := map[string]int{}
	profiles := map[string]bool{}
	for _, r := range runs {
		name := r.Label
		if name == "" && r.Fingerprint != nil {
			name = r.Fingerprint.Host.Hostname
		}
		if name == "" {
			name = r.ID
		}
		seen[name]++
		if seen[name] > 1 {
			name = fmt.Sprintf("%s (%d)", name, seen[name])
		}
		c.Machines = append(c.Machines, Machine{name, r.Profile})
		profiles[r.Profile] = true
	}
	n := len(runs)
	groups := map[string]*Group{}
	tests := map[string]*Test{}
	metrics := map[string]*Metric{}
	for ri, r := range runs {
		for _, t := range r.Results {
			g := groups[t.Group]
			if g == nil {
				g = &Group{ID: t.Group}
				groups[t.Group] = g
				c.Groups = append(c.Groups, g)
			}
			te := tests[t.ID]
			if te == nil {
				te = &Test{ID: t.ID, Title: t.Title}
				tests[t.ID] = te
				g.Tests = append(g.Tests, te)
			}
			for _, kv := range t.Metrics {
				mk := t.ID + "\x00" + kv.Key
				me := metrics[mk]
				if me == nil {
					me = &Metric{Key: kv.Key, Unit: kv.M.Unit, Better: kv.M.Better, Values: make([]*Value, n), Best: -1}
					metrics[mk] = me
					te.Metrics = append(te.Metrics, me)
				}
				me.Values[ri] = &Value{kv.M.Median, kv.M.CV}
			}
		}
	}
	sort.SliceStable(c.Groups, func(i, j int) bool { return rank(c.Groups[i].ID) < rank(c.Groups[j].ID) })

	for _, g := range c.Groups {
		ratios := make([][]float64, n)
		for _, te := range g.Tests {
			for _, me := range te.Metrics {
				best, present := 0.0, 0
				for i, v := range me.Values {
					if v == nil || !(v.V > 0) || math.IsInf(v.V, 0) {
						continue
					}
					present++
					if present == 1 || (me.Better == "lower" && v.V < best) || (me.Better != "lower" && v.V > best) {
						best, me.Best = v.V, i
					}
				}
				if present < 2 {
					me.Best = -1
					continue
				}
				for i, v := range me.Values {
					if v == nil || !(v.V > 0) {
						continue
					}
					r := v.V / best
					if me.Better == "lower" {
						r = best / v.V
					}
					ratios[i] = append(ratios[i], r)
				}
			}
		}
		for i := range runs {
			s := geomean(ratios[i])
			if s != nil {
				*s = math.Round(*s * 100)
			}
			g.Scores = append(g.Scores, s)
		}
	}
	for i := range runs {
		var xs []float64
		for _, g := range c.Groups {
			if g.Scores[i] != nil {
				xs = append(xs, *g.Scores[i])
			}
		}
		s := geomean(xs)
		if s != nil {
			*s = math.Round(*s)
		}
		c.Overall = append(c.Overall, s)
	}
	shared := false
	for _, s := range c.Overall {
		shared = shared || s != nil
	}
	if !shared {
		c.Warnings = append(c.Warnings, "No metric is shared by two or more runs (e.g. machine-probe mp_* vs machine-lab js_* keys), so no scores can be computed.")
	}
	if len(profiles) > 1 {
		c.Warnings = append(c.Warnings, "Runs used different profiles (quick vs full) — repeat counts and sizes differ.")
	}
	return c
}

func rank(g string) int {
	if r, ok := groupOrder[g]; ok {
		return r
	}
	return len(groupOrder)
}
