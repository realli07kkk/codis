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
```

配置含义：

- `hot_key_cache_enabled`：是否开启本 proxy 进程内缓存，默认 `false`。
- `hot_key_cache_ttl`：缓存值可被返回的最长时间，也是跨 proxy 或直连后端写入后的最大收敛窗口。
- `hot_key_cache_max_entries`：单个 proxy 进程最多保留的缓存条目数。
- `hot_key_cache_max_value_size`：单个 string value 的最大缓存大小，超过后直通返回但不缓存。
- `hot_key_cache_keys`：允许缓存的 exact key 列表，不支持通配符、prefix 或正则。

## 行为边界

- 只缓存 Redis string 的完整 `GET` / `MGET` bulk value。
- 不缓存 nil bulk、Redis error、超过大小限制的 value。
- 不缓存 hash、list、set、zset、stream 或 module value。
- 不缓存 `GETRANGE`、`GETBIT`、`STRLEN`、`TTL`、`PTTL` 等派生读结果。
- 不自动发现热 key，不维护 per-key QPS 排行。

## 一致性模型

缓存只存在于单个 proxy 进程内。通过同一个 proxy 执行 `SET`、`MSET`、`DEL`、`EXPIRE`、`PERSIST` 等写命令时，本地缓存会被失效；下一次读取会访问后端 Redis 并重新填充。

如果写入发生在另一个 proxy，或客户端直连后端 Redis Server，当前 proxy 不会收到实时失效通知，只能在 `hot_key_cache_ttl` 到期后收敛。因此该能力适合能接受短时间旧值的热点读场景，不适合要求跨 proxy 强一致的 key。

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
