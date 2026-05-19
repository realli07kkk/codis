---
doc_type: architecture
slug: system-overview
scope: Codis 仓库当前整体架构
summary: Codis 通过 proxy 层隐藏 Redis 分片细节，由 dashboard/topom 维护拓扑、迁移和高可用状态，并用 models 抽象的 coordinator 存储集群元数据。
status: current
last_reviewed: 2026-05-19
tags: [redis, proxy, topology, consul]
depends_on: []
implements: [redis-cluster-service]
---

# Codis 架构总入口

## 0. 术语

- **Codis Proxy**：客户端连接的 Redis 协议代理，负责认证、命令解析、slot 路由、pipeline 响应写回和运行时指标；入口在 `cmd/proxy/main.go:31`，核心类型是 `pkg/proxy/proxy.go:29`。
- **Topom / Codis Dashboard**：集群拓扑管理进程。`cmd/dashboard` 创建 `topom.Topom` 并启动管理 HTTP API；`Topom` 内部持有 store、cache、slot action、stats 和 HA 状态，见 `pkg/topom/topom.go:28`。
- **Coordinator / Store**：保存集群元数据的外部存储抽象。`models.Client` 定义读写、列表、watch、临时节点接口，支持 zookeeper、etcd、filesystem 和 consul 实现；Consul 后端用 KV 保存 `/codis3` / `/jodis` 数据，用 Session 表达 ephemeral ownership，见 `pkg/models/client.go:15`、`pkg/models/client.go:31` 和 `pkg/models/consul/consulclient.go:27`。
- **Slot**：Codis 的路由分片单位，固定为 1024 个；模型常量在 `pkg/models/slots.go:13`，proxy 内部 slot 状态在 `pkg/proxy/slots.go:12`。
- **Group**：一组后端 Redis Server，第一台按现有代码语义承担 master 位置，类型定义在 `pkg/models/group.go:8`。
- **RDB Analysis**：dashboard/topom 进程内的离线 RDB 文件分析能力。它通过 `github.com/hdt3213/rdb` 解析已有 RDB 文件，输出 DB/type/prefix 聚合、big keys、hot keys 和 flamegraph 树形数据；它不在 proxy 业务请求路径中执行，也不是 Redis 在线命令或远端 RDB 抓取通道。核心类型在 `pkg/topom/topom_rdb_analysis.go:36` 到 `pkg/topom/topom_rdb_analysis.go:95`。
- **Go module manifest**：仓库根目录的 `go.mod` / `go.sum`，用于现代 Go module mode 下解析 cmd/pkg 默认构建标签依赖；当前使用 `go 1.26.1`，默认 cmd/pkg、`cgo_jemalloc` proxy 构建和 Makefile 测试入口都已接通。

## 1. 定位与受众

这份文档描述当前仓库已经落地的系统地图，服务于 feature-design、issue-analyze 和新人上手。读完应能判断一次改动会碰到哪个入口、哪个核心包、哪些运行期状态，以及哪些变更必须经过 dashboard/topom。

## 2. 结构与交互

```mermaid
flowchart LR
  Client[Redis Client] --> Proxy[Codis Proxy]
  Proxy --> Server[Codis Server / Redis]
  Dashboard[Codis Dashboard / Topom] --> Proxy
  Dashboard --> Server
  Dashboard --> Store[Coordinator Store]
  FE[Codis FE] --> Dashboard
  Admin[Codis Admin] --> Dashboard
  HA[Codis HA] --> Dashboard
  Proxy --> Jodis[Jodis Registry]
```

Coordinator 后端通过 `pkg/models/client.go` 的工厂集中挂入，调用方仍只依赖 `models.Client` / `models.Store`。Consul 后端位于 `pkg/models/consul`，只接入 HashiCorp Go API 模块 `github.com/hashicorp/consul/api`，把 Codis slash path 转成无前导 `/` 的 Consul KV key；`Create` 使用 `KV.CAS(ModifyIndex=0)`，`List` 使用 `KV.Keys(prefix, "/", ...)`，`WatchInOrder` 使用 blocking query 的 `WaitIndex`，Jodis ephemeral 节点使用 `Session.CreateNoChecks` + `KV.Acquire` + `Session.RenewPeriodic`。dashboard、proxy、admin 和 fe 仍通过原有配置/CLI 参数选择 coordinator，新增 `consul` 分支不改变默认 coordinator。

