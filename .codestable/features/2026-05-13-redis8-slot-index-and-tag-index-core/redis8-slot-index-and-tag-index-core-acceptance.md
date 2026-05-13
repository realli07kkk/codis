---
doc_type: feature-acceptance
feature: 2026-05-13-redis8-slot-index-and-tag-index-core
status: current
accepted_at: 2026-05-13
summary: Redis 8 Codis mode 已补齐 slot keyspace helper 和 codis_tagged_keys 生命周期维护，kvstore 仍是唯一 slot keyspace。
tags: [redis, codis-server, redis8, slot-keyspace]
---

# redis8-slot-index-and-tag-index-core 验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-05-13
> 关联方案 doc：`.codestable/features/2026-05-13-redis8-slot-index-and-tag-index-core/redis8-slot-index-and-tag-index-core-design.md`

## 1. 接口契约核对

对照方案第 2.1 节名词层逐一核查：

**接口示例逐项核对**：
- [x] `codisSlotKeyCount`（`extern/redis-8.6.3/src/slots.c`）：`codisSlotKeyCount(db, 899, &count)` 从当前 DB 的 Redis 8 `kvstore` slot dict 返回 key count；`slot < 0 || slot >= CODIS_SLOTS` 返回 `C_ERR`。`SLOTSINFO` 仍先通过 `parseSlot` / `parseSlotCount` 保持原错误语义。
- [x] `codisHashInfoForKey`（`extern/redis-8.6.3/src/server.h`）：`"{tag}:a"` 返回完整 CRC32、`slot=crc & 0x3ff`、`has_tag=1`；`"alpha"` 返回整 key CRC32、Codis slot 和 `has_tag=0`；`"{}abc"` 保持空 tag 视为 tagged key。
- [x] `codisTagIndexAdd` / `codisTagIndexDelete` / `codisTagIndexRebuild`（`extern/redis-8.6.3/src/slots.c`）：tagged key 进入 `redisDb.codis_tagged_keys`，untagged key 不进入；节点 score 是完整 CRC32，元素由 Redis 8 skiplist 嵌入式 SDS 持有。
- [x] `codisTagIndexRebuild`（`extern/redis-8.6.3/src/rdb.c` 触发）：RDB load 成功后从 `kvstore` 扫描重建 tag index，`DEBUG RELOAD` 后 `DEBUG CODIS-TAGINDEX-ASSERT` 通过。

**名词层“现状 -> 变化”逐项核对**：
- [x] Codis slot keyspace core：`SLOTSINFO` 不再直接调用 `kvstoreDictSize`，改为走 `codisSlotKeyCount`；slot keyspace 仍只来自 `redisDb.keys` 的 1024-slot `kvstore`。
- [x] Codis tag hash 信息：`codisHashSlot()` 退化为 `codisHashInfoForKey().slot`，不改变 `SLOTSHASHKEY` 返回协议。
- [x] Codis tag index：`redisDb` 新增 `zskiplist *codis_tagged_keys`，DB init、temp DB、flush/swap/lazyfree、add/delete、RDB load 均有维护点。
- [x] Full-load rebuild：`rdbLoadRioWithLoadingCtx()` 成功后对加载目标 DB array 执行 `codisTagIndexRebuild`；replica full sync temp DB 最终随 `swapMainDbWithTempDb` 进入 active DB。

**流程图核对**：
- [x] DB init -> create tag index：`server.c:initServer` 调用 `codisTagIndexCreate`。
- [x] key add/delete -> tag index add/delete：`db.c:dbAddInternal` 和 `db.c:dbGenericDelete` 调用 helper。
- [x] flush/async empty -> reset/free old index：`db.c:emptyDbStructure`、`lazyfree.c:emptyDbAsync`、`lazyfree.c:emptyDbDataAsync` 覆盖。
- [x] RDB load/full sync -> rebuild：`rdb.c:rdbLoadRioWithLoadingCtx` 调用 `codisTagIndexRebuild`。
- [x] observable assert：`debug.c` 注册 `DEBUG CODIS-TAGINDEX-ASSERT`，Tcl 用例调用 `codis_tag_assert`。

