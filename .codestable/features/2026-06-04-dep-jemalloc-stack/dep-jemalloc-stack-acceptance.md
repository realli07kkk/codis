---
doc_type: feature-acceptance
feature: 2026-06-04-dep-jemalloc-stack
status: accepted
accepted_at: 2026-06-04
summary: 验收 jemalloc-go require 升级与 third_party/jemalloc-go local replace 同步
tags: [go, modules, dependency-upgrade, jemalloc, cgo, acceptance]
---

# dep-jemalloc-stack 验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-06-04
> 关联方案 doc：`.codestable/features/2026-06-04-dep-jemalloc-stack/dep-jemalloc-stack-design.md`

## 1. 接口契约核对

对照方案第 2.1 节名词层逐一核查。

**接口示例逐项核对**：

- [x] `go.mod` jemalloc-go module diff 示例：`github.com/spinlock/jemalloc-go` 已从 `v0.0.0-20161230074307-26719b2ee618` 升级到 `v0.0.0-20201010032256-e81523fb8524`。
- [x] local replace 示例：`replace github.com/spinlock/jemalloc-go => ./third_party/jemalloc-go` 保留。

**名词层“现状 → 变化”逐项核对**：

- [x] `module_set` 覆盖 roadmap 中 `github.com/spinlock/jemalloc-go`。
- [x] `github.com/spinlock/jemalloc-go` scope 仍为 direct，target 为 upstream latest pseudo version。
- [x] `third_party/jemalloc-go` 已同步 upstream latest `jemalloc-5.2.1` 布局，并保留 local `go.mod`。
- [x] `pkg/utils/unsafe2` 的 jemalloc import surface 不变。
- [x] `go.sum` 未因本次操作产生变化。

**流程图核对**：

- [x] 图中节点均已执行：版本查询、local replace 触达分类、临时副本准备、`make config`、third_party 同步、临时产物清理、`cgo_jemalloc` 构建、默认测试和范围守护。

## 2. 行为与决策核对

**需求摘要逐项验证**：

- [x] `github.com/spinlock/jemalloc-go` require 已升级到 `v0.0.0-20201010032256-e81523fb8524`。
- [x] `third_party/jemalloc-go` 已同步为 upstream latest source layout，并保留 local module `go.mod`。
- [x] `pkg/utils/unsafe2` allocator 调用点没有源码改动。
- [x] `go build -tags cgo_jemalloc ./cmd/proxy` 通过。
- [x] `go test ./cmd/... ./pkg/...` 通过。

**明确不做逐项核对**：

- [x] 未修改 `pkg/utils/unsafe2/je_malloc.go` 或 `cgo_malloc.go` 的 allocator 调用语义。
- [x] 未改变 `cgo_jemalloc` build tag、Makefile proxy 构建目标、默认配置或运行期行为。
- [x] 未引入新的 allocator abstraction，未改 Redis/proxy 内存管理策略。
- [x] 未恢复旧 `vendor/` / `Godeps/`，未编辑旧 vendor 路径。
- [x] 未升级 Go toolchain，未改变 root `go 1.26.1` module directive。
- [x] 未运行全量 `go mod tidy`。
- [x] 未修改 `extern/redis-8.6.3/`、Docker、部署脚本、前端资源或配置模板。

**关键决策落地**：

- [x] D1 升级 jemalloc-go require：`go.mod` direct require 已改到 latest pseudo version。
- [x] D2 同步 `third_party/jemalloc-go`：actual module Dir 仍为 `./third_party/jemalloc-go`，源码已更新。
- [x] D3 采用 upstream latest `jemalloc-5.2.1` layout：本地目录包含 `jemalloc-5.2.1/`、顶层 `jemalloc` / `VERSION` / `je_*.c` relink 入口。
- [x] D4 恢复脚本执行位并运行 `make config`：generated headers 和 relink surface 已生成，且构建通过。

**编排层“现状 → 变化”逐项核对**：

- [x] 版本查询 → 触达分类 → 定点 `go get` → local replace 同步 → native build 验证 → 默认测试 → 范围守护，执行顺序与 checklist 一致。
- [x] post-upgrade module graph 显示 `github.com/spinlock/jemalloc-go v0.0.0-20201010032256-e81523fb8524 => ./third_party/jemalloc-go`。

**流程级约束核对**：

- [x] 错误语义：native build 和默认测试均通过，无需修改 Codis allocator 行为。
- [x] 幂等性：重复构建后未留下 `.o`、`.sym`、`config.log`、`proxy` 二进制或 vendor 目录。
- [x] 兼容性：`cgo_malloc` / `cgo_free` 调用语义、build tag、Makefile 目标和默认非 jemalloc 构建不变。
- [x] 可观测点：`go list`、`go mod why`、`go list -deps -tags cgo_jemalloc`、`git diff`、target build/tests、默认 tests、`git status` 均已核对。

**挂载点反向核对（可卸载性）**：

