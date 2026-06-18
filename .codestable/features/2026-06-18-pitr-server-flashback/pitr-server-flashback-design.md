---
doc_type: feature-design
feature: 2026-06-18-pitr-server-flashback
requirement: redis-cluster-service
status: approved
summary: 让 dashboard 编排单台 Redis 8 Codis Server 的 AOF point-in-time 截断恢复，复用 Redis 8 原生 aof-timestamp-enabled + redis-check-aof --truncate-to-timestamp，FE/admin 提供触发入口
tags: [pitr, flashback, aof, dashboard, redis8, recovery]
---

# pitr-server-flashback design

## 0. 术语约定

- **PITR（Point-In-Time Recovery）/ 数据闪回**：本 feature 指把一台 Redis 8 Codis Server 的 keyspace 恢复到某个秒级时间点。它复用 Redis 8 原生 AOF 时间戳 + `redis-check-aof --truncate-to-timestamp` 离线截断能力，由 dashboard 编排"停服 → 截断 → 重启 → 副本重同步"流程。它**不是**跨 group 全局一致性恢复，也**不是**指定 key 子集恢复。
- **PITR job / 恢复任务**：dashboard 进程内的一次单 server 恢复任务对象，仿照 `RDBAnalysisJob` 的 manager + job 模式，带 UUID v7 id、状态机、并发上限、进程内 registry。它只活在 dashboard 进程内存，不进 coordinator。
- **目标 server / target**：用户指定的、要被恢复的 Redis 8 Codis Server。必须精确匹配当前 topom model 中某个 group 的 server addr；首版只允许对该 server 所在 group 的 master 执行。
- **截断时间点 / truncate-ts**：用户指定的 unix 秒时间戳，AOF 将被截断到该时间点之前最后一条带 `#TS:` 注释的命令。
- **AOF timestamp 注释**：Redis 8 `aof-timestamp-enabled yes` 开启后，`feedAppendOnlyFile` / `rewriteAppendOnlyFileRio` 在每条命令前写入的 `#TS:<unix秒>` 行。实现锚点 `extern/redis-8.6.3/src/aof.c:1415` 和 `extern/redis-8.6.3/src/aof.c:2489`。
- **redis-check-aof truncate**：Redis 8 自带的 `redis-check-aof --truncate-to-timestamp <TS> --fix <manifest|file>` 离线截断工具。它扫描 `#TS:` 注释，把超过 TS 的部分截断并就地改文件；实现锚点 `extern/redis-8.6.3/src/redis-check-aof.c:185` 和 `:543`。
- **AOF manifest**：Redis 8 MP-AOF（multi-part AOF）清单文件，描述 base + incr 文件序列。实现锚点 `extern/redis-8.6.3/src/aof.c:65` 和 `aofLoadManifestFromFile`。

防冲突结论：

- grep 全仓库无 `PITR` / `flashback` / `闪回` / `point-in-time` 既有概念，术语安全。
- "恢复"在 Codis 现有语境只指 `Redis 8 异步迁移` 里的 `SLOTSRESTORE-ASYNC` 和 slot 迁移，不冲突；本 feature 的"恢复"一律用"闪回 / PITR / flashback"表述，避免歧义。
- `aof-timestamp-enabled` 和 `redis-check-aof --truncate-to-timestamp` 是 Redis 8 **已有**能力，本 feature **不改**其 C 源码，只在 Go 侧编排调用。

## 1. 决策与约束

### 需求摘要

**做什么**：值班人员通过 dashboard/FE 或 `codis-admin`，指定当前 product 内一台 Redis 8 Codis Server 和一个秒级时间点，dashboard 编排该 server 的 keyspace 恢复到该时间点。

**为谁**：平台维护者 / 值班人员，用于误操作（误删、误改、误 FLUSH）后的快速回滚。

**成功标准**：

- dashboard 新增 PITR job manager + REST API（`xauth` 保护），FE 加触发入口，`codis-admin` 加 flag，三者走同一条 dashboard API。
- 合法请求能对一个满足前提（见下）的 server 执行：校验 → 优雅停服 → AOF 截断到 TS → 重启 → 等待 loading 完成 → 通知副本重做全量同步，全过程返回 job 进度，失败时留下明确错误并尽力回滚到可服务状态。
- 目标 server 的 `appendonly` 和 `aof-timestamp-enabled` 必须已开启；未开启时返回明确错误并指引运维，不自动改配置、不偷偷开 AOF。
- `truncate-ts` 必须落在当前 AOF manifest 覆盖的时间范围内；范围外或 server 当前不在该 AOF 的时间轴上时拒绝执行。
- 截断后的 AOF 能被 Redis 8 正常 load 并对外服务；副本能通过全量 resync 跟上恢复后的 master。
- 默认关闭（dashboard 配置 `pitr_enabled = false`），不影响现有任何行为。

**假设**：

- 目标 server 是 Redis 8 Codis Server（Redis 3 fallback 不支持，AOF timestamp 是 Redis 8 能力）。
- 目标 server 已在 Redis 运维流程中开启 `appendonly yes` + `aof-timestamp-enabled yes`，并有覆盖目标时间点的 AOF 文件（运维既有保留策略）。
- dashboard 进程所在机器上**已安装** `redis-check-aof` 二进制（默认随 `make codis-server` 产出到 `bin/redis-check-aof`），或可配置 `pitr_redis_check_aof_bin` 指向绝对路径。
- dashboard 网络上能访问目标 server 的 Redis 端口（发 `SHUTDOWN`/`CONFIG`/探测命令），且 dashboard 进程对目标 server 的 AOF 目录有**直接文件系统读写权限**（首版假设 dashboard 与目标 server 同机或共享存储，跨机文件操作见"明确不做"）。
- 同一 group 内副本在 master 恢复后会被 `SetMaster` 触发全量 resync；Codis 现有 HA/sentinel 不在恢复期间抢占 master（首版靠运维在 dashboard 标记 group 维护态 + 可选 sentinel 暂停）。

**明确不做**：

