---
doc_type: feature-note
feature: 2026-05-17-redis8-validation-cutover
status: draft
summary: Redis 8 本地 Mac 非性能验证 gate 矩阵
---

# Redis 8 local validation matrix

判断：本 feature 的本地验证只回答“默认 Redis 8 发布物能否在本地短生命周期 Codis 集群里正确协作”，不回答正式性能和上线 SLA。

## Gate 矩阵

| Gate | 触发命令 | 预期结果 | 失败归属 | 证据位置 |
| --- | --- | --- | --- | --- |
| default-build | `make build-all` | `bin/codis-server` 是 Redis 8，Go 组件和默认配置可重建 | packaging/build | 本地 shell log；runner metadata 记录二进制版本 |
| go-regression | `make gotest` | `go test ./cmd/... ./pkg/...` 通过 | Go component | 本地 shell log |
| redis-tcl-codis-suite | `cd extern/redis-8.6.3 && ./runtest --single unit/codis --single unit/codis_migration --single unit/codis_slotsrestore --single unit/codis_async_migration` | Redis 8 Codis Tcl 回归通过 | Redis 8 Codis server | Redis runtest log |
| e2e-local-codis-cluster | `python3 scripts/redis8_local_validation.py --only e2e --output .codestable/features/2026-05-17-redis8-validation-cutover/redis8-local-validation-evidence.json` | `semi-async` 和 `sync` 临时集群均完成 proxy 写读、slot assign、slot migration、迁移后读回 | integration/topom/proxy/server | evidence JSON |
| cross-version-fragment-matrix | `python3 scripts/redis8_local_validation.py --only cross-version --output .codestable/features/2026-05-17-redis8-validation-cutover/redis8-local-validation-evidence.json` | Redis 3/Redis 8 双向 `SLOTSMGRTTAGONE` 记录 success 或 observable failure；失败时源端 key 保留 | RDB fragment compatibility | evidence JSON |
| linux-formal-handoff | review `redis8-linux-validation-handoff.md` | Linux e2e、性能基线、fork/RDB、replication、Docker/deploy 项目完整 | validation planning | handoff 文档 |
| cutover-runbook-draft | review `redis8-cutover-runbook-draft.md` | preflight/canary/ramp-up/full cutover/rollback 草案完整 | operations planning | runbook 文档 |

## 本地 runner 范围

- runner 启动临时 `codis-dashboard`、`codis-proxy`、两个默认 Redis 8 `codis-server` group master。
- `migration_method` 分两轮覆盖：`semi-async`、`sync`。
- 每轮通过 proxy 写入普通 key、hash tag key、非 0 DB key，再把相关 slot 迁移到 group 2 并验证迁移后读回。
- 跨版本矩阵直接启动 Redis 3 fallback 和 Redis 8 server，覆盖 string/hash/list/zset 样本。
- runner 不运行 `redis-benchmark`，不输出 throughput/latency 结论。

## 失败归属规则

- 二进制缺失或版本不对：归 `redis8-build-config-packaging` 或本地环境。
- Redis Tcl 单测失败：归 Redis 8 Codis server 对应 feature/issue。
- Go 回归失败：归 Go component adapter 或既有测试债务。
- e2e proxy/dashboard/admin 操作失败：先看 runner evidence 中 dashboard/proxy/redis log，再归 topom/proxy/server 集成边界。
- 跨版本迁移失败但源端 key 保留：可作为 observable failure 交给 Linux 正式阶段复核，不等价于 silent data loss。
- 跨版本迁移失败且源端 key 消失：阻塞 cutover，必须单独开 issue。