- [x] `go.mod` 中 jemalloc-go direct require：实际升级挂载点存在。
- [x] `go.mod` 中 local replace：仍指向 `./third_party/jemalloc-go`。
- [x] `third_party/jemalloc-go` local module contents：实际 native source、headers 和 relink surface 已更新。
- [x] `pkg/utils/unsafe2/je_malloc.go` import surface：使用面仍是既有挂载点，未新增代码引用。
- [x] `cgo_jemalloc` build gate：已执行并通过。
- [x] 反向核查：范围外 diff 为空；无清单外 Codis Go 源码改动。
- [x] 拔除沙盘推演：回退 `go.mod` require 并恢复旧 `third_party/jemalloc-go` 内容后，依赖升级在系统视角消失。

## 3. 验收场景核对

- [x] **S1**：`go list -m -u -json github.com/spinlock/jemalloc-go`。
  - 结果：当前旧 pseudo version，Update 为 `v0.0.0-20201010032256-e81523fb8524`。
- [x] **S2**：`go list -m -json github.com/spinlock/jemalloc-go@latest`。
  - 结果：`@latest` 等于 `v0.0.0-20201010032256-e81523fb8524`。
- [x] **S3**：`go mod why -m github.com/spinlock/jemalloc-go`。
  - 结果：依赖链路来自 `github.com/CodisLabs/codis/pkg/utils/unsafe2`。
- [x] **S4**：`go list -deps -tags cgo_jemalloc ./cmd/proxy`。
  - 结果：触达 `github.com/spinlock/jemalloc-go` 和 `pkg/utils/unsafe2`。
- [x] **S5**：定点 `go get` 后检查 `go.mod`。
  - 结果：jemalloc-go 改到 latest pseudo；replace、`go 1.26.1` 和其他 direct module 不变。
- [x] **S6**：检查 `third_party/jemalloc-go`。
  - 结果：包含 `jemalloc-5.2.1`、local `go.mod` 和顶层 relink symlink；未发现 `.o`、`.sym`、`config.log`、`config.status` 或 `autom4te.cache`。
- [x] **S7**：`go test ./pkg/utils/unsafe2 ./cmd/proxy`。
  - 结果：通过。
- [x] **S8**：`go build -tags cgo_jemalloc ./cmd/proxy`。
  - 结果：通过；根目录 `proxy` 临时二进制已清理。
- [x] **S9**：`go test ./cmd/... ./pkg/...`。
  - 结果：通过。
- [x] **S10**：重复验收后检查临时产物。
  - 结果：未出现 `vendor/`、`Godeps/`、`vendor/modules.txt`、根目录 `proxy` 或 third_party 构建中间件。

前端浏览器验证：不适用。本 feature 未改 FE 静态资源、HTML、JS、CSS 或可见 UI，只升级 Go native build 依赖并通过编译测试。

## 4. 术语一致性

- `Jemalloc stack`：文档中均指 `github.com/spinlock/jemalloc-go`。
- `local replace`：代码中仍由 root `go.mod` 的 replace 指向 `./third_party/jemalloc-go`。
- `upstream latest`：实现和验收均使用 `v0.0.0-20201010032256-e81523fb8524`。
- `generated relink surface`：实际保留顶层 `jemalloc`、`VERSION`、`je_*.c` symlink 和 generated headers。
- `cgo_jemalloc behavior unchanged`：`pkg/utils/unsafe2`、Makefile 和配置未改。

## 5. 架构归并

对照方案第 4 节，本 feature 不新增运行期能力，不改变 `cgo_jemalloc` 调用面或构建入口，只维护 local replace module 的版本和源码布局。

- [x] `.codestable/architecture/ARCHITECTURE.md`：不需要更新。理由：架构文档已记录 `third_party/jemalloc-go` 是 `cgo_jemalloc` local replace 模块；本次只同步版本，不改变系统级能力或交互。
- [x] `.codestable/attention.md`：不需要更新。理由：已有注意事项已覆盖“jemalloc 修改应改 `third_party/jemalloc-go`”和“不要全量 go mod tidy”；本次未新增通用项目陷阱。

## 6. requirement 回写

- [x] `requirement: null`，且方案明确为依赖维护 / 技术债，不新增用户可感能力。

结论：无 requirement 回写。

## 7. roadmap 回写

- [x] design frontmatter 包含 `roadmap: go-dependency-upgrade` 和 `roadmap_item: dep-jemalloc-stack`。
- [x] `.codestable/roadmap/go-dependency-upgrade/go-dependency-upgrade-items.yaml` 对应条目已从 `in-progress` 改为 `done`，feature 保持 `2026-06-04-dep-jemalloc-stack`。
- [x] `.codestable/roadmap/go-dependency-upgrade/go-dependency-upgrade-roadmap.md` 第 5 节对应子 feature 已同步为 `done`，并写入 feature 目录名。
- [x] roadmap 主文档变更日志追加本 feature 完成记录。

## 8. attention.md 候选盘点

- [x] 无候选。本 feature 未暴露需要补入 `.codestable/attention.md` 的新环境、工具或工作流约束。脚本执行位和 `make config` 已作为本 feature 的设计/验收细节记录，不是每个后续 feature 都会重复遇到的通用项目规则。

## 9. 遗留

- 后续优化点：如要减少 `third_party/jemalloc-go` vendored source 体积，应另起明确方案评估本地源码裁剪或改用系统 jemalloc 链接，不应在依赖升级条目里混做。
- 已知限制：本次只验证当前本机 `cgo_jemalloc` 构建，不等价于所有跨平台 C toolchain 验证。
- 实现阶段顺手发现：无。
