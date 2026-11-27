# etcd 3.7 coordinator 实施交付记录

**记录版本 v0.5 · 2026-11-28。** 本文是 [新建 Codis 集群使用 etcd 3.7 的实施契约](etcd_3_7_coordinator_contract_zh.md)（v1.3，2026-11-28）的配套交付记录：契约写承诺，本文写 P0 实测证据、决策、实施进度与 A0–A7 验收状态。结论先行：**P0 前置调研四项交付齐备，A0 通过；接线方案选定"组件内构造"（D2），ha/shake 不引入 gRPC 且产物逐字节不变；第 1–5 步与 A1–A7 已完成（A6 于 2026-11-28 在华为云三台 Linux ECS 的三成员 etcd 3.7.4 上通过（2026-11-28 签收））。**

## 1. 文档定位

| 项目 | 说明 |
| --- | --- |
| 契约 | [etcd_3_7_coordinator_contract_zh.md](etcd_3_7_coordinator_contract_zh.md) v1.3（2026-11-28），定义对外行为、实施顺序（第 6 节）与验收表 A0–A7（第 7 节）。本记录不修改契约。 |
| 本记录 | 固化 P0 调研（契约第 0 节四项）、验收决策 D1–D10、契约修订建议 R1–R6、实施进度与验收状态。所有数字来自隔离工作区 `codis-etcd-p0/` 的报告与产物，或本次实测。 |
| 状态口径 | **完成 / 进行中 / 未开始**用于实施步骤；**通过 / 待验 / 待真机**用于 A0–A7。本机（macOS）结果只算本地回归，不替代 A6 的 Linux 真机门槛（对齐 [validation/README.md](validation/README.md) 的口径）。 |
| 权威性 | 实施结论以本次验收为准；材料间数字若不一致，以 `report/p0-deps.md`、`report/module-diff.md`、`report/p0-runtime.md` 及产物元数据为准并注明差异（见 §3.7 脚注）。 |
| 未决事项 | 无。R1–R6 均已确认并回写契约 v1.3（见 §7）；A6 已于 2026-11-28 签收（见 §8.2 A6、§10）。 |

## 2. 环境与版本

| 项 | 值 | 证据 |
| --- | --- | --- |
| Codis 基线 | `c635ade0424a1448fb251f32b88d64ca7cf9a555`（= 主仓 HEAD~1；HEAD `3a5890d4` 仅多契约文档），P0 开始时工作树干净 | `env/candidate-head.txt`、`env/main-repo-status.txt`（空） |
| 构建环境 | Go 1.27.4 darwin/arm64，CGO_ENABLED=1，GOPROXY=`https://goproxy.cn`，默认 build tags；`-tags cgo_jemalloc` 另测 | `env/env.txt`、`env/goenv.txt` |
| etcd 服务端与工具 | 3.7.4，由本地仓库 `~/work/etcd` 的标签 `v3.7.4`（`2e8a5c99e4982c8d4c521be15364ea4c5c68a489`）经 `git archive` 导出后自建 `etcd`/`etcdctl`/`etcdutl`；`etcd --version` 报 Go 1.27.2；**未使用 release 包** | `etcd-bin/`、`env/build-commands.txt` |
| 隔离工作区 | `~/work/codis-etcd-p0/`：`candidate/`（隔离克隆）、`snapshots/`、`builds/`、`report/`、`env/`、`runtime-probe/`；主仓与 etcd 仓零改动 | `env/main-repo-status.txt`、`env/etcd-repo-status.txt` |
| 模块指令 | `go 1.26.1` / `toolchain go1.27.4`；两条相对 replace（`spinlock/jemalloc-go`、`hdt3213/rdb`）未变 | `snapshots/go-get/gomod.diff` |
| protoc | 未安装，也不需要：client/v3 的 pb 生成代码随上游发布 | `env/env.txt` |

## 3. P0-A：依赖与构建结论

### 3.1 结论速览

| 项目 | 基线 `c635ade0` | 候选（组件内构造，选定） |
| --- | --- | --- |
| build list 模块数 | 133 | **182**（新增 49、升级 15、版本相同 117、消失 0） |
| 六入口可达的包行数 | dashboard/proxy/admin 101、fe 33、ha 100、shake 91 | 253 / 253 / 253 / 185、**ha 100、shake 91** |
| 六入口可达的模块数 | 35 / 35 / 35 / 19 / 35 / 33 | 50 / 50 / 50 / 34 / **35 / 33** |
| 是否引入 gRPC | 否 | 仅 dashboard/fe/admin/proxy；**ha、shake 完全不引入** |
| ha/shake 产物字节 | — | **与基线逐字节相同**（sha256 一致，默认与 cgo_jemalloc 两组） |

结论：契约第 0 节“具体客户端在需要连接的组件中构造”的方案成立；工厂内构造（否决方案）会把 65 个 gRPC 包拖进全部六个入口，ha 包行 100→251、shake 91→242。

### 3.2 新增模块 49 个

| 模块 | 版本 |
| --- | --- |
| `cel.dev/expr` | v0.25.2 |
| `cloud.google.com/go/auth` | v0.18.2 |
| `cloud.google.com/go/compute/metadata` | v0.9.0 |
| `github.com/GoogleCloudPlatform/opentelemetry-operations-go/detectors/gcp` | v1.33.0 |
| `github.com/antihax/optional` | v1.0.0 |
| `github.com/cncf/xds/go` | v0.0.0-20260202195803-dba9d589def2 |
| `github.com/coreos/go-semver` | v0.3.1 |
| `github.com/envoyproxy/go-control-plane` | v0.14.0 |
| `github.com/envoyproxy/go-control-plane/envoy` | v1.37.0 |
| `github.com/envoyproxy/go-control-plane/ratelimit` | v0.1.0 |
| `github.com/envoyproxy/protoc-gen-validate` | v1.3.3 |
| `github.com/felixge/httpsnoop` | v1.0.4 |
| `github.com/go-jose/go-jose/v4` | v4.1.4 |
| `github.com/go-logr/logr` | v1.4.3 |
| `github.com/go-logr/stdr` | v1.2.2 |
| `github.com/golang/glog` | v1.2.5 |
| `github.com/google/s2a-go` | v0.1.9 |
| `github.com/googleapis/enterprise-certificate-proxy` | v0.3.11 |
| `github.com/googleapis/gax-go/v2` | v2.17.0 |
| `github.com/grpc-ecosystem/go-grpc-middleware/providers/prometheus` | v1.1.0 |
| `github.com/grpc-ecosystem/go-grpc-middleware/v2` | v2.3.3 |
| `github.com/grpc-ecosystem/grpc-gateway/v2` | v2.29.0 |
| `github.com/munnerz/goautoneg` | v0.0.0-20191010083416-a7dc8b61c822 |
| `github.com/planetscale/vtprotobuf` | v0.6.1-0.20240319094008-0393e58bdf10 |
| `github.com/rogpeppe/fastuuid` | v1.2.0 |
| `github.com/rogpeppe/go-internal` | v1.14.1 |
| `github.com/spiffe/go-spiffe/v2` | v2.7.0 |
| `go.etcd.io/etcd/api/v3` | v3.7.4 |
| `go.etcd.io/etcd/client/pkg/v3` | v3.7.4 |
| `go.etcd.io/etcd/client/v3` | v3.7.4 |
| `go.opentelemetry.io/auto/sdk` | v1.2.1 |
| `go.opentelemetry.io/contrib/detectors/gcp` | v1.44.0 |
| `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp` | v0.61.0 |
| `go.opentelemetry.io/otel` | v1.44.0 |
| `go.opentelemetry.io/otel/metric` | v1.44.0 |
| `go.opentelemetry.io/otel/sdk` | v1.44.0 |
| `go.opentelemetry.io/otel/sdk/metric` | v1.44.0 |
| `go.opentelemetry.io/otel/trace` | v1.44.0 |
| `go.uber.org/goleak` | v1.3.0 |
| `go.uber.org/multierr` | v1.11.0 |
| `go.uber.org/zap` | v1.27.1 |
| `go.yaml.in/yaml/v2` | v2.4.3 |
| `go.yaml.in/yaml/v3` | v3.0.4 |
| `golang.org/x/oauth2` | v0.36.0 |
| `gonum.org/v1/gonum` | v0.17.0 |
| `google.golang.org/genproto/googleapis/api` | v0.0.0-20260803160001-6ac0973c030d |
| `google.golang.org/genproto/googleapis/rpc` | v0.0.0-20260803160001-6ac0973c030d |
| `google.golang.org/grpc` | v1.83.2 |
| `sigs.k8s.io/yaml` | v1.6.0 |

> 装进 build list ≠ 链接进二进制：dashboard/proxy/admin/fe 产物内 prometheus 与 opentelemetry 包数均为 0。

### 3.3 升级模块 15 个

| 模块 | 基线 → 候选 |
| --- | --- |
| `google.golang.org/protobuf` | v1.34.1 → **v1.36.11** |
| `github.com/golang/protobuf` | v1.5.0 → **v1.5.4** |
| `github.com/prometheus/client_golang` | v1.4.0 → **v1.23.2** |
| `github.com/prometheus/client_model` | v0.2.0 → v0.6.2 |
| `github.com/prometheus/common` | v0.9.1 → v0.67.5 |
| `github.com/prometheus/procfs` | v0.0.8 → v0.16.1 |
| `github.com/coreos/go-systemd/v22` | v22.3.3-0.20220203105225-a9a7ef127534 → v22.7.0 |
| `golang.org/x/mod` | v0.33.0 → v0.41.0 |
| `golang.org/x/sync` | v0.19.0 → v0.24.0 |
| `golang.org/x/telemetry` | v0.0.0-20260209163413-e7419c687ee4 → v0.0.0-20260924152758-ed294f943157 |
| `golang.org/x/tools` | v0.42.0 → v0.51.0 |
| `github.com/godbus/dbus/v5` | v5.0.4 → v5.1.0 |
| `github.com/kr/pretty` | v0.1.0 → v0.3.1 |
| `github.com/stretchr/testify` | v1.10.0 → v1.11.1 |
| `gopkg.in/check.v1` | v1.0.0-20200227125254-8fa46927fb4f → v1.0.0-20201130134442-10cb98267c6c |

### 3.4 版本相同模块（117 个）

契约点名模块全部未变：

| 模块 | 版本 | 结论 |
| --- | --- | --- |
| `golang.org/x/net` | v0.62.0 | 未变 |
| `golang.org/x/sys` | v0.49.0 | 未变 |
| `golang.org/x/text` | v0.43.0 | 未变 |
| `github.com/dustin/go-humanize` | v1.0.1 | 未变 |
| `github.com/cespare/xxhash/v2` | v2.3.0 | 未变 |

- etcd client/v3 依赖链上版本未变的共 37 个（含上表 5 项）。代表项：`go.uber.org/atomic v1.11.0`（基线已由 go-redis 引入，无冲突、无升级）、`golang.org/x/crypto v0.58.0`、`x/term v0.47.0`、`google/uuid v1.6.0`、`gogo/protobuf v1.1.1`、`pkg/errors v0.9.1`、`gopkg.in/yaml.v2/v3`、`julienschmidt/httprouter v1.2.0`、`beorn7/perks v1.0.1`、`matttproud/golang_protobuf_extensions v1.0.1`、`sirupsen/logrus v1.4.2`。
- 其余 80 个（含两条本地 replace）见 `report/module-diff.md` 表 3c，不在此重复。

### 3.5 可移除模块（三种口径，均为解析结论）

| 口径 | 模块数 | 模块 | 说明 |
| --- | --- | --- | --- |
| 契约算法：`go mod graph` 可达集差 | **2** | `github.com/hashicorp/consul/sdk`、`github.com/hashicorp/go-version` | 基线 `reach(zk ∪ consul)=91`、`reach(main)=130`；候选图复算 92/179，独占仍为 2。`go mod graph` 含所有模块测试依赖，主模块经其它模块的测试依赖几乎可达全部模块，故独占集偏小。 |
| 包图口径：六入口屏蔽驱动包后不可达 | **15** | `github.com/armon/go-metrics`、`github.com/fatih/color`、`github.com/go-viper/mapstructure/v2`、`github.com/go-zookeeper/zk`、`github.com/hashicorp/consul/api`、`github.com/hashicorp/errwrap`、`github.com/hashicorp/go-cleanhttp`、`github.com/hashicorp/go-hclog`、`github.com/hashicorp/go-immutable-radix`、`github.com/hashicorp/go-multierror`、`github.com/hashicorp/go-rootcerts`、`github.com/hashicorp/golang-lru`、`github.com/hashicorp/serf`、`github.com/mitchellh/go-homedir`、`golang.org/x/exp` | 从六个 `cmd/*` 出发、不得经过 `pkg/models/{zk,consul}` 包做 BFS；**退役删除候选以此为准**。证据 `report/raw/build-exclusive-baseline.txt`。 |
| 交叉核对：`go mod why -m all` 代表路径经过驱动 | **34** | 4b 的 15 个 + `github.com/davecgh/go-spew`、`github.com/google/btree`、`github.com/google/go-cmp`、`github.com/hashicorp/consul/sdk`、`github.com/hashicorp/go-msgpack`、`github.com/hashicorp/go-sockaddr`、`github.com/hashicorp/go-uuid`、`github.com/hashicorp/go-version`、`github.com/hashicorp/memberlist`、`github.com/mattn/go-isatty`、`github.com/miekg/dns`、`github.com/pascaldekloe/goe`、`github.com/pkg/errors`、`github.com/pmezard/go-difflib`、`github.com/sean-/seed`、`github.com/stretchr/objx`、`github.com/stretchr/testify`、`golang.org/x/net`、`gopkg.in/yaml.v3` | 4b ⊆ 4c（交集 15）。仅 4c 有的 19 个仍存在不经驱动的路径，`why` 只打印一条代表路径，故 4c 仅作旁证。 |

