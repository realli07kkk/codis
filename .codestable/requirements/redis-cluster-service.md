---
doc_type: requirement
slug: redis-cluster-service
pitch: 让业务像使用单机 Redis 一样使用可扩缩容的 Redis 集群
status: current
last_reviewed: 2026-05-22
implemented_by: [system-overview]
tags: [redis, cluster, operations]
---

# 像使用单机 Redis 一样使用可扩缩容集群

## 用户故事

- 作为业务开发者，我希望继续使用普通 Redis 客户端连接服务，而不是为了分片、迁移和主从切换改造业务代码。
- 作为平台维护者，我希望 Codis Proxy 能对客户端 `session_auth` 的连续错误认证做临时锁定，而不是让暴露在 Redis 协议面的密码被同一来源 IP 高频尝试。
- 作为使用 cluster-mode Redis SDK 的业务开发者，我希望 SDK 只依赖 `CLUSTER NODES` bootstrap 时也能接入 Codis Proxy，而不是为了接入 Codis 额外维护一套客户端适配层。
- 作为热点读业务的开发者，我希望在不改 Redis 客户端的前提下缓解少量 string 热 key 对后端 Redis 的读放大，而不是把缓存逻辑散落到每个业务进程。
- 作为集群运维者，我希望在不停机的情况下增减 Redis Server、迁移 slot、重新均衡容量，而不是每次扩容都重启整套服务。
- 作为平台维护者，我希望多台 proxy 能共同承载流量并被自动发现，而不是把一个 proxy 实例变成单点。
- 作为值班人员，我希望能通过 dashboard、FE 或命令行看到 proxy、group、slot、sentinel 的状态并执行修复动作，而不是直接改底层元数据。
- 作为值班人员，我希望能在 Redis 协议面查询某个 proxy 实例当前接入了哪些客户端连接，而不是只能从进程日志或外部监控间接推断。
- 作为值班人员，我希望能在 Codis FE 中上传或选择受控目录内的 RDB 文件，查看 key 数、内存估算、类型/DB/prefix 分布和 big/hot key，而不是只能离线登录机器手动跑独立分析工具。

## 为什么需要

普通 Redis 容量和运维边界很快会撞到单机限制；直接让业务感知分片又会把复杂度推到每个调用方。Codis 的核心价值是把分片、迁移、管理和高可用动作收进服务侧，让业务连接 proxy 后仍按接近单机 Redis 的方式使用。

## 怎么解决

业务流量先进 proxy，proxy 根据 key 所在 slot 转发到后端 Redis；dashboard 维护集群拓扑、slot 归属和迁移动作，并把状态同步给各个 proxy；FE、admin 和 HA 工具围绕 dashboard 提供可视化、命令行和巡检维护入口。proxy 还在 Redis 协议面提供有限的本地观测、安全和兼容能力：`CLIENT LIST` 返回当前 proxy 实例的活动客户端连接快照；显式启用的 `session_auth` 防暴力破解 guard 会按客户端 remote IP 记录错误 `AUTH` 次数并临时锁定未认证 session 的后续认证尝试，锁定到期自动解除，已有已认证 session 不受影响；显式启用的 `CLUSTER NODES` 返回伪 Redis Cluster 节点清单用于 cluster-mode SDK bootstrap。`CLUSTER NODES` 的 `self` 模式只返回当前 proxy，`all` 模式从 Jodis/coordinator 注册信息轮询所有 online proxy 并均分 Redis Cluster 逻辑 slot `0-16383`，但真实请求路由仍按 Codis 1024 slot 执行。对运维显式配置的 string hot key，proxy 可启用进程内短 TTL Hot key cache，让 `GET` / `MGET` 在本 proxy 本地命中时直接返回 copied bulk value，减少后端读放大；启用写后广播时，source proxy 会把可枚举的 DB+key 失效事件上报 dashboard/topom，由 dashboard/topom 通知其他 online proxy 删除本地缓存条目，缩短跨 proxy 旧值窗口。dashboard/topom 还提供进程内 RDB Analysis 任务能力，允许 FE 通过 `xauth` 上传 RDB 文件或选择 dashboard 受控 workspace 内的 RDB 文件，异步解析并展示 DB/type/prefix 聚合、big keys、hot keys 和 flamegraph 树形数据；该能力只保留摘要和 top N，不把完整 key/value 列表常驻前端。后端 Codis Server 承载 slot keyspace、slot 查询/删除和迁移命令；默认构建产物已切到 Redis 8 Codis Server，具备 Redis 8 ↔ Redis 8 同步迁移、异步迁移与 `SLOTSRESTORE` / `SLOTSRESTORE-ASYNC` RDB fragment restore 能力，Redis 3 通过显式 fallback 构建目标保留。底层元数据放在 filesystem、Zookeeper、Etcd 或 Consul 这类 coordinator 中；Consul 后端只使用 KV 和 Session 语义。

