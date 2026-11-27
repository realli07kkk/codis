# etcd 3.7 coordinator · A6 三成员 Linux 真机验证（2026-11-28）

本文记录[etcd 3.7 coordinator 契约](../etcd_3_7_coordinator_contract_zh.md)第 7 节 A6 的 Linux 真机门槛：三成员 etcd 3.7.4 集群上的 leader 切换、隔离一个成员的 peer 流量、失去/恢复多数派、正常退出四个场景。执行方式为手工运维命令（仓库内暂无 etcd 真机脚本），证据是本地快照文件而非 evidence JSON。

**结论先行：A6 通过（2026-11-28 签收）。** 四场景的 leader 切换、peer 层隔离单成员、失去/恢复多数派、正常退出均达到契约预期：隔离与 quorum 抖动窗口内 proxy 读写 0 失败、拓扑锁 owner/create_revision 不变、在线记录 0 误删、查询与管理命令在 0.85–5.0s 内返回错误而不挂死、没有 proxy 被误判下线。

## 1. 环境与版本

| 项 | 值 |
| --- | --- |
| 云环境 | 华为云 ECS 3 台，可用区 cn-east-3a，规格 `c7.xlarge.2`（4 vCPU / 8 GiB） |
| OS / 架构 | Ubuntu 22.04.5 LTS，x86_64，内核 `5.15.0-186` |
| etcd | **v3.7.4 官方 release 包**：本机下载后校验 SHA256 再上传到三台主机（`etcd Version 3.7.4 GitSHA 2e8a5c9`，`etcdctl version: 3.7.4`，存储版本 3.7.0） |
| etcd 传输与鉴权 | HTTP 客户端端口（无 TLS），`--auth-token simple`，开启 RBAC：`root`（管理员）+ `dashboard` / `proxy` / `admin` 三个业务用户，角色 `codis-rw` 对 `/codis3/` 前缀 `readwrite` |
| etcd 启停 | `/usr/local/etcd/etcd-start.sh` / `etcd-stop.sh`，pid 文件 `/var/run/etcd-a6.pid` |
| Codis 构建 | host1（Linux 原生）`make`，`go1.27.4 linux/amd64`（`/topom` 回报 `2026-11-28 01:12:04 +0800 by go version go1.27.4 linux/amd64`）；`codis-proxy` 以 `-tags=cgo_jemalloc` 构建，`go version -m` dep **35** |
| codis-server | Redis **8.6.3**，`jemalloc-5.3.0` |
| 产物 sha256（前缀） | proxy `69d79553…`、codis-server `a80398c6…`；dashboard `6985b125…`、admin `53130fc4…`；三台主机同版本产物 sha256 一致 |
| 执行时 HEAD | `3a5890d42a6366bddc86e1ed218081323600ae90` + 未提交工作树改动（与[实施交付记录](../etcd_3_7_implementation_record_zh.md) §11.1 的交付口径一致） |
| 执行时间 | 2026-11-28 01:35–02:10 CST |

> 本文不含任何口令、私钥、EIP 或云实例 ID。etcd 口令只在远端以 `$(cat /root/codis-a6/secrets/root.pw)` 形式被 shell 展开，未进入命令行记录或日志；EIP 属临时资源，已省略。

## 2. 拓扑

| 主机 | 内网 IP | etcd 成员 | 承载组件 |
| --- | --- | --- | --- |
| host1 | 192.168.0.2 | a6-1（`4b938b23a924378d`） | **codis-dashboard** `192.168.0.2:18080`（product `codis-a6`）；S-C 另起第二个 dashboard `:18081`（临时，未上线） |
| host2 | 192.168.0.179 | a6-2（`86c39da66ea766bc`） | **codis-server** `:6379`（group1，slots 0–511）+ **codis-proxy** `:19000`/`:11080` |
| host3 | 192.168.0.221 | a6-3（`c4b4c908cabfee87`） | **codis-server** `:6379`（group2，slots 512–1023）+ **codis-proxy** `:19000`/`:11080` |

