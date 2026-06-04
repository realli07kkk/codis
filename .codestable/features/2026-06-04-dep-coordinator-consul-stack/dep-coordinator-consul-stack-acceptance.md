---
doc_type: feature-acceptance
feature: 2026-06-04-dep-coordinator-consul-stack
status: accepted
accepted_at: 2026-06-04
summary: 验收 Consul API patch 升级和 Consul coordinator 构建测试闭环
tags: [go, modules, dependency-upgrade, consul, coordinator, acceptance]
---

# dep-coordinator-consul-stack 验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-06-04
> 关联方案 doc：`.codestable/features/2026-06-04-dep-coordinator-consul-stack/dep-coordinator-consul-stack-design.md`

## 1. 接口契约核对

对照方案第 2.1 节名词层逐一核查。

**接口示例逐项核对**：

- [x] `go.mod` module diff 示例：`github.com/hashicorp/consul/api` 已从 `v1.34.2` 升级到 `v1.34.3`。
  - 证据：`git diff -- go.mod go.sum` 只显示 `go.mod` 一行版本变化和 `go.sum` 两条目标 checksum。

**名词层“现状 → 变化”逐项核对**：

- [x] `module_set` 覆盖 roadmap 中 Consul API 与 HashiCorp/Consul indirect 依赖链。
- [x] `github.com/hashicorp/consul/api`：目标版本已落在 `go.mod` direct require，scope 未变化。
- [x] roadmap 覆盖的 indirect module 已逐个确认：父 `consul/api@v1.34.3` 未要求独立更新，因此保留当前 parent-driven 版本。
- [x] `github.com/armon/go-metrics@v0.5.4` path mismatch 已记录：其 `.mod` 声明 `module github.com/hashicorp/go-metrics`。
- [x] `go.sum` 只新增 `github.com/hashicorp/consul/api v1.34.3` content 和 go.mod checksum。
- [x] import surface：`pkg/models/consul/consulclient.go` 和测试仍是仓库内直接 import `github.com/hashicorp/consul/api` 的使用面。

**流程图核对**：

- [x] 图中节点均有实际落点：版本查询、策略分类、path mismatch 复核、定点 `go get`、manifest diff、target test、默认 test、范围守护均已执行并有命令证据。

## 2. 行为与决策核对

**需求摘要逐项验证**：

- [x] 升级 Consul API：`go.mod` 中 `github.com/hashicorp/consul/api` 已为 `v1.34.3`。
- [x] HashiCorp/Consul indirect 依赖链已确认保留边界：未出现独立 indirect 升级 churn。
- [x] Consul coordinator/Jodis 使用面没有可观察构建回归：`go test ./pkg/models/consul ./pkg/models ./cmd/dashboard ./cmd/proxy ./cmd/admin ./cmd/fe` 通过。
- [x] 默认构建测试闭环：`go test ./cmd/... ./pkg/...` 通过。

**明确不做逐项核对**：

- [x] 未引入 `github.com/hashicorp/consul` server 主模块：`go list -m all` 无该主模块。
- [x] 未引入 `github.com/hashicorp/go-metrics`：`go mod why -m github.com/hashicorp/go-metrics` 显示 main module does not need，`go list -deps` 无该 module package。
- [x] `github.com/hashicorp/consul/sdk` 未成为 direct require；仍只是 `consul/api` 的传递依赖。
- [x] 未修改 `pkg/models/consul` 源码：`git diff -- pkg/models/consul/consulclient.go` 无输出。
- [x] 未修改 `models.Client` 或 `models.NewClient`：`git diff -- pkg/models/client.go` 无输出。
- [x] 未修改 dashboard/proxy/admin/fe coordinator 参数或配置语义：diff 无 `cmd/`、`config/` 或 `doc/` 改动。
- [x] 未修改 Zookeeper、Etcd、filesystem coordinator 后端。
- [x] 未升级 Redis client、Martini、RDB parser、metrics 上报、jemalloc 或其他 roadmap 子 feature module。
- [x] 未修改 Go toolchain directive、jemalloc replace、`extern/redis-8.6.3/`、Docker、部署脚本、前端资源或配置模板。
- [x] 未生成 `vendor/`、`Godeps/` 或 `vendor/modules.txt`。

