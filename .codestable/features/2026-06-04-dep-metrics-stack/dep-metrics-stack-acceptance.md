---
doc_type: feature-acceptance
feature: 2026-06-04-dep-metrics-stack
status: accepted
accepted_at: 2026-06-04
summary: 验收 InfluxDB metrics client 升级到 v1.12.4，并确认 StatsD 已在最新版本
tags: [go, modules, dependency-upgrade, metrics, proxy, acceptance]
---

# dep-metrics-stack 验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-06-04
> 关联方案 doc：`.codestable/features/2026-06-04-dep-metrics-stack/dep-metrics-stack-design.md`

## 1. 接口契约核对

对照方案第 2.1 节名词层逐一核查。

**接口示例逐项核对**：

- [x] `go.mod` InfluxDB module diff 示例：`github.com/influxdata/influxdb` 已从旧 pseudo version 升级到 `v1.12.4`。
- [x] StatsD 保留示例：`gopkg.in/alexcesaro/statsd.v2` 仍为 `v2.0.0`。

**名词层“现状 → 变化”逐项核对**：

- [x] `module_set` 覆盖 roadmap 中 InfluxDB 和 StatsD metrics module。
- [x] InfluxDB direct module 已升级到 `v1.12.4`，scope 仍为 direct。
- [x] StatsD direct module 已确认当前即 `@latest v2.0.0`。
- [x] 新增 indirect require/checksum 均由 `influxdb@v1.12.4` parent graph 引入。
- [x] `pkg/proxy/metrics.go` 和 `metrics_report_*` 配置不变化。

**流程图核对**：

- [x] 图中节点均已执行：版本查询、触达分类、定点 `go get`、依赖图收口、target tests、默认 tests、范围守护。

## 2. 行为与决策核对

**需求摘要逐项验证**：

- [x] `github.com/influxdata/influxdb` 已升级到 `v1.12.4`。
- [x] `gopkg.in/alexcesaro/statsd.v2` 已确认 `v2.0.0` 无 Update。
- [x] proxy metrics 编译面通过：`go test ./pkg/proxy ./cmd/proxy ./cmd/dashboard ./cmd/admin ./cmd/fe` 通过。
- [x] 默认构建测试闭环通过：`go test ./cmd/... ./pkg/...` 通过。

**明确不做逐项核对**：

- [x] 未修改 `pkg/proxy/metrics.go`、`pkg/proxy/config.go` 或 `config/proxy.toml`。
- [x] 未改变 `metrics_report_*` 配置字段、默认值、校验规则、InfluxDB measurement/tags/fields 或 StatsD key 语义。
- [x] 未引入 InfluxDB v2 client/module，也未迁移指标系统。
- [x] 未升级 StatsD、RDB parser、coordinator、Redis client、Martini、jemalloc 或其他 roadmap 子 feature module。
- [x] 未升级 Go directive，未修改 jemalloc replace。
- [x] 未运行全量 `go mod tidy`，未生成 `vendor/`、`Godeps/` 或 `vendor/modules.txt`。
- [x] 未修改 `extern/redis-8.6.3/`、Docker、部署脚本、前端资源或配置模板。

**关键决策落地**：

- [x] D1 升级 InfluxDB 到 `v1.12.4`：`go.mod` direct require 已变更。
- [x] D2 保留 StatsD `v2.0.0`：版本查询显示已是 `@latest`。
- [x] D3 接受 parent-required indirect 增量：`go mod graph` 可追溯到 `influxdb@v1.12.4`。
- [x] D4 不改 metrics reporter 代码：源码/config diff 为空。

**编排层“现状 → 变化”逐项核对**：

- [x] 版本查询 → 触达分类 → 定点升级 → 依赖图收口 → target tests → 默认 tests → 范围守护，执行顺序与 checklist 一致。
- [x] post-upgrade package graph 仍触达 `github.com/influxdata/influxdb/client/v2` 和 `gopkg.in/alexcesaro/statsd.v2`。

**流程级约束核对**：

- [x] 错误语义：target/default tests 均通过，无需行为修复。
- [x] 幂等性：定点升级后重复测试没有继续产生源码或配置 diff。
- [x] 兼容性：metrics 配置、report period、measurement/tags/fields、StatsD key 和日志语义未改。
- [x] 可观测点：`go list`、`go mod why`、`go list -deps`、`go mod graph`、`git diff`、target tests、默认 tests、`git status` 均已核对。

**挂载点反向核对（可卸载性）**：

