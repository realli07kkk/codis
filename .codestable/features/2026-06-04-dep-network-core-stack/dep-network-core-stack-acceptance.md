---
doc_type: feature-acceptance
feature: 2026-06-04-dep-network-core-stack
status: current
accepted_at: 2026-06-04
summary: golang.org/x/net 已升级到 v0.55.0，golang.org/x/sys 已升级到 v0.45.0，并完成默认 cmd/pkg 测试与 cgo_jemalloc proxy 构建闭环。
tags: [go, modules, dependency-upgrade, x-net, x-sys, cgo]
---

# dep-network-core-stack 验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-06-04
> 关联方案 doc：`.codestable/features/2026-06-04-dep-network-core-stack/dep-network-core-stack-design.md`

## 1. 接口契约核对

对照方案第 2.1 节名词层逐一核查。

**接口示例逐项核对**：
- [x] `go.mod` direct require：`golang.org/x/net v0.51.0 -> v0.55.0`，实际已落在 `go.mod:23`。
- [x] `go.mod` indirect require：`golang.org/x/sys v0.41.0 -> v0.45.0 // indirect`，实际已落在 `go.mod:55`。
- [x] `go.sum` checksum：新增 `golang.org/x/net v0.55.0` / `v0.55.0/go.mod` 与 `golang.org/x/sys v0.45.0` / `v0.45.0/go.mod` 四条 checksum，实际落在 `go.sum:215` 到 `go.sum:246`。

**名词层"现状 -> 变化"逐项核对**：
- [x] `module_set`：只覆盖 `golang.org/x/net` 与 `golang.org/x/sys`，没有新增其他 module。
- [x] `golang.org/x/net`：scope 保持 direct，目标版本为 tagged `v0.55.0`。
- [x] `golang.org/x/sys`：scope 保持 indirect，目标版本为 tagged `v0.45.0`。
- [x] import surface：`rg 'golang\.org/x/(net|sys)' --glob '*.go' --glob '!extern/**' --glob '!third_party/**'` 仍只命中三处 `golang.org/x/net/context`，没有本仓库新增 `golang.org/x/sys/*` 直接 import。

**流程图核对**：
- [x] 版本查询节点：`go list -m -json golang.org/x/net@latest` 返回 `v0.55.0`；`go list -m -json golang.org/x/sys@latest` 返回 `v0.45.0`。
- [x] 定点 `go get` 节点：实现阶段执行 `go get golang.org/x/net@v0.55.0 golang.org/x/sys@v0.45.0`，只升级这两个 module。
- [x] 默认测试节点：`go test ./cmd/... ./pkg/...` 通过。
- [x] `cgo_jemalloc` 节点：`go build -tags cgo_jemalloc -o /tmp/codis-proxy-cgo-test ./cmd/proxy` 通过，且临时产物已删除。

## 2. 行为与决策核对

**需求摘要逐项验证**：
- [x] `golang.org/x/net` 已从 `v0.51.0` 升到 `v0.55.0`。
- [x] `golang.org/x/sys` 已从 `v0.41.0` 升到 `v0.45.0`。
- [x] `go.mod/go.sum` 只出现目标 module 的最小机械变化：`go.mod` 两行版本变化，`go.sum` 四行新增 checksum。
- [x] 默认 `cmd/pkg` 测试与 `cgo_jemalloc` proxy 构建均通过。

**明确不做逐项核对**：
- [x] 未迁移 `github.com/coreos/etcd` 到现代 `go.etcd.io/etcd/*` module path。
- [x] 未把三处 `golang.org/x/net/context` 改成标准库 `context`。
- [x] 未升级 `github.com/hashicorp/consul/api`、`github.com/mattn/go-isatty` 或其他父依赖链。
- [x] 未修改 `cmd/`、`pkg/` 运行逻辑、Redis 协议、proxy 路由、coordinator 语义或配置格式。
- [x] 未修改 `third_party/jemalloc-go`、`extern/redis-8.6.3/`、Docker、部署脚本、前端资源或配置模板。
- [x] 未运行全量 `go mod tidy` 收口，未生成 `vendor/`、`Godeps/` 或 `vendor/modules.txt`。
- [x] 未升级 Go toolchain，`go.mod:3` 仍是 `go 1.26.1`。

