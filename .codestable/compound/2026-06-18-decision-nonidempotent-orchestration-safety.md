---
doc_type: decision
category: constraint
slug: nonidempotent-orchestration-safety
status: active
created: 2026-06-18
source: feature/2026-06-18-pitr-server-flashback
applies_to: [topom, dashboard]
tags: [pitr, orchestration, safety, lock, snapshot, process-management]
---

# 非幂等高危编排的安全约束

## 背景

PITR（数据闪回）是 dashboard 第一次编排"停服 → 改文件 → 重启"这种带不可逆副作用的运维状态机。在此之前的 dashboard 编排（slot 迁移、group promotion、ACL 同步）要么幂等可重试，要么失败可观察回退。PITR 不具备这些性质——重复执行会把数据往更早的时间点再截一次，截断失败可能损坏 AOF，重启依赖外部进程。

在 design + 两轮 review 中针对这类高危编排确立了一组安全约束。它们不是 PITR 专属的——**任何"非幂等、带文件副作用、依赖外部进程"的 dashboard 编排都应遵守**。

## 决定

### 约束 1：非幂等编排的 per-resource lock 必须持续到 operator 显式确认才释放

**不**在 job 到达 terminal（succeeded/failed/cancelled）状态时释放 lock。Lock 持续到 operator 显式调用 Remove（`PUT .../remove`）才释放。

- 理由：PITR 不幂等。一个 terminal 但未被运维确认结果的 job，如果立刻释放 lock，下一个 job 可能对同一 server 再截一次（数据往更早时间点再截，或对已截断 AOF 再截报错）。operator 必须先确认恢复结果（比如读回 key 验证）再 Remove，才能起新 job。
- 考虑过的替代：terminal 即释放 lock + 靠 terminal job 历史防重入——被否决，因为"历史"本身没有强制确认机制，operator 容易忽略。
- 影响：Remove 必须拒绝 running job（"Cancel it first"），否则会静默中止 in-flight 截断并掩盖结果。

### 约束 2：snapshot 用 targeted copy + pre/post stat diff，不用全目录 cp -r

对"就地改文件"类操作（如 `redis-check-aof --truncate-to-timestamp` 的 `ftruncate`），snapshot **只拷真实写入面**（manifest + 最后一个被改的文件），加 pre/post 双向 stat diff 兜底模型漂移。

- 理由 1（targeted copy）：全目录 `cp -r` 会在恢复窗口内把磁盘占用翻倍，**磁盘打满本身就是恢复工具最不该触发的故障**——把一个可恢复场景变成更糟的。targeted copy 成本与风险面精确匹配。
- 理由 2（stat diff）：`redis-check-aof` 只对最后一个文件 `ftruncate`、不改更早文件、不重写 manifest（已源码验证 `checkMultiPartAof`）——但这是"我们的模型"，不能盲信。pre/post stat diff（记录所有文件 size+mtime，truncate 后复查，非最后文件变化即归"不确定"失败）近零成本兜底版本漂移/bug。
- 考虑过的替代：全目录 `cp -r` 买"防御深度"——被否决，因为 stat diff 用更低成本拿到了同样的保险，还能主动发现模型失效，比闷头多拷一份强。
- 影响：targeted copy 要求先源码确认工具的真实写入面（本例是读 `checkMultiPartAof` 验证只动最后文件），否则 snapshot 会漏。

### 约束 3：dashboard 绝不 fork redis-server，进程生命周期交给外部监管系统

Dashboard 不直接 `exec redis-server` 拉起 Redis 进程。重启靠外部进程管理器（systemd/supervisor/k8s）拉起 + dashboard 轮询 `INFO loading` 确认上线。可选的 `pitr_restart_command`（运维配置的 shell 命令）只是同机部署时"踢一脚"的便利项，踢完照样轮询。

- 理由 1：进程监管能力（crash 自动重启、cgroup 资源限制、ulimit、日志重定向、numa 绑定、用户降权）是 fork 给不了的。dashboard fork 出来的 redis 是 dashboard 的子进程——dashboard 自己重启或崩溃，这台生产 redis 跟着死或变孤儿。
- 理由 2：dashboard 无法忠实重建启动命令。`CONFIG GET port/bind/*file` 拿不到 unixsocket、logfile、daemonize、确切二进制路径、env、启动用户、ulimit。靠它拼启动命令注定不全。
- 理由 3：这类操作本就是人发起的高危低频操作，full automation 的价值点是"正确和安全"，不是省掉进程管理那一步。fork 不买任何东西。
- 考虑过的替代：dashboard 直接 `exec redis-server` + CONFIG GET 拼参数——被否决，理由 1+2。
- 影响：dashboard 编排"重启"步骤的设计是"等外部拉起 + 轮询"为默认主路径，restart_command 为可选便利项。

## 后果与边界

- 这三条约束适用于**非幂等、带文件副作用、依赖外部进程**的编排。对幂等可重试的编排（slot 迁移、ACL 同步）不强制——那些 terminal 即释放 lock 是合理的。
- 约束 1 的"持续到 Remove"有代价：operator 忘了 Remove，该 server 会被永久锁住（无法再起 PITR job）。这是有意的——宁可要运维显式确认，也不要静默重入。dashboard 重启会清空进程内 job registry（lock 也清空），这是逃生口。
- 约束 2 的 targeted copy 依赖"先读工具源码确认写入面"——换工具或工具升级时要重新核验，stat diff 是兜底不是替代。

## 相关文档

- `.codestable/features/2026-06-18-pitr-server-flashback/pitr-server-flashback-design.md`（design 决策 7/8/9 的详细论证）
- `.codestable/architecture/ARCHITECTURE.md`（PITR 术语条目 + 已知约束已引用这些约束）