构建层面，仓库已有 `go.mod` / `go.sum`，`go.mod` 使用 `go 1.26.1`，旧 `vendor/` / `Godeps/` 依赖目录已经退休。常规 Go 依赖由 `go.mod/go.sum` 解析，`GO111MODULE=on go test ./cmd/... ./pkg/...`、`GO111MODULE=on go build ./cmd/... ./pkg/...` 和 `GO111MODULE=on go build -tags cgo_jemalloc ./cmd/proxy` 都可在 module mode 下通过。`cgo_jemalloc` 路径通过 `go.mod` 的 `replace github.com/spinlock/jemalloc-go => ./third_party/jemalloc-go` 指向仓库内受控的本地模块，不再依赖旧 `vendor/github.com/spinlock/jemalloc-go` 的预处理状态。`Makefile` 已切换到 module mode（移除了 `GO15VENDOREXPERIMENT` 和旧 `vendor/github.com/spinlock/jemalloc-go` 预处理调用），产出 `codis-dashboard`、`codis-proxy`、`codis-admin`、`codis-ha`、`codis-fe` 和嵌入式 `codis-server`；Go 二进制构建规则在 `Makefile:12` 到 `Makefile:28`。默认 `make` / `make build-all` / `make codis-server` 现在从 `extern/redis-8.6.3/` 构建正式 Redis 8 Codis Server，复制为默认 `bin/codis-server`、`bin/redis-cli`、`bin/redis-benchmark` 和 `bin/redis-sentinel`，并刷新 tracked 的 `config/redis.conf` / `config/sentinel.conf`；`config/redis.conf` 来自 Redis 8 模板并显式写入 `codis-enabled yes`，仍不启用 Redis Cluster。Redis 3 通过显式 `make codis-server-redis3` fallback 目标保留，产物使用 `*-redis3` 后缀，不覆盖默认 Redis 8 发布物；`make codis-server-redis8` 只作为兼容 alias，从默认 Redis 8 产物复制 suffixed 调试二进制和 `config/redis8.conf` / `config/sentinel8.conf`。当前 Redis 8 Codis Server 已具备 `codis-enabled yes` 运行模式：Redis 8 server 可在不启用 Redis Cluster 协议的前提下按 Codis 1024 slot 组织 keyspace，通过 `SLOTSHASHKEY`、`SLOTSINFO`、`SLOTSSCAN`、`SLOTSDEL`、`SLOTSCHECK` 暴露当前 DB 的 slot 查询、统计、扫描、删除和一致性检查能力，通过 `SLOTSMGRTSLOT`、`SLOTSMGRTONE`、`SLOTSMGRTTAGSLOT`、`SLOTSMGRTTAGONE` 与 `SLOTSRESTORE` 支撑 Redis 8 ↔ Redis 8 同步迁移，通过 `SLOTSMGRT*-ASYNC`、`SLOTSRESTORE-ASYNC*`、`SLOTSMGRT-ASYNC-FENCE/CANCEL/STATUS` 和 `SLOTSMGRT-EXEC-WRAPPER` 支撑 Redis 8 ↔ Redis 8 异步迁移、ACK 推进、迁移屏障和迁移中写保护，并维护 `redisDb.codis_tagged_keys` 作为 tag-aware migration 的辅助索引。Go proxy/topom/admin 对 Redis 8 Codis Server 的兼容面已通过测试矩阵和真实 Redis 8 smoke 验证：Go 侧继续使用 `AUTH <password>` default-user 模型、`SELECT` 当前 DB、`SLAVEOF` alias、`CONFIG GET/REWRITE`、`CLIENT KILL TYPE normal`、`SLOTSINFO`、同步/异步迁移返回 `[migrated_count, remaining_count]` 和 `SLOTSMGRT-EXEC-WRAPPER [code, reply]`；未新增生产 adapter、ACL username 配置或 proxy allow-list 条目。发布包装入口围绕默认 `codis-server`：Docker 镜像基础环境使用 module-capable Go 版本，`scripts/docker.sh server` 显式加载 tracked `config/redis.conf` 并在容器场景下传 `--protected-mode no --bind 0.0.0.0`，`example/server.py` 生成临时配置时写入 `codis-enabled yes`，`kubernetes/codis-server.yaml` 继续通过仓库默认配置启动。Redis 8 本地非性能 validation gate 由 `scripts/redis8_local_validation.py` 承载，只作为验证入口，不是生产组件：它使用临时 filesystem coordinator、临时 dashboard/proxy/Redis 进程，覆盖 `semi-async` 与 `sync` slot migration、proxy 写读和 Redis 3/Redis 8 fragment 方向性观察，证据落在 `.codestable/features/2026-05-17-redis8-validation-cutover/redis8-local-validation-evidence.json`。

版本元数据由 `pkg/utils/version.go` 提供 clean checkout 默认值，`version` 脚本生成 `bin/version` 和 `bin/version.ldflags`，Makefile 通过 `-ldflags -X` 注入真实 git/date 信息。该文件不再由构建脚本覆盖，避免一次 `make` 后源码进入脏状态。

运行入口按组件拆在 `cmd/`。`cmd/proxy/main.go:31` 解析 proxy 参数和配置，创建 `proxy.New(config)` 后按 dashboard、coordinator 或 fillslots 三种方式上线，见 `cmd/proxy/main.go:187` 和 `cmd/proxy/main.go:219`。`cmd/dashboard/main.go:25` 解析 dashboard 参数，创建 coordinator client 和 `topom.New`，启动时通过 store 抢占拓扑锁，见 `cmd/dashboard/main.go:118`、`cmd/dashboard/main.go:136` 和 `pkg/topom/topom.go:179`。

