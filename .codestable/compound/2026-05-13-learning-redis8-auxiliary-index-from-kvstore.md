---
doc_type: learning
track: knowledge
date: "2026-05-13"
slug: redis8-auxiliary-index-from-kvstore
component: codis-server
tags: [redis, redis8, codis-server, kvstore, index]
related_feature: 2026-05-13-redis8-slot-index-and-tag-index-core
---

# Redis 8 辅助索引应以 kvstore 为权威重建

## 1. 背景

Redis 8 的主 keyspace 已经不是 Redis 3 时代的单 dict，而是按 slot 组织的 `kvstore`。移植 Codis 的 slot/tag 能力时，最容易犯的错是把 Redis 3 的并行索引原样搬回来，例如重新维护 `hash_slots[1024]`。

这次 `redis8-slot-index-and-tag-index-core` 采用了另一条路径：slot 维度完全信任 Redis 8 `kvstore`，只为后续 tag migration 补一个必要的辅助视图 `redisDb.codis_tagged_keys`。该辅助视图不持久化、不作为 slot keyspace 权威，只在运行期维护，并在 full-load 后从 `kvstore` 重建。

## 2. 指导原则

- 主 keyspace 已经按目标维度分区时，不再建立同维度平行索引；Codis slot keyspace 直接复用 Redis 8 `kvstore`。
- 辅助索引只保存主 keyspace 缺失的查询维度；`codis_tagged_keys` 只保存带 `{...}` hash tag 的 key，并按完整 CRC32 分组。
- 生命周期调用点只调用 helper，不直接操作底层数据结构；skiplist 插入、删除、reset、rebuild 和 assert 都收在 `codisTagIndex*` helper 内。
- RDB load / replica full sync 后以 full-load rebuild 作为权威修复点，不把辅助索引写入 RDB/AOF。
- 内部不可见状态需要测试可观测点；`DEBUG CODIS-TAGINDEX-ASSERT` 用来扫描 `kvstore` 并校验辅助索引，不提前暴露业务命令。

## 3. 为什么重要

平行索引会制造双写一致性问题：每条 key 生命周期路径都要同时维护主 keyspace 和索引，漏掉 expire、evict、rename、move、copy、flush、RDB load 中任意一条都会留下脏状态。

把 `kvstore` 定义为唯一权威后，问题变得简单：

- slot count / slot dict 访问直接来自 `kvstore`，不会和另一份 slot 索引打架。
- tag index 只是可丢弃缓存式辅助视图，崩溃恢复或全量加载后可以从 `kvstore` 重建。
- helper 边界让 Redis 8 skiplist 的 embedded SDS 所有权集中处理，避免 lifecycle 调用点误持有 `robj*` 或 `kvobj` 内部指针。
- DEBUG assert 把“内部结构正确”变成可测试行为，后续扩展迁移命令前可以先守住数据结构不变量。

## 4. 何时适用

适用：

- 上游 Redis 新版本已经提供了更合适的主存储结构，例如 Redis 8 `kvstore`。
- 旧版本补丁里存在平行索引，但其中一部分查询维度已经被新主结构覆盖。
- 辅助索引可以从主 keyspace 完整重建，不需要跨重启保留额外状态。
- 改动处在 Redis key lifecycle、RDB load、lazyfree 这类高兼容路径，必须减少调用点直接操作复杂结构。

不适用：

- 辅助索引包含无法从主 keyspace 推导的业务状态。
- 索引本身是对外持久化协议的一部分，不能在 load 后重新计算。
- 查询语义要求实时包含不在 keyspace 中的 pending / tombstone / migration fence 状态。

## 5. 示例

这次 Redis 8 Codis mode 的落地形态：

- `SLOTSINFO`：通过 `codisSlotKeyCount` 从 `redisDb.keys` 的 1024-slot `kvstore` 取当前 DB slot key count。
- `codisHashInfoForKey`：一次解析返回 `slot`、完整 `crc` 和 `has_tag`，避免不同调用点重复实现 tag 规则。
- `codis_tagged_keys`：只记录 tagged key；score 是完整 CRC32，element 是 skiplist 自持有的 key SDS 副本。
- `dbAddInternal` / `dbGenericDelete`：运行期 add/delete hook 调 helper 维护 tag index。
- `rdbLoadRioWithLoadingCtx`：RDB load 成功后执行 `codisTagIndexRebuild`，从 `kvstore` 重建 tag index。
- `DEBUG CODIS-TAGINDEX-ASSERT`：测试时扫描 `kvstore`，确认 tagged key 与 `codis_tagged_keys` 一致。

一个 review 细节值得保留：不要假设所有加载路径都走普通 add hook。Redis 8 RDB load 调的是 `dbAddRDBLoad`，而本次 tag index 增量 hook 接在 `dbAddInternal`；因此“RDB load 逐 key 增量维护后又 full rebuild”的担忧需要先核对真实调用图，不能只凭函数名相似下结论。
