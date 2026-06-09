---
doc_type: refactor-design
refactor: 2026-06-09-codis-server-build
status: approved
scope: extern/redis-8.6.3/deps/、extern/redis-8.6.3/tests/modules/、extern/redis-8.6.3/deps/jemalloc/、Makefile codis-server 目标
summary: 补齐 Redis 8 构建链路缺失文件 + 预构建 jemalloc + Makefile 守卫，使 make codis-server 在干净 Linux 环境一键通过
---

# codis-server-build refactor design

## 1. 本次范围

- 从 scan 勾选了 #1 #2 #3 #4（全部 4 条）
- 不涉及：Redis 3 编译（codis-server-redis3）、Go 二进制构建、跨平台构建（build-platforms）

## 2. 前置依赖

- **编译工具链**（执行 #3 需要）：autoconf automake libtool gcc-c++ 已在验证阶段安装，后续 #4 的 Makefile 守卫会提示后来者安装
- **文件备份**：`Makefile`、`extern/redis-8.6.3/src/zmalloc.h`（只读不改）、`.codestable/attention.md`——均已有 git 保护，无需额外备份
- **#3 依赖 #1 的 deps/Makefile**：执行顺序上先做 #1，再做 #3

## 3. 执行顺序

### 步骤 1：创建 deps/Makefile（scan #1）

- **引用方法**：M-L3-04（服务层抽取——把散落的手动编译命令收敛到统一 make target）
- **具体操作**：
  1. 创建 `extern/redis-8.6.3/deps/Makefile`
  2. 参考上游 Redis 8 deps/Makefile 结构，列出 8 个 target：
     - `hiredis`: `cd hiredis && $(CC) -O3 -fPIC -std=c99 -c *.c && $(AR) rcs libhiredis.a *.o`
     - `linenoise`: `cd linenoise && $(CC) -O3 -fPIC -std=c99 -c linenoise.c -o linenoise.o`
     - `lua`: `cd lua/src && $(CC) -O3 -fPIC -std=c99 -c *.c && $(AR) rcs liblua.a *.o`
     - `hdr_histogram`: `cd hdr_histogram && $(CC) $(CFLAGS) -std=c99 -DHDR_MALLOC_INCLUDE=\"hdr_redis_malloc.h\" -c hdr_histogram.c && $(AR) rcs libhdrhistogram.a hdr_histogram.o`
     - `fpconv`: `cd fpconv && $(CC) -O3 -fPIC -std=c99 -c fpconv_dtoa.c && $(AR) rcs libfpconv.a fpconv_dtoa.o`
     - `fast_float`: `cd fast_float && $(CXX) -O3 -fPIC -std=c++17 -c fast_float_strtod.cpp && $(AR) rcs libfast_float.a fast_float_strtod.o`
     - `xxhash`: `cd xxhash && $(CC) -O3 -fPIC -std=c99 -c xxhash.c && $(AR) rcs libxxhash.a xxhash.o`
     - `jemalloc`: 空操作（由 #3 独立处理）
     - `all` 伪目标依赖上述 8 个
     - `distclean`/`clean` 为空操作
  3. 验证：`make -j4 -C extern/redis-8.6.3/deps/ all` 全部成功
- **退出信号**：8 个 target 全部通过；各子目录产出期望的 .a 或 .o
- **验证责任**：AI 自证
- **回滚**：`git checkout -- extern/redis-8.6.3/deps/Makefile` 删除该文件即可（新文件无旧版本）

### 步骤 2：创建 tests/modules/Makefile（scan #2）

- **引用方法**：M-L2-02（内联函数——把阻塞 build 的测试编译降级为空操作）
- **具体操作**：
  1. 创建 `extern/redis-8.6.3/tests/modules/Makefile`
  2. 内容：`.PHONY: all clean` + `all:` + `clean:`
  3. 验证：`make -C extern/redis-8.6.3/` 不再因 module_tests 步骤报错
- **退出信号**：`make codis-server` 零退出码（前提是 #1 和 #3 已完成）
- **验证责任**：AI 自证
- **回滚**：删除该文件即可

### 步骤 3：预构建 jemalloc + 提交构建产物（scan #3）

