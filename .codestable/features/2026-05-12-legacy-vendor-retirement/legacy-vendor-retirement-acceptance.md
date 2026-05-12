---
doc_type: feature-acceptance
feature: 2026-05-12-legacy-vendor-retirement
status: current
accepted_at: 2026-05-12
summary: 旧 vendor/Godeps 依赖目录已退休，go.mod 收口到 go 1.26.1，Go modules 成为唯一常规依赖入口。
tags: [go, modules, vendor, build]
---

# legacy-vendor-retirement 验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-05-12
> 关联方案 doc：`.codestable/features/2026-05-12-legacy-vendor-retirement/legacy-vendor-retirement-design.md`

## 1. 接口契约核对

对照方案第 2.1 节名词层逐一核查：

**接口示例逐项核对**：
- [x] `test ! -d vendor && test ! -d Godeps`：实测通过，仓库根目录不再存在旧 GOPATH/Godeps 依赖目录。
- [x] `GO111MODULE=on go list -m`：返回 `github.com/CodisLabs/codis`，module path 未变化。
- [x] `grep -E "go 1\\.13|Temporary: keep go < 1\\.14|toolchain go1\\.26\\.1" go.mod`：无命中。
- [x] `GO111MODULE=on go list -m -json github.com/spinlock/jemalloc-go`：`Replace.Dir` 指向 `/Users/liyiming/gitcode-private/codis/third_party/jemalloc-go`，不含 `vendor`。

**名词层"现状 -> 变化"逐项核对**：
- [x] Legacy vendor source：`Godeps/` 与 `vendor/` 均已删除；没有新增 `archive/`、`doc/vendor-archive/` 或 `vendor/modules.txt`。
- [x] Go module manifest：`go.mod` 当前为 `module github.com/CodisLabs/codis` + `go 1.26.1`，保留现有 `require` / `replace` 关系；Go 1.26 机械要求的 indirect require 已显式分组，未升级现有依赖版本。
- [x] Jemalloc module source：`third_party/jemalloc-go` 未改动，仍是 `github.com/spinlock/jemalloc-go` 的 local replace 来源。

**流程图核对**：
- [x] 删除 `vendor/` / `Godeps/` -> `go.mod` 改为 `go 1.26.1` -> module mode 解析 -> `third_party/jemalloc-go` replace -> 构建测试通过，所有节点都有实际文件或命令证据。

## 2. 行为与决策核对

**需求摘要逐项验证**：
- [x] 删除顶层 `vendor/` 与 `Godeps/`：实测目录不存在，git status 显示 402 个 tracked 删除。
- [x] `go.mod` 临时规避解除：`go.mod` 已收口到 `go 1.26.1`，无 `go 1.13` / `toolchain go1.26.1`。
- [x] 默认测试、`cgo_jemalloc` proxy 构建、Makefile 测试入口均已通过。

**明确不做逐项核对**：
- [x] 未处理 `Dockerfile`、`scripts/docker.sh`、`kubernetes/`、`ansible/`。
- [x] 未更新 README 或 `doc/` 用户文档；`.codestable/attention.md` 仅按用户本轮明确要求更新 Go modules 启动注意事项和 `go mod tidy` 命令陷阱。
- [x] 未修改 `cmd/`、`pkg/`、`extern/` 运行逻辑。
- [x] 未修改 `third_party/jemalloc-go/`。
- [x] 未升级现有依赖版本，未执行全量 `go mod tidy` 收口。
- [x] 未生成 `vendor/modules.txt` 或 `go mod vendor` 输出。
- [x] 未删除或改向 `replace github.com/spinlock/jemalloc-go => ./third_party/jemalloc-go`。

**关键决策落地**：
- [x] 删除旧目录，不做源码级归档目录：已删除 `vendor/` / `Godeps/`，无替代归档目录。
- [x] `go.mod` 回到长期契约 `go 1.26.1`：已落地。
- [x] 不重新求解依赖图：没有全量 tidy 结果写入；仅保留 Go 1.26 当前命令要求的 indirect require 显式化。
- [x] 保留 `third_party/jemalloc-go`：目录未改，module graph 指向正确。

**编排层"现状 -> 变化"逐项核对**：
- [x] 实施顺序符合方案：先删除旧目录，再提升 `go.mod`，避免在旧 vendor 存在时触发自动 vendor mode。
- [x] 验证顺序符合方案：module graph -> 默认测试 -> cgo build -> Makefile 测试。

**流程级约束核对**：
- [x] 未出现 `vendor/modules.txt`、`-mod=vendor` 或从 `vendor/` 解析依赖的现象。
- [x] 重复验收命令后没有生成 `vendor/`、`Godeps/`、`vendor/modules.txt`。
- [x] 运行期行为不变：无 `cmd/`、`pkg/`、`extern/` diff。

**挂载点反向核对（可卸载性）**：
- [x] 挂载点 M1 `vendor/`：已删除；恢复它会重新引入自动 vendor mode 风险。
- [x] 挂载点 M2 `Godeps/`：已删除；恢复它会重新暗示 Godeps 是支持入口。
- [x] 挂载点 M3 `go.mod` Go 版本声明：已从临时 `go 1.13` + `toolchain go1.26.1` 改为 `go 1.26.1`。
- [x] 反向核查：`git diff --name-only -- Dockerfile scripts/docker.sh kubernetes ansible README.md doc cmd pkg extern third_party go.sum` 无输出。
- [x] 拔除沙盘推演：恢复 `vendor/` / `Godeps/` 或 `go 1.13` 即回退本 feature 的核心效果；删除这三个挂载点变化后无其他隐藏挂载点残留。

