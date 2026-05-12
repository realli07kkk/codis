---
doc_type: feature-acceptance
feature: 2026-05-11-go-module-compile-baseline
status: current
accepted_at: 2026-05-12
summary: Go modules 最小编译闭环已建立，cmd/pkg 默认构建标签下可在 module mode 编译测试。
tags: [go, modules, build]
---

# go-module-compile-baseline 验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-05-12
> 关联方案 doc：`.codestable/features/2026-05-11-go-module-compile-baseline/go-module-compile-baseline-design.md`

## 1. 接口契约核对

对照方案第 2.1 节名词层逐一核查：

**接口示例逐项核对**：
- [x] Go module manifest：`GO111MODULE=on go test ./cmd/... ./pkg/...` 从 `go.mod/go.sum` 解析依赖，实际通过，不再报 `directory prefix cmd does not contain main module`。
- [x] Version metadata：`pkg/utils/version.go` 提供 `utils.Version` / `utils.Compile` 默认变量，`go build ./cmd/... ./pkg/...` 在未依赖 `bash version` 覆盖源码的情况下通过。

**名词层“现状 -> 变化”逐项核对**：
- [x] `go.mod` / `go.sum`：仓库根目录已新增，`go.mod` 模块名为 `github.com/CodisLabs/codis`，`go.sum` 由验收范围内的 module mode 测试重新生成。
- [x] `version metadata`：从“脚本生成缺失文件”改为“源码默认变量 + Makefile ldflags 注入真实值”。`bash version` 后 `pkg/utils/version.go` 不再变脏。
- [x] `cgo_jemalloc`：默认构建标签下未要求 `github.com/spinlock/jemalloc-go` module 化，仍留给 roadmap 后续条目。

**流程图核对**：
- [x] Clean checkout -> `go.mod/go.sum` -> `pkg/utils` 兜底 -> `GO111MODULE=on go test ./cmd/... ./pkg/...` 的节点均有代码落点：`go.mod`、`go.sum`、`pkg/utils/version.go` 和测试命令证据。
- [x] 依赖不兼容分支已记录：`github.com/ugorji/go v1.2.14` 避免旧 codec 初始化 panic；`github.com/coreos/etcd v3.3.27+incompatible` 避免 `v3.0.17` generated codec helper 编译失败。

## 2. 行为与决策核对

**需求摘要逐项验证**：
- [x] 在仓库根目录执行 module mode 测试：`GO111MODULE=on go test ./cmd/... ./pkg/...` 已通过。
- [x] 默认构建标签下编译：`GO111MODULE=on go build ./cmd/... ./pkg/...` 已通过。
- [x] 版本元数据 clean checkout 兜底：`pkg/utils.Version` / `pkg/utils.Compile` 由源码定义；Makefile 通过 `bin/version.ldflags` 注入真实值。

**明确不做逐项核对**：
- [x] 未处理 Dockerfile：diff 不包含 `Dockerfile`。
- [x] 未删除 `vendor/` 或 `Godeps/`：diff 不包含删除或迁移这两个目录。
- [x] 未改 proxy/topom/models 运行逻辑绕过测试：改动只在 `pkg/proxy/proxy_test.go` 和 `pkg/topom/topom_test.go` 的测试监听地址。
- [x] 未改 jemalloc 源码或 `pkg/utils/unsafe2/je_malloc.go`。
- [x] 未生成 `vendor/modules.txt`，也没有把本 feature 改成 vendor mode 方案。
- [x] Makefile 仅为版本注入和现代 Go 去掉失效 `-i` 参数做最小调整，完整 module mode Makefile 迁移仍留给 `makefile-module-mode`。

**关键决策落地**：
- [x] 模块名固定为 `github.com/CodisLabs/codis`：见 `go.mod`。
- [x] 临时 `go 1.13` + `toolchain go1.26.1`：见 `go.mod` 注释，已说明这是旧 vendor 清理前的过渡，不是长期最低版本承诺。
- [x] 依赖版本按源码 import 和 Godeps baseline 收敛：`go.mod` 保留必要依赖；`etcd v3.0.17` 降级实测失败，保留 `v3.3.27+incompatible`。
- [x] version metadata 所有权明确：`pkg/utils/version.go` 是源码默认值，`version` 脚本只生成 `bin/version` 和 `bin/version.ldflags`。

