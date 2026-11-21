# Redis 8 验证与切换

当前 `codis-server` 只内置 Redis 8（`extern/redis-8.6.3`）。本文给出 Redis 8 的验证门槛、Linux 正式验证要求和切换手顺；本地 macOS 结果只能证明验证编排与协议协作路径，不能替代 Linux 上的性能、fork/RDB、复制和部署包装结论。

历史背景：项目早期曾基于 Redis 3.2 内核；Redis 3 源码、构建目标和兼容性测试已从仓库移除，当前不再提供 Redis 3 fallback。

## 验证 Gate

| Gate | 触发命令 | 预期结果 | 失败时排查方向 |
| --- | --- | --- | --- |
| default-build | `make build-all` | `bin/codis-server` 是 Redis 8，Go 组件与默认配置可重建 | 构建与打包：确认 `extern/redis-8.6.3` 已展开、`config/redis.conf` 含 `codis-enabled yes`，版本信息看 `bin/version` |
| go-regression | `make gotest` | `go test ./cmd/... ./pkg/...` 全部通过 | Go 组件 adapter 或既有测试债务；先确认失败包是否落在 Redis 客户端、迁移编排路径上 |
| redis-tcl-codis-suite | `cd extern/redis-8.6.3 && ./runtest --single unit/codis --single unit/codis_migration --single unit/codis_slotsrestore --single unit/codis_async_migration` | Redis 8 Codis 四组 Tcl 用例通过 | Redis 8 Codis Server 的 slot、迁移、restore 实现；先看 runtest 日志里第一个失败用例 |
| e2e-local-codis-cluster | `python3 scripts/redis8_local_validation.py --only e2e --output redis8-evidence.json` | `semi-async` 与 `sync` 两轮临时集群都完成 proxy 写读、slot 分配、slot 迁移与迁移后读回 | 先看 evidence 里的 dashboard、proxy、redis 日志，再判断落在 topom、proxy 还是 server 集成边界 |
| linux-formal-handoff | 逐项落实下文“Linux 正式验证要求” | 环境字段、功能与性能命令、fork/RDB、复制、部署包装都有结论 | 验证规划缺项：缺什么就算未验证，不得用本地数字补位 |
| cutover-runbook-draft | 评审下文“切换手顺” | preflight、canary、ramp-up、full cutover、rollback 五阶段齐全 | 运维规划缺项：补阶段与阶段退出条件后再动手 |

判定规则：

- 失败归属：二进制缺失或版本不对归构建与打包；Tcl 失败归 Redis 8 Codis Server；Go 回归失败归 Go 组件 adapter 或既有测试债务；e2e 失败先看 evidence 日志再归集成边界。
- 迁移失败但源端 key 保留是可观测失败，交 Linux 阶段复核；失败且源端 key 消失直接阻塞 cutover。

## 本地验证脚本

`scripts/redis8_local_validation.py` 是本地非性能验证入口，用真实二进制起短生命周期集群跑端到端场景，不跑 `redis-benchmark`，也不给出吞吐或延迟结论：

```text
python3 scripts/redis8_local_validation.py [--repo <repo root>] [--only all|e2e] [--output <evidence.json>] [--keep-temp]
```

- `--repo`：仓库根目录，默认取脚本所在仓库。
- `--only`：默认 `all`，目前等价于 `e2e`；也可显式写 `--only e2e`。
- `--output`：把 JSON 证据写入指定路径；不传则只打印到标准输出。
- `--keep-temp`：保留临时工作目录，便于事后查日志；不加时通过后自动清理。

前置条件是 `bin/` 下已存在 `codis-admin`、`codis-dashboard`、`codis-proxy`、`codis-server`、`redis-cli`，缺任何一个直接报错退出。进程退出码 0 表示所选场景全部通过，1 表示存在失败场景，失败原因写在证据 JSON 的 `error` 字段。

- e2e 场景：对 `semi-async` 和 `sync` 各起一套临时集群（dashboard、proxy、两个 Redis 8 group master，端口自动分配），经 proxy 写入普通 key、带 hash tag 的 key 和非 0 DB key，再通过 dashboard 把这些 slot 迁到 group 2 并读回，同时校验源端与目标端的 key 归属。

## Linux 正式验证要求

正式判定必须在 Linux 上完成，本地 macOS 结果不能替代。以下内容按顺序执行，缺项按未验证处理。

### 必须记录的环境字段

