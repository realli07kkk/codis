# Go 现代化改造计划与回归验证

> 状态：已完成（macOS 本机回归通过；Linux 发布验证待执行）
> 日期：2026-11-22
> 范围：`cmd/`、`pkg/`、Go 测试与 Go 静态检查配置
> 当时排除：etcd 客户端迁移，以及 `pkg/models/etcd/` 下的任何改动（以下内容均为历史记录，该客户端现已移除）

本文是本轮 Go 现代化改造的执行依据和最终回归清单。顺序固定为：**先写方案与验证方法，再建立基线，然后分阶段实施，最后按本文逐项回归**。实施结果和偏离必须回写到本文末尾，不能只以“编译通过”作为完成标准。

## 1. 目标与原则

### 1.1 目标

1. 移除已归档或无必要的第三方依赖，优先使用当前 Go 标准库。
2. 将老旧的排序、错误判断、字符串处理、计时器和基准测试写法迁移到现代 Go API。
3. 用 `log/slog` 提供结构化日志能力，同时保留现有运维入口和诊断信息。
4. 统一管理后台协程的取消与退出，在 proxy 生命周期中引入 `context.Context` 和 `sync.WaitGroup`。
5. 在可证明所有权和生命周期的前提下减少 proxy 热路径分配与锁竞争；优化必须有基准对比，不凭感觉改写。
6. 为 CLI、HTTP 管理面、并发和热路径补充可重复的回归测试。

### 1.2 行为约束

- 不改变 Redis 协议、slot 路由、迁移、ACL、QPS limit、hot key cache、PITR、RDB Analysis 的外部语义。
- 不改变既有命令行参数名称、配置 key、HTTP API 路径、方法、鉴权规则和成功响应结构。
- 不修改 `third_party/`、`extern/`、Docker 或部署配置。
- 不运行全量 `go mod tidy`；`go.mod` / `go.sum` 只允许由本次实际替换驱动的最小编辑。
- 不为追求“现代 API 使用率”牺牲可审计性。标准库不能表达原有语义时，保留原实现并在本文记录理由。
- 性能改造必须先分析对象所有权，再补充回归和基准；不使用全局缓存改变请求之间的隔离性。
- 不夹带 etcd 迁移或已知排序算法问题的修复；需要改变外部行为的问题另行立项。

## 2. 明确排除与兼容边界

### 2.1 etcd 完全排除（历史记录）

以下内容和约束描述的是当时状态：

- `pkg/models/etcd/etcdclient.go` 及其 etcd v2 API 用法保持原样；
- `github.com/coreos/etcd` 依赖及版本保持原样；
- etcd 的 TTL、目录、CAS、watch 和 ephemeral 语义保持原样；
- 不做与 etcd 客户端迁移相关的测试或文档变更。

`pkg/utils/resolver.go` 和 `pkg/utils/redis/sentinel.go` 可以从 `golang.org/x/net/context` 改用标准库 `context`，但因为当时的 etcd 文件仍引用 `golang.org/x/net/context`，**本轮不删除 `golang.org/x/net` 模块**。

> 后续清理：etcd 客户端及其专属依赖已整体移除，本节只记录当时划分的改造范围，不构成“永久冻结、仍保留、不得修改”的约束；`golang.org/x/net` 仍由其他依赖使用。

### 2.2 兼容适配层

- 日志：以 `slog` 作为底层日志抽象，保留现有 printf 风格 API、滚动文件、错误栈和 `Panic*` 进程退出语义作为兼容层；新增结构化入口。批量改写所有日志文本或把库内退出改写成另一套控制流不属于行为等价改造。
- 错误：保留 `TracedError` 的堆栈诊断与 `Unwrap`，把根因判断迁到 `errors.Is/As`，让格式化错误支持 `%w` 链；不靠错误字符串判等。
- proxy context：覆盖进程和后台任务生命周期，不为每个 Redis 命令虚构超时，也不改变现有连接关闭时机。
- 热路径：只实施所有权可证明、测试可覆盖的池化或替换。若 `RequestChan` 等组件承载了 channel 不具备的语义，则保留并记录，而不是强行删减。

## 3. 实施阶段

### M0：基线与刻画测试

