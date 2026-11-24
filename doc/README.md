# Codis 文档索引

Codis 是基于代理的高性能 Redis 集群方案。本目录是项目的全部文档，按主题分组如下。

用法：按任务在下面找主题，打开对应文档，再沿文档内的链接到达相关文档或代码锚点。不要在仓库里模糊搜索关键字来定位文档。新增或移动文档时，同步更新本索引或对应子索引。

## 入门与使用

- [tutorial_zh.md](tutorial_zh.md) / [tutorial_en.md](tutorial_en.md)：主线构建、本地快速启动、组件配置、Jodis、HA 与 Docker；中文教程附 Codis 2.x 元数据迁移历史说明
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
- [data_migration_zh.md](data_migration_zh.md)：数据集成控制台 / 数据迁移（内嵌 RedisShake 内核、三种源/目标、任务状态与安全边界）

## 构建与验证

- [build_and_release_zh.md](build_and_release_zh.md)：构建与发布指南（常用命令、Go modules 约定、codis-server 编译排障、多平台发布、测试约定）
- [go_modernization_zh.md](go_modernization_zh.md)：Go 现代化改造计划、行为兼容边界与逐项回归矩阵（含 etcd 移除前的历史约束）
- [redis8_validation_zh.md](redis8_validation_zh.md)：Redis 8 迁移验证与切换（验证 gate、Linux 正式验证、切换手顺、Go 组件兼容矩阵）
- [zookeeper_sdk_migration_zh.md](zookeeper_sdk_migration_zh.md)：ZooKeeper Go SDK 迁移说明

## 真机验证

- [validation/README.md](validation/README.md)：真机验证进度索引（各能力在 Linux 真机上的验证状态、环境记录与遗留项；每个能力的详细验证记录在 `validation/` 下单独开文档）

## 性能

- [benchmark.md](benchmark.md)：2015–2016 年的历史基准测试（图表源文件在 [bench1/](bench1/)、[bench2/](bench2/)）。当前 Redis 8 Linux 性能基线见[真机验证索引](validation/README.md)

## 仓库内的其他 README

不在 `doc/` 下但承载专项知识的文档：

- [../pkg/shake/README.md](../pkg/shake/README.md)：内嵌 RedisShake 内核的来源版本、与上游的差异、为什么每个迁移任务一个进程、升级上游的流程
- [../third_party/rdb/README.md](../third_party/rdb/README.md)：RDB 解析器 fork 相对上游 v1.3.2 的改动（RDB v13、stream/IDMP、KEY_META/SLOT_INFO 跳过）
- [../cmd/fe/assets/README.md](../cmd/fe/assets/README.md)：codis-fe 前端的结构、部署到真实集群、内置 Mock 预览、状态与监控数据
- [../kubernetes/README.md](../kubernetes/README.md)：k8s 上 buildup / cleanup / 扩容 proxy 与 server 的脚本用法
