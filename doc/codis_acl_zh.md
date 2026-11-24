# Codis ACL 管理

Codis 支持由 dashboard/topom 统一管理 Redis 8 的 ACL 用户与规则，并让客户端继续连接 `codis-proxy`、使用 Redis 8 风格的多账号认证（`AUTH password` 或 `AUTH username password`）。该能力默认关闭；未启用时沿用 `session_auth` 单密码行为。

## 配置

proxy 与 dashboard 的配置项：

```toml
# codis-proxy
codis_acl_enabled = false
backend_auth_username = ""
backend_auth_password = ""

# codis-dashboard
backend_auth_username = ""
backend_auth_password = ""
```

Redis 8 Codis Server 的迁移认证配置在 `redis.conf`：

```text
codis-migration-auth-user ""
codis-migration-auth-pass ""
```

配置含义：

- `codis_acl_enabled`：proxy 是否启用 Codis ACL 客户端认证，默认 `false`。为 `false` 时完全走 `session_auth`；该键只在 proxy 侧生效，修改后需要重启 proxy。
- `backend_auth_username` / `backend_auth_password`：dashboard 和 proxy 连接 Redis 8 Codis Server 时使用的服务身份。两者都为空时沿用 `product_auth`（发送单参数 `AUTH <product_auth>`）；只配 password 时按 `default` 用户发送 `AUTH <password>`；两者都配时发送 `AUTH <username> <password>`；只配 username 时配置校验失败。该身份用于 `INFO`、slot 迁移、`ACL SETUSER`、`ACL DRYRUN`、stats 等内部动作，不等同于业务账号。
- `backend_number_databases`：proxy 允许的后端 db 数量。绑定 db 的 ACL 用户认证时用它做上限校验，越界时 fail-closed。
- `codis-migration-auth-user` / `codis-migration-auth-pass`：Redis 8 Codis Server 同步/异步迁移连接目标时使用的认证。两者都为空时保持 `requirepass` 兼容；只配 password 时按 `default` 用户认证；两者都配时使用 `AUTH user pass` 或 `SLOTSRESTORE-ASYNC-AUTH2 user pass`。
- coordinator 没有额外的 ACL 配置键；ACL 目标状态保存在 `/codis3/{product}/acl`。

topom 会拒绝与 `backend_auth_username` 同名的业务 ACL 用户。

## 架构与数据流

| 组件 | 职责 |
| --- | --- |
| coordinator | 保存 ACL 目标状态和单调递增的 revision，路径为 `/codis3/{product}/acl`。 |
| dashboard/topom | ACL 的唯一配置源：校验用户、同步 Redis Server、写 coordinator、下发 proxy。 |
| codis-proxy | 用本地 snapshot 校验客户端 `AUTH`，按用户身份选择后端连接并执行 `ACL DRYRUN`。 |
| Redis 8 Codis Server | 保存 runtime ACL 用户，执行期按 Redis ACL 规则判断命令和 key 权限。 |
| Codis FE / codis-admin | 只调用 dashboard/topom ACL API 的配置界面和脚本入口，不直连 proxy 或 Redis。 |

ACL 模型：

| 字段 | 含义 |
| --- | --- |
| `revision` | ACL 单调版本号，每次成功写入 coordinator 时递增。Redis 逐条应用 ACL 命令，proxy 在目标 revision 写入后接收 snapshot。 |
| `enabled` | 是否启用 ACL 认证。关闭后用户和规则仍保留，但 proxy 不再用 ACL 校验客户端。 |
| `users[].name` / `enabled` | 用户名和启用状态；禁用用户认证统一返回 `WRONGPASS`。 |
| `users[].password_hashes` | Redis ACL SHA-256 十六进制 hash 列表，不保存明文。 |
| `users[].rules` / `db` | Redis ACL rule token 列表；`db` 为可选的固定 db 绑定，`nil` 表示未绑定。 |

下发与同步：

