---
doc_type: feature-acceptance
feature: 2026-05-14-redis8-go-component-adapters
status: ready-for-review
summary: Redis 8 Go component adapters acceptance report
tags: [redis, redis8, go, compatibility, acceptance]
---

# redis8-go-component-adapters 验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-05-14
> 关联方案 doc：`.codestable/features/2026-05-14-redis8-go-component-adapters/redis8-go-component-adapters-design.md`

## 1. 接口契约核对

对照方案第 2.1 节名词层逐一核查：

**接口示例逐项核对**：

- [x] `InfoFull()` 示例（`pkg/utils/redis/client.go:179`）：Redis 8 `INFO` 返回 `master_host`、`master_port`、`master_link_status` → `InfoFull()["master_addr"] == "127.0.0.1:6380"`。代码实际行为一致；`pkg/utils/redis/client_test.go` 覆盖 replica 字段和 standalone 不伪造 `master_addr`。
- [x] `MigrateSlotAsync()` 示例（`pkg/utils/redis/client.go:251`）：`SLOTSMGRTTAGSLOT-ASYNC ...` 返回 `[migrated_count, remaining_count]` → 返回 `remaining_count`。代码实际行为一致；`pkg/utils/redis/client_test.go` 和 `pkg/topom/topom_action_test.go` 覆盖。
- [x] `SetMaster()` 示例（`pkg/utils/redis/client.go:205`）：Redis 8 接受 `SLAVEOF host port` → Go 继续发送 `SLAVEOF`，没有改成 `REPLICAOF-only`。代码实际行为一致；fake test 和真实 smoke 均验证。
- [x] proxy wrapper 示例（`pkg/proxy/forward.go:169`）：`SLOTSMGRT-EXEC-WRAPPER` 返回 `[2, reply]` → proxy 返回 wrapped reply。代码实际行为一致；`pkg/proxy/redis8_adapters_test.go` 覆盖 `[0,err]`、`[1,err]`、`[2,reply]`。

**名词层“现状 → 变化”逐项核对**：

- [x] Go Redis client 兼容面：实现新增测试矩阵和 smoke 证据，没有改 public method 签名；`SlotsInfo()` / `MigrateSlot()` / `MigrateSlotAsync()` 仍严格解析数组结构。
- [x] Topom/admin/HA 状态消费面：`RefreshRedisStats()` 仍通过 `InfoFull()` 供 admin/HA 消费 `master_addr` / `master_link_status`；新增 topom fake server handler 覆盖 Redis 8 迁移返回。
- [x] Proxy Redis 8 后端协议面：新增 AUTH/SELECT/INFO keepalive、`SLOTSINFO` / `SLOTSSCAN` dispatch、exec wrapper 返回码测试；`mapper.go` 命令边界未放宽。
- [x] Redis 8 restore async AUTH 能力：Go 侧不新增 username 配置；真实 smoke 覆盖 default-user `AUTH <password>` 下同步/异步迁移。

**流程图核对**：

- [x] `梳理 Go Redis 兼容矩阵` → `.codestable/.../redis8-go-component-adapters-compatibility-matrix.md` 已落地。
- [x] `补 fake Redis 单元测试` → `pkg/proxy/redis/redistest/server.go`、`pkg/utils/redis/client_test.go`、`pkg/proxy/redis8_adapters_test.go`、`pkg/topom/topom_action_test.go` 已落地。
- [x] `测试证明现有 Go 代码兼容? 是 → 保持生产代码不变` → `git diff --name-only` 显示本 feature 未修改生产 Go 文件。
- [x] `启动 Redis 8 Codis Server smoke` → `.codestable/.../redis8-go-component-adapters-smoke.md` 记录真实 Redis 8 smoke 结果。

## 2. 行为与决策核对

对照方案第 1 节 + 第 2.2 节：

**需求摘要逐项验证**：