差异原因：Codis `go.mod` 在 pruned graph 下把 Consul 的传递依赖都列为 `// indirect` require，使它们在模块图上“可从主模块直达”，所以契约算法的独占集只有 2。**三者都是解析结论，最终以第 3 步退役旧驱动后的快照复核；实际 `droprequire` 清单与退役后选版见 §3.11（15 个候选全部删除，build list 182→126）。**

### 3.6 逐入口包图与两种接线

包行数（baseline / 组件内构造 / 工厂内构造，默认 tags）：

| 入口 | 基线 | 组件内构造（选定） | 工厂内构造（否决） |
| --- | --- | --- | --- |
| dashboard | 101 | 253 | 252 |
| proxy | 101 | 253 | 252 |
| admin | 101 | 253 | 252 |
| fe | 33 | 185 | 184 |
| ha | 100 | **100** | 251 |
| shake | 91 | **91** | 242 |

可达模块数（基线 → 两种候选，二者相同）：dashboard/proxy/admin 35→50、fe 19→34、ha 35→35、shake 33→33。接线方式不改变模块集合，只改变包与链接位置；工厂内构造比组件内构造少 1 行（253→252），差额是组件方案新增的接入包自身，不影响结论。

目标包族出现次数（基线 / 组件内构造 / 工厂内构造）：

| 入口 | grpc | protobuf(google) | protobuf(golang) | prometheus | zap | opentelemetry | etcd |
| --- | --- | --- | --- | --- | --- | --- | --- |
| dashboard | 0/65/65 | 0/38/38 | 0/1/1 | 0/0/0 | 0/10/10 | 0/0/0 | 0/18/18 |
| proxy | 0/65/65 | 0/38/38 | 0/1/1 | 0/0/0 | 0/10/10 | 0/0/0 | 0/18/18 |
| admin | 0/65/65 | 0/38/38 | 0/1/1 | 0/0/0 | 0/10/10 | 0/0/0 | 0/18/18 |
| fe | 0/65/65 | 0/38/38 | 0/1/1 | 0/0/0 | 0/10/10 | 0/0/0 | 0/18/18 |
| ha | 0/**0**/65 | 0/**0**/38 | 0/**0**/1 | 0/0/0 | 0/**0**/10 | 0/0/0 | 0/**0**/18 |
| shake | 0/**0**/65 | 0/**0**/38 | 0/**0**/1 | 0/0/0 | 0/**0**/10 | 0/0/0 | 0/**0**/18 |

- **组件内构造（选定）**：最短链 `cmd/{dashboard,proxy,admin,fe} → pkg/coordinator/etcdp0 → go.etcd.io/etcd/client/v3 → google.golang.org/grpc`（到 zap 同为 4 跳；`pkg/coordinator/etcdp0` 是 P0 探针包，正式落地按 D2 换成组件入口或其专属包）；ha、shake 不存在任何到达目标包族的路径。
- **工厂内构造（否决）**：`cmd/ha → pkg/models → client/v3 → grpc`；shake 经 `cmd/shake → pkg/migration → pkg/proxy → pkg/models → client/v3`。ha/shake 各 +65 gRPC、+38 `google.golang.org/protobuf`、+1 `golang/protobuf`、+10 zap、+18 etcd 包。

### 3.7 产物字节与构建成本

darwin/arm64，默认 tags，基线与候选同 flags（`-trimpath -buildvcs=false` + Makefile tags/ldflags + 冻结的 `bin/version.ldflags`），独立重建 sha256 可复现：

| 入口 | 基线字节 | 候选字节 | 字节差 | 增幅 | sha256 一致 |
| --- | --- | --- | --- | --- | --- |
| dashboard | 16,346,114 | 35,213,330 | +18,867,216 | +115.4% | 否 |
| proxy | 15,904,914 | 34,696,514 | +18,791,600 | +118.1% | 否 |
| admin | 12,969,362 | 26,052,962 | +13,083,600 | +100.8% | 否 |
| fe | 11,994,418 | 24,428,354 | +12,433,936 | +103.7% | 否 |
| ha | 13,330,178 | 13,330,178 | **0** | 0% | **是** |
| shake | 14,268,066 | 14,268,066 | **0** | 0% | **是** |

`go version -m` 的 dep 条数：

| 入口 | 基线 | 候选 | 变化 |
| --- | --- | --- | --- |
| dashboard | 34 | 49 | +15 |
| proxy | 34 | 49 | +15 |
| admin | 34 | 49 | +15 |
| fe | 18 | 33 | +15 |
| ha | 34 | 34 | +0 |
| shake | 32 | 32 | +0 |

> 交接摘要曾把 proxy 记为 18→33、admin 记为 34→34；按 `report/p0-deps.md` §7、`report/module-diff.md` “dep 条数”表与 `builds/{baseline,candB}/*.goversion.txt` 实测，proxy 与 admin 均为 **34→49**，本记录以实测为准。

构建成本接受口径见 D10：dashboard +18.9MB、proxy +18.8MB、admin +13.1MB、fe +12.4MB，ha/shake 为 0。

### 3.8 cgo_jemalloc 与交叉编译

`-tags cgo_jemalloc`（六入口 × 基线/候选共 12 个产物）**12/12 构建成功**：`jemalloc` 符号链接、`private_namespace.h`、`jemalloc_defs.h` 均已入库，无需 `make jemalloc-config`，构建日志为空。

| 入口 | 基线字节 | 候选字节 | 字节差 |
| --- | --- | --- | --- |
| dashboard | 17,354,546 | 36,139,170 | +18,784,624 |
| proxy | 16,880,258 | 35,671,826 | +18,791,568 |
| admin | 13,928,258 | 27,094,386 | +13,166,128 |
| fe | 11,994,418 | 24,428,354 | +12,433,936 |
| ha | 14,289,042 | 14,289,042 | **0**（sha256 一致） |
| shake | 15,243,442 | 15,243,442 | **0**（sha256 一致） |

> fe 的默认与 cgo_jemalloc 两组字节相同（11,994,418 → 24,428,354）但 sha256 不同，源于 `-tags` 引起的 build ID 差异；两组的包图完全一致。

交叉编译 `GOOS=linux GOARCH={amd64,arm64} CGO_ENABLED=0`、无 tags：24/24 成功（2 平台 × 6 入口 × 基线/候选）。基线交叉编译在 `git archive` 导出的 `/tmp/p0-baseline-src` 内完成，避免污染候选。

| 平台 | 入口 | 基线字节 | 候选字节 | 字节差 |
| --- | --- | --- | --- | --- |
| linux/amd64 | dashboard | 16,906,229 | 35,938,334 | +19,032,105 |
| linux/amd64 | proxy | 16,399,983 | 35,415,316 | +19,015,333 |
| linux/amd64 | admin | 13,340,468 | 26,571,745 | +13,231,277 |
| linux/amd64 | fe | 12,272,519 | 24,641,176 | +12,368,657 |
| linux/amd64 | ha | 13,752,177 | 13,752,177 | **0**（sha256 一致） |
| linux/amd64 | shake | 14,779,148 | 14,779,148 | **0**（sha256 一致） |
| linux/arm64 | dashboard | 15,796,518 | 33,503,310 | +17,706,792 |
| linux/arm64 | proxy | 15,311,267 | 32,947,279 | +17,636,012 |
| linux/arm64 | admin | 12,363,400 | 24,911,636 | +12,548,236 |
| linux/arm64 | fe | 11,354,839 | 23,119,338 | +11,764,499 |
| linux/arm64 | ha | 12,791,413 | 12,791,413 | **0**（sha256 一致） |
| linux/arm64 | shake | 13,752,088 | 13,752,088 | **0**（sha256 一致） |

**待验**：linux + `cgo_jemalloc` 的交叉/真机构建未做（需 cgo 交叉工具链），转交发布验证；证据 `builds/{baseline,candB}/cross-results.tsv` 与 `linux-{amd64,arm64}/`。

### 3.9 测试依赖

- 基线/候选 `go list -deps -test` 与不带的差集**都只有 2 条**（`cmd/<X>.test` 自身），候选未引入任何测试独占包。
- `prometheus/{client_golang,client_model,common,procfs}` 与 `grpc-ecosystem/go-grpc-middleware/providers/prometheus` 停留在 requirement 层面：`go mod why -m` 为“main module does not need module”；仅随 MVS 抬高版本（client_golang v1.23.2 等），无包参与构建。
- `go.opentelemetry.io/otel{,+metric,+trace,+sdk,+sdk/metric}`、`auto/sdk`、`go.uber.org/goleak` 仅被 gRPC 的测试包需要，正常构建不链接；六入口包图与 `-deps -test` 结果内均为 0 包。
- 账面成本：build list 与 `go.sum`（363→395 行）变大，对产物字节贡献为 0。模块侧精简需另验，本轮未执行。

### 3.10 偏差与未执行

| 项 | 状态 | 说明 |
| --- | --- | --- |
| `go mod tidy` | 未执行（D9 禁止全量 tidy） | etcd 三模块在候选 `go.mod` 中仍带 `// indirect`；`-mod=readonly` 下可构建，第 1–2 步按 D9 手工归一。 |
| `bin/version.ldflags` | 人工冻结 | 隔离克隆无 `bin/`（`.gitignore`）；基线/候选共用同一份（sha256 `f27a9460…`），保证字节可比。 |
| 真连 etcd 的探针、TLS 文件装配、用户名密码真机验证 | 未执行/待验 | 指正式接入实现（第 1 步）的探针与文件装配；P0 单成员实验见 §4。 |
| 单元测试、`codis-server` C 目标 | 未执行/未构建 | 本轮只做编译、链接与运行行为验证；C 目标与 Go 依赖无关。 |
| 4a/4b/4c 删除候选 | **已复核（§3.11）** | 第 3 步退役后重跑快照：15 个候选包图命中 0、`go mod why` 均“main module does not need”，全部 `droprequire`；另有 41 个旧驱动专属传递模块自动消失 |

### 3.11 退役后依赖复核（第 3 步 3-B，不跑 `go mod tidy`）

**方法。** 删除前快照：`go list -m all` = **182** 个模块（含主模块），`go mod graph` **621** 行；`go.mod`/`go.sum` 副本存 `/tmp`。候选集取 §3.5 的包图口径 15 个（已含 `go-zookeeper/zk`、`hashicorp/consul/api`）。逐项取证三条：`go list -mod=readonly -deps -test ./cmd/... ./pkg/...`（538 行包图，生产 + 测试）、`go mod why -m <mod>`、`go mod graph` 的入边（来源模块）。删除只用 `go mod edit -droprequire=<mod>`，分两批（2 个直接依赖 + 13 个 `// indirect`），每批后跑 `go build ./...` 与 `go list -mod=readonly -deps -test ./cmd/... ./pkg/... >/dev/null`，**两批均一次通过，没有恢复任何 require**。

| 模块（全部为退役前主模块的显式 require） | 版本 | 包图命中 | `go mod why -m` | 图入边来源（删除前） | 处置 |
| --- | --- | --- | --- | --- | --- |
| `github.com/go-zookeeper/zk` | v1.0.4 | 0 | main module does not need | 仅 main | **droprequire**（直接依赖） |
| `github.com/hashicorp/consul/api` | v1.34.3 | 0 | main module does not need | 仅 main | **droprequire**（直接依赖） |
| `github.com/armon/go-metrics` | v0.4.1 | 0 | main module does not need | main、consul/api、serf、memberlist | **droprequire** |
| `github.com/fatih/color` | v1.16.0 | 0 | main module does not need | main、consul/api、go-hclog、serf、mitchellh/cli | **droprequire** |
| `github.com/go-viper/mapstructure/v2` | v2.4.0 | 0 | main module does not need | main、consul/api | **droprequire** |
| `github.com/hashicorp/errwrap` | v1.1.0 | 0 | main module does not need | main、consul/api、go-multierror | **droprequire** |
| `github.com/hashicorp/go-cleanhttp` | v0.5.2 | 0 | main module does not need | main、consul/api、go-retryablehttp | **droprequire** |
| `github.com/hashicorp/go-hclog` | v1.5.0 | 0 | main module does not need | main、consul/api | **droprequire** |
| `github.com/hashicorp/go-immutable-radix` | v1.3.1 | 0 | main module does not need | main、armon/go-metrics、consul/api、memberlist | **droprequire** |
| `github.com/hashicorp/go-multierror` | v1.1.1 | 0 | main module does not need | main、consul/api、memberlist、serf、mitchellh/cli、posener/complete | **droprequire** |
| `github.com/hashicorp/go-rootcerts` | v1.0.2 | 0 | main module does not need | main、consul/api | **droprequire** |
| `github.com/hashicorp/golang-lru` | v0.5.4 | 0 | main module does not need | main、consul/api、go-immutable-radix | **droprequire** |
| `github.com/hashicorp/serf` | v0.10.1 | 0 | main module does not need | main、consul/api | **droprequire** |
| `github.com/mitchellh/go-homedir` | v1.1.0 | 0 | main module does not need | main、consul/api、go-rootcerts | **droprequire** |
| `golang.org/x/exp` | v0.0.0-20260218203240 | 0 | main module does not need | main、consul/api | **droprequire** |

