---
doc_type: requirement
slug: redis-cluster-service
pitch: 让业务像使用单机 Redis 一样使用可扩缩容的 Redis 集群
status: current
last_reviewed: 2026-05-19
implemented_by: [system-overview]
tags: [redis, cluster, operations]
---

# 像使用单机 Redis 一样使用可扩缩容集群

## 用户故事

- 作为业务开发者，我希望继续使用普通 Redis 客户端连接服务，而不是为了分片、迁移和主从切换改造业务代码。
- 作为集群运维者，我希望在不停机的情况下增减 Redis Server、迁移 slot、重新均衡容量，而不是每次扩容都重启整套服务。
- 作为平台维护者，我希望多台 proxy 能共同承载流量并被自动发现，而不是把一个 proxy 实例变成单点。
- 作为值班人员，我希望能通过 dashboard、FE 或命令行看到 proxy、group、slot、sentinel 的状态并执行修复动作，而不是直接改底层元数据。
- 作为值班人员，我希望能在 Redis 协议面查询某个 proxy 实例当前接入了哪些客户端连接，而不是只能从进程日志或外部监控间接推断。
- 作为值班人员，我希望能在 Codis FE 中上传或选择受控目录内的 RDB 文件，查看 key 数、内存估算、类型/DB/prefix 分布和 big/hot key，而不是只能离线登录机器手动跑独立分析工具。

## 为什么需要

普通 Redis 容量和运维边界很快会撞到单机限制；直接让业务感知分片又会把复杂度推到每个调用方。Codis 的核心价值是把分片、迁移、管理和高可用动作收进服务侧，让业务连接 proxy 后仍按接近单机 Redis 的方式使用。

## 怎么解决

业务流量先进 proxy，proxy 根据 key 所在 slot 转发到后端 Redis；dashboard 维护集群拓扑、slot 归属和迁移动作，并把状态同步给各个 proxy；FE、admin 和 HA 工具围绕 dashboard 提供可视化、命令行和巡检维护入口。proxy 还在 Redis 协议面提供有限的本地观测命令，例如 `CLIENT LIST` 返回当前 proxy 实例的活动客户端连接快照。dashboard/topom 还提供进程内 RDB Analysis 任务能力，允许 FE 通过 `xauth` 上传 RDB 文件或选择 dashboard 受控 workspace 内的 RDB 文件，异步解析并展示 DB/type/prefix 聚合、big keys、hot keys 和 flamegraph 树形数据；该能力只保留摘要和 top N，不把完整 key/value 列表常驻前端。后端 Codis Server 承载 slot keyspace、slot 查询/删除和迁移命令；默认构建产物已切到 Redis 8 Codis Server，具备 Redis 8 ↔ Redis 8 同步迁移、异步迁移与 `SLOTSRESTORE` / `SLOTSRESTORE-ASYNC` RDB fragment restore 能力，Redis 3 通过显式 fallback 构建目标保留。底层元数据放在 filesystem、Zookeeper、Etcd 或 Consul 这类 coordinator 中；Consul 后端只使用 KV 和 Session 语义。

## 实现进展

