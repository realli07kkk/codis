# Codis 架构总览

Codis 是 Redis 的代理式分片集群：客户端只连接 Codis Proxy，proxy 按固定 1024 个 slot 把命令路由到后端 Codis Server；dashboard/topom 通过 coordinator 存储维护 slot 映射、group、proxy、sentinel、ACL 等元数据，并驱动数据迁移、配置下发和故障切换。本文记录仓库当前已落地的拓扑、状态归属与代码位置，用于判断一次改动会触及哪个入口、哪个包、哪些运行期状态，以及哪些变更必须经过 dashboard/topom。

## 术语

| 术语 | 含义 | 位置 |
|---|---|---|
| Codis Proxy | 面向客户端的 Redis 协议代理：认证、命令解析、slot 路由、pipeline 写回和运行期指标 | `cmd/proxy/main.go`、`pkg/proxy/proxy.go` |
| Topom / Codis Dashboard | 集群拓扑管理进程，持有 store、cache、slot action、stats 和 HA 状态，对外暴露管理 HTTP API | `cmd/dashboard/main.go`、`pkg/topom/topom.go` |
| Coordinator / Store | 集群元数据的外部存储抽象：读写、前缀列表、带 revision 的持久锁与带 lease 的在线记录；唯一后端是外部部署的 etcd 3.7.x | `pkg/models/client.go`、`pkg/models/store.go`、`pkg/models/etcd` |
| Slot / Group | Slot 是路由分片单位，固定 1024 个，模型常量与 proxy 内部状态分属两层；Group 是一组后端 Redis Server，第一台按现有代码语义承担 master 位置 | `pkg/models/slots.go`、`pkg/proxy/slots.go`、`pkg/models/group.go` |
| Hot key cache | proxy 进程内、默认关闭、按 exact key 配置的短 TTL string 读缓存；只作用于稳定 slot 上的 `GET`/`MGET` 完整 bulk value，不进入 coordinator；启用广播后 source proxy 可经 dashboard 通知其他 online proxy 删除本地条目 | `pkg/proxy/hot_key_cache.go`、`pkg/proxy/hot_key_cache_broadcast.go` |
| Session auth brute-force guard | 默认关闭的 proxy 客户端认证保护，覆盖单密码和 Codis ACL 模式。各 proxy 按来源 IP 记录失败次数和临时锁定截止时间 | `pkg/proxy/auth_bruteforce.go` |
| Codis-managed Redis ACL | 默认关闭的 Redis 8 ACL 管理与 proxy 多账号认证：dashboard 是管理面，coordinator 存 `/codis3/{product}/acl` 目标 revision，topom 先同步后端 Redis 再下发 proxy snapshot；ACL 用户可绑定固定 db（DB-bound ACL user），认证后 proxy 以 Forced DB 强制该 session 路由到绑定 db，优先级高于客户端 `SELECT` | `pkg/topom/topom_acl.go`、`pkg/models/acl.go` |
| Proxy QPS limit | 默认关闭的单 proxy 进程级普通请求 QPS 限流；目标 limit 由 dashboard 写入 coordinator 再经 proxy admin API 下发 | `pkg/proxy/qps_limiter.go`、`pkg/topom/topom_qps_limit.go` |
| Cluster nodes compat | proxy 进程内、默认关闭的 `CLUSTER NODES` 兼容输出，仅为 cluster-mode SDK bootstrap 生成伪 Redis Cluster 节点清单，不改变 1024 slot 真实路由 | `pkg/proxy/cluster_nodes.go` |
| Stream command support | proxy 对 Redis Stream 命令的受控路由子集：单 stream 命令、`XGROUP`/`XINFO` 容器命令、非阻塞同 hash tag `XREAD`/`XREADGROUP`；不是完整 Stream 语义承诺 | `pkg/proxy/stream_commands.go` |
| RDB Analysis / Remote RDB fetch | dashboard 进程内的离线 RDB 文件分析，输出 DB/type/prefix 聚合、big keys、hot keys 与 flamegraph；输入来自上传、受控 workspace 路径，或 dashboard 从当前 product 内 Redis 8 Codis Server 的 RDB HTTP export 受控拉取。不在 proxy 请求路径中执行 | `pkg/topom/topom_rdb_analysis.go`、`pkg/topom/topom_rdb_analysis_remote_fetch.go` |
| RDB HTTP export | Redis 8 Codis Server 上默认关闭的本机 RDB 导出入口：复用 Redis 监听端口，只接受 `GET /codis/rdb/latest` 与 `X-Codis-RDB-Auth` header，不调用 `SAVE`/`BGSAVE` | `extern/redis-8.6.3/src/codis_rdb_export.c` |
| PITR / 数据闪回 | 默认关闭的 dashboard 进程内单 Redis 8 Codis Server AOF 时间点截断恢复编排，复用原生 `aof-timestamp-enabled` 与 `redis-check-aof --truncate-to-timestamp`，不修改 Redis C 源码 | `pkg/topom/topom_pitr.go`、`pkg/topom/topom_pitr_run.go` |
| 数据集成 / 数据迁移 | dashboard 的数据迁移控制台：每个任务由 dashboard 拉起一个 `codis-shake` 子进程执行内嵌 RedisShake v4.8.0 内核，支持 standalone / Redis Cluster / Codis 静态拓扑之间的 `sync` / `scan` / `rdb` 迁移；任务记录只落 dashboard 本地 workspace，不写 coordinator | `pkg/topom/topom_data_migration.go`、`pkg/migration`、`pkg/shake`、`cmd/shake` |

## 系统定位

- 数据面由 proxy 承担：客户端只看到单点 Redis，slot 决定 key 归属，proxy 本身不持久化业务数据；coordinator 只保存拓扑与配置，限流 token、热 key 缓存、防暴破计数、RDB analysis 与 PITR job 只活在单个进程内。
- dashboard/topom 持有拓扑锁，负责常规拓扑变更、迁移动作、ACL 和 QPS limit 目标配置。同一 product 同一时刻只有一个 active dashboard。Admin 的锁恢复和元数据导入是直接访问 coordinator 的运维入口，见 [使用教程](tutorial_zh.md)。
- 不是 Redis Cluster：不实现 cluster bus、gossip、`MOVED`/`ASK`，`CLUSTER NODES` 只是可选兼容输出；Redis 8 Codis Server 使用 `codis-enabled yes` 而非 `cluster-enabled`。

## 组件与交互

```mermaid
flowchart LR
  Client[Redis 客户端] --> Proxy[Codis Proxy]
  Proxy --> Server[Codis Server / Redis 8]
  Dashboard[Codis Dashboard / Topom] -->|admin HTTP API| Proxy
  Dashboard -->|迁移 / ACL / 复制| Server
  Dashboard <--> Store[Coordinator: etcd 3.7.x]
  FE[Codis FE] --> Dashboard
  FE -.->|动态发现| Store
  Admin[Codis Admin] --> Dashboard
  Admin -.->|元数据 / 锁 / 在线记录| Store
  HA[Codis HA] --> Dashboard
  Proxy -.->|在线注册 / 在线列表| Store
  Dashboard -->|启动任务| Shake[Codis Shake]
  Shake -->|直连| Server
```

