---
doc_type: feature-acceptance
feature: 2026-05-17-redis8-validation-cutover
status: current
accepted_at: 2026-05-17
summary: Redis 8 默认发布物已完成本地 Mac 非性能 e2e、slot migration、跨版本 fragment 观察和 Linux 正式验证交接。
tags: [redis, redis8, validation, cutover]
---

# redis8-validation-cutover 验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-05-17
> 关联方案 doc：`.codestable/features/2026-05-17-redis8-validation-cutover/redis8-validation-cutover-design.md`

## 1. 接口契约核对

**接口示例逐项核对**：

- [x] Redis 8 local validation gate：输入执行本地 gate，输出本地 Mac 证据报告。
  - 实际行为：一致。`scripts/redis8_local_validation.py` 输出 `redis8-local-validation-evidence.json`；证据包含 gate definitions、版本、e2e、cross-version 和清理结果。
- [x] 端到端 Codis 集群演练：临时集群中把 slot 从 group 1 迁到 group 2。
  - 实际行为：一致。evidence 中 `e2e-semi-async` 和 `e2e-sync` 均 `passed`，迁移后 proxy 读回一致，目标 group 持有 key。
- [x] 跨版本迁移矩阵：Redis 8 source 对 Redis 3 target 执行 `SLOTSMGRTTAGONE`。
  - 实际行为：一致。Redis 8 → Redis 3 返回 `ERR error on slotsrestore, migration failed`，分类为 `observable_failure`，源端 key 保留；Redis 3 → Redis 8 成功。
- [x] Linux 正式验证交接清单：完成本地 gate 后输出 Linux 必跑项。
  - 实际行为：一致。`redis8-linux-validation-handoff.md` 列出 Linux 功能 gate、性能 gate、Docker/部署包装 gate、环境字段和阻塞条件。
- [x] 灰度与回滚草案：canary 异常时按数据来源决定回滚路径。
  - 实际行为：一致。`redis8-cutover-runbook-draft.md` 覆盖 preflight、canary、ramp-up、full cutover、rollback，并明确 Redis 8 RDB/AOF 不保证降级。

**名词层“现状 → 变化”逐项核对**：

- [x] Local validation gate 从分散验证变成明确 gate 矩阵。
  - 证据：`redis8-local-validation-matrix.md` 的 gate 表和 runner evidence 的 `gate_definitions`。
- [x] 端到端演练从无限 demo 脚本变成短生命周期 runner。
  - 证据：runner 使用临时 filesystem root、临时端口和进程清理；最终 evidence `temporary_root_removed: true`。
- [x] 跨版本迁移从未决项变成方向性矩阵。
  - 证据：Redis 3 → Redis 8 success；Redis 8 → Redis 3 observable failure 且 source preserved。
- [x] Linux handoff 和 runbook 草案已成为后续 Linux 阶段输入。
  - 证据：handoff/runbook 两份文档已落入 feature 目录。

**流程图核对**：

- [x] 构建/版本节点：`bin/codis-server --version` 显示 Redis 8.6.3；未跑 `make build-all`，避免刷新已有脏 tracked config。
- [x] Redis Tcl 节点：`./runtest --single unit/codis --single unit/codis_migration --single unit/codis_slotsrestore --single unit/codis_async_migration` 通过。
- [x] Go 回归节点：`make gotest` 通过。
- [x] e2e、slot migration、cross-version、handoff、runbook 节点：均有文件或 runner 证据落点。

## 2. 行为与决策核对

**需求摘要逐项验证**：

- [x] 核心 Tcl、Go、默认构建验证形成明确命令。
  - 结果：矩阵已列出 `make build-all`、`make gotest`、Redis Tcl suite；实际验收补跑了 `make gotest`、Redis Tcl suite 和 Redis 8.6.3 版本检查。
- [x] 本地端到端 Codis 集群演练覆盖 proxy、topom/dashboard、admin、Redis 8 server、slot assign、slot migration、proxy 写读和状态查询。
  - 结果：evidence 中 `e2e-semi-async` / `e2e-sync` 均通过。
- [x] Redis 3 fallback 与 Redis 8 跨版本 fragment 迁移方向性有记录。
  - 结果：Redis 3 → Redis 8 success；Redis 8 → Redis 3 observable failure，source key preserved。
