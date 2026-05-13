---
doc_type: feature-acceptance
feature: 2026-05-13-redis8-codis-mode-foundation
status: current
accepted_at: 2026-05-13
summary: Redis 8 Codis mode 最小基座已完成，支持 codis-enabled、1024 slot kvstore、CRC32 slot 计算和 SLOTSHASHKEY/SLOTSINFO smoke test。
tags: [redis, codis-server, redis8, roadmap]
---

# redis8-codis-mode-foundation 验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-05-13
> 关联方案 doc：`.codestable/features/2026-05-13-redis8-codis-mode-foundation/redis8-codis-mode-foundation-design.md`

## 1. 接口契约核对

**接口示例逐项核对**：

- [x] `redis-server --codis-enabled yes --cluster-enabled no`：`tests/unit/codis.tcl` 实测 `CONFIG GET codis-enabled` 返回 `yes`，`cluster-enabled` 仍为 `no`。结果：一致。
- [x] `redis-server --codis-enabled yes --cluster-enabled yes`：`config.c` 启动 sanity 阶段返回 `codis-enabled and cluster-enabled are mutually exclusive`，Tcl 用例覆盖失败路径。结果：一致。
- [x] `SLOTSHASHKEY alpha {tag}:a {tag}:b {}abc`：Tcl 用例断言返回 `{362 899 899 0}`；`codisHashSlot()` 使用 CRC32 和 `CODIS_SLOT_MASK`。结果：一致。
- [x] `SLOTSINFO [start] [count]`：`slotsinfoCommand()` 返回当前 DB 的非空 `[slot, key_count]` 列表，`SLOTSINFO 899 1` 在 DB 0 / DB 1 分别按当前 DB 统计。结果：一致。

**名词层“现状 -> 变化”逐项核对**：

- [x] Codis mode 配置：`server.h` 新增 `server.codis_enabled`，`config.c` 新增 immutable bool config `codis-enabled`，默认 `no`，并拒绝和 `cluster-enabled` 同开。
- [x] Codis slot 计算：`CODIS_SLOT_MASK_BITS=10`、`CODIS_SLOTS=1024`、`CODIS_SLOT_MASK` 已落地；`codisHashSlot()` 为 `static inline`，空 hash tag `{}` 按 Codis 规则参与 CRC32。
- [x] Codis kvstore：`initServer()`、`initTempDb()`、`emptyDbAsync()` 和 `dbExpandGeneric()` 均识别 Codis 10-bit keyspace；`pubsubshard_channels` 不进入 Codis 1024 slot。
- [x] Foundation slot commands：新增 `SLOTSHASHKEY` / `SLOTSINFO` command JSON，并重新生成 `commands.def`。

**流程图核对**：

- [x] `load config -> 互斥检查 -> initServer db_slot_count_bits -> getKeySlot/calculateKeySlot -> codisHashSlot -> SLOTSHASHKEY/SLOTSINFO` 均有代码落点，grep 已确认。

## 2. 行为与决策核对

**需求摘要逐项验证**：

- [x] `codis-enabled yes` 可启动 Redis 8 standalone 协议 server，不启用 Redis Cluster。
- [x] 普通 key 写入后按 Codis CRC32 进入 1024 slot `kvstore`，`SLOTSINFO` 可观察 per-slot key count。
- [x] `SLOTSHASHKEY` 与 Go proxy `Hash()` 的 CRC32/hash tag 语义保持一致，含空 tag `{}` 行为。

**明确不做逐项核对**：

- [x] Diff 不包含 Go 源码改动；`cmd/`、`pkg/`、`go.mod`、`go.sum` 无 diff。
- [x] 未切换默认 `codis-server` / `build-all` 到 Redis 8；本 feature 只改 Redis 8 支线。
- [x] 未新增 `SLOTSSCAN`、`SLOTSDEL`、`SLOTSCHECK`、`SLOTSMGRT*`、`SLOTSRESTORE*` 实现。
- [x] 未新增 Redis Cluster `MOVED` / `ASK`、cluster bus 或 cluster state 初始化路径。
- [x] 未引入 Redis 3 `hash_slots[1024]` parallel index。

