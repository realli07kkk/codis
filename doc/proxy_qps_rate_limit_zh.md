# Codis Proxy QPS 限流

Codis Proxy 支持按 QPS 对普通 Redis 请求做进程级限流。该能力默认关闭，用于在突发流量或误用场景下保护单个 proxy 进程和后端 Redis。

## 配置

proxy 本地配置项：

```toml
proxy_qps_limit = 0
```

配置含义：

- `proxy_qps_limit = 0`：不限流，默认值。
- `proxy_qps_limit > 0`：限制当前单个 proxy 进程每秒接受的普通请求数量。
- `proxy_qps_limit < 0`：proxy 配置校验失败。

该阈值是 per-proxy 语义。假设同一个 product 下有 3 个 proxy，并设置 `proxy_qps_limit = 10000`，则每个 proxy 各自最多接受约 10000 QPS，整体上限约为 30000 QPS，不是 3 个 proxy 共享 10000 QPS。

## Dashboard 管理

proxy 本地配置只决定启动时的初始运行态。proxy 加入 dashboard 管理后，可以通过 codis-fe 页面中的 `Proxy QPS Limit` 区域修改 product 级目标配置。dashboard/topom 会把目标配置写入 coordinator：

```text
/codis3/{product}/proxy_qps_limit
```

然后下发给当前 online proxy。新 proxy create、online 或 reinit 时，dashboard/topom 会重放 coordinator 中已有 revision 的目标配置。

管理 API：

```text
GET /api/topom/proxy/qps-limit/:xauth
PUT /api/topom/proxy/qps-limit/:xauth
```

PUT body 示例：

```json
{
    "limit": 20000
}
```

返回字段包含 `revision`、`limit`、`enabled` 和 `sync_status`。`sync_status` 可能是：

- `not_configured`：coordinator 里还没有 dashboard 管理的目标配置。
- `ready`：目标 revision 已成功下发给当前 online proxy，或当前没有 online proxy。
- `proxy_sync_failed:<token>`：目标配置已保存，但部分 proxy 下发失败。可以排查对应 proxy admin 地址连通性后重新提交或 reinit。

## 请求行为

QPS 限流只作用于普通 Redis 请求。`AUTH`、`QUIT` 和 proxy admin HTTP API 不被 QPS limit 阻断，避免错误配置后无法认证、退出或恢复。

超过当前 proxy 进程预算的请求会立即返回：

```text
ERR proxy qps limit exceeded
```

被拒绝的请求不会访问后端 Redis，也不会等待 token。pipeline 中每条命令独立消耗一次 budget，accepted 和 rejected response 按原始请求顺序写回。

## 运行期调整

dashboard/topom 下发配置时会调用单个 proxy 的 admin API：

```text
PUT /api/proxy/qps-limit/:xauth
```

该接口只负责修改当前 proxy 进程运行态，不写 coordinator。运维应优先通过 codis-fe 或 dashboard/topom API 修改 product 级配置，避免单 proxy 临时值在下一次 dashboard sync 或 reinit 后被覆盖。

业务客户端的 Redis 协议 `CONFIG` 命令仍不开放。当前版本不支持通过业务 Redis 连接执行 `CONFIG SET proxy_qps_limit ...` 修改限流阈值。

## 观测

开启 QPS limit 或 dashboard 下发过 revision 后，proxy stats JSON 中包含 `qps_limit` 字段：

```json
{
    "qps_limit": {
        "revision": 1,
        "limit": 20000,
        "enabled": true,
        "accepted": 123456,
        "rejected": 89
    }
}
```

其中 `accepted` 和 `rejected` 是当前 proxy 进程内累计计数。`rejected` 只表示 QPS limiter 拒绝的请求数量，不等同于后端 Redis error 数。
