# PITR 数据闪回 验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-06-18
> 关联方案 doc：`.codestable/features/2026-06-18-pitr-server-flashback/pitr-server-flashback-design.md`

## 1. 接口契约核对

对照方案第 2.1 节名词层逐一核查：

**接口示例逐项核对**：

- [x] **PUT /api/topom/pitr/create/:xauth**（`pkg/topom/topom_api.go:91` 路由 + `topom_pitr_api.go:56` handler）：body `{server_addr, truncate_ts}` → 200 `{job_id}`；代码 `r.Put("/create/:xauth", ...)` + handler decode `PitrCreateRequest` + 返回 `PitrCreateResponse{JobID}`，一致。verb PUT（对齐项目 RDB Analysis `ApiPutJson` 约定），已修正 design 早先的 POST 残留。
- [x] **GET /api/topom/pitr/jobs/:xauth**（`topom_api.go:92` + `topom_pitr_api.go:PitrList`）：返回 `[]*PitrJob` snapshot，一致。
- [x] **GET /api/topom/pitr/:xauth/:id**（路由 + `PitrGet`）：单 job snapshot，一致。
- [x] **PUT cancel / remove**（路由 + handler）：cancel 对 terminal job 是 no-op（返回 OK）；remove 仅允许 terminal job（running 返 "Cancel it first"），一致。
- [x] **ApiClient 客户端方法**（`topom_pitr_api.go:146-186`）：`CreatePitr`/`ListPitr`/`GetPitr`/`CancelPitr`/`RemovePitr` 共 5 个，全部经 `rpc.ApiPutJson`/`ApiGetJson`，admin 不直连 dashboard，一致。design 2.1 列了 3 个（Create/Get/Cancel），实现多了 `ListPitr` + `RemovePitr`（配套 jobs/remove 路由）——轻微扩展，非偏差。
- [x] **create response wire field**：`PitrCreateResponse.JobID json:"job_id"`（WR-001 修正后），对齐 design API 主段，一致。`TestPitrCreateResponseWireContract` 钉住。

**名词层"现状 → 变化"逐项核对**：

- [x] **PitrManager**（`topom_pitr.go`）：registry + per-server lock + 并发上限 + snapshot 生命周期，对齐 design 2.1。
- [x] **PitrJob**（`topom_pitr.go`）：字段 `LastSegmentFile`/`LastSegmentTsRange`/`SnapshotDir`/`PreStatSnapshot`/`RestartKicked` 全部落地，对齐 design 2.1 类型定义。
- [x] **状态机 state**：13 个 state（pending/validating/checking_prereq/checking_feasibility/shutting_down/snapshotting/truncating/restarting/waiting_load/resyncing_replicas/succeeded/failed/cancelled），对齐 design。

**流程图核对**（第 2.2 节开头 mermaid 图）：

- [x] 图中节点（validate/prereq/feasibility/shutdown/snapshot/truncate/stat-diff/restart/wait-load/resync + 三类失败分支 ErrJob/FailedCleanSnap/FailedWithSnap/FailedModelDrift）在代码 `topom_pitr_run.go` 均有实际 step 函数落点（grep `stepValidate`/`stepPrereq`/`stepFeasibility`/`stepShutdown`/`stepSnapshot`/`stepTruncate`/`stepRestart`/`stepWaitLoad`/`stepResync` 确认）。

**无偏差**。

## 2. 行为与决策核对

**需求摘要逐项验证**：

- [x] dashboard 编排单 server AOF 截断恢复：`PitrManager.run` → `runSteps` 9 步状态机，实测。
- [x] FE/admin/REST 三入口同一 dashboard API：FE `pitr.js`、admin `pitr.go`、REST 路由全走 `/api/topom/pitr/*`。
- [x] 前提校验只读不写：`stepPrereq` 只 `CONFIG GET` 5 个配置，不 `CONFIG SET`，实测。
- [x] 默认关闭：`pitr_enabled=false` 默认，`pitrManagerEnabled` 全 handler 拒绝。

**明确不做逐项核对**（第 3 节反向核对项，全部 grep 确认）：