**关键决策落地**：

- [x] D1 独立 `codis_enabled`：配置与状态独立于 `cluster_enabled`，互斥而不复用 cluster。
- [x] D2 1024 slot `kvstore` 主存储：Codis mode 下 DB keyspace 使用 `CODIS_SLOT_MASK_BITS`，不维护平行索引。
- [x] D3 新增 `codisHashSlot()`：不修改 Redis Cluster `keyHashSlot()`，Codis 使用 CRC32/1024，Cluster 仍使用 CRC16/16384。
- [x] D4 最小观察面：只注册 `SLOTSHASHKEY` / `SLOTSINFO`，后续完整 slot command set 留给后续 feature。

**编排层“现状 -> 变化”逐项核对**：

- [x] 配置层：`codis-enabled` 注册和互斥检查落在 Redis config load sanity 阶段。
- [x] keyspace 初始化层：cluster、codis、standalone 三态分支清晰，DB slot bits 与 pubsub shard slot bits 分离。
- [x] slot 计算层：cluster -> `keyHashSlot()`，codis -> `codisHashSlot()`，standalone -> 0。
- [x] 命令层：`SLOTSHASHKEY` / `SLOTSINFO` 是只读快速观察命令，返回 RESP array。

**流程级约束核对**：

- [x] 错误语义：配置互斥启动期失败，不静默关闭任一方。
- [x] 幂等性：重复写入同一组 key，slot hash 和 `SLOTSINFO` 统计稳定。
- [x] 兼容性：默认 `codis-enabled no`，`SLOTSINFO` 在非 Codis mode 返回 `codis mode is disabled`。
- [x] 扩展点：后续 slot command 可继续扩展 `slots.c`，复用 Codis slot 常量和 hash helper。

**挂载点反向核对**：

- [x] 清单内挂载点均有实际落点：`server.h`、`config.c`、`server.c`、`db.c`、`lazyfree.c`、`slots.c`、command JSON、`commands.def`、`tests/unit/codis.tcl`。
- [x] 反向 grep：本 feature 新增的 `codis_enabled`、`codis-enabled`、`CODIS_SLOT_*`、`codisHashSlot`、`SLOTSHASHKEY`、`SLOTSINFO` 引用均落在清单内或生成的 `commands.def` 内。
- [x] 拔除沙盘推演：移除上述挂载点后，Redis 8 会回到 only build harness 状态；没有 Go proxy/topom、默认构建目标或 cluster 协议残留需要反向清理。

## 3. 验收场景核对

- [x] **S1**：执行 `make codis-server-redis8`。
  - 证据来源：命令执行。
  - 结果：通过，Redis 8 server 构建成功。
- [x] **S2**：以 `--codis-enabled yes` 启动 Redis 8。
  - 证据来源：`./runtest --single unit/codis`。
  - 结果：通过，`codis-enabled yes`，`cluster-enabled no`。
- [x] **S3**：同时指定 `--codis-enabled yes --cluster-enabled yes`。
  - 证据来源：`./runtest --single unit/codis`。
  - 结果：通过，启动失败并匹配互斥错误。
- [x] **S4**：执行 `SLOTSHASHKEY alpha {tag}:a {tag}:b {}abc`。
  - 证据来源：`./runtest --single unit/codis`。
  - 结果：通过，返回 `{362 899 899 0}`。
- [x] **S5**：DB 0 写入 `{tag}:a`、`{tag}:b` 后执行 `SLOTSINFO 899 1`。
  - 证据来源：`./runtest --single unit/codis`。
  - 结果：通过，返回 `{{899 2}}`。
- [x] **S6**：`SELECT 1` 后写入 `{tag}:c` 并执行 `SLOTSINFO 899 1`。
  - 证据来源：`./runtest --single unit/codis`。
  - 结果：通过，只统计当前 DB，返回 `{{899 1}}`。
