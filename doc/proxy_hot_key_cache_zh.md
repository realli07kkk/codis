# Codis Proxy 热 key 缓存

Codis Proxy 支持对显式配置的 string 热 key 做进程内短 TTL 读缓存。该能力默认关闭，用于缓解少量高频 `GET` / `MGET` 请求对后端 Redis 的读放大。

## 配置

在 proxy 配置中开启：

```toml
hot_key_cache_enabled = true
hot_key_cache_ttl = "1s"
hot_key_cache_max_entries = 1024
hot_key_cache_max_value_size = "64kb"
hot_key_cache_keys = ["hot-key-1", "hot-key-2"]
hot_key_cache_broadcast_enabled = false
hot_key_cache_broadcast_timeout = "100ms"
hot_key_cache_broadcast_queue_size = 1024
```

配置含义：

- `hot_key_cache_enabled`：是否开启本 proxy 进程内缓存，默认 `false`。
- `hot_key_cache_ttl`：缓存值可被返回的最长时间，也是跨 proxy 或直连后端写入后的最大收敛窗口。
- `hot_key_cache_max_entries`：单个 proxy 进程最多保留的缓存条目数。
- `hot_key_cache_max_value_size`：单个 string value 的最大缓存大小，超过后直通返回但不缓存。
- `hot_key_cache_keys`：允许缓存的 exact key 列表，不支持通配符、prefix 或正则。
- `hot_key_cache_broadcast_enabled`：是否启用跨 proxy 写后失效广播，默认 `false`。
- `hot_key_cache_broadcast_timeout`：source proxy 上报 dashboard/topom 以及 dashboard/topom 通知目标 proxy 的单次超时预算。
- `hot_key_cache_broadcast_queue_size`：source proxy 内部异步广播队列大小；队列满时丢弃本次广播事件，写命令响应不受影响。

## 行为边界

- 只缓存 Redis string 的完整 `GET` / `MGET` bulk value。
- 不缓存 nil bulk、Redis error、超过大小限制的 value。
- 不缓存 hash、list、set、zset、stream 或 module value。
- 不缓存 `GETRANGE`、`GETBIT`、`STRLEN`、`TTL`、`PTTL` 等派生读结果。
- 不自动发现热 key，不维护 per-key QPS 排行。

## 一致性模型

缓存只存在于单个 proxy 进程内。通过同一个 proxy 执行 `SET`、`MSET`、`DEL`、`EXPIRE`、`PERSIST` 等写命令时，本地缓存会被失效；下一次读取会访问后端 Redis 并重新填充。

如果开启 `hot_key_cache_broadcast_enabled`，写命令经过某个 proxy 并成功返回后，该 source proxy 会把可精确枚举的 DB+key 失效事件放入本地有界队列；短窗口内相同 DB+key 会合并，然后异步上报给 dashboard/topom 管理面。dashboard/topom 再调用其余 online proxy 的 admin HTTP API，让目标 proxy 检查本地 allowlist 并删除对应缓存条目。这里的“刷新”是删除旧 entry 后等待下一次读请求从后端 Redis 重新加载，不会把新 value 主动写入所有 proxy。广播 API 使用 `keys_base64` 承载 Redis key 原始字节，避免非 UTF-8 key 被 JSON string 改写。

广播链路是 best-effort：广播队列满、dashboard/topom 不可达、目标 proxy admin API 超时、部分 proxy 下线或返回错误，都不会改变客户端写命令的响应；未收到通知的 proxy 仍靠 `hot_key_cache_ttl` 收敛。客户端直连后端 Redis Server 的写入也不会触发广播。因此该能力适合能接受短时间旧值的热点读场景，不适合要求跨 proxy 强一致的 key。

如果 proxy 通过 `--fillslots` 离线方式运行，没有 dashboard/topom 管理面地址，广播会自动退化为本 proxy 本地失效 + TTL 收敛。本文中的 admin 指 dashboard/topom 管理面和 proxy admin HTTP API，不指 `codis-admin` 命令行工具。

slot 处于迁移或锁定状态时，proxy 不从热 key 缓存返回结果；slot 映射更新后，该 slot 的本地缓存条目会被清理。

## 观测

开启热 key 缓存或已有缓存统计时，proxy stats JSON 中包含 `hot_key_cache` 字段，可查看：

- `enabled`
- `entries`
- `hits`
- `misses`
- `stores`
- `invalidations`
- `evictions`
- `broadcast_attempts`
- `broadcast_failures`
- `broadcast_dropped`
- `broadcast_coalesced`
- `remote_invalidations`
