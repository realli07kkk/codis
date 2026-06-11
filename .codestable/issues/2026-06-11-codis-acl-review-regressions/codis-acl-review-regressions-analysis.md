---
doc_type: issue-analysis
issue: 2026-06-11-codis-acl-review-regressions
status: confirmed
root_cause_type: state-pollution
related: [codis-acl-review-regressions-report.md]
tags: [acl, proxy, topom, security, concurrency]
---

# Codis ACL Review Regressions 根因分析

## 1. 问题定位

| 关键位置 | 说明 |
|---|---|
| `pkg/proxy/backend.go:504` | `sharedBackendConnPool.pool` 是普通 map，`GetOrCreate` 写入时没有内部锁。 |
| `pkg/proxy/router.go:178` | `userBackendConn` 获取 user pool 后释放 `userPools.mu`，随后才调用 `pool.GetOrCreate(addr)`，同一用户多 session 可并发写 pool map。 |
| `pkg/proxy/forward.go:56` | forward path 默认 `forward2` 一定返回 backend conn；user pool 被关闭后该假设不成立，可能注册 waitgroup 后返回 nil backend。 |
| `pkg/proxy/router.go:127` | `SetACL` 在 enabled/revision 变化时只 close user pools 和 mark stale，不清理 session legacy `authorized` 状态。 |
| `pkg/proxy/session_auth.go:92` | ACL AUTH 失败只在 `!wasAuthorized` 时记录 brute-force failure，已认证 session re-auth 失败被跳过。 |
| `pkg/topom/topom_acl.go:62` | `UpdateACL` 是 Redis ACL 下发唯一入口，后续 Redis 重启或新 server 加入缺少重同步入口。 |
| `pkg/topom/topom_group.go:104` | `GroupAddServer` 只更新 group，不向新 server 下发当前 ACL。 |
| `pkg/topom/topom_acl.go:311` | `ensureBackendServiceUser` 使用 `>` 明文 password token 下发服务账号密码。 |

## 2. 失败路径还原

**正常路径**：topom 更新 ACL -> 同步所有 Redis Server -> 写 coordinator revision -> 下发 proxy snapshot -> 客户端 AUTH 后保存 ACL identity -> 普通命令使用 user-bound backend connection，由 Redis backend 执行 ACL。

**失败路径**：

- 并发路径：多个已认证 session 同时转发到不同 addr -> `userBackendConn` 取到同一个 user pool -> 并发 `GetOrCreate` -> 同一个 map 被并发写入或 close 遍历。
- 关闭竞争路径：ACL revision 切换关闭 user pool -> 已在 forward path 中的请求拿不到 user-bound backend -> 原代码仍注册 slot waitgroup 并可能向 nil backend 转发。
- 运行态同步路径：ACL 已写入 coordinator -> Redis Server 重启或新 server 加入 -> Redis 内存 ACL 用户丢失 -> topom 没有重推 -> user-bound backend AUTH / migration auth 失败。
- 授权污染路径：ACL session 成功 AUTH 后 `authorized=true` -> 运维把 ACL disabled -> `handleRequest` 进入 legacy 分支 -> `isAuthorized()` 为 true，跳过 `session_auth` 检查。
- 暴力破解路径：低权限用户已认证 -> 反复 `AUTH other wrong` -> ACL path 保留旧身份但不记录失败 -> guard 永不锁定。
- 明文泄露路径：topom 每次 Redis ACL sync 都执行 `ACL SETUSER svc ... >password` -> 目标端 MONITOR / 命令观测能看到明文。

**分叉点**：ACL 首版把 new user pool、session authorized 标志和 Redis runtime ACL 当作局部状态处理，没有补齐跨 goroutine / 跨运行态切换的生命周期收口。

## 3. 根因

**根因类型**：state-pollution + concurrency + missing-guard

