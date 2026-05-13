# redis8-patch-inventory-and-build-harness 验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-05-13
> 关联方案 doc：`.codestable/features/2026-05-13-redis8-patch-inventory-and-build-harness/redis8-patch-inventory-and-build-harness-design.md`

## 1. 接口契约核对

**接口示例逐项核对**：

- [x] `redis8 patch inventory`：打开 `.codestable/features/2026-05-13-redis8-patch-inventory-and-build-harness/redis8-patch-migration-matrix.md`，可看到 Redis 3 patch 修改面、Redis 8 对应位置/API、迁移风险和后续归属 feature。结果：一致。
- [x] `make codis-server-redis8`：根 `Makefile` 提供独立目标，产出 `bin/codis-server-redis8`、`bin/redis-cli-redis8`、`bin/redis-benchmark-redis8`、`bin/redis-sentinel-redis8`，并生成 ignored 的 `config/redis8.conf` / `config/sentinel8.conf`。结果：一致。

**名词层“现状 → 变化”逐项核对**：

- [x] Redis 3 patch 修改面：矩阵覆盖 `slots.c`、`slots_async.c`、`crc32.c` 以及 `server.h/server.c/db.c/networking.c/object.c/config.c/Makefile`。结果：一致。
- [x] Redis 8 build harness：`extern/redis-8.6.3/src/Makefile` 的 `REDIS_SERVER_OBJ` 包含 `slots.o`、`slots_async.o`、`crc32.o`；Redis 8 下新增 3 个 Codis stub 源文件。结果：一致。
- [x] Redis 3 默认冒烟兼容：`extern/redis-3.2.11/src/config.h` 和 `debug.c` 只做 macOS SDK / ARM64 mcontext 编译兼容补丁，未改变 Codis patch 运行逻辑。结果：一致。

**流程图核对**：

- [x] `make codis-server-redis8` 的实际流程是根 Makefile → `make -C extern/redis-8.6.3/` → Redis 8 `REDIS_SERVER_OBJ` → link `redis-server` → copy suffixed binaries/configs。命令 dry run 与实际构建输出均匹配。

## 2. 行为与决策核对

**需求摘要逐项验证**：

- [x] 文件级移植矩阵已落盘，后续 feature 能按归属列消费。
- [x] Redis 8 最小构建目标已接通，`make codis-server-redis8` 通过。
- [x] 默认 `make codis-server` 仍构建 Redis 3，未被切换到 Redis 8。

**明确不做逐项核对**：

- [x] 未切换 `build-all` 的默认 Redis Server 目标，`build-all` 仍依赖 `codis-server`。
- [x] 未修改 Go 源码，`git status` / `git diff` 未出现 `cmd/`、`pkg/`、`go.mod`、`go.sum`。
- [x] 未修改 Redis 8 `commands.def`，未新增 Redis 8 `src/commands/slots*.json`。
- [x] 未修改 Redis 8 `db.c`、`server.c`、`server.h`、`config.c` 的 `codis_enabled` / slot 初始化逻辑。
- [x] 未把 Redis 3 `slots.c` / `slots_async.c` 真实逻辑复制进 Redis 8；Redis 8 目前只有 stub object。

**关键决策落地**：

- [x] D1 新增独立 `codis-server-redis8`：落在 `Makefile`，默认 `codis-server` 保持 Redis 3。
- [x] D2 Redis 8 先链接 stub Codis object：`slots.c` / `slots_async.c` 只有 marker 函数，`crc32.c` 保留基础 CRC32。
- [x] D3 移植矩阵落在 feature 目录：已落 `.codestable/features/.../redis8-patch-migration-matrix.md`。
- [x] D4 暂不改 Redis 8 command metadata：diff 和 grep 均确认未触碰。

**编排层“现状 → 变化”逐项核对**：

- [x] 新增 Redis 8 独立构建支线，不接入默认 `build-all`。
- [x] Redis 8 `redis-server` 链接 Codis stub objects。
- [x] migration matrix 作为后续 feature 的前置输入，不扩大实现到 Codis 模式或迁移命令。

**流程级约束核对**：

- [x] 错误语义：`make codis-server-redis8` 实际构建并链接 stub objects，未绕开失败。
- [x] 幂等性：Redis 8 suffixed configs 被标为 ignored；重复构建不会污染 tracked source。
- [x] 兼容性：`make codis-server` 通过并显示 Redis 3.2.11。
- [x] 扩展点：Redis 8 stub files 已可被后续 feature 填入真实 hash/command 逻辑。

**挂载点反向核对**：

- [x] 根 `Makefile`：新增 `REDIS3_DIR` / `REDIS8_DIR` 和 `codis-server-redis8`，默认目标未变。
- [x] `extern/redis-8.6.3/src/Makefile`：新增 Codis stub objects。
- [x] `extern/redis-8.6.3/src/slots.c` / `slots_async.c` / `crc32.c`：新增 stub/source。
- [x] `.gitignore`：不再忽略 `extern/redis-8.6.3/` 源码目录，并忽略 suffixed config 生成物。
- [x] `extern/redis-3.2.11/src/config.h` / `debug.c`：只做默认 Redis 3 构建冒烟所需的 macOS 兼容补丁。
- [x] 拔除沙盘推演：移除上述挂载点后，Redis 8 harness 不再存在；默认 Redis 3 构建兼容补丁也可独立回滚，但当前 macOS SDK 下会重新暴露编译失败。

