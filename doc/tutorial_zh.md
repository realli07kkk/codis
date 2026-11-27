# Codis 使用文档

Codis 是基于 Proxy 的 Redis 分片集群。客户端连接 Codis Proxy，Proxy 按 slot 将命令路由到 Redis 后端。上线前请核对[命令兼容范围](unsupported_cmds.md)。

## 版本分界

当前主线只支持外部部署的 etcd 3.7.x 作为 coordinator：控制面的拓扑与目标配置写入 etcd，业务键仍由 codis-server 保存。Go 依赖固定为 `go.etcd.io/etcd/client/v3 v3.7.4`，新二进制不包含旧驱动、回退开关、双写和旧的 proxy 注册协议。显式使用旧 CLI/TOML 参数会在建立连接前报错，并提示旧线 SHA（错误文本不带参数值）。

ZooKeeper、Consul、filesystem coordinator 与 Jodis 发现机制的最后支持基线是提交 `c635ade0424a1448fb251f32b88d64ca7cf9a555`。需要这些能力的部署检出该 SHA，按该提交的构建指南执行 `make`。不兼容变更与升级须知见 [etcd 3.7 发布说明](etcd_3_7_release_notes_zh.md)。

## Codis 版本简介

以下是项目历史版本和主线沿用的主要能力。
### codis 1.9
> * codis-server 基于 redis-2.8.13
> * codis-proxy 对 pipeline 支持不友好
> * codis-proxy 对 zk 强依赖
> * 同步迁移性能差，不支持大 key 迁移
### codis 2.0
> * codis-server 基于 redis-2.8.21
> * 重构了codis-proxy，性能大幅提升，对 pipeline 支持比较好
> * codis-proxy 对 zk 强依赖
> * 同步迁移性能差，不支持大key迁移
### codis 3.x
> * codis-3.2 是历史 release，早期 codis-server 曾基于 redis-3.2.8；当前主线已移除 Redis 3 支持，改用内嵌 Redis 8
> * 支持 slot 同步迁移、异步迁移和并发迁移，迁移支持范围见 [Redis 8 Codis Server 扩展](redis8_codis_server_zh.md)
> * 相比 2.0：重构了整个集群组件通信方式，codis-proxy 与 zookeeper 实现了解耦，废弃了codis-config 等
> * 元数据存储当前只使用外部 etcd 3.7.x；历史版本曾支持 zookeeper/filesystem/consul 等（仅旧线 ≤ `c635ade0…`）
> * 对 codis-proxy 进行了大量性能优化,通过控制GC频率、减少对象创建、内存预分配、引入 `cgo`、`jemalloc` 等，使其吞吐还是延迟，都已达到 codis 项目中最佳
> * proxy 实现 select 命令，支持多 DB
> * proxy 支持读写分离、优先读同 IP/同 DC 下副本功能
> * 基于 redis-sentinel 实现主备自动切换
> * 实现动态 pipeline 缓存区（减少内存分配以及所引起的 GC 问题）
> * proxy 支持通过 HTTP 请求实时获取 runtime metrics，便于监控、运维
> * 支持通过 influxdb 和 statsd 采集 proxy metrics
> * slot auto rebalance 算法从 2.0 的基于 max memory policy 变更成基于 group 下 slot 数量
> * 提供了更加友好的 dashboard 和 fe 界面，新增了很多按钮、跳转链接、错误状态等，有利于快速发现、处理集群故障
> * 新增 `SLOTSSCAN` 指令，便于获取集群各个 slot 下的所有 key

Codis 3.x 由以下组件组成：

* **Codis Server**：基于内嵌 Redis 8（`extern/redis-8.6.3`）开发。增加了额外的数据结构，以支持 slot 有关的操作以及数据迁移指令。具体的修改可以参考文档 [Redis 8 Codis Server 扩展](redis8_codis_server_zh.md)。

