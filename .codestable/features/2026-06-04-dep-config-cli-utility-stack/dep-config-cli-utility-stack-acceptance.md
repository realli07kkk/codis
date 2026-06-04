---
doc_type: feature-acceptance
feature: 2026-06-04-dep-config-cli-utility-stack
status: current
accepted_at: 2026-06-04
summary: 配置、CLI、UUID、通用数据结构和 bpool 相关 Go module 已定点升级，ugorji/go 已确认保留，并完成默认 cmd/pkg 测试闭环。
tags: [go, modules, dependency-upgrade, config, cli, utility]
---

# dep-config-cli-utility-stack 验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-06-04
> 关联方案 doc：`.codestable/features/2026-06-04-dep-config-cli-utility-stack/dep-config-cli-utility-stack-design.md`

## 1. 接口契约核对

对照方案第 2.1 节名词层逐一核查。

**接口示例逐项核对**：
- [x] `go.mod` direct require：五个升级 module 已落在 `go.mod:6`、`go.mod:8`、`go.mod:9`、`go.mod:12`、`go.mod:19`。
- [x] `go.mod` retain-with-note：`github.com/ugorji/go v1.2.14` 保留在 `go.mod:22`。
- [x] `go.sum` checksum：新增五个目标版本的 10 条 checksum；`github.com/ugorji/go` 未新增 checksum。

**名词层"现状 -> 变化"逐项核对**：
- [x] `module_set`：只覆盖 `github.com/BurntSushi/toml`、`github.com/docopt/docopt-go`、`github.com/google/uuid`、`github.com/emirpasic/gods`、`github.com/oxtoacart/bpool`、`github.com/ugorji/go`。
- [x] `github.com/BurntSushi/toml`：scope 保持 direct，目标版本为 `v1.6.0`。
- [x] `github.com/docopt/docopt-go`：scope 保持 direct，目标版本为 `v0.0.0-20180111231733-ee0de3bc6815`。
- [x] `github.com/google/uuid`：scope 保持 direct，目标版本为 `v1.6.0`。
- [x] `github.com/emirpasic/gods`：scope 保持 direct，目标版本为 `v1.18.1`。
- [x] `github.com/oxtoacart/bpool`：scope 保持 direct，目标版本为 `v0.0.0-20190530202638-03653db5a59c`。
- [x] `github.com/ugorji/go`：保留 `v1.2.14`，处理策略为 `retain-with-note`。
- [x] import surface：`rg` 核对显示配置、CLI、topom 相关 import 未新增或删除；本 feature 没有 Go 源码 diff。

**流程图核对**：
- [x] 版本查询节点：`go list -m -json <module>@latest` 返回 design 记录的六个目标版本。
- [x] 策略分类节点：`go mod why -m github.com/oxtoacart/bpool` 仍追溯到 `cmd/fe -> martini-contrib/render -> bpool`；`github.com/ugorji/go` 仍显示 main module 不需要。
- [x] 定点 `go get` 节点：实现阶段只升级五个 module，不把 `ugorji/go` 放进升级命令。
- [x] `go.mod/go.sum` diff 节点：`go.mod` 只改五行版本，`go.sum` 只新增 10 条 checksum。
- [x] 默认测试节点：`go test ./cmd/... ./pkg/...` 通过。
- [x] 范围守护节点：`git diff --name-only` 除 CodeStable/roadmap 文档外只包含 `go.mod`、`go.sum`。

## 2. 行为与决策核对

**需求摘要逐项验证**：
- [x] 配置解析依赖 `github.com/BurntSushi/toml` 已升级到 `v1.6.0`。
- [x] CLI 参数解析依赖 `github.com/docopt/docopt-go` 已升级到 `v0.0.0-20180111231733-ee0de3bc6815`。
- [x] UUID 依赖 `github.com/google/uuid` 已升级到 `v1.6.0`。
- [x] slot action 排序辅助依赖 `github.com/emirpasic/gods` 已升级到 `v1.18.1`。
- [x] Martini render 间接 buffer pool 依赖 `github.com/oxtoacart/bpool` 已升级到 `v0.0.0-20190530202638-03653db5a59c`。
- [x] `github.com/ugorji/go` 当前版本等于 latest，已保留 `v1.2.14`。
- [x] 默认 `cmd/pkg` 测试通过，配置默认值、CLI 参数解析、slot action 流程和 RDB Analysis job id 没有因代码修改产生回归面。