- **不做跨 group 全局一致性恢复**。首版只做单 server。全局恢复需要 proxy 写入暂停、跨 group 时间对齐、原子性保证，留后续 feature。
- **不做指定 key 子集恢复**。AOF 截断是全实例级的；key 级恢复需要"AOF 重放到 T0 后提取指定 key 再 SET 回当前实例"的额外工具链，留后续。
- **不改 Redis 8 C 源码**。复用原生 `aof-timestamp-enabled` + `redis-check-aof --truncate-to-timestamp`，不新增 Redis 命令。
- **不自动开启 AOF / aof-timestamp-enabled**。前提由运维在 Redis 配置层保证，dashboard 只校验不修改这两个危险配置。
- **不做 AOF 保留策略 / 轮转 / 备份管理**。能恢复到多久完全取决于运维既有 AOF rewrite + 备份策略。
- **不实现跨机 AOF 文件操作**（scp/rsync/NFS/共享存储探测）。首版假设 dashboard 直接能 `open` 目标 server 的 AOF 目录；跨机场景留后续。
- **不编排 proxy 层的只读切换**。单 server 恢复期间，对应 slot 的写入会失败（master 重启），首版不替业务做透明切换；运维需自行评估该 group 的写流量影响或提前切流。
- **不在业务 Redis 协议路径暴露任何 PITR 命令**。普通客户端不能通过 proxy 触发闪回。
- **不把 PITR job / 截断产物写入 coordinator**。job 只在 dashboard 进程内存，重启消失（与 RDB Analysis 一致）。
- **不修改 `config/redis.conf` 默认值**。`appendonly no` / `aof-timestamp-enabled no` 保持默认，由运维显式开启。
- **不 fork redis-server**。dashboard 不直接拉起 Redis 进程；重启靠外部进程管理器（systemd/supervisor/k8s）拉起 + dashboard 轮询 `INFO loading` 确认。`pitr_restart_command` 只是同机部署时"踢一脚"的可选便利项，踢完照样轮询。不为重启读 `CONFIG GET port/bind/*file`（拿不齐且无意义）。
- **不对整个 AOF 目录做快照**。只快照处于风险面的"最后一个 segment 文件"（`redis-check-aof` 只对该文件就地 `ftruncate`，不改更早文件、不重写 manifest）；全目录 `cp -r` 会在恢复窗口内把磁盘翻倍打满。

### 复杂度档位

走"受控运维编排任务"档位，偏离默认项：

- Robustness = L3：输入是用户指定的时间戳 + server addr，编排涉及停服、改文件、重启；必须校验 xauth、server allowlist、AOF 前提、TS 范围、子进程退出码、loading 状态、副本重同步；失败要可观察、可重试、尽力回退到可服务。
- Security = validated：dashboard API 受 `xauth` 保护；目标 server 校验复用 topom model；截断操作只动目标 server 的 AOF 文件，不接受任意路径；不把凭据写入 job/coordinator/日志。
- Performance = interactive：恢复是低频重操作，单次秒~分钟级；不需要高并发，但有并发上限和超时。
- Compatibility = additive：默认关闭，不用该能力时现有行为零变化；不碰业务协议路径。
- Observability = logged：job 状态机每步带时间戳和错误；stats 暴露当前/历史 job 摘要；不打印 AOF 内容或凭据。

### 关键决策

1. **复用 Redis 8 原生 `aof-timestamp-enabled` + `redis-check-aof --truncate-to-timestamp`，不改 C 源码**。
   - 依据：Redis 8 已原生支持 AOF 时间戳注释（`aof.c:1415/2489`）和离线截断（`redis-check-aof.c:185/543`）。重新发明一套日志/截断机制会和 Redis 既有的 AOF/复制/RDB 路径冲突，且无法复用 Redis 自身的 load 校验。
   - 名词层影响：不引入新的"操作日志/undo log"实体；PITR job 是纯编排对象，不持有数据。
   - 编排层影响：编排流程里必须包含"调用外部 `redis-check-aof` 子进程"这一步，而不是内部 Go 库调用。

2. **编排放在 dashboard/topom，仿 `RDBAnalysisManager` 的 manager + job 模式**。
   - 依据：已有 `RDBAnalysisManager` 提供了"进程内异步 job + UUID v7 id + 并发上限 + workspace + cleanup + REST API + FE 入口 + admin flag"的完整范式，PITR 是同类型的受控运维任务。
   - 名词层影响：新增 `PitrManager` + `PitrJob` 类型，不扩 `RDBAnalysisManager`（职责不同：一个是分析只读，一个是写改重启）。
   - 编排层影响：主流程是状态机（validate → prereq → pre-truncate-feasibility → shutdown → snapshot → truncate → restart → wait-load → resync-replicas → done/failed），不是 RDB Analysis 那种线性解析。

3. **单 server 级，不做跨 group 协调**。
   - 依据：跨 group 全局恢复需要 proxy 写入暂停 + 跨 group 时间对齐 + 原子性，复杂度档位跳升一档；单 server 已能覆盖"误操作回滚单实例"这一最高频场景，且每个 group 的 AOF 时间轴本就独立，无法严格全局一致。
   - 名词层影响：`PitrJob` 只绑定单个 server addr，不绑定时间点集合或 slot 集合。
   - 编排层影响：无需 proxy 配合（不新增 proxy admin API），不进 coordinator，不影响其他 group。

4. **前提校验只读不写：未开 AOF 直接拒绝**。
   - 依据：`appendonly yes` + `aof-timestamp-enabled yes` 是 Redis 危险/不可逆配置（开启 appendonly 会触发 AOF rewrite，aof-timestamp-enabled 改变 AOF 格式）。dashboard 自动开这两个等于在用户不知情下改变持久化行为和磁盘占用。
   - 名词层影响：`PitrJob` 有显式 `prerequisites_checked` 字段；校验失败是独立 job 状态。
   - 编排层影响：流程第一步是只读 `CONFIG GET appendonly / aof-timestamp-enabled`，不满足直接 terminate。

5. **目标必须是当前 product 的 group server，且首版只允许该 group 的 master**。
   - 依据：和 Remote RDB fetch 一致——dashboard 已持有 topom model，允许任意 addr 会把管理 API 变 SSRF/任意主机操作通道。只允许 master 是因为副本恢复语义复杂（副本截断后会与 master 分裂），首版聚焦 master。
   - 名词层影响：`PitrJob` 校验阶段产出 `{gid, server_addr, is_master}`。
   - 编排层影响：校验阶段遍历当前 topom model，非 master 直接拒绝。

6. **截断用外部 `redis-check-aof` 子进程，不内嵌解析**。
   - 依据：MP-AOF manifest + base/incr 多文件解析逻辑复杂且 Redis 版本相关；`redis-check-aof` 是 Redis 自带、与 server 同源同版本的权威工具。内嵌解析会有"我方解析和 Redis load 不一致"风险。
   - 名词层影响：`PitrManager` 配置项含 `pitr_redis_check_aof_bin`（默认 `bin/redis-check-aof`）。
   - 编排层影响：truncate 步骤是 `exec.Command(bin, "--truncate-to-timestamp", ts, "--fix", manifestPath)`，检查 stdout/exit code。