## 2. 行为与决策核对

**需求摘要逐项验证**：
- [x] `SLOTSINFO` 继续只从当前 DB 的 1024-slot `kvstore` 取 key count；`tests/unit/codis.tcl` 覆盖 DB0/DB1 当前 DB 语义。
- [x] tagged key 在新增、删除、rename、move、copy、expire、evict、flush、RDB reload 后 tag index 与 `kvstore` 扫描一致；证据为 `./runtest --single unit/codis` 通过。
- [x] untagged key 不进入 tag index；`SET plain 1` 后 `DEBUG CODIS-TAGINDEX-ASSERT` 通过。
- [x] 后续 tag migration 所需的完整 CRC32 分组已具备：`codis_tagged_keys` score 使用完整 CRC32，不使用 1024 slot id 混分组。

**明确不做逐项核对**：
- [x] 未新增 `hash_slots` 字段、数组或 `dictCreate(&hashSlotType...)` 路径；`git diff -U0 -- extern/redis-8.6.3/src ... | rg` 无新增命中。
- [x] 未注册 `SLOTSSCAN`、`SLOTSDEL`、`SLOTSCHECK`、`SLOTSMGRT*`、`SLOTSRESTORE*` 业务命令；源码 diff 无新增命令注册。
- [x] 未修改 Go `pkg/` 或 `cmd/` 代码；`git diff --name-only | rg '^(cmd|pkg)/'` 无命中。
- [x] 未切换默认 `codis-server` / `make` 到 Redis 8；只继续使用独立 `make codis-server-redis8`。
- [x] 未修改 Redis Cluster `keyHashSlot()` 的 CRC16/16384 语义；源码 diff 的新增行无 `keyHashSlot(` 功能改动。
- [x] 未把 tag index 写入 RDB/AOF 持久化格式；源码 diff 的新增行无 RDB save / AOF 写入路径。

**关键决策落地**：
- [x] 决策 1：slot index 只用 Redis 8 `kvstore`。代码没有恢复 Redis 3 `hash_slots`，`SLOTSINFO` 通过 slot helper 读取 `kvstore`。
- [x] 决策 2：tag index 只存 tagged key，score 使用完整 CRC32。`codisHashInfo` 暴露 `crc`，`zslInsert(..., info.crc, key)` 落地。
- [x] 决策 3：tag index 使用 helper 封装。生命周期调用点只调用 `codisTagIndex*` helper；skiplist 直接操作集中在 `slots.c` 和 `t_zset.c` 删除 helper。
- [x] 决策 4：RDB/full sync 使用 full-load rebuild。`rdbLoadRioWithLoadingCtx` 统一 rebuild，RDB load 不依赖运行期增量自然补齐。
- [x] 决策 5：测试用 DEBUG 断言，不提前暴露 slot/tag 业务命令。新增的是 `DEBUG CODIS-TAGINDEX-ASSERT`，没有新增 Codis 业务命令。

**编排层“现状 -> 变化”逐项核对**：
- [x] DB 初始化、temp DB 初始化、async empty 重建路径同步创建/替换 `codis_tagged_keys`。
- [x] key 新增侧在 `dbAddInternal()` 维护 tag index；覆盖同名 key 不重复插入，`codisTagIndexAdd` 先查重。
- [x] key 删除侧在 `dbGenericDelete()` 对 key SDS 仍可读时移除 tag index；expire/evict 通过该路径覆盖。
- [x] full-load 路径在 RDB load 成功后 rebuild，覆盖启动加载和 replica full sync temp DB。
- [x] DEBUG assert 扫描 `kvstore` 并对比 `codis_tagged_keys`，作为本阶段可观察验收面。

