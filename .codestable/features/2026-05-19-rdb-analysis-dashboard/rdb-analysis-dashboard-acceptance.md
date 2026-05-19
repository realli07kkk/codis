---
doc_type: feature-acceptance
feature: 2026-05-19-rdb-analysis-dashboard
status: ready-for-review
accepted_at: 2026-05-19
summary: RDB Analysis Dashboard 能力已按设计验收，架构与 requirement 已回写
tags: [dashboard, fe, rdb, observability, acceptance]
---

# RDB Analysis Dashboard 验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-05-19
> 关联方案 doc：`.codestable/features/2026-05-19-rdb-analysis-dashboard/rdb-analysis-dashboard-design.md`

## 1. 接口契约核对

对照方案第 2.1 节名词层逐一核查：

**接口示例逐项核对**：

- [x] `RDBAnalysisManager`：已落在 `pkg/topom/topom_rdb_analysis.go`，负责 job id、并发限流、workspace/upload 输入、生命周期、结果快照和过期清理。
- [x] `RDBAnalysisJob` snapshot：返回 `id/status/source/options/file_size/bytes_read/objects_read/db_count/total_size/type_summary/db_summary/top_big_keys/top_hot_keys/prefix_summary/flamegraph/error`；内部 `mu/cancel/path/cleanup` 不进入 JSON。
- [x] `RDBAnalysisOptions`：支持 `top_n/prefix_separators/max_depth/regex/include_expired`，并在 manager 内统一默认值和上限。
- [x] dashboard API：`upload/start/get/cancel/remove` 已挂到 `/api/topom/rdb-analysis`，handler 全部调用 `verifyXAuth`。
- [x] FE 展示模型：`cmd/fe/assets/rdb-analysis.js` 持有当前 job、options、上传文件、轮询 timer 和 flamegraph 行视图；设计稿已回填为“首版不维护历史 job 列表”。

**名词层“现状 → 变化”逐项核对**：

- [x] `Topom` 增加 `rdbAnalysis *RDBAnalysisManager`，初始化和 `Close` 释放均已接入。
- [x] `source` 只暴露 `upload:{filename}` 或 `workspace:{relative-path}`，不返回 dashboard 本地绝对路径。
- [x] `github.com/hdt3213/rdb` 只作为 Go 后端依赖接入；浏览器不解析 RDB。
- [x] flamegraph 数据由后端构建，FE 首版用树形表格展示，没有启动 `rdb` 独立 flamegraph web server。

**流程图核对**：

- [x] `FE 选择 product → upload/workspace start → dashboard xauth API → Topom job goroutine → rdb decoder → snapshot → FE poll` 均有代码落点。
- [x] `codis-fe` 仍只做静态资源和 reverse proxy；API 请求继续通过 `?forward={product}` 转到 dashboard。

未发现未处理的实现偏差。验收中发现的 FE state 文档偏差已回填设计稿。

## 2. 行为与决策核对

**需求摘要逐项验证**：

- [x] FE 页面有 RDB Analysis 区域，选中 product 后展示上传、workspace path、TopN、Depth、Sep、Regex、expired 控件。
- [x] dashboard API 可接收上传、创建任务、读取 snapshot、取消和删除任务。
- [x] 小 RDB 上传经 `codis-fe` reverse proxy 跑通，返回 job id，最终 snapshot 为 `done`，包含 `objects_read=2`、`total_size=144`、type summary、big keys 和 flamegraph。
- [x] 任务状态和结果只在 dashboard/topom 进程内保存，不写 coordinator。
- [x] `make gotest` 通过，原有 cmd/pkg 测试未因新增依赖和配置字段失败。

**明确不做逐项核对**：

- [x] 未修改 `pkg/proxy` 命令处理、router、backend 或 Redis 协议 mapper；反向 grep 无命中。
- [x] 未在 `cmd/fe/main.go` 实现后端分析任务；`codis-fe` 仍只负责静态资源和 reverse proxy。
- [x] 未向 coordinator 写入 RDB analysis job 或结果。
- [x] `go.mod` 依赖 `github.com/hdt3213/rdb v1.3.2`，没有 `/Users/liyiming/gitcode/rdb` 绝对路径 `replace`。
- [x] 未自动执行 `BGSAVE` / `SAVE`，未读取 Redis Server 机器任意文件，未实现 replication stream 抓取。
- [x] 未把完整 JSON/AOF/全量 key 列表返回浏览器；只返回 summary 和 top N。

**关键决策落地**：

- [x] D1：分析任务放在 dashboard/topom，不放在 `codis-fe` 或 proxy。
- [x] D2：live view 使用 FE 轮询，没有新增 WebSocket/SSE。
- [x] D3：直接使用 `parser.NewDecoder(file).Parse(callback)` 流式聚合，没有 shell 调 CLI。
- [x] D4：首版只支持上传和 dashboard workspace 内文件。
- [x] D5：结果只保留摘要和 top N，`max_top_n` 限制已接入。

**流程级约束核对**：

