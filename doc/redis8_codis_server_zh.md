# Codis Server（内嵌 Redis 8）扩展

`codis-server` 现在默认构建内嵌 Redis 8（`extern/redis-8.6.3`），`make codis-server` 产出的二进制同时是 Codis 的 slot server。配置 `codis-enabled yes` 后 Redis 8 进入 Codis 1024 slot 模式，本文描述该模式下的配置、slot 模型、扩展命令、tag 索引、RDB HTTP 导出和内部实现约束。历史背景：项目早期曾使用 Redis 3.2 内核，相关支持已移除；当前唯一内置版本为 Redis 8。

## 启用与配置

构建链路上 `make codis-server` 编译 `extern/redis-8.6.3` 并把 `src/redis-server` 复制为 `bin/codis-server`，同时由上游 `extern/redis-8.6.3/redis.conf` 注入 `codis-enabled yes` 与迁移认证配置生成 `config/redis.conf`（`codis-server-redis8` 目标再同步为 `config/redis8.conf`）。

Codis 模式配置项在 redis.conf：

```text
codis-enabled yes
databases 16
codis-migration-auth-user ""
codis-migration-auth-pass ""
```

- `codis-enabled`：内置默认 `no`，随包生成的 `config/redis.conf` 已写成 `yes`。immutable 配置，`CONFIG SET` 不接受，改动需重启。
- `cluster-enabled`：与 `codis-enabled` 互斥。两者同时为 `yes` 时配置加载失败并报 `codis-enabled and cluster-enabled are mutually exclusive`，不会静默选择一边。
- `databases`：Codis 模式不把 DB 数量固定为 1，`SELECT <db>` 和 Redis 多 DB 语义保持原样。
- `codis-migration-auth-user` / `codis-migration-auth-pass`：迁移连接使用的 ACL 用户与密码。两者为空时回退到 `requirepass`，保持旧行为；配置后迁移端以命名用户认证。
- `CONFIG REWRITE` 保留上述 Codis 自定义配置项。

不做什么：

- 不启用 Redis Cluster 协议：不产生 MOVED/ASK，不创建 cluster bus，不把 cluster-only 命令语义暴露给业务客户端。
- 不改变 RDB/AOF 持久化格式，也不改变协议层默认行为；Codis 模式只影响 slot 维度和 Codis 命令。

## Slot 模型

Codis 模式固定 1024 slot，与 Redis Cluster 的 CRC16/16384 是两套独立空间。

- 常量 `CODIS_SLOT_MASK_BITS`（10）、`CODIS_SLOTS`（1024）、`CODIS_SLOT_MASK`（`0x3ff`）定义在 `extern/redis-8.6.3/src/server.h`。
- `codisHashInfoForKey()` / `codisHashSlot()`：取 key 中第一个 `{...}` 子串作为 hash tag，对整个 key 或 tag 计算 CRC32，再 `& 0x3ff` 得到 0..1023 的 slot。与 Go proxy 侧 `pkg/proxy/mapper.go` 的 `Hash()`（`crc32.ChecksumIEEE(hashTagOrKey(key))`）再对 1024 取模等价。
- tag 规则差异：Codis 把第一个 `{...}` 视为 tag，即使 `{}` 为空也当作 tag；Redis Cluster 的 `keyHashSlot()` 把 `{}` 视为无 tag。这个差异在 `codisHashInfoForKey()` 中有显式注释。
- keyspace：`redisDb.keys` / `redisDb.expires` 是 `kvstore`，Codis 模式下以 `slot_count_bits = 10` 创建，per-slot dict 就是主 keyspace。不存在额外的 `hash_slots[1024]` 平行二级索引。
- `getKeySlot()` / `calculateKeySlot()` 在 Codis 模式返回 CRC32 slot；`SLOTSINFO`、`SLOTSSCAN`、`SLOTSDEL` 和迁移流程都通过 `kvstore` 的 slot dict API 访问，不重建 key 索引。
- `initTempDb()` 等临时 DB 与主 DB 使用相同 slot 数，不能只改主 DB。

