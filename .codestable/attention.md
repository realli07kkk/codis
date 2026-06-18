# Attention

本文件是 CodeStable 技能启动必读的项目注意事项入口。所有 CodeStable 子技能开始工作前必须读取它。

## 项目碎片知识

<!-- cs-note managed: 用 cs-note 维护，新条目按下面分节追加 -->

### 编译与构建

- 本仓库已完成 Go modules 构建迁移，`go.mod` / `go.sum` 是默认 Go 依赖入口；不要恢复旧 GOPATH/vendor 构建路径。
- 默认构建命令是 `make`；它会构建 Go 二进制、嵌入式 Redis、前端资源和默认配置。
- 多平台发布产物走显式 `make build-platforms` 或 `make release-platforms`；默认 `make` 仍是 host build，非 host full artifact 需要 C/cgo cross toolchain。
- 不要顺手现代化 Go 依赖；依赖版本偏离必须有可复现的现代 Go 编译原因。
- `cgo_jemalloc` 的 module mode 来源现在走 `go.mod` 的 `replace github.com/spinlock/jemalloc-go => ./third_party/jemalloc-go`；后续相关修改应改 `third_party/jemalloc-go`，不是旧 `vendor/github.com/spinlock/jemalloc-go`。
- 涉及 Redis 8 源码 object 接入时，相关 Redis Makefile 必须是 tracked/unignored 构建输入；不要在构建时 patch ignored 本地 Makefile。
- `make codis-server` 编译嵌入式 Redis 8 需要 C/C++ 工具链和 autotools：`dnf install -y gcc gcc-c++ autoconf automake libtool`（Rocky/EL）或等价的 apt 包。构建过程中 jemalloc 会自动完成 autogen → configure → make。

### 运行与本地起服务

- 运行期配置在 `config/`；快速启动脚本在 `admin/`；部署材料在 `deploy/`、`kubernetes/`、`ansible/`。

### 测试

- 项目测试使用 Go 内置 `testing`。常规入口是 `make gotest`，它会跑 `go test ./cmd/... ./pkg/...`。
- 代理或拓扑相关改动先跑目标包测试，例如 `go test ./pkg/proxy -run TestName`，再视风险跑更大范围。
- darwin 开发机上 `/bin/true` `/bin/false` 路径不固定（常在 `/usr/bin/`），写涉及外部 binary 的 `os/exec` 测试不要硬编码 `/bin/xxx`，用 `exec.LookPath` 或多路径 fallback helper。

### 命令与脚本陷阱

- `make codis-proxy` 和 `make codis-dashboard` 会刷新对应默认配置文件。
- 重生成 `config/dashboard.toml` 用 `go run ./cmd/dashboard --default-config`（直接 print `DefaultConfig` 常量含注释），不要用 `Config.String()`（后者丢注释压成裸 key 列表）。
- `make clean` 会删除 `bin/`、`scripts/tmp` 和测试临时文件；`make distclean` 还会清理嵌入式 Redis 与 jemalloc 构建输出。
- 当前环境没有 `python` 命令；运行 `.codestable/tools/*.py` 时使用 `python3`。
- 不要用全量 `go mod tidy` 收口本仓库；它会扫描 etcd 依赖测试链路，更新 `go.mod` 时优先用验收命令驱动最小机械变化。
- 除非任务明确要求 Docker / 容器相关内容，默认不要修改 Dockerfile、Docker 脚本或把 docker build / Docker daemon 检查作为必跑验收。
- `codis-server-redis3` 不接受 Redis 8 的 `codis-enabled yes` 配置；跨版本直连验证时不要把 Redis 8 配置原样用于 Redis 3 fallback。

### 路径与目录约定

- 可执行入口在 `cmd/`；核心 Go 包在 `pkg/`；文档和图片在 `doc/`。
- 避免编辑 `bin/` 生成物和 `extern/`；旧 `vendor/` / `Godeps/` 已退休，不要恢复。
- Redis 8 源码固定在 `extern/redis-8.6.3/`，不要再使用根目录 `redis-8.6.3/`；该目录不应被 `.gitignore` 忽略，后续 Redis 8 patch 改动必须能被 `git status` 看到。

### 环境变量与凭证

### 其他

- 本项目重视 Redis/proxy 行为稳定性和兼容性，默认遵守 Never break userspace，优先小范围兼容修复。
