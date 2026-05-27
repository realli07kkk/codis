---
doc_type: feature-acceptance
feature: 2026-05-27-proxy-stream-commands
status: current
accepted_at: 2026-05-27
summary: Codis Proxy 已按设计新增 Redis Stream 命令受控路由子集，架构与 requirement 已回写
tags: [proxy, redis8, stream, redis-protocol, acceptance]
---

# proxy-stream-commands 验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-05-27
> 关联方案 doc：`.codestable/features/2026-05-27-proxy-stream-commands/proxy-stream-commands-design.md`

## 1. 接口契约核对

对照方案第 2.1 节名词层逐一核查：

- [x] Stream 命令从未知命令 fallback 变成显式 metadata：`pkg/proxy/stream_commands.go` 的 `streamCommandSpecs` 是单一来源，`pkg/proxy/mapper.go` 通过 `registerStreamCommandOps(opTable)` 注册读写属性。
- [x] `StreamRoute` 最终契约为 `hashKey + keys`：`hashKey` 是用于 Codis slot.forward 和迁移包装的代表性原始 stream key，`keys` 是写后 hot key cache 失效使用的 stream key 列表；实现中没有 `redisKeyCount` 镜像字段。
- [x] 单 key Stream 命令按第 1 参数路由：`XADD`、`XLEN`、`XRANGE`、`XREVRANGE`、`XDEL`、`XTRIM`、`XACK`、`XPENDING`、`XCLAIM`、`XAUTOCLAIM` 等都走 `streamRouteSingleKey`。
- [x] 容器命令按 subcommand 后的 key 路由：`XGROUP CREATE/SETID/DESTROY/CREATECONSUMER/DELCONSUMER` 和 `XINFO STREAM/GROUPS/CONSUMERS` 按第 2 参数 stream key 路由。
- [x] `XREAD` / `XREADGROUP` 从 `STREAMS` 段解析 key/id 列表，要求 key 数与 id 数成对；`XREADGROUP` 按 `FlagWrite` 走 master。
- [x] multi-stream 只在所有 key 共享同一 hash tag/key 时放行，整条命令用第一个原始 stream key 作为代表性 `hashKey` 转发，保证 `Hash(hashKey)` 和迁移期 tag-aware 包装语义一致。
- [x] Stream dispatch 位于 default backend dispatch 前，只对显式 Stream 命令生效，成功后复用 `dispatchByHashKey`、slot.forward、迁移包装、后端连接池、pipeline 写回和 hot key cache 写后失效。

结论：接口契约与设计一致。设计文档已同步实现期优化：`StreamRoute` 不再记录 `redisKeyCount`，Stream command metadata 合并为单一来源。

## 2. 行为与决策核对

**需求摘要逐项验证**：

- [x] Redis 8 backend 的常见 Stream 单 key 命令不再依赖未知命令 fallback。
- [x] `XGROUP` / `XINFO` 不再把 subcommand 当 key。
- [x] 非阻塞 `XREAD` / `XREADGROUP` 支持单 stream 和同 hash tag multi-stream。
- [x] 带 `BLOCK`、跨 hash tag multi-stream、`XGROUP HELP`、`XINFO HELP` 和未知 Stream subcommand 在 proxy 返回 Redis error，不转发后端。
- [x] Redis 3 backend 场景只保证 proxy 按 stream key 正确路由，最终 unknown command 由后端返回。

**明确不做逐项核对**：

- [x] 未实现带 `BLOCK` 的阻塞 Stream 读调度。
- [x] 未实现跨 slot / 跨 hash tag multi-stream 拆分、并发转发和响应合并。
- [x] 未纳入 Vector Set、Hash field expiration、RedisJSON、RedisBloom、RedisTimeSeries、RediSearch 或 Redis Stack 模块能力。
- [x] 未动态读取 Redis `COMMAND` / command JSON 生成 proxy 命令表。
- [x] 未改变 `EVAL` / `EVALSHA`、transaction、pub/sub、Redis Cluster、Codis migration 命令等既有 allow/deny 策略。
- [x] 未新增配置项、dashboard/FE 页面、coordinator schema 或 proxy admin API。