| 模式 | `slot_count_bits` | slot 空间 | slot 计算 |
|---|---:|---:|---|
| standalone | 0 | 无 slot 分区 | 不适用 |
| `cluster-enabled yes` | 14 | 16384 | CRC16（Redis Cluster） |
| `codis-enabled yes` | 10 | 1024 | CRC32 & 0x3ff |

## 命令扩展

Codis 命令通过 Redis 8 的 command JSON 注册，源文件是 `extern/redis-8.6.3/src/commands/*.json`，生成的 `extern/redis-8.6.3/src/commands.def` 由生成器同步（`make -C extern/redis-8.6.3/src commands.def`）。实现集中在 `extern/redis-8.6.3/src/slots.c`（slot 查询、同步迁移、`SLOTSRESTORE`）和 `extern/redis-8.6.3/src/slots_async.c`（异步迁移、restore async、fence/cancel/status）。

| 命令 | arity | 函数 | flags | ACL | JSON |
|---|---:|---|---|---|---|
| `SLOTSCHECK` | 1 | `slotscheckCommand` | `READONLY` | `KEYSPACE` | `slotscheck.json` |
| `SLOTSDEL` | -2 | `slotsdelCommand` | `WRITE` | `KEYSPACE` | `slotsdel.json` |
| `SLOTSHASHKEY` | -1 | `slotshashkeyCommand` | `READONLY,FAST` | `KEYSPACE` | `slotshashkey.json` |
| `SLOTSINFO` | -1 | `slotsinfoCommand` | `READONLY,FAST` | `KEYSPACE` | `slotsinfo.json` |
| `SLOTSMGRT-ASYNC-CANCEL` | 1 | `slotsmgrtAsyncCancelCommand` | `FAST` | `KEYSPACE,DANGEROUS` | `slotsmgrt-async-cancel.json` |
| `SLOTSMGRT-ASYNC-FENCE` | 1 | `slotsmgrtAsyncFenceCommand` | `READONLY,NOSCRIPT,BLOCKING` | `KEYSPACE,DANGEROUS` | `slotsmgrt-async-fence.json` |
| `SLOTSMGRT-ASYNC-STATUS` | 1 | `slotsmgrtAsyncStatusCommand` | `FAST` | `KEYSPACE,DANGEROUS` | `slotsmgrt-async-status.json` |
| `SLOTSMGRT-EXEC-WRAPPER` | -3 | `slotsmgrtExecWrapperCommand` | `WRITE,DENYOOM` | `KEYSPACE,DANGEROUS` | `slotsmgrt-exec-wrapper.json` |
| `SLOTSMGRTONE-ASYNC-DUMP` | -4 | `slotsmgrtOneAsyncDumpCommand` | `READONLY,DENYOOM` | `KEYSPACE,DANGEROUS` | `slotsmgrtone-async-dump.json` |
| `SLOTSMGRTONE-ASYNC` | -7 | `slotsmgrtOneAsyncCommand` | `WRITE,NOSCRIPT,BLOCKING` | `KEYSPACE,DANGEROUS` | `slotsmgrtone-async.json` |
| `SLOTSMGRTONE` | 5 | `slotsmgrtoneCommand` | `WRITE` | `KEYSPACE,DANGEROUS` | `slotsmgrtone.json` |
| `SLOTSMGRTSLOT-ASYNC` | 8 | `slotsmgrtSlotAsyncCommand` | `WRITE,NOSCRIPT,BLOCKING` | `KEYSPACE,DANGEROUS` | `slotsmgrtslot-async.json` |
| `SLOTSMGRTSLOT` | 5 | `slotsmgrtslotCommand` | `WRITE` | `KEYSPACE,DANGEROUS` | `slotsmgrtslot.json` |
| `SLOTSMGRTTAGONE-ASYNC-DUMP` | -4 | `slotsmgrtTagOneAsyncDumpCommand` | `READONLY,DENYOOM` | `KEYSPACE,DANGEROUS` | `slotsmgrttagone-async-dump.json` |
| `SLOTSMGRTTAGONE-ASYNC` | -7 | `slotsmgrtTagOneAsyncCommand` | `WRITE,NOSCRIPT,BLOCKING` | `KEYSPACE,DANGEROUS` | `slotsmgrttagone-async.json` |
| `SLOTSMGRTTAGONE` | 5 | `slotsmgrttagoneCommand` | `WRITE` | `KEYSPACE,DANGEROUS` | `slotsmgrttagone.json` |
| `SLOTSMGRTTAGSLOT-ASYNC` | 8 | `slotsmgrtTagSlotAsyncCommand` | `WRITE,NOSCRIPT,BLOCKING` | `KEYSPACE,DANGEROUS` | `slotsmgrttagslot-async.json` |
| `SLOTSMGRTTAGSLOT` | 5 | `slotsmgrttagslotCommand` | `WRITE` | `KEYSPACE,DANGEROUS` | `slotsmgrttagslot.json` |
| `SLOTSRESTORE-ASYNC-ACK` | 3 | `slotsrestoreAsyncAckCommand` | `WRITE` | `KEYSPACE,DANGEROUS` | `slotsrestore-async-ack.json` |
| `SLOTSRESTORE-ASYNC-AUTH` | 2 | `slotsrestoreAsyncAuthCommand` | `NOSCRIPT,LOADING,STALE,FAST,NO_AUTH` | `CONNECTION` | `slotsrestore-async-auth.json` |
| `SLOTSRESTORE-ASYNC-AUTH2` | 3 | `slotsrestoreAsyncAuth2Command` | `NOSCRIPT,LOADING,STALE,FAST,NO_AUTH` | `CONNECTION` | `slotsrestore-async-auth2.json` |
| `SLOTSRESTORE-ASYNC-SELECT` | 2 | `slotsrestoreAsyncSelectCommand` | `LOADING,STALE,FAST` | `CONNECTION` | `slotsrestore-async-select.json` |
| `SLOTSRESTORE-ASYNC` | -2 | `slotsrestoreAsyncCommand` | `WRITE,DENYOOM` | `KEYSPACE,DANGEROUS` | `slotsrestore-async.json` |
| `SLOTSRESTORE` | -4 | `slotsrestoreCommand` | `WRITE,DENYOOM` | `KEYSPACE,DANGEROUS` | `slotsrestore.json` |
| `SLOTSSCAN` | -3 | `slotsscanCommand` | `READONLY` | `KEYSPACE` | `slotsscan.json` |

