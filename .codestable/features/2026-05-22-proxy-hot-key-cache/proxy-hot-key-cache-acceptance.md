---
doc_type: feature-acceptance
feature: 2026-05-22-proxy-hot-key-cache
status: ready-for-review
accepted_at: 2026-05-22
summary: Codis Proxy Hot key cache 已按设计验收，包含本地短 TTL 缓存、写后本地失效，以及 source proxy -> dashboard/topom -> other proxies 的 best-effort 失效广播链路。
tags: [proxy, hot-key, cache, redis-protocol, broadcast, acceptance]
---

# proxy-hot-key-cache 验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-05-22
> 关联方案 doc：`.codestable/features/2026-05-22-proxy-hot-key-cache/proxy-hot-key-cache-design.md`

## 1. 接口契约核对

对照方案第 2.1 节名词层逐一核查。

**接口示例逐项核对**

- [x] proxy 配置契约：`pkg/proxy/config.go` 和 `config/proxy.toml` 已新增 `hot_key_cache_enabled`、`hot_key_cache_ttl`、`hot_key_cache_max_entries`、`hot_key_cache_max_value_size`、`hot_key_cache_keys`、`hot_key_cache_broadcast_enabled`、`hot_key_cache_broadcast_timeout`、`hot_key_cache_broadcast_queue_size`；默认关闭，启用时校验 TTL、entries、broadcast timeout 和 queue size。
- [x] `GET hot`：`pkg/proxy/session.go` 将 `GET` 分派到 `Router.handleRequestGet`；`pkg/proxy/hot_key_cache.go` 在 allowlist、slot stable、TTL 内 hit 时直接返回 copied bulk bytes，miss 时仍 dispatch 后端并在 response coalesce 阶段尝试 store。
- [x] `MGET hot cold hot2`：`Session.handleRequestMGetWithHotKeyCache` 对每个 key 独立 lookup，hit 直接填原数组位置，miss/不可缓存 key 走现有 subrequest dispatch，最终保持原始顺序 coalesce。
- [x] 写失效契约：`Router.handleRequestWithHotKeyCacheInvalidation` 包装 `MSET` / `DEL` / default 写路径，后端响应完成后再执行 invalidation plan；写失败不触发精确 key 广播。
- [x] source proxy 上报契约：`HotKeyCacheBroadcastRequest` 使用 `source_proxy_token`、`database`、`keys_base64`、`timeout_millis`；`keys_base64` 对应 Go `[][]byte`，JSON wire 上保留 Redis key 原始 bytes。
- [x] target proxy 失效契约：`HotKeyCacheInvalidationRequest` 只包含 `database` 和 `keys_base64`；目标 proxy 只做本地 allowlist 检查和 DB+key 删除，不访问后端 Redis。
- [x] stats 契约：`Proxy.Stats` 在 cache 开启或已有非零统计时输出 `hot_key_cache`，包含 entries/hits/misses/stores/invalidations/evictions/broadcast_attempts/broadcast_failures/broadcast_dropped/broadcast_coalesced/remote_invalidations；默认关闭且无统计时通过 `omitempty` 省略。

**名词层“现状 → 变化”逐项核对**

- [x] `Config`：从无 hot key cache 配置变为默认关闭、显式 exact key allowlist、本地资源上限和广播队列/timeout 的配置组。
- [x] `HotKeyCache`：新增 proxy 内部运行态，cache key 为 `database + raw Redis key bytes`，entry 保存 copied bulk bytes、slot、expireAt、LRU element；stats 覆盖读写失效和广播。
- [x] `Router`：额外持有 `slotVersions`、`hotKeyCache` 和 `hotKeyCacheBroadcast`；`FillSlot` 推进 slot version 并失效该 slot 本地条目。
- [x] dashboard/topom 广播契约：source 上报 API 挂在 `/api/topom/hot-key-cache/invalidate/:xauth`，target 失效 API 挂在 `/api/proxy/hot-key-cache/invalidate/:xauth`，两条 API 的 request/result 类型已拆分，字段语义不再混用。
- [x] `Request` 编排：不新增 Redis 客户端可见命令；只在现有 `GET` / `MGET` / 写命令分支挂入本地 cache lookup/store/invalidation。

**流程图核对**

- [x] `GET/MGET -> Session.handleRequest -> hot key cache enabled -> configured key and slot stable -> hit/miss -> dispatch/store/return` 均有代码落点。
- [x] `SET/MSET/DEL -> backend response completed -> local invalidation -> bounded queue -> dashboard/topom fan-out -> target proxy local delete` 均有代码落点。
- [x] `loopWriter preserves pipeline order` 未被绕过：cache hit 设置 `Request.Resp`，miss store 和 MGET 合并仍通过 `Request.Coalesce`。