* **Codis Proxy**：客户端连接的 Redis 代理服务, 实现了 Redis 协议。 除部分命令不支持以外([不支持的命令列表](unsupported_cmds.md))，表现的和原生的 Redis 没有区别（就像 Twemproxy）。

    + 对于同一个业务集群而言，可以同时部署多个 codis-proxy 实例；
    + 不同 codis-proxy 之间由 codis-dashboard 保证状态同步。

* **Codis Dashboard**：集群管理工具，支持 codis-proxy、codis-server 的添加、删除，以及据迁移等操作。在集群状态发生改变时，codis-dashboard 维护集群下所有 codis-proxy 的状态的一致性。

    + 对于同一个业务集群而言，同一个时刻 codis-dashboard 只能有 0个或者1个；
    + 常规拓扑变更由 codis-dashboard 处理。

* **Codis Admin**：集群管理的命令行工具。

    + 可用于控制 codis-proxy、codis-dashboard 状态以及访问外部存储。

* **Codis FE**：集群管理界面。

    + 多个集群实例共享可以共享同一个前端展示页面；
    + 支持从配置文件加载 dashboard 列表，也支持通过 etcd 动态发现。

* **Codis Shake**：由 Dashboard 拉起的迁移 worker，执行 [跨集群数据迁移](data_migration_zh.md)。

* **Storage**：为集群状态提供外部存储。

    + 提供 Namespace 概念，不同集群的会按照不同 product name 进行组织；
    + 只支持外部部署的 etcd 3.7.x，连接方式见下文“etcd 连接配置”。

## 0. 下载与编译

