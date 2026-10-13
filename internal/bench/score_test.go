package bench

import "testing"

func TestPointScoreAnchors(t *testing.T) {
	hi := Ref{Weak: 100, Typical: 1000, Excellent: 5000}
	lo := Ref{Weak: 250, Typical: 80, Excellent: 20}
	lin := Ref{Weak: 15, Typical: 5, Excellent: 1, Linear: true}
	cases := []struct {
		r    Ref
		v    float64
		want float64
	}{
		{hi, 100, 40}, {hi, 1000, 65}, {hi, 5000, 90}, {hi, 1e9, 100}, {hi, 0, 0},
		{lo, 250, 40}, {lo, 80, 65}, {lo, 20, 90}, {lo, 0, 100},
		{lin, 15, 40}, {lin, 5, 65}, {lin, 1, 90}, {lin, 100, 0},
	}
	for _, c := range cases {
		if got := PointScore(c.v, c.r); got < c.want-0.01 || got > c.want+0.01 {
			t.Errorf("PointScore(%v, %+v) = %.2f, want %.0f", c.v, c.r, got, c.want)
		}
	}
}

func TestScorePicksBlockDiskAndDerives(t *testing.T) {
	m := func(v float64) Metric { return Metric{Median: v, Better: "higher"} }
	run := &Run{Profile: "full", Results: []Result{
		{ID: "cpu.go", Group: "cpu", Metrics: Metrics{{"mp_single_score", m(14000)}, {"mp_efficiency", m(50)}}},
		{ID: "disk.seq@/dev/shm", Group: "disk", Notes: "O_DIRECT unsupported", Metrics: Metrics{{"mp_seq_write_mibs", m(9000)}}},
		{ID: "disk.seq@/tmp", Group: "disk", Metrics: Metrics{{"mp_seq_write_mibs", m(250)}}},
	}}
	sc := Score(run)
	if sc == nil || len(sc.Groups) != 2 {
		t.Fatalf("scorecard = %+v", sc)
	}
	cpu, disk := sc.Groups[0], sc.Groups[1]
	if len(cpu.Metrics) != 2 || cpu.Metrics[1].Key != "mp_multi_per_vcpu" || cpu.Metrics[1].Value != 7000 {
		t.Fatalf("cpu metrics = %+v", cpu.Metrics)
	}
	if disk.Metrics[0].Test != "disk.seq@/tmp" || disk.Score != 65 {
		t.Fatalf("disk = %+v", disk)
	}
	if Score(&Run{Results: []Result{{ID: "x", Metrics: Metrics{{"js_score", m(1)}}}}}) != nil {
		t.Fatal("unscorable run should yield nil")
	}
}