边界说明：etcd 只保存控制面（拓扑、目标配置、拓扑锁和 proxy 在线记录），业务键始终由 codis-server 保存；proxy 转发业务请求时只读本地路由，不访问 etcd。

| 组件 | 进程入口 | 主要职责与共享状态 |
|---|---|---|
| codis-proxy | `cmd/proxy/main.go` | Redis 协议代理、slot 路由、本地命令、限流/缓存/ACL 运行态；开启 `etcd_register_online` 后向 etcd 注册在线记录，`CLUSTER NODES` 的 `all` 模式读在线列表；被 dashboard 通过 admin API 下发 |
| codis-dashboard | `cmd/dashboard/main.go` | 拓扑管理、slot action、配置管理面、RDB Analysis、PITR 与数据迁移任务管理。读写 etcd 并持有持久拓扑锁 |
| codis-server | `extern/redis-8.6.3` | 后端存储；Codis mode 下按 1024 slot 组织 keyspace 并支持迁移命令，不依赖 coordinator |
| codis-shake | `cmd/shake/main.go` | 单个 RedisShake 迁移任务的执行进程，直连源端与目标端 |
| codis-fe / codis-admin / codis-ha | `cmd/fe/main.go`、`cmd/admin/main.go`、`cmd/ha/main.go` | FE 反向代理和动态发现；Admin 管理 API 与 coordinator 运维入口（元数据导出/导入、查锁/解锁）；HA 通过 dashboard API 巡检和 promote |
| coordinator / 在线注册 | `pkg/models/etcd`、`pkg/models/store.go`、`pkg/proxy/etcdonline`、`pkg/proxy/online.go` | 保存 `/codis3/{product}` 元数据、不带 lease 的拓扑锁与 `/codis3/{product}/online-proxies/{token}` 带 lease 的在线记录 |

### Codis Proxy

- 启动与会话：`Proxy.New` 创建 router、启动 admin/proxy 服务与 metrics reporter，`Proxy.setup` 填入两个 listener 的 model；`serveProxy` accept 后为每个连接创建 `Session`，reader 解码 Redis multi bulk 生成 `Request` 交给 router，writer 等待后端响应后按原序写回。活动 session 注册进进程内 `clientSessionRegistry`，只服务 `CLIENT LIST` 与 `SessionsAlive()`，不进 coordinator、不跨 proxy 同步。
- 命令分流：`Session.handleRequest` 先处理 `QUIT`、`AUTH` 与鉴权状态，再执行可选 QPS 检查，然后处理 `SELECT`、`CLIENT LIST`、`ACL WHOAMI`、启用后的 `CLUSTER NODES` 等本地命令，`GET`/`MGET`/`MSET`/`DEL`/`EXISTS`/`SLOTSINFO`/`SLOTSSCAN`/`SLOTSMAPPING` 走特殊处理，其余命令走 `Router.dispatch`。
- 转发与迁移：`Router.dispatchByHashKey` 用 hash key 计算 slot 并调用 `Slot.forward`；迁移中的 slot 由 `forwardSync`/`forwardSemiAsync` 包装，`SLOTSMGRT-EXEC-WRAPPER` 由 `forwardHelper.slotsmgrtExecWrapper` 解析。
- 默认关闭的本地能力：Hot key cache、QPS limit、brute-force guard、`CLUSTER NODES` 兼容、ACL 多账号，均只影响单个 proxy 进程；观测面是 `/api/proxy/...` 的 stats/model/slots API 与 JSON、InfluxDB、StatsD 上报。

### Codis Dashboard / Topom

- `topom.New` 载入 store 与 cache，`Topom.Start` 启动周期任务：刷新 Redis/proxy stats、处理 slot action、观察 sentinel；启动时通过 `Store.AcquireLock` 用事务（锁键版本为 0）抢占不带 lease 的 `/codis3/{product}/topom` 持久锁，同一集群同时只能有一个 active dashboard；锁值含每进程随机的 `owner_id`，正常停止按本次 `create_revision` 释放，异常退出留下的锁由运维用 Admin 查/解锁流程处理。
- 下发：`ReinitProxy` 向 proxy 填充 slot、启动 proxy 并设置 sentinel；局部 slot 变更由 `resyncSlotMappings` 并发调用所有 proxy 的 `FillSlots`。
- 迁移：`SlotCreateAction*` 在 slot mapping 上写 action，`ProcessSlotAction` 与 `newSlotActionExecutor` 把状态从 pending 推进到 preparing/prepared/migrating/finished，实际搬迁调用 Redis 侧 `SLOTSMGRT*`。
- `newApiServer` 注册拓扑、ACL、QPS limit、热 key 缓存失效、RDB Analysis、PITR 和数据迁移路由。stats 和 slots 分别有带 `xauth` 的 API 和直接读取路由，具体路由见 [topom_api.go](../pkg/topom/topom_api.go)。RDB Analysis 和 PITR job 保存在进程内。数据迁移尽力将脱敏快照写入本地 `jobs.json`，重启时把已落盘的非终态任务标记为 `failed`。
- 数据迁移控制台：`DataMigrationManager` 为每个任务在 `data_migration_workspace/jobs/<id>/` 写 `spec.json`，拉起 `codis-shake --spec <job>/spec.json` 子进程（工作目录为任务目录），轮询 spec 指定的 loopback `status_port` 上的 RedisShake 原生状态 JSON（直接 `GET /`），读取 `shake.log`/`console.log`，用 SIGTERM → 15s → SIGKILL 停止；dashboard 关闭时对非终态任务并行停止，所有 worker 共用一个 5s 宽限（超时 SIGKILL 后再最多等 5s），关闭耗时与任务数无关。worker 每 5 秒检查 `parent_pid`，父进程消失后自行停止，避免孤儿进程。`/api/topom/data-migration/*` 提供 create/list/get/stop/remove/log 与 Codis 拓扑预览。

### Codis Server（内嵌 Redis 8）