**流程级约束核对**：
- [x] 错误语义：Codis mode 未开启时 helper no-op；DEBUG 子命令返回 `codis mode is disabled`。
- [x] 幂等性：`codisTagIndexRebuild` 先 reset 再扫描；`codisTagIndexDelete` 删除不存在节点不崩溃。
- [x] 内存所有权：skiplist 节点持有 embedded SDS 副本，不持有 `robj*`。
- [x] 顺序约束：delete hook 放在对象释放前；async flush 先替换 active index，再把旧 index 交给 lazyfree。
- [x] 兼容性：Redis Cluster 语义未改，Codis mode 继续独立于 Redis Cluster 协议。
- [x] 可观测点：`SLOTSINFO` 和 `DEBUG CODIS-TAGINDEX-ASSERT` 均有 Tcl 覆盖。

**挂载点反向核对（可卸载性）**：
- [x] 清单内挂载点已落地：`redisDb.codis_tagged_keys`、Codis hash/tag helper、lifecycle hook、RDB rebuild、DEBUG 子命令、Tcl tests。
- [x] 反向 grep：`rg codisTagIndex|codis_tagged_keys|codisHashInfo` 命中均落在上述清单或声明处；`cluster_asm.c` 只因 `emptyDbDataAsync` 参数扩展传 `NULL`，属于 lazyfree 签名联动。
- [x] 拔除沙盘推演：删除 `redisDb.codis_tagged_keys` 字段、`codisTagIndex*` helper、DB/RDB/lazyfree hook、`DEBUG CODIS-TAGINDEX-ASSERT` 和对应 Tcl 后，系统退回 foundation 阶段；`SLOTSINFO` 可退回直接 `kvstoreDictSize`，但后续 slot command 无法复用 helper。

## 3. 验收场景核对

- [x] `make codis-server-redis8`：通过。
  - 证据来源：本轮验收命令。
  - 结果：通过。
- [x] `./runtest --single unit/codis`：foundation 原有 Codis mode / `SLOTSHASHKEY` / `SLOTSINFO` 用例继续通过。
  - 证据来源：Redis Tcl 单测。
  - 结果：通过。
- [x] `SET {tag}:a 1`、`SET {tag}:b 2`、`SET alpha 3` 后 tag assert。
  - 证据来源：`Codis tag index tracks key lifecycle operations`。
  - 结果：通过，alpha 不进入 tag index。
- [x] 同一个 tagged key 多次 overwrite。
  - 证据来源：`Codis tag index tracks key lifecycle operations`。
  - 结果：通过，无重复节点。
- [x] `DEL` / `UNLINK` tagged key。
  - 证据来源：`Codis tag index tracks key lifecycle operations`。
  - 结果：通过。
- [x] tagged key 短 TTL 过期后 assert。
  - 证据来源：`Codis tag index tracks key lifecycle operations`。
  - 结果：通过。
- [x] maxmemory eviction 后 assert。
  - 证据来源：`Codis tag index remains consistent after eviction`。
  - 结果：通过。
- [x] `RENAME` / `MOVE` / `COPY ... REPLACE` 后源/目标 DB assert。
  - 证据来源：`Codis tag index tracks key lifecycle operations`。
  - 结果：通过。
- [x] `FLUSHDB` / `FLUSHALL` / lazy user flush 后 assert。
  - 证据来源：`Codis tag index survives flush and RDB reload`，以及 async flush 路径的 lazyfree 编译覆盖。
  - 结果：通过。
- [x] `DEBUG RELOAD` 或等价 RDB reload 后 assert。
  - 证据来源：`Codis tag index survives flush and RDB reload`。
  - 结果：通过。
- [x] `SLOTSINFO 899 1` 返回格式保持上一阶段一致。
  - 证据来源：`SLOTSINFO reports Codis slot counts in the current DB`。
  - 结果：通过。

本 feature 无前端改动，不需要浏览器验证。

## 4. 术语一致性