**明确不做逐项核对**：
- [x] 未修改 `pkg/proxy/config.go` 或 `pkg/topom/config.go` 的配置结构、TOML tag、默认配置文本或输出格式。
- [x] 未修改 `cmd/admin`、`cmd/dashboard`、`cmd/fe`、`cmd/ha`、`cmd/proxy` 的 usage 文本、参数名、参数默认值或分发逻辑。
- [x] 未修改 `pkg/topom/topom_slots.go` 的 slot action 编排、排序规则或迁移语义。
- [x] 未修改 RDB Analysis job id 生成逻辑，未替换 `uuid.NewV7`。
- [x] 未升级 Martini web stack 本体、Redis client、coordinator、RDB parser、metrics 或其他 roadmap 子 feature 覆盖的 module。
- [x] 未删除 `github.com/ugorji/go` direct require，未运行全量 `go mod tidy`。
- [x] 未升级 Go toolchain，`go.mod:3` 仍为 `go 1.26.1`。
- [x] 未修改 `third_party/jemalloc-go`、`extern/redis-8.6.3`、Docker、部署脚本、前端资源或配置模板。
- [x] 未生成 `vendor/modules.txt` 或新增 vendor/Godeps 目录。

**关键决策落地**：
- [x] 五个 module 采用 `@latest`：验收查询确认目标版本分别为 `toml v1.6.0`、`docopt-go v0.0.0-20180111231733-ee0de3bc6815`、`uuid v1.6.0`、`gods v1.18.1`、`bpool v0.0.0-20190530202638-03653db5a59c`。
- [x] `docopt-go` 与 `bpool` 走 pseudo latest：`go list -m -versions -json` 对二者没有 tagged version 列表，实际升级后默认测试通过。
- [x] 同一 diff 定点升级五个 module：`go.mod` 只改五个 direct require 的版本。
- [x] 不删除 `github.com/ugorji/go`：`go.mod:22` 保留 `v1.2.14`。
- [x] 不修改调用代码适配新版 API：本次没有 Go 源码 diff。

**编排层"现状 -> 变化"逐项核对**：
- [x] 执行顺序符合方案：版本查询 -> 策略分类 -> 定点升级 -> diff 守护 -> 默认测试 -> 范围守护。
- [x] `go.sum` 保留旧 checksum 并新增目标 checksum；没有手工清理历史 checksum。
- [x] 验证结果没有引出代码层兼容性修补，运行代码保持零 diff。

**流程级约束核对**：
- [x] 未先做全量依赖整理；未用 `go mod tidy` 收口。
- [x] `go test ./cmd/... ./pkg/...` 通过，没有用 Martini stack 升级或业务代码重写掩盖问题。
- [x] 重复验收命令后没有生成 `vendor/`、`Godeps/` 或 `vendor/modules.txt`。
- [x] 配置格式、CLI 参数、Redis 协议、proxy/topom/coordinator 行为保持不变。
- [x] 可观测点已覆盖：`go list`、`go mod why -m`、`go.mod/go.sum` diff、`go test`、`git diff --check`、`git status --short`。

**挂载点反向核对（可卸载性）**：
- [x] 挂载点 M1 `go.mod` 中五个 upgraded direct require：实际落点 `go.mod:6`、`go.mod:8`、`go.mod:9`、`go.mod:12`、`go.mod:19`。
- [x] 挂载点 M2 `go.mod` 中 `github.com/ugorji/go v1.2.14` direct require：实际落点 `go.mod:22`。
- [x] 挂载点 M3 `go.sum` 中五个目标版本 checksum：实际新增 10 条 checksum。
- [x] 挂载点 M4 默认 cmd/pkg 测试 gate：实际命令 `go test ./cmd/... ./pkg/...` 通过。
- [x] 反向核查：`git diff --name-only` 中除 CodeStable/roadmap 文档外，只包含 `go.mod`、`go.sum`，无清单外代码挂载点。
- [x] 拔除沙盘推演：回退 `go.mod` 五条版本、删除 `go.sum` 10 条 checksum，并保留 `ugorji/go v1.2.14` 即可拔除本 feature 的依赖升级效果；没有运行代码残留。

## 3. 验收场景核对

- [x] **S1**：`go list -m -json github.com/BurntSushi/toml@latest` 返回 `v1.6.0`
  - 证据来源：手工命令
  - 结果：通过。

- [x] **S2**：`go list -m -json github.com/docopt/docopt-go@latest` 返回 `v0.0.0-20180111231733-ee0de3bc6815`，且 `go list -m -versions` 无 tagged release 列表
  - 证据来源：手工命令
  - 结果：通过。

- [x] **S3**：`go list -m -json github.com/google/uuid@latest` 返回 `v1.6.0`
  - 证据来源：手工命令
  - 结果：通过。

- [x] **S4**：`go list -m -json github.com/emirpasic/gods@latest` 返回 `v1.18.1`
  - 证据来源：手工命令
  - 结果：通过。

- [x] **S5**：`go list -m -json github.com/oxtoacart/bpool@latest` 返回 `v0.0.0-20190530202638-03653db5a59c`，且 `go list -m -versions` 无 tagged release 列表
  - 证据来源：手工命令
  - 结果：通过。