- [x] Linux 正式验证交接清单已产出。
  - 结果：`redis8-linux-validation-handoff.md` 包含 Linux e2e、benchmark、fork/RDB、replication、Docker/部署包装和证据字段。
- [x] 灰度与回滚草案已产出。
  - 结果：runbook 覆盖 preflight/canary/ramp-up/full cutover/rollback，并明确不能把 Redis 8 RDB/AOF 当作 Redis 3 降级回滚。

**明确不做逐项核对**：

- [x] 未新增 Redis 命令、未改变 slot hash / tag index / 迁移协议 / Redis Cluster `MOVED` / `ASK` / cluster bus。
  - grep / diff：本轮未修改 `extern/redis-8.6.3/`。
- [x] 未修改 Go proxy/topom/admin 生产协议适配，未扩大 proxy `mapper.go` allow-list。
  - grep / diff：本轮未修改 `pkg/`、`cmd/`。
- [x] 未删除 Redis 3 fallback，未删除 `extern/redis-3.2.11/`。
  - diff：未触碰 `extern/` 或 Makefile fallback 目标。
- [x] 未修改 `go.mod` / `go.sum`，未新增 vendor/Godeps。
  - diff：无 `go.mod`、`go.sum`、`vendor/`、`Godeps/` 改动。
- [x] 未现代化 Docker/Kubernetes，也未声称 Docker 已运行。
  - handoff 中明确 Docker daemon 不可用时只能 blocked。
- [x] 本地 Mac 没有执行性能验证，也没有产出 throughput/latency 或生产 SLA。
  - runner scope 明确为 local Mac non-performance validation。
- [x] 未写真实生产地址、密码或线上 cutover 结果。
  - evidence 只包含临时 localhost 端口和临时目录。
- [x] 未保证 Redis 8 持久化 RDB/AOF 可被 Redis 3 读取。
  - runbook 和 architecture 均写明该边界。

**关键决策落地**：

- [x] 产出验证 gate 和 runbook，不引入新协议。
  - 落点：`scripts/redis8_local_validation.py` 和三份 feature 文档。
- [x] 端到端演练使用 filesystem coordinator 的短生命周期本地集群。
  - 落点：runner 写入临时 dashboard config，coordinator 为 filesystem root。
- [x] 性能基线后移到远程 Linux。
  - 落点：handoff 的 Linux 性能 gate；本地 evidence 无 benchmark 数据。
- [x] 回滚边界按数据来源和验证方向书写。
  - 落点：runbook rollback 节。
- [x] 跨版本 fragment 兼容只作为灰度前置观察项。
  - 落点：evidence direction_result 与 architecture/requirement 边界更新。

**编排层和流程级约束核对**：

- [x] 错误语义：runner 任一失败返回非 0；初次运行发现的问题已修正后最终 evidence 为 `passed`。
- [x] 幂等性：最终完整运行未使用 `--keep-temp`，`temporary_root_removed: true`，各子 workdir `workdir_removed: true`。
- [x] 顺序：补跑 Go/Tcl/版本，再使用最终 runner evidence 做 e2e/cross-version/handoff/runbook 收口。
- [x] 安全性：未写真实生产凭据。
- [x] 可观测点：evidence 包含命令、返回、关键 stdout、版本和状态。

**挂载点反向核对**：

- [x] 挂载点均在清单内：runner、矩阵、handoff、runbook、evidence、既有 Go/Tcl test 入口。
- [x] grep 反向核查：`redis8-validation-cutover` 命中 feature 目录和 roadmap/architecture/requirement 回写；`redis8_local_validation` 仅作为脚本和文档引用。
- [x] 拔除沙盘：删除 `scripts/redis8_local_validation.py` 和 feature 目录会移除本地 validation-cutover 证据能力，不影响生产 proxy/topom/Redis 协议。

## 3. 验收场景核对

