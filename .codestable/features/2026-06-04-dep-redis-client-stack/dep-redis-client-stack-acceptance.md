---
doc_type: feature-acceptance
feature: 2026-06-04-dep-redis-client-stack
status: current
accepted_at: 2026-06-04
summary: Redis client 依赖 github.com/garyburd/redigo 已同路径升级到 v1.6.4，并完成 Redis client、topom 和默认 cmd/pkg 测试闭环。
tags: [go, modules, dependency-upgrade, redigo, redis-client]
---

# dep-redis-client-stack 验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-06-04
> 关联方案 doc：`.codestable/features/2026-06-04-dep-redis-client-stack/dep-redis-client-stack-design.md`

## 1. 接口契约核对

对照方案第 2.1 节名词层逐一核查。

**接口示例逐项核对**：
- [x] `go.mod` direct require：`github.com/garyburd/redigo` 已从 `v1.0.1-0.20170208211623-48545177e92a` 升级到 `v1.6.4`，实际落点 `go.mod:10`。
- [x] `go.sum` checksum：新增 `github.com/garyburd/redigo v1.6.4` 的 content checksum 与 `go.mod` checksum，实际落点 `go.sum:54` 到 `go.sum:55`。
- [x] import path：代码仍使用 `github.com/garyburd/redigo/redis`，没有迁移到 `github.com/gomodule/redigo/redis`。

**名词层"现状 -> 变化"逐项核对**：
- [x] `module_set`：只覆盖 `github.com/garyburd/redigo`。
- [x] `scope`：保持 direct require。
- [x] `upgrade_mode`：按方案执行 `direct-go-get`，没有运行全量 `go mod tidy`。
- [x] checksum lockfile：旧版本 checksum 保留，新版本 checksum 新增，没有清理历史 checksum。
- [x] import surface：`pkg/utils/redis/client.go`、`pkg/utils/redis/sentinel.go` 仍是默认 cmd/pkg 内唯一 direct import；`extern/deprecated/redis-test` 只作为触点观察，未改动。

**流程图核对**：
- [x] 版本查询节点：`github.com/garyburd/redigo@latest` 返回 tagged `v1.6.4`。
- [x] deprecated path 记录节点：`go list -m -u -json github.com/garyburd/redigo` 在升级后仍返回 `Deprecated: Use github.com/gomodule/redigo instead.`。
- [x] 定点升级节点：`go.mod` 只改 redigo 一条版本声明。
- [x] checksum 节点：`go.sum` 只新增 redigo `v1.6.4` 两条 checksum。
- [x] import surface 节点：`go list -deps ./cmd/... ./pkg/...` 仍只触达 `github.com/garyburd/redigo/internal` 和 `github.com/garyburd/redigo/redis`。
- [x] 测试节点：`go test ./pkg/utils/redis`、`go test ./pkg/topom`、`go test ./cmd/... ./pkg/...` 均通过。

## 2. 行为与决策核对

**需求摘要逐项验证**：
- [x] Redis client stack 依赖已升级到 `github.com/garyburd/redigo v1.6.4`。
- [x] Redis 连接、password/named `AUTH`、`SELECT`、`INFO`/`CONFIG`、`SLOTSINFO`、同步/异步迁移返回解析由 `go test ./pkg/utils/redis` 覆盖并通过。
- [x] Topom stats、slot action、ACL/Sentinel 相关 Redis client 调用路径由 `go test ./pkg/topom` 覆盖并通过。
- [x] 默认 `cmd/pkg` 构建测试由 `go test ./cmd/... ./pkg/...` 覆盖并通过。

**明确不做逐项核对**：
- [x] 未迁移 import path 到 `github.com/gomodule/redigo/redis`。
- [x] 未替换 Redis client library，未引入 go-redis 或其他新 client。
- [x] 未修改 `pkg/utils/redis.Client` / `Pool` / `InfoCache` / `Sentinel` 的公开方法、错误包装、pipeline 计数、连接复用或 timeout 语义。
- [x] 未修改 `RedisAuthIdentity`、`AUTH [username] password`、replication `masteruser/masterauth`、ACL 同步或 migration auth 语义。
- [x] 未修改 slot migration 编排、`SLOTSMGRT*` 参数、返回解析或 Redis 8 adapter 行为。
- [x] 未修改 `pkg/proxy/redis` RESP codec、proxy session、本地 `AUTH`/`SELECT`、Stream、hot key cache 或 ACL 路由。
- [x] 未把 proxy `EVAL` / `EVALSHA` 纳入 redigo script 调用路径；grep 证实没有 `redigo.Script` / `NewScript` 用法。
- [x] 未修改 `extern/deprecated/redis-test`，也未把该目录既有失败作为本 feature gate。
- [x] 未升级 Martini、coordinator、RDB parser、metrics、jemalloc 或其他 roadmap 子 feature 覆盖的 module。
- [x] 未改变 `go 1.26.1` module directive，未修改 `third_party/jemalloc-go`、`extern/redis-8.6.3`、Docker、部署脚本、前端资源或配置模板。

