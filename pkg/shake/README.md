# pkg/shake — 内嵌的 RedisShake 内核

本目录是 RedisShake `internal/` 的逐文件拷贝，用于给 Codis 数据集成控制台
（`cmd/shake` 的 `codis-shake` + dashboard `DataMigrationManager`）提供
Reader / Writer / RDB 解析 / 过滤 / 状态统计能力。

## 来源（provenance）

| 项 | 值 |
| --- | --- |
| 上游仓库 | `https://github.com/tair-opensource/RedisShake` |
| 分支 | `v4` |
| tag | `v4.8.0` |
| 提交 | `ec0b08a`，`git describe` = `v4.8.0-2-gec0b08a`（tag 之后 2 个上游提交：`083f034 fix: flush buffered commands before waiting for replies`、`ec0b08a fix: correct Valkey detection and HASH_2 TTL rewrite`；两者都是上游修复，不是 Codis 改动） |
| 拷贝范围 | 上游 `internal/` 下全部 67 个非测试 `.go` 文件（`aof`、`client`、`client/proto`、`commands`、`config`、`entry`、`filter`、`log`、`rdb`、`reader`、`status`、`utils`、`writer`） |
| 版本常量 | `cmd/shake --version` 打印 `redis-shake v4.8.0` |

Go 的 `internal` 包规则禁止其它 module 导入上游 `internal/`，因此这里采用
“源码拷贝 + 改写 import path”的方式集成，而不是 `go.mod replace`。

## 与上游的差异

只保留可 diff 的最小改动。核对方式是把本目录每个文件的
`github.com/CodisLabs/codis/pkg/shake/` 前缀替换回 `RedisShake/internal/` 后与上游逐文件
`diff`：除下表三个文件外，其余 64 个 `.go` 文件与上游**逐字节一致**（仅 import path 不同）。

| 文件 | 差异 | 原因 |
| --- | --- | --- |
| 全部 `.go` | `"RedisShake/internal/..."` → `"github.com/CodisLabs/codis/pkg/shake/..."` | 包路径迁移 |
| 全部上游 `*_test.go` | 未拷贝；本目录只有 Codis 自己写的 `shake_test.go`（校验 `config.Default()` 默认值、命令表 key 位置、`utils` 解析、`status` 计数等） | 上游测试依赖 `testify`；Codis 侧集成测试在 `pkg/migration`、`pkg/topom` |
| `client/proto/license.txt` | 未拷贝（上游只有一行 “port from go-redis/internal/proto” 的来源说明） | 纯注释，不影响构建；升级时如需严格保留可补拷 |
| `config/config.go` | 删除 `LoadConfig` / `envWithFallback`（viper + TOML + zerolog 控制台输出），新增 `Default()`（内部只调 `defaults.SetDefaults`） | `codis-shake` 通过 JSON spec 接收配置，不读 TOML，也不引入 viper |
| `log/func.go` | `Panicf` 的栈帧前缀从 `redis-shake/main.go` 改为 `github.com/CodisLabs/codis/cmd/shake/main.go`（输出显示为 `codis/cmd/...`）；`os.Exit(1)` 语义不变 | 栈日志指向真正的入口 |
| `status/handler.go` | 状态端口从 `:<port>` 改为只绑定 `127.0.0.1:<port>`，并只打印一行状态地址提示 | 状态由同机 dashboard 轮询，不对网络暴露 |
| 格式化 | 一律不对上游文件执行 `gofmt` | 保持与上游可 diff。`gofmt -l pkg/shake` 当前会列出 7 个文件：`client/sentinel.go`、`reader/interface.go`、`utils/file_rotate/aof_reader.go`、`writer/file_writer.go`、`writer/interface.go`（import path 改写后 import 块不再有序），以及 `rdb/structure/listpack.go`、`rdb/types/hash.go`（上游本身未格式化）。升级时不要顺手 gofmt |

## 为什么每个迁移任务一个进程

RedisShake 的运行模型是“一个进程一个任务”：

- `config.Opt`、`status` 包的全局状态只支持单个任务；
- 任何致命错误都走 `log.Panicf` → `os.Exit(1)`；
- `utils.ChdirAndAcquireFileLock` 会 `chdir` 到任务目录并用 `pid.lockfile` 加文件锁（同一任务目录只允许一个实例）；
- `scan_reader` 会预分配可容纳 1 亿 key 的队列（`utils.NewUniqueQueue(100000000)`），内存占用高。

因此 Codis 不在 dashboard 进程内直接调用这些包，而是由 dashboard 为每个任务拉起
`bin/codis-shake --spec <job>/spec.json` 子进程（工作目录为任务目录），通过
`status_port`（只绑 `127.0.0.1`）的 HTTP 接口采集进度，用 SIGTERM / SIGKILL 控制
生命周期。运行方式与上游 `redis-shake <config>` 一样是“一进程一任务”，差别只在
配置来源：上游读 TOML，这里由 dashboard 生成 `spec.json`。

## Codis 相关的组合层

Codis 不是 Redis Cluster：proxy 不支持 `PSYNC`、`RESTORE`、`SCAN`（`pkg/proxy/mapper.go`
里均带 `FlagNotAllow`），默认也不提供 `CLUSTER NODES`（只有启用
`cluster_nodes_compat` 时才输出伪节点清单）。因此“Codis 作为源端 / 目标端”时，
dashboard 在创建任务时从 topom 快照导出 Group Master 地址与 1024 slot 归属写入 spec，
由 `pkg/migration` 用本目录的 `reader` / `writer` 组合出静态拓扑的
`StaticClusterReader` / `StaticClusterWriter`：每个分片一个 standalone reader，
写入按 Codis 的 `crc32(hashTagOrKey) % 1024`（与 `pkg/proxy.Hash` 同一实现）路由，
无 key 的命令广播到所有分片，多 key 跨分片按上游 CROSSSLOT 语义 `log.Panicf`。

## 升级上游时的流程

1. 按上表的 tag/提交重新拷贝 `internal/`（跳过 `*_test.go`）。
2. 批量改写 import path 前缀。
3. 重新应用 `config/config.go`、`log/func.go`、`status/handler.go` 三处改动。
4. 不要 `gofmt` 上游文件。
5. 用上面的“替换 import path 后 diff”方法确认没有其它漂移，再跑
   `go test ./pkg/migration ./pkg/shake ./cmd/shake ./pkg/topom`。
