---
doc_type: feature-acceptance
feature: 2026-05-19-rdb-remote-transfer-analysis
status: accepted
accepted_at: 2026-06-02
requirement: redis-cluster-service
summary: "RDB Analysis remote fetch 已按 Redis 8 HTTP export 能力重设计并落地，旧 tus/resume 方案已完全移除。"
tags:
  - dashboard
  - admin
  - rdb-analysis
  - redis8
  - http-export
---

# RDB Remote Transfer Analysis Acceptance

## 1. Interface Contract 核对

- 配置契约已落地：`pkg/topom/config.go:89` 到 `pkg/topom/config.go:92` 增加 `rdb_analysis_remote_fetch_enabled`、`rdb_analysis_remote_fetch_auth`、`rdb_analysis_remote_fetch_timeout`、`rdb_analysis_remote_fetch_max_concurrent`；`rdb_analysis_remote_fetch_auth` 使用 `json:"-"` 隐藏；默认配置在 `config/dashboard.toml:32` 到 `config/dashboard.toml:35`。
- 配置校验已落地：开启 remote fetch 但 auth 为空时，`pkg/topom/config.go:162` 到 `pkg/topom/config.go:164` 返回配置错误。
- Dashboard API 已落地：`PUT /api/topom/rdb-analysis/remote-fetch/:xauth` 挂在 `pkg/topom/topom_api.go:82` 到 `pkg/topom/topom_api.go:88`，请求体为 `server_addr` 和 `RDBAnalysisOptions`，定义在 `pkg/topom/topom_rdb_analysis_api.go:25` 到 `pkg/topom/topom_rdb_analysis_api.go:28`。
- Handler 编排已符合设计：`pkg/topom/topom_rdb_analysis_api.go:85` 到 `pkg/topom/topom_rdb_analysis_api.go:108` 先校验 xauth，再校验配置启用和持久化 auth，然后进入 Topom 层 allowlist 与下载。
- ApiClient 已落地：`pkg/topom/topom_rdb_analysis_api.go:179` 到 `pkg/topom/topom_rdb_analysis_api.go:186`。
- Redis HTTP export 调用契约已落地：`pkg/topom/topom_rdb_analysis_remote_fetch.go:23` 到 `pkg/topom/topom_rdb_analysis_remote_fetch.go:24` 固定 `/codis/rdb/latest` 和 `X-Codis-RDB-Auth`；`pkg/topom/topom_rdb_analysis_remote_fetch.go:105` 到 `pkg/topom/topom_rdb_analysis_remote_fetch.go:118` 只构造 `http://{server_addr}/codis/rdb/latest`。
- server allowlist 已落地：`pkg/topom/topom_rdb_analysis_remote_fetch.go:38` 到 `pkg/topom/topom_rdb_analysis_remote_fetch.go:58` 拒绝空值、scheme、path、query、fragment，并要求精确命中当前 product group server。
- 内部边界已收紧：外部 handler 只调用 Topom 层 `startRDBAnalysisRemoteFetch`，manager 的 remote fetch 入口只接收已验证的 `rdbAnalysisRemoteFetchTarget`，见 `pkg/topom/topom_rdb_analysis_remote_fetch.go:34` 到 `pkg/topom/topom_rdb_analysis_remote_fetch.go:69` 和 `pkg/topom/topom_rdb_analysis_remote_fetch.go:179` 到 `pkg/topom/topom_rdb_analysis_remote_fetch.go:194`。
- Job id 已按补充要求使用 UUID v7：`pkg/topom/topom_rdb_analysis.go:376` 到 `pkg/topom/topom_rdb_analysis.go:382` 调用 `github.com/google/uuid.NewV7()`；`go.mod:12` 引入 `github.com/google/uuid v1.5.0`。
- codis-admin 入口已落地：`cmd/admin/main.go:52` 增加命令用法，`cmd/admin/dashboard.go:87` 到 `cmd/admin/dashboard.go:88` 进入 handler，`cmd/admin/rdb_analysis_remote_fetch.go:15` 到 `cmd/admin/rdb_analysis_remote_fetch.go:27` 只接收 `--server` 和 analysis options，不接收 Redis export auth。
- FE 入口已落地：`cmd/fe/assets/index.html:140` 到 `cmd/fe/assets/index.html:154` 增加 `Remote` 按钮、`Redis server addr` 输入和页面内错误区域；`cmd/fe/assets/rdb-analysis.js:206` 到 `cmd/fe/assets/rdb-analysis.js:223` 调用 remote fetch API 并把失败写入 `rdb_error`。

