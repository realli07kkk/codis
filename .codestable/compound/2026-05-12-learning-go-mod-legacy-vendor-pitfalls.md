---
doc_type: learning
track: pitfall
date: "2026-05-12"
slug: go-mod-legacy-vendor-pitfalls
component: build-system
severity: high
tags: [go, modules, vendor, build]
related_feature: 2026-05-11-go-module-compile-baseline
---

# Go modules 迁移旧 vendor 项目的坑

## 1. 问题

旧 GOPATH + vendor 项目接入 Go modules 时，最危险的点不是新增 `go.mod` 本身，而是旧构建链路和 Go module 规则同时存在：

- 顶层旧 `vendor/` 会影响 Go 的默认 module 解析模式。
- 全量 `go mod tidy` 会扫描超出当前验收范围的 build tags 和依赖测试链路。
- 原本由脚本生成的 `pkg/utils/version.go` 如果改成 tracked source，会和 Makefile 构建链路争抢同一个文件。

这三个问题容易让“能编译”的临时迁移变成不稳定的半迁移状态。

## 2. 症状

本次迁移中观察到的症状：

- `go.mod` 直接使用较新的 `go` directive 时，Go 会因为顶层 `vendor/` 存在而默认进入 vendor mode，并要求 `vendor/modules.txt`。
- `go mod tidy` 会尝试解析 `pkg/utils/unsafe2/je_malloc.go` 的 `cgo_jemalloc` 路径和 etcd 依赖测试链路，拉入本 feature 明确不处理的依赖范围。
- 把 `pkg/utils/version.go` 直接提交为静态文件后，如果 `version` 脚本继续覆盖它，执行 `make` 会让源码文件变脏。
- 旧 `github.com/coreos/etcd v3.0.17+incompatible` 搭配现代 `github.com/ugorji/go` 会在 generated codec helper 上编译失败。

## 3. 没用的做法

- 直接把 `go.mod` 写成当前工具链版本：在旧 `vendor/` 未清理前会触发自动 vendor mode，不适合作为最小闭环。
- 用全量 `go mod tidy` 当作第一阶段收敛工具：它会越过“默认构建标签 cmd/pkg 编译测试”的验收边界。
- 继续让 `version` 脚本写 `pkg/utils/version.go`：这会把同一个文件同时变成源码和构建产物。
- 强行降回 Godeps 里的 etcd v3.0.17：实测 `pkg/models/etcd` 编译失败，旧 generated code 依赖不存在的 `codec1978.GenHelperEncoder/Decoder`。

## 4. 解法

本次采用的临时解法：

- `go.mod` 临时使用 `go 1.13` + `toolchain go1.26.1`，并在文件内注释说明原因：旧 `vendor/` 未清理前避免 Go 1.14+ 自动 vendor mode。
- `go.sum` 不靠全量 `go mod tidy` 生成，而是以当前验收命令 `GO111MODULE=on go test ./cmd/... ./pkg/...` 重新生成最小范围内需要的 checksum。
- `pkg/utils/version.go` 作为 tracked source，只保留默认 `Version` / `Compile` 变量。
- `version` 脚本改为只生成 `bin/version` 和 `bin/version.ldflags`；Makefile 通过 `-ldflags -X` 注入真实 git/date 信息。
- 依赖升级必须记录具体编译原因；例如 `etcd v3.3.27+incompatible` 是为了保留 `github.com/coreos/etcd/client` import 同时兼容现代 codec 生成代码。

## 5. 为什么有效

- `go < 1.14` 避免 Go 在存在顶层 `vendor/` 时自动等同 `-mod=vendor`，让第一阶段可以走 module cache，而不提前生成或维护 `vendor/modules.txt`。
- 以验收命令生成 `go.sum`，使依赖锁定与本 feature 的真实成功标准一致，不把 `cgo_jemalloc` 和外部依赖测试链路提前纳入。
- `version.go` 的所有权变成单一来源：源码文件归 git，真实版本信息归 build flags；`make` 不再覆盖源码。
- etcd/ugorji 的版本调整有可复现的失败证据，后续 review 可以按 module path 和错误原因追溯，而不是把升级当作模糊“现代化”。

## 6. 预防

下次遇到旧 Go 项目迁移 Go modules，先按这个顺序检查：

1. 顶层是否已有旧 `vendor/`，以及是否存在 `vendor/modules.txt`。
2. `go.mod` 的 `go` directive 是否会触发 Go 自动 vendor mode。
3. 是否有 build tag 路径明确不属于当前最小闭环，避免用全量 `go mod tidy` 把范围扩大。
4. 生成文件是否正在被改成 tracked source；如果是，必须先确定唯一 owner。
5. 依赖版本偏离 Godeps 时，必须留下“旧版本为什么不能编译 / 新版本覆盖哪些 import 路径”的证据。

本仓库当前的临时状态已经写入 `go.mod` 注释、feature design、acceptance 和 architecture；旧 vendor 清理完成后，应移除 `go 1.13` 临时规避并提升到当前工具链版本。