| 类别 | 内容 |
| --- | --- |
| 仓库版本 | Codis commit、分支、dirty diff 摘要 |
| 操作系统 | 发行版与内核版本 |
| 硬件 | CPU 型号与核心数、内存、磁盘类型与文件系统 |
| 构建工具链 | Go 版本、compiler flags、实际执行的 `make` 命令、`bin/version` 内容 |
| 后端二进制 | Redis 8 Codis Server 路径 |
| 运行配置 | dashboard、proxy、server 的关键配置 diff |
| 测试资源 | 测试端口、数据目录、日志目录 |
| 清理结果 | 临时目录、临时集群与端口是否回收 |

### 必跑命令

功能 gate：

```bash
make build-all
make gotest
cd extern/redis-8.6.3 && ./runtest --single unit/codis --single unit/codis_migration --single unit/codis_slotsrestore --single unit/codis_async_migration
cd - && python3 scripts/redis8_local_validation.py --output redis8-linux-evidence.json
```

构建机依赖：Linux 上 `codis-proxy` 依赖 `jemalloc-config` 目标，会重新运行 jemalloc autogen，需要 `autoconf`、`automake`、`libtool`、`gawk`。仓库里随代码带来的 jemalloc 头文件是在 macOS 上生成的，**不能复用到 Linux 构建**，必须在目标机重新生成。

性能 gate（只在 Linux 执行）：

```bash
bin/redis-benchmark -h 127.0.0.1 -p <redis8-direct-port> -t get,set -n <N> -c <C> -P <P>
bin/redis-benchmark -h 127.0.0.1 -p <codis-proxy-port> -t get,set -n <N> -c <C> -P <P>
```

部署包装 gate：

```bash
make docker
docker run --rm <codis-image> <smoke-command>
```

Docker daemon 不可用时只能记为 blocked，不能写成已验证。

### 必须产出的结果

- 两条 benchmark 的原始输出与测试参数；proxy 相对直连 Redis 8 的开销必须用 Linux 实测，不得用 macOS 数字补位。
- fork/RDB：`BGSAVE` 是否成功、日志、耗时、RDB 文件大小。执行前记录 `vm.overcommit_memory`，为 0 时记录 Redis 的 overcommit 警告，并在生产切换前评估改为 1。
- 复制：master/replica link 状态、初次同步、断连恢复。复制关系必须写入持久配置（`redis.conf` 的 `replicaof` 或服务启动参数），不能只执行运行时 `SLAVEOF`；重启副本后要确认 link 恢复且新增写入可读。
- 迁移与包装：迁移期间持续读写的表现、slot action 是否清零、失败重试行为；镜像构建、启动命令、默认配置与日志路径。

### 阻塞条件

- Linux 上 `make gotest` 或 Redis 8 Codis Tcl suite 失败，且没有明确的环境解释。
- e2e 的 `semi-async` 或 `sync` 任一路径失败。
- 迁移失败造成源端 key 静默丢失。
- Redis 8 直连或 proxy 路径性能明显低于可接受阈值，且没有容量预案。
- fork/RDB、复制、Docker/部署包装任一生产必需路径未验证。

## 切换手顺

五个阶段顺序执行，上一阶段不通过不进入下一阶段。任何阶段都禁止绕过 dashboard/topom 直接改 coordinator 元数据或数据目录。

### 1. Preflight

- 确认当前 Codis commit、默认 `bin/codis-server` 指向 Redis 8，且只使用仓库内 Redis 8 构建产物。
- 跑完 Linux 功能 gate 与性能 gate，并完成 fork/RDB、复制、迁移期间读写、Docker 包装检查。
- 备份 coordinator 元数据、Redis 数据目录、关键配置，记录操作窗口。
- 明确 Redis 8 的 RDB/AOF 与旧大版本不保证兼容；回滚只能依赖已验证的备份或恢复方案。

### 2. Canary

- 新建少量 Redis 8 group，先不改动现有 coordinator 元数据；所有 group、server、slot、proxy 操作只经 dashboard/topom 或 `codis-admin`。
- 选低风险 slot 范围迁到 Redis 8 group，验证 proxy 写读、dashboard slot action、Redis 侧 `SLOTSINFO` 与 key 归属、业务监控。
- 观察窗口内不扩大范围；出现异常先冻结新的迁移。

### 3. Ramp-up

- 按 group 或 slot range 分批扩大 Redis 8 承载范围。
- 每批记录迁移前 slot owner、迁移命令、迁移耗时、失败重试和读写错误率。
- 迁移期间持续运行读写探针，确认 proxy 不返回异常的 `MOVED`、`ASK` 或 backend error；每批结束后等监控稳定再进入下一批。

