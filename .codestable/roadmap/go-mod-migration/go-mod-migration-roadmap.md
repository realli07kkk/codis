---
doc_type: roadmap
slug: go-mod-migration
status: active
created: 2026-05-11
last_reviewed: 2026-05-12
tags: [go, modules, build]
related_requirements: [redis-cluster-service]
related_architecture: [system-overview]
---

# Go Modules 迁移

## 1. 背景

Codis 当前是 GOPATH + vendor/Godeps 项目。当前本机 Go 为 go1.26.1，GO111MODULE=on，仓库没有 go.mod 时无法在默认现代 Go 环境下编译。目标是在 Go modules 模式下完成 cmd/pkg 的编译与测试，保持 Redis/proxy/topom 运行行为不变。

## 2. 范围与明确不做

### 本 roadmap 覆盖

- 建立 `github.com/CodisLabs/codis` 的 `go.mod` / `go.sum`
- 从 `Godeps/Godeps.json` 迁移依赖锁定，尽量保持原依赖行为
- 处理 clean checkout 下 `pkg/utils/version.go` 缺失导致的编译问题
- 更新 Go 构建/测试入口，移除旧 `GO15VENDOREXPERIMENT` 和不兼容构建参数
- 保留 `cgo_jemalloc` 构建能力

### 明确不做

- 不处理 `Dockerfile`
- 不升级 Codis 运行协议、proxy 路由、topom 状态机或 Redis 命令行为
- 不现代化前端 `cmd/fe/assets/node_modules`
- 不迁移嵌入式 Redis 源码

## 3. 模块拆分（概设）

```text
go-mod-migration
├── module-manifest：Go module 依赖清单与版本锁定
├── build-entrypoints：Makefile 和测试/构建命令
├── generated-version：版本元数据生成与 clean checkout 编译
└── jemalloc-integration：cgo_jemalloc 的模块模式构建
```

### module-manifest

- **职责**：把 Godeps/vendor 依赖转成 `go.mod/go.sum`，模块名固定为 `github.com/CodisLabs/codis`。
- **承载的子 feature**：`go-module-compile-baseline`, `legacy-vendor-retirement`
- **触碰的现有代码 / 模块**：`Godeps/Godeps.json`, `vendor/`, 新增 `go.mod`, 新增 `go.sum`

### build-entrypoints

- **职责**：让 `make gotest`、单组件 build 在 module mode 下工作。
- **承载的子 feature**：`makefile-module-mode`
- **触碰的现有代码 / 模块**：`Makefile`

### generated-version

- **职责**：保证 clean checkout 下包能编译，构建时仍能写入真实版本信息。
- **承载的子 feature**：`go-module-compile-baseline`
- **触碰的现有代码 / 模块**：`version`, `pkg/utils/version.go`

### jemalloc-integration

- **职责**：保留 `go build -tags cgo_jemalloc ./cmd/proxy`。
- **承载的子 feature**：`jemalloc-module-build`
- **触碰的现有代码 / 模块**：`pkg/utils/unsafe2/je_malloc.go`, `vendor/github.com/spinlock/jemalloc-go`, `Makefile`

## 4. 模块间接口契约 / 共享协议（架构层详设）

### 4.1 Go module manifest

**方向**：Go toolchain -> Codis source tree

**形式**：`go.mod` / `go.sum`

**长期契约**：

```text
module github.com/CodisLabs/codis
go 1.26.1
```

**当前过渡态**：

```text
module github.com/CodisLabs/codis
go 1.13
toolchain go1.26.1
```

**约束**：

- 常规 Go 依赖必须由 `go.mod/go.sum` 解析。
- 普通 build/test 不再依赖 `vendor/` 或 `Godeps/`。
- 依赖版本选择优先保持现有 Godeps 行为；确实无法在现代 Go 下编译的依赖升级必须单独记录原因。
- `go 1.13` 仅用于旧 `vendor/` 清理前避免 Go 1.14+ 自动 vendor mode；`legacy-vendor-retirement` 完成后提升到当前工具链版本。

### 4.2 构建命令契约

**方向**：维护者 -> Makefile / Go toolchain

**形式**：shell 命令

**契约**：

```text
GO111MODULE=on go test ./cmd/... ./pkg/...
GO111MODULE=on go build -o bin/codis-dashboard ./cmd/dashboard
GO111MODULE=on go build -tags cgo_jemalloc -o bin/codis-proxy ./cmd/proxy
make gotest
```

**约束**：

