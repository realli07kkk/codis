---
doc_type: requirement
slug: redis-cluster-service
pitch: 让业务像使用单机 Redis 一样使用可扩缩容的 Redis 集群
status: current
last_reviewed: 2026-05-11
implemented_by: [system-overview]
tags: [redis, cluster, operations]
---

# 像使用单机 Redis 一样使用可扩缩容集群

## 用户故事

- 作为业务开发者，我希望继续使用普通 Redis 客户端连接服务，而不是为了分片、迁移和主从切换改造业务代码。
- 作为集群运维者，我希望在不停机的情况下增减 Redis Server、迁移 slot、重新均衡容量，而不是每次扩容都重启整套服务。
- 作为平台维护者，我希望多台 proxy 能共同承载流量并被自动发现，而不是把一个 proxy 实例变成单点。
- 作为值班人员，我希望能通过 dashboard、FE 或命令行看到 proxy、group、slot、sentinel 的状态并执行修复动作，而不是直接改底层元数据。

## 为什么需要

普通 Redis 容量和运维边界很快会撞到单机限制；直接让业务感知分片又会把复杂度推到每个调用方。Codis 的核心价值是把分片、迁移、管理和高可用动作收进服务侧，让业务连接 proxy 后仍按接近单机 Redis 的方式使用。

## 怎么解决

业务流量先进 proxy，proxy 根据 key 所在 slot 转发到后端 Redis；dashboard 维护集群拓扑、slot 归属和迁移动作，并把状态同步给各个 proxy；FE、admin 和 HA 工具围绕 dashboard 提供可视化、命令行和巡检维护入口。底层元数据放在 filesystem、Zookeeper 或 Etcd 这类 coordinator 中。

## 边界

- 它不是 Redis Cluster 协议实现，客户端不需要也不应该依赖 Redis Cluster 协议。
- 它不保证支持所有 Redis 命令；命令兼容边界以 `doc/unsupported_cmds.md` 和 proxy 命令处理逻辑为准。
- 集群拓扑变更必须经由 dashboard/topom 管理，不应绕过它直接改 coordinator 中的状态。
- 后端数据最终仍存放在 Codis Server/Redis Server；Redis 本身的容量、持久化和资源隔离仍需要单独规划。
- HA 能降低 proxy 和 Redis Server 故障影响，但不能替代监控、备份和故障演练。