## 3. 验收场景核对

- [x] **S1**：打开 migration matrix → 覆盖 Redis 3 patch 修改文件和 Codis 新增源文件。
  - 证据来源：文档 review。
  - 结果：通过。
- [x] **S2**：执行 `make codis-server-redis8` → Redis 8 构建通过。
  - 证据来源：命令执行；`./bin/codis-server-redis8 --version` 输出 `Redis server v=8.6.3`。
  - 结果：通过。
- [x] **S3**：检查 Redis 8 link 依赖 → `slots.o`、`slots_async.o`、`crc32.o` 被编译。
  - 证据来源：构建输出和 `ls extern/redis-8.6.3/src/{slots.o,slots_async.o,crc32.o}`。
  - 结果：通过。
- [x] **S4**：执行 `make codis-server` → 默认 Redis 3 构建仍通过。
  - 证据来源：命令执行；`./bin/codis-server --version` 输出 `Redis server v=3.2.11`。
  - 结果：通过。
- [x] **S5**：grep Redis 8 command metadata → 未新增 Codis command JSON，未修改 `commands.def`。
  - 证据来源：`git diff -- extern/redis-8.6.3/src/commands.def 'extern/redis-8.6.3/src/commands/*.json'` 无输出。
  - 结果：通过。
- [x] **S6**：grep `codis_enabled` / `getKeySlot` / Go proxy/topom → 未提前实现 Codis 模式或 Go 适配。
  - 证据来源：`git diff -G 'codis_enabled|getKeySlot|calculateKeySlot|keyHashSlot'` 无输出；Go 路径无 diff。
  - 结果：通过。

## 4. 术语一致性

- Redis 3 Codis patch：矩阵、design、acceptance 统一使用该术语。
- Redis 8 source：统一指 `extern/redis-8.6.3/`。
- File-level migration matrix：落盘文件名和报告口径一致。
- Build harness：实现为 `codis-server-redis8`，没有误称为正式 packaging。
- Stub Codis object：实现只包含 `slots.o`、`slots_async.o`、`crc32.o` stub/source，没有暴露命令。
- 防冲突：未引入 `codis_enabled`、Codis command JSON、Redis Cluster 语义等后续阶段术语实现。

## 5. 架构归并

- [x] `.codestable/architecture/ARCHITECTURE.md`：已在构建层补充 Redis 8 独立 harness，说明 `make codis-server-redis8` 的产物和默认 `codis-server` 仍指向 Redis 3。
- [x] `.codestable/architecture/ARCHITECTURE.md`：已在代码锚点补充 Redis 8 harness 的 `src/Makefile`、`slots.c`、`slots_async.c`、`crc32.c`。
- [x] `.codestable/architecture/ARCHITECTURE.md`：已在已知约束中补充 Redis 8 harness 当前只是 stub object link，正式切换等待后续 roadmap item。

## 6. requirement 回写

- [x] `requirement: redis-cluster-service` 当前为 `current`。本 feature 是 Redis 8 升级的内部构建/规划能力，不改变业务用户故事、边界或对外能力，因此 requirement 无需更新。

## 7. roadmap 回写

- [x] `.codestable/roadmap/redis8-upgrade/redis8-upgrade-items.yaml`：`redis8-patch-inventory-and-build-harness` 已从 `in-progress` 改为 `done`，`feature` 指向 `2026-05-13-redis8-patch-inventory-and-build-harness`。
- [x] `.codestable/roadmap/redis8-upgrade/redis8-upgrade-roadmap.md`：第 5 节对应子 feature 已同步为 `done` 并填入 feature 目录。
- [x] YAML 校验通过。

## 8. attention.md 回写

- [x] 已通过 `cs-note` 写入 `.codestable/attention.md`。

- Redis 8 源码固定在 `extern/redis-8.6.3/`，不要再使用根目录 `redis-8.6.3/`；该目录不应被 `.gitignore` 忽略，后续 Redis 8 patch 改动必须能被 `git status` 看到。

## 9. 遗留

- 后续优化点：`redis8-codis-mode-foundation` 需要真正引入 `codis-enabled`、1024 slot `kvstore` 和 Codis CRC32 slot path。
- 已知限制：当前 Redis 8 `slots.c` / `slots_async.c` 是 stub，不注册命令、不具备 Codis 运行能力。
- 已知限制：`config/redis8.conf` / `config/sentinel8.conf` 是 `codis-server-redis8` 的 ignored 生成物；正式配置模板归 `redis8-build-config-packaging`。
- 顺手发现：Redis 3 在当前 macOS / Apple Silicon 工具链下需要 `config.h` 和 `debug.c` 的最小平台兼容补丁才能构建；本 feature 已修复。