- 历史背景：早期版本基于 Redis 3.2 内核，该支持已移除；当前唯一内置版本为 Redis 8。
- 源码位于 `extern/redis-8.6.3`。`codis-enabled` 与 `cluster-enabled` 互斥；Codis mode 保留 standalone 多 DB 行为，DB keyspace 使用 10-bit `kvstore`/`estore` 分区，slot keyspace 的唯一权威来源是 `kvstore`，不维护 `hash_slots[1024]` 平行索引。slot 命令为 `SLOTSHASHKEY`、`SLOTSINFO`、`SLOTSSCAN`、`SLOTSDEL`、`SLOTSCHECK`：`SLOTSDEL` 先收集 key 快照再走 Redis 正常删除路径，`SLOTSCHECK` 校验 key 所在 dict index 与 Codis hash slot 一致。
- 同步迁移：源端 `SLOTSMGRTSLOT`/`SLOTSMGRTONE`/`SLOTSMGRTTAGSLOT`/`SLOTSMGRTTAGONE` 复用 `server.slotsmgrt_cached_sockets` 中按 `host:port` 缓存的裸 TCP 连接，用 `createDumpPayload` 生成 RDB fragment 并发送 `SLOTSRESTORE`；目标端恢复成功后源端才删除本地 key，并把删除传播为确定性 `DEL`。
- 异步迁移：`src/slots_async.c` 承载 `SLOTSMGRT*-ASYNC`、`SLOTSRESTORE-ASYNC*` ACK 子协议、per-DB cached migration client、`FENCE`/`CANCEL`/`STATUS` 与 `EXEC-WRAPPER` 写保护；`redisDb.codis_tagged_keys` 只记录带 `{...}` hash tag 的 key，score 使用完整 CRC32，供 tag-aware 迁移扩展同 tag key。
- 本机 RDB export：复用 Redis 端口，`networking.c` 在 parser 前识别精确 HTTP 请求，IO thread 只做标记并转回主线程；auth、文件校验与 streaming 都在主线程完成，`codis-rdb-export-rate-limit` 为 server 级 body bytes/sec 预算。

### 运维入口：codis-fe / codis-admin / codis-ha

- `codis-fe` 的 `StaticLoader` 读取 dashboard 列表文件，`DynamicLoader` 通过 coordinator 发现 dashboard。`ReverseProxy` 按 product 转发管理请求，包括 ACL、QPS Limit、RDB Analysis、PITR 和数据迁移。
- `codis-admin` 的常规管理命令通过 proxy 或 dashboard API 执行。直接访问元数据的 `--dashboard-list`、`--config-dump`、`--config-restore`（带 `--confirm`）、`--show-lock`、`--remove-lock`（带 `--expected-revision` 与 `--confirm`）要求至少给出一个共享参数（`--etcd`/`--etcd-config`）；`--config-convert` 是纯文件转换。
- `codis-ha` 的 `HealthyChecker.Maintains` 周期读取 dashboard stats，按 `maxdown` 阈值清理或 promote。

### 构建与发布层

- `go.mod` 声明 `go 1.26.1` 和 `toolchain go1.27.4`。Go 组件的构建与测试都使用 Go modules，`cgo_jemalloc` 依赖通过 `replace github.com/spinlock/jemalloc-go => ./third_party/jemalloc-go` 指向仓库内的模块。
- `Makefile` 产出 `codis-dashboard`、`codis-proxy`、`codis-admin`、`codis-ha`、`codis-fe`、`codis-shake` 和嵌入式 `codis-server`，并刷新 tracked 的 `config/dashboard.toml`、`config/proxy.toml`、`config/redis.conf`、`config/sentinel.conf`；默认 `make`/`make codis-server` 从 `extern/redis-8.6.3` 构建正式 Redis 8 Codis Server，复制为 `bin/codis-server`、`bin/redis-cli`、`bin/redis-benchmark`、`bin/redis-sentinel`。
- 显式平台矩阵：`make build-platforms` / `make release-platforms`，默认 `TARGET_PLATFORMS` 为 `darwin/arm64 linux/amd64 linux/arm64`，产物落在 `bin/<os>-<arch>/`；`build-platform-artifact` 先做 target allowlist、unsafe label 校验与 C/cgo cross toolchain guard，非 host full build 缺少 `ALLOW_CROSS_FULL_BUILD=1` 与对应 `PLATFORM_CC_<os>_<arch>` 时 fail-fast。`build-no-redis-platform` 只产出 `bin/<os>-<arch>-no-redis/` 下的 Go 入口、FE assets 和配置副本，不构建 Redis Server 与 helper，也不承诺纯 Go（非 jemalloc allocator 路径仍使用 cgo）。
- 版本元数据由 `pkg/utils/version.go` 提供 clean checkout 兜底，`version` 脚本生成 `bin/version` 与 `bin/version.ldflags`，Makefile 通过 `-ldflags -X` 注入；发布包装入口为 `Dockerfile`、`scripts/docker.sh`、`example/server.py`、`kubernetes/codis-server.yaml`。

## 数据与状态

### coordinator 命名空间与后端

| 路径 | 模型 | 写入方 / 语义 |
|---|---|---|
| `/codis3/{product}/topom` | `models.Topom` | dashboard 启动时用事务抢占的持久拓扑锁；不附 lease，值含 `owner_id`，按 `create_revision` 释放 |
| `/codis3/{product}/slots/slot-XXXX` | `models.SlotMapping` | dashboard；`Action` 记录迁移状态与 target group |
| `/codis3/{product}/group/group-XXXX` | `models.Group` / `GroupServer` | dashboard；master 为 group 内第一台 |
| `/codis3/{product}/proxy/proxy-{token}`、`/codis3/{product}/sentinel` | `models.Proxy` / `models.Sentinel` | dashboard 注册、状态维护与 sentinel 配置同步 |
| `/codis3/{product}/acl` | `models.ACL` | dashboard 提交的 ACL 目标 revision，只存 password hash |
| `/codis3/{product}/proxy_qps_limit` | `models.ProxyQPSLimit` | dashboard 提交的 QPS limit 目标 revision |
| `/codis3/{product}/online-proxies/{token}` | `models.OnlineRecord` | 开启 `etcd_register_online` 的 proxy 自身创建并续约的在线记录（`token`、`addr`），绑定独立 lease，TTL 固定 30s |

- 新线只有 `pkg/models/etcd` 一个后端，由各组件入口按 D2（组件内构造）创建客户端并注入，`pkg/models` 与 `pkg/topom` 不导入该包；旧的 `models.NewClient` 工厂与 `pkg/models/{fs,zk,consul}` 已删除，`models.Client` 收窄为 `Create/Update/Delete/Read/List/Close`，`Store` 在其上加 product 命名空间与缓存失效，路径 helper 集中在 `pkg/models/store.go`（`LockPath`、`SlotPath`、`GroupPath`、`ProxyPath`、`SentinelPath`、`ACLPath`、`ProxyQPSLimitPath`、`OnlineProxyPath`）。
- 锁与在线记录是追加在 `Store` 与 `etcd` 客户端上的可选接口：`Store.AcquireLock`/`ReleaseLock`/`ShowLock`/`RemoveLock` 用版本为 0 的比较事务抢锁、按 `create_revision` 释放或删除，应答未知时线性一致回读；`pkg/proxy/etcdonline` 用 concurrency lease 原语（`GrantLease`/`KeepAlive`/`RevokeLease`/`PutWithLeaseIfAbsent`）维护在线记录，`pkg/proxy/online.go` 只暴露 `OnlineRegistry`/`OnlineLister` 接口，`pkg/proxy` 不导入 etcd 包。
- ACL 元数据只保存 revision、enabled、user enabled、password hash、rules 和 updated_at，不保存客户端明文密码；QPS limit 模型只保存 revision、limit 和 updated_at，不保存每秒 token 状态。

