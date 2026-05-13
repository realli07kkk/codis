---
doc_type: feature-acceptance
feature: 2026-05-13-redis8-slot-basic-commands
status: current
accepted_at: 2026-05-13
summary: Redis 8 Codis mode 已补齐基础 slot 命令，SLOTSHASHKEY/SLOTSINFO/SLOTSSCAN/SLOTSDEL/SLOTSCHECK 均以 1024-slot kvstore 为权威来源。
tags: [redis, codis-server, redis8, slot-commands]
---

# redis8-slot-basic-commands 验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-05-13
> 关联方案 doc：`.codestable/features/2026-05-13-redis8-slot-basic-commands/redis8-slot-basic-commands-design.md`

## 1. 接口契约核对

对照方案第 2.1 节名词层逐一核查：

**接口示例逐项核对**：
- [x] `COMMAND INFO SLOTSSCAN SLOTSDEL SLOTSCHECK` 可发现三条新命令；`SLOTSDEL` 标记为 write，slot 参数没有被声明为 key spec。
- [x] `SLOTSHASHKEY key [key ...]` 保持 Codis CRC32 / hash tag 语义；无参数时返回空 array。
- [x] `SLOTSINFO [start] [count]` 继续返回 `[[slot_id, key_count], ...]`，只包含当前 DB 范围内非空 slot；负数 count 报错，超大 count 截断到 `CODIS_SLOTS`。
- [x] `SLOTSSCAN slot cursor [COUNT n]` 使用 `kvstoreScan(..., onlydidx=slot, ...)` 扫描指定 slot dict，返回 `[next_cursor, [key ...]]`；`COUNT < 1` 和未知选项返回 syntax error。
- [x] `SLOTSDEL slot [slot ...]` 先完成所有 slot 参数预校验，再按输入 slot 顺序删除当前 DB 的 key，返回 `[[slot_id, remaining_count], ...]`；重复 slot 保留重复返回项。
- [x] `SLOTSCHECK` 扫描当前 DB 的 1024-slot `kvstore`，确认 key 所在 dict index 与 `codisHashInfoForKey(...).slot` 一致，并复用 `codisTagIndexAssert`。

**实现细节核对**：
- [x] 新增 command JSON：`slotsscan.json`、`slotsdel.json`、`slotscheck.json`；`commands.def` 同步生成，`server.h` 增加命令声明。
- [x] `SLOTSSCAN` 返回 key 时复制 SDS 并设置 list free method，不持有 dict-owned SDS 裸指针。
- [x] `SLOTSDEL` 删除时先在 scan callback 中收集 key object 副本，再在 callback 外调用 `dbSyncDelete`、`keyModified` 和 `server.dirty++`。
- [x] `SLOTSDEL` 删除后读取剩余 count 时检查 `codisSlotKeyCount` 返回值，避免失败路径向客户端返回未初始化值。

## 2. 行为与决策核对

**需求摘要逐项验证**：
- [x] Redis 8 Codis Server 在 `codis-enabled yes` 下具备基础 slot 命令完整可用面。
- [x] 查询、统计、扫描、删除和一致性检查都只作用于 `c->db`，`SELECT` 后不跨 DB 聚合。
- [x] 返回 envelope 与 Redis 3 Codis 文档和 Go proxy/topom parser 预期兼容。
- [x] Tcl 覆盖正常路径、边界值和错误路径，`make codis-server-redis8` 继续通过。

**明确不做逐项核对**：
- [x] 未新增 `hash_slots`、`hashSlotType` 或 `dictCreate(&hashSlotType...)`。
- [x] 未注册 `SLOTSMGRT*`、`SLOTSRESTORE*` 或 `SLOTSRESTORE-ASYNC*` 命令。
- [x] 未修改 Go `pkg/` / `cmd/` 代码。
- [x] 未切换默认 `make` / `codis-server` 到 Redis 8。
- [x] 未修改 Redis Cluster `keyHashSlot()` 的 CRC16/16384 语义。
- [x] `SLOTSSCAN` 不接受 `MATCH` / `TYPE` 作为有效选项。

**关键决策落地**：
- [x] 决策 1：`kvstore` 是基础 slot 命令唯一 keyspace 来源，未恢复 Redis 3 平行 slot index。
- [x] 决策 2：`SLOTSSCAN` 走 per-slot scan，不做全 DB scan 后过滤。
- [x] 决策 3：`SLOTSDEL` 走 Redis 8 正常删除路径，保留 tag index、dirty、key modified 等副作用。
- [x] 决策 4：`SLOTSCHECK` 校验 Redis 8 当前真实不变量，而不是 Redis 3 双索引模型。
- [x] 决策 5：命令注册以 Redis 8 command JSON 为源，`commands.def` 只作为生成产物同步更新。

**挂载点反向核对（可卸载性）**：
- [x] 清单内挂载点已落地：三个 command JSON、`commands.def`、`server.h`、`slots.c`、`tests/unit/codis.tcl`。
- [x] 反向 grep：新增行没有越界命中 `hash_slots`、迁移命令、Go 代码、默认构建切换或 Redis Cluster slot hash 行为。
- [x] 拔除沙盘推演：删除三份 command JSON、对应 `commands.def` 条目、`server.h` 声明、`slots.c` 中三条命令和 Tcl 新用例后，系统回到前置 feature 的 `SLOTSHASHKEY` / `SLOTSINFO` 基线；不会留下独立运行时状态。

## 3. 验收场景核对