**根因描述**：user-bound backend connection 引入了按用户动态创建的 pool，但复用了原 shared pool “外部路由锁保护写入”的隐含约定；该约定对 user pool 不成立。ACL session 同时复用了 legacy `authorized` 布尔位，ACL disabled 时没有把 ACL 身份和 legacy 授权状态同时清掉。topom 把 ACL 下发绑定在“修改配置”这一条路径，没有把 Redis ACL 的运行时态恢复建模成单独操作。

**是否有多个根因**：是。主根因是 ACL 运行时状态生命周期不完整；次根因是认证失败计数沿用了单密码模型下“已认证 session 不受影响”的旧语义。

## 4. 影响面

- **影响范围**：仅影响启用 Codis-managed Redis ACL 的集群；默认关闭路径不受影响。
- **潜在受害模块**：proxy backend 连接池、proxy session auth、topom ACL API、group add server、admin CLI ACL 操作。
- **数据完整性风险**：无直接数据损坏；但 ACL 失效可能导致业务请求失败或权限扩大。
- **严重程度复核**：维持 P1。ACL 是安全功能，启用前必须修；但默认关闭且不影响旧 `session_auth` 集群。

## 5. 修复方案

### 方案 A：最小生命周期补齐

- **做什么**：
  - 给 `sharedBackendConnPool` 增加内部锁和 closed 标记，`GetOrCreate` / `Retain` / `Get` / `Close` 都由 pool 自己保护；关闭后的 user pool 不再创建新连接。
  - forward path 在拿不到 user-bound backend 时按 slot not ready 返回，不注册 request waitgroup。
  - `SetACL` 在 ACL disabled 时清理当前 proxy 的 ACL session 身份和 legacy `authorized` 状态。
  - ACL AUTH 失败总是记录 brute-force failure，但仍保留已有 ACL identity 以符合 Redis re-auth 失败语义。
  - 增加 topom `SyncACL` / API / admin CLI 入口，用当前 store ACL 重新同步 Redis 和 proxy；`GroupAddServer` 对新 server 先同步当前 ACL 再写 group。
  - service user 改用 `#sha256` token 下发。
- **优点**：改动范围小，直接修复已确认问题，不改变 ACL source-of-truth 和 revision 模型。
- **缺点 / 风险**：Redis 重启恢复需要运维显式触发 `acl-sync`，不是后台自动对账。
- **影响面**：`pkg/proxy/backend.go`、`pkg/proxy/router.go`、`pkg/proxy/forward.go`、`pkg/proxy/client_list.go`、`pkg/proxy/session_auth.go`、`pkg/topom/topom_acl.go`、`pkg/topom/topom_group.go`、`pkg/topom/topom_api.go`、`cmd/admin/main.go`、`cmd/admin/dashboard.go` 及相关测试。

### 方案 B：后台周期对账

- **做什么**：topom 启动后台 goroutine 周期扫描 Redis ACL runtime 并重推不一致状态。
- **优点**：Redis 重启可自动恢复，运维负担低。
- **缺点 / 风险**：需要定义漂移检测、重试退避、日志降噪和长耗时网络 IO 生命周期，首个修复 PR 影响面偏大。
- **影响面**：topom 主循环、stats、ACL sync 状态和测试都会扩大。

### 方案 C：依赖 Redis aclfile 持久化

- **做什么**：配置 Redis `aclfile` 并在 ACL 更新后执行 `ACL SAVE`。
- **优点**：Redis 重启自动恢复。
- **缺点 / 风险**：涉及 Redis 配置兼容性、requirepass/aclfile 组合约束、文件权限和部署迁移，不适合作为小修。
- **影响面**：Redis config、文档、topom sync、部署脚本。

### 推荐方案

**推荐方案 A**，理由：它直接修复 review 中的 blocker 和生产前必修安全口子，同时保留当前设计的 source-of-truth 和 fail-closed 方向。自动周期对账和 `aclfile` 可以作为后续运维增强，不应混入这次安全修复。