- etcd 客户端端点：`http://192.168.0.2:2379`、`http://192.168.0.179:2379`、`http://192.168.0.221:2379`；peer 端口 2380。
- 两台 proxy 均 `etcd_register_online=true`、`cluster_nodes_compat="all"`，启动参数同时带 `--dashboard`（纳管）与 `--etcd-config`（连接文件）。两台互相可见：各自 `CLUSTER NODES` 看到 2 个节点，slot 区间互补（0–8191 / 8192–16383），`myself` 只出现在本机那条。
- 在线记录前缀 `/codis3/codis-a6/online-proxies/`：基线 `count=2`，两条记录均带 `token`/`addr` 与 `lease`，`lease timetolive` 返回值落在 1–30s（TTL 30s）。
- 负载：host1 上两条循环（`a6-load.sh proxy1|proxy2`），每 ~200ms 一次 `SET` + `GET`，逐行记 `seq / 累计 ok / 累计 fail`，失败行带错误文本前 80 字符。起 01:41:58.48、止 02:07:23.34，每条 7391 次迭代；日志 `load-proxy1.log` / `load-proxy2.log`。

## 3. 场景与观察

采样方式：每个阶段前后用 `a6-phase.sh <phase>` 落盘 `etcd-status` / `member-list` / `show-lock` / `cluster-nodes` / `online-records` / `lease-ttl` / `load-summary` 快照，整体拉回本地证据目录（下称 `ev/`）。

> 采样注意：etcdctl 3.7 的 `endpoint status --cluster` 会按成员表展开**所有**成员，任一成员客户端端口不可达即整体失败（`context deadline exceeded`）。隔离窗口内改为只对存活端点执行 `endpoint status`，`--cluster` 的失败本身也是证据。

### 3.1 S-A：leader 切换（通过）

| 操作 | 观察 | 耗时 | 结论 | 证据文件名 |
| --- | --- | --- | --- | --- |
| 初始 leader = host3（`c4b4c908cabfee87`）term 4；在 host3 执行 `etcd-stop.sh`（SIGTERM） | host1 raft 收到 `MsgTimeoutNow from c4b4c908cabfee87 and starts an election to get leadership`（etcd 停机前主动 leadership transfer），随即 `became leader at term 5`（host2 同时 `became follower at term 5`） | T0+7.0s 触发；**新 leader 产生 2ms**（相对 transfer 请求）；`etcd-stop.sh` 返回 9.04s（优雅停机）；轮询首次观测到新 leader T0+**9.18s** | 通过：切换期间 proxy 读写 **0 失败**，端到端 9.18s 主要来自 etcd 优雅停机（flush + 关服） | `timeline-sa-leader-switch.txt`、`checkpoint-sa-leader-down.txt`、`log-sa.txt` |
| 窗口内采样（01:47:56–01:48:12） | `--show-lock` 仍是 `owner_id b76ec111…`、`create_revision 4`（不变）；`member list` 仍列 3 成员；两台 `CLUSTER NODES` 仍互见 2 节点；在线记录 `count=2` 且 lease 未变；负载 1864 次 0 失败 | — | 通过：锁与在线状态不因 leader 切换变化 | `show-lock-sa-after.txt`、`member-list-sa-after.txt`、`cluster-nodes-sa-after.txt`、`online-records-sa-after.json`、`load-summary-proxy1-sa-after.txt`、`load-summary-proxy2-sa-after.txt` |
| `etcd-start.sh` 拉起 host3 | `became follower at term 5` → `elected leader 4b938b23a924378d at term 5` → `ready to serve client requests`，`initialized peer connections ... active-remote-members: 2`；01:50:42 `member list` **3/3 started**，三台 raft index 2285/2286/2287（差 ≤1） | 归队后 3/3 追平 | 通过：成员重启后自动归队并追平 | `etcd-status-sa-after.txt`、`log-sa.txt` |

### 3.2 S-B：隔离一个成员（通过，按两种隔离口径分别验证）

隔离对象 host3（当时非 leader，leader = host1）。契约 v1.3 已把主口径明确为 **peer 流量隔离**（如 2380）；此前字面口径"2380 + 2379 双向共 4 条 DROP"的全隔离会同时切断 host3 上 proxy 到**所有** etcd 端点的通路，故保留全隔离变体作为补充观察，其行为符合 TTL 设计（见 §5 偏差 ①）。