proxy 内部先建立两个监听器：对客户端的 Redis 协议监听和对管理面的 HTTP API 监听，分别由 `Proxy.setup` 填入 model，见 `pkg/proxy/proxy.go:105` 到 `pkg/proxy/proxy.go:143`。`Proxy.New` 创建 router、启动 admin/proxy 服务并启动 metrics reporter，见 `pkg/proxy/proxy.go:58` 到 `pkg/proxy/proxy.go:102`。

客户端连接在 `serveProxy` 中被 accept 后创建 `Session`，每个 session 拆出 reader 和 writer 两条 goroutine；reader 解码 Redis multi bulk、生成 `Request` 并交给 router，writer 等待后端响应后按原顺序写回，见 `pkg/proxy/proxy.go:396`、`pkg/proxy/session.go:114`、`pkg/proxy/session.go:152` 和 `pkg/proxy/session.go:199`。已通过上线检查的活动 session 会注册进 proxy 进程内的 session registry，用于当前 proxy 实例的连接观测；该 registry 不进入 coordinator，也不跨 proxy 同步，见 `pkg/proxy/client_list.go:18` 和 `pkg/proxy/session.go:122`。

命令路由的第一层在 `Session.handleRequest`。它先处理 `QUIT`、`AUTH`、`SELECT`、`CLIENT LIST` 等本地命令，再对 `MGET`、`MSET`、`DEL`、`EXISTS`、`SLOTSINFO`、`SLOTSSCAN`、`SLOTSMAPPING` 做特殊处理，其余命令走 `Router.dispatch`，见 `pkg/proxy/session.go:257` 到 `pkg/proxy/session.go:308`。`CLIENT LIST` 在 proxy 本地生成当前 proxy 实例的活动客户端连接快照，返回 RESP bulk string 的 Redis `key=value` 行，不转发到后端 Redis，见 `pkg/proxy/client_list.go:155` 到 `pkg/proxy/client_list.go:239`。`Router.dispatch` 用 hash key 计算 slot id 后调用 slot 的 forward method，见 `pkg/proxy/router.go:139` 到 `pkg/proxy/router.go:143`。

slot 路由状态由 dashboard 下发到 proxy。`Topom.reinitProxy` 会向 proxy 填充 slot、启动 proxy 并设置 sentinel；局部 slot 变化通过 `resyncSlotMappings` 并发调用所有 proxy 的 `FillSlots`，见 `pkg/topom/topom_proxy.go:126` 到 `pkg/topom/topom_proxy.go:170`。proxy 侧 `FillSlot` 根据 `models.Slot` 更新 backend、migrate、replicaGroups 和 forward method，见 `pkg/proxy/router.go:102` 到 `pkg/proxy/router.go:120` 以及 `pkg/proxy/router.go:168` 到 `pkg/proxy/router.go:213`。

迁移由 dashboard/topom 组织状态机，proxy 在请求转发时配合迁移。dashboard 创建 slot action 后把状态从 pending 推进到 prepared/migrating/finished，见 `pkg/topom/topom_slots.go:19`、`pkg/topom/topom_slots.go:188` 和 `pkg/topom/topom_slots.go:307`。真正搬迁 key 时，topom 根据 migration method 调用 Redis 侧迁移命令，见 `pkg/topom/topom_slots.go:358` 到 `pkg/topom/topom_slots.go:446`；proxy 在转发中遇到 migrating slot 会先执行同步或半异步迁移包装逻辑，见 `pkg/proxy/forward.go:35` 到 `pkg/proxy/forward.go:62` 和 `pkg/proxy/forward.go:72` 到 `pkg/proxy/forward.go:132`。Redis 8 Codis Server 的同步迁移路径由源端 `SLOTSMGRT*` 命令发起：源端复用 `server.slotsmgrt_cached_sockets` 中按 `host:port` 缓存的裸 TCP 连接，必要时先发 `AUTH` / `SELECT`，再用 `createDumpPayload` 生成 Redis 8 RDB fragment 并发送 `SLOTSRESTORE key ttlms payload [...]` 到目标；目标端反序列化成功后源端才 `dbSyncDelete` 删除本地 key，并把删除传播为确定性的 `DEL`。