### slot 查询与同步迁移

```text
SLOTSHASHKEY key [key ...]                   -> array<int>，每项 0..1023
SLOTSINFO [start] [count]                    -> array<array<int,int>>，仅非空 slot 的 [slot, key_count]
SLOTSSCAN slot cursor [COUNT n]              -> [next_cursor, [key ...]]
SLOTSDEL slot [slot ...]                     -> array<array<int,int>>，删除后各 slot 的 [slot, key_count]
SLOTSCHECK                                  -> +OK，或 keyspace / tag 索引不一致的 error
SLOTSMGRTSLOT host port timeout slot         -> [migrated_count, remaining_count]
SLOTSMGRTTAGSLOT host port timeout slot      -> [migrated_count, remaining_count]
SLOTSMGRTONE host port timeout key           -> migrated_count（0 或 1）
SLOTSMGRTTAGONE host port timeout key        -> migrated_count（同 tag 组内迁移的 key 数）
SLOTSMGRT-EXEC-WRAPPER key command [arg ...] -> [status, <command 的原始回复>]
```

- 同步 `SLOTSMGRTSLOT` / `SLOTSMGRTONE` 一次迁移一个 key：`SLOTSMGRTSLOT` 返回 `[本次迁移的 key 数, 该 slot 剩余 key 数]`，`SLOTSMGRTONE` 返回 0 或 1；proxy 反复调用 `SLOTSMGRTSLOT` 直到剩余数为 0。
- `SLOTSMGRTTAGONE` / `SLOTSMGRTTAGSLOT` 以第一个参数定位 hash tag，把同一 tag 下的整组 key 一起迁移，返回该组迁移的 key 数；key 没有 tag 时退化为单 key 迁移。
- `SLOTSMGRT-EXEC-WRAPPER` 在源端包一层：`status = -1` 表示包装参数非法或命令未知（附 error），`0` 表示 key 不存在，`1` 表示 key 正在迁移且命令有写语义，`2` 表示已按原命令执行并附上原回复。
- 返回格式必须保持 Go 侧解析兼容（`pkg/utils/redis/client.go` 的 `SLOTSINFO` 与迁移返回数组解析），错误一律 Redis error reply，不新增非标准 envelope；slot 命令作用于当前 `SELECT` 后的 DB。

