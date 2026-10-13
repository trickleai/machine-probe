# machine-probe

参考 [machine-lab](https://github.com/SamdyChen2023/machine-lab) 用 Go 编写的单文件 CLI：采集机器环境指纹 + 跑一组基准测试，输出 **JSON 结果文件**，并用**内置的静态 HTML 模板**同时生成一份离线可打开的 **HTML 报告**。

- 零外部依赖：只用 Go 标准库，`CGO_ENABLED=0` 静态编译，拷一个二进制到目标机器即可运行
- HTML 模板通过 `go:embed` 编进二进制，报告是单文件（内嵌 CSS/JS/数据，无 CDN）
- 结果文档结构与 machine-lab 一致（`schema/id/label/profile/fingerprint/results[].metrics{median,mean,min,max,cv,n,samples}`），`report` / `compare` 也能读取 machine-lab 的结果文件

## 安装

```bash
go install github.com/trickleai/machine-probe@latest   # 需要 Go 1.22+
```

或者克隆后本地构建：

```bash
make build          # 生成 ./machine-probe
make test           # go vet + 单元测试
make dist           # 交叉编译 dist/machine-probe-linux-{amd64,arm64}
```

## 使用

```bash
# 快速档（约 40 秒），生成 result.json + result.html
./machine-probe run --profile quick --label my-vm --out result.json

# 完整档（约 4-5 分钟）：加线程扩展曲线、60s 持续负载、多目录磁盘、下载/上传吞吐
./machine-probe run --profile full --label my-vm --provider aws --price 0.17

# 只跑部分分组；离线环境关闭网络测试；分享前脱敏
./machine-probe run --groups cpu,memory --no-egress --redact

# 只看环境指纹
./machine-probe info            # 可读表格
./machine-probe info --json     # 完整 JSON

# 从已有 JSON（包括 machine-lab 的结果）重新生成 HTML 报告
./machine-probe report result.json --out result.html

# 多台机器对比（终端表格，按组几何平均打分，100 = 该组最优）
./machine-probe compare a.json b.json c.json
```

| 参数 | 说明 |
|---|---|
| `--profile quick\|full` | 测试档位，默认 `quick` |
| `--groups` | 逗号分隔：`cpu,memory,disk,workload,network` |
| `--label` / `--provider` / `--price` | 结果元数据，`--price` 为每小时美元价格 |
| `--out` | JSON 路径，默认 `machine-probe-<label>-<profile>.json` |
| `--html` / `--no-html` | HTML 路径（默认与 JSON 同名 `.html`）/ 不生成 HTML |
| `--no-egress` | 不访问外网（跳过 network 组和出口 IP 查询） |
| `--redact` | 去掉主机名、IP、MAC、内核命令行 |

运行中按 Ctrl-C 会停止并把已完成的结果写到 `*.partial.json`。

## 测试项

| 分组 | 测试 | 指标 |
|---|---|---|
| cpu | `cpu.go` SHA-256 + 素数筛 + fib 混合负载，单线程 / 全核 | `mp_single_score` `mp_multi_score` `mp_speedup` `mp_efficiency` `mp_sha256_mibs` |
| cpu | `cpu.sweep`（full）1→2×vCPU 线程扩展曲线 | `mp_peak_score` `mp_score_at_vcpus` `mp_scaling_at_vcpus` + 折线图 |
| cpu | `cpu.sustained`（full）60s 全核持续负载，每 5s 一个采样 | `mp_sustained_first/last` `mp_throttle_drop` `mp_cpu_steal` + 折线图 |
| memory | `mem.go` []float64 写 / 读 / 拷贝带宽 | `mp_mem_{write,read,copy}_gibs` |
| disk | `disk.seq@DIR` 1 MiB 块顺序写读（O_DIRECT，不支持时回退并注明）+ 4K 随机写 + fdatasync 延迟 | `mp_seq_{write,read}_mibs` `mp_sync4k_{iops,p50_us,p99_us}` |
| disk | `disk.smallfiles@DIR` 8 并发 4 KiB 小文件 create/stat/read/delete | `mp_files_*_per_s` |
| workload | `proc.spawn` /bin/true 进程创建速率；`wl.gzip` gzip-6 压缩/解压 | `mp_spawn_per_s` `mp_gzip_mibs` `mp_gunzip_mibs` `mp_gzip_ratio` |
| network | `net.latency` HTTPS HEAD 延迟（4 个站点各 5 次取中位数）、DNS、TCP connect | `mp_https_<host>_ms` `mp_dns_median_ms` `mp_tcp_connect_ms` |
| network | `net.throughput`（full）Cloudflare 50 MB 下载 / 10 MB 上传 | `mp_download_mbps` `mp_upload_mbps` |

| 档位 | 重复次数 | 预热 | CPU 时长 | 内存 | 磁盘 | 其它 |
|---|---|---|---|---|---|---|
| quick | 3 | 0 | 3s | 256 MiB | 128 MiB, 临时目录 | — |
| full | 5 | 1 | 5s | 512 MiB | 512 MiB, 临时目录 + $HOME + /dev/shm（按设备去重） | sweep、60s sustained、下载/上传 |

每个指标给出中位数和 CV（变异系数），CV > 25% 标记为 ⚠ noisy。

## 评分

每次 `run` 都会在 JSON 的 `score` 字段里给出一份 **0–100 的绝对评分**（HTML 报告顶部的“综合评分”），让单份结果也能看出好坏，不需要另一台机器对比：

- 每个计分指标有三个锚点：**偏弱 = 40 分、主流 = 65 分、高端 = 90 分**，中间按对数插值（降频、steal 等百分比指标按线性），超出范围外推并截断到 0–100。
- 评级：≥85 优秀，65–84 良好，40–64 一般，<40 偏弱。
- 分组分 = 组内指标加权平均；总分 = 分组加权平均（CPU 35%、磁盘 25%、内存 15%、负载 15%、网络 10%），缺少的分组按剩余权重重新归一。
- “主流”指当前一代 4 vCPU 通用型云主机（Graviton3 / Sapphire Rapids / Genoa + 普通云盘）。CPU 类指标以 Neoverse-V1（Graviton3）vCPU 为主流锚点校准；磁盘、网络锚点参考常见云盘限速和本地 NVMe 水平。**锚点是估算值**，用来建立直观感受，不是厂商排名；锚点表在 `internal/bench/references.go`，`score.reference` 记录了版本号。
- 磁盘只对第一个支持 O_DIRECT 的目录（真实块设备）计分，`/dev/shm` 等内存盘只展示不计分。
- 总分衡量的是“单位资源质量”，多核按“每 vCPU 性能”计分，不随 vCPU 数量线性增长；比较不同规格的机器时请同时看 `mp_multi_score`。
- `report` 重新生成 HTML 时会用当前锚点表重算评分；`compare` 同时输出绝对评分和相对评分（100 = 参与对比中最好的机器）。

## 注意

- 指标 key 统一加 `mp_` 前缀，与 machine-lab 的 `js_*` / `dd_*` 区分。两者算法不同，**不要直接比较同名含义的数值**；`compare` 只会对两边都有的 key 打分。
- CPU 分数是相对分，只用于机器之间横向比较（同一 machine-probe 版本）。
- 顺序写在某些虚拟磁盘上会比 `dd bs=1M oflag=direct` 高很多：Go 的大缓冲区通常由透明大页支撑、物理连续，而 `dd` 的缓冲区是 4K 页，在 `max_segments` 很小的 virtio 设备上会被拆成小请求。写入数据为随机内容且每个 4K 页唯一，避免存储层压缩/去重虚高。
- 磁盘目录不支持 O_DIRECT（如 tmpfs）时回退到缓冲 I/O + fdatasync，读速度此时主要反映页缓存，结果里会附注说明。

## 代码结构

```
main.go                      CLI：run / info / report / compare
internal/fingerprint/        环境指纹（/proc、/sys、lscpu、cgroup、虚拟化判定）+ 可读摘要
internal/bench/              测试定义、档位、执行与聚合、结果文档类型、评分锚点与计分
internal/compare/            多结果对比与打分
internal/report/             go:embed 的 report.html 模板 + 渲染
internal/util/               统计、原子写文件、/proc 读取、平台相关系统调用
```