1. FE 或 `codis-admin` 提交完整用户列表；topom 校验后生成 `revision = 当前 revision + 1`。
2. topom 用服务身份连接当前 product 的所有 Redis Server，执行 `ACL SETUSER <name> reset on|off #<hash> <rules...>`；配置了服务身份时，服务账号由 `ensureBackendServiceUser` 每轮重设为 `+@all ~*`；被移除的用户执行 `ACL DELUSER`，被移除的 `default` 用户改为 `off nopass`。
3. 所有 Redis Server 成功后，topom 才通过 `storeUpdateACL` 写 coordinator，再通过 `syncACLToProxies` 调用每个 online proxy 的 `PUT /api/proxy/acl/:xauth`。
4. proxy 的 `Router.SetACL` 以 copy-on-write 方式安装新 snapshot，关闭旧 user-bound backend pool，并把旧 revision 的已认证 session 标记为 stale。
5. Redis 同步遇错即停止，coordinator 和 proxy 保持旧 revision。此前已成功执行的 ACL 命令会留在 Redis 上，同一实例也可能只更新了部分用户。若 Redis 全部更新后 coordinator 写入失败，Redis 同样保留已执行的修改。若失败发生在 proxy 下发阶段，coordinator 已保存新 revision，`sync_status` 显示 `proxy_sync_failed:<token,...>`。
6. 新增 Redis Server 时 `GroupAddServer` 会先同步当前 ACL；proxy online/reinit 时会先收到当前 ACL snapshot，再开始接受 ACL 认证。

回滚语义：重新提交旧 ACL 内容作为新 revision，proxy 不做本地手工回滚。

proxy 不实现 Redis ACL 规则解释器，授权判断分两路：

| 路径 | 连接身份 | 授权方式 |
| --- | --- | --- |
| 稳定 slot 普通客户端命令 | user pool，key 为 `addr + db + username + credential hash + revision` | Redis 后端按该用户 ACL 执行期判断。 |
| 本地 proxy 命令（`SELECT`、`CLIENT LIST`、`CLUSTER NODES`、`SLOTSMAPPING` 等）与迁移包装 | 本地处理；迁移包装使用 service pool | 先对原始命令执行 `ACL DRYRUN <username> <command> <args...>`，通过后本地返回或用服务身份执行包装命令。 |

hot key cache 命中前同样先做 `ACL DRYRUN`，未授权用户不能从 proxy 本地缓存读到 value。业务用户不需要 `SLOTSMGRT*`、`SLOTSRESTORE*`、`ACL SETUSER`、`CONFIG` 等内部权限，但需要拥有被执行的原始命令权限。

## 用户管理与 API/FE

管理 API（全部要求 `xauth`）：

| 接口 | 用途 |
| --- | --- |
| `GET /api/topom/acl/:xauth` | 读取 ACL view：revision、enabled、sync status 和 redacted 用户列表。 |
| `PUT /api/topom/acl/:xauth` | 整量提交 enabled 和用户列表，触发同步并生成新 revision。 |
| `PUT /api/topom/acl/sync/:xauth` | 按 coordinator 中已保存的期望状态重新同步 Redis Server 和 online proxy。 |
| `PUT /api/proxy/acl/:xauth` | topom 向单个 proxy 下发 ACL snapshot；不应由 FE 或业务客户端直接调用。 |

命令行入口：

```text
codis-admin --dashboard=ADDR --acl
codis-admin --dashboard=ADDR --acl-set=FILE [--confirm]
codis-admin --dashboard=ADDR --acl-sync
```

`--acl-set` 不带 `--confirm` 时只打印 redacted 预览，密码和新密码字段会被清空；确认后再提交。`--acl-sync` 是幂等的重推操作，用于 Redis 重启、节点新增或 proxy 下发失败后的恢复。

FE 入口位于 `安全与限流 / 访问控制` 页面，由 `cmd/fe/assets/index.html` 加载 `cmd/fe/assets/acl.js`。页面展示 revision、enabled、sync status 和用户表（用户名、状态、密码数量、规则、db 绑定），提交时整量提交用户列表；新密码只作为写入字段短暂存在，页面不会回显已保存的密码或完整 hash。ACL view 只包含 `revision`、`enabled`、`sync_status` 和每个用户的 `name`、`enabled`、`db`、`password_count`、`rules`、`last_error`（如有）。

输入校验：