dashboard 的管理 API 在 `pkg/topom/topom_api.go:31` 注册。它暴露 proxy、group、slot action、rebalance、sentinel 等操作路由，见 `pkg/topom/topom_api.go:72` 到 `pkg/topom/topom_api.go:126`。RDB Analysis API 也挂在 dashboard `/api/topom/rdb-analysis` 下，提供上传文件启动、workspace 路径启动、读取 job snapshot、取消和删除任务，所有路由都要求 `xauth`，见 `pkg/topom/topom_api.go:81` 到 `pkg/topom/topom_api.go:85` 和 `pkg/topom/topom_rdb_analysis_api.go:32` 到 `pkg/topom/topom_rdb_analysis_api.go:113`。proxy 自己的管理 API 在 `pkg/proxy/proxy_api.go:29` 注册，提供 model、stats、slots、start、fillslots、sentinels、forcegc、shutdown 等操作，见 `pkg/proxy/proxy_api.go:63` 到 `pkg/proxy/proxy_api.go:83`。

运维入口分三类：`codis-admin` 是命令行入口，按参数分发到 proxy、dashboard 或底层配置操作，见 `cmd/admin/main.go:12` 到 `cmd/admin/main.go:85`；`codis-fe` 既提供静态前端资源，也根据静态列表或 coordinator 动态发现 dashboard 并做 reverse proxy，见 `cmd/fe/main.go:127` 到 `cmd/fe/main.go:194` 和 `cmd/fe/main.go:259` 到 `cmd/fe/main.go:330`；`codis-fe` 的 AngularJS 页面还提供 RDB Analysis 区域，复用 product 选择、`xauth` 和 reverse proxy 访问 dashboard，前端逻辑在 `cmd/fe/assets/rdb-analysis.js:3`，页面入口在 `cmd/fe/assets/index.html:104`；`codis-ha` 周期性读取 dashboard stats，发现异常 proxy/server 后通过 dashboard API 做清理或 promote，见 `cmd/ha/main.go:90` 到 `cmd/ha/main.go:99` 和 `cmd/ha/main.go:248` 到 `cmd/ha/main.go:369`。

## 3. 数据与状态

集群元数据统一放在 `/codis3/{product}` 命名空间下：topom 锁、slot、group、proxy、sentinel 路径由 `pkg/models/store.go:27` 到 `pkg/models/store.go:59` 定义。`Store` 封装对这些路径的读写，包含 slot mappings、group、proxy、sentinel 的 load/list/update/delete，见 `pkg/models/store.go:73` 到 `pkg/models/store.go:256`。

当 coordinator 选择 Consul 时，上述 slash path 在 client 内部映射为 Consul KV key，例如 `/codis3/{product}/topom` 对应 `codis3/{product}/topom`。普通元数据使用 KV CAS/Put/Get/Delete 维护；Jodis `/jodis/{product}/proxy-{token}` 使用 Consul Session 持有 KV ownership，`Delete` / `Close` 会销毁 session 并只删除仍归属当前 session 的 key。Consul token 通过 `coordinator_auth` / `jodis_auth` 写入 API client config，不进入结构化日志。

dashboard/topom 的内存状态分为 model、store、cache、action、stats、ha 和 rdbAnalysis 几块：cache 保存 slots/group/proxy/sentinel 快照，action 保存迁移执行状态，stats 保存 Redis 和 proxy 统计，ha 保存 sentinel 观察到的 masters，rdbAnalysis 保存当前 dashboard 进程内的 RDB analysis job registry、并发限制、上传 workspace 和结果快照，见 `pkg/topom/topom.go:28` 到 `pkg/topom/topom.go:78` 和 `pkg/topom/topom_rdb_analysis.go:146` 到 `pkg/topom/topom_rdb_analysis.go:187`。RDB analysis job 不写入 coordinator，dashboard 重启后任务消失；job snapshot 只返回 source 摘要、进度和聚合结果，workspace 输入通过 `rdb_analysis_workspace` 限制，见 `pkg/topom/topom_rdb_analysis.go:195` 到 `pkg/topom/topom_rdb_analysis.go:438`。

proxy 的内存状态包括身份认证 token、`models.Proxy`、两个 listener、router、HA sentinel monitor、可选 Jodis 注册器和活动 session registry，见 `pkg/proxy/proxy.go:29` 到 `pkg/proxy/proxy.go:54` 以及 `pkg/proxy/client_list.go:18` 到 `pkg/proxy/client_list.go:55`。router 持有 1024 个 slot、主从后端连接池和 online/closed 状态，见 `pkg/proxy/router.go:18` 到 `pkg/proxy/router.go:30`。session registry 以进程内递增 id 索引活动 `Session`，是 `SessionsAlive()` 和 `CLIENT LIST` 的当前活动连接权威来源；`stats.go` 中的 `sessions.total` 只表示累计连接数，见 `pkg/proxy/stats.go:169` 到 `pkg/proxy/stats.go:183`。

