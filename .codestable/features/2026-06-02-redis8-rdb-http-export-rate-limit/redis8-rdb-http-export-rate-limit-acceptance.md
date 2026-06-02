# redis8-rdb-http-export-rate-limit 验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-06-02
> 关联方案 doc：`.codestable/features/2026-06-02-redis8-rdb-http-export-rate-limit/redis8-rdb-http-export-rate-limit-design.md`
> 关联 checklist：`.codestable/features/2026-06-02-redis8-rdb-http-export-rate-limit/redis8-rdb-http-export-rate-limit-checklist.yaml`

## 1. 接口契约核对

对照方案第 2.1 节名词层逐一核查：

- [x] `codis-rdb-export-rate-limit` 已注册为 Redis size_t config，默认 `0`，`0` 表示 RDB HTTP export 不额外限速。
- [x] 正数语义是单 Redis Server 进程内所有 RDB export 连接共享的 body bytes/sec；实现使用 `redisServer` 级 token bucket，不是 per-client bucket。
- [x] 运行期可通过 `CONFIG SET codis-rdb-export-rate-limit <bytes-per-second>` 调整；apply callback 会调用 `codisRdbExportRateLimitChanged()` clamp/reset bucket 状态，并唤醒 paused export client 重新评估。
- [x] 限速只作用于成功响应的 RDB body streaming；HTTP header、`403`、`404`、`400` 短错误响应不消耗 token。
- [x] 普通 Redis 命令解析、执行和普通 reply 写出不检查、不消耗该 token bucket。
- [x] HTTP path、method、`X-Codis-RDB-Auth`、RDB candidate 校验和禁止 `SAVE` / `BGSAVE` 的规则保持 2026-06-01 基线。

代码落点：

- `extern/redis-8.6.3/src/config.c` 注册配置和 apply callback。
- `extern/redis-8.6.3/src/server.h` / `server.c` 持有并初始化 server 级 bucket 状态。
- `extern/redis-8.6.3/src/codis_rdb_export.c` 实现 refill/consume、CONFIG SET 后 clamp/reset、pause/resume timer 和 body 写出预算控制。
- `extern/redis-8.6.3/redis.conf` / `config/redis.conf` 写入默认配置与边界说明。

## 2. 行为与决策核对

- [x] 方案要求“限速和降低优先级不能影响原始命令执行线程模型”：实现未修改 `aeProcessEvents`、Redis command table、普通 `processCommand` 路径或普通 reply 写出路径。
- [x] RDB export 在 IO thread 命中时仍只标记 `CLIENT_IO_PENDING_RDB_EXPORT` 并交回主线程；auth、open、streaming state 安装和 token bucket 状态变更均在主线程执行。
- [x] token 不足时只移除当前 export client 的 writable handler，并创建 ae time event 延后恢复；time event 只恢复 write handler，不直接读取或写出 RDB body。
- [x] cleanup 会删除 pending resume timer、关闭 fd、释放 state，避免 client 断开后 timer 悬挂。
- [x] 多个 RDB export 连接共享总预算，但实现不承诺连接间严格公平，符合 design 非目标。
- [x] review 中指出的两个缺口已修复：CONFIG SET 调低限速会同步修正运行态 bucket；测试已增加真实耗时下限、并发共享预算和运行期调低限速场景。

## 3. 验收场景核对

- [x] 默认值：`CONFIG GET codis-rdb-export-rate-limit` 返回 `0`。
- [x] 配置修改：`CONFIG SET codis-rdb-export-rate-limit` 接受 `0` 和正数，拒绝非法值。
- [x] 不影响普通命令：限速下载期间 `PING`、`SET`、`GET` 仍及时返回。
- [x] 单连接限速：160KB body 在 64KB/s 下断言宽松耗时下限，避免“绕过 token bucket 也绿灯”。
- [x] 并发共享预算：两个 96KB 并发下载在 64KB/s 下共享总预算，而不是各自获得一份 64KB/s。
- [x] CONFIG SET 调低限速：先用高限速 warmup，再调低到 16KB/s，后续 body 传输不沿用旧 token 余额突发。
- [x] 兼容回归：HTTP header/body、auth、404、symlink/非 RDB 拒绝、`io-threads 2` handoff、非精确 HTTP-like fallback 仍由原测试覆盖。