**实现阶段 review 问题回归**：

- [x] Stream resolver 实现路径没有无界 `string(...)`、`strings.ToUpper` 或 `strings.EqualFold`；Redis token 比较由 `streamTokenEqual([]byte, string)` 完成，只比较短白名单 token。
- [x] unsupported subcommand error 不再回显客户端 bulk，避免错误响应放大。
- [x] Stream command list 已合并为 `streamCommandSpecs`，命令 flag 和 routing kind 不再维护两份列表。
- [x] `redisKeyCount` 镜像字段已删除，测试和实现均直接使用 `len(route.keys)` / `keys` 列表语义。

**挂载点反向核对（可卸载性）**：

- [x] `pkg/proxy/stream_commands.go`：Stream metadata、resolver、拒绝策略和 Stream dispatch 接入口。
- [x] `pkg/proxy/mapper.go`：只新增从 Stream metadata 注册 `OpInfo` 的调用，并复用 hash tag/key helper。
- [x] `pkg/proxy/session.go`：只在 default dispatch 前增加显式 Stream 分支。
- [x] `pkg/proxy/router.go`：抽出 `dispatchByHashKey`，让 Stream resolver 提供的 key 复用原 slot.forward。
- [x] `pkg/proxy/hot_key_cache.go`：新增按解析后 keys 生成写后失效 plan 的 helper。
- [x] `doc/unsupported_cmds.md`：写明 Stream 是受控 Redis 8 proxy-routing 子集，不宣传完整 Stream 支持。

结论：实现符合核心决策，返工已处理上一轮 Critical 和主要 Warning。

## 3. 验收场景核对

- [x] 输入 `XADD mystream * f v` 时，proxy 按 `mystream` 计算 slot，写请求发往 master，后端响应原样返回。
  - 证据来源：`TestSessionStreamSingleKeyBackendErrorPassThrough`。
- [x] 输入 `XLEN mystream` / `XRANGE mystream - +` 时，proxy 识别为 read-only Stream 命令并按 `mystream` 路由。
  - 证据来源：`TestGetOpInfoStreamCommands`、`TestSessionStreamReadWriteReplicaMasterRouting`。
- [x] 输入 `XGROUP CREATE mystream group-1 0` 时，proxy 按第 2 参数 `mystream` 路由，而不是按 `CREATE` 路由。
  - 证据来源：`TestStreamResolveSingleAndContainerRoutes`、`TestSessionStreamContainerDispatchUsesStreamKey`。
- [x] 输入 `XINFO GROUPS mystream` / `XINFO CONSUMERS mystream group-1` 时，proxy 按第 2 参数 `mystream` 路由。
  - 证据来源：`TestStreamResolveSingleAndContainerRoutes`。
- [x] 输入 `XREAD STREAMS mystream 0-0` 时，proxy 解析 `STREAMS` 段并按 `mystream` 路由。
  - 证据来源：`TestStreamResolveSingleAndContainerRoutes`。
- [x] 输入 `XREADGROUP GROUP group-1 consumer-1 STREAMS mystream >` 时，proxy 解析 `STREAMS` 段，按 `mystream` 路由，并按写命令走 master。
  - 证据来源：`TestSessionStreamReadWriteReplicaMasterRouting`。
- [x] 输入 `XREAD STREAMS {u1}:a {u1}:b 0-0 0-0` 时，proxy 识别两个 stream key 共享 hash tag `u1` 并整条转发。
  - 证据来源：`TestStreamResolveSingleAndContainerRoutes`。
- [x] 输入 `XREAD STREAMS a b 0-0 0-0` 时，proxy 返回 cross-slot 风格 Redis error，不转发后端。
  - 证据来源：`TestSessionStreamReadRejectsWithoutBackendDispatch`。
