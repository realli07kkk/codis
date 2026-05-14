---
doc_type: roadmap
slug: redis8-upgrade
status: active
created: 2026-05-13
last_reviewed: 2026-05-14
tags: [redis, codis-server, migration, redis8]
related_requirements: [redis-cluster-service]
related_architecture: [system-overview]
---

# Redis 8 Codis Server 升级 Roadmap

## 1. 背景

当前 Codis Server 基于 Redis 3.2.11 源码加补丁构建，核心补丁包括 1024 slot 索引、slot 查询/删除命令、同步与异步迁移命令、RDB fragment restore、Codis 配置项和若干结构体字段。目标是把底层 Redis 源码升级到仓库内 `extern/redis-8.6.3/`，并参考 `redis-6.2.22/` 处理 Redis 6 到 Redis 8 之间的内部 API 断层。

这不是简单重放 patch。Redis 8 的主 keyspace 已从 Redis 3 的单 dict 变为 `kvstore`，并且 cluster 模式才启用多 slot dict；Codis 需要在不启用 Redis Cluster 协议的前提下，让 Redis 8 的 keyspace 按 Codis 1024 slot 分区，同时保持 proxy/topom 的现有 CRC32 路由和迁移协议兼容。

## 2. 范围与明确不做

### 本 roadmap 覆盖

- 把 Redis 8.6.3 接入 Codis Server 构建路径，形成可编译、可测试的升级分支。
- 引入 `codis-enabled` 模式，使 Redis 8 在 `cluster_enabled=0` 时仍按 1024 个 Codis slot 管理 keyspace。
- 将 Codis 的 slot 命令、tag index、同步迁移、异步迁移和 RDB fragment restore 移植到 Redis 8 API。
- 验证 Go proxy/topom/admin 对 Redis 8 Codis Server 的命令、INFO、CONFIG、AUTH、SELECT、SLOTSINFO 返回格式兼容性。
- 建立每个迁移步骤的 Redis Tcl 测试和最终端到端 cutover 验证。

### 明确不做

- 不实现 Redis Cluster 协议，不要求业务客户端使用 MOVED/ASK、cluster bus 或 Redis Cluster slot 语义。
- 不改变 Codis 对外固定 1024 slot 的模型，不调整 coordinator 中 slot 元数据格式。
- 不保证 Redis 8 生成的持久化 RDB/AOF 可降级回 Redis 3 Codis Server。
- 不默认保证 Redis 3 Codis Server 与 Redis 8 Codis Server 之间的 `SLOTSRESTORE` RDB fragment 可双向兼容；该兼容性需要单独验证并在灰度策略中明确。
- 不顺手升级 Go 组件依赖、不重构 proxy/topom 架构、不扩大 unsupported command 范围。

## 3. 模块拆分（概设）

```
redis8-upgrade
├── patch-inventory-build：补丁清单、文件级移植矩阵和最小构建接入
├── codis-mode-foundation：Redis 8 中的 Codis 模式、1024 slot kvstore 和 CRC32 slot 计算
├── slot-keyspace-core：slot 维度 key 生命周期、tag index 和基础 slot 命令
├── migration-protocol：同步/异步迁移、RDB fragment、restore auth 和 lazy release
├── go-component-adapters：proxy/topom/admin 与 Redis 8 Codis Server 的兼容验证
└── validation-cutover：系统级验证、性能基线、灰度和回滚策略
```

### patch-inventory-build · 补丁清单与构建接入

- **职责**：把 Redis 3 Codis patch 修改过的原始 Redis 文件逐项映射到 Redis 8 对应文件/API，并把 Redis 8 源码接入 Codis Server 最小编译链路。
- **承载的子 feature**：`redis8-patch-inventory-and-build-harness`
- **触碰的现有代码 / 模块**：根 `Makefile`、Redis 8 `src/Makefile`、Redis 8 command metadata、Redis 3 patch 文件、Codis Server 配置模板。

### codis-mode-foundation · Codis 模式基座

- **职责**：在 Redis 8 中增加 `codis_enabled` 第三态，使 `cluster_enabled=0` 时也能按 Codis 1024 slot 组织 keyspace，但不触发 Redis Cluster 协议行为。
- **承载的子 feature**：`redis8-codis-mode-foundation`
- **触碰的现有代码 / 模块**：`server.h`、`server.c`、`db.c`、`config.c`、`cluster.h`、`kvstore` 调用点、Redis Tcl test。

