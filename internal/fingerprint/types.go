package fingerprint

import (
	"encoding/json"
	"strconv"
)

type Fingerprint struct {
	Schema         int            `json:"schema"`
	CollectedAt    string         `json:"collectedAt"`
	Host           Host           `json:"host"`
	Virtualization Virtualization `json:"virtualization"`
	CPU            CPU            `json:"cpu"`
	Memory         Memory         `json:"memory"`
	Storage        Storage        `json:"storage"`
	Network        Network        `json:"network"`
	Security       Security       `json:"security"`
	Tools          Tools          `json:"tools"`
	Redacted       bool           `json:"redacted,omitempty"`
}

type Host struct {
	Hostname      string `json:"hostname"`
	OS            string `json:"os"`
	Kernel        string `json:"kernel"`
	KernelVersion string `json:"kernelVersion"`
	Arch          string `json:"arch"`
	UptimeSec     int64  `json:"uptimeSec"`
	Clocksource   string `json:"clocksource"`
	Timezone      string `json:"timezone"`
	Cmdline       string `json:"cmdline"`
}

type Virtualization struct {
	Class      string            `json:"class"`
	Label      string            `json:"label"`
	Hypervisor string            `json:"hypervisor"`
	DMI        map[string]string `json:"dmi"`
	Evidence   []string          `json:"evidence"`
	Note       string            `json:"note"`
}

type CPU struct {
	Model              string            `json:"model"`
	Vendor             string            `json:"vendor"`
	Family             *int              `json:"family"`
	ModelID            *int              `json:"modelId"`
	Stepping           *int              `json:"stepping"`
	Microcode          string            `json:"microcode,omitempty"`
	Arch               string            `json:"arch"`
	VCPUs              int               `json:"vcpus"`
	OnlineCPUs         int               `json:"onlineCpus"`
	Sockets            *int              `json:"sockets"`
	CoresPerSocket     *int              `json:"coresPerSocket"`
	ThreadsPerCore     *int              `json:"threadsPerCore"`
	MHz                *float64          `json:"mhz"`
	MaxMHz             *float64          `json:"maxMhz"`
	BogoMIPS           *float64          `json:"bogomips"`
	Caches             Caches            `json:"caches"`
	NUMANodes          *int              `json:"numaNodes"`
	Flags              []string          `json:"flags"`
	Vulnerabilities    map[string]string `json:"vulnerabilities"`
	CgroupQuota        *CgroupQuota      `json:"cgroupQuota"`
	Throttle           *Throttle         `json:"throttle"`
	HypervisorVendor   string            `json:"hypervisorVendor"`
	VirtualizationType string            `json:"virtualizationType"`
}

type Caches struct {
	L1d string `json:"l1d"`
	L1i string `json:"l1i"`
	L2  string `json:"l2"`
	L3  string `json:"l3"`
}

type CgroupQuota struct {
	QuotaUs   int64   `json:"quotaUs,omitempty"`
	PeriodUs  int64   `json:"periodUs,omitempty"`
	CPUs      float64 `json:"cpus,omitempty"`
	Unlimited bool    `json:"unlimited,omitempty"`
}

type Throttle struct {
	Periods       int64 `json:"periods"`
	Throttled     int64 `json:"throttled"`
	ThrottledUsec int64 `json:"throttledUsec"`
}

type Memory struct {
	Total          int64  `json:"total"`
	SwapTotal      int64  `json:"swapTotal"`
	HugepagesTotal int64  `json:"hugepagesTotal"`
	THP            string `json:"thp"`
	CgroupMax      Limit  `json:"cgroupMax"`
	CgroupSwapMax  Limit  `json:"cgroupSwapMax"`
	Overcommit     string `json:"overcommit"`
}

// Limit is a cgroup limit: "max" (unlimited), a byte count, or unknown (null).
type Limit struct {
	Unlimited bool
	Bytes     *int64
}

func (l Limit) MarshalJSON() ([]byte, error) {
	switch {
	case l.Unlimited:
		return []byte(`"max"`), nil
	case l.Bytes != nil:
		return []byte(strconv.FormatInt(*l.Bytes, 10)), nil
	}
	return []byte("null"), nil
}

func (l *Limit) UnmarshalJSON(b []byte) error {
	*l = Limit{}
	var s string
	if json.Unmarshal(b, &s) == nil {
		l.Unlimited = s == "max"
		return nil
	}
	var n int64
	if json.Unmarshal(b, &n) == nil {
		l.Bytes = &n
	}
	return nil
}

type Storage struct {
	Filesystems  []Filesystem  `json:"filesystems"`
	BlockDevices []BlockDevice `json:"blockDevices"`
}

type Filesystem struct {
	Path       string  `json:"path"`
	MountPoint string  `json:"mountPoint"`
	FSType     string  `json:"fstype"`
	Source     string  `json:"source"`
	Options    string  `json:"options"`
	Overlay    string  `json:"overlay,omitempty"`
	TotalBytes *uint64 `json:"totalBytes"`
	FreeBytes  *uint64 `json:"freeBytes"`
	Inodes     *uint64 `json:"inodes"`
	InodesFree *uint64 `json:"inodesFree"`
}

type BlockDevice struct {
	Name         string `json:"name"`
	SizeBytes    int64  `json:"sizeBytes"`
	Rotational   bool   `json:"rotational"`
	ReadOnly     bool   `json:"readOnly"`
	Scheduler    string `json:"scheduler"`
	Model        string `json:"model"`
	LogicalBlock int    `json:"logicalBlock"`
}

type Network struct {
	Interfaces []Interface `json:"interfaces"`
	DNS        []string    `json:"dns"`
	IPv6       bool        `json:"ipv6"`
	Egress     *Egress     `json:"egress"`
}

type Interface struct {
	Name      string   `json:"name"`
	MTU       int      `json:"mtu"`
	MAC       string   `json:"mac,omitempty"`
	Addresses []string `json:"addresses"`
}

type Egress struct {
	IP       string `json:"ip,omitempty"`
	City     string `json:"city,omitempty"`
	Region   string `json:"region,omitempty"`
	Country  string `json:"country,omitempty"`
	Org      string `json:"org,omitempty"`
	Timezone string `json:"timezone,omitempty"`
	Error    string `json:"error,omitempty"`
}

type Security struct {
	UID        int     `json:"uid"`
	PID1       string  `json:"pid1"`
	Seccomp    string  `json:"seccomp"`
	NoNewPrivs bool    `json:"noNewPrivs"`
	CapEff     string  `json:"capEff"`
	LSM        string  `json:"lsm"`
	Checks     []Check `json:"checks"`
}

type Check struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	OK    *bool  `json:"ok"`
}

type Tools struct {
	Go       string  `json:"go"`
	Node     *string `json:"node"`
	NPM      *string `json:"npm"`
	Python   *string `json:"python"`
	GCC      *string `json:"gcc"`
	Git      *string `json:"git"`
	Fio      *string `json:"fio"`
	Sysbench *string `json:"sysbench"`
	Docker   *string `json:"docker"`
}
