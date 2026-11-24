# PITR 数据闪回

Dashboard 支持编排单台 Redis 8 Codis Server 的 AOF 时间点截断恢复（PITR / 数据闪回）：把 keyspace 恢复到指定 unix 秒时间点，用于误删、误改、误 `FLUSH` 后的快速回滚。该能力默认关闭，复用 Redis 8 原生的 `aof-timestamp-enabled` 和 `redis-check-aof --truncate-to-timestamp`，不修改 Redis C 源码。

## 配置

dashboard 配置项：

```toml
pitr_enabled = false
pitr_redis_check_aof_bin = "bin/redis-check-aof"
pitr_max_concurrent_jobs = 1
pitr_job_timeout = "30m"
pitr_restart_command = ""
```

Redis 8 Codis Server 的 `redis.conf` 由运维预先开启：

```text
appendonly yes
aof-timestamp-enabled yes
```

配置含义：

- `pitr_enabled`：PITR 总开关，默认 `false`。为 `false` 时所有 `/pitr/*` 接口都返回 `pitr is disabled`，不创建任何 job；修改后需要重启 dashboard。
- `pitr_redis_check_aof_bin`：dashboard 主机上的 `redis-check-aof` 路径，相对路径会转成绝对路径。create 阶段会检查文件存在且可执行，缺失或不可执行时同步拒绝，不会等到停服后才失败。
- `pitr_max_concurrent_jobs`：dashboard 进程内同时运行的 PITR job 上限，小于等于 0 时按 1 处理。不同 server 的 job 可并行。
- `pitr_job_timeout`：restart 轮询和 `INFO` 中 `loading` 等待的单步超时预算，小于等于 0 时按 30 分钟处理。
- `pitr_restart_command`：可选的 `sh -c` 命令，仅用于同机部署时“踢一脚”进程管理器，例如重启 systemd unit。命令失败只记日志，不会中断后续轮询。dashboard 不 fork `redis-server`；无论是否配置该命令，restart 步骤都会轮询 `INFO` 确认 server 上线。
- `appendonly` / `aof-timestamp-enabled`：Redis 侧前提。dashboard 只校验、不自动开启这两个配置，也不会修改 `config/redis.conf` 的默认值。

## 前置条件

| 条件 | 要求 | 不满足时 |
| --- | --- | --- |
| 目标 server | 必须是当前 product 中某个 group 的 master（`Servers[0]`），且已存在于 topom model。 | job 在异步 `validating` 阶段进入 `failed`，此时尚未向目标发送 Redis 命令。 |
| AOF | `appendonly yes`。 | prereq 失败，server 未被触碰。 |
| 时间戳 | `aof-timestamp-enabled yes`，AOF 中存在 `#TS:` 注释。 | prereq 失败，server 未被触碰。 |
| 时间范围 | `truncate_ts` 落在最后一个 AOF segment 的 `#TS:` 范围内。 | feasibility 失败，server 仍在线。 |
| 磁盘 | snapshot 与 AOF 目录同文件系统，可用空间不小于最后一个 segment 文件大小。 | feasibility 失败，server 仍在线。 |
| 工具 | dashboard 主机可执行 `redis-check-aof`，版本应与目标 Redis 一致。 | create 阶段拒绝。 |
| 文件系统 | dashboard 进程对目标 server 的 AOF 目录有直接读写权限。 | 首版不支持跨机文件操作，无法执行。 |
| 进程管理 | Redis 由 systemd/supervisor/k8s 等外部进程管理器接管；生产环境应使用 `Restart=no` 或 `Restart=on-failure`。 | `Restart=always` 会在 `SHUTDOWN NOSAVE` 后立即拉起实例，与停服窗口竞争。 |
| 访问控制 | API 要求 `xauth`，且 `pitr_enabled=true`。 | 请求被拒绝。 |
| 业务影响 | 恢复期间该 group 承载的 slot 写入会失败。 | dashboard 不自动切 slot，需自行评估或提前切流。 |

## 恢复流程

