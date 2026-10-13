package bench

// RefVersion identifies the reference table; bump it whenever anchors change so scores stay traceable.
const RefVersion = "2026.10"

// Ref anchors one metric to three quality levels. Weak, Typical and Excellent map to scores 40, 65 and 90;
// values in between are interpolated (on a log scale unless Linear) and extrapolated beyond, clamped to 0–100.
//
// Typical means a mainstream general-purpose cloud VM of the current generation (e.g. 4 vCPU on AWS
// Graviton3 / Intel Sapphire Rapids / AMD Genoa, gp3-class block storage). CPU-bound metrics are calibrated
// on a Neoverse-V1 (Graviton3) vCPU as "typical"; storage and network anchors come from published cloud
// limits and common local-NVMe figures. They are estimates for orientation, not a ranking of providers.
type Ref struct {
	Group, Test, Key string // Test is matched as a result-ID prefix ("disk.seq" matches "disk.seq@/tmp")
	Label, Hint      string
	Weak             float64
	Typical          float64
	Excellent        float64
	Linear           bool
	Weight           float64 // within the group; 0 means 1
}

// GroupWeights sets how much each group contributes to the overall score (renormalised over the groups present).
var GroupWeights = map[string]float64{"cpu": 0.35, "memory": 0.15, "disk": 0.25, "workload": 0.15, "network": 0.10}

var GroupTitles = map[string]string{"cpu": "CPU", "memory": "内存", "disk": "磁盘", "workload": "负载", "network": "网络"}

