---
doc_type: feature-note
feature: 2026-05-17-redis8-validation-cutover
status: draft
summary: Redis 8 cutover runbook 草案
---

# Redis 8 cutover runbook draft

判断：runbook 只能作为 Linux 正式验证前的草案。最终 cutover 结论以后续 `redis8-linux-validation-cutover` 的证据为准。

## 1. Preflight

- 确认当前 Codis commit、默认 `bin/codis-server` 指向 Redis 8，`bin/codis-server-redis3` fallback 仍可用。
- 完成 Linux 功能 gate：`make build-all`、`make gotest`、Redis 8 Codis Tcl suite、本地验证 runner。
- 完成 Linux 性能 gate：direct Redis 8、proxy + Redis 8、Redis 3 fallback 对照 benchmark。
- 完成 fork/RDB、replication、migration under write、Docker/部署包装检查。
- 备份 coordinator 元数据、Redis 数据目录、关键配置和操作窗口信息。
- 明确 Redis 8 RDB/AOF 不保证 Redis 3 可读，不能把“换回 Redis 3 二进制”写成通用回滚。

## 2. Canary

- 新建少量 Redis 8 group，不直接改 coordinator 元数据。
- 所有 group、server、slot、proxy 操作只经 dashboard/topom 或 `codis-admin`。
- 选择低风险 slot 范围迁移到 Redis 8 group。
- 验证 proxy 写读、dashboard slot action、Redis server `SLOTSINFO`/key placement、业务监控。
- 观察窗口内不扩大范围；异常先冻结新迁移。

## 3. Ramp-up

- 按 group 或 slot range 分批扩大 Redis 8 承载范围。
- 每批记录迁移前 slot owner、迁移命令、迁移耗时、失败重试、读写错误率。
- 迁移期间持续运行读写探针，确认 proxy 不返回异常的 `MOVED`/`ASK`/backend error。
- 每批结束后等待监控稳定，再进入下一批。

## 4. Full cutover

- 全量 slot 迁移到 Redis 8 group 后，确认 dashboard/topom action 清零。
- 确认 proxy 列表、group 状态、replication 状态、RDB/fork 最近一次结果。
- 保留 Redis 3 fallback 二进制和旧 group 数据到既定观察窗口结束。
- 只在 Linux 正式性能和稳定性证据通过后，才标记 cutover gate 通过。

## 5. Rollback

- Canary 阶段：如果旧 Redis 3 group 仍是权威数据源，可通过 dashboard/topom 把 canary slot 迁回。
- Ramp-up 阶段：如果 Redis 8 已承接写入，优先使用已经验证过的反向 fragment 迁移；未验证方向不能临场假设可用。
- Full cutover 后：如果 Redis 8 已成为唯一写入源，回滚只能依赖已验证反向迁移或备份恢复。
- 禁止直接编辑 filesystem/zookeeper/etcd coordinator 元数据绕过 topom。
- 禁止用 Redis 8 的 RDB/AOF 直接启动 Redis 3 作为回滚手段，除非已有同版本数据文件兼容性验证。