### dashboard/topom 进程内状态

`Topom` 分 model、store、cache、action、stats、ha、aclSync、rdbAnalysis、dataMigration 几块：cache 保存 slots/group/proxy/sentinel/ACL/QPS limit 快照；action 保存迁移执行状态；stats 保存 Redis 与 proxy 统计；ha 保存 sentinel 观察到的 masters；aclSync 保存最近一次 ACL 下发的 per-proxy 失败结果与 `sync_status`；rdbAnalysis 保存 job registry、analysis 并发限制、remote fetch 计数、上传 workspace、临时文件路径和结果快照；dataMigration 保存数据迁移 job registry、并发上限、worker 进程句柄与状态轮询结果，任务目录与脱敏快照由 `data_migration_workspace` 承载。

### proxy 进程内状态

- `Proxy` 持有身份认证 token、`models.Proxy`、两个 listener、router、HA sentinel monitor、可选的在线注册器（`etcd_register_online` 且 token 已知后才构造）、默认关闭的 brute-force guard 和活动 session registry；`Router` 持有 1024 个 `Slot`、主从后端连接池、online/closed 状态、`ACLSnapshot`、按 ACL revision 绑定的 user-bound backend pools、默认关闭的 `HotKeyCache`、broadcast reporter 与 cluster nodes provider（`self` 模式保存本 proxy 快照，`all` 模式周期读取 `/codis3/{product}/online-proxies` 前缀并过滤坏 JSON、空地址、非 online、重复 token/addr 后生成 `0-16383` 伪节点，命令路径只读内存快照）。
- ACL snapshot 采用 copy-on-write：新 revision 安装时关闭旧 user pools，把旧 revision 的已认证 session 标记 stale，下一条非 `AUTH` 命令返回重新认证要求；session identity 保存 username、credential hash、短期 password、revision 和可选 ForcedDB，只用于 backend reconnect。
- `HotKeyCache` 以 `database + raw Redis key bytes` 为条目 key，保存 copied bulk bytes、slot id、expire time、LRU 元素与统计；读 miss 回填前校验 slot version 与 cache 全局失效 version，防止旧响应被重新写回。broadcast reporter 只保存 source proxy token、dashboard admin addr、xauth、HTTP timeout 与有界事件队列，队列内只有 DB+raw key bytes。
- `QPSLimiter` 保存 applied revision、limit、tokens、last_refill、accepted、rejected，调低 limit 时 clamp 当前 burst；brute-force guard 以规范化客户端 IP 为 key 保存 `failures`、`lastFailureAt`、`lockedUntil` 和聚合统计，状态表有固定 tracked IP 上限。
- 后端连接：service connection 按配置发送 `AUTH [username] password` 与按 DB `SELECT`，user-bound connection 使用 session ACL identity；stale keepalive 只在 `INFO` 的 `loading` 不为 1、`master_link_status` 不为 `down` 时恢复。

### 状态归属与收敛

| 状态 | 归属 | 传播 / 收敛方式 |
|---|---|---|
| slot 映射、group、proxy、sentinel | coordinator | dashboard 写，proxy 通过 admin API 接收下发 |
| ACL revision | coordinator | dashboard 先应用后端 Redis ACL，再保存目标 revision 并下发 online proxy |
| QPS limit 目标值 | coordinator | dashboard 保存目标值后下发 online proxy |
| 业务数据 | 后端 Redis | 不进入 coordinator |
| hot key cache 条目 | 单个 proxy | TTL 收敛，或经 dashboard 广播删除 |
| QPS token bucket、accepted/rejected、brute-force 计数 | 单个 proxy | 仅通过 proxy stats 上报 revision/limit；重启清空计数与锁定 |
| RDB analysis job、PITR job 与 per-server lock | dashboard | 进程内，重启丢失；PITR lock 持续到显式 Remove |
| 数据迁移 job | dashboard 内存和本地 workspace | `jobs/<id>/` 保存 `spec.json`（明文凭据，0600）和日志。dashboard 尽力保存脱敏的 `jobs.json`，重启时将已落盘的非终态任务标记为 `failed` |

## 代码地图

### cmd/

| 路径 | 符号 / 关键内容 |
|---|---|
| `cmd/proxy/main.go`、`cmd/dashboard/main.go` | `main`：proxy 解析参数与配置并按 `--dashboard`、`--etcd`、`--fillslots` 三种自动上线方式之一启动（全部省略则等待手动纳管）；dashboard 创建 etcd client、`topom.New` 并在启动时抢占持久拓扑锁（冲突退避、拒绝退出、未知回读） |
| `cmd/admin/main.go`、`cmd/admin/proxy.go`、`cmd/admin/dashboard.go`、`cmd/admin/pitr.go`、`cmd/admin/rdb_analysis_remote_fetch.go` | CLI 参数分发、proxy/dashboard 操作、ACL view 读取与提交（dry-run redaction）、`--pitr-*`、`--rdb-analysis-remote-fetch`、`--show-lock`/`--remove-lock --expected-revision` |
| `cmd/fe/main.go`、`cmd/fe/assets/{index.html,acl.js,qps-limit.js,rdb-analysis.js,pitr.js,data-migration.js}`、`cmd/ha/main.go`、`cmd/internal/coordinator/args.go` | `StaticLoader`/`DynamicLoader`/`ReverseProxy` 与 FE 各配置区域（动态来源首刷 3×2s、拒绝即退出）；`main`、`HealthyChecker.Maintains` 巡检；`Args`/`Flags`（`--etcd`/`--etcd-config`、旧参数按名称拒绝并提示旧线 SHA、地址优先级解析）与 `DeprecatedCoordinatorSHA` |
| `cmd/shake/main.go`、`cmd/shake/args.go` | `codis-shake` 二进制：`--spec=FILE`、`--version`；第一次 SIGINT/SIGTERM 取消 context 等待 writer 排空，第二次 SIGINT 强制退出 |

### pkg/proxy

