---
doc_type: feature-acceptance
feature: 2026-05-14-redis8-async-migration
status: current
accepted_at: 2026-05-14
summary: Redis 8 Codis Server 已具备异步迁移、restore async ACK、fence/cancel/status 和 exec wrapper 写保护能力。
tags: [redis, codis-server, redis8, migration, async]
---

# Redis 8 异步迁移验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-05-14
> 关联方案 doc：`.codestable/features/2026-05-14-redis8-async-migration/redis8-async-migration-design.md`
> 终审状态：待用户确认

## 1. 接口契约核对

对照方案第 2.1 节名词层逐一核查，接口契约通过。

**接口示例逐项核对**：

- [x] `SLOTSMGRTSLOT-ASYNC` / `SLOTSMGRTTAGSLOT-ASYNC`：返回 `[migrated_count, remaining_count]`。
  - 代码实际行为：一致。入口在 `extern/redis-8.6.3/src/slots_async.c` 的 `slotsmgrtSlotAsyncCommand` / `slotsmgrtTagSlotAsyncCommand`，共用 `slotsmgrtAsyncGenericCommand`；Tcl 覆盖 `SLOTSMGRTSLOT-ASYNC migrates current DB slot keys` 和 `SLOTSMGRTTAGSLOT-ASYNC migrates hash-tag group from slot`。
- [x] `SLOTSMGRTONE-ASYNC` / `SLOTSMGRTTAGONE-ASYNC`：返回 migrated count。
  - 代码实际行为：一致。显式 key 和 tag-aware key 都由 `slotsmgrtAsyncCreateIteratorFromCommand` 收集，测试覆盖单 key、同 tag key 和 timeout/cancel。
- [x] `SLOTSMGRTONE-ASYNC-DUMP` / `SLOTSMGRTTAGONE-ASYNC-DUMP`：只输出 restore async 命令流，不修改源端 keyspace。
  - 代码实际行为：一致。`slotsmgrtAsyncDumpGenericCommand` 只构建 iterator 并向当前客户端回复 `SLOTSRESTORE-ASYNC delete/object` 命令；Tcl 验证 dump 后源端 key 仍存在，目标端可执行命令流。
- [x] `SLOTSMGRT-ASYNC-FENCE/CANCEL/STATUS`：按当前 DB 的 async migration 状态工作。
  - 代码实际行为：一致。per-DB 状态存放在 `redisServer.slotsmgrt_cached_clients[dbid]`；`databases 32` smoke 已覆盖 DB 31 状态访问。
- [x] `SLOTSMGRT-EXEC-WRAPPER hashkey command ...`：迁移中写命令返回 `[1, error]`，读命令或无关 key 正常执行。
  - 代码实际行为：一致。`slotsmgrtExecWrapperCommand` 先返回 code，再用 `replaceClientCommandVector()` 执行包装命令；Tcl raw-read 覆盖 error element，普通读/无关写均通过。
- [x] `SLOTSRESTORE-ASYNC*`：目标端返回 `SLOTSRESTORE-ASYNC-ACK errno message`。
  - 代码实际行为：一致。`slotsrestoreReplyAck` 统一 ACK envelope；AUTH、AUTH2、SELECT、bad payload 和 object aliases 均有测试覆盖。

**名词层“现状 -> 变化”逐项核对**：

- [x] async migration 状态：`slotsmgrtAsyncClient`、client flags、`slotsmgrt_fenceq` 和 `redisServer.slotsmgrt_cached_clients` 已落地。
- [x] batched object iterator：slot 收集使用 Redis 8 `kvstore` per-slot dict，tag 扩展使用 `codisHashInfoForKey` 和 `redisDb.codis_tagged_keys`。
- [x] async command 接口：15 个 command JSON 文件已新增，`commands.def` 已同步生成。
- [x] lazy release：未新增 Redis 3 私有 pthread worker；Redis 8 metadata 对象统一走 `object` RDB payload 或失败保源。

**流程图核对**：

