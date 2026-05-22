---
doc_type: feature-acceptance
feature: 2026-05-22-proxy-cluster-nodes-compat
status: current
accepted_at: 2026-05-22
summary: Codis Proxy 已按设计新增默认关闭的 CLUSTER NODES 有限兼容能力，架构与 requirement 已回写
tags: [proxy, redis-protocol, cluster-compat, jodis, acceptance]
---

# proxy-cluster-nodes-compat 验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-05-22
> 关联方案 doc：`.codestable/features/2026-05-22-proxy-cluster-nodes-compat/proxy-cluster-nodes-compat-design.md`

## 1. 接口契约核对

对照方案第 2.1 节名词层逐一核查：

- [x] `CLUSTER` 顶层命令从全局禁用变成本地受控容器：`pkg/proxy/mapper.go` 已允许 `CLUSTER` 进入本地分支；默认 disabled 时仍返回 `command 'CLUSTER' is not allowed`。
- [x] `cluster_nodes_compat` 支持 `disabled` / `self` / `all`：`pkg/proxy/config.go` 已新增配置字段和枚举校验，空字符串按 `disabled` 归一，保持旧程序化 `Config` 零值兼容。
- [x] `cluster_nodes_refresh_period` 控制 all 模式轮询周期：`all` 模式要求该值为正；disabled/self 不依赖后台轮询。
- [x] `CLUSTER NODES` 输出格式落地：`pkg/proxy/cluster_nodes.go` 生成 40 位 hex fake node id、`host:port@bus-port`、`master` / `myself,master`、`connected` 和 slot range。
- [x] `self` 模式只返回当前 proxy，slot range 为 `0-16383`。
- [x] `all` 模式复用 Jodis/coordinator 后端存储读取 proxy 注册清单，不引入 proxy-to-proxy 通信。

## 2. 行为与决策核对

**需求摘要逐项验证**：

- [x] cluster-mode SDK 只依赖 `CLUSTER NODES` bootstrap 时可拿到伪 Redis Cluster 节点清单。
- [x] 默认关闭，旧配置和旧程序化 `Config` 不会因为新增字段被打断。
- [x] `self` / `all` 均输出标准 Redis Cluster 逻辑 slot `0-16383`；真实 Codis 路由仍使用 1024 slot。
- [x] `all` 模式从 Jodis 注册读取 proxy 清单，后台轮询刷新，命令路径只读内存快照。

**明确不做逐项核对**：

- [x] 未实现完整 Redis Cluster 协议。
- [x] 未实现 `MOVED`、`ASK`、cluster bus、gossip、failover 或 Redis Cluster 配置文件。
- [x] 未实现 `CLUSTER SLOTS`、`CLUSTER INFO`、`CLUSTER KEYSLOT`、`CLUSTER SHARDS` 等其他子命令成功路径。
- [x] 未修改 Codis 真实 `MaxSlotNum=1024`，未把后端真实路由改成 Redis Cluster 16384 slot。
- [x] 未新增 proxy 间 RPC、探活或广播节点状态。
- [x] 未读取 Consul service catalog、health check 或 service mesh API。

**实现阶段 review 问题回归**：

- [x] `ClusterNodesCompat == ""` 已归一为 `disabled`，`TestClusterNodesConfigEmptyCompatIsDisabled` 覆盖旧程序化配置兼容。
- [x] all 模式已增加 Jodis 节点规范化，按 token / addr 去重并优先保留当前 proxy，`TestClusterNodesNormalizeDuplicateJodisNodes` 覆盖重复 token、重复 addr 和 self 优先。

## 3. 验收场景核对

- [x] 默认配置执行 `CLUSTER NODES` 仍返回不允许，不返回伪节点清单。
  - 证据来源：`TestClusterNodesDisabledByDefault`。
- [x] self 模式执行 `CLUSTER NODES` 返回当前 proxy 一行，包含 40 位 node id、`myself,master` 和 `0-16383`。
  - 证据来源：`TestClusterNodesSelfCommand`。
- [x] 未认证连接在 `SessionAuth` 非空时执行 `CLUSTER NODES` 返回 `NOAUTH Authentication required`。
  - 证据来源：`TestClusterNodesRequiresAuth`。
- [x] all 模式下多个 online proxy 返回多行，并完整覆盖 `0-16383` 且无 gap/overlap。
  - 证据来源：`TestFormatClusterNodesAndSlotDistribution`、`TestClusterNodesDiscoveryRefreshAndFallback`。
