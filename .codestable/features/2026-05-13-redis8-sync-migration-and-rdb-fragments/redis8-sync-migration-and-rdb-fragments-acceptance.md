# Redis 8 同步迁移与 RDB fragment 验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-05-13
> 关联方案 doc：`.codestable/features/2026-05-13-redis8-sync-migration-and-rdb-fragments/redis8-sync-migration-and-rdb-fragments-design.md`
> 终审状态：已确认（2026-05-13）

## 1. 接口契约核对

对照方案第 2.1 节名词层，接口契约核对通过。

**接口示例逐项核对**：

- [x] `SLOTSMGRTTAGONE host port timeout key`：命令注册在 `commands.def`，port 为 `ARG_TYPE_INTEGER`；入口调用 `slotsmgrtKeyCommand(..., slotsmgrtTagCommand)`，返回 migrated count。
- [x] `SLOTSMGRTTAGSLOT host port timeout slot`：命令注册在 `commands.def`；入口调用 `slotsmgrtSlotCommand(..., slotsmgrtTagCommand)`，返回 `[migrated_count, remaining_count]`。
- [x] `SLOTSRESTORE key ttlms payload [...]`：`slotsrestoreCommand` 校验三元组参数，使用 `verifyDumpPayload`、`rdbLoadType`、`rdbResolveKeyType`、`rdbLoadObject`、`dbAddInternal` 写入，返回 `+OK`。

**名词层“现状 → 变化”逐项核对**：

- [x] 同步迁移命令：5 个 command JSON 文件已加入，`commands.def` 已生成 `SLOTSMGRT*` 和 `SLOTSRESTORE` 的 `MAKE_CMD` 条目。
- [x] socket 缓存：`slotsmgrt_sockfd` 声明在 `server.h`，`redisServer.slotsmgrt_cached_sockets` 初始化在 `server.c`，清理 hook 接入 `serverCron`。
- [x] RDB fragment：源端用 `createDumpPayload` 生成 payload，目标端沿 Redis 8 metadata-aware restore 路径消费 payload。
- [x] tag-aware migration：通过 `redisDb.codis_tagged_keys` 和 `zslNthInRange` 查找同 CRC hash tag key。

**流程图核对**：

- [x] `Topom -> Src SLOTSMGRT* -> socket cache -> Dst SLOTSRESTORE -> Src DEL propagation` 均有代码落点；本 feature 没改 Go topom/proxy，只补齐 Redis 8 侧命令能力。

## 2. 行为与决策核对

**需求摘要逐项验证**：

- [x] Redis 8 ↔ Redis 8 同步迁移：`unit/codis_migration` 覆盖单 key、slot 随机、tag-aware 迁移。
- [x] RDB fragment restore：`unit/codis_slotsrestore` 覆盖 string/hash/list/set/zset/stream、TTL、多 key、覆盖已有 key。
- [x] 成功迁移后源端删除：`slotsmgrtRemoveKeys` 使用 `dbSyncDelete`，并通过 `alsoPropagate(... DEL ...)` + `preventCommandPropagation` 传播确定性 `DEL`。
- [x] 命令 metadata：迁移命令归 `@dangerous`，`SLOTSRESTORE` 带 `DENYOOM` 和 key spec；port 参数按 review 修正为 integer。

**明确不做逐项核对**：

- [x] 未移植 async migration：`SLOTSMGRT-ASYNC*`、`SLOTSRESTORE-ASYNC*`、`CLIENT_SLOTSMGRT_ASYNC_*` 无命中。
- [x] 未移植 `SLOTSMGRT-EXEC-WRAPPER`：命令注册无命中。
- [x] 未新增 `redisServer.slotsmgrt_lazy_release` / `slotsmgrt_cached_clients` 字段。
- [x] 未新增 `client.slotsmgrt_flags` / `slotsmgrt_fenceq` 字段。
- [x] 未修改 Go `cmd/` / `pkg/` 代码；`git diff --name-only -- cmd pkg` 为空。
- [x] 未宣称 Redis 3 ↔ Redis 8 RDB fragment 双向兼容；该项仍留给灰度验证。

**关键决策落地**：

- [x] 同步迁移放在 `slots.c`，不引入 `slots_async.c`。
- [x] socket cache 复用 Redis `migrateCacheDictType`，按 `host:port` 缓存，带 DB/AUTH/lasttime 状态。
- [x] `createDumpPayload` 统一声明在 `server.h`，源端迁移复用 Redis 8 `DUMP` payload 生成路径。
- [x] `SLOTSRESTORE` 不退回 Redis 3 的裸 `dbAdd` 路径，而是用 `dbAddInternal` 保留 Redis 8 metadata 行为。
- [x] review 中指出的 `kvobj *` / `robj *` 类型体操已消掉：迁移内核用 `kvobj *vals[]`，`lookupKeyWrite` 返回值不再硬转 `robj *`。
- [x] review 中指出的 `atoi(port)` 已消掉：`parsePort` 使用 `getRangeLongFromObjectOrReply(..., 0, 65535, ...)`。