#### 变体 B2：仅 peer 端口隔离（2380 两条 DROP）——契约主口径

| 操作 | 观察 | 耗时 | 结论 | 证据文件名 |
| --- | --- | --- | --- | --- |
| host3 `INPUT/OUTPUT dport 2380 → DROP`（01:53:50.46） | host3 etcd 持续 `starting a new election` / `became pre-candidate`（无 quorum 无法当选）；`prober detected unhealthy status ... dial tcp 192.168.0.2:2380: i/o timeout`（对 host2 同） | 隔离窗口约 108s（01:53:50→01:55:38） | 通过：被隔离成员无法参与 raft，但集群继续服务 | `timeline-sb2-isolate-peer.txt`、`log-sb.txt`（host3 raft 选举尝试 + prober unhealthy）、`log-sb1-etcd-host3.txt`（prober 探测失败样本） |
| 隔离中采样（01:55:21） | **2/3 保持 leader**（host1）；在线记录 **count=2**、lease `remaining 27s / 28s`（**未丢**）；两台 `CLUSTER NODES` 互见；负载 3891 次 **0 失败** | — | 通过：契约"在线状态不丢"在此口径成立 | `etcd-status-sb2-peer-isolated.txt`、`online-records-sb2-peer-isolated.json`、`lease-ttl-sb2-peer-isolated.txt`、`cluster-nodes-sb2-peer-isolated.txt`、`load-summary-proxy1-sb2-peer-isolated.txt`、`load-summary-proxy2-sb2-peer-isolated.txt` |
| proxy2 客户端痕迹（01:54:26） | `retrying of unary invoker failed ... peer 192.168.0.221:2379 ... RST_STREAM: CANCEL`，随后自动切换其余端点（`refresh cluster nodes from online registry failed` 仅 1 次） | — | 通过：SDK 在端点间切换，不影响读写 | `log-sb2-proxy2.txt` |
| 删除 2 条规则（01:55:38.18） | 01:55:38.169 host3 `became follower at term 5` + `elected leader 4b938b23a924378d at term 5` | **0.1s 内归队**；完全追平约 **1.6 分钟**（期间 `ReadIndex response took too long`、`peer became inactive ... write i/o timeout` 反复） | 通过（追平时长见 §5 偏差 ⑤） | `timeline-sb2-restore-peer.txt`、`cluster-nodes-sb2-restored.txt`、`member-list-sb2-restored.txt`、`etcd-status-sb2-restored.txt` |

#### 变体 B1：契约字面口径（2380 + 2379 共 4 条 DROP）——补充观察

| 操作 | 观察 | 耗时 | 结论 | 证据文件名 |
| --- | --- | --- | --- | --- |
| host3 加 2380 + 2379 共 4 条 DROP（01:51:22.82） | proxy2 `keepalive stream of lease 7964512715818654784 closed; re-confirming ownership`，随后 `cannot read the ttl ... not registered yet; retrying in 2s/4s/8s/16s/30s`；01:52:31 在线记录 `count=1`（proxy2 记录被租约到期合规撤销，rev 1039） | 隔离约 68s 后记录过期 | **符合 TTL 设计**，契约 v1.3 已将其列为预期行为（见 §5 偏差 ①） | `timeline-sb1-isolate-full.txt`、`log-sb1-proxy2.txt`、`online-records-sb-isolated.json`、`lease-ttl-sb-isolated.txt`、`etcd-status-sb-isolated.txt` |
| 其余观察（01:52:31） | 2/3 仍选出/保持 leader；负载 3069 次 **0 失败**；两台 `CLUSTER NODES` 正常；dashboard 的 proxy 注册表（`--proxy-status`）**始终保留 proxy-1/proxy-2**，未删 token | — | 通过：**无误判下线**；被撤销的只是 proxy 自己的 online 记录 | `cluster-nodes-sb-isolated.txt`、`load-summary-proxy1-sb-isolated.txt`、`load-summary-proxy2-sb-isolated.txt`、`show-lock-sb-isolated.txt` |
| 删除 4 条规则（01:53:07.28） | 01:53:18 proxy2 `registered addr "192.168.0.221:19000" with lease 7402970137736842471`（**恢复后 11s 自动重注册**）；01:53:39 两台重新互见、负载 3400 次 0 失败 | 11s | 通过：恢复后自动重注册 | `timeline-sb1-restore-full.txt`、`log-sb1-proxy2.txt`、`online-records-sb-b1-restored.json`、`cluster-nodes-sb-b1-restored.txt` |

