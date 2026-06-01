---
doc_type: feature-acceptance
feature: 2026-06-01-multi-platform-build
status: current
accepted_at: 2026-06-01
summary: Codis 已保留默认 host build，并新增显式多平台平台目录产物构建入口
tags: [build, makefile, platform, release, acceptance]
---

# multi-platform-build 验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-06-01
> 关联方案 doc：`.codestable/features/2026-06-01-multi-platform-build/multi-platform-build-design.md`

## 1. 接口契约核对

对照方案第 2.1 节名词层逐一核查：

**接口示例逐项核对**：

- [x] `make`：`Makefile:34` 将 `build-all` 指向 `host-build-all`，`make -n build-all` 展开的是根 `bin/codis-*` host build，不进入 Linux matrix guard。
- [x] `make build-platforms`：`Makefile:40` 到 `Makefile:50` 解析 `TARGET_PLATFORMS` 并逐项调用 `build-platform-artifact`；默认矩阵定义在 `Makefile:6`。
- [x] `make build-platforms TARGET_PLATFORMS="darwin/arm64"`：dry-run 只展开 `bin/darwin-arm64`，实际执行通过并由 `file` 输出确认 `codis-dashboard`、`codis-proxy`、`codis-server` 都是 `Mach-O arm64`。

**名词层"现状 → 变化"逐项核对**：

- [x] Target platform：`TARGET_OS` / `TARGET_ARCH` / `TARGET_LABEL` / `TARGET_DIR` 在 `Makefile:7` 到 `Makefile:14` 落地。
- [x] Platform output：full platform build 写入 `bin/<os>-<arch>/`，no-redis build 写入 `bin/<os>-<arch>-no-redis/`。
- [x] Proxy allocator mode：host build 和 full platform build 继续 `go build -tags "cgo_jemalloc"`；no-redis target 允许 `PROXY_JEMALLOC` 控制，但不声称纯 Go。
- [x] Config generation：`default-configs` 用 host helper binary 生成配置，platform build 只复制配置，不执行 target dashboard/proxy。

**流程图核对**：

- [x] `make / make build-all -> host-build-all -> root bin/codis-* + config`：落点在 `Makefile:34` 到 `Makefile:38` 和既有 host target。
- [x] `build-platforms / release-platforms -> TARGET_PLATFORMS -> build-platform-artifact`：落点在 `Makefile:40` 到 `Makefile:50`。
- [x] allowlist、cross guard、default-configs、Go/cgo、Redis 8、assets/config copy、`file` 校验：落点在 `Makefile:55` 到 `Makefile:151`。

结论：接口契约与设计一致。

## 2. 行为与决策核对

**需求摘要逐项验证**：

- [x] 默认 userspace 保留：`make -n build-all` 未触发 Linux target guard，仍构建 root `bin/codis-*`。
- [x] 显式矩阵入口存在：`build-platforms` / `release-platforms` 都落在 Makefile。
- [x] 平台产物隔离：full platform build 写 `bin/<label>/`，no-redis 写 `bin/<label>-no-redis/`。
- [x] 平台写入点自带 guard：直接执行 `make build-platform-artifact TARGET_OS=linux TARGET_ARCH=amd64 CGO_ENABLED=0` 在 `rm -rf bin/linux-amd64` 前失败，退出码 2。
- [x] 非 host 缺 C/cgo toolchain fail-fast：`make check-target-platform TARGET_OS=linux TARGET_ARCH=amd64 CGO_ENABLED=0` 输出 host、target 和 `PLATFORM_CC_linux_amd64` 建议，退出码 2。

**明确不做逐项核对**：

- [x] 未修改 Redis 8 `extern/redis-8.6.3/src/Makefile`。
- [x] 未把不同平台产物平铺到根 `bin/`；根 `bin/` 继续属于 host build。
- [x] 未让默认 `make` 因默认矩阵缺 cross toolchain 而失败。
- [x] 未默认关闭 `codis-proxy` 的 `cgo_jemalloc`。
- [x] 未新增或修改 Docker / CI / release 上传逻辑。
- [x] 未修改 proxy/topom/admin/Redis 运行期行为。
- [x] 未执行全量 `go mod tidy`，未恢复 GOPATH/vendor 构建。

**关键决策落地**：