**编排层“现状 → 变化”逐项核对**：

- [x] 源端流程：parse host/port/timeout/slot → 获取 key/value → `slotsmgrtGetSockfd` → AUTH/SELECT/SLOTSRESTORE → 成功后删除源端。
- [x] 目标端流程：`SLOTSRESTORE` 先完整解析所有 payload，全部合法后再执行覆盖写入，避免半解析半写入。
- [x] slot 随机迁移使用 `kvstoreGetDict(c->db->keys, slot)`，不恢复 Redis 3 `hash_slots[1024]` 平行索引。

**流程级约束核对**：

- [x] I/O 错误不删源端：connect/write/read/AUTH/SELECT/SLOTSRESTORE 任一失败返回 `-1`，调用方关闭 socket 后直接返回，不进入 `slotsmgrtRemoveKeys`。
- [x] 覆盖语义：`SLOTSRESTORE` 写入前 `dbDelete`，再 `dbAddInternal`。
- [x] DEL 传播语义：成功删除才加入 propagation argv；没有删除就不传播。
- [x] socket cache 清理：`serverCron` 每秒调用 `slotsmgrt_cleanup`，清理超过 TTL 的空闲连接；容量满时按 `lasttime` 驱逐最旧项。

**挂载点反向核对（可卸载性）**：

- [x] 挂载点清单与代码落点一致：`slots.c`、`server.h`、`server.c`、5 个 command JSON、`commands.def`、2 个 Tcl 测试。
- [x] 反向 grep：`SLOTSMGRT*` / `SLOTSRESTORE` / `slotsmgrt_cached_sockets` / `slotsmgrt_sockfd` 的命中均落在挂载点清单内。
- [x] 拔除沙盘推演：删除 5 个 JSON、`commands.def` 对应生成块、`slots.c` 同步迁移段、`server.h/server.c` socket cache 字段和 hook、2 个 Tcl 测试后，不应留下 Redis 8 同步迁移入口；Go 组件无需回滚。

## 3. 验收场景核对

对照方案第 3 节关键场景清单，当前结果如下：

- [x] S1 `SLOTSMGRTONE` 单 key 迁移：`unit/codis_migration` 覆盖源端删除、目标端值一致、TTL 保持。
- [x] S2 `SLOTSMGRTSLOT` 非空 slot：测试覆盖迁移一个随机 key 并返回 `[1, remaining]`。
- [x] S3 `SLOTSMGRTTAGONE` tag 迁移：测试覆盖同 `{tag}` key 全部迁移。
- [x] S4 `SLOTSMGRTTAGSLOT` tag slot 迁移：测试覆盖按 slot 随机 key 找同 tag group 并迁移。
- [x] S5 `SLOTSRESTORE` 单 key：`unit/codis_slotsrestore` 覆盖。
- [x] S6 `SLOTSRESTORE` 多 key：`unit/codis_slotsrestore` 覆盖。
- [x] S7 空 slot 执行 `SLOTSMGRTSLOT`：返回 `[0, 0]`，测试覆盖。
- [x] S8 不存在 key 执行 `SLOTSMGRTONE`：返回 `:0`，测试覆盖。
- [x] S9 socket 缓存复用：连续迁移共用同一 `host:port` cache key；测试套件在同一源/目标对上连续执行多条迁移，清理路径由 `serverCron` hook 和 `slotsmgrt_cleanup` 代码证据确认。
- [x] S10 目标不可达：返回 `-IOERR` 且源端 key 保留，测试覆盖。
- [x] S11 目标需 AUTH/SELECT：正常路径发送 `AUTH` / `SELECT`；密码不一致返回 `auth failed`，测试覆盖源端 key 保留。
- [x] S12 写入/读取失败不删除源端：错误路径均在 `slotsmgrtRemoveKeys` 前返回，目标不可达和 auth failure 测试覆盖。
- [x] S13 负 TTL：`SLOTSRESTORE` 返回 `invalid ttl value, must be >= 0`，测试覆盖。
- [x] S14 覆盖已有 key：`SLOTSRESTORE` 替换旧值，测试覆盖。
- [x] S15 成功迁移传播 `DEL`：replication stream 看到 `DEL prop:key`，测试覆盖。
- [x] S16 stream 类型恢复：stream entry 可读，测试覆盖。
- [x] S17 命令 metadata：5 个 JSON 和 `commands.def` 均已注册；`git diff --check` 通过。
- [x] S18 Redis 8 构建：`make codis-server-redis8` 通过。
- [x] S19 无效 payload：返回 checksum/version error，测试覆盖。
- [x] S20 错误 ttl 值：返回 `invalid ttl value, must be >= 0`，测试覆盖。

**本次执行过的验证命令**：