- [x] `Topom/Proxy -> Src SLOTSMGRT*-ASYNC -> cached client -> Dst SLOTSRESTORE-ASYNC -> ACK -> Src DEL propagation` 均有代码落点。

## 2. 行为与决策核对

**需求摘要逐项验证**：

- [x] Redis 8 Codis Server 已具备半异步迁移能力，命令面覆盖方案列出的 `SLOTSMGRT*-ASYNC`、dump-only、fence/cancel/status、exec wrapper 和 restore async ACK。
- [x] 成功迁移后源端 key 删除，目标端值和 TTL 正确；证据为 `unit/codis_async_migration` 的单 key、slot、tag、tag-slot、SELECT 和 DEL propagation 用例。
- [x] 任一失败路径不删除源端 key；证据覆盖目标不可达、AUTH failure、bad payload、timeout、cancel 和连接关闭。
- [x] 自定义 `databases 32` 下 DB 31 访问 async 状态不越界；review blocker 已修复，smoke：`SELECT 31` + `SLOTSMGRT-ASYNC-CANCEL` 返回 `OK` / `0`。

**明确不做逐项核对**：

- [x] 未修改 Go `pkg/` / `cmd/` 代码；`git diff --name-only` 无 `pkg/` / `cmd/`。
- [x] 未切换默认 `make` / `codis-server` 到 Redis 8；仍只使用独立 `make codis-server-redis8`。
- [x] 未实现 Redis Cluster 协议，未改 MOVED/ASK、cluster bus 或 Redis Cluster slot 状态。
- [x] 未恢复 Redis 3 `hash_slots[1024]` 平行索引；diff grep 无 `hash_slots`、`HASH_SLOTS_SIZE`、`hashSlotType` 或 `dictCreate(&hashSlotType...)`。
- [x] 未承诺 Redis 3 ↔ Redis 8 异步迁移跨版本互通；requirement 和 architecture 已保留该边界。
- [x] 未给 stream/module/HFE 新增 chunked 子协议；stream object payload 已用 `SLOTSRESTORE-ASYNC object` 测试覆盖。
- [x] 未新增 `redisServer.slotsmgrt_lazy_release` 或 `pthread_create` lazy release worker。

**关键决策落地**：

- [x] D1：继续使用 `slots_async.c` 承载异步迁移。真实状态机和 target ACK 子协议均集中在该文件。
- [x] D2：按 DB 维护 cached migration client。`server.slotsmgrt_cached_clients` 在 `initServer()` 中按最终 `server.dbnum` 分配。
- [x] D3：slot 来源只用 `kvstore`，tag 扩展只用 `codis_tagged_keys`。无平行 slot 索引。
- [x] D4/D5：restore async 写入路径 metadata-safe。`string/object/list/hash/dict/zset` 均走 RDB payload 校验和 Redis 8 restore 链路，stream 走 object payload。
- [x] D6：lazy release 不移植私有 pthread。实现没有引入新后台线程；旧 chunked old values 路径未恢复。
- [x] D7：AUTH 保持 password 兼容，同时补 AUTH2。`SLOTSRESTORE-ASYNC-AUTH` / `AUTH2` 均调用 Redis 8 ACL 认证并遮蔽密码参数。
- [x] D8：命令注册以 command JSON 为源。每个 async 命令都有 JSON，`commands.def` 是生成同步结果。

**编排层“现状 -> 变化”逐项核对**：

- [x] 源端主流程：校验参数 -> 当前 DB 无 active migration -> create/reuse cached client -> iterator 生成命令流 -> ACK 推进 -> 全 ACK 成功后删除源 key。
- [x] 目标端主流程：`SLOTSRESTORE-ASYNC` 子命令 -> payload 校验/恢复/删除/expire -> ACK 0 或 ACK 非 0 + close。
- [x] 错误流程：connect/write/read/AUTH/SELECT/restore/ACK/timeout/cancel/client close 均释放 async 状态并保源。
- [x] 可观测点：`STATUS` 返回 host、port、used、timeout、lastuse、sending_msgs、blocked_clients、batched_iterator；连接创建/释放和 ACK error 有 server log。

