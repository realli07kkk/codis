---
doc_type: feature-acceptance
feature: 2026-05-18-consul-coordinator-sdk-upgrade
status: ready-for-review
accepted_at: 2026-05-18
summary: Consul coordinator/Jodis 后端已按设计验收，架构与 requirement 已回写
tags: [consul, coordinator, jodis, acceptance]
---

# Consul coordinator SDK upgrade 验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-05-18
> 关联方案 doc：`.codestable/features/2026-05-18-consul-coordinator-sdk-upgrade/consul-coordinator-sdk-upgrade-design.md`

## 1. 接口契约核对

对照方案第 2.1 节名词层逐一核查：

**接口示例逐项核对**：

- [x] `models.NewClient("consul", "127.0.0.1:8500", "CONSUL_HTTP_TOKEN", timeout)`：`pkg/models/client.go` 已注册 `"consul"` 分支，委托 `consulclient.New`；`pkg/models/consul/consulclient.go` 使用 `api.DefaultConfig()`、`config.Address` 和 `config.Token` 创建 Consul API client。
- [x] `Client.Create("/codis3/codis-demo/topom", data)`：`Create` 先 `cleanKey` 去掉前导 slash，再用 `KV.CAS(ModifyIndex=0)` 写入；重复创建返回已存在错误且不覆盖，`TestKVOperations` 已覆盖。
- [x] `Client.CreateEphemeral("/jodis/codis-demo/proxy-token", data)`：`CreateEphemeral` 创建 Session、`KV.Acquire` 对应 key、后台 `RenewPeriodic`，`Delete` / `Close` 会关闭 signal 并清理；`TestEphemeralLifecycle`、`TestConsulStoreAndJodisIntegration` 和真实 Consul 集成测试已覆盖。

**名词层“现状 → 变化”逐项核对**：

- [x] 新增 `pkg/models/consul` 包：已存在 `consulclient.Client`，实现 `models.Client` 所有方法。
- [x] `models.NewClient` 新增 `"consul"`：已落在 `pkg/models/client.go`。
- [x] 路径转换：实现为 `cleanKey`、`cleanPrefix`、`consulPath` 和 `list`，外部保持 slash path，内部使用 Consul KV key。
- [x] `sessionRecord`：记录 path、key、session id、stop/signal channel，供 `Delete`、`Close`、续约失败清理使用。
- [x] CLI/config 增加 `coordinator_name = "consul"` / `jodis_name = "consul"` 和 `--consul` / `--consul-auth` 挂载；dashboard CLI 沿用现状只提供地址参数，token 走 config。

**流程图核对**：

- [x] `CLI / TOML → models.NewClient → pkg/models/consul.Client → Consul KV/Session` 均有代码落点。
- [x] `dashboard/topom → models.Store → Client` 已通过 `TestConsulStoreAndJodisIntegration` 验证 `/codis3/{product}/topom` 写读。
- [x] `proxy Jodis → Client` 已通过 `proxy.New` + `Start` 触发 Jodis ephemeral 注册，并验证关闭 proxy 后节点消失。
- [x] `FE` / `Admin` 挂载点通过参数解析 grep 和 `make gotest` 编译验证。

未发现需要回改设计或生产代码的偏差。

## 2. 行为与决策核对

**需求摘要逐项验证**：

- [x] Consul 可作为 coordinator 存储：`models.NewClient("consul", ...)` 可创建 client；KV、List、Watch、Ephemeral 均有测试。
- [x] dashboard / proxy / admin / fe 可选择 Consul：usage、参数解析和默认配置注释已接入；旧 zookeeper / etcd / filesystem 分支保留。
- [x] Go module 依赖最小化：直接依赖为 `github.com/hashicorp/consul/api v1.34.2`，未引入 Consul server 主模块。
- [x] 本地 Consul 源码构建验证：使用 `/Users/liyiming/gitcode/consul` 构建的 `scripts/tmp/consul-dev` 启动 dev agent，跑通真实 Consul 集成测试。

**明确不做逐项核对**：

- [x] 默认 coordinator 未改成 Consul：`config/dashboard.toml` 仍是 `coordinator_name = "filesystem"`。
- [x] 未实现 Zookeeper / Etcd / filesystem 到 Consul 的迁移工具。
- [x] 未引入 Consul catalog、health check、service mesh API；grep 未发现 `api.Catalog`、`api.Health`、`ServiceRegister` 等调用。
- [x] 未修改 Redis proxy 路由、slot 迁移、Redis 8 Codis Server 源码或 `pkg/proxy/mapper.go`。
- [x] 生产依赖目标不是 Consul server 主模块；`go list -m all` 只显示 `github.com/hashicorp/consul/api v1.34.2`，`github.com/hashicorp/consul/sdk v0.18.1` 是 API 模块传递依赖。

