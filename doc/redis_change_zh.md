# Redis 3 内核历史背景（已移除）

Codis 早期版本的 `codis-server` 基于 Redis 3.2.x 内核，并在 Redis 3 上增加过 SLOTS 系列命令，用于 Codis 的 1024 slot 管理和数据迁移。这一段实现仅作为项目历史背景保留。

当前主线只内置 Redis 8（`extern/redis-8.6.3`）。Redis 3 源码、构建目标和兼容性测试已经从仓库移除；不要使用本文的历史信息进行构建、部署或迁移。当前 Codis Server 的配置、slot 模型和迁移命令见 [redis8_codis_server_zh.md](redis8_codis_server_zh.md)。