- `make gotest` 仍是常规测试入口。
- 组件 build 仍刷新现有默认配置文件的行为，不能改变运行配置格式。
- `Dockerfile` 不在本次构建命令契约内。

### 4.3 版本元数据契约

**方向**：Go packages -> `pkg/utils`

**形式**：Go 变量

**契约**：

```go
package utils

var Version = string
var Compile = string
```

**约束**：

- clean checkout 下 `utils.Version` / `utils.Compile` 必须存在。
- 构建脚本可覆盖为真实 git/date 信息，但不能让裸 `go test` 因文件缺失失败。
- 版本文件方案不能引入运行期外部依赖。

### 4.4 jemalloc 契约

**方向**：`pkg/utils/unsafe2` -> jemalloc Go package

**形式**：Go import + build tag

**契约**：

```text
import "github.com/spinlock/jemalloc-go"
build tag: cgo_jemalloc
```

**约束**：

- `pkg/utils/unsafe2/je_malloc.go` 的 import path 不变，除非 feature-design 证明 `replace` 无法满足构建。
- 如 upstream 模块无法直接从 module cache 构建，允许把该依赖迁到 `third_party/jemalloc-go` 并在 `go.mod` 使用 `replace`，但不再放在旧 `vendor/` 目录。
- `go build -tags cgo_jemalloc ./cmd/proxy` 必须作为验收命令。

## 5. 子 feature 清单

1. **go-module-compile-baseline** — 建立 go.mod/go.sum 与版本元数据兜底，使 `GO111MODULE=on go test ./cmd/... ./pkg/...` 在非 `cgo_jemalloc` 路径下编译通过。
   - 所属模块：module-manifest, generated-version
   - 依赖：无
   - 状态：done
   - 对应 feature：`2026-05-11-go-module-compile-baseline`
   - 备注：最小闭环；当前 `go.mod` 临时使用 `go 1.13` + `toolchain go1.26.1`，旧 vendor 清理后再提升 directive。

2. **jemalloc-module-build** — 保留 `cgo_jemalloc` proxy 构建路径，解决 `github.com/spinlock/jemalloc-go` 的 C 源码与模块模式集成。
   - 所属模块：jemalloc-integration
   - 依赖：`go-module-compile-baseline`
   - 状态：done
   - 对应 feature：`2026-05-12-jemalloc-module-build`
   - 备注：使用 `go.mod replace` 将 `github.com/spinlock/jemalloc-go` 指向 `third_party/jemalloc-go`，建立 module mode 下的 `cgo_jemalloc` 构建闭环。

3. **makefile-module-mode** — 更新 Makefile，移除 GOPATH/vendor 时代构建参数，让 `make gotest` 和单组件 build 使用 module mode。
   - 所属模块：build-entrypoints
   - 依赖：`go-module-compile-baseline`, `jemalloc-module-build`
   - 状态：planned
   - 对应 feature：未启动

4. **legacy-vendor-retirement** — 移除或归档旧 `Godeps/` 与普通 `vendor/` 依赖，保留必要迁移说明。
   - 所属模块：module-manifest
   - 依赖：`makefile-module-mode`
   - 状态：planned
   - 对应 feature：未启动

5. **module-migration-doc-notes** — 更新项目注意事项和相关工程说明，说明此后默认 Go modules 构建。
   - 所属模块：build-entrypoints
   - 依赖：`legacy-vendor-retirement`
   - 状态：planned
   - 对应 feature：未启动

**最小闭环**：第 1 条 `go-module-compile-baseline` 做完后，clean checkout 在默认 module mode 下可以跑通 `GO111MODULE=on go test ./cmd/... ./pkg/...`，即证明 Go modules 迁移的最窄路径可行。

## 6. 排期思路

先做最小闭环，让 module mode 能编译 cmd/pkg；再处理最特殊的 jemalloc；最后更新 Makefile 和清理旧 vendor。技术依赖外不强行扩展到 Dockerfile，避免把构建系统迁移扩大成部署系统迁移。

## 7. 观察项

- `.codestable/attention.md` 当前写明“不要迁移 Go modules”，但本次用户已明确要求，因此不冲突；完成后需要在 acceptance 阶段回写。
- `scripts/docker.sh`、`kubernetes/*.yaml` 存在 GOPATH 路径，但本 roadmap 只处理不含 Dockerfile 的编译迁移；是否跟进部署路径另行决定。
- 旧 GOPATH 基线在正确路径下可解析依赖，但直接 `go test` 会因为 `pkg/utils/version.go` 未生成而失败；最小闭环必须覆盖版本元数据兜底。
