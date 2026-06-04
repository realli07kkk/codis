# codis-acl 验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-06-04
> 关联方案 doc：`.codestable/features/2026-06-04-codis-acl/codis-acl-design.md`
> 关联 checklist：`.codestable/features/2026-06-04-codis-acl/codis-acl-checklist.yaml`

## 1. 接口契约核对

对照方案第 2.1 节名词层逐一核查：

- [x] `models.ACL` / `ACLUser` / `ACLSnapshot` 已落地，包含 revision、enabled、users、updated_at、password hashes 和 rules；coordinator 路径为 `/codis3/{product}/acl`，由 `Store.LoadACL` / `Store.UpdateACL` 读写。
- [x] dashboard/topom ACL API 已挂到 `/api/topom/acl/:xauth`，返回 `ACLView`，提交 `ACLUpdateRequest`；返回 view 不包含明文 `new_password`。
- [x] proxy ACL snapshot 已挂到 `/api/proxy/acl/:xauth`，由 dashboard/topom 下发；FE 不直接调用该 proxy admin API。
- [x] proxy 配置新增 `codis_acl_enabled`、`backend_auth_username`、`backend_auth_password`；topom/dashboard 配置新增 backend service identity；username-only 配置会校验失败。
- [x] `RedisAuthIdentity` 支持 password-only 和 username/password 两种认证；复制 `SetMaster()` 在 named auth 下写入 `masteruser` + `masterauth`，password-only 旧路径保持兼容。
- [x] Redis 8 Codis Server 配置新增 `codis-migration-auth-user` / `codis-migration-auth-pass`，同步和异步迁移 socket 使用 named ACL auth。
- [x] FE ACL view 与 coordinator 内部模型分离，`cmd/fe/assets/acl.js` 只持有 view/edit state；`new_password` 只在编辑提交中短期存在。

## 2. 行为与决策核对

- [x] 默认关闭：ACL 未启用时，旧 `session_auth`、password-only backend auth 和 Redis 3 fallback 语义不变。
- [x] Source of truth：ACL 由 dashboard/topom 管理，先同步后端 Redis Server，再写 coordinator revision，再同步 proxy snapshot。
- [x] 客户端认证：ACL active 后，`AUTH password` 映射 `default` 用户，`AUTH username password` 映射命名用户；失败 re-AUTH 不清除已有有效身份。
- [x] 客户端授权：稳定 slot 普通命令走 user-bound backend connection；本地命令、addr-specific 命令、迁移包装和 service path 在返回用户可见结果前做 ACL gate。
- [x] 管理命令边界：普通客户端通过 proxy 执行 `ACL SETUSER` / `DELUSER` / `DRYRUN` 等不会落到后端；只保留本地 `ACL WHOAMI`。
- [x] Revision 切换：proxy 采用 copy-on-write 安装 ACL snapshot，新 revision 会关闭旧 user pools，并让旧 session stale 后重新认证。
- [x] Partial failure：Redis Server 同步失败时不写新 revision；proxy 同步失败时收集所有失败 token，并通过 ACL view `sync_status` 暴露，允许幂等重试。

## 3. 验收场景核对

- [x] 模型与 store：`TestACLModelEncodeDecode`、`TestStoreACLPathAndUpdate` 覆盖 ACL encode/decode、路径和持久化。
- [x] 配置兼容：`TestConfigBackendAuthIdentity`、`TestConfigRejectsBackendAuthUsernameWithoutPassword` 覆盖 proxy/topom backend auth 配置。
- [x] Proxy auth：`TestACLAuthNamedUserAndWhoami`、`TestACLAuthDefaultUserPasswordForm`、`TestACLAuthFailureDoesNotInstallIdentity`、`TestACLReAuthFailureKeepsExistingIdentity` 覆盖多账号认证和 WHOAMI。
- [x] Proxy 授权边界：`TestACLCommandNeverFallsThroughToBackend`、`TestACLCommandRejectedWhenCodisACLDisabled`、`TestACLStableCommandUsesUserBoundBackend`、`TestACLLocalCommandDryRunDenied`、`TestACLSelectDryRunDenied`、`TestACLAddrSpecificCommandUsesUserBoundBackend` 覆盖 ACL 管理命令拒绝、user-bound backend、本地/addr-specific gate。
- [x] Hot key cache：`TestACLHotKeyCacheHitDryRunDenied` 覆盖 cache 命中前仍做 ACL 判断。
- [x] Revision stale：`TestACLRevisionSwitchStalesSession` 覆盖旧 session 需要重新认证。
- [x] Topom 编排：`TestUpdateACLSyncsRedisAndStoresRedactedView`、`TestUpdateACLFailureDoesNotStoreRevision`、`TestUpdateACLProxySyncFailureRecordsAllFailedTokens` 覆盖成功、Redis 同步失败和 proxy partial failure。
- [x] Redis auth/replication：`TestClientRedis8SetMasterWritesMasterUserForNamedAuth`、`TestClientSetMasterIgnoresUnsupportedMasterUserClear`、`TestClientRedis8AuthNamedUserPath` 覆盖 named auth 和 password-only 兼容。
- [x] CLI redaction：`TestRedactACLUpdateRequestHidesNewPassword` 覆盖 dry-run 不输出明文新密码。
- [x] FE 静态与浏览器核对：`node -c cmd/fe/assets/acl.js`、`node -c cmd/fe/assets/dashboard-fe.js` 通过；用本地静态 server + Microsoft Edge 打开 `index.html`，确认 ACL 面板、`Revision 0 / not_configured` 和 RDB Analysis 区域正常渲染。
- [x] 编译与回归：`make gotest` 通过；`make codis-server-redis8` 通过；`git diff --check` 通过。
- [x] Redis 8 smoke：关闭 default user、只保留 service user 后，Redis 8 同步迁移和半异步迁移 smoke 通过；named service user 复制 smoke 通过，确认 `masteruser` / `masterauth` 可建立复制链路。