### 3.3 S-C：失去多数派后恢复（通过）

对 host2 与 host3 同时加 4 条 DROP，只剩 host1（1/3，无 quorum）。共两轮：第一轮恢复方式有缺陷（host2 删除命令下发失败），第二轮为同时恢复的干净复测。

| 操作 | 观察 | 耗时 | 结论 | 证据文件名 |
| --- | --- | --- | --- | --- |
| 第一轮隔离（01:58:28.86） | `--show-lock`（host1）：exit=1，`[PANIC] create etcd client failed` / `[error]: etcd client: initialize failed: context deadline exceeded` | **5011ms** | 通过：**返回错误而非挂死** | `timeline-sc1-isolate-host2.txt`、`timeline-sc1-isolate-host3.txt`、`timeline-sc1-showlock-lost-1.txt` |
| dashboard HTTP 探测（01:58:59） | `/topom` **200（3.1ms，内存态）**；`/api/topom` 404（见 §5 偏差 ③）；`--slots-status` 经 dashboard 15ms 成功、`--list-proxy` 18ms 成功 | 3–18ms | 通过：只读接口走内存缓存仍 200 | `timeline-sc1-dash-probe.txt`、`etcd-status-sc-before.txt` |
| 需要写 store 的管理命令（01:59:18.656） | `--create-group --gid=9`：**5013ms** 返回 `[Remote Error] store: load slots failed`（组未创建）；dashboard 日志每 5s `[ERROR] store: load slots failed` | 5013ms | 通过：加载 slots 失败、组未创建（符合预期：写 store 的操作在失去多数派时报错） | `timeline-sc1-dash-write-probe.txt`、`log-sc.txt` |
| 起第二个 dashboard（01:58:39→01:58:44，`:18081`） | 01:58:44 打出 `[PANIC] create etcd client failed` 后**进程退出**，始终没有 `dashboard is working ...`，**从未上线** | — | **符合契约 v1.3 §2**：建连/鉴权失败发生在抢锁事务发出前，非零退出由进程管理器重启（见 §5 偏差 ②） | `timeline-sc1-dashboard-log.txt`、`log-sc.txt` |
| proxy 观察（01:58:50 / 02:05:25） | 两台 proxy `keepalive stream of lease ... closed; re-confirming ownership` → `cannot read the ttl ... not registered yet; retrying in 2s/4s/8s/16s/30s`（1/2/4/8/16/30s 退避）；两台 `CLUSTER NODES` 仍各自保有 2 节点快照；负载到 5435 次 **0 失败**；在线记录仍是原两条（create_revision 1036/1040 未变） | — | 通过：proxy 不误删、快照保留 | `log-sc.txt`、`cluster-nodes-sc-before.txt`、`online-records-sc-before.json`、`load-summary-proxy1-sc-before.txt`、`load-summary-proxy2-sc-before.txt` |
| 第一轮恢复（02:01:12 先恢复 host3，02:02:39 补恢复 host2） | 02:02:04.517 `--show-lock` 恢复成功（`owner_id b76ec111…`、`create_revision 4` 不变），距 host3 恢复 **52.7s**（含成员归队/追平）；02:03:46 proxy1 重新注册（create_revision 1042，属租约更早到期后的合规重建） | 52.7s | 通过 | `timeline-sc1-recover.txt`、`timeline-sc1-restore-host3.txt`、`timeline-sc1-restore-host2.txt`、`show-lock-sc-restored.txt` |
| 第二轮隔离（02:05:02 同时加 4 条） | `--show-lock` 848ms exit=1，`rpc error: code = Unavailable desc = etcdserver: leader changed`（更快失败，同样不挂死）；隔离窗口约 36s 内负载 0 失败，`CLUSTER NODES` 保持 2 节点快照 | 848ms | 通过：错误分类可区分（leader changed） | `timeline-sc2-isolate-host2.txt`、`timeline-sc2-isolate-host3.txt`、`timeline-sc2-showlock-lost.txt` |
| 第二轮同时恢复（02:05:38.75 / .39.03） | 02:05:57.880 `--show-lock` 恢复成功，距恢复 **19.2s**；`owner_id`/`create_revision` 不变；proxy 以 `re-confirming ownership` **静默续上（无重新 registered 日志，沿用原 lease）**；两台 `CLUSTER NODES` 重新互见，etcd 3/3 term 8；02:06:24 在线记录 `count=2`，`create_revision` 仍为 **1040 / 1042**、lease 与窗口前完全一致；负载 7109 次 0 失败 | 19.2s | 通过：**在线记录 0 误删**、锁不丢 | `timeline-sc2-recover.txt`、`timeline-sc2-restore-host2.txt`、`timeline-sc2-restore-host3.txt`、`show-lock-sc-restored.txt`、`online-records-sc-restored.json`、`lease-ttl-sc-restored.txt`、`cluster-nodes-sc-restored.txt`、`etcd-status-sc-restored.txt`、`load-summary-proxy1-sc-restored.txt`、`load-summary-proxy2-sc-restored.txt` |