后端 Redis 数据本身不进入 coordinator；coordinator 只保存拓扑和动作状态。Codis Server 基于嵌入式 Redis 源码构建，并增加 slot 查询、slot 删除和迁移相关命令，说明见 `doc/redis_change_zh.md:1` 到 `doc/redis_change_zh.md:17` 以及 `doc/redis_change_zh.md:84` 到 `doc/redis_change_zh.md:103`。Redis 8 支线新增独立的 `server.codis_enabled` 状态和 `codis-enabled` immutable config；`cluster-enabled` 与 `codis-enabled` 互斥，Codis mode 保留 standalone 多 DB 行为，DB 的 `keys`、`expires`、`subexpires` 在该模式下使用 10-bit `kvstore` / `estore` slot 分区。Codis mode 下 slot keyspace 的唯一权威来源是 Redis 8 `kvstore`，不再恢复 Redis 3 的 `hash_slots[1024]` 平行索引；`SLOTSSCAN` 只扫描目标 slot dict，`SLOTSDEL` 先收集 key 快照再通过 Redis 正常删除路径触发 tag index、dirty 和 key modified 副作用，`SLOTSCHECK` 校验 key 所在 dict index 与 Codis hash slot 一致后复用 tag index assert。`redisDb.codis_tagged_keys` 只记录带 `{...}` hash tag 的 key，score 使用完整 CRC32，元素由 skiplist 持有 SDS 副本。tag index 在 DB init、temp DB、flush/swap/lazyfree、key add/delete、RDB load 和 replica full sync load 后通过 helper 维护或从 `kvstore` full-load rebuild。同步迁移 socket 缓存由 `slotsmgrt_sockfd` 描述单条目标连接，记录 fd、已选择 DB、AUTH 状态和最近使用时间；`redisServer.slotsmgrt_cached_sockets` 持有这些连接，`serverCron` 周期清理超过 TTL 的空闲项。异步迁移状态按 DB 存放在 `redisServer.slotsmgrt_cached_clients[dbid]`，每个条目持有连接到目标 Redis 的内部 cached client、目标 host/port、timeout、待 ACK 消息数、当前 `batchedObjectIterator` 和 blocked/fence 客户端列表；该数组在 `initServer()` 中按最终 `server.dbnum` 分配，保证自定义 `databases` 配置下 DB 隔离仍正确。

Redis 8 异步迁移由 `extern/redis-8.6.3/src/slots_async.c` 承载。源端 `SLOTSMGRTSLOT-ASYNC` / `SLOTSMGRTTAGSLOT-ASYNC` 从 `kvstore` per-slot dict 收集 key，tag-aware 分支用 `codisHashInfoForKey` 和 `redisDb.codis_tagged_keys` 扩展同完整 CRC32 tag key；`SLOTSMGRTONE-ASYNC` / `SLOTSMGRTTAGONE-ASYNC` 处理显式 key 列表；dump-only 命令只返回可执行的 `SLOTSRESTORE-ASYNC delete/object` 命令流，不改源端 keyspace。源端首次使用 cached client 时发送 `SLOTSRESTORE-ASYNC-AUTH`（如需要）和 `SLOTSRESTORE-ASYNC-SELECT`，之后按 `maxbulks` / `maxbytes` 推进命令流。目标端 `SLOTSRESTORE-ASYNC` 对 `string/object/list/hash/dict/zset` 统一走 Redis 8 RDB payload 校验和恢复链路，返回 `SLOTSRESTORE-ASYNC-ACK errno message`；坏 payload、认证失败、连接关闭、timeout 或 cancel 都会释放 async 状态并保留源端 key。所有 ACK 成功后，源端才删除已确认迁移的 key，并通过确定性的 `DEL` 传播删除，阻止原始 `SLOTSMGRT*-ASYNC` 进入 AOF/replica。`SLOTSMGRT-ASYNC-FENCE` 等待当前 DB 的 active migration 完成，`SLOTSMGRT-ASYNC-CANCEL` 中断当前 DB migration，`SLOTSMGRT-ASYNC-STATUS` 暴露 host、port、timeout、sending_msgs、blocked_clients 和 batched_iterator 快照。`SLOTSMGRT-EXEC-WRAPPER` 在包装命令为写且 hash key 命中当前迁移 key 或同 tag key 时返回 being migrated，读命令和无关 key 保持可执行。

Go 组件访问 Redis Server 的协议边界仍由现有 `pkg/utils/redis.Client`、proxy backend 和 topom slot action 维护。`InfoFull()` 解析 Redis `INFO` 文本并通过 `CONFIG GET maxmemory` 补充 `maxmemory`，仅在 `master_host` / `master_port` 存在时合成 `master_addr`；`SetMaster()` 保持 `MULTI`、`CONFIG SET masterauth`、`SLAVEOF`、`CONFIG REWRITE`、`CLIENT KILL TYPE normal`、`EXEC` 序列并检查事务子命令 error；`SlotsInfo()`、`MigrateSlot()`、`MigrateSlotAsync()` 继续严格要求 Redis 返回数组形状。proxy 后端连接仍在建连后发送 `AUTH` 和按 DB 发送 `SELECT`，stale keepalive 只在 `INFO` 未显示 `loading:1` 或 `master_link_status:down` 时恢复 connected；业务客户端经 proxy 的命令边界仍由 `pkg/proxy/mapper.go` 控制，Redis 8 支线存在的 `CONFIG`、`SLAVEOF`、`SLOTSMGRT*`、`SLOTSRESTORE-ASYNC*`、`SLOTSMGRT-EXEC-WRAPPER` 等危险命令仍不对业务流量放开。

