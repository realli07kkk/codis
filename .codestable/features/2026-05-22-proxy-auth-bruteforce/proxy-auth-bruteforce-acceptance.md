---
doc_type: feature-acceptance
feature: 2026-05-22-proxy-auth-bruteforce
status: current
accepted_at: 2026-05-22
summary: Codis Proxy 已按设计新增默认关闭的 session_auth 防暴力破解能力，架构与 requirement 已回写
tags: [proxy, auth, security, redis-protocol, acceptance]
---

# proxy-auth-bruteforce 验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-05-22
> 关联方案 doc：`.codestable/features/2026-05-22-proxy-auth-bruteforce/proxy-auth-bruteforce-design.md`

## 1. 接口契约核对

对照方案第 2.1 节名词层逐一核查：

- [x] 配置契约落地：`config/proxy.toml` 和 `pkg/proxy/config.go` 均新增 `session_auth_bruteforce_enabled=false`、`session_auth_bruteforce_max_failures=5`、`session_auth_bruteforce_lock_duration="60s"` 默认值。
- [x] 配置校验落地：`Config.Validate()` 只在启用 guard 时要求 `max_failures > 0` 和 `lock_duration > 0`，未强制要求 `session_auth != ""`，保留 CLI 覆盖空间。
- [x] 运行态契约落地：`pkg/proxy/auth_bruteforce.go` 定义 `AuthBruteforceGuard`、`authFailureRecord` 和 `AuthBruteforceStats`，记录 `failures`、`lastFailureAt`、`lockedUntil`。
- [x] guard API 边界落地：`BeforeAuth(remoteAddr, authorized, now)`、`RecordAuthFailure(remoteAddr, authorized, now)`、`RecordAuthSuccess(remoteAddr)` 和 `Stats()` 由 guard 内部完成 active 判断和 IP 解析，`Session` 不持有状态表细节。
- [x] Redis `AUTH` 响应契约落地：参数错误、未配置 `session_auth`、密码错误、密码正确的原有错误顺序保留；新增 locked error 仅出现在启用 guard 且未认证 session 的 IP 被锁定时。
- [x] 聚合观测契约落地：`Stats.SessionAuthBruteforce` 暴露 `enabled/tracked_ips/locked_ips/failures/locks/unlocks`，不输出完整 IP 列表和密码。

## 2. 行为与决策核对

**需求摘要逐项验证**：

- [x] 开启配置后，同一客户端 IP 连续输错 `AUTH` 达到阈值会被临时锁定。
- [x] 锁定期间，该 IP 的新未认证连接即使提交正确密码也返回 locked error。
- [x] 超过 `session_auth_bruteforce_lock_duration` 后，下一次 `AUTH` 自动解除锁定并重新校验密码。
- [x] 已经认证成功的 session 不因同 IP 后续锁定而关闭、降级或返回 `NOAUTH`。
- [x] 默认关闭时保持现有 `AUTH` 行为，不生成 locked error。

**明确不做逐项核对**：

- [x] 未修改 `product_auth`、dashboard/topom `xauth`、coordinator/Jodis auth 或后端 Redis auth。
- [x] 未把锁定状态写入 coordinator、`models.Store` 或 Jodis。
- [x] 未新增跨 proxy RPC、pubsub 或广播同步失败计数。
- [x] 未主动关闭已有已认证 session，未让普通命令在 IP 锁定后返回 `NOAUTH`。
- [x] 未新增 dashboard/FE 管理页面、手动解锁 API、allowlist/denylist/CIDR 配置。
- [x] 未引入 Redis ACL username 语义，未改变当前 `AUTH <password>` 契约。
- [x] 日志和 stats 未输出客户端提交的密码，stats 未列出完整 IP 明细。

**关键决策落地**：

- [x] D1：保护挂在 proxy session auth 路径。代码只在 `Session.handleAuth` 和 proxy session 创建路径接入 guard，不进入 `Router.dispatch`、backend 连接池或迁移逻辑。
- [x] D2：默认关闭配置组。默认配置和 `DefaultConfig` 都是 disabled，新增校验只在 enabled 时生效。
- [x] D3：进程内 IP 失败记录和自动失效。guard 状态由 `Proxy` 持有，新 session 共享同一 guard；成功认证、锁定到期和记录过期会清理。
- [x] D4：锁定只拦截未认证 session 的 `AUTH`。`authorized=true` 时 `BeforeAuth` 和 `RecordAuthFailure` no-op，已有已认证 session 可继续普通命令。
- [x] D5：只暴露聚合观测。stats 没有 IP 列表，文档明确不把它当分布式撞库防护。