- [x] 输入带 `BLOCK` 的 `XREAD` / `XREADGROUP` 时，proxy 返回 unsupported blocking Stream error，不转发后端。
  - 证据来源：`TestStreamResolveRejectsUnsafeForms`、`TestSessionStreamReadRejectsWithoutBackendDispatch`。
- [x] 输入 `XGROUP HELP` / `XINFO HELP` / `XGROUP UNKNOWN mystream` 时，proxy 返回 unsupported Stream subcommand error，不转发后端。
  - 证据来源：`TestStreamResolveRejectsUnsafeForms`。
- [x] unsupported subcommand error 不回显大 bulk 参数。
  - 证据来源：`TestStreamResolveUnsupportedSubcommandDoesNotEchoBulk`。
- [x] 后端是 Redis 3 时输入 `XADD mystream * f v`，proxy 仍正确路由，由后端返回 unknown command error。
  - 证据来源：`TestSessionStreamSingleKeyBackendErrorPassThrough`。
- [x] 迁移 slot 上使用解析出的 stream key 作为 hkey。
  - 证据来源：`TestSessionStreamMigrationUsesResolvedStreamKey`。
- [x] Stream 写命令按解析出的 stream key 触发现有写后失效。
  - 证据来源：`TestStreamWriteInvalidatesHotKeyCacheByResolvedKey`。
- [x] 现有非 Stream 命令如 `GET`、`MGET`、`CLUSTER NODES`、未知 `FOO key` 的行为未被本 feature 扩大。
  - 证据来源：`go test ./pkg/proxy`、既有 proxy 测试、`TestGetOpStrCmd`、`TestHashSlot`。

执行过的验证命令：

```bash
go test ./pkg/proxy -run 'Test(GetOpInfoStreamCommands|Stream|SessionStream|HashSlot)'
go test ./pkg/proxy
make gotest
git diff --check
gofmt -l pkg/proxy/hot_key_cache.go pkg/proxy/mapper.go pkg/proxy/mapper_test.go pkg/proxy/router.go pkg/proxy/session.go pkg/proxy/stream_commands.go pkg/proxy/stream_commands_test.go
python3 .codestable/tools/validate-yaml.py --file .codestable/features/2026-05-27-proxy-stream-commands/proxy-stream-commands-design.md
python3 .codestable/tools/validate-yaml.py --file .codestable/features/2026-05-27-proxy-stream-commands/proxy-stream-commands-acceptance.md
python3 .codestable/tools/validate-yaml.py --file .codestable/features/2026-05-27-proxy-stream-commands/proxy-stream-commands-checklist.yaml --yaml-only
python3 .codestable/tools/validate-yaml.py --file .codestable/architecture/ARCHITECTURE.md
python3 .codestable/tools/validate-yaml.py --file .codestable/requirements/redis-cluster-service.md
```

## 4. 术语一致性

- `Redis Stream`：文档中只指 Redis 5+ Stream 数据结构和 `X*` 命令族，不等同于 Redis Stack。
- `Stream command support`：统一解释为 Codis Proxy 的命令属性、key 提取、slot 路由和拒绝策略，不表示 Redis 3 backend 能执行 Stream。
- `Stream key resolver`：代码中由 `resolveStreamRoute` 和各类 resolver 承载，覆盖单 key、`XGROUP`、`XINFO`、`XREAD` / `XREADGROUP`。
- `Blocking stream read`：统一指带 `BLOCK` 的 `XREAD` / `XREADGROUP`，首版拒绝。
- `Multi-stream request`：统一指单条 `XREAD` / `XREADGROUP` 包含多个 stream key；只有同 hash tag/key 才放行。
- `hashKey`：实现中是代表性原始 stream key，不是抽象 tag 字节；比较时使用 hash tag/key，转发时保留原始 key 供 `Hash()` 和迁移包装使用。

结论：术语与设计、代码和文档一致，没有把受控路由子集包装成完整 Redis Stream 支持。

