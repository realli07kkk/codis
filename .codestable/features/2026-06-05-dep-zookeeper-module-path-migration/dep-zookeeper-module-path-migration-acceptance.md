---
doc_type: feature-acceptance
feature: 2026-06-05-dep-zookeeper-module-path-migration
status: accepted
accepted_at: 2026-06-05
summary: 验收 Zookeeper Go SDK module path 迁移、Client.Do breaking API 说明和构建测试闭环
tags: [go, modules, dependency-upgrade, zookeeper, coordinator, acceptance]
---

# dep-zookeeper-module-path-migration 验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-06-05
> 关联方案 doc：`.codestable/features/2026-06-05-dep-zookeeper-module-path-migration/dep-zookeeper-module-path-migration-design.md`

## 1. 接口契约核对

对照方案第 2.1 节名词层逐一核查。

**接口示例逐项核对**：

- [x] SDK import 示例：`pkg/models/zk/zkclient.go` 已从 `github.com/samuel/go-zookeeper/zk` 改为 `github.com/go-zookeeper/zk`。
  - 证据：`pkg/models/zk/zkclient.go:14`。
- [x] module diff 示例：`go.mod` 已删除 `github.com/samuel/go-zookeeper`，新增 direct require `github.com/go-zookeeper/zk v1.0.4`。
  - 证据：`go.mod:11`；`git diff -- go.mod go.sum pkg/models/zk/zkclient.go`。

**名词层“现状 → 变化”逐项核对**：

- [x] `module_set`：旧 `github.com/samuel/go-zookeeper` direct require 已移除，新 `github.com/go-zookeeper/zk v1.0.4` 已加入 direct require。
- [x] `pkg/models/zk.Client.Do`：方法签名随新 import path 使用 `*github.com/go-zookeeper/zk.Conn`；新增外部包风格 compile guard。
  - 证据：`pkg/models/zk/zkclient_api_test.go:14` 到 `pkg/models/zk/zkclient_api_test.go:17`。
- [x] `go.sum`：新增新 SDK checksum，并清理旧 SDK checksum。
  - 证据：`go.sum:77` 到 `go.sum:78`；旧 path 在 `go.sum` 无命中。
- [x] `models.Client`、`models.NewClient`、cmd 参数和配置语义未变。
  - 证据：diff 无 `pkg/models/client.go`、`cmd/internal/coordinator/args.go`、`cmd/` 配置语义变更。

**流程图核对**：

- [x] 图中节点均有实际落点：版本查询、旧 path 触达复核、import 迁移、module manifest 迁移、module graph 收口、`Client.Do` 迁移说明与 compile guard、target tests、默认 tests、范围守护均已执行。

## 2. 行为与决策核对

**需求摘要逐项验证**：

- [x] 所有 Go 组件使用的 Zookeeper SDK 已迁到 `github.com/go-zookeeper/zk v1.0.4`。
- [x] 仓库实现文件和 module 文件不再使用 `github.com/samuel/go-zookeeper`。
- [x] `Client.Do` 的源码级 breaking change 已通过 `doc/zookeeper_sdk_migration_zh.md` 明确说明。
- [x] coordinator 相关目标测试和默认 cmd/pkg 测试均通过。

**明确不做逐项核对**：

- [x] 未修改 `models.Client`、`models.NewClient`、dashboard/proxy/admin/fe coordinator 参数或配置语义。
- [x] 未修改 Zookeeper CRUD、watch、ephemeral、sequence、digest auth 或 reconnect 逻辑。
- [x] 未保留旧 `Client.Do(func(*github.com/samuel/go-zookeeper/zk.Conn) error)` 回调签名；该 breaking API 已文档化。
- [x] 未修改 etcd、filesystem、Consul coordinator 后端。
- [x] 未升级 Redis client、Martini、etcd、Consul、RDB parser、metrics、jemalloc 或其他 module。
- [x] 未使用长期 `replace` 桥接旧 path。
- [x] 未通过 unsafe、双连接或保留旧 SDK 伪装 `Client.Do` 兼容。
- [x] 未运行全量 `go mod tidy` 造成无关 module churn。
- [x] 未修改 `go 1.26.1` directive、jemalloc replace、`extern/redis-8.6.3/`、Docker、部署脚本或前端资源。

**关键决策落地**：

