# Codis 文档索引

Codis 是基于代理的高性能 Redis 集群方案。本目录是项目的全部文档，按主题分组如下。

## 入门与使用

- [tutorial_zh.md](tutorial_zh.md) / [tutorial_en.md](tutorial_en.md)：使用文档（编译、快速启动、各组件参数、Jodis 与 HA、Docker、从 2.x 升级）
- [FAQ_zh.md](FAQ_zh.md) / [FAQ_en.md](FAQ_en.md)：常见问题
- [unsupported_cmds.md](unsupported_cmds.md)：Proxy 不支持与半支持的命令清单

## 架构与能力边界

- [architecture_zh.md](architecture_zh.md)：系统架构总览（组件交互、数据与状态、代码地图、已知约束）
- [capability_boundaries_zh.md](capability_boundaries_zh.md)：各能力域的明确边界（支持什么、不做什么）

## Codis Server（内嵌 Redis）

- [redis8_codis_server_zh.md](redis8_codis_server_zh.md)：内嵌 Redis 8 codis-server 扩展（codis-enabled、slot 模型、tag 索引、同步/异步迁移、RDB HTTP 导出、线程模型）
- [rdb_analysis_zh.md](rdb_analysis_zh.md)：Dashboard RDB 分析（本地/远程拉取、RDB 解析器 fork）

## 历史背景

- [redis_change_zh.md](redis_change_zh.md)：项目早期曾基于 Redis 3.2 内核，当前版本已移除 Redis 3 支持

## Proxy 能力

- [proxy_protocol_extensions_zh.md](proxy_protocol_extensions_zh.md)：协议扩展（CLIENT LIST、CLUSTER NODES 兼容、Stream 命令路由、已知路由缺口）
- [proxy_hot_key_cache_zh.md](proxy_hot_key_cache_zh.md)：热 key 缓存
- [proxy_qps_rate_limit_zh.md](proxy_qps_rate_limit_zh.md)：QPS 限流
- [proxy_session_auth_bruteforce_zh.md](proxy_session_auth_bruteforce_zh.md)：AUTH 防暴力破解

## 管理与运维

- [codis_acl_zh.md](codis_acl_zh.md)：Codis 统一 ACL 管理（配置、revision 同步、用户绑定 db、运维恢复手顺）
- [pitr_flashback_zh.md](pitr_flashback_zh.md)：PITR 数据闪回（AOF 时间点恢复、9 步状态机、切换手顺与运维须知）

## 构建与验证

- [build_and_release_zh.md](build_and_release_zh.md)：构建与发布指南（常用命令、Go modules 约定、codis-server 编译排障、多平台发布、测试约定）
- [redis8_validation_zh.md](redis8_validation_zh.md)：Redis 8 迁移验证与切换（验证 gate、Linux 正式验证、切换手顺、Go 组件兼容矩阵）
- [zookeeper_sdk_migration_zh.md](zookeeper_sdk_migration_zh.md)：ZooKeeper Go SDK 迁移说明

## 性能

- [benchmark.md](benchmark.md)：基准测试结果（图表源文件在 [bench1/](bench1/)、[bench2/](bench2/)）
