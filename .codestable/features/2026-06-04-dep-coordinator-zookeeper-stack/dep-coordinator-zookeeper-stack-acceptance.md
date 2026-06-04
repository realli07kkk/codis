---
doc_type: feature-acceptance
feature: 2026-06-04-dep-coordinator-zookeeper-stack
status: accepted
accepted_at: 2026-06-04
summary: 验收 go-zookeeper 依赖升级和 Zookeeper coordinator 构建测试闭环
tags: [go, modules, dependency-upgrade, zookeeper, coordinator, acceptance]
---

# dep-coordinator-zookeeper-stack 验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-06-04
> 关联方案 doc：`.codestable/features/2026-06-04-dep-coordinator-zookeeper-stack/dep-coordinator-zookeeper-stack-design.md`

## 1. 接口契约核对

对照方案第 2.1 节名词层逐一核查。

**接口示例逐项核对**：

- [x] `go.mod` module diff 示例：`github.com/samuel/go-zookeeper` 已从 `v0.0.0-20161028232340-1d7be4effb13` 升级到 `v0.0.0-20201211165307-7117e9ea2414`。
  - 证据：`git diff -- go.mod go.sum` 只显示 `go.mod` 一行版本变化和 `go.sum` 两条目标 checksum。

**名词层“现状 → 变化”逐项核对**：

- [x] `module_set` 覆盖 `github.com/samuel/go-zookeeper`。
- [x] `github.com/samuel/go-zookeeper`：目标版本已落在 `go.mod` direct require，scope 未变化。
- [x] 目标版本是 `@latest` pseudo version，不是 tagged release。
- [x] `checksum lockfile`：`go.sum` 只新增目标 pseudo version content 和 go.mod checksum。
- [x] import surface：`pkg/models/zk/zkclient.go` 仍是唯一直接 import `github.com/samuel/go-zookeeper/zk` 的仓库代码。

**流程图核对**：

- [x] 图中节点均有实际落点：版本查询、策略分类、定点 `go get`、manifest diff、target test、默认 test、范围守护均已执行并有命令证据。

## 2. 行为与决策核对

**需求摘要逐项验证**：

- [x] 升级 go-zookeeper：`go.mod` 中 `github.com/samuel/go-zookeeper` 已为目标 pseudo version。
- [x] Zookeeper coordinator/Jodis 使用面没有可观察构建回归：`go test ./pkg/models/zk ./pkg/models ./cmd/dashboard ./cmd/proxy ./cmd/admin ./cmd/fe` 通过。
- [x] 默认构建测试闭环：`go test ./cmd/... ./pkg/...` 通过。

**明确不做逐项核对**：

- [x] 未修改 `pkg/models/zk` 源码：`git diff -- pkg/models/zk/zkclient.go` 无输出。
- [x] 未修改 `models.Client` 或 `models.NewClient`：`git diff -- pkg/models/client.go` 无输出。
- [x] 未修改 dashboard/proxy/admin/fe coordinator 参数或配置语义：diff 无 `cmd/`、`config/` 或 `doc/` 改动。
- [x] 未修改 etcd、filesystem、Consul coordinator 后端。
- [x] 未升级 Redis client、Martini、etcd、Consul、RDB analysis、metrics、jemalloc 或其他 roadmap 子 feature module。
- [x] 未修改 Go toolchain directive、jemalloc replace、`extern/redis-8.6.3/`、Docker、部署脚本、前端资源或配置模板。
- [x] 未生成 `vendor/`、`Godeps/` 或 `vendor/modules.txt`。

**关键决策落地**：

- [x] D1 升级到旧 module path 的 `@latest` pseudo version：落地为单条 `go.mod` 版本变更。
- [x] D2 不声称 tagged release：versions 查询没有 `Versions` 列表，报告按 pseudo version 表述。
- [x] D3 不改 Zookeeper coordinator 代码语义：无 Go 源码 diff。
- [x] D4 target 验证覆盖 Jodis/coordinator 编译面：目标测试已覆盖 `pkg/models/zk`、`pkg/models` 和相关 cmd 入口。
- [x] D5 不做全量 `go mod tidy`：diff 没有 require block 重排或无关 module churn。

**编排层“现状 → 变化”逐项核对**：

- [x] 版本查询 → 策略分类 → 定点升级 → diff 守护 → target test → 默认 test → 范围守护，执行顺序与 checklist 一致。
- [x] post-upgrade module graph 仍触达 `github.com/samuel/go-zookeeper/zk`，触达来自 `pkg/models/zk`。

**流程级约束核对**：

- [x] 错误语义：target test 和默认 test 均通过，无需回退 design。
- [x] 幂等/范围：重复验收后 `go.mod/go.sum` 未出现额外 churn。
- [x] 兼容性：无 coordinator 源码、cmd 参数、配置模板或运行期行为改动。
- [x] 可观测点：`go list`、`go mod why`、`go list -deps`、`git diff`、target test、默认 test、`git status` 均已核对。

**挂载点反向核对（可卸载性）**：

- [x] `go.mod` 中 `github.com/samuel/go-zookeeper` direct require：实际升级挂载点存在。
- [x] `go.sum` 中目标 checksum：lockfile 挂载点存在。
- [x] `pkg/models/zk/zkclient.go` 的 `github.com/samuel/go-zookeeper/zk` import：Zookeeper client 使用面挂载点存在。
- [x] target test gate：`go test ./pkg/models/zk ./pkg/models ./cmd/dashboard ./cmd/proxy ./cmd/admin ./cmd/fe` 已执行。
- [x] 默认 cmd/pkg test gate：`go test ./cmd/... ./pkg/...` 已执行。
- [x] 反向 grep：本 feature 的非文档代码变更只命中 `go.mod` / `go.sum`；没有清单外挂载点。
- [x] 拔除沙盘推演：回退 `go.mod` go-zookeeper 版本和删除新增两条 checksum 后，版本升级在系统视角消失。