### slot-keyspace-core · Slot keyspace 与基础命令

- **职责**：基于 Redis 8 `kvstore` 主存储实现 Codis slot 维度统计、扫描、删除、校验和 hash 查询；补齐 Codis tag index。
- **承载的子 feature**：`redis8-slot-index-and-tag-index-core`、`redis8-slot-basic-commands`
- **触碰的现有代码 / 模块**：`db.c`、`expire.c`、`evict.c`、`object.c`、新移植的 `slots.c`、Redis command JSON / command table。

### migration-protocol · 迁移协议

- **职责**：移植 Codis 同步与异步迁移命令，处理 Redis 8 RDB fragment 格式、ACL/AUTH、lazy release 和迁移状态机。
- **承载的子 feature**：`redis8-sync-migration-and-rdb-fragments`、`redis8-async-migration`
- **触碰的现有代码 / 模块**：`slots.c`、`slots_async.c`、`rdb.c`、`networking.c`、`server.c`、`bio.c` / lazyfree 相关路径、Redis Tcl test。

### go-component-adapters · Go 组件适配

- **职责**：验证并必要时适配 Go proxy/topom/admin 与 Redis 8 Codis Server 的命令、INFO、CONFIG、AUTH、SELECT 和返回格式兼容性。
- **承载的子 feature**：`redis8-go-component-adapters`
- **触碰的现有代码 / 模块**：`pkg/utils/redis/client.go`、`pkg/topom/topom_slots.go`、`pkg/proxy/forward.go`、`pkg/proxy/session.go`、相关 Go 测试。

### validation-cutover · 验证与切换

- **职责**：把前面各模块的测试串成端到端升级验收，明确性能基线、灰度路径和回滚边界。
- **承载的子 feature**：`redis8-build-config-packaging`、`redis8-validation-cutover`
- **触碰的现有代码 / 模块**：构建脚本、配置模板、部署文档、测试脚本、Codis 集群演练环境。

## 4. 模块间接口契约 / 共享协议（架构层详设）

### 4.1 Codis 模式配置契约

**方向**：Codis Server 配置 → Redis 8 server 初始化

**形式**：Redis config + `redisServer` 全局状态

**契约**：

```c
/* server.h */
struct redisServer {
    int cluster_enabled;
    int codis_enabled;
};

/* config */
codis-enabled yes|no
```

**约束**：

- `codis_enabled=1` 与 `cluster_enabled=1` 互斥；启动期必须拒绝同时开启，不能静默选择一边。
- `codis_enabled=1` 不改变 Redis `dbnum` 为 1，必须保留 Codis 现有多 DB 行为和 `SELECT <db>` 兼容性。
- `codis_enabled=1` 不启用 Redis Cluster 协议，不产生 MOVED/ASK，不加入 cluster bus，不把 cluster-only 命令语义暴露给业务客户端。
- Redis 8 `CONFIG REWRITE` 必须能保留 `codis-enabled` 及其他 Codis 自定义配置。

### 4.2 Codis slot hash 契约

**方向**：proxy/topom slot 路由 ↔ Redis 8 Codis Server keyspace

**形式**：共享 hash 规则

**契约**：

```c
#define CODIS_SLOT_MASK_BITS 10
#define CODIS_SLOTS 1024
#define CODIS_SLOT_MASK 0x000003ff

uint32_t codisHashSlot(const char *key, size_t keylen);
/* 返回值必须等价于现有 Go proxy 的 CRC32 hash tag 规则：crc32(tag_or_key) & 0x3ff */
```

```go
// 现有 Go 侧契约保持不变
slotID := crc32.ChecksumIEEE(hashTagOrKey) % 1024
```

**约束**：

- 采用方案 A：Codis 模式下 Redis 8 `kvstore` 直接使用 1024 slot，`getKeySlot()` / `calculateKeySlot()` 返回 Codis CRC32 slot。
- 不维护 Redis 3 的平行 `dict *hash_slots[1024]` 二级索引；Redis 8 `kvstore` slot dict 是主 keyspace。
- `keyHashSlot()` 是 Redis 8 `cluster.h` 中的热路径 static inline。Codis 模式不能让 cluster、pubsub shard、evict、expire、random key 等调用点误认为自己仍在 16384 Redis Cluster slot 空间。
- 对所有 `server.cluster_enabled` 分支必须审计并分类：cluster-only、codis-or-cluster slot-aware、standalone-only。Codis 只进入第二类。

