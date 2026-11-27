# 真机验证进度索引

本目录记录 Codis 各能力在**真机**上的验证进度。本文只是索引：状态在这里汇总，每个能力的环境、步骤、结果与证据单独开文档放在本目录，不写进这里。

## 口径

- **真机**指 Linux 真实部署环境（云 ECS 或等价的物理机/虚拟机），不是 macOS 本机。macOS 上的单测、`make gotest`、`scripts/redis8_local_validation.py` e2e、Playwright headless 都只算本地回归，不计入真机验证。
- 真机结论只对**执行时的 HEAD** 负责。之后新增的提交不继承旧结论：改动到已通过能力的代码时，状态回到"待确认"，或在备注里注明"基于 HEAD xxx 验证"。
- 真机报告、日志和截图保存在本地 `.runtime/`（`.gitignore` 忽略，不入库）；入库的只有本目录的文档。引用 `.runtime/` 路径时注明是本地证据。脚本默认把工作目录与 evidence JSON 放在 `.runtime/realmachine/` 下：集群工作目录是 `<run-id>/`，evidence 是 `<脚本名>-<run-id>.json`（gate 类为 `redis8-gate-` / `security-audit-`）。
- 环境销毁后不可复现，所以每次真机验证都必须在详细文档里写清环境字段（参见 `../redis8_validation_zh.md` 的"必须记录的环境字段"）。

## 状态定义

| 状态 | 含义 |
| --- | --- |
| 未验证 | 明确没有在真机上跑过，或只有本地回归 |
| 部分通过 | 真机上跑过主要路径，但有未闭环项、跳过项或未覆盖的场景 |
| 已通过 | 真机上验证通过，遗留项已闭环或有明确记录的接受理由 |
| 待确认 | 本索引没有记录，需负责人确认后填写 |

## 真机环境记录

| 编号 | 日期 | 环境 | 执行时 HEAD | 状态 | 说明 |
| --- | --- | --- | --- | --- | --- |
| ENV-01 | 2026-11-21 | 华为云 cn-east-3：ECS-A（4C/8G）/ ECS-B（2C/4G），Ubuntu 22.04 x86_64，Go 1.27.4，Redis 8.6.3 | `c99592b1` | 已销毁 | filesystem coordinator。A：dashboard 18080、fe 8080、group1 master 6379、group2 replica 6382、group3 6383；B：proxy 19000/19001、group2 master 6380、group1 replica 6381。未跑性能、codis-ha。详见本地 `.runtime/state.md` 与各 `REPORT.md` |
| ENV-02 | 2026-11-28 | 华为云 cn-east-3a：3 台 ECS `c7.xlarge.2`（4C/8G），Ubuntu 22.04.5 x86_64（kernel 5.15.0-186），Go 1.27.4（host1 Linux 原生 `make`），etcd 3.7.4 官方 release 包三成员（HTTP + `--auth-token simple` + RBAC），Redis 8.6.3 | `3a5890d4` + 未提交工作树 | 保留（未销毁；Codis 组件已停，etcd 三成员运行） | etcd coordinator 新线。host1：dashboard 18080；host2/host3：codis-server 6379（slots 0–511 / 512–1023）+ codis-proxy 19000/11080（`etcd_register_online=true`、`cluster_nodes_compat=all`）。A6 四场景结果见 [etcd_3_7_coordinator_a6_zh.md](etcd_3_7_coordinator_a6_zh.md)；证据在本地 `.runtime/codis-a6/a6/`（`report.md` + `ev/`） |

## 进度总表