1. 记录当前 `go version`、工作区状态和依赖清单。
2. 运行 `go test ./cmd/... ./pkg/...`，记录已有失败，避免把历史问题算入本次改造。
3. 为 CLI 参数解析、HTTP 路由/中间件、错误链、日志级别与退出路径补充刻画测试。
4. 运行 proxy 编解码、Request、RequestChan 等相关基准，保存为改造前基线。

**退出条件**：基线结果写入本文；新增测试在原实现上通过。

### M1：低风险依赖与清理

1. 非 etcd 两处 `golang.org/x/net/context` 改为标准库 `context`。
2. InfluxDB 1.x client 改为上游独立维护的 `github.com/influxdata/influxdb1-client/v2` 兼容包，保持 HTTP 写入配置、measurement、tags、fields 不变。
3. 删除已无引用的 `pkg/utils/math2`，用手写 `math.MaxInt` 替代自定义常量。
4. 处理 `go.mod` 中 bpool 的直接/indirect 归属；martini 移除后确认它不再进入构建图。
5. 引入静态检查配置，但不接 CI、不增加构建期网络依赖。

**验证**：目标包测试；`go list -deps` 检查旧 import；检查 module diff 最小化。

### M2：标准库与语言惯用法

1. 将可等价迁移的 `sort.Strings/Ints/Slice/SliceStable/Sort` 改为 `slices.Sort/SortFunc/SortStableFunc`，必要时使用 `cmp.Compare`、`cmp.Or`；删除只服务旧排序接口的类型。
2. `topom_slots.go` 中依赖外部可变状态的排序和 `sort.Search` 不做算法修复；如替换，必须用刻画测试证明结果与旧实现一致，否则保留并记录。
3. 用 `math.MaxInt` 替代手写常量；移除 `net.Error.Temporary()` 分支并改为当前支持的错误分类方式。
4. INFO 等文本解析使用 `strings.Cut` / `bytes.Cut` 和 Go 1.24 迭代器 API，避免先构造中间切片或不必要的全量复制。
5. `bytesize` / `timesize` 的配置解析去掉重复正则，保持既有单位、纯数字秒、错误文本类别和序列化行为。
6. 轮询循环中的 `time.After` 改为可停止的 ticker/timer；不得在热路径为“现代写法”增加分配。

**验证**：排序、配置解析、INFO 解析单测；相关包全测；grep 检查旧 API 残留。

### M3：错误处理与结构化日志

1. `errors.Equal/NotEqual/Cause` 的调用点迁移为标准库 `errors.Is/As`；兼容包装内部不再比较错误字符串。
2. 保留 `TracedError` 堆栈与 `Unwrap`，为 `%w` 包装补测试，确保 RPC remote error 和 Redis sentinel 的错误分类不变。
3. 用 `slog.Logger` 和可扩展 handler 支撑现有 logger，保留级别、堆栈、rolling writer、脱敏和现有 printf API；提供结构化日志入口。
4. 不批量改变 649 个日志调用点的文本；不改变 `Panic*` 的退出码。新增测试覆盖级别过滤、结构化字段、错误链和 writer 关闭。

**验证**：`pkg/utils/log`、`pkg/utils/errors`、sentinel、RPC 测试；相关包全测；日志样例对比。

### M4：CLI 与指标依赖

1. 用标准库 `flag.FlagSet` + 显式互斥/必选校验替代 docopt；五个入口使用类型化参数，不再依赖 `map[string]any` 的 nil/false 双缺省语义。
2. 保持 proxy、dashboard、admin、ha、fe 的 flag 名称、布尔参数、`--default-config`、`--version` 和错误退出行为。
3. 先为 coordinator 四选一、admin 互斥参数、PITR 与 RDB Analysis 参数补解析测试，再迁移实现。
4. StatsD 只用 UDP Gauge 的部分改为小型内部 writer，保持 metric 名、prefix 清洗、周期和 UDP 协议；删除 `alexcesaro/statsd.v2` 依赖。

**验证**：所有 CLI 解析测试；`--help` / `--version` / `--default-config`  smoke test；StatsD UDP 本地收包测试。

### M5：HTTP 管理面与 FE

