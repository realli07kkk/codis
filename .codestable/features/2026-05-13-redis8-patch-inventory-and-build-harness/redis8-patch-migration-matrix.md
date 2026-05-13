# Redis 8 Codis Patch Migration Matrix

> Feature: `2026-05-13-redis8-patch-inventory-and-build-harness`
> Roadmap item: `redis8-patch-inventory-and-build-harness`
> 日期：2026-05-13

## 1. 结论

Redis 3 Codis patch 不能按文件重放到 Redis 8。关键原因是 Redis 8 的 keyspace 已经变为 `kvstore` 主存储，slot 分区由 `slot_count_bits` 控制；Codis 的 `hash_slots[1024]` 平行索引不应继续移植。第一阶段只建立构建 harness 和文件级迁移矩阵，真实 Codis 模式、slot keyspace、tag index、迁移命令分别交给后续 roadmap item。

## 2. Codis 新增源文件

| Redis 3 文件 | 规模 / 角色 | Redis 8 当前状态 | 本阶段动作 | 后续归属 |
|---|---:|---|---|---|
| `extern/redis-3.2.11/src/slots.c` | 882 行；同步迁移、slot 查询/扫描/删除/hash/restore | `extern/redis-8.6.3/src/` 无对应文件 | 新增 `extern/redis-8.6.3/src/slots.c` stub object，不注册命令 | `redis8-codis-mode-foundation` 填 hash/基础命令入口；`redis8-slot-basic-commands` 和 `redis8-sync-migration-and-rdb-fragments` 填真实逻辑 |
| `extern/redis-3.2.11/src/slots_async.c` | 1770 行；异步迁移、restore async、fence/cancel/status、lazy release pthread | `extern/redis-8.6.3/src/` 无对应文件 | 新增 `extern/redis-8.6.3/src/slots_async.c` stub object | `redis8-async-migration` |
| `extern/redis-3.2.11/src/crc32.c` | 41 行；Codis CRC32 hash | `extern/redis-8.6.3/src/` 无对应文件 | 新增 `extern/redis-8.6.3/src/crc32.c`，保留 `crc32_init()` / `crc32_checksum()` 基础函数 | `redis8-codis-mode-foundation` 接入 `codisHashSlot()` |

## 3. Redis 原始文件修改矩阵