| 路径 | 符号 / 关键内容 |
|---|---|
| `pkg/proxy/proxy.go`、`pkg/proxy/proxy_api.go` | `Proxy`、`New`、`setup`、`Start`、`serveProxy`、`serveAdmin`、`Stats`、`Overview`；`newApiServer`、`ApiClient`、`SetACL`、`SetQPSLimit`、`HotKeyCacheInvalidate`、`FillSlots` |
| `pkg/proxy/session.go`、`pkg/proxy/slots.go` | `Session`、`serve`、`handleRequest`、`handleRequestMGet`/`MSet`/`Del`/`Exists`、`handleRequestSlotsInfo`/`SlotsScan`/`SlotsMapping`；`Slot`、`blockAndWait`、`unblock`、`forward` |
| `pkg/proxy/session_auth.go`、`acl.go`、`acl_backend.go` | `Session.handleAuth`、`handleLegacyAuth`、`handleACLAuth`、`ACLSnapshot`、user-bound backend pool、`ACL DRYRUN` |
| `pkg/proxy/router.go`、`pkg/proxy/forward.go`、`pkg/proxy/backend.go` | `Router`、`dispatch`、`dispatchByHashKey`、`dispatchSlot`、`FillSlot`、`fillSlot`、`SetACL`；`forwardSync`、`forwardSemiAsync`、`forwardHelper.slotsmgrt`/`slotsmgrtExecWrapper`；`BackendConn`、`NewBackendConnWithAuth`、`KeepAlive` |
| `pkg/proxy/client_list.go`、`pkg/proxy/stats.go` | `clientSessionRegistry`、`handleRequestClientList`、`formatClientList`；`opStats`、`OpStats`、`OpTotal`、`OpRedisErrors`、`OpQPS` |
| `pkg/proxy/cluster_nodes.go`、`stream_commands.go`、`mapper.go` | `clusterNodesProvider`、`loadOnlineNodes`（读 `/codis3/{product}/online-proxies`）、`normalizeClusterNodes`；`streamCommandSpecs`、`registerStreamCommandOps`、`resolveStreamRoute`、`Router.handleRequestStream`；`OpFlag`、`OpInfo` 与命令 allow-list |
| `pkg/proxy/qps_limiter.go`、`hot_key_cache.go`、`hot_key_cache_broadcast.go`、`auth_bruteforce.go`、`online.go`、`etcdonline`、`metrics.go`、`redis/redistest` | `QPSLimiter`、`QPSLimitConfig`、`QPSLimitStats`；`HotKeyCache`、`hotKeyCacheInvalidationPlan`；`hotKeyCacheBroadcastReporter`、`HotKeyCacheBroadcastRequest`；`AuthBruteforceGuard`、`AuthBruteforceStats`；`OnlineRegistry`/`OnlineLister` 接口与 `SetModel` 接线；`etcdonline.New`、`Options`（固定 30s TTL、版本为 0 的条件写、KeepAlive channel 关闭后重确认所有权、按前缀一致读）；`startMetricsReporter`、`startMetricsJson`/`Influxdb`/`Statsd`；fake Redis server helper |

### pkg/topom

| 路径 | 符号 / 关键内容 |
|---|---|
| `pkg/topom/topom.go`、`topom_api.go`、`topom_cache.go`、`topom_stats.go` | `Topom`、`New`、`Start`、`runPeriodic`、`Stats`、`Overview`；`newApiServer`、`verifyXAuth`；`refillCache`、`storeUpdateSlotMapping`/`ACL`/`ProxyQPSLimit`；`RedisStats`、`RefreshRedisStats` |
| `pkg/topom/topom_action.go`、`topom_slots.go` | `ProcessSlotAction`、`processSlotAction`、`ProcessSyncAction`；`SlotCreateAction`、`SlotCreateActionSome`、`SlotCreateActionRange`、`SlotActionPrepareFilter`、`SlotActionComplete`、`newSlotActionExecutor`、`SlotsRebalance` |
| `pkg/topom/topom_group.go`、`topom_proxy.go`、`topom_sentinel.go` | group/server 增删与主从 promotion；`CreateProxy`、`OnlineProxy`、`ReinitProxy`、`resyncSlotMappings`；sentinel 配置同步与 master 切换 |
| `pkg/topom/topom_acl.go`、`topom_acl_api.go`、`topom_qps_limit.go`、`topom_hot_key_cache.go` | `ACLView`、`ACLUpdateRequest`、Redis `ACL SETUSER` 同步与 proxy snapshot fan-out；`ProxyQPSLimitView`、revision 写入、proxy fan-out 与 `sync_status`；hot key cache 失效 fan-out |
| `pkg/topom/topom_rdb_analysis.go`、`topom_rdb_analysis_api.go`、`topom_rdb_analysis_remote_fetch.go`、`topom_pitr.go`、`topom_pitr_run.go`、`topom_pitr_api.go`、`topom_pitr_disk.go` | `RDBAnalysisManager`、`RDBAnalysisJob`、`RDBAnalysisOptions`、`rdbAnalysisSnapshot`、`rdbAnalysisFlameBuilder`；`RDBAnalysisUpload`/`Start`/`RemoteFetch`/`Get`/`Cancel`/`Remove`、`startRDBAnalysisRemoteFetch`、`fetchRemoteRDB`；`PitrManager`、`PitrJob`、`PitrStep`、`pitrServerLock`、9 步状态机（validate/prereq/feasibility/shutdown/snapshot/truncate/restart/wait-load/resync）、`PitrCreate` 等 handler、bin preflight 与 free disk 辅助 |
| `pkg/topom/topom_data_migration.go`、`topom_data_migration_api.go`、`config.go` | `DataMigrationManager`、进程内 job registry、并发上限、worker 生命周期监管、状态轮询与日志读取、`jobs.json` 持久化；data-migration HTTP handler 与 `ApiClient` 方法；`data_migration_*` 配置项 |

### pkg/migration 与 pkg/shake

| 路径 | 符号 / 关键内容 |
|---|---|
| `pkg/migration/spec.go`、`topology.go`、`worker.go` | `Spec`、`Endpoint`、`Mode`、`Validate`、`ValidateSlotCoverage`、`SlotRouter`、`ShakeOptions()`；`StaticClusterReader`、`StaticClusterWriter`、`CodisSlot`；`Run`、`LoadSpec`、`watchParent`（`spec.json` → reader → filter → lua function → writer 流水线） |
| `pkg/shake` | RedisShake v4.8.0 `internal/` 的逐文件拷贝（import path 改写为 `github.com/CodisLabs/codis/pkg/shake/...`）；与上游的差异见 `pkg/shake/README.md` |

### pkg/models 与 pkg/utils

| 路径 | 符号 / 关键内容 |
|---|---|
| `pkg/models/client.go`、`store.go` | `Client` 接口（Create/Update/Delete/Read/List/Close）、`Store`、`NewStore`、路径 helper、`ValidateProduct`；`AcquireLock`/`ReleaseLock`/`ShowLock`/`RemoveLock` 与 `ErrLockHeld`/`ErrLockOwnershipChanged`/`ErrLockGone` |
| `pkg/models/slots.go`、`action.go` | `MaxSlotNum`、`ForwardSync`/`ForwardSemiAsync`、`Slot`、`SlotMapping`；`ActionPending`、`ActionPreparing`、`ActionPrepared`、`ActionMigrating`、`ActionFinished`、`ActionSyncing` |
| `pkg/models/group.go`、`proxy.go`、`sentinel.go`、`proxy_qps_limit.go`、`acl.go`、`topom.go`、`rejected.go` | `Group`、`GroupServer`、`Proxy`（不再有 `JodisPath`）、`Sentinel`、`ProxyQPSLimit`、`ACL`、`ACLUser`（含 `DB` 绑定字段）、`ACLPasswordHash`；`Topom.OwnerID`；`IsRejected` 判定 |
| `pkg/models/etcd` | 唯一 coordinator 实现：`Config`/`LoadConfigFile`/`New`（地址与 TLS 规则、连接预检）、`Store` 的 KV/前缀 List、`lease.go` 的 lease 原语、错误分类（rejected/unknown）与集成测试入口 |
| `pkg/models/modelstest`、`pkg/models/proxy_online.go` | 纯单测用的内存替身（含 `LockClient` 与故障注入）；在线记录路径与 `OnlineRecord` 模型 |
| `pkg/utils/redis/client.go` | `Client`、`Pool`、`InfoCache`、`NewClient`、`NewClientWithAuthIdentity`、`InfoFull`、`SetMaster`、`SlotsInfo`、`MigrateSlot`、`MigrateSlotAsync` |
| `pkg/utils/redis/reply.go`、`sentinel.go` | `replyString`、`replyInt`、`replyInts`、`replyStringMap` 等 reply shape 解析；`Sentinel`、`SentinelMaster`、`SentinelGroup`、`Subscribe` |
| `pkg/utils/version.go`、`version` | clean checkout 版本兜底与 ldflags 注入来源 |