- [x] 默认构建 gate：`bin/codis-server --version` 显示 Redis 8.6.3；`make build-all` 未在 acceptance 中重跑，原因是会刷新 tracked config，当前 `config/dashboard.toml` 已有用户侧脏改。Linux handoff 仍要求正式执行 `make build-all`。
- [x] Redis 8 Tcl Codis suite：4 个 suite 全部通过，输出 `All tests passed without errors!`。
- [x] Go 回归：`make gotest` 通过，覆盖 `cmd/...` 和 `pkg/...`。
- [x] 启动端到端 Redis 8 Codis 集群：`e2e-semi-async` / `e2e-sync` 均启动 dashboard/proxy/Redis 8 group 并完成 group/slot/proxy 配置。
- [x] proxy 写入普通 key、hash tag key、非 0 DB key：迁移前后读回一致。
- [x] `semi-async` migration：slot 186/314 迁到 group 2，source_exists=0，target_exists=1。
- [x] `sync` migration：slot 269/1021 迁到 group 2，source_exists=0，target_exists=1。
- [x] dashboard/admin status：final_slots 显示目标 slot owner 为 group 2。
- [x] Redis 3 → Redis 8 fragment：string/hash/list/zset 全部 success。
- [x] Redis 8 → Redis 3 fragment：string/hash/list/zset 均 observable failure，source_exists=1，target_exists=0。
- [x] Linux handoff：包含 direct/proxy/fallback benchmark、正式 e2e、Docker/部署包装、fork/RDB/复制和证据字段。
- [x] cutover runbook：覆盖 preflight、canary、ramp-up、full cutover、rollback 和 RDB/AOF 降级边界。
- [x] runner 清理：最终 evidence `temporary_root_removed: true`，各场景 `workdir_removed: true`，cleanup 状态均为 `stopped`。

## 4. 术语一致性

- [x] `Redis 8 local validation gate`：落在 matrix、runner gate_definitions、architecture 回写中，语义一致。
- [x] `端到端 Codis 集群演练`：落在 runner e2e 场景中，语义一致。
- [x] `跨版本迁移矩阵`：落在 runner cross_version 场景和 evidence 中，语义一致。
- [x] `Linux 正式验证交接清单`：落在 handoff 和 roadmap 新条目中，语义一致。
- [x] `灰度与回滚草案`：落在 runbook 中，语义一致。
- [x] 防冲突：未引入新的生产类型、命令名或协议名。

## 5. 架构归并

- [x] `.codestable/architecture/ARCHITECTURE.md`：已写入 Redis 8 local validation gate 的脚本入口、非生产定位、evidence 路径和 Linux 正式验证仍待后续阶段。
- [x] `.codestable/architecture/ARCHITECTURE.md`：已在已知约束中写入跨版本 fragment 结论：Redis 3 → Redis 8 成功，Redis 8 → Redis 3 可观测失败且源端 key 保留。
- [x] `.codestable/architecture/ARCHITECTURE.md`：已写明该结论不等价于 Redis 8 RDB/AOF 可降级。

## 6. requirement 回写

- [x] `.codestable/requirements/redis-cluster-service.md` 已回写实现进展。
  - 结论：requirement 仍为 `current`；新增 2026-05-17 进展，说明本地 Mac 非性能 validation-cutover 已完成，Linux 正式性能、fork/RDB、复制、Docker/部署包装和最终 cutover gate 仍待后续。

## 7. roadmap 回写

- [x] `.codestable/roadmap/redis8-upgrade/redis8-upgrade-items.yaml`：`redis8-validation-cutover` 已从 `in-progress` 改为 `done`。
- [x] `.codestable/roadmap/redis8-upgrade/redis8-upgrade-roadmap.md`：第 9 条状态同步为 `done`，备注更新为本地验收已完成。
- [x] `redis8-linux-validation-cutover` 保持 `planned`，继续承接 Linux 正式 gate。

## 8. attention.md 候选盘点

- [x] 有候选，暂不直接写入，等待用户确认后走 `cs-note`：
  - Redis 3 fallback 的 `codis-server-redis3` 不接受 `codis-enabled yes` 配置；需要跨版本直连验证时不要把 Redis 8 config 原样用于 Redis 3。

## 9. 遗留

- 后续优化点：建立长期 integration test harness 可另走 refactor/roadmap；本 feature 只保留本地 validation runner。
- 已知限制：Linux 正式性能、fork/RDB、复制、Docker/部署包装和最终 production cutover gate 未完成，归 `redis8-linux-validation-cutover`。
- 顺手发现：`codis-admin` 在当前树没有 `--version` 命令，runner evidence 以说明项记录；这不影响本 feature。
