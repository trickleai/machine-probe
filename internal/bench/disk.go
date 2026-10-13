package bench

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
	"unsafe"

	"github.com/trickleai/machine-probe/internal/util"
)

const blk = 1 << 20

func alignedBuf(size int) []byte {
	b := make([]byte, size+4096)
	off := int(4096 - uintptr(unsafe.Pointer(&b[0]))&4095)
	if off == 4096 {
		off = 0
	}
	return b[off : off+size]
}

// seqIO writes then reads mb MiB in 1 MiB blocks, using O_DIRECT where the filesystem allows it.
func seqIO(ctx context.Context, path string, mb int) (w, r float64, direct bool, err error) {
	buf := alignedBuf(blk)
	rand.New(rand.NewSource(7)).Read(buf)
	write := func(flag int) (time.Duration, error) {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC|flag, 0o600)
		if err != nil {
			return 0, err
		}
		defer f.Close()
		t := time.Now()
		for i := 0; i < mb; i++ {
			if ctx.Err() != nil {
				return 0, ctx.Err()
			}
			stampPages(buf, i)
			if _, err := f.Write(buf); err != nil {
				return 0, err
			}
		}
		if err := util.Fdatasync(f); err != nil {
			return 0, err
		}
		return time.Since(t), nil
	}
	direct = util.ODirect != 0
	wt, err := write(util.ODirect)
	if err != nil && direct && ctx.Err() == nil {
		direct = false
		wt, err = write(0)
	}
	if err != nil {
		return
	}
	flag := 0
	if direct {
		flag = util.ODirect
	}
	f, err := os.OpenFile(path, os.O_RDONLY|flag, 0)
	if err != nil {
		return
	}
	defer f.Close()
	t := time.Now()
	for {
		if _, e := io.ReadFull(f, buf); e != nil {
			if e != io.EOF && e != io.ErrUnexpectedEOF {
				err = e
				return
			}
			break
		}
	}
	rt := time.Since(t)
	return float64(mb) / wt.Seconds(), float64(mb) / rt.Seconds(), direct, nil
}

// stampPages makes every 4 KiB page of the (random) block unique, so storage that
// compresses or deduplicates cannot inflate the write rate.
func stampPages(buf []byte, block int) {
	for off := 0; off+8 <= len(buf); off += 4096 {
		binary.LittleEndian.PutUint64(buf[off:], uint64(block)<<32|uint64(off))
	}
}

const (
	syncSpanMiB = 16 // file size used by syncLatency
	spareMiB    = 16 // headroom left free on the filesystem under test
)

// freeMiB reports the space available to unprivileged writers at dir.
func freeMiB(dir string) (uint64, bool) {
	st, ok := util.Statfs(dir)
	return st.Free >> 20, ok
}

// syncLatency does random 4 KiB writes, each followed by fdatasync.
func syncLatency(ctx context.Context, path string, ops int) (lat []float64, total time.Duration, err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	const span = syncSpanMiB << 20
	if err = f.Truncate(span); err != nil {
		return
	}
	buf := make([]byte, 4096)
	rng := rand.New(rand.NewSource(1))
	start := time.Now()
	for i := 0; i < ops && ctx.Err() == nil; i++ {
		t := time.Now()
		if _, err = f.WriteAt(buf, int64(rng.Intn(span/4096))*4096); err != nil {
			return
		}
		if err = util.Fdatasync(f); err != nil {
			return
		}
		lat = append(lat, float64(time.Since(t).Microseconds()))
	}
	return lat, time.Since(start), ctx.Err()
}

// parallel runs fn(i) for i in [0,n) on 8 goroutines and returns ops/s.
func parallel(n int, fn func(i int) error) (float64, error) {
	var wg sync.WaitGroup
	var mu sync.Mutex
	var first error
	next := make(chan int, 64)
	t := time.Now()
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				if err := fn(i); err != nil {
					mu.Lock()
					if first == nil {
						first = err
					}
					mu.Unlock()
				}
			}
		}()
	}
	for i := 0; i < n; i++ {
		next <- i
	}
	close(next)
	wg.Wait()
	return float64(n) / time.Since(t).Seconds(), first
}