## 2. Behavior 核对

- 旧 tus push/resume 方案已完全收掉：实现中没有新增 tus endpoint、Range request、resume state，也没有远端主动上传路径。
- 数据流符合重新设计后的 pull 模型：Dashboard 作为受控 caller，从当前 product Redis 8 server 拉取 `/codis/rdb/latest`，完整落本地临时文件后才创建 analysis job。
- 只允许当前 product model 中的 Redis server：allowlist 在 Topom 层封装，后续新增内部调用方也不能直接把裸 `server_addr` 交给 manager。
- export auth 使用持久化配置：auth 不在 API request、FE、job source 或 ApiClient 参数中出现，只在 dashboard 侧发往 Redis 的 header 中使用。
- 下载阶段不展示实时进度：符合设计取舍；下载成功后进入现有 job 进度，下载失败直接作为 API 错误返回，FE 可在 RDB Analysis 页面内展示。
- 不触发 SAVE、BGSAVE、replication stream，不修改 proxy 请求路径，不修改 Redis 8 RDB HTTP export C 代码。

## 3. 验收场景

- 默认关闭：`TestApiRDBAnalysisRemoteFetchDisabled` 覆盖 remote fetch disabled；旧 workspace/upload API 未改。
- 开启但 auth 为空：`TestRDBAnalysisRemoteFetchConfigRequiresAuthWhenEnabled` 覆盖配置校验失败。
- 错误 dashboard xauth：`TestApiRDBAnalysisRemoteFetchRequiresXAuthBeforeOutbound` 覆盖先拒绝 xauth 且无出站请求。
- 非当前 product server：`TestApiRDBAnalysisRemoteFetchRejectsServerOutsideProduct` 覆盖 allowlist 拒绝。
- scheme/path/query/fragment：`TestApiRDBAnalysisRemoteFetchRejectsInvalidServerAddr` 覆盖无任意 URL 和路径注入。
- Redis HTTP status 错误：`TestRDBAnalysisRemoteFetchRejectsHTTPError` 覆盖 403/404 类状态不创建 job。
- HTTP redirect：`TestRDBAnalysisRemoteFetchDoesNotFollowRedirect` 覆盖不跟随 redirect 且不向跳转地址发送 auth。
- Content-Length 超限：`TestRDBAnalysisRemoteFetchRejectsOversizedContentLength` 覆盖下载前拒绝。
- 无 Content-Length 但 body 超限：`TestRDBAnalysisRemoteFetchRejectsOversizedBody` 覆盖 copy 阶段拒绝并清理临时文件。
- client cancel/context cancel：`TestRDBAnalysisRemoteFetchHonorsContextCancel` 覆盖取消下载。
- 下载成功到临时文件：`TestRDBAnalysisRemoteFetchDownloadsToTempFile` 覆盖 header、source、size、临时文件内容。
- remote fetch 成功创建 job：`TestApiRDBAnalysisRemoteFetchStartsJob` 覆盖返回 job id 且可通过现有 job API 轮询。
- analysis 并发满：`TestApiRDBAnalysisRemoteFetchCleansUpWhenAnalysisLimitReached` 覆盖下载完成但 job 创建失败时清理临时文件。
- remote fetch 并发满：`TestApiRDBAnalysisRemoteFetchRejectsFetchConcurrencyLimit` 覆盖 `rdb_analysis_remote_fetch_max_concurrent`。
- UUID v7：RDB Analysis 测试中的 UUID 断言覆盖 workspace/upload/remote fetch 入口返回的 job id 可解析且 `Version()==7`。
- codis-admin options：`TestRDBAnalysisRemoteFetchOptions` 和 `TestRDBAnalysisRemoteFetchOptionsOmitAuth` 覆盖命令参数不包含 export auth。
- FE 可见性：用 Safari 打开临时本地 dashboard stub `http://127.0.0.1:18081/index.html#test`，Angular 正常展开 RDB Analysis，Accessibility tree 中可见 `Remote` 按钮和 `Redis server addr` 输入，且无 export auth 输入。
- FE 错误承载：`cmd/fe/assets/index.html:152` 到 `cmd/fe/assets/index.html:154` 提供 RDB Analysis 区域内错误显示；`cmd/fe/assets/rdb-analysis.js:100` 到 `cmd/fe/assets/rdb-analysis.js:115` 和 `cmd/fe/assets/rdb-analysis.js:221` 到 `cmd/fe/assets/rdb-analysis.js:223` 将 remote fetch 失败写入 `rdb_error`。