- [x] `Info()` / `InfoKeySpace()` / `InfoFull()` 兼容 Redis 8 INFO/CONFIG：fake test 覆盖 master 字段、standalone 字段和 `CONFIG GET maxmemory`，真实 smoke 覆盖 INFO replication。
- [x] `SetMaster()` 流程在 Redis 8 可用：fake test 验证 `MULTI` / `CONFIG` / `SLAVEOF` / `CLIENT KILL TYPE normal` 命令序列，真实 smoke 验证 `SLAVEOF` alias、`CLIENT KILL TYPE normal`、`CONFIG REWRITE`、`SLAVEOF NO ONE`。
- [x] slot 查询和迁移返回格式严格解析：fake test 覆盖 `SLOTSINFO` 正常/畸形返回和 sync/async migration 返回，真实 smoke 覆盖 `SLOTSINFO`、`SLOTSMGRTTAGSLOT`、`SLOTSMGRTTAGSLOT-ASYNC`。
- [x] proxy 后端 AUTH/SELECT/keepalive 和 slot dispatch 兼容：`pkg/proxy/redis8_adapters_test.go` 覆盖 AUTH/SELECT、loading/stale keepalive、`SLOTSINFO <addr>` 和 `SLOTSSCAN` dispatch。
- [x] admin/HA 消费 Redis 8 INFO 字段不误判：生产路径仍由 `InfoFull()` 提供 `master_addr` / `master_link_status`，fake test 覆盖字段解析。

**明确不做逐项核对**：

- [x] 默认 `make` / `make codis-server` 没切到 Redis 8：`Makefile` 未修改，默认 Redis 3 构建语义保持。
- [x] `go.mod` / `go.sum` 未修改，未做依赖升级或 `go mod tidy`。
- [x] `pkg/proxy/mapper.go` 未修改；`CONFIG`、`SLAVEOF`、`SLOTSMGRT*`、`SLOTSRESTORE-ASYNC*`、`CLIENT KILL` 等危险命令未被放开。
- [x] 未新增 Redis Cluster `MOVED` / `ASK` / `CLUSTER` 协议分支。
- [x] 未新增 ACL username 配置项；只验证现有 default-user `AUTH <password>` 路径。
- [x] Redis 8 command metadata / packaging 修正未混入本 feature。

**关键决策落地**：

- [x] D1 以真实 RESP 为 Go 适配源：新增 fake RESP tests 和真实 Redis 8 smoke，没有依赖 command JSON metadata 决策。
- [x] D2 优先保留 `SLAVEOF`：`SetMaster()` 生产代码未变，测试确认不发送 `REPLICAOF`。
- [x] D3 保持 parser 严格：`SlotsInfo()` 畸形返回测试要求失败，迁移返回仍必须是两个整数。
- [x] D4 兼容验证先落测试：实现没有生产代码 churn。
- [x] D5 AUTH 只覆盖 default user：无 username 配置变更。
- [x] D6 proxy 命令边界不放宽：mapper 未改，危险命令仍为 `FlagNotAllow`。

**编排层“现状 → 变化”逐项核对**：

- [x] 兼容矩阵已从 roadmap 4.7 落到 feature artifact。
- [x] topom fake server 改为 handler 模式，避免把 Redis 8 场景状态塞进通用 struct field。
- [x] proxy 和 utils/redis 共享 fake Redis server helper，避免三套 RESP fake server 独立漂移。
- [x] 真实 Redis 8 smoke 已证明 Go 组件实际可驱动 Redis 8 Codis Server。

**流程级约束核对**：

- [x] 错误语义：RESP 类型不匹配、畸形数组、Redis error reply 继续返回 error；新增测试覆盖畸形 `SLOTSINFO`，既有生产代码对 AUTH/SELECT/CONFIG/EXEC error 显式返回。
- [x] 兼容性：`go test ./pkg/proxy/redis/redistest ./pkg/utils/redis ./pkg/proxy ./pkg/topom` 和 `make gotest` 通过。
- [x] 安全性：测试 fixture 使用 `"secret"`，没有新增真实凭据或日志泄露。
- [x] 当前 DB：真实 smoke 覆盖 DB2 下 `SLOTSINFO`、sync migration、async migration。
- [x] 可观测性：smoke 文档区分 Go parser/编排、Redis server 协议和 packaging handoff。
- [x] 命令边界：`mapper.go` 未变，Redis 8 server 支持不等于 proxy 对业务开放。

**挂载点反向核对（可卸载性）**：