**关键决策落地**：

- [x] D1 只对 `github.com/hashicorp/consul/api` 执行 direct-go-get：落地为单条 `go.mod` 版本变更。
- [x] D2 indirect module parent-driven retain：`go.mod` 未修改这些 indirect 版本。
- [x] D3 `github.com/armon/go-metrics` 保留旧 path 当前版本：没有迁移到 `github.com/hashicorp/go-metrics`。
- [x] D4 不升级 `github.com/hashicorp/serf` 到 `v0.10.2`：避免引入 `hashicorp/go-metrics` 和额外 module churn。
- [x] D5 不做全量 `go mod tidy`：diff 没有 require block 重排或无关 checksum churn。

**编排层“现状 → 变化”逐项核对**：

- [x] 版本查询 → 策略分类 → path mismatch 复核 → 定点升级 → diff 守护 → target test → 默认 test → 范围守护，执行顺序与 checklist 一致。
- [x] post-upgrade module graph 仍触达 `github.com/hashicorp/consul/api` 和 retained indirect；`hashicorp/go-metrics` 不在 API-only package graph。

**流程级约束核对**：

- [x] 错误语义：target test 和默认 test 均通过，无需回退 design。
- [x] 幂等/范围：重复验收后 `go.mod/go.sum` 未出现额外 churn。
- [x] 兼容性：无 Consul coordinator 源码、cmd 参数、配置模板或运行期行为改动。
- [x] 可观测点：`go list`、`go mod why`、`go list -deps`、`git diff`、target test、默认 test、`git status` 均已核对。

**挂载点反向核对（可卸载性）**：

- [x] `go.mod` 中 `github.com/hashicorp/consul/api` direct require：实际升级挂载点存在。
- [x] `go.sum` 中目标 Consul API checksum：lockfile 挂载点存在。
- [x] `pkg/models/consul/consulclient.go` 的 Consul API import：Consul client 使用面挂载点存在。
- [x] target test gate：`go test ./pkg/models/consul ./pkg/models ./cmd/dashboard ./cmd/proxy ./cmd/admin ./cmd/fe` 已执行。
- [x] 默认 cmd/pkg test gate：`go test ./cmd/... ./pkg/...` 已执行。
- [x] 反向 grep：本 feature 的非文档代码变更只命中 `go.mod` / `go.sum`；没有清单外挂载点。
- [x] 拔除沙盘推演：回退 `go.mod` Consul API 版本和删除新增两条 checksum 后，版本升级在系统视角消失。

## 3. 验收场景核对

- [x] **S1**：执行 `GOPROXY=https://proxy.golang.org,direct go list -m -u -json github.com/hashicorp/consul/api`。
  - 证据来源：验收命令。
  - 结果：当前版本为 `v1.34.2`，Update 为 `v1.34.3`。

- [x] **S2**：执行 `go list -m -json github.com/hashicorp/consul/api@latest`。
  - 证据来源：验收命令。
  - 结果：`@latest` 等于 `v1.34.3`。

- [x] **S3**：复核 `github.com/armon/go-metrics@v0.5.4.mod`。
  - 证据来源：module cache `.mod` 文件和失败试跑。
  - 结果：module path mismatch，目标 `.mod` 声明 `module github.com/hashicorp/go-metrics`，本条不迁移。

- [x] **S4**：执行 `go mod why -m` 覆盖 Consul stack 关键 module。
  - 证据来源：验收命令。
  - 结果：`consul/api` 和 retained indirect 可追溯到 `pkg/models/consul`；`github.com/hashicorp/go-metrics` 不被 API-only graph 需要。

- [x] **S5**：执行 `go list -deps ./cmd/... ./pkg/...` 并 grep Consul stack。
  - 证据来源：验收命令。
  - 结果：默认 cmd/pkg 仍触达 `github.com/hashicorp/consul/api` 和当前 retained indirect。

- [x] **S6**：定点 `go get` 后检查 `go.mod`。
  - 证据来源：`git diff -- go.mod go.sum`。
  - 结果：只有 `github.com/hashicorp/consul/api` 改到 `v1.34.3`；`go 1.26.1` 和 jemalloc replace 不变。

