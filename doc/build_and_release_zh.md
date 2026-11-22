# 构建与发布指南

本仓库用 Go modules 管理 Go 依赖，`make` 是唯一的常规构建入口：它负责编译 Go 二进制、内嵌的 Redis 8 Codis Server、前端资源和默认配置。本文说明常用命令、Go modules 约定、`codis-server` 的编译依赖与排障、多平台发布布局，以及测试和路径约定。

## 常用命令

`make` 的默认目标是 `build-all`，也就是当前机器平台的 host build：依次构建 `codis-server`、`codis-dashboard`、`codis-proxy`、`codis-admin`、`codis-ha`、`codis-fe`，最后清理测试临时目录。

| 命令 | 作用 |
| --- | --- |
| `make` / `make build-all` / `make build-host` | host build，产物平铺在根 `bin/`，并刷新 `config/` 下的默认配置 |
| `make gotest` | 常规测试入口，执行 `go test ./cmd/... ./pkg/...`，并注入版本 ldflags |
| `make gobench` | 在 `./pkg/...` 上跑 benchmark（`-gcflags -l -bench=. -v`） |
| `make codis-dashboard` | 只构建 dashboard，并用新二进制重写 `config/dashboard.toml` |
| `make codis-proxy` | 只构建 proxy（带 `cgo_jemalloc`），并重写 `config/proxy.toml`；Linux 上会先跑 `jemalloc-config` |
| `make codis-server` | 构建内嵌 Redis 8，产出 `bin/codis-server` 与 `redis-*` helper，并重写 `config/redis.conf`、`config/sentinel.conf` |
| `make codis-server-redis8` | 以 `codis-server` 为前提，额外产出 `bin/*-redis8` 与 `config/redis8.conf`、`config/sentinel8.conf` |
| `make build-platforms` / `make release-platforms` | 按 `TARGET_PLATFORMS` 矩阵构建平台产物到 `bin/<os>-<arch>/` |
| `make build-no-redis-platform` | 只构建 Go 入口与 FE assets 到 `bin/<os>-<arch>-no-redis/`，不含 Redis Server |
| `make clean` | 删除 `bin/`、`scripts/tmp/` 和 `pkg/topom/gotest.tmp` |
| `make distclean` | 在 `clean` 之上再清理内嵌 Redis（含 jemalloc）的构建输出 |

几个容易踩到的行为：

- `make codis-proxy` 和 `make codis-dashboard` 会顺带刷新对应默认配置文件，配置文件格式属于对外契约，不要随手改动。
- `make codis-server` 生成 `config/redis.conf` 时会在 `databases` 行后注入 `codis-enabled yes`（1024 slot 模式）和 `codis-migration-auth-user` / `codis-migration-auth-pass` 空占位，并校验注入结果；`config/sentinel.conf` 由 Redis 8 的 `sentinel.conf` 注释掉 `sentinel` 行后生成。这两份文件是生成物，改配置模板请改 `extern/redis-8.6.3` 下的原始文件。
- 单组件 target 都依赖 `codis-deps`：创建 `bin/`、`config/`、`scripts/tmp/` 并运行 `version` 脚本生成 `bin/version` 与 `bin/version.ldflags`。
- 除非任务明确要求容器相关内容，不要改动 `Dockerfile` 或 docker 脚本，也不要把 `docker build` 当作默认必跑步骤。

绕过 make 直接构建单个组件也能跑通，代价是不会刷新默认配置，适合快速验证：

```bash
go test ./cmd/... ./pkg/...
go build -o bin/codis-dashboard ./cmd/dashboard
go build -tags cgo_jemalloc -o bin/codis-proxy ./cmd/proxy
```

## Go modules 约定

依赖只从 `go.mod` / `go.sum` 解析，`go` 指令为 `go 1.26.1`。旧的 `vendor/` 与 `Godeps/` 已退休，不要恢复基于 GOPATH 或源码级 vendor 副本的构建路径；真需要 vendor mode 时应另起方案用 `go mod vendor` 生成带 `vendor/modules.txt` 的一致目录树。

