# proxy-client-list 验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-05-13
> 关联方案 doc：`.codestable/features/2026-05-12-proxy-client-list/proxy-client-list-design.md`

## 1. 接口契约核对

对照方案第 2.1 节名词层逐一核查。

**接口示例逐项核对**：

- [x] `CLIENT LIST`：输入 `CLIENT LIST` → 输出 RESP bulk string，每行一个 proxy client session，字段为 `key=value`，以 `\n` 结尾。
  - 代码实际行为：一致。入口在 `pkg/proxy/session.go` 的 `CLIENT` 分支，处理逻辑在 `pkg/proxy/client_list.go` 的 `handleRequestClientList` / `formatClientList`。证据：`TestClientListCommand`、`TestFormatClientList`。
- [x] `CLIENT LIST TYPE normal`：等价于 `CLIENT LIST`。
  - 代码实际行为：一致。证据：`TestClientListFilters`。
- [x] `CLIENT LIST TYPE replica/master/pubsub`：返回空 bulk string。
  - 代码实际行为：一致。证据：`TestClientListFilters`。
- [x] `CLIENT LIST ID <id>`：只返回命中的 session，未命中 id 忽略。
  - 代码实际行为：一致。证据：`TestClientListFilters`。
- [x] `CLIENT KILL ...`：返回不支持错误，不转发到 backend。
  - 代码实际行为：一致。证据：`TestClientUnsupportedSubcommand`；grep 未发现 `CLIENT LIST` 进入 `dispatch` / `dispatchAddr` / `dispatchSlot`。

**名词层“现状 → 变化”逐项核对**：

- [x] `CLIENT` 命令表从顶层禁用变成本地受控容器。
  - 代码改动：`pkg/proxy/mapper.go` 将 `CLIENT` 从 `FlagNotAllow` 改为 `0`；`Session.handleRequest` 增加本地分支。
- [x] 新增 `Session registry` 和 session id。
  - 代码改动：`pkg/proxy/client_list.go` 新增 `clientSessionRegistry`；`pkg/proxy/session.go` 新增 `Session.Id`，并在 session 生命周期内注册 / 注销。
- [x] `ClientListEntry` 快照名词落地。
  - 代码改动：`pkg/proxy/client_list.go` 的 `clientListEntry` 保存格式化所需字段，snapshot 复制后再格式化。
- [x] 活动 session 数量以 registry 为权威来源。
  - 代码改动：`pkg/proxy/stats.go` 的 `SessionsAlive()` 从 `clientSessions.count()` 读取；`sessions.total` 只保留累计连接数。

**流程图核对**：

- [x] `loopReader` 解码 → `getOpInfo` 得到 `CLIENT` → 鉴权 → `handleRequestClient` → 解析过滤 → snapshot → format → `loopWriter` 写回。
  - 代码落点：`pkg/proxy/session.go`、`pkg/proxy/client_list.go` 均可 grep 到对应节点。

## 2. 行为与决策核对

**需求摘要逐项验证**：

- [x] 已认证连接执行 `CLIENT LIST` 返回当前 proxy 实例客户端连接列表。
  - 实测证据：`TestClientListCommand`。
- [x] 响应是 RESP bulk string，内容为 Redis 风格 `key=value` 行。
  - 实测证据：`TestClientListCommand`、`TestFormatClientList`。
- [x] 不转发到后端 Redis。
  - 代码证据：`CLIENT` 在 `Session.handleRequest` 的本地分支返回；`client_list.go` 不依赖 `Router`。

**明确不做逐项核对**：

- [x] 未实现 `CLIENT KILL`、`CLIENT SETNAME`、`CLIENT INFO`、`CLIENT ID`、`CLIENT TRACKING` 成功路径。
  - grep / review 结果：只有 `LIST` case；其他子命令返回 unsupported error。
- [x] 未聚合 dashboard/topom 中所有 proxy 连接列表。
  - grep / review 结果：无 dashboard/topom 代码改动。
- [x] 未向后端 Redis Server 发送 `CLIENT LIST`。
  - grep / review 结果：无 `CLIENT LIST` backend 调用。
- [x] 未引入 RESP3 verbatim string。
  - grep / review 结果：未修改 RESP encoder 类型，仍返回 RESP bulk string。
- [x] 未改变 `SessionAuth` 鉴权语义。
  - 实测证据：`TestClientListRequiresAuth`。