1. 先按现有 martini 路由建立契约测试：路径参数、通配符、方法、xauth、JSON binding、gzip、Content-Type、审计日志和错误状态码。
2. 用 Go 1.22+ `http.ServeMux` 的 method/host/path pattern 替代 martini 路由，handler 使用 `http.ResponseWriter`、`*http.Request` 和 `Request.PathValue`。
3. 用标准库中间件替代 Recovery、gzip、render redirect 和 JSON binding；保持非 GET `/api/` 审计日志的 secret 脱敏。
4. FE 静态文件与反向代理改用 `net/http`；保持 dashboard 列表和静态资源路径。
5. 删除 martini、martini-contrib 及不再需要的 bpool 传递依赖。

**验证**：proxy/topom/FE 路由契约测试；上传大 body、通配路径和 gzip 测试；`go list -deps` 确认 martini/bpool 消失。

### M6：协程生命周期与 context

1. 把 `backend.go`、`stats.go` 中 `init()` 启动的永久协程收编到显式生命周期，允许测试隔离与退出。
2. 为 `Proxy` 增加根 context、cancel 和 WaitGroup；后台任务统一注册，`Close` 取消并等待，保留幂等关闭。
3. 给裸 `go func` 根部补一致的 panic 兜底；不能让 recover 吞掉错误后留下假健康状态。
4. 保留 `topom.runPeriodic` 的语义并复用统一 ticker/cancel 方式；清理 RPC idle-connection 任务的退出路径。
5. 检查 session 跨协程状态的同步，只修明确的 data race，不借现代化名义改变请求时序。

**验证**：`go test -race` 覆盖 proxy/topom 目标测试；重复 Start/Close、退出等待、panic 隔离测试；确认无 goroutine 泄漏。

### M7：proxy 热路径

候选项按以下顺序独立实施、独立基准：

1. `Request` / `sync.WaitGroup` / `Resp` 的分配：先画清 reader、writer、backend、subrequest 的所有权，再决定内嵌、复用或 `sync.Pool`；任何 pool 必须完整 reset，且不得跨请求保留敏感数据。
2. `Seed16` 移除指针地址哈希，改用 `math/rand/v2` 或请求内预计算 seed，保持拆分请求的随机打散作用。
3. 审计 `RequestChan` 的 close、长度、等待和前后入队语义；能用 channel 等价表达才替换，否则保留。
4. 将只读命令字面量提升为共享值；RESP 编解码和 INFO 解析避免可证明多余的复制。
5. opstats 在保持统计一致性的前提下减少全局锁；排序 API 与 M2 同步迁移。

**验证**：协议与并发测试、race、相同参数基准对比。若某项没有可靠收益或所有权无法证明，保留原实现并记录，不以牺牲正确性换取 benchmark 数字。

### M8：测试与静态检查

1. 测试断言失败改为 `testing.TB` 驱动，避免 `log.Panic` / `os.Exit` 吞掉测试上下文；保留共享 fake Redis 设施。
2. 基准循环迁移到 `b.Loop()`，确保结果不会被编译器优化掉。
3. 仅在无共享状态时引入 `t.Parallel()`；环境变量测试使用 `t.Setenv()`。
4. 增加 gofmt、`go vet` 和静态检查入口；不修改 CI，不强制所有开发机安装额外工具。

**验证**：目标测试、全量测试、race、vet、gofmt；故意制造一次断言失败确认输出包含测试上下文（可在临时复现中验证，不留失败用例）。

### M9：最终回归与审查

按第 4 节完整执行，并将实际命令、结果、基准和保留项写入第 5 节。最后审查完整 diff，确认没有 etcd、生成物、部署文件或无关改动。

## 4. 回归验证矩阵

最终完成必须逐项勾选；无法执行的项目必须写明环境原因，不能静默跳过。

