# Codis Proxy 协议扩展

Codis Proxy 以普通 Redis 协议面向客户端，除常规 key 路由外还在协议层提供了三类兼容/扩展行为：proxy 本地的 `CLIENT LIST`、可选的伪 `CLUSTER NODES` 节点清单，以及 Redis 8 Stream 命令的受控路由子集。它们都由 `Session.handleRequest` 在默认后端转发之前分流：`CLIENT LIST` 与 Stream 路由默认生效，`CLUSTER NODES` 需要显式配置才开启。

## CLIENT LIST

`CLIENT LIST` 返回当前 proxy 进程的活动客户端连接快照，语义是“这个 proxy 实例接入的 Redis 连接”。它在 `pkg/proxy/client_list.go` 的 `clientSessionRegistry.handleRequestClient` 中本地处理：`Session.handleRequest` 的 `CLIENT` 分支位于默认后端转发之前，`LIST` 之外的同族子命令也在这个分支里被拒绝，不会被路由到后端 slot。

参数形态：

| 请求 | 行为 |
| --- | --- |
| `CLIENT LIST` | 返回全部活动 session，按 `id` 升序 |
| `CLIENT LIST TYPE normal` | 等价于 `CLIENT LIST` |
| `CLIENT LIST TYPE master` / `replica` / `pubsub` | 返回空 bulk string：proxy 不把这些角色暴露为客户端 session |
| `CLIENT LIST TYPE <unknown>` | `ERR Unknown client type '<type>'` |
| `CLIENT LIST ID <id> [id ...]` | 只返回 id 命中的 session，不存在的 id 被忽略；非数字返回 `ERR Invalid client ID` |
| 其他参数组合 | `ERR syntax error` |

其他 `CLIENT` 子命令（`CLIENT KILL`、`CLIENT SETNAME`、`CLIENT INFO`、`CLIENT TRACKING` 等）返回 `ERR unsupported CLIENT subcommand '<subcommand>'`，不进入后端路由。

字段子集（`formatClientList` 固定输出，空格分隔、行尾 `\n`）：

| 字段 | 取值来源 | 说明 |
| --- | --- | --- |
| `id` | session registry 递增 ID | 稳定的进程内标识，可用于 `CLIENT LIST ID` |
| `addr` / `laddr` | 客户端连接的远端/本地地址 | 与 proxy 到后端的连接无关 |
| `name` | 恒为空 | 不支持 `CLIENT SETNAME` |
| `age` / `idle` | session 创建时间、最近一次命令时间 | 单位为秒 |
| `flags` | 恒为 `N` | 不区分 `P` / `S` / `M` 等角色 |
| `db` | session 当前 db | 受 ACL DB 绑定影响 |
| `cmd` | 最近一次命令名 | 尚未执行过命令时为 `NULL` |
| `user` | ACL 用户名；无 ACL 身份且已授权时为 `default` | 尚未通过认证的 session 输出空字符串 |
| `events` | 恒为 `r` | 不支持 pub/sub，不存在 `rw` 组合 |
| `resp` | 恒为 `2` | proxy 只有 RESP2 encoder |
| `sub` `psub` `ssub` `multi` `qbuf` `qbuf-free` `obl` `oll` `omem` `redir` | 固定占位值 `0` 或 `-1` | 没有真实状态来源 |

不输出 `tot-net-in` / `tot-net-out`、`lib-name` / `lib-ver`、`io-thread` 等 Redis 8.x 字段：proxy 没有对应真实数据，宁缺不伪造。

与后端 server 的关系与数据来源：

