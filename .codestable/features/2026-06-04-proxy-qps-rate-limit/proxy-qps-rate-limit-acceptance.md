# proxy-qps-rate-limit 验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-06-04
> 关联方案 doc：`.codestable/features/2026-06-04-proxy-qps-rate-limit/proxy-qps-rate-limit-design.md`
> 关联 checklist：`.codestable/features/2026-06-04-proxy-qps-rate-limit/proxy-qps-rate-limit-checklist.yaml`

## 1. 接口契约核对

对照方案第 2.1 节名词层逐一核查：

- [x] `proxy_qps_limit` 已加入 `pkg/proxy/config.go` 和 `config/proxy.toml`，默认 `0`，小于 `0` 返回 `invalid proxy_qps_limit`。
- [x] `ProxyQPSLimit` model 已落到 `pkg/models/proxy_qps_limit.go`，包含 `revision`、`limit`、`updated_at`；Store 提供 `/codis3/{product}/proxy_qps_limit` 的 load/update。
- [x] `QPSLimiter` 已落到 `pkg/proxy/qps_limiter.go`，保存 revision、limit、tokens、last refill、accepted、rejected，并提供 `Allow`、`SetLimit`、`Stats`、`ResetStats`。
- [x] proxy admin API 已新增 `PUT /api/proxy/qps-limit/:xauth`，`ApiClient.SetQPSLimit(revision, limit)` 可运行期应用配置。
- [x] dashboard/topom API 已新增 `GET/PUT /api/topom/proxy/qps-limit/:xauth`，view 返回 `revision`、`limit`、`enabled`、`sync_status`。
- [x] FE 已新增 `cmd/fe/assets/qps-limit.js` 和 `index.html` 配置区，选择 product 后加载，提交非负整数后刷新 view。

## 2. 行为与决策核对

- [x] 默认关闭：`proxy_qps_limit = 0` 时 limiter fast path 放行，不访问 coordinator、不发 HTTP、不累计 accepted，默认 stats 不暴露 `qps_limit`。
- [x] per-proxy 语义：limit 为正数时是单个 proxy 进程内所有 session 共享 token bucket，不跨 proxy 分布式协调。
- [x] 热路径边界：QPS 检查在 `Session.handleRequest` 中完成，通过后才进入本地命令、cache、stream、slot routing 或 backend 转发。
- [x] bypass：`AUTH` 和 `QUIT` 在 QPS 检查前处理；dashboard/admin HTTP API 不走 Redis 请求限流路径。
- [x] 拒绝语义：超限普通请求返回 `ERR proxy qps limit exceeded`，不访问后端 Redis；计入 request fails 和 limiter rejected，不计入后端 Redis error stats。
- [x] 动态配置：dashboard/topom 是统一目标配置源，写 coordinator revision 后 fan-out 到 online proxy；proxy reinit 会重放当前目标配置。
- [x] sync status：GET 使用目标 config 与 online proxy stats 中 applied revision/limit 比对，不依赖 topom 进程内失败缓存；stats 探测在 topom 全局锁外执行。
- [x] 范围守护：未开放 Redis protocol `CONFIG SET proxy_qps_limit`，也未开放完整 Redis `CONFIG` 命令族给业务客户端。

## 3. 验收场景核对

- [x] 默认行为、负数配置和 token bucket：`TestConfigRejectsNegativeProxyQPSLimit`、`TestQPSLimiterDisabledAllowsRequests`、`TestQPSLimiterAllowsAndRejectsByTokenBudget` 覆盖。
- [x] 降低阈值、关闭限流和 burst clamp：`TestQPSLimiterSetLimitClampsBurst`、`TestQPSLimiterSetLimitZeroDisables` 覆盖。
- [x] session 接入、拒绝错误、AUTH/QUIT bypass 和响应顺序：`TestSessionRejectsOrdinaryRequestWhenQPSLimitExceeded`、`TestSessionBypassesQPSLimitForAuthAndQuit` 覆盖。
- [x] proxy admin 动态应用：`TestProxySetQPSLimitAPI`、`TestProxySetQPSLimitAPIRejectsNegativeLimit` 覆盖正数、关闭和非法值。
- [x] stats 兼容：`TestProxyDefaultQPSLimitStatsHiddenAfterDisabledAllow` 覆盖默认隐藏；`TestProxyResetStatsClearsQPSLimitCounters` 覆盖 reset 清理 counters 且保留 revision/limit。
- [x] rejected 不污染 Redis error stats：`TestQPSRejectDoesNotCountRedisError` 覆盖 fails、rejected 和 `ops.redis.errors` 语义。
- [x] dashboard/topom model、store、API 和 revision：`TestProxyQPSLimitDefaultView`、`TestUpdateProxyQPSLimitStoresRevision`、`TestUpdateProxyQPSLimitRejectsNegative`、`TestApiProxyQPSLimit` 覆盖。
- [x] fan-out partial failure 和真实 sync status：`TestUpdateProxyQPSLimitProxySyncFailureRecordsAllFailedTokens`、`TestProxyQPSLimitStatusAfterTopomRestartDoesNotAssumeReady` 覆盖失败 token 和 topom 重启后不假报 ready。
- [x] 锁外 stats 探测：`TestGetProxyQPSLimitStatsProbeDoesNotHoldTopomLock` 覆盖慢 proxy stats 不阻塞轻量 topom 操作。
- [x] proxy reinit replay：`TestReinitProxyReplaysProxyQPSLimit` 覆盖新 proxy/reinit 后 stats 显示目标 revision/limit，并可动态关闭。
- [x] FE 静态验证：`node --check cmd/fe/assets/qps-limit.js`、`node --check cmd/fe/assets/dashboard-fe.js` 通过；本地 `codis-fe` 静态 server 返回 `Proxy QPS Limit` 区域和 `qps-limit.js` 资源。
- [x] 回归命令：`go test ./pkg/proxy ./pkg/topom ./cmd/fe`、checklist YAML 校验和 `git diff --check` 通过。