- 2026-05-13：Redis 8 Codis Server 支线完成同步迁移命令和 `SLOTSRESTORE` 移植，覆盖 `SLOTSMGRTSLOT`、`SLOTSMGRTONE`、`SLOTSMGRTTAGSLOT`、`SLOTSMGRTTAGONE` 与 Redis 8 RDB fragment restore。该进展不改变业务客户端协议，不切换默认 Redis 3 Codis Server 构建，也不承诺 Redis 3 ↔ Redis 8 RDB fragment 双向兼容。
- 2026-05-14：Redis 8 Codis Server 支线完成异步迁移移植，覆盖 `SLOTSMGRT*-ASYNC`、`SLOTSRESTORE-ASYNC*`、`SLOTSMGRT-ASYNC-FENCE/CANCEL/STATUS` 与 `SLOTSMGRT-EXEC-WRAPPER`。该进展提供 Redis 8 ↔ Redis 8 半异步迁移能力，不改变业务客户端协议，不切换默认 Redis 3 Codis Server 构建，也不承诺 Redis 3 ↔ Redis 8 异步迁移协议跨版本互通。
- 2026-05-14：Redis 8 支线完成 Go proxy/topom/admin 兼容验证，覆盖 `INFO` / `CONFIG`、default-user `AUTH <password>`、`SELECT` 当前 DB、`SLAVEOF` alias、`CLIENT KILL TYPE normal`、`SLOTSINFO`、同步/异步迁移返回格式和 `SLOTSMGRT-EXEC-WRAPPER`。真实 Redis 8 Codis Server smoke 未发现必须新增生产 adapter 的不兼容点；默认构建、配置模板、打包切换和灰度 cutover 仍按 roadmap 后续条目推进。
- 2026-05-14：默认 `codis-server` 构建、tracked Redis 配置模板和 Docker / example 包装入口已切到 Redis 8 Codis Server；`config/redis.conf` 显式启用 `codis-enabled yes` 且不启用 Redis Cluster，Redis 3 通过 `codis-server-redis3` fallback 目标保留。该进展让后续 cutover 验证覆盖真实发布物，但不等同于已完成端到端灰度、性能基线、跨版本迁移兼容或回滚策略。
- 2026-05-17：Redis 8 默认发布物完成本地 Mac 非性能 validation-cutover。证据覆盖 `make gotest`、Redis 8 Codis Tcl suite、短生命周期 dashboard/proxy/admin/Redis 8 e2e、`semi-async` 与 `sync` slot migration、普通 key / hash tag key / 非 0 DB key 迁移后读回，以及 Redis 3 ↔ Redis 8 fragment 方向性观察。当前矩阵中 Redis 3 → Redis 8 成功，Redis 8 → Redis 3 可观测失败且源端 key 保留；Linux 正式性能基线、fork/RDB、复制、Docker/部署包装和最终 cutover gate 仍待后续 `redis8-linux-validation-cutover`。
- 2026-05-18：Coordinator / Jodis 后端新增 Consul 支持。Codis 现在可通过 `coordinator_name = "consul"` 或 `jodis_name = "consul"` 使用 Consul KV 保存 `/codis3` 元数据、使用 Consul Session 维护 `/jodis` ephemeral 注册；默认 coordinator、既有 Zookeeper/Etcd/filesystem 行为和存量元数据迁移边界不变。
- 2026-05-19：Dashboard / FE 新增 RDB Analysis 离线分析能力。值班人员可在选中 product 后上传 RDB 或填写 dashboard `rdb_analysis_workspace` 下的相对路径，dashboard/topom 在进程内创建异步 job，通过 `github.com/hdt3213/rdb` 输出进度、summary、top big/hot keys、prefix 和 flamegraph 数据；所有 API 均要求 `xauth`，任务结果不进入 coordinator，首版不自动对 Redis Server 执行 `BGSAVE`/`SAVE` 或远端文件抓取。

## 边界

- 它不是 Redis Cluster 协议实现，客户端不需要也不应该依赖 Redis Cluster 协议。
- 它不保证支持所有 Redis 命令；命令兼容边界以 `doc/unsupported_cmds.md` 和 proxy 命令处理逻辑为准。
- `CLIENT` 命令族只支持 `CLIENT LIST`；该命令只返回当前 proxy 实例接入的客户端连接，不聚合多个 proxy，不下探后端 Redis，也不承诺 Redis 8.x 的所有字段。
- 集群拓扑变更必须经由 dashboard/topom 管理，不应绕过它直接改 coordinator 中的状态。
- Consul 只作为 coordinator/Jodis 的 KV + Session 后端，不提供存量元数据自动迁移，也不代表 Codis 使用 Consul service catalog、health check 或 service mesh。
- 后端数据最终仍存放在 Codis Server/Redis Server；Redis 本身的容量、持久化和资源隔离仍需要单独规划。
- RDB Analysis 只分析已有 RDB 文件；它不是在线 keyspace 实时订阅，不替代 Redis `INFO`/stats，也不自动从远端 Redis Server 生成、复制或读取 RDB。分析结果可能包含 key 名，只通过 dashboard `xauth` API 暴露，且 dashboard 重启后任务消失。
- Redis 8 已成为默认 Codis Server 构建和包装入口，并已完成本地 Mac 非性能 validation-cutover；当前不承诺 Linux 性能基线、fork/RDB、复制、Docker/部署包装、最终生产 cutover gate 或 Redis 8 持久化文件降级回 Redis 3。跨版本 fragment 仅以本地矩阵记录的方向性结论为准，不能外推为持久化 RDB/AOF 降级能力。
- HA 能降低 proxy 和 Redis Server 故障影响，但不能替代监控、备份和故障演练。
