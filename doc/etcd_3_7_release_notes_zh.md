# etcd 3.7 新线发布说明

**2026-11-27 · 新线首个版本。** 这是 Codis 采用 etcd 3.7 单后端后的首份发布说明：新线只支持外部部署的 etcd 3.7.x 作为 coordinator，二进制移除了旧驱动、回退开关、双写与旧注册协议。发布 tag 由发布流程在打 tag 时确定，本文按“新线首个版本”称呼。

## 1. 版本分界

| 项目 | 内容 |
| --- | --- |
| 新线起点 | 首个只支持外部 etcd 3.7.x 作为 coordinator 的 Codis 版本；Go 依赖固定 `go.etcd.io/etcd/client/v3 v3.7.4`，部署验收基线为 etcd server v3.7.4 |
| 旧线终点 | **`c635ade0424a1448fb251f32b88d64ca7cf9a555`** |
| 含义 | 该提交是 ZooKeeper、Consul、filesystem coordinator 与 Jodis 发现机制的最后支持基线。依赖其中任一能力的用户检出该 SHA，按该提交的构建指南执行 `make` |
| 服务范围 | etcd 保存控制面（拓扑、目标配置、拓扑锁、proxy 在线记录）；业务键继续由 codis-server 保存，Redis 客户端仍连接 proxy |

旧线 SHA 永久列在此处，后续发布说明不再重复其正文。

## 2. 新线内容

- **etcd 连接**：dashboard / fe / admin / proxy 共用 `--etcd=URL[,URL...]` 与 `--etcd-config=FILE`（顶层 `endpoints`、`dial_timeout`、`request_timeout`、`username`、`password`、`ca_file`、`cert_file`、`key_file`，相对路径按文件目录解析）。地址优先级为显式 `--etcd` → 文件 `endpoints` → Dashboard 的 `coordinator_addr` → `http://127.0.0.1:2379`。
- **连接预检**：跨 endpoint 统一截止时间、任一可用即通过；TLS 握手/证书/鉴权失败直接返回原因（脱敏）；启动依赖 etcd 的入口用首次实际操作验证角色权限。
- **持久拓扑锁**：`/codis3/{product}/topom` 不附 lease，值含每进程随机 `owner_id`；启动争锁冲突退避重试、拒绝立即退出、结果未知回读。
- **锁的运维入口**：Admin `--show-lock`、`--remove-lock --expected-revision=N --confirm`；Dashboard 不再提供 `--remove-lock`。
- **Proxy 在线注册**：TOML 新增 `etcd_register_online`（默认 false），键 `/codis3/{product}/online-proxies/{token}`，独立 lease TTL 固定 30 秒（服务端授予值实测 `GrantedTTL=30`），后台惰性建连；`cluster_nodes_compat` 的 `all` 改为读在线列表，不再读旧注册协议。开启 `etcd_register_online` 时记录地址取 Dashboard 纳管后 model 的代理地址（`--host-proxy` 可覆盖），无需额外参数。
- **FE 动态发现**：`--listen` 必填，静态 `--dashboard-list=FILE` 与动态 `--etcd`/`--etcd-config` 恰居其一；动态来源扫描 `/codis3` 读各 product 的 `topom`。
- **默认配置**：`config/dashboard.toml`、`config/proxy.toml` 由 `--default-config` 再生（`./bin/codis-dashboard --default-config`、`./bin/codis-proxy --default-config`）。

## 3. 不兼容变更