- [x] 挂载点 M1：方案声明“不引入新的运行时挂入点”。代码实际落点全部为测试文件、共享测试 helper 和 CodeStable 文档，生产运行时无新增配置 key、HTTP endpoint、Redis 命令、定时任务、coordinator schema 或 proxy allow-list 条目。
- [x] 反向核查：`git diff --name-only` + `git ls-files --others --exclude-standard` 显示代码改动为 `pkg/topom/*_test.go`、`pkg/proxy/redis8_adapters_test.go`、`pkg/utils/redis/client_test.go`、`pkg/proxy/redis/redistest/server.go`，无生产 Go 文件。
- [x] 拔除沙盘推演：删除新增测试、`redistest` helper 和 feature 文档后，运行时系统不会留下新开关或新 API；仅失去 Redis 8 Go 组件兼容回归证据。

## 3. 验收场景核对

对照方案第 3 节关键场景清单，逐条可观察证据验证：

- [x] **S1**：Redis 8 standalone `INFO` + `CONFIG GET maxmemory` → `InfoFull()` 包含 `maxmemory`，不伪造 `master_addr`。
  - 证据来源：`pkg/utils/redis/client_test.go`
  - 结果：通过
- [x] **S2**：Redis 8 replica `INFO` 字段 → `InfoFull()` 合成 `master_addr`，admin/HA 可消费。
  - 证据来源：`pkg/utils/redis/client_test.go` + smoke INFO replica 字段
  - 结果：通过
- [x] **S3**：`loading:1` 或 `master_link_status:down` → proxy backend keepalive 不把 stale backend 标成 connected。
  - 证据来源：`pkg/proxy/redis8_adapters_test.go`
  - 结果：通过
- [x] **S4**：Redis 8 default-user requirepass → `NewClient()` / proxy backend `AUTH <password>` 成功，错误路径不泄露密码。
  - 证据来源：`pkg/utils/redis/client_test.go`、`pkg/proxy/redis8_adapters_test.go`、smoke AUTH/PING
  - 结果：通过
- [x] **S5**：`SELECT <db>` 后 `SLOTSINFO`、sync migration、async migration 只作用当前 DB。
  - 证据来源：`pkg/utils/redis/client_test.go`、`pkg/proxy/redis8_adapters_test.go`、smoke DB2 覆盖
  - 结果：通过
- [x] **S6**：`SLOTSINFO` 返回 `[[slot_id,key_count], ...]` → `SlotsInfo()` 解析；畸形返回报错。
  - 证据来源：`pkg/utils/redis/client_test.go`
  - 结果：通过
- [x] **S7**：`SLOTSMGRTTAGSLOT` 返回 `[migrated_count, remaining_count]` → `MigrateSlot()` 返回 `remaining_count`。
  - 证据来源：`pkg/utils/redis/client_test.go` + smoke sync migration
  - 结果：通过
- [x] **S8**：`SLOTSMGRTTAGSLOT-ASYNC` 返回 `[migrated_count, remaining_count]` → `MigrateSlotAsync()` 返回 `remaining_count`，restore async ACK error 不被吞。
  - 证据来源：`pkg/utils/redis/client_test.go`、`pkg/topom/topom_action_test.go`、smoke async migration
  - 结果：通过
- [x] **S9**：`SLOTSMGRT-EXEC-WRAPPER` `[0,err]`、`[1,err]`、`[2,reply]` 三种返回码按 proxy 现有语义处理。
  - 证据来源：`pkg/proxy/redis8_adapters_test.go`
  - 结果：通过
- [x] **S10**：`SLOTSINFO <addr>` 后端收到无 addr 参数的 `SLOTSINFO`；`SLOTSSCAN slot cursor COUNT n` 按 slot 后端转发。
  - 证据来源：`pkg/proxy/redis8_adapters_test.go`
  - 结果：通过
- [x] **S11**：`Client.SetMaster("host:port")` 在 Redis 8 上完成复制控制和连接清理；`SLAVEOF NO ONE` promote 语义保持。
  - 证据来源：`pkg/utils/redis/client_test.go` + smoke replication control
  - 结果：通过
- [x] **S12**：Redis 8 `CONFIG REWRITE` 后 `codis-enabled yes` 不丢。
  - 证据来源：smoke CONFIG REWRITE
  - 结果：通过

前端改动：无前端改动，不需要浏览器验证。

## 4. 术语一致性

对照方案第 0 节 + 第 2.1 节命名 grep 代码：