**实现阶段 review 问题回归**：

- [x] Critical：无界 `records` map 已修复为内部固定 tracked IP 上限；插入新 IP 前清理过期记录，满载时淘汰最旧未锁定记录，全部锁定时拒绝新增 tracking。
- [x] Warning：Session 层的 guard 特例已收束，IP 提取、active/nil 判断和解析失败处理在 guard 内部完成。
- [x] Warning：`firstFailureAt` 冗余字段未保留；实际记录只保存 `failures`、`lastFailureAt`、`lockedUntil`。

**挂载点反向核对**：

- [x] `pkg/proxy/config.go` / `config/proxy.toml`：新增配置项、默认值和校验。
- [x] proxy runtime lifecycle：`Proxy.New` 创建 guard，`serveProxy` 创建 session 时传入共享 guard。
- [x] `pkg/proxy/session.go`：`handleAuth` 在密码比较前后挂入 locked check、failure record 和 success reset。
- [x] `pkg/proxy/proxy.go` stats：新增 `session_auth_bruteforce` 可选聚合字段。
- [x] 用户文档：新增 `doc/proxy_session_auth_bruteforce_zh.md`。
- [x] 反向 grep：`AuthBruteforce` / `session_auth_bruteforce` 命中点均落在上述挂载点、测试、feature 文档和验收回写文档内。
- [x] 拔除沙盘推演：删除新配置字段、`auth_bruteforce.go`、session/proxy hook、stats 字段、测试和文档即可卸载；未发现 coordinator、dashboard、backend 或 router 残留挂载。

## 3. 验收场景核对

- [x] 默认配置或 disabled 下，错误 `AUTH` 返回现有 `ERR invalid password`，不会生成 locked error。
  - 证据来源：`TestSessionAuthBruteforceDefaultDisabledKeepsAuthErrors`。
- [x] 启用 guard、`max_failures=3` 时，同一 IP 连续 3 次 `AUTH wrong` 后进入 locked 状态。
  - 证据来源：`TestAuthBruteforceGuardLockUnlockAndSuccessReset`、`TestSessionAuthBruteforceLocksAndUnlocks`。
- [x] 锁定期间，新未认证连接或当前未认证连接执行 `AUTH secret` 返回 locked error，不设置 authorized。
  - 证据来源：`TestSessionAuthBruteforceLocksAndUnlocks`。
- [x] 锁定超过 `lock_duration` 后，同一 IP 可重新认证并返回 `OK`。
  - 证据来源：`TestAuthBruteforceGuardLockUnlockAndSuccessReset`、`TestSessionAuthBruteforceLocksAndUnlocks`。
- [x] 未达阈值前正确 `AUTH` 会清零失败计数，之后重新累计。
  - 证据来源：`TestSessionAuthBruteforceSuccessClearsFailureCount`。
- [x] 不同 IP 的失败计数和锁定状态互相隔离。
  - 证据来源：`TestAuthBruteforceGuardLockUnlockAndSuccessReset`。
- [x] 大量不同 IP 失败不会让 `tracked_ips` 超过内部上限；过期记录会在后续新 IP 插入前被清理。
  - 证据来源：`TestAuthBruteforceGuardBoundsTrackedIPs`、`TestAuthBruteforceGuardCleansExpiredBeforeInsert`。
- [x] 同 IP 已认证 session 不因新连接触发锁定而受影响。
  - 证据来源：`TestProxyAuthBruteforceAuthorizedSessionUnaffected`。
- [x] `session_auth=""` 且 guard enabled 时，`AUTH` 仍返回 no-password error，不记录 failure。
  - 证据来源：`TestSessionAuthBruteforceNoSessionAuthNoFailureRecord`。
- [x] `AUTH` 参数数量错误时沿用 wrong-arity error，不记录 failure。
  - 证据来源：`TestSessionAuthBruteforceWrongArityDoesNotRecordFailure`。
- [x] 无法解析 remote IP 的输入不触发锁定，回退现有 AUTH 行为。
  - 证据来源：`TestAuthBruteforceClientIP`、`TestAuthBruteforceGuardSkipsEmptyIP`。
- [x] proxy stats 启用或已有统计时包含 `session_auth_bruteforce` 聚合字段，不包含密码和 IP 列表。
  - 证据来源：`TestProxyAuthBruteforceStats`、代码 review。