结论：接口契约与设计一致，未发现需回改生产代码或方案的偏差。

## 2. 行为与决策核对

**需求摘要逐项验证**

- [x] 默认关闭时行为不变：`TestHotKeyCacheDisabledKeepsGetPathUnchanged` 覆盖连续 `GET` 仍访问 backend 两次；`TestHotKeyCacheStatsAreOmittedWhenDisabled` 覆盖 stats 默认不扩展 JSON 字段。
- [x] 启用并声明 hot key 后，第一次 `GET` miss 访问 backend，第二次 TTL 内 hit 不访问 backend：`TestHotKeyCacheGetHitMiss` 覆盖。
- [x] `MGET` 局部命中保持顺序且只转发 miss/不可缓存 key：`TestHotKeyCacheMGetPartialHit` 覆盖。
- [x] 同 proxy 写命令后本地 cache 失效：`TestHotKeyCacheSetInvalidatesLocalEntry`、`TestHotKeyCacheInvalidatesAfterWriteResponse`、`TestHotKeyCacheWriteInvalidationCommandGroups` 覆盖。
- [x] 启用写后广播时，source proxy 只上报可枚举 hot key，dashboard/topom 排除 source 后通知其他 online proxy：`TestHotKeyCacheBroadcastAfterWrite` 和 `TestHotKeyCacheInvalidateFansOutToOtherProxies` 覆盖。

**明确不做逐项核对**

- [x] 未新增 dashboard/FE hot key cache 管理页面：生产改动不触碰 `cmd/fe`、dashboard FE 资产或前端路由。
- [x] 未新增 coordinator schema 或 `models.Store` hot key cache 元数据：生产改动不触碰 `pkg/models`。
- [x] 未实现自动 per-key QPS 统计和热点晋升逻辑：cache key 来源只来自 `hot_key_cache_keys` exact allowlist。
- [x] 未缓存 hash/list/set/zset/stream/module value：store 只接受 Redis bulk bytes，测试覆盖 nil/error/oversize 不 store。
- [x] 未给 `GETRANGE`、`GETBIT`、`STRLEN` 等派生读命令增加 cache hit 成功路径：cache-aware 分支只挂在 `GET` / `MGET`。
- [x] 未新增 proxy-to-proxy 直接通信、pubsub、gossip 或 coordinator 持久化广播队列：广播只走 source proxy -> dashboard/topom -> target proxy admin API。
- [x] 未让 dashboard/topom 主动拉取或保存 Redis value：broadcast payload 只含 token、database、keys、timeout 等失效元数据。
- [x] 未让 `codis-admin` CLI 成为运行时广播链路的一环。

**关键决策落地**

- [x] D1 配置式 exact key：`HotKeyCache.keys` 从 `Config.HotKeyCacheKeys` 初始化，不做采样或自动晋升。
- [x] D2 cache 归属在 proxy/router：`Router` 持有 `hotKeyCache`、`slotVersions` 和 reporter，不写 coordinator。
- [x] D3 只缓存完整 string 读结果，写请求只失效不写穿：`hotKeyCacheStore` 只接受 non-nil bulk bytes；写路径只执行 invalidation plan。
- [x] D4 短 TTL + 本 proxy 写失效 + dashboard/topom best-effort 广播：写响应不等待其他 proxy 成功，失败只计数和日志。
- [x] D5 slot 迁移或映射变化绕过/失效：`hotKeyCacheSlotStable` 排除 locked/migrating/backend nil slot，`FillSlot` 触发 slot invalidation。
- [x] D6 dashboard/topom 协调广播：source proxy 不直接访问其他 proxy；topom fan-out 网络 IO 在锁外执行。

**编排层“现状 → 变化”逐项核对**

- [x] `Session.handleRequest` 新增 `GET` / `MGET` cache-aware 分支；关闭或不适用时回落现有 dispatch/MGET。
- [x] GET miss 和 MGET miss 的 cache store 都发生在 response coalesce 阶段，不改变后端响应错误语义。
- [x] MSET、DEL 和 default 写路径通过统一 wrapper 后置失效，避免写前失效导致旧 miss 回填。
- [x] source proxy 写后广播先进入有界队列，短窗口按 DB+key 合并；合并后仍按 `HotKeyCacheBroadcastMaxKeys` 二次分片上报。
- [x] topom 收到 source report 后，锁内只做 source 校验和 proxy registry snapshot；锁外并发调用 target proxy admin API，并对 request timeout 做 clamp。
- [x] `Proxy.Stats` 只在 `HotKeyCacheStats.Visible()` 为真时输出新增 stats 字段，降低默认关闭下的 JSON 契约扰动。

