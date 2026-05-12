# makefile-module-mode 验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-05-12
> 关联方案 doc：makefile-module-mode-design.md

## 1. 接口契约核对

对照方案第 2.1 节名词层逐一核查：

**接口示例逐项核对**：
- [x] `make gotest` → `go test -ldflags "$(cat bin/version.ldflags)" ./cmd/... ./pkg/...`：实测 `make gotest` 全部 9 个包编译测试通过，一致。
- [x] `make codis-proxy` → `go build -tags "cgo_jemalloc" -ldflags "$(cat bin/version.ldflags)" -o bin/codis-proxy ./cmd/proxy`：实测构建成功，jemalloc-go 由 go.mod replace 解析，一致。

**名词层"现状 → 变化"逐项核对**：
- [x] 删除 `export GO15VENDOREXPERIMENT=1`：Makefile L3 已不存在该行，grep 确认零命中。
- [x] 删除 `codis-deps` 中 `make -C vendor/github.com/spinlock/jemalloc-go/`：codis-deps 现仅含 `mkdir -p bin config && bash version`。
- [x] 删除 `distclean` 中 `make -C vendor/github.com/spinlock/jemalloc-go/ distclean`：distclean 现仅 clean + extern/redis distclean。
- [x] 所有其他 target 不变：`codis-dashboard`、`codis-admin`、`codis-ha`、`codis-fe` build 命令原样保留。

**流程图核对**（第 2.2 节 mermaid 图）：
- [x] `codis-deps` → `bash version` → `bin/version + bin/version.ldflags`：路径完整。
- [x] `go.mod replace → third_party/jemalloc-go → Go cgo 直接编译 je_*.c`：在 `make codis-proxy` 中验证通过。

## 2. 行为与决策核对

**需求摘要逐项验证**：
- [x] `make gotest` 在 module mode 下工作：实测通过（9 ok）。
- [x] `make codis-proxy` 在 module mode 下工作：实测构建成功。
- [x] `make codis-dashboard` 在 module mode 下工作：实测构建成功并刷新 config。

**明确不做逐项核对**：
- [x] 未处理 Dockerfile：diff 中无 Dockerfile。
- [x] 未删除 vendor/ 或 Godeps/：diff 中无变动。
- [x] 未提升 go.mod 的 go 1.13 directive：diff 中无 go.mod。
- [x] 未改 proxy/topom/Redis 运行行为或配置格式：diff 中无 pkg/、cmd/ 源码变动。
- [x] 未改 version 脚本：diff 中无 version。
- [x] 未改 third_party/jemalloc-go：diff 中无变动。
- [x] 未添加 GO111MODULE=on 到 Makefile：grep 确认零命中。

**关键决策落地**：
- [x] 移除 GO15VENDOREXPERIMENT：已删除，环境默认 module mode 生效。
- [x] 移除旧 jemalloc Makefile 调用：已删除，Go module mode cgo 直接编译。
- [x] 保留 bash version：codis-deps 仍包含，VERSION_LDFLAGS 仍被所有 target 使用。
- [x] distclean jemalloc 路径跟随：已删除旧 vendor 路径调用。

**编排层"现状 → 变化"逐项核对**：
- [x] codis-deps 不再进入旧 vendor jemalloc：已验证。
- [x] jemalloc 编译完全由 Go module mode + cgo 处理：`make codis-proxy` 通过。
- [x] target 名称和产出路径不变：build-all、gotest、各 codis-* 均保持不变。

**流程级约束核对**：
- [x] 错误语义：`bash version` 失败时使用默认值，ldflags 注入失败不影响编译（pkg/utils/version.go 提供兜底）。
- [x] 幂等性：重复 `make` 不生成 tracked source 变更，只写 bin/ 和 config/。
- [x] 兼容性：target 名称、产出路径、配置格式均不变。

