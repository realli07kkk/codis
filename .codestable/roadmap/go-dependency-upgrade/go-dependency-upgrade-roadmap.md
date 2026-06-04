---
doc_type: roadmap
slug: go-dependency-upgrade
status: active
created: 2026-06-04
last_reviewed: 2026-06-04
tags: [go, modules, dependency-upgrade, maintenance]
related_requirements: [platform-release-artifacts, redis-cluster-service]
related_architecture: [system-overview]
---

# Go 外部依赖升级

## 1. 背景

当前仓库已经完成 Go modules 迁移，`go.mod` 是 Go 依赖入口。后续依赖升级不能回到旧 `vendor/` / `Godeps/` 路径，也不能用一次性全量 `go mod tidy` 把依赖图机械重写。

第一版 roadmap 把 `go.mod` 中 47 个显式 `require` 外部 module 拆成 47 个子 feature，颗粒度过细。更新后的目标是保留完整版本调查矩阵，但把执行项合并为 10 个按风险域、父依赖链和验收方式划分的子 feature。

## 2. 范围与明确不做

### 本 roadmap 覆盖

- `go.mod` 中 47 个显式 `require` 外部 module：19 个 direct，28 个 indirect。
- 默认构建路径 `go test ./cmd/... ./pkg/...` 实际触达的 38 个外部 module，以及 `-tags cgo_jemalloc` 下额外触达的 `github.com/spinlock/jemalloc-go`。
- 每个合并子 feature 覆盖一组外部 module，并在 feature-design 中逐个确认当前版本、目标版本、兼容性风险和验证命令。
- 对没有 tagged release 的 module，明确写出最新 `@latest` pseudo version，并要求 feature-design 决定保留还是升级到 pseudo。

### 明确不做

- 不在 roadmap 阶段直接改 `go.mod`、`go.sum` 或业务代码。
- 不把完整 `go list -m all` 的 116 个传递 module 全部建成子 feature；未写入 `go.mod` 的传递依赖由其父依赖升级时自然收敛。
- 不做旧 `github.com/coreos/etcd` 到新 `go.etcd.io/etcd/*` module path 的大迁移；这属于 coordinator 后端迁移，不是同一路径版本升级。
- 不升级 Go toolchain，也不改变 `go 1.26.1` module directive。
- 不修改 `extern/redis-8.6.3/`、前端资源、Docker 或部署脚本。

## 3. 模块拆分（概设）

```text
go-dependency-upgrade
├── dependency-inventory：依赖枚举、版本来源和升级规则
├── low-risk-runtime-stacks：network、配置、CLI、utility、Redis client 等低到中风险升级
├── service-integration-stacks：dashboard、coordinator、RDB analysis、metrics 等业务集成依赖升级
├── native-build-stack：jemalloc local replace 与 cgo 构建链
└── verification-gates：构建、测试、cgo_jemalloc 和运行期兼容性验收
```

### dependency-inventory

- **职责**：固定依赖清单、版本查询来源和“正式版本”的解释口径。
- **承载的子 feature**：全部 `dep-*` 合并子 feature。
- **触碰的现有代码 / 模块**：`go.mod`, `go.sum`。

### low-risk-runtime-stacks

- **职责**：处理通常不改变 Codis 运行协议的基础依赖，包括 `golang.org/x/*`、配置、CLI、UUID、常用数据结构和 Redis client。
- **承载的子 feature**：`dep-network-core-stack`, `dep-config-cli-utility-stack`, `dep-redis-client-stack`。
- **触碰的现有代码 / 模块**：`cmd/`, `pkg/`, `go.mod`, `go.sum`。

### service-integration-stacks

- **职责**：处理和 dashboard、coordinator、RDB analysis、metrics 绑定较深的依赖组，每组按运行期边界独立验证。
- **承载的子 feature**：`dep-dashboard-martini-stack`, `dep-coordinator-etcd-stack`, `dep-coordinator-zookeeper-stack`, `dep-coordinator-consul-stack`, `dep-rdb-analysis-stack`, `dep-metrics-stack`。
- **触碰的现有代码 / 模块**：`cmd/fe`, `cmd/dashboard`, `pkg/topom`, `pkg/models`, `pkg/utils`。

### native-build-stack

- **职责**：处理 `github.com/spinlock/jemalloc-go` 与本地 `third_party/jemalloc-go` replace 的升级和 cgo 验证。
- **承载的子 feature**：`dep-jemalloc-stack`。
- **触碰的现有代码 / 模块**：`third_party/jemalloc-go`, `pkg/utils/unsafe2`, `go.mod`, `go.sum`。

