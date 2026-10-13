package fingerprint

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/trickleai/machine-probe/internal/util"
)

// Collect gathers the fingerprint. Every probe is best-effort: failures leave fields empty.
func Collect(ctx context.Context, egress bool) *Fingerprint {
	fp := &Fingerprint{Schema: 1, CollectedAt: time.Now().UTC().Format(time.RFC3339Nano), Host: host(), CPU: collectCPU(ctx), Memory: memory(), Storage: storage()}
	var wg sync.WaitGroup
	wg.Add(4)
	go func() { defer wg.Done(); fp.Virtualization = collectVirtualization(ctx, fp.CPU) }()
	go func() { defer wg.Done(); fp.Network = network(ctx, egress) }()
	go func() { defer wg.Done(); fp.Security = security(ctx) }()
	go func() { defer wg.Done(); fp.Tools = tools(ctx) }()
	wg.Wait()
	return fp
}

var secretArgRe = regexp.MustCompile(`(?i)token|key|secret|pass|auth`)

func host() Host {
	h := Host{Arch: runtime.GOARCH, OS: runtime.GOOS}
	h.Hostname, _ = os.Hostname()
	osr := map[string]string{}
	for _, l := range strings.Split(util.Read("/etc/os-release"), "\n") {
		if k, v, ok := strings.Cut(l, "="); ok {
			osr[k] = strings.Trim(v, `"`)
		}
	}
	h.OS = or(osr["PRETTY_NAME"], h.OS)
	h.Kernel = util.First(util.Read("/proc/sys/kernel/osrelease"))
	h.KernelVersion = util.First(util.Read("/proc/sys/kernel/version"))
	if f := strings.Fields(util.Read("/proc/uptime")); len(f) > 0 {
		up, _ := strconv.ParseFloat(f[0], 64)
		h.UptimeSec = int64(up + 0.5)
	}
	h.Clocksource = util.First(util.Read("/sys/devices/system/clocksource/clocksource0/current_clocksource"))
	h.Timezone = os.Getenv("TZ")
	if h.Timezone == "" {
		if t, err := os.Readlink("/etc/localtime"); err == nil {
			if _, z, ok := strings.Cut(t, "zoneinfo/"); ok {
				h.Timezone = z
			}
		}
	}
	if h.Timezone == "" {
		h.Timezone, _ = time.Now().Zone()
	}
	var keep []string
	for _, a := range strings.Fields(util.Read("/proc/cmdline")) {
		if !secretArgRe.MatchString(a) {
			keep = append(keep, a)
		}
	}
	h.Cmdline = strings.Join(keep, " ")
	return h
}

var thpRe = regexp.MustCompile(`\[(\w+)\]`)

func cgLimit(f string) Limit {
	v := util.First(util.Read("/sys/fs/cgroup/" + f))
	if v == "max" {
		return Limit{Unlimited: true}
	}
	if n, err := strconv.ParseInt(v, 10, 64); err == nil {
		return Limit{Bytes: &n}
	}
	return Limit{}
}

func memory() Memory {
	m := util.MeminfoBytes()
	thp := ""
	if s := thpRe.FindStringSubmatch(util.Read("/sys/kernel/mm/transparent_hugepage/enabled")); s != nil {
		thp = s[1]
	}
	return Memory{Total: m["MemTotal"], SwapTotal: m["SwapTotal"], HugepagesTotal: m["HugePages_Total"], THP: thp,
		CgroupMax: cgLimit("memory.max"), CgroupSwapMax: cgLimit("memory.swap.max"), Overcommit: util.First(util.Read("/proc/sys/vm/overcommit_memory"))}
}

type mount struct{ point, opts, fstype, source, superOpts string }

func mounts() []mount {
	var out []mount
	for _, l := range strings.Split(util.Read("/proc/self/mountinfo"), "\n") {
		pre, post, ok := strings.Cut(l, " - ")
		f, g := strings.Fields(pre), strings.Fields(post)
		if !ok || len(f) < 6 || len(g) < 2 {
			continue
		}
		out = append(out, mount{point: strings.ReplaceAll(f[4], `\040`, " "), opts: f[5], fstype: g[0], source: g[1], superOpts: strings.Join(g[2:], " ")})
	}
	return out
}

