# Codis 使用文档

Codis 是基于 Proxy 的 Redis 分片集群。客户端连接 Codis Proxy，Proxy 按 slot 将命令路由到 Redis 后端。上线前请核对[命令兼容范围](unsupported_cmds.md)。

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
> * 元数据存储支持 zookeeper/filesystem/consul 等，可自行扩展支持新的存储，集群正常运行期间，即便元存储故障也不再影响 codis 集群，大大提升 codis-proxy 稳定性
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
> * codis-proxy 与 codis-dashbaord 支持 docker 部署

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
    + 支持从配置文件加载 dashboard 列表，也支持通过 coordinator 动态发现。

* **Codis Shake**：由 Dashboard 拉起的迁移 worker，执行 [跨集群数据迁移](data_migration_zh.md)。

* **Storage**：为集群状态提供外部存储。

    + 提供 Namespace 概念，不同集群的会按照不同 product name 进行组织；
    + 目前提供了 Zookeeper、Consul、Fs 三种实现，但是提供了抽象的 interface 可自行扩展。

## 0. 下载与编译

在当前仓库的检出目录构建。当前主线使用 Go modules，`go.mod` 声明 `go 1.26.1` 和 `toolchain go1.27.4`。构建内嵌 Redis 8 还需要 C/C++ 和 autotools，安装命令见 [构建与发布指南](build_and_release_zh.md#依赖工具链)。

```bash
go version
make
```

`make` 将 `codis-dashboard`、`codis-proxy`、`codis-admin`、`codis-ha`、`codis-fe`、`codis-shake`、`codis-server`、Redis 工具和 FE 资源放在 `bin/`，并刷新 `config/` 中的默认配置。上游 [GitHub Releases](https://github.com/CodisLabs/codis/releases) 中的旧版本请使用对应版本的文档。

## 1. 快速启动

在仓库根目录启动本地测试集群。示例使用 filesystem coordinator、临时目录和 loopback 监听，要求本机的 6379、9090、11080、18080、19000 端口空闲。保留这个终端，后续停止时会用到进程 ID。

```bash
demo_dir=$(mktemp -d "${TMPDIR:-/tmp}/codis-demo.XXXXXX")

cat > "$demo_dir/dashboard.toml" <<EOF
coordinator_name = "filesystem"
coordinator_addr = "$demo_dir/coordinator"
admin_addr = "127.0.0.1:18080"
data_migration_workspace = "$demo_dir/migration"
rdb_analysis_workspace = "$demo_dir/rdb"
EOF

cat > "$demo_dir/proxy.toml" <<EOF
admin_addr = "127.0.0.1:11080"
proxy_addr = "127.0.0.1:19000"
EOF

./bin/codis-dashboard --config="$demo_dir/dashboard.toml" \
  > "$demo_dir/dashboard.log" 2>&1 &
dashboard_pid=$!

./bin/codis-server config/redis.conf --dir "$demo_dir" \
  --pidfile "$demo_dir/redis.pid" > "$demo_dir/redis.log" 2>&1 &
server_pid=$!

./bin/codis-proxy --config="$demo_dir/proxy.toml" --dashboard=127.0.0.1:18080 \
  > "$demo_dir/proxy.log" 2>&1 &
proxy_pid=$!

./bin/codis-fe --filesystem="$demo_dir/coordinator" --listen=127.0.0.1:9090 \
  > "$demo_dir/fe.log" 2>&1 &
fe_pid=$!
```

`config/redis.conf` 由 `make` 生成，包含 `codis-enabled yes`。Redis 的标准输出写入示例中的 `$demo_dir/redis.log`。使用 `admin/codis-server-admin.sh start` 时，当前默认配置的标准输出写到 `log/codis-server.out`，脚本的 stop 路径仍依赖 `/tmp/redis_6379.pid`，要使用该路径需显式配置 Redis 的 `pidfile`。

### 通过 FE 添加 group 和初始化 slot

打开 `http://127.0.0.1:9090/`，选择 `codis-demo`，确认 `Proxy 实例`页的 proxy 已上线。在 `Group 与 Server`页创建 Group 1，添加 `127.0.0.1:6379`，再到 `Slots 分片`页点击`自动 Rebalance`，检查预览并确认。也可以在 dashboard 和 Redis 就绪后用命令行完成相同步骤。

```bash
./bin/codis-admin --dashboard=127.0.0.1:18080 --create-group --gid=1
./bin/codis-admin --dashboard=127.0.0.1:18080 --group-add --gid=1 --addr=127.0.0.1:6379
./bin/codis-admin --dashboard=127.0.0.1:18080 --rebalance
./bin/codis-admin --dashboard=127.0.0.1:18080 --rebalance --confirm
```

确认 Rebalance 后，Dashboard 在后台处理 slot action。在 FE 的 `Slots 分片`页等待 1024 个 slot 都分配到 Group 1、迁移动作结束。使用命令行时，可运行 `./bin/codis-admin --dashboard=127.0.0.1:18080 --slots-status`，确认各 slot 的 `backend_addr_group_id` 为 1，且没有 `locked: true`。再用业务端口验证，预期依次返回 `OK` 和 `ok`。

```bash
./bin/redis-cli -h 127.0.0.1 -p 19000 SET codis-demo:key ok
./bin/redis-cli -h 127.0.0.1 -p 19000 GET codis-demo:key
```

### 停止本地集群

```bash
kill "$fe_pid" "$proxy_pid" "$dashboard_pid" "$server_pid"
wait "$fe_pid" "$proxy_pid" "$dashboard_pid" "$server_pid"
```

进程退出后可清理 `$demo_dir` 中的临时配置、日志和数据。

## 通过 Ansible 部署集群

`ansible/` 中的 playbook 是旧部署示例。用当前主线部署前，先按 `config/redis.conf` 更新 Server 模板，确保 `codis-enabled yes`，并按本次部署的实例地址和 coordinator 调整 `ansible/group_vars/all` 与 `ansible/hosts`。从 playbook 所在目录执行。

```bash
cd ansible
ansible-playbook -i hosts site.yml
```

## 2. 启动及参数

用各二进制的 `--help` 查看参数。`codis-dashboard` 和 `codis-proxy` 均支持 `--default-config`，完整默认配置见 [dashboard.toml](../config/dashboard.toml) 和 [proxy.toml](../config/proxy.toml)。

`product_name` 和 `product_auth` 要在 dashboard 与 proxy 上匹配。客户端单密码使用 proxy 的 `session_auth`。后端 Redis 服务身份由 `backend_auth_username` / `backend_auth_password` 配置，两者都为空时回退到 `product_auth`。多账号认证见 [Codis ACL 管理](codis_acl_zh.md)。

#### 2.1 Codis Dashboard

`coordinator_name` 支持 `filesystem`、`zookeeper` 和 `consul`。随仓库生成的默认配置用 `filesystem` 和 `/tmp/codis`，`admin_addr` 默认为 `0.0.0.0:18080`。部署到多机时，按所选 coordinator 配置 `coordinator_addr` 和需要的 `coordinator_auth`。

```bash
./bin/codis-dashboard --config=config/dashboard.toml
```

#### 2.2 Codis Proxy

`admin_addr` 是管理 HTTP 地址，默认端口 11080。`proxy_addr` 是业务 Redis 协议地址，默认端口 19000。启动时可用 `--dashboard=ADDR` 自动注册，也可在 FE 的 `Proxy 实例`页添加，或用下列命令注册。完成 slot 同步和上线后，proxy 才开始正常路由业务请求。

```bash
./bin/codis-admin --dashboard=127.0.0.1:18080 --create-proxy --addr=127.0.0.1:11080
```

客户端读缓冲和超时使用 `session_recv_bufsize`（`128kb`）与 `session_recv_timeout`（`30m`），写缓冲和超时使用 `session_send_bufsize`（`64kb`）与 `session_send_timeout`（`30s`）。`session_max_pipeline` 默认 `10000`，`session_keepalive_period` 默认 `75s`。Jodis 注册通过 `jodis_name`、`jodis_addr`、`jodis_auth` 配置，`jodis_timeout` 默认 `20s`。

#### 2.3 Codis Server

```bash
./bin/codis-server config/redis.conf
```

自备配置也需要包含 `codis-enabled yes`。配置、slot 模型和迁移命令见 [Codis Server 扩展](redis8_codis_server_zh.md)。

#### 2.4 Codis FE（可选组件）

指定 `--listen`，并从 `--dashboard-list`、`--filesystem`、`--zookeeper`、`--consul` 中选择一个 dashboard 列表来源。使用动态发现时，coordinator 应与 dashboard 使用的一致。静态列表可以手工编辑，也可用 `codis-admin` 生成。

```bash
./bin/codis-admin --dashboard-list --filesystem=/tmp/codis > codis.json
./bin/codis-fe --dashboard-list=codis.json --listen=127.0.0.1:9090
```

#### 2.5 Codis Admin（命令行工具）

正常的拓扑操作通过 dashboard API 完成。异常退出后，如果 dashboard 的拓扑锁仍存在，先确认原进程已退出，再使用该 product 的实际 coordinator 清理。例如默认 filesystem 配置对应以下命令。

```bash
./bin/codis-admin --remove-lock --product=codis-demo --filesystem=/tmp/codis
```

如果 proxy 已异常退出并且无法正常移除，确认进程已退出后再强制从 dashboard 移除。

```bash
./bin/codis-admin --dashboard=127.0.0.1:18080 --remove-proxy --addr=127.0.0.1:11080 --force
```

## 3. Jodis 与 HA

可以为同一个 product 部署多个 proxy。[Jodis](https://github.com/CodisLabs/jodis) 客户端通过 ZooKeeper 注册信息发现 proxy。proxy 的注册后端支持 ZooKeeper 和 Consul，默认路径为 `/jodis/{product}/proxy-{token}`。只有兼容旧 Codis 2.0 客户端时才需要 `jodis_compatible`。

Redis 后端可使用 Sentinel 完成故障转移，dashboard 和 proxy 根据监控结果更新所使用的 master。`codis-ha` 是另一个通过 dashboard API 巡检和 promote 的运维入口。手动 promote 或 `codis-ha` promote 后，检查组内其它实例的复制指向和同步进度，必要时创建 sync action。

## 4. Docker 部署

下面以 proxy 为例。将 `100.0.1.100` 换成 dashboard 和客户端都可访问的宿主机地址。`--host-admin` 和 `--host-proxy` 指定对外公布的地址，容器内监听地址由配置文件决定。

```bash
make docker
docker run --name Codis-Proxy -d -p 29000:19000 -p 21080:11080 \
  -v "$PWD/config/proxy.toml:/codis/proxy.toml:ro" codis-image \
  codis-proxy --config=/codis/proxy.toml \
  --host-admin=100.0.1.100:21080 --host-proxy=100.0.1.100:29000
```

在 dashboard 中用 `100.0.1.100:21080` 注册 proxy，客户端连接 `100.0.1.100:29000`。其它容器示例见 [scripts/docker.sh](../scripts/docker.sh)。

## 5. 从Codis 2.x 升级

以下是早期 Codis 2.x 到 Codis 3.x 的元数据迁移步骤。命令处理 slot、group 和 proxy 元数据，数据和 Redis 内核兼容性需单独处理。使用当前 Redis 8 主线时，请参阅 [Redis 8 验证与切换](redis8_validation_zh.md) 和 [跨集群数据迁移](data_migration_zh.md)。

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

Codis 3.x 的组件兼容 Jodis 协议。

因为 Codis 2.x 与 Codis 3.x 在外部存储中的组织结构不同，所以可以安全的 `kill` 掉全部 Codis 2.x 组件。

**注意：关闭过程请不要使用 `kill -9`，因为旧组件在退出时会自动清理部分注册信息。**
