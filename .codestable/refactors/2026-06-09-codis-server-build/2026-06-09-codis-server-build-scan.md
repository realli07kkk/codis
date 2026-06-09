---
doc_type: refactor-scan
refactor: 2026-06-09-codis-server-build
status: pending-user-selection
scope: extern/redis-8.6.3/deps/、extern/redis-8.6.3/tests/modules/、extern/redis-8.6.3/src/zmalloc.h、Makefile codis-server 目标
summary: 发现 4 条优化点，全部为结构/构建基础设施缺陷，无性能或可读性条目
---

# codis-server-build scan

## 总览

- 扫描范围：`extern/redis-8.6.3/deps/`（8 个依赖目录）、`extern/redis-8.6.3/tests/modules/`、`extern/redis-8.6.3/src/zmalloc.h:28-36`、`Makefile:189-201`
- 发现 4 条优化点：结构 3 / 构建修复 1
- 按风险：低 1 / 中 2 / 高 1
- 建议先做：#1 #2（低风险、纯文件补齐、独立、编译即自证）
- 建议后做：#3 #4（依赖 #1 #2 完成后才能验证、需要人工确认产物正确性）
- 前置检查 7 条全过：✓

## 条目

### #1 补齐 deps/Makefile——Redis 8 依赖构建入口 ✓

- **位置**：`extern/redis-8.6.3/deps/`（Makefile 不存在）
- **分类**：结构
- **现状**：`extern/redis-8.6.3/deps/` 下有 8 个依赖目录（hiredis, linenoise, lua, hdr_histogram, fpconv, fast_float, xxhash, jemalloc），各目录仅有源码无构建脚本。Redis 8 的 `src/Makefile:426` 通过 `(cd ../deps && make $(DEPENDENCY_TARGETS))` 触发依赖构建，但 deps 目录缺少 Makefile 导致所有依赖无法构建。
- **问题**：`make codis-server` 在干净环境必然失败——deps/Makefile 是 Redis 8 构建链的入口文件，上游 Redis 8 源码包含此文件（deps/README.md 第 58 行明确引用 "Update jemalloc's version in `deps/Makefile`"），本项目缺失。影响范围：8 个依赖全部不可构建，约 50+ .c/.cpp 源文件无法编译链接。
- **建议**：参照上游 Redis 8 的 deps/Makefile 结构创建，包含 hiredis/linenoise/lua/hdr_histogram/fpconv/fast_float/xxhash/jemalloc 八个 target，每个 target 封装对应子目录的编译命令和静态库打包
- **建议映射的方法**：M-L3-04（服务层抽取——此处为 Makefile 构建层的"服务化"，把散落的手动编译步骤收敛到统一的 make target）
- **风险**：低（纯新增文件，不修改已有文件；编译通过即自证正确；`bin/` 和 `extern/` 下的 .o/.a 产物都在 `.gitignore` 覆盖范围内）
- **验证**：AI 自证（`make -j4 -C extern/redis-8.6.3/deps/` 全部 target 成功退出；各子目录产出 .a 或 .o 文件）
- **范围**：1 新文件 / 约 40 行

### #2 补齐 tests/modules/Makefile——解除 all 目标对测试模块的硬依赖 ✓

- **位置**：`extern/redis-8.6.3/tests/modules/`（Makefile 不存在）
- **分类**：结构
- **现状**：`extern/redis-8.6.3/src/Makefile:394` 的 `all` 目标依赖 `module_tests`，而 `module_tests`（第 408 行）执行 `make -C ../tests/modules`。tests/modules 目录有 .c 源文件但无 Makefile，导致 `make all` 在此步失败。注意：redis-server 二进制在此步之前已成功链接，失败的是测试模块编译。
- **问题**：`all` 目标对 `module_tests` 是硬依赖（非 `-` 前缀忽略错误），缺失 Makefile 导致整个 `make codis-server` 返回非零退出码——即使 redis-server 已经编译成功。测试模块不是 codis-server 运行时的必需组件。
- **建议**：创建 `tests/modules/Makefile`，提供空的 `all` 和 `clean` 目标（`.PHONY` 声明），使 `module_tests` 步骤不报错通过
- **建议映射的方法**：M-L2-02（内联函数——此处为 Makefile 目标的"内联"，把不应阻塞的测试模块依赖降级为空操作）
- **风险**：低（不影响 redis-server 编译结果；空 Makefile 不会覆盖任何已有文件）
- **验证**：AI 自证（`make -C extern/redis-8.6.3/` 不再因 module_tests 失败；最终 `make codis-server` 返回零退出码）
- **范围**：1 新文件 / 约 5 行

### #3 预构建 jemalloc——生成 configure 产物并设定正确版本号 ✓

