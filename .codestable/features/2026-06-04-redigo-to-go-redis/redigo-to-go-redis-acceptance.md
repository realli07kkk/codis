---
doc_type: feature-acceptance
feature: 2026-06-04-redigo-to-go-redis
status: completed
accepted_at: 2026-06-04
design: .codestable/features/2026-06-04-redigo-to-go-redis/redigo-to-go-redis-design.md
checklist: .codestable/features/2026-06-04-redigo-to-go-redis/redigo-to-go-redis-checklist.yaml
tags: [go, modules, dependency-upgrade, redis-client, go-redis, redigo]
---

# redigo-to-go-redis 验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-06-04
> 关联方案 doc：`.codestable/features/2026-06-04-redigo-to-go-redis/redigo-to-go-redis-design.md`

## 1. 接口契约核对

对照方案第 2.1 节名词层逐一核查：

**接口示例逐项核对**：

- [x] `module_set`：`go.mod` 已删除 `github.com/garyburd/redigo v1.6.4` direct require，并新增 `github.com/redis/go-redis/v9 v9.20.0` direct require；`go.sum` 已移除 redigo checksum，保留 go-redis checksum。
- [x] `client_handle`：`pkg/utils/redis.Client` 对外构造函数和方法签名保持；内部持有 go-redis base client + dedicated `Conn`，`Addr` / `Auth` / `AuthIdentity` / `Database` / `LastUse` / `Timeout` / `Pipeline` 字段保留。
- [x] `reply_shape`：`replyString` / `replyValues` / `replyInt` / `replyInts` / `replyStringMap` 已集中在 `pkg/utils/redis/reply.go`，替代 redigo conversion helper。
- [x] `sentinel_client`：`subscribeCommand` 已使用 go-redis `PubSub.Receive` / `ReceiveMessage`；Sentinel masters/slaves/monitor/remove/flushconfig 仍走 `Client` pipeline wrapper 和 string map helper。
- [x] `deprecated_redis_test`：`extern/deprecated/redis-test` 中 `utils.go`、`extra_incr.go`、`bench/benchmark.go` 已清除 redigo import，改用 go-redis 或本目录最小 helper。

**流程图核对**：

- [x] A 新增 reply helper：`pkg/utils/redis/reply.go`。
- [x] B/C 版本与触点复核：验收重跑 `go list -m -json @latest` 与 `go mod download -json v9.20.0`，`rg` 无 redigo Go 源码命中。
- [x] D/E client 与 reply conversion：`pkg/utils/redis/client.go` 迁移到 go-redis dedicated `Conn`，业务解析走 `reply.go`。
- [x] F Sentinel 适配：`pkg/utils/redis/sentinel.go` 与 `pkg/utils/redis/sentinel_test.go` 覆盖。
- [x] G deprecated extern 清理：`extern/deprecated/redis-test` 可在 `-vet=off` 下编译 smoke。
- [x] H module 切换：`go.mod` / `go.sum` 已收口。
- [x] I/J/K/L 测试和 grep：`go test ./pkg/utils/redis`、`go test ./pkg/topom`、`go test ./cmd/... ./pkg/...`、redigo grep 均通过。

## 2. 行为与决策核对

**需求摘要逐项验证**：

- [x] 所有 Go 源码 redigo import 清除：`rg "github\\.com/garyburd/redigo|github\\.com/gomodule/redigo" --glob '*.go' .` 无输出。
- [x] `go.mod` 替换为 go-redis：`go.mod` direct require 为 `github.com/redis/go-redis/v9 v9.20.0`。
- [x] `pkg/utils/redis`、`pkg/topom`、默认 cmd/pkg gate 通过：验收重跑三个 `go test` 命令均通过。
- [x] Redis/Sentinel 关键路径保持：AUTH、SELECT、INFO/CONFIG、MULTI/EXEC、migration、Sentinel pub/sub 和 batch 管理路径均有单测或 topom/default gate 证据。

**明确不做逐项核对**：