## 3. 验收场景核对

- [x] **S1**：执行 `GOPROXY=https://proxy.golang.org,direct go list -m -u -json github.com/samuel/go-zookeeper`。
  - 证据来源：验收命令。
  - 结果：当前版本为 `v0.0.0-20161028232340-1d7be4effb13`，Update 为 `v0.0.0-20201211165307-7117e9ea2414`。

- [x] **S2**：执行 `go list -m -json github.com/samuel/go-zookeeper@latest`。
  - 证据来源：验收命令。
  - 结果：`@latest` 等于目标 pseudo version。

- [x] **S3**：执行 `go list -m -versions -json github.com/samuel/go-zookeeper`。
  - 证据来源：验收命令。
  - 结果：无 tagged `Versions` 列表；目标来自 `@latest` pseudo version。

- [x] **S4**：执行 `go mod why -m github.com/samuel/go-zookeeper`。
  - 证据来源：验收命令。
  - 结果：可追溯到 `pkg/models/zk -> github.com/samuel/go-zookeeper/zk`。

- [x] **S5**：执行 `go list -deps ./cmd/... ./pkg/...` 并 grep go-zookeeper。
  - 证据来源：验收命令。
  - 结果：默认 cmd/pkg 仍触达 `github.com/samuel/go-zookeeper/zk`。

- [x] **S6**：定点 `go get` 后检查 `go.mod`。
  - 证据来源：`git diff -- go.mod go.sum`。
  - 结果：只有 `github.com/samuel/go-zookeeper` 改到目标版本；`go 1.26.1` 和 jemalloc replace 不变。

- [x] **S7**：检查 `go.sum` diff。
  - 证据来源：`git diff -- go.mod go.sum`。
  - 结果：只新增目标 pseudo version content/go.mod checksum。

- [x] **S8**：执行 `go test ./pkg/models/zk ./pkg/models ./cmd/dashboard ./cmd/proxy ./cmd/admin ./cmd/fe`。
  - 证据来源：Go test。
  - 结果：通过。

- [x] **S9**：执行 `go test ./cmd/... ./pkg/...`。
  - 证据来源：Go test。
  - 结果：通过。

- [x] **S10**：重复验收后查看仓库状态。
  - 证据来源：`git status --short --untracked-files=all`、`find`。
  - 结果：无 `vendor/`、`Godeps/`、`vendor/modules.txt` 或仓库内临时构建产物；临时 detached worktree 已移除。

前端浏览器验证：不适用。本 feature 未改 FE 静态资源、HTML、JS、CSS 或可见 UI，只升级 Go coordinator 依赖并通过编译测试。

## 4. 术语一致性

- `Zookeeper coordinator stack`：代码和文档中均指 `github.com/samuel/go-zookeeper` 与 `pkg/models/zk` 使用面。
- `Target pseudo version`：实际目标为 `v0.0.0-20201211165307-7117e9ea2414`，未误写为 tagged release。
- `Minimal module diff`：实际符合，仅 `go.mod` 一行版本和 `go.sum` 两条 checksum。
- 防冲突：无新 coordinator 后端、新默认 coordinator、Zookeeper 迁移工具或 watch/ACL 新抽象命名进入代码 diff。

## 5. 架构归并

对照方案第 4 节，本 feature 不新增运行期能力，不改变 `Coordinator / Store` 抽象、Zookeeper 后端语义或 Go module manifest 契约，只维护 Zookeeper coordinator 依赖版本。

- [x] `.codestable/architecture/ARCHITECTURE.md`：不需要更新。理由：架构文档已记录 Coordinator / Store 支持 zookeeper 和 `go.mod/go.sum` 是 Go modules 入口；本次没有新模块、接口或跨模块纪律。
- [x] `.codestable/attention.md`：不需要更新。理由：本次没有暴露新的项目通用命令陷阱；既有“不要全量 go mod tidy”和 `python3` 约束已经覆盖。

## 6. requirement 回写

- [x] `requirement: null`，且方案明确为依赖维护 / 技术债，不新增用户可感能力。

结论：无 requirement 回写。

## 7. roadmap 回写

- [x] design frontmatter 包含 `roadmap: go-dependency-upgrade` 和 `roadmap_item: dep-coordinator-zookeeper-stack`。
- [x] `.codestable/roadmap/go-dependency-upgrade/go-dependency-upgrade-items.yaml` 中对应条目已从 `in-progress` 改为 `done`，feature 保持 `2026-06-04-dep-coordinator-zookeeper-stack`。
- [x] `.codestable/roadmap/go-dependency-upgrade/go-dependency-upgrade-roadmap.md` 第 5 节对应子 feature 已同步为 `done`，并写入 feature 目录名。
- [x] roadmap 主文档变更日志追加本 feature 完成记录。
- [x] items YAML 已通过 `python3 .codestable/tools/validate-yaml.py --file ... --yaml-only` 校验。

## 8. attention.md 候选盘点

- [x] 无候选。本 feature 未暴露需要补入 `.codestable/attention.md` 的新环境、工具或工作流约束。

## 9. 遗留

- 后续优化点：`doc/FAQ_zh.md` 中仍保留旧 Zookeeper session timeout 历史说明；本次不改用户文档，若要刷新运行手册应另走 guide/doc 工作流。
- 已知限制：本次只升级 Go client module，不验证真实外部 Zookeeper server 的运行期 watch/ACL/ephemeral 行为。
- 实现阶段顺手发现：无。
