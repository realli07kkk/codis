---
doc_type: feature-acceptance
feature: 2026-05-12-jemalloc-module-build
status: current
accepted_at: 2026-05-12
summary: cgo_jemalloc proxy 构建路径已在 Go modules 模式下接通，并改由 third_party/jemalloc-go 的本地 replace 模块提供源码来源。
tags: [go, modules, cgo, jemalloc, build]
---

# jemalloc-module-build 验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-05-12
> 关联方案 doc：`.codestable/features/2026-05-12-jemalloc-module-build/jemalloc-module-build-design.md`

## 1. 接口契约核对

对照方案第 2.1 节名词层逐一核查：

**接口示例逐项核对**：
- [x] `third_party/jemalloc-go/jemalloc.go`：`Malloc(size int) unsafe.Pointer` / `Free(ptr unsafe.Pointer)` 已实际提供，签名与方案一致。
- [x] `pkg/utils/unsafe2/je_malloc.go`：仍通过 `import "github.com/spinlock/jemalloc-go"` 调用 `jemalloc.Malloc` / `jemalloc.Free`，import path 未变化。

**名词层“现状 → 变化”逐项核对**：
- [x] `go.mod`：已新增 `github.com/spinlock/jemalloc-go v0.0.0-20161230074307-26719b2ee618` 的 `require`，并添加 `replace => ./third_party/jemalloc-go`。
- [x] `third_party/jemalloc-go`：已新增本地 module source，包含 `go.mod`、Go wrapper、头文件目录和 `je_*.c` 源文件。
- [x] `vendor/github.com/spinlock/jemalloc-go`：未再作为 module mode 的依赖解析入口；本 feature 没有改动该目录。

**流程图核对**：
- [x] `go.mod` -> `replace` -> `third_party/jemalloc-go` -> `pkg/utils/unsafe2/je_malloc.go` -> `cmd/proxy` 的节点均有实际代码或构建命令落点。

## 2. 行为与决策核对

对照方案第 1 节 + 第 2.2 节：

**需求摘要逐项验证**：
- [x] `GO111MODULE=on go build -tags cgo_jemalloc -o /tmp/codis-proxy-cgo-test ./cmd/proxy` 已通过，证明 `cgo_jemalloc` proxy 构建路径在 module mode 下可用。
- [x] `GO111MODULE=on go test -tags cgo_jemalloc ./pkg/utils/unsafe2` 已通过，证明 allocator 来源切换后 `unsafe2` 行为仍成立。

**明确不做逐项核对**：
- [x] 未修改 `Dockerfile`、`scripts/docker.sh`、`kubernetes/` 或部署路径。
- [x] 未删除 `vendor/` 或 `Godeps/`。
- [x] 未把 `Makefile` 改成完整 module mode 入口。
- [x] 未提升 `go.mod` 的 `go 1.13` 临时 directive，也未生成 `vendor/modules.txt`。
- [x] 未改 proxy 路由、Redis 协议或 offheap slice 的对外语义 / 内存限额逻辑。

**关键决策落地**：
- [x] 使用 `third_party/jemalloc-go` + `go.mod replace`：已落在 `go.mod` 和 `third_party/jemalloc-go/go.mod`。
- [x] 保持 `pkg/utils/unsafe2/je_malloc.go` import path 不变：代码保持 `github.com/spinlock/jemalloc-go`。
- [x] 不引入 vendor mode：构建链路通过 local replace 完成，没有 `vendor/modules.txt`。
- [x] build-ready 文件为 tracked source：重复构建后没有生成新的 `je_*.c` / `jemalloc` / `VERSION` 工作区污染。

**编排层“现状 → 变化”逐项核对**：
- [x] `GO111MODULE=on go list -tags cgo_jemalloc -deps ./cmd/proxy` 已能解析完整依赖图，不再停在找不到 `github.com/spinlock/jemalloc-go`。
- [x] `cgo_jemalloc` 构建从“依赖旧 vendor 预处理状态”切换为“依赖 `third_party/jemalloc-go` 的 local replace 模块”。

**流程级约束核对**：
- [x] 依赖缺失时仍由 `go build` 直接失败，不存在静默 fallback 到 `C.malloc` 的新逻辑。
- [x] 先执行 `go test -tags cgo_jemalloc ./pkg/utils/unsafe2`，再执行 proxy 构建，顺序符合方案。
- [x] `MakeSlice` / `MakeOffheapSlice` / `FreeSlice` 的对外行为通过 `pkg/utils/unsafe2` 测试保持不变。