### 4.3 Redis 8 kvstore slot 契约

**方向**：Codis 模式基座 → Redis 8 key lifecycle

**形式**：`kvstore` 初始化与 per-slot 访问

**契约**：

```c
/* initServer() / initTempDb() */
int slot_count_bits = 0;
if (server.cluster_enabled) slot_count_bits = CLUSTER_SLOT_MASK_BITS; /* 14 */
else if (server.codis_enabled) slot_count_bits = CODIS_SLOT_MASK_BITS; /* 10 */

db->keys = kvstoreCreate(..., slot_count_bits, ...);
db->expires = kvstoreCreate(..., slot_count_bits, ...);
```

**约束**：

- `server.codis_enabled=1` 时，`keys`、`expires`、必要的临时 DB 都必须使用 1024 slot；不能只改主 DB。
- 需要验证 `kvstore` 内部 `dict_sizes` fenwick tree 在 `slot_count_bits=10` 时能正确支持公平随机 dict 选择，尤其是 `kvstoreGetFairRandomDictIndex` 相关路径。
- `SLOTSINFO`、`SLOTSSCAN`、`SLOTSDEL` 等命令必须通过 `kvstore` per-slot API 或等价安全迭代器访问 slot dict，不得重新引入并行 key 索引。

### 4.4 Tag index 契约

**方向**：Redis 8 key lifecycle → Codis tag migration commands

**形式**：`redisDb` 新增 tag index 状态

**契约**：

```c
typedef struct redisDb {
    kvstore *keys;
    kvstore *expires;
    zskiplist *codis_tagged_keys;
} redisDb;
```

**约束**：

- `codis_tagged_keys` 用于支撑 `SLOTSMGRTTAGONE` / `SLOTSMGRTTAGSLOT` 按 hash tag 批量迁移同一 tag 的 key。
- 必须在 Redis 8 key 增删生命周期中维护，至少覆盖 `dbAddInternal()`、`dbGenericDelete()`、覆盖 set/overwrite 场景的 `setKey` 相关路径、`emptyDb()`、`flushdb()`。
- RDB 加载、replica 全量同步载入数据后必须能重建 tag index，不能依赖运行期增量维护自然补齐。
- 每次 key 增删都会触发 tag index 维护，必须在测试和性能基线中评估额外开销。

### 4.5 Slot 命令返回协议

**方向**：Redis 8 Codis Server → Go proxy/topom/admin

**形式**：RESP 协议

**契约**：

```text
SLOTSHASHKEY key [key ...]
-> array<int>，每个元素为 0..1023 的 Codis slot id

SLOTSINFO [start] [count]
-> array<array<int,int>>，每项为 [slot_id, key_count]

SLOTSSCAN slot cursor [COUNT n]
-> [next_cursor, [key ...]]

SLOTSMGRTTAGSLOT host port timeout slot
-> [migrated_count, remaining_count]

SLOTSMGRTTAGSLOT-ASYNC host port timeout slot
-> [migrated_count, remaining_count]
```

**约束**：

- 返回格式必须保持现有 Go parser 兼容，尤其是 `pkg/utils/redis/client.go` 对 `SLOTSINFO` 和迁移返回数组的解析。
- 错误响应保持 Redis error reply，不新增 Go 侧难以分类的非标准 envelope。
- `SELECT <db>` 后的 slot 命令必须在当前 DB 上工作。

### 4.6 RDB fragment restore 契约

**方向**：源 Codis Server → 目标 Codis Server

**形式**：`SLOTSRESTORE` / `SLOTSRESTORE-ASYNC-*` 内部 RDB 序列化片段

**契约**：

```text
SLOTSRESTORE key ttlms serialized-value [key ttlms serialized-value ...]
SLOTSRESTORE-ASYNC-AUTH password
SLOTSRESTORE-ASYNC-SELECT db
SLOTSRESTORE-ASYNC-AUTH2 username password  # 如 Redis 8 ACL 需要
```

**约束**：