| 输入 | 规则 |
| --- | --- |
| 用户名 | 匹配 `^[A-Za-z0-9_][A-Za-z0-9_.@-]*$`，且不能与服务身份同名。 |
| 规则 | 至少 1 条；`on` / `off` 由 enabled 字段管理，提交时忽略；`reset` / `resetpass` 由 Codis 管理，拒绝；`>明文密码` 拒绝；`#<hash>` 作为密码 hash 接收。 |
| 密码 | 64 位十六进制 SHA-256；没有 hash 时必须包含 `nopass`，否则拒绝。 |
| db | 非负整数或留空；topom 只校验 `db >= 0`。 |
| 重名 | 同一份提交中重复用户名直接拒绝。 |

`sync_status` 取值：

- `not_configured`：coordinator 中还没有 ACL 状态，或状态为 revision 0、disabled 且没有用户。
- `ready`：当前 topom 进程内没有记录到该 revision 的 proxy 下发失败；topom 刚重启、尚未重放时也会显示 `ready`。
- `proxy_sync_failed:<token,...>`：ACL 已写入 coordinator，但部分 proxy 下发失败；按 token 排查 proxy admin 连通性后重试。

`sync_status` 只反映 proxy 下发结果，不表示每台 Redis Server 上不存在人工漂移用户。

## Proxy 认证行为

- `AUTH password` 映射到 `default` 用户；`AUTH username password` 映射到指定用户。认证成功返回 `OK`，session 保存 username、credential hash、短期明文 password、revision 和可选 ForcedDB；明文只用于后端重连，不写日志、stats、coordinator 或 API 响应。
- 未知用户、禁用用户、错误密码统一返回 `WRONGPASS invalid username-password pair or user is disabled`，不写入新身份；未认证或 revision stale 时，非 `AUTH` 命令返回 `NOAUTH Authentication required`。ACL 关闭后回到 `session_auth` 路径。
- `ACL WHOAMI` 由 proxy 本地返回当前用户名；其他 ACL 子命令（包括 `ACL SETUSER`、`ACL DELUSER`、`ACL DRYRUN`）返回 `ERR unsupported ACL subcommand '<subcommand>'`，不会透传到后端。
- revision 切换后旧 user pool 被关闭，旧 session 下一条非 `AUTH` 命令返回 `NOAUTH`，客户端必须重新认证；关闭 ACL 时会清空所有 session 的 ACL 授权并回到 `session_auth` 路径，`session_auth` 为空时不要求认证。
- 失败的 re-AUTH 不会替换已有有效身份：如果 session 原本已认证成功，错误密码只返回 `WRONGPASS`，`ACL WHOAMI` 仍返回旧用户。
- 如果启用了 `session_auth_bruteforce_enabled`，ACL 认证失败同样进入按来源 IP 计数的 guard，成功认证会清理该 IP 的失败记录，详见 [Codis Proxy AUTH 防暴力破解](proxy_session_auth_bruteforce_zh.md)。

DB 绑定（DB-bound ACL user / Forced DB）：

| 用户配置 | AUTH 后行为 |
| --- | --- |
| `db` 未设置 | 行为不变：db 从 0 开始，`SELECT` 正常改变路由。 |
| `db = n` 且 `n < backend_number_databases` | session 记录 `ForcedDB=n` 并切到 db n；后续 `SELECT` 一律返回 `OK` 但不改变路由。 |
| `db = n` 且 `n >= backend_number_databases` | AUTH fail-closed，返回 `ERR acl user ... bound db ... exceeds backend db count ...`，不写身份、不落任何命令。 |

Forced DB 是 proxy 的路由属性，不会渲染进 Redis `ACL SETUSER`；迁移、`ACL DRYRUN`、stats 等内部路径使用各自构造的 db，不受 Forced DB 影响。`default` 用户只能绑定一个 db，所有 `AUTH password` 共享该绑定；不支持“同一用户不同密码绑不同 db”。

## 行为边界