- [x] **S7**：`FLUSHALL` / `FLUSHDB` 后继续写入 Codis tag key。
  - 证据来源：`./runtest --single unit/codis`；实现阶段已补 `emptyDbAsync()` Codis 10-bit 重建路径。
  - 结果：通过，无崩溃，slot 统计继续正确。
- [x] **S8**：默认不启用 Codis mode 启动。
  - 证据来源：`./runtest --single unit/codis`。
  - 结果：通过，`codis-enabled no`，`SLOTSINFO` 返回禁用错误。

## 4. 术语一致性

- `Codis mode`：代码中统一表现为 `server.codis_enabled` / `codis-enabled`，未误用 `cluster_enabled`。
- `Codis slot`：统一为 1024 slot，常量为 `CODIS_SLOT_MASK_BITS` / `CODIS_SLOTS` / `CODIS_SLOT_MASK`。
- `Codis hash tag`：`codisHashSlot()` 注释明确 `{}` 空 tag 与 Redis Cluster 的差异是刻意设计。
- `Codis kvstore`：DB keyspace、temp DB、async flush 重建均使用 10-bit slot。
- `Foundation slot commands`：只出现 `SLOTSHASHKEY` / `SLOTSINFO`，未提前引入后续命令。
- 防冲突：`SLOTSSCAN`、`SLOTSDEL`、`SLOTSCHECK`、`SLOTSMGRT`、`SLOTSRESTORE` 的新增实现无命中。

## 5. 架构归并

- [x] `.codestable/architecture/ARCHITECTURE.md`：已把 Redis 8 harness 现状从 stub 更新为 `codis-enabled yes` 最小运行模式。
- [x] `.codestable/architecture/ARCHITECTURE.md`：已补充 Redis 8 支线的 `codis_enabled` 状态、互斥配置、1024 slot keyspace 和多 DB 边界。
- [x] `.codestable/architecture/ARCHITECTURE.md`：已更新代码锚点，列出 Redis 8 Codis mode、观察命令和 Tcl smoke test 的主要承载文件。
- [x] `.codestable/attention.md`：无需补充。本 feature 未暴露新的通用环境/命令陷阱；Redis 8 路径和 `python3` 约束已在 attention 中存在。

## 6. requirement 回写

- [x] `requirement: redis-cluster-service` 当前为 `current`。
- [x] 本 feature 是 Redis 8 Codis Server 的底层实现基座，未改变业务用户故事、对外 pitch 或当前 proxy/topom 操作边界。

结论：`redis-cluster-service` 无需更新；本次能力变化归并到 architecture 和 roadmap。

## 7. roadmap 回写

- [x] `.codestable/roadmap/redis8-upgrade/redis8-upgrade-items.yaml`：`redis8-codis-mode-foundation` 已从 `in-progress` 改为 `done`，`feature` 保持 `2026-05-13-redis8-codis-mode-foundation`。
- [x] `.codestable/roadmap/redis8-upgrade/redis8-upgrade-roadmap.md`：第 5 节子 feature 清单同步为 `done` 并填入对应 feature。
- [x] YAML 校验通过。

## 8. attention.md 候选盘点

- [x] 本 feature 未暴露需要补入 `.codestable/attention.md` 的新候选。现有注意事项已覆盖 Redis 8 源码路径、`python3`、不误改默认 Go/Redis 构建路径。

## 9. 遗留

- 后续优化点：Codis mode 下 `c->slot` 多 key 缓存路径仍是后续多 key/路由能力的扩展点，当前 feature 不处理。
- 后续优化点：`redis8-slot-index-and-tag-index-core` 继续补 `codis_tagged_keys` 生命周期维护。
- 后续优化点：`redis8-slot-basic-commands` 继续移植 `SLOTSSCAN`、`SLOTSDEL`、`SLOTSCHECK` 等完整基础命令。
- 已知限制：Redis 8 Codis Server 仍不是默认 `codis-server`，正式 packaging 和 cutover 留给后续 roadmap item。