func diskTests(p Profile, dirs []string) []Test {
	var ts []Test
	for _, dir := range dirs {
		dir := dir
		ts = append(ts, Test{
			ID: "disk.seq@" + dir, Group: "disk", Title: fmt.Sprintf("Disk · sequential %d MiB + 4K sync-write latency @ %s", p.DiskMB, dir), Tool: "go-os",
			Run: func(ctx context.Context, e *Env) (Rep, error) {
				tmp, err := os.MkdirTemp(dir, ".machine-probe-")
				if err != nil {
					return Rep{}, err
				}
				defer os.RemoveAll(tmp)
				var r Rep
				need := uint64(p.DiskMB + syncSpanMiB + spareMiB)
				if free, ok := freeMiB(dir); ok && free < need {
					// Shrinking the file would make the numbers incomparable with other runs,
					// so skip the sequential part on small filesystems (e.g. a 64 MiB /dev/shm).
					r.Notes = fmt.Sprintf("Sequential test skipped: %d MiB free at %s, it needs %d MiB.", free, dir, need)
					if free < syncSpanMiB+spareMiB {
						return r, nil
					}
				} else {
					w, rd, direct, err := seqIO(ctx, filepath.Join(tmp, "seq.bin"), p.DiskMB)
					if err != nil {
						return r, err
					}
					r.Hi("mp_seq_write_mibs", "MiB/s", w)
					r.Hi("mp_seq_read_mibs", "MiB/s", rd)
					if !direct {
						r.Notes = "O_DIRECT unsupported here; buffered I/O + fdatasync used (reads likely served from page cache)."
					}
				}
				lat, total, err := syncLatency(ctx, filepath.Join(tmp, "sync.bin"), p.LatOps)
				if err != nil || len(lat) == 0 {
					return r, err
				}
				r.Hi("mp_sync4k_iops", "IOPS", float64(len(lat))/total.Seconds())
				sort.Float64s(lat)
				r.Lo("mp_sync4k_p50_us", "µs", util.Percentile(lat, 50))
				r.Lo("mp_sync4k_p99_us", "µs", util.Percentile(lat, 99))
				return r, nil
			},
		}, Test{
			ID: "disk.smallfiles@" + dir, Group: "disk", Title: fmt.Sprintf("Disk · small files (4 KiB × %d) @ %s", p.SmallFiles, dir), Tool: "go-os",
			Run: func(ctx context.Context, e *Env) (Rep, error) {
				tmp, err := os.MkdirTemp(dir, ".machine-probe-")
				if err != nil {
					return Rep{}, err
				}
				defer os.RemoveAll(tmp)
				data := make([]byte, 4096)
				name := func(i int) string { return filepath.Join(tmp, fmt.Sprintf("f%06d", i)) }
				var r Rep
				phases := []struct {
					key string
					fn  func(int) error
				}{
					{"mp_files_create_per_s", func(i int) error { return os.WriteFile(name(i), data, 0o600) }},
					{"mp_files_stat_per_s", func(i int) error { _, err := os.Stat(name(i)); return err }},
					{"mp_files_read_per_s", func(i int) error { _, err := os.ReadFile(name(i)); return err }},
					{"mp_files_delete_per_s", func(i int) error { return os.Remove(name(i)) }},
				}
				for _, ph := range phases {
					if ctx.Err() != nil {
						return r, ctx.Err()
					}
					v, err := parallel(p.SmallFiles, ph.fn)
					if err != nil {
						return r, err
					}
					r.Hi(ph.key, "/s", v)
				}
				return r, nil
			},
		})
	}
	return ts
}

// candidateDirs picks writable directories on distinct devices: the temp dir, plus $HOME and /dev/shm in multi-dir mode.
func candidateDirs(multi bool) []string {
	cands := []string{os.TempDir()}
	if multi {
		home, _ := os.UserHomeDir()
		cands = append(cands, home, "/dev/shm")
	}
	var out []string
	devs := map[uint64]bool{}
	for _, d := range cands {
		if d == "" || !util.Writable(d) {
			continue
		}
		if dev, ok := util.DevOf(d); ok {
			if devs[dev] {
				continue
			}
			devs[dev] = true
		}
		out = append(out, d)
	}
	return out
}