### verification-gates

- **职责**：给每个子 feature 提供统一验收命令和风险门槛。
- **承载的子 feature**：全部 `dep-*` 合并子 feature。
- **触碰的现有代码 / 模块**：`Makefile`, `cmd/`, `pkg/`, `third_party/jemalloc-go`。

## 4. 模块间接口契约 / 共享协议（架构层详设）

### 4.1 依赖版本调查契约

**方向**：feature-design -> Go toolchain

**形式**：Go module query

**契约**：

```bash
GOPROXY=https://proxy.golang.org,direct go list -m -u -json <module>
GOPROXY=https://proxy.golang.org,direct go list -m -json <module>@latest
GOPROXY=https://proxy.golang.org,direct go list -m -versions -json <module>
```

**约束**：

- `@latest` 是子 feature 的默认目标版本，除非它是 pseudo version、pre-release，或会引入明确兼容性风险。
- `go list -m -versions` 没有 tagged version 时，不能声称存在正式 release；只能写“无 tagged release，最新可解析版本为 pseudo”。
- `github.com/bytedance/sonic` 和 `github.com/bytedance/sonic/loader` 存在更新的 dev pre-release tag，但 `@latest` 分别解析为 `v1.15.1` 和 `v0.5.1`，子 feature 不应默认升到 dev pre-release。
- 查询失败、proxy 镜像不一致或 upstream module path 迁移时，子 feature 必须记录实际命令和错误，不用猜测版本。

### 4.2 合并子 feature 升级契约

**方向**：子 feature -> Go module manifest

**形式**：`go.mod` / `go.sum`

**契约**：

```text
feature_slug: string
module_set:
  - module_path: string
    current_version: string
    target_version: string
    scope: direct | indirect
    replace_path: string | null
    upgrade_mode: direct-go-get | parent-driven | retain-with-note
```

**约束**：

- 每个合并子 feature 覆盖一组强相关 module；feature-design 必须在 `module_set` 中逐个列出版本和处理策略。
- direct module 可用 `go get <module>@<target_version>` 定点升级；同组 direct module 可以在同一 feature 内按顺序提交到同一个 diff。
- indirect module 优先等待父 direct module 升级后自然收敛；若仍需要显式 pin，必须说明 `go mod why -m <module>` 的原因。
- `github.com/spinlock/jemalloc-go` 当前有本地 replace 到 `./third_party/jemalloc-go`，升级不是单纯改版本，必须同步评估本地 third_party 源码。
- 不运行无目标的全量 `go mod tidy`；需要收敛 `go.sum` 时由具体 feature 的验收命令驱动最小变化。

### 4.3 验收命令契约

**方向**：子 feature -> 构建/测试系统

**形式**：shell 命令

**契约**：

```bash
go test ./cmd/... ./pkg/...
make gotest
go build -tags cgo_jemalloc ./cmd/proxy
```

**约束**：

- 每个子 feature 至少运行目标 package 测试；影响 shared module、coordinator、proxy 或 dashboard 时运行 `go test ./cmd/... ./pkg/...`。
- coordinator 相关升级必须覆盖对应后端包：`pkg/models/etcd`、`pkg/models/zk`、`pkg/models/consul`。
- `github.com/spinlock/jemalloc-go` 或 `golang.org/x/sys` 相关升级必须跑 `go build -tags cgo_jemalloc ./cmd/proxy`。
- 批量阶段收口时再运行 `make gotest`；如果命令刷新 tracked 配置，必须在验收报告里说明并确认 diff。

### 4.4 版本矩阵

数据来源：2026-06-04 运行 `GOPROXY=https://proxy.golang.org,direct go list -m -u -json`、`go list -m <module>@latest`、`go list -m -versions`。`latest` 指 Go 工具当前解析的 `@latest`，不是一定有 tagged release。

