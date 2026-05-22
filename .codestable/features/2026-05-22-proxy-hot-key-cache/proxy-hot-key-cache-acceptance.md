---
doc_type: feature-acceptance
feature: 2026-05-22-proxy-hot-key-cache
status: ready-for-review
accepted_at: 2026-05-22
summary: Codis Proxy Hot key cache 已按设计验收，写失效时序、架构文档和 requirement 已回写。
tags: [proxy, hot-key, cache, redis-protocol, acceptance]
---

# proxy-hot-key-cache 验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-05-22
> 关联方案 doc：`.codestable/features/2026-05-22-proxy-hot-key-cache/proxy-hot-key-cache-design.md`

## 1. 接口契约核对

对照方案第 2.1 节名词层逐一核查：

**接口示例逐项核对**：

- [x] proxy 配置契约：`config/proxy.toml` 和 `pkg/proxy/config.go` 已新增 `hot_key_cache_enabled`、`hot_key_cache_ttl`、`hot_key_cache_max_entries`、`hot_key_cache_max_value_size`、`hot_key_cache_keys`；默认关闭，启用时校验 TTL > 0、max entries > 0、value size 合法。
- [x] `GET hot`：`pkg/proxy/session.go` 将 `GET` 分派到 `Router.handleRequestGet`；`pkg/proxy/hot_key_cache.go` 在 allowlist、slot stable、TTL 内 hit 时直接返回 copied bulk bytes，miss 时仍 dispatch 后端并在 response coalesce 阶段尝试 store。
- [x] `MGET hot cold hot2`：`Session.handleRequestMGetWithHotKeyCache` 对每个 key 独立 lookup，hit 直接填原数组位置，miss/不可缓存 key 走现有 subrequest dispatch，最终保持原始顺序 coalesce。
- [x] 写失效契约：`Router.handleRequestWithHotKeyCacheInvalidation` 包装 `MSET` / `DEL` / default 写路径，先生成 invalidation plan，后端响应完成后再 `defer plan.apply()`；原 `Coalesce` 语义保留。
- [x] stats 契约：`Proxy.Stats` 在 cache 开启或已有非零统计时输出 `hot_key_cache`，默认关闭且无统计时通过 `omitempty` 省略新增字段。

**名词层“现状 → 变化”逐项核对**：

- [x] `Config`：从无 hot key cache 配置变为默认关闭、显式 exact key allowlist 的配置组，代码与 TOML 模板一致。
- [x] `HotKeyCache`：新增 proxy 内部运行态，cache key 为 `database + raw Redis key bytes`，entry 保存 copied bulk bytes、slot、expireAt、LRU element；stats 包含 hits/misses/stores/invalidations/evictions/entries。
- [x] `Router`：从只持有 slots/pools/config 变为额外持有 `slotVersions` 和 `hotKeyCache`；`FillSlot` 推进 slot version 并失效该 slot 本地条目。
- [x] `Request` 编排：不新增对外 Redis 协议命令；只在现有 `GET` / `MGET` / 写命令分支挂入本地 cache lookup/store/invalidation。

**流程图核对**：

- [x] `client GET/MGET → Session.handleRequest → hot key cache enabled? → configured key and slot stable? → hit/miss → dispatch/store/return` 均有代码落点。
- [x] `loopWriter preserves pipeline order` 未被绕过：cache hit 设置 `Request.Resp`，miss store 和 MGET 合并仍通过 `Request.Coalesce`。

未发现需要回改设计或生产代码的偏差。

## 2. 行为与决策核对

**需求摘要逐项验证**：

- [x] 默认关闭时行为不变：`TestHotKeyCacheDisabledKeepsGetPathUnchanged` 覆盖连续 `GET` 仍访问 backend 两次，`TestHotKeyCacheStatsAreOmittedWhenDisabled` 覆盖 stats 默认不扩展 JSON 字段。
- [x] 启用并声明 hot key 后，第一次 `GET` miss 访问 backend，第二次 TTL 内 hit 不访问 backend：`TestHotKeyCacheGetHitMiss` 覆盖。
- [x] `MGET` 局部命中保持顺序且只转发 miss/不可缓存 key：`TestHotKeyCacheMGetPartialHit` 覆盖。
- [x] 同 proxy 写命令后本地 cache 失效：`TestHotKeyCacheSetInvalidatesLocalEntry`、`TestHotKeyCacheInvalidatesAfterWriteResponse` 和 `TestHotKeyCacheWriteInvalidationCommandGroups` 覆盖。