**15/15 可删**：包图命中全为 0，`go mod why` 全为“main module does not need”，入边全部来自 main 与同一批退役模块；因此 §3.5 的 4a（2 个）含在 4b 内，4c 的旁证结论已被直接验证取代。**无“未能确认而保留”的候选**。

**消失模块（56 个，含 15 个 droprequire）。** 除 15 个显式删除外，另有 41 个只能经旧驱动族的 `go.mod` 入边到达、主模块未显式 require，删除后自动退出 build list（未单独 `droprequire`）：`DataDog/datadog-go`、`alecthomas/template`、`alecthomas/units`、`armon/circbuf`、`armon/go-radix`、`bgentry/speakeasy`、`circonus-labs/circonus-gometrics`、`circonus-labs/circonusllhist`、`go-kit/kit`、`go-logfmt/logfmt`、`gogo/protobuf`、`google/btree`、`hashicorp/consul/sdk`、`hashicorp/go-msgpack`、`hashicorp/go-retryablehttp`、`hashicorp/go-sockaddr`、`hashicorp/go-syslog`、`hashicorp/go-uuid`、`hashicorp/go-version`、`hashicorp/logutils`、`hashicorp/mdns`、`hashicorp/memberlist`、`julienschmidt/httprouter`、`konsorten/go-windows-terminal-sequences`、`kr/logfmt`、`matttproud/golang_protobuf_extensions`、`miekg/dns`、`mitchellh/cli`、`mitchellh/mapstructure`、`mwitkow/go-conntrack`、`pascaldekloe/goe`、`posener/complete`、`ryanuber/columnize`、`sean-/seed`、`sirupsen/logrus`、`tv42/httpunix`、`golang.org/x/tools/go/expect`、`golang.org/x/tools/go/packages/packagestest`、`golang.org/x/xerrors`、`gopkg.in/alecthomas/kingpin.v2`、`gopkg.in/yaml.v2`。

**build list 与选版。** 182 → **126**（主模块 + 125）。与 P0 基线（c635ade0，133）对比净减 **7**，算式可对齐：`133（含旧驱动链）− 56（旧驱动专属链）+ 49（etcd 链新增）= 126`；etcd 三模块、grpc、zap、genproto、oauth2、xds/otel/goleak 等 49 个新增模块全部保留。**没有版本回退**：§3.3 的 15 个升级项逐一保持（protobuf v1.36.11、golang/protobuf v1.5.4、prometheus/client_golang v1.23.2、client_model v0.6.2、common v0.67.5、procfs v0.16.1、go-systemd v22.7.0、x/mod v0.41.0、x/sync v0.24.0、x/telemetry/x/tools、godbus v5.1.0、kr/pretty v0.3.1、testify v1.11.1、check.v1 固定版）；`go list -m all` 前后逐行比对显示 0 个新增模块、0 个版本变化，仅 56 个模块消失。`go mod graph` 621 → **345** 行。最终 `go.mod`：10 个直接 require（6 个原有 + `client/v3`、`api/v3`、`zap`、`grpc` + 本地 replace 两项保留）、47 个 `// indirect`；两条本地 `replace` 未变。

**有依据的保留项。** 保留项均不满足“只为旧驱动服务”：

| 保留项 | 引入链 / 证据 |
| --- | --- |
| `mattn/go-colorable`、`mattn/go-isatty` | `go mod why -m` = `pkg/shake/log → rs/zerolog → go-colorable → go-isatty`；`rs/zerolog` 仍在生产包图（2 包）。不是旧驱动专属。 |
| `prometheus/*`、`grpc-ecosystem/go-grpc-middleware/providers/prometheus`、`go.opentelemetry.io/otel*`、`go.uber.org/goleak`、`xds`/`cel.dev/expr`、`oauth2`、`gonum` 等 | 图入边 `go.etcd.io/etcd/client/v3@v3.7.4`（及 grpc/api 的 go.mod）；`go mod why -m` 多为“main module does not need”，属 requirement-only，仅参与 MVS 选版（§3.9 结论不变），产物包数为 0。 |
| `stretchr/testify` 及其 `objx`/`spew`/`difflib`、`google/go-cmp`、`yaml.v3`、`kr/pretty`、`check.v1`、`godbus/dbus` | `go mod why -m testify` = `go.etcd.io/etcd/api/v3/etcdserverpb.test → testify/assert`：etcd/grpc 的测试依赖；主模块自身无新增测试独占包（§3.9）。 |
| `benbjohnson/tmpl`、`Masterminds/{semver,sprig}`、`aokoli/goutils`、`huandu/xstrings`、`imdario/mergo`、`bytedance/sonic` 族、`json-iterator/go`、`modern-go/*`、`spf13/pflag`、`go-stack/stack` | 均为 **P0 基线已存在的 requirement-only 条目**：退役后图入边只剩 main、包图命中 0，与本项目迁移无关；按“不顺手清理/不升级”约定不动。 |
| `hdt3213/rdb`、`spinlock/jemalloc-go` | 两条本地 replace，保留。 |

**go.sum。** 仅执行 `go mod verify`（all modules verified）与构建/测试；未出现缺失校验条目，因此 **`go.sum` 未改动（395 行）**，全程未跑 `go mod tidy`。已删模块的校验行仍留在 `go.sum`（账面冗余，不影响构建；由后续发布前受控清理决定）。

**验证证据（本机 darwin/arm64，CGO_ENABLED=1，默认 tags）。**

| 检查 | 命令 | 结果 |
| --- | --- | --- |
| 格式 | `gofmt -l ./cmd ./pkg/models ./pkg/proxy ./pkg/topom ./cmd/internal` | 输出为空 |
| 编译 | `go build ./...`；六入口 `go build ./cmd/{dashboard,proxy,admin,fe,ha,shake}` | 全部成功 |
| 静态检查 | `go vet ./cmd/... ./pkg/...` | 0 问题 |
| 包图 | `go list -mod=readonly -deps -test ./cmd/... ./pkg/...` | 538 行，退出 0 |
| 单元/包测试 | `go test ./cmd/... ./pkg/... -count=1` | 全部 ok |
| 项目测试入口 | `make gotest`（依赖 `codis-deps`：mkdir + `bash version`，不依赖 codis-server） | **成功，real 18.80s**，28 个包 ok、0 FAIL |
| etcd 集成 | `CODIS_ETCD_SERVER_BINARY=<P0 etcd> go test ./pkg/models/etcd ./pkg/proxy/etcdonline -count=1` | ok 20.35s / 14.75s |
| A5 集成 | 同上 `go test ./pkg/proxy -run 'A5\|Online'` | PASS 10.79s（明细见 §8.2 A5） |
| `cgo_jemalloc` 发布口径 | `go build -tags cgo_jemalloc -trimpath -buildvcs=false -ldflags "$(cat bin/version.ldflags)" -o /tmp/... ./cmd/{proxy,dashboard}` | **2/2 成功**，proxy 5.15s、dashboard 2.38s；`go version -m` dep 条数均为 **35**（P0 候选 49，差额为旧驱动链依赖），无 zookeeper/consul/hashicorp 条目；产物未产生新的未跟踪文件（`bin/` 属 `.gitignore`） |

