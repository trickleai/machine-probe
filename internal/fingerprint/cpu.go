package fingerprint

import (
	"context"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/trickleai/machine-probe/internal/util"
)

func parseKV(text string) map[string]string {
	m := map[string]string{}
	for _, l := range strings.Split(text, "\n") {
		if k, v, ok := strings.Cut(l, ":"); ok && strings.TrimSpace(k) != "" {
			m[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return m
}

func or(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func intPtr(s string) *int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return nil
	}
	return &n
}

func floatPtr(s string) *float64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return nil
	}
	return &f
}

// onlineCPUs parses a cpu list such as "0-3,6".
func onlineCPUs() int {
	n := 0
	for _, part := range strings.Split(util.First(util.Read("/sys/devices/system/cpu/online")), ",") {
		a, b, isRange := strings.Cut(part, "-")
		x, err := strconv.Atoi(a)
		if err != nil {
			continue
		}
		if !isRange {
			n++
			continue
		}
		if y, err := strconv.Atoi(b); err == nil {
			n += y - x + 1
		}
	}
	if n == 0 {
		n = runtime.NumCPU()
	}
	return n
}

var cgroupPathRe = regexp.MustCompile(`(?m)^0::(.*)$`)

func collectCPU(ctx context.Context) CPU {
	l := parseKV(util.Run(ctx, 5*time.Second, "lscpu").Stdout)
	ci := parseKV(strings.SplitN(util.Read("/proc/cpuinfo"), "\n\n", 2)[0])
	c := CPU{
		Model:              or(l["Model name"], ci["model name"]),
		Vendor:             or(l["Vendor ID"], ci["vendor_id"]),
		Family:             intPtr(or(l["CPU family"], ci["cpu family"])),
		ModelID:            intPtr(or(l["Model"], ci["model"])),
		Stepping:           intPtr(or(l["Stepping"], ci["stepping"])),
		Microcode:          ci["microcode"],
		Arch:               or(l["Architecture"], runtime.GOARCH),
		VCPUs:              runtime.NumCPU(),
		OnlineCPUs:         onlineCPUs(),
		Sockets:            intPtr(l["Socket(s)"]),
		CoresPerSocket:     intPtr(l["Core(s) per socket"]),
		ThreadsPerCore:     intPtr(l["Thread(s) per core"]),
		MHz:                floatPtr(or(l["CPU MHz"], ci["cpu MHz"])),
		MaxMHz:             floatPtr(l["CPU max MHz"]),
		BogoMIPS:           floatPtr(or(l["BogoMIPS"], ci["bogomips"], ci["BogoMIPS"])),
		Caches:             Caches{L1d: l["L1d cache"], L1i: l["L1i cache"], L2: l["L2 cache"], L3: l["L3 cache"]},
		NUMANodes:          intPtr(l["NUMA node(s)"]),
		Flags:              strings.Fields(or(l["Flags"], ci["flags"], ci["Features"])),
		Vulnerabilities:    map[string]string{},
		HypervisorVendor:   l["Hypervisor vendor"],
		VirtualizationType: l["Virtualization type"],
	}
	if c.Flags == nil {
		c.Flags = []string{}
	}
	const vdir = "/sys/devices/system/cpu/vulnerabilities"
	if ents, err := os.ReadDir(vdir); err == nil {
		for _, e := range ents {
			c.Vulnerabilities[e.Name()] = util.First(util.Read(vdir + "/" + e.Name()))
		}
	}

	cgPath := ""
	if m := cgroupPathRe.FindStringSubmatch(util.Read("/proc/self/cgroup")); m != nil {
		cgPath = strings.TrimSpace(m[1])
	}
	cg := func(f string) string {
		return or(util.Read("/sys/fs/cgroup"+cgPath+"/"+f), util.Read("/sys/fs/cgroup/"+f))
	}
	if q := strings.Fields(util.First(cg("cpu.max"))); len(q) > 0 {
		if q[0] == "max" {
			c.CgroupQuota = &CgroupQuota{Unlimited: true}
		} else if len(q) == 2 {
			quota, _ := strconv.ParseInt(q[0], 10, 64)
			period, _ := strconv.ParseInt(q[1], 10, 64)
			if period > 0 {
				c.CgroupQuota = &CgroupQuota{QuotaUs: quota, PeriodUs: period, CPUs: float64(quota) / float64(period)}
			}
		}
	}
	st := map[string]int64{}
	for _, line := range strings.Split(cg("cpu.stat"), "\n") {
		if f := strings.Fields(line); len(f) == 2 {
			st[f[0]], _ = strconv.ParseInt(f[1], 10, 64)
		}
	}
	if _, ok := st["nr_throttled"]; ok {
		c.Throttle = &Throttle{Periods: st["nr_periods"], Throttled: st["nr_throttled"], ThrottledUsec: st["throttled_usec"]}
	}
	return c
}