**挂载点反向核对（可卸载性）**：
- [x] `go.mod`：新增 `require` / `replace`，是 module mode 解析 `cgo_jemalloc` 的入口。
- [x] `third_party/jemalloc-go`：提供 `github.com/spinlock/jemalloc-go` 的本地源码来源。
- [x] `pkg/utils/unsafe2/je_malloc.go`：保留 `cgo_jemalloc` build tag 和 import path。
- [x] 反向 grep：代码侧 `github.com/spinlock/jemalloc-go` 只落在 `go.mod`、`third_party/jemalloc-go` 和 `pkg/utils/unsafe2/je_malloc.go`，没有超出清单的运行挂载点。
- [x] 拔除沙盘推演：删除 `go.mod` 的 `replace` 或移除 `third_party/jemalloc-go` 会回到本 feature 之前的 module 解析失败；移除 `je_malloc.go` 的 build tag 路径则 `cgo_jemalloc` 能力消失。

## 3. 验收场景核对

- [x] **S1**：执行 `GO111MODULE=on go list -m -json github.com/spinlock/jemalloc-go`
  - 证据来源：`/tmp/jemalloc-module.json`
  - 结果：通过，`Replace.Dir` 指向 `/Users/liyiming/gitcode-private/codis/third_party/jemalloc-go`。

- [x] **S2**：执行 `GO111MODULE=on go list -tags cgo_jemalloc -deps ./cmd/proxy`
  - 证据来源：`/tmp/jemalloc-proxy-deps.txt`
  - 结果：通过，输出 253 条依赖并包含 `github.com/spinlock/jemalloc-go`。

- [x] **S3**：执行 `GO111MODULE=on go test -tags cgo_jemalloc ./pkg/utils/unsafe2`
  - 证据来源：包测试命令
  - 结果：通过。

- [x] **S4**：执行 `GO111MODULE=on go build -tags cgo_jemalloc -o /tmp/codis-proxy-cgo-test ./cmd/proxy`
  - 证据来源：构建命令
  - 结果：通过。

- [x] **S5**：重复执行 `cgo_jemalloc` 构建后查看 `git status`
  - 证据来源：重复构建 + 工作区检查
  - 结果：通过，工作区只包含本 feature 预期的 `go.mod` 和 `third_party/jemalloc-go` 变更。

- [x] **S6**：执行 `GO111MODULE=on go test ./cmd/... ./pkg/...`
  - 证据来源：默认构建标签全量测试
  - 结果：通过。

## 4. 术语一致性

- `cgo_jemalloc`：代码和文档统一指 `pkg/utils/unsafe2/je_malloc.go` 的 build tag 路径。
- `third_party/jemalloc-go`：文档、`go.mod replace` 和代码目录命名一致。
- `github.com/spinlock/jemalloc-go`：运行代码 import path 与 roadmap 契约一致。
- 防冲突：未引入新的平行术语或替代命名。

## 5. 架构归并

- [x] `.codestable/architecture/ARCHITECTURE.md`：已补充 `Go module manifest` 和构建层现状，说明 `cgo_jemalloc` 现已通过 `third_party/jemalloc-go` 的 local replace 模块接通。
- [x] `.codestable/architecture/ARCHITECTURE.md`：已更新已知约束，改为“默认 cmd/pkg 和 `cgo_jemalloc` 已接通，后续仅剩 Makefile module mode 与旧 vendor/Godeps 清理”。

## 6. requirement 回写

- [x] `requirement: null`，本 feature 只调整构建链路，不新增 Redis/Codis 用户可见能力。
- [x] `redis-cluster-service` 的用户故事、边界和 pitch 未变化，无 requirement 回写。

## 7. roadmap 回写

- [x] `roadmap: go-mod-migration` / `roadmap_item: jemalloc-module-build` 已对齐。
- [x] `.codestable/roadmap/go-mod-migration/go-mod-migration-items.yaml` 已从 `in-progress` 改为 `done`，并通过 YAML 校验。
- [x] `.codestable/roadmap/go-mod-migration/go-mod-migration-roadmap.md` 已同步主文档状态和 feature 链接。

## 8. attention.md 候选盘点

- [x] 有候选：
  - 候选 1：`cgo_jemalloc` 的 module mode 来源现在是 `go.mod` 的 `replace github.com/spinlock/jemalloc-go => ./third_party/jemalloc-go`；后续相关改动应改 `third_party/jemalloc-go`，不是旧 `vendor/github.com/spinlock/jemalloc-go`。

## 9. 遗留

- 后续优化点：
  - `makefile-module-mode`：让 `Makefile` 不再进入旧 `vendor/github.com/spinlock/jemalloc-go`。
  - `legacy-vendor-retirement`：清理旧 `vendor/` / `Godeps/`，再提升 `go.mod` 的 `go` directive。
- 已知限制：
  - `go.mod` 的 `go 1.13` 仍是迁移过渡态，不是长期最低版本承诺。
  - `third_party/jemalloc-go` 当前是受控本地镜像，未来若要升级 upstream 版本，需要单独做 feature 或 roadmap update。
  - 实现阶段顺手发现：`Makefile` 仍保留旧 vendor jemalloc 预处理路径，但已由 roadmap 下一条 `makefile-module-mode` 承接。