- Codis slot keyspace core：代码集中在 `slots.c` 的 `codisSlotKeyDict` / `codisSlotKeyCount`，未引入其他命名。
- Codis tag hash 信息：`codisHashInfo` / `codisHashInfoForKey` 命名一致，`codisHashSlot` 保持兼容包装。
- Codis tag index：`codis_tagged_keys` 字段和 `codisTagIndex*` helper 统一。
- Tagged key：实现以 `has_tag` 表达，空 tag `{}` 保持 tagged。
- Full-load rebuild：`codisTagIndexRebuild` 命名与 design 一致。
- 防冲突：源码 diff 没有新增 `hash_slots`、`hashSlotType`、`dictCreate(&hashSlotType...)`、`SLOTSSCAN` / `SLOTSDEL` / `SLOTSCHECK` / `SLOTSMGRT*` / `SLOTSRESTORE*`。

## 5. 架构归并

对照方案第 4 节，已实际更新 `.codestable/architecture/ARCHITECTURE.md`：

- [x] 名词归并：补入 Redis 8 Codis mode 下 `kvstore` 是唯一 slot keyspace，`redisDb.codis_tagged_keys` 是按完整 CRC32 维护的 tag migration 辅助索引。
- [x] 动词骨架归并：补入 tag index 在 DB init、temp DB、flush/swap/lazyfree、key add/delete、RDB load、replica full sync 后维护或 rebuild。
- [x] 流程级约束归并：把 Redis 8 支线边界从“tag index 仍等待后续条目”更新为“slot scan/delete/check 命令、迁移协议、Go 组件适配仍等待后续条目”。
- [x] 代码锚点归并：`slots.c` 的说明从最小观察命令扩展为 slot keyspace helper / tag index helper / Tcl 回归测试。

未新增 architecture 子文档。当前只有总入口需要更新，且本 feature 没有引入新的长期子系统边界。

## 6. requirement 回写

- [x] 方案 frontmatter 指向 `requirement: redis-cluster-service`，该 req 当前为 `status: current`。
- [x] 本 feature 改的是 Redis 8 Codis Server 升级支线的内部 keyspace/tag index 能力，没有改变业务开发者、运维者或值班人员可见的用户故事、pitch 或边界。
- [x] 结论：`requirements/redis-cluster-service.md` 无需更新；能力承载形态已归并到 architecture。

## 7. roadmap 回写

对照方案 frontmatter：

- `roadmap: redis8-upgrade`
- `roadmap_item: redis8-slot-index-and-tag-index-core`

已完成回写：

- [x] `.codestable/roadmap/redis8-upgrade/redis8-upgrade-items.yaml`：对应 item 从 `in-progress` 改为 `done`，`feature` 保持 `2026-05-13-redis8-slot-index-and-tag-index-core`。
- [x] `.codestable/roadmap/redis8-upgrade/redis8-upgrade-roadmap.md`：第 5 节子 feature 清单对应条目改为 `状态：done`，对应 feature 改为本 feature 目录名，并同步备注。
- [x] YAML 校验：`python3 .codestable/tools/validate-yaml.py --file ...` 对 checklist 和 items.yaml 均通过。

## 8. attention.md 候选盘点

- [x] 无候选：本 feature 未暴露需要补入 attention.md 的新环境、工具或路径陷阱。Redis 8 路径、`make codis-server-redis8`、`python3` 规则已在 attention.md 中存在。

## 9. 遗留

- 后续优化点：无必须立即开 issue 的缺陷；实现阶段 review 提到的 rebuild 冗余 contains 和 assert 错误信息不足已修复。
- 已知限制：本 feature 不包含 `SLOTSSCAN`、`SLOTSDEL`、`SLOTSCHECK`、`SLOTSMGRT*`、`SLOTSRESTORE*`，这些仍在后续 roadmap item。
- 实现阶段顺手发现：RDB load 使用 `dbAddRDBLoad`，当前不存在“RDB load 逐 key 增量维护 tag index 后再 full rebuild 丢弃”的双重维护问题；该结论已在 review 处理时核对。