### 4. Full cutover

- 全量 slot 迁到 Redis 8 group 后，确认 dashboard/topom 的 slot action 清零，并确认 proxy 列表、group 状态、复制状态和最近一次 RDB/fork 结果。
- 保留迁移前的旧 group 数据与备份到既定观察窗口结束。
- 只有 Linux 正式性能与稳定性结论都通过后，才标记切换完成。

### 5. Rollback

- Canary 阶段：迁移前的旧 group 仍是权威数据源，可通过 dashboard/topom 把 canary slot 迁回。
- Ramp-up 阶段：Redis 8 已承接写入时，优先使用已经验证过的反向迁移方向或恢复备份；未验证的方向不能临场假设可用。
- Full cutover 后：Redis 8 已是唯一写入源，回滚只能依赖已验证的反向迁移或备份恢复。禁止直接编辑文件系统、ZooKeeper、Etcd 等 coordinator 元数据绕过 topom，也禁止用不兼容的数据文件直接启动旧版本回滚。

## Go 组件兼容矩阵

下表把 Go 入口与 Redis 8 契约的对应关系压缩保留。“fake test”指用假 Redis 的单元测试，“smoke”指对真实 Redis 8 Codis Server 的受控验证。

| Go 入口 | Redis 8 契约 | 验证方式 |
| --- | --- | --- |
| `pkg/utils/redis.Client.InfoFull` | `INFO` 返回普通 `key:value` 文本，replica 字段名 `master_host`、`master_port`、`master_link_status` 保持可解析，无副本时不生成 `master_addr`；`CONFIG GET maxmemory` 返回 `[maxmemory, integer]` | fake test + smoke |
| `pkg/proxy.BackendConn.KeepAlive` | `loading` 为 1 或 `master_link_status` 为 down 时不恢复 connected | proxy fake test |
| `pkg/utils/redis.Client.SetMaster`、`cmd/admin`、`cmd/ha` | `CONFIG REWRITE` 不丢 `codis-enabled` 等 Codis 自定义配置；`CLIENT KILL TYPE normal` 可用，事务子命令 error 必须冒泡；发送 `SLAVEOF`（Redis 8 支持的别名） | fake test + smoke |
| `pkg/utils/redis.NewClient`、`pkg/proxy.BackendConn.verifyAuth` | `AUTH <password>` 对 default user 成功；失败返回错误且不泄露密码 | fake test + smoke |
| `pkg/utils/redis.Client.Select`、`pkg/proxy.BackendConn.selectDatabase` | `SELECT <db>` 后 slot 命令和迁移只作用当前 DB | fake test + smoke |
| `pkg/utils/redis.Client.SlotsInfo`、proxy `SLOTSINFO` | `[[slot_id,key_count], ...]`；畸形返回报错 | fake test + smoke |
| `pkg/utils/redis.Client.MigrateSlot`、`Client.MigrateSlotAsync`、topom slot executor | `SLOTSMGRTTAGSLOT` 与 `-ASYNC` 返回 `[migrated_count, remaining_count]`，restore async 的 ACK error 不被吞 | fake test + smoke |
| proxy `forwardSemiAsync` | `SLOTSMGRT-EXEC-WRAPPER`：`[0,err]` 重试、`[1,err]` 等待/阻塞、`[2,reply]` 直接返回 | proxy fake test |
| `pkg/proxy/mapper.go` | 不放开 `CONFIG`、`SLAVEOF`、`SLOTSMGRT*`、`SLOTSRESTORE*` 等危险命令 | mapper test + diff review |
| Redis 8 server async restore | `AUTH2` 只验证命令存在与 ACK/error 语义；Go 侧不新增 username 配置 | smoke（可选） |

真实 Redis 8 Codis Server 的 smoke 没有暴露必须新增生产 adapter 的不兼容点：`SLAVEOF` alias 可用，`INFO` 字段名仍可被 Go parser 消费，`SLOTSINFO` 与同步/异步迁移返回格式符合严格 parser 预期，`CONFIG REWRITE` 未丢 `codis-enabled`。

## 相关文档

- [build_and_release_zh.md](build_and_release_zh.md)：构建目标、平台矩阵与发布产物。
- [redis8_codis_server_zh.md](redis8_codis_server_zh.md)：Redis 8 Codis Server 的 Codis mode、slot 模型与迁移命令。
- [architecture_zh.md](architecture_zh.md)：组件职责、状态归属与模块位置。