**流程级约束核对**

- [x] 错误语义：backend Redis error 原样返回且不缓存；内部 cache 不命中或不可用时退化为 dispatch。
- [x] 广播错误语义：source report 失败、target proxy 失败、timeout、队列满都不改变客户端成功写响应。
- [x] 并发：`HotKeyCache` 用 mutex 保护 entries/LRU，用 atomic stats/version；读 miss token 记录 slotVersion/cacheVersion，失效后旧 token 不能回填。
- [x] 顺序：没有绕过 `RequestChan` / `loopWriter`；响应合并仍由 `Coalesce` 执行。
- [x] 一致性：同 proxy 写请求后置失效；跨 proxy 广播 best-effort；直连后端写入仍靠 TTL 收敛。
- [x] 迁移：slot locked/migrating 不使用 cache；slot mapping 更新清理对应 slot cache。
- [x] 资源边界：entries、value size、TTL、broadcast queue size、单次 keys 上限、HTTP timeout 均有边界。
- [x] 可观测性：proxy stats JSON 暴露 cache 命中/失效、broadcast attempts/failures/dropped/coalesced 和 remote invalidations。

**挂载点反向核对（可卸载性）**

- [x] M1 `pkg/proxy/config.go` / `config/proxy.toml`：新增 hot key cache 与 broadcast 配置项、默认值和校验。
- [x] M2 `pkg/proxy/session.go`：新增 `GET` / `MGET` cache-aware 分支和写路径 invalidation wrapper。
- [x] M3 `pkg/proxy/router.go`：新增 `slotVersions`、`hotKeyCache`、`hotKeyCacheBroadcast` 初始化/关闭和 `FillSlot` 失效。
- [x] M4 `pkg/proxy/proxy.go`：新增 source token/xauth 注入、topom admin addr 注入、target proxy local invalidation 入口和 stats snapshot。
- [x] M5 `pkg/proxy/proxy_api.go`：新增 target proxy invalidation API、ApiClient 方法和新高频 API path redaction。
- [x] M6 `pkg/topom/topom_api.go` / `pkg/topom/topom_hot_key_cache.go`：新增 source report API、topom fan-out 编排、timeout clamp、source 校验和 target snapshot。
- [x] M7 `cmd/proxy/main.go`：proxy 通过 dashboard/coordinator 上线成功后记录 dashboard/topom admin addr；`--fillslots` 模式不设置。
- [x] M8 `pkg/utils/rpc/api.go`：新增不改变默认 RPC 行为的 `ApiPutJsonWithTimeout`。
- [x] 反向 grep：`HotKeyCache|hotKeyCache|hot_key_cache|HotKeyCacheBroadcast|HotKeyCacheInvalidation|keys_base64|InvalidateHotKeyCache|SetTopomAdminAddr|ApiPutJsonWithTimeout` 的生产命中均落在设计挂载点、主题实现文件、配置、文档和测试内。
- [x] 拔除沙盘推演：删除 `pkg/proxy/hot_key_cache.go`、`pkg/proxy/hot_key_cache_broadcast.go`、`pkg/topom/topom_hot_key_cache.go` 及对应测试，移除 Router/Proxy/API/config/main/rpc 挂载点后，生产引用可清空；不会残留 coordinator schema、dashboard FE、codis-admin CLI 或 proxy-to-proxy 通信。

## 3. 验收场景核对

- [x] **S1 默认配置下执行 GET/MGET**。
  - 证据来源：`TestHotKeyCacheDisabledKeepsGetPathUnchanged`、`TestHotKeyCacheStatsAreOmittedWhenDisabled`。
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
- [x] **S12 默认 `hot_key_cache_broadcast_enabled=false` 时不触发 dashboard/topom broadcast**。
  - 证据来源：默认配置和 `TestHotKeyCacheBroadcastRequiresTopomAddr` 覆盖 reporter 不可用时不产生 attempts/failures。
  - 结果：通过。
- [x] **S13 开启广播后 source proxy -> dashboard/topom -> other proxies fan-out**。
  - 证据来源：`TestHotKeyCacheBroadcastAfterWrite`、`TestHotKeyCacheInvalidateFansOutToOtherProxies`。
  - 结果：通过。
