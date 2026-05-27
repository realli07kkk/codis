These commands are disallowed in codis proxy, if you use them, proxy will close the connection to warn you.

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
|                  |                  |
|   Codis Slot     | SLOTSCHECK       |
|                  | SLOTSDEL         |
|                  | SLOTSINFO        |
|                  | SLOTSMGRTONE     |
|                  | SLOTSMGRTSLOT    |
|                  | SLOTSMGRTTAGONE  |
|                  | SLOTSMGRTTAGSLOT |


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


These commands is "half-supported". Codis does not support cross-node operation, so you must use Hash Tags (See [this blog](http://oldblog.antirez.com/post/redis-presharding.html)'s "Hash tags" section) to put all the keys which may shown in one request into the same slot then you can use these commands. Codis does not check if the keys have same tag, so if you don't use tag, your program will get wrong response.

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