- [x] D1 迁移到最新正式 tag `v1.0.4`：`go list @latest` 和 `go.mod` 均指向 `github.com/go-zookeeper/zk v1.0.4`。
- [x] D2 不使用 `replace` 桥接旧 path：`go.mod` 只保留 jemalloc local replace。
- [x] D3 保持 Zookeeper coordinator 代码语义不变：`pkg/models/zk/zkclient.go` diff 只有 import path。
- [x] D4 target 验证覆盖 Jodis/coordinator 编译面：目标测试覆盖 `pkg/models/zk`、`pkg/models`、`cmd/internal/coordinator` 和相关 cmd 入口。
- [x] D5 不做全量 `go mod tidy`：module diff 只涉及 Zookeeper SDK path 和 checksum。
- [x] D6 显式声明 `Client.Do` breaking change：迁移文档和 compile guard 均已落地。

**编排层“现状 → 变化”逐项核对**：

- [x] 旧 path 触达链路由 `pkg/models/zk -> github.com/samuel/go-zookeeper/zk` 切换为 `pkg/models/zk -> github.com/go-zookeeper/zk`。
- [x] `go list -deps ./cmd/... ./pkg/...` 只命中新 path。
- [x] `doc/zookeeper_sdk_migration_zh.md` 说明外部调用方需要迁移 `Client.Do` 回调 import path。

**流程级约束核对**：

- [x] 错误语义：测试均通过，无需引入特殊错误映射。
- [x] 幂等性：重复运行验收命令未产生额外 module churn。
- [x] 兼容性：CLI/config/coordinator 运行期语义未变；`Client.Do` public API break 已显式文档化。
- [x] 可观测点：`go list`、`go mod why`、`go list -deps`、`rg`、`git diff`、target test、默认 test、`git status` 均已核对。

**挂载点反向核对（可卸载性）**：

- [x] `pkg/models/zk/zkclient.go` SDK import path：实际落点存在。
- [x] `go.mod` direct require：实际落点存在。
- [x] `go.sum` checksum：实际落点存在。
- [x] `doc/zookeeper_sdk_migration_zh.md`：`Client.Do` breaking API 迁移说明存在。
- [x] `pkg/models/zk/zkclient_api_test.go`：新签名 compile guard 存在。
- [x] 旧 path 反向 grep gate：`pkg cmd go.mod go.sum` 无旧 path 命中。
- [x] coordinator target test gate：已执行并通过。
- [x] 反向核查：本 feature 的业务代码挂载点均在清单内；迁移文档中旧 path 只作为用户迁移说明出现。
- [x] 拔除沙盘推演：回退 import、`go.mod` require、`go.sum` checksum，删除迁移文档和 compile guard 后，本迁移在系统视角消失。

## 3. 验收场景核对

- [x] **S1**：`go list -m -json github.com/go-zookeeper/zk@latest`。
  - 证据来源：Go module query。
  - 结果：`Path` 为 `github.com/go-zookeeper/zk`，`Version` 为 `v1.0.4`。

- [x] **S2**：`go list -m -versions -json github.com/go-zookeeper/zk`。
  - 证据来源：Go module query。
  - 结果：版本列表为 `v1.0.0` 到 `v1.0.4`，最高正式 tag 为 `v1.0.4`。

- [x] **S3**：旧 path 反向搜索。
  - 证据来源：`rg -n "github\\.com/samuel/go-zookeeper/zk|github\\.com/samuel/go-zookeeper" pkg cmd go.mod go.sum`。
  - 结果：无匹配。

- [x] **S4**：`go mod why -m github.com/go-zookeeper/zk`。
  - 证据来源：Go module graph。
  - 结果：可追溯到 `pkg/models/zk -> github.com/go-zookeeper/zk`。

- [x] **S5**：`go list -deps ./cmd/... ./pkg/...` 并 grep go-zookeeper。
  - 证据来源：Go dependency graph。
  - 结果：只触达 `github.com/go-zookeeper/zk`。

- [x] **S6**：检查 `go.mod` diff。
  - 证据来源：`git diff -- go.mod`。
  - 结果：删除旧 direct require，新增 `github.com/go-zookeeper/zk v1.0.4`；`go 1.26.1` 和 jemalloc replace 不变。

