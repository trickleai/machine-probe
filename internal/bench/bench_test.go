package bench

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestMetricsJSONKeepsOrder(t *testing.T) {
	in := Metrics{{"z_last", Metric{Unit: "s", Better: "lower", Median: 1}}, {"a_first", Metric{Unit: "x", Better: "higher", Median: 2}}}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Index(string(b), "z_last") > strings.Index(string(b), "a_first") {
		t.Fatalf("order lost: %s", b)
	}
	var out Metrics
	if err := json.Unmarshal(b, &out); err != nil || len(out) != 2 || out[0].Key != "z_last" || out[1].M.Median != 2 {
		t.Fatalf("round trip failed: %v %+v", err, out)
	}
}

func TestRunTestAggregates(t *testing.T) {
	n := 0.0
	tc := Test{ID: "t", Group: "cpu", Run: func(ctx context.Context, e *Env) (Rep, error) {
		n++
		var r Rep
		r.Hi("v", "pts", n)
		return r, nil
	}}
	res := runTest(context.Background(), tc, Profile{Repeats: 3, Warmup: 1}, func(string, string) {})
	m, ok := res.Metrics.Get("v")
	if !ok || m.N != 3 || m.Median != 3 || m.Min != 2 { // warmup sample (1) is discarded
		t.Fatalf("bad aggregate %+v", m)
	}
}

func TestLCGTextDeterministic(t *testing.T) {
	a, b := lcgText(4096), lcgText(4096)
	if string(a) != string(b) || len(a) != 4096 {
		t.Fatal("lcgText must be deterministic and exact-size")
	}
}

func TestCPUScorePositive(t *testing.T) {
	if s := cpuScore(context.Background(), 1, 200*time.Millisecond); s <= 0 {
		t.Fatalf("score = %v", s)
	}
}