**关键决策落地**：

- [x] 本地实现，不转发 backend。
  - 代码体现：`clientSessions.handleRequestClient` 直接生成 `redis.NewBulkBytes`。
- [x] 只支持 `CLIENT LIST`。
  - 代码体现：`handleRequestClient` 仅 `LIST` case，其余返回 unsupported error。
- [x] 新增 registry，不复用简单计数。
  - 代码体现：`clientSessionRegistry` 持有 `map[int64]*Session`，并成为 `SessionsAlive()` 来源。
- [x] 输出 Codis 可证明字段子集。
  - 代码体现：`formatClientList` 固定输出 `id/addr/laddr/name/age/idle/flags/db/sub/psub/ssub/multi/qbuf/qbuf-free/obl/oll/omem/events/cmd/user/redir/resp`。

**编排层“现状 → 变化”逐项核对**：

- [x] session 生命周期接入 registry。
  - 代码落点：`Session.Start` 中 register，router offline 和 writer exit 中 unregister。
- [x] `CLIENT` 分支插入 `default -> d.dispatch(r)` 之前。
  - 代码落点：`Session.handleRequest` 的 `case "CLIENT"`。
- [x] `CLIENT` 子命令不得进入 router。
  - 代码落点：`handleRequestClient` 处理所有 `CLIENT` 子命令路径，未知子命令直接返回 error。

**流程级约束核对**：

- [x] 认证：未认证时返回 `NOAUTH Authentication required`。
  - 证据：`TestClientListRequiresAuth`。
- [x] 错误语义：未知 TYPE / 无效 ID / syntax error 返回 Redis error reply。
  - 证据：`TestClientListFilters`。
- [x] 并发：registry snapshot 短锁复制 session 指针，释放 registry 锁后再读 session 快照。
  - 代码证据：`snapshot` / `snapshotByIDs`。
- [x] 顺序：仍走原 request/response pipeline。
  - 代码证据：本地处理只设置 `Request.Resp`，仍由 `loopWriter` 统一写回。
- [x] 可观测性：`CLIENT LIST` 计入 op stats，`cmd=CLIENT` 可见。
  - 证据：`TestClientListCommand` 核对 `cmd=CLIENT`。

**挂载点反向核对（可卸载性）**：

- [x] `pkg/proxy/mapper.go` command table：`CLIENT` 改为允许进入本地分支。
- [x] `pkg/proxy/session.go` request dispatch：新增 `CLIENT` 本地处理分支。
- [x] proxy session lifecycle：注册 / 注销活动连接集合。
- [x] 反向 grep：本 feature 的核心引用集中在 `mapper.go`、`session.go`、`stats.go`、`client_list.go`、`client_list_test.go`、`doc/unsupported_cmds.md` 和 CodeStable 文档内。
- [x] 拔除沙盘推演：恢复 `mapper.go` 的 `CLIENT` 禁用、移除 `session.go` 的 `CLIENT` 分支和 lifecycle registry 调用、删除 `client_list.go` / `client_list_test.go`、恢复 `stats.go` 的 alive 计数和 unsupported doc，即可拔除用户可见能力。

## 3. 验收场景核对

- [x] **S1**：已认证连接执行 `CLIENT LIST` → 返回 bulk string，至少包含当前连接一行，字段包含 `id/addr/laddr/age/idle/flags/db/cmd/user`，且不访问 backend。
  - 证据来源：单测 `TestClientListCommand` + 代码 review。
  - 结果：通过。
- [x] **S2**：未认证连接在 `SessionAuth` 非空时执行 `CLIENT LIST` → 返回 `NOAUTH Authentication required`。
  - 证据来源：单测 `TestClientListRequiresAuth`。
  - 结果：通过。
- [x] **S3**：`CLIENT LIST TYPE normal` → 返回当前 proxy 普通客户端连接列表。
  - 证据来源：单测 `TestClientListFilters`。
  - 结果：通过。
- [x] **S4**：`CLIENT LIST TYPE replica/master/pubsub` → 返回空 bulk string。
  - 证据来源：单测 `TestClientListFilters`。
  - 结果：通过。
- [x] **S5**：`CLIENT LIST TYPE unknown` → 返回 unknown client type 错误。
  - 证据来源：单测 `TestClientListFilters`。
  - 结果：通过。
