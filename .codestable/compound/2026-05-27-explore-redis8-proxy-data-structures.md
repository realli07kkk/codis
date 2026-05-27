---
doc_type: explore
type: spike
date: 2026-05-27
slug: redis8-proxy-data-structures
topic: Redis 3 升级到 Redis 8 后，哪些新增数据结构或相关命令没有在 Codis proxy 同步支持
scope: pkg/proxy 命令识别、key 路由、Redis 8.6.3 command metadata、Redis 8 bundled modules
keywords: [redis8, proxy, command-table, stream, vector-set, module, key-spec]
status: active
confidence: high
---

## 问题与范围

问题：之前 Codis Server 的 Redis 版本从 3.2.11 升级到 8.6.3 后，Redis 新增的数据结构/能力里，哪些没有在 `codis-proxy` 同步支持。

范围只看“proxy 能否正确识别命令、提取 key、路由 slot、设置读写/阻塞标记”。不评估 Redis 8 server 内部实现是否完整，也不设计修复方案。

## 速答

结论：`codis-proxy` 没有同步 Redis 8 的 command metadata。最明确的数据结构缺口是 **Stream** 和 **Vector Set**；其中 Stream 是高风险缺口，Vector Set 是“多数字面可转发但没有显式支持”的中风险缺口。Redis 8 外置 Stack 模块（JSON / Bloom / TimeSeries / Search）默认没有随当前 Codis `make` 构建，但一旦启用，proxy 也没有动态 key-spec 支持。

```mermaid
flowchart LR
    C[client command] --> P[pkg/proxy getOpInfo]
    P -->|opTable hit| F[known flags + limited key parser]
    P -->|opTable miss| U[FlagMayWrite + assume argv1 is key]
    F --> R[Hash(key) % 1024]
    U --> R
    R --> B[backend slot]
    Redis8[Redis 8 command metadata] -.not imported.-> P
    Stream[Stream X* key specs] --> Redis8
    Vector[Vector Set V* commands] --> Redis8
```

分类：

- **Stream：未同步支持，且部分命令会错误路由。** `XADD key ...` 这类 key 在第 1 参数的单 key 命令可能“碰巧”进正确 slot；但 `XGROUP CREATE key ...` 的 key 在第 2 参数，proxy 会把子命令 `CREATE` 当 key；`XREAD` / `XREADGROUP` 的 key 在 `STREAMS` 后且可多 key，proxy 既不会解析，也不会拆分/合并或禁止阻塞。
- **Vector Set：未显式支持。** Redis 8 源码默认可内置 `vector_set`，命令包括 `VADD` / `VSIM` / `VCARD` 等；这些命令不在 proxy 表内。多数 V* 命令 key 在第 1 参数，所以路由可能碰巧正确，但只读命令会被当成 `MayWrite`，读副本、热 key 缓存失效和运维统计语义都会失真。
- **Redis Stack 外置模块：默认不在当前 Codis 构建里，但启用后 proxy 不具备泛化支持。** 顶层 Redis 8 Makefile 只有 `BUILD_WITH_MODULES=yes` 才进入 `modules/`；当前 Codis `Makefile` 只是 `make -C extern/redis-8.6.3/`，没有传这个开关。若未来启用 JSON / Bloom / TimeSeries / Search，proxy 仍只会按未知命令处理，无法消费模块命令自己的 key spec。
- **Hash field expiration 等不是新增数据结构，但命令也未同步。** `HEXPIRE`、`HGETEX`、`HSETEX`、`HTTL` 等属于 Hash 新能力；key 都在第 1 参数，路由大体可用，但读写 flags 不准。它们不应被归类为“数据结构不支持”，而是“现有类型的新命令元数据未同步”。

## 关键证据

1. Redis 3.2.11 只有 string/list/set/zset/hash 五类对象；没有 stream/module/vector set。证据：`extern/redis-3.2.11/src/server.h:191` 定义 Object types，`192-196` 只有 `OBJ_STRING` 到 `OBJ_HASH`。

2. Redis 8.6.3 增加了 `OBJ_MODULE` 和 `OBJ_STREAM`，Stream 是明确新增对象类型。证据：`extern/redis-8.6.3/src/server.h:875` 到 `894` 定义基础类型、module 和 stream；`extern/redis-8.6.3/src/object.c:510` 到 `514` 创建 `OBJ_STREAM`。

3. Redis 8 command metadata 明确把 Stream 命令归为 `group: stream`。例如 `XADD` 的 `group` 是 stream、`since` 是 5.0.0，key 在参数 1；证据：`extern/redis-8.6.3/src/commands/xadd.json:2` 到 `7`、`34` 到 `54`。

4. Stream 的复杂 key spec 不能被当前 proxy 的固定 key parser 覆盖。`XGROUP CREATE` 的容器是 `XGROUP`，key 从位置 2 开始；证据：`extern/redis-8.6.3/src/commands/xgroup-create.json:7` 到 `9`、`23` 到 `41`。`XREAD` 使用 `STREAMS` 关键字找多 key，且是 `BLOCKING` + `READONLY`；证据：`extern/redis-8.6.3/src/commands/xread.json:8` 到 `12`、`16` 到 `35`。

5. `codis-proxy` 的命令模型没有消费 Redis 8 metadata：`opTable` 是手写表，未知命令默认 `FlagMayWrite`；证据：`pkg/proxy/mapper.go:59` 到 `63`、`291` 到 `294`。路由时默认只通过 `getHashKey` 取一个 key，除 `ZINTERSTORE` / `ZUNIONSTORE` / `EVAL` / `EVALSHA` 外都是参数 1；证据：`pkg/proxy/mapper.go:310` 到 `319`。最终 `dispatch` 只按这个 key 算一个 Codis slot；证据：`pkg/proxy/router.go:156` 到 `160`。