| module | scope | current | latest |
|---|---:|---|---|
| `github.com/BurntSushi/toml` | direct | `v0.2.1-0.20160717150709-99064174e013` | `v1.6.0` |
| `github.com/coreos/etcd` | direct | `v3.3.27+incompatible` | `v3.3.27+incompatible` |
| `github.com/docopt/docopt-go` | direct | `v0.0.0-20160216232012-784ddc588536` | `v0.0.0-20180111231733-ee0de3bc6815` |
| `github.com/emirpasic/gods` | direct | `v1.9.0` | `v1.18.1` |
| `github.com/garyburd/redigo` | direct | `v1.0.1-0.20170208211623-48545177e92a` | `v1.6.4` |
| `github.com/go-martini/martini` | direct | `v0.0.0-20160908070901-fe605b5cd210` | `v0.0.0-20170121215854-22fa46961aab` |
| `github.com/google/uuid` | direct | `v1.5.0` | `v1.6.0` |
| `github.com/hashicorp/consul/api` | direct | `v1.34.2` | `v1.34.3` |
| `github.com/hdt3213/rdb` | direct | `v1.3.2` | `v1.3.2` |
| `github.com/influxdata/influxdb` | direct | `v1.1.1-0.20170109231301-8c2cfd14af25` | `v1.12.4` |
| `github.com/martini-contrib/binding` | direct | `v0.0.0-20160701174519-05d3e151b6cf` | `v0.0.0-20160701174519-05d3e151b6cf` |
| `github.com/martini-contrib/gzip` | direct | `v0.0.0-20151124214156-6c035326b43f` | `v0.0.0-20151124214156-6c035326b43f` |
| `github.com/martini-contrib/render` | direct | `v0.0.0-20150707142108-ec18f8345a11` | `v0.0.0-20150707142108-ec18f8345a11` |
| `github.com/oxtoacart/bpool` | direct | `v0.0.0-20150712133111-4e1c5567d7c2` | `v0.0.0-20190530202638-03653db5a59c` |
| `github.com/samuel/go-zookeeper` | direct | `v0.0.0-20161028232340-1d7be4effb13` | `v0.0.0-20201211165307-7117e9ea2414` |
| `github.com/spinlock/jemalloc-go` | direct | `v0.0.0-20161230074307-26719b2ee618` | `v0.0.0-20201010032256-e81523fb8524` |
| `github.com/ugorji/go` | direct | `v1.2.14` | `v1.2.14` |
| `golang.org/x/net` | direct | `v0.51.0` | `v0.55.0` |
| `gopkg.in/alexcesaro/statsd.v2` | direct | `v2.0.0` | `v2.0.0` |
| `github.com/armon/go-metrics` | indirect | `v0.4.1` | `v0.5.4` |
| `github.com/bytedance/gopkg` | indirect | `v0.1.3` | `v0.1.4` |
| `github.com/bytedance/sonic` | indirect | `v1.15.0` | `v1.15.1` |
| `github.com/bytedance/sonic/loader` | indirect | `v0.5.0` | `v0.5.1` |
| `github.com/cloudwego/base64x` | indirect | `v0.1.6` | `v0.1.7` |
| `github.com/codegangsta/inject` | indirect | `v0.0.0-20150114235600-33e0aa1cb7c0` | `v0.0.0-20150114235600-33e0aa1cb7c0` |
| `github.com/coreos/go-semver` | indirect | `v0.3.1` | `v0.3.1` |
| `github.com/fatih/color` | indirect | `v1.16.0` | `v1.19.0` |
| `github.com/go-viper/mapstructure/v2` | indirect | `v2.4.0` | `v2.5.0` |
| `github.com/hashicorp/errwrap` | indirect | `v1.1.0` | `v1.1.0` |
| `github.com/hashicorp/go-cleanhttp` | indirect | `v0.5.2` | `v0.5.2` |
| `github.com/hashicorp/go-hclog` | indirect | `v1.5.0` | `v1.6.3` |
| `github.com/hashicorp/go-immutable-radix` | indirect | `v1.3.1` | `v1.3.1` |
| `github.com/hashicorp/go-multierror` | indirect | `v1.1.1` | `v1.1.1` |
| `github.com/hashicorp/go-rootcerts` | indirect | `v1.0.2` | `v1.0.2` |
| `github.com/hashicorp/golang-lru` | indirect | `v0.5.4` | `v1.0.2` |
| `github.com/hashicorp/serf` | indirect | `v0.10.1` | `v0.10.2` |
| `github.com/json-iterator/go` | indirect | `v1.1.12` | `v1.1.12` |
| `github.com/klauspost/cpuid/v2` | indirect | `v2.2.9` | `v2.3.0` |
| `github.com/mattn/go-colorable` | indirect | `v0.1.13` | `v0.1.15` |
| `github.com/mattn/go-isatty` | indirect | `v0.0.20` | `v0.0.22` |
| `github.com/mitchellh/go-homedir` | indirect | `v1.1.0` | `v1.1.0` |
| `github.com/modern-go/concurrent` | indirect | `v0.0.0-20180306012644-bacd9c7ef1dd` | `v0.0.0-20180306012644-bacd9c7ef1dd` |
| `github.com/modern-go/reflect2` | indirect | `v1.0.2` | `v1.0.2` |
| `github.com/twitchyliquid64/golang-asm` | indirect | `v0.15.1` | `v0.15.1` |
| `golang.org/x/arch` | indirect | `v0.9.0` | `v0.27.0` |
| `golang.org/x/exp` | indirect | `v0.0.0-20260218203240-3dfff04db8fa` | `v0.0.0-20260603202125-055de637280b` |
| `golang.org/x/sys` | indirect | `v0.41.0` | `v0.45.0` |