运行指标在 proxy 和 dashboard 两侧都有。proxy 的 HTTP stats/model/slots API 见 `pkg/proxy/proxy_api.go:104` 到 `pkg/proxy/proxy_api.go:149`，并可上报 JSON、InfluxDB、StatsD，见 `pkg/proxy/metrics.go:42` 到 `pkg/proxy/metrics.go:174`。dashboard 周期刷新 Redis 和 proxy stats，见 `pkg/topom/topom.go:204` 到 `pkg/topom/topom.go:226`。

## 5. 代码锚点

- `cmd/proxy/main.go:main` — proxy 进程入口、配置解析、上线方式选择。
- `pkg/proxy.Proxy` — proxy 运行态对象，持有 listener、router、HA、Jodis 和 metrics。
- `pkg/proxy.Session` — 客户端连接生命周期、Redis 命令解析、pipeline 读写。
- `pkg/proxy/client_list.go` — `CLIENT LIST` 的本地命令处理、session registry、活动连接快照和 Redis `key=value` 输出格式。
- `pkg/proxy.Router` — slot 到后端连接的路由表和 master/replica 选择。
- `pkg/proxy.forwardSync` / `forwardSemiAsync` — slot 迁移期间的请求转发策略。
- `pkg/utils/redis/client.go` — topom/admin/HA 访问 Redis Server 的 Go client 封装，承载 INFO/CONFIG、复制控制、slot 查询和同步/异步迁移返回解析。
- `pkg/proxy/backend.go` / `pkg/proxy/session.go` / `pkg/proxy/forward.go` / `pkg/proxy/mapper.go` — proxy 后端 AUTH/SELECT/INFO keepalive、本地 `SLOTSINFO` / `SLOTSSCAN` dispatch、半异步迁移 wrapper 解析和业务命令 allow-list 边界。
- `pkg/proxy/redis/redistest` — Go 兼容性测试共用的 fake Redis server helper，用于复用 RESP transport、命令记录和测试响应构造。
- `cmd/dashboard/main.go:main` — dashboard 进程入口、coordinator client 创建、topom 启动。
- `pkg/topom.Topom` — 拓扑管理核心对象，承载 store/cache/action/stats/ha。
- `pkg/topom/topom_api.go:newApiServer` — dashboard HTTP 管理 API 路由。
- `pkg/topom/topom_rdb_analysis.go` — dashboard/topom 进程内 RDB analysis manager、job 生命周期、workspace/upload 输入控制、RDB 流式解析和聚合结果构建。
- `pkg/topom/topom_rdb_analysis_api.go` — RDB Analysis dashboard API handler 与 `ApiClient` 封装，覆盖 upload/start/get/cancel/remove。
- `pkg/topom/topom_slots.go` — slot action、迁移推进、rebalance。
- `pkg/topom/topom_group.go` — group/server 增删、主从 promotion、同步标记。
- `pkg/topom/topom_proxy.go` — proxy 注册、上线、重初始化和 slot 同步。
- `pkg/topom/topom_sentinel.go` — sentinel 配置同步和 master 切换。
- `pkg/models.Client` / `pkg/models.Store` — coordinator 存储抽象和 Codis 元数据路径。
- `pkg/models/consul` — Consul coordinator/Jodis 后端，使用 Consul KV + Session 实现 `models.Client`。
- `cmd/fe/main.go` — FE 静态资源服务和 dashboard reverse proxy。
- `cmd/fe/assets/rdb-analysis.js` / `cmd/fe/assets/index.html` — FE RDB Analysis 区域、任务创建、轮询、取消、错误展示、结果表格和 flamegraph 树形表格。
- `cmd/admin/main.go` — admin CLI 参数分发。
- `cmd/ha/main.go` — HA 巡检和自动维护循环。
- `Makefile` — 二进制、嵌入式 Redis、默认配置的构建入口。
- `config/redis.conf` / `config/sentinel.conf` — Redis 8 Codis Server tracked 默认配置模板；`redis.conf` 显式包含 `codis-enabled yes`，Sentinel 模板保留 Redis 8 默认并补充 Codis packaging 暴露面说明。
- `Dockerfile` / `scripts/docker.sh` / `example/server.py` / `kubernetes/codis-server.yaml` — 默认 Redis 8 Codis Server 的发布包装和本地示例入口。
- `scripts/redis8_local_validation.py` — Redis 8 本地 Mac 非性能 validation gate，启动临时 filesystem coordinator、dashboard、proxy、Redis 8 group 和 Redis 3 fallback 进程，输出 e2e 与跨版本迁移 evidence；它不是生产运行入口。
- `.codestable/features/2026-05-17-redis8-validation-cutover/redis8-local-validation-matrix.md` / `redis8-linux-validation-handoff.md` / `redis8-cutover-runbook-draft.md` / `redis8-local-validation-evidence.json` — Redis 8 cutover 验证矩阵、Linux 正式验证交接、runbook 草案和本地证据。
- `extern/redis-8.6.3/src/config.c` / `server.h` / `server.c` / `db.c` / `lazyfree.c` — Redis 8 `codis-enabled` 模式、1024 slot `kvstore` keyspace、Codis CRC32 slot 计算和 flush/temp DB 重建路径。
- `extern/redis-8.6.3/src/slots.c` / `extern/redis-8.6.3/src/commands/slotshashkey.json` / `extern/redis-8.6.3/src/commands/slotsinfo.json` / `extern/redis-8.6.3/src/commands/slotsscan.json` / `extern/redis-8.6.3/src/commands/slotsdel.json` / `extern/redis-8.6.3/src/commands/slotscheck.json` / `extern/redis-8.6.3/tests/unit/codis.tcl` — Redis 8 Codis mode 基础 slot 命令、slot keyspace helper、tag index helper 和 Tcl 回归测试。
- `extern/redis-8.6.3/src/slots.c` / `extern/redis-8.6.3/src/server.h` / `extern/redis-8.6.3/src/server.c` / `extern/redis-8.6.3/src/commands/slotsmgrtone.json` / `extern/redis-8.6.3/src/commands/slotsmgrtslot.json` / `extern/redis-8.6.3/src/commands/slotsmgrttagone.json` / `extern/redis-8.6.3/src/commands/slotsmgrttagslot.json` / `extern/redis-8.6.3/src/commands/slotsrestore.json` / `extern/redis-8.6.3/tests/unit/codis_migration.tcl` / `extern/redis-8.6.3/tests/unit/codis_slotsrestore.tcl` — Redis 8 同步迁移、RDB fragment restore、`slotsmgrt_sockfd` socket 缓存和迁移 Tcl 回归测试。
- `extern/redis-8.6.3/src/slots_async.c` / `extern/redis-8.6.3/src/server.h` / `extern/redis-8.6.3/src/server.c` / `extern/redis-8.6.3/src/networking.c` / `extern/redis-8.6.3/src/blocked.c` / `extern/redis-8.6.3/src/commands/*async*.json` / `extern/redis-8.6.3/src/commands/slotsmgrt-exec-wrapper.json` / `extern/redis-8.6.3/tests/unit/codis_async_migration.tcl` — Redis 8 异步迁移、restore async ACK 子协议、per-DB cached migration client、fence/cancel/status、exec wrapper 写保护和 Tcl 回归测试。
- `go.mod` / `go.sum` — Go modules 最小编译闭环的依赖入口和校验锁定。
- `third_party/jemalloc-go` — `github.com/spinlock/jemalloc-go` 的本地 replace 模块，提供 `cgo_jemalloc` 构建所需的 Go wrapper、头文件和 C 源码。
- `pkg/utils/version.go` / `version` — clean checkout 版本元数据兜底和 Makefile 构建时的 ldflags 注入来源。

