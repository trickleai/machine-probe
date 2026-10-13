package bench

import "context"

type Profile struct {
	Name             string
	Repeats, Warmup  int
	CPUSeconds       float64
	MemMB, DiskMB    int
	LatOps           int
	SmallFiles       int
	ZlibMB           int
	SpawnSeconds     float64
	Download         bool
	Sweep            bool
	SustainedSeconds int
	MultiDir         bool
}

var Profiles = map[string]Profile{
	"quick": {Name: "quick", Repeats: 3, Warmup: 0, CPUSeconds: 3, MemMB: 256, DiskMB: 128, LatOps: 300, SmallFiles: 1500, ZlibMB: 16, SpawnSeconds: 2},
	"full": {Name: "full", Repeats: 5, Warmup: 1, CPUSeconds: 5, MemMB: 512, DiskMB: 512, LatOps: 1500, SmallFiles: 8000, ZlibMB: 64, SpawnSeconds: 4,
		Download: true, Sweep: true, SustainedSeconds: 60, MultiDir: true},
}

var Groups = []string{"cpu", "memory", "disk", "workload", "network"}

// Env is passed to every test execution.
type Env struct {
	P        Profile
	Dir      string // disk tests only
	Progress func(string)
}

type Test struct {
	ID, Group, Title, Tool string
	Once                   bool // single run, no warmup (long or traffic-heavy tests)
	Run                    func(ctx context.Context, e *Env) (Rep, error)
}