7. **不 fork redis-server，重启走"等外部拉起 + 轮询 INFO loading"为默认主路径，`pitr_restart_command` 只是同机便利项**。
   - 依据：进程监管（crash 自动重启、cgroup、ulimit、日志重定向、降权）是 systemd/supervisor/k8s 的职责，dashboard fork 出来的 redis 会变成 dashboard 的子进程——dashboard 自己重启/崩溃，这台生产 redis 跟着死或变孤儿。PITR 是低频高危人发操作，full automation 的价值点是"正确和安全"，不是省掉进程管理那一步。fork 不买任何东西。
   - 副作用：既然不 fork，就**不需要**为重启读 `CONFIG GET port/bind/*file`——CONFIG GET 也拿不齐（unixsocket/logfile/daemonize/二进制路径/env/ulimit），靠它拼启动命令是脆的、注定不全的死代码。prereq 只保留定位 AOF 必需的 5 个配置。
   - 名词层影响：`PitrJob` 不持有"启动命令"字段；restart 步骤只产出"是否已 kick + 轮询结果"。
   - 编排层影响：restart 步骤 = 可选 `pitr_restart_command`（`sh -c <toml>`，非 API 传入，记日志）踢一脚，然后**无论是否 kick 都轮询** `INFO` 的 `loading` 直到 server 上线。`pitr_restart_command` 未配则纯靠外部进程管理器拉起 + 轮询。

8. **truncate 前快照 manifest + 最后一个 AOF segment 文件，并用 pre/post stat diff 兜底模型漂移**。
   - 依据：`redis-check-aof --truncate-to-timestamp` 是就地 `ftruncate`（`redis-check-aof.c:207/:319`），且只动**manifest 列表里的最后一个文件**、不重写更早文件、不重写 manifest。
   - **"truncate 不改 manifest" 已源码验证（非待测）**：`checkMultiPartAof`（`redis-check-aof.c:466-507`）对 manifest 全程只读（`aofLoadManifestFromFile` → 遍历 → `aofManifestFree`），从不以写模式打开；整个工具唯一写操作是作用于数据文件的 `ftruncate`。逻辑上也必然——truncate 只把最后一个文件截到某 offset，filename/seq/type 全不变，而 manifest 恰好只存这三样不存 size，没有重写的理由。manifest 仍纳入快照属近零成本的自包含保险（让快照能作为一个单元整体还原），不是必需。
   - **"最后一个文件"定义**：`checkMultiPartAof` 里 `last_file = (++aof_num == total_num)`，只有 last_file 才允许 truncate（`:199-205` 非最后文件直接 exit）。正常是小 incr，退化场景（只有 base 无 incr）它就是 base，targeted-copy 逻辑天然拷对那个文件，无需特判。
   - **stat diff 兜底（取代全目录拷贝的防御深度）**：truncate 前 stat（只 stat 不拷）AOF 目录每个文件的 size+mtime，truncate 后复查。最后一个文件以外的任何文件变了 → 说明 redis-check-aof 行为偏离我们的模型（版本漂移/bug）→ 直接归到"不确定"失败类、大声告警。这把"全目录拷贝想买的那点保险"用近零成本拿回来，还能主动发现模型失效。
   - **版本漂移是受控事件**：`extern/redis-8.6.3` 是 in-tree vendored，版本只在团队主动 bump 时变；stat diff + bump 时重新核验 `checkMultiPartAof` 两道防线下，targeted copy 残余风险可接受。
   - 名词层影响：`PitrJob` 增 `SnapshotDir` / `LastSegmentFile` / `LastSegmentTsRange` / `PreStatSnapshot`（map[filename]{size,mtime}）字段；snapshot dir 由 AOF 目录同级派生（`<aofdir>.pitr-snapshot-<jobid>/`），不新增 config。
   - 编排层影响：流程在 SHUTDOWN 与 truncate 之间插入 snapshot 步骤（拷 manifest + 最后文件）+ 在 truncate 前后插入 stat diff 校验；snapshot 自身失败（如磁盘满）→ job failed，但此时 AOF 未被任何 truncate 触碰、干净如初；stat diff 检测到非最后文件变化 → job failed 归"不确定"类、大声告警、保留 snapshot 供运维处理。

9. **"TS 落在最后一个 AOF 文件"的可行性校验前置到 SHUTDOWN 之前**。
   - 依据：`redis-check-aof` 的绝大多数失败（TS 不在最后文件 `redis-check-aof.c:200`、TS 早于全部记录、`#TS:` 注释非法）都在 `ftruncate` 之前 exit，AOF 根本没动。把这些拦截在停服之前，根本到不了危险分支；真正走到"停服后 truncate 才失败"的，就只剩磁盘 I/O 这类真·不确定，这时"停 + 人工介入 + 有快照可回退"才名正言顺。
   - 名词层影响：`PitrJob` 的校验阶段产物含"最后一个 segment 文件名 + 其 `#TS:` 时间范围"。
   - 编排层影响：新增 pre-truncate feasibility 步骤（server 仍在 UP 时执行）：Go 侧读 manifest 定位最后 segment，扫描其 `#TS:` 注释（行首 `#TS:<unix秒>`），确认截断点落在该文件内且 ts 在范围内；不满足直接 failed，server 未被触碰。

### 前置依赖

无。评估的目标文件（`pkg/topom/topom.go`、`pkg/topom/config.go`、`pkg/topom/topom_api.go`、`cmd/admin/dashboard.go`、`cmd/fe/assets/*`）无结构性问题需要先解决。

## 2. 名词与编排

### 2.1 名词层

#### 现状

- `pkg/topom/topom.go:28-78` `Topom` 持有 `store/cache/action/stats/ha/aclSync/rdbAnalysis` 等子状态，每个子状态是独立 manager。
- `pkg/topom/topom_rdb_analysis.go:36-95` `RDBAnalysisManager` + `RDBAnalysisJob` 是 manager+job 范式：UUID v7 id、并发上限、workspace、进程内 registry、cleanup、snapshot。
- `pkg/topom/config.go:92-100` dashboard `Config` 含 `RDBAnalysis*` 系列 toml 字段。
- `pkg/models/group.go:8-31` `Group` / `GroupServer` 是 group 模型；master = `Servers[0]`。
- `pkg/utils/redis/client.go:139` `Client.Do` 可发任意 Redis 命令；`:258` `Client.Shutdown()`；已有 `InfoFull`/`SetMaster`。
- Redis 8 侧 `extern/redis-8.6.3/src/aof.c:1415/2489` 时间戳注释、`extern/redis-8.6.3/src/redis-check-aof.c:185/543` 截断工具，均**已存在，不改**。

#### 变化

**新增**（Go 侧）：