## 3. 验收场景核对

- [x] **S1**：`test ! -d vendor && test ! -d Godeps`
  - 证据来源：手工命令
  - 结果：通过。

- [x] **S2**：检查 `go.mod`
  - 证据来源：`rg "go 1\\.26\\.1|replace github.com/spinlock/jemalloc-go => ./third_party/jemalloc-go" go.mod`，以及旧临时字段 grep 无命中
  - 结果：通过。

- [x] **S3**：`GO111MODULE=on go list -m -json github.com/spinlock/jemalloc-go`
  - 证据来源：命令输出
  - 结果：通过，`Replace.Dir` 指向 `third_party/jemalloc-go`。

- [x] **S4**：`GO111MODULE=on go test ./cmd/... ./pkg/...`
  - 证据来源：实现阶段命令
  - 结果：通过，不报 `vendor/modules.txt` 或 `-mod=vendor`。

- [x] **S5**：`GO111MODULE=on go build -tags cgo_jemalloc -o /tmp/codis-proxy-cgo-test ./cmd/proxy`
  - 证据来源：实现阶段命令
  - 结果：通过。

- [x] **S6**：`make gotest`
  - 证据来源：实现阶段命令
  - 结果：通过。

- [x] **S7**：重复执行验收命令后查看 `git status --short`
  - 证据来源：工作区检查
  - 结果：未生成 `vendor/`、`Godeps/`、`vendor/modules.txt`；仅有预期删除、`go.mod`、CodeStable 文档和用户批准的 attention 更新。

## 4. 术语一致性

- `Legacy vendor source`：对应 `vendor/` / `Godeps/`，实际已删除。
- `Module dependency source`：对应 `go.mod` / `go.sum` 与 `third_party/jemalloc-go` replace，实际一致。
- `Vendor retirement`：未生成 `vendor/modules.txt`，未保留源码级归档目录，实际一致。
- `Final go directive`：`go.mod` 使用 `go 1.26.1`，实际一致。
- `Migration evidence`：保留在 `go.mod/go.sum`、CodeStable 文档和 git history 中，实际一致。
- 防冲突：未引入新运行期术语或替代依赖入口。

## 5. 架构归并

- [x] `.codestable/architecture/ARCHITECTURE.md`：已更新 `Go module manifest` 术语，记录 `go.mod` 使用 `go 1.26.1`，默认 cmd/pkg、`cgo_jemalloc` proxy 构建和 Makefile 测试入口已接通。
- [x] `.codestable/architecture/ARCHITECTURE.md`：已更新构建层结构描述，说明旧 `vendor/` / `Godeps/` 已退休，常规 Go 依赖由 `go.mod/go.sum` 解析，`cgo_jemalloc` 仍通过 `third_party/jemalloc-go` local replace。
- [x] `.codestable/architecture/ARCHITECTURE.md`：已更新已知约束，移除旧 `go 1.13` 临时规避描述，改为后续由 `module-migration-doc-notes` 收尾工程说明。

## 6. requirement 回写

- [x] 方案 frontmatter `requirement: null`，本 feature 是构建系统迁移收尾，不新增 Redis/Codis 用户可见能力。
- [x] `redis-cluster-service` 的用户故事、边界和 pitch 未变化。

结论：无 requirement 回写。

## 7. roadmap 回写

- [x] design frontmatter：`roadmap: go-mod-migration` / `roadmap_item: legacy-vendor-retirement`。
- [x] `.codestable/roadmap/go-mod-migration/go-mod-migration-items.yaml`：对应条目已从 `in-progress` 改为 `done`，并保留 `feature: 2026-05-12-legacy-vendor-retirement`，YAML 校验通过。
- [x] `.codestable/roadmap/go-mod-migration/go-mod-migration-roadmap.md`：第 4.1 节 module manifest 契约已移除过渡态；第 5 节子 feature 清单中 `legacy-vendor-retirement` 已同步为 `done`。

## 8. attention.md 候选盘点

- [x] 已按用户本轮明确要求写入 `.codestable/attention.md`：
  - Go modules 已是默认依赖入口，不要恢复旧 GOPATH/vendor 构建路径。
  - 不要顺手现代化 Go 依赖，版本偏离必须有可复现编译原因。
  - 不要用全量 `go mod tidy` 收口本仓库；更新 `go.mod` 时优先用验收命令驱动最小机械变化。
  - 旧 `vendor/` / `Godeps/` 已退休，不要恢复。

无额外 attention 候选。

## 9. 遗留

- 后续优化点：`module-migration-doc-notes` 仍需更新 README / docs / 工程说明中关于默认 Go modules 构建的叙述。
- 已知限制：`Dockerfile`、`scripts/docker.sh`、`kubernetes/` 的 GOPATH 路径仍不在本 roadmap 当前完成范围内。
- 实现阶段顺手发现：全量 `go mod tidy` 会扫描 etcd 依赖测试链路并拉入非运行范围依赖；已写入 attention，避免后续重复踩坑。