- **位置**：`extern/redis-8.6.3/deps/jemalloc/`
- **分类**：结构
- **现状**：jemalloc 源码在 `deps/jemalloc/` 下已提交，`configure` 脚本存在，但以下产物缺失：
  - `VERSION` 文件内容是占位值 `0.0.0-0-g000000missing_version`（实际源码是 jemalloc 5.3.0）
  - `Makefile` 不存在（configure 未执行）
  - `config.stamp` 不存在
  - `include/jemalloc/jemalloc.h` 不存在（含内联版本宏，zmalloc.h 直接 include 此文件）
  - `include/jemalloc/jemalloc_macros.h` 不存在
  - `lib/libjemalloc.a` 不存在
- **问题**：`src/Makefile:299-302` 设定 `MALLOC=jemalloc` 并期望 `-I../deps/jemalloc/include` 下的 `jemalloc.h` 定义 `JEMALLOC_VERSION_MAJOR/MINOR`。当 header 缺失时编译因 `#include <jemalloc/jemalloc.h>` 失败；当 header 存在但版本宏为 0 时，`zmalloc.h:31` 的 `#if (JEMALLOC_VERSION_MAJOR > 2)` 检查失败输出 `#error "Newer version of jemalloc required"`。deps/README.md 第 54-58 行描述了正确流程：`rm VERSION configure && ./autogen.sh --with-version=5.3.0-0-g0 && ./configure`。
- **建议**：
  1. 删除当前占位 `VERSION` 和生成的 `configure`
  2. 运行 `./autogen.sh --with-version=5.3.0-0-g0` 生成带正确版本号的 configure
  3. 运行 `./configure --with-jemalloc-prefix=je_ --enable-static --disable-shared` 生成 Makefile 和所有 header（含 `jemalloc.h` 内联版本宏）
  4. 运行 `make -j4` 产出 `lib/libjemalloc.a`
  5. 将 `VERSION`、`configure`、`Makefile`、`config.stamp`、`include/jemalloc/jemalloc.h`、`include/jemalloc/jemalloc_macros.h` 和 `include/jemalloc/jemalloc_protos.h` 提交到 git
- **建议映射的方法**：M-L1-04（刻画测试——用"先固化正确的构建产物"保证后继构建可复现）
- **风险**：中（需要 autoconf/automake/libtool 工具链；生成的 configure/Makefile 等文件需要提交到 git，文件和行数较多；版本号 5.3.0 需人工确认与源码版本一致——已验证 ChangeLog 起始条目为 "5.3.0 (May 6, 2022)"）
- **验证**：HUMAN（确认提交的 VERSION/configure/Makefile/header 文件内容正确，版本号 5.3.0 与源码一致）+ AI 自证（`make -j4 -C extern/redis-8.6.3/deps/jemalloc/` 成功；`lib/libjemalloc.a` 存在；`jemalloc.h` 中 `JEMALLOC_VERSION_MAJOR=5, MINOR=3`）
- **范围**：1 目录 / 约 7 个生成文件需提交 / 约 3 条 shell 命令

### #4 在 codis-server Makefile 目标中增加依赖安装提示 ✓

- **位置**：`Makefile:189-201`（codis-server 目标）
- **分类**：结构
- **现状**：`codis-server` 目标的构建链依赖 gcc、g++（fast_float C++17 编译）和 autoconf/automake/libtool（jemalloc autogen），但目标没有前置检查。干净 Linux 环境直接执行 `make codis-server` 会因 "g++: command not found" 或 `#include <jemalloc/jemalloc.h>` 缺失而失败，错误信息不直观。
- **问题**：首次编译的开发者需要手动排查 4 层依赖（Go → make → Redis deps → jemalloc autogen），且错误信息分散在 C 编译输出中。`attention.md` 中目前没有编译前提的常驻提示。
- **建议**：在 `codis-server` 目标的 `make -j4 -C $(REDIS8_DIR)/` 之前增加一行检测：`@which g++ >/dev/null 2>&1 || { echo "ERROR: g++ required (dnf install -y gcc-c++)"; exit 1; }` 和 autoconf 检测。同时在 `.codestable/attention.md` 的"编译与构建"节追加一行编译依赖清单
- **建议映射的方法**：M-L2-08（守卫语句——在构建入口增加前置条件守卫，提前失败并给出可操作的错误信息）
- **风险**：中（Makefile 改动会影响 CI 和本地构建；需确保 `which` 命令在所有目标环境可用；`g++` 检测与 codis-proxy 的 cgo_jemalloc 构建共享同一依赖，改动可能被感知为"重复检查"）
- **验证**：AI 自证（模拟干净环境——临时 unset PATH 中的 g++——验证错误信息友好可操作）+ HUMAN（确认错误信息准确描述所需包名和安装命令）
- **范围**：1 文件 / 约 3-5 行新增 + attention.md 1 行追加
