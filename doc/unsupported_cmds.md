# Codis Proxy command compatibility

## Rejected commands

Codis Proxy returns a Redis error for rejected commands. With the default
`session_break_on_failure = false`, the connection stays open. Setting it to
`true` can close the session for command-dispatch errors.

|   Command Type   |   Command Name   |
|:----------------:|:---------------- |
|   Keys           | KEYS             |
|                  | MIGRATE          |
|                  | MOVE             |
|                  | OBJECT           |
|                  | RANDOMKEY        |
|                  | RENAME           |
|                  | RENAMENX         |
|                  | SCAN             |
|                  |                  |
|   Strings        | BITOP            |
|                  | MSETNX           |
|                  |                  |
|   Lists          | BLPOP            |
|                  | BRPOP            |
|                  | BRPOPLPUSH       |
|                  |                  |
|   Pub/Sub        | PSUBSCRIBE       |
|                  | PUBLISH          |
|                  | PUNSUBSCRIBE     |
|                  | SUBSCRIBE        |
|                  | UNSUBSCRIBE      |
|                  |                  |
|   Transactions   | DISCARD          |
|                  | EXEC             |
|                  | MULTI            |
|                  | UNWATCH          |
|                  | WATCH            |
|                  |                  |
|   Scripting      | SCRIPT           |
|                  |                  |
|   Server         | BGREWRITEAOF     |
|                  | BGSAVE           |
|                  | ACL (except ACL WHOAMI when Codis ACL is active) |
|                  | CLIENT (except CLIENT LIST) |
|                  | CLUSTER (except CLUSTER NODES when explicitly enabled) |
|                  | CONFIG           |
|                  | DBSIZE           |
|                  | DEBUG            |
|                  | FLUSHALL         |
|                  | FLUSHDB          |
|                  | LASTSAVE         |
|                  | LATENCY          |
|                  | MONITOR          |
|                  | PSYNC            |
|                  | REPLCONF         |
|                  | RESTORE          |
|                  | SAVE             |
|                  | SHUTDOWN         |
|                  | SLAVEOF          |
|                  | SLOWLOG          |
|                  | SYNC             |
|                  | TIME             |
|                  | WAIT             |
|                  |                  |
|   Redis Cluster  | ASKING           |
|                  | READONLY         |
|                  | READWRITE        |
|                  | RESTORE-ASKING   |
|                  |                  |
|   Codis Slot     | SLOTSCHECK       |
|                  | SLOTSDEL         |
|                  | SLOTSMGRTONE     |
|                  | SLOTSMGRTSLOT    |
|                  | SLOTSMGRTTAGONE  |
|                  | SLOTSMGRTTAGSLOT |
|                  | SLOTSMGRTONE-ASYNC |
|                  | SLOTSMGRTSLOT-ASYNC |
|                  | SLOTSMGRTTAGONE-ASYNC |
|                  | SLOTSMGRTTAGSLOT-ASYNC |
|                  | SLOTSMGRT-ASYNC-FENCE |
|                  | SLOTSMGRT-ASYNC-CANCEL |
|                  | SLOTSMGRT-ASYNC-STATUS |
|                  | SLOTSMGRT-EXEC-WRAPPER |
|                  | SLOTSRESTORE-ASYNC |
|                  | SLOTSRESTORE-ASYNC-AUTH |
|                  | SLOTSRESTORE-ASYNC-ACK |

## Supported extensions

`SLOTSINFO <backend-addr>` queries slot key counts on the specified backend.
`SLOTSMAPPING` reports the proxy's current slot routing table.
`SLOTSSCAN <slot> <cursor> [COUNT n]` iterates the keys of one slot and returns
the next cursor plus a key batch. `SLOTSHASHKEY key [key ...]` returns the
Codis slot id of each key. Both are Codis Server commands that the proxy
routes: `SLOTSSCAN` by its slot argument, `SLOTSHASHKEY` by its first key.

`SLOTSRESTORE key ttlms payload [key ttlms payload ...]` writes RDB fragments
straight into the slots of the given keys and is the write path used by
migration tooling. The proxy routes the request by its first key, so every key
in one `SLOTSRESTORE` request must belong to the same slot. Plain `RESTORE`
stays rejected because it carries no slot for the proxy to route on.

`CLIENT LIST` is supported by codis proxy and returns the client connections
attached to the current proxy instance. Other `CLIENT` subcommands are still
disallowed.

`CLUSTER NODES` is supported only when `cluster_nodes_compat` is set to `self`
or `all` in codis proxy config. It returns a limited fake Redis Cluster node
list for cluster-mode client bootstrap. Other `CLUSTER` subcommands are still
disallowed, and codis proxy does not implement Redis Cluster routing, MOVED/ASK,
cluster bus, or failover semantics. Old configs keep the default `disabled`
behavior; `all` mode depends on Jodis proxy registrations and filters duplicate
or invalid records before building the fake node list.

Redis Stream commands are supported as a constrained Redis 8 proxy-routing
subset. Single-key commands such as `XADD`, `XDEL`, `XTRIM`, `XACK`, `XLEN`,
`XPENDING`, `XRANGE`, and `XREVRANGE` are routed by their stream key. `XGROUP`
supports `CREATE`, `SETID`, `DESTROY`, `CREATECONSUMER`, and `DELCONSUMER`,
and `XINFO` supports `STREAM`, `GROUPS`, and `CONSUMERS`; both route by the
stream key argument after the subcommand. Non-blocking `XREAD` and `XREADGROUP`
are supported when all stream keys in one request share the same hash tag/key.
`XREAD` / `XREADGROUP` with `BLOCK`, cross-hash-tag multi-stream requests,
`XGROUP HELP`, `XINFO HELP`, and unknown Stream subcommands are rejected by
proxy instead of being forwarded to an arbitrary backend slot.


## Commands that require colocated keys

For the following commands, use the same Codis hash tag for all keys in a
request so they route to the same slot. The proxy forwards the command to
one backend and leaves key colocation to the caller. For additional limits
on newer commands, see [Proxy protocol extensions](proxy_protocol_extensions_zh.md#已知路由缺口).

|   Command Type   |   Command Name   |
|:----------------:|:---------------- |
|   Lists          | RPOPLPUSH        |
|                  |                  |
|   Sets           | SDIFF            |
|                  | SINTER           |
|                  | SINTERSTORE      |
|                  | SMOVE            |
|                  | SUNION           |
|                  | SUNIONSTORE      |
|                  |                  |
|   Sorted Sets    | ZINTERSTORE      |
|                  | ZUNIONSTORE      |
|                  |                  |
|   HyperLogLog    | PFMERGE          |
|                  |                  |
|   Scripting      | EVAL             |
|                  | EVALSHA          |