- **不要跑全量 `go mod tidy`。** 它会顺着既有第三方依赖（包括其测试依赖）的链路重写 `go.mod` / `go.sum`，产生大量与本意无关的改动。更新依赖时让构建或测试命令驱动最小的机械变化即可，例如先跑 `go build ./cmd/...` 再补它明确报缺的条目。
- **不要顺手升级依赖版本。** 版本偏离必须有可复现的现代 Go 编译原因；能编译就保持原状。
- **`version.go` 归 git。** `pkg/utils/version.go` 提交到仓库，只放 `unknown version` / `unknown datetime` 兜底值，保证 clean checkout 下裸 `go test` 不会因缺少生成文件失败；真实版本信息由 `version` 脚本写成 `-X` 形式塞进 `bin/version.ldflags`，Makefile 通过 `VERSION_LDFLAGS` 注入到每个 build/test 命令。版本信息不能变成运行期外部依赖。
- **jemalloc 走本地 replace。** `pkg/utils/unsafe2` 在 `cgo_jemalloc` build tag 下 import `github.com/spinlock/jemalloc-go`，`go.mod` 用 `replace github.com/spinlock/jemalloc-go => ./third_party/jemalloc-go` 指向本地副本；相关修改改 `third_party/jemalloc-go`，不要改 vendor 副本或 module cache 里的文件。Linux 上构建 proxy 前会先执行 `make -C third_party/jemalloc-go -f config.mk config` 生成 jemalloc 配置。
- **RDB 解析同样是本地 fork。** `replace github.com/hdt3213/rdb => ./third_party/rdb` 引入了 Redis 8 RDB version 13 与 stream IDMP 解析，升级该库时要连同本地补丁一起考虑。

## 编译 codis-server（内嵌 Redis 8）

`make codis-server` 进入 `extern/redis-8.6.3` 用 `make -j4` 构建上游 Redis 8，然后拷贝 `redis-server`、`redis-benchmark`、`redis-cli`、`redis-sentinel`、`redis-check-aof`、`redis-check-rdb` 到 `bin/`。

### 依赖工具链

除 Go 之外还需要 C/C++ 编译器（`fast_float` 是 C++17）和 autotools（jemalloc 需要 `autogen.sh`）。Makefile 在构建前用 `which` 做前置检查，缺失时直接给出安装提示：

```bash
# Rocky / EL
dnf install -y gcc gcc-c++ autoconf automake libtool

# Debian / Ubuntu
apt install -y gcc g++ make autoconf automake libtool
```

另外需要 OpenSSL 开发头文件才能编出 `libhiredis_ssl.a`（仅在 `BUILD_TLS=1` 时需要），对应 `openssl-devel` / `libssl-dev`。

### jemalloc 自动构建链路

`extern/redis-8.6.3/deps/jemalloc` 只提交源码，它自己的 `.gitignore` 忽略了 `/Makefile` 和 `/lib/`，所以 `lib/libjemalloc.a` 永远不会被 git 跟踪。`deps/Makefile` 的 `jemalloc` target 因此在产物缺失时自动补全整条链路：

```bash
cd extern/redis-8.6.3/deps/jemalloc
[ -f configure ] || ./autogen.sh --with-version=5.3.0-0-g0
./configure --with-jemalloc-prefix=je_ --with-lg-quantum=3 --enable-static --disable-shared
make -j4
```

`autogen.sh` 内部会调用一次 `./configure`，但不能依赖它：它用不到我们需要的参数，所以 `deps/Makefile` 在 autogen 之后无条件再跑一遍自己的 `configure`。`deps/Makefile` 同时手写编译 8 个内嵌依赖：hiredis、linenoise、lua、hdr_histogram、fpconv、fast_float、xxhash、jemalloc（仓库内嵌的这些依赖子目录不带各自的上游 Makefile）。

### Linux 构建排障

