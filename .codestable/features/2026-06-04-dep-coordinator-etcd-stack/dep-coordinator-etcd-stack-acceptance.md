---
doc_type: feature-acceptance
feature: 2026-06-04-dep-coordinator-etcd-stack
status: accepted
accepted_at: 2026-06-04
summary: 验收旧 etcd coordinator client 栈保留确认和 coordinator 构建测试闭环
tags: [go, modules, dependency-upgrade, etcd, coordinator, acceptance]
---

# dep-coordinator-etcd-stack 验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-06-04
> 关联方案 doc：`.codestable/features/2026-06-04-dep-coordinator-etcd-stack/dep-coordinator-etcd-stack-design.md`

## 1. 接口契约核对

对照方案第 2.1 节名词层逐一核查。

**接口示例逐项核对**：

- [x] `go list -m -json github.com/coreos/etcd@latest` 返回 `v3.3.27+incompatible`。
  - 代码实际行为：`go.mod` 继续保持 `github.com/coreos/etcd v3.3.27+incompatible`，不产生升级 diff。

**名词层“现状 → 变化”逐项核对**：

- [x] `module_set` 覆盖五个 module：`github.com/coreos/etcd`、`github.com/coreos/go-semver`、`github.com/json-iterator/go`、`github.com/modern-go/concurrent`、`github.com/modern-go/reflect2`。
- [x] 五个 module 的 `@latest` 均等于当前版本，全部处理策略为 `retain-with-note`。
- [x] `github.com/coreos/etcd` 保持 direct require，版本为 `v3.3.27+incompatible`。
- [x] 四个 indirect module 保持当前版本，且仍经 etcd client 依赖链触达。
- [x] `go.mod/go.sum` 对本 feature 零 diff。
- [x] import surface 不变：`pkg/models/etcd/etcdclient.go` 仍是唯一直接 import `github.com/coreos/etcd/client` 的仓库代码。

**流程图核对**：

- [x] 图中节点均有实际落点：版本查询、策略分类、manifest 零 diff、target test、默认 test、范围守护均已执行并有命令证据。

## 2. 行为与决策核对

**需求摘要逐项验证**：

- [x] 确认旧 etcd client 栈没有同路径升级目标：五个 module 的 `go list -m -u -json` 均无 `Update` 字段。
- [x] 保留 `github.com/coreos/etcd/client`：代码中没有 `go.etcd.io/etcd` import。
- [x] coordinator 编译测试闭环：`go test ./pkg/models/etcd ./pkg/models ./cmd/dashboard ./cmd/proxy ./cmd/admin ./cmd/fe` 通过。
- [x] 默认构建测试闭环：`go test ./cmd/... ./pkg/...` 通过。

**明确不做逐项核对**：

- [x] 未迁移到 `go.etcd.io/etcd/*`：`rg "go.etcd.io/etcd"` 无命中。
- [x] 未修改 `pkg/models/etcd` 源码：`git diff -- pkg/models/etcd/etcdclient.go` 无输出。
- [x] 未修改 `models.Client` 或 `models.NewClient`：`git diff -- pkg/models/client.go` 无输出。
- [x] 未修改 dashboard/proxy/admin/fe coordinator 参数或配置语义：diff 无 `cmd/`、`config/` 或 `doc/` 运行说明改动。
- [x] 未修改 Zookeeper、filesystem、Consul coordinator 后端。
- [x] 未升级 Redis client、Martini、Zookeeper、Consul、RDB analysis、metrics、jemalloc 或其他 roadmap 子 feature module。
- [x] 未修改 Go toolchain directive、jemalloc replace、`extern/redis-8.6.3/`、Docker、部署脚本、前端资源或配置模板。
- [x] 未生成 `vendor/`、`Godeps/` 或 `vendor/modules.txt`。

**关键决策落地**：

- [x] D1 五个 module 全部 `retain-with-note`：`go.mod/go.sum` 零 diff，验收报告记录版本查询证据。
- [x] D2 不迁移现代 etcd module path：代码仍只使用 `github.com/coreos/etcd/client`。
- [x] D3 保留 `etcd v3.3.27+incompatible` 历史编译理由：design 已引用 2026-05-12 learning。
- [x] D4 target 验证覆盖 coordinator 编译面：目标测试已覆盖 `pkg/models/etcd`、`pkg/models` 和相关 cmd 入口。
- [x] D5 不做全量 `go mod tidy`：diff 没有 require block 重排或无关 module churn。

**编排层“现状 → 变化”逐项核对**：

- [x] 版本查询 → 策略分类 → manifest 零 diff → target test → 默认 test → 范围守护，执行顺序与 checklist 一致。
- [x] post-confirm module graph 仍触达 etcd stack，所有触达均来自 `pkg/models/etcd` 依赖链。

**流程级约束核对**：

- [x] 错误语义：target test 和默认 test 均通过，无需回退 design。
- [x] 幂等/范围：重复验收后 `go.mod/go.sum` 未出现 diff。
- [x] 兼容性：无 coordinator 源码、cmd 参数、配置模板或运行期行为改动。
- [x] 可观测点：`go list`、`go mod why`、`go list -deps`、`git diff`、target test、默认 test、`git status` 均已核对。

**挂载点反向核对（可卸载性）**：