### extern/redis-8.6.3（Redis 8 Codis Server）

| 路径 | 符号 / 关键内容 |
|---|---|
| `extern/redis-8.6.3/src/config.c`、`server.h`、`server.c`、`db.c`、`lazyfree.c` | `codis-enabled` 状态、1024 slot `kvstore` keyspace、Codis CRC32 slot 计算、flush/temp DB 重建 |
| `extern/redis-8.6.3/src/slots.c`、`src/commands/slotshashkey.json`、`slotsinfo.json`、`slotsscan.json`、`slotsdel.json`、`slotscheck.json`、`tests/unit/codis.tcl` | slot 查询/统计/扫描/删除/一致性检查、slot keyspace 与 tag index helper |
| `extern/redis-8.6.3/src/slots.c`、`server.h`、`server.c`、`src/commands/slotsmgrt*.json`、`slotsrestore.json`、`tests/unit/codis_migration.tcl`、`codis_slotsrestore.tcl` | 同步迁移、RDB fragment restore、`slotsmgrt_sockfd` socket 缓存 |
| `extern/redis-8.6.3/src/slots_async.c`、`networking.c`、`blocked.c`、`src/commands/*async*.json`、`slotsmgrt-exec-wrapper.json`、`tests/unit/codis_async_migration.tcl` | 异步迁移、restore async ACK、per-DB cached client、fence/cancel/status、exec wrapper 写保护 |
| `extern/redis-8.6.3/src/slots.c`、`slots_async.c`、`config.c`、`server.h` | `codis-migration-auth-user` / `codis-migration-auth-pass` named ACL 认证 |
| `extern/redis-8.6.3/src/codis_rdb_export.c`、`networking.c`、`iothread.c`、`server.h`、`server.c`、`config.c`、`tests/unit/codis_rdb_export.tcl` | RDB HTTP export：parser 前识别、IO-thread 主线程转交、文件校验、fd streaming、server 级 token bucket、`CONFIG SET` clamp |

### third_party 与构建发布

| 路径 | 符号 / 关键内容 |
|---|---|
| `third_party/jemalloc-go` | `github.com/spinlock/jemalloc-go` 的本地 replace 模块，提供 `cgo_jemalloc` 所需 wrapper、头文件与 C 源码 |
| `third_party/rdb` | `github.com/hdt3213/rdb` 的最小 fork，支持 Redis 8 RDB version 13 与 Stream type 26 |
| `Makefile`、`extern/redis-8.6.3/Makefile`、`extern/redis-8.6.3/src/Makefile` | `build-all`、`build-platforms`、`release-platforms`、`build-platform-artifact`、`build-no-redis-platform`；Redis 8 源码接入点（`codis_rdb_export.o` 在同一 Makefile 中登记） |
| `config/redis.conf`、`config/sentinel.conf` | Redis 8 Codis Server tracked 默认模板，`redis.conf` 显式包含 `codis-enabled yes` |
| `go.mod`、`go.sum` | module 依赖、`replace` 指令与校验锁定 |
| `Dockerfile`、`scripts/docker.sh`、`example/server.py`、`kubernetes/codis-server.yaml`、`scripts/redis8_local_validation.py`、`scripts/validation/` | 发布包装与本地示例入口；本地 Mac 非性能 validation gate。注意：Docker 入口与 `scripts/redis8_local_validation.py` 里的 filesystem coordinator 配置尚未适配 etcd，见[发布说明](etcd_3_7_release_notes_zh.md)；真机验证脚本（环境搭建、能力场景、Playwright） |

## 已知约束与边界

### 编译与构建

- 仓库已有完整 Go modules 编译闭环：默认 cmd/pkg 可编译测试，`cgo_jemalloc` proxy 经本地 replace 模块在 module mode 下构建，`make gotest`、`make build-all` 不再依赖 GOPATH/vendor 参数；旧 `vendor/`、`Godeps/` 已退休。
- 默认 `make`/`make build-all` 是 host build，显式多平台发布走 `make build-platforms`/`make release-platforms`，首版 allowlist 为 `darwin|linux` × `amd64|arm64`。完整非 host 产物需要原生 target runner 或显式 C/cgo cross toolchain，仅设 `GOOS`/`GOARCH` 不能保证 Redis 8 与 proxy cgo 产物正确。
- `go.mod` 使用 `go 1.26.1`。Go Redis helper 的第三方 client 固定 `github.com/redis/go-redis/v9`，必须保持 RESP2、禁用 identity、禁用 command retry，并用 dedicated connection 维持 Codis 的单连接 DB 和 pipeline 计数语义；测试用 fake Redis 需对 `HELLO` 返回 Redis error 以触发 RESP2 fallback。
- `Makefile` 的组件构建会刷新 `config/dashboard.toml`、`config/proxy.toml`、`config/redis.conf`、`config/sentinel.conf`。

### proxy 命令面与协议