**关键决策落地**：
- [x] 决策 1：采用旧 module path `@latest` tagged release `v1.6.4`。实际 `go.mod:10` 已落地。
- [x] 决策 2：暂不迁移到 `github.com/gomodule/redigo`。`rg "github.com/gomodule/redigo"` 无代码命中。
- [x] 决策 3：只改 manifest，不改 Redis client 封装代码。`git diff --name-status` 中没有 `pkg/utils/redis` 或 Go 源码文件。
- [x] 决策 4：`extern/deprecated/redis-test` 仅观察不验收。实际未修改 `extern/`。

**编排层"现状 -> 变化"逐项核对**：
- [x] 执行顺序符合方案：版本查询 -> deprecated path 记录 -> 定点升级 -> diff 守护 -> Redis client 包测试 -> topom 测试 -> 默认测试 -> 范围守护。
- [x] `go.mod/go.sum` 最小变化成立：除 CodeStable/roadmap 文档外，代码侧 diff 只有 `go.mod` 和 `go.sum`。
- [x] post-upgrade `go list -m -u` 不再显示 `Update` 字段，因为当前已是 `v1.6.4`；旧版本到目标版本由 `go.mod` diff 和实现阶段查询记录证明。

**流程级约束核对**：
- [x] 未先迁移 module path，未运行全量依赖整理。
- [x] 测试失败时不需要补丁分支；所有测试一次按目标版本通过。
- [x] 重复验收命令没有继续改动 `go.mod/go.sum`，也没有生成 `vendor/`、`Godeps/`、`vendor/modules.txt`。
- [x] Redis 管理命令、ACL/migration/auth 语义、proxy RESP codec、coordinator 和运行配置保持不变。

**挂载点反向核对（可卸载性）**：
- [x] 挂载点 M1 `go.mod` redigo direct require：实际落点 `go.mod:10`。
- [x] 挂载点 M2 `go.sum` redigo `v1.6.4` checksum：实际落点 `go.sum:54` 到 `go.sum:55`。
- [x] 挂载点 M3 `pkg/utils/redis` target test gate：实际命令 `go test ./pkg/utils/redis` 通过。
- [x] 挂载点 M4 `pkg/topom` target test gate：实际命令 `go test ./pkg/topom` 通过。
- [x] 挂载点 M5 默认 cmd/pkg test gate：实际命令 `go test ./cmd/... ./pkg/...` 通过。
- [x] 反向核查：`git diff --name-status` 除 CodeStable/roadmap 文档外只包含 `go.mod`、`go.sum`，没有清单外代码挂载点。
- [x] 拔除沙盘推演：回退 `go.mod:10` 并删除 `go.sum:54` 到 `go.sum:55` 即可拔除本 feature 的依赖升级效果；没有 Go 源码残留。

## 3. 验收场景核对

- [x] **S1**：`go list -m -json github.com/garyburd/redigo@latest` 返回 tagged `v1.6.4`
  - 证据来源：手工命令
  - 结果：通过。

- [x] **S2**：`go list -m -u -json github.com/garyburd/redigo` 记录 deprecated path note
  - 证据来源：手工命令
  - 结果：通过。升级后当前版本已是 `v1.6.4`，不再显示 `Update` 字段；旧版本到新版本由 `go.mod` diff 证明。

- [x] **S3**：`go list -m -versions -json github.com/garyburd/redigo` 版本列表包含 `v1.6.4`，且不选择 deprecated marker tag
  - 证据来源：手工命令
  - 结果：通过。

- [x] **S4**：`go list -m -json github.com/gomodule/redigo@latest` 只作为观察记录
  - 证据来源：手工命令 + grep
  - 结果：通过，未切换 module path。

- [x] **S5**：定点 `go get` 后检查 `go.mod`
  - 证据来源：`git diff -- go.mod`
  - 结果：通过，只有 redigo 改到 `v1.6.4`；`go 1.26.1` 和 jemalloc replace 不变。

- [x] **S6**：检查 `go.sum` diff
  - 证据来源：`git diff -- go.sum`
  - 结果：通过，只新增 redigo `v1.6.4` 两条 checksum。