**关键决策落地**：
- [x] 目标版本采用 `@latest` tagged release：验收查询确认 `x/net v0.55.0` 与 `x/sys v0.45.0` 均为 tagged release。
- [x] 同一 diff 定点升级两个 module：`go.mod` 只改 `x/net` 和 `x/sys` 两个 require。
- [x] 保留 `x/sys` indirect 身份：`go.mod:55` 保留 `// indirect`，`go mod why -m golang.org/x/sys` 仍显示 Consul/HashiCorp 间接链路。
- [x] 不做 context import 迁移：三处 `golang.org/x/net/context` import 未改。
- [x] 不触碰 jemalloc-go local replace：`go.mod:58` 仍为 `replace github.com/spinlock/jemalloc-go => ./third_party/jemalloc-go`。

**编排层"现状 -> 变化"逐项核对**：
- [x] 执行顺序符合方案：版本查询 -> 定点升级 -> diff 守护 -> 默认测试 -> cgo build -> 范围守护。
- [x] `go.sum` 保留旧 checksum 并新增目标 checksum；没有手工清理历史 checksum。
- [x] 验证结果没有引出代码层兼容性修补，运行代码保持零 diff。

**流程级约束核对**：
- [x] 未先做全量依赖整理；重复定点 `go get` 没有产生额外 diff。
- [x] `go test` 和 `cgo_jemalloc` build 都通过，没有用父依赖大升级掩盖问题。
- [x] `test ! -e vendor && test ! -e Godeps && test ! -e vendor/modules.txt` 通过。
- [x] `go list -m -json github.com/spinlock/jemalloc-go` 的 `Replace.Dir` 指向 `third_party/jemalloc-go`。

**挂载点反向核对（可卸载性）**：
- [x] 挂载点 M1 `go.mod` 中 `golang.org/x/net` direct require：实际落点 `go.mod:23`。
- [x] 挂载点 M2 `go.mod` 中 `golang.org/x/sys` indirect require：实际落点 `go.mod:55`。
- [x] 挂载点 M3 `go.sum` 目标 checksum：实际落点 `go.sum:215` 到 `go.sum:246`。
- [x] 挂载点 M4 `cgo_jemalloc` verification gate：实际命令通过。
- [x] 反向核查：`git diff --name-only` 中除 CodeStable 文档与 roadmap 外，只包含 `go.mod`、`go.sum`，无清单外代码挂载点。
- [x] 拔除沙盘推演：回退 `go.mod` 两条版本和删除 `go.sum` 四条 checksum 即可拔除本 feature 的依赖升级效果；没有运行代码残留。

## 3. 验收场景核对

- [x] **S1**：`go list -m -json golang.org/x/net@latest` 返回 tagged `v0.55.0`
  - 证据来源：手工命令
  - 结果：通过。

- [x] **S2**：`go list -m -json golang.org/x/sys@latest` 返回 tagged `v0.45.0`
  - 证据来源：手工命令
  - 结果：通过。

- [x] **S3**：定点 `go get` 后检查 `go.mod`
  - 证据来源：`git diff -- go.mod`
  - 结果：通过，`x/net` 与 `x/sys` 已到目标版本，`go 1.26.1` 和 jemalloc replace 保留。

- [x] **S4**：检查 `go.sum` diff
  - 证据来源：`git diff -- go.sum`
  - 结果：通过，只新增目标版本 checksum。

- [x] **S5**：`go mod why -m golang.org/x/net`
  - 证据来源：手工命令
  - 结果：通过，仍追溯到本仓库 `pkg/models/etcd -> golang.org/x/net/context`。