## 6. 已知约束 / 边界情况

- 当前仓库已建立 Go modules 编译闭环：默认 cmd/pkg module mode 可编译测试，`cgo_jemalloc` proxy 也可通过 `third_party/jemalloc-go` 的本地 replace 模块在 module mode 下构建；`Makefile` 已完成 module mode 切换（`make gotest`、`make build-all` 均不再依赖 GOPATH/vendor 参数）；旧 `vendor/` / `Godeps/` 已退休。
- RDB Analysis 首版只分析“已经存在的 RDB 文件”：浏览器上传会先落到 dashboard 的 `rdb_analysis_workspace/uploads` 临时文件，本地路径模式只能读取 `rdb_analysis_workspace` 下的文件；它不自动执行 `BGSAVE` / `SAVE`，不读取 Redis Server 机器上的任意文件，不实现 replication stream 抓取 RDB。分析结果可能包含 key 名，所有 RDB Analysis API 都要求 `xauth`，任务和结果只保存在 dashboard 进程内存中，不进入 coordinator。
- 默认 `make` / `make codis-server` 已切换为 Redis 8 Codis Server 发布物，`config/redis.conf` 显式启用 `codis-enabled yes`，但仍不启用 Redis Cluster 协议。Redis 3 只作为显式 `make codis-server-redis3` fallback 保留；`make codis-server-redis8` 是默认 Redis 8 产物的 suffixed alias，不再代表独立第二套 Redis 8 构建。Redis 8 ↔ Redis 8 的 1024 slot keyspace、tag index、RDB fragment restore、restore async ACK、fence/cancel/status、exec wrapper 写保护、Go proxy/topom/admin 关键协议兼容面、本地 Mac 非性能 e2e 和跨版本 fragment 观察已验证；远程 Linux 正式 e2e、性能基线、fork/RDB、复制、Docker/部署包装和最终 cutover 证据仍属于 `redis8-linux-validation-cutover`。
- 本地跨版本 fragment 矩阵结论是 Redis 3 → Redis 8 `SLOTSMGRTTAGONE` 对 string/hash/list/zset 样本成功，Redis 8 → Redis 3 对同类样本返回可观测 `SLOTSRESTORE` 错误且源端 key 保留。该结论只约束 slot migration fragment 方向性，不等价于 Redis 8 持久化 RDB/AOF 可降级给 Redis 3。
- 本地 Mac validation gate 不产出性能、fork/RDB、复制或网络栈正式结论；所有 throughput/latency、RDB/fork、replication、Docker/部署包装和生产 cutover 判断以后续 Linux 验证为准。
- Redis 8 同步迁移的安全边界是“目标端确认写入成功后才删除源端 key”：connect/write/read、AUTH、SELECT 或 `SLOTSRESTORE` 任一失败都会关闭对应 socket 缓存并返回错误，不能删除源端数据；成功路径只传播 `DEL`，不传播原始 `SLOTSMGRT*` 命令。
- Redis 8 异步迁移同样遵守“所有 ACK 成功后才删除源端 key”：connect/write/read、AUTH、SELECT、restore async ACK、timeout、cancel 或 client close 任一失败都会保留源端数据；同一 DB 同时只允许一个 async migration，`SLOTSMGRT-ASYNC-STATUS` / `FENCE` / `CANCEL` 都按当前 DB 隔离。
- `go.mod` 使用当前本地工具链版本 `go 1.26.1`；Go modules 构建迁移全部完成，编译契约和注意事项已落档入 `CLAUDE.md` 和 `.codestable/attention.md`。
- 对同一个业务集群，现有文档要求同一时刻最多一个 dashboard，且所有集群修改都经由 dashboard 完成，见 `doc/tutorial_zh.md:43` 到 `doc/tutorial_zh.md:46`。
- proxy 通过普通 Redis 协议面向客户端，但并不支持所有 Redis 命令；现有 README 明确提到 unsupported command list，见 `README.md:23` 到 `README.md:26`。`CLIENT` 命令族只支持 `CLIENT LIST`，且语义限定为当前 proxy 实例的活动客户端连接快照；它不聚合多个 proxy，不下探后端 Redis，不承诺 Redis 8.6.3 的全字段 parity，其余 `CLIENT` 子命令仍不支持，见 `doc/unsupported_cmds.md:37` 和 `pkg/proxy/client_list.go:155` 到 `pkg/proxy/client_list.go:239`。
- slot 数固定为 1024，改变该常量会影响模型、路由、迁移和外部元数据兼容性，见 `pkg/models/slots.go:13`。
- `product_name` 同时参与元数据命名空间、proxy/dashboard 鉴权和运行期路由隔离，格式校验在 `pkg/models/store.go:258` 到 `pkg/models/store.go:263`。
- `Makefile` 的组件构建会刷新 `config/dashboard.toml`、`config/proxy.toml`、`config/redis.conf`、`config/sentinel.conf`，见 `Makefile:12` 到 `Makefile:37`。
- Consul coordinator 后端只覆盖 Codis 需要的 KV、blocking query 和 Session ephemeral 语义，不表示 Codis 使用 Consul service catalog、health check 或 service mesh。默认配置仍不切到 Consul；迁移既有 filesystem/Zookeeper/Etcd 元数据到 Consul 不属于该后端实现本身。

## 7. 相关文档

- `.codestable/requirements/redis-cluster-service.md` — 当前架构承载的核心能力描述。
- `.codestable/features/2026-05-18-consul-coordinator-sdk-upgrade/consul-coordinator-sdk-upgrade-design.md` / `.codestable/features/2026-05-18-consul-coordinator-sdk-upgrade/consul-coordinator-sdk-upgrade-acceptance.md` — Consul coordinator/Jodis 后端设计与验收记录。
- `.codestable/features/2026-05-17-redis8-validation-cutover/redis8-validation-cutover-acceptance.md` — Redis 8 本地 validation-cutover 验收报告。
- `README.md` — 项目定位、特性对比、用户入口。
- `doc/tutorial_zh.md` / `doc/tutorial_en.md` — 组件说明、快速启动、HA 和部署说明。
- `doc/redis_change_zh.md` — Codis Server 对 Redis 的命令扩展。
- `doc/unsupported_cmds.md` — proxy 命令兼容边界。
- `.codestable/reference/shared-conventions.md` — CodeStable 目录和命名规范。
- `.codestable/roadmap/go-mod-migration/go-mod-migration-roadmap.md` — Go modules 迁移后续条目和边界。