### 异步迁移家族

```text
SLOTSMGRTSLOT-ASYNC host port timeout maxbulks maxbytes slot numkeys     -> [migrated_count, remaining_count]
SLOTSMGRTTAGSLOT-ASYNC host port timeout maxbulks maxbytes slot numkeys  -> [migrated_count, remaining_count]
SLOTSMGRTONE-ASYNC host port timeout maxbulks maxbytes key [key ...]     -> migrated_count
SLOTSMGRTTAGONE-ASYNC host port timeout maxbulks maxbytes key [key ...]  -> migrated_count
SLOTSMGRTONE-ASYNC-DUMP timeout maxbulks key [key ...]                   -> array<SLOTSRESTORE-ASYNC 命令>
SLOTSMGRTTAGONE-ASYNC-DUMP timeout maxbulks key [key ...]                -> array<SLOTSRESTORE-ASYNC 命令>
SLOTSMGRT-ASYNC-FENCE                                                    -> +OK
SLOTSMGRT-ASYNC-CANCEL                                                   -> 0 或 1（是否释放了缓存的迁移连接）
SLOTSMGRT-ASYNC-STATUS                                                   -> 18 元素 key/value 数组，或 null array
```

- `-ASYNC` 变体在源端按 DB 保留一个缓存的迁移客户端，边扫描边把 `SLOTSRESTORE-ASYNC` 命令 pipeline 到目标；`maxbulks` 限制单批条数，`maxbytes` 限制目标输出缓冲上限，`timeout` 为 0 时取默认值。调用方阻塞到当前批次完成（命令 flags 带 `BLOCKING`），之后拿到 slot 维度的 `[migrated, remaining]` 或单 key 维度的计数。
- 流控靠目标端回 ACK：每收到一条 `SLOTSRESTORE-ASYNC-ACK` 才继续发下一批。ACK 带非 0 errcode 时源端中止本次迁移，slot 数据保持在源端。
- `-DUMP` 变体不建立连接：直接把要发送的 `SLOTSRESTORE-ASYNC` 命令整段序列化返回，由 proxy 侧自行转发。
- 同一 DB 同时只允许一个进行中的异步迁移；再次发起迁移会报错，迁移中 key 的写入由 proxy 通过 `SLOTSMGRT-EXEC-WRAPPER` 拦截（返回 1）。`SLOTSMGRT-ASYNC-FENCE` 阻塞到当前批次边界并返回 `+OK`。
- `SLOTSMGRT-ASYNC-STATUS` 输出 `host`、`port`、`used`、`timeout`、`lastuse`、`since_lastuse`、`sending_msgs`、`blocked_clients` 和 `batched_iterator`（含 `keys`、`maxbulks`、`maxbytes`、`emitted_msgs`）等字段，供 proxy 观测进度。

### RDB fragment restore

```text
SLOTSRESTORE key ttlms serialized-value [key ttlms serialized-value ...] -> +OK
SLOTSRESTORE-ASYNC delete key
SLOTSRESTORE-ASYNC <type> key ttlms serialized-value
SLOTSRESTORE-ASYNC-AUTH password            -> ACK
SLOTSRESTORE-ASYNC-AUTH2 username password  -> ACK
SLOTSRESTORE-ASYNC-SELECT db                -> ACK
SLOTSRESTORE-ASYNC-ACK errcode message
```