PITR job 只存在于 dashboard 进程内存：UUID v7 id、目标 server、`truncate_ts`、状态机、步骤记录、snapshot 目录和错误信息；不写入 coordinator，dashboard 重启后 job 和锁都会消失。job 管理沿用 [Codis RDB Analysis](rdb_analysis_zh.md) 的进程内任务模式。

入口：

| 入口 | 用法 |
| --- | --- |
| Codis FE | `PITR 数据闪回` 页面，`cmd/fe/assets/pitr.js` 提供 create/list/cancel/remove 和非终态 job 轮询。 |
| codis-admin | 均需 `--dashboard=ADDR`：`--pitr-create --server=ADDR --truncate-ts=TS`、`--pitr-list`、`--pitr-get --job=ID`、`--pitr-cancel --job=ID`、`--pitr-remove --job=ID`。 |
| REST API | `PUT /api/topom/pitr/create/:xauth`、`GET /api/topom/pitr/jobs/:xauth`、`GET /api/topom/pitr/:xauth/:id`、`PUT /api/topom/pitr/cancel/:xauth/:id`、`PUT /api/topom/pitr/remove/:xauth/:id`。 |

9 步状态机（实现位于 `pkg/topom/topom_pitr.go` 和 `pkg/topom/topom_pitr_run.go`）：

| 步骤（state） | 动作 | 关键检查 / 失败后果 |
| --- | --- | --- |
| validate（`validating`） | 对已创建的 job 检查目标是否属于当前 product 且为 master。 | 失败即 `failed`，此时尚未向目标发送 Redis 命令。 |
| prereq（`checking_prereq`） | `CONFIG GET appendonly / aof-timestamp-enabled / dir / appendfilename / appenddirname`。 | 两个开关必须为 `yes`；只读这 5 个键，不读 port/bind。失败时 server 未被触碰。 |
| feasibility（`checking_feasibility`） | 读 MP-AOF manifest 定位最后一个 segment，扫描 `#TS:` 得到 `[min,max]`；best-effort 检查可用空间。 | `truncate_ts` 必须在范围内；失败时 server 仍在线，绝大多数截断错误在停服前被拦截。 |
| shutdown（`shutting_down`） | 发送 `SHUTDOWN NOSAVE`（避免退出时写 RDB 覆盖数据），然后用全新连接 `PING` 探活。 | 只有探活确认不可达才继续；仍能 PING 通则 `failed`，绝不进入 snapshot/truncate。 |
| snapshot（`snapshotting`） | 把 manifest 和最后一个 segment 复制到与 AOF 目录同级的 `.pitr-snapshot-<jobid>/`，并记录 AOF 目录所有文件的 size+mtime。 | 只拷真实写入面，不做全目录 `cp -r`；失败时清理半成品，AOF 未被截断。 |
| truncate（`truncating`） | 执行 `redis-check-aof --truncate-to-timestamp <ts> <manifest>`（不附加 `--fix`）；退出后重扫 AOF 目录做 pre/post stat diff。 | 非 0 退出 → `failed` 并给出从 snapshot 恢复的指引；最后一个文件以外的文件变化 → 归“不确定”类 `failed`（model drift）。 |
| restart（`restarting`） | 可选执行 `pitr_restart_command`，随后始终轮询 `INFO`。 | 不 fork redis-server；轮询超时 → `failed`。 |
| wait-load（`waiting_load`） | 轮询 `INFO` 中的 `loading` 字段，直到它不再为 1。 | 超时 → `failed`；此时 AOF 已被正确截断，server 可能仍在 loading 或未起来。 |
| resync（`resyncing_replicas`） | 对 group 内其余 server 发送 `SLAVEOF <host> <port>`。已指向同一地址的副本可以直接返回 `OK`。 | 副本同步方式和完成时机由 Redis 决定。单个副本命令失败记为 warning，job 仍可进入 `succeeded`。 |

创建请求先同步检查 `xauth`、功能开关、请求字段、工具路径、并发上限和同 server 锁。收到 job ID 后需继续查询状态，目标校验与恢复都在异步步骤执行。