- [x] 鉴权：所有 RDB Analysis API 路由都有 `:xauth`，handler 全部 `verifyXAuth`；坏 xauth 经本地 e2e 返回 `HTTP_STATUS:800`。
- [x] 路径安全：workspace path 经过 `Abs`、`EvalSymlinks`、`Rel` 和越界判断，`TestRDBAnalysisManagerRejectsWorkspaceEscape` 覆盖。
- [x] 资源边界：上传大小、并发、保留 job 数和 top N 上限由 manager 控制；新增单测覆盖 oversized upload、concurrent limit、option cap。
- [x] 取消语义：API 和 manager 通过 `context.CancelFunc` 停止解析，`Remove/Close` 也会 cancel 并清理上传临时文件。
- [x] 并发：job snapshot 用 job 局部锁复制，不持有 topom 全局锁解析 RDB。
- [x] 错误语义：非法 RDB 进入 `error` 状态并保留可读错误，`TestRDBAnalysisManagerReportsInvalidRDB` 覆盖。
- [x] 可观测性：开始、完成、取消、失败日志记录 job id、source、文件大小、耗时和对象数。

**挂载点反向核对（可卸载性）**：

- [x] M1 `pkg/topom/topom_api.go`：新增 `/api/topom/rdb-analysis` route group。
- [x] M2 `pkg/topom/topom_rdb_analysis.go` / `_api.go` / `_test.go`：任务状态、解析聚合、API handler 和测试。
- [x] M3 `pkg/topom/config.go` / `config/dashboard.toml`：新增 workspace、上传大小、并发、保留数、top N 上限配置。
- [x] M4 `cmd/fe/assets/index.html` / `dashboard-fe.js` / `rdb-analysis.js`：新增浏览器入口和轮询展示。
- [x] M5 `go.mod` / `go.sum`：新增 `github.com/hdt3213/rdb v1.3.2`。
- [x] 反向 grep：`RDBAnalysis|rdb-analysis|rdb_analysis|hdt3213/rdb` 的生产命中都落在上述挂载点；`pkg/proxy`、`cmd/fe/main.go`、`cmd/admin`、`cmd/ha`、`pkg/models`、`extern` 无命中。
- [x] 拔除沙盘推演：删除 M1-M5 后，RDB Analysis 能力被移除，Codis 原有 dashboard/proxy/server 行为不应残留依赖。

## 3. 验收场景核对

- [x] **S1**：FE 选中 product 后上传小 RDB 并查看结果。
  - 证据来源：本地 `codis-dashboard` + `codis-fe` e2e；`POST /api/topom/rdb-analysis/upload/:xauth?...forward=codis-demo` 返回 `{"id":"1"}`，`GET` snapshot 返回 `status=done`、`objects_read=2`、`total_size=144`、type summary、big keys 和 flamegraph。
  - 结果：通过。小文件完成过快，未稳定肉眼观察 running，但状态机和轮询路径已由代码与 API 覆盖。
- [x] **S2**：workspace 内合法相对路径启动分析，响应不暴露绝对路径。
  - 证据来源：`TestRDBAnalysisManagerParsesWorkspaceFile`、`TestApiRDBAnalysis`。
  - 结果：通过，source 为 `workspace:api.rdb`。
- [x] **S3**：workspace 外路径不创建 job。
  - 证据来源：`TestRDBAnalysisManagerRejectsWorkspaceEscape`。
  - 结果：通过。
- [x] **S4**：上传超过 `max_upload_size` 返回错误并清理临时文件。
  - 证据来源：`TestRDBAnalysisManagerRejectsOversizedUpload`。
  - 结果：通过。
- [x] **S5**：并发超过 `max_concurrent_jobs` 返回明确错误。
  - 证据来源：`TestRDBAnalysisManagerRejectsConcurrentJobLimit`。
  - 结果：通过。
- [x] **S6**：非 RDB 文件进入 `error`，dashboard 不 panic。
  - 证据来源：`TestRDBAnalysisManagerReportsInvalidRDB`。
  - 结果：通过。
- [x] **S7**：取消任务走 cancel 语义，完成态 cancel/remove 幂等可用。
  - 证据来源：`TestApiRDBAnalysis` 调用 `CancelRDBAnalysis` 与 `RemoveRDBAnalysis`；代码审阅确认运行中 cancel 会触发 context。
  - 结果：通过。
- [x] **S8**：LFU hot key 排序；无 LFU 信息时 hot key 表为空且状态正常。
  - 证据来源：`TestRDBAnalysisTopHotKeysSortsByFrequency`；小 RDB e2e 无 LFU，结果正常完成。
  - 结果：通过。
- [x] **S9**：prefix separator 和 max depth 限制 prefix summary。
  - 证据来源：`TestRDBAnalysisOptionsAndPrefixHelpers`。
  - 结果：通过。
- [x] **S10**：切换 Codis product 时旧轮询停止，新 product 不复用旧结果。
  - 证据来源：`dashboard-fe.js` product reset 调用 `resetRDBAnalysis`，`rdb-analysis.js` 中 `stopPolling/clear` 清理 timer 和当前 job；浏览器选择 `codis-demo` 后 RDB Analysis 控件按当前 product 渲染。
  - 结果：通过。