**明确不做逐项核对**：

- [x] 未新增 dashboard/FE hot key cache 管理页面：本 feature 生产改动不触碰 `cmd/fe`、`cmd/dashboard` 或 dashboard API。
- [x] 未新增 coordinator schema 或 `models.Store` hot key cache 元数据：本 feature 不触碰 `pkg/models`。
- [x] 未实现自动 per-key QPS 统计和热点晋升逻辑：cache key 来源只来自 `hot_key_cache_keys` exact allowlist。
- [x] 未缓存 hash/list/set/zset/stream/module value：store 只接受 Redis bulk bytes，测试覆盖 nil/error/oversize 不 store。
- [x] 未给 `GETRANGE`、`GETBIT`、`STRLEN` 等派生读命令增加 cache hit 成功路径：cache-aware 分支只挂在 `GET` / `MGET`。
- [x] 未新增跨 proxy 失效广播或 pubsub 依赖：cache 只存在于 `Router.hotKeyCache` 进程内。

**关键决策落地**：

- [x] D1 配置式 exact key：`HotKeyCache.keys` 从 `Config.HotKeyCacheKeys` 初始化，不做采样或自动晋升。
- [x] D2 cache 归属在 proxy/router：`Router` 持有 `hotKeyCache` 和 `slotVersions`，不写 coordinator。
- [x] D3 只缓存完整 string 读结果，写请求只失效不写穿：`hotKeyCacheStore` 只接受 non-nil bulk bytes；写路径只执行 invalidation plan。
- [x] D4 短 TTL + 本 proxy 写失效：配置文档和用户文档均说明跨 proxy/直连后端只能 TTL 收敛。
- [x] D5 slot 迁移或映射变化绕过/失效：`hotKeyCacheSlotStable` 排除 locked/migrating/backend nil slot，`FillSlot` 触发 slot invalidation。

**编排层“现状 → 变化”逐项核对**：

- [x] `Session.handleRequest` 新增 `GET` / `MGET` cache-aware 分支；关闭或不适用时回落现有 dispatch/MGET。
- [x] GET miss 和 MGET miss 的 cache store 都发生在 response coalesce 阶段，不改变后端响应错误语义。
- [x] MSET、DEL 和 default 写路径通过统一 wrapper 后置失效，修正了写前失效导致旧 miss 回填的竞态。
- [x] `Proxy.Stats` 只在 `HotKeyCacheStats.Visible()` 为真时输出新增 stats 字段，降低默认关闭下的 JSON 契约扰动。

**流程级约束核对**：

- [x] 错误语义：backend Redis error 原样返回且不缓存；内部 cache 不命中或不可用时退化为 dispatch。
- [x] 并发：`HotKeyCache` 用 mutex 保护 entries/LRU，用 atomic stats/version；读 miss token 记录 slotVersion/cacheVersion，失效后旧 token 不能回填。
- [x] 顺序：没有绕过 `RequestChan` / `loopWriter`；响应合并仍由 `Coalesce` 执行。
- [x] 一致性：同 proxy 写请求后置失效；跨 proxy/直连后端写入不承诺实时失效。
- [x] 迁移：slot locked/migrating 不使用 cache；slot mapping 更新清理对应 slot cache。
- [x] 资源边界：entries、value size、TTL 均配置化，淘汰走 LRU。
- [x] 可观测性：proxy stats JSON 暴露 enabled、entries、hits、misses、stores、invalidations、evictions。

**挂载点反向核对（可卸载性）**：