- `pkg/topom/topom_pitr.go`：`PitrManager`（registry、并发上限、bin 路径、snapshot）+ `PitrJob`（id / target / ts / state / created_at / updated_at / steps / error）。
- `pkg/topom/topom_pitr_api.go`：dashboard REST handler。
- `pkg/models/pitr.go`：可选——首版 job 不进 coordinator，此文件**不创建**，job 类型直接放 topom 包内。留这条注释提醒未来若要把 job 持久化再建 model。
- `cmd/fe/assets/pitr.js` + `cmd/fe/assets/index.html` 增加 PITR 区域。
- `cmd/admin/pitr.go` + `cmd/admin/dashboard.go` 增 `--pitr` flag。

**修改**（Go 侧）：

- `pkg/topom/topom.go`：`Topom` struct 增 `pitr *PitrManager` 字段；`setup` / `Close` 接入生命周期。
- `pkg/topom/config.go`：`Config` 增 `PitrEnabled bool` / `PitrRedisCheckAofBin string` / `PitrMaxConcurrentJobs int` / `PitrJobTimeout timesize.Duration` 字段 + toml 默认值 + 校验。
- `pkg/topom/topom_api.go`：注册 `/api/topom/pitr/:xauth` 路由组。
- `cmd/fe/assets/index.html`：加 PITR 区域入口。
- `cmd/admin/main.go` / `cmd/admin/dashboard.go`：加 `--pitr-create` / `--pitr-get` / `--pitr-cancel` flag 分发。

**不改**（Redis 8 C 侧）：`extern/redis-8.6.3/src/aof.c`、`redis-check-aof.c`、`config.c` 的 `aof-timestamp-enabled` 全部保持原样。

#### 接口示例

**PITR job 状态机**（新增类型，来源：`pkg/topom/topom_pitr.go`）：

```
state ∈ {pending, validating, checking_prereq, checking_feasibility,
         shutting_down, snapshotting, truncating, restarting,
         waiting_load, resyncing_replicas, succeeded, failed, cancelled}

PitrJob {
  ID              string    // uuid.NewV7()
  ProductName     string
  GroupID         int
  ServerAddr      string    // 校验后确认为 master
  TruncateTs      int64     // unix 秒，用户指定
  AofDir          string    // 从 CONFIG GET dir 取得（定位 AOF + snapshot 同级目录）
  AofManifest     string    // 从 CONFIG GET appendfilename/appenddirname 推导
  LastSegmentFile string    // feasibility 阶段从 manifest 定位（manifest 列表最后一个文件，base 或 last incr）
  LastSegmentTsRange [2]int64 // feasibility 阶段扫描该文件 #TS: 得到 [min,max]
  SnapshotDir     string    // AOF 目录同级派生（如 <aofdir>.pitr-snapshot-<jobid>/），不新增 config
  PreStatSnapshot map[string]FileStat // truncate 前 AOF 目录每个文件的 {size,mtime}；truncate 后复查检测模型漂移
  RestartKicked   bool      // 是否执行了 pitr_restart_command（未配则为 false）
  State           string
  Steps           []PitrStep // 每步 {name, status, started_at, finished_at, error}
  CreatedAt, UpdatedAt time.Time
}
```

**Dashboard REST API**（来源：`pkg/topom/topom_pitr_api.go`，仿 `topom_rdb_analysis_api.go`；写操作一律 PUT，对齐项目 RDB Analysis `ApiPutJson` 约定）：

```
PUT /api/topom/pitr/create/:xauth
  body: {"server_addr":"10.0.0.5:6379","truncate_ts":1716000000}
  → 200 {"job_id":"0193..."}（uuid v7）
  错误路径（沿用项目 rpc 800 错误码，非字面 HTTP 400/403/409/503）:
    - xauth 错 → 800 "invalid xauth"
    - pitr_enabled=false → 800 "pitr is disabled"（所有 /pitr/* handler 统一拒绝，不止 create）
    - server 不在 product / 不是 master → 800 "server ... is not the master of group ..."
    - appendonly/aof-timestamp-enabled 未开 → 800 "prerequisites not met: appendonly=..."
    - 同 server 已有 job（running OR terminal 未 Remove）→ 800 "server ... is locked by pitr job ...; Remove that job first"
    - redis-check-aof bin 缺失/不可执行 → 800 "... binary not found or not executable ..."
    - 并发超上限 → 800 "too many running pitr jobs"

GET /api/topom/pitr/jobs/:xauth
  → 200 [{"id":...,"server_addr":...,"state":...,"steps":[...],"error":...}]
  只返回摘要，不含 AOF 内容/凭据

GET /api/topom/pitr/:xauth/:id
  → 200 单个 job snapshot

PUT /api/topom/pitr/cancel/:xauth/:id
  → 200 "OK"（terminal job cancel 是 no-op）

PUT /api/topom/pitr/remove/:xauth/:id
  → 200 "OK"（仅允许 terminal job；running job 返回 800 "... Cancel it first"）
```

**HTTP 状态码约定（review 标记的 silent area，现裁定）**：沿用项目既有 rpc 约定（错误统一走 `rpc.ApiResponseError`，业务错误为自定义 800 而非字面 HTTP 400/409/503），与 ACL / QPS limit / RDB Analysis handler 一致；不在本 feature 引入新约定。验收契约里"返回 503/400/409"按"返回明确可识别的错误文本"理解。

**触发示例**：

```bash
# 来源: codis-admin --pitr-create（cmd/admin/pitr.go）
codis-admin --dashboard=127.0.0.1:18080 --pitr-create --server=10.0.0.5:6379 --truncate-ts=1716000000
# 成功只打印 job_id
```

### 2.2 编排层

#### 主流程图