## 5. 架构归并

- [x] `.codestable/architecture/ARCHITECTURE.md` 已新增 `Stream command support` 术语。
- [x] 结构与交互已补充 Stream 命令在 `Session.handleRequest` default dispatch 前解析、拒绝和转发的路径。
- [x] 命令路由说明已补充 `stream_commands.go` 单一 metadata、mapper 注册、`XGROUP` / `XINFO` key 位置、`XREAD` / `XREADGROUP` `STREAMS` 解析和同 hash tag 限制。
- [x] 架构文档已明确 Stream resolver 成功后复用 `slot.forward`、迁移包装、master/replica 选择和 hot key cache 写后失效计划。
- [x] 代码锚点已新增 `pkg/proxy/stream_commands.go`。
- [x] 已知约束已补充 Stream 支持不是完整 Redis Stream 支持声明，并列出 `XGROUP HELP`、`XINFO HELP`、`BLOCK`、跨 hash tag multi-stream、Redis 3 backend 和 Redis Stack/Vector/HFE 边界。
- [x] 相关文档已挂入 design / acceptance 链接。

## 6. requirement 回写

- [x] design frontmatter 指向 `requirement: redis-cluster-service`，该 requirement 当前为 `current`。
- [x] `.codestable/requirements/redis-cluster-service.md` 已新增 Redis 8 Stream 业务开发者用户故事。
- [x] “怎么解决”已补充 Redis 8 Stream 受控子集进入 proxy 命令边界：单 key、`XGROUP` / `XINFO`、非阻塞同 hash tag `XREAD` / `XREADGROUP` 放行，阻塞读、跨 hash tag 和无 key/未知 subcommand 拒绝。
- [x] “实现进展”已追加 2026-05-27 Codis Proxy Redis Stream 命令受控路由子集。
- [x] “边界”已补充 Stream 支持不是完整 Redis Stream 支持声明，不包含 Vector Set、Hash field expiration、RedisJSON、RedisBloom、RedisTimeSeries、RediSearch 或 Redis Stack 模块能力。

## 7. roadmap 回写

- [x] design frontmatter 没有 `roadmap` / `roadmap_item` 字段。
- [x] `.codestable/roadmap/redis8-upgrade/` 是 Redis 8 升级大项历史规划，本 feature 未声明为其中某个 roadmap item 的验收闭环。

结论：本 feature 不需要更新 `.codestable/roadmap/*`。

## 8. attention.md 候选盘点

- [x] 本 feature 没有新增通用构建命令、环境变量、路径陷阱或每次启动 CodeStable 都必须知道的项目特殊设置。
- [x] 需要长期记住的能力边界已放入 requirement、architecture 和 `doc/unsupported_cmds.md`。

结论：不更新 `.codestable/attention.md`。

## 9. 遗留

- 已知限制：`XGROUP HELP` / `XINFO HELP` 仍是有意拒绝的 userspace 可见行为变化；当前 design 和文档均已明确，首版不返回静态 help 文本。
- 已知限制：带 `BLOCK` 的 `XREAD` / `XREADGROUP` 不支持；不做阻塞连接隔离、长轮询资源预算或 pipeline 调度改造。
- 已知限制：跨 hash tag multi-stream 不支持；同 slot 但不同 hash tag/key 的 multi-stream 也不放行，以保证迁移期 key group 语义。
- 发布表述：release note 只能称为“Redis Stream 命令受控路由子集”或“常见 Stream 命令 proxy routing 支持”，不能写成“完整 Redis Stream 支持”。
- 后续扩展：如用户强依赖 `XGROUP HELP` / `XINFO HELP`，可另开小 feature 在 proxy 本地返回静态 help；如要支持 blocking read 或跨 slot merge，必须另起设计重新评估资源隔离和响应合并模型。

验收通过。实现与设计一致，架构与 requirement 已回写，checklist checks 已全部标记为 `passed`，用户已终审确认。