- `SLOTSRESTORE` 一次可带多个 `key ttl payload` 三元组，`payload` 是单个对象的 RDB 序列化片段（与 `DUMP`/`RESTORE` 同编码），`ttl` 为 0 表示无过期，重复 key 会先删除再写入。
- `SLOTSRESTORE-ASYNC` 一条只处理一个 key，`<type>` 取 `object`（不限定类型）、`string`、`list`、`hash`、`dict`（即 set）、`zset`，目标端校验声明类型与 payload 实际类型一致，`delete` 表示在目标端删除该 key。
- 认证与切库由 `SLOTSRESTORE-ASYNC-AUTH`、`-AUTH2`、`-SELECT` 完成；回复统一是 `["SLOTSRESTORE-ASYNC-ACK", errcode, message]`，errcode 非 0 时目标端关闭连接，避免留下半认证状态。
- 迁移兼容性：只保证 Redis 8 ↔ Redis 8 使用当前 Redis 8 RDB fragment 编码；不承诺跨大版本 fragment 兼容。遇到目标端不能识别的 object encoding 必须显式失败，不能静默丢 key 或按错误类型写入。

## Tag 索引

`SLOTSMGRTTAGONE` / `SLOTSMGRTTAGSLOT` 需要按 hash tag 找出同组 key，Redis 8 `kvstore` 只提供 slot 维度视图，因此 Codis 模式在 DB 上补了一个可丢弃的辅助索引。

- `redisDb.codis_tagged_keys` 是 `zskiplist`：score 是完整 CRC32，element 是 skiplist 自持有的 key SDS 副本，只记录带 `{...}` tag 的 key。
- 生命周期调用点只调用 helper：`codisTagIndexAdd`、`codisTagIndexDelete`、`codisTagIndexReset`、`codisTagIndexRebuild`、`codisTagIndexAssert`。skiplist 插入、删除、reset、rebuild 和断言都收在 helper 内，调用点不直接操作结构，避免误持有 `robj*` 或 `kvobj` 内部指针。
- 维护挂点覆盖 `dbAddInternal()`、`dbGenericDelete()`、覆盖写（set/overwrite）路径、`emptyDb()` / `FLUSHDB` 以及异步 flush 的释放路径。
- 可丢弃性：索引不写 RDB/AOF，不是 keyspace 权威。RDB 加载或 replica 全量同步完成后由 `codisTagIndexRebuild` 以 `kvstore` 为准全量重建；索引丢失只影响按 tag 迁移的可用性，不影响数据正确性。
- 一致性检查：`DEBUG CODIS-TAGINDEX-ASSERT` 扫描当前 DB 的 `kvstore` 并校验 tag 索引，`SLOTSCHECK` 同时校验 slot keyspace 与 tag 索引。两者都是诊断入口，不对业务暴露额外命令。

不做什么：

- 不重建 `hash_slots[1024]` 平行 slot 索引。主 keyspace 已按 slot 分区，再维护同维度索引会引入双写一致性负担：expire、evict、rename、move、copy、flush、RDB load 任意一条路径漏写都会留下脏状态。
- 不把 tag 索引做成持久化状态，也不在加载路径上假设“增量 hook 会补齐”；全量重建是权威修复点。

## RDB HTTP 导出

Redis 8 Codis Server 可以在既有 Redis 端口上提供只读的 RDB 下载口，默认关闭，只导出本机 `dbfilename` 对应的最新已完成 RDB 文件。

```text
codis-rdb-export-enabled no
codis-rdb-export-auth ""
codis-rdb-export-rate-limit 0
```

- `codis-rdb-export-enabled`：默认 `no`，immutable，改动需重启。开启后进程只拦截精确的 `GET /codis/rdb/latest`（HTTP/1.0 或 HTTP/1.1）。
- `codis-rdb-export-auth`：`X-Codis-RDB-Auth` header 密钥，sensitive 且 immutable。enabled 为 `yes` 而 auth 为空时启动失败；密钥不放 URL 或 query。
- `codis-rdb-export-rate-limit`：成功响应的 body 全局限速，默认 `0` 表示不限速，可写字节数或 Redis size 单位（如 `1mb`）。支持 `CONFIG SET` 运行期调整，`CONFIG REWRITE` 持久化。