6. Vector Set 在 Redis 8 源码中以内置模块形式加载：`src/Makefile` 默认 `SKIP_VEC_SETS?=no`，支持时加 `INCLUDE_VEC_SETS=1`；server 启动时加载 internal modules；证据：`extern/redis-8.6.3/src/Makefile:56`、`340` 到 `344`，`extern/redis-8.6.3/src/server.c:7978` 到 `7981`，`extern/redis-8.6.3/src/module.c:12944` 到 `12950`。Vector Set 命令 metadata 在 `extern/redis-8.6.3/modules/vector-sets/commands.json:2` 到 `7`、`123` 到 `131`、`237` 到 `246`。

7. 外置 Redis Stack 模块不是当前默认 Codis server 构建的一部分：Redis 8 顶层 Makefile 只有 `BUILD_WITH_MODULES=yes` 才把 `modules` 加入构建；模块 Makefile 列出 `redisjson redistimeseries redisbloom redisearch`；当前 Codis `Makefile` 的 `codis-server` 目标只执行 `make -j4 -C $(REDIS8_DIR)/`，没有设置 `BUILD_WITH_MODULES`。证据：`extern/redis-8.6.3/Makefile:3` 到 `9`、`extern/redis-8.6.3/modules/Makefile:2`、`Makefile:30` 到 `34`。

## 细节展开

### Stream 缺口

`XADD`、`XLEN`、`XRANGE`、`XDEL`、`XTRIM` 这类 key 在第 1 参数的命令，当前 proxy 会因为未知命令 fallback 到 `FlagMayWrite` + `argv[1]`，因此有机会发到正确 slot。但这不是同步支持，只是参数位置碰巧匹配。

风险命令：

- `XGROUP ...` / `XINFO ...`：proxy 看到的主命令是 `XGROUP` / `XINFO`，会把第 1 参数的子命令当 key，导致错误 slot。
- `XREAD` / `XREADGROUP`：key 位于 `STREAMS` 之后，且可多 key；proxy 不解析 `STREAMS`，也没有拆分多 slot 请求或限制 blocking 命令。
- `XREAD BLOCK` / `XREADGROUP BLOCK`：老的 `BLPOP` / `BRPOP` 在 proxy 表中被 `FlagNotAllow` 禁止，但新 Stream blocking 命令不在表中，会作为未知命令下发。

### Vector Set 缺口

Vector Set 的命令在 `modules/vector-sets/commands.json` 中声明为 `group: vector_set`。这些命令没有进入 `pkg/proxy/mapper.go` 的 `opTable`。由于它们的 key 基本都在第 1 参数，核心 slot 路由通常不会像 `XGROUP` 那样错位；但读写语义不准，会带来这些影响：

- `VSIM` / `VCARD` / `VDIM` 等只读命令会被当成 `MayWrite`，不会走 replica 读路径。
- 启用 hot key cache 时，未知 `MayWrite` 命令会触发保守的 DB 级缓存清理。
- 运维统计和未来命令策略无法区分 Vector Set 读/写/只读快速命令。

### 外置模块与现有类型新命令

Redis 8 源码树中有 RedisJSON / RedisBloom / RedisTimeSeries / RediSearch 的构建入口，但当前 Codis 默认构建没有打开 `BUILD_WITH_MODULES=yes`。所以默认 Redis 8 Codis Server 里不能把 JSON/Bloom/TimeSeries/Search 视为已启用业务数据结构。

现有类型的新命令也有缺口，但它们不是新增数据结构：

- Hash field expiration：`HEXPIRE` / `HPEXPIRE` / `HTTL` / `HGETEX` / `HSETEX` 等。
- List / Set / ZSet 新命令：`LMPOP` / `BLMPOP` / `SINTERCARD` / `ZMPOP` / `BZ(M)POP` / `ZINTER` / `ZDIFF` / `ZRANGESTORE` 等。
- String / generic 新命令：`GETEX` / `GETDEL` / `LCS` / `COPY` / `UNLINK` / `MSETEX` 等。

这些命令里，单 key 且 key 在第 1 参数的多半能被转发；多 key、keynum、目标 key + 源 key、blocking、container subcommand 语义则需要逐条处理，不能用“未知命令 fallback”当作正式支持。

## 未决问题

- 是否要把“支持 Redis 8 新数据结构”定义为只允许单 key 命令，还是要支持多 key 同 slot / 跨 slot 拆分 / blocking 语义。
- Vector Set 是否是目标部署必须开启的能力；如果构建环境缺 C11 atomic，`SKIP_VEC_SETS` 会被置为 yes，Vector Set 可能不进入实际二进制。
- 是否计划启用 `BUILD_WITH_MODULES=yes`。如果启用 RedisJSON / RedisBloom / RedisTimeSeries / RediSearch，proxy 需要额外设计模块命令 key-spec 来源。

## 后续建议

建议下一步把 Stream 作为独立 feature 先设计：先明确禁止/支持哪些 X* 命令，再补 proxy 的 command metadata、key 提取和 blocking 策略；Vector Set 和 Hash field TTL 可作为后续“Redis 8 命令元数据同步”批次处理。

## 相关文档

- `.codestable/roadmap/redis8-upgrade/redis8-upgrade-roadmap.md`
- `.codestable/features/2026-05-14-redis8-build-config-packaging/redis8-command-metadata-audit.md`
- `.codestable/features/2026-05-14-redis8-go-component-adapters/redis8-go-component-adapters-design.md`
