package compare

import (
	"testing"

	"github.com/trickleai/machine-probe/internal/bench"
)

func run(label string, score, lat float64) *bench.Run {
	return &bench.Run{Label: label, Profile: "quick", Results: []bench.Result{{
		ID: "cpu.go", Group: "cpu", Metrics: bench.Metrics{
			{Key: "score", M: bench.Metric{Better: "higher", Median: score}},
			{Key: "lat", M: bench.Metric{Better: "lower", Median: lat}},
		}}}}
}

func TestScores(t *testing.T) {
	c := Runs([]*bench.Run{run("a", 100, 10), run("b", 50, 20), run("a", 100, 10)})
	if c.Machines[2].Name != "a (2)" {
		t.Fatalf("duplicate names not disambiguated: %+v", c.Machines)
	}
	g := c.Groups[0]
	if *g.Scores[0] != 100 || *g.Scores[1] != 50 {
		t.Fatalf("scores = %v %v", *g.Scores[0], *g.Scores[1])
	}
	if *c.Overall[1] != 50 || g.Tests[0].Metrics[1].Best != 0 {
		t.Fatal("overall / best mismatch")
	}
}
