---
doc_type: feature-note
feature: 2026-05-17-redis8-validation-cutover
status: draft
summary: Redis 8 Linux 正式验证交接清单
---

# Redis 8 Linux validation handoff

判断：正式 cutover gate 必须在远程 Linux 上完成。本地 Mac 结果只能证明验证编排和非性能协议路径，不得替代 Linux 性能、fork/RDB、复制、部署包装结论。

## 环境字段

正式报告必须记录：

- Codis commit、branch、dirty diff 摘要。
- OS、kernel、CPU 型号和核心数、内存、磁盘类型、文件系统。
- Go version、compiler flags、`make` 命令、`bin/version`。
- Redis 8 默认 server 路径、Redis 3 fallback 路径。
- Codis dashboard/proxy/server 关键配置 diff。
- 测试端口、数据目录、日志目录、清理结果。

## Linux 必跑命令

功能 gate：

```bash
make build-all
make gotest
cd extern/redis-8.6.3
./runtest --single unit/codis --single unit/codis_migration --single unit/codis_slotsrestore --single unit/codis_async_migration
cd -
python3 scripts/redis8_local_validation.py --output .codestable/features/2026-05-17-redis8-validation-cutover/redis8-linux-evidence.json
```

性能 gate 只在 Linux 执行：

```bash
bin/redis-benchmark -h 127.0.0.1 -p <redis8-direct-port> -t get,set -n <N> -c <C> -P <P>
bin/redis-benchmark -h 127.0.0.1 -p <codis-proxy-port> -t get,set -n <N> -c <C> -P <P>
bin/redis-benchmark-redis3 -h 127.0.0.1 -p <redis3-fallback-port> -t get,set -n <N> -c <C> -P <P>
```

部署包装 gate：

```bash
make docker
docker run --rm <codis-image> <smoke-command>
```

如果 Docker daemon 不可用，只能记录 `blocked: docker daemon unavailable`，不能写成已验证。

## 必须记录的结果

- direct Redis 8、proxy + Redis 8、Redis 3 fallback 的 throughput/latency 原始输出和测试参数。
- proxy 路径相对 direct Redis 8 的开销，不用 Mac 数字补位。
- Redis 8 fork/RDB 行为：`BGSAVE` 成功、日志、耗时、RDB 文件大小。
- replication 行为：master/replica link、初次同步、断连恢复。
- migration 行为：迁移期间持续写读、slot action 清零、失败重试行为。
- Docker/部署包装行为：镜像构建、启动命令、默认配置、日志路径。

## Cutover 阻塞条件

- Linux `make gotest` 或 Redis Tcl Codis suite 失败且无明确环境解释。
- e2e `semi-async` 或 `sync` 任一路径失败。
- 迁移失败造成源端 key 静默丢失。
- Redis 8 direct 或 proxy 路径性能明显低于可接受阈值且无容量预案。
- fork/RDB、replication、Docker/部署包装任一生产必需路径未验证。