// Derived keys (not emitted by any test): mp_multi_per_vcpu = single × efficiency, mp_https_median_ms = median over hosts.
var References = []Ref{
	{Group: "cpu", Test: "cpu.go", Key: "mp_single_score", Label: "单核性能", Weight: 2,
		Hint: "单线程跑 SHA-256 + 素数筛 + 斐波那契的混合得分，决定单个请求、编译单文件、脚本执行等的快慢。",
		Weak: 8000, Typical: 14000, Excellent: 24000},
	{Group: "cpu", Test: "cpu.go", Key: "mp_multi_per_vcpu", Label: "多核每 vCPU 性能", Weight: 1.5,
		Hint: "全核并行时平均每个 vCPU 的得分（单核 × 扩展效率）。x86 的 vCPU 通常是超线程，此项会明显低于单核。",
		Weak: 5500, Typical: 10000, Excellent: 18000},
	{Group: "cpu", Test: "cpu.sustained", Key: "mp_throttle_drop", Label: "持续负载降频",
		Hint: "60 秒满载后性能相对开始时的下降比例。突发型实例（如 t 系列）积分耗尽后会大幅下降。",
		Weak: 15, Typical: 5, Excellent: 1, Linear: true},
	{Group: "cpu", Test: "cpu.sustained", Key: "mp_cpu_steal", Label: "CPU steal",
		Hint: "被宿主机其他租户抢走的 CPU 时间占比，高说明超卖严重、性能不稳定。",
		Weak: 8, Typical: 2, Excellent: 0.5, Linear: true},

	{Group: "memory", Test: "mem.go", Key: "mp_mem_copy_gibs", Label: "内存拷贝带宽", Weight: 2,
		Hint: "单线程大块内存拷贝速度，影响大数据处理、序列化、缓存类负载。",
		Weak: 6, Typical: 12, Excellent: 24},
	{Group: "memory", Test: "mem.go", Key: "mp_mem_read_gibs", Label: "内存读带宽",
		Hint: "单线程顺序读内存速度。", Weak: 7, Typical: 13, Excellent: 25},
	{Group: "memory", Test: "mem.go", Key: "mp_mem_write_gibs", Label: "内存写带宽",
		Hint: "单线程顺序写内存速度。", Weak: 5, Typical: 10, Excellent: 20},

	{Group: "disk", Test: "disk.seq", Key: "mp_seq_write_mibs", Label: "磁盘顺序写",
		Hint: "1 MiB 块直写磁盘（绕过缓存）。云盘常见 125–250 MiB/s，本地 NVMe 可达 1–3 GiB/s。",
		Weak: 80, Typical: 250, Excellent: 1500},
	{Group: "disk", Test: "disk.seq", Key: "mp_seq_read_mibs", Label: "磁盘顺序读",
		Hint: "1 MiB 块直读磁盘（绕过缓存），影响大文件加载、备份恢复。",
		Weak: 80, Typical: 250, Excellent: 1500},
	{Group: "disk", Test: "disk.seq", Key: "mp_sync4k_p99_us", Label: "4K 同步写延迟 p99", Weight: 1.5,
		Hint: "每次 4K 写入后 fdatasync 的尾延迟，直接决定数据库提交、日志落盘的快慢。云盘约 1–3 ms，本地 NVMe < 0.3 ms。",
		Weak: 15000, Typical: 3000, Excellent: 300},
	{Group: "disk", Test: "disk.seq", Key: "mp_sync4k_iops", Label: "4K 同步写 IOPS",
		Hint: "串行“写 4K + fdatasync”每秒能完成多少次，相当于单线程数据库每秒提交上限。",
		Weak: 200, Typical: 1000, Excellent: 8000},
	{Group: "disk", Test: "disk.smallfiles", Key: "mp_files_create_per_s", Label: "小文件创建",
		Hint: "8 并发创建 4 KiB 小文件的速度，影响解压、npm install、git checkout 等场景。",
		Weak: 10000, Typical: 40000, Excellent: 150000},
	{Group: "disk", Test: "disk.smallfiles", Key: "mp_files_delete_per_s", Label: "小文件删除",
		Hint: "8 并发删除小文件的速度，反映文件系统元数据性能。",
		Weak: 15000, Typical: 60000, Excellent: 200000},

	{Group: "workload", Test: "proc.spawn", Key: "mp_spawn_per_s", Label: "进程创建速率",
		Hint: "每秒能启动多少个 /bin/true，影响 shell 脚本、构建工具、CI 等频繁起进程的场景。容器沙箱（如 gVisor）会很低。",
		Weak: 500, Typical: 2000, Excellent: 6000},
	{Group: "workload", Test: "wl.gzip", Key: "mp_gzip_mibs", Label: "gzip 压缩",
		Hint: "Go 标准库 gzip-6 单线程压缩速度（Go 实现比 C zlib 慢，只适合横向比较）。",
		Weak: 5, Typical: 9, Excellent: 15},
	{Group: "workload", Test: "wl.gzip", Key: "mp_gunzip_mibs", Label: "gzip 解压",
		Hint: "Go 标准库 gzip 单线程解压速度。", Weak: 100, Typical: 190, Excellent: 320},

	{Group: "network", Test: "net.throughput", Key: "mp_download_mbps", Label: "下载带宽", Weight: 1.5,
		Hint: "从 Cloudflare 下载 50 MB 的速度。受出口带宽和到 Cloudflare 节点距离影响。",
		Weak: 100, Typical: 1000, Excellent: 5000},
	{Group: "network", Test: "net.throughput", Key: "mp_upload_mbps", Label: "上传带宽",
		Hint: "向 Cloudflare 上传 10 MB 的速度。", Weak: 50, Typical: 500, Excellent: 2500},
	{Group: "network", Test: "net.latency", Key: "mp_https_median_ms", Label: "HTTPS 访问延迟",
		Hint: "访问几个常用站点（GitHub、npm 等）HTTPS 请求的中位耗时，受地理位置影响很大。",
		Weak: 250, Typical: 80, Excellent: 20},
	{Group: "network", Test: "net.latency", Key: "mp_tcp_connect_ms", Label: "TCP 建连延迟",
		Hint: "与 1.1.1.1:443 建立 TCP 连接的耗时，约等于到最近 Cloudflare 节点的往返时间。",
		Weak: 80, Typical: 15, Excellent: 2},
}