### 3.4 S-D：正常退出（通过）

| 操作 | 观察 | 耗时 | 结论 | 证据文件名 |
| --- | --- | --- | --- | --- |
| 02:07:14.94/96 对两台 proxy 发 SIGINT（`stop-proxy.sh`） | 02:07:15.148 online-watch 观测 `count=2 → 0`；proxy 日志 `revoked lease 4003033894011694342 on shutdown` / `revoked lease 7402970137736842471 on shutdown`，随后 `proxy is exiting` | 在线记录 **~0.2s** 消失（采样间隔 150ms） | 通过：关闭顺序=停循环→有界 Revoke→Close | `timeline-sd-online-watch.txt`、`timeline-sd-stop-proxy1.txt`、`timeline-sd-stop-proxy2.txt`、`log-sd-proxy1-shutdown.txt`、`log-sd-proxy2-shutdown.txt`、`online-records-sd-after.json` |
| 02:07:52 对 dashboard 发 SIGINT | 02:07:52.9 `--show-lock` → `null` | **76ms** | 通过：锁已释放 | `timeline-sd-stop-dashboard.txt`、`show-lock-sd-after.txt`、`log-sd.txt` |
| 02:07:23.34 停负载循环 | `stop ok=7351 fail=40 total=7391`（两台同） | — | 40 次失败全部落在 S-D 停 proxy 后的 ~8s（§4） | `load-summary-proxy1-sd-after.txt`、`load-summary-proxy2-sd-after.txt` |

## 4. 负载统计

| 代理 | 迭代数 | 成功 | 失败（主动停机后的负载计数） | 失败时间窗与原因 |
| --- | --- | --- | --- | --- |
| proxy1（192.168.0.179:19000） | 7391 | 7351 | **40** | 02:07:15.114–02:07:23.3，全部 `SET=Could not connect to Redis at …:19000: Connection refused` |
| proxy2（192.168.0.221:19000） | 7391 | 7351 | **40** | 同上 |

- **S-A / S-B / S-C 故障窗口（01:41:58 → 02:07:14）：0 失败。** 各阶段 `load-summary-*` 均为 `FAIL lines in log so far: 0`；三个故障窗口内既无 error 也无超时计数。
- 上表 40 条/台 `Connection refused` 单列为「**主动停机后的负载计数**」：总计 40 次/台（合计 80 行失败）**全部发生在 S-D 主动停 proxy 之后**的 ~8s 内（负载循环比 proxy 晚 8s 被停掉），不属任何 etcd 故障窗口。这是预期内的，恰好证明负载探针能区分"连接中断"与"命令错误"；若按"先停负载再停组件"的顺序，总失败数可为 0（§5 偏差 ⑥）。