- [x] **S11**：`go test ./pkg/topom -run RDBAnalysis`。
  - 证据来源：命令输出 `ok github.com/CodisLabs/codis/pkg/topom 0.886s`。
  - 结果：通过。
- [x] **S12**：`make gotest`。
  - 证据来源：全量 cmd/pkg 测试通过，`pkg/topom` 约 8.204s。
  - 结果：通过。

前端浏览器肉眼验证：

- [x] 使用 Microsoft Edge 打开 `http://127.0.0.1:29190/#codis-demo`，页面真实渲染出 `RDB Analysis` 区块、`Upload`、`Workspace`、文件选择、`TopN`、`Depth`、`Sep`、`Regex`、`expired` 控件。

执行过的验证命令：

```bash
node -c cmd/fe/assets/rdb-analysis.js && node -c cmd/fe/assets/dashboard-fe.js
go test ./pkg/topom -run RDBAnalysis
go vet ./pkg/topom
make gotest
```

## 4. 术语一致性

- `RDB Analysis`：架构、requirement、FE 标题和文档统一使用该能力名；代码导出类型使用 `RDBAnalysis*`。
- `Analysis job`：代码中对应 `RDBAnalysisJob`，状态值为 `queued/running/done/error/canceled`。
- `Input source`：实现为 `upload:` 和 `workspace:` 两类安全摘要。
- `Live view`：实现为 FE 轮询 job snapshot，不是 Redis keyspace 实时订阅。
- 防冲突：未把本功能写入 `pkg/proxy` 或 Redis Server RDB fragment/slot migration 语义；反向 grep 无冲突命中。

## 5. 架构归并

- [x] `.codestable/architecture/ARCHITECTURE.md`：已在术语中补充 `RDB Analysis` 定义。
- [x] `.codestable/architecture/ARCHITECTURE.md`：已在 dashboard API 结构中补充 `/api/topom/rdb-analysis` upload/start/get/cancel/remove。
- [x] `.codestable/architecture/ARCHITECTURE.md`：已在运维入口中补充 FE RDB Analysis 区域和 reverse proxy 访问方式。
- [x] `.codestable/architecture/ARCHITECTURE.md`：已在数据与状态中补充 `rdbAnalysis` manager、进程内 job registry、workspace 限制、任务不进 coordinator。
- [x] `.codestable/architecture/ARCHITECTURE.md`：已在代码锚点和已知约束中补充新增文件与首版边界。

归并后，只读 architecture 也能知道系统里存在该能力、它属于 dashboard/topom、通过 FE/API 交互、以及不做远端 RDB 抓取。

## 6. requirement 回写

- [x] 方案 frontmatter 指向 `requirement: redis-cluster-service`，该 req 当前为 `current`。
- [x] `.codestable/requirements/redis-cluster-service.md` 已更新 `last_reviewed: 2026-05-19`。
- [x] 用户故事已补充值班人员在 FE 上传或选择 RDB 文件并查看内存分析结果。
- [x] “怎么解决”已补充 dashboard/topom 进程内 RDB Analysis 任务能力。
- [x] “实现进展”已追加 2026-05-19 RDB Analysis 离线分析能力。
- [x] “边界”已补充只分析已有 RDB 文件、不是在线实时订阅、不远端生成/复制/读取 RDB、结果只通过 `xauth` 暴露、dashboard 重启后任务消失。

`VISION.md` 索引不需要变更：能力 slug、pitch、status 均未改变。

## 7. roadmap 回写

- [x] design frontmatter 的 `roadmap` 和 `roadmap_item` 均为空。

结论：本 feature 非 roadmap 起头，不需要更新 `.codestable/roadmap/*`。

## 8. attention.md 候选盘点

- 候选 1：本地联调 dashboard 监听端口不要用 `--host-admin`；该参数是对外声明的 admin 地址。要改 dashboard 监听地址，应在临时配置里设置 `admin_addr`。

本节只登记候选，不擅自写入 `attention.md`。

## 9. 遗留

- 后续优化点：`cmd/fe/assets/dashboard-fe.js` 仍是胖 controller，后续继续扩展 FE 页面时建议单独走 `cs-refactor` 拆功能文件边界。
- 后续优化点：`pkg/topom/topom_api.go` 已偏胖，后续 API 继续扩展时可整理 route registry、handler 和 `ApiClient` 边界。
- 已知限制：首版不自动从 Redis Server 生成或拉取 RDB，不提供导出完整 JSON/AOF，不把任务持久化到 coordinator，dashboard 重启后 job 消失。
- 已知限制：flamegraph 首版以树形表格展示，不接入 D3 flamegraph 交互图。
- 实现阶段顺手发现：`context` import 必须保留 `stdcontext` alias，因为 `pkg/topom/context.go` 已有包内 `context` 类型。

验收通过。实现与设计一致，架构与 requirement 已回写，checklist checks 已全部标记为 `passed`。等待用户终审确认。