按能力域排列。"详细文档"为空表示还没开，后续按 [详细文档约定](#详细文档约定) 补齐。

### 负责人明确未验证

| 能力 | 范围 | 状态 | 已有覆盖 | 未覆盖 | 详细文档 |
| --- | --- | --- | --- | --- | --- |
| Redis 8 现代化（Go 现代化改造） | `../go_modernization_zh.md` 所述改造，提交 `1b0bd208`…`2a173931`（2026-11-17 ~ 11-22） | **未验证** | macOS 本机回归全过（该文 §5）。ENV-01 的构建、单测、e2e 冒烟覆盖到 `c99592b1`，含 11-17 批次，但未针对性验证 | `aadcf284`…`2a173931`（slog、flag、ServeMux、proxy 运行时、etcd 移除）未上过真机；Linux 发布验证；新 CLI/HTTP 入口在真机的启动与契约；coordinator 三选一；真实流量下的 proxy 生命周期与 RESP decoder 改动 | 待开：`go_modernization_zh.md` |
| 数据迁移控制台 | `2298133d`…`c739fb38`（2026-11-23）：`pkg/shake`、`pkg/migration`、topom 任务管理与 HTTP API、FE 数据迁移页 | **未验证** | macOS 真实 redis-server 8.x 本地 e2e（`../data_migration_zh.md` §端到端验证） | 真机上 standalone / cluster / codis 三种源目标 × sync / scan / rdb；dashboard 拉起 `codis-shake` 子进程、状态端口轮询、孤儿保护、停止语义；FE 数据迁移页 | 待开：`data_migration_zh.md` |

### ENV-01 历史结果（`c99592b1`）

表中状态来自 ENV-01。后续 Go 现代化提交改动了 CLI、管理 HTTP 和 proxy 运行时，当前主线的这些入口仍待 Linux 复验。

| 能力 | 状态 | 结果摘要 | 遗留 / 未覆盖 | 详细文档 |
| --- | --- | --- | --- | --- |
| Redis 8 内核接入（codis 模式、slot、同步/异步迁移、slotsrestore） | 已通过（功能） | `make gotest`、4 组 Tcl、e2e semi-async / sync、跨版本 Redis 3→8 成功 / 8→3 可观测失败（源 key 保留）、fork/RDB、复制断连恢复均 PASS；干净 Linux 树 `make build-all`（cgo）PASS，cgo proxy 实际运行 | 性能基线 SKIPPED；跨版本只有单向成功 | gate 见 `../redis8_validation_zh.md`；真机记录待开 |
| Redis 8 RDB HTTP 导出 + 限速 | 已通过 | Tcl `codis_rdb_export` 13 项 PASS；dashboard remote fetch PASS | 当前 dashboard HTTP 拉取入口待复验 | — |
| RDB Analysis（本地 / 远程拉取 / upload / codis-admin） | 已通过 | fork `third_party/rdb` 修复 RDB v13 解析后 PASS；stream / IDMP 元素计数正确 | 当前 CLI、管理 HTTP 和任务生命周期待复验 | — |
| Codis ACL | 已通过 | API + 两 proxy 实测：多账号、db 绑定、管理命令拒绝、revision 切换、`--acl-sync`；Playwright 新增/删除用户 | 浏览器只覆盖部分交互；当前 CLI、管理 HTTP 和 proxy 认证路径待复验 | — |
| Proxy QPS 限流 | 已通过 | 两 proxy 各 5/20 accepted、reinit 重放、负值拒绝；Playwright 0→3→0 | 未测被拒请求的后端 commandstats delta；当前管理 HTTP 与 proxy 运行时路径待复验 | — |
| PITR 数据闪回 | 部分通过 | happy path、并发锁、cancel-before、systemd `Restart=on-failure` 均 PASS；Playwright 成功 / 非法 ts / 副本失败 / 详情 / 删除 | `stepResync` 尚需核对复制结果；truncate 中 cancel 的终态为 `failed`（S17 未闭环）；当前 CLI 和管理 HTTP 待复验 | — |
| Redis 3 移除 + 依赖安全扫描回归 | 已通过 | govulncheck 无漏洞；go build / test PASS；npm audit 2→1 | AngularJS 1.8.3 high 为已知残留 | — |
| FE 控制台重设计（浏览器冒烟） | 已通过 | Playwright headless：总览 / 安全与限流 / RDB / PITR 四页加载无 console error；HTML 错误页渲染问题已修复 | 当前 FE 反向代理待复验；数据迁移页为后加，未覆盖 | — |

### A6 真机结果（2026-11-28，ENV-02）

| 能力 | 范围 | 状态 | 结果摘要 | 遗留 / 未覆盖 | 详细文档 |
| --- | --- | --- | --- | --- | --- |
| etcd 3.7 coordinator（新线单后端） | A6：三成员 etcd 3.7.4 Linux 真机（leader 切换、隔离一成员的 peer 流量、失去/恢复多数派、正常退出） | **已通过（2026-11-28 签收）** | S-A leader 切换：新 leader 2ms（leadership transfer）、端到端 9.18s，锁/在线记录不变；S-B 隔离单成员（仅 peer 2380 隔离 108s）：2/3 保持 leader、负载 0 失败、在线记录 TTL 27/28s 不丢、0.1s 归队、约 1.6min 追平；S-C 失去/恢复多数派：`--show-lock` 5011ms/848ms 报错不挂死、`--create-group` 5013ms `store: load slots failed`（加载 slots 失败、组未创建，符合预期）、恢复 19.2s、锁 owner/create_revision 不变、在线记录误删 0；S-D 正常退出：在线记录 ~0.2s 消失、锁 76ms 释放。S-A/B/C 故障窗口负载 0 失败，S-D 的 40 条/台 `Connection refused` 为主动停机后的负载计数 | 无阻塞项：启动期建连/鉴权失败（抢锁事务发出前）的非零退出由进程管理器退避重启、全隔离变体下 lease 按 TTL 过期并自动重注册，均已按契约 v1.3 裁决为预期；无仓库内真机脚本 | [etcd_3_7_coordinator_a6_zh.md](etcd_3_7_coordinator_a6_zh.md) |

### 无真机记录

| 能力 | 状态 | 备注 | 详细文档 |
| --- | --- | --- | --- |
| Consul coordinator（旧线 ≤ `c635ade0…`） | 待确认 | ENV-01 用的是 filesystem；仓库内无真机记录；该后端已在新线移除 | — |
| ZooKeeper coordinator（旧线 ≤ `c635ade0…`，go-zookeeper SDK 迁移） | 待确认 | 同上 | — |
| 热 key 缓存 + 跨 proxy 广播失效 | 待确认 | | — |
| CLUSTER NODES 兼容 | 待确认 | | — |
| Stream 命令路由 | 待确认 | | — |
| AUTH 防暴力破解 | 待确认 | | — |
| CLIENT LIST | 待确认 | | — |
| 多平台发布构建 | 待确认 | linux/amd64 `build-all` PASS；linux/arm64 与发布目录结构未验证 | — |
| codis-ha | 未验证 | ENV-01 未启动 | — |
| 性能基线 | 未验证 | ENV-01 明确 SKIPPED | — |

## 脚本索引

各能力的真机验证脚本已入库到 `scripts/validation/`，脚本总索引（目录说明、快速开始、能力↔文档↔脚本总表、证据约定、实测与文档差异、限制）见 [scripts/validation/README.md](../../scripts/validation/README.md)。浏览器用例路径相对 `scripts/validation/`。

| 能力 | 脚本 | 浏览器用例 | 对应文档 |
| --- | --- | --- | --- |
| 环境搭建 · 采集 · 清理 | `scripts/validation/env/collect_env.py`、`scripts/validation/env/setup_cluster.py`、`scripts/validation/env/teardown_cluster.py` | — | [redis8_validation_zh.md](../redis8_validation_zh.md)（必须记录的环境字段）、本文件（ENV 登记） |
| Redis 8 功能 gate | `scripts/validation/redis8/run_functional_gate.py` | — | [redis8_validation_zh.md](../redis8_validation_zh.md) |
| 依赖安全扫描 | `scripts/validation/security/run_dependency_audit.py` | — | [redis8_validation_zh.md](../redis8_validation_zh.md)、[build_and_release_zh.md](../build_and_release_zh.md) |
| Codis ACL | `scripts/validation/acl/run_acl.py` | `browser/tests/acl.spec.js` | [codis_acl_zh.md](../codis_acl_zh.md) |
| Proxy QPS 限流 | `scripts/validation/qps/run_qps_limit.py` | `browser/tests/qps-limit.spec.js` | [proxy_qps_rate_limit_zh.md](../proxy_qps_rate_limit_zh.md) |
| PITR | `scripts/validation/pitr/run_pitr.py` | `browser/tests/pitr.spec.js` | [pitr_flashback_zh.md](../pitr_flashback_zh.md) |
| RDB Analysis | `scripts/validation/rdb/run_rdb_analysis.py` | `browser/tests/rdb-analysis.spec.js` | [rdb_analysis_zh.md](../rdb_analysis_zh.md) |
| FE 浏览器冒烟 | —（环境由 `scripts/validation/env/setup_cluster.py --fe` 提供） | `browser/tests/fe-smoke.spec.js` | [../../cmd/fe/assets/README.md](../../cmd/fe/assets/README.md) |
| 尚无脚本：性能基线（暂不固化）、数据迁移控制台、codis-ha、旧线（≤ `c635ade0…`）Consul/ZooKeeper coordinator、多平台发布；etcd 3.7 coordinator 目前只有 Go 集成测试入口（`CODIS_ETCD_SERVER_BINARY`），没有真机脚本 | — | — | [data_migration_zh.md](../data_migration_zh.md)、[benchmark.md](../benchmark.md)、[architecture_zh.md](../architecture_zh.md) |

## 详细文档约定

- 放在本目录，命名 `<topic>_zh.md`，例如 `data_migration_zh.md`、`go_modernization_zh.md`；一个能力一份，多次验证在同一份里按日期追加章节。
- 至少包含：环境字段（机器规格、OS、Go / Redis 版本、coordinator 类型、部署布局）、执行时 HEAD、场景与命令、结果、证据路径、遗留项。
- 必须列出本次使用的脚本路径（相对仓库根，例如 `scripts/validation/acl/run_acl.py`）与 evidence JSON 路径；脚本实测行为与文档描述不一致时，在详细文档写明实测值。
- 结论要区分"通过 / 可观测失败 / 未执行"；未执行就是未验证，不能用本地结果补位。
- 开好后在上面总表对应行填上链接。

## 维护流程

1. 用 `python3 scripts/validation/env/collect_env.py` 采集环境字段、用 `python3 scripts/validation/env/setup_cluster.py` 拉起验证集群；登记 ENV-xx 时引用 `env-<run-id>.json`（必要时同时引用 `cluster.json`）。
2. 逐能力运行 `scripts/validation/<能力>/run_*.py`（需要 FE 交互时另跑 `scripts/validation/browser/` 用例），把 evidence 路径与 case 状态写进详细文档，并更新总表的状态、结果摘要和遗留项。
3. 用 `python3 scripts/validation/env/teardown_cluster.py --cluster <cluster.json>` 产出 `cleanup.json` 后更新 ENV 状态，并确认 `.runtime/` 里保留的报告和日志可被详细文档引用。