## 实现进展

- 2026-05-13：Redis 8 Codis Server 支线完成同步迁移命令和 `SLOTSRESTORE` 移植，覆盖 `SLOTSMGRTSLOT`、`SLOTSMGRTONE`、`SLOTSMGRTTAGSLOT`、`SLOTSMGRTTAGONE` 与 Redis 8 RDB fragment restore。该进展不改变业务客户端协议，不切换默认 Redis 3 Codis Server 构建，也不承诺 Redis 3 ↔ Redis 8 RDB fragment 双向兼容。
- 2026-05-14：Redis 8 Codis Server 支线完成异步迁移移植，覆盖 `SLOTSMGRT*-ASYNC`、`SLOTSRESTORE-ASYNC*`、`SLOTSMGRT-ASYNC-FENCE/CANCEL/STATUS` 与 `SLOTSMGRT-EXEC-WRAPPER`。该进展提供 Redis 8 ↔ Redis 8 半异步迁移能力，不改变业务客户端协议，不切换默认 Redis 3 Codis Server 构建，也不承诺 Redis 3 ↔ Redis 8 异步迁移协议跨版本互通。
- 2026-05-14：Redis 8 支线完成 Go proxy/topom/admin 兼容验证，覆盖 `INFO` / `CONFIG`、default-user `AUTH <password>`、`SELECT` 当前 DB、`SLAVEOF` alias、`CLIENT KILL TYPE normal`、`SLOTSINFO`、同步/异步迁移返回格式和 `SLOTSMGRT-EXEC-WRAPPER`。真实 Redis 8 Codis Server smoke 未发现必须新增生产 adapter 的不兼容点；默认构建、配置模板、打包切换和灰度 cutover 仍按 roadmap 后续条目推进。
- 2026-05-14：默认 `codis-server` 构建、tracked Redis 配置模板和 Docker / example 包装入口已切到 Redis 8 Codis Server；`config/redis.conf` 显式启用 `codis-enabled yes` 且不启用 Redis Cluster，Redis 3 通过 `codis-server-redis3` fallback 目标保留。该进展让后续 cutover 验证覆盖真实发布物，但不等同于已完成端到端灰度、性能基线、跨版本迁移兼容或回滚策略。
- 2026-05-17：Redis 8 默认发布物完成本地 Mac 非性能 validation-cutover。证据覆盖 `make gotest`、Redis 8 Codis Tcl suite、短生命周期 dashboard/proxy/admin/Redis 8 e2e、`semi-async` 与 `sync` slot migration、普通 key / hash tag key / 非 0 DB key 迁移后读回，以及 Redis 3 ↔ Redis 8 fragment 方向性观察。当前矩阵中 Redis 3 → Redis 8 成功，Redis 8 → Redis 3 可观测失败且源端 key 保留；Linux 正式性能基线、fork/RDB、复制、Docker/部署包装和最终 cutover gate 仍待后续 `redis8-linux-validation-cutover`。
- 2026-05-18：Coordinator / Jodis 后端新增 Consul 支持。Codis 现在可通过 `coordinator_name = "consul"` 或 `jodis_name = "consul"` 使用 Consul KV 保存 `/codis3` 元数据、使用 Consul Session 维护 `/jodis` ephemeral 注册；默认 coordinator、既有 Zookeeper/Etcd/filesystem 行为和存量元数据迁移边界不变。
- 2026-05-19：Dashboard / FE 新增 RDB Analysis 离线分析能力。值班人员可在选中 product 后上传 RDB 或填写 dashboard `rdb_analysis_workspace` 下的相对路径，dashboard/topom 在进程内创建异步 job，通过 `github.com/hdt3213/rdb` 输出进度、summary、top big/hot keys、prefix 和 flamegraph 数据；所有 API 均要求 `xauth`，任务结果不进入 coordinator，首版不自动对 Redis Server 执行 `BGSAVE`/`SAVE` 或远端文件抓取。
- 2026-05-22：Codis Proxy 新增默认关闭的 Hot key cache。运维可在 proxy 配置中显式声明 exact string hot key、TTL、条目数和 value size 上限；同一 proxy 内 `GET` / `MGET` 首次 miss 仍访问后端，后续 TTL 内可本地命中，写命令经过同一 proxy 后在后端响应完成时失效本地条目。开启写后广播时，source proxy 通过 dashboard/topom 管理面 best-effort 通知其他 online proxy 删除同一 DB+key 的本地 cache，payload 保留 Redis key 原始字节。该进展不改变默认行为，不新增 dashboard 管理页或 coordinator 元数据，不承诺跨 proxy 强一致。
- 2026-05-22：Codis Proxy 新增默认关闭的 `CLUSTER NODES` 有限兼容能力。运维可配置 `cluster_nodes_compat = "self"` 返回当前 proxy 单节点清单，或配置 `"all"` 从 Jodis/coordinator 后端存储轮询 online proxy 清单并均分 `0-16383`；该进展只服务 cluster-mode SDK bootstrap，不实现 Redis Cluster 路由、`MOVED` / `ASK`、cluster bus、gossip、failover 或其他 `CLUSTER` 子命令成功路径。
- 2026-05-22：Codis Proxy 新增默认关闭的 `session_auth` 防暴力破解能力。运维可配置 `session_auth_bruteforce_enabled`、`session_auth_bruteforce_max_failures` 和 `session_auth_bruteforce_lock_duration`；同一 proxy 进程内同一来源 IP 的错误 `AUTH` 达到阈值后会临时锁定，锁定期内未认证 session 即使提交正确密码也不能继续认证，锁定到期后自动解除。该进展不改变默认行为，不影响已认证 session，不新增 coordinator 状态或跨 proxy 同步。