## 5. 子 feature 清单

1. **dep-network-core-stack** — 升级 `golang.org/x/net` 与 `golang.org/x/sys`，完成依赖升级最小闭环并验证 cgo 相关链路。
   - 所属模块：low-risk-runtime-stacks
   - 覆盖 module：`golang.org/x/net`, `golang.org/x/sys`
   - 依赖：无
   - 状态：done
   - 对应 feature：`2026-06-04-dep-network-core-stack`

2. **dep-config-cli-utility-stack** — 升级或确认配置、CLI、UUID、通用数据结构和 codec 相关 module。
   - 所属模块：low-risk-runtime-stacks
   - 覆盖 module：`github.com/BurntSushi/toml`, `github.com/docopt/docopt-go`, `github.com/google/uuid`, `github.com/emirpasic/gods`, `github.com/oxtoacart/bpool`, `github.com/ugorji/go`
   - 依赖：无
   - 状态：done
   - 对应 feature：`2026-06-04-dep-config-cli-utility-stack`

3. **dep-redis-client-stack** — 升级 `github.com/garyburd/redigo`，验证 Redis 连接、AUTH、SELECT、迁移和脚本调用路径。
   - 所属模块：low-risk-runtime-stacks
   - 覆盖 module：`github.com/garyburd/redigo`
   - 依赖：无
   - 状态：done
   - 对应 feature：`2026-06-04-dep-redis-client-stack`

4. **dep-dashboard-martini-stack** — 升级或确认 Martini web stack，验证 dashboard/FE middleware、binding、gzip 和 render 行为。
   - 所属模块：service-integration-stacks
   - 覆盖 module：`github.com/go-martini/martini`, `github.com/martini-contrib/binding`, `github.com/martini-contrib/gzip`, `github.com/martini-contrib/render`, `github.com/codegangsta/inject`
   - 依赖：无
   - 状态：done
   - 对应 feature：`2026-06-04-dep-dashboard-martini-stack`

5. **dep-coordinator-etcd-stack** — 确认旧 etcd client 栈保留或做同路径可行升级，明确现代 etcd module path 迁移不纳入本条。
   - 所属模块：service-integration-stacks
   - 覆盖 module：`github.com/coreos/etcd`, `github.com/coreos/go-semver`, `github.com/json-iterator/go`, `github.com/modern-go/concurrent`, `github.com/modern-go/reflect2`
   - 依赖：无
   - 状态：planned
   - 对应 feature：未启动

6. **dep-coordinator-zookeeper-stack** — 升级 `github.com/samuel/go-zookeeper`，验证 Zookeeper coordinator/Jodis 后端。
   - 所属模块：service-integration-stacks
   - 覆盖 module：`github.com/samuel/go-zookeeper`
   - 依赖：无
   - 状态：planned
   - 对应 feature：未启动

7. **dep-coordinator-consul-stack** — 升级 Consul API 与 HashiCorp 依赖链，验证 Consul coordinator/Jodis 后端和 Session/KV watch 语义。
   - 所属模块：service-integration-stacks
   - 覆盖 module：`github.com/hashicorp/consul/api`, `github.com/armon/go-metrics`, `github.com/fatih/color`, `github.com/go-viper/mapstructure/v2`, `github.com/hashicorp/errwrap`, `github.com/hashicorp/go-cleanhttp`, `github.com/hashicorp/go-hclog`, `github.com/hashicorp/go-immutable-radix`, `github.com/hashicorp/go-multierror`, `github.com/hashicorp/go-rootcerts`, `github.com/hashicorp/golang-lru`, `github.com/hashicorp/serf`, `github.com/mattn/go-colorable`, `github.com/mattn/go-isatty`, `github.com/mitchellh/go-homedir`, `golang.org/x/exp`
   - 依赖：`dep-network-core-stack`
   - 状态：planned
   - 对应 feature：未启动

