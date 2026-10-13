package bench

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"time"
)

func spawnRate(ctx context.Context, d time.Duration) (float64, error) {
	bin := "/bin/true"
	if p, err := exec.LookPath("true"); err == nil {
		bin = p
	}
	start, n := time.Now(), 0
	for time.Since(start) < d && ctx.Err() == nil {
		if err := exec.CommandContext(ctx, bin).Run(); err != nil {
			return 0, err
		}
		n++
	}
	return float64(n) / time.Since(start).Seconds(), ctx.Err()
}

var words = []string{"alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf", "hotel", "india", "juliet", "kilo", "lima"}

// lcgText builds deterministic, moderately compressible text so the ratio is comparable across machines.
func lcgText(size int) []byte {
	var b bytes.Buffer
	b.Grow(size + 32)
	x := uint32(42)
	for b.Len() < size {
		x = x*1103515245 + 12345
		b.WriteString(words[x%12])
		b.WriteString(strconv.Itoa(int((x >> 8) % 97)))
		b.WriteByte(' ')
	}
	return b.Bytes()[:size]
}

func workloadTests(p Profile) []Test {
	return []Test{
		{
			ID: "proc.spawn", Group: "workload", Title: "Process · spawn rate (/bin/true)", Tool: "go-exec",
			Run: func(ctx context.Context, e *Env) (Rep, error) {
				v, err := spawnRate(ctx, secs(p.SpawnSeconds))
				var r Rep
				r.Hi("mp_spawn_per_s", "/s", v)
				return r, err
			},
		},
		{
			ID: "wl.gzip", Group: "workload", Title: fmt.Sprintf("Workload · gzip level 6 on %d MiB of text", p.ZlibMB), Tool: "go-compress/gzip",
			Run: func(ctx context.Context, e *Env) (Rep, error) {
				src := lcgText(p.ZlibMB << 20)
				var out bytes.Buffer
				t := time.Now()
				w, _ := gzip.NewWriterLevel(&out, 6)
				w.Write(src)
				w.Close()
				ct := time.Since(t)
				t = time.Now()
				zr, err := gzip.NewReader(bytes.NewReader(out.Bytes()))
				if err != nil {
					return Rep{}, err
				}
				n, err := io.Copy(io.Discard, zr)
				if err != nil {
					return Rep{}, err
				}
				dt := time.Since(t)
				mib := float64(n) / (1 << 20)
				var r Rep
				r.Hi("mp_gzip_mibs", "MiB/s", mib/ct.Seconds())
				r.Hi("mp_gunzip_mibs", "MiB/s", mib/dt.Seconds())
				r.Hi("mp_gzip_ratio", "×", float64(len(src))/float64(out.Len()))
				return r, ctx.Err()
			},
		},
	}
}