## 边界

- 它不是完整 Redis Cluster 协议实现；只有显式启用 `cluster_nodes_compat` 时，proxy 才为 `CLUSTER NODES` 返回伪节点清单，客户端不应该依赖 Redis Cluster 的路由、重定向、failover 或 bus 语义。
- 它不保证支持所有 Redis 命令；命令兼容边界以 `doc/unsupported_cmds.md` 和 proxy 命令处理逻辑为准。
- `CLUSTER` 命令族只支持显式启用后的 `CLUSTER NODES`；`CLUSTER SLOTS`、`CLUSTER INFO`、`CLUSTER KEYSLOT`、`CLUSTER SHARDS` 等其他子命令仍不支持。`CLUSTER NODES` 输出中的 `0-16383` 是 Redis Cluster 兼容文本，不改变 Codis 真实 `MaxSlotNum=1024` 和后端路由。
- `CLIENT` 命令族只支持 `CLIENT LIST`；该命令只返回当前 proxy 实例接入的客户端连接，不聚合多个 proxy，不下探后端 Redis，也不承诺 Redis 8.x 的所有字段。
- `session_auth` 防暴力破解默认关闭，只是单 proxy、单 remote IP 维度的客户端 `AUTH` 保护；它不是分布式撞库防护，不解析 Proxy Protocol / `X-Forwarded-For`，不持久化到 coordinator，不跨 proxy 同步，也不提供 dashboard 管理页、手动解锁 API、allowlist/denylist/CIDR 或 Redis ACL username 维度。NAT 或四层代理场景下，多个真实客户端可能共享同一个锁定维度。
- Hot key cache 默认关闭，只缓存配置 allowlist 中 exact key 的 `GET` / `MGET` string bulk value；它是 proxy 进程内短 TTL 弱一致缓存，同 proxy 写入会清理本地 cache。开启写后广播后，source proxy 可经 dashboard/topom best-effort 通知其他 online proxy 删除本地 DB+key cache，但广播队列满、dashboard/topom 不可达、目标 proxy 超时或直连后端 Redis 写入仍只能靠 TTL 收敛，不适合要求跨 proxy 强一致的 key。
- 集群拓扑变更必须经由 dashboard/topom 管理，不应绕过它直接改 coordinator 中的状态。
- Consul 只作为 coordinator/Jodis 的 KV + Session 后端，不提供存量元数据自动迁移，也不代表 Codis 使用 Consul service catalog、health check 或 service mesh。
- 后端数据最终仍存放在 Codis Server/Redis Server；Redis 本身的容量、持久化和资源隔离仍需要单独规划。
- RDB Analysis 只分析已有 RDB 文件；它不是在线 keyspace 实时订阅，不替代 Redis `INFO`/stats，也不自动从远端 Redis Server 生成、复制或读取 RDB。分析结果可能包含 key 名，只通过 dashboard `xauth` API 暴露，且 dashboard 重启后任务消失。
- Redis 8 已成为默认 Codis Server 构建和包装入口，并已完成本地 Mac 非性能 validation-cutover；当前不承诺 Linux 性能基线、fork/RDB、复制、Docker/部署包装、最终生产 cutover gate 或 Redis 8 持久化文件降级回 Redis 3。跨版本 fragment 仅以本地矩阵记录的方向性结论为准，不能外推为持久化 RDB/AOF 降级能力。
- HA 能降低 proxy 和 Redis Server 故障影响，但不能替代监控、备份和故障演练。
