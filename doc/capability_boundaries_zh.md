# Codis 能力边界

Codis 的目标是让业务像用单机 Redis 一样使用可扩缩容的 Redis 集群：客户端只连 proxy，分片、迁移和高可用动作都收在服务侧。代价是每项能力都有明确范围——本文按能力域列出“支持什么”和“明确不做什么”，用于判断一个诉求该由 Codis 承担，还是应落到 Redis Server、外部监控或部署层。

## 分片与路由

| 能力 | 支持 | 明确不支持或限制 |
| --- | --- | --- |
| 分片 | 固定 1024 slot，key 取第一个 `{...}` 作 hash tag 后按 CRC32 落到 slot | 不是 Redis Cluster：没有 cluster bus、gossip、`MOVED`/`ASK`、cluster failover；1024 与 16384 是两套独立 slot 空间 |
| 拓扑变更 | 不停机增删 Codis Server、迁移 slot、重新均衡容量 | 必须经 dashboard/topom 管理，不得绕过它直接改 coordinator 中的状态 |
| 数据归属 | proxy 按 slot 转发，不在业务路径持久化数据；元数据存于 filesystem、Zookeeper、Consul | 后端容量、持久化和资源隔离仍由 Redis 自身承担，Codis 不代为规划 |
| Consul 后端 | 只使用 Consul 的 KV 与 Session 语义保存 `/codis3` 元数据与 `/jodis` 注册 | 不迁移存量元数据；不使用 Consul service catalog、health check 或 service mesh |

## 数据迁移

| 能力 | 支持 | 明确不支持或限制 |
| --- | --- | --- |
| Redis 8 ↔ Redis 8 | `SLOTSMGRTSLOT`/`SLOTSMGRTONE`/`SLOTSMGRTTAGSLOT`/`SLOTSMGRTTAGONE`、`-ASYNC` 系列，以及 `SLOTSRESTORE`/`SLOTSRESTORE-ASYNC` fragment restore | 同步与异步协议只在同一大版本内互通，不能跨版本假设可用 |
| 跨大版本 | 不提供兼容性承诺 | 只保证 Redis 8 ↔ Redis 8；不要依赖旧版本二进制或数据文件回滚 |

## ACL 与认证

| 能力 | 支持 | 明确不支持或限制 |
| --- | --- | --- |
| 统一 ACL 管理 | 默认关闭的 `codis_acl_enabled`：dashboard/topom 提交 Redis 8 ACL 用户、password hashes 与 rules，并按 revision 下发 proxy snapshot；客户端可用 `AUTH [username] password` 与 `ACL WHOAMI` | 只面向 Redis 8 backend；不实现完整 Redis Cluster ACL、RESP3/`HELLO` parity、Pub/Sub channel ACL、模块 key-spec 语义、跨 dashboard 原子事务或跨 proxy 强一致瞬时切换 |
| 客户端权限 | 普通命令走 user-bound backend connection，返回前对原始命令做 ACL 校验 | 普通客户端不能通过 proxy 执行 ACL 管理命令 |
| db 绑定 | ACL 用户可绑定固定 db，认证后 proxy 强制该 session 路由到绑定 db，优先级高于 `SELECT` | 只是 proxy 路由属性，不渲染进 `ACL SETUSER`、不放大权限；`default` 用户只能绑一个 db，同一用户无法按密码分流到不同 db |
| 凭据 | 明文 `new_password` 只作为提交时的短期输入 | 不写 coordinator、stats、API 响应或 FE 视图；backend service 密码与 session 密码仍属运行期凭据，需按配置与进程内存风险管理 |

## QPS 限流

| 能力 | 支持 | 明确不支持或限制 |
| --- | --- | --- |
| proxy QPS limit | 默认关闭；dashboard/FE 读写 `proxy_qps_limit`，topom 写 coordinator 并下发 online proxy，proxy reinit 时重放；超限返回 `ERR proxy qps limit exceeded` 且不访问后端 Redis | 只表示单个 proxy 进程内所有 session 共享的普通请求上限，不是全局分布式限流 |
| 粒度 | 进程内 token bucket，默认 0 表示不限制 | 无 per-client、per-IP、per-user、per-command、per-DB、per-slot、per-key 或 per-backend group 粒度；token 状态不写 coordinator |
| 例外 | `AUTH` 与 `QUIT` 不被限流阻断 | 普通客户端不能通过 Redis 协议 `CONFIG` 管理该能力 |