- [x] M1 `pkg/proxy/config.go` / `config/proxy.toml`：新增配置项、默认值和校验。
- [x] M2 `pkg/proxy/session.go`：新增 `GET` / `MGET` cache-aware 分支和写路径 invalidation wrapper。
- [x] M3 `pkg/proxy/router.go`：新增 `slotVersions`、`hotKeyCache` 初始化和 `FillSlot` 失效。
- [x] M4 `pkg/proxy/proxy.go`：新增 `hot_key_cache` stats snapshot。
- [x] 反向 grep：`HotKeyCache|hotKeyCache|hot_key_cache|handleRequestGet|handleRequestMGetWithHotKeyCache|handleRequestWithHotKeyCacheInvalidation` 的生产命中集中在上述挂载点、`pkg/proxy/hot_key_cache.go`、配置和用户文档；测试命中在 `pkg/proxy/hot_key_cache_test.go`。
- [x] 拔除沙盘推演：删除 `pkg/proxy/hot_key_cache.go` / `hot_key_cache_test.go`，移除 `Router` 的 cache 字段/version、`Session.handleRequest` 的 cache-aware 分支、`Proxy.Stats` 字段和配置项后，生产引用可清空；不会残留 coordinator/dashboard/FE 元数据。

## 3. 验收场景核对

- [x] **S1 默认配置下执行 GET/MGET**。
  - 证据来源：`TestHotKeyCacheDisabledKeepsGetPathUnchanged`、`TestHotKeyCacheStatsAreOmittedWhenDisabled`、`go test ./pkg/proxy -run TestHotKeyCache -count=1`。
  - 结果：通过。
- [x] **S2 启用 cache 且 `hot_key_cache_keys=["hot"]`，连续两次 `GET hot`**。
  - 证据来源：`TestHotKeyCacheGetHitMiss`。
  - 结果：通过。
- [x] **S3 `GET cold` 不在 allowlist**。
  - 证据来源：`TestHotKeyCacheColdKeyBypassesCache`。
  - 结果：通过。
- [x] **S4 nil bulk、Redis error、超过 max value size 不 store**。
  - 证据来源：`TestHotKeyCacheDoesNotStoreUncacheableResponses`。
  - 结果：通过。
- [x] **S5 `MGET hot cold hot2` 混合 hit/miss**。
  - 证据来源：`TestHotKeyCacheMGetPartialHit`。
  - 结果：通过。
- [x] **S6 同 proxy 上执行 `SET` / `MSET` / `DEL` / `EXPIRE` / `PERSIST` 后再 `GET`**。
  - 证据来源：`TestHotKeyCacheSetInvalidatesLocalEntry`、`TestHotKeyCacheInvalidatesAfterWriteResponse`、`TestHotKeyCacheStaleMissTokenCannotStoreAfterInvalidation`、`TestHotKeyCacheWriteInvalidationCommandGroups`。
  - 结果：通过。
- [x] **S7 TTL 过期后再次 `GET` 访问 backend**。
  - 证据来源：`TestHotKeyCacheTTLAndEviction`。
  - 结果：通过。
- [x] **S8 entries 超过 max entries 发生淘汰**。
  - 证据来源：`TestHotKeyCacheTTLAndEviction`。
  - 结果：通过。
- [x] **S9 slot migrating/locked 或 `FillSlot` 更新后不从旧 cache 返回**。
  - 证据来源：`TestHotKeyCacheSlotInvalidationAndMayWriteClear`。
  - 结果：通过。
- [x] **S10 `EVAL` / 未知 may-write 保守清理当前 DB cache**。
  - 证据来源：`TestHotKeyCacheSlotInvalidationAndMayWriteClear`、`TestHotKeyCacheWriteInvalidationCommandGroups`。
  - 结果：通过。
- [x] **S11 proxy `/api/proxy/stats` 包含 hot key cache stats**。
  - 证据来源：`TestHotKeyCacheStatsAreExposedByProxyStats`。
  - 结果：通过。
- [x] **S12 目标测试通过**。
  - 证据来源：`go test ./pkg/proxy -run TestHotKeyCache -count=1`。
  - 结果：通过。

已执行或复核的验证命令：

