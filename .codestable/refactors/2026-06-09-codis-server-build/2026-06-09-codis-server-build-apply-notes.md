---
doc_type: refactor-apply-notes
refactor: 2026-06-09-codis-server-build
---

# codis-server-build apply notes

## 步骤 1: 创建 deps/Makefile
- 完成时间: 2026-06-09
- 改动文件: `extern/redis-8.6.3/deps/Makefile` (新增)
- 验证结果:
  - hiredis/libhiredis.a ✓ (126856 bytes)
  - linenoise/linenoise.o ✓ (34536 bytes)
  - lua/src/liblua.a ✓ (526304 bytes)
  - hdr_histogram/libhdrhistogram.a ✓ (18642 bytes)
  - fpconv/libfpconv.a ✓ (6756 bytes)
  - fast_float/libfast_float.a ✓ (32128 bytes)
  - xxhash/libxxhash.a ✓ (48512 bytes)
  - jemalloc: 预期失败（提示需预构建，步骤 3 处理）
- 偏离: 无

## 步骤 2: 创建 tests/modules/Makefile
- 完成时间: 2026-06-09
- 改动文件: `extern/redis-8.6.3/tests/modules/Makefile` (新增)
- 验证结果: `make -C tests/modules/ all` → Nothing to be done for 'all'. ✓
- 偏离: 无

## 步骤 3: 预构建 jemalloc
- 完成时间: 2026-06-09
- 改动文件: `extern/redis-8.6.3/deps/jemalloc/` (VERSION/configure/Makefile/headers 已生成)、`extern/redis-8.6.3/deps/Makefile` (jemalloc target 改为自动构建)
- **偏离**: jemalloc 自带的 `.gitignore` 屏蔽了 VERSION/configure/Makefile/headers 等所有生成文件，无法直接提交。改为让 deps/Makefile 的 jemalloc target 在 libjemalloc.a 缺失时自动运行 autogen → configure → make。这避免了与 vendored .gitignore 的冲突，同时保持一键编译能力。
- 验证结果:
  - VERSION: 5.3.0-0-g0 ✓
  - jemalloc.h: JEMALLOC_VERSION_MAJOR=5, MINOR=3 ✓
  - lib/libjemalloc.a: 41MB ✓
  - `make codis-server` 全链路零错误通过 ✓
  - redis-server 启动: malloc=jemalloc-5.3.0 ✓

## 步骤 4: Makefile 守卫 + attention.md
- 完成时间: 2026-06-09
- 改动文件: `Makefile:192-193` (新增 2 行守卫)、`.codestable/attention.md:17` (新增 1 行编译依赖)
- 验证结果:
  - guard 错误信息格式测试通过（模拟缺失 g++ 输出可操作的安装提示）
  - attention.md 条目追加完成
- 偏离: 无