## 防暴力破解

| 能力 | 支持 | 明确不支持或限制 |
| --- | --- | --- |
| session_auth guard | 默认关闭；按客户端 remote IP 统计错误 `AUTH`，达到 `session_auth_bruteforce_max_failures` 后临时锁定未认证 session，到期自动解除，已认证 session 不受影响 | 只是单 proxy、单来源 IP 维度的保护，不是分布式撞库防护 |
| 状态 | 进程内计数 | 不持久化到 coordinator、不跨 proxy 同步；不提供 dashboard 管理页、手动解锁 API、allowlist/denylist/CIDR 或 ACL username 维度 |
| 部署前提 | 同一 IP 下的客户端共享同一失败计数 | 不解析 Proxy Protocol 或 `X-Forwarded-For`；NAT、四层代理场景下多个真实客户端可能共用一个锁定维度 |

## 协议兼容

| 能力 | 支持 | 明确不支持或限制 |
| --- | --- | --- |
| 普通 Redis 命令 | 常用命令按 key 所在 slot 路由，普通客户端无需改造 | 不保证支持所有 Redis 命令，边界以 [unsupported_cmds.md](unsupported_cmds.md) 和 proxy 命令处理逻辑为准 |
| `CLUSTER NODES` | 默认关闭；`cluster_nodes_compat = "self"` 只返回当前 proxy，`"all"` 从 Jodis/coordinator 轮询 online proxy 并均分逻辑 slot `0-16383` | 只服务 cluster-mode SDK bootstrap；不实现真实 Redis Cluster 路由、重定向、failover 或 bus；`CLUSTER SLOTS`、`CLUSTER INFO`、`CLUSTER KEYSLOT`、`CLUSTER SHARDS` 仍不支持；`0-16383` 不改变 1024 slot 的真实路由 |
| `CLIENT` 命令族 | 只支持 `CLIENT LIST`，返回当前 proxy 实例的活动客户端连接快照 | 不聚合多个 proxy、不下探后端 Redis、不承诺 Redis 8 全字段；其余 `CLIENT` 子命令不支持 |
| Stream | 单 key Stream 命令、`XGROUP`/`XINFO` 容器命令、非阻塞 `XREAD`/`XREADGROUP`（所有 stream key 共享同一 hash tag 或同一个 key） | 不是完整 Stream 支持：`XGROUP HELP`/`XINFO HELP`、带 `BLOCK` 的读、跨 hash tag multi-stream、未知 subcommand 由 proxy 拒绝；不含 Vector Set、Hash field expiration 与 RedisJSON/RedisBloom/RedisTimeSeries/RediSearch 等模块能力 |

## 热 key 缓存

| 能力 | 支持 | 明确不支持或限制 |
| --- | --- | --- |
| Hot key cache | 默认关闭；按 `hot_key_cache_keys` allowlist 缓存 exact string key 的 `GET`/`MGET` bulk value，TTL、条目数与单 value 大小可配 | 只支持 exact key，不支持通配符、prefix 或正则；只缓存 string bulk value，其他类型与命令直通后端 |
| 一致性 | 同一 proxy 写入后本地失效；开启广播后 source proxy 经 dashboard/topom best-effort 通知其他 online proxy 删除 DB+key | 进程内短 TTL 弱一致缓存：广播队列满、管理面不可达、目标 proxy 超时或客户端直连后端写入都只能靠 TTL 收敛；不适合要求跨 proxy 强一致的 key |

## RDB 分析与导出

| 能力 | 支持 | 明确不支持或限制 |
| --- | --- | --- |
| RDB Analysis | dashboard 进程内异步分析 RDB，输出进度、summary、big/hot keys、prefix 与 flamegraph | 只分析已有 RDB 文件；不是在线 keyspace 订阅，不替代 `INFO`/stats，不自动生成 RDB；结果可能含 key 名，只经 dashboard `xauth` API 暴露，dashboard 重启后任务消失；只保留摘要与 top N |
| Remote fetch | dashboard 从当前 product 内 Redis 8 server 的固定 export 拉取已有 `dbfilename` 文件后建分析任务，export auth 取自 dashboard 本地持久化配置 | 只接受当前 product model 中精确匹配的 `server_addr`，不接受任意 URL、path、query 或跨 product server；不支持远端主动上传、tus、Range、断点续传或从 replication stream 抓 RDB |
| RDB HTTP export | Redis 8 Codis Server 默认关闭；复用监听端口，只接受 `GET /codis/rdb/latest` 与 `X-Codis-RDB-Auth` | 只导出本机当前 `dbfilename` 的已有 RDB，不触发 `SAVE`/`BGSAVE`、不扫描其他 RDB；不支持客户端指定路径、Range、压缩、加密或独立端口；不替代网络隔离、TLS 与外层访问控制；`codis-rdb-export-enabled` 与 `codis-rdb-export-auth` 需重启生效 |
| 导出限速 | `codis-rdb-export-rate-limit` 可运行期调整，0 表示不限速 | 只限制成功响应 body 的写出，多个 export 连接共享同一预算，不承诺 per-client fairness 或并发连接上限；不影响普通 Redis 命令执行线程模型，也不限制普通 reply |