- Redis 8 ↔ Redis 8 Codis Server 必须支持当前 Redis 8 RDB fragment 编码。
- Redis 3 ↔ Redis 8 Codis Server 是否支持跨版本迁移必须作为灰度前置验证项；不把它和持久化 RDB/AOF 降级能力混为一谈。
- 如果 Redis 8 引入 Redis 3 不能识别的新 object encoding，跨版本迁移必须失败得可观测，不能静默丢 key 或错误写入。

### 4.7 Go 组件兼容契约

**方向**：Go proxy/topom/admin → Redis 8 Codis Server

**形式**：Redis 命令与 INFO/CONFIG 文本协议

**约束清单**：

- `SLAVEOF` vs `REPLICAOF`：现有 Go 代码发送 `SLAVEOF`，需验证 Redis 8 alias 行为和 TLS/端口处理。
- INFO 字段：验证 `master_link_status`、`loading`、`master_host`、`master_port` 字段名和解析行为。
- `CONFIG REWRITE`：验证 Redis 8 rewrite 不丢 `codis-enabled`、迁移配置和 Codis 自定义配置。
- `CLIENT KILL TYPE normal`：验证 Redis 8 TYPE 枚举兼容。
- ACL/AUTH：验证 `AUTH <password>` 对 default 用户的行为，以及 `SLOTSRESTORE-ASYNC-AUTH` 在 ACL 下的兼容路径。
- `SELECT <db>`：验证迁移流程中的多 DB select 行为保持兼容。
- `SLOTSINFO`：验证返回格式与 Go parser 完全一致。

## 5. 子 feature 清单

1. **redis8-patch-inventory-and-build-harness** — 建立 Redis 3 patch 到 Redis 8 的文件级移植矩阵，并接通 Redis 8 Codis Server 最小编译目标。
   - 所属模块：patch-inventory-build
   - 依赖：无
   - 状态：done
   - 对应 feature：2026-05-13-redis8-patch-inventory-and-build-harness
   - 备注：这一步的“可编译”只要求 slots/crc32 等目标文件可接入二进制；Codis 逻辑可以尚未触发。

2. **redis8-codis-mode-foundation** — 增加 `codis-enabled`、1024 slot `kvstore`、Codis CRC32 slot 计算和最小 Tcl smoke test。
   - 所属模块：codis-mode-foundation
   - 依赖：`redis8-patch-inventory-and-build-harness`
   - 状态：done
   - 对应 feature：2026-05-13-redis8-codis-mode-foundation
   - 备注：最小闭环已完成；Redis 8 Codis Server 可启动、写入 key、按 1024 slot 统计，并通过 `SLOTSHASHKEY` / `SLOTSINFO` 验证 slot 语义。

3. **redis8-slot-index-and-tag-index-core** — 基于 Redis 8 `kvstore` 主存储实现 Codis slot keyspace 能力，并补齐 `codis_tagged_keys` 生命周期维护。
   - 所属模块：slot-keyspace-core
   - 依赖：`redis8-codis-mode-foundation`
   - 状态：done
   - 对应 feature：2026-05-13-redis8-slot-index-and-tag-index-core
   - 备注：已基于 Redis 8 `kvstore` 收口 slot keyspace helper，并补齐 `codis_tagged_keys` 的 DB 生命周期、key add/delete、flush/lazyfree、RDB load 和 replica full sync rebuild 覆盖。

4. **redis8-slot-basic-commands** — 移植 `SLOTSHASHKEY`、`SLOTSINFO`、`SLOTSSCAN`、`SLOTSDEL`、`SLOTSCHECK` 并逐条补 Tcl 测试。
   - 所属模块：slot-keyspace-core
   - 依赖：`redis8-slot-index-and-tag-index-core`
   - 状态：done
   - 对应 feature：2026-05-13-redis8-slot-basic-commands
   - 备注：已补齐 Redis 8 Codis mode 基础 slot 命令与 Tcl 回归，返回格式保持现有 Go proxy/topom parser 兼容。

5. **redis8-sync-migration-and-rdb-fragments** — 移植同步迁移命令和 `SLOTSRESTORE`，验证 Redis 8 RDB fragment 与灰度跨版本迁移边界。
   - 所属模块：migration-protocol
   - 依赖：`redis8-slot-basic-commands`
   - 状态：done
   - 对应 feature：2026-05-13-redis8-sync-migration-and-rdb-fragments
   - 备注：已完成 Redis 8 ↔ Redis 8 同步迁移命令、`SLOTSRESTORE`、socket 缓存和 Tcl 回归；Redis 3 ↔ Redis 8 RDB fragment 双向兼容仍作为灰度验证边界，不等同于持久化 RDB/AOF 降级能力。

