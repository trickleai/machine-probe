package bench

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/trickleai/machine-probe/internal/util"
)

func memTests(p Profile) []Test {
	size := int64(p.MemMB) << 20
	if b := util.MemBudgetBytes() / 2; size > b { // two buffers for the copy test
		size = b
	}
	mib := size >> 20
	return []Test{{
		ID: "mem.go", Group: "memory", Title: "Memory · built-in Go slice bandwidth", Tool: "go",
		Run: func(ctx context.Context, e *Env) (Rep, error) {
			n := int(size / 8)
			a, b := make([]float64, n), make([]float64, n)
			gib := float64(size) / (1 << 30)
			const passes = 4
			timed := func(f func()) float64 {
				t := time.Now()
				for i := 0; i < passes; i++ {
					f()
				}
				return gib * passes / time.Since(t).Seconds()
			}
			for i := range a { // fault pages in before timing
				a[i], b[i] = 1, 1
			}
			var r Rep
			r.Hi("mp_mem_write_gibs", "GiB/s", timed(func() {
				for i := range a {
					a[i] = 1.5
				}
			}))
			var sum float64
			r.Hi("mp_mem_read_gibs", "GiB/s", timed(func() {
				// four independent accumulators so the loop is bound by loads, not FP-add latency
				var s0, s1, s2, s3 float64
				for i := 0; i+3 < len(a); i += 4 {
					s0 += a[i]
					s1 += a[i+1]
					s2 += a[i+2]
					s3 += a[i+3]
				}
				sum += s0 + s1 + s2 + s3
			}))
			r.Hi("mp_mem_copy_gibs", "GiB/s", timed(func() { copy(b, a) }))
			if math.IsNaN(sum) {
				sink.Add(1)
			}
			r.Notes = fmt.Sprintf("%d MiB working set per buffer. Copy counts bytes copied (not read+write).", mib)
			return r, ctx.Err()
		},
	}}
}