- [x] `make codis-server-redis8`：通过。
- [x] `./runtest --single unit/codis`：通过，包含 memory leak 检查。
- [x] `SLOTSHASHKEY alpha "{tag}:a" "{tag}:b" "{}abc"` 返回 `{362 899 899 0}`；`SLOTSHASHKEY` 无参数返回空 array。
- [x] `SLOTSINFO 899 1` / `SLOTSINFO 1023 999999` 返回 Redis 3 Codis 兼容格式；超大 count 截断而不是报错。
- [x] DB0/DB1 分别写入同 slot key 后，`SLOTSINFO` / `SLOTSSCAN` 只观察当前 DB。
- [x] `SLOTSSCAN <slot> 0 COUNT 2` 循环到 cursor `0` 后，收集到的 key 全部 hash 到指定 slot；空 slot 返回 cursor `0` 和空 key array。
- [x] `SLOTSSCAN` 对越界 slot、非法 cursor、`COUNT 0`、未知选项 `MATCH` 返回 Redis error reply。
- [x] `SLOTSDEL 899 362` 删除指定 slot 并保留其他 slot key；`SLOTSDEL 899 899` 返回重复结果且不报错。
- [x] DB1 执行 `SLOTSDEL 899` 不影响 DB0 同 slot key。
- [x] `SLOTSDEL` 后 `SLOTSCHECK` 和 `DEBUG CODIS-TAGINDEX-ASSERT` 均返回 OK。
- [x] 正常 keyspace 下 `SLOTSCHECK` 返回 OK。
- [x] 非 Codis mode 下 `SLOTSINFO` / `SLOTSSCAN` / `SLOTSDEL` / `SLOTSCHECK` 返回 `codis mode is disabled` 类错误；`SLOTSHASHKEY` 仍可做纯 hash 计算。
- [x] `COMMAND INFO SLOTSSCAN SLOTSDEL SLOTSCHECK` 可发现三条命令，且 `SLOTSDEL` 是 write 命令。

本 feature 无前端改动，不需要浏览器验证。

## 4. 术语一致性

- 基础 slot 命令：本阶段覆盖 `SLOTSHASHKEY`、`SLOTSINFO`、`SLOTSSCAN`、`SLOTSDEL`、`SLOTSCHECK`，与 design/checklist/测试命名一致。
- 当前 DB 语义：实现统一使用 `c->db`，测试覆盖 `SELECT 0` / `SELECT 1`。
- per-slot kvstore dict：实现通过 `kvstoreScan` 和 `kvstoreGetDict` 访问 Redis 8 `kvstore`，未引入 Redis 3 `hash_slots`。
- slot scan cursor：`SLOTSSCAN` 使用 Redis `SCAN` unsigned cursor 解析和返回，没有新增私有 cursor 编码。
- slot check：`SLOTSCHECK` 检查 key 所在 dict index 与 Codis hash slot，并复用 tag index assert。

## 5. 架构归并

对照方案第 4 节，已更新 `.codestable/architecture/ARCHITECTURE.md`：

- [x] 结构与交互：Redis 8 Codis mode 从内部 slot/tag keyspace core 推进到基础 slot 命令层。
- [x] 数据与状态：补入 `SLOTSSCAN` per-slot scan、`SLOTSDEL` 正常删除副作用路径、`SLOTSCHECK` kvstore/tag index 一致性检查。
- [x] 代码锚点：补入 `slotsscan.json`、`slotsdel.json`、`slotscheck.json`。
- [x] 边界约束：更新为基础 slot scan/delete/check 已完成，迁移协议、RDB fragment restore、Go 组件适配和默认产物切换仍属后续 roadmap。

未新增 architecture 子文档；当前能力仍属于 Redis 8 Codis Server 支线的 slot-keyspace-core 范围。

## 6. requirement 回写

- [x] 方案 frontmatter 指向 `requirement: redis-cluster-service`，该 req 当前为 `status: current`。
- [x] 本 feature 是 Redis 8 Codis Server 升级支线的命令兼容补齐，不改变业务开发者、运维者或值班人员的用户故事、pitch 或外部边界。
- [x] 结论：`.codestable/requirements/redis-cluster-service.md` 无需更新；能力承载形态已归并到 architecture 和 roadmap。

## 7. roadmap 回写

对照方案 frontmatter：

- `roadmap: redis8-upgrade`
- `roadmap_item: redis8-slot-basic-commands`

已完成回写：

- [x] `.codestable/roadmap/redis8-upgrade/redis8-upgrade-items.yaml`：对应 item 从 `in-progress` 改为 `done`，`feature` 保持 `2026-05-13-redis8-slot-basic-commands`。
- [x] `.codestable/roadmap/redis8-upgrade/redis8-upgrade-roadmap.md`：第 5 节子 feature 清单对应条目改为 `状态：done`，对应 feature 改为本 feature 目录名，并同步备注。
- [x] checklist 所有 `checks` 已按验收结果从 `pending` 更新为 `passed`。

## 8. attention.md 候选盘点

- [x] 无候选：本 feature 未引入新的环境前置、路径陷阱或必须每次提醒的特殊命令。Redis 8 独立构建入口、CodeStable YAML 校验和 `python3` 规则已在 `.codestable/attention.md` 中存在。

## 9. 遗留

- 后续 roadmap：`redis8-sync-migration-and-rdb-fragments` 继续负责同步迁移命令、`SLOTSRESTORE` 和 RDB fragment 边界。
- 后续 roadmap：`redis8-async-migration`、`redis8-go-component-adapters`、`redis8-build-config-packaging`、`redis8-validation-cutover` 仍按既定依赖推进。
- 维护提示：如果后续迁移协议主体继续扩大 `slots.c`，应在对应 feature 内重新评估是否拆出 Codis migration 专用文件；本 feature 不提前重构。