- proxy 通过普通 Redis 协议面向客户端，但不支持全部 Redis 命令（见 [unsupported_cmds.md](unsupported_cmds.md)）。`CLIENT` 命令族只支持 `CLIENT LIST`，语义限定为当前 proxy 实例的活动客户端连接快照：不聚合多 proxy、不下探后端 Redis、不承诺 Redis 8.6.3 全字段 parity，其余 `CLIENT` 子命令仍不支持。
- `CLUSTER` 命令族默认不可用。只有 `cluster_nodes_compat = "self"` 或 `"all"` 时 `CLUSTER NODES` 才返回伪节点清单；`0-16383` 是 Redis Cluster 逻辑 slot 输出，不代表 Codis 真实路由（真实 `MaxSlotNum` 仍为 1024）。不实现其他 `CLUSTER` 子命令、`MOVED`/`ASK`、cluster bus、gossip、failover 和 cluster 配置文件；`all` 模式只轮询 `/codis3/{product}/online-proxies` 的在线注册信息（需要 `etcd_register_online = true`），不做 proxy 间 RPC、探活或广播；该记录不是 Jodis 协议，也不是 etcd 的 service registry 或健康检查。
- Redis Stream support 是 Redis 8 backend 的 proxy 路由子集，不是完整 Stream 支持声明。`XGROUP HELP`、`XINFO HELP`、未知 Stream subcommand、带 `BLOCK` 的 `XREAD`/`XREADGROUP` 和跨 hash tag multi-stream 会被 proxy 直接拒绝；后端必须是 Redis 8 Codis Server，proxy 不伪造成功响应。该能力不新增 Vector Set、Hash field expiration、模块数据类型、配置开关、dashboard/FE 页面、coordinator schema 或 proxy admin API。
- Hot key cache 是 opt-in bounded-stale 能力：默认关闭，只缓存 allowlist 中 exact key 的 `GET`/`MGET` string bulk value，写命令不做写穿填充。广播链路 best-effort，失败、队列满、proxy 离线或 dashboard 不可达都不改变客户端写响应，未收到通知的 proxy 靠 TTL 收敛；不做自动热点探测、dashboard 管理页、coordinator 元数据、proxy-to-proxy 通信，也不缓存派生读或非 string value；slot 迁移或锁定状态不从 cache 返回。
- AUTH 防暴力破解默认关闭，覆盖 proxy 客户端的单密码和 Codis ACL 认证。计数和锁定按单 proxy 看到的来源 IP 维护。NAT、四层代理或负载均衡后的多个客户端可能共享计数。已有认证身份仍可使用，ACL 模式下 IP 锁定期间会拒绝重新 `AUTH`。详见 [AUTH 防暴力破解](proxy_session_auth_bruteforce_zh.md)。

### 数据面与迁移

- slot 数固定 1024，改变该常量会同时影响模型、路由、迁移和外部元数据兼容性。
- `product_name` 同时参与元数据命名空间、proxy/dashboard 鉴权和运行期路由隔离，格式校验见 `pkg/models/store.go` 的 `ValidateProduct`。
- Redis 8 同步迁移的安全边界是「目标端确认写入成功后才删除源端 key」：connect/write/read、AUTH、SELECT 或 `SLOTSRESTORE` 任一失败都会关闭对应 socket 缓存并返回错误，不删除源端数据；成功路径只传播 `DEL`，不传播原始 `SLOTSMGRT*` 命令。
- 异步迁移同样遵守「所有 ACK 成功后才删除源端 key」：connect/write/read、AUTH、SELECT、restore async ACK、timeout、cancel 或 client close 任一失败都保留源端数据；同一 DB 同时只允许一个 async migration，`SLOTSMGRT-ASYNC-STATUS`/`FENCE`/`CANCEL` 按当前 DB 隔离。
- 迁移只保证 Redis 8 ↔ Redis 8；不承诺跨大版本 fragment 或持久化 RDB/AOF 兼容。
- 数据迁移控制台（`pkg/migration` + `pkg/shake` + `codis-shake`）不在 proxy 请求路径中，也不经过 coordinator：Codis 源端/目标端都直连 codis-server，因为 proxy 不允许 `RESTORE`、`SCAN`、`PSYNC`，且 proxy 默认不提供 `CLUSTER NODES`（只有启用 `cluster_nodes_compat` 时才返回伪节点清单）。
- Codis 端拓扑是创建任务时的静态快照（Group Master + 1024 slot 归属），之后不会被跟随；迁移期间不要做 slot 迁移/重平衡或 failover，拓扑变化会导致迁移到错误分片或任务失败。
- 数据迁移任务不支持断点续传：dashboard 重启后运行中的任务被标记 `failed`，需要重新创建；任务记录只保存在 dashboard 本地 workspace，不写 coordinator。
- 默认 `make`/`make codis-server` 构建内嵌 Redis 8 Codis Server，`config/redis.conf` 显式 `codis-enabled yes`，但仍不启用 Redis Cluster 协议；`make codis-server-redis8` 只是默认 Redis 8 产物的 suffixed alias。
- 本地 Mac validation gate 不产出性能、fork/RDB、复制或网络栈的正式结论；所有 throughput/latency、RDB/fork、replication、Docker 包装和生产 cutover 判断以 Linux 验证为准（见 [redis8_validation_zh.md](redis8_validation_zh.md)）。

### 配置管理与运维语义

- Codis-managed Redis ACL 默认关闭，旧 `session_auth` 与 password-only backend auth 保持兼容。启用后 ACL source of truth 是 dashboard 提交到 coordinator 的 `/codis3/{product}/acl` revision，并同步到 Redis Server 与 proxy snapshot；普通客户端不能通过 proxy 执行 ACL 管理命令，FE 只能调用 dashboard ACL API。当前只管理 Redis 8 OSS ACL 用户、命令和 key pattern，不实现 RESP3/HELLO parity、Redis Cluster ACL、Pub/Sub channel ACL、Redis Stack/module key-spec 语义或跨 dashboard 分布式原子 rollout；fan-out 部分失败时在 `sync_status` 暴露 partial failure 并允许幂等重试。ACL 用户的 db 绑定是 proxy 路由属性，不渲染进 Redis `ACL SETUSER`、不放大权限；topom 只校验 db 非负，上限由 proxy 在认证时按 `backend_number_databases` fail-closed 兜底，且仅在 `codis_acl_enabled` 下生效。
- Proxy QPS limit 默认关闭，0 表示不限制，正数表示单个 proxy 进程内所有 session 共享的普通请求 QPS 上限。它不是跨 proxy 分布式限流，不实现 per-client、per-IP、per-user、per-command、per-DB、per-slot、per-key 或 per-backend group 限流，也不把每秒 token 状态写入 coordinator。业务客户端的 `CONFIG` 命令族仍不开放；被拒绝的请求返回 Redis error 文本，但不代表后端 Redis 错误。
- PITR / 数据闪回默认关闭，只做单 Redis 8 Codis Server 级 AOF 时间点截断恢复：不做跨 group 全局一致性恢复、不做指定 key 子集恢复、不修改 Redis C 源码、不自动开启 AOF 或 `aof-timestamp-enabled`、不做 AOF 保留策略/轮转/备份管理、不实现跨机 AOF 文件操作（scp/rsync/NFS）、不编排 proxy 只读切换、不在业务 Redis 协议路径暴露 PITR 命令、不把 job 写入 coordinator。它依赖运维预先开启 AOF、文件系统直访目标 server AOF 目录，并由外部进程管理器拉起 redis（dashboard 绝不 fork redis-server）。PITR 不幂等：per-server lock 持续到 operator 显式 Remove；SHUTDOWN 后用新连接探活确认 server 真停才继续；truncate 前快照 manifest 与最后 segment 使 `ftruncate` 可逆，并用双向 stat-diff 检测模型漂移；truncate 失败不自动重启。
- RDB Analysis 只分析「已经存在的 RDB 文件」：上传先落到 dashboard 的 `rdb_analysis_workspace/uploads` 临时文件，本地路径模式只能读取 `rdb_analysis_workspace` 下文件，remote fetch 模式只能从当前 product model 里的 Redis 8 Codis Server 拉取固定 `GET /codis/rdb/latest`。它不自动执行 `BGSAVE`/`SAVE`、不读取 Redis Server 机器任意路径、不实现 replication stream 抓取，也不支持 tus、Range 或断点续传；结果可能包含 key 名，所有 API 都要求 `xauth`，任务与结果只保存在 dashboard 进程内存。
- Redis 8 RDB HTTP export 只导出当前 `server.rdb_filename` 对应的已存在本机 RDB 文件，默认关闭，依赖独立 `X-Codis-RDB-Auth` header secret 与 Redis 端口网络隔离；不复用 Redis `AUTH`/ACL，不提供 Range、压缩、加密、独立 HTTP 端口、目录扫描或客户端指定路径。`codis-rdb-export-enabled` 与 `codis-rdb-export-auth` 是 immutable config，开启需重启；`codis-rdb-export-rate-limit` 默认 0 不限速，可运行期调整为所有 export 连接共享的 server 级 body bytes/sec，但不承诺 per-client fairness、并发连接上限或硬事件循环优先级。生产使用仍需配合网络隔离、TLS 或外层访问控制。
- 对同一个业务集群，同一时刻最多一个 dashboard，且所有集群修改都经由 dashboard 完成。
- 拓扑锁 `/codis3/{product}/topom` 是持久锁（不附 lease），dashboard 异常退出后锁会保留：先隔离原进程与自动拉起机制，再用 `codis-admin --show-lock` 读取 `create_revision`，最后按该 revision 执行 `--remove-lock --expected-revision=N --confirm`。缺失、不匹配、结果未知或请求被拒绝都以非零退出；回读发现替代锁时一律保留。
- 注册型 proxy 的发现是 lease 语义：在线键随 lease 过期消失，`CLUSTER NODES` 的 `all` 模式最多保留最后一次成功快照到下一个刷新周期，因此 peer 陈旧窗口不超过 TTL（30s）加一个刷新周期；etcd 失去 quorum 时旧快照会继续被返回，不能当作"节点仍然健康"的证据。