- [x] D1 保留默认 host build：`build-all: host-build-all`。
- [x] D2 平台目录隔离：`TARGET_DIR := bin/$(TARGET_LABEL)`。
- [x] D3/D4 C/cgo 不伪装成 Go cross build：非 host full build 必须显式 `ALLOW_CROSS_FULL_BUILD=1` 和 `PLATFORM_CC_<label>`。
- [x] D5 安全 invariant 在写入 target 上：`build-platform-artifact` 自身先调用 `check-target-platform`。
- [x] D6 不再命名为 Go-only：target 名称为 `build-no-redis-platform`，输出为 `bin/<label>-no-redis/`。
- [x] D7 target binary 不用于配置生成：platform build 调用 host-side `default-configs`。

**流程级约束核对**：

- [x] 错误语义：unsupported platform、缺 cross compiler、artifact mismatch 分别有独立错误。
- [x] 兼容性：root `bin/` host build 未被平台目录替代。
- [x] 幂等性：同一 `TARGET_PLATFORMS` 只 `rm -rf` 对应平台目录；未列入矩阵的平台目录不被循环触碰。
- [x] 顺序约束：`check-target-platform` 在 `default-configs` 和 `rm -rf $(TARGET_DIR)` 前执行。
- [x] 可观测点：full platform build 最后对 dashboard/proxy/server 运行 `file` 并匹配 target pattern。

**挂载点反向核对（可卸载性）**：

- [x] 兼容默认入口：`build-all`、`host-build-all`、`build-host`。
- [x] 显式矩阵入口：`build-platforms`、`release-platforms`。
- [x] Platform 变量契约：`TARGET_PLATFORMS`、`TARGET_OS`、`TARGET_ARCH`、`TARGET_LABEL`、`TARGET_DIR`。
- [x] Host-side config generation：`default-configs`。
- [x] 产物写入 guard：`validate-target-platform`、`check-target-platforms`、`check-target-platform`。
- [x] No-redis platform build：`build-no-redis-platform`。
- [x] 反向 grep：`TARGET_PLATFORMS`、`TARGET_OS`、`TARGET_ARCH`、`TARGET_DIR`、`NO_REDIS_DIR`、`build-platform`、`build-no-redis-platform` 的代码命中均在 Makefile 的挂载点清单内；无方案外业务代码引用。
- [x] 拔除沙盘推演：移除上述 Makefile target 后功能消失，运行期 cmd/pkg 代码无残留耦合。

结论：行为、决策和挂载点与设计一致。

## 3. 验收场景核对

- [x] **S1**：在当前 Mac arm64 host 上执行 `make` 或 `make build-all`，继续生成 root host 产物。
  - 证据来源：`make -n build-all` 展开 host build；实现阶段也实际执行过 `make` 通过。
  - 结果：通过。

- [x] **S2**：执行 `make build-platforms` 且未声明 Linux cross C toolchain，Linux target fail-fast。
  - 证据来源：`make check-target-platform TARGET_OS=linux TARGET_ARCH=amd64 CGO_ENABLED=0` 和直接 `make build-platform-artifact ...` 均退出 2，错误包含 host、target 和 `PLATFORM_CC_linux_amd64`。
  - 结果：通过。

- [x] **S3**：执行 `make build-platforms TARGET_PLATFORMS="darwin/arm64"`，只构建 `bin/darwin-arm64/`。
  - 证据来源：dry-run 只展开 `bin/darwin-arm64`；实际命令通过。
  - 结果：通过。

- [x] **S4**：直接执行 `make build-platform-artifact TARGET_OS=linux TARGET_ARCH=amd64` 且未配置 cross toolchain，在写目录前 fail-fast。
  - 证据来源：命令退出 2，输出缺 cross toolchain；未打印 `building linux-amd64 into bin/linux-amd64`。
  - 结果：通过。

- [x] **S5**：非法平台输入不进入产物目录删除。
  - 证据来源：`make validate-target-platform TARGET_OS='linux/../../tmp' TARGET_ARCH=amd64` 输出 unsupported TARGET_OS，退出 2。
  - 结果：通过。

- [x] **S6**：`make build-no-redis-platform TARGET_OS=darwin TARGET_ARCH=arm64` 生成 no-redis 目录且不声称 Go-only。
  - 证据来源：实际命令通过，日志为 `building no-redis darwin-arm64 into bin/darwin-arm64-no-redis`。
  - 结果：通过。

- [x] **S7**：full platform build 的 `codis-proxy` 继续使用 `cgo_jemalloc`。
  - 证据来源：`Makefile:118` full platform proxy build 固定带 `-tags "cgo_jemalloc"`；dry-run 和实际 host platform build 均展开该命令。
  - 结果：通过。

执行过的验证命令：