| 变更 | 影响 | 行为 |
| --- | --- | --- |
| 旧 coordinator CLI 参数被拒 | `--zookeeper`、`--zookeeper-auth`、`--filesystem`、`--consul`、`--consul-auth` | 建立连接前报错，文案给出选项名与旧线 SHA，**不回显参数值** |
| 旧 TOML 字段被拒 | `coordinator_name` 取 `zookeeper`/`filesystem`/`consul`、`coordinator_auth`（即使为空）、proxy 的 `jodis_*` | 启动即失败并给出原因与旧线 SHA；TOML 里字段存在（即使值为空）就能被检出 |
| Jodis 注册机制移除 | 客户端依赖 `/jodis/{product}/proxy-{token}` 发现 proxy | 新线不再写入该路径、不接受 Jodis 配置；改由 etcd 在线注册 + `cluster_nodes_compat = "all"` |
| Proxy 模型字段移除 | `models.Proxy` 的 `jodis_path` | 存储模型与对外 JSON 中不再出现该字段 |
| Dashboard `--remove-lock` 移除 | 运维脚本直接调用该参数 | 改用 Admin `--show-lock` / `--remove-lock --expected-revision=N --confirm` |
| `config-dump -1` 移除 | 读取 Codis 1.x/ZooKeeper 配置树的导出命令 | `--config-dump` 只输出 Codis 3.x 格式；`--config-convert` 仍为纯文件转换 |
| `--fillslots`/`--dashboard`/`--etcd` 语义收窄 | proxy 自动上线入口 | 三者至多一个；`--etcd-config` 只提供连接选项，可与 `--dashboard` 同用 |
| `coordinator_name` 只接受 `etcd` | Dashboard 配置 | 默认值已改为 `etcd`，`coordinator_addr` 只作为地址回退 |

## 4. 升级须知

- **存量元数据迁移不在本次范围**：新线不提供 ZooKeeper / Consul / filesystem → etcd 的元数据迁移，也不提供双写、原地替换或跨版本回退。已有集群请继续使用旧线 `c635ade0424a1448fb251f32b88d64ca7cf9a555` 构建的版本；要转到新线，请在 etcd 的空 product 命名空间中按新教程建群，再用 [数据迁移控制台](data_migration_zh.md) 搬运业务数据并切流。
- **首次建群预检**：对带尾斜杠的 product 前缀做一次线性一致前缀查询（部署脚本可用 `etcdctl get --prefix --keys-only /codis3/{product}/`），空结果才继续。
- **异常退出的锁需要人工恢复**：先隔离原进程与自动拉起机制，再 `--show-lock`，再按观察到的 `create_revision` 执行 `--remove-lock`。三种写结果（成功 / 明确拒绝 / 结果未知）的判定与迟到请求保护见[契约](etcd_3_7_coordinator_contract_zh.md)第 2 节。
- **监控与编排脚本**：任何固定写入 `coordinator_name = "zookeeper"/"filesystem"/"consul"`、`--filesystem`、`jodis_*` 的模板都必须先替换为新连接文件，否则会在启动阶段被拒绝。
- **etcd 运维**：三成员及以上部署、鉴权与 TLS 轮换、备份恢复按 etcd 自身规范执行；Codis 只依赖 KV、事务、lease 与前缀读，不使用 etcd 的 service registry 语义。

## 5. 适配状态

| 入口 | 状态 | 说明 |
| --- | --- | --- |
| `deploy/` | **已适配** | 模板与渲染样例改为 etcd 连接参数（无 `coordinator_auth`、无 `jodis_*`）；首次建群预检脚本化 |
| `ansible/` | **已适配** | 角色变量与模板改为 etcd 连接参数 |
| `example/` | **已适配** | 示例配置与启动命令按新线再生 |
| `deploy/Makefile` | **未动** | 按仓库约定本次未改 |
| `scripts/validation/env/*`、`scripts/redis8_local_validation.py` | **未适配（后续验证工作）** | 仍生成旧线 coordinator 配置，不作为新线建群证据；新线本地端到端证据见[实施交付记录](etcd_3_7_implementation_record_zh.md) §8.4 |

## 6. 验证状态