在当前仓库的检出目录构建。当前主线使用 Go modules，`go.mod` 声明 `go 1.26.1` 和 `toolchain go1.27.4`。构建内嵌 Redis 8 还需要 C/C++ 和 autotools，安装命令见 [构建与发布指南](build_and_release_zh.md#依赖工具链)。

```bash
go version
make
```

`make` 将 `codis-dashboard`、`codis-proxy`、`codis-admin`、`codis-ha`、`codis-fe`、`codis-shake`、`codis-server`、Redis 工具和 FE 资源放在 `bin/`，并刷新 `config/` 中的默认配置。上游 [GitHub Releases](https://github.com/CodisLabs/codis/releases) 中的旧版本请使用对应版本的文档；依赖旧 coordinator 或旧注册机制的版本以提交 `c635ade0424a1448fb251f32b88d64ca7cf9a555` 为最后基线。

## 1. 快速启动

### 1.1 前置准备：etcd 3.7.x

新线不内置 coordinator，也不负责拉起它：先在集群外部署 etcd 3.7.x（单成员即可用于本地试用，生产按部署规范做多成员），确认客户端端口可用。

```bash
etcd --version
# 预期：etcd Version: 3.7.x
```

首次建群前，对所选 product 的命名空间做一次前缀查询，确认它为空。该查询是线性一致读，输出为空才继续：

```bash
etcdctl get --prefix --keys-only /codis3/codis-demo/
```

前缀非空时换一个 product 名，不要复用已有命名空间。这个检查只供部署人员确认所选 product，并发 Dashboard 之间始终由拓扑锁排他（见 2.2 与 2.6 节）。

### 1.2 连接文件与组件启动

下面的示例在本机 `http://127.0.0.1:2379` 启动一个演示集群：两个 group 各一台 codis-server，proxy 开启在线注册并由 Dashboard 上线。要求本机的 2379（etcd）、6379、6479（codis-server）、9090（FE）、11080（proxy 管理端口）、18080（Dashboard）、19000（proxy 业务端口）空闲。保留这个终端，后续停止时会用到进程 ID。

```bash
demo_dir=$(mktemp -d "${TMPDIR:-/tmp}/codis-demo.XXXXXX")
mkdir -p "$demo_dir/redis-a" "$demo_dir/redis-b"

cat > "$demo_dir/etcd.toml" <<EOF
endpoints = ["http://127.0.0.1:2379"]
dial_timeout = "5s"
request_timeout = "5s"
EOF

cat > "$demo_dir/dashboard.toml" <<EOF
coordinator_name = "etcd"
coordinator_addr = "http://127.0.0.1:2379"
admin_addr = "127.0.0.1:18080"
data_migration_workspace = "$demo_dir/migration"
rdb_analysis_workspace = "$demo_dir/rdb"
EOF

cat > "$demo_dir/proxy.toml" <<EOF
admin_addr = "127.0.0.1:11080"
proxy_addr = "127.0.0.1:19000"
etcd_register_online = true
EOF

./bin/codis-dashboard --config="$demo_dir/dashboard.toml" --etcd-config="$demo_dir/etcd.toml" \
  > "$demo_dir/dashboard.log" 2>&1 &
dashboard_pid=$!

./bin/codis-server config/redis.conf --dir "$demo_dir/redis-a" \
  --pidfile "$demo_dir/redis-a.pid" > "$demo_dir/redis-a.log" 2>&1 &
server_a_pid=$!

./bin/codis-server config/redis.conf --dir "$demo_dir/redis-b" --port 6479 \
  --pidfile "$demo_dir/redis-b.pid" > "$demo_dir/redis-b.log" 2>&1 &
server_b_pid=$!

./bin/codis-proxy --config="$demo_dir/proxy.toml" --dashboard=127.0.0.1:18080 \
  --etcd-config="$demo_dir/etcd.toml" > "$demo_dir/proxy.log" 2>&1 &
proxy_pid=$!

./bin/codis-fe --listen=127.0.0.1:9090 --etcd-config="$demo_dir/etcd.toml" \
  > "$demo_dir/fe.log" 2>&1 &
fe_pid=$!
```

`--dashboard` 让 proxy 通过 Dashboard 上线，`--etcd-config` 只提供 etcd 连接选项，两者可以同用：proxy 上线拿到 token 后在后台做在线注册，etcd 不可达不会阻塞上线。`codis-fe` 走动态来源，扫描 `/codis3` 下的 product 并读取其中的 `topom` 得到 Dashboard 地址。

`config/redis.conf` 由 `make` 生成，包含 `codis-enabled yes`。Redis 的标准输出写入示例中的 `$demo_dir/redis-*.log`。使用 `admin/codis-server-admin.sh start` 时，当前默认配置的标准输出写到 `log/codis-server.out`，脚本的 stop 路径仍依赖 `/tmp/redis_6379.pid`，要使用该路径需显式配置 Redis 的 `pidfile`。

等 Dashboard 与两台 codis-server 就绪后再执行后续命令（示例最多等待约 30 秒）：

```bash
for i in $(seq 1 60); do
  ./bin/codis-admin --dashboard=127.0.0.1:18080 --slots-status >/dev/null 2>&1 \
    && ./bin/redis-cli -h 127.0.0.1 -p 6379 PING >/dev/null 2>&1 \
    && ./bin/redis-cli -h 127.0.0.1 -p 6479 PING >/dev/null 2>&1 && break
  sleep 0.5
done
```

### 1.3 建群：两个 group、分配 slot、纳管 proxy

打开 `http://127.0.0.1:9090/`，选择 `codis-demo`，确认 `Proxy 实例`页的 proxy 已上线。在 `Group 与 Server`页创建 Group 1 并添加 `127.0.0.1:6379`，创建 Group 2 并添加 `127.0.0.1:6479`，再到 `Slots 分片`页点击`自动 Rebalance`，检查预览并确认。也可以在 dashboard 和 Redis 就绪后用命令行完成相同步骤。

```bash
./bin/codis-admin --dashboard=127.0.0.1:18080 --create-group --gid=1
./bin/codis-admin --dashboard=127.0.0.1:18080 --group-add --gid=1 --addr=127.0.0.1:6379
./bin/codis-admin --dashboard=127.0.0.1:18080 --create-group --gid=2
./bin/codis-admin --dashboard=127.0.0.1:18080 --group-add --gid=2 --addr=127.0.0.1:6479
./bin/codis-admin --dashboard=127.0.0.1:18080 --rebalance
./bin/codis-admin --dashboard=127.0.0.1:18080 --rebalance --confirm
```

确认 Rebalance 后，Dashboard 在后台处理 slot action。在 FE 的 `Slots 分片`页等待 1024 个 slot 都分配到两个 group（各 512 个）、迁移动作结束。使用命令行时，可运行 `./bin/codis-admin --dashboard=127.0.0.1:18080 --slots-status`，确认每个 slot 的 `backend_addr_group_id` 为 1 或 2，且没有 `locked: true`。proxy 需要短暂时间应用新的 slot 映射，立即读写报错时等一两秒重试。

再用业务端口验证两个 group 都承载数据。先用 `SLOTSHASHKEY` 找两个落在不同 group 的 key（slot 归属可从 `--slots-status` 查到），预期两次 `SET` 返回 `OK`、两次 `GET` 读回 `ok`；`GET` 之后再从该 key 所属 group 的 server 端口直连读取，确认数据落在预期后端，另一台返回空。

```bash
./bin/redis-cli -h 127.0.0.1 -p 19000 SLOTSHASHKEY codis-demo:key-a codis-demo:key-b
./bin/redis-cli -h 127.0.0.1 -p 19000 SET codis-demo:key-a ok
./bin/redis-cli -h 127.0.0.1 -p 19000 SET codis-demo:key-b ok
./bin/redis-cli -h 127.0.0.1 -p 19000 GET codis-demo:key-a
./bin/redis-cli -h 127.0.0.1 -p 19000 GET codis-demo:key-b
```

### 1.4 停止本地集群

```bash
kill "$fe_pid" "$proxy_pid" "$dashboard_pid" "$server_b_pid" "$server_a_pid"
wait "$fe_pid" "$proxy_pid" "$dashboard_pid" "$server_b_pid" "$server_a_pid"
```

进程退出后可清理 `$demo_dir` 中的临时配置、日志和数据。演示用的 product 命名空间可一并删除（只在演示环境执行）：

```bash
etcdctl del --prefix /codis3/codis-demo/
```

## 通过 Ansible 部署集群

`ansible/` 中的 playbook 示例已按 etcd 新线调整：`ansible/group_vars/all` 的 `etcd_endpoints` 为连接地址，凭据/mTLS 通过 `--etcd-config` 连接文件提供（见 playbook 模板注释）。用当前主线部署前，先按 `config/redis.conf` 更新 Server 模板，确保 `codis-enabled yes`，并按本次部署的实例地址与 etcd 连接参数调整 `ansible/group_vars/all` 与 `ansible/hosts`。从 playbook 所在目录执行。

```bash
cd ansible
ansible-playbook -i hosts site.yml
```

## 2. 启动及参数

### 2.1 etcd 连接配置（四个组件共用）

Dashboard、FE、Admin、Proxy 共用同一套连接参数：

- `--etcd=URL[,URL...]`：一个或多个同集群的 HTTP(S) 客户端 URL，逗号分隔，去掉项间空白；拒绝空项、嵌入凭据、文件路径和混用 HTTP/HTTPS。
- `--etcd-config=FILE`：独立的 TOML 连接文件，四个程序按同一规则解析，各角色可以使用各自凭据。

连接文件顶层字段：

| 字段 | 类型与默认值 | 规则 |
|---|---|---|
| `endpoints` | 字符串数组；未给出时按地址优先级取值 | 一个或多个同集群的 HTTP(S) 客户端 URL |
| `dial_timeout` | Go duration，默认 `"5s"` | 用于连接预检与构造期取 token；首次 lease 续约等待为该值 + 1s；不约束 TCP 建连 |
| `request_timeout` | Go duration，默认 `"5s"` | 每次 KV/Txn 的总预算，含该调用内的 SDK 重试 |
| `username`、`password` | 字符串，默认空 | 同设或同空；含密码的文件仅授予运行账号读取权限，密码不得出现在 CLI、URL、日志或回显 |
| `ca_file`、`cert_file`、`key_file` | 文件路径，默认空 | HTTPS 默认用系统信任根或显式 CA 并校验主机名；客户端证书与私钥成对出现；相对路径按连接文件所在目录解析 |

地址按显式 `--etcd` → 连接文件 `endpoints` → Dashboard 的 `coordinator_addr` → `http://127.0.0.1:2379` 依次取值，其余连接字段取连接文件或表中默认值；显式空地址、无效文件会在建立连接前报错，不会静默回退。

生产环境的 HTTPS/mTLS 连接文件示例（各角色按部署权限使用自己的证书）：

```toml
endpoints = ["https://etcd-a:2379", "https://etcd-b:2379", "https://etcd-c:2379"]
ca_file = "tls/ca.pem"
cert_file = "tls/client.pem"
key_file = "tls/client-key.pem"
```

角色凭据：Dashboard 可读写本 product 的持久元数据；开启在线注册的 Proxy 写自身在线键；FE 动态发现要跨 product 枚举 `/codis3` 并读取 Dashboard 地址，只需发现所需的读权限（要严格隔离不同 product 时用静态 FE）；Admin 按查询或变更操作分别授权。

组件建立连接时执行有界连接预检：所有 endpoint 共用同一个截止时间（`dial_timeout`），任一可用即通过；TLS 握手、证书校验或鉴权失败直接返回原因并脱敏。启动依赖 etcd 的入口通过首次实际操作验证角色权限，例如 Dashboard 用真实抢锁、FE 用首次发现、Admin 用首次读取。下面是四条启动命令：

```bash
codis-dashboard --config=dashboard.toml --etcd-config=etcd.toml
codis-fe --listen=127.0.0.1:9090 --etcd-config=etcd.toml
codis-admin --dashboard-list --etcd-config=etcd.toml
codis-proxy --config=proxy.toml --dashboard=127.0.0.1:18080 --etcd-config=etcd.toml
```

### 2.2 Codis Dashboard

用各二进制的 `--help` 查看参数。`codis-dashboard` 和 `codis-proxy` 均支持 `--default-config`，完整默认配置见 [dashboard.toml](../config/dashboard.toml) 和 [proxy.toml](../config/proxy.toml)，需要时用 `./bin/codis-dashboard --default-config` 再生。

Dashboard 始终使用 etcd：`coordinator_name` 默认且只接受 `"etcd"`，`coordinator_addr` 仅作为地址回退项，凭据与 TLS 统一放在连接文件；旧的 `coordinator_auth` 已移除，写入即报错。

```bash
./bin/codis-dashboard --config=config/dashboard.toml
```

Dashboard 启动时抢占本 product 的持久锁 `/codis3/{product}/topom`（不附 lease，值含每进程随机的 `owner_id`），锁在，管理写入与后台拓扑动作才开放。冲突时记录持有者的 `admin_addr`/`owner_id`/`create_revision` 并每 2 秒退避重试（约 16 次后放弃）；权限或鉴权被拒绝时立即退出；结果未知时重试回读锁，不会把读不到解释成锁空闲。正常停止时按本次持有的 revision 释放锁，异常退出则保留锁，需要人工恢复（见 2.6 与 [FAQ](FAQ_zh.md)）。Dashboard 不再提供 `--remove-lock`。

`product_name` 和 `product_auth` 要在 dashboard 与 proxy 上匹配。客户端单密码使用 proxy 的 `session_auth`。后端 Redis 服务身份由 `backend_auth_username` / `backend_auth_password` 配置，两者都为空时回退到 `product_auth`。多账号认证见 [Codis ACL 管理](codis_acl_zh.md)。

### 2.3 Codis Proxy

`admin_addr` 是管理 HTTP 地址，默认端口 11080。`proxy_addr` 是业务 Redis 协议地址，默认端口 19000。启动时可用 `--dashboard=ADDR` 通过 Dashboard 自动上线，也可用 `--etcd`（经 etcd 中发布的 Dashboard 地址）、`--fillslots=FILE`，三者至多一个；全部省略时等待 Dashboard 在 FE 的 `Proxy 实例`页手动纳管。完成 slot 同步和上线后，proxy 才开始正常路由业务请求。

```bash
./bin/codis-admin --dashboard=127.0.0.1:18080 --create-proxy --addr=127.0.0.1:11080
```

客户端读缓冲和超时使用 `session_recv_bufsize`（`128kb`）与 `session_recv_timeout`（`30m`），写缓冲和超时使用 `session_send_bufsize`（`64kb`）与 `session_send_timeout`（`30s`）。`session_max_pipeline` 默认 `10000`，`session_keepalive_period` 默认 `75s`。

TOML 新增 `etcd_register_online`（默认 `false`）：proxy 被 Dashboard 纳管、拿到 token 后在后台惰性建连并注册在线记录，失败只退避重试，不影响业务上线。`cluster_nodes_compat` 仍为 `disabled`/`self`/`all`，`all` 需要开启注册；`cluster_nodes_refresh_period` 保留（默认 `30s`）。在线记录与 `CLUSTER NODES` 语义见第 3 节。

### 2.4 Codis Server

```bash
./bin/codis-server config/redis.conf
```

自备配置也需要包含 `codis-enabled yes`。配置、slot 模型和迁移命令见 [Codis Server 扩展](redis8_codis_server_zh.md)。codis-server 不依赖 etcd。

### 2.5 Codis FE（可选组件）

`--listen` 必填。Dashboard 列表来源二选一：静态文件 `--dashboard-list=FILE`，或动态 etcd 来源（给出 `--etcd` 或 `--etcd-config` 任一即选中，二者同用算同一种来源）。动态来源扫描 `/codis3` 下的 product 并读取各自的 `topom` 得到 Dashboard 地址，因此 FE 账号需要这些读权限。

```bash
./bin/codis-admin --dashboard-list --etcd-config=etcd.toml > codis.json
./bin/codis-fe --dashboard-list=codis.json --listen=127.0.0.1:9090
```

动态来源的首次刷新同时充当连接预检：权限/鉴权被拒绝立即退出并给出原因；连接类错误重试 3 次、每次间隔 2 秒后退出。启动成功后，刷新失败只保留上一次成功快照。

### 2.6 Codis Admin（命令行工具）

正常的拓扑操作通过 dashboard API 完成，用法不变。直接访问元数据的命令要求至少给出一个共享参数（`--etcd` 或 `--etcd-config`），`--config-convert` 除外：

- `--show-lock --product=NAME`：输出该 product 拓扑锁的 JSON（至少含 `product`、`admin_addr`、`owner_id`、`create_revision`，另有 `mod_revision` 与 `lease`）；无锁时输出 `null` 并成功退出。
- `--remove-lock --product=NAME --expected-revision=N --confirm`：只按输入 revision 在事务中删除该锁。缺 revision、revision 非正、缺 `--confirm` 都失败；锁不存在、revision 不匹配、结果未知或请求被拒绝均以非零退出，且文案互相区分；回读发现替代锁时一律保留。
- `--dashboard-list`：列出 `/codis3` 下各 product 的 Dashboard 地址，可重定向成静态 FE 列表；`--config-dump`：导出本 product 元数据，只输出 Codis 3.x 格式，旧的 `-1`（读 Codis 1.x/ZK 树）已移除。
- `--config-restore=FILE --product=NAME --confirm`：把配置文件提交到该 product 命名空间；加 `--confirm` 才真正提交，缺省只打印作为调试。
- `--config-convert FILE`：纯文件格式转换，不连接后端。

异常退出后的拓扑锁需要人工恢复。先隔离原进程与自动拉起机制（确认它不会再启动），再查锁，最后按观察到的 revision 解锁：

```bash
./bin/codis-admin --etcd-config=etcd.toml --product=codis-demo --show-lock
./bin/codis-admin --etcd-config=etcd.toml --product=codis-demo --remove-lock --expected-revision=417 --confirm
```

如果 proxy 已异常退出并且无法正常移除，确认进程已退出后再强制从 dashboard 移除。

```bash
./bin/codis-admin --dashboard=127.0.0.1:18080 --remove-proxy --addr=127.0.0.1:11080 --force
```

## 3. Proxy 高可用与在线发现

可以为同一个 product 部署多个 proxy，由 Dashboard 通过管理 API 保证它们的状态同步。

### 3.1 在线注册

开启 `etcd_register_online = true` 后，proxy 在被纳管（token 已知）且上线之后，后台惰性建立 etcd 连接并维护在线记录：

- 键为 `/codis3/{product}/online-proxies/{token}`，值为包含 `token` 与 `addr`（客户端访问的 proxy 地址）的 JSON；
- 每个在线实例申请独立 lease，TTL 固定 30 秒，不另设调优参数；重试同一轮注册时复用该 lease，用版本为 0 的条件事务把键绑定到 lease；
- 相同 token 已由其他 lease 持有时不覆盖、不认领，等待对方正常退出或过期后再竞争；
- 注册失败在后台有界退避重试，业务上线不等待注册成功；
- 关闭顺序为：停止并等待本实例注册循环 → 有界撤销本实例 lease → 关闭客户端；撤销失败由服务端 TTL 收敛，崩溃同样靠 TTL 过期，不按 token 裸删键。

`all` 模式每个刷新周期对在线前缀做一次一致读，跳过坏记录后复用自身的补全、去重、排序与伪 slot 分配；首次读失败只提供自身，此后失败保留最后一次成功快照，成功读到空列表时只返回自身。服务端删除在线键后，最迟在下一个成功刷新周期移除该 peer。因此陈旧窗口的上限是该 lease 的 TTL（30 秒）加上一个刷新周期。

### 3.2 CLUSTER NODES

`cluster_nodes_compat` 的三种取值与注册开关的组合如下。它不是 Jodis 协议：内部在线记录只供 Codis 自身发现，普通 Redis 请求仍读本地路由。

| 模式 | 注册关闭 | 注册开启 |
|---|---|---|
| `disabled` | 沿用禁用响应，无发现连接 | 发布自身，其余 proxy 可发现它；本机仍禁用此命令 |
| `self` | 只返回自身，无发现连接 | 发布自身，本机仍只返回自身 |
| `all` | 配置校验失败，提示启用注册 | 发布自身，轮询同 product 的在线列表并生成响应 |

### 3.3 旧线说明（仅 ≤ `c635ade0424a1448fb251f32b88d64ca7cf9a555`）

Jodis 是旧线的 proxy 发现机制：客户端通过 ZooKeeper（或在 Consul 上模拟临时节点）读取 `/jodis/{product}/proxy-{token}` 注册信息发现 proxy，proxy 侧使用 `jodis_name`、`jodis_addr`、`jodis_auth`、`jodis_timeout`、`jodis_compatible` 等配置，`models.Proxy` 模型还带 `jodis_path` 字段。新线移除了这套注册协议、`jodis_*` 配置项与该模型字段，替换为上面的 etcd 在线注册；使用 Jodis 的既有客户端与部署请检出上述 SHA 构建。同理，旧线的 `--zookeeper` / `--filesystem` / `--consul` 及 `coordinator_auth` 已被 etcd 连接参数取代，显式使用会报错并提示该 SHA。

### 3.4 Redis 后端高可用

Redis 后端可使用 Sentinel 完成故障转移，dashboard 和 proxy 根据监控结果更新所使用的 master。`codis-ha` 是另一个通过 dashboard API 巡检和 promote 的运维入口。手动 promote 或 `codis-ha` promote 后，检查组内其它实例的复制指向和同步进度，必要时创建 sync action。

## 4. Docker 部署（未适配）

仓库内的 `Dockerfile`、`make docker` 与 `scripts/docker.sh` 仍是旧线打包方式，尚未适配 etcd 3.7 新线：镜像与脚本中的 coordinator 参数、默认配置均未随新线更新，按它们启动会得到旧参数报错。在完成适配前，请直接用 `bin/` 产物部署或自行编写镜像，运行时按第 2 节提供 `--etcd` / `--etcd-config`。适配状态记录在 [发布说明](etcd_3_7_release_notes_zh.md)。

## 5. 从Codis 2.x 升级（仅旧线）

以下步骤涉及从 ZooKeeper 读取 Codis 1.x 配置树（`--config-dump -1`），新线已移除该选项与旧 ZK 读取路径，因此本节只适用于检出旧线 `c635ade0424a1448fb251f32b88d64ca7cf9a555` 构建的工具。新线不做存量元数据迁移，也不在停机和切换流程上提供承诺；需要长期使用请在新线按第 1 节重新建群，业务数据用 [跨集群数据迁移](data_migration_zh.md) 搬运。

**注意1：升级时，需要保证所有 slot 都处在 `online` 状态。即没有任何数据迁移操作正在进行。**

**注意2：升级完成后，需要手动关闭 Codis 2.x 的所有 proxy 和 config 组件。**

#### step 1. 导出配置文件

```bash
$ ./bin/codis-admin --config-dump --product=codis_v2.0 --zookeeper=127.0.0.1:2181 -1 | tee codis_v2.0.json
```

该命令会从 zookeeper 上拉取 `/zk/codis/db_codis_v2.0` 下全部的文件，并组织成 json 格式并输出。

选项 `-1` 表示配置文件是 Codis 1.x 版本，缺省是 Codis 3.x 版本。

#### step 2. 转换配置文件

```bash
$ ./bin/codis-admin --config-convert codis_v2.0.json | tee codis_v3.0.json
```

该命令会将 Codis 1.x 版本的配置文件中有效信息提取出来，并转成 Codis 3.x 版本的配置文件并输出。

#### step 3. 更新配置文件

**注意：更新配置文件时，请确保 Codis 3.x 中该集群不存在，否则可能导致更新失败或者集群状态异常。**

```bash
$ ./bin/codis-admin --config-restore=codis_v3.0.json --product=codis_v3.0 --zookeeper=127.0.0.1:2181 --confirm
```

该命令会将 Codis 3.x 版本的配置文件提交到 `/codis3/codis_v3.0` 目录下。

选项 `--confirm` 选项表示确认提交，缺省时该命令仅仅打印配置文件作为调试。

#### step 4. 启动 Codis 3.x dashboard 以及 proxy

过程参考之前章节。因为集群信息已经存在，所以可以安全启动 codis-dashboard，并逐一添加 codis-proxy 到集群中。

#### step 5. 关闭 Codis 2.x dashboard 以及 proxy

旧线的 Codis 3.x 组件兼容 Jodis 协议；新线已用第 3 节的 etcd 在线注册取代它。

因为 Codis 2.x 与 Codis 3.x 在外部存储中的组织结构不同，所以可以安全的 `kill` 掉全部 Codis 2.x 组件。

**注意：关闭过程请不要使用 `kill -9`，因为旧组件在退出时会自动清理部分注册信息。**
