package fingerprint

import (
	"fmt"
	"strings"
)

type Row struct {
	Section string `json:"section"`
	Label   string `json:"label"`
	Value   string `json:"value"`
}

func HumanBytes(b float64) string {
	switch {
	case b >= 1<<40:
		return fmt.Sprintf("%.1f TiB", b/(1<<40))
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GiB", b/(1<<30))
	}
	return fmt.Sprintf("%.0f MiB", b/(1<<20))
}

func optBytes(p *uint64) string {
	if p == nil {
		return "?"
	}
	return HumanBytes(float64(*p))
}

func q[T any](p *T) string {
	if p == nil {
		return "?"
	}
	return fmt.Sprint(*p)
}

func dash(s string, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// Summarize flattens a fingerprint into human-readable rows for the report and the compare view.
func Summarize(fp *Fingerprint) []Row {
	if fp == nil {
		return nil
	}
	var rows []Row
	add := func(section, label, value string) { rows = append(rows, Row{section, label, dash(value, "—")}) }
	h, c, m, s, n, sec, t, v := fp.Host, fp.CPU, fp.Memory, fp.Storage, fp.Network, fp.Security, fp.Tools, fp.Virtualization

	add("Identity", "Hostname", h.Hostname)
	add("Identity", "OS", h.OS)
	add("Identity", "Kernel", fmt.Sprintf("%s (%s)", h.Kernel, h.Arch))
	add("Identity", "Clock source", h.Clocksource)

	add("Isolation", "Class", v.Label)
	add("Isolation", "Hypervisor", v.Hypervisor)
	add("Isolation", "PID 1", sec.PID1)

	add("CPU", "Model", c.Model)
	add("CPU", "Vendor / family-model-stepping", fmt.Sprintf("%s / %s-%s-%s", dash(c.Vendor, "?"), q(c.Family), q(c.ModelID), q(c.Stepping)))
	add("CPU", "vCPUs", fmt.Sprint(c.VCPUs))
	add("CPU", "Topology", fmt.Sprintf("%s socket × %s core × %s thread", q(c.Sockets), q(c.CoresPerSocket), q(c.ThreadsPerCore)))
	freq := ""
	if c.MHz != nil {
		freq = fmt.Sprintf("%.0f MHz", *c.MHz)
		if c.MaxMHz != nil {
			freq += fmt.Sprintf(" (max %.0f)", *c.MaxMHz)
		}
	}
	add("CPU", "Frequency", freq)
	add("CPU", "Caches (L1d / L2 / L3)", fmt.Sprintf("%s / %s / %s", dash(c.Caches.L1d, "?"), dash(c.Caches.L2, "?"), dash(c.Caches.L3, "?")))
	var isa []string
	have := map[string]bool{}
	for _, f := range c.Flags {
		have[f] = true
	}
	for _, f := range []string{"sse4_2", "avx", "avx2", "avx512f", "amx_tile", "aes", "sha_ni", "bmi2", "asimd", "sha2", "sha512", "sve", "sve2", "atomics"} {
		if have[f] {
			isa = append(isa, f)
		}
	}
	add("CPU", "ISA extensions", strings.Join(isa, " "))
	quota := "no cgroup v2 cpu.max"
	if c.CgroupQuota != nil {
		quota = "unlimited"
		if !c.CgroupQuota.Unlimited {
			quota = fmt.Sprintf("%g CPUs", c.CgroupQuota.CPUs)
		}
	}
	add("CPU", "cgroup CPU quota", quota)
	if c.Throttle != nil {
		add("CPU", "Throttled periods", fmt.Sprintf("%d / %d", c.Throttle.Throttled, c.Throttle.Periods))
	} else {
		add("CPU", "Throttled periods", "")
	}
	if len(c.Vulnerabilities) > 0 {
		var mit, vul, na int
		for _, x := range c.Vulnerabilities {
			switch {
			case strings.HasPrefix(x, "Mitigation"):
				mit++
			case strings.HasPrefix(x, "Vulnerable"):
				vul++
			case strings.HasPrefix(x, "Not affected"):
				na++
			}
		}
		add("CPU", "Vulnerability status", fmt.Sprintf("%d mitigated, %d vulnerable, %d n/a", mit, vul, na))
	} else {
		add("CPU", "Vulnerability status", "")
	}

	add("Memory", "Total", HumanBytes(float64(m.Total)))
	swap := "none"
	if m.SwapTotal > 0 {
		swap = HumanBytes(float64(m.SwapTotal))
	}
	add("Memory", "Swap", swap)
	cgMax := "n/a"
	if m.CgroupMax.Unlimited {
		cgMax = "unlimited"
	} else if m.CgroupMax.Bytes != nil {
		cgMax = HumanBytes(float64(*m.CgroupMax.Bytes))
	}
	add("Memory", "cgroup memory.max", cgMax)
	add("Memory", "Transparent huge pages", m.THP)

	for _, f := range s.Filesystems {
		if f.Path != "/" && f.Path != "/tmp" && f.Path != "/dev/shm" {
			continue
		}
		val := fmt.Sprintf("%s · %s total, %s free", dash(f.FSType, "?"), optBytes(f.TotalBytes), optBytes(f.FreeBytes))
		if f.Inodes != nil && *f.Inodes > 0 {
			val += fmt.Sprintf(" · %d inodes", *f.Inodes)
		}
		add("Storage", f.Path, val)
	}
	var bd []string
	for _, d := range s.BlockDevices {
		x := d.Name + " " + HumanBytes(float64(d.SizeBytes))
		if d.ReadOnly {
			x += " ro"
		}
		bd = append(bd, x)
	}
	add("Storage", "Block devices", strings.Join(bd, ", "))

	eg := "not queried"
	if n.Egress != nil {
		eg = n.Egress.Error
		if n.Egress.IP != "" {
			var loc []string
			for _, x := range []string{n.Egress.City, n.Egress.Region, n.Egress.Country} {
				if x != "" {
					loc = append(loc, x)
				}
			}
			eg = fmt.Sprintf("%s · %s · %s", n.Egress.IP, strings.Join(loc, ", "), n.Egress.Org)
		}
	}
	add("Network", "Egress", eg)
	var ifs []string
	for _, i := range n.Interfaces {
		ifs = append(ifs, fmt.Sprintf("%s mtu %d", i.Name, i.MTU))
	}
	add("Network", "Interfaces", strings.Join(ifs, ", "))
	add("Network", "IPv6", map[bool]string{true: "yes", false: "no"}[n.IPv6])
	add("Network", "DNS servers", strings.Join(n.DNS, ", "))

	add("Security", "seccomp", sec.Seccomp)
	add("Security", "uid", fmt.Sprint(sec.UID))
	add("Security", "LSM", sec.LSM)
	for _, k := range sec.Checks {
		val := "unknown"
		if k.OK != nil {
			val = map[bool]string{true: "yes", false: "no"}[*k.OK]
		}
		add("Capabilities", k.Label, val)
	}

	rt := "go " + dash(t.Go, "?")
	for _, x := range []struct {
		name string
		v    *string
	}{{"node", t.Node}, {"python", t.Python}, {"gcc", t.GCC}} {
		if x.v != nil {
			rt += ", " + x.name + " " + *x.v
		}
	}
	add("Tools", "Runtime", rt)
	native := func(p *string) string {
		if p == nil {
			return "✗"
		}
		return *p
	}
	add("Tools", "Native bench tools", fmt.Sprintf("fio %s, sysbench %s", native(t.Fio), native(t.Sysbench)))
	return rows
}