| 编号 | 验证项 | 命令 / 方法 | 通过标准 |
|---|---|---|---|
| R01 | 格式 | 对全部改动 `.go` 文件执行 `gofmt` | 无格式 diff |
| R02 | 目标包单测 | `go test ./pkg/utils/... ./pkg/models/... ./pkg/proxy/... ./pkg/topom/... ./cmd/...` | 全部通过 |
| R03 | 项目测试 gate | `make gotest` | 全部通过 |
| R04 | 编译 | `go build ./cmd/... ./pkg/...` | 全部通过 |
| R05 | race | `go test -race ./pkg/proxy ./pkg/topom ./pkg/utils/log ./pkg/utils/errors` | 无 race、无失败 |
| R06 | vet | `go vet ./cmd/... ./pkg/...` | 无新增诊断 |
| R07 | CLI 契约 | 五个入口的解析测试及 `--help`、`--version`、默认配置 smoke test | 参数和输出类别兼容 |
| R08 | HTTP 契约 | proxy/topom/FE 路由、中间件、xauth、JSON、gzip、上传测试 | 路径、状态码、响应结构兼容 |
| R09 | 日志与错误 | 日志级别/字段/rolling、errors.Is/As/%w、RPC/sentinel 测试 | 错误分类和诊断不丢失 |
| R10 | 协程退出 | Start/Close 重复调用、取消等待、panic 隔离、goroutine 检查 | 无泄漏、无 panic 出进程 |
| R11 | 性能基准 | 与 M0 相同 package、bench、benchtime、count 对比 | 无不可解释回退；记录环境噪声 |
| R12 | 依赖图 | `go list -deps ./cmd/... ./pkg/...` + import grep | docopt/martini/statsd/旧 influx client 消失；当时 etcd 与 x/net 保留 |
| R13 | module 卫生 | 审查 `go.mod` / `go.sum` diff | 无全量 tidy、无无关升级 |
| R14 | 生成物与配置 | `git status --short` / `git diff -- config/` | 无 bin、临时文件或意外配置变化 |
| R15 | 排除项 | `git diff -- pkg/models/etcd go.mod` | 当时 etcd 源码与 `coreos/etcd` 版本不变；目录和依赖现已删除，本项不再适用 |
| R16 | 全量 diff 审查 | 审查所有变更和新增测试 | 无行为夹带、敏感信息、遗留调试代码 |

macOS 上的结论只覆盖本地 Go 单元、集成和微基准。Linux 生产性能、跨平台 cgo/jemalloc、真实 Redis/coordinator 集群切换不在本机回归的替代范围内；需要发布时仍应执行 `doc/redis8_validation_zh.md` 的正式 gate。

## 5. 执行记录（实施后填写）

### 5.1 基线

- 时间：2026-11-22；环境：macOS arm64、Apple M2 Pro、Go 1.27.4。
- `go test ./cmd/... ./pkg/...`：通过，无历史失败。
- `go vet ./cmd/... ./pkg/...`：通过，无历史诊断。
- 依赖基线：`go list -m all` 共 271 行，`go list -deps ./cmd/... ./pkg/...` 共 316 行。
- 基准命令：`go test ./pkg/proxy ./pkg/proxy/redis -run '^$' -bench . -benchmem -benchtime=300ms -count=5`，共 34 组 benchmark。代表性中位数如下；后续必须使用相同参数复跑。

| Benchmark | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| RequestGoChannel | 42.31 | 0 | 0 |
| RequestChan128 | 82.89 | 19 | 0 |
| RequestChan1024 | 77.02 | 11 | 0 |
| Conn16K | 3195 | 33224 | 12 |
| ConnOffheap16K | 1222 | 2033 | 10 |
| Decode16B | 55.78 | 90 | 2 |
| Decode1K | 210.20 | 1224 | 3 |
| Decode16K | 2038 | 18521 | 3 |
| Encode16B | 32.29 | 0 | 0 |
| Encode1K | 45.57 | 0 | 0 |
| Encode16K | 394.70 | 0 | 0 |

### 5.2 各阶段结果