- [x] **S7**：检查 `go.sum` diff。
  - 证据来源：`git diff -- go.sum`。
  - 结果：新增新 SDK checksum，删除旧 SDK checksum，无无关 module churn。

- [x] **S8**：执行 `go test ./pkg/models/zk ./pkg/models ./cmd/internal/coordinator ./cmd/dashboard ./cmd/proxy ./cmd/admin ./cmd/fe`。
  - 证据来源：Go test。
  - 结果：通过。

- [x] **S9**：执行 `go test ./cmd/... ./pkg/...`。
  - 证据来源：Go test。
  - 结果：通过。

- [x] **S10**：重复验收后查看仓库状态和临时产物。
  - 证据来源：`git status --short --untracked-files=all`、`find`。
  - 结果：无 `vendor/`、`Godeps/`、`vendor/modules.txt` 或仓库内临时构建产物。

- [x] **S11**：检查 `doc/zookeeper_sdk_migration_zh.md`。
  - 证据来源：文档内容。
  - 结果：明确旧 `Client.Do` 回调签名是源码级 breaking change，并给出新 import path 示例。

- [x] **S12**：检查 `pkg/models/zk` 的 API compile guard。
  - 证据来源：`pkg/models/zk/zkclient_api_test.go` 和 `go test ./pkg/models/zk`。
  - 结果：外部包风格测试能编译 `var do func(func(*github.com/go-zookeeper/zk.Conn) error) error = client.Do`。

前端浏览器验证：不适用。本 feature 未改 FE 静态资源、HTML、JS、CSS 或可见 UI。

## 4. 术语一致性

- `Zookeeper module path migration`：设计和实现均指 SDK module/import path 从 `samuel` 迁到 `go-zookeeper`。
- `Target release`：实际目标为 `github.com/go-zookeeper/zk v1.0.4`。
- `Old-path upgrade feature`：历史旧 path pseudo version 升级未被误用为本 feature 完成证据。
- `Minimal module-path diff`：业务代码 diff 只改 import path；module diff 只改 Zookeeper SDK identity/checksum。
- `Client.Do breaking API`：迁移文档和 compile guard 均使用同一术语和新 SDK path。
- 防冲突：`pkg cmd go.mod go.sum` 无旧 `github.com/samuel/go-zookeeper` 命中；文档中的旧 path 只作为迁移说明出现。

## 5. 架构归并

对照方案第 4 节，本 feature 不新增运行期能力，不改变 `Coordinator / Store` 抽象、Zookeeper 后端外部语义、cmd 参数或 requirement 用户故事。

- [x] `.codestable/architecture/ARCHITECTURE.md`：不需要更新。
  - 理由：架构总入口只记录 `Coordinator / Store` 抽象和 `go.mod/go.sum` 是 Go module manifest 入口，不列具体 Zookeeper SDK module path；本次没有新模块、运行期状态、跨模块流程或 coordinator schema 变化。
- [x] public API 兼容性说明已写入 `doc/zookeeper_sdk_migration_zh.md`，比 architecture 更适合承载外部 Go 调用方迁移步骤。
- [x] `.codestable/attention.md`：不需要更新。
  - 理由：本次没有暴露新的项目通用命令陷阱；既有“不要全量 go mod tidy”和 `python3` 约束已覆盖。

## 6. requirement 回写

- [x] `requirement: null`，且本 feature 是依赖维护 / 技术债，不新增用户可感运行期能力。

结论：无 requirement 回写。

## 7. roadmap 回写

- [x] design frontmatter 的 `roadmap` 和 `roadmap_item` 均为 `null`。

结论：非 roadmap 起头，无 roadmap 回写。

## 8. attention.md 候选盘点

- [x] 无候选。本 feature 未暴露需要补入 `.codestable/attention.md` 的新环境、工具或工作流约束。

## 9. 遗留

- 后续优化点：`pkg/models/zk.Client.Do` 继续暴露第三方 concrete SDK 类型；本次已显式文档化 breaking API。如要彻底降低未来 SDK 迁移扩散，应另走 `cs-refactor` 设计新的不泄露 SDK concrete type 的 escape hatch。
- 已知限制：本次只验证编译和默认测试闭环，未启动真实外部 Zookeeper server 验证 watch、ephemeral、digest auth 或 session 行为。
- 实现阶段顺手发现：无。
