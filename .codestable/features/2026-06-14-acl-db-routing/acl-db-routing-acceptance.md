# ACL 按 db 绑定路由 验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-06-14
> 关联方案 doc：.codestable/features/2026-06-14-acl-db-routing/acl-db-routing-design.md

## 1. 接口契约核对

对照 design 第 2.1 节名词层：

**接口示例逐项核对**：
- [x] `models.ACLUser.DB *int`（`pkg/models/acl.go:18`）：示例 `{name:app1,db:3,...}` 编码为 `"db":3`，旧记录无 db → DB=nil → 一致。`TestACLUserDBBindingEncodeDecode` 同时验证 db=0 与 unbound 可区分。
- [x] `SessionACLIdentity.ForcedDB *int`（`pkg/proxy/session_auth.go:25`）：认证时按用户 db 解析；绑定→非 nil，越界→认证失败不写身份 → 与示例三分支一致（`session_auth.go:106-127`）。
- [x] topom `ACLUserUpdate.DB` / `ACLUserView.DB`（`pkg/topom/topom_acl.go:46,30`）：提交 db→回显 db，且 `redisACLSetUser` 不含 db → 一致（`TestUpdateACLEchoesDBAndKeepsRedisACLEndpointClean`）。

**名词层"现状 → 变化"逐项核对**：
- [x] ACL 模型加 db 字段：用 `*int`（nil=未绑定）而非 `int`，避免存量旧 JSON 缺省 0 误判为"绑定到 db 0" → 代码与 design 决定一致，`pkg/proxy/acl.go:34` snapshot deep copy 同步带上。
- [x] 会话身份携带 Forced DB：`getACLIdentity` 浅拷贝复制 ForcedDB 指针（只读，无 mutation），`r.ACLIdentity` 携带到请求 → 一致。

**流程图核对**（design 2.2 mermaid）：
- [x] 认证解析点 `handleACLAuth` 设 ForcedDB + setDatabase（grep 确认 `session_auth.go:110,125,129`）。
- [x] SELECT 强制点 `handleSelect` 绑定会话早返回 OK（`session.go:329`）。
- [x] 转发链 `r.Database = s.getDatabase()` 未改（`session.go:201` 无 diff）→ 天然返回绑定 db。

无偏差。

## 2. 行为与决策核对

**需求摘要逐项验证**：
- [x] 绑定用户命令落绑定 db：`TestACLDBBoundUserRoutesToBoundDB`（断言后端收到 `SELECT 3`）。
- [x] SELECT 优先级低于绑定：`TestACLDBBoundSessionSelectIsNoOp`（SELECT 5 回 OK，后端只见 SELECT 3、无 SELECT 5）。
- [x] unbound 用户行为不变：`TestACLDBUnboundUserSelectTakesEffect`（SELECT 4 生效）。
- [x] 越界 fail-closed：`TestACLDBBoundOutOfRangeFailClosed`（AUTH 报错、后续 GET 返回 NOAUTH、后端无 GET）。
- [x] 存量兼容：`TestACLUserDBBindingEncodeDecode`（legacy JSON → DB=nil）。

**三个关键决策落地**：
- [x] D1 按用户绑定：db 落在 `models.ACLUser`，`PasswordHashes` 结构未动 → 一致。
- [x] D2 SELECT 返回 OK 但忽略：`handleSelect` 绑定分支 `r.Resp = RespOK; return nil`，不 setDatabase → 一致。
- [x] D3 仅 ACL 模式：`handleSelect` 判 `r.ACLIdentity != nil && ForcedDB != nil`；legacy auth 的 ACLIdentity 为 nil → legacy 路径无 db 绑定。

**明确不做逐项核对**（design 第 3 节反向核对）：
- [x] `redisACLSetUser` 无 db 引用（grep 空）+ `TestUpdateACLEchoesDBAndKeepsRedisACLEndpointClean` 断言 SETUSER 无 db/select token → db 不下发 Redis。
- [x] PasswordHashes 仍是 `[]string`，未改 per-password 结构。
- [x] legacy `session_auth` 路径无改动（无 ForcedDB）。
- [x] 无 db 越界静默降级（fail-closed 测试）。
- [x] 内部命令未读 session ForcedDB：`TestACLDBBoundUserMigratingSlotDryRunSucceeds` 证明迁移/ACL DRYRUN 走 `r.Database`（绑定 db）一致且未被破坏。

**流程级约束核对**：
- [x] 优先级：setDatabase + SELECT no-op 双点实现，SELECT 改不动绑定 db。
- [x] 一致性：db 随 `models.ACL` 经 `SetACL` snapshot 下发，`TestACLDBBoundUserRevisionReAuthUsesNewDB` 验证 revision 切换→stale→重认证→新 db。
- [x] 越界 fail-closed：proxy 在认证时按 `backend_number_databases` 兜底（topom 只校 db≥0，`TestUpdateACLRejectsNegativeDB`）。

**挂载点反向核对（可卸载性）**——对照 design 第 2.3 节：
- [x] M1 `models.ACLUser.DB`：落点确认。
- [x] M2 proxy 认证+SELECT+snapshot：`ForcedDB` 仅出现在 `session.go`/`session_auth.go`/测试；`user.DB` 仅出现在 `acl.go`/`session_auth.go`。
- [x] M3 topom：`input.DB`/`user.DB` 仅在 `topom_acl.go:206,277`。
- [x] M4 FE+admin：`dashboard.go:371` redact 保留；`acl.js`/`index.html` db 字段。
- [x] **反向 grep**：`ForcedDB` 与 ACLUser `.DB` 的全部引用均落在 M1-M4 内；`client_list.go:254`、`topom_rdb_analysis.go:653` 的 `.DB` 是无关既有字段，非本 feature。无清单外引用。
- [x] **拔除沙盘**：删 `models.ACLUser.DB` → 编译断在 proxy/topom/admin/FE 的上述点 → 逐一移除即可，无散落残留（无 log/stats/coordinator 额外落点）。