- [x] `go.mod` 中 `github.com/coreos/etcd` direct require：保留挂载点存在。
- [x] `go.mod` 中四个 indirect require：依赖链保留挂载点存在。
- [x] `pkg/models/etcd/etcdclient.go` 的 `github.com/coreos/etcd/client` import：旧 client path 保留挂载点存在。
- [x] target test gate：`go test ./pkg/models/etcd ./pkg/models ./cmd/dashboard ./cmd/proxy ./cmd/admin ./cmd/fe` 已执行。
- [x] 默认 cmd/pkg test gate：`go test ./cmd/... ./pkg/...` 已执行。
- [x] 反向 grep：本 feature 的非文档代码变更为空；没有清单外挂载点。
- [x] 拔除沙盘推演：移除本 feature spec 和 roadmap done 状态后，本条“保留确认”在系统视角消失；由于 manifest 零 diff，无依赖版本需要回退。

## 3. 验收场景核对

- [x] **S1**：执行 `GOPROXY=https://proxy.golang.org,direct go list -m -u -json` 覆盖五个 module。
  - 证据来源：验收命令。
  - 结果：五个 module 均无 `Update` 字段。

- [x] **S2**：执行 `go list -m -json <module>@latest` 覆盖五个 module。
  - 证据来源：验收命令。
  - 结果：`@latest` 均等于当前版本。

- [x] **S3**：执行 `go list -m -versions -json` 覆盖五个 module。
  - 证据来源：验收命令。
  - 结果：`etcd` 最新同路径 tagged version 为 `v3.3.27+incompatible`，`go-semver` 为 `v0.3.1`，`json-iterator/go` 为 `v1.1.12`，`reflect2` 为 `v1.0.2`；`modern-go/concurrent` 无 tagged `Versions` 列表但当前 pseudo 是 `@latest`。

- [x] **S4**：执行 `go mod why -m` 覆盖五个 module。
  - 证据来源：验收命令。
  - 结果：五个 module 均可追溯到 `pkg/models/etcd` 和 `github.com/coreos/etcd/client` 依赖链。

- [x] **S5**：执行 `go list -deps ./cmd/... ./pkg/...` 并 grep etcd stack。
  - 证据来源：验收命令。
  - 结果：默认 cmd/pkg 仍触达 etcd stack。

- [x] **S6**：检查 `git diff -- go.mod go.sum`。
  - 证据来源：git diff。
  - 结果：零 diff。

- [x] **S7**：执行 `go test ./pkg/models/etcd ./pkg/models ./cmd/dashboard ./cmd/proxy ./cmd/admin ./cmd/fe`。
  - 证据来源：Go test。
  - 结果：通过。

- [x] **S8**：执行 `go test ./cmd/... ./pkg/...`。
  - 证据来源：Go test。
  - 结果：通过。

- [x] **S9**：重复验收后查看仓库状态。
  - 证据来源：`git status --short --untracked-files=all`、`find`。
  - 结果：无 `vendor/`、`Godeps/`、`vendor/modules.txt` 或仓库内临时构建产物。

前端浏览器验证：不适用。本 feature 未改 FE 静态资源、HTML、JS、CSS 或可见 UI，只确认 Go coordinator 依赖栈保留并通过编译测试。

## 4. 术语一致性

- `Etcd coordinator stack`：文档中均指五个 module 和 `pkg/models/etcd` 的旧 client 使用面。
- `旧 etcd client path`：代码命中只有 `pkg/models/etcd/etcdclient.go:13` 的 `github.com/coreos/etcd/client`。
- `retain-with-note`：已落实为五个 module 保持当前版本，未引入新代码概念。
- `Zero module diff`：实际符合，`go.mod/go.sum` 对本 feature 无变化。
- 防冲突：无 `go.etcd.io/etcd`、`clientv3`、lease/watch 新 API 命名进入代码 diff。

## 5. 架构归并

对照方案第 4 节，本 feature 不新增运行期能力，不改变 `Coordinator / Store` 抽象、Etcd 后端语义或 Go module manifest 契约，只维护旧 etcd client 栈的依赖版本确认。

- [x] `.codestable/architecture/ARCHITECTURE.md`：不需要更新。理由：架构文档已记录 Coordinator / Store 支持 etcd 和 `go.mod/go.sum` 是 Go modules 入口；本次没有新模块、接口或跨模块纪律。
- [x] `.codestable/attention.md`：不需要更新。理由：本次没有暴露新的项目通用命令陷阱；既有“不要全量 go mod tidy”和 etcd 历史注意事项已覆盖。

## 6. requirement 回写

- [x] `requirement: null`，且方案明确为依赖维护 / 技术债，不新增用户可感能力。

结论：无 requirement 回写。

## 7. roadmap 回写

- [x] design frontmatter 包含 `roadmap: go-dependency-upgrade` 和 `roadmap_item: dep-coordinator-etcd-stack`。
- [x] `.codestable/roadmap/go-dependency-upgrade/go-dependency-upgrade-items.yaml` 中对应条目已从 `in-progress` 改为 `done`，feature 保持 `2026-06-04-dep-coordinator-etcd-stack`。
- [x] `.codestable/roadmap/go-dependency-upgrade/go-dependency-upgrade-roadmap.md` 第 5 节对应子 feature 已同步为 `done`，并写入 feature 目录名。
- [x] roadmap 主文档变更日志追加本 feature 完成记录。
- [x] items YAML 已通过 `python3 .codestable/tools/validate-yaml.py --file ... --yaml-only` 校验。

## 8. attention.md 候选盘点

- [x] 无候选。本 feature 未暴露需要补入 `.codestable/attention.md` 的新环境、工具或工作流约束。

## 9. 遗留

- 后续优化点：如目标是跟进现代 etcd client，应另起 roadmap/feature 评估 `go.etcd.io/etcd/*` module path、client API、lease/watch 语义和 server 兼容边界。
- 已知限制：`github.com/coreos/etcd` 旧 path 最新同路径版本仍停在 `v3.3.27+incompatible`；本次仅确认保留，不提升到现代 etcd client。
- 实现阶段顺手发现：无。