var overlayDirRe = regexp.MustCompile(`(lowerdir|upperdir|workdir)=[^,]*`)
var keptOpts = map[string]bool{"ro": true, "rw": true, "noatime": true, "relatime": true, "nosuid": true, "nodev": true, "noexec": true, "sync": true}

func fsFor(target string, mt []mount) Filesystem {
	fs := Filesystem{Path: target}
	var best *mount
	for i := range mt {
		m := &mt[i]
		prefix := m.point + "/"
		if m.point == "/" {
			prefix = "/"
		}
		if (target == m.point || strings.HasPrefix(target, prefix)) && (best == nil || len(m.point) >= len(best.point)) {
			best = m
		}
	}
	if best != nil {
		fs.MountPoint, fs.FSType, fs.Source = best.point, best.fstype, best.source
		var o []string
		for _, x := range strings.Split(best.opts, ",") {
			if keptOpts[x] {
				o = append(o, x)
			}
		}
		fs.Options = strings.Join(o, ",")
		if best.fstype == "overlay" {
			fs.Overlay = overlayDirRe.ReplaceAllString(best.superOpts, "$1=…")
		}
	}
	if st, ok := util.Statfs(target); ok {
		fs.TotalBytes, fs.FreeBytes, fs.Inodes, fs.InodesFree = &st.Total, &st.Free, &st.Inodes, &st.InodesFree
	}
	return fs
}

var skipBlockRe = regexp.MustCompile(`^(loop|ram|zram|nbd)`)
var schedRe = regexp.MustCompile(`\[(\S+)\]`)

func storage() Storage {
	mt := mounts()
	home, _ := os.UserHomeDir()
	s := Storage{Filesystems: []Filesystem{}, BlockDevices: []BlockDevice{}}
	seen := map[string]bool{}
	for _, t := range []string{"/", os.TempDir(), home, "/dev/shm", "/var"} {
		if t == "" || seen[t] || !util.Exists(t) {
			continue
		}
		seen[t] = true
		s.Filesystems = append(s.Filesystems, fsFor(t, mt))
	}
	ents, _ := os.ReadDir("/sys/block")
	for _, e := range ents {
		d := e.Name()
		if skipBlockRe.MatchString(d) {
			continue
		}
		b := func(f string) string { return util.First(util.Read(filepath.Join("/sys/block", d, f))) }
		size, _ := strconv.ParseInt(b("size"), 10, 64)
		lb, _ := strconv.Atoi(b("queue/logical_block_size"))
		sched := b("queue/scheduler")
		if m := schedRe.FindStringSubmatch(sched); m != nil {
			sched = m[1]
		}
		s.BlockDevices = append(s.BlockDevices, BlockDevice{Name: d, SizeBytes: size * 512, Rotational: b("queue/rotational") == "1", ReadOnly: b("ro") == "1", Scheduler: sched, Model: b("device/model"), LogicalBlock: lb})
	}
	return s
}

func network(ctx context.Context, egress bool) Network {
	n := Network{Interfaces: []Interface{}, DNS: []string{}}
	ifs, _ := net.Interfaces()
	for _, i := range ifs {
		if i.Flags&net.FlagLoopback != 0 {
			continue
		}
		it := Interface{Name: i.Name, MTU: i.MTU, MAC: i.HardwareAddr.String(), Addresses: []string{}}
		addrs, _ := i.Addrs()
		for _, a := range addrs {
			it.Addresses = append(it.Addresses, a.String())
			if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() == nil && ipn.IP.IsGlobalUnicast() {
				n.IPv6 = true
			}
		}
		n.Interfaces = append(n.Interfaces, it)
	}
	for _, l := range strings.Split(util.Read("/etc/resolv.conf"), "\n") {
		if f := strings.Fields(l); len(f) >= 2 && f[0] == "nameserver" {
			n.DNS = append(n.DNS, f[1])
		}
	}
	if egress {
		n.Egress = &Egress{Error: "lookup failed or blocked"}
		ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://ipinfo.io/json", nil)
		if resp, err := http.DefaultClient.Do(req); err == nil {
			var e Egress
			if json.NewDecoder(resp.Body).Decode(&e) == nil && e.IP != "" {
				n.Egress = &e
			}
			resp.Body.Close()
		}
	}
	return n
}