- [x] **S14 非 UTF-8 hot key 通过广播链路仍按原始 bytes 失效**。
  - 证据来源：`TestHotKeyCacheRemoteInvalidationAPI`、`TestHotKeyCacheBroadcastAfterWrite`、`TestHotKeyCacheInvalidateFansOutToOtherProxies` 使用 `0xff 'h' 'o' 't'`。
  - 结果：通过。
- [x] **S15 `MSET hot v hot2 v2 cold v3` 只广播 configured hot keys**。
  - 证据来源：`TestHotKeyCacheBroadcastAfterWrite` 断言 payload 只含 binary hot key 和 `hot2`。
  - 结果：通过。
- [x] **S16 后端返回错误的写请求不触发精确 key 广播**。
  - 证据来源：`TestHotKeyCacheBroadcastAfterWrite` 中后端 `SET` 返回 error 后 attempts 不增加。
  - 结果：通过。
- [x] **S17 短时间重复写同一 DB+key 或广播队列满**。
  - 证据来源：`TestHotKeyCacheBroadcastCoalescesDuplicateKeys`、`TestHotKeyCacheBroadcastDropsWhenQueueIsFull`。
  - 结果：通过。
- [x] **S18 coalesce 后仍遵守单次 max keys 上限**。
  - 证据来源：`TestHotKeyCacheBroadcastSplitsCoalescedKeys` 覆盖 `HotKeyCacheBroadcastMaxKeys+1` unique keys 拆成 2 次 report。
  - 结果：通过。
- [x] **S19 dashboard/topom 不可达、target timeout 或错误不改变 source/client 写响应**。
  - 证据来源：`TestHotKeyCacheInvalidateFansOutToOtherProxies` 覆盖单个 target 失败仍返回摘要；source reporter 异步 best-effort，失败只计数。
  - 结果：通过。
- [x] **S20 target proxy local invalidation 幂等**。
  - 证据来源：`TestHotKeyCacheRemoteInvalidationAPI` 覆盖 hot key 删除和 cold key 忽略；实现对 disabled/unconfigured/missing entry 返回成功。
  - 结果：通过。
- [x] **S21 `--fillslots` / topom addr 未知退化**。
  - 证据来源：`TestHotKeyCacheBroadcastRequiresTopomAddr`。
  - 结果：通过。
- [x] **S22 proxy stats 或 topom fan-out 摘要提供可观测证据**。
  - 证据来源：`HotKeyCacheStats` 字段、broadcast tests 对 attempts/failures/dropped/coalesced/remote invalidations 的断言，以及 topom fan-out result 的 total/failed/invalidated。
  - 结果：通过。
- [x] **S23 目标测试通过**。
  - 证据来源：本次验收命令见下。
  - 结果：通过。

已执行验证命令：

```bash
python3 .codestable/tools/validate-yaml.py --file .codestable/features/2026-05-22-proxy-hot-key-cache/proxy-hot-key-cache-checklist.yaml --yaml-only
python3 .codestable/tools/validate-yaml.py --file .codestable/features/2026-05-22-proxy-hot-key-cache/proxy-hot-key-cache-acceptance.md
python3 .codestable/tools/validate-yaml.py --file .codestable/features/2026-05-22-proxy-hot-key-cache/proxy-hot-key-cache-design.md
python3 .codestable/tools/validate-yaml.py --file .codestable/architecture/ARCHITECTURE.md
python3 .codestable/tools/validate-yaml.py --file .codestable/requirements/redis-cluster-service.md
python3 .codestable/tools/validate-yaml.py --file .codestable/requirements/VISION.md
go test ./pkg/proxy -run 'TestHotKeyCacheBroadcastSplitsCoalescedKeys|TestHotKeyCacheBroadcastCoalescesDuplicateKeys' -count=5
go test ./cmd/proxy ./pkg/proxy ./pkg/topom ./pkg/utils/rpc -count=1
git diff --check
```

## 4. 术语一致性

- [x] `Hot key cache`：设计、架构、requirement、用户文档和代码命名统一指 proxy 进程内短 TTL string 读缓存。
- [x] `HotKeyCache` / `hotKeyCache`：Go 类型、字段和 helper 命名集中在 `pkg/proxy`，未与 topom cache 或 Redis migration socket cache 混用。
- [x] `HotKeyCacheBroadcastRequest`：专指 source proxy -> dashboard/topom 上报，不再混用为 target proxy request。
- [x] `HotKeyCacheInvalidationRequest`：专指 dashboard/topom -> target proxy 本地失效，不包含 source token 或 timeout。
- [x] `keys_base64`：文档、request struct 和测试一致，表达 Redis key binary-safe wire payload。
- [x] `hot_key_cache_*`：TOML/JSON 配置项和 stats 字段使用 snake_case，与现有配置风格一致。
- [x] `Admin component`：文档明确指 dashboard/topom 管理面和 proxy admin HTTP API，不指 `codis-admin` CLI。
- [x] 防冲突：未新增 `topom.cache`、Redis Server migration cache、coordinator cache 相关概念或字段。