6. **redis8-async-migration** — 移植异步迁移、restore async、fence/cancel/status/auth，并评估 pthread lazy release 是否改接 Redis 8 `bio` / lazyfree。
   - 所属模块：migration-protocol
   - 依赖：`redis8-sync-migration-and-rdb-fragments`
   - 状态：done
   - 对应 feature：2026-05-14-redis8-async-migration
   - 备注：已完成 Redis 8 ↔ Redis 8 异步迁移、restore async ACK、AUTH/AUTH2/SELECT、fence/cancel/status、exec wrapper 写保护和 Tcl 回归；未新增 Codis 私有 pthread lazy release worker，无法安全拆分的新对象统一走 `object` payload 或失败保源。

7. **redis8-go-component-adapters** — 验证并适配 Go proxy/topom/admin 对 Redis 8 Codis Server 的命令、INFO、CONFIG、AUTH、SELECT 和返回格式兼容性。
   - 所属模块：go-component-adapters
   - 依赖：`redis8-slot-basic-commands`、`redis8-sync-migration-and-rdb-fragments`
   - 状态：planned
   - 对应 feature：未启动
   - 备注：覆盖 4.7 中 7 项兼容清单。

8. **redis8-build-config-packaging** — 完成 Redis 8 Codis Server 的正式构建、配置模板、命令 metadata 和打包切换。
   - 所属模块：validation-cutover
   - 依赖：`redis8-async-migration`、`redis8-go-component-adapters`
   - 状态：planned
   - 对应 feature：未启动
   - 备注：第 1 步已接最小构建；本条负责正式默认构建和发布包装。

9. **redis8-validation-cutover** — 完成端到端迁移演练、性能基线、灰度切换和回滚策略。
   - 所属模块：validation-cutover
   - 依赖：`redis8-build-config-packaging`
   - 状态：planned
   - 对应 feature：未启动
   - 备注：必须覆盖 proxy/topom/dashboard/admin 联调和 Redis Tcl + Go 测试组合。

**最小闭环**：第 2 条 `redis8-codis-mode-foundation` 完成后，可以启动 Redis 8 Codis Server，开启 `codis-enabled`，写入多个 key，观察 key 按 Codis CRC32 1024 slot 进入 `kvstore`，并通过 `SLOTSHASHKEY` / 基础 `SLOTSINFO` 验证 slot 语义。

## 6. 排期思路

先做 patch inventory 和构建接入，是为了避免后续移植长期处于“无法编译验证”的状态；但真正的技术最小闭环放在 `redis8-codis-mode-foundation`，因为 Redis 8 的 slot 分区、hash 规则和 cluster 分支审计是所有命令与迁移功能的前提。

中段先完成 slot keyspace 和基础命令，再做同步迁移和异步迁移。原因是迁移命令依赖 slot 统计、slot 扫描、tag index 和 RDB fragment restore；异步迁移又依赖同步迁移的协议和 object 序列化能力。Go 组件适配放在基础命令和同步迁移之后启动，因为 topom/proxy 的关键交互需要这些 Redis 命令先有稳定返回格式。

最后再做正式 packaging 和 cutover。第 1 步只负责最小编译入口，避免构建后置；第 8 步负责把 Redis 8 Codis Server 切成正式默认产物、配置模板和发布包装。

## 7. 观察项

- Redis 8 的 command metadata 由 JSON 生成 `commands.def`，移植 Codis 命令时应优先补 command JSON 和生成流程，不应长期手改生成物。
- Redis 8 `keyHashSlot()` 是 static inline 热路径，具体实现时可能需要新增 Codis 专用 hash helper，避免污染 Redis Cluster 的 16384 slot 语义。
- Redis 8 `kvstore` 的 fenwick tree 随 `slot_count_bits` 改变，需要在 `slot_count_bits=10` 下专门测随机 key、过期扫描和淘汰行为。
- Redis 8 license / 上游版本策略如果影响发布，需要另起决策文档记录；本 roadmap 只处理工程移植路径。