- [x] 新增 proxy 测试和全量 cmd/pkg 测试通过。
  - 证据来源：下方验证命令。

执行过的验证命令：

```bash
python3 .codestable/tools/validate-yaml.py --file .codestable/features/2026-05-22-proxy-auth-bruteforce/proxy-auth-bruteforce-checklist.yaml --yaml-only
go test ./pkg/proxy -run 'TestAuthBruteforce|TestSessionAuthBruteforce|TestProxyAuthBruteforce' -count=1
go test -race ./pkg/proxy -run TestAuthBruteforceGuardConcurrentRecords -count=1
go test ./pkg/proxy -count=1
make gotest
git diff --check
```

## 4. 术语一致性

- `session_auth_bruteforce_*`：配置项、`Config` 字段、stats JSON、用户文档和测试命名一致。
- `AuthBruteforceGuard`：只表示 proxy 进程内防暴力破解运行态，不误称为 ACL、backend auth 或 coordinator auth。
- `Client IP` / remote IP：设计、实现和文档都明确来自连接 remote address；不解析 `X-Forwarded-For`、Proxy Protocol 或客户端自报身份。
- `Locked IP`：语义限定为未认证 session 的 `AUTH` 被拦截，不表示连接被踢出或普通命令被限流。
- 防冲突：`product_auth`、`xauth`、Jodis/coordinator auth、后端 Redis auth、Redis ACL username 语义均未复用该术语。

## 5. 架构归并

- [x] `.codestable/architecture/ARCHITECTURE.md` 已新增 `Session auth brute-force guard` 术语。
- [x] 结构与交互已补充 `AUTH` 是 proxy 本地 session auth 流程，启用 guard 后按 remote IP 临时锁定未认证 session。
- [x] 数据与状态已补充 guard 由 proxy 持有、按 IP 记录 `failures/lastFailureAt/lockedUntil`、有内部 tracked IP 上限、清理和淘汰策略。
- [x] stats 描述已补充 `session_auth_bruteforce.enabled/tracked_ips/locked_ips/failures/locks/unlocks`，并明确不包含完整 IP 列表和密码。
- [x] 代码锚点已补充 `pkg/proxy/auth_bruteforce.go`。
- [x] 已知约束已补充默认关闭、只覆盖 `session_auth`、不跨 proxy、不持久化、NAT/shared IP 边界、已有已认证 session 不受影响。
- [x] 相关文档已补充本 feature design/acceptance 和用户文档链接。

## 6. requirement 回写

- [x] design frontmatter 指向 `requirement: redis-cluster-service`，该 req 当前为 `current`。
- [x] `.codestable/requirements/redis-cluster-service.md` 已新增平台维护者视角的 `session_auth` 连续错误认证临时锁定用户故事。
- [x] “怎么解决”已补充 proxy 本地 `session_auth` 防暴力破解 guard、按 remote IP 记录失败、自动解锁和已有 session 不受影响。
- [x] “实现进展”已追加 2026-05-22 `session_auth` 防暴力破解能力。
- [x] “边界”已补充默认关闭、单 proxy、单 remote IP、非分布式撞库防护、不解析转发头、不提供 dashboard 管理页或手动解锁 API。

## 7. roadmap 回写

- [x] design frontmatter 没有 `roadmap` / `roadmap_item` 字段。

结论：本 feature 非 roadmap 起头，不需要更新 `.codestable/roadmap/*`。

## 8. attention.md 候选盘点

- [x] 本 feature 未暴露新的通用构建、测试、路径或环境陷阱。

结论：不更新 `.codestable/attention.md`。现有 `attention.md` 中的 `python3` 和 `make gotest` 约束仍适用。

## 9. 遗留

- 已知限制：该能力是单 proxy 进程内、remote IP 维度的防护，不做跨 proxy / 重启后累计，不承诺抵御分布式低频撞库。
- 已知限制：NAT、四层代理或负载均衡场景下，多个真实客户端可能在 proxy 视角共享同一来源 IP。
- 已知限制：首版没有 dashboard/FE 管理页、手动解锁 API、allowlist/denylist/CIDR、Proxy Protocol / `X-Forwarded-For` 解析或 Redis ACL username 维度。
- 后续优化点：如果继续增加 session 本地安全策略，建议另走 `cs-refactor` 把认证相关本地命令处理从 `session.go` 中进一步拆出；本 feature 不阻塞在该重构上。

验收通过。实现与设计一致，review 发现的无界 map 和 guard 边界问题已修复，架构与 requirement 已回写，checklist checks 已全部为 `passed`。