| Redis 3 patch 修改文件 | Redis 3 修改点 | Redis 8 对应位置 / API | 迁移风险 | 后续归属 |
|---|---|---|---|---|
| `server.h` | 增加 `HASH_SLOTS_MASK`、`HASH_SLOTS_SIZE`、`redisDb.hash_slots[1024]`、`hash_slots_rehashing`、`tagged_keys` | `extern/redis-8.6.3/src/server.h` 中 `redisDb` 已是 `kvstore *keys`、`kvstore *expires`、`estore *subexpires` | 高：不应移植 `hash_slots` 平行索引；只应新增 Codis tag index 和 Codis mode 状态 | `redis8-codis-mode-foundation`、`redis8-slot-index-and-tag-index-core` |
| `server.h` | 增加 `client.slotsmgrt_flags`、`slotsmgrt_fenceq` | Redis 8 `client` 结构体字段更多，client 生命周期和 async free 路径变化 | 中：异步迁移状态字段需要重新确认挂点，避免破坏 Redis 8 client 生命周期 | `redis8-async-migration` |
| `server.h` | 增加 `redisServer.slotsmgrt_cached_sockfds`、`slotsmgrt_lazy_release`、`slotsmgrt_cached_clients` | Redis 8 server 已有 `bio`、lazyfree、ACL/TLS 等更多运行态 | 中：cached client 可以移植，但 lazy release 应评估接 Redis 8 `bio` / lazyfree | `redis8-async-migration` |
| `server.h` | 声明 `slots*` / `slotsmgrt*` / `slotsrestore*` command proc | Redis 8 command proc 声明仍可放 `server.h`，但命令元数据来自 JSON → `commands.def` | 中：不要只改 command table；需要 command JSON/generator 协同 | `redis8-slot-basic-commands`、`redis8-sync-migration-and-rdb-fragments`、`redis8-async-migration` |
| `server.c` | 静态命令表直接注册 `SLOTSINFO`、`SLOTSMGRT*`、`SLOTSRESTORE*` | `extern/redis-8.6.3/src/commands.c` include `commands.def`；`commands.def` 由 `src/commands/*.json` 生成 | 高：Redis 8 命令表体系已变；需要新增 JSON specs 并生成 `commands.def`，本阶段不注册 | 后续各 command feature |
| `server.c` | `incrementallyRehash()` / `serverCron` 处理 `hash_slots` rehash | Redis 8 `kvstore` 管理 per-slot dict 和 rehash；keyspace 主存储已经 slot-aware | 高：不迁移 `hash_slots_rehashing`；改为验证 `kvstore` 在 Codis 1024 slot 下行为 | `redis8-codis-mode-foundation`、`redis8-slot-index-and-tag-index-core` |
| `server.c` | `prepareForShutdown()` 调用 `slotsmgrt_cleanup()` / `slotsmgrtAsyncCleanup()` | Redis 8 shutdown 流程仍在 `server.c`，但后台线程/bio 清理更多 | 中：异步迁移如继续自建资源，需接入 shutdown；若改用 `bio` 则清理方式不同 | `redis8-async-migration` |
| `server.c` | `initServer()` 初始化 slotsmgrt cache、lazy release worker、每个 DB 的 `hash_slots` / `tagged_keys` | Redis 8 `initServer()` 以 `slot_count_bits` 创建 `kvstore`；cluster 模式才使用 14 bits | 高：必须先引入 `codis_enabled`，再决定 `slot_count_bits=10`；tag index 单独新增 | `redis8-codis-mode-foundation`、`redis8-slot-index-and-tag-index-core` |
| `server.c` | `processCommand()` 特判 async restore auth | Redis 8 ACL / command flags / auth 体系变化 | 中：`SLOTSRESTORE-ASYNC-AUTH` 需验证 ACL default user 和 AUTH2 路径 | `redis8-async-migration`、`redis8-go-component-adapters` |
| `db.c` | `slotToKeyAdd()` / `slotToKeyDel()` 在 `dbAdd` / `dbDelete` 维护 `hash_slots` 和 `tagged_keys` | Redis 8 `dbAddInternal()`、`dbGenericDelete()`、`setKey` 等已按 `getKeySlot()` 访问 `kvstore` | 高：slot index 由 `kvstore` 主存储承担；只需补 tag index 生命周期 | `redis8-slot-index-and-tag-index-core` |
| `db.c` | `emptyDb()` / `flushdb` 清空 `hash_slots` 和重建 `tagged_keys` | Redis 8 `emptyDb()` / flush 流程已按 `kvstore` 清理 key/expires/subexpires | 中：不清 `hash_slots`；需要清理/重建 `codis_tagged_keys` | `redis8-slot-index-and-tag-index-core` |
| `db.c` | `freeDb()` 释放 `hash_slots` 和 `tagged_keys` | Redis 8 DB 生命周期释放 `kvstore` / `estore` | 中：只新增 tag index 释放；不要释放不存在的 parallel slot dict | `redis8-slot-index-and-tag-index-core` |
| `networking.c` | `createClient()` 初始化 `slotsmgrt_flags` / `slotsmgrt_fenceq` | Redis 8 client 初始化路径更复杂，包含 RESP3、tracking、client metadata 等 | 中：async migration client 字段需要在正确生命周期初始化 | `redis8-async-migration` |
| `networking.c` | `freeClient()` / reply path 调用 `slotsmgrtAsyncUnlinkClient()` 并处理 cached client 状态 | Redis 8 client free、async free、client eviction 路径变化 | 中：需保证 cached migration client 不被普通 free 路径误释放 | `redis8-async-migration` |
| `object.c` | 修改共享对象相关逻辑，使用 `OBJ_SHARED_REFCOUNT` / `makeObjectShared` | Redis 8 object encoding、shared object 和 refcount 机制已有变化 | 中：迁移 RDB/object restore 时需重新评估共享对象语义，避免直接套旧 refcount | `redis8-sync-migration-and-rdb-fragments` |
| `config.c` | 增加 Codis 配置项，例如迁移相关参数 | Redis 8 config system 使用 `createBoolConfig` / `standardConfig` 宏数组，`CONFIG REWRITE` 体系不同 | 高：`codis-enabled` 和后续迁移配置必须进入 Redis 8 config registry，且 rewrite 保留 | `redis8-codis-mode-foundation`、`redis8-async-migration` |
| `Makefile` | `REDIS_SERVER_OBJ` 增加 `slots.o`、`slots_async.o`、`crc32.o`，链接 `-lrt` | Redis 8 `src/Makefile` 的 object 列表更长，依赖 `commands.def`、`fpconv`、`fast_float`、`xxhash` 等 | 中：本阶段只加 objects；`-lrt` 在 macOS 不适用，不应无条件继承 | `redis8-patch-inventory-and-build-harness` |

## 4. Redis 8 关键 API 对照

| 能力 | Redis 3 依赖 | Redis 8 对应 | 迁移原则 |
|---|---|---|---|
| Slot 统计 / 扫描 | `redisDb.hash_slots[1024]` parallel dict | `kvstore *keys` 的 per-slot dict | Codis 模式下 `kvstore` 直接 1024 slot，不重建 parallel index |
| Key slot 计算 | `slots_num()` → CRC32 & `0x3ff` | `getKeySlot()` / `calculateKeySlot()`；cluster 模式用 CRC16 16384 | 后续引入 `codisHashSlot()`，Codis 模式返回 CRC32 1024 slot |
| Tag 迁移索引 | `redisDb.tagged_keys` zskiplist | 无等价结构 | 新增 `codis_tagged_keys` 并覆盖 key 生命周期和 RDB load rebuild |
| 命令注册 | `server.c` 静态命令表 | `src/commands/*.json` → `commands.def` → `commands.c` | 每个 Codis 命令补 JSON/generator，不长期手改 generated def |
| Config | 手工 config 项 | Redis 8 standard config registry | `codis-enabled` 和迁移配置必须可 parse/rewrite |
| Async lazy release | `slots_async.c` 自建 pthread | Redis 8 `bio` / lazyfree | 优先评估复用 Redis 8 机制 |

## 5. 本阶段验证重点

- `make codis-server-redis8` 必须走 Redis 8 构建并链接 `slots.o`、`slots_async.o`、`crc32.o`。
- 默认 `make codis-server` 必须仍走 Redis 3。
- 本阶段不出现 `codis_enabled`、Codis command JSON、`commands.def` diff、Go 组件适配 diff。
