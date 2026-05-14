---
doc_type: feature-artifact
feature: 2026-05-14-redis8-go-component-adapters
status: current
summary: Redis 8 Codis Server smoke evidence for Go component adapters
tags: [redis, redis8, smoke, compatibility]
---

# redis8-go-component-adapters smoke

执行日期：2026-05-14

## 环境

- 二进制：`bin/codis-server-redis8`、`bin/redis-cli-redis8`
- 临时目录：`/tmp/codis-redis8-go-adapters.*`（执行后已清理）
- 实例：
  - source：`127.0.0.1:46379`
  - target：`127.0.0.1:46380`
  - replica-control：`127.0.0.1:46381`
- 关键配置：`codis-enabled yes`、`databases 4`、`requirepass secret`、`masterauth secret`

## 覆盖项

| 场景 | 结果 |
|---|---|
| AUTH / PING | source 返回 `PONG` |
| SELECT / SET 当前 DB | source DB2 `SET {go-sync}:k sync-value` 返回 `OK` |
| SLOTSHASHKEY / SLOTSINFO | `{go-sync}:k` slot 为 `329`，DB2 `SLOTSINFO` 返回 `329, 1` |
| 同步迁移 | `SLOTSMGRTTAGSLOT 127.0.0.1 46380 3000 329` 返回 `1, 0`；target DB2 `GET {go-sync}:k` 返回 `sync-value` |
| 异步迁移 | `SLOTSMGRTTAGSLOT-ASYNC 127.0.0.1 46380 3000 200 1048576 180 100` 返回 `1, 0`；target DB2 `GET {go-async}:k` 返回 `async-value` |
| INFO master 字段 | source `INFO replication` 返回 `role:master` |
| SLAVEOF alias | replica-control 执行 `SLAVEOF 127.0.0.1 46379` 返回 `OK` |
| INFO replica 字段 | replica-control `INFO replication` 返回 `role:slave`、`master_host:127.0.0.1`、`master_port:46379`、`master_link_status:up` |
| CLIENT KILL TYPE normal | replica-control 返回 `0`，命令可用 |
| CONFIG REWRITE | replica-control 返回 `OK`，重写后的临时配置保留 `codis-enabled yes` |
| SLAVEOF NO ONE | replica-control 返回 `OK` |

## 结论

真实 Redis 8 Codis Server smoke 未暴露 Go 侧必须新增生产 adapter 的不兼容点。`SLAVEOF` alias 在 Redis 8 可用，`INFO` 字段名保持 Go parser 可消费，`SLOTSINFO` 与同步/异步迁移返回格式符合 Go 侧严格 parser 预期，`CONFIG REWRITE` 未丢 `codis-enabled yes`。