**关键决策落地**：

- [x] D1：只接 `github.com/hashicorp/consul/api`，不接 Consul server 主模块。
- [x] D2：新增 `pkg/models/consul` 包，`pkg/models/client.go` 只做工厂路由。
- [x] D3：外部 slash path 不变，内部 normalize 为 Consul KV key。
- [x] D4：ephemeral 使用 Session + KV Acquire，续约失败时 best-effort destroy session，并只 CAS 删除仍归属当前 session 的 key。
- [x] D5：watch 使用 KV blocking query 的 `WaitIndex` / `WaitTime`。

**流程级约束核对**：

- [x] 错误语义：Consul API 错误通过 `errors.Trace` 返回；CAS/acquire false 转换为已存在错误。
- [x] 幂等性：`Delete` 对普通 missing key 不报错；`Close` 幂等；session destroy / key cleanup 支持重复调用。
- [x] 顺序约束：`CreateEphemeral` 先 Session 后 Acquire；Acquire 或 putSession 失败会 destroy session。
- [x] 兼容性：旧 coordinator 名称、旧配置默认值、旧 CLI 参数均保留。
- [x] 安全：Consul token 只进 API config，不写日志。

**挂载点反向核对（可卸载性）**：

- [x] M1 `models.NewClient`：`pkg/models/client.go` import + switch 分支。
- [x] M2 Consul 后端包：`pkg/models/consul`。
- [x] M3 Dashboard：`cmd/dashboard/main.go`、`pkg/topom/config.go`、`config/dashboard.toml`。
- [x] M4 Proxy / Jodis：`cmd/proxy/main.go`、`pkg/proxy/config.go`、`config/proxy.toml`。
- [x] M5 Admin / FE：`cmd/admin/admin.go`、`cmd/admin/main.go`、`cmd/fe/main.go`。
- [x] 反向 grep：`rg 'consul|Consul|hashicorp/consul' cmd pkg config go.mod` 的生产命中全部落在上述清单内；额外命中是测试、feature 文档和 Angular 第三方文本。
- [x] 拔除沙盘推演：删除 `pkg/models/consul`、`pkg/models/client.go` 的 import/switch、四类 CLI/config 挂载和 `go.mod/go.sum` 的 Consul API 依赖后，不会残留生产引用；测试引用随包删除。

## 3. 验收场景核对

- [x] **S1**：`models.NewClient("consul", "127.0.0.1:8500", "", timeout)` 返回可用 client，旧 coordinator 名称仍可用。
  - 证据来源：`go test ./pkg/models ./pkg/models/consul`、`make gotest`。
  - 结果：通过。
- [x] **S2**：同一 key `Create` 两次，第一次成功，第二次已存在且不覆盖。
  - 证据来源：`TestKVOperations`。
  - 结果：通过。
- [x] **S3**：`Update` 后 `Read`，missing read 的 optional/required 语义正确。
  - 证据来源：`TestKVOperations`。
  - 结果：通过。
- [x] **S4**：`List(prefix, false)` 返回直接子节点 slash path，排序稳定。
  - 证据来源：`TestKVOperations`、真实 Consul `TestIntegrationConsulClient`。
  - 结果：通过。
- [x] **S5**：`CreateEphemeral` 后删除或关闭，Session/KV 清理且 signal 关闭。
  - 证据来源：`TestEphemeralLifecycle`、`TestEphemeralUsesDistinctSessions`、真实 Consul `TestIntegrationConsulClient`。
  - 结果：通过。
- [x] **S6**：`WatchInOrder` 初始列表正确，新增子节点后 channel 关闭。
  - 证据来源：`TestWatchInOrder`、真实 Consul `TestIntegrationConsulClient`。
  - 结果：通过。
- [x] **S7**：dashboard/topom 元数据可写入 Consul KV 的 `codis3/{product}` 前缀。
  - 证据来源：新增 `TestConsulStoreAndJodisIntegration`，使用 `models.Store.Acquire/LoadTopom/Release` 验证 topom lock 写读。
  - 结果：通过。
- [x] **S8**：proxy Jodis 可写入 `jodis/{product}`，关闭 proxy 后注册节点消失。
  - 证据来源：新增 `TestConsulStoreAndJodisIntegration`，使用 `proxy.New` + `Start` 触发 Jodis ephemeral 注册并验证 Close 清理。
  - 结果：通过。
- [x] **S9**：`make gotest` 通过，不要求 Docker daemon。
  - 证据来源：`make gotest`。
  - 结果：通过。

执行过的验证命令：