## 5. 偏差与注意项（本次未改动任何代码）

1. **主场景口径为 peer 隔离（已回写契约 v1.3）**。契约 A6 已把单成员隔离改为"仅隔离一个成员的 peer 流量（如 2380），保持多数派健康且 Proxy 仍可续约"，在线记录保留、Dashboard 持久纳管记录不变。此前的全隔离变体（2379 双向 DROP）会切断该主机 proxy 到所有 etcd 端点的通路，proxy 无法续约，租约到期后自身记录被服务端撤销（B1 实测 68s 后 `count=1`）、恢复后 11s 自动重注册——**符合 TTL 设计，属预期行为，不作为失败**；dashboard 注册表同样无误判（两台 proxy 始终在册）。
2. **无多数派时新起的 dashboard 非零退出（已裁决）**。`pkg/models/etcd/client.go` 的 `NewWithOptions` 在构造期鉴权时返回 `context deadline exceeded`，`cmd/dashboard/main.go:124` 打印 `[PANIC] create etcd client failed` 并退出进程（第二个 dashboard 的日志 01:58:44）。按 2026-11-28 裁决符合契约 v1.3 §2：**建连/鉴权失败发生在抢锁事务发出前**，Dashboard 报出脱敏原因并非零退出、由进程管理器退避重启，不在此阶段重试；抢锁事务发出后若结果未知，仍按 §2 用原 `owner_id` 线性一致回读确认持锁后才上线。已上线的 dashboard 不受影响（沿用既有 client，读接口继续 200）；本次不改代码。
3. **dashboard API 前缀是 `/topom` 而非 `/api/topom`**：`/api/topom` 返回 404，`/topom` 200。多数派丢失时 dashboard 的**读**接口仍从内存返回 200（3ms），只有落 store 的操作报错（`--show-lock` 由 CLI 直连 etcd、`--create-group` 经 dashboard 写 store）。契约"查询与管理命令返回错误"应按"是否落 store"区分。
4. **etcdctl 3.7 `endpoint status --cluster` 在任一成员不可达时整体失败**：它按成员表展开所有成员，任一客户端端口不可达即 `Error: failed to fetch endpoints from etcd cluster member list: context deadline exceeded`（S-A/S-B/S-C 窗口均复现）；`--endpoints` 含不可达端点且带 `--user` 时同样整体超时。巡检须改用**存活端点子集**。
5. **长时间隔离的成员归队后追平较慢**：归队是 0.1s 级（`became follower at term 5` → `elected leader …`），但 ReadIndex/apply 需 **1.6–4 分钟**才完全追上（期间 `dropped internal Raft message since sending buffer is full`、`write i/o timeout` 反复）。属真实网络/背压行为，不是 Codis 侧问题；若判据是"恢复即等于立即可读"，需留足超时。
6. **teardown 顺序：先停负载、再停组件**。本轮先 SIGINT proxy 后停负载，造成 40 次 `Connection refused` 计数（§4）。
7. **多主机 codis-server 需改 `bind` 并关 `protected-mode`**。官方 `config/redis.conf` 默认 `bind 127.0.0.1 -::1` + `protected-mode yes`，跨主机连接被拒（实测 `DENIED ... protected mode`）；多机部署须 `bind 127.0.0.1 <本机内网IP>` + `protected-mode no`，教程 §1.2 是单机示例，未覆盖该差异。

清理核实：三台仅剩 etcd 进程（`pgrep -fa 'codis-'` 唯一命中是 etcd 命令行里的集群令牌），iptables 三条主机 `INPUT/OUTPUT` 均无 DROP 规则，etcd 三成员保持运行（term 8，index 2486/2487/2488，leader = host3）；云资源未删除。

## 6. 与契约 A6 条目的逐条对照