8. **dep-rdb-analysis-stack** — 升级或确认 RDB parser 与 sonic/bytedance 依赖链，验证 dashboard RDB Analysis 和 remote fetch analysis。
   - 所属模块：service-integration-stacks
   - 覆盖 module：`github.com/hdt3213/rdb`, `github.com/bytedance/gopkg`, `github.com/bytedance/sonic`, `github.com/bytedance/sonic/loader`, `github.com/cloudwego/base64x`, `github.com/klauspost/cpuid/v2`, `github.com/twitchyliquid64/golang-asm`, `golang.org/x/arch`
   - 依赖：`dep-network-core-stack`
   - 状态：planned
   - 对应 feature：未启动

9. **dep-metrics-stack** — 升级或确认 InfluxDB/statsd metrics module，验证 proxy/topom metrics 上报路径。
   - 所属模块：service-integration-stacks
   - 覆盖 module：`github.com/influxdata/influxdb`, `gopkg.in/alexcesaro/statsd.v2`
   - 依赖：无
   - 状态：planned
   - 对应 feature：未启动

10. **dep-jemalloc-stack** — 评估 `github.com/spinlock/jemalloc-go` 最新 pseudo version，并同步处理 `third_party/jemalloc-go` local replace。
    - 所属模块：native-build-stack
    - 覆盖 module：`github.com/spinlock/jemalloc-go`
    - 依赖：`dep-network-core-stack`
    - 状态：planned
    - 对应 feature：未启动

**最小闭环**：第 1 条 `dep-network-core-stack` 做完后，项目能完成一次合并依赖升级、`go.mod/go.sum` 最小变更、`go test ./cmd/... ./pkg/...` 和 `go build -tags cgo_jemalloc ./cmd/proxy` 验证闭环。

## 6. 排期思路

先做 `dep-network-core-stack` 作为最小闭环，因为它覆盖 direct `golang.org/x/net` 和跨平台/OS 相关 `golang.org/x/sys`，能验证依赖升级流程和 cgo 构建边界。随后处理低风险 utility、Redis client 和 Martini stack；再处理 coordinator 后端和 RDB analysis 这类运行期集成依赖；最后处理 `jemalloc-go` local replace。技术依赖之外的执行优先级可按用户最关心的风险域调整。

## 7. 观察项

- `github.com/coreos/etcd` 最新同路径版本仍是 `v3.3.27+incompatible`；若目标是跟进现代 etcd client，需要另起 roadmap/feature 做 module path 和 API 迁移。
- 多个 Martini 相关 module 只有 pseudo version 或多年无 tagged release；如果安全或维护性目标强，应评估替换 web 框架，而不是只升 patch。
- `github.com/spinlock/jemalloc-go` 有本地 replace，升级必须同步 third_party 源码，不能只改 `require` 版本。
- `go.mod` 中一些 indirect 条目实际被默认构建触达；后续子 feature 如果发现项目直接 import，应顺手把 `// indirect` 分类修正并说明。

## 8. 变更日志

- 2026-06-04：创建 roadmap；按 `go.mod` 47 个外部 module 建立一包一子 feature，并记录 2026-06-04 的 `@latest` 查询结果。
- 2026-06-04：将 47 个子 feature 合并为 10 个按风险域、父依赖链和验证方式划分的子 feature；版本矩阵保留全部 47 个 module。
- 2026-06-04：完成 `dep-network-core-stack`，将 `golang.org/x/net` 升级到 `v0.55.0`、`golang.org/x/sys` 升级到 `v0.45.0`，并通过默认 cmd/pkg 测试与 `cgo_jemalloc` proxy 构建。
- 2026-06-04：完成 `dep-redis-client-stack`，将 `github.com/garyburd/redigo` 升级到 `v1.6.4`，并通过 Redis client、topom 和默认 cmd/pkg 测试。
- 2026-06-04：完成 `dep-dashboard-martini-stack`，将 `github.com/go-martini/martini` 升级到 `v0.0.0-20170121215854-22fa46961aab`，确认 `binding`、`gzip`、`render` 和 `inject` 已是当前 `@latest`，并通过 proxy/topom/FE 目标测试与默认 cmd/pkg 测试。
