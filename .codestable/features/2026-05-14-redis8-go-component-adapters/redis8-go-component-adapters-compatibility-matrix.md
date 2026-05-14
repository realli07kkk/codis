---
doc_type: feature-artifact
feature: 2026-05-14-redis8-go-component-adapters
status: current
summary: Redis 8 Go component adapter compatibility matrix
tags: [redis, redis8, go, compatibility]
---

# redis8-go-component-adapters compatibility matrix

本矩阵把 roadmap 4.7 的兼容清单落成实现阶段可验证场景。优先级是：fake Redis 单元测试覆盖 Go parser / 编排，真实 Redis 8 smoke 覆盖 server 行为，packaging handoff 只记录本 feature 不改的边界。

| 场景 | Go 入口 | Redis 8 契约 | 验证方式 |
|---|---|---|---|
| INFO standalone 字段 | `pkg/utils/redis.Client.InfoFull` | `INFO` 返回普通 `key:value` 文本；无 replica 字段时不生成 `master_addr` | fake test + Redis 8 smoke |
| INFO replica 字段 | `InfoFull`、`cmd/admin`、`cmd/ha` | `master_host`、`master_port`、`master_link_status` 字段名保持可解析 | fake test + Redis 8 smoke |
| loading / master link keepalive | `pkg/proxy.BackendConn.KeepAlive` | `loading:1` 或 `master_link_status:down` 时不恢复 connected | proxy fake test |
| CONFIG GET maxmemory | `InfoFull` | 返回 `[maxmemory, integer]` | fake test + Redis 8 smoke |
| CONFIG REWRITE | `Client.SetMaster`、Redis 8 server | `CONFIG REWRITE` 不丢 `codis-enabled` 等 Codis 自定义配置 | Redis 8 smoke；失败交接 packaging / config |
| SLAVEOF / REPLICAOF | `Client.SetMaster` | 继续优先发送 `SLAVEOF`；Redis 8 alias 应可用 | fake test + Redis 8 smoke |
| CLIENT KILL TYPE normal | `Client.SetMaster` | Redis 8 接受 TYPE normal；事务子命令 error 必须冒泡 | fake test + Redis 8 smoke |
| AUTH default user | `NewClient`、`BackendConn.verifyAuth` | `AUTH <password>` 对 default user 成功；失败返回错误且不泄露密码 | fake test + Redis 8 smoke |
| SELECT current DB | `Client.Select`、`BackendConn.selectDatabase` | `SELECT <db>` 后 slot 命令和迁移只作用当前 DB | fake test + Redis 8 smoke |
| SLOTSINFO parser | `Client.SlotsInfo`、proxy `SLOTSINFO <addr>` | `[[slot_id,key_count], ...]`；畸形返回报错 | fake test + Redis 8 smoke |
| SLOTSMGRTTAGSLOT parser | `Client.MigrateSlot`、topom slot executor | `[migrated_count, remaining_count]`；返回 remaining count | fake test + Redis 8 smoke |
| SLOTSMGRTTAGSLOT-ASYNC parser | `Client.MigrateSlotAsync`、topom slot executor | `[migrated_count, remaining_count]`；restore async ACK error 不被吞 | fake test + Redis 8 smoke |
| SLOTSMGRT-EXEC-WRAPPER parser | proxy `forwardSemiAsync` | `[0,err]` retry，`[1,err]` wait/block，`[2,reply]` return reply | proxy fake test |
| proxy command boundary | `pkg/proxy/mapper.go` | 不放开 `CONFIG`、`SLAVEOF`、`SLOTSMGRT*`、`SLOTSRESTORE*` 等危险命令 | mapper test + diff review |
| ACL AUTH2 | Redis 8 server async restore | Go 不新增 username 配置；只验证命令存在与 ACK/error 语义 | Redis 8 smoke optional；packaging handoff |
| 默认构建切换 | Makefile / packaging | 本 feature 不切换默认 `codis-server` | diff review；packaging handoff |
