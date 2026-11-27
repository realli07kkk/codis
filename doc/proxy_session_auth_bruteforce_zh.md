# Codis Proxy AUTH 防暴力破解

Codis Proxy 支持对客户端 AUTH 失败按来源 IP 做临时锁定，覆盖 `session_auth` 单密码模式和 Codis ACL 模式。该能力默认关闭。

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

在单密码模式下，`session_auth = ""` 表示客户端无需认证，AUTH 不计入失败次数。启用 Codis ACL 后，guard 按 ACL 认证的结果计数，与 `session_auth` 的值无关。

## 行为边界

- 保护范围只覆盖 Codis Proxy 的客户端 `AUTH` 命令，不影响 `product_auth`、dashboard/topom `xauth`、etcd 连接文件里的 coordinator 凭据或后端 Redis auth。
- 锁定状态只存在于当前 proxy 进程内，不写入 coordinator，不跨 proxy 同步，proxy 重启后清空。
- proxy 会对内部 tracked IP 状态表设置固定上限。插入新来源 IP 前会清理过期记录；仍达到上限时优先淘汰最旧的未锁定记录，避免该功能变成无界内存记录器。
- 锁定期间，单密码模式拒绝该 IP 未认证连接的 `AUTH`。ACL 模式还会拒绝已认证连接的重新 `AUTH`。
- IP 锁定保留已有会话身份。ACL 模式下被拒绝的重新 `AUTH` 也保留原身份。
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
