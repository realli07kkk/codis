# Attention

本文件是 CodeStable 技能启动必读的项目注意事项入口。所有 CodeStable 子技能开始工作前必须读取它。

## 项目碎片知识

<!-- cs-note managed: 用 cs-note 维护，新条目按下面分节追加 -->

### 编译与构建

- 本仓库是旧式 GOPATH Go 项目，没有 `go.mod`；使用 legacy Go 工具链时保持路径为 `github.com/CodisLabs/codis`。
- 默认构建命令是 `make`；它会构建 Go 二进制、嵌入式 Redis、前端资源和默认配置。
- 不要在未明确要求时把项目迁移到 Go modules，也不要顺手现代化依赖。
- `cgo_jemalloc` 的 module mode 来源现在走 `go.mod` 的 `replace github.com/spinlock/jemalloc-go => ./third_party/jemalloc-go`；后续相关修改应改 `third_party/jemalloc-go`，不是旧 `vendor/github.com/spinlock/jemalloc-go`。

### 运行与本地起服务

- 运行期配置在 `config/`；快速启动脚本在 `admin/`；部署材料在 `deploy/`、`kubernetes/`、`ansible/`。

### 测试

- 项目测试使用 Go 内置 `testing`。常规入口是 `make gotest`，它会跑 `go test ./cmd/... ./pkg/...`。
- 代理或拓扑相关改动先跑目标包测试，例如 `go test ./pkg/proxy -run TestName`，再视风险跑更大范围。

### 命令与脚本陷阱

- `make codis-proxy` 和 `make codis-dashboard` 会刷新对应默认配置文件。
- `make clean` 会删除 `bin/`、`scripts/tmp` 和测试临时文件；`make distclean` 还会清理嵌入式 Redis 与 jemalloc 构建输出。
- 当前环境没有 `python` 命令；运行 `.codestable/tools/*.py` 时使用 `python3`。

### 路径与目录约定

- 可执行入口在 `cmd/`；核心 Go 包在 `pkg/`；文档和图片在 `doc/`。
- 避免编辑 `bin/` 生成物、`vendor/`、`Godeps/`、`extern/`，除非任务明确要求。

### 环境变量与凭证

### 其他

- 本项目重视 Redis/proxy 行为稳定性和兼容性，默认遵守 Never break userspace，优先小范围兼容修复。