**最终包图分类**（`go list -mod=readonly -deps -test ./cmd/... ./pkg/...`，538 行）：`go-zookeeper` **0**、`hashicorp/consul` **0**、`go.etcd.io/etcd/client/v3` 4、`google.golang.org/grpc` 65、`google.golang.org/protobuf` 38、`go.uber.org/zap` 10。逐入口（非标准库包/模块）：dashboard 234/35、proxy 235/35、admin 234/35、fe 167/19、ha 81/**20**、shake 73/19；`go list -deps ./cmd/ha`、`./cmd/shake` 的 etcd/gRPC 包数均为 **0**（P0 基线 ha/shake 模块数 35/33，退役后 20/19，差额即旧驱动链）。

## 4. P0-B：单成员 etcd 3.7.4 运行行为

实验条件：固定版外部单成员 etcd 3.7.4（源码自建），HTTP 实例 127.0.0.1:2379、HTTPS/mTLS 实例 127.0.0.1:23790（auth 复用 HTTPS 实例），故障 TCP 代理 23801（丢响应/扣请求）、黑洞 23802。探针为 `runtime-probe/probe`（`e1`–`e6` 子命令）。以下结论**只覆盖单成员**，多成员/quorum 属 A6。表中“支撑条款”指契约章节，E1–E6 编号沿用 `report/p0-runtime.md`。

### 4.1 观测表

| # | 实验 | 命令/方法要点 | 观测 | 支撑条款 |
| --- | --- | --- | --- | --- |
| 1 | E1 构造非阻塞 | `clientv3.New` 后立即 Get，11 个 E1 用例 | New 0.05–0.36ms，端点全不可用亦然 | 契约 §3 `dial_timeout` |
| 2 | E1 全不可用超时 | 仅坏地址/静默黑洞，`request_timeout` 5s | 首个 Get 5001–5004ms 返回 | §3 `request_timeout` |
| 3 | E1 `dial_timeout` 不约束 RPC | `-dial-timeout 1s` | 首个 Get 仍 5001.54ms；源码 `client/v3/config.go:36-44` 只用于 getToken/版本检查，并派生 keepalive 首超时 `DialTimeout+1s` | §3（修订建议 R1） |
| 4 | E1 单坏地址 | `good + 坏地址` 两种顺序 | Get 1–2ms 成功，坏地址不拖慢正常请求 | §3 多 endpoint 优先级 |
| 5 | E1 错误分类 | 全不可用错误类型 | `*context.deadlineExceededError`；`status.FromError=false`；`errors.Is(ctx.DeadlineExceeded)=true` | §2 三类结果、D4 |
| 6 | E2 TLS 握手 | 明文→TLS、无 CA、系统根、缺客户端证书、rogue 证书 | 调用方一律 5s deadline；底层原因只在 SDK 默认 zap Warn（stderr） | §3 TLS 规则、D5 |
| 7 | E2 文件级错误 | 证书路径缺失 / 非 PEM | fail-fast：0.01ms / 0.47ms | §3 文件不可读/无法解析报错 |
| 8 | E2 凭据错误 | `Config.Username/Password` 错误 | `clientv3.New` 阶段 64.88ms 返回 `etcdserver: authentication failed, invalid user ID or password` | §3 鉴权、D5 |
| 9 | E2 auth 开启后无凭据 | mTLS 但不带用户名 | `rpctypes.EtcdError` `etcdserver: permission denied`（不是“用户名为空”） | §3 明确拒绝判定 |
| 10 | E2 密码不泄漏 | 19 字符随机密码；日志/报告/二进制 `strings`/命令行 `grep` | 全部 no match；服务端错误文案固定不含密码 | §3 密码不得暴露 |
| 11 | E2 macOS 文案差异 | 系统根 vs 显式空 CA | `x509: ... not standards compliant`（Security 框架）vs `unknown authority`（纯 Go） | §3 / A1（Linux 文案待验 U9） |
| 12 | E3 丢响应 vs 扣请求 | 故障代理 `lose-resp`（已提交）/`drop-req`（未提交） | 客户端结果完全相同：`Unavailable "error reading from server: EOF"`，170–172ms；键状态只能靠新 client 回读区分（found=true/false） | §2 结果未知、D4 |
| 13 | E3 写不重放 | `-once` 故障 | Put/Txn 第二次连接无任何请求帧，客户端首次即返回；写至多一次 | §2 禁止自动重放 Put/Delete |
| 14 | E3 Get 可重试 | `lose-resp` Get | 5s 内 28 次重连后 ctx 超时；unary 重试上限 100、退避 25ms±10%、受 ctx 约束 | §2 回读需新的有界 context |
| 15 | E4 续约节奏 | TTL=30 持续 70s；TTL=5 持续 20s | 响应间隔稳定 10.02s（TTL/3，500ms 检查粒度）；TTL=5 为 2.0s | §4 首版 TTL 固定 30s |
| 16 | E4 失效信号 | ctx cancel / `Client.Close()` / 服务端 kill -9 | channel 关闭是唯一信号、无错误文本：3ms / 0.3ms；服务端 kill -9：channel 在 KeepAlive 启动后 7.01s 关闭（距最后一次成功响应约 5.00s，当次 TTL=5s；SDK 以最后一次响应携带的 TTL 设定截止、每秒扫描） | §4 续约失效感知、D6 |
| 17 | E4 Close 不撤销 lease | TTL=30，Close 后独立 client 轮询 | 键存活到 grant+30.3s；显式 `Revoke` 10.66ms 后键立即消失 | §4 关闭须显式撤销、D6 |
| 18 | E4 崩溃与停机 | client kill -9；停机 > TTL 后重启 | 键在最后续约+TTL 左右消失（TTL=30 约 +30s，服务端按 TTL 过期）；旧 lease `TTL=-1`，KeepAlive channel 立即关闭 | 契约 §4 崩溃依赖服务端到期 |
| 19 | E5 默认日志 | 正常路径 / 拒绝连接 / TLS 失败 | 正常 stdout/stderr 0 字节；失败 stderr 4 行/2052 字节（TLS 2296 字节），来源 `retry_interceptor.go:68` warn | 契约 §3 日志、D5(b)、D7 |
| 20 | E5 默认指标 | 创建前后 + Put/Get/Txn/Close 的指标族数 | 均 34、diff 为空；v3.7.4 clientv3 无 `metrics.go`，不默认注册 | 契约 §0 指标行、R4 |
| 21 | E5 空闲流量 | 30s 空闲经故障代理逐帧记录 | 双向 0 帧；客户端 keepalive `Time=infinity`，服务端 ping 默认 2h | 契约 §3 普通转发不得新增每请求 gRPC |
| 22 | E6 调用计数 | 链式拦截器在 SDK 重试内层计数 | Put/Get/Txn/Delete 各恰 1 次；`New` 0 次；3xGet=3 | 契约 §3 每请求 gRPC 调用数 |

### 4.2 未验项

| # | 未验项 | 原因与去向 |
| --- | --- | --- |
| U1 | 多成员集群、leader 切换、quorum 丢失下的错误分类 | 本轮按契约只做单成员；转 A6 |
| U2 | 服务端 lease 过期时 KeepAlive 流的服务端错误文案 | 客户端只见到 channel 关闭；需服务端侧日志/抓包 |
| U3 | 请求已发出但服务端未提交（apply 前崩溃）的分支 | 单成员 kill -9 会 WAL 重放，无法稳定构造 |
| U4 | 真实静默丢包（半开连接）下的 deadline 行为 | 只用 150ms 后 RST 的代理与“接受但不应答”黑洞；与生产中间设备不同 |
| U5 | 中间 CA、证书轮换、CRL/吊销 | 仅自签 CA + 单层证书 |
| U6 | auth Token 模式与 token 刷新路径 | 只用 Username/Password |
| U7 | `DialKeepAliveTime>0` 的显式 keepalive 影响 | 只测默认 0 |
| U8 | 手工注册 `grpcprom` 的实际指标名与开销 | 只验证“默认不注册” |
| U9 | Linux（生产）上的 x509 文案与 cgo 行为 | 本机 darwin/arm64；转 A6/发布 |
| U10 | GitHub release 作为 etcd 来源 | 源码自建一次成功，未触发回退 |
| U11 | 配置凭据时 `request_timeout`/`DialTimeout` 的组合 | E2 只单测 New 阶段，未叠加故障代理 |

## 5. 代码地图摘要（供第 1–3 步引用）

| 路径 | 要点 |
| --- | --- |
| [pkg/models/client.go](../pkg/models/client.go) | `Client` 接口 9 方法（Create/Update/Delete/Read/List/Close/WatchInOrder/CreateEphemeral/CreateEphemeralInOrder）；工厂 `NewClient(coordinator, addrlist, auth, timeout)` 静态导入 `pkg/models/{zk,fs,consul}`；调用点 8 处：`cmd/admin/admin.go:43`、`cmd/dashboard/main.go:117`、`cmd/fe/main.go:147`、`cmd/proxy/main.go:289`、`pkg/proxy/proxy.go:168`、`pkg/proxy/cluster_nodes.go:66`、`pkg/models/consul_integration_test.go:25,73`；`pkg/models/client_test.go:11` 已有 `TestNewClientRejectsEtcd` |
| [pkg/models/store.go](../pkg/models/store.go) | `Store{client, product}`；`Acquire→client.Create(LockPath)`（create-only）、`Release→client.Delete(LockPath)`（无条件）；`must` 仅透传，driver 侧把 `must=false` 的缺失归一为 `(nil,nil)`；`WatchInOrder`/`CreateEphemeralInOrder` 无生产调用方；`CreateEphemeral` 仅 `pkg/proxy/jodis.go:104` |
| [pkg/models/topom.go](../pkg/models/topom.go) | JSON 字段：token、start_time、admin_addr、product_name、pid、pwd、sys；需新增 `owner_id` |
| [pkg/topom/topom.go](../pkg/topom/topom.go) | `:225-239` Start 抢锁；`:174-223` Close 顺序；`:214-220` 仅 online 时 Release |
| [cmd/internal/coordinator/args.go](../cmd/internal/coordinator/args.go) | 96 行共享 CLI：zookeeper / zookeeper-auth / filesystem / consul / consul-auth；`FlagNames()` 被 admin tail 模式与 `args_test.go` 硬编码 |
| [pkg/topom/config.go](../pkg/topom/config.go) | Dashboard 配置 `coordinator_name/addr/auth`；Validate 不校验取值 |
| [pkg/proxy/config.go](../pkg/proxy/config.go) | `jodis_*`、`cluster_nodes_compat`/`cluster_nodes_refresh_period`；`all` 模式强制 jodis |
| [cmd/fe/main.go](../cmd/fe/main.go) | `:302-322` 列 `/codis3` 并读各 product `topom` 取 `admin_addr`，5s 缓存；静态文件为 JSON 数组 |
| [cmd/admin/args.go](../cmd/admin/args.go) | `:522-570` tail 模式；`--remove-lock` 需 `--product`；`dumpConfigV1` 写死 `/zk/codis/db_%s` |
| [cmd/proxy/args.go](../cmd/proxy/args.go) | 无 `--etcd` flag；`AtMostOne("dashboard","zookeeper","filesystem","consul","fillslots")`（`:85-87`） |
| [pkg/proxy/jodis.go](../pkg/proxy/jodis.go)、[pkg/proxy/cluster_nodes.go](../pkg/proxy/cluster_nodes.go) | jodis.go 161 行；cluster_nodes.go 435 行，`all` 模式列 jodis 前缀 |
| [pkg/models/proxy.go](../pkg/models/proxy.go) | `:15` `jodis_path`；[pkg/proxy/stats.go](../pkg/proxy/stats.go) 无 jodis 字段；FE 前端仅 README 提到 zk |
| 依赖链 | `cmd/ha → pkg/topom → pkg/models`；`cmd/shake → pkg/migration → pkg/models` / `pkg/proxy`；`pkg/proxy` 直接导入 `pkg/models` |
| 旧驱动部署面 | deploy/ 21、ansible/ 2、scripts/ 2、config/ 2、example/ 3 个文件引用（第 4 步用） |
| 测试约定 | 无 build tag；[pkg/topom/topom_test.go](../pkg/topom/topom_test.go) `:42-54` 用 fsclient 临时目录当 store；[pkg/models/consul_integration_test.go](../pkg/models/consul_integration_test.go) `:17-21` 以 `CODIS_CONSUL_ADDR` 门控 skip；helper-process 模式见 [pkg/topom/topom_data_migration_test.go](../pkg/topom/topom_data_migration_test.go) `:132-138` |

> **第 3 步退役后本表的变化（引用时以此为准）：** `Client` 接口收窄为 6 方法，`NewClient` 工厂与 `pkg/models/{zk,fs,consul}` 已删除；`pkg/proxy/jodis.go`、`models.Proxy.JodisPath`、`models.JodisDir/JodisPath` 已删除；`pkg/proxy/config.go` 的 `jodis_*` 字段已删除（改为 `LoadFromFile` 的 `toml.MetaData.IsDefined` 拒绝）；测试改用 [pkg/models/modelstest](../pkg/models/modelstest)；consul 集成测试与 fsclient 测试替身已随驱动删除。在线注册/发现见 [pkg/proxy/etcdonline](../pkg/proxy/etcdonline) 与 [pkg/proxy/online.go](../pkg/proxy/online.go)；依赖处置见 §3.11。

## 6. 决策 D1–D10

| 编号 | 决策与约束（含验收） |
| --- | --- |
| **D1 选版** | 直接依赖 `go.etcd.io/etcd/client/v3 v3.7.4`，由 MVS 带入 `api/v3`、`client/pkg/v3` v3.7.4、grpc v1.83.2、protobuf v1.36.11、golang/protobuf v1.5.4、zap v1.27.1；`go`/`toolchain` 指令与两条 replace 不变；不引入 protoc；不启用 exporter、指标端口、xDS。 |
| **D2 接线硬规则** | client/v3 只在需要连接的组件入口（cmd/dashboard、cmd/fe、cmd/admin、cmd/proxy）或其专属包中构造；`pkg/models`、`pkg/topom`、`pkg/proxy`、`pkg/migration` 禁止导入 `go.etcd.io/etcd/client/v3` 与 `pkg/models/etcd`；这些包通过接口接收实例（Proxy 在线注册/发现、Topom 锁原语均按接口注入）。验收：`go list -deps ./cmd/ha ./cmd/shake` 不含 gRPC/etcd 包，ha/shake 产物字节不变。 |
| **D3 Store 收窄** | `pkg/models/etcd.Client` 实现现有 `models.Client`：Create=Txn(Version==0) 后写入，冲突返回可区分错误；Update=单键 upsert；Delete 精确删键，缺失视为达成；Read/List 的 must 语义与现有一致；List=带尾斜杠前缀查询→去重、排序的直接子路径；WatchInOrder/CreateEphemeral/CreateEphemeralInOrder 返回 ErrNotSupported，第 3 步随 Jodis 退役删除接口。 |
| **D4 错误三分类** | 成功 / 明确拒绝（exists、permission denied、authentication failed、invalid argument、Txn 比较为 false）/ 结果未知（context deadline/canceled、Unavailable、EOF、连接断开，以及任何无法确认服务端是否已处理的错误）。实现约束：`context.DeadlineExceeded` 不是 gRPC status，`rpctypes.EtcdError` 需用 `errors.As` 识别；Put/Txn 在 EOF 下不被 SDK 重放（实测），Get 会重试到超时，因此结果未知时的回读必须用新的有界 context，并按 `request_timeout` 预算耗时。 |
| **D5 连接与超时（R1/R2 已确认）** | `dial_timeout` 用于 Codis 的连接预检和 SDK 构造期获取鉴权 token；首次 lease 续约等待由 SDK 设为 dial_timeout+1s；版本检查仅在启用 RejectOldCluster 时使用该值，Codis 未启用；gRPC 实际 TCP 建连没有独立的 dial_timeout 上限。`request_timeout` 是每次业务 KV/Txn 调用的总预算，包含该调用内的 SDK 重试。连接预检：跨 endpoint 统一截止时间（`dial_timeout`），任一可用 endpoint 即通过；失败时已识别的鉴权拒绝、证书校验或 TLS 握手错误直接返回原因并脱敏；启动依赖 etcd 的入口通过首次实际操作验证角色权限；仅开启在线注册的 Proxy 在后台预检与重试。第 1 步的预检实现曾对每个 HTTPS 地址分别计时，第 2 步改为并发探测 + 统一截止时间，并在 A1 补测「坏地址在前、健康地址在后」「全部地址不可用」。SDK 日志通过 zap → Codis 日志桥接（Warn 级）。 |
| **D6 Lease（R3 已确认）** | `Client.Close()` 不撤销 lease。正常关闭顺序：停止并等待注册循环 → 有界 Revoke 本实例 lease → Close；撤销失败记录错误，由服务端 TTL 收敛。`KeepAlive` 同时处理调用时的直接错误和启动后的 channel 关闭；channel 关闭是重新确认所有权并注册的触发条件。SDK 以最后一次成功响应携带的 TTL 设定截止时间、每秒扫描，服务端失联后 channel 最迟在最后一次成功响应 + TTL 左右关闭（E4 的 7.01s 从 KeepAlive 启动计时，距最后一次成功响应约 5.00s，当次 TTL=5s；此前「最长约 TTL+7s」的表述作废）。崩溃依靠服务端 lease 到期。 |
| **D7 日志与指标** | 默认无 Prometheus 注册、空闲连接无后台帧；失败路径 SDK 默认 zap Warn 输出到 stderr，由 D5(b) 桥接。 |
| **D8 测试策略** | 纯单测使用 `pkg/models/modelstest` 内存替身（替换 `pkg/topom`、`pkg/models` 测试对 fsclient 的依赖）；真实后端走 `pkg/models/etcd` 集成测试入口，环境变量 `CODIS_ETCD_SERVER_BINARY` 指向 etcd 二进制（未设则 skip），测试自行拉起单成员实例（随机端口、临时 data-dir）并清理；二进制由标签源码构建（P0 的 `codis-etcd-p0/etcd-bin/etcd`）。 |
| **D9 go.mod 变更方式** | 仅 `go get go.etcd.io/etcd/client/v3@v3.7.4` 与实际验证命令驱动的 `go.sum` 更新；直接导入的模块手工去掉 `// indirect` 注释（等价于 tidy 对这几行的结果）；禁止全量 `go mod tidy`。 |
| **D10 构建成本接受** | dashboard +18.9MB、proxy +18.8MB、admin +13.1MB、fe +12.4MB（darwin/arm64 默认 tags），ha/shake 0。 |

## 7. 契约修订 R1–R6（2026-11-27/28 已确认并回写契约 v1.3）

状态：**R1–R4 已于 2026-11-27 按用户裁决回写契约 v1.2，R5/R6 于 2026-11-28 按 A6 真机裁决回写契约 v1.3**；实施按契约 v1.3 与 D5/D6/D7 执行。

| 编号 | 契约位置 | 回写文本 | 实测依据 |
| --- | --- | --- | --- |
| **R1** | §3 配置表 `dial_timeout`/`request_timeout` 行拆为两行 | `dial_timeout`：用于 Codis 的连接预检和 SDK 构造期获取鉴权 token；首次 lease 续约等待由 SDK 设为 dial_timeout + 1s；版本检查仅在启用 RejectOldCluster 时使用该值，Codis 当前未启用；gRPC 实际 TCP 建连没有独立的 dial_timeout 上限。`request_timeout`：每次业务 KV/Txn 调用的总预算，包含该调用内的 SDK 重试。 | 现状“分别用于客户端连接设置和单次 KV/Txn RPC”与 v3.7.4 实测不符（`client/v3/config.go:36-44`；`dial_timeout=1s` 时首个 Get 仍 5001.54ms） |
| **R2** | §3 新增“连接预检”段 / A1 | 对全部 endpoint 采用统一的预检截止时间（`dial_timeout`），任一可用 endpoint 即通过；已识别的鉴权拒绝、证书校验或 TLS 握手错误直接返回原因并脱敏；启动依赖 etcd 的入口通过首次实际操作验证角色权限；仅开启在线注册的 Proxy 在后台预检与重试。A1 补测“坏地址在前、健康地址在后”与“全部地址不可用”。 | E2：TLS 五类错误对调用方都是 5s deadline，原因只在 SDK zap Warn；文件级错误才 fail-fast |
| **R3** | §4 续约与关闭 | `KeepAlive` 同时处理调用时的直接错误和启动后的 channel 关闭，channel 关闭是重新确认所有权并注册的触发条件（SDK 以最后一次成功响应携带的 TTL 设定截止、每秒扫描）；关闭顺序：停止并等待注册循环 → 有界撤销本实例 lease → Close，撤销失败记录错误、由服务端 TTL 收敛；`Client.Close()` 不撤销 lease。删除原“服务端失联后最长感知时限”的旧表述（新结论见 D6）。 | E4：Close 后键存活到 TTL 到期；Revoke 10.66ms 立即删键；服务端 kill -9 后 channel 在 KeepAlive 启动后 7.01s 关闭（距最后一次成功响应约 5.00s，当次 TTL=5s） |
| **R4** | §0 表“指标与其他传递依赖”行 | 直接补注“client/v3 v3.7.4 默认不注册 Prometheus collector（上游示例显式创建并注册 collector；P0 实测创建客户端及执行 RPC 前后均为 34 个指标族）”。 | E5：创建前后指标族数均 34、diff 为空；`go mod why -m` 为“main module does not need” |
| **R5**（2026-11-28 已确认并回写契约 v1.3） | §2“获得所有权”条目末尾新增“**启动失败**” | “建连或鉴权失败（抢锁事务尚未发出）时，Dashboard 报出脱敏原因并非零退出，由进程管理器退避重启，不在此阶段重试；抢锁事务发出后若结果未知，仍须按本条用原 `owner_id` 线性一致回读，确认持锁后才能上线。”裁决选 B（仅限抢锁事务发出前），本次不给 Dashboard 增加第二套重试循环（代码不改）。 | A6 S-C：第二个 dashboard 在 `NewWithOptions` 构造期鉴权 5s `context deadline exceeded`，`cmd/dashboard/main.go:124` 打印 `[PANIC] create etcd client failed` 非零退出、从未上线；已上线实例读接口仍 200 |
| **R6**（2026-11-28 已确认并回写契约 v1.3） | §7 A6 行“隔离一个成员”场景与结果栏 | 场景改为“仅隔离一个成员的 peer 流量（如 2380），保持多数派健康且 Proxy 仍可续约”；结果栏改为“在线记录保留；Dashboard 持久纳管记录不变”，并补“全隔离变体（peer 与客户端端口均不通）下 lease 按 TTL 过期、恢复后自动重注册属预期，不作为失败”。 | A6 S-B：peer 2380 隔离 108s，2/3 保持 leader、在线记录 TTL 27/28s 不丢、注册表未删 token；全隔离变体 68s 后记录按 TTL 撤销、恢复后 11s 自动重注册 |

## 8. 实施进度与验收状态

### 8.1 实施步骤（契约第 6 节）

| 步骤 | 状态 | 已完成 / 进行中的内容 | 完成判断（待闭环） |
| --- | --- | --- | --- |
| 0 · P0 依赖调研 | **完成** | 模块差异四表、逐入口包图、产物与交叉构建证据、单成员运行实验；A0 本机通过 | 交付时复核最终选版、产物与退役后快照 |
| 1 · 接入客户端与 Store | **完成** | `pkg/models/etcd`：连接文件解析与校验（密码脱敏、未知字段拒绝、相对 TLS 路径）、地址优先级函数、TLS 握手预检、SDK zap → Codis 日志桥接、`Classify` 错误三分类、`models.Client` 9 方法实现（Create=Txn Version==0，List=去重排序直接子路径）；`pkg/models/modelstest` 内存替身替换 `pkg/topom`、`pkg/models` 测试对 fsclient 的依赖；go.mod 按 D9 归一（直接导入的 4 行去 `// indirect`，其余与 P0 候选一致） | 已闭环：`go build ./...`、`go vet`、`go test ./pkg/models/... ./pkg/topom/...`（含 `CODIS_ETCD_SERVER_BINARY` 指向 etcd 3.7.4 的集成用例，`-race` 亦通过）全绿；六个入口包图仍为 0 个 etcd/gRPC 包（入口尚未接入，属第 2 步） |
| 2 · Dashboard 与运维 | **完成** | 2-A：`pkg/models` 新增可选接口 `LockClient`（`CreateWithRevision/ReadWithRevision/DeleteByCreateRevision`）与 `AcquireLock/ReleaseLock/ShowLock/RemoveLock`（Txn Version==0 抢锁、按 create_revision 释放、应答未知时线性回读、明确拒绝直接返回原因不回读）；`Topom.owner_id`（每进程 UUID，重试沿用）；`pkg/topom` 启停顺序；`modelstest` 实现 LockClient 与故障注入。2-B：`cmd/internal/coordinator` 重写为 `--etcd/--etcd-config`（优先级链、旧 flag 报错含旧线 SHA 且不含值）、`pkg/topom/config.go` 默认 `coordinator_name="etcd"`、拒绝旧取值与 `coordinator_auth`（含空值）。2-C：Dashboard 接线并移除 `--remove-lock`、锁启动策略；Admin `--show-lock`、`--remove-lock --expected-revision --confirm`、移除 `--config-dump -1`；FE 静态/动态互斥与首刷策略；Proxy `--dashboard/--etcd/--fillslots` 互斥与 `--etcd-config` 仅提供连接选项。R2 修正：TLS 预检改为跨 endpoint 并发 + 统一截止时间。删除无调用方的 `Store.Release()`。 | 已闭环：`go build ./...`、`go vet ./cmd/... ./pkg/...`、`go test ./cmd/... ./pkg/...`（29 包全 ok，含真实单成员 etcd 集成）通过；四组件入口各链接 69 个 etcd/gRPC 包，ha/shake 为 0；`pkg/proxy`、`pkg/topom` 不导入 `pkg/models/etcd`。四组件同接启用认证 etcd 的端到端场景留第 5 步。 |
| 3 · 在线发现和退役 | **完成** | **3-A 在线发现**：`pkg/models/etcd/lease.go` 新增 lease 原语（`GrantLease/KeepAlive/RevokeLease/TimeToLive/PutWithLeaseIfAbsent/LeaseOfKey/RangeWithPrefix`）；`pkg/models/proxy_online.go` 定义 `/codis3/{product}/online-proxies/{token}` 键与 `OnlineRecord`；`pkg/proxy/etcdonline` 实现注册与发现（固定 30s TTL，version==0 绑定 lease，后台退避重试、channel 关闭后重确认所有权、按 product 前缀一致读并跳过坏记录，关闭顺序=停循环→有界 Revoke→Close）；`pkg/proxy` 只依赖注入的 `OnlineRegistry`/`OnlineRegistryFactory`/`OnlineLister` 接口（D2：不导入 etcd）；`Proxy.SetModel` 在 token 已知后才启用注册（惰性建连，上线不等 etcd）；`cluster_nodes.go` 的 `all` 模式改读在线前缀，保留 disabled/self/all 与刷新周期。**3-B 退役**：删除 `pkg/models/{fs,zk,consul}` 三驱动与 consul 集成测试、`pkg/models/client_test.go`；`models.NewClient` 工厂删除，`models.Client` 收窄为 `Create/Update/Delete/Read/List/Close`（包注释写明新线唯一后端是 `pkg/models/etcd`、由组件入口构造注入）；`etcd.Client`/`modelstest` 同步删除 `WatchInOrder/CreateEphemeral/CreateEphemeralInOrder`（连同不再被调用的 `etcd.ErrNotSupported` 哨兵及其分类用例）；删除 `pkg/proxy/jodis.go`、`Proxy.jodis` 字段与 setup/Start/Close 接线、`proxy.Config` 的 5 个 `jodis_*` 字段/校验/默认注释、`models.Proxy.JodisPath`、`models.JodisDir/JodisPath`、FE assets README 旧 flag 说明；`proxy.Config.LoadFromFile` 用 `toml.MetaData.IsDefined` 检测 `jodis_*`（含空值）并由 `Validate` 报 `jodis_* options are no longer supported; Jodis registration was last supported at c635ade0…a555, use etcd_register_online`（带回检测到的选项名、不回显值），附 7 个用例；删除 `coordinator.Args` 的零值兼容字段与 `topom.Config.CoordinatorAuth`（保留 IsDefined 拒绝）；go.mod 按 §3.11 删除 15 个旧驱动模块（182→126）。 | **已闭环**：`gofmt` 空、`go build ./...`、`go vet`、`go test ./cmd/... ./pkg/...`、`make gotest`（18.80s）、`go list -mod=readonly -deps -test`（538 行）全绿；A5 集成 PASS 10.79s；默认配置未开注册的普通 proxy 不连接 etcd（`TestProxyWithoutOnlineRegistrationDoesNotWireRegistry`）；生产包图 zk/consul 为 0，ha/shake etcd/gRPC 包为 0；§3.11 记录逐项依赖处置。默认 TOML/帮助/示例/部署入口再生在第 4 步完成（见下行；当时 `config/proxy.toml` 仍陈旧）。 |
| 4 · 部署与文档 | **完成** | 再生 `config/dashboard.toml`（`coordinator_name="etcd"`、`coordinator_addr`，无 `coordinator_auth`）与 `config/proxy.toml`（`etcd_register_online=false`，无 `jodis_*`），单测钉死与 `--default-config` 输出逐字节一致；三处凭据日志脱敏（Dashboard `--product_auth`；Proxy `--product_auth`、`--session_auth` 只输出 `<set>`/`<unset>`）；tutorial zh/en、architecture、capability_boundaries、FAQ、根 README、proxy_protocol_extensions、unsupported_cmds；发布说明与索引；deploy 模板与渲染样例、ansible、example。 | 新部署示例只需 etcd、Codis 组件与应用入口；帮助无旧后端选项。 |
| 5 · 构建与签收 | **完成（本地 + A6 真机）** | `make gotest` 19.4s 28 包 ok；完整 `make` 28.2s（codis-server 增量）产物：dashboard 33.8MB/34 dep、proxy 34.3MB/35、admin 24.7MB/34、ha 13.3MB/19、fe 23.0MB/18、shake 14.3MB/18、codis-server 8.6.3；`go list -deps ./cmd/... ./pkg/...` 35 模块，zk/consul 0，ha/shake 0 个 etcd/gRPC 包；A7 干净副本（6428 文件、无 .git/bin）build/vet/test/默认配置 cmp 全部一致；A1–A3 完整产物端到端见 §8.4。**A6（2026-11-28）**：同一批改动在 host1 用 **Linux 原生 `make`** 构建（Go 1.27.4 linux/amd64；`codis-proxy -tags=cgo_jemalloc` dep 35；`codis-server` Redis 8.6.3 / jemalloc-5.3.0；proxy sha256 `69d79553…`、server `a80398c6…`），在三成员 etcd 3.7.4 真机跑完 S-A/S-B/S-C/S-D 四场景（明细见 §8.2 A6 与 [validation/etcd_3_7_coordinator_a6_zh.md](validation/etcd_3_7_coordinator_a6_zh.md)）。 | A6 已于 2026-11-28 通过并签收；启动期 PANIC 退出与单成员隔离口径已按裁决回写契约 v1.3（见 §7 R5/R6、§10）。Linux 跨界构建（`linux/arm64`、交叉工具链）仍在 A7/发布侧待验。交付说明见 §11。 |

### 8.2 交付验收 A0–A7（契约第 7 节）

| 编号 | 状态 | 已有证据 / 备注 |
| --- | --- | --- |
| A0 依赖调研 | **通过（本机 darwin/arm64）** | P0 四项交付齐备；契约要求正式接入前有候选报告、交付时核对最终选版/产物/退役结果，退役后快照与候选的差异须在交付说明中记录 |
| A1 配置与入口 | **通过（本地端到端）** | 四组件各用自身角色凭据（mTLS + 四个 etcd 用户 dashboard/fe/proxy/admin）接同一启用认证的单成员 etcd 3.7.4：Dashboard 取锁、FE 动态 `/list`（含 `/api/topom/model?forward=demo`）、Admin `--dashboard-list`/`--show-lock`、Proxy `--etcd` 自动上线与 `--dashboard`+`--etcd-config` 组合。负向 21 项：错误密码 0.34s 给出明确原因（不含密码）、缺客户端证书预检 0.03s 报 `certificate required`、旧 flag 与空值 TOML 旧字段（`jodis_name=""`、`coordinator_auth=""`）报选项名+旧线 SHA 且不含值、静态+动态互斥、Proxy 三入口互斥、坏地址在前 0.37s 通过/全死 0.03s 失败。86 个日志文件密码/私钥扫描 0 命中；Dashboard `--product_auth`、Proxy `--product_auth`/`--session_auth` 三处凭据日志为 `<set>`。 |
| A2 建群与隔离 | **通过（本地端到端）** | 空前缀预检继续、非空停止（`PRODUCT NOT EMPTY … aborting`）；2 group/2 codis-server/1024 slot（512+512、无 locked）；`k1`（slot 10）→6379、`k20`（slot 820）→6479 经 proxy 写入后直连核对落点；`demo-bar` 同前缀预置记录（2 键/404 字节）重启前后逐字节未变；Dashboard 正常停止释放锁、重启 0.69s 取新锁，group/slot/proxy/config 模型逐字段一致（差异仅在易变 `stats`）。 |
| A3 锁与迟到请求 | **通过（本地进程级）** | 双 Dashboard 争同一 product：恰一个持锁、另一个每 2s 记录冲突（含持有者 owner_id/create_revision）并继续重试；`kill -9` 后锁保留（持久锁、无 lease）；同地址 C 冲突重试；按观察到的 revision 1040 `--remove-lock` 成功后 C 1.17s 接管（owner 与 create_revision 均变化：`015d2eef…`/1042）；旧 revision 1040 的迟到请求 exit 1「lock is now held by another instance … left intact」且 C 的记录完整；缺 revision/0/负数/缺 confirm/缺 product 均 exit 1 且文案区分。 |
| A4 结果未知 | **通过（测试替身）** | 故障注入覆盖提交前失败、锁已提交但应答丢失（回读确认本实例 owner 并取回 revision）、回读不可用（`ErrResultUnknown`、不持有、无后台任务）、删除已提交但应答丢失（`ErrLockGone`）、旧 owner 迟到释放/移除（比较失败、新锁完整）、明确拒绝不回读直接返回原因。 |
| A5 混合 Proxy | **通过（本地单成员）** | `CODIS_ETCD_SERVER_BINARY` 指向 P0 的 etcd 3.7.4 二进制，`go test ./pkg/proxy -run 'A5\|Online'` PASS（10.79s），`pkg/proxy/etcdonline` 集成 14.75s 同期通过。实测：`all+注册` 在 186ms 内列出 self+B+C 且不含未注册 D；`self+注册` 仅返回自身；正常退出后 peer 在 184ms 移除（revoke 于 23:13:06.623）；etcd 停机时保留最后快照并告警，重启后 675ms 收敛；同 token 旧进程关闭无法撤走新进程 lease（新记录保留、旧进程后台重试）；`kill -9` 后键在 2.304s 消失、A 在 2.421s 移除 peer（刷新 200ms，短 TTL 回归用服务端授予 TTL 1s、请求 2s）。**生产 TTL 证据**：默认配置（仅 `etcd_register_online=true`）注册后，Go `TimeToLive` 返回 `GrantedTTL=30`（查询时剩余 TTL 26→21 递减；`etcdctl lease timetolive` 显示的是剩余值）；此前测试打印的 1s 为 `resp.TTL` 查询时剩余值，不代表授予值。短 TTL 回归继续保留；三成员 Linux 场景归 A6。 |
| A6 Linux 发布门槛 | **通过（三成员 Linux 真机，2026-11-28 签收）** | 华为云 3 台 ECS（cn-east-3a，`c7.xlarge.2` 4C8G，Ubuntu 22.04.5，kernel 5.15.0-186，x86_64）+ 三成员 etcd 3.7.4（HTTP 客户端端口、`--auth-token simple`、RBAC：root/dashboard/proxy/admin，角色 `codis-rw` 对 `/codis3/` 前缀 readwrite），Codis 产物为 host1 Linux 原生 `make`（go1.27.4 linux/amd64；proxy `-tags=cgo_jemalloc` dep 35；codis-server Redis 8.6.3 / jemalloc-5.3.0）。**S-A leader 切换通过**：leadership transfer 2ms 选出新 leader，端到端 9.18s（含 etcd 优雅停机 9.04s），锁 owner/create_revision 不变，负载 0 失败，重启后 3/3 追平。**S-B 隔离单成员通过**（仅隔离 peer 2380，契约 v1.3 主口径；隔离 108s）：2/3 保持 leader、负载 0 失败、在线记录 TTL 27/28s 不丢、`CLUSTER NODES` 正常、0.1s 归队、约 1.6min 追平；全隔离变体（2379+2380 均不通）中该主机 proxy 因无法续约、租约到期后 lease 按 TTL 过期，恢复后 11s 自动重注册——契约 v1.3 已列为预期，且 Dashboard 持久纳管记录始终不变。**S-C 失去/恢复多数派通过**：`--show-lock` 5011ms 返回 `create etcd client failed … context deadline exceeded`、复测 848ms `etcdserver: leader changed`；写 store 的 `--create-group` **5013ms** `store: load slots failed`（加载 slots 失败、组未创建，符合预期：写 store 的操作在失去多数派时报错）；内存读接口 200；两台 proxy `keepalive stream closed; re-confirming ownership` 并 1/2/4/8/16/30s 退避；`CLUSTER NODES` 保留快照；负载 0 失败；恢复 19.2s；锁 owner/create_revision 不变；在线记录 create_revision 1040/1042 不变（误删 0）。**启动失败（已裁决）**：第二个 dashboard 从未上线，以 `[PANIC] create etcd client failed` 非零退出；按 2026-11-28 裁决符合契约 v1.3 §2——建连/鉴权失败发生在抢锁事务发出前，由进程管理器退避重启，非缺陷（见 §10）。**S-D 正常退出通过**：proxy SIGINT 后在线记录 ~0.2s 消失（`revoked lease … on shutdown`），dashboard SIGINT 后 `--show-lock` = `null` 76ms。负载：S-A/B/C 故障窗口 0 失败；S-D **主动停机后的负载计数** 40 条/台，全部是 proxy 停机后负载多跑 ~8s 的 `Connection refused`，不计入故障窗口。证据为本地路径（不入库）`.runtime/codis-a6/a6/report.md` 与 `ev/`（139 文件）；关键表已复制到 [validation/etcd_3_7_coordinator_a6_zh.md](validation/etcd_3_7_coordinator_a6_zh.md)。 |
| A7 构建与清理 | **通过（本地）** | 旧 SDK（zk/consul）、filesystem coordinator 包、Jodis 注册路径与 `jodis_*` 已从生产代码退出，名称只留在拒绝诊断与测试；依赖图无旧驱动（zk/consul 0 包），`go list -deps ./cmd/... ./pkg/...` 35 模块、ha/shake 0 个 etcd/gRPC 包；干净副本检查见 §8.1 第 5 步（6428 文件、无 `.git`/`bin`，`build`/`vet`/`test`/默认配置 `cmp` 全部一致）；帮助文本无 `zookeeper`/`consul`/`filesystem`/`jodis` 选项（dashboard/admin 仅在结论句提到旧后端不再支持）。Linux 原生 `make` 与 `cgo_jemalloc` 真机构建已由 A6（host1，2026-11-28）覆盖；`linux/arm64` 与交叉工具链仍待验。 |

### 8.3 第 2 步实现要点（供第 3–5 步引用）

- **锁 API**：`AcquireLock(t) (*LockOwnership, error)`、`ReleaseLock(o)`、`ShowLock() (*LockInfo, error)`、`RemoveLock(rev)`；哨兵 `ErrLockHeld/ErrLockOwnershipChanged/ErrLockGone/ErrResultUnknown/ErrLockUnsupported`；`IsRejected(err)`；比较为 false 时若锁值为本进程 owner_id 则取回 revision 接管。
- **Dashboard**：`Start` 失败三类处理（拒绝 → 非零退出；冲突 → `ShowLock` 记录持有者并退避重试；未知 → 重试）；`Close` 顺序为关监听 → 等后台任务 → `ReleaseLock` → `Close`，`ErrLockGone/ErrLockOwnershipChanged` 仅告警。
- **Admin**：`--show-lock` 输出 JSON（product/admin_addr/owner_id/create_revision/mod_revision/lease）或 `null`；`--remove-lock` 退出码：成功 0；Gone/OwnershipChanged/Unknown/Rejected 均 1 且文案区分；`--config-dump -1` 移除；直接访问元数据的命令要求至少一个共享参数。
- **FE**：`--dashboard-list` 与 `--etcd/--etcd-config` 恰居其一；启动同步首刷（拒绝立即失败、连接类 3×2s 重试后失败）；周期刷新失败保留上次快照，不产生空列表。
- **Proxy**：`--dashboard`/`--etcd`/`--fillslots` 至多一个；仅 `--etcd-config` 不连接 etcd（留给第 3 步注册）；`--dashboard` 与 `--etcd-config` 可共存。
- **预检**：TLS 配置下对全部 endpoint 并发握手探测，共享 `dial_timeout` 截止；失败返回已识别原因并脱敏。

### 8.4 第 5 步本地端到端（A1–A3、TTL）

本节是第 5 步本地部分的证据摘要：**完整产物**（`bin/` 下的 `codis-dashboard`/`codis-proxy`/`codis-admin`/`codis-fe`/`codis-server`）对**单成员、mTLS、启用 auth** 的 etcd 3.7.4 跑通 A1/A2/A3 与生产 TTL 观测。证据文件在本机路径（不入库），关键表已复制到本节。

| 项目 | 内容 |
| --- | --- |
| 环境 | 单成员 etcd 3.7.4（client URL `https://127.0.0.1:2379`，peer `http://127.0.0.1:2380`，`--client-cert-auth` + 自建 CA）；每个角色一张客户端证书（`CN=dashboard/fe/proxy/admin`）+ 四个 etcd 用户与同名角色：dashboard/proxy/admin 在 `/codis3/` 前缀读写、fe 只读；连接文件用相对路径指向 `tls/` |
| 组件版本 | Codis `@3a5890d4`（运行期编译版本），codis-server 8.6.3；Dashboard `18080`（并行 B `18081`）、FE 动态 `9090`/静态 `9091`、proxy `19000/11080`、codis-server `6379`/`6479` |
| 关键数字 | 取锁成功且 `--show-lock` 返回 `owner_id`/`create_revision`；FE `/list` → `["demo"]` 且 `/api/topom/model?forward=demo` → 200；坏地址在前预检 0.37s 通过、全部不可用 0.03s 失败、缺证书 0.03s、错误密码 0.34s；Dashboard 正常停止后重启 0.69s 取新锁；按 revision 1040 解锁后 C 1.17s 接管（`015d2eef…`/1042）；`GrantedTTL=30`（剩余 26→24→21）；关闭 Proxy 后注册键 0.08s 消失 |
| 集群复核 | 1024 slot（512+512）、2 group、2 codis-server；`k1`(slot 10)→6379、`k20`(slot 820)→6479 直连核对；`demo-bar` 预置记录 `cmp` 逐字节一致（重启前后各一次）；1029 个键（1024 slots + 2 group + 2 proxy + topom） |
| 凭据扫描 | 86 个日志/证据文件扫描 5 个口令 + 2 个测试凭据 + 每张客户端私钥的 base64 行 + `PRIVATE KEY` 字样：**全部 0 命中**；三处凭据日志为 `<set>`，配置回显为 `product_auth = "<redacted>"` |
| 证据路径 | `~/work/codis-etcd-p0/e2e-5b/report.md`（本机路径，不入库）；配套 `logs/`、`ev/`、`scripts/`（`gen-certs`、`start-etcd`、`bootstrap-auth`、`gen-conn-files`、`precheck-product`、`stop-all`、`scan-secrets`）与 `ttlcheck/` |
| 清理核实 | 19 个登记 pid 全部退出，22 个端口经 `lsof` 复核全部空闲，仓库目录在运行期间零新增/变更文件 |

复现入口（`$W` 为上述证据目录）：

```sh
bash $W/scripts/gen-certs.sh && bash $W/scripts/start-etcd.sh
bash $W/scripts/bootstrap-auth.sh && bash $W/scripts/gen-conn-files.sh
BIN=$HOME/work/codis/bin
$BIN/codis-dashboard --config=$W/dashboard.toml --etcd-config=$W/etcd-dashboard.toml
$BIN/codis-fe --listen=127.0.0.1:9090 --etcd-config=$W/etcd-fe.toml
$BIN/codis-proxy --config=$W/proxy.toml --etcd=https://127.0.0.1:2379 --etcd-config=$W/etcd-proxy.toml
$W/scripts/scan-secrets.sh
```

## 9. 证据位置与可复现命令

以下均为**本机路径**（`~/work/codis-etcd-p0/`），不在仓库内、不入库；交付说明附本记录作为关键表副本，并按契约列出 Codis SHA、etcd/server 与 client/v3 版本、实际命令与结果。

| 内容 | 路径（相对 `codis-etcd-p0/`） |
| --- | --- |
| 环境与基线 | `env/env.txt`、`env/goenv.txt`、`env/candidate-head.txt`、`env/version.ldflags.frozen`、`env/main-repo-status.txt`、`env/etcd-repo-status.txt` |
| 快照 | `snapshots/{baseline,candA,candB}/`（modules.json、modgraph.txt、go.mod/go.sum、`<entry>.packages*.txt`）、`snapshots/graphs/`、`snapshots/go-get/{gomod,gosum}.diff`、`snapshots/candidate.diff` |
| 产物 | `builds/{baseline,candB}/<entry>[.cgo_jemalloc]`、`*.goversion.txt`、`results.tsv`、`cross-results.tsv`、`linux-{amd64,arm64}/` |
| 报告与中间数据 | `report/p0-deps.md`、`report/module-diff.md`、`report/p0-runtime.md`、`report/raw/{chains.txt,newpkgs-*.txt,testonly-*.txt,reach-*.txt,build-exclusive-*.txt,summary.json}` |
| 运行实验 | `etcd-bin/{etcd,etcdctl,etcdutl}`、`etcd-data/{http,https,logs,tls,pids}/`、`runtime-probe/{probe,scripts,logs}/`、`report/p0-runtime-cleanup.txt` |
| 脚本 | `report/scripts/{snapshot,modwhy,build,crossbuild}.sh`、`report/scripts/analyze.py`、`runtime-probe/scripts/{gen-certs,run-e1..run-e6,check-secret-leak}.sh` |

```sh
P0=$HOME/work/codis-etcd-p0

# 候选接入与快照（基线 c635ade0 隔离克隆）
(cd $P0/candidate && go get go.etcd.io/etcd/client/v3@v3.7.4)
bash $P0/report/scripts/snapshot.sh $P0/snapshots/candB
bash $P0/report/scripts/modwhy.sh   $P0/snapshots/candB

# 产物与交叉构建（基线与候选同 flags；v3.7.4 由本地标签自建）
bash $P0/report/scripts/build.sh     $P0/builds/candB
bash $P0/report/scripts/crossbuild.sh $P0/candidate $P0/builds/candB

# 差异分析（生成 report/module-diff.md 与 report/raw）
/usr/bin/python3 $P0/report/scripts/analyze.py

# 单成员运行实验（E1–E6，etcd 与 TLS 实例由脚本拉起）
bash $P0/runtime-probe/scripts/run-e1.sh
bash $P0/runtime-probe/scripts/run-e2.sh
bash $P0/runtime-probe/scripts/run-e3.sh
bash $P0/runtime-probe/scripts/run-e4.sh
bash $P0/runtime-probe/scripts/run-e5.sh
bash $P0/runtime-probe/scripts/run-e6.sh
```

## 10. 风险与待办

| 风险 / 待办 | 影响 | 处置 | 关联 |
| --- | --- | --- | --- |
| SDK 补丁变化（client/v3 或 gRPC 版本调整） | 模块图、包图、产物字节与运行结论失效 | 重做受影响的比较：快照、构建、交叉构建、E1–E6 | A0、D1 |
| linux + `cgo_jemalloc` 真机构建 | ~~发布产物形态未验证~~ **已部分闭环** | A6（2026-11-28）在 host1 用 Linux 原生 `make` 构建并在真机运行：go1.27.4 linux/amd64、`codis-proxy -tags=cgo_jemalloc` dep 35、`codis-server` Redis 8.6.3 / jemalloc-5.3.0（sha256 前缀 proxy `69d79553…`、server `a80398c6…`），tags/CGO/ldflags 已记录。`linux/arm64` 与交叉工具链仍未验证 | A6、A7 |
| 可移除模块仅为解析结论 | ~~4a/4b/4c 与退役后实际清单可能偏离~~ **已闭环** | 第 3 步退役后重跑快照：15 个候选全部 `droprequire`，build list 182→126，另 41 个旧驱动专属传递模块自动消失（§3.11） | A5、A7 |
| ~~R1–R4 已回写契约 v1.2~~ **R1–R6 已回写契约 v1.3** | 契约文本与实测语义（dial_timeout、连接预检、lease 撤销、指标、Dashboard 启动失败、单成员隔离口径）已对齐 | 实施按契约 v1.3 与 D5/D6/D7 执行 | A1、A5、A6 |
| 预检并发化与 A1 两项补测 | 第 1 步预检曾按地址分别计时，多 endpoint 语义与「全部不可用」未覆盖 | 第 2 步改为并发探测 + 统一截止时间；A1 补测「坏地址在前、健康地址在后」「全部地址不可用」，在该步闭环 | A1、R2 |
| U1–U11 运行缺口（多成员、TLS 轮换、Token、Linux 文案等） | ~~A6 无法由现有证据闭环~~ **U1 已由 A6 闭环** | U1（多成员/quorum、leader 切换、隔离成员、失去/恢复多数派）已由 A6 真机覆盖（§8.2）；U9 的 Linux 行为在认证/连接/租约路径上已实测，x509 文案未单独验证；U2–U8、U10–U11（TLS 轮换、CRL、auth Token 刷新、真实静默丢包等）仍待验 | A6 |
| **已裁决：无多数派时新起的 dashboard 非零退出，按契约 v1.3 §2 执行，非缺陷，已闭环** | 契约 §2 的“启动失败”条款已明确：建连或鉴权失败发生在抢锁事务发出前时，Dashboard 报脱敏原因并非零退出，由进程管理器退避重启；抢锁事务发出后若结果未知，仍须用原 `owner_id` 线性一致回读确认持锁后才上线 | `pkg/models/etcd/client.go` 的 `NewWithOptions` 在构造期鉴权 `context deadline exceeded`，`cmd/dashboard/main.go:124` 打印 `[PANIC] create etcd client failed` 并退出进程；已上线实例不受影响（沿用既有 client）。裁决选“修订契约措辞”（选项 b，仅限抢锁事务发出前），本次不改代码、不给 Dashboard 增加第二套重试循环；A6 单成员隔离口径为 peer 隔离（已回写契约 §7 A6） | A6、§8.2 A6、§7 R5/R6 |
| **注意项 ③** dashboard API 前缀是 `/topom`（`/api/topom` 返回 404）；多数派丢失时读接口仍从内存返回 200（3ms），只有落 store 的操作报错 | 契约里的探测路径与"查询/管理命令返回错误"的断言若按路径/命令粗粒度判定会误判：`--show-lock` 由 CLI 直连 etcd、`--create-group` 经 dashboard 写 store 才失败；`--slots-status`/`--list-proxy`/`/topom` 走内存缓存仍成功 | 契约探测路径改 `/topom`；"查询与管理命令返回错误"按"是否落 store"区分，并在验证文档写明两类结果 | A6、§8.2 A6 |
| **注意项 ④** `etcdctl endpoint status --cluster` 在任一成员不可达时整体失败 | 隔离/停机窗口内巡检脚本会整体超时报错（`failed to fetch endpoints from etcd cluster member list: context deadline exceeded`），可能被误读为集群全部不可用 | 巡检脚本按"存活端点子集"动态收窄后执行 `endpoint status`；`--cluster` 的失败本身作为"存在不可达成员"的证据 | A6 |
| **注意项 ⑤** 长时间隔离的成员归队后 ReadIndex/apply 需 1.6–4 分钟才完全追上 | 若判据是"恢复即等于立即可读/可写一致性"，会误判为失败；期间服务端出现 `dropped internal Raft message since sending buffer is full`、`write i/o timeout` | 恢复判据留足超时（建议 ≥4min）；区分"成员归队（0.1s 级）"与"追平（分钟级）"两个里程碑 | A6 |
| ~~`go.mod` 中 etcd 三模块残留 `// indirect`~~ **已闭环** | 依赖声明不准确 | 第 1 步按 D9 手工归一：`client/v3`、`api/v3`、`zap`、`grpc` 为直接 require；`client/pkg/v3` 保持 `// indirect`（正常传递）；第 3 步退役后仍未 tidy | A0、A7 |
| 构建成本 +18.9/+18.8/+13.1/+12.4MB | 发布包体积上升 | D10 已接受；若发布限值收紧，再评估模块侧屏蔽（未验证） | 发布 |
| requirement-only 模块（prometheus、otel 等）放大 `go.sum` | 账面成本，易被误读为运行依赖 | 以包图与产物为准；不启用 exporter/指标端口/xDS。退役后 `go.sum` 仍保留已删模块的校验行（395 行，未 tidy），发布前如需清理另行授权 | A0、D1 |
| 证据为本地路径、不入库 | 交付不可独立复现 | 本记录附关键表；发布说明列出 SHA、版本、命令与结果 | 交付 |
| ~~`cmd/dashboard/main.go` 启动日志会打印 `--product_auth` 的值（既有行为，与 etcd 连接文件凭据无关）~~ **已闭环** | 口令出现在日志与采集系统 | 第 4 步改为脱敏输出（只报 `<set>`/`<unset>`）：Dashboard `--product_auth`、Proxy `--product_auth`/`--session_auth` 三处均已脱敏，并纳入 A1 的日志扫描（86 文件 0 命中） | A1、第 4 步 |
| ~~`cmd/internal/coordinator.Args` 中 `Name/Addr/Auth/HasAuth` 兼容字段、`pkg/topom.Config.CoordinatorAuth`~~ **已删除** | 残留字段被误读为仍生效 | 第 3 步已删；`coordinator_auth` 的拒绝仍由 `topom.Config.coordinatorAuthSet`（`toml.MetaData.IsDefined`）保留，`Redacted()` 与相关测试同步 | A5、A7 |
| ~~`proxy.Config.LoadFromFile` 的 `jodis_*` 拒绝依赖 `toml.MetaData.IsDefined`；`config/proxy.toml` 仍含旧示例行~~ **已闭环** | 旧字段即使为空也会被拒（符合契约）；此前默认 TOML 未再生，用旧文件启动会被拒 | 第 4 步用 `--default-config` 再生 `config/proxy.toml`（`etcd_register_online=false`，无 `jodis_*`）与 `config/dashboard.toml`，并以单测钉死与 `--default-config` 输出逐字节一致 | A7、第 4 步 |
| `pkg/proxy/etcdonline.Options.TTL` 是为集成测试导出的字段 | 生产固定 30s 的语义可被误用（调用方设小 TTL 会缩短注册记录寿命） | 生产入口（`cmd/proxy` 的 `onlineRegistryFactory`）不设置该字段；§4 的“固定 30s、不暴露调优参数”口径以入口为准，测试用例使用 1–2s | A5、D6 |
| `Proxy.SetModel` 为第 3 步新增的公开方法（token 已知后才启用在线注册） | 调用方若在 `Start` 前未调用，注册不会开始；误传其他 proxy 的 token 会被拒绝 | 生产入口 `cmd/proxy/main.go` 在 `etcd_register_online=true` 时构造后立即调用；`SetModel` 拒绝 token 变更并记录告警 | A5、第 3 步 |
| 第 1 步记录中“预检对每个 HTTPS 地址分别计时”的问题已在第 2 步修正（并发 + 统一截止时间） | 旧结论若被后续步骤引用，会误判多 endpoint 语义 | 以 D5/R2 的并发预检为准；A1 已实测「坏地址在前」「全部不可用」 | A1、R2 |
| **F1** 在线记录 `addr` 曾取自仅 CLI 可设的 `HostProxy` | 默认配置（只设 `proxy_addr`、无 `--host-proxy`）下注册记录 `addr` 为空，`all` 发现恒为空（端到端报告 F1） | **已修复**：优先使用纳管后 model 的代理地址，回退 `HostProxy`/`ProxyAddr`，并补回归测试 | A5、契约 §4 |
| **F2** Dashboard 正常停止时的 shutdown 日志噪声 | 停止时打印 `admin server shutdown failed / use of closed network connection`，掩盖真实错误 | **已修复**：忽略 `net.ErrClosed`，不再记录该噪声 | A2 |
| **F3** `--host-admin` 不改变监听端口（运维注意） | 并行多个 Dashboard 用同一 `admin_addr` 会 `bind: address already in use` | 与上游 Codis 行为一致，非本次引入；多实例部署必须给每个实例不同的 `admin_addr`（配置文件级） | A2 |
| `scripts/validation/env/*` 仍生成旧 coordinator 配置 | 本地验证脚本产出的仍是旧线 coordinator 参数，直接使用会被拒绝 | 属后续验证工作（与 `scripts/redis8_local_validation.py` 一并处理），不作为新线建群证据 | A7、发布说明 |
| 第一轮 code review：W-1（`Close` 竞态遗漏撤销新授予 lease）、N-1（ansible 文档矛盾）、N-2（无端口 HTTPS 预检） | 契约 §4「关闭先取消并等待注册循环，再有界撤销本实例 lease」的落地、文档一致性、无端口 HTTPS endpoint 的可用性 | **已闭环**：W-1 改为等待循环退出后读回最终 lease 并去重撤销、N-1 修正两处文档、N-2 新增 `probeAddr` 补默认端口；修法与新增测试见 §12；`go.mod`/`go.sum` 未再改动 | §12、A1、A5 |

## 11. 交付说明（本地签收 + A6 真机）

契约末段要求交付时列出 Codis SHA、etcd/server 与 client/v3 版本、实际命令与结果、A0–A7 逐项结论与待验项。以下为签收口径：§11.2 的命令与产物为本地（macOS）签收，A6 真机（2026-11-28）的环境与结论见 §11.1 的 A6 行与 §11.3。

### 11.1 版本与来源

| 项 | 值 |
| --- | --- |
| Codis 基线 | `c635ade0424a1448fb251f32b88d64ca7cf9a555`（旧线终点，= `origin/master`） |
| Codis 交付改动 | 基于 **HEAD `3a5890d42a6366bddc86e1ed218081323600ae90`**（仅多一份契约文档）的工作树，**未提交**；A6 真机在同一工作树构建与验证 |
| etcd server（本地 P0） | 3.7.4，由本地仓库标签 `v3.7.4`（`2e8a5c99e4982c8d4c521be15364ea4c5c68a489`）经 `git archive` 导出后自建 `etcd`/`etcdctl`/`etcdutl`；未使用 release 包 |
| etcd server（**A6 真机，2026-11-28**） | **v3.7.4 官方 release 包**：本机下载并校验 SHA256 后上传三台主机（`etcd Version 3.7.4 GitSHA 2e8a5c9`，`etcdctl version: 3.7.4`，存储版本 3.7.0）；三成员 HTTP 客户端端口（无 TLS）+ `--auth-token simple` + RBAC（`root`/`dashboard`/`proxy`/`admin`，角色 `codis-rw` 对 `/codis3/` readwrite）；部署于华为云 `c7.xlarge.2`（4C8G，Ubuntu 22.04.5，kernel 5.15.0-186） |
| etcd client | `go.etcd.io/etcd/client/v3 v3.7.4`（MVS 带入 `api/v3`/`client/pkg/v3` v3.7.4、gRPC v1.83.2、protobuf v1.36.11） |
| 构建环境（本地） | Go 1.27.4 darwin/arm64，`CGO_ENABLED=1`，默认 build tags；`-tags cgo_jemalloc` 另验通过 |
| 构建环境（**A6 真机**） | host1 **Linux 原生 `make`**，go1.27.4 linux/amd64（`/topom` 回报 `2026-11-28 01:12:04 +0800`）；`codis-proxy -tags=cgo_jemalloc` dep 35；`codis-server` Redis 8.6.3 / jemalloc-5.3.0；产物 sha256 前缀 proxy `69d79553…`、codis-server `a80398c6…`（dashboard `6985b125…`、admin `53130fc4…`），三台一致 |
| 依赖图 | `go list -deps ./cmd/... ./pkg/...` 35 模块；zk/consul 0 包；ha/shake 0 个 etcd/gRPC 包 |

### 11.2 实际命令与结果

```sh
make gotest                                    # 19.4s，28 包 ok
make                                           # 28.2s（codis-server 增量）
go test ./cmd/... ./pkg/...
CODIS_ETCD_SERVER_BINARY=$P0/etcd-bin/etcd go test ./pkg/models/etcd/ ./pkg/proxy/...
# 端到端脚本目录：$P0/e2e-5b/（gen-certs、start-etcd、bootstrap-auth、gen-conn-files、precheck-product、stop-all、scan-secrets）
```

产物（darwin/arm64，默认 tags）：dashboard 33.8MB/34 dep、proxy 34.3MB/35、admin 24.7MB/34、ha 13.3MB/19、fe 23.0MB/18、shake 14.3MB/18、codis-server 8.6.3。`$P0 = ~/work/codis-etcd-p0`。

A6 真机（2026-11-28，host1 Linux 原生 `make`）的实际命令形态：`make`（go1.27.4 linux/amd64，proxy 带 `-tags=cgo_jemalloc`）、`codis-dashboard --config=… --etcd-config=…`、`codis-server`（Redis 8.6.3）、两台 `codis-proxy --config=… --dashboard=192.168.0.2:18080 --etcd-config=…`、`codis-admin --dashboard=… --create-group/--group-add/--slots-assign/--confirm` 建群与 slot 划分，场景用 `etcd-start.sh`/`etcd-stop.sh` 与 iptables DROP 规则构造，阶段快照由 `a6-phase.sh <phase>` 落盘。四场景步骤与逐条证据见 [validation/etcd_3_7_coordinator_a6_zh.md](validation/etcd_3_7_coordinator_a6_zh.md)（证据原始文件在本地 `.runtime/codis-a6/a6/`，不入库）。

### 11.3 A0–A7 逐项结论

| 编号 | 结论 | 依据 |
| --- | --- | --- |
| A0 依赖调研 | **通过** | P0 四项交付齐备（§3） |
| A1 配置与入口 | **通过（本地端到端）** | 四组件对单成员 mTLS+认证 etcd 的 21 项负向 + 正向（§8.2、§8.4） |
| A2 建群与隔离 | **通过（本地端到端）** | 2 group/2 codis-server/1024 slot、同前缀隔离、重启一致（§8.2、§8.4） |
| A3 锁与迟到请求 | **通过（本地进程级）** | 双实例争锁、`kill -9` 后按 revision 解锁、接管与迟到请求失败（§8.2、§8.4） |
| A4 结果未知 | **通过（测试替身）** | 故障注入六类分支（§8.2） |
| A5 混合 Proxy | **通过（本地单成员）** | 在线注册/发现集成 + 生产 TTL `GrantedTTL=30`（§8.2、§8.4） |
| A6 Linux 发布门槛 | **通过（三成员 Linux 真机，2026-11-28 签收）** | 四场景 S-A leader 切换 / S-B 隔离单成员（peer 隔离主口径）/ S-C 失去与恢复多数派 / S-D 正常退出均通过，S-A/B/C 故障窗口负载 0 失败、锁 owner/create_revision 不变、在线记录误删 0；S-D 的 40 条/台 `Connection refused` 为主动停机后的负载计数。启动期非零退出与全隔离变体已按契约 v1.3 裁决为符合预期（§7 R5/R6）。明细见 §8.2 A6、§10 与 [validation/etcd_3_7_coordinator_a6_zh.md](validation/etcd_3_7_coordinator_a6_zh.md) |
| A7 构建与清理 | **通过（本地 + A6 真机）** | 干净副本 build/vet/test/cmp 全一致、帮助与默认配置无旧后端（§8.1 第 5 步）；A6 在 host1 Linux 原生 `make` 构建并运行，场景结束仅剩 etcd 进程、iptables DROP 规则全部删除 |

**待验项**：Linux 平台上的 x509 证书文案（A6 的认证/连接/租约路径已在 Linux 实测，x509 具体文案未单独验证）；`linux/arm64` 与交叉工具链构建；证书轮换与 CRL/吊销；auth Token 模式与 token 刷新；U2–U8、U10–U11 的运行缺口。三成员 quorum 场景已于 2026-11-28 真机通过并签收（A6）；A6 遗留项均已裁决闭环（§10 “无多数派时新起的 dashboard 非零退出”按契约 v1.3 §2 执行，单成员隔离主口径为 peer 隔离）。

## 12. 评审与修复（第一轮 code review，2026-11-27）

**结论**：`approve_with_notes`；最高严重级别 **Warning**（唯一的 Warning 为 W-1）。本轮 W-1/N-1/N-2 三项已闭环，复核以 §12.3 命令为准。

### 12.1 问题、修法与验证

- **W-1 [Warning] `Registry.Close` 竞态遗漏撤销新授予的 lease**。现象：注册循环通过 `ctx.Err()` 检查后进入 `GrantLease`；此时 `Close` 已在 `r.mu` 下快照到 `leaseID=0` 并置零；随后 grant 成功、`setLease(ID1)`、`bind` 写键，`keepAlive` 因 ctx 已取消立即返回，循环退出，`Close` 以旧快照 `0` 跳过 `Revoke`，记录残存至 TTL（≤30s），偏离契约 §4「关闭先取消并等待注册循环，再有界撤销本实例 lease」。修法：`Close` 开头仍在 `r.mu` 下置零 `r.leaseID`/`r.client` 以阻止后续轮次复用，但**在等待 `<-done`（或 `stopWait` 超时）之后**再读取循环最终设置的 `r.leaseID`，与本轮开始前的快照一起逐个有界撤销（去重），撤销失败记 Error 交 TTL 收敛，之后 `client.Close()`（超时路径同样读回尽力撤销，且全程不在持 `r.mu` 时发起 RPC，无死锁）。为配合 `Close` 清空 `r.client`，注册循环的 `bind`/`abandonLease` 改用本轮已捕获的 client（`bindWith(client, …)`、`abandonLease(client, …)`），使正在收尾的一轮仍能写键与撤销。验证：新增 `TestCloseRevokesLeaseGrantedWhileClosing`（fake `GrantLease` 阻塞至 `Close` 已开始并经 `onCloseWait` 释放后返回新 lease 并完成 bind；断言以新 ID 撤销恰好一次、记录被删除、`Close` 返回 nil）；`TestCloseIsIdempotentAndOrdered` 继续通过。
- **N-1 [Nit] 文档自相矛盾**。现象：`doc/tutorial_zh.md:204`、`doc/tutorial_en.md:263` 曾把 `ansible/` playbook 写成旧部署示例、未适配 etcd，与发布说明及模板事实（`ansible/group_vars/all` 的 `etcd_endpoints` 为连接地址，凭据/mTLS 走 `--etcd-config`）不一致。修法：两处改为「已按 etcd 新线调整」的一致表述并指向 `etcd_endpoints`/`--etcd-config`。验证：对上述短语执行 `grep` 已不再命中 ansible 两处（仅余 filesystem coordinator 的既有说明）。
- **N-2 [Nit] `validateEndpoint` 接受无端口 HTTPS，但 TLS 预检的 `tls.Dialer` 要求显式端口**。现象：`https://host`（无端口）通过校验后，预检以无端口地址 dial 失败。决定（主代理裁决）：不拒绝该形式（gRPC resolver 对无端口目标默认 443，SDK 本可工作），在预检探测地址上按 scheme 补默认端口；不改写用户配置的 `endpoints`。修法：新增纯函数 `probeAddr(*url.URL)`（`https`→443、`http`→80，显式端口与 IPv6 字面量原样），`preflightTLS`/`probeEndpointTLS` 经它取探测地址。验证：新增 `TestProbeAddr`（`https://etcd.example.com`→`etcd.example.com:443`、`https://h:2379` 原样、`http://h`→`h:80`、`https://[::1]`→`[::1]:443`）与 `TestValidateEndpointAllowsPortlessHTTPS`（`validateEndpoint("https://etcd.example.com")` 仍通过）；需真实 443 端口的无端口 HTTPS 预检端到端用例因需特权端口绑定而未纳入（跳过）。

### 12.2 已知 / 接受（不阻塞合并）— 评审列出的 Residual Risks

1. `Topom.Close` 不等待 `serveAdmin` 5s drain 即释放锁（BASE 既有）。
2. FE 动态发现每 5s keys-only 全前缀扫描，复杂度 O(键数)。
3. A1–A5 的端到端证据位于本机路径，未随仓库分发。
4. R1–R6 修订由用户在 2026-11-27/28 的裁决中确认（此处仅为事实陈述）。

### 12.3 复核命令与结果

`gofmt -l ./pkg/proxy/etcdonline ./pkg/models/etcd`（空）；`go build ./...`；`go vet ./pkg/proxy/etcdonline/ ./pkg/models/etcd/`；`go test ./pkg/proxy/etcdonline/ ./pkg/models/etcd/ ./pkg/proxy/ -count=1`；`go test -race ./pkg/proxy/etcdonline/ -count=3`；`CODIS_ETCD_SERVER_BINARY=$HOME/work/codis-etcd-p0/etcd-bin/etcd go test ./pkg/proxy/etcdonline/ ./pkg/models/etcd/ -count=1 -v` —— 全部通过；结束 `pgrep -fl 'etcd-bin'` 无残留。

评审人的边界事件：评审中曾误用 `-mod=mod` 运行并改动依赖文件，事后已恢复为默认 `-mod=readonly`；`go.mod` 现为 10 个直接依赖 + 47 个 `// indirect`，与 HEAD 差异 38 行（`go.sum` 32 行），本轮复核未再改动。