终态为 `succeeded`、`failed`、`cancelled`。步骤报错时记为 `failed`，在步骤之间取消时记为 `cancelled`。成功路径要求目标 master 重新加载截断后的 AOF，最后向副本发送重新指向 master 的命令。

## 行为边界

- 只做单 Redis 8 Codis Server 级恢复，不做跨 group 全局一致性恢复，也不做指定 key 子集恢复。
- 不修改 Redis 8 C 源码，不新增 Redis 命令；复用原生 `aof-timestamp-enabled` 和 `redis-check-aof`。
- 不自动开启 AOF 或 `aof-timestamp-enabled`，不管理 AOF 保留策略、轮转或备份；能恢复到多久取决于运维既有策略。
- 不实现跨机 AOF 文件操作（scp/rsync/NFS/共享存储探测）；首版要求 dashboard 与目标 server 同机或共享文件系统。
- 不编排 proxy 只读切换，不迁移 slot；恢复期间该 group 的写入失败由业务自行承担。
- 不在业务 Redis 协议路径暴露 PITR 命令；只能通过 dashboard API、FE 或 `codis-admin` 触发。
- 不把 job 或截断产物写入 coordinator；job 只在 dashboard 进程内，重启后消失。
- 不 fork `redis-server`，不读 `CONFIG GET port/bind/*file` 拼启动命令；进程生命周期交给外部进程管理器。
- snapshot 只复制 manifest 和最后一个 segment 文件，不复制整个 AOF 目录。
- truncate 失败后不自动重启：AOF 可能损坏，`down + 大声告警` 优于 `up + 悄悄错`。
- 时间精度为秒级；亚秒级恢复不在支持范围。

## 运维须知

**systemd 的 Restart 要求**：`Restart=always` 会在 `SHUTDOWN NOSAVE` 后立刻拉起 Redis，和 PITR 的停服窗口竞争，可能导致 snapshot/truncate 面对一个仍在运行的实例。生产环境必须使用 `Restart=no` 或 `Restart=on-failure`。使用 `on-failure` 时，干净的 `SHUTDOWN` 会让 unit 保持 inactive；PITR 完成后需要由 `pitr_restart_command` 或人工 `systemctl start` 启动实例，并确认 `PING` 和 `INFO loading` 正常。

**取消语义**：

| 取消时机 | job 结果 | server 状态 |
| --- | --- | --- |
| 第一个副作用步骤之前 | `cancelled` | 仍在线，AOF 未动。 |
| truncate 执行中 | `failed`（AOF 状态不确定） | 已停服，保留 snapshot。 |
| 其它有副作用步骤执行中或之后 | 可能是 `cancelled` 或 `failed`，取决于该步骤是否已记录失败 | 可能已停服、处于 loading 或仍在线；必须看 `steps`、`error` 和 `snapshot_dir`。 |
| 已到终态后再 cancel | no-op，返回 `OK` | 不变。 |

cancel 只是停止后续步骤并终止 `redis-check-aof` 子进程的信号，不是回滚。取消后必须查看 `steps`、`error` 和 `snapshot_dir`，确认 AOF 与服务状态，必要时从 snapshot 回退后再启动；不要假设 cancel 后 server 一定还在线。

**失败处置**：

| 失败阶段 | AOF 状态 | server 状态 | 处置 |
| --- | --- | --- | --- |
| validate / prereq / feasibility | 未改 | 未被 PITR 触碰（通常仍在线） | 修正参数、前提或磁盘后重新创建；先 Remove 旧 job 释放锁。 |
| snapshot | 未改 | 已停服 | 直接启动 Redis；确认恢复后 Remove 旧 job。 |
| truncate | 可能已被 `ftruncate` 改坏 | 已停服 | 按 job error 的指引把 snapshot 中的 manifest 和最后 segment 拷回 AOF 目录，再启动；不要直接重启加载不确定的 AOF。 |
| restart / wait-load | 已正确截断 | 可能 loading 或未启动 | 手动确认并启动；AOF 数据正确，可清理 job。 |