- 不向后端 Redis 发送 `CLIENT LIST`，也不暴露 proxy 到后端的连接池连接；要看后端连接应在对应 Redis 上执行命令。
- 不聚合 dashboard/topom 中所有 proxy 的连接；查看其他 proxy 的连接必须连到那个 proxy。
- 数据来自 `clientSessionRegistry`：`Session.Start` 调 `register` 并同时执行 `proxy_max_clients` 上限判断，writer 退出时 `unregister`；`snapshot` 在短锁内复制 session 指针，释放锁后再格式化。
- 该 registry 同时是 `SessionsAlive()` 的权威来源，只在单 proxy 进程内存在，不写入 coordinator、不跨 proxy 同步。

## CLUSTER NODES

`CLUSTER NODES` 是可选的伪集群视图，供只依赖该命令做启动发现的 cluster-mode SDK 使用。清单里的每个节点都是等价的 Codis Proxy，返回的 slot 归属只是让 SDK 选择连接，不代表真实数据所有权；实现集中在 `pkg/proxy/cluster_nodes.go` 的 `clusterNodesProvider`。

三种模式由 `config/proxy.toml` 的 `cluster_nodes_compat` 控制，取值校验在 `pkg/proxy/config.go` 的 `Config.Validate`：

| 模式 | 节点来源 | 输出 |
| --- | --- | --- |
| `disabled`（默认） | 无 | `CLUSTER` 与既有不允许命令同路径，返回错误；开启 `session_break_on_failure` 时关闭连接 |
| `self` | 当前 proxy 的 `models.Proxy`，不访问 coordinator | 一行当前 proxy，slot range 为 `0-16383` |
| `all` | 后台按 `cluster_nodes_refresh_period`（默认 `30s`）轮询 Jodis/coordinator 的 proxy 注册信息 | 每个 online proxy 一行，`0-16383` 均分 |

`all` 模式要求同时配置 `jodis_name` 与 `jodis_addr`，并兼容 `jodis_auth`、`jodis_timeout`、`jodis_compatible`。命令路径只读原子快照，不等待 coordinator IO：轮询失败时保留上一份成功快照并写日志，从未成功过则退回 self 快照。同一份 proxy 列表在各实例上输出一致，当前 proxy 只影响 `myself` 标记。

输出行格式与取值：

```text
<node-id> <host:port>@<bus-port> <flags> - 0 0 0 connected <slot-range>
```

| 字段 | 取值 |
| --- | --- |
| `node-id` | `fakeClusterNodeID` 对 `product_name`、proxy token、addr 做 SHA1，取 40 位十六进制 |
| `host:port` | proxy 对客户端的监听地址 |
| `bus-port` | TCP 端口 + 10000；端口解析失败填 `0`。proxy 不监听 cluster bus，该字段只为格式完整 |
| `flags` | 当前 proxy 为 `myself,master`，其他为 `master` |
| `master` | 固定 `-`，proxy 之间没有主从关系 |
| `ping` / `pong` / `epoch` | 固定 `0 0 0` |
| `link` | 固定 `connected` |
| `slot-range` | `all` 模式按 `16384 / N` 均分，前 `16384 % N` 个节点多 1 个 slot；N=3 时为 `0-5461`、`5462-10922`、`10923-16383` |

与真实 Redis Cluster 的差异，也是本能力的边界：

- `0-16383` 只是面向 SDK 的逻辑 slot 标签。Codis 真实路由仍由 `pkg/models/slots.go` 的 `MaxSlotNum = 1024` 和 dashboard 下发的 slot mapping 决定，真实归属用 `SLOTSINFO` / `SLOTSMAPPING` 查询。
- 不存在 16384 slot 的数据所有权、slot 迁移或 rebalance；迁移仍由 Codis slot action 完成。
- 不实现 `MOVED` / `ASK`、cluster bus、gossip、failover 和 Redis Cluster 配置文件。
- 不做 proxy 之间的 RPC、探活或广播；`all` 模式只读取 Jodis/coordinator 中已注册的 proxy，并按 token、addr 去重（当前 proxy 优先保留）。
- 不读取 Consul service catalog、health check 或 service mesh；Consul 只通过 `models.Client` 的 KV/Session 语义参与。
- `CLUSTER SLOTS`、`CLUSTER INFO`、`CLUSTER KEYSLOT`、`CLUSTER SHARDS` 等其他子命令返回 `ERR unsupported CLUSTER subcommand '<subcommand>'`，不转发后端。
- 不承诺所有 cluster-mode SDK 可用：SDK 若强制调用 `CLUSTER SLOTS` 或依赖重定向，本能力无法补足。