- [x] Jodis 新增或删除 proxy 节点后，后台刷新会更新 snapshot；存储失败保留 last good，空列表回退 self。
  - 证据来源：`TestClusterNodesDiscoveryRefreshAndFallback`。
- [x] 坏 JSON、空 addr、非 online state、重复 token 和重复 addr 不污染最终节点集合。
  - 证据来源：`TestClusterNodesDiscoveryRefreshAndFallback`、`TestClusterNodesNormalizeDuplicateJodisNodes`。
- [x] 其他 `CLUSTER` 子命令返回 unsupported error，不转发 backend。
  - 证据来源：`TestClusterNodesUnsupportedSubcommand`。

执行过的验证命令：

```bash
go test ./pkg/proxy -run 'Test(ClusterNodes|FormatClusterNodes|ClusterCommand)'
make gotest
git diff --check
python3 .codestable/tools/validate-yaml.py --file .codestable/features/2026-05-22-proxy-cluster-nodes-compat/proxy-cluster-nodes-compat-design.md
python3 .codestable/tools/validate-yaml.py --file .codestable/features/2026-05-22-proxy-cluster-nodes-compat/proxy-cluster-nodes-compat-checklist.yaml --yaml-only
```

## 4. 术语一致性

- `CLUSTER NODES`：代码、文档和测试均使用 Redis 原生命令名。
- `cluster_nodes_compat`：配置项只表达兼容输出模式，不暗示启用 Redis Cluster。
- `self` / `all`：语义与设计一致；`self` 是当前 proxy，`all` 是 Jodis/coordinator 中的 online proxy 快照。
- `Redis Cluster 逻辑 slot`：文档明确 `0-16383` 只用于兼容输出，不改变 Codis 1024 slot 路由。
- 防冲突：未把该能力命名为真实 cluster mode，也未修改 dashboard/topom slot 模型。

## 5. 架构归并

- [x] `.codestable/architecture/ARCHITECTURE.md` 已新增 `Cluster nodes compat` 术语。
- [x] 结构与交互已补充 `CLUSTER NODES` 是 proxy 本地命令，其他 `CLUSTER` 子命令不转发 backend。
- [x] 数据与状态已补充 cluster nodes provider、Jodis 轮询、节点清洗、snapshot 和 fallback 语义。
- [x] 代码锚点已补充 `pkg/proxy/cluster_nodes.go`。
- [x] 已知约束已补充默认禁用、有限兼容、真实 1024 slot 不变、无 Redis Cluster 协议语义、无 proxy 间通信和无 Consul catalog/health/service mesh。

## 6. requirement 回写

- [x] design frontmatter 指向 `requirement: redis-cluster-service`，该 req 当前为 `current`。
- [x] `.codestable/requirements/redis-cluster-service.md` 已新增 cluster-mode SDK bootstrap 用户故事。
- [x] “怎么解决”已补充显式启用的 `CLUSTER NODES` 兼容输出、self/all 模式和真实路由不变。
- [x] “实现进展”已追加 2026-05-22 `CLUSTER NODES` 有限兼容能力。
- [x] “边界”已补充这不是完整 Redis Cluster 协议，其他 `CLUSTER` 子命令仍不支持，`0-16383` 不改变 `MaxSlotNum=1024`。

## 7. roadmap 回写

- [x] design frontmatter 没有 `roadmap` / `roadmap_item` 字段。

结论：本 feature 非 roadmap 起头，不需要更新 `.codestable/roadmap/*`。

## 8. attention.md 候选盘点

- [x] 本 feature 未暴露新的通用构建、测试、路径或环境陷阱。

结论：不更新 `.codestable/attention.md`。

## 9. 遗留

- 已知限制：该能力只服务 `CLUSTER NODES` bootstrap，不提供 Redis Cluster 路由语义、重定向语义、failover 语义或 bus 语义。
- 运维注意：`all` 模式依赖 Jodis/coordinator 注册质量；实现会过滤明显坏记录和重复记录，但最终对外拓扑仍来自后端存储。
- 后续扩展：如要支持 `CLUSTER SLOTS` 或其他 cluster-mode SDK 行为，应另开 feature 重新定义协议边界，不应在当前本地容器里顺手放行。

验收通过。实现与设计一致，架构与 requirement 已回写，checklist checks 已全部标记为 `passed`，用户已终审确认。