已执行验证：

- [x] `make codis-server` 通过。
- [x] `cd extern/redis-8.6.3 && ./runtest --single unit/codis_rdb_export` 通过。
- [x] `cd extern/redis-8.6.3 && ./runtest --single unit/protocol` 通过。
- [x] `python3 .codestable/tools/validate-yaml.py --file .codestable/features/2026-06-02-redis8-rdb-http-export-rate-limit/redis8-rdb-http-export-rate-limit-checklist.yaml --yaml-only` 通过。
- [x] `git diff --check` 通过。

前端改动：无，未新增 dashboard/FE 页面，不需要浏览器验证。

## 4. 术语一致性

- `RDB HTTP export`：仍指 Redis 8 Codis Server 本机固定 HTTP 下载口，不等同于 dashboard RDB Analysis。
- `codis-rdb-export-rate-limit`：统一表示 server 级共享 body bytes/sec 限速，`0` 为 unlimited。
- `body token bucket`：只服务 RDB export body streaming，不是 Redis 全局网络写出预算。
- `协作式低优先级`：指 export client 自己暂停 writable handler 并由 timer 恢复，不是 ae 硬优先级队列。
- `ordinary command thread model`：普通命令仍按 Redis 既有主线程执行模型推进，IO thread 仍只做既有网络读写/解析辅助和主线程移交。

## 5. 架构归并

- [x] `.codestable/architecture/ARCHITECTURE.md` 已补充 `codis-rdb-export-rate-limit` 的默认值、运行期配置、共享预算、body-only 作用域和普通命令不接入 token bucket 的边界。
- [x] RDB HTTP export 当前系统地图已补充主线程状态归属、IO-thread handoff 不变、CONFIG SET apply/clamp、暂停 writable handler + ae time event 恢复的流程。
- [x] 已知约束已从“首版不支持限速”更新为“支持 server 级 body 限速，但不承诺 per-client fairness、并发连接上限或硬事件循环优先级”。
- [x] 代码锚点和测试锚点已补充 `server.c`、CONFIG SET apply、token bucket、限速耗时下限、并发共享预算和运行期调低配置测试。
- [x] 相关文档列表已加入本 feature 的 design / acceptance 文档。

## 6. requirement 回写

- [x] `.codestable/requirements/redis-cluster-service.md` 已补充 RDB HTTP export 可通过 `codis-rdb-export-rate-limit` 做 server 级 body 限速，默认 0 不限速。
- [x] 实现进展新增 2026-06-02 rate-limit 记录，说明共享预算、暂停恢复和“不影响普通 Redis 命令执行线程模型”的约束。
- [x] 边界已更新：enabled/auth 重启生效，rate-limit 可运行期调整；限速不作用于普通 Redis 命令或普通 reply，不提供 per-client fairness、并发连接上限、Range、压缩、加密或独立 HTTP 端口。

## 7. roadmap 回写

- [x] 方案 frontmatter 没有 `roadmap` / `roadmap_item` 字段，本 feature 非 roadmap 拆分条目，跳过 roadmap 回写。

## 8. attention.md 候选盘点

- [x] 未发现需要追加到 `.codestable/attention.md` 的新“一句话必读约束”。本次涉及的关键注意项已经在现有 memory 覆盖：CodeStable 工作流先读 `attention.md`，Redis 8 server 源码在 `extern/redis-8.6.3/`，`make codis-server` 会刷新 tracked Redis 配置。

## 9. 遗留

- 非目标仍成立：不实现 per-client fairness、并发连接配额、Range、断点续传、压缩、加密、独立 HTTP 端口、dashboard/proxy API 或 worker thread export。
- 残余风险：大量并发 export client 会带来多个 resume timer；当前实现按 feature scope 接受，后续若线上需要大并发下载，应单独做压测和并发控制设计。
- 运维提醒：`codis-rdb-export-rate-limit` 只能降低 RDB body 写出对主线程事件循环的占用趋势，不能替代 Redis 端口网络隔离、TLS、外层访问控制或下载并发治理。