## Stream 命令

Stream 支持是 proxy 的**受控路由子集**：命令属性写进 `pkg/proxy/mapper.go` 的 `opTable`（由 `registerStreamCommandOps` 注入 `streamCommandSpecs`），key 位置由 `pkg/proxy/stream_commands.go` 的 `resolveStreamRoute` 解析，`Router.handleRequestStream` 完成转发。它保证请求落到正确 slot，不承诺完整 Stream 语义，也不做 Stream emulation。

| 命令 | 参数路由 | 行为 |
| --- | --- | --- |
| `XADD`、`XACK`、`XACKDEL`、`XAUTOCLAIM`、`XCFGSET`、`XCLAIM`、`XDEL`、`XDELEX`、`XIDMPRECORD`、`XSETID`、`XTRIM` | 第 1 参数是 stream key | 写命令，走 master，并按解析到的 stream key 触发写后热 key 缓存失效 |
| `XLEN`、`XPENDING`、`XRANGE`、`XREVRANGE` | 第 1 参数是 stream key | 只读命令，`IsMasterOnly` 为 false，可按现有规则走 replica |
| `XGROUP CREATE` / `SETID` / `DESTROY` / `CREATECONSUMER` / `DELCONSUMER` | 第 2 参数是 stream key，子命令不是 key | 写命令 |
| `XINFO STREAM` / `GROUPS` / `CONSUMERS` | 第 2 参数是 stream key | 只读命令 |
| `XREAD` | 从 `STREAMS` 之后解析 key/id 对 | 只读命令 |
| `XREADGROUP GROUP ...` | 从 `STREAMS` 之后解析 key/id 对，第 2 参数必须是 `GROUP` | 写命令：会改动 consumer group 与 PEL，即使 key spec 标为只读 |

关键差异是容器命令按子命令定位 key：`XGROUP CREATE mystream ...` 按 `mystream` 路由，而不是把 `CREATE` 当 key 发到错误 slot。解析只扫描当前命令参数，不加载全局命令表，也不做每请求 JSON 解析。

`XREAD` / `XREADGROUP` 的放行条件：

| 形态 | 行为 |
| --- | --- |
| 单 stream key，不带 `BLOCK` | 支持，按该 key 路由 |
| 多 stream key，共享同一 hash tag（如 `{u1}:a`、`{u1}:b`） | 支持，整条命令转发到第一个 key 所在 slot |
| 多 stream key，hash tag 不一致，或裸 key 与带 tag key 混用 | `CROSSSLOT Stream keys in request don't share the same hash tag` |
| 带 `BLOCK` | `ERR unsupported blocking Stream command '<cmd>'` |
| 缺少 `STREAMS` | `ERR missing STREAMS for '<cmd>' command` |
| key 与 id 数量不一致或不成对 | `ERR Stream keys and IDs must be paired for '<cmd>' command` |
| `XREADGROUP` 不以 `GROUP` 开头或参数不足 | `ERR wrong number of arguments for 'XREADGROUP' command` |

放行条件比较的是 hash tag / key 原文是否一致，而不只是 `Hash(key) % 1024` 相同：迁移期同 slot 但不同 tag 的 key 不会进入同一个 `SLOTSMGRTTAGONE` 批次。proxy 不拆分 multi-stream 请求、不并发转发、不合并响应。

拒绝与错误语义：

