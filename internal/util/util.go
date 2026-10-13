package util

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

func Read(p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return string(b)
}

func Exists(p string) bool { _, err := os.Stat(p); return err == nil }

func First(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

type CmdResult struct {
	OK     bool
	Stdout string
	Stderr string
}

// Run never returns an error; callers check OK.
func Run(ctx context.Context, timeout time.Duration, name string, args ...string) CmdResult {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	var out, errb strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	return CmdResult{OK: err == nil, Stdout: out.String(), Stderr: errb.String()}
}

func Has(cmd string) bool { _, err := exec.LookPath(cmd); return err == nil }

type Stats struct {
	Median, Mean, Min, Max, CV float64
	N                          int
}

func ComputeStats(samples []float64) (Stats, bool) {
	s := make([]float64, 0, len(samples))
	for _, x := range samples {
		if !math.IsNaN(x) && !math.IsInf(x, 0) {
			s = append(s, x)
		}
	}
	if len(s) == 0 {
		return Stats{}, false
	}
	sort.Float64s(s)
	n := len(s)
	sum := 0.0
	for _, x := range s {
		sum += x
	}
	mean := sum / float64(n)
	median := s[n/2]
	if n%2 == 0 {
		median = (s[n/2-1] + s[n/2]) / 2
	}
	sq := 0.0
	for _, x := range s {
		sq += (x - mean) * (x - mean)
	}
	cv := 0.0
	if mean != 0 {
		cv = math.Sqrt(sq/float64(n)) / math.Abs(mean) * 100
	}
	return Stats{Median: median, Mean: mean, Min: s[0], Max: s[n-1], CV: cv, N: n}, true
}

// Percentile expects an ascending slice.
func Percentile(sorted []float64, p float64) float64 {
	i := int(math.Floor(p / 100 * float64(len(sorted))))
	if i >= len(sorted) {
		i = len(sorted) - 1
	}
	return sorted[i]
}

// WriteFileAtomic writes via temp file + rename so a crash never leaves a half-written file.
func WriteFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.%d.tmp", path, os.Getpid())
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// CPUCounters returns the aggregate steal and total jiffies from /proc/stat.
func CPUCounters() (steal, total uint64) {
	f := strings.Fields(First(Read("/proc/stat")))
	for i, v := range f {
		if i == 0 {
			continue
		}
		n, _ := strconv.ParseUint(v, 10, 64)
		total += n
		if i == 8 {
			steal = n
		}
	}
	return
}

// MeminfoBytes parses /proc/meminfo into bytes.
func MeminfoBytes() map[string]int64 {
	m := map[string]int64{}
	for _, l := range strings.Split(Read("/proc/meminfo"), "\n") {
		k, v, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		f := strings.Fields(v)
		if len(f) == 0 {
			continue
		}
		n, _ := strconv.ParseInt(f[0], 10, 64)
		if len(f) > 1 && f[1] == "kB" {
			n *= 1024
		}
		m[k] = n
	}
	return m
}

// MemBudgetBytes is how much memory a test may safely use: 40% of what the OS and the cgroup say is available.
func MemBudgetBytes() int64 {
	m := MeminfoBytes()
	avail := m["MemAvailable"]
	if avail == 0 {
		avail = m["MemFree"]
	}
	if avail == 0 {
		avail = 512 << 20
	}
	max, err1 := strconv.ParseInt(First(Read("/sys/fs/cgroup/memory.max")), 10, 64)
	cur, err2 := strconv.ParseInt(First(Read("/sys/fs/cgroup/memory.current")), 10, 64)
	if err1 == nil && err2 == nil && max > 0 && max-cur < avail {
		avail = max - cur
	}
	b := int64(float64(avail) * 0.4)
	if b < 32<<20 {
		b = 32 << 20
	}
	return b
}

// Writable reports whether a file can be created in dir.
func Writable(dir string) bool {
	f, err := os.CreateTemp(dir, ".probe-w-*")
	if err != nil {
		return false
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
	return true
}