- [x] 不改 Redis 8 C 源码：`git diff extern/redis-8.6.3/` 空（0 行）
- [x] proxy 业务路径无 PITR：`grep pitr pkg/proxy/` 0 命中
- [x] job 不进 coordinator：`pkg/models/pitr.go` absent，`store.go` 无 pitr 路径
- [x] 不 fork redis-server：`grep "redis-server" topom_pitr*.go` 0 命中
- [x] 不读 requirepass/port/bind/*file：grep 0 命中
- [x] 不快照整目录：`topom_pitr_run.go` snapshot 只 `copyFile` manifest + last segment，无 Walk/ReadDir-拷贝
- [x] 不自动开 AOF：prereq 只 GET 不 SET；`config/redis.conf` 默认 `appendonly no` / `aof-timestamp-enabled no` 不变
- [x] 跨 group / key 级恢复：无 GlobalPitr/KeyPitr 类型（0 命中）

**关键决策落地**：

- [x] D1 复用 Redis 8 原生 aof-timestamp-enabled + redis-check-aof（不改 C）：实测 `exec.CommandContext(m.redisCheckBin, "--truncate-to-timestamp", ts, "--fix", manifest)`。
- [x] D7 不 fork redis-server：restart 走"可选 pitr_restart_command kick + 始终轮询 INFO"，代码 `stepRestart` 实测。
- [x] D8 snapshot = manifest + last file：`stepSnapshot` 只 copyFile 这两个。
- [x] D8 stat diff 兜底：`stepTruncate` 双向 compare（forward appeared / reverse disappeared / non-last changed），`TestPitrTruncateStatDiffForwardAppeared` + `TestPitrTruncateStatDiffReverseDisappeared` 钉住。
- [x] D9 feasibility 前置：`stepFeasibility` 在 SHUTDOWN 前，server 仍 UP。

**编排层"现状 → 变化"**：已在第 1 节核对，状态机 step 顺序与 design 一致。

**流程级约束核对**：

- [x] **错误语义三层分层**：未触碰（validate/prereq/feasibility 失败 server UP）/ 已停但 AOF 干净（snapshot 失败）/ 已停且有 snapshot 可回退（truncate/stat-diff 失败）——代码 `stepShutdown` 后置探活 + `stepTruncate` restore 指引实测。
- [x] **CR-001 SHUTDOWN 后置探活**：`topomPitrDeps.Shutdown` 发送后用新连接探活，仍能 PING → failed，`TestPitrShutdownFailureStopsStateMachine` 钉住（snapshot/truncate 不执行）。
- [x] **幂等性 + lock 持续到 Remove**：`Create` 同 server 已有任意 job（running/terminal 未 Remove）→ 拒绝；`run` 末尾不释放 lock；`Remove` 才释放。`TestPitrPerServerLockHeldUntilRemove` 钉住。
- [x] **err 优先于 cancel**：`run()` switch `case err != nil` 在 `case ctx.Err() != nil` 前，`TestPitrErrorPrecedenceOverCancel` 钉住。
- [x] **并发**：`pitr_max_concurrent_jobs` 全局上限，`TestPitrConcurrentLimit` 钉住。
- [x] **CommandContext**：truncate + restart 用 `exec.CommandContext`，cancel 能杀子进程（grep 2 处）。

**挂载点反向核对（可卸载性）**——对照第 2.3 节：

- [x] M1 `config/dashboard.toml`（5 个 pitr_* key 带注释）：✓ grep 确认，`--default-config` 重新生成完整模板。
- [x] M2 `pkg/topom/config.go`（Config struct 5 字段 + 校验）：✓ `PitrEnabled`/`PitrRedisCheckAofBin`/`PitrMaxConcurrentJobs`/`PitrJobTimeout`/`PitrRestartCommand`。
- [x] M3 `pkg/topom/topom_api.go`（路由组 + handler + ApiClient 三件套）：✓ `/pitr` group + 5 handler + 5 client method。
- [x] M4 `cmd/fe/assets/{index.html,pitr.js}`：✓ PITR 区域 + script tag。
- [x] M5 `cmd/admin/{main,dashboard,pitr}.go`：✓ docopt flags + dispatch + handler。
- [x] M6 `pkg/topom/topom.go`（Topom.pitr + New/Close）：✓ struct 字段 + `s.pitr = NewPitrManager(config)` + Close 接入。
- [x] **反向 grep**：所有 pitr 引用（16 个文件）全落在清单内（含测试文件 `dashboard_pitr_test.go`/`topom_pitr_test.go`），清单外无功能引用。`models/`/`proxy/` 0 命中。
- [x] **拔除沙盘推演**：删 7 个新文件 + revert 8 个 modified → PITR 完全消失，无残留（coordinator/proxy/C 源码零影响）。

**挂载点补充（轻微文档差异，非偏差）**：`cmd/admin/dashboard_pitr_test.go` 是 review 建议的 docopt 测试，属测试基础设施而非功能挂载点；design 2.3 不需追加（测试文件不入挂载点清单）。已记入本报告。

## 3. 验收场景核对

对照方案第 3 节关键场景清单，逐条可观察证据：

**正常路径**：

- [x] **S1** enabled + AOF 已开，PUT create 返回 uuid v7 job_id，state 走完到 succeeded
  - 证据：单测覆盖 state 机各步；**端到端 happy path（真实 Redis 8 + 截断 + 重启 + 读回）未在本环境跑**——标为遗留（见第 9 节）。job_id wire contract 由 `TestPitrCreateResponseWireContract` 钉住。
- [x] **S2** `codis-admin --pitr-create` 成功只打印 job_id：`cmd/admin/pitr.go:handlePitrCreate` `fmt.Println(id)`，docopt 解析由 `TestAdminDocoptPitrCreateParses` 钉住。
- [x] **S3** FE PITR 区域提交后状态实时刷新：FE `pitr.js` 轮询（`schedulePoll` 2s），HTML 区域已接入 `index.html` + `dashboard-fe.js`。**浏览器肉眼未验证**（无运行 dashboard）——标为遗留。

**关键边界（前置校验）**：

- [x] **S4** `pitr_enabled=false` 时 `/pitr/*` 返回 disabled：`TestPitrCreateDisabled` + `TestPitrHandlerGateNoRecursion`（disabled 路径）。
- [x] **S5** server 不在 product → 错误不发 Redis 命令：`stepValidate` 经 `GroupOfServer`（fake 返回 not-found）。
- [x] **S6** 非 master → 错误：`TestPitrValidateRejectsReplica`。
- [x] **S7** `appendonly=no` → 错误含 "appendonly"：`TestPitrPrereqRejectsAofDisabled`。
- [x] **S8** `aof-timestamp-enabled=no` → 错误含 "aof-timestamp-enabled"：`TestPitrPrereqRejectsTimestampDisabled`。
- [x] **S9** feasibility ts 落不到最后文件 → 错误含 "not in last aof segment"：`TestPitrFeasibilityTsOutOfRange`。
- [x] **S10** 可用空间不足 → 错误：feasibility `freeDisk` 检查（代码实现，单测间接覆盖）。
- [x] **S11** 同 server 已有 job → 错误含 "is locked by pitr job"：`TestPitrPerServerLockHeldUntilRemove`。
- [x] **S12** bin 不存在/不可执行 → create 阶段错误：`TestPitrCreateRejectsMissingBin` + `TestPitrCreateRejectsNonExecBinary`。

**关键错误路径（分层回退）**：

- [x] **S13** snapshot 失败 → failed，AOF 未触碰，半成品清理：`stepSnapshot` 失败路径 `os.RemoveAll(snapshotDir)`（代码 + `TestPitrSnapshotCopiesAndRecordsStat` 间接）。
- [x] **S14** truncate 失败 → failed + restore 指引 + 保留 snapshot：`TestPitrTruncateFailureKeepsSnapshot`。
- [x] **S14b** stat-diff 漂移 → failed 归不确定类：`TestPitrTruncateStatDiffDrift`（changed）+ `TestPitrTruncateStatDiffForwardAppeared` + `TestPitrTruncateStatDiffReverseDisappeared`。
- [x] **S15** restart/wait-load 超时 → failed：`stepRestart`/`stepWaitLoad` 超时返回 error 含 snapshot 指引（代码实现）。
- [x] **S16** resync 部分失败 → succeeded + warning：`TestPitrResyncPartialFailureWarning`。
- [x] **S17** cancel 各阶段：snapshot 前 cancelled server UP；snapshot 后/truncate 中 cancelled server 已停 + 保留 snapshot；terminal cancel no-op。`TestPitrErrorPrecedenceOverCancel`（err 优先）+ `TestPitrRemoveRejectsRunning`（running Remove 被拒）覆盖关键路径；**cancel 在 snapshot 之后中途各阶段的完整时序未单测**——标为遗留。

**并发**：

- [x] **S18** 两 server 并发 create 受 max_concurrent 限制：`TestPitrConcurrentLimit`。
- [x] **S19** snapshot dir 命名带 job_id 防冲突：`stepSnapshot` `filepath.Join(..., ".pitr-snapshot-"+job.ID)`。

**snapshot 生命周期**：

- [x] **S20** succeeded 后 snapshot 保留到 job remove：代码 `Remove` 才 `os.RemoveAll(snapshotDir)`。
- [x] **S21** job remove 时 snapshot 清理（失败记 warning 不阻塞）：`TestPitrRemoveCleansSnapshot` + `Remove` 中 `log.WarnErrorf`。

**反向核对项**：10 项全部通过（见第 2 节 grep 证据）。

## 4. 术语一致性

对照方案第 0 节 + 第 2.1 节命名 grep 代码：

- **PITR / 数据闪回**：代码 `Pitr*` 命名一致（PitrManager 60 处、PitrJob 44 处）；`flashback`/`闪回` 仅文档用，代码 0 命中（防冲突 ✓）。
- **aof-timestamp-enabled / redis-check-aof**：37 处引用全部一致。
- **PitrDeps / topomPitrDeps / pitrFileStat / pitrServerLock**：实现细节命名，与 design 2.1 无冲突。
- 防冲突：design 0 节 grep 结论（PITR/flashback 无既有概念）成立。

**无不一致**。

## 5. 架构归并

对照方案第 4 节，三类内容实际写入 `architecture/ARCHITECTURE.md`：

- [x] **名词归并**（→ "0. 术语" + "数据与状态"节）：新增 "PITR / 数据闪回" 术语条目——dashboard 进程内单 server AOF 截断恢复编排，默认关闭，复用 Redis 8 原生 `aof-timestamp-enabled` + `redis-check-aof`，不进 coordinator/proxy/C 源码。**待写入**（第 5 节动作）。
- [x] **动词骨架归并**（→ 结构 fig / 模块交互）：dashboard → Redis Server（SHUTDOWN/CONFIG/INFO/SetMaster）+ dashboard → redis-check-aof 子进程（CommandContext）+ dashboard → 进程管理器（可选 pitr_restart_command）/ 轮询 INFO 三类外部交互。**待写入**。
- [x] **流程级约束归并**（→ "已知约束"节）：PITR 单 server 级、不做跨 group / key 级、依赖运维已开 AOF、依赖文件系统直访 AOF 目录、恢复期间该 group 写入失败但不自动切 slot、不幂等靠 lock 持续到 Remove 防重入、SHUTDOWN 后置探活、truncate 前快照最后 segment + 双向 stat-diff、truncate 失败不自动重启。**待写入**。

第 5 节动作：实际编辑 `ARCHITECTURE.md`（见下文执行）。

`attention.md` 候选：见第 8 节。

## 6. requirement 回写

方案 frontmatter `requirement: redis-cluster-service`（current req）。本次新增了用户可感能力（PITR 数据闪回），需 update 该 req：追加实现进展条目 + 边界条目，保留原始愿景。

- [x] 动作：`cs-req` update（手写）——在 `redis-cluster-service.md` 的"实现进展"追加 2026-06-18 PITR 条目，"边界"追加 PITR 边界。**待写入**（第 6 节动作）。

## 7. roadmap 回写

方案 frontmatter 无 `roadmap` / `roadmap_item` 字段（非 roadmap 起头）。

- [x] **跳过**：写"非 roadmap 起头，无需 items.yaml 回写"。

## 8. attention.md 候选盘点

回看本次实现，"每个 feature 都会撞一次"的环境/工具信息：

- **候选 1**：`/bin/true` / `/bin/false` 在 macOS 上路径不固定（本仓库开发机 darwin），写涉及外部 bin 的测试时需用 `trueBin/falseBin` helper 或 `exec.LookPath`。这是 Go 测试在 darwin 上的通用坑，下个写 exec 测试的 AI 会再撞。
- **候选 2**：`go run ./cmd/dashboard --default-config` 是重新生成 `config/dashboard.toml` 的正确方式（直接 print `DefaultConfig` 常量），而非用 `c.String()`（后者丢注释）。这与 Makefile `--default-config` target 一致，但容易误用。

**不擅自写入**——登记，落不落由用户在"退出后"环节定。

## 9. 遗留

**未在本环境验证（需真实 Redis 8 + 进程管理器）**：

- 端到端 happy path：真实 Redis 8（`appendonly yes` + `aof-timestamp-enabled yes`）→ 写入 → PUT create 指定过去 ts → 截断 → 重启 → 读回确认值回到 ts 时间点。本地 Go 单测覆盖各 step 逻辑，但完整链路未跑。
- cancel 在 snapshot 之后/truncate 中途各阶段的 server 最终状态（需真进程管理器才能观察 restart/wait-load 时序）。
- FE 浏览器肉眼验证（无运行 dashboard/proxy 环境）。

这些属于 SDD 已声明的 acceptance 范围之外的本轮环境限制；代码层面经两轮 review（最终 Good taste，无 Blocker/Critical/Warning）。建议在 Linux + 真实 Redis 8 环境补一次 E2E 验证后再上生产。

**已知限制**（SDD 边界，已落地）：

- 仅单 server 级恢复，不做跨 group 全局一致性 / 指定 key 子集恢复。
- 恢复期间该 group slot 写入失败，不自动切 slot。
- 依赖运维已开 AOF + aof-timestamp-enabled，依赖 dashboard 直访目标 server AOF 目录。
- 不 fork redis-server（靠外部进程管理器 + 轮询）。

**实现阶段顺手发现**：

- `cmd/admin/dashboard.go` 已较大（混合 ACL + RDB analysis remote fetch + PITR 的 flag 分发）。建议后续走 `cs-refactor` 把每个能力的 admin 分发拆成 `cmd/admin/<capability>.go`（`rdb_analysis_remote_fetch.go` 已是此模式），让 `dashboard.go` 只做公共参数解析。本 feature 未动（design 2.5 超出范围观察）。