**挂载点反向核对（可卸载性）**：

- [x] 清单内挂载点均已落地：`slots_async.c`、`server.h`、`server.c`、`networking.c`、`blocked.c`、command JSON / `commands.def`、`codis_async_migration.tcl`。
- [x] 反向 grep：async 相关符号只命中上述挂载点。
- [x] 拔除沙盘推演：删除 async command JSON、`commands.def` 条目、`server.h` async 字段/声明、`server.c` async 数组和 cleanup、`networking.c` async hook、`blocked.c` blocked 类型分支、`slots_async.c` 真实逻辑和 Tcl 测试后，可回到同步迁移 feature 的 Redis 8 状态；不会留下独立运行时状态。

## 3. 验收场景核对

对照方案第 3 节关键场景清单，全部通过。

- [x] S1 command discoverable：`unit/codis_async_migration` 覆盖全部 async 命令 `COMMAND INFO`。
- [x] S2-S5 正常迁移 / slot / tag / dump-only：`unit/codis_async_migration` 覆盖单 key、slot、tagone、tagslot 和 dump-only。
- [x] S6-S8 restore async / AUTH / SELECT：`unit/codis_async_migration` 覆盖 object aliases、bad payload、AUTH/AUTH2 和 DB1 SELECT 隔离。
- [x] S9-S11 fence / status / exec wrapper：`unit/codis_async_migration` 覆盖 fence 等待、status 字段、写阻断、读命令和无关 key。
- [x] S12 large object：`unit/codis_async_migration` 使用 256 KiB string + 小 `maxbytes` 覆盖大对象 object payload 路径。
- [x] S13-S18 边界：空 slot、不存在 key、SELECT DB 隔离、stream object payload、默认 maxbytes/maxbulks、idle/timeout 清理均有测试或命令证据。
- [x] S19-S25 错误路径：目标不可达、AUTH failure、坏 payload、ACK 丢失/timeout、cancel、重复迁移、DEL propagation 均有测试覆盖。
- [x] 范围回归：`unit/codis_migration`、`unit/codis_slotsrestore`、`unit/codis` 通过，证明同步迁移、SLOTSRESTORE 和基础 Codis mode 无回归。

验证命令：

```text
make codis-server-redis8
./runtest --single unit/codis_async_migration
./runtest --single unit/codis_migration
./runtest --single unit/codis_slotsrestore
./runtest --single unit/codis
python3 .codestable/tools/validate-yaml.py --file .codestable/roadmap/redis8-upgrade/redis8-upgrade-items.yaml
python3 .codestable/tools/validate-yaml.py --file .codestable/features/2026-05-14-redis8-async-migration/redis8-async-migration-checklist.yaml
```

## 4. 术语一致性

- [x] `async migration`：代码命名统一为 `slotsmgrtAsync*`、`SLOTSMGRT*-ASYNC`、`SLOTSRESTORE-ASYNC*`。
- [x] `cached migration client`：代码统一使用 `slotsmgrt_cached_clients` / `CLIENT_SLOTSMGRT_ASYNC_CACHED_CLIENT`。
- [x] `restore async`：目标端命令统一为 `SLOTSRESTORE-ASYNC*`，ACK envelope 统一为 `SLOTSRESTORE-ASYNC-ACK`。
- [x] `fence`：命令为 `SLOTSMGRT-ASYNC-FENCE`，blocked type 为 `BLOCKED_SLOTSMGRT`。
- [x] `exec wrapper`：命令为 `SLOTSMGRT-EXEC-WRAPPER`，行为与方案一致。
- [x] 防冲突 grep：diff 未新增 `hash_slots`、`HASH_SLOTS_SIZE`、`hashSlotType`、`slotsmgrt_lazy_release`、`pthread_create` 或 Redis Cluster `keyHashSlot()` 语义修改。

## 5. 架构归并