- [x] **S7**：检查 `go.sum` diff。
  - 证据来源：`git diff -- go.mod go.sum`。
  - 结果：只新增 Consul API 目标版本 content/go.mod checksum。

- [x] **S8**：执行 `go test ./pkg/models/consul ./pkg/models ./cmd/dashboard ./cmd/proxy ./cmd/admin ./cmd/fe`。
  - 证据来源：Go test。
  - 结果：通过。

- [x] **S9**：执行 `go test ./cmd/... ./pkg/...`。
  - 证据来源：Go test。
  - 结果：通过。

- [x] **S10**：重复验收后查看仓库状态。
  - 证据来源：`git status --short --untracked-files=all`、`find`、`git worktree list`。
  - 结果：无 `vendor/`、`Godeps/`、`vendor/modules.txt` 或仓库内临时构建产物；临时 detached worktree 已移除。

前端浏览器验证：不适用。本 feature 未改 FE 静态资源、HTML、JS、CSS 或可见 UI，只升级 Go Consul API 依赖并通过编译测试。

## 4. 术语一致性

- `Consul coordinator stack`：代码和文档中均指 `github.com/hashicorp/consul/api` 与 roadmap 覆盖的 Consul/HashiCorp indirect 依赖链。
- `Consul API direct module`：实际 direct require 只有 `github.com/hashicorp/consul/api`。
- `Parent-driven indirect`：验收落实为 indirect 不做独立 `@latest` 升级。
- `Path-mismatch retain`：`github.com/armon/go-metrics` 的 path mismatch 已记录，未引入 `hashicorp/go-metrics`。
- `Minimal module diff`：实际符合，仅 `go.mod` 一行版本和 `go.sum` 两条 checksum。
- 防冲突：无 Consul server 主模块、Consul SDK direct require、hashicorp/go-metrics 或新 coordinator 抽象进入代码 diff。

## 5. 架构归并

对照方案第 4 节，本 feature 不新增运行期能力，不改变 `Coordinator / Store` 抽象、Consul 后端语义或 Go module manifest 契约，只维护 Consul API 依赖版本。

- [x] `.codestable/architecture/ARCHITECTURE.md`：不需要更新。理由：架构文档已记录 Consul 后端使用 `github.com/hashicorp/consul/api` 和 KV/Session 语义；本次没有新模块、接口或跨模块纪律。
- [x] `.codestable/attention.md`：不需要更新。理由：本次 path mismatch 是本 feature 的依赖决策，不是每个后续 feature 都会撞的项目通用命令陷阱；既有“不要全量 go mod tidy”和“不顺手现代化 Go 依赖”约束已经覆盖。

## 6. requirement 回写

- [x] `requirement: null`，且方案明确为依赖维护 / 技术债，不新增用户可感能力。

结论：无 requirement 回写。

## 7. roadmap 回写

- [x] design frontmatter 包含 `roadmap: go-dependency-upgrade` 和 `roadmap_item: dep-coordinator-consul-stack`。
- [x] `.codestable/roadmap/go-dependency-upgrade/go-dependency-upgrade-items.yaml` 中对应条目已从 `in-progress` 改为 `done`，feature 保持 `2026-06-04-dep-coordinator-consul-stack`。
- [x] `.codestable/roadmap/go-dependency-upgrade/go-dependency-upgrade-roadmap.md` 第 5 节对应子 feature 已同步为 `done`，并写入 feature 目录名。
- [x] roadmap 主文档变更日志追加本 feature 完成记录。
- [x] items YAML 已通过 `python3 .codestable/tools/validate-yaml.py --file ... --yaml-only` 校验。

## 8. attention.md 候选盘点

- [x] 无候选。本 feature 未暴露需要补入 `.codestable/attention.md` 的新环境、工具或工作流约束。

## 9. 遗留

- 后续优化点：如目标是整体升级 HashiCorp indirect 依赖链，应另起明确依赖迁移条目，特别是 `serf v0.10.2` 引入 `github.com/hashicorp/go-metrics` 的 module identity 变化。
- 已知限制：本次只升级 Consul API patch 版本，不验证真实外部 Consul agent 的运行期 KV/Session/watch 行为。
- 实现阶段顺手发现：`github.com/armon/go-metrics@v0.5.4` path mismatch；已在本报告记录，不单独开 issue。