## etcd 3.7 后端边界

`pkg/models/etcd` 是新线唯一的 coordinator 实现，基于 `go.etcd.io/etcd/client/v3 v3.7.4`，只使用 Codis 实际调用的操作：

- KV：`Create` 用 `Txn` 比较 `Version(key)==0` 后写入，`Update` 为单键 upsert，`Delete` 精确删键（键已缺失视为达成目标）；`List` 用带尾斜杠的前缀查询，返回去重、排序的直接子路径，支持 FE 在 `/codis3` 枚举 product；超时、拒绝访问与失去 quorum 都返回错误，不会被翻译成空集群。
- 持久锁：比较锁键版本为 0 的 `Txn` 抢占（值含 `owner_id`），按本次 `create_revision` 释放或删除；应答未知时用新 context 做线性一致回读，读不到 quorum 时绝不解释成锁空闲。
- 在线记录：lease `Grant`/`KeepAlive`/`Revoke`，`PutWithLeaseIfAbsent` 绑定 lease，`RangeWithPrefix` 一致读在线列表；首次 lease 续约等待为 `dial_timeout + 1s`。
- 连接：`Config` 负责地址优先级、TLS 文件、密码脱敏与错误分类（`Rejected`/`ResultUnknown`）；组件入口（`cmd/internal/coordinator`）负责解析 CLI 并构造客户端，`pkg/models`、`pkg/topom`、`pkg/proxy` 不导入该包。

边界：etcd 只保存控制面，不保存业务键；不提供 ZooKeeper/Consul/filesystem 到 etcd 的存量元数据迁移，也不保留双写或回退开关；不支持把 Codis 当作 etcd 的 service registry，不读取 etcd 的鉴权用户管理接口。部署支持范围是 etcd 3.7.x，版本核对放在部署预检与验收（A6），业务进程不额外取得集群管理权限。

## 第三方依赖 fork：third_party/rdb

`third_party/rdb` 是 `github.com/hdt3213/rdb` 的最小 fork，通过根 `go.mod` 的 `replace github.com/hdt3213/rdb => ./third_party/rdb` 接入。存在理由是上游 v1.3.2 解析不了 Redis 8.6.3 生成的 `REDIS0013`（报 `cannot parse version: 13`），而 dashboard 的 RDB Analysis 必须能读 Redis 8 Codis Server 产出的 RDB。fork 的差异面保持在 parser 最小集：

- 接受 RDB version 13；
- 支持 `RDB_TYPE_STREAM_LISTPACKS_4`（26）及 Redis 8.6 IDMP 元数据；
- 跳过 Redis 8 新增的 `RDB_OPCODE_KEY_META`（243）与 `RDB_OPCODE_SLOT_INFO`（244）；
- 为 stream 补齐 `GetElemCount()`。

升级上游版本时必须重新确认以上四点，否则 Redis 8 RDB 会在 dashboard 侧解析失败。

## 相关文档

- [tutorial_zh.md](tutorial_zh.md)：组件说明、etcd 连接配置与启动、两 group 建群、锁与人工恢复、Proxy 在线注册与部署。
- [etcd_3_7_release_notes_zh.md](etcd_3_7_release_notes_zh.md)：新线首个版本的发布说明（版本分界与旧线 SHA、不兼容变更、升级须知、未适配项）。
- [redis8_codis_server_zh.md](redis8_codis_server_zh.md)：Redis 8 Codis Server 的 Codis mode、slot keyspace 与迁移实现。
- [codis_acl_zh.md](codis_acl_zh.md)：Codis-managed Redis ACL、多账号认证与 db 绑定。
- [pitr_flashback_zh.md](pitr_flashback_zh.md)：PITR / 数据闪回的状态机与安全边界。
- [data_migration_zh.md](data_migration_zh.md)：数据集成控制台 / 数据迁移的进程模型、API、状态机与边界。
- [proxy_protocol_extensions_zh.md](proxy_protocol_extensions_zh.md)：proxy 的本地命令与协议扩展。
- [proxy_hot_key_cache_zh.md](proxy_hot_key_cache_zh.md)：热 key 缓存配置、一致性模型与 stats。
- [proxy_qps_rate_limit_zh.md](proxy_qps_rate_limit_zh.md)：Proxy QPS limit 配置、动态管理与拒绝语义。
- [proxy_session_auth_bruteforce_zh.md](proxy_session_auth_bruteforce_zh.md)：单密码与 Codis ACL 模式的 AUTH 防暴力破解配置与锁定语义。
- [rdb_analysis_zh.md](rdb_analysis_zh.md)：RDB Analysis 使用方式与边界。
- [build_and_release_zh.md](build_and_release_zh.md)：构建、平台矩阵与发布产物。
- [redis8_validation_zh.md](redis8_validation_zh.md)：Redis 8 本地与 Linux 验证矩阵。
- [capability_boundaries_zh.md](capability_boundaries_zh.md)：各项能力的统一边界清单。