**编排层“现状 -> 变化”逐项核对**：
- [x] 从无 module manifest 变为存在 `go.mod/go.sum`。
- [x] 从缺失生成文件导致编译失败，变为 `pkg/utils/version.go` clean checkout 可编译。
- [x] 从旧 vendor/GOPATH 解析，变为默认 build/test 走 module cache；旧 vendor 清理尚未完成，不提前宣称全量迁移。

**流程级约束核对**：
- [x] 依赖解析失败有具体原因：`ugorji/go` 和 `coreos/etcd` 均在 design 记录了编译原因。
- [x] 不引入旧 vendor 机制作为新依赖入口：未生成 `vendor/modules.txt`，`go.mod` 临时保持 `go < 1.14` 以避免自动 vendor mode。
- [x] 运行期行为不变：除测试地址和默认配置打印的 vet 修复外，未改 proxy/topom/models 运行路径。

**挂载点反向核对**：
- [x] `go.mod`：新增依赖解析入口；拔除会让 module mode 回到无主模块失败。
- [x] `go.sum`：新增 checksum lockfile；拔除后依赖校验信息缺失，会在恢复依赖时重新生成。
- [x] `pkg/utils.Version` / `pkg/utils.Compile`：新增源码默认变量；拔除会让引用这些符号的 cmd/pkg 编译失败。
- [x] 反向 grep：本 feature 额外触及 `Makefile`、`version`、`.gitignore`、`cmd/*/main.go` 和测试文件；均已在 design 或本验收报告补齐说明。
- [x] 拔除沙盘推演：删除 `go.mod/go.sum/pkg/utils/version.go` 后 feature 能力消失；恢复旧 `version` 覆盖源码会重新引入 make 后脏工作区问题。

## 3. 验收场景核对

- [x] **S1**：仓库根目录执行 `GO111MODULE=on go test ./cmd/... ./pkg/...`。
  - 证据来源：集成测试命令。
  - 结果：通过。

- [x] **S2**：clean checkout 未先运行 `bash version`，引用 `utils.Version` / `utils.Compile` 的包可编译。
  - 证据来源：源码默认变量 + `GO111MODULE=on go build ./cmd/... ./pkg/...`。
  - 结果：通过。

- [x] **S3**：删除或更换本地 module cache 后重新执行 module mode 测试。
  - 证据来源：空 `GOMODCACHE=/tmp/codis-gomodcache-cold` 下执行 `GO111MODULE=on GOSUMDB=off go test ./cmd/... ./pkg/...`。
  - 结果：通过，依赖从 `go.mod/go.sum` 恢复。

- [x] **S4**：默认构建标签下不要求 `cgo_jemalloc` 链路通过。
  - 证据来源：`GO111MODULE=on go test ./cmd/... ./pkg/...` 和 `GO111MODULE=on go build ./cmd/... ./pkg/...` 均未触发 `github.com/spinlock/jemalloc-go` module 解析。
  - 结果：通过；`cgo_jemalloc` 留给 `jemalloc-module-build`。

- [x] **S5**：查看 `go.mod`。
  - 证据来源：`go.mod` 文件内容。
  - 结果：`module github.com/CodisLabs/codis`，临时 `go 1.13` + `toolchain go1.26.1`，并带有旧 vendor 过渡注释。

- [x] **S6**：Makefile version 注入链路不污染源码。
  - 证据来源：`make codis-admin` 通过；之后 `git status` 未出现 `pkg/utils/version.go` 被覆盖的修改。
  - 结果：通过。

## 4. 术语一致性