| 现象 | 原因 | 处理 |
| --- | --- | --- |
| `jemalloc/jemalloc.h: No such file` 或 `#error "Newer version of jemalloc required"` | jemalloc 生成物缺失，或 `VERSION` 是占位值导致版本宏为 0 | 跑 `make distclean` 后重编，让 autogen → configure → make 重新生成头文件与静态库 |
| 运行期 jemalloc 相关断言，或 Redis 8 要求的 quantum 不一致 | configure 漏了 `--with-lg-quantum=3`（上游 `deps/README.md` 的要求） | 不要在 jemalloc 目录手跑裸 `./configure`，统一走 `deps/Makefile` |
| `+inf`、`+1.5` 之类带正号的浮点解析失败 | `fast_float_strtod.cpp` 编译时缺 `-DFASTFLOAT_ALLOWS_LEADING_PLUS` | 保留该宏，不要简化成默认 C++17 参数 |
| `make distclean` 之后重新构建仍复用旧产物 | deps 下的 `clean` / `distclean` 退化成 no-op | `clean` 必须清掉所有 `.o` / `.a`，`distclean` 额外清 jemalloc 生成物 |
| `BUILD_TLS=1` 时链接失败，缺 `libhiredis_ssl.a` | hiredis 未编 `ssl.c`，或系统无 OpenSSL 头文件 | 保留 `ssl.c` 的 best-effort 编译分支；缺 OpenSSL 开发包时先装再编 |
| `make codis-server` 在 `module_tests` 步骤非零退出 | `src/Makefile` 的 `all` 硬依赖 `tests/modules`，该目录需要提供空的 `all` / `clean` target | 保留 `tests/modules/Makefile`，让测试模块不阻塞 server 链接 |
| 改了 Redis 8 构建文件却不出现在 `git status` | 根 `.gitignore` 的 `makefile` 规则会误伤 `Makefile` | `extern/redis-8.6.3/Makefile` 与 `src/Makefile` 必须保持 tracked/unignored；不要在构建时 patch 被忽略的本地 Makefile |

## 多平台发布

默认 `make` 始终是 host build，多平台产物必须显式触发，避免普通开发机因为缺交叉工具链而无法构建。

```bash
make build-platforms                                  # 默认矩阵 darwin/arm64 linux/amd64 linux/arm64
make build-platforms TARGET_PLATFORMS="darwin/arm64"  # 只构建一个平台
make build-no-redis-platform TARGET_OS=darwin TARGET_ARCH=arm64
```

- **产物布局**：每个平台独立目录 `bin/<os>-<arch>/`，内含 `codis-dashboard`、`codis-proxy`、`codis-admin`、`codis-ha`、`codis-fe`、`assets/`、`codis-server` 及 `redis-benchmark` / `redis-cli` / `redis-sentinel` / `redis-check-aof` / `redis-check-rdb`，外加 `dashboard.toml`、`proxy.toml`、`redis.conf`、`sentinel.conf` 四份配置副本。`bin/<os>-<arch>-no-redis/` 只含 Go 入口、`assets/` 和配置。
- **平台校验**：`TARGET_OS` 只接受 `darwin` / `linux`，`TARGET_ARCH` 只接受 `amd64` / `arm64`，label 还要过字符白名单检查。校验发生在 `rm -rf bin/<label>` 之前，任何直接调用产物写入 target 的方式都绕不过去。
- **交叉构建 fail-fast**：target 与 host 不一致时，必须显式设置 `ALLOW_CROSS_FULL_BUILD=1` 并提供目标编译器（`PLATFORM_CC_<os>_<arch>`，必要时 `PLATFORM_CXX_<os>_<arch>`），否则命令直接失败并打印 host、target 和缺失条件，不会把 host 的 Redis 产物复制成 target 产物。
- **配置生成在 host 侧**：`default-configs` 先用 host 可执行程序生成配置到 `scripts/tmp/default-config-bin`，平台构建只复制配置，绝不在 host 上运行目标平台二进制。
- **产物平台自检**：构建结束对 `codis-dashboard`、`codis-proxy`、`codis-server` 跑 `file`，按目标平台匹配 Mach-O / ELF 与架构特征，不匹配即失败。

这层能力的边界需要写清楚：

1. 不保证在任意 host 上完整交叉构建任意 target；完整跨平台产物仍依赖目标平台构建环境或明确配置好的 C/cgo cross toolchain。
2. 不改变运行期协议、proxy 路由、dashboard 行为或 Redis 数据格式。
3. 不负责 Docker 镜像、CI 发布流水线、release 包上传或生产部署编排。
4. 不是纯 Go cross build：`codis-proxy` 与 Redis Server 的完整发布物仍带 C/cgo 边界，即使关掉 `cgo_jemalloc`，`pkg/utils/unsafe2/cgo_malloc.go` 仍然走 `import "C"`。