- **引用方法**：M-L1-04（刻画测试——先把正确的构建产物固化到 git，保证后续可复现）
- **具体操作**：
  1. 删除当前占位文件：`rm deps/jemalloc/VERSION deps/jemalloc/configure`
  2. 运行 autogen（带版本号）：`./autogen.sh --with-version=5.3.0-0-g0`（在 deps/jemalloc 目录下）
  3. 运行 configure：`./configure --with-jemalloc-prefix=je_ --enable-static --disable-shared`
  4. 运行 make：`make -j4`
  5. 验证产物：
     - `lib/libjemalloc.a` 存在且为静态库
     - `include/jemalloc/jemalloc.h` 中 `JEMALLOC_VERSION_MAJOR=5, MINOR=3`
  6. **关键决策点**：确认将 `VERSION`、`configure`、`Makefile`、`config.stamp`、`include/jemalloc/jemalloc.h`、`include/jemalloc/jemalloc_macros.h`、`include/jemalloc/jemalloc_protos.h`、`include/jemalloc/jemalloc_typedefs.h` 提交到 git
  7. 运行 `make distclean` 清理 .o 文件（保留上述生成文件）
- **退出信号**：`lib/libjemalloc.a` 存在；header 版本宏正确；zmalloc.h 版本检查通过
- **验证责任**：HUMAN（确认提交的文件列表和版本号正确） + AI 自证（编译 + 版本宏检查）
- **回滚**：`git checkout -- extern/redis-8.6.3/deps/jemalloc/`（但生成文件大多未 track，需额外清理）

### 步骤 4：Makefile 守卫 + attention.md（scan #4）

- **引用方法**：M-L2-08（守卫语句——构建入口加前置条件检查）
- **具体操作**：
  1. 在 `Makefile` 的 `codis-server:` 目标中，`make -j4 -C $(REDIS8_DIR)/` 之前插入：
     ```makefile
     	@which g++ >/dev/null 2>&1 || { echo "ERROR: g++ required. Install with: dnf install -y gcc-c++ (Rocky/EL) or apt install -y g++"; exit 1; }
     	@which autoconf >/dev/null 2>&1 || { echo "ERROR: autoconf required. Install with: dnf install -y autoconf automake libtool"; exit 1; }
     ```
  2. 在 `.codestable/attention.md` 的"编译与构建"节追加一行：
     ```markdown
     - `make codis-server` 编译嵌入式 Redis 8 需要 C/C++ 工具链和 autotools：`dnf install -y gcc-c++ autoconf automake libtool`（Rocky/EL）或等价包。
     ```
  3. 验证：模拟缺 g++（临时 PATH 调整）验证错误信息友好可操作
- **退出信号**：缺 g++ 时 make 失败并打印 "ERROR: g++ required..."；缺 autoconf 同理
- **验证责任**：AI 自证（模拟缺工具）+ HUMAN（确认错误信息准确）
- **回滚**：`git checkout -- Makefile .codestable/attention.md`

## 4. 风险与看点

- **高风险步骤**：#3——jemalloc 构建产物提交 git。生成文件（configure 1.5MB+、Makefile ~8KB）体积较大，需确认团队接受提交生成文件的策略。备选方案是仅提交 `VERSION` + 手动 patch 版本来跳过 jemalloc autogen，但会增加维护复杂度
- **偏离**（步骤 3，执行中记录）: jemalloc 自带的 `.gitignore` 屏蔽了 VERSION/configure/Makefile/headers 等所有生成文件，无法像原设计那样直接提交。改为让 deps/Makefile 的 jemalloc target 在 libjemalloc.a 缺失时自动运行 autogen → configure → make。用户已确认此策略。

- **容易出错的点**：
  - #1 deps/Makefile 中的编译选项需与 src/Makefile 的 FINAL_CFLAGS 对齐（特别是 `-DHDR_MALLOC_INCLUDE`）
  - #3 autogen 的 `--with-version` 参数格式必须正确（`MAJOR.MINOR.PATCH-0-g0`）
  - #4 `which` 在极端精简容器（如 scratch/distroless）中可能不存在——但对标当前 Makefile 的其他目标，未使用 `command -v`，保持一致用 `which`