- 被拒绝的请求由 proxy 直接返回 Redis error，不转发到任何后端 slot，也不关闭连接。
- `XGROUP HELP`、`XINFO HELP` 和未知 Stream 子命令返回 `ERR unsupported Stream subcommand for '<cmd>'`，不为静态 help 文本随机挑一个后端。
- Stream 支持不改变 `MULTI` / `EXEC`、`EVAL` / `EVALSHA`、`SCRIPT`、pub/sub、Redis Cluster 和 Codis 迁移命令的既有 allow/deny 策略，不扩大危险命令面。
- 后端仍是 Redis 3 时 proxy 仍按 stream key 正确路由，但最终由后端返回 unknown command；proxy 不伪造成功响应。

## 已知路由缺口

`opTable` 是手写命令表，没有消费 Redis 8 的 command metadata。未命中时 `getOpInfo` 返回大写命令名和 `FlagMayWrite`，`getHashKey` 默认把第 1 参数当 key。这个 fallback 决定了剩余缺口：key 恰好在第 1 参数的命令“看起来能用”，但读写语义、缓存失效和运维统计失真；key 不在第 1 参数或带 keynum 的命令会错路由。

| 能力 | 现状 | 影响 |
| --- | --- | --- |
| Vector Set（`VADD`、`VSIM`、`VCARD`、`VDIM` 等） | 中风险：未进入 `opTable` | key 基本在第 1 参数，slot 通常正确；但 `VSIM` / `VCARD` / `VDIM` 等只读命令被当成 `FlagMayWrite`，不能走 replica 读，启用热 key 缓存时会触发 DB 级保守清理 |
| Redis Stack 模块（RedisJSON / RedisBloom / RedisTimeSeries / RediSearch） | 默认不构建：`Makefile` 构建 `codis-server` 时未打开 `BUILD_WITH_MODULES`；proxy 也没有模块 key-spec 支持 | 启用后模块命令按未知命令处理，只有 key 恰好在第 1 参数时才碰巧正确；模块命令自带的 key spec 不会被消费 |
| Hash field expiration 及 Hash 新命令（`HEXPIRE`、`HPEXPIRE`、`HTTL`、`HGETEX`、`HSETEX`） | 属“现有数据结构的新命令未同步” | key 在第 1 参数，路由大体可用，但读写 flag 不准：写命令被当成 `FlagMayWrite`，缓存失效退化为 DB 级保守清理 |
| 其他多 key / keynum 新命令（`LMPOP`、`ZMPOP`、`SINTERCARD`、`ZINTER`、`ZDIFF`、`ZRANGESTORE` 等） | 未进入 `opTable` | 第 1 参数常是 key 数量或目标 key，可能错路由；多 key 形态既不做同 slot 校验，也不拆分或合并 |

Stream 缺口已修复：Stream 命令不再走未知命令 fallback，而由上文“Stream 命令”的受控子集覆盖——`XGROUP` / `XINFO` 按子命令后的 stream key 路由、`XREAD` / `XREADGROUP` 解析 `STREAMS`、阻塞与跨 hash tag 请求被拒绝。仍未覆盖的是 Vector Set、Stack 模块以及 Hash/多 key 新命令。

这些缺口统一的收敛方向是补齐 Redis 8 command metadata 或等价的受控 key spec，而不是把未知命令 fallback 当作正式支持。当前没有逐个打开这些命令的配置开关，也没有模块 key-spec 的动态加载机制。

## 相关文档

- [unsupported_cmds.md](unsupported_cmds.md)：proxy 拒绝与半支持命令清单；本页是它在 `CLIENT LIST`、`CLUSTER NODES` 和 Stream 三处边界的展开说明。
- [architecture_zh.md](architecture_zh.md)：proxy 命令分流、session registry、cluster nodes provider 与 Stream 路由在整体架构中的位置。
- [codis_acl_zh.md](codis_acl_zh.md)：ACL 模式下本地命令的 `ACL DRYRUN` 校验、`CLIENT LIST` 的 `user` 字段与 DB 绑定对 `db` 字段的影响。