- [x] `go.mod` 中 `github.com/influxdata/influxdb v1.12.4` direct require：实际升级挂载点存在。
- [x] `go.mod/go.sum` 中 InfluxDB parent-required indirect/checksum：实际 graph 锁定挂载点存在。
- [x] `pkg/proxy/metrics.go` 的 metrics imports：使用面仍是既有挂载点，未新增代码引用。
- [x] target test gate 和默认 test gate 均已执行。
- [x] 反向 grep：本 feature 无新增代码引用；现有 metrics imports 全落在设计清单内。
- [x] 拔除沙盘推演：回退 `go.mod` InfluxDB 版本并删除本次新增 indirect/checksum 后，依赖升级在系统视角消失。

## 3. 验收场景核对

- [x] **S1**：`go list -m -u -json github.com/influxdata/influxdb`。
  - 结果：旧 pseudo version，Update 为 `v1.12.4`。
- [x] **S2**：`go list -m -json github.com/influxdata/influxdb@latest`。
  - 结果：`@latest` 等于 `v1.12.4`。
- [x] **S3**：StatsD `go list` 查询。
  - 结果：当前 `v2.0.0` 等于 `@latest`，无 Update。
- [x] **S4**：`go mod why -m` 覆盖 Metrics stack。
  - 结果：InfluxDB 和 StatsD 都可追溯到 `pkg/proxy`。
- [x] **S5**：`go list -deps ./cmd/... ./pkg/...` grep Metrics stack。
  - 结果：默认包图触达 InfluxDB `client/v2` 和 StatsD。
- [x] **S6**：定点 `go get` 后检查 `go.mod`。
  - 结果：InfluxDB 改到 `v1.12.4`；StatsD、`go 1.26.1` 和 jemalloc replace 不变。
- [x] **S7**：检查 `go.sum` 和 `go.mod` indirect diff。
  - 结果：新增内容可追溯到 `influxdb@v1.12.4` parent graph；无不相关 direct module 升级。
- [x] **S8**：`go test ./pkg/proxy ./cmd/proxy ./cmd/dashboard ./cmd/admin ./cmd/fe`。
  - 结果：通过。
- [x] **S9**：`go test ./cmd/... ./pkg/...`。
  - 结果：通过。
- [x] **S10**：`git status --short --untracked-files=all`、`find`、`git worktree list`。
  - 结果：未出现 `vendor/`、`Godeps/`、`vendor/modules.txt` 或临时 worktree。

前端浏览器验证：不适用。本 feature 未改 FE 静态资源、HTML、JS、CSS 或可见 UI，只升级 Go metrics 依赖并通过编译测试。

## 4. 术语一致性

- `Metrics stack`：文档中均指 InfluxDB 和 StatsD 两个 metrics module。
- `InfluxDB metrics client module`：实际升级的是 `github.com/influxdata/influxdb`，仓库 import 仍是 `client/v2`。
- `StatsD metrics client module`：仍是 `gopkg.in/alexcesaro/statsd.v2 v2.0.0`。
- `Parent-required indirect churn`：新增 indirect 经 `go mod graph` 可追溯到 `influxdb@v1.12.4`。
- `Metrics behavior unchanged`：源码/config diff 为空。

## 5. 架构归并

对照方案第 4 节，本 feature 不新增运行期能力，不改变 proxy metrics 上报路径、配置或指标契约，只维护 InfluxDB client module 版本。

- [x] `.codestable/architecture/ARCHITECTURE.md`：不需要更新。理由：架构文档已记录 proxy 可上报 JSON、InfluxDB、StatsD metrics；本次只是依赖版本升级。
- [x] `.codestable/attention.md`：不需要更新。理由：未暴露新的项目通用命令陷阱；既有 module/tidy 约束已覆盖。

## 6. requirement 回写

- [x] `requirement: null`，且方案明确为依赖维护 / 技术债，不新增用户可感能力。

结论：无 requirement 回写。

## 7. roadmap 回写

- [x] design frontmatter 包含 `roadmap: go-dependency-upgrade` 和 `roadmap_item: dep-metrics-stack`。
- [x] `.codestable/roadmap/go-dependency-upgrade/go-dependency-upgrade-items.yaml` 对应条目已从 `in-progress` 改为 `done`，feature 保持 `2026-06-04-dep-metrics-stack`。
- [x] `.codestable/roadmap/go-dependency-upgrade/go-dependency-upgrade-roadmap.md` 第 5 节对应子 feature 已同步为 `done`，并写入 feature 目录名。
- [x] roadmap 主文档变更日志追加本 feature 完成记录。

## 8. attention.md 候选盘点

- [x] 无候选。本 feature 未暴露需要补入 `.codestable/attention.md` 的新环境、工具或工作流约束。

## 9. 遗留

- 后续优化点：如要减少 InfluxDB module graph 的体积，应另起明确方案评估替换轻量 InfluxDB line protocol/client，而不是在依赖升级条目里改 reporter 实现。
- 已知限制：本次只做编译测试，不连接真实 InfluxDB/StatsD 服务验证运行期写入。
- 实现阶段顺手发现：无。