- [x] 未替换或重写 `pkg/proxy/redis` RESP codec；只改了测试 helper `pkg/proxy/redis/redistest/server.go` 的 `HELLO` fallback。
- [x] 未修改 Redis Server / Redis 8 Codis Server 源码、slot migration 命令协议、RDB export、Redis 8 adapter 行为。
- [x] 未改变 `pkg/utils/redis.Client` / `Pool` / `InfoCache` / `Sentinel` 对上层公开 API。
- [x] 未引入 go-redis cluster/ring/failover client。
- [x] 未开启 RESP3 默认路径；`Protocol: 2` 已落地。
- [x] 未启用 go-redis command retry；`MaxRetries: -1` 已落地。
- [x] 未运行全量 `go mod tidy`；module 变化来自定点 `go get` / `go mod edit` 和验收命令。
- [x] 未修复 `extern/deprecated/redis-test` 既有 vet 问题；只做 import 清除和 `-vet=off` 编译 smoke。
- [x] 未升级 Go toolchain、未改变 `go 1.26.1` module directive、未修改 `third_party/jemalloc-go`、Docker、部署脚本、前端资源或配置模板。

**关键决策落地**：

- [x] D1 使用 `github.com/redis/go-redis/v9 v9.20.0`：版本、checksum、GitHub tag ref 复核通过。
- [x] D2 保留 `pkg/utils/redis` 适配层：上层 `topom` / `proxy` / `cmd` 调用没有扩散到 go-redis API。
- [x] D3 dedicated `Conn`：`Client` 内部使用 go-redis standalone client + dedicated `Conn`，Codis `Pool` 仍管理复用。
- [x] D4 兼容选项：`Protocol: 2`、`DisableIdentity: true`、`MaxRetries: -1` 和 timeout 已在 `NewClientWithAuthIdentity` 落地。
- [x] D5 保留 `Send` / `Flush` / `Receive`：pipeline queue、`pipeRecv` 和 `Pipeline.Send/Recv` 计数已落地并有测试覆盖。
- [x] D6 deprecated extern 只清 import：未纳入默认 gate，普通 `go test` 仍被历史 vet 问题挡住，`-vet=off` 编译 smoke 通过。

**流程级约束核对**：

- [x] 错误语义：go-redis Redis command error 与 network/context 错误区分，非 Redis command error 会关闭当前 client。
- [x] 幂等性：`MaxRetries: -1` 避免 migration / ACL / Sentinel 管理命令自动重试。
- [x] 兼容性：AUTH、SELECT、MULTI/EXEC、SLOTSMGRT*、Sentinel majority 均有测试或 default gate 证据。
- [x] 可观测点：版本查询、download checksum、go.mod/go.sum diff、redigo grep、target tests、git status 均已记录。

**挂载点反向核对**：

- [x] `go.mod` direct require：删除 go-redis 或恢复 redigo 会直接回滚依赖替换。
- [x] `pkg/utils/redis` 底层 client handle：恢复 redigo import 会让默认 cmd/pkg Redis 管理路径回到旧库。
- [x] `pkg/utils/redis/reply.go`：删除后 INFO/CONFIG/SLOTSINFO/migration/Sentinel reply shape 校验缺失。
- [x] `pkg/utils/redis/sentinel.go`：删除 Sentinel 适配后 monitor/remove/masters/slaves/switch-master 监听无法证明 go-redis 下可用。
- [x] `extern/deprecated/redis-test` cleanup：删除后 all Go redigo usage grep 会失败。
- [x] 反向 grep：`rg "github.com/redis/go-redis/v9" go.mod pkg cmd extern --glob '*.go'` 只命中 `go.mod`、`pkg/utils/redis`、允许的 deprecated extern。
- [x] 拔除沙盘推演：逆向删除上述挂载点后分别会恢复 redigo 依赖、破坏 helper 行为、破坏 Sentinel 行为或让 all-Go grep 留尾巴；无额外漏记挂载点。

## 3. 验收场景核对

- [x] `go list -m -json github.com/redis/go-redis/v9@latest`
  - 证据来源：验收命令。
  - 结果：通过，`Version` 为 `v9.20.0`，`Time` 为 `2026-05-28T07:31:45Z`，`GoVersion` 为 `1.24`。