请求与成功响应：

```text
GET /codis/rdb/latest HTTP/1.1
X-Codis-RDB-Auth: <secret>

HTTP/1.1 200 OK
Content-Type: application/octet-stream
Content-Length: <bytes>
Content-Disposition: attachment; filename="dump.rdb"
X-Codis-RDB-Mtime: <unix seconds>
Connection: close
```

- 候选文件只有当前 `dir` 下 basename 等于 `server.rdb_filename` 的普通文件；其他 `.rdb` 备份副本、`temp-*.rdb` 和 symlink 都不在候选内。Redis 写入走临时文件 rename 到 `dbfilename`，因此这条路径就是最新完成的持久化 RDB。
- 双重校验：文件身份（basename、`lstat` 为普通文件、`open` 后 `fstat` 的 device/inode/mode 与 `lstat` 一致）+ 文件内容（前 5 字节为 `REDIS` magic），降低 TOCTOU 与文件被替换的风险。
- 认证失败返回 403 且不打开文件；没有有效 RDB 返回 404；header 超过 8KB 返回 400。错误响应都是短文本加 `Connection: close`，不回显绝对路径或密钥。
- 传输走专用 streaming 状态，按 chunk 从 fd 写出，不把 RDB 放进 reply buffer，也不使用 `sendfile`。连接进入导出后不再回 RESP parser，完成后关闭连接；传输期间文件被删除或 rename 不影响已打开的 fd。
- 限速为进程级 token bucket：所有导出连接共享一个 body 预算，bucket 容量最多一个写出切片（`min(rate_limit, NET_MAX_WRITES_PER_EVENT)`），避免空闲后攒出突发。额度不足时移除该连接的 writable handler，并用 time event 延后恢复，让主循环先处理普通命令、IO 线程回主线程的队列和 `beforeSleep` 任务。
- 低优先级是协作式的，不是事件循环的硬优先级；HTTP header 与错误响应不参与限速，普通 Redis 命令和普通 reply 也不消耗这个 bucket。

不做什么：

- 不执行 `SAVE`、`BGSAVE`，不调用 `rdbSave*`，不改 `lastsave`、dirty、AOF 或复制状态；一次下载不生成新快照。
- 不接受客户端指定路径、DB、slot 或 key pattern；不支持 Range、断点续传、压缩、加密、独立 HTTP 端口和 per-client 公平调度。

Dashboard 侧的 RDB Analysis 可以直接用这个接口远程拉取 RDB，配置在 `dashboard.toml`：

```toml
rdb_analysis_remote_fetch_enabled = false
rdb_analysis_remote_fetch_auth = ""
rdb_analysis_remote_fetch_timeout = "30m"
rdb_analysis_remote_fetch_max_concurrent = 1
```

- `rdb_analysis_remote_fetch_enabled`：默认关闭；开启时 `rdb_analysis_remote_fetch_auth` 必须非空，否则 dashboard 配置校验失败。
- `rdb_analysis_remote_fetch_auth`：dashboard 持久化的 export 密钥，只放在发往 Redis 的 `X-Codis-RDB-Auth` header，不进入请求体、API 响应、job source 或日志。
- `rdb_analysis_remote_fetch_timeout` / `rdb_analysis_remote_fetch_max_concurrent`：单次拉取时长上限与并发上限；文件大小上限复用 `rdb_analysis_max_upload_size`。
- 目标只允许当前 product 拓扑内的 server 地址，dashboard 不跟随 redirect，下载完整后才创建分析任务。

细节见 [rdb_analysis_zh.md](rdb_analysis_zh.md)。

## 内部实现要点

### 改 C 代码前必读：线程模型