| 维度 | 契约 A6 要求 | 实测 | 结论 |
| --- | --- | --- | --- |
| leader 切换 | 停掉 leader 后集群选出新 leader，进程不停摆 | leadership transfer 2ms 选出新 leader，端到端 9.18s（含优雅停机 9.04s），term 4→5；负载 0 失败；锁 owner/rev 不变；CLUSTER NODES 互见；在线记录 2 条不变；重启后 3/3 追平 | ✓ |
| 隔离单成员 | 仅隔离一个成员的 peer 流量（如 2380），保持多数派健康且 Proxy 仍可续约，2/3 仍选出并保持 leader | peer（2380）隔离 108s：2/3 保持 leader、负载 0 失败、在线记录 TTL 27/28s 不丢、`CLUSTER NODES` 正常、0.1s 归队、约 1.6min 追平 | ✓（主口径已回写契约 v1.3；全隔离变体行为见 §5 ①） |
| 失去/恢复多数派 | 失去 quorum 时集群不可写、恢复后自动续上 | 两轮隔离：`--show-lock` 5011ms / 848ms 返回错误，恢复 19.2s（同时恢复）/ 52.7s（异步恢复）；锁 owner/create_revision 不变；在线记录 create_revision 1040/1042 不变 | ✓（启动期非零退出按 §5 ② 已裁决合规） |
| 读写无错误 | 场景窗口内 proxy 读写无错误 | S-A/B/C 窗口 0 失败；总 40 次/台失败全在 S-D 主动停 proxy 后（主动停机后的负载计数） | ✓ |
| 在线状态不丢 | 在线记录在抖动窗口内不被误删 | quorum 抖动与 peer 隔离窗口内 create_revision/lease 完全未变（**误删 0**）；proxy 以 `re-confirming ownership` 静默续上 | ✓ |
| 查询/管理命令返回错误不挂死 | 不可用时返回错误，不无限阻塞 | `--show-lock` 0.85–5.0s 返回错误（`etcd client: initialize failed: context deadline exceeded` / `etcdserver: leader changed`）；`--create-group` 5.0s 返回 `store: load slots failed`；dashboard 读接口 200/3–18ms | ✓ |
| 无误判下线 | 不因 etcd 抖动把 proxy 判为下线 | 全隔离变体中 dashboard 注册表始终保留两台 proxy，未删 token；被撤销的只是 proxy 自己的 online 记录 | ✓ |

**裁决结论（2026-11-28 签收）**：偏差 ② 按契约 v1.3 §2 执行——建连/鉴权失败发生在抢锁事务发出前，Dashboard 报脱敏原因并非零退出、由进程管理器退避重启，非缺陷、已闭环；偏差 ① 的主场景口径为 peer 隔离（已回写契约），全隔离变体的 lease 按 TTL 过期与自动重注册符合预期。本次不改代码。

## 7. 证据位置与复现说明

- 关键证据是本地路径（**非仓库路径**，不入库）：`~/work/codis/.runtime/codis-a6/a6/report.md` 与同目录 `ev/`（139 个文件）；本文 §3 的"证据文件名"均相对 `ev/`。
- 本次为手工运维执行，**没有**对应的 `scripts/validation/etcd*/run_*.py`，也没有 evidence JSON（与 [README](README.md) 脚本索引"etcd 3.7 coordinator 目前只有 Go 集成测试入口 `CODIS_ETCD_SERVER_BINARY`，没有真机脚本"一致）。使用的辅助脚本均为**远端路径**（非仓库内）：`/root/codis-a6/a6-load.sh`（负载循环）、`/root/codis-a6/a6-phase.sh <phase>`（阶段快照：`etcd-status` / `member-list` / `show-lock` / `cluster-nodes` / `online-records` / `online-keys` / `lease-ttl` / `load-summary`），以及各组件 `start-*.sh` / `stop-*.sh` 与 `/usr/local/etcd/etcd{,-start,-stop}.sh`。
- 证书与口令：口令/私钥未进入任何命令行记录、日志或报告；本文与证据文件均不含口令、私钥、EIP 或实例 ID。
- 复现前提：三台 Linux 主机、三成员 etcd 3.7.4（HTTP + `--auth-token simple` + RBAC）、两个 codis-server + 两个 proxy（`etcd_register_online=true`、`cluster_nodes_compat="all"`）、一个 dashboard；负载与阶段快照脚本按上述字段自备。