## 4. 术语一致性

- `Proxy QPS limit`：代码、FE、文档统一表示 codis-proxy 进程级普通请求限流。
- `qps_limit`：统一作为 JSON/stats/FE model 字段；本地 TOML 使用 `proxy_qps_limit`。
- `ProxyQPSLimit`：统一作为 coordinator/topom 目标配置模型，不表示 proxy 本地 token bucket。
- `QPSLimiter`：统一作为 proxy 运行态 token bucket。
- `sync_status`：统一表示 dashboard/topom 基于目标 revision 和 online proxy applied stats 的同步观测，不是分布式事务证明。
- `Redis protocol CONFIG SET`：保持未开放；动态修改统一走 dashboard/topom API 与 proxy admin API。

## 5. 架构归并

- [x] `.codestable/architecture/ARCHITECTURE.md` 已更新 `rate-limit` tag。
- [x] 术语归并：新增 Proxy QPS limit，明确 per-proxy、default-disabled、dashboard-managed 和 Redis protocol `CONFIG` 边界。
- [x] 流程归并：补入 session 请求入口、proxy admin API、dashboard/topom GET/PUT、coordinator revision、fan-out、reinit replay 和 FE 配置入口。
- [x] 状态归并：补入 `/codis3/{product}/proxy_qps_limit` 目标状态、proxy 本地 `QPSLimiter` runtime 状态，以及基于 proxy stats 的 `sync_status`。
- [x] 代码锚点归并：补入 `pkg/proxy/qps_limiter.go`、`pkg/topom/topom_qps_limit.go`、`pkg/models/proxy_qps_limit.go`、FE `qps-limit.js`。
- [x] 约束归并：补入默认 0、不做跨 proxy 分布式限流、不做细粒度限流、不开放 Redis protocol `CONFIG` 管理入口、rejected 不代表后端 Redis error。

## 6. requirement 回写

- [x] `.codestable/requirements/redis-cluster-service.md` 已更新 `rate-limit` tag 和平台维护者用户故事。
- [x] “怎么解决”已补入 dashboard/FE 管理 Proxy QPS limit、coordinator revision、fan-out、proxy token bucket 和拒绝语义。
- [x] “实现进展”已新增 2026-06-04 QPS limit 记录，覆盖默认关闭、API 管理、reinit replay、AUTH/QUIT bypass 和 CONFIG 边界。
- [x] “边界”已补入 per-proxy、非分布式、非细粒度、不持久化 token 状态和不开放 Redis protocol `CONFIG` 管理入口。

## 7. roadmap 回写

- [x] 方案 frontmatter 没有 `roadmap` / `roadmap_item` 字段，本 feature 非 roadmap 拆分条目，跳过 roadmap 回写。

## 8. attention.md 候选盘点

- [x] 本 feature 未暴露需要补入 `.codestable/attention.md` 的项目级固定注意事项。CR-001 的经验更适合沉淀为 learning：dashboard 展示 rollout 状态时，不能用易丢的进程内失败列表作为唯一 source of truth。

## 9. 遗留

- FE 交互已完成静态资源和语法验证；本环境没有可用 in-app Browser 工具，且 Playwright 未安装，因此未做真实浏览器提交表单 E2E。提交语义由 FE 控制器代码、dashboard/topom API 测试和本地 `codis-fe` 静态 HTTP 资源验证覆盖。
- 首版按设计不实现跨 proxy 分布式 token bucket、自动调参、细粒度限流或 Redis protocol `CONFIG SET proxy_qps_limit` 例外。
- 后续若继续新增 dashboard-managed runtime config，建议先沉淀 shared pattern，避免每个 feature 重复处理 model/store/fan-out/sync status。