- [x] `make codis-server-redis8`
- [x] `cd extern/redis-8.6.3 && ./runtest --single unit/codis_migration`
- [x] `cd extern/redis-8.6.3 && ./runtest --single unit/codis_slotsrestore`
- [x] `cd extern/redis-8.6.3 && ./runtest --single unit/codis`
- [x] `git diff --check`
- [x] `python3 .codestable/tools/validate-yaml.py --file .codestable/roadmap/redis8-upgrade/redis8-upgrade-items.yaml`
- [x] `python3 .codestable/tools/validate-yaml.py --file .codestable/features/2026-05-13-redis8-sync-migration-and-rdb-fragments/redis8-sync-migration-and-rdb-fragments-checklist.yaml`

## 4. 术语一致性

- `SLOTSMGRT*`：只表示 Codis 同步迁移命令，不与 Redis Cluster `MIGRATE` 混用。
- `SLOTSRESTORE`：只表示同步迁移目标端 RDB fragment restore，不引入 async restore 命令。
- `slotsmgrt_sockfd` / `slotsmgrt_cached_sockets`：单条连接状态和 server dict 命名一致；review 中的 `slotsmgrt_cached_sockfds` 命名漂移已避免。
- `kvobj *`：keyspace value 数据流使用 `kvobj *`，不再通过 `robj *` cast 往返。
- 防冲突 grep：`slots_async`、`SLOTSMGRT-EXEC-WRAPPER`、`SLOTSRESTORE-ASYNC`、`slotsmgrt_lazy_release`、`slotsmgrt_flags` 等范围外术语无命中。

## 5. 架构归并

已按方案第 4 节实际更新 `.codestable/architecture/ARCHITECTURE.md`：

- [x] 名词归并：补入 `slotsmgrt_sockfd`、`redisServer.slotsmgrt_cached_sockets`、Redis 8 同步迁移命令和 `SLOTSRESTORE` 的系统级存在。
- [x] 动词骨架归并：补入 Redis 8 侧 `SLOTSMGRT* -> socket cache -> AUTH/SELECT -> SLOTSRESTORE -> dbSyncDelete -> DEL propagation` 主流程。
- [x] 流程级约束归并：补入 I/O / AUTH / SELECT / restore 失败不得删除源端 key、成功路径只传播 `DEL` 的约束。
- [x] 代码锚点归并：新增同步迁移相关 C 文件、command JSON 和 Tcl 测试锚点。

归并后，未读过 design 的人只看 architecture 也能知道 Redis 8 支线现在具备同步迁移和 RDB fragment restore，并能看到主要数据结构和错误边界。

## 6. requirement 回写

方案 frontmatter 指向 `requirement: redis-cluster-service`，该 requirement 已是 `current`。

- [x] 已更新 `.codestable/requirements/redis-cluster-service.md` 的“怎么解决”，补入后端 Codis Server 负责 slot keyspace 和迁移命令、Redis 8 支线已具备 Redis 8 ↔ Redis 8 同步迁移和 `SLOTSRESTORE`。
- [x] 已追加“实现进展”记录：2026-05-13 完成 Redis 8 同步迁移和 RDB fragment restore，同时明确不改变业务客户端协议、不切换默认 Redis 3 构建、不承诺 Redis 3 ↔ Redis 8 fragment 双向兼容。

## 7. roadmap 回写

方案 frontmatter 指向 `roadmap: redis8-upgrade` / `roadmap_item: redis8-sync-migration-and-rdb-fragments`。

- [x] 已更新 `.codestable/roadmap/redis8-upgrade/redis8-upgrade-items.yaml`：该 item 从 `in-progress` 改为 `done`。
- [x] 已同步 `.codestable/roadmap/redis8-upgrade/redis8-upgrade-roadmap.md` 第 5 节子 feature 清单：状态改为 `done`，对应 feature 改为本 feature 目录名，并补充完成备注。
- [x] 已运行 `validate-yaml.py --file` 校验 roadmap items YAML，通过。

## 8. attention.md 候选盘点

- [x] 本 feature 未暴露需要补入 `.codestable/attention.md` 的内容。现有 attention 已记录 Redis 8 路径、`make codis-server-redis8` 和 `python3` 工具约束，足够支撑后续 feature。

## 9. 遗留

**后续优化点**：

- `redis8-async-migration` 仍未启动；async restore、fence/cancel/status、lazy release 归后续 roadmap item。
- `redis8-go-component-adapters` 仍未启动；Go proxy/topom/admin 对 Redis 8 Codis Server 的系统级联调归后续 item。

**已知限制**：

- 当前只验收 Redis 8 ↔ Redis 8 同步迁移；Redis 3 ↔ Redis 8 RDB fragment 双向兼容仍需灰度前置验证。
- Redis 8 Codis Server 仍是独立构建支线；默认 `make` / `make codis-server` 不切换到 Redis 8。
- socket cache cleanup 的 >15 秒路径以代码 hook 和函数逻辑验收，没有为了该路径给 Tcl suite 增加真实 sleep。

**实现阶段顺手发现**：

- review 指出的 port 类型、`kvobj *` 类型体操、伪 `do { } while (0)`、命令入口 copy-paste、socket 随机驱逐、`createDumpPayload` extern 位置均已处理；未留下新的 design 偏差。