func boolPtr(b bool) *bool { return &b }

func security(ctx context.Context) Security {
	st := parseKV(util.Read("/proc/self/status"))
	probe := func(cmd string, args ...string) *bool {
		if !util.Has(cmd) {
			return nil
		}
		return boolPtr(util.Run(ctx, 8*time.Second, cmd, args...).OK)
	}
	kvm := false
	if f, err := os.OpenFile("/dev/kvm", os.O_RDWR, 0); err == nil {
		kvm = true
		f.Close()
	}
	var iou *bool
	if v := util.First(util.Read("/proc/sys/kernel/io_uring_disabled")); v != "" {
		iou = boolPtr(v == "0")
	}
	pid1 := util.First(util.Read("/proc/1/comm"))
	return Security{
		UID: os.Getuid(), PID1: pid1,
		Seccomp:    map[string]string{"0": "disabled", "1": "strict", "2": "filter"}[st["Seccomp"]],
		NoNewPrivs: st["NoNewPrivs"] == "1", CapEff: st["CapEff"], LSM: util.First(util.Read("/sys/kernel/security/lsm")),
		Checks: []Check{
			{"kvm", "/dev/kvm usable (nested virtualization)", boolPtr(kvm)},
			{"fuse", "/dev/fuse present", boolPtr(util.Exists("/dev/fuse"))},
			{"tun", "/dev/net/tun present", boolPtr(util.Exists("/dev/net/tun"))},
			{"userns", "unprivileged user namespaces", probe("unshare", "-Ur", "true")},
			{"sudo", "passwordless sudo", probe("sudo", "-n", "true")},
			{"raw", "raw sockets (ping)", probe("ping", "-c1", "-W1", "127.0.0.1")},
			{"docker", "Docker daemon socket", boolPtr(util.Exists("/var/run/docker.sock"))},
			{"io_uring", "io_uring enabled", iou},
			{"systemd", "systemd as PID 1", boolPtr(pid1 == "systemd")},
		},
	}
}

func tools(ctx context.Context) Tools {
	ver := func(cmd, re string, args ...string) *string {
		if !util.Has(cmd) {
			return nil
		}
		r := util.Run(ctx, 8*time.Second, cmd, args...)
		v := "installed"
		if m := regexp.MustCompile(re).FindStringSubmatch(r.Stdout + r.Stderr); m != nil {
			v = m[1]
		}
		return &v
	}
	return Tools{
		Go:       runtime.Version(),
		Node:     ver("node", `(v[\d.]+)`, "--version"),
		NPM:      ver("npm", `([\d.]+)`, "--version"),
		Python:   ver("python3", `([\d.]+)`, "--version"),
		GCC:      ver("gcc", `(?m)([\d.]+)\s*$`, "--version"),
		Git:      ver("git", `([\d.]+)`, "--version"),
		Fio:      ver("fio", `fio-([\d.]+)`, "--version"),
		Sysbench: ver("sysbench", `([\d.]+)`, "--version"),
		Docker:   ver("docker", `([\d.]+)`, "--version"),
	}
}

// Redact strips identifying data (hostname, IPs, MACs) before a result file is shared.
func Redact(fp *Fingerprint) *Fingerprint {
	b, _ := json.Marshal(fp)
	var x Fingerprint
	json.Unmarshal(b, &x)
	x.Host.Hostname, x.Host.Cmdline = "redacted", ""
	for i := range x.Network.Interfaces {
		x.Network.Interfaces[i].MAC = ""
		x.Network.Interfaces[i].Addresses = []string{}
	}
	x.Network.DNS = []string{}
	if x.Network.Egress != nil && x.Network.Egress.IP != "" {
		x.Network.Egress.IP = "redacted"
	}
	x.Redacted = true
	return &x
}