- Go component adapters：产物目录和文档使用 `redis8-go-component-adapters`，代码侧不新增运行时命名。
- 兼容矩阵：`redis8-go-component-adapters-compatibility-matrix.md` 已落地。
- Redis 8 Codis Server：smoke 文档使用 `bin/codis-server-redis8` / `codis-enabled yes`。
- 复制控制命令：代码和测试保持 `SLAVEOF`，未引入 `REPLICAOF-only`。
- default-user AUTH：测试覆盖 `AUTH <password>`，未新增 ACL username 配置。
- 防冲突：`rg` 核查未发现生产代码新增 `MOVED` / `ASK` / `CLUSTER` 分支、ACL username 配置或 proxy allow-list 放宽。

## 5. 架构归并

对照方案第 4 节，实际更新 `.codestable/architecture/ARCHITECTURE.md`：

- [x] 架构 doc：`.codestable/architecture/ARCHITECTURE.md`
  - 归并内容：Redis 8 支线状态从“Go 组件适配等待后续条目”推进为“Go proxy/topom/admin 已通过兼容验证”。
  - 已写入：第 2 节结构与交互增加 Go 侧 Redis 8 兼容边界，包括 `SLAVEOF` alias、INFO 字段、default-user AUTH、SELECT 当前 DB、`SLOTSINFO`、迁移返回和 exec wrapper。
  - 已写入：第 3 节数据与状态补充 Go Redis client / proxy backend / topom slot action 的稳定协议边界。
  - 已写入：第 5 节代码锚点补充 `pkg/utils/redis/client.go`、proxy backend/session/forward/mapper 和共享测试 helper。
  - 已写入：第 6 节已知约束更新为“Go 组件兼容已验证，正式默认产物切换仍等待 packaging/cutover”。
- [x] `.codestable/attention.md` 评估：无需补新规约；既有条目已经记录 `make gotest`、Redis 8 源码路径、默认构建边界和 `python3` 工具要求。

## 6. requirement 回写

对照方案 frontmatter 的 `requirement: redis-cluster-service`：

- [x] `.codestable/requirements/redis-cluster-service.md` 为 `status: current`，本次改了 Redis 8 支线能力进展与边界，已 update。
- [x] 实际写入：`## 实现进展` 追加 2026-05-14 Go proxy/topom/admin 兼容验证完成记录。
- [x] 实际写入：`## 边界` 更新为同步/异步迁移能力和 Go 组件兼容验证已完成，正式打包切换和灰度 cutover 仍未完成。

## 7. roadmap 回写

对照方案 frontmatter 的 `roadmap: redis8-upgrade` / `roadmap_item: redis8-go-component-adapters`：

- [x] `.codestable/roadmap/redis8-upgrade/redis8-upgrade-items.yaml`：对应条目从 `in-progress` 改为 `done`，feature 保持 `2026-05-14-redis8-go-component-adapters`。
- [x] `.codestable/roadmap/redis8-upgrade/redis8-upgrade-roadmap.md`：第 5 节子 feature 清单对应条目从 `planned / 未启动` 更新为 `done / 2026-05-14-redis8-go-component-adapters`，备注同步兼容验证结论。
- [x] YAML 校验：使用 `python3 .codestable/tools/validate-yaml.py` 校验通过。

## 8. attention.md 候选盘点

- [x] 无候选：本 feature 未暴露需要补入 `attention.md` 的新内容。既有注意事项已经覆盖 `make gotest`、Redis 8 源码目录、默认构建边界和 `python3` 工具使用。

## 9. 遗留

- 后续优化点：
  - `waitUntil` 仍是短轮询测试 helper，可在后续测试清理中考虑 channel 通知，但当前不影响稳定性。
  - `pkg/topom/topom_stats_test.go` 的 fake server 已支持 handler，但仍保留 topom 包内局部实现；如未来更多包需要 topom 风格默认行为，可再评估是否迁入共享 helper。
- 已知限制：
  - 本 feature 不切换默认 `make` / `make codis-server` 到 Redis 8；正式构建、配置模板、命令 metadata 和发布包装仍属 `redis8-build-config-packaging`。
  - 本 feature 不承诺 Redis 3 ↔ Redis 8 RDB fragment 双向迁移兼容，也不做系统级灰度 / 性能基线。
  - 本 feature 不新增非 default ACL 用户配置。
- 实现阶段顺手发现：
  - 初版测试存在 fake Redis server copy-paste，已通过 `pkg/proxy/redis/redistest` 收口。
  - topom fake server 初版用 struct field 控制迁移/keyspace 场景，已改为 handler 回调模式。
