---
doc_type: learning
track: pitfall
date: "2026-05-14"
slug: redis8-per-db-state-after-config-load
component: codis-server
severity: high
tags: [redis, redis8, codis-server, init, dbnum, migration]
related_feature: 2026-05-14-redis8-async-migration
---

# Redis 8 per-DB 状态必须按配置加载后的 dbnum 分配

## 1. 问题

Redis 8 里新增按 DB 隔离的运行时状态时，不能因为字段语义上“属于 server config”就放在 `initServerConfig()` 里按 `server.dbnum` 分配。

`initServerConfig()` 执行时 `server.dbnum` 仍可能只是默认值。用户配置文件里的 `databases` 会在后续 `loadServerConfig()` 中覆盖它。任何长度依赖最终 DB 数量的数组，都必须等配置加载完成后再分配。

本次 `redis8-async-migration` 一开始把 `server.slotsmgrt_cached_clients` 放在 `initServerConfig()` 中分配。默认 `server.dbnum` 是 16；如果用户配置 `databases 32`，运行时访问 DB 16-31 的 async migration 状态就会越界。

## 2. 症状

这个问题不一定在默认测试里暴露，因为 Redis 默认 DB 数是 16，数组大小和默认访问范围刚好一致。

风险会在这些条件同时成立时出现：

- 配置文件或命令行把 `databases` 设置成大于默认值，例如 32。
- 客户端 `SELECT` 到默认范围之外的 DB，例如 DB 31。
- 后续命令访问 `redisServer.slotsmgrt_cached_clients[dbid]`，例如 `SLOTSMGRT-ASYNC-CANCEL`、`SLOTSMGRT-ASYNC-STATUS` 或异步迁移命令。

后果是堆越界访问，轻则状态错乱，重则进程崩溃或破坏其他运行时数据。

## 3. 没用的做法

- 只看 `server.db` 的分配位置，不检查新增 per-DB 数组的分配位置。`server.db` 本身在 `initServer()` 中按最终 `server.dbnum` 分配，是正确样板；新增数组如果放早了，仍会出错。
- 只跑默认配置下的 async migration 测试。默认 16 DB 不会触发越界范围。
- 把字段初始化为“早分配 + 后续假设 dbnum 不变”。Redis 配置加载顺序不支持这个假设。

## 4. 解法

把依赖最终 DB 数量的内存分配移到 `initServer()`，跟 `server.db` 使用同一时序：

- `initServerConfig()` 里只把指针初始化为 `NULL`。
- `loadServerConfig()` 完成后，`initServer()` 使用最终 `server.dbnum`。
- 在 `server.db = zmalloc(sizeof(redisDb) * server.dbnum);` 之后分配 `server.slotsmgrt_cached_clients = zcalloc(sizeof(slotsmgrtAsyncClient) * server.dbnum);`。

本次修复后，`databases 32` smoke 覆盖了 `SELECT 31` 后执行 async 状态命令，证明数组按最终 DB 数量可访问。

## 5. 为什么有效

Redis 8 的启动顺序是关键约束：

- `initServerConfig()` 先建立默认配置值。
- `loadServerConfig()` 再读取配置文件和命令行覆盖项。
- `initServer()` 最后初始化依赖最终配置的运行时数据结构。

`server.db` 已经遵循这个模式。新增 `slotsmgrt_cached_clients` 与 `server.db` 一样是 per-DB 运行时状态，长度必须等于最终 `server.dbnum`，所以应该跟随 `server.db` 的分配时机，而不是跟随默认配置初始化。

这不是 async migration 特有问题。任何 Redis 8 Codis mode 新增的 per-DB 数组、per-DB 指针表或按 DB 编号索引的缓存，都有同一风险。

## 6. 预防

下次给 Redis server 增加 per-DB 状态时，先按这个顺序检查：

1. 这个结构是否通过 `c->db->id`、`dbid` 或 `SELECT` 后的当前 DB 编号索引。
2. 结构长度是否必须等于最终 `server.dbnum`。
3. 分配位置是否晚于 `loadServerConfig()`。
4. 是否可以放在 `initServer()` 中靠近 `server.db` 分配处。
5. 测试是否覆盖 `databases` 大于默认值的配置，例如 `databases 32` + `SELECT 31`。

代码 review 时遇到 `zmalloc(sizeof(T) * server.dbnum)` 或 `zcalloc(sizeof(T) * server.dbnum)`，必须同时核对调用点是在默认配置阶段还是最终配置阶段。
