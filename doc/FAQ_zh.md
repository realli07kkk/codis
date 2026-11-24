# Codis 常见问题

### Codis 是什么？

Codis 用 Proxy 为 Redis 提供分片、容量扩缩和 slot 迁移。客户端按 Redis 协议连接 proxy。后端仍需配置容量、持久化、复制和故障转移。整体组件见 [架构总览](architecture_zh.md)。

### 我的服务能直接迁移到 Codis 上吗？

先核对客户端用到的命令、多 key 操作和认证方式。常用单 key 命令按 slot 路由。事务、Pub/Sub、全局扫描等用法需要按[命令兼容清单](unsupported_cmds.md)评估。Twemproxy 或 Redis 应用也应按实际命令核对。

### Codis 和 Redis Cluster 的区别是什么？

Codis Proxy 管理 1024 个真实 slot 的路由，客户端可以连接单个 proxy 发送普通 Redis 命令。Redis Cluster 使用 16384 个 slot，客户端按集群拓扑和重定向选择节点。Codis 可选的 `CLUSTER NODES` 兼容输出只用于特定 SDK 的启动发现。详见 [Proxy 协议扩展](proxy_protocol_extensions_zh.md#cluster-nodes)。

### Codis 是如何分片的？

每个 key 使用 `CRC32(hashTagOrKey) % 1024` 计算 slot。key 含成对的大括号时，取第一个 `{` 和它之后第一个 `}` 之间的字节作为 hash tag，否则使用整个 key。每个已分配 slot 由一个 Group 承载，Dashboard 以 slot 为单位管理分配与迁移。详见 [Codis Server 的 slot 模型](redis8_codis_server_zh.md#slot-模型)。

### 如何扩容和缩容？

扩容时添加 Group 和 Server，在 FE 的 `Slots 分片`页用`自动 Rebalance`预览并确认分配计划。缩容时先把待下线 Group 的 slot 迁出，确认迁移完成后再移除 Server 和 Group。

### Codis 可以当队列使用吗？

可使用 `LPUSH`、`LPOP` 等非阻塞操作。单个 list key 仍落在一个后端，队列容量及吞吐受该后端约束。阻塞列表命令的边界见[命令清单](unsupported_cmds.md)。

### Codis 支持 MSET、MGET、事务或 Lua 脚本吗？

`MSET` 和 `MGET` 支持跨 slot，proxy 拆分请求并汇总响应。`MSET` 的跨分片写入可能部分完成。`WATCH` / `MULTI` / `EXEC` 不受支持。`EVAL` / `EVALSHA` 按首个声明的 key 路由，多 key 脚本需要保证相关 key 位于同一 slot。详见 [Proxy 命令清单](unsupported_cmds.md)。

### Redis 故障或扩缩容会怎样影响数据？

数据由 Redis 后端持久化和复制。主从异步复制下，故障切换可能丢失尚未复制的写入。内存写满时的行为取决于 Redis 的 `maxmemory-policy`。迁移时，Dashboard 下发 slot 状态，Proxy 对迁移中的 slot 走专用转发路径。详见 [Codis Server 迁移实现](redis8_codis_server_zh.md#异步迁移家族)。

### Codis 支持哪些外部存储？

支持 ZooKeeper、filesystem 和 Consul。Dashboard 使用 `coordinator_name` 和 `coordinator_addr` 选择后端，随仓库生成的默认配置为 filesystem。ZooKeeper SDK 迁移说明见 [专项文档](zookeeper_sdk_migration_zh.md)。

### 如何把现有 Redis 或 Codis 的数据迁入新集群？

在 FE 的 `数据集成 / 数据迁移`页创建任务，或调用 Dashboard API。支持 standalone、Redis Cluster 和 Codis 静态拓扑间的 `sync`、`scan` 和 `rdb`。Dashboard 启动 `codis-shake` worker，worker 直连源端和目标端。任务停止后按运维手顺核对退出状态和目标数据，再切流。 [数据迁移控制台](data_migration_zh.md)

### Dashboard 中 Ops 一直是 0？

确认业务流量经过 proxy，并从 dashboard 所在主机检查已注册 proxy 的 `admin_addr` 是否可达。Dashboard 通过该管理地址获取 proxy stats。需要定位采集错误时，检查 Dashboard 日志和 proxy 管理面状态。

### Dashboard 重启提示拓扑锁已存在怎么办？

当前拓扑锁路径为 `/codis3/{product}/topom`。确认持有锁的 Dashboard 已退出后，使用 `codis-admin --remove-lock`，并指定该 product 实际使用的 coordinator。命令示例见[使用教程](tutorial_zh.md#25-codis-admin命令行工具)。

### ZooKeeper session 过期会让 proxy 退出吗？

不会。Jodis 注册由 proxy 的后台 watcher 维护：watch 断开或临时节点创建失败时，按 1 秒起、上限 30 秒的指数退避重试，恢复后重新注册到默认路径 `/jodis/{product}/proxy-{token}`（`jodis_compatible = true` 时用 Codis 2.0 的 `/zk/codis/...` 路径）。运行中的 slot 路由由 Dashboard 通过管理 API 下发，不依赖 zk watch，当前主线没有“zk 会话过期即退出”的处理路径。

会退出的失败发生在启动阶段：`--dashboard` 自动上线每 3 秒尝试一次、共 10 次，仍失败时打印 `online proxy failed` 后退出；coordinator 模式重试 30 次后同样退出。排查时先区分 Jodis 注册失败、启动自动上线失败和运行中退出，再看对应日志。默认 `jodis_timeout` 为 `20s`。

### 编译或版本信息异常怎么办？

从当前仓库检出目录按 Go modules 流程构建。`pkg/utils/version.go` 提供版本兜底值，`make` 通过 ldflags 注入实际构建版本。详见[构建与发布指南](build_and_release_zh.md)。

### Codis 的性能如何？

[历史 benchmark](benchmark.md) 记录了早期版本的数据。当前 Redis 8 Linux 性能基线状态见[真机验证索引](validation/README.md)。