## 4. 术语一致性

- `Codis-managed Redis ACL`：统一表示 dashboard/topom 管理、coordinator 存储目标 revision、proxy 安装 snapshot、Redis Server 执行 ACL 的能力，不等同于 Redis Cluster ACL。
- `backend service identity`：统一表示 proxy/topom 内部访问 Redis Server 的服务账号，用于内部命令、迁移包装、DRYRUN 和复制/迁移 auth。
- `user-bound backend connection`：统一表示绑定客户端 username、credential hash、DB 和 ACL revision 的后端连接池。
- `ACL DRYRUN`：统一表示 service path 在替用户返回结果前，用原始客户端命令在后端模拟授权判断。
- `sync_status`：统一表示 dashboard/topom 观察到的 ACL rollout 状态；不是分布式事务提交证明。

## 5. 架构归并

- [x] `.codestable/architecture/ARCHITECTURE.md` 已更新 `last_reviewed` 和 `acl` tag。
- [x] 术语归并：新增 Codis-managed Redis ACL，明确 dashboard/topom、coordinator、proxy snapshot、user-bound backend 和 service identity 的边界。
- [x] 流程归并：写入 ACL API、Redis Server 同步、coordinator revision、proxy fan-out、客户端 AUTH、ACL WHOAMI、管理命令拒绝、cache gate 和 migration auth。
- [x] 状态归并：补充 `/codis3/{product}/acl`、topom `aclSync`、proxy ACL snapshot、session identity 和 user pools。
- [x] 代码锚点归并：补充 `pkg/models/acl.go`、`pkg/topom/topom_acl*.go`、`pkg/proxy/acl*.go`、`session_auth.go`、FE `acl.js`、admin ACL 和 Redis 8 migration auth 修改点。
- [x] 约束归并：补充默认关闭、旧 auth 兼容、普通客户端不能管理 ACL、partial proxy sync 可观测和首版非目标。

## 6. requirement 回写

- [x] `.codestable/requirements/redis-cluster-service.md` 已更新 `last_reviewed`、tags 和用户故事。
- [x] “怎么解决”已补入 Codis-managed Redis ACL 的 source of truth、客户端认证、user-bound backend、service identity 和管理命令边界。
- [x] “实现进展”已新增 2026-06-04 ACL 记录，覆盖 dashboard/topom/admin/FE、proxy 多账号认证、复制 masteruser/masterauth 和 Redis 8 migration named auth。
- [x] “边界”已补入默认关闭、Redis 8 scope、Redis 3 fallback、明文密码边界和首版非目标。

## 7. roadmap 回写

- [x] 方案 frontmatter 没有 `roadmap` / `roadmap_item` 字段，本 feature 非 roadmap 拆分条目，跳过 roadmap 回写。

## 8. attention.md 候选盘点

- [x] 有候选，暂不写入 `attention.md`，待用户确认：
  - 候选 1：Redis 8 named backend auth 涉及复制时必须同时设置 `masteruser` 和 `masterauth`；Codis 仍保留 `SLAVEOF` alias 以兼容旧 Redis 路径。
  - 候选 2：新增较大的 Codis FE 功能时，优先拆独立 `cmd/fe/assets/{feature}.js`，`dashboard-fe.js` 只保留共享入口和既有控制器粘合。

## 9. 遗留

- 首版不实现 Redis Cluster ACL、RESP3/HELLO parity、Pub/Sub channel ACL、Redis Stack/module key-spec 语义，也不承诺 proxy rollout 的分布式原子切换。
- FE 已完成静态渲染和控制器语法核对；未在本轮单独启动真实 dashboard 做浏览器端提交表单 E2E，提交语义由 topom API 测试和 FE 控制器代码核对覆盖。
- 后续适合补用户指南：ACL 配置示例、服务账号最小权限、客户端 AUTH 迁移方式、回滚步骤、partial `sync_status` 排障和 Redis 8 smoke 操作手册。