```mermaid
flowchart TD
  Req[PUT /pitr/create] --> Val[validate: xauth, enabled, server in product & master]
  Val -->|fail| ErrJob[job state=failed, 记录错误]
  Val -->|ok| Pre[validate prerequisites: CONFIG GET appendonly/aof-timestamp-enabled/dir/appendfilename/appenddirname]
  Pre -->|not enabled| ErrJob
  Pre -->|ok| Lock[acquire per-server lock]
  Lock -->|busy| ErrJob
  Lock -->|ok| Feas[FEASIBILITY - server UP: locate last segment from manifest, scan #TS: range, confirm truncate point in it]
  Feas -->|ts out of range / not in last file / bad annotation| ErrJob
  Feas -->|ok| Shut[SHUTDOWN NOSAVE target master]
  Shut -->|fail| ErrJob
  Shut -->|ok| Snap[SNAPSHOT manifest + last segment file to sibling snapshot dir, same fs; record PreStatSnapshot size+mtime of all AOF files]
  Snap -->|disk full / io err| FailedCleanSnap[failed but AOF untouched, clean restart possible - server down]
  Snap -->|ok| Trunc[exec redis-check-aof --truncate-to-timestamp ts --fix manifest]
  Trunc -->|non-zero exit| FailedWithSnap[failed, server down, RESTORE FROM SNAPSHOT guidance in job error]
  Trunc -->|exit 0| StatDiff[re-stat all AOF files, diff vs PreStatSnapshot]
  StatDiff -->|non-last file changed| FailedModelDrift[failed: UNCERTAIN, redis-check-aof behavior drift, keep snapshot, alert loudly]
  StatDiff -->|only last file changed as expected| Restart[optional pitr_restart_command kick, then ALWAYS poll INFO loading]
  Restart -->|poll timeout| FailedWithSnap
  Restart -->|up| WaitLoad[INFO loading == 0 confirmed]
  WaitLoad -->|timeout| FailedWithSnap
  WaitLoad -->|ok| Resync[SetMaster on each replica to force full resync]
  Resync -->|ok| OK[state=succeeded, snapshot kept for audit until job removed]
  Resync -->|partial| PartialOK[state=succeeded with warning: list of failed replicas]
  Cancel[cancel request] --> AnyRunning{any non-terminal step?}
  AnyRunning -->|yes| MarkCancelled[kill subproc / stop polling, state=cancelled, snapshot kept if taken]
  AnyRunning -->|no| rpc error "job not running"
```

#### 现状

- `Topom` 的现有编排都是同步的（group/slot/proxy 操作在锁内做），没有长时异步 job 状态机。唯一的异步 job 范式是 `RDBAnalysisManager`，但它是只读解析，没有"停服/改文件/重启"这种带副作用的步骤。
- `pkg/utils/redis.Client.Shutdown()` 已存在（`client.go:258`），直接发 `SHUTDOWN`。
- Redis 进程的启动**不在** dashboard 现有职责内——Codis 假设 Redis server 由 systemd/supervisor/docker 等外部进程管理器拉起。这是本 feature 的一个关键编排边界（见决策 7 与流程级约束）。

#### 变化

**新增状态机编排**（`PitrManager.run`）：