```bash
go test ./pkg/models ./pkg/models/consul
CODIS_CONSUL_ADDR=127.0.0.1:18500 go test ./pkg/models -run TestConsulStoreAndJodisIntegration -count=1
CODIS_CONSUL_ADDR=127.0.0.1:18500 go test ./pkg/models/consul -run TestIntegrationConsulClient -count=1
go test ./pkg/models/consul
go vet ./pkg/models ./pkg/models/consul
make gotest
git diff --check
python3 .codestable/tools/validate-yaml.py --file .codestable/features/2026-05-18-consul-coordinator-sdk-upgrade/consul-coordinator-sdk-upgrade-checklist.yaml --yaml-only
```

## 4. 术语一致性

- `Consul Go API`：生产依赖只出现 `github.com/hashicorp/consul/api`，代码 import 使用 `capi` alias，符合设计。
- `Consul coordinator client`：代码落点为 `pkg/models/consul` / `consulclient.Client`，与现有 `zkclient` / `etcdclient` / `fsclient` 命名模式一致。
- `Jodis registry`：继续通过 `models.Client.CreateEphemeral` 注册 `/jodis/{product}/proxy-{token}`；Consul 只是新增后端。
- `最新 Consul`：本地 `v2.0.0-dev` server 仅作前向验证；Codis 依赖的是 `consul/api v1.34.2`。
- 防冲突：未把 Consul 写成默认 coordinator，未出现 `catalog` / `health check` / `service mesh` 生产 API 调用。

## 5. 架构归并

- [x] `.codestable/architecture/ARCHITECTURE.md`：已在术语中补充 Coordinator / Store 支持 Consul KV + Session。
- [x] `.codestable/architecture/ARCHITECTURE.md`：已在结构与交互中补充 `models.NewClient` 工厂、`pkg/models/consul`、KV CAS/List/blocking query、Session acquire/renew 编排。
- [x] `.codestable/architecture/ARCHITECTURE.md`：已在数据与状态中补充 `/codis3` / `/jodis` slash path 到 Consul KV key 的映射、Jodis session ownership 和 token 日志边界。
- [x] `.codestable/architecture/ARCHITECTURE.md`：已在代码锚点补充 `pkg/models/consul`。
- [x] `.codestable/architecture/ARCHITECTURE.md`：已在已知约束中说明 Consul 只覆盖 KV / Session，不代表 catalog / health / mesh，不改变默认 coordinator，也不提供存量迁移。

`attention.md` 暂不直接更新；候选见第 8 节。

## 6. requirement 回写

- [x] 方案 frontmatter 指向 `requirement: redis-cluster-service`，该 req 当前为 `current`。
- [x] `.codestable/requirements/redis-cluster-service.md` 已更新 `last_reviewed: 2026-05-18`。
- [x] “怎么解决”已补充底层元数据可放在 filesystem、Zookeeper、Etcd 或 Consul coordinator 中。
- [x] “实现进展”已追加 2026-05-18 Consul coordinator/Jodis 支持。
- [x] “边界”已补充 Consul 只作为 KV + Session 后端，不做存量元数据迁移，不使用 catalog / health / mesh。

`VISION.md` 索引不需要变更：能力 slug、pitch、status 都未改变。

## 7. roadmap 回写

- [x] design frontmatter 的 `roadmap` 和 `roadmap_item` 均为空。

结论：本 feature 非 roadmap 起头，不需要更新 `.codestable/roadmap/*`。

## 8. attention.md 候选盘点

- 候选 1：Consul 集成测试需要先启动本地 Consul agent，并设置 `CODIS_CONSUL_ADDR`；本次使用 `scripts/tmp/consul-dev agent -dev ... -http-port=18500`。
- 候选 2：本地 Consul 源码构建产物较大，落在 `scripts/tmp/consul-dev`，可用 `rm -rf scripts/tmp/consul-dev scripts/tmp/consul-data` 清理。

本节只登记候选，不擅自写入 `attention.md`。

## 9. 遗留

- 后续优化点：CLI coordinator 参数解析在 dashboard/proxy/admin/fe 仍有重复；建议单独走 `cs-refactor` 抽公共解析，不阻塞本 feature。
- 已知限制：不提供 Zookeeper / Etcd / filesystem 到 Consul 的存量元数据迁移；不接入 Consul catalog / health check / service mesh；不改变默认 coordinator。
- 实现阶段顺手发现：`golang.org/x/net` 因 Consul API 依赖解析显式升级到 `v0.51.0`，已通过 `make gotest` 验证。

验收通过。实现与设计一致，架构与 requirement 已回写，checklist checks 已全部标记为 `passed`。等待用户终审确认。