- [x] **S7**：`go mod why -m github.com/garyburd/redigo`
  - 证据来源：手工命令
  - 结果：通过，仍可追溯到旧 import path；`extern/deprecated/redis-test` 仅作为观察触点。

- [x] **S8**：`go list -deps ./cmd/... ./pkg/... | rg "github.com/garyburd/redigo"`
  - 证据来源：手工命令
  - 结果：通过，默认 cmd/pkg 依赖仍触达 `github.com/garyburd/redigo/internal` 和 `github.com/garyburd/redigo/redis`。

- [x] **S9**：`go test ./pkg/utils/redis`
  - 证据来源：Go 测试
  - 结果：通过。

- [x] **S10**：`go test ./pkg/topom`
  - 证据来源：Go 测试
  - 结果：通过。

- [x] **S11**：搜索 `redigo.Script`、`NewScript`、`EVAL`、`EVALSHA` 与 Sentinel script 配置调用面
  - 证据来源：`rg`
  - 结果：通过，只覆盖 Sentinel `notification-script` / `client-reconfig-script` 配置透传；proxy `EVAL` / `EVALSHA` 不走 redigo。

- [x] **S12**：`go test ./cmd/... ./pkg/...`
  - 证据来源：Go 测试
  - 结果：通过。

- [x] **S13**：重复验收命令后查看仓库状态
  - 证据来源：`git diff --name-status`、`find . -maxdepth 2 ... vendor/Godeps ...`
  - 结果：通过，未生成 `vendor/`、`Godeps/`、`vendor/modules.txt` 或仓库内临时构建产物。

## 4. 术语一致性

- `Redis client stack`：仅作为 CodeStable spec 中本 feature 分组名；代码未新增同名概念。
- `Target module version`：已落实为 `go.mod:10` 的 `github.com/garyburd/redigo v1.6.4`。
- `Deprecated module path note`：验收报告和 design 均明确记录旧路径 deprecated，但本 feature 不迁移到 `github.com/gomodule/redigo`。
- `Minimal module diff`：实际 diff 符合，仅 redigo 版本声明和 checksum 变化。
- 防冲突：代码没有新增术语、类型、函数或抽象；无需额外命名修正。

## 5. 架构归并

- [x] `.codestable/architecture/ARCHITECTURE.md`：不需要更新。理由：架构文档已经记录 `go.mod/go.sum` 是 Go modules 依赖入口、旧 vendor/Godeps 已退休、默认 cmd/pkg module mode 可测试；本次只更新 redigo 外部 module 版本，没有新增模块、接口、运行期流程或跨 feature 稳定约束。
- [x] 架构总入口无需新增链接。理由：本次是 roadmap 子 feature 进度和 lockfile 变化，持久证据已在 `go.mod/go.sum`、feature acceptance 和 roadmap 中。

## 6. requirement 回写

- [x] `requirement` 为空，且方案第 4 节明确本 feature 不新增运行期能力：跳过 requirement 回写。
- [x] `redis-cluster-service` 与 `platform-release-artifacts` 的用户故事、边界和 pitch 均未变化；本 feature 是依赖维护，不改变运行能力或发布产物结构。

## 7. roadmap 回写

- [x] design frontmatter 包含 `roadmap: go-dependency-upgrade` 与 `roadmap_item: dep-redis-client-stack`。
- [x] `.codestable/roadmap/go-dependency-upgrade/go-dependency-upgrade-items.yaml`：对应条目已从 `in-progress` 改为 `done`，并保留 `feature: 2026-06-04-dep-redis-client-stack`。
- [x] `.codestable/roadmap/go-dependency-upgrade/go-dependency-upgrade-roadmap.md`：第 5 节子 feature 清单已同步为 `状态：done` 和对应 feature 目录，变更日志已追加本条完成记录。
- [x] YAML 校验：`python3 .codestable/tools/validate-yaml.py --file .codestable/roadmap/go-dependency-upgrade/go-dependency-upgrade-items.yaml --yaml-only` 通过。

## 8. attention.md 候选盘点

- [x] 本 feature 未暴露需要补入 attention.md 的新内容。已有注意事项已经覆盖关键约束：不要全量 `go mod tidy`、Go modules 是默认依赖入口、使用 `python3` 运行 `.codestable/tools/*.py`。

## 9. 遗留

- 后续优化点：`github.com/garyburd/redigo` 旧 module path 已 deprecated；若后续要继续跟进 redigo 新版本，应另起 feature 评估迁移到 `github.com/gomodule/redigo`。
- 已知限制：`make gotest` 未在本 feature 中运行；按 roadmap 第 4.3 节，批量阶段收口时再运行 `make gotest`。
- 实现阶段顺手发现：无。