**挂载点反向核对（可卸载性）**：
- [x] 挂载点 M1：Makefile L3 `export GO15VENDOREXPERIMENT=1` — 已删除 ✓
- [x] 挂载点 M2：codis-deps 中 `make -C vendor/github.com/spinlock/jemalloc-go/` — 已删除 ✓
- [x] 挂载点 M3：distclean 中 `make -C vendor/github.com/spinlock/jemalloc-go/ distclean` — 已删除 ✓
- [x] **反向核查**：grep `GO15VENDOREXPERIMENT\|vendor/github.com/spinlock/jemalloc-go` Makefile → 零命中，清单外无遗漏。
- [x] **拔除沙盘推演**：恢复三条删除 → GOPATH/vendor 构建参数回归 → module mode Makefile 回退到旧状态。无残留。

## 3. 验收场景核对

- [x] **S1**：`make gotest` → cmd/pkg 编译测试通过，不依赖 GOPATH 或旧 vendor 预处理
  - 证据来源：手工执行，输出全部 9 个包 ok
  - 结果：通过

- [x] **S2**：`make codis-proxy` → `go build -tags cgo_jemalloc` 通过，jemalloc-go 由 go.mod replace → third_party/jemalloc-go 解析
  - 证据来源：手工执行，构建成功无报错
  - 结果：通过

- [x] **S3**：`make codis-dashboard` → 构建成功，config/dashboard.toml 刷新
  - 证据来源：手工执行，构建成功
  - 结果：通过

- [x] **S4**：`make build-all` Go 组件 → 全部 5 个 Go 组件构建成功
  - 证据来源：手工执行 `make codis-admin codis-ha codis-fe`，全部成功
  - 结果：通过

- [x] **S5**：`make distclean` → 不报错
  - 证据来源：手工执行，退出码 0
  - 结果：通过

- [x] **S6**：重复 `make build-all` 后 `git status` → 仅 bin/ 和 config/ 有未跟踪变更
  - 证据来源：`git diff --stat` 仅含 Makefile、items.yaml、config/*.toml
  - 结果：通过

**明确不做的反向核对项**（与第 2 节合并核对）：
- [x] Diff 不包含 Dockerfile ✓
- [x] Diff 不包含 go.mod、go.sum ✓
- [x] Diff 不删除 vendor/ 或 Godeps/ ✓
- [x] Diff 不修改 third_party/jemalloc-go/ ✓
- [x] Diff 不修改 version 脚本 ✓
- [x] Diff 不修改 pkg/、cmd/ Go 源码 ✓
- [x] Diff 不添加 GO111MODULE=on ✓

## 4. 术语一致性

对照方案第 0 节：
- `GO15VENDOREXPERIMENT`：Makefile 中零命中（已删除）✓
- `vendor/github.com/spinlock/jemalloc-go`：Makefile 中零命中（已删除）✓
- 无新增术语，无冲突。

## 5. 架构归并

对照方案第 4 节，需要更新 `.codestable/architecture/ARCHITECTURE.md`：

- [x] **构建层描述更新**（L43）：原文"Makefile 通过 `export GO15VENDOREXPERIMENT=1` 和 GOPATH/vendor 语义构建"已过时，需更新为 module mode 构建。
- [x] **已知约束更新**（L100-101）：关于 Makefile 的约束表述需同步。
- [x] **代码锚点行号**（L93）：Makefile 行号因删除 4 行偏移。

→ 执行架构 doc 更新。

## 6. requirement 回写

- [x] 方案 frontmatter `requirement: null`，本 feature 属构建系统技术债迁移，不新增用户可感能力 → 跳过，无 requirement 回写。

## 7. roadmap 回写

- [x] 方案 frontmatter：`roadmap: go-mod-migration`、`roadmap_item: makefile-module-mode`
- [x] items.yaml 当前状态：`status: in-progress` + `feature: 2026-05-12-makefile-module-mode` → 改 `status: done`
- [x] 同步主文档第 5 节子 feature 清单条目 3 的状态

→ 执行回写。

## 8. attention.md 候选盘点

- [x] 无候选：本 feature 改动局限在 Makefile 三行删除，未暴露需要补入 attention.md 的环境 / 工具 / 工作流信息。

## 9. 遗留

- 后续优化点：无。`legacy-vendor-retirement` 是 roadmap 下一条，会处理 vendor/ 和 Godeps/ 清理。
- 已知限制：`go.mod` 的 `go 1.13` 临时 directive 仍保留，等待 `legacy-vendor-retirement` 完成后提升。
- 实现阶段"顺手发现"：无。