- 不实现 Redis Cluster ACL、`MOVED` / `ASK`、Pub/Sub channel ACL 或 Redis module / Redis Stack 命令的完整 key-spec。
- 首版只支持 RESP2 下的 `AUTH [username] password`；不承诺 `HELLO 3` / RESP3 的完整 reply parity。
- ACL 允许不会扩大 proxy 的命令边界：原本不支持的命令、`CONFIG` / `SUBSCRIBE` / `MONITOR` 等危险命令仍按既有规则拒绝。
- 普通客户端不能通过 proxy 执行 ACL 管理命令；ACL 管理只走 dashboard/topom、`codis-admin` 和 FE。
- 不在 coordinator、日志、stats、API 响应或 FE 中保存或回显客户端明文密码，只保存 SHA-256 hash。
- ACL 只面向 Redis 8 backend；`codis_acl_enabled=false` 时保留旧 `session_auth` 与 `product_auth` 语义。
- 同一 product 由持有拓扑锁的 dashboard 管理。ACL 更新依次写入 Redis、coordinator，再下发 proxy，各阶段可能分别失败。
- 直连 Redis Server 手工执行 `ACL SETUSER` 会在下一次 topom 同步时被覆盖。proxy 管理面的下发模式与 [Codis Proxy QPS 限流](proxy_qps_rate_limit_zh.md) 类似：coordinator 保存目标状态，topom 负责 fan-out 和重放。

## 运维须知

**启用顺序**：

1. 在 dashboard 和所有 proxy 配置一致的服务身份；确认 dashboard 能连接所有 Redis Server。
2. 通过 FE 或 `--acl-set=FILE --confirm` 提交 ACL 用户和 `enabled=true`，确认 `sync_status=ready`。
3. 在所有 proxy 置 `codis_acl_enabled=true` 并重启 proxy（该配置只在启动时读取）。
4. 客户端改用 `AUTH username password`，用 `ACL WHOAMI` 验证身份，观察 `NOAUTH` / `NOPERM`。

回退时先提交 `enabled=false` 的 ACL（保留用户和规则），再把 proxy 的 `codis_acl_enabled` 改回 `false` 并重启，恢复到 `session_auth` 路径。

**Redis 重启后的恢复**：Redis 的 runtime ACL 默认只存在内存中。Redis 进程重启后，已同步的用户会消失；此时 proxy 仍能凭 snapshot 让客户端 `AUTH` 成功，但后端没有对应用户，命令会返回 `NOPERM`。如果关闭了 `default` 用户而依赖服务账号，重启后服务账号也会消失，dashboard/proxy 可能连不上后端。出现任一情况时，尽快执行 `codis-admin --dashboard=ADDR --acl-sync`。该命令按 coordinator 中保存的 revision 重推所有 Redis Server 和 online proxy，不依赖 Redis 侧状态，可重复执行。新增 Redis Server 会在加入 group 时自动同步，新 proxy 会在 reinit 时自动收到 snapshot。

**漂移用户清理**：`--acl-sync` 只按期望用户重推，不会枚举 Redis 侧的 `ACL LIST`。如果某台 Redis 在包含 `ACL DELUSER` 的 revision 中失联，后续 `--acl-sync` 不会删掉该 server 上已删除的用户。需要在对应 server 上手工执行 `ACL DELUSER <name>`，或重启该 Redis（runtime ACL 清空）后再执行 `--acl-sync`。

**密码轮换**：提交 `new_password` 或新的 `password_hashes` 会生成新 revision，旧 session 随即 stale，客户端需要重新 `AUTH`。密码不可读取，丢失后只能设置新密码。

**故障排查**：提交失败时检查 dashboard 返回的错误和当前 revision，并核查失败前已更新的 Redis 实例。修复故障后可重新提交目标 ACL，或用 `--acl-sync` 重推已保存的 revision。后者只重推期望用户，部分失败时新增但未写入 coordinator 的用户需要在 Redis 上核查并单独清理。`sync_status=proxy_sync_failed:<token>` 时检查对应 proxy 的 admin 地址和 xauth，再执行 `--acl-sync`。

**服务账号**：`backend_auth_username` 每轮同步都会被重设为 `reset on #<hash> +@all ~*`，以保证迁移和内部命令可用。不要把该账号用作业务账号，也不要把它写进 ACL 用户列表；妥善保护 dashboard/proxy 配置中的服务密码。

## 已知限制

