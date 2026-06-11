---
doc_type: issue-fix
issue: 2026-06-11-codis-acl-review-regressions
path: standard
fix_date: 2026-06-11
related: [codis-acl-review-regressions-analysis.md]
tags: [acl, proxy, topom, security, concurrency]
---

# Codis ACL Review Regressions 修复记录

## 1. 实际采用方案

采用 analysis 中的方案 A：最小生命周期补齐。

- proxy 侧让 user-bound backend pool 自己维护并发安全和关闭态，关闭后不再创建新 backend conn；`KeepAlive` 锁内只取连接快照，锁外执行可能阻塞的 backend PING / INFO；forward path 拿不到 backend 时返回 slot not ready，避免 nil backend 和 waitgroup 泄漏。
- ACL revision / enabled 切换时先 mark/clear session，再关闭 user pools；user pool 创建前校验 identity revision 仍等于当前 snapshot revision，避免旧 identity 复活旧 revision pool。
- ACL disabled 时清理当前 proxy 已有 ACL session 的 identity 和 legacy `authorized` 状态。
- ACL AUTH 失败统一进入 brute-force guard，同时保留已认证 ACL session 的原 identity，保持 re-auth 失败不登出的 Redis 语义。
- topom 增加显式 `SyncACL` API / admin CLI，支持用 store 中的 ACL revision 重推 Redis runtime 和 proxy；新 server 加入 group 前先同步当前 ACL。
- backend service user 改用 `#sha256` password token 下发，避免 `ACL SETUSER >plain` 暴露明文。

## 2. 改动文件清单

- `pkg/proxy/backend.go`：`sharedBackendConnPool` 增加 `mu` / `closed`，保护 `Get` / `Retain` / `GetOrCreate` / `Close`；`Release` 在 owner pool 锁内更新 refcount 和 map；`KeepAlive` 锁内复制连接列表后锁外执行。
- `pkg/proxy/router.go`、`pkg/proxy/client_list.go`：ACL disabled 时清理当前 session 授权；ACL revision / enabled 切换先处理 session 再关闭 user pools；user backend pool 创建前校验 identity revision；user backend pool 关闭后返回 nil。
- `pkg/proxy/forward.go`：user-bound backend 缺失时返回 `ErrSlotIsNotReady`，不注册 request waitgroup。
- `pkg/proxy/session_auth.go`：ACL AUTH 失败总是记录 brute-force failure；新增 `clearACLAuthorization`。
- `pkg/topom/topom_acl.go`：新增 `SyncACL`，Redis ACL sync 使用短超时，service user 使用 hash token。
- `pkg/topom/topom_group.go`：`GroupAddServer` 对新 Redis server 先同步当前 ACL runtime。
- `pkg/topom/topom_api.go`：新增 `/api/topom/acl/sync/:xauth` 和 `ApiClient.SyncACL`。
- `cmd/admin/main.go`、`cmd/admin/dashboard.go`：新增 `codis-admin --dashboard=ADDR --acl-sync`。
- `pkg/proxy/backend_test.go`、`pkg/proxy/acl_test.go`、`pkg/topom/topom_acl_test.go`：补充 pool 并发 GetOrCreate / Close、ACL disabled、re-auth brute-force、service user hash、显式 resync、group add server sync 覆盖。

## 3. 验证结果

- `go test ./pkg/proxy ./pkg/topom -run ACL`
- `go test ./pkg/proxy ./pkg/topom`
- `go test ./cmd/admin`
- `make gotest`
- `git diff --check`

以上均通过。

## 4. 遗留事项

- Redis 重启后的 runtime ACL 恢复当前通过 `codis-admin --acl-sync` 显式触发；后台周期对账和 Redis `aclfile` 持久化不纳入本次小修。
- `SyncACL` 只按当前 store ACL 重推期望用户，不会枚举 Redis 侧现存用户并删除漂移用户。若某台 Redis 在包含 `DELUSER` 的 `UpdateACL` 中失联，后续 `--acl-sync` 不能自动清掉该 server 上的已删除用户；需要后续引入 `ACL LIST` 对账或文档化恢复手顺。
- brute-force guard 当前按 IP 计数，且任意成功 AUTH 会清除该 IP 的失败记录；持有有效账号的客户端仍可用“失败 N-1 次 + 成功 1 次”方式规避锁定。后续可改为成功衰减或按 `(ip, username)` 维度计数。
- user-bound backend pool 当前不在 `Router.KeepAlive` 周期覆盖范围内；ACL 用户的空闲后端连接依赖请求路径上的 BackendConn 自动重连。
- review 中的性能和运维增强项未纳入本 issue：ACL DRYRUN 热路径优化、user pool 容量上限 / idle 淘汰、sync_status 漂移观测细化。
- review 中的清理项未纳入本 issue：空 `topom_acl_api.go`、helper 命名、`dispatchAddr` 局部变量命名、`NewPassword` 与 `PasswordHashes` 同时提交的校验、`ioutil.ReadFile` 迁移。