## 3. 验收场景核对

对照 design 第 3 节，逐条可观察证据：

- [x] **绑定生效**：`TestACLDBBoundUserRoutesToBoundDB` — 单测，后端见 SELECT 3 + SET。
- [x] **SELECT 被忽略**：`TestACLDBBoundSessionSelectIsNoOp` — 单测，无 SELECT 5。
- [x] **默认用户绑定**：`TestACLDBBoundDefaultUser` — 单测，AUTH(无用户名)→ SELECT 2。
- [x] **unbound 不变**：`TestACLDBUnboundUserSelectTakesEffect` — 单测，SELECT 4 生效。
- [x] **越界 fail-closed**：`TestACLDBBoundOutOfRangeFailClosed` — 单测。
- [x] **存量兼容**：`TestACLUserDBBindingEncodeDecode` — 单测，legacy → nil。
- [x] **revision 切换**：`TestACLDBBoundUserRevisionReAuthUsesNewDB` — 单测，3→7。
- [x] **topom 回显 + SETUSER 干净**：`TestUpdateACLEchoesDBAndKeepsRedisACLEndpointClean` — 单测。
- [x] **内部隔离**：`TestACLDBBoundUserMigratingSlotDryRunSucceeds` — 单测，迁移 DRYRUN 用绑定 db。
- [~] **FE 配置**：代码审查通过（`acl.js` db_text 校验 + cloneACLUsers round-trip 保留其他用户 db；`index.html` 编辑表单 Bound DB 输入 + 列表列）。**浏览器肉眼验证未执行**（本环境无浏览器），列入遗留，建议合并前手工 smoke。

测试运行证据：`go test ./pkg/models ./pkg/proxy ./pkg/topom ./cmd/admin` 全部 `ok`。

## 4. 术语一致性

对照 design 第 0 节：
- "Forced DB" → 代码 `ForcedDB`，命中 `session.go`/`session_auth.go`，一致。
- "DB-bound ACL user" → 代码注释一致；字段 `models.ACLUser.DB`。
- "Unbound user" → 以 `DB == nil` / `ForcedDB == nil` 表达，一致。
- 防冲突：`ForcedDB`/`db_routing` 在本 feature 前无命中（起草阶段已 grep）；`.DB` 既有命中（client list、rdb analysis）语义无关，不冲突。

## 5. 架构归并

对照 design 第 4 节，已实际写入 `.codestable/architecture/ARCHITECTURE.md`：
- [x] 术语区（L24 Codis-managed Redis ACL 条目）：追加 **DB-bound ACL user** / **Forced DB**，说明强制路由优先级高于 SELECT。
- [x] proxy 内存/身份段（L92）：Session ACL identity 增加 ForcedDB；写入 db 绑定语义（认证 setDatabase、SELECT no-op、越界 fail-closed、随 revision 重认证、不渲染 SETUSER、内部命令不受影响）。
- [x] 已知约束段（L177）：追加 db 绑定是 proxy 路由属性、不下发 Redis、topom 校 db≥0 + proxy 兜底、仅 ACL 模式。
- [x] 相关输入列表：新增本 feature design/acceptance 引用。

## 6. requirement 回写

design frontmatter `requirement: redis-cluster-service`（status=current）。本 feature 新增用户可感能力（ACL 用户 db 绑定）→ 触发 update（已实际写入）：
- [x] `.codestable/requirements/redis-cluster-service.md` 变更日志追加 2026-06-14 条目，记录 db 绑定能力、Forced DB 优先级、范围与边界。
- [x] ACL 边界条目追加：db 绑定是 proxy 路由属性（按用户粒度）、不下发 SETUSER、default 用户单 db、需多 db 建多用户。
- 愿景 pitch / 用户故事主体未改（能力归入既有 ACL 用户故事），仅追加变更日志与边界，符合"保留原始愿景"。

## 7. roadmap 回写

design frontmatter 无 `roadmap` / `roadmap_item` 字段 → 非 roadmap 起头，跳过。

## 8. attention.md 候选盘点

本 feature 未引入新的编译/环境/起服务/工作流约定，复用现有 ACL 测试入口（`go test ./pkg/proxy ./pkg/topom ./pkg/models ./cmd/admin`）。
- 无候选：本 feature 未暴露需要补入 attention.md 的内容。

## 9. 遗留

- **FE 浏览器验证待补**：`acl.js`/`index.html` 的 Bound DB 输入与展示仅做代码审查，未在浏览器肉眼验证。建议合并前手工 smoke（编辑用户填 db → 提交 → 刷新列表 Bound DB 列显示；清空 db 提交 → 显示 `-`）。
- **已知行为（非缺陷）**：`buildACLUser` 对 db 无 current-fallback——更新请求若省略 db 字段，用户绑定会被置为 unbound（`TestUpdateACLEditDBToUnbound` 已固化）。FE 通过 `cloneACLUsers` + 编辑表单 round-trip db，正常操作不会丢绑定；裸 admin JSON 调用者需自行带上 db 以保留绑定。
- **可沉淀经验（cs-learn 候选）**：给可选/可空字段加结构时用 `*int`（nil=未设置）而非 `int` 哨兵，可天然兼容存量无该字段的 JSON，避免缺省零值与合法零值歧义。