- **M0**：原实现全量测试、vet、依赖图与 34 组 benchmark 已建立基线；CLI 与 proxy/topom/FE HTTP 契约测试先在旧实现上通过，再开始迁移。
- **M1/M2**：非 etcd 两处 context import 改标准库；InfluxDB 改独立 1.x client；StatsD 改内部 UDP writer；删除 math2、旧 CLI 参数助手及无引用函数；可等价排序改 slices/cmp；配置与 INFO 解析去重复正则/中间切片；手写 MaxInt 和轮询 time.After 已清理。
- **M3**：`log/slog` 自定义 handler 保留旧格式、级别、堆栈、rolling 和 Panic 退出语义，新增结构化入口；错误分类改 `errors.Is/As`，保留 TracedError 堆栈。修复了全局 StdLog 替换、无 file flag 时多余 caller 采集和 fork module path 的调用方识别。
- **M4**：五个入口使用标准库 `flag.FlagSet` + 类型化参数与显式互斥校验；完整保留 admin 子命令、PITR/RDB Analysis 参数和 coordinator auth 绑定。StatsD 保留负数 gauge、1440 字节切包和去尾换行协议。
- **M5**：proxy/topom/FE 的 martini/binding/gzip/render 全部移除；标准库路由、中间件、静态文件与反代落地。原契约测试未放宽，另补全路由表测试。审查后修复 gzip+panic 被误写为 200 的问题，现返回 500。
- **M6**：init 永久协程收编为显式 worker；Proxy 有根 context、cancel、WaitGroup 和幂等 Close；Topom 周期任务在 stores 关闭前退出；RPC idle cleaner 可停止；accept 不再使用 Temporary。审查后补了 Close panic 唤醒、metrics 注册失败 cleanup、Topom 二次 Close 等待及第二次 SIGTERM 的默认终止行为。
- **M7**：Request+WaitGroup 合并为一次分配；Seed16 去 unsafe 并保持同请求幂等、子请求连接打散；keepalive INFO 解析降分配；迁移/ACL 字面量共享。RequestChan、Resp 树和 opstats 架构按所有权审计保留。
- **M8**：919 处既有断言迁移到 `testing.TB`，不再从断言里 os.Exit；纯工具测试有选择地并行；新增 golangci-lint v2 配置和独立 `make lint`。producer/consumer 与批量 drain 基准保留 b.N 测量形状，其余基准使用 b.Loop，避免把测量对象改成 cond 唤醒延迟。

### 5.3 最终回归结果

| 编号 | 结果 | 证据 |
|---|---|---|
| R01 | 通过 | `gofmt -l cmd pkg` 无输出；`git diff --check` 通过 |
| R02/R03 | 通过 | `make gotest` 全部 cmd/pkg 包通过 |
| R04 | 通过 | `go build ./cmd/... ./pkg/...` 通过 |
| R05 | 通过 | race：proxy、topom、log、errors、httpx、rpc、FE 全部通过 |
| R06 | 通过 | `make lint`：go vet + 临时目录 golangci-lint v2.15.0，`0 issues`；未改项目工具依赖 |
| R07 | 通过 | 五个入口 help 通过；proxy/dashboard/ha/fe version 各两行（admin 本无 version）；proxy/dashboard 默认配置与 config/ 逐字节一致；非法 one-shot 组合退出 1 |
| R08 | 通过 | proxy/topom/FE 契约、全路由表、gzip、xauth、静态/反代测试通过；含 race |
| R09 | 通过 | slog 格式/级别/堆栈/Close、errors.Is/As/%w、RPC/sentinel 测试通过 |
| R10 | 通过 | 重复 Close、Close panic、metrics cleanup、后台 panic、周期任务退出与 accept 分类测试通过 |
| R11 | 通过（有波动说明） | 同参数完整复跑，40 组 benchmark（原 34 + 新 A/B 6）完成；见下表及限制 |
| R12 | 通过 | 构建图中 docopt/martini/bpool/statsd/旧 influx client 为零；influxdb1-client 存在；x/net/context 当时仅因 etcd 保留 |
| R13 | 通过 | 未运行 go mod tidy；仅有目标依赖替换/删除，无其他版本升级 |
| R14 | 通过 | `git diff -- config/` 为空；无 bin、临时测试或 smoke 产物进入工作树 |
| R15 | 通过（当时） | 当时 `git diff -- pkg/models/etcd` 为空，`coreos/etcd` 版本未变；该目录与依赖已在后续清理中移除 |
| R16 | 通过 | 三路独立 diff 审查完成；发现的 gzip、Close、cleanup、benchmark、assert goroutine 等问题均已修复并回归 |

最终基准与基线的代表性中位数（相同机器、Go 1.27.4、300ms、count=5）：