| 验收项 | 状态 | 证据 |
| --- | --- | --- |
| A0 依赖调研 | 通过 | [实施交付记录](etcd_3_7_implementation_record_zh.md) §3 |
| A1 配置与入口 | 通过（本地端到端） | 同上 §8.2、§8.4 |
| A2 建群与隔离 | 通过（本地端到端） | 同上 §8.2、§8.4 |
| A3 锁与迟到请求 | 通过（本地进程级） | 同上 §8.2、§8.4 |
| A4 结果未知 | 通过（测试替身） | 同上 §8.2 |
| A5 混合 Proxy | 通过（本地单成员，生产 TTL 授予 30s） | 同上 §8.2、§8.4 |
| A6 Linux 发布门槛 | **通过（三成员 Linux 真机，2026-11-28 签收）** | 华为云 3 台 ECS（`c7.xlarge.2` 4C8G，Ubuntu 22.04.5）+ 三成员 etcd 3.7.4（HTTP + `--auth-token simple` + RBAC），Codis 产物为 host1 Linux 原生 `make`（go1.27.4 linux/amd64，proxy `-tags=cgo_jemalloc`）；四场景通过：leader 切换（2ms leadership transfer / 端到端 9.18s）、隔离单成员（仅 peer 2380 隔离 108s，负载 0 失败、在线记录保留）、失去/恢复多数派（错误返回 5011ms/848ms、`--create-group` 5013ms 加载 slots 失败、恢复 19.2s，锁与在线记录不变）、正常退出（记录 ~0.2s 消失、锁 76ms 释放）；S-A/B/C 故障窗口负载 0 失败，S-D 的 40 条/台 `Connection refused` 为主动停机后的负载计数。启动期非零退出与全隔离变体按契约 v1.3 属预期。明细见[A6 三成员真机验证](validation/etcd_3_7_coordinator_a6_zh.md) |
| A7 构建与清理 | 通过（本地） | 干净副本 build/vet/test/默认配置一致；帮助与部署样例无旧后端选项，见记录 §8.1 第 5 步 |

A6 已于 **2026-11-28 在 Linux 三成员真机上通过并签收**（华为云 3 台 ECS + 三成员 etcd 3.7.4，四场景 leader 切换 / 隔离单成员 / 失去与恢复多数派 / 正常退出），环境、步骤与逐条证据登记在[etcd 3.7 coordinator · A6 三成员 Linux 真机验证](validation/etcd_3_7_coordinator_a6_zh.md)，索引行见[真机验证索引](validation/README.md)（ENV-02）。本版本**仍没有新的性能数字**；A6 遗留项已按裁决闭环：启动期建连/鉴权失败（抢锁事务发出前）的非零退出由进程管理器退避重启、全隔离变体下 lease 按 TTL 过期与自动重注册，均属契约 v1.3 预期（细节见记录 §7 R5/R6、§10）。其余待验项：Linux 平台 x509 文案、`linux/arm64` 与交叉工具链构建、证书轮换/CRL、auth Token 刷新。全部实施细节、决策 D1–D10 与逐项证据见[etcd 3.7 coordinator 实施交付记录](etcd_3_7_implementation_record_zh.md)（交付说明见其 §11）。

## 7. 相关文档

- [tutorial_zh.md](tutorial_zh.md)：etcd 前置准备、连接文件、四条启动命令、两 group 建群、锁与人工恢复、Proxy 在线注册。
- [architecture_zh.md](architecture_zh.md)：组件交互、coordinator 命名空间、代码地图与已知约束。
- [capability_boundaries_zh.md](capability_boundaries_zh.md)：coordinator 能力域与各项边界。
- [etcd_3_7_coordinator_contract_zh.md](etcd_3_7_coordinator_contract_zh.md)：对外行为契约 v1.3 与验收表 A0–A7。
- [etcd_3_7_implementation_record_zh.md](etcd_3_7_implementation_record_zh.md)：实施交付记录、P0 实测证据与验收状态。
- [FAQ_zh.md](FAQ_zh.md)：常见问题（外部存储、锁冲突、人工解锁、etcd 不可用）。