- [x] `.codestable/architecture/ARCHITECTURE.md` 已更新 Redis 8 支线能力描述：同步迁移之后新增异步迁移、restore async ACK、fence/cancel/status 和 exec wrapper。
- [x] `.codestable/architecture/ARCHITECTURE.md` 数据与状态节已归并 `redisServer.slotsmgrt_cached_clients[dbid]`、cached client、blocked/fence client、batched iterator 和最终 `server.dbnum` 分配约束。
- [x] `.codestable/architecture/ARCHITECTURE.md` 代码锚点已新增 `slots_async.c`、server/networking/blocked hook、async command JSON 和 `codis_async_migration.tcl`。
- [x] `.codestable/architecture/ARCHITECTURE.md` 已知约束已补 async migration 的安全边界：所有 ACK 成功后才删源端 key，同 DB 单 active migration，status/fence/cancel 当前 DB 隔离。
- [x] `.codestable/attention.md` 不需要直接写入：本 feature 没暴露每个 CodeStable 工作流都会重复踩的环境/工具陷阱。

## 6. requirement 回写

- [x] 方案 frontmatter 指向 `requirement: redis-cluster-service`，该 req 当前为 `status: current`。
- [x] `.codestable/requirements/redis-cluster-service.md` 已追加 2026-05-14 实现进展：Redis 8 Codis Server 支线完成异步迁移、restore async、fence/cancel/status 和 exec wrapper。
- [x] req 边界已补充：Redis 8 支线仍不切换默认 Redis 3 Codis Server 构建，Go 组件适配、正式打包和 cutover 仍按 roadmap 后续条目推进。
- [x] `.codestable/requirements/VISION.md` 无需更新：能力仍为 `redis-cluster-service` current，不新增 requirement。

## 7. roadmap 回写

- [x] 方案 frontmatter 包含 `roadmap: redis8-upgrade` 和 `roadmap_item: redis8-async-migration`。
- [x] `.codestable/roadmap/redis8-upgrade/redis8-upgrade-items.yaml` 已将 `redis8-async-migration` 改为 `status: done`，`feature: 2026-05-14-redis8-async-migration`。
- [x] `.codestable/roadmap/redis8-upgrade/redis8-upgrade-roadmap.md` 第 5 节子 feature 清单已同步为 done，并记录本次异步迁移完成内容。
- [x] items.yaml 已通过 `validate-yaml.py` 校验。

## 8. attention.md 候选盘点

- [x] 无候选。本 feature 没暴露新的“每次启动都必须知道”的环境命令、路径陷阱或工作流约束；`Redis 8 源码固定在 extern/redis-8.6.3/`、`make codis-server-redis8` 和 `python3` 约束已在 attention.md 中存在。

## 9. 遗留

- 后续优化点：
  - `redis8-go-component-adapters`：验证 Go proxy/topom/admin 对 Redis 8 async/sync migration 命令、AUTH、SELECT、INFO、CONFIG 的兼容。
  - `redis8-build-config-packaging`：正式构建、配置模板和发布包装仍未切换。
  - `redis8-validation-cutover`：端到端演练、性能基线、灰度和回滚策略仍未完成。
- 已知限制：
  - 不承诺 Redis 3 ↔ Redis 8 异步迁移协议跨版本互通。
  - 不支持 Redis Cluster 协议，不改变 Codis 1024 slot 模型。
  - Redis 8 Codis Server 仍是独立支线，默认 `make` / `codis-server` 仍指向 Redis 3。
- 实现阶段顺手发现：
  - `slotsmgrt_cached_clients` 必须在 `initServer()` 中按配置加载后的最终 `server.dbnum` 分配；该 review blocker 已修复并通过 `databases 32` smoke。
  - `SLOTSMGRT-EXEC-WRAPPER` 在 Redis 8 中必须使用 `replaceClientCommandVector()` 改写命令向量，不能手动替换 `c->argv`，否则 pending command cleanup 会碰到引用生命周期问题；实现已按该方式收口。