```bash
go test ./pkg/proxy -run TestHotKeyCache -count=1
go test ./pkg/proxy -count=1
make gotest
python3 .codestable/tools/validate-yaml.py --file .codestable/features/2026-05-22-proxy-hot-key-cache/proxy-hot-key-cache-design.md
python3 .codestable/tools/validate-yaml.py --file .codestable/features/2026-05-22-proxy-hot-key-cache/proxy-hot-key-cache-checklist.yaml --yaml-only
git diff --check
```

## 4. 术语一致性

- [x] `Hot key cache`：设计、架构、requirement、用户文档和代码命名统一指 proxy 进程内短 TTL string 读缓存。
- [x] `HotKeyCache` / `hotKeyCache`：Go 类型、字段和 helper 命名集中在 `pkg/proxy`，未与 topom cache 或 Redis migration socket cache 混用。
- [x] `hot_key_cache_*`：TOML/JSON 配置项和 stats 字段使用 snake_case，与现有配置风格一致。
- [x] `Cacheable string read`：代码只挂 `GET` / `MGET`，用户文档明确不包含 `GETRANGE`、`GETBIT`、`STRLEN`、`TTL`、`PTTL`。
- [x] 防冲突：未新增 `topom.cache`、Redis Server migration cache、coordinator cache 相关概念或字段。

## 5. 架构归并

- [x] `.codestable/architecture/ARCHITECTURE.md`：已在术语中补入 Hot key cache，说明它是 proxy 进程内、默认关闭、exact key、短 TTL string 读缓存，不进入 coordinator。
- [x] `.codestable/architecture/ARCHITECTURE.md`：已在命令路由中补入 `GET` / `MGET` 本地命中、miss 回退 router/forward、写后失效 coalesce 收尾的主流程。
- [x] `.codestable/architecture/ARCHITECTURE.md`：已在 proxy 内存状态中补入 `Router.hotKeyCache`、`slotVersions`、cacheVersion、DB+key entry、LRU 和 stats ownership。
- [x] `.codestable/architecture/ARCHITECTURE.md`：已在运行指标和已知约束中补入 stats 输出、opt-in bounded stale、一致性边界、slot 迁移绕过和明确不做范围。

## 6. requirement 回写

- [x] `requirement: redis-cluster-service` 指向 current requirement，本次新增用户可感能力和边界，已执行 update。
- [x] `.codestable/requirements/redis-cluster-service.md`：已新增热点读业务用户故事。
- [x] `.codestable/requirements/redis-cluster-service.md`：已在“怎么解决”中补入 proxy 可选 Hot key cache 缓解少量 string 热 key 后端读放大。
- [x] `.codestable/requirements/redis-cluster-service.md`：已在“实现进展”记录 2026-05-22 Hot key cache 能力。
- [x] `.codestable/requirements/redis-cluster-service.md`：已在“边界”记录默认关闭、exact key、string GET/MGET、同 proxy 写失效、跨 proxy/直连后端仅 TTL 收敛。

## 7. roadmap 回写

- [x] 方案 frontmatter 没有 `roadmap` / `roadmap_item` 字段，本 feature 非 roadmap 起头；跳过 roadmap items.yaml 和 roadmap 主文档回写。

## 8. attention.md 候选盘点

- [x] 无候选：本 feature 未暴露每个后续 feature 都会重复踩的环境、工具、路径或命令陷阱。已有 `attention.md` 中“proxy/topology 改动先跑目标包测试再跑更大范围”的规则足够覆盖本次验证节奏。

## 9. 遗留

- 后续优化点：`HotKeyCache.version` 当前是全局失效版本，任意 key 失效都会阻止所有 in-flight miss 回填；这是保守一致性设计，会损失少量 hit rate，不会返回旧值。首版可接受，后续如果 hit rate 压力明确，可评估按 DB/key 细化 version。
- 已知限制：默认关闭；只支持配置 allowlist 中 exact key 的 string `GET` / `MGET`；不做自动热点探测、不做跨 proxy 失效广播、不保证直连后端写入后立即失效。
- 实现阶段顺手发现：`pkg/proxy/session.go` 继续偏胖。后续若继续增加 proxy 本地 Redis 命令，建议单独走 `cs-refactor` 拆本地命令处理，本 feature 不阻塞。