- [x] **S6**：`CLIENT LIST ID <existing-id>` → 只返回对应连接。
  - 证据来源：单测 `TestClientListFilters`。
  - 结果：通过。
- [x] **S7**：`CLIENT LIST ID not-number` → 返回 invalid client ID 错误。
  - 证据来源：单测 `TestClientListFilters`。
  - 结果：通过。
- [x] **S8**：`CLIENT KILL ...` 或 `CLIENT SETNAME ...` → 返回不支持 / 不允许错误，不转发到 backend。
  - 证据来源：单测 `TestClientUnsupportedSubcommand`。
  - 结果：通过。
- [x] **S9**：大量连接并发建立、关闭，同时执行 `CLIENT LIST` → 不 panic、不 data race。
  - 证据来源：`go test -race -count=1 ./pkg/proxy`。
  - 结果：通过。
- [x] **S10**：执行目标 proxy 测试。
  - 证据来源：`go test ./pkg/proxy -run 'Test(ClientList|FormatClientList|ClientCommand|SessionRegistry|ClientUnsupportedSubcommand|Stats)'`。
  - 结果：通过。

## 4. 术语一致性

- `CLIENT LIST`：代码、文档、测试均使用 Redis 原生命令名。
- `Proxy client session`：代码落点为 `Session` 和 `clientSessionRegistry`，没有引入冲突性公开术语。
- `Session registry`：代码命名为 `clientSessionRegistry` / `clientSessions`，含义与 design 一致。
- `Redis field subset`：输出字段集中在 `formatClientList`，未出现 Redis 8.x 全字段 parity 承诺。
- 防冲突：grep `CLIENT` 命中均为命令表、本地处理、测试、文档或已有无关配置注释；未发现“后端 CLIENT LIST 聚合”或“dashboard CLIENT LIST”命名冲突。

## 5. 架构归并

- [x] 架构 doc：`.codestable/architecture/ARCHITECTURE.md`
  - 归并内容：proxy 命令路由新增本地 `CLIENT LIST`；proxy 内存状态新增活动 session registry；已知约束新增 `CLIENT` 命令族只支持 `CLIENT LIST` 且语义限定为当前 proxy 实例。
  - 已写入。
- [x] 架构总入口是否需要新增描述：需要。
  - 已在“结构与交互 / 数据与状态 / 代码锚点 / 已知约束”中补充，不只是贴 design 链接。
- [x] `.codestable/attention.md` 是否需要补充：不需要。
  - 理由：本 feature 未暴露新的通用构建、测试、路径或环境陷阱。

## 6. requirement 回写

- [x] `requirement=redis-cluster-service`，状态为 `current`。
  - 本次新增了用户可见运维能力和命令兼容边界变化，因此需要 update。
  - 已更新 `.codestable/requirements/redis-cluster-service.md`：
    - `last_reviewed` 改为 `2026-05-13`。
    - 用户故事新增值班人员通过 Redis 协议面查询 proxy 当前客户端连接。
    - 解决方式新增 proxy 提供有限本地观测命令 `CLIENT LIST`。
    - 边界新增 `CLIENT` 命令族只支持 `CLIENT LIST`，不聚合、不下探后端、不承诺 Redis 8.x 全字段。

## 7. roadmap 回写

- [x] 非 roadmap 起头。
  - design frontmatter 没有 `roadmap` / `roadmap_item` 字段，因此跳过 roadmap 回写。

## 8. attention.md 候选盘点

- [x] 本 feature 未暴露需要补入 attention.md 的内容。
  - 运行测试仍按已有 Go 测试入口；没有新增通用环境变量、构建步骤或路径陷阱。

## 9. 遗留

- 后续优化点：
  - `pkg/proxy/session.go` 继续偏胖，已经在 design 2.5 标为超出范围观察；后续如继续增加本地 Redis 命令，建议单独走 `cs-refactor`。
- 已知限制：
  - 仅支持 `CLIENT LIST`，不支持其他 `CLIENT` 子命令。
  - `CLIENT LIST` 只返回当前 proxy 实例活动客户端连接，不跨 proxy 聚合，不下探后端 Redis。
  - 输出字段为 Codis 可证明子集，不承诺 Redis 8.6.3 full parity。
- 实现阶段“顺手发现”列表：
  - 无新增。