```bash
make -n build-all
make -n build-platforms TARGET_PLATFORMS="darwin/arm64"
make check-target-platform TARGET_OS=linux TARGET_ARCH=amd64 CGO_ENABLED=0
make build-platform-artifact TARGET_OS=linux TARGET_ARCH=amd64 CGO_ENABLED=0
make validate-target-platform TARGET_OS='linux/../../tmp' TARGET_ARCH=amd64
make build-platforms TARGET_PLATFORMS="darwin/arm64"
make build-no-redis-platform TARGET_OS=darwin TARGET_ARCH=arm64
make gotest
git diff --check
```

当前环境没有 Linux cross C toolchain，因此没有实跑 `linux/amd64` / `linux/arm64` full artifact 成功路径；该项保留为真实 release runner 的环境验证。

## 4. 术语一致性

- `Host platform`：代码中为 `HOST_OS` / `HOST_ARCH`，来源 `go env GOHOSTOS` / `GOHOSTARCH`。
- `Target platform`：代码中为 `TARGET_OS` / `TARGET_ARCH`。
- `Build matrix`：代码中为 `TARGET_PLATFORMS`，只由显式 `build-platforms` / `release-platforms` 使用。
- `Host build`：代码中为 `host-build-all` / `build-host`，默认 `build-all` 指向它。
- `Platform artifact`：代码中为 `TARGET_DIR := bin/$(TARGET_LABEL)`。
- `Full platform build`：代码中由 `build-platform-artifact` 承载，包含 Go 二进制、proxy、Redis 8、helper、assets、配置和 `file` 校验。
- `No-redis platform build`：代码中为 `build-no-redis-platform`，未出现 `build-go-platform` / `goonly` target。

结论：术语与设计、代码和文档一致。

## 5. 架构归并

- [x] `.codestable/architecture/ARCHITECTURE.md` frontmatter `last_reviewed` 已更新到 2026-06-01，并将 `platform-release-artifacts` 加入 `implements`。
- [x] 构建层段落已补充：默认 `make` / `make build-all` 是 host build；显式 `build-platforms` / `release-platforms` 使用 `TARGET_PLATFORMS` 生成 `bin/<os>-<arch>/`。
- [x] 构建层段落已补充：`build-platform-artifact` 的 allowlist、unsafe label、C/cgo cross toolchain guard 和 `file` 平台校验。
- [x] 构建层段落已补充：`build-no-redis-platform` 与 full platform build 的边界，不宣称纯 Go。
- [x] 代码锚点已扩展 `Makefile` 描述。
- [x] 已知约束已补充：默认 host build 与显式多平台发布构建的关系，以及完整非 host 平台产物需要原生 runner 或显式 C/cgo cross toolchain。
- [x] 相关文档已挂入本 feature design / acceptance 链接。

## 6. requirement 回写

- [x] design frontmatter `requirement: null`，但本 feature 新增了用户可见的发布构建能力，因此按 accept 规则触发 req backfill。
- [x] 新增 `.codestable/requirements/platform-release-artifacts.md`，`status: current`，`implemented_by: [system-overview]`。
- [x] `.codestable/requirements/VISION.md` 已加入 `platform-release-artifacts` current 条目。
- [x] cs-req 自查：标题直接、用户故事来自本 feature 场景、没有塞 Makefile target 级实现细节、pitch 可独立理解、边界明确写出非任意 cross compile / 非 Docker / 非运行期变更 / 非纯 Go cross build。

## 7. roadmap 回写

- [x] design frontmatter `roadmap: null`。
- [x] design frontmatter `roadmap_item: null`。

结论：非 roadmap 起头，不需要更新 `.codestable/roadmap/*`。

## 8. attention.md 候选盘点

- [x] 候选：显式多平台发布构建入口为 `make build-platforms` / `make release-platforms`；默认 `make` 仍是 host build；非 host full artifact 需要 C/cgo cross toolchain。

该信息对后续构建相关 feature 会反复有用，建议用户确认后走 `cs-note` 加入 `.codestable/attention.md` 的“编译与构建”分节。

## 9. 遗留

- 已知限制：当前环境未验证真实 Linux cross C toolchain 下的 Redis/cgo full build；需要在配置好 `PLATFORM_CC_linux_amd64` / `PLATFORM_CC_linux_arm64` 的 runner 上执行 `make build-platforms` 并用 `file` / 基础命令验证。
- 已知限制：platform build 目录仍是先删后建，矩阵中某个平台失败会保留已成功平台目录；后续 release 流程如要求原子替换，可另起改进。
- 可选优化：后续如果 Makefile 继续增长，把 platform guard 和 artifact file 校验拆到 `scripts/`，降低 recipe 膨胀风险。

验收通过。实现与设计一致，架构文档和 requirement 已回写，checklist checks 已全部标记为 `passed`。