- 命令执行仍在主线程串行：`accept`、命令执行、DB/复制/AOF 状态修改、`serverCron`、`beforeSleep` / `afterSleep` 都在主线程。
- 网络 IO 线程默认不启用：`io-threads` 默认值为 1，`config/redis8.conf` 只留注释示例；配置为 N 时实际创建 N-1 个 `io_thd_*`，每个线程有自己的事件循环。
- IO 线程只做 socket 读写、query buffer 填充和 RESP 解析。读到完整命令后不执行，只标记 `CLIENT_IO_PENDING_COMMAND` 交回主线程；主线程在 `beforeSleep` 里处理这些客户端和 pending write，再把客户端送回 IO 线程。有竞态风险的客户端（Pub/Sub、monitor、blocked、tracking、Lua debug、Codis RDB export、迁移中的客户端）留在或切回主线程。
- BIO 三线程 `bio_close_file`、`bio_aof`、`bio_lazy_free` 只处理主线程逻辑上已摘除的资源：文件 close、AOF fsync、大对象与异步 `FLUSH*` 的延迟释放。它们不执行命令，也不碰 keyspace。
- `jemalloc-bg-thread yes` 默认开启 jemalloc 后台 purge 线程；模块线程由模块自己创建，通过 module pipe 与 module GIL 和主线程协作。`BGSAVE`、AOF rewrite 是 `fork()` 子进程，不是线程。
- 结论：新增的 Codis 逻辑默认只在主线程执行；跨线程只允许“识别形态 + 移交客户端”，不允许在 IO 线程里读 `server.rdb_filename`、打开文件或改迁移状态。

### per-DB 状态分配时机

- 长度等于最终 `server.dbnum` 的数组必须在 `initServer()` 中、`server.db` 分配之后创建，例如 `server.slotsmgrt_cached_clients = zcalloc(sizeof(slotsmgrtAsyncClient) * server.dbnum)`。
- 不能放在 `initServerConfig()`：那时 `server.dbnum` 还是默认值，用户配置的 `databases` 要在 `loadServerConfig()` 之后才生效。按默认 16 分配、又在 `SELECT 31` 后索引，就是堆越界。
- `initServerConfig()` 只把指针置 `NULL`；任何按 DB 编号索引的缓存或指针表都遵守同一时序。

### 实现约束

- Redis 8 的 keyspace 已是 `kvstore` 主存储，slot 分区由 `slot_count_bits` 控制，不再需要 `hash_slots[1024]` 平行索引。
- slot 维度直接复用 `kvstore`，只有 tag 维度补 `codis_tagged_keys`。
- 新增命令必须同时补 command JSON 并让生成器同步 `commands.def`，不要长期手改生成文件；新增配置必须进 Redis 8 标准 config registry，保证 `CONFIG GET` 与 `CONFIG REWRITE` 行为一致。

## 已知限制

- 迁移只保证 Redis 8 ↔ Redis 8；不承诺跨大版本 fragment 或持久化文件兼容。
- tag 索引是运行期辅助视图，崩溃恢复或全量加载后依赖 `codisTagIndexRebuild` 重建；它不写 RDB/AOF，故障时只影响按 tag 迁移。
- `SLOTSINFO` 只返回非空 slot，proxy 侧不要假设返回长度等于请求区间。
- `SLOTSCHECK` 和 `DEBUG CODIS-TAGINDEX-ASSERT` 会全量扫描当前 DB，适合诊断而非高负载实例的常规巡检。
- RDB HTTP 导出默认关闭、只导出 `dbfilename`，不支持 Range、断点续传、压缩、加密，也没有独立端口或 TLS 终止；暴露面仍依赖网络隔离与可信内网。
- 导出限速是进程级总预算，不承诺多连接之间公平。
- 迁移中的写请求会被拒绝（`SLOTSMGRT-EXEC-WRAPPER` 返回 1），客户端需要依赖 proxy 的重试或等待策略。

相关文档：[rdb_analysis_zh.md](rdb_analysis_zh.md)（dashboard RDB 分析）、[tutorial_zh.md](tutorial_zh.md)（集群部署与运维）。