## 4. 术语和命名一致性

- `remote fetch`、`remote-http:<server>/<filename>`、`RDBAnalysisRemoteFetch*`、`rdb_analysis_remote_fetch_*` 命名在 config、API、manager、admin、FE 中一致。
- `server_addr` 被定义为当前 product group server 地址，不是 URL；代码中显式拒绝 scheme/path/query/fragment。
- `RDBAnalysisJob.ID` 被重新定义为 opaque string display id；生成函数名为 `newRDBAnalysisJobID`，实现使用 `uuid.NewV7()`。
- 代码检索未发现 `uuid.NewString()` 作为 RDB Analysis job id 生成入口。
- 代码检索未发现 tus 相关 endpoint、resume state 或 Range request 被引入。

## 5. Architecture 回写

- 已更新 `.codestable/architecture/ARCHITECTURE.md`：
  - 增加 `Remote RDB fetch analysis` 术语。
  - 更新 RDB Analysis 输入面：browser upload、workspace path、remote fetch。
  - 更新 Redis 8 RDB HTTP export 与 dashboard/topom 的受控消费关系。
  - 更新 Dashboard API、codis-admin、FE、state ownership、约束和代码锚点。
  - 明确 remote fetch 不做 tus/resume、任意 URL、SAVE/BGSAVE、replication stream、proxy 修改。

## 6. Requirement 回写

- 已更新 `.codestable/requirements/redis-cluster-service.md`：
  - 用户故事补入“从当前 product Redis 8 server 拉取当前 dbfilename RDB 并进入 RDB Analysis”。
  - 方案说明补入 Dashboard/FE/codis-admin remote fetch 能力。
  - 边界补入“只允许当前 product server 的固定 HTTP export，不支持远端主动上传、tus、Range、resume、任意 URL/path/query”。
  - 实现进展补入 2026-06-02 的 remote fetch analysis 落地状态。

## 7. Roadmap 回写

- 本 feature design frontmatter 没有 `roadmap` 或 `roadmap_item` 字段，不属于 roadmap 拆解项，未更新 `.codestable/roadmap/`。

## 8. Attention 候选

- 无需追加 `.codestable/attention.md`。本次新增的是 feature-specific 配置/API/边界，不是所有后续 CodeStable 工作流都必须预读的一句话项目陷阱。

## 9. 验证记录与残留风险

- 已通过：`go test ./pkg/topom -run 'RDB.*Remote'`
- 已通过：`go test ./pkg/topom -run RDBAnalysis`
- 已通过：`go test ./cmd/admin -run 'RDB.*Remote'`
- 已通过：`node --check cmd/fe/assets/rdb-analysis.js`
- 已通过：`make gotest`
- 已通过：`python3 .codestable/tools/validate-yaml.py --file .codestable/features/2026-05-19-rdb-remote-transfer-analysis/rdb-remote-transfer-analysis-design.md`
- 已通过：`python3 .codestable/tools/validate-yaml.py --file .codestable/features/2026-05-19-rdb-remote-transfer-analysis/rdb-remote-transfer-analysis-checklist.yaml --yaml-only`
- 已通过：`git diff --check`
- 浏览器验证：Browser 插件当前没有可用 `iab` 实例；已用 Safari + Computer Use 验证本地 dashboard stub 展开后的 RDB Analysis Remote 控件可见。Computer Use 点击 Remote 未触发可观察 PUT 请求，因此错误提示的视觉点击链路以代码锚点和 JS 静态检查作为证据。
- 已知产品限制：首版不支持下载阶段实时进度、不支持断点续传、不支持 Range/tus，不支持任意 URL，不触发 SAVE/BGSAVE，export auth 采用 dashboard 持久化单密钥配置。

结论：设计、实现、架构文档、需求文档和 checklist 已闭环；本 feature 通过 acceptance。