1. **validate**（锁内 + 只读）：xauth、`pitr_enabled`、server 在 topom model 且是 group master、无同 server running job。
2. **validate prerequisites**（只读，发 Redis 命令）：`CONFIG GET appendonly` / `aof-timestamp-enabled` / `dir` / `appendfilename` / `appenddirname`——只保留定位 AOF 必需的 5 个，**不读** port/bind/*file（fork 残味，见决策 7）。任一未满足 → failed。
3. **acquire per-server lock**：进程内 map，同 server 同时只允许一个 job；**lock 持续到该 job 被 Remove 才释放**（PITR 不幂等，terminal-but-unacknowledged job 也必须挡住新 job——运维须确认恢复结果并 Remove 后才能再起）。create 阶段即校验：同 server 已有任意 job（running 或 terminal 未 Remove）→ 拒绝。
4. **feasibility check（server 仍在 UP）**：Go 侧读 manifest 定位最后 segment，扫描该文件 `#TS:` 注释得到 [min,max]，确认 `truncate_ts` 落在该文件内且 ≥ min。不满足 → failed，**server 未被触碰**。这一步把 `redis-check-aof` 在 `ftruncate` 前就会 exit 的失败（TS 不在最后文件、TS 早于全部记录、注释非法）提前拦截，让真正走到"停服后 truncate 才失败"的只剩磁盘 I/O 这类真·不确定。
5. **SHUTDOWN NOSAVE + 后置探活**：`Client.Do("SHUTDOWN", "NOSAVE")`，避免 Redis 在退出时写 RDB 覆盖。**关键**：发送后无论返回什么错误（transport close vs Redis command error 如 NOAUTH/unknown command），都必须**用一条新连接探活**确认 server 真的不可达，才视为成功；若探活仍能 PING 通 → failed（server 仍在线），**绝不进入 snapshot/truncate**（否则会截到一个正在写的 AOF）。这是 CR-001 的硬约束。
6. **snapshot manifest + last file**：把 manifest 文件 + 最后一个 segment 文件拷到 `<aofdir>.pitr-snapshot-<jobid>/`（AOF 目录同级、同文件系统，保证拷贝不跨盘）。只拷这两个（redis-check-aof 只对最后文件就地 `ftruncate`，已验证不改 manifest/更早文件）。拷贝失败（磁盘满/IO）→ failed，但此时 AOF 未被任何 truncate 触碰、干净如初，运维只需手动重启。同时记录 AOF 目录每个文件的 size+mtime（`PreStatSnapshot`）。
7. **truncate**：`exec.CommandContext(ctx, redisCheckAofBin, "--truncate-to-timestamp", strconv(ts), "--fix", manifestPath)`——用 CommandContext 以便 cancel 能真正杀死子进程，捕获 stdout/stderr/exit code。
   - 非 0 → failed，job error 显式给出"从快照 `<snapshotdir>` 恢复 `<lastsegmentfile>` 后重启"指引，保留 snapshot。
   - cancel 触发（`ctx.Err()!=nil`）→ failed（AOF 状态不确定，server 已停），保留 snapshot，绝不静默标 cancelled。
   - exit 0 但 **stat diff 复查**（双向 compare：post 新增、pre 缺失、非 last 文件 size/mtime 变化都算 model drift）发现异常 → failed，归"不确定"类（redis-check-aof 行为偏离模型/版本漂移），大声告警，保留 snapshot，job error 显式列出变化的文件。
8. **restart**：可选执行 `pitr_restart_command`（`sh -c <toml>`，记日志，标 `RestartKicked=true`）踢一脚，然后**无论是否 kick 都轮询** `INFO` 的 `loading` 直到 server 上线。`pitr_restart_command` 未配则纯靠外部进程管理器拉起 + 轮询。轮询超时 → failed（server 可能在 loading 或没起来），不强制 shutdown。
9. **wait load**：轮询 `INFO persistence` 的 `loading`==0，超时（`pitr_job_timeout`）→ failed。
10. **resync replicas**：遍历该 group 其余 server，`SetMaster(masterAddr)` 强制全量 resync；部分失败记 warning，不阻塞 succeeded。
11. **terminal 但不 release lock**。lock 持续到 operator 显式 Remove（`PUT /api/topom/pitr/remove`）才释放，给非幂等 PITR 留防重入窗口。snapshot 同样保留到 Remove，供事后审计或回退。Remove 仅允许 terminal job；running job Remove 返回错误（"Cancel it first"），绝不静默中止 in-flight truncate。run() 末尾裁定终态时 **step 错误优先于 ctx 取消**：一个真实 step error 不会被 cancelled 掩盖（否则会隐藏 AOF 损坏）。

**变化点小结**：编排拓扑从"无"升级为"带副作用的状态机"；与现有同步编排的区别是**跨进程、带文件副作用、有外部进程依赖（redis-check-aof / 进程管理器）**；核心安全设计是 **feasibility 前置拦截 + snapshot 把 ftruncate 变可逆 + 不 fork 把进程生命周期留给监管系统**。

#### 流程级约束

- **错误语义（按"server 是否被触碰 + 是否可干净回退"分层）**：
  - validate/prereq/lock/feasibility 失败 → job failed，**server 未被触碰**（仍 UP），零副作用。
  - snapshot 失败（SHUTDOWN 后）→ job failed，**server 已停但 AOF 未被 truncate 触碰**，干净如初，运维直接重启即可。job error 标注 "server is down, AOF untouched, just restart"。
  - truncate 失败（SHUTDOWN + snapshot 后）→ job failed，**server 已停且最后 segment 可能被 ftruncate 改坏**，但**有 snapshot 可一键回退**。job error 显式给出回退指引（把 snapshot 拷回 + 重启）。**不自动重启**——AOF 可能损坏，自动重启会加载到不确定状态，"up + 悄悄错"远比 "down + 大声告警"严重。
  - **stat diff 检测到非最后文件变化**（truncate exit 0 但复查发现）→ job failed，归"不确定"类：redis-check-aof 行为偏离我们的模型（版本漂移/bug），保留 snapshot，job error 显式列出变化的文件 + 大声告警。这主动暴露模型失效，而非闷头相信 truncate 成功。
  - restart 轮询超时 / wait-load 超时 → failed，server 可能在 loading 或没起来，不强制 shutdown，留给运维；此时 AOF 已被正确截断，若 server 最终起来其实数据是对的，运维可手动确认后清理 job。
  - resync 部分失败 → succeeded-with-warning，不影响主流程，warning 列表写入 job。
- **幂等性**：PITR job **不幂等**（重复执行会把 server 往更早的时间点再截一次，或对已截断的 AOF 再截报错）。用 per-server lock + terminal job 历史防重入；`cancel` 后的 server 状态由运维确认才能再起新 job。
- **并发/顺序**：同一 server 同时最多 1 个 job（per-server lock）；不同 server 可并行，受 `pitr_max_concurrent_jobs` 全局上限约束。同一 group 内不会有两个 server 同时被 PITR（因为只允许 master，而 group 同时只有一个 master）。
- **外部进程依赖**：`redis-check-aof` 和 redis-server 进程管理器是 dashboard 之外的依赖。`pitr_redis_check_aof_bin` 缺失 → create 阶段拒绝；`pitr_restart_command` 未配则走"等待外部拉起 + 轮询"分支，两者都显式。dashboard **绝不 fork redis-server**。
- **磁盘空间约束**：snapshot 落 AOF 目录同级同文件系统；feasibility 阶段应 best-effort 检查可用空间 ≥ 最后 segment 文件大小（不足 → failed，server 未被触碰）。snapshot 失败的清理（删半成品）必须在失败路径里做。
- **可观测点**：job 每步带 started_at/finished_at/error；`GET /pitr/jobs` 返回摘要；dashboard log 记录 server addr / ts / bin 路径 / exit code / snapshot dir / 耗时；**不记录** AOF 内容、CONFIG 中的密码（不读 `CONFIG GET requirepass`）。
- **时间精度**：秒级。`aof-timestamp-enabled` 只写 unix 秒注释，`redis-check-aof --truncate-to-timestamp` 按 `#TS:` 秒比较；亚秒级精度不在本 feature 范围。
- **slot 路由影响**：master 重启期间，该 group 承载的 slot 写入会失败。dashboard 不主动切 slot 到其他 group（那是 slot migration 的职责，且会破坏 PITR 的"恢复该 server"语义）。运维需自行评估或提前切流。job 创建时在响应里提示受影响的 slot 范围。

### 2.3 挂载点清单

判据"删了它 feature 是否消失"：

- `config/dashboard.toml`（committed 默认模板，运维实际看到的配置文件，对齐 RDBAnalysis 段 `config/dashboard.toml:31-39` 的 9-key 先例）：新增 `pitr_enabled` / `pitr_redis_check_aof_bin` / `pitr_max_concurrent_jobs` / `pitr_job_timeout` / `pitr_restart_command` 5 个带注释默认值的 key — **新增**。删了则运维发现不了开关，"默认关闭、可发现"成功标准达不到。
- `pkg/topom/config.go` 的 `Config` struct + 代码默认值 + 校验：同 5 个字段 — **新增**。删了则 manager 无法初始化、所有 `/pitr/*` 返回 disabled/not-initialized 错误。（与上一条是两件事：toml 模板管可发现性，config.go 管运行期默认值与校验。）
- `pkg/topom/topom_api.go`：server 路由组 `/api/topom/pitr/:xauth`（create/get/cancel）+ handler + `ApiClient` 客户端方法 `CreatePitr`/`GetPitrJobs`/`CancelPitr` — **新增**（三件合一：codis-admin 不直连 dashboard，经 ApiClient 客户端方法调用，对齐 `cmd/admin/rdb_analysis_remote_fetch.go:21` 调 `c.StartRDBAnalysisRemoteFetch` 的先例）。删了则无 HTTP 入口、admin CLI 也调不通。
- `cmd/fe/assets/index.html` + `cmd/fe/assets/pitr.js`：FE PITR 区域 — **新增**。删了则无 UI 入口（但 admin CLI 和 API 仍可用，所以这条是"删了用户视角的 FE 入口消失"）。
- `cmd/admin/main.go` + `cmd/admin/dashboard.go`：`--pitr-*` flag 分发 — **新增**。删了则无 CLI 自动化入口。
- `pkg/topom/topom.go` 的 `Topom` struct + `setup`/`Close`：接入 `pitr *PitrManager` — **修改**。删了则 manager 不被创建，feature 不工作。

共 6 条（dashboard.toml 与 config.go 拆开计，因可发现性与运行期默认是两个独立成功标准）。无 Redis 8 C 侧新挂载点（复用既有）。

### 2.4 推进策略

按"编排骨架 → 计算节点 → 外部依赖接通 → 测试"切片：

```
1. 编排骨架：PitrManager + PitrJob 类型 + 状态机骨架（每步 stub）；topom_api.go 三件套（server 路由组 + handler + ApiClient 客户端方法 CreatePitr/GetPitrJobs/CancelPitr，cmd/admin 依赖此客户端方法不直连 dashboard）；Config（config.go struct + 代码默认值 + 校验）+ config/dashboard.toml 默认模板（5 个带注释 pitr_* key，对齐 RDBAnalysis 段 committed 先例）；FE pitr.js + index.html；codis-admin --pitr-* flag；Topom 接入 manager 生命周期
   退出信号：PUT /pitr/create 创建 job 走完状态机（每步 stub），GET /pitr/jobs 返回摘要，cancel 终止；pitr_enabled=false 时所有 /pitr/* handler 返回 "pitr is disabled"；codis-admin --pitr-create 经 ApiClient 走通；dashboard.toml 含全部 5 个 pitr_* key 带注释
2. 计算节点 - 校验阶段：实现 xauth/enabled/server-in-product/is-master/prereq（CONFIG GET 5 个配置）/feasibility（读 manifest 定位最后 segment + 扫 #TS: 范围）
   退出信号：单测覆盖正常 + 未开 AOF + 非 master + ts 越界 + ts 不在最后文件 + 重复 job + 可用空间不足
3. 计算节点 - 副作用步骤：实现 SHUTDOWN NOSAVE、snapshot（manifest + 最后文件，含失败清理）、pre/post stat diff（检测非最后文件变化即归"不确定"失败）、exec redis-check-aof truncate（捕获 exit code）、restart（可选 kick + 始终轮询 INFO loading）、wait-load、resync replicas
   退出信号：单测覆盖 exit code 解析、snapshot 失败清理、stat diff 漂移检测、超时、部分副本失败；集成测试用本地 Redis 8 跑通完整 happy path
4. 接通外部依赖与并发：pitr_redis_check_aof_bin 缺失检测、pitr_restart_command 可选分支、per-server lock、并发上限、snapshot 在 job 删除前的保留与清理
   退出信号：bin 缺失时 create 拒绝；并发上限触发时拒绝；同 server 重入被拒；job remove 时 snapshot 目录被清理
5. 测试覆盖：补齐验收契约剩余场景（snapshot 回退指引、truncate 失败、cancel 中途、跨 server 并行、FE/admin 端到端）
   退出信号：所有验收场景有可观察证据
```

### 2.5 结构健康度与微重构

##### compound convention 检索

```bash
python3 .codestable/tools/search-yaml.py --dir .codestable/compound \
  --query "目录组织 OR 命名 OR 归属 OR topom 文件拆分"
```
命中结果：无 topom 文件组织或 manager 拆分相关 convention。

##### 评估

- **文件级 — `pkg/topom/topom.go`**：现有约 470 行，职责是 Topom 核心（setup/close/start/routines/model/stats/overview/admin server）。本次只在 struct 加一个字段 + setup/Close 各加一行，改动密度低（2 处），不触发"一文件多职责"信号。
- **文件级 — `pkg/topom/config.go`**：现有含 RDBAnalysis* 系列字段。本次加 5 个 Pitr* 字段，延续既有"按能力分组的 toml 字段"模式，属同类延伸而非第 N+1 件事。
- **文件级 — `pkg/topom/topom_api.go`**：现有已按 `r.Group("/rdb-analysis", ...)` 模式分组注册。本次加 `r.Group("/pitr", ...)`，延续既有模式。
- **目录级 — `pkg/topom/`**：现有已按"一个能力一个 `topom_xxx.go` 文件"组织（topom_rdb_analysis.go、topom_acl.go、topom_hot_key_cache.go、topom_qps_limit.go 等）。本次新增 `topom_pitr.go` + `topom_pitr_api.go` 完全符合既有 convention，目录不摊平（约 20 个文件，但命名前缀清晰可分组，未达 6 份同类阈值）。
- **目录级 — `cmd/fe/assets/`**：现有按能力一个 js 文件（acl.js/qps-limit.js/rdb-analysis.js）。本次加 pitr.js 符合 convention。
- **目录级 — `cmd/admin/`**：现有 dashboard.go 已较大（含 acl + rdb-analysis-remote-fetch 分发）。本次再加 pitr flag 分发会让 dashboard.go 进一步变大。

##### 结论：不做微重构

理由：所有目标位置都延续既有"一能力一文件/一组字段/一个路由组"convention，目录组织健康；唯一观察点是 `cmd/admin/dashboard.go` 偏大，但本次只加一个 flag 分发函数（约 30 行），未到"必须先拆"的程度，强行拆会超出"只搬不改行为"边界（拆 flag 分发涉及改 main.go 分发逻辑）。

##### 超出范围的观察（仅提示不阻塞）

- `cmd/admin/dashboard.go` 已接近 360 行并混合 ACL / RDB analysis remote fetch 两个能力的 flag 分发。若未来再加第 4 个能力的 admin flag，建议走 `cs-refactor` 把每个能力的 admin 分发拆成 `cmd/admin/<capability>.go`（如已存在的 `rdb_analysis_remote_fetch.go` 模式），让 `dashboard.go` 只做公共参数解析。本 feature 不动。

## 3. 验收契约

### 关键场景清单

**正常路径**：

1. 开启 `pitr_enabled=true`、目标 server 已开 `appendonly yes` + `aof-timestamp-enabled yes`，PUT `/pitr/create` 带合法 server_addr + ts → 返回 uuid v7 job_id；轮询 GET 看到 state 走完 validating→...→succeeded；恢复后 `GET key` 返回 ts 时间点的值。
2. `codis-admin --pitr-create --server=... --truncate-ts=...` 成功时只打印 job_id。
3. FE PITR 区域填入 server + ts 提交 → 出现 job 行 → 状态实时刷新到 succeeded。

**关键边界（前置校验，server 未被触碰即失败；下述 4xx/5xx 均指"返回明确可识别的错误文本"，沿用项目 rpc 800 约定，非字面 HTTP 状态码——见上文 HTTP 状态码约定）**：

4. `pitr_enabled=false` 时所有 `/pitr/*` 返回 disabled 错误，且不创建任何 job。
5. 目标 server 不在当前 topom model → 错误，不发任何 Redis 命令。
6. 目标 server 在 model 但非 master（是 replica）→ 错误。
7. 目标 server `appendonly=no` → 错误，error 文本含 "appendonly"；job state=failed，server 未被改动。
8. 目标 server `aof-timestamp-enabled=no` → 错误，error 文本含 "aof-timestamp-enabled"。
9. **feasibility**：`truncate_ts` 落不到最后一个 segment 文件内（TS 不在最后文件 / TS 早于该文件全部 `#TS:` / 注释非法）→ 错误，error 含 "truncate point not in last aof segment"；**server 仍在 UP**，未被 SHUTDOWN。
10. **feasibility**：可用空间 < 最后 segment 文件大小 → 错误，error 含 "insufficient disk for snapshot"；server 仍在 UP。
11. 同一 server 已有 job（running 或 terminal 未 Remove），再次 create → 错误，error 含 "is locked by pitr job ...; Remove that job first"。
12. `pitr_redis_check_aof_bin` 指向的文件不存在/不可执行 → create 阶段错误，error 含 "redis-check-aof binary"。

**关键错误路径（带副作用后的分层回退）**：

13. **snapshot 失败**（SHUTDOWN 后，磁盘满/IO 错）→ job failed，error 标注 "server is down, AOF untouched, just restart"；半成品 snapshot 目录被清理；AOF 未被任何 truncate 触碰。
14. **truncate 失败**（snapshot 后，exit code != 0）→ job failed，error 含子进程 stderr 摘要 + **显式回退指引**（"restore `<snapshotdir>/<file>` over `<aofdir>/<file>` then restart"）；snapshot 目录保留不清理；**不自动重启**。
14b. **stat diff 漂移**（truncate exit 0 但复查发现最后一个文件以外的文件 size/mtime 变了）→ job failed，归"不确定"类，error 含 "aof model drift detected" + 变化文件列表；保留 snapshot；不自动重启。
15. **restart 轮询超时 / wait-load 超时**（`pitr_job_timeout`）→ job failed，不强制 shutdown；此时 AOF 已正确截断，运维可手动确认 server 状态后决定。
16. **resync replicas 部分失败** → job state=succeeded，但 steps/resync 含 warning 列表（哪些副本 resync 失败）。
17. **job 运行中收到 cancel**：在 snapshot 前 → state=cancelled，server 仍在 UP；在 snapshot 后/truncate 中 → state=cancelled，server 已停，job 标注当前阶段 + 保留 snapshot 供运维处理；terminal job 的 cancel 是 no-op（返回 OK）。

**并发**：

18. 两个不同 server 同时 create → 都成功，并发受 `pitr_max_concurrent_jobs` 限制；超限时后到的返回 "too many running pitr jobs" 错误。
19. snapshot 目录命名带 job_id，两个并发 job 即使碰巧同 server（被 lock 拦）或不同 server 也不冲突。

**snapshot 生命周期**：

20. job succeeded 后 snapshot 保留（供事后审计/回退），直到 job 被 `remove` 或 dashboard 重启清理；`GET /pitr/jobs` 摘要里暴露 snapshot dir 路径。
21. job remove 时 snapshot 目录被一并删除；删除失败不阻塞 job 删除，记 warning。

### 明确不做的反向核对项

- 代码中**不应出现**对 Redis 8 C 源码（`extern/redis-8.6.3/src/aof.c`、`redis-check-aof.c`）的修改：`git diff extern/redis-8.6.3/src/aof.c extern/redis-8.6.3/src/redis-check-aof.c` 为空。
- proxy 业务路径**不应**新增任何 PITR 相关分支：`pkg/proxy/` 下不出现 pitr/Pitr 符号。
- `pkg/models/pitr.go` **不应被创建**（首版 job 不进 coordinator）。
- coordinator 路径下**不应**新增 `/codis3/{product}/pitr`：`pkg/models/store.go` 无 pitr 路径常量。
- dashboard API 响应、job snapshot、FE 状态、日志**不应**包含 `requirepass`/ACL 密码/AOF 文件内容：grep test。
- `config/redis.conf` 的 `appendonly` / `aof-timestamp-enabled` **默认值不变**（仍为 no）。
- proxy mapper **不应**把任何 PITR 命令加入 allow-list：`pkg/proxy/mapper.go` 无 pitr 相关 FlagNotAllow 变更。
- 跨 group 全局恢复、指定 key 子集恢复**不应**出现在代码中：无 GlobalPitr / KeyPitr 类型。
- **不应 fork redis-server**：代码中不出现 `exec.Command(... "redis-server" ...)` 或等价的直接拉起 Redis 进程的调用（`pitr_restart_command` 走 `sh -c` 间接调用运维脚本不算）。
- prereq **不应读** `CONFIG GET port/bind/*file`（fork 残味）：grep `pkg/topom/topom_pitr*.go` 无这些 CONFIG GET 调用。
- snapshot **不应拷贝整个 AOF 目录**：snapshot 实现只 cp manifest + manifest 列表最后一个文件（base 或 last incr），不出现对整个 appendonlydir 的递归拷贝。
- truncate 前后**必须做 stat diff**：snapshot 步骤记录 `PreStatSnapshot`，truncate exit 0 后复查 AOF 目录所有文件 size+mtime；最后一个文件以外的任何文件变化即归"不确定"失败。grep `pkg/topom/topom_pitr*.go` 应有 pre/post stat 比对逻辑。

## 4. 与项目级架构文档的关系

acceptance 阶段需提炼回 `architecture/ARCHITECTURE.md`：

- **名词**（→ "结构与交互 / 数据与状态"节）：新增 "PITR / 数据闪回"术语条目，描述它是 dashboard 进程内单 server AOF 截断恢复编排，默认关闭，复用 Redis 8 原生 `aof-timestamp-enabled` + `redis-check-aof`。
- **动词骨架**（→ 结构 fig / 模块交互）：dashboard 新增 PITR job manager 的状态机流程；dashboard → Redis Server（SHUTDOWN/CONFIG/INFO/SetMaster）+ dashboard → redis-check-aof 子进程 + dashboard → 进程管理器（restart）三类外部交互。
- **流程级约束**（→ "已知约束"）：PITR 单 server 级、不做跨 group / key 级、依赖运维已开 AOF、依赖文件系统直访 AOF 目录、恢复期间该 group 写入失败但不自动切 slot、不幂等靠 lock 防重入、**不 fork redis-server（靠外部进程管理器 + 轮询）**、**truncate 前快照最后 segment 使 ftruncate 可逆**、**feasibility 前置使绝大多数截断失败在停服前被拦截**、truncate 失败不自动重启（AOF 可能损坏，down+告警优于 up+悄悄错）。

关联已有架构 doc：无独立子系统 doc，归入 `ARCHITECTURE.md` 总入口的 Codis 能力清单（与 RDB Analysis、ACL、QPS limit 并列）。

需在 `redis-cluster-service` requirement 的"实现进展"加一条本 feature 的 entry，"边界"加 PITR 边界条目。

---

> 方案已通过整体 review，status 改为 approved。snapshot 定稿为 manifest + 最后一个文件 + pre/post stat diff 兜底；"truncate 不改 manifest" 已源码验证。进入实现阶段。