- **Redis 重启不自动恢复**：runtime ACL 没有接入 `aclfile` 持久化，也没有后台周期对账；必须显式执行 `--acl-sync` 恢复。Redis 重启后到同步完成前，proxy 侧认证与后端权限可能不一致。
- **无法自动清理漂移用户**：`SyncACL` 不枚举 `ACL LIST`，某台 server 在删除用户的 revision 中失联后，该 server 上会残留已删除用户；需要手工 `ACL DELUSER` 或重启该 server 后再同步。
- **brute-force 可被规避**：guard 按来源 IP 计数，且任意成功 `AUTH` 会清空该 IP 的失败记录。持有有效账号的客户端可以用“失败 N-1 次 + 成功 1 次”的方式规避锁定；当前没有按 `(ip, username)` 维度计数或成功衰减机制。该能力只用于降低高频尝试风险，不是强认证防护。
- **user-bound backend pool 不在 KeepAlive 周期内**：`Router.KeepAlive` 只保活 service pool，ACL 用户的空闲后端连接依赖请求路径上的 `BackendConn` 自动重连；当前也没有 user pool 容量上限或 idle 淘汰。
- **db 上限只由 proxy 兜底**：topom 只能校验 `db >= 0`，拿不到各 proxy 的 `backend_number_databases`。提交 `db=99` 这类配置会成功，直到客户端 `AUTH` 时才被 proxy fail-closed 拒绝；部署时应保持所有 proxy 的 db 数量一致。
- **`AUTH password` 的报错与旧模式不同**：ACL 模式下 `default` 用户没有密码时，单参数 `AUTH password` 返回 `WRONGPASS`，而不是旧 `session_auth` 风格的 `ERR invalid password`；迁移客户端时需要识别新错误文本。
- **失败的 re-AUTH 保留旧身份**：已认证 session 再次 `AUTH` 失败不会清除原身份，`ACL WHOAMI` 仍返回旧用户。客户端不能用 `WHOAMI` 判断最近一次 `AUTH` 是否失败。
- **`SELECT` 的 OK 不代表切换生效**：绑定 db 的用户收到 `SELECT` 一律返回 `OK`，但实际仍路由到绑定 db；客户端不能靠 `SELECT` 的返回值判断当前 db。
- **`sync_status` 不覆盖 Redis 侧漂移**：它只表示当前 topom 进程记录的 proxy 下发结果；Redis Server 上的人工 `ACL SETUSER` / `ACL DELUSER` 不会被该字段发现，topom 重启后失败缓存也会清空并先显示 `ready`，需要结合 `--acl-sync` 返回和实际 `ACL LIST` 判断。
- **其它未纳入的能力**：`ACL DRYRUN` 热路径优化、user pool 容量与 idle 淘汰、`sync_status` 漂移观测细化、FE 的 ACL 规则 DSL 编辑器均未实现；FE 只提供结构化字段和 raw rule token 输入。

## 真机验证脚本

- API 级：[`../scripts/validation/acl/run_acl.py`](../scripts/validation/acl/run_acl.py)，18 个 case：启用、后端 Redis 同步、`NOAUTH` / 权限拒绝、`db` 绑定、改密、`db=99` fail-closed、`--acl-sync`、master 重启后重同步、清理（`acl-cleanup` 在 `finally` 中执行）。
- 浏览器：[`../scripts/validation/browser/tests/acl.spec.js`](../scripts/validation/browser/tests/acl.spec.js)，覆盖新增 / 删除用户的 FE 交互，并与 `GET /api/topom/acl/<xauth>` 交叉核对。
- 证据：`run_acl-<run-id>.json`（默认 `.runtime/realmachine/`），退出码 0 = passed、1 = failed、2 = 仅 blocked / skipped。
- 实测差异：明文密码规则被拒的实际文案是 `acl user <name>: plain password token is not allowed`，不是 `plaintext ...`。
- 实测差异：Redis master 重启后 runtime ACL 丢失，proxy 上以该用户 `AUTH` 仍成功，但后续命令实际返回 `ERR handle response, backend conn failure, EOF`（proxy 为 ACL 用户单独建后端连接并以该用户认证，连 Redis 的 AUTH 就失败），并非上文所述 `NOPERM`；`codis-admin --acl-sync` 后恢复。
- 脚本总索引见 [`../scripts/validation/README.md`](../scripts/validation/README.md)。