- `Go module manifest`：代码落点为 `go.mod` / `go.sum`，文档、roadmap、architecture 命名一致。
- `module mode`：验收命令统一使用 `GO111MODULE=on`。
- `Godeps baseline`：仍作为版本选择依据保留，未改动 `Godeps/Godeps.json`。
- `version metadata`：代码中统一为 `utils.Version` / `utils.Compile`。
- `cgo_jemalloc`：仍作为 build tag / roadmap 后续条目名称使用，未提前实现。
- `temporary go directive`：`go.mod` 和 design 均明确其临时性。

防冲突 grep 结果：未引入与现有运行模型冲突的新术语。

## 5. 架构归并

- [x] `.codestable/architecture/ARCHITECTURE.md` 已更新：
  - 新增 `Go module manifest` 术语。
  - 构建层记录 `go.mod/go.sum` 最小 module mode 编译闭环。
  - 记录 `go 1.13` 临时 directive 的 vendor auto-mode 原因。
  - 记录 `pkg/utils/version.go` 与 `version` / `bin/version.ldflags` 的版本元数据链路。
  - 已知约束从“没有 go.mod 的 GOPATH 项目”改为“Go modules 最小编译闭环已建立，但仍处于迁移过渡态”。

判据：没读过 design 的维护者只看 architecture，也能知道当前 Go modules 迁移已经到达最小闭环、哪些部分仍留给 roadmap 后续条目。

## 6. requirement 回写

- [x] `requirement: null`，本 feature 是构建系统迁移的技术能力，不新增 Redis/Codis 用户可见能力。
- [x] 关联的核心 requirement `redis-cluster-service` 未改变用户故事、边界或 pitch。

结论：无 requirement 回写。

## 7. roadmap 回写

- [x] design frontmatter 指向 `roadmap: go-mod-migration` / `roadmap_item: go-module-compile-baseline`。
- [x] `.codestable/roadmap/go-mod-migration/go-mod-migration-items.yaml` 对应条目已从 `in-progress` 改为 `done`，并保留 feature 链接。
- [x] `.codestable/roadmap/go-mod-migration/go-mod-migration-roadmap.md` 第 5 节子 feature 清单已同步为 `done`。
- [x] roadmap 第 4.1 节已补充当前过渡态，避免长期契约和临时 `go 1.13` 状态冲突。

## 8. attention.md 候选盘点

有候选，不在 accept 阶段直接写入，等待用户决定是否用 `cs-note` 追加：

- 候选 1：当前仓库已存在 `go.mod/go.sum`，默认 cmd/pkg 可在 module mode 下测试；但这是迁移过渡态，不代表 cgo_jemalloc、Makefile 全量入口和旧 vendor 清理完成。
- 候选 2：在旧 `vendor/` 未清理前，`go.mod` 的 `go 1.13` 是临时规避；不要直接升到 `go >= 1.14`，否则 Go 会因顶层 vendor 自动进入 `-mod=vendor` 并要求 `vendor/modules.txt`。
- 候选 3：当前阶段不要用全量 `go mod tidy` 作为收敛手段；它会扫描默认范围外的 `cgo_jemalloc` 和依赖测试链路。最小闭环以 `GO111MODULE=on go test ./cmd/... ./pkg/...` 验证。

## 9. 遗留

- 后续优化点：
  - `jemalloc-module-build`：保留并模块化 `cgo_jemalloc` proxy 构建路径。
  - `makefile-module-mode`：完整更新 Makefile module mode 入口，移除剩余 GOPATH/vendor 时代约束。
  - `legacy-vendor-retirement`：删除或归档旧 `vendor/` / `Godeps/`，再把 `go.mod` 的 `go` directive 提升到当前工具链版本。
  - `module-migration-doc-notes`：更新项目注意事项和工程说明。

- 已知限制：
  - `go 1.13` 是临时过渡，不是长期最低版本承诺。
  - `go mod tidy` 在旧 vendor / cgo_jemalloc 未处理前不能代表本 feature 的最小验收范围。
  - `codis-admin` 当前没有 `--version` 入口，Makefile 版本注入链路用 `codis-dashboard --version` 验证。

- 实现阶段顺手发现：
  - `Makefile` 里旧 `go build -i` 在 Go 1.26 已不可用；本次在已触碰 Makefile 的范围内移除，避免新版本注入链路无法实际构建。