- [x] **S6**：`go mod why -m golang.org/x/sys`
  - 证据来源：手工命令
  - 结果：通过，仍追溯到 `pkg/models/consul -> hashicorp/consul/api -> hashicorp/go-hclog -> mattn/go-isatty -> x/sys/unix`。

- [x] **S7**：`go test ./cmd/... ./pkg/...`
  - 证据来源：Go 测试
  - 结果：通过。

- [x] **S8**：`go build -tags cgo_jemalloc -o /tmp/codis-proxy-cgo-test ./cmd/proxy`
  - 证据来源：Go 构建
  - 结果：通过，且 `/tmp/codis-proxy-cgo-test` 已删除。

- [x] **S9**：重复验收命令后检查工作区
  - 证据来源：重复定点 `go get`、`git status --short`、`test ! -e vendor && test ! -e Godeps && test ! -e vendor/modules.txt`
  - 结果：通过，未生成 vendor/Godeps 或仓库内临时构建产物。

## 4. 术语一致性

- `Network core stack`：只在 CodeStable spec 中作为本 feature 的分组名使用，代码未新增同名概念。
- `Target module version`：已落实为 `go.mod` 中 `x/net v0.55.0` 与 `x/sys v0.45.0`。
- `Minimal module diff`：实际 diff 符合，仅 `go.mod` 两行版本和 `go.sum` 四行 checksum。
- `cgo_jemalloc verification gate`：保留为验收命令，代码未新增平行 build tag 或配置名。
- 防冲突：代码没有新增术语、类型、函数或抽象；无需额外 grep 禁用词。

## 5. 架构归并

- [x] `.codestable/architecture/ARCHITECTURE.md`：不需要更新。理由：架构文档已经记录 `go.mod/go.sum` 是 Go modules 依赖入口、`cgo_jemalloc` 通过 `third_party/jemalloc-go` local replace 接入；本次只更新两个外部 module 版本，没有新增模块、接口、运行期流程或跨 feature 稳定约束。
- [x] 架构总入口无需新增链接。理由：本次是 roadmap 子 feature 进度和 lockfile 变化，持久证据已在 `go.mod/go.sum`、feature acceptance 和 roadmap 中。

## 6. requirement 回写

- [x] `requirement` 为空，且方案明确不新增用户可感能力：跳过 requirement 回写。
- [x] `redis-cluster-service` 与 `platform-release-artifacts` 的用户故事、边界和 pitch 均未变化；本 feature 是依赖维护，不改变运行能力或发布产物结构。

## 7. roadmap 回写

- [x] design frontmatter 包含 `roadmap: go-dependency-upgrade` 与 `roadmap_item: dep-network-core-stack`。
- [x] `.codestable/roadmap/go-dependency-upgrade/go-dependency-upgrade-items.yaml`：对应条目已从 `in-progress` 改为 `done`，并保留 `feature: 2026-06-04-dep-network-core-stack`。
- [x] `.codestable/roadmap/go-dependency-upgrade/go-dependency-upgrade-roadmap.md`：第 5 节子 feature 清单已同步为 `状态：done` 和对应 feature 目录；第 8 节追加完成记录。
- [x] YAML 校验：`python3 .codestable/tools/validate-yaml.py --file .codestable/roadmap/go-dependency-upgrade/go-dependency-upgrade-items.yaml --yaml-only` 通过。

## 8. attention.md 候选盘点

- [x] 本 feature 未暴露需要补入 attention.md 的新内容。已有注意事项已经覆盖本次关键约束：不要全量 `go mod tidy`、Go modules 是默认依赖入口、jemalloc replace 指向 `third_party/jemalloc-go`、使用 `python3` 运行 `.codestable/tools/*.py`。

## 9. 遗留

- 后续优化点：可另起小型 refactor 评估把三处 `golang.org/x/net/context` 迁到标准库 `context`；本次不做，因为它不是依赖升级最小闭环的必要条件。
- 已知限制：`make gotest` 未在本 feature 中运行；按 roadmap 第 4.3 节，批量阶段收口时再运行 `make gotest`。
- 实现阶段顺手发现：无。
