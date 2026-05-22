# Codis Proxy AUTH 防暴力破解

Codis Proxy 支持对客户端 `session_auth` 认证失败按来源 IP 做临时锁定。该能力默认关闭，用于降低单个 IP 高频尝试 Redis `AUTH <password>` 的风险。

## 配置

在 proxy 配置中开启：

```toml
session_auth = "secret"

session_auth_bruteforce_enabled = true
session_auth_bruteforce_max_failures = 5
session_auth_bruteforce_lock_duration = "60s"
```

配置含义：

- `session_auth_bruteforce_enabled`：是否开启本 proxy 进程内的 session AUTH 失败保护，默认 `false`。
- `session_auth_bruteforce_max_failures`：同一客户端 IP 在当前失败周期内允许的连续错误次数。达到该次数后，该 IP 会被锁定。
- `session_auth_bruteforce_lock_duration`：锁定持续时间。超过该时间后，下一次 `AUTH` 会自动解除锁定并重新校验密码。

如果 `session_auth = ""`，proxy 不要求客户端认证，此保护逻辑不记录失败次数。

## 行为边界

- 保护范围只覆盖 Codis Proxy 的客户端 `AUTH` 命令，不影响 `product_auth`、dashboard/topom `xauth`、coordinator/Jodis auth 或后端 Redis auth。
- 锁定状态只存在于当前 proxy 进程内，不写入 coordinator，不跨 proxy 同步，proxy 重启后清空。
- proxy 会对内部 tracked IP 状态表设置固定上限。插入新来源 IP 前会清理过期记录；仍达到上限时优先淘汰最旧的未锁定记录，避免该功能变成无界内存记录器。
- 锁定期间，该 IP 的未认证连接执行 `AUTH` 会返回错误，即使密码正确也不能继续认证。
- 已经认证成功的会话不受后续 IP 锁定影响，不会被主动关闭，也不会被降级成 `NOAUTH`。
- 如果 proxy 前面有 NAT、四层负载均衡或代理，多个真实客户端可能在 proxy 视角共享同一个来源 IP。
- 首版不提供手动解锁 API、allowlist/denylist、CIDR 规则或 Redis ACL username 维度。

## 观测

开启该能力或已有统计时，proxy stats JSON 中包含 `session_auth_bruteforce` 字段，可查看：

- `enabled`
- `tracked_ips`
- `locked_ips`
- `failures`
- `locks`
- `unlocks`

stats 不包含完整 IP 列表，也不会输出客户端提交的密码。锁定和自动解锁事件会写入 proxy 日志，日志中只包含来源 IP、失败次数和锁定截止时间。