- [x] `go mod download -json github.com/redis/go-redis/v9@v9.20.0`
  - 证据来源：验收命令。
  - 结果：通过，checksum 为 `h1:WnQYxLkgO2xiXTCJY0ldIiI8dNqCDlQAG+AtaH7a2a0=`，GoModSum 为 `h1:v/M13XI1PVCDcm01VtPFOADfZtHf8YW3baQf57KlIkA=`，GitHub tag ref 为 `refs/tags/v9.20.0`。

- [x] `NewClientNoAuth(fake, timeout)`
  - 证据来源：`TestClientGoRedisHandshakeUsesResp2AndDisablesIdentity`。
  - 结果：通过，fake server 可观测 `HELLO 2` fallback，未出现 `CLIENT SETINFO`。

- [x] `NewClient(fake, "secret", timeout)`
  - 证据来源：`TestClientRedis8AuthDefaultUserPath`。
  - 结果：通过，fallback 后发送 `AUTH secret`。

- [x] `NewClientWithAuthIdentity(fake, svc/secret, timeout)`
  - 证据来源：`TestClientRedis8AuthNamedUserPath`。
  - 结果：通过，fallback 后发送 `AUTH svc secret`。

- [x] `Select(2)` 连续调用两次
  - 证据来源：`TestClientRedis8SelectTracksDatabase`。
  - 结果：通过，只发送一次业务 `SELECT`，`Database` 更新为 2。

- [x] `InfoFull()`
  - 证据来源：`TestClientRedis8InfoFullFields`、`TestClientRedis8InfoFullStandaloneDoesNotInventMasterAddr`。
  - 结果：通过，`master_addr` 和 `maxmemory` 解析保持。

- [x] `SetMaster()`
  - 证据来源：`TestClientRedis8SetMasterKeepsSlaveofAlias`、`TestClientRedis8SetMasterWritesMasterUserForNamedAuth`、`TestClientSetMasterIgnoresUnsupportedMasterUserClear`。
  - 结果：通过，保留 `SLAVEOF` alias、`masterauth`、named auth `masteruser`、`CONFIG REWRITE`、`CLIENT KILL TYPE normal` 和 `EXEC` 内部错误检查。

- [x] `SlotsInfo()` / `MigrateSlot()` / `MigrateSlotAsync()`
  - 证据来源：`TestClientRedis8SlotsInfoStrictShape`、`TestClientRedis8SlotsInfoRejectsMalformedShape`、`TestClientRedis8MigrationResponsesReturnRemainingCount`。
  - 结果：通过，严格解析既有返回 shape 并返回 remaining count。

- [x] Sentinel subscribe `+switch-master`
  - 证据来源：`TestSentinelSubscribeCommandWaitsForAckAndSwitchMaster`、`go test -race ./pkg/utils/redis -run Sentinel -count=1 -timeout=120s`。
  - 结果：通过，多数派订阅 ack 前不触发 callback，同 product event 触发返回；race smoke 通过。

- [x] Sentinel masters/slaves/monitor/remove/flushconfig
  - 证据来源：`TestSentinelMastersAndSlavesParseStringMaps`、`TestSentinelMonitorRemoveAndFlushConfigCommands`。
  - 结果：通过，string map、pipeline 顺序和 flushconfig 命令保持。

- [x] `go test ./pkg/utils/redis -count=1 -timeout=120s`
  - 结果：通过。

- [x] `go test ./pkg/topom -count=1 -timeout=120s`
  - 结果：通过。

- [x] `go test ./cmd/... ./pkg/... -count=1 -timeout=180s`
  - 结果：通过。

- [x] `go test -vet=off ./extern/deprecated/redis-test/... -run '^$' -count=1 -timeout=120s`
  - 结果：通过。普通 `go test ./extern/deprecated/redis-test/...` 仍会被该 deprecated 目录既有 vet 问题挡住，符合 design 非默认 gate 边界。

- [x] `rg "github.com/garyburd/redigo" --glob '*.go'`
  - 结果：无命中。