var virtioCmdRe = regexp.MustCompile(`virtio_mmio\.device=`)
var containerCgRe = regexp.MustCompile(`docker|kubepods|containerd|crio|lxc|libpod`)
var gvisorRe = regexp.MustCompile(`(?i)gvisor|runsc`)
var wslRe = regexp.MustCompile(`(?i)microsoft|wsl`)

func collectVirtualization(ctx context.Context, c CPU) Virtualization {
	var ev []string
	dmi := map[string]string{}
	var dmiVals []string
	for _, n := range []string{"sys_vendor", "product_name", "bios_vendor", "board_vendor"} {
		if v := util.First(util.Read("/sys/class/dmi/id/" + n)); v != "" {
			dmi[n] = v
			dmiVals = append(dmiVals, v)
		}
	}
	hvFlag := false
	for _, f := range c.Flags {
		if f == "hypervisor" {
			hvFlag = true
		}
	}
	if hvFlag {
		ev = append(ev, `CPU flag "hypervisor" is set`)
	}
	if c.HypervisorVendor != "" {
		ev = append(ev, "lscpu hypervisor vendor: "+c.HypervisorVendor)
	}
	if len(dmi) > 0 {
		ev = append(ev, "DMI: "+strings.Join(dmiVals, " / "))
	} else {
		ev = append(ev, "no DMI/SMBIOS data exposed")
	}
	hvType := util.First(util.Read("/sys/hypervisor/type"))
	if hvType != "" {
		ev = append(ev, "/sys/hypervisor/type: "+hvType)
	}
	virtioMmio := virtioCmdRe.MatchString(util.Read("/proc/cmdline"))
	if ents, err := os.ReadDir("/sys/devices/platform"); err == nil && !virtioMmio {
		for _, e := range ents {
			if strings.HasPrefix(e.Name(), "virtio-mmio") {
				virtioMmio = true
				break
			}
		}
	}
	if virtioMmio {
		ev = append(ev, "virtio-mmio devices present (typical of microVMs such as Firecracker / Cloud Hypervisor)")
	}
	pci, _ := os.ReadDir("/sys/bus/pci/devices")
	if len(pci) > 0 {
		ev = append(ev, "PCI bus has devices")
	} else {
		ev = append(ev, "no PCI devices")
	}

	release := util.First(util.Read("/proc/sys/kernel/osrelease"))
	dmesg := util.Run(ctx, 5*time.Second, "dmesg").Stdout
	if len(dmesg) > 4000 {
		dmesg = dmesg[:4000]
	}
	version := util.Read("/proc/version")
	gvisor := gvisorRe.MatchString(dmesg+version) || release == "4.4.0"
	if gvisor {
		if release == "4.4.0" {
			ev = append(ev, "kernel release is the fixed value 4.4.0 reported by gVisor")
		} else {
			ev = append(ev, "gVisor marker in dmesg or /proc/version")
		}
	}

	var signals []string
	if util.Exists("/.dockerenv") {
		signals = append(signals, "/.dockerenv")
	}
	if util.Exists("/run/.containerenv") {
		signals = append(signals, "/run/.containerenv")
	}
	if containerCgRe.MatchString(util.Read("/proc/1/cgroup")) {
		signals = append(signals, "container runtime in /proc/1/cgroup")
	}
	if os.Getenv("KUBERNETES_SERVICE_HOST") != "" {
		signals = append(signals, "Kubernetes env vars")
	}
	if wslRe.MatchString(version) {
		ev = append(ev, "WSL kernel string")
	}
	for _, s := range signals {
		ev = append(ev, "container signal: "+s)
	}

	microHint := virtioMmio || (len(pci) == 0 && len(dmi) == 0)
	vmm := or(c.HypervisorVendor, "unknown VMM")
	v := Virtualization{Hypervisor: or(c.HypervisorVendor, hvType), DMI: dmi, Evidence: ev, Note: "Heuristic classification from the signals listed in evidence."}
	switch {
	case gvisor:
		v.Class, v.Label = "gvisor", "gVisor (user-space kernel)"
	case hvFlag && len(signals) > 0:
		kind := "VM"
		if microHint {
			kind = "microVM"
		}
		v.Class, v.Label = "container-in-vm", "Container inside a "+kind+" ("+vmm+")"
	case hvFlag && microHint:
		v.Class, v.Label = "microvm", "microVM ("+vmm+")"
	case hvFlag:
		v.Class, v.Label = "vm", "Virtual machine ("+or(c.HypervisorVendor, dmi["sys_vendor"], "hypervisor")+")"
	case len(signals) > 0:
		v.Class, v.Label = "container", "Container (shared host kernel)"
	default:
		v.Class, v.Label = "bare-metal-or-unknown", "Bare metal or undetectable"
	}
	return v
}