## 5. 架构归并

- [x] `.codestable/architecture/ARCHITECTURE.md`：已在术语中补入 Hot key cache 的当前形态，包含默认关闭、exact key、短 TTL、proxy-local 数据和 dashboard/topom best-effort 失效广播。
- [x] `.codestable/architecture/ARCHITECTURE.md`：已在命令路由中补入 `GET` / `MGET` 本地命中、miss 回退 router/forward、写后本地失效、source proxy 有界合并队列和 dashboard/topom 锁外 fan-out 主流程。
- [x] `.codestable/architecture/ARCHITECTURE.md`：已在管理 API 描述中补入 `/api/topom/hot-key-cache/invalidate/:xauth` 和 `/api/proxy/hot-key-cache/invalidate/:xauth`。
- [x] `.codestable/architecture/ARCHITECTURE.md`：已在 proxy 内存状态中补入 `Router.hotKeyCacheBroadcast`、binary-safe queue event、source token/topom addr/xauth/timeout ownership，明确不保存 Redis value。
- [x] `.codestable/architecture/ARCHITECTURE.md`：已在运行指标和已知约束中补入 broadcast attempts/failures/dropped/coalesced/remote_invalidations、opt-in bounded stale、一致性边界、slot 迁移绕过、队列满/失败降级和明确不做 proxy-to-proxy 通信。

## 6. requirement 回写

- [x] `requirement: redis-cluster-service` 指向 current requirement，本次新增用户可感能力边界，已执行 update。
- [x] `.codestable/requirements/redis-cluster-service.md`：已在“怎么解决”中补入 Hot key cache 写后广播可经 dashboard/topom 通知其他 online proxy 删除本地缓存。
- [x] `.codestable/requirements/redis-cluster-service.md`：已在“实现进展”记录 2026-05-22 Hot key cache 包含默认关闭、本地短 TTL 缓存、同 proxy 写后失效和 best-effort 跨 proxy 失效广播。
- [x] `.codestable/requirements/redis-cluster-service.md`：已在“边界”记录广播队列满、dashboard/topom 不可达、target timeout、直连后端写入等仍靠 TTL 收敛，不承诺跨 proxy 强一致。
- [x] `.codestable/requirements/VISION.md`：已刷新 `last_reviewed` 到 2026-05-22；req status/index 分组未变化。

## 7. roadmap 回写

- [x] 方案 frontmatter 没有 `roadmap` / `roadmap_item` 字段，本 feature 非 roadmap 起头；跳过 roadmap items.yaml 和 roadmap 主文档回写。

## 8. attention.md 候选盘点

- [x] 无候选：本 feature 未暴露每个后续 feature 都会重复踩的环境、工具、路径或命令陷阱。已有 `attention.md` 中“proxy/topology 改动先跑目标包测试再跑更大范围”的规则足够覆盖本次验证节奏。

## 9. 遗留

- 后续优化点：`HotKeyCache.version` 当前是全局失效版本，任意 key 失效都会阻止所有 in-flight miss 回填；这是保守一致性设计，会损失少量 hit rate，不会返回旧值。首版可接受，后续如果 hit rate 压力明确，可评估按 DB/key 细化 version。
- 后续优化点：`pkg/proxy/session.go` 和 `pkg/topom/topom_api.go` 仍偏胖。design 2.5 已判定本 feature 不做前置微重构；后续继续增加本地 Redis 命令或 dashboard 管理 API 业务域时，建议单独走 `cs-refactor`。
- 已知限制：默认关闭；只支持配置 allowlist 中 exact key 的 string `GET` / `MGET`；不做自动热点探测、不做 dashboard 管理页、不写 coordinator 元数据、不做 proxy-to-proxy 通信、不保证跨 proxy 强一致。
- 实现阶段顺手发现：新高频 endpoint 继续沿用既有 `:xauth` path 鉴权模式，但本 feature 已对 hot key cache invalidate path 做日志 redaction；彻底改变 xauth path 鉴权属于更大的管理 API 兼容改造，不纳入本 feature。