两个不要：不要为了通用 C 交叉编译去改 `extern/redis-8.6.3/src/Makefile`（平台检测归上游维护）；不要把不同平台的产物平铺进根 `bin/` 的文件名空间。

## 测试约定

- 常规入口是 `make gotest`，它执行 `go test ./cmd/... ./pkg/...`。项目只用 Go 内置 `testing`，不引入额外测试框架。
- 改代理、拓扑、store 等核心包时先跑目标包缩小反馈，例如 `go test ./pkg/proxy -run TestName`，确认通过后再按风险扩大范围。
- 涉及外部 binary 的 `os/exec` 测试不要硬编码 `/bin/xxx`：darwin 上 `/bin/true`、`/bin/false` 常落在 `/usr/bin/`。用 `exec.LookPath` 或多路径 fallback helper 取可执行文件位置。
- 需要模拟 Redis 协议时优先复用 `pkg/proxy/redis/redistest` 的共享 fake server：它负责 TCP 监听、RESP 编解码、命令 deep copy 与 `CountCommand`，默认返回 `OK`；`redistest.Array` / `Bulk` / `Int` / `String` / `Error` / `OK` 统一构造测试响应。
- 共享 helper 的职责停在 transport 层。业务语义留在各测试包自己的 handler 里，例如 `INFO` 返回哪些字段、`SLOTSMGRT-EXEC-WRAPPER` 返回哪个 code。两个以上包都需要同一套协议模拟时抽共享 helper，而不是复制一份"稍微改名"的 fake server；helper 要保持小而稳定，不把单个场景的状态塞进通用 struct。历史测试里包内 fake server 可以保留 handler 覆盖模式，不必为了统一强行迁走。
- 测试优先用 fake 替代外部服务：需要 Redis 协议时用 fake server，需要 coordinator 时用 fake store，避免单测依赖本机正在运行的 Redis 或 coordinator 实例。

## 目录与路径约定

| 路径 | 用途 |
| --- | --- |
| `cmd/` | 可执行入口：`dashboard`、`proxy`、`admin`、`ha`、`fe` |
| `pkg/` | 核心 Go 包（proxy、topom、models、utils 等） |
| `config/` | 运行期默认配置，`make` 相关 target 会自动刷新；`redis8.conf`、`sentinel8.conf` 是生成物 |
| `doc/` | 文档与图片 |
| `extern/redis-8.6.3/` | 内嵌 Redis 8.6.3 源码，必须被 git 跟踪 |
| `third_party/` | 本地 replace 的依赖副本（`jemalloc-go`、`rdb`） |
| `bin/` | 构建产物，`make clean` 直接删除，不要手工编辑 |
| `scripts/tmp/` | 构建中间产物（含 host 侧默认配置生成用的二进制） |
| `admin/` | 本地快速启动脚本 |
| `deploy/`、`kubernetes/`、`ansible/` | 部署材料 |

- 只有一处 Redis 8 源码：`extern/redis-8.6.3/`，不要另建根目录 `redis-8.6.3/`。`extern/redis-8.6.3/` 不能被 `.gitignore` 忽略，否则 Redis 8 的补丁改动无法被 `git status` 看到。
- `extern/` 下的生成物（`.o`、`.a`、jemalloc 的 `Makefile` / `lib/`）不要提交，也不要手改；需要重建时用 `make clean` / `make distclean` 再走正常构建。
- 重新生成 `config/dashboard.toml` 用 `go run ./cmd/dashboard --default-config`，它直接打印带注释的 `DefaultConfig` 常量；不要用 `Config.String()`，后者会丢掉注释压成裸 key 列表。

## 相关文档

- [architecture_zh.md](architecture_zh.md)：仓库分层、模块职责与关键代码位置。
- [redis8_validation_zh.md](redis8_validation_zh.md)：Redis 8 / Codis Server 的行为核对与兼容性说明。
- [redis8_codis_server_zh.md](redis8_codis_server_zh.md)：内嵌 Redis 8 在 Codis 下的改造点。
- [tutorial_zh.md](tutorial_zh.md)：从零搭一套可运行的 Codis 集群。