| Benchmark | 基线 ns/op | 最终 ns/op | 基线 B/op、allocs | 最终 B/op、allocs |
|---|---:|---:|---:|---:|
| RequestGoChannel | 42.31 | 42.89 | 0、0 | 1、0 |
| RequestChan128 | 82.89 | 81.65 | 19、0 | 19、0 |
| RequestChan1024 | 77.02 | 94.88 | 11、0 | 20、0 |
| Conn16K | 3195 | 2876 | 33224、12 | 33224、12 |
| ConnOffheap16K | 1222 | 1254 | 2033、10 | 2108、10 |
| Decode16B | 55.78 | 55.67 | 90、2 | 90、2 |
| Decode1K | 210.20 | 234.80 | 1224、3 | 1224、3 |
| Decode16K | 2038 | 2096 | 18521、3 | 18504、3 |
| Encode16B | 32.29 | 31.83 | 0、0 | 0、0 |
| Encode1K | 45.57 | 43.82 | 0、0 | 0、0 |
| Encode16K | 394.70 | 356.80 | 0、0 | 0、0 |

新 A/B 基准的同轮中位数：

| 项目 | 旧形状 | 新形状 | 结论 |
|---|---:|---:|---|
| Request+WaitGroup 创建 | 32.39ns，160B，2 allocs | 29.89ns，160B，1 alloc | 每次创建少 1 次分配，字节数不变 |
| keepalive INFO 解析 | 489.10ns，915B，14 allocs | 116.60ns，6B，2 allocs | 约 4.2× 时间改善、12 次更少分配 |
| Seed16 读取 | unsafe 指针+时间哈希 | 1.87ns，0 allocs | 无 unsafe、幂等 |
| Session nextSeed | — | 3.73ns，0 allocs | 每请求一次原子序列 |

decoder/offheap 大块项目的多轮结果波动明显：例如 Decode32K/128K 在完整轮次中分别为 4466/13794ns，另一完整轮次曾为 3172/9930ns；这些生产 decoder 文件未改，B/op 与 allocs/op 保持一致，因此只能认定**本机时延受运行状态影响较大**，不能据此宣称生产回退或改善。RequestChan 生产实现同样未改，最终各轮也覆盖原基线附近值。

### 5.4 保留项、偏离与限制

- **etcd 完整排除（当时）**：当时源码与 `coreos/etcd` 版本均未改，`golang.org/x/net` 因它继续保留，lint 也对 `pkg/models/etcd` 排除诊断。后续清理已删除该客户端、专属依赖和 lint 排除规则；本条只属于历史记录。
- **有意保留**：`topom_slots.go` 两处依赖可变外部状态的旧排序/搜索；RequestChan 的关闭后丢弃、非阻塞入队和关闭后排空；Resp 不池化（共享子树/包级单例无法证明唯一所有权）；WaitGroup 不按值内嵌（子请求必须共享 join）；opstats 不做 atomic snapshot（全局锁不在每请求路径）。
- **基准例外**：channel producer/consumer 与 RequestChan 批量 drain 使用现代 `for range b.N`，不强行改 b.Loop；这是保证测量形状和不死锁所必需的例外。
- **CLI 差异**：docopt 的长选项唯一前缀缩写不再支持；help 展示完整 usage；空字符串选项在解析阶段报错而非稍后 panic，失败退出码不变。`--` 恢复为 option terminator，不作为 positional。
- **HTTP 差异**：martini 未锚定正则意外接受的“路由前缀 + 额外后缀”现在按完整路径匹配并返回 404；非法 `.`/`..` 段由 ServeMux 清理规则拒绝。FE panic 仍返回 500 并记录堆栈，但不再向客户端返回开发态 HTML 堆栈页面。
- **错误 API**：兼容函数 `errors.Equal/NotEqual` 从字符串判等改为标准 `errors.Is` 语义；仓库内调用点已迁移，外部 fork 若仍直接调用需注意。
- **InfluxDB**：独立 1.x client 冻结于 2022-03-02，但覆盖本项目全部 API，换来移除 influxdb 大依赖树与 pkg-config；其写入无 context，Proxy.Close 最坏可能等待既有 5s client timeout。
- **日志与关闭**：保留旧 printf/Panic API 和退出码；新代码使用 SetStdLog/结构化入口。优雅 Close 会等待有确定退出条件的后台任务，第二次 SIGTERM 已恢复默认终止作为运维兜底。
- **环境限制**：以上结论来自 macOS arm64 本地测试与微基准。Linux 生产性能、跨平台 cgo/jemalloc、真实 Redis/coordinator 集群与发布验证仍须执行 `doc/redis8_validation_zh.md`。
