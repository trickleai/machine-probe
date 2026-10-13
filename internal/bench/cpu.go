package bench

import (
	"context"
	"crypto/sha256"
	"fmt"
	"math"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SamdyChen2023/machine-probe/internal/util"
)

var sink atomic.Uint64 // keeps the compiler from discarding benchmark work

var chunk = func() []byte {
	b := make([]byte, 64<<10)
	for i := range b {
		b[i] = byte(i * 31)
	}
	return b
}()

func sieve(n int) int {
	comp := make([]bool, n+1)
	c := 0
	for i := 2; i <= n; i++ {
		if !comp[i] {
			c++
			for j := i * i; j <= n; j += i {
				comp[j] = true
			}
		}
	}
	return c
}

func fib(n int) uint64 {
	a, b := uint64(0), uint64(1)
	for i := 0; i < n; i++ {
		a, b = b, a+b
	}
	return a
}

// unit is one loop of the mixed CPU kernel: SHA-256 of 64 KiB + sieve(20000) + fib(500).
func unit() uint64 {
	h := sha256.Sum256(chunk)
	return uint64(h[0]) + uint64(sieve(20000)) + fib(500)
}

// score maps loop throughput to a relative score: cube root of the three normalized rates, ×100.
func score(loops uint64, elapsed time.Duration) float64 {
	per3s := float64(loops) * 3 / elapsed.Seconds()
	hashMB := per3s * 64 / 1024
	return math.Round(math.Cbrt(hashMB/100*per3s/100*per3s/100) * 100)
}

// load runs n workers that bump counter until stop is set.
func load(n int, counter *atomic.Uint64, stop *atomic.Bool) *sync.WaitGroup {
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			var acc uint64
			for !stop.Load() {
				acc += unit()
				counter.Add(1)
			}
			sink.Add(acc)
		}()
	}
	return &wg
}

func cpuScore(ctx context.Context, threads int, d time.Duration) float64 {
	var c atomic.Uint64
	var stop atomic.Bool
	start := time.Now()
	wg := load(threads, &c, &stop)
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
	stop.Store(true)
	wg.Wait()
	return score(c.Load(), time.Since(start))
}

func sha256Rate(d time.Duration) float64 {
	start, n := time.Now(), 0
	var acc byte
	for time.Since(start) < d {
		h := sha256.Sum256(chunk)
		acc += h[0]
		n++
	}
	sink.Add(uint64(acc))
	return float64(n) * 64 / 1024 / time.Since(start).Seconds()
}

func secs(f float64) time.Duration { return time.Duration(f * float64(time.Second)) }

func cpuTests(p Profile) []Test {
	cores := runtime.NumCPU()
	ts := []Test{{
		ID: "cpu.go", Group: "cpu", Title: "CPU · built-in Go (SHA-256 + sieve + fib)", Tool: "go-goroutines",
		Run: func(ctx context.Context, e *Env) (Rep, error) {
			var r Rep
			single := cpuScore(ctx, 1, secs(p.CPUSeconds))
			multi := cpuScore(ctx, cores, secs(p.CPUSeconds))
			r.Hi("mp_single_score", "pts", single)
			r.Hi("mp_multi_score", "pts", multi)
			r.Hi("mp_speedup", "×", multi/single)
			r.Hi("mp_efficiency", "%", multi/single/float64(cores)*100)
			r.Hi("mp_sha256_mibs", "MiB/s", sha256Rate(500*time.Millisecond))
			r.Notes = fmt.Sprintf("Relative score for comparing machines only. Multi-thread uses %d workers (one per vCPU).", cores)
			return r, ctx.Err()
		},
	}}
	if p.Sweep {
		ts = append(ts, Test{
			ID: "cpu.sweep", Group: "cpu", Title: "CPU · thread scaling curve", Tool: "go-goroutines", Once: true,
			Run: func(ctx context.Context, e *Env) (Rep, error) {
				limit := cores * 2
				if limit > 64 {
					limit = 64
				}
				seen := map[int]bool{}
				var counts []int
				for _, n := range []int{1, 2, 4, 8, 16, 32, cores, cores * 2} {
					if n <= limit && !seen[n] {
						seen[n] = true
						counts = append(counts, n)
					}
				}
				sort.Ints(counts)
				s := Series{Name: "score by thread count", XLabel: "threads", YLabel: "score"}
				var peak, atCores, one float64
				for _, n := range counts {
					e.Progress(fmt.Sprintf("%d threads", n))
					v := cpuScore(ctx, n, 2*time.Second)
					if ctx.Err() != nil {
						return Rep{}, ctx.Err()
					}
					s.Points = append(s.Points, [2]float64{float64(n), v})
					peak = math.Max(peak, v)
					if n == 1 {
						one = v
					}
					if n == cores {
						atCores = v
					}
				}
				var r Rep
				r.Hi("mp_peak_score", "pts", peak)
				r.Hi("mp_score_at_vcpus", "pts", atCores)
				if one > 0 {
					r.Hi("mp_scaling_at_vcpus", "×", atCores/one)
				}
				r.Series = []Series{s}
				return r, nil
			},
		})
	}
	if p.SustainedSeconds > 0 {
		ts = append(ts, Test{
			ID: "cpu.sustained", Group: "cpu", Title: fmt.Sprintf("CPU · sustained all-core load (%ds)", p.SustainedSeconds), Tool: "go-goroutines", Once: true,
			Run: func(ctx context.Context, e *Env) (Rep, error) { return sustained(ctx, e, cores, p.SustainedSeconds) },
		})
	}
	return ts
}

func sustained(ctx context.Context, e *Env, cores, seconds int) (Rep, error) {
	const slice = 5 * time.Second
	var c atomic.Uint64
	var stop atomic.Bool
	s0, t0 := util.CPUCounters()
	wg := load(cores, &c, &stop)
	s := Series{Name: "score over time", XLabel: "seconds", YLabel: "score"}
	start, last, lastN := time.Now(), time.Now(), uint64(0)
	tick := time.NewTicker(slice)
	defer tick.Stop()
loop:
	for time.Since(start) < time.Duration(seconds)*time.Second {
		select {
		case <-ctx.Done():
			break loop
		case now := <-tick.C:
			n := c.Load()
			s.Points = append(s.Points, [2]float64{math.Round(now.Sub(start).Seconds()), score(n-lastN, now.Sub(last))})
			last, lastN = now, n
			e.Progress(fmt.Sprintf("%.0fs / %ds", now.Sub(start).Seconds(), seconds))
		}
	}
	stop.Store(true)
	wg.Wait()
	if ctx.Err() != nil {
		return Rep{}, ctx.Err()
	}
	var r Rep
	if len(s.Points) > 0 {
		first, lastV := s.Points[0][1], s.Points[len(s.Points)-1][1]
		r.Hi("mp_sustained_first", "pts", first)
		r.Hi("mp_sustained_last", "pts", lastV)
		r.Lo("mp_throttle_drop", "%", math.Max(0, (first-lastV)/first*100))
	}
	s1, t1 := util.CPUCounters()
	if t1 > t0 {
		r.Lo("mp_cpu_steal", "%", float64(s1-s0)/float64(t1-t0)*100)
	}
	r.Series = []Series{s}
	r.Notes = "A large drop between the first and last slices indicates throttling or burst-credit exhaustion. Steal is read from /proc/stat."
	return r, nil
}