**锁与 Remove**：同一 server 同时只允许一个 PITR job。per-server lock 不会在 job 到达终态时释放，而是持续到运维显式执行 `--pitr-remove --job=ID` 或 `PUT /api/topom/pitr/remove/:xauth/:id`。Remove 只接受终态 job；running job 必须先 cancel。Remove 会删除该 job 的 snapshot 目录并释放锁；清理失败只记 warning，不阻塞删除。忘记 Remove 会导致该 server 无法再创建新 job，直到 dashboard 重启清空进程内 registry。

**监控要点**：轮询 `GET /api/topom/pitr/jobs/:xauth` 或 FE 任务列表，关注 `state`、`steps[].status`、`steps[].error`、`snapshot_dir`、`restart_kicked`。`state=failed`、`aof model drift detected`、`snapshot` 相关错误需要立即告警。dashboard 日志会记录 server、ts、bin 路径、退出码、snapshot 目录和耗时，不记录 AOF 内容或 `requirepass`。

**副本一致性**：job `succeeded` 表示 master 已完成截断和加载。恢复流量前，检查副本是否指向预期 master、`master_link_status` 是否为 `up`、`master_sync_in_progress` 是否为 `0`，并核对恢复点附近的业务数据。

**磁盘与快照**：snapshot 目录与 AOF 目录同级、同文件系统，只占最后一个 segment 大小的额外空间。job 成功后 snapshot 会保留供审计或回退，直到 Remove；不要手工删除仍在使用的 snapshot。

## 已知限制

- **S16：副本重同步未闭环**。`stepResync` 发送 `SLAVEOF` 后即进入收尾，同步进度与数据一致性需按运维手顺核对。
- **S17：truncate 中取消的语义未定稿**。cancel 落在 truncate 期间时，当前结果是 `failed`（server down、snapshot 保留），而设计语义期望 `cancelled`；当前实现遵循“AOF 状态不确定绝不静默标 cancelled”。该语义仍需定稿。
- **cancel 跨 step 的终态**。cancel 在 step 之间会终止为 `cancelled`；如果 cancel 落在会响应 ctx 的步骤中（如 truncate/restart/wait-load），该步骤以错误结束，job 为 `failed`；如果当前步骤正常完成，`runSteps` 会在下一步前以 `cancelled` 终止。如果 API 长时间停在某个非终态且 `steps` 中没有 failed，应按状态机未收尾处理。cancel 不是回滚。
- **`--fix` 与 `--truncate-to-timestamp` 互斥**。Redis 8.6.3 的 `redis-check-aof` 不能同时接受这两个参数；同时传 `--fix` 会打印 usage 并退出，不会截断。实现只传 `--truncate-to-timestamp`，运维手工排障时也不要把两者合并。
- **`SHUTDOWN` 的返回值不可靠**。从客户端视角，`SHUTDOWN` 返回的 error 既可能是 server 拒绝命令（仍在线），也可能是 server 退出导致连接断开（已停）。实现忽略 error 类型，统一用新连接 `PING` 探活；只要还能 PING 通就判定 shutdown 失败，绝不 snapshot/truncate 一个仍在写的 AOF。
- **snapshot 模型依赖 `redis-check-aof` 的写入面**。当前只快照 manifest 和最后一个 segment，前提是工具只对最后一个文件就地 `ftruncate`、不改更早文件、不重写 manifest。该前提已在 vendored Redis 8.6.3 上核对；升级 Redis 后需要重新核对。pre/post stat diff 会在非最后文件发生变化时把 job 判为“不确定”失败并告警。
- **job 不持久化**。dashboard 重启会丢失 job 历史、步骤记录、per-server lock 和轮询状态；已停服的 server 不会因此自动恢复。高危操作期间不要重启 dashboard。
- **snapshot 清理是 best-effort**。Remove 删除 snapshot 目录失败时只记录 warning，需要人工清理残留目录。
- **单 server、秒级、依赖外部进程管理**。不做跨 group 全局恢复、指定 key 恢复、跨机文件操作、自动 AOF 开启、AOF 轮转/备份管理或 proxy 只读切换；恢复窗口内该 group 的写入失败不会被自动兜底。