- [x] **S6**：`go list -m -json github.com/ugorji/go@latest` 返回 `v1.2.14`，不产生升级 diff
  - 证据来源：手工命令 + `git diff -- go.mod`
  - 结果：通过。

- [x] **S7**：定点 `go get` 后检查 `go.mod`
  - 证据来源：`git diff -- go.mod`
  - 结果：通过，五个升级 module 到目标版本；`github.com/ugorji/go v1.2.14`、`go 1.26.1` 和 `replace github.com/spinlock/jemalloc-go => ./third_party/jemalloc-go` 不变。

- [x] **S8**：检查 `go.sum` diff
  - 证据来源：`git diff -- go.sum`
  - 结果：通过，只新增五个目标版本的 module/content checksum。

- [x] **S9**：`go mod why -m github.com/oxtoacart/bpool`
  - 证据来源：手工命令
  - 结果：通过，仍可追溯到 `cmd/fe -> martini-contrib/render -> bpool`。

- [x] **S10**：`go mod why -m github.com/ugorji/go`
  - 证据来源：手工命令
  - 结果：通过，仍显示 main module 不需要该 module；本 feature 没删除它。

- [x] **S11**：`go test ./cmd/... ./pkg/...`
  - 证据来源：Go 测试
  - 结果：通过。

- [x] **S12**：重复验收命令后查看 `git status --short`
  - 证据来源：`git status --short`、`find . -maxdepth 3 ... vendor/Godeps ...`
  - 结果：通过，未生成 `vendor/`、`Godeps/`、`vendor/modules.txt` 或仓库内临时构建产物。

## 4. 术语一致性

- `Config / CLI / utility stack`：只在 CodeStable spec 中作为本 feature 分组名使用，代码未新增同名概念。
- `Target module version`：已落实为 `go.mod` 中五个升级版本和 `ugorji/go v1.2.14` 保留。
- `retain-with-note`：已落实为 `github.com/ugorji/go v1.2.14` 保留，且报告记录 `go mod why` 结果。
- `Minimal module diff`：实际 diff 符合，仅 `go.mod` 五行版本和 `go.sum` 10 行 checksum。
- 防冲突：代码没有新增术语、类型、函数或抽象；无需额外代码命名修正。

## 5. 架构归并

- [x] `.codestable/architecture/ARCHITECTURE.md`：不需要更新。理由：架构文档已经记录 `go.mod/go.sum` 是 Go modules 依赖入口、旧 vendor/Godeps 已退休、默认 cmd/pkg module mode 可测试；本次只更新外部 module 版本，没有新增模块、接口、运行期流程或跨 feature 稳定约束。
- [x] 架构总入口无需新增链接。理由：本次是 roadmap 子 feature 进度和 lockfile 变化，持久证据已在 `go.mod/go.sum`、feature acceptance 和 roadmap 中。

## 6. requirement 回写

- [x] `requirement` 为空，且方案明确不新增用户可感能力：跳过 requirement 回写。
- [x] `redis-cluster-service` 与 `platform-release-artifacts` 的用户故事、边界和 pitch 均未变化；本 feature 是依赖维护，不改变运行能力或发布产物结构。

## 7. roadmap 回写

- [x] design frontmatter 包含 `roadmap: go-dependency-upgrade` 与 `roadmap_item: dep-config-cli-utility-stack`。
- [x] `.codestable/roadmap/go-dependency-upgrade/go-dependency-upgrade-items.yaml`：对应条目已从 `in-progress` 改为 `done`，并保留 `feature: 2026-06-04-dep-config-cli-utility-stack`。
- [x] `.codestable/roadmap/go-dependency-upgrade/go-dependency-upgrade-roadmap.md`：第 5 节子 feature 清单已同步为 `状态：done` 和对应 feature 目录。
- [x] YAML 校验：`python3 .codestable/tools/validate-yaml.py --file .codestable/roadmap/go-dependency-upgrade/go-dependency-upgrade-items.yaml --yaml-only` 通过。

## 8. attention.md 候选盘点

- [x] 本 feature 未暴露需要补入 attention.md 的新内容。已有注意事项已经覆盖本次关键约束：不要全量 `go mod tidy`、Go modules 是默认依赖入口、使用 `python3` 运行 `.codestable/tools/*.py`。

## 9. 遗留

- 后续优化点：`github.com/ugorji/go` 当前不被默认构建路径需要；如果要减少 direct require 噪音，可以另起依赖清理任务，本次不做。
- 已知限制：`make gotest` 未在本 feature 中运行；按 roadmap 第 4.3 节，批量阶段收口时再运行 `make gotest`。
- 实现阶段顺手发现：无。
