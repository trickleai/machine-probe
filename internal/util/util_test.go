package util

import (
	"math"
	"testing"
)

func TestComputeStats(t *testing.T) {
	s, ok := ComputeStats([]float64{4, 1, 3, 2, math.NaN()})
	if !ok || s.N != 4 || s.Median != 2.5 || s.Mean != 2.5 || s.Min != 1 || s.Max != 4 {
		t.Fatalf("unexpected stats %+v", s)
	}
	if math.Abs(s.CV-44.72) > 0.01 {
		t.Fatalf("cv = %v", s.CV)
	}
	if _, ok := ComputeStats(nil); ok {
		t.Fatal("empty input should not be ok")
	}
}

func TestPercentile(t *testing.T) {
	xs := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	if Percentile(xs, 50) != 6 || Percentile(xs, 99) != 10 {
		t.Fatal("percentile mismatch")
	}
}