## PITR 与高可用

| 能力 | 支持 | 明确不支持或限制 |
| --- | --- | --- |
| PITR / 数据闪回 | 默认关闭；对当前 product 内单台 Redis 8 Codis Server 做 AOF 时间点截断恢复，复用 `aof-timestamp-enabled` 与 `redis-check-aof --truncate-to-timestamp`，不修改 Redis C 源码 | 不做跨 group 全局一致性恢复、不做指定 key 子集恢复；不自动开启 AOF 或 `aof-timestamp-enabled`；不做 AOF 保留策略、轮转、备份管理或跨机文件操作；不编排 proxy 只读切换；不在业务协议路径暴露命令；job 不进 coordinator；精度为秒级 |
| PITR 运行前提 | dashboard 可直访目标 server 的 AOF 目录（同机或共享存储），`redis-check-aof` 可执行，由外部进程管理器拉起 redis（dashboard 绝不 fork `redis-server`） | 不幂等：per-server lock 持续到人工显式 Remove；`SHUTDOWN` 后必须 fresh probe 确认已停才继续；truncate 失败不自动重启（AOF 可能已损坏，down 加告警优于 up 后悄悄出错） |
| HA | `codis-ha` 周期巡检与 promote，降低 proxy 与 Codis Server 故障影响 | 不能替代监控、备份和故障演练 |

## 构建与发布

| 能力 | 支持 | 明确不支持或限制 |
| --- | --- | --- |
| 默认构建 | `make` / `make build-all` 产出当前机器可运行的根 `bin/` 产物；`codis-server` 默认内嵌 Redis 8 并启用 `codis-enabled yes` | 不承诺 Linux 性能基线、fork/RDB、复制、部署包装与最终生产切换结论；不承诺跨大版本 RDB/AOF 兼容 |
| 平台矩阵 | 发布入口按 `darwin/arm64`、`linux/amd64`、`linux/arm64` 等目标生成独立目录，每个目录是一个平台的完整产物集合 | 不保证在任意 host 上交叉构建任意 target，完整产物仍依赖目标平台环境或配置好的 C/cgo toolchain；不是纯 Go cross build，proxy 与 Codis Server 仍含 C/cgo 边界 |
| 构建边界 | 构建前检查目标平台与必要工具链，条件不满足直接报错 | 不改变运行期协议、proxy 路由、dashboard 行为或 Redis 数据格式；不负责 Docker 镜像、CI 发布流水线、release 包上传或生产部署编排 |

## 相关文档

- [architecture_zh.md](architecture_zh.md)：组件职责、状态归属与模块位置。
- [codis_acl_zh.md](codis_acl_zh.md)：统一 ACL、多账号认证与 db 绑定。
- [pitr_flashback_zh.md](pitr_flashback_zh.md)：PITR 状态机与安全边界。
- [proxy_protocol_extensions_zh.md](proxy_protocol_extensions_zh.md)：proxy 的本地命令与协议扩展。
- [proxy_hot_key_cache_zh.md](proxy_hot_key_cache_zh.md)、[proxy_qps_rate_limit_zh.md](proxy_qps_rate_limit_zh.md)、[proxy_session_auth_bruteforce_zh.md](proxy_session_auth_bruteforce_zh.md)：proxy 缓存、限流与防暴破配置。
- [rdb_analysis_zh.md](rdb_analysis_zh.md)：RDB Analysis 使用方式与边界。
- [redis8_codis_server_zh.md](redis8_codis_server_zh.md)：Redis 8 Codis Server 的 slot 模型与迁移命令。
- [build_and_release_zh.md](build_and_release_zh.md)：构建、平台矩阵与发布产物。