- [x] `rg "github.com/redis/go-redis/v9" go.mod pkg cmd extern --glob '*.go'`
  - 结果：命中 `go.mod`、`pkg/utils/redis/client.go`、`pkg/utils/redis/sentinel.go`、`extern/deprecated/redis-test/utils.go`、`extern/deprecated/redis-test/bench/benchmark.go`；未把 `pkg/proxy/redis` 当作替换对象。

- [x] `git diff --name-status`
  - 结果：不包含 `extern/redis-8.6.3`、Docker、部署脚本、前端资源、配置模板或无关 module 升级。

## 4. 术语一致性

- `Redis client library migration`：只作为 feature/design/acceptance 术语使用，代码不新增冲突概念。
- `Codis Redis helper API`：代码仍集中在 `pkg/utils/redis`，上层 API 保持。
- `Target go-redis version`：`go.mod`、验收命令和报告均为 `github.com/redis/go-redis/v9 v9.20.0`。
- `All Go redigo usage`：Go 源码、`go.mod/go.sum`、module graph 对 redigo 均无命中。
- `Reply conversion helpers`：代码落点为 `pkg/utils/redis/reply.go`，命名与 design 一致。
- 防冲突：`pkg/proxy/redis` 仍是 RESP codec 包，没有被本 feature 替换或重命名；`redigo` 仅保留在 CodeStable 历史/本 feature 文档描述中。

## 5. 架构归并

- [x] `.codestable/architecture/ARCHITECTURE.md`：已新增 `Go Redis helper` 术语，记录 `pkg/utils/redis` 对上层保留 API、底层使用 go-redis/v9 standalone client + dedicated `Conn`，并明确它不是 `pkg/proxy/redis` RESP codec。
- [x] `.codestable/architecture/ARCHITECTURE.md`：已更新 Go Redis Server 协议边界段，写入 `Protocol: 2`、`DisableIdentity: true`、`MaxRetries: -1`、dedicated connection、Codis pool 复用语义、Sentinel PubSub/batch 适配和不启用 RESP3/CLIENT SETINFO/cluster client 的约束。
- [x] `.codestable/architecture/ARCHITECTURE.md`：已更新代码锚点，新增 `pkg/utils/redis/reply.go` 与 `pkg/utils/redis/sentinel.go`，并更新 `client.go` 描述。
- [x] `.codestable/architecture/ARCHITECTURE.md`：已在已知约束中记录 go-redis/v9 module path、默认兼容选项和 fake Redis `HELLO` fallback 测试纪律。

## 6. requirement 回写

- [x] 方案 frontmatter `requirement: null`，且本 feature 是运行期依赖迁移，不新增用户可见能力。
- [x] 结论：无 requirement 回写；不触发 `cs-req backfill/update`。

## 7. roadmap 回写

- [x] 方案 frontmatter `roadmap: null` / `roadmap_item: null`。
- [x] 结论：非 roadmap 起头，无 roadmap items 或主文档需要回写。

## 8. attention.md 候选盘点

- [x] 本 feature 未暴露需要补入 `.codestable/attention.md` 的通用环境/工具陷阱。
- 说明：`HELLO` fallback 规则已经归并进 architecture 的 Go Redis helper 已知约束；它不是所有 feature 都会遇到的命令/环境前置。

## 9. 遗留

- 后续优化点：
  - 可选：测试 helper 里 `HELLO` fallback 处理有轻微重复，可后续小重构清理；当前不影响生产行为。
  - 可选：真实 Redis 3/6/8 与 Sentinel smoke 可补充运行环境证据；当前 fake server、topom/default gate 与 Sentinel race smoke 已覆盖代码级契约。
- 已知限制：
  - `extern/deprecated/redis-test` 普通 `go test` 仍被历史 vet 问题阻塞，本 feature 仅要求 `-vet=off` 编译 smoke。
  - `pkg/utils/redis.Client` 状态面比 redigo 时代更大，后续改 pipeline 需守住 `Pipeline.Send/Recv` 和 dedicated connection 不变量。
- 实现阶段顺手发现：
  - 无需要立即进入 issue 流程的顺手发现；完整拆分 `pkg/utils/redis/client.go` / `sentinel.go` 职责应另走 `cs-refactor`，不属于本 feature。
