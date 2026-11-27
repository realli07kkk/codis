# 新建 Codis 集群使用 etcd 3.7 的实施契约

**实施交接版 v1.1 · 2026-11-26。** 实施 Agent 首先完成第 0 节 P0 调研，再按第 1–5 节的对外行为开发，第 6 节给出开发顺序，第 7 节用于签收。实现与验证均待执行。适用场景为使用新版 Codis，在 etcd 的空 product 命名空间中首次建群。存量集群的后端迁移、原地替换和跨版本回退均在本契约范围之外。

## 0. P0 前置调研：Go 依赖链与 gRPC 的影响

**采用官方 `go.etcd.io/etcd/client/v3` 会引入 gRPC。** v3.7.4 的 `client.go` 调用 `grpc.NewClient`，KV 和 Lease 通过已生成的 protobuf stub 使用这条连接，续约使用双向流。新增网络链路是 Codis 组件向外部 etcd 客户端端口发起的 gRPC/HTTP2 连接。Codis 继续提供原有 Redis 和管理 HTTP 入口。正常构建使用上游已生成的 Go 文件，不需要安装 `protoc`、生成 Codis RPC 服务或嵌入 etcd server。[客户端连接](https://github.com/etcd-io/etcd/blob/v3.7.4/client/v3/client.go) · [KV/Lease 调用](https://github.com/etcd-io/etcd/blob/v3.7.4/client/v3/retry.go) · [生成的 gRPC stub](https://github.com/etcd-io/etcd/blob/v3.7.4/api/etcdserverpb/rpc_grpc.pb.go)

### 已核对的基线与预计变化

Codis 基线为 `c635ade0424a1448fb251f32b88d64ca7cf9a555`，本地 Go 1.27.4、darwin/arm64、默认 build tags。已用 `go list -m`、`go mod graph` 和逐入口的 `go list -deps` 核对旧图。下表右列取自 etcd 的固定标签，表示上游要求；候选 Codis 的最终选版由 Go 的最小版本选择（MVS）决定，仍须在 P0 中实测。[Codis 模块清单](../go.mod) · [client/v3 模块清单](https://github.com/etcd-io/etcd/blob/v3.7.4/client/v3/go.mod) · [Go 模块选版规则](https://go.dev/ref/mod#minimal-version-selection)

| 模块或链路 | 当前 Codis | 接入 v3.7.4 的变化及核查重点 |
| --- | --- | --- |
| etcd SDK、gRPC-Go | `client/v3`、`google.golang.org/grpc` 均不在当前选定模块中 | Codis 新增直接依赖 `go.etcd.io/etcd/client/v3 v3.7.4`；它要求 `go.etcd.io/etcd/api/v3`、`go.etcd.io/etcd/client/pkg/v3` 同为 v3.7.4，要求 `google.golang.org/grpc v1.83.2`。若 Codis 自己导入 `grpc/status` 等包，再按实际 import 记为直接依赖。 |
| 两代 protobuf Go 模块 | 已选 `google.golang.org/protobuf v1.34.1`、`github.com/golang/protobuf v1.5.0` | 客户端分别要求 v1.36.11 和 v1.5.4。前者供生成代码使用，客户端 KV 源码也直接引用后者的兼容包；两条模块路径要分别列差异。 |
| 日志、生成代码相关包 | `go.uber.org/zap` 不在当前选定模块中 | 客户端要求 zap v1.27.1；etcd API 要求 `grpc-gateway/v2 v2.29.0` 和 `genproto/googleapis/{api,rpc}`。核对实际进入各命令的包，例如生成消息引用的 OpenAPI options；业务范围不含网关监听或替换 Codis 日志系统。 |
| 指标与其他传递依赖 | 现有 `armon/go-metrics` 已要求 `prometheus/client_golang v1.4.0`，六入口默认包图未包含其包 | client/v3 声明 `client_golang v1.23.2` 和 gRPC Prometheus provider v1.1.0。gRPC 自身还声明 OpenTelemetry、xDS 等模块。对这些体量较大的依赖，分别记录模块选版、实际导入包和是否启用运行行为；本方案不要求启用 exporter、指标端口或 xDS。 |
| 已有公共模块 | 已选 `x/net v0.62.0`、`x/sys v0.49.0`、`x/text v0.43.0`，以及 `go-humanize v1.0.1`、`xxhash/v2 v2.3.0` | 这些版本与 client/v3 清单对应项相同。检查合图后是否还有其他依赖抬高版本，避免把所有上游模块都列成新增。 |
| 旧驱动与本地构建 | `github.com/go-zookeeper/zk v1.0.4`、`github.com/hashicorp/consul/api v1.34.3` 为直接依赖；filesystem 是仓内 Go 包 | 三个生产驱动都要移除。Consul 专属间接依赖逐条以引用链确认，共享模块按实际使用保留。Codis 的本地 RDB、jemalloc 两项 `replace` 保留；上游开发仓的相对 `replace` 不复制进 Codis。 |

**容易漏算的两个入口。** 本地六次 `go list -deps` 显示，dashboard、proxy、admin、fe、ha、shake 目前全部可达 ZK 和 Consul 包，全部未出现 gRPC 包。共享 `pkg/models` 工厂直接导入具体驱动，是模型消费者也能带上后端依赖的一条路径。P0 应以“具体客户端在需要连接的组件中构造”为候选接线方式，检查间接经过 `pkg/proxy` 或 `pkg/topom` 的路径；用六个入口的包图验证 HA、Shake 是否被动引入 gRPC。即使普通 Proxy 在配置关闭时不拨号，其可执行文件仍须支持开启后的 etcd 模式。模块在 build list 中、包参与编译、代码实际运行须分别回答。[现有工厂](../pkg/models/client.go) · [etcd API 声明](https://github.com/etcd-io/etcd/blob/v3.7.4/api/go.mod) · [gRPC-Go v1.83.2 声明](https://github.com/grpc/grpc-go/blob/v1.83.2/go.mod)

### 实施 Agent 的首项任务与通过条件

**优先级最高，先交付依赖调研结论，再开始正式修改 Store、配置和命令。** 在隔离副本内做最小接入实验，允许临时引用实际要用的 client/v3 操作；主工作树不合入探针代码。调研产物写入实施交付记录，包含下列四项。

1. **取得可复现的前后快照。** 记录 Codis SHA、etcd/client 标签、Go 版本、GOOS/GOARCH、CGO、tags 和本地 replace。先存基线，再在保留两个相对 replace 目录的隔离副本引入固定 SDK。保留 `go.mod/go.sum` 差异、`go list -m -json all` 选版、`go mod graph` 引用边。按新增、升级、版本相同、可移除列出模块与引入方。单列暂时并存的旧驱动，最终以退役后快照复核。不得用 `go.sum` 行数推断运行依赖，也不要为取得差异执行全量 `go mod tidy`。
2. **逐入口验证编译和产物。** 在候选副本让真实入口能调用构造器、KV/Txn 和 Lease 路径，避免只在空程序里添加一个 import。分别对六个命令运行下面的包图命令，并对 gRPC、protobuf、Prometheus、zap、OpenTelemetry 及两个旧驱动用 `go mod why -m` 辅助追踪；将测试依赖单列。按相同 toolchain、tags、CGO、ldflags 构建基线和候选，记录六个产物的字节差及 `go version -m` 模块信息。优先验证本机与发布使用的 `cgo_jemalloc` 配置，列明 darwin/arm64、linux/amd64、linux/arm64 的已有证据与待验项。若 HA/Shake 出现新增传输包，给出最短导入链、拆开构造器的成本及选定方案。
3. **验证主要运行行为。** 用固定版外部单成员 etcd 做最小实验并记录四组观察：多 endpoint 中一个不可达时的首次实际 RPC 与 deadline；TLS/mTLS 与鉴权失败如何暴露；KV/Txn 的重试策略和响应丢失后如何区分未知结果；Lease KeepAlive 流在断连、取消和 Client.Close 时如何退出。查明 SDK/zap 的默认日志及指标注册行为，普通 Redis 转发不得新增每请求 gRPC 调用。说明各结果支撑第 2、3、4 节的哪条设计。此阶段的单成员实验单独记录；三成员失去多数派的发布验证仍属 A6。
4. **形成可执行结论。** 逐项给出“采用的选版与原因、受影响组件、实测证据、未验项及对应后续验收”。至少拿到可复现模块差异、逐入口包图、本机候选构建及关键 RPC/lease 实验后方可进入步骤 1。若出现本机编译冲突、需要变更 Go/toolchain、破坏仓内 replace 或需修改既定锁/重试语义，先在调研记录中定位原因并更新相应实施条款。其他平台实测条件不足时写明平台和缺口，转交发布验收。实施期间 SDK 补丁变化须重做受影响的比较。

基线与候选使用相同命令；以下示例列出默认 tags 下各入口可达的模块和包，发布 tags 用同样方式另取一份。

```sh
for component in dashboard proxy admin fe ha shake; do
  go list -mod=readonly -deps \
    -f '{{if .Module}}{{.Module.Path}} {{.Module.Version}} {{.ImportPath}}{{end}}' \
    "./cmd/$component" > "$component.packages.txt" || exit 1
done
```

在各自的临时仓库根目录执行并保存结果。P0 报告应把已实测的新增构建成本与尚待发布验证的风险列清楚；此文表中的上游声明仅是调研起点。

## 1. 版本分界与承诺

| 项目 | 契约 |
| --- | --- |
| 旧线终点 | 指定 Codis 提交 `c635ade0424a1448fb251f32b88d64ca7cf9a555` 为 ZooKeeper、Consul、filesystem coordinator 及 Jodis 发现机制的最后支持基线。依赖任一旧能力的用户检出此 SHA，按该提交的构建指南执行 `make`。发布说明永久列出完整 SHA。 |
| 新线起点 | 首个采用本契约发布的 Codis 版本只支持外部部署的 etcd 3.7.x 作为 coordinator。新二进制移除旧驱动、回退开关、双写和 Jodis 注册协议。此处的 Consul 指 HashiCorp Consul，Jodis 指通过注册节点发现 proxy 的客户端。 |
| 首个固定版本 | 本次评审采用上游最新稳定补丁 **v3.7.4**。服务端验收基线为 v3.7.4，Go 依赖固定为 `go.etcd.io/etcd/client/v3 v3.7.4`。实施开始时核对上游 3.7 稳定发布；若已有更新补丁，在依赖变更中明确记录同一补丁的服务端 tag、提交及客户端版本，按本文验收。3.7.x 是部署支持范围，版本核对放在部署预检和验收，不要求所有业务进程为检查版本额外取得集群管理权限。构建不得浮动解析 `latest`。 |
| 影响范围 | etcd 保存 Codis 控制面的拓扑和目标配置。业务键继续由 codis-server 保存。Redis 客户端仍可直连 proxy；新集群向应用提供 proxy 地址或由部署方提供的稳定服务入口。 |

本地参考仓库为 `~/work/etcd`。其工作分支是 `main`；服务端标签 `v3.7.4` 和模块标签 `client/v3.7.4` 均指向 `2e8a5c99e4982c8d4c521be15364ea4c5c68a489`。实现按标签阅读源码，通过 Go modules 引用客户端，不提交指向个人目录的 `replace`。客户端声明 `go 1.26.0`，首轮沿用 Codis 的 `go 1.26.1`、`toolchain go1.27.4` 做依赖解析与构建验证。[上游正式发布](https://github.com/etcd-io/etcd/releases/tag/v3.7.4) · [客户端模块声明](https://github.com/etcd-io/etcd/blob/v3.7.4/client/v3/go.mod)

## 2. 元数据与并发语义

沿用 `/codis3/{product}/`、现有 JSON 模型和领域 revision。不同 product 必须隔离，包括 `foo` 与 `foo-bar`。Redis 键、RDB/AOF、PITR 文件和迁移任务 workspace 维持各自生命周期。[Store 路径与模型操作](../pkg/models/store.go) · [架构与状态归属](architecture_zh.md)

| 对象 | 首次建群与后续写入 |
| --- | --- |
| `topom` | Dashboard 首次启动即竞争 product 锁。成功后发布 Dashboard 地址，供 FE、Admin、Proxy 发现。 |
| `slots/slot-XXXX`、`group/group-XXXX` | 空 product 有 1024 个逻辑 slot，初始均未分配，允许对应键缺失。创建 group、加入 Redis 实例并分配 slot 后，持久化组成员顺序和 slot 到 group 的映射。 |
| `proxy/proxy-{token}`、`sentinel` | Dashboard 在纳管 proxy 或配置 Sentinel 时写入相应记录。纳管记录独立于在线状态，proxy 模型不再生成 `jodis_path`。 |
| `acl`、`proxy_qps_limit` | 首次建群允许键缺失。配置后持久化 ACL 用户、密码哈希、规则、DB 绑定或 QPS 上限，保留各自的启用或限额语义、业务 revision 与更新时间。业务 revision 独立于 etcd MVCC revision。 |

**Store 行为。** 保留上层 `models.Store`，底层收窄到实际调用的操作。`Create` 用 `Txn` 比较 `Version(key)==0` 后写入；`Update` 为单键 upsert；普通 `Delete` 精确删键，键已缺失时视为达成目标；存在并发重建时不得无条件重放删除。`Read/List(must=false)` 对缺失返回空结果，`must=true` 对缺失返回可区分的错误。列表用带尾部斜杠的前缀查询，返回去重、排序的直接子路径，支持 FE 在 `/codis3` 枚举 product。空目录由前缀语义处理。超时、拒绝访问、失去 quorum 均返回错误，不能被翻译成空集群。删除只由旧驱动或 Jodis 使用的顺序节点、watch 等接口前，先核对调用方。[现有 Client 接口](../pkg/models/client.go) · [FE 动态发现](../cmd/fe/main.go)

### 写请求的三种结果

| 结果 | 调用方行为 |
| --- | --- |
| 收到提交成功响应 | 返回成功。并发测试通过提交 revision 或隔离键验证结果。 |
| 明确拒绝 | 参数校验失败、鉴权拒绝、事务比较返回 false 等，返回对应原因。锁冲突须能与连接失败区分。 |
| 提交结果未知 | 请求发出后遇到 deadline、连接断开或响应丢失时，保留 key、操作及预期所有者或 revision，用新的有界 context 做线性一致读。能确认目标状态就报告观察结果；无法确认时返回“结果未知”。禁止自动重放可能覆盖其他写入的 Put/Delete，或据此重复调用 Redis/Proxy 管理动作。 |

普通 KV 的回读只能证明当前值；锁和在线注册还须检查本实例身份。故障恢复前无法读到 quorum 时，绝不把缺少回读结果解释为锁空闲。测试须分别覆盖“提交前失败”和“已提交但应答丢失”。

### Dashboard 持久锁与人工恢复

锁键为 `/codis3/{product}/topom`，不附 lease。值保留现有 Topom JSON 和 `admin_addr`，新增 `owner_id`。每次启动生成随机 UUID，本进程重试沿用同一个 ID。同 product、同监听地址重启仍须产生不同 ID，原 Topom token 保留其原有用途。[Topom 模型](../pkg/models/topom.go) · [启动、后台任务与关闭](../pkg/topom/topom.go)

1. **获得所有权。** 用同一事务比较锁键版本为 0 并写入 Topom。成功时记下该键的 `create_revision`。若应答不确定，线性一致地读取锁；值为本实例时取回 revision 并继续，值为其他实例时报冲突。键缺失时只允许用原 owner ID 重试条件创建。未确认持锁期间不得开放管理写入或启动后台拓扑动作。
2. **正常释放。** 停止接收新的管理写入，等待进行中的管理动作和后台任务退出，再用事务比较锁键 `CreateRevision == 本次取得的 revision` 并删除。比较失败表示所有权已变更。取得锁后若初始化失败，同样按本次 revision 清理再失败退出。锁记录在一次持有期间不可被原地更新。
3. **异常恢复。** 进程异常退出时保留持久锁。运维先隔离原进程和自动拉起机制，再检查锁并显式提交观察到的 revision。Dashboard 的启动参数 `--remove-lock` 移除。Admin 新增和修改的命令如下，示例中的 revision 是操作者上一步实际读到的值。

```sh
codis-admin --etcd-config=etcd.toml --product=demo --show-lock
codis-admin --etcd-config=etcd.toml --product=demo --remove-lock --expected-revision=417 --confirm
```

查询输出 JSON，至少含 `product`、`admin_addr`、`owner_id` 和 `create_revision`。无锁时输出 `null` 并成功退出。删除须提供正整数 revision 和确认参数；只可按输入 revision 在事务中删该锁。首次调用遇到缺失或不匹配应以非零退出并报告原因；删除应答丢失后若回读发现锁已消失，可报告“目标锁已不存在”，不能声称本请求一定完成了删除。回读发现替代锁时一律保留。此流程依赖运维先隔离存活的旧实例，Redis 和 Proxy 不校验 etcd 锁。[Admin 现有入口](../cmd/admin/admin.go)

## 3. etcd 连接配置与组件入口

四个使用 coordinator 的程序共用 `--etcd=URL[,URL...]` 和 `--etcd-config=FILE`。后者读取独立 TOML 连接文件，四个程序按同一规则解析；各角色可使用各自凭据。下表中的新字段均属于该文件顶层。

| 字段 | 类型与默认值 | 规则 |
| --- | --- | --- |
| `endpoints` | 字符串数组；未给出时按下文优先级取值 | 一个或多个同集群的 HTTP(S) 客户端 URL。拒绝空项、嵌入凭据、文件路径、混用 HTTP/HTTPS。CLI 以逗号分隔，去除项间空白。 |
| `dial_timeout`、`request_timeout` | Go duration 字符串，均为 `"5s"` | 必须大于 0。分别用于客户端连接设置和单次 KV/Txn RPC；Dashboard 用实际抢锁 RPC 确认可写。lease 续约流由进程 context 管理。 |
| `username`、`password` | 字符串，默认空 | 同设或同空。含密码的生产文件仅授予运行账号读取权限。禁止通过 CLI、URL、日志、配置回显或错误文本暴露密码。 |
| `ca_file`、`cert_file`、`key_file` | 文件路径，默认空 | HTTPS 默认使用系统信任根或显式 CA 并校验主机名。客户端证书与私钥成对出现。HTTP 配 TLS 文件、文件不可读、证书无法解析均报错。相对路径按连接文件所在目录解析。 |

**优先级。** 地址按显式 `--etcd` → 连接文件 `endpoints` → Dashboard 的 `coordinator_addr` → `http://127.0.0.1:2379` 依次取值，其余连接字段取连接文件或表中默认值。显式空地址、无效文件、旧后端字段在建立连接前报错，禁止静默回退。Dashboard 的默认 `coordinator_name="etcd"`。旧 `coordinator_auth` 和旧后端选择值报错，凭据统一用连接文件。发布的示例分别给出本机 HTTP 和生产 HTTPS/mTLS 两种配置。

| 程序 | 何时选用 etcd |
| --- | --- |
| Dashboard | 始终使用 etcd，支持两个共享参数。常规启动只竞争该 product 的锁并加载拓扑。检查 product 前缀为空属于首次部署步骤，不能写成 Dashboard 的通用启动条件。 |
| FE | 保留 `--listen`。静态 `--dashboard-list=FILE` 与动态 etcd 来源互斥。显式给出任一共享参数即选动态来源，二者同用算一种来源。 |
| Admin | 直接访问元数据的 `--dashboard-list`、dump、restore、查锁和解锁要求显式给出至少一个共享参数。HTTP 管理命令保留原用法。dump/restore 保留原字段范围；移除读旧 ZK 树的 `--config-dump -1`。已有纯文件 `--config-convert` 不连接后端。 |
| Proxy | `--dashboard`、`--etcd`、`--fillslots` 三种自动上线入口互斥，也允许全部省略后由 Dashboard 手动纳管。`--etcd-config` 只提供连接选项；因此 `--dashboard` 与 `--etcd-config` 可以一起用于“由 Dashboard 上线，同时向 etcd 注册”。选择 etcd 自动上线时显式给 `--etcd`。 |
| HA、codis-server、codis-shake | 保留现有入口。HA 经 Dashboard API 管理，Redis 和 Shake 的数据路径无需初始化 etcd 客户端。 |

下面是生产连接文件的最小 mTLS 示例。各角色按部署权限使用自己的证书。Dashboard 可读写本 product 的持久元数据，注册型 Proxy 写自身在线键，`all` 读取本 product 的在线前缀。FE 动态扫描跨 product 的键名并读取 Dashboard 地址，其账号只授予发现所需的读取权限；需严格隔离不同 product 时可使用静态 FE。Admin 按查询或变更操作分别授权。

```toml
endpoints = ["https://etcd-a:2379", "https://etcd-b:2379", "https://etcd-c:2379"]
ca_file = "tls/ca.pem"
cert_file = "tls/client.pem"
key_file = "tls/client-key.pem"
```

四个组件使用同一种连接文件格式。示例中的 proxy 配置已开启第 4 节的在线注册。

```sh
codis-dashboard --config=dashboard.toml --etcd-config=etcd.toml
codis-fe --listen=127.0.0.1:9090 --etcd-config=etcd.toml
codis-admin --dashboard-list --etcd-config=etcd.toml
codis-proxy --config=proxy.toml --dashboard=127.0.0.1:18080 --etcd-config=etcd.toml
```

删除生产工厂、默认配置、帮助与部署入口里的 ZooKeeper、Consul 和 filesystem 选择，以及 `jodis_*` 配置和两套旧注册路径。对显式旧 CLI/TOML 参数报出选项名和旧线 SHA，错误文本不带参数值；TOML 即使旧字段值为空也能检测。保留静态 FE 地址文件和 Proxy slots 文件，它们各自提供启动输入。[共享 CLI](../cmd/internal/coordinator/args.go) · [FE](../cmd/fe/args.go) · [Admin](../cmd/admin/args.go) · [Proxy](../cmd/proxy/args.go)

## 4. Proxy 在线注册与 CLUSTER NODES

Proxy TOML 新增 `etcd_register_online`（bool，默认 `false`）。保留 `cluster_nodes_compat` 的 `disabled/self/all` 及现有 `cluster_nodes_refresh_period`。首版在线 lease 请求 TTL 固定 30 秒，不额外暴露调优参数。发布在线记录的前提是 Proxy 已完成上线。[Proxy 生命周期](../pkg/proxy/proxy.go) · [现有节点过滤和输出](../pkg/proxy/cluster_nodes.go)

| 模式 | 注册关闭 | 注册开启 |
| --- | --- | --- |
| `disabled` | 沿用禁用响应，无发现连接 | 发布自身，其余 proxy 可发现它；本机仍禁用此命令 |
| `self` | 只返回自身，无发现连接 | 发布自身，本机仍只返回自身 |
| `all` | 配置校验失败，提示启用注册 | 发布自身，轮询同 product 的在线列表并生成响应 |

- **记录与所有权。** 注册键为 `/codis3/{product}/online-proxies/{token}`，JSON 至少含 `token` 和 `addr`（客户端访问的 Proxy 地址）。每个在线实例申请独立 lease，重试同一轮注册时复用该 lease，采用版本为 0 的条件事务将键与 lease 绑定。应答不确定时核对键的 lease ID。相同 token 已由其他 lease 持有时，不覆盖、不认领；等待其正常退出或过期后再竞争。
- **续约与关闭。** 注册失败在后台有界退避重试，业务上线不等待注册成功；日志记录未注册、冲突和恢复。续约失效后重新确认所有权并注册。关闭先取消并等待本实例注册循环，再有界撤销本实例 lease；不按 token 裸删键。崩溃依靠服务端 lease 到期。
- **读取与故障。** `all` 每个刷新周期对该 product 在线前缀做一次一致范围读取；跳过坏记录，复用现有自身补全、去重、排序、节点 ID 和伪 slot 分配。首次读失败时只提供自身，此后失败保留最后一次成功快照；成功读到空列表则只返回自身。在 quorum 健康、客户端可达时，服务端删除注册键后，最迟在下一个成功刷新完成时移除该 peer。该内部记录不提供 Jodis 客户端协议，普通 Redis 请求继续读取本地路由。

## 5. 新建集群与重启

1. **首次部署预检。** 选定合法 product，对带尾斜杠的 product 前缀做一次线性一致前缀查询。空结果允许继续，非空停止本次新建，读失败直接报错。可在部署脚本中用 etcdctl 实现。该检查只供部署人员确认所选命名空间；并发 Dashboard 始终由第 2 节的事务排他。
2. **启动控制面。** Dashboard 抢锁成功后，FE 和 Admin 能定位其地址。空 product 的 1024 个逻辑 slot 均未分配，可以尚无对应 KV。另启一个相同 product 的 Dashboard 必须争锁失败，不能处理管理动作。
3. **建群联通。** 建立两个 group，各加入一个 codis-server；通过 Dashboard 分配全部 1024 个 slot，纳管并上线 proxy。用映射到两个不同 group 的键分别执行 SET/GET，核对读回值及实际后端。[现有管理命令](../cmd/admin/args.go) · [快速启动示例](tutorial_zh.md)
4. **同版本重启。** 正常停止 Dashboard 后沿用相同配置启动。已有拓扑且无锁时恢复 group、slot、proxy；已启用的 Sentinel、ACL（含 DB 绑定）和 QPS 目标值及领域 revision 可读回。异常停机且锁存在时按第 2 节人工恢复。整个流程只操作指定 product。

## 6. 实施顺序

| 顺序 | 修改入口与产物 | 该步完成的判断 |
| --- | --- | --- |
| 0 · P0 依赖调研 | 先执行第 0 节的隔离实验，交付模块差异、逐入口影响、构建成本和主要运行行为。 | A0 的前置调研部分通过后开始正式接入。 |
| 1 · 接入客户端与 Store | 在 `pkg/models` 接入固定版本的 client/v3，实现现有路径的 KV、前缀 List、错误语义与共享连接配置。替换 `pkg/topom` 等测试对 fsclient 的依赖，纯单测使用测试专用替身，真实后端用明确的集成测试入口。 | Store 单测和真实单成员 etcd 集成通过。当前 Go modules 约束下可构建，依赖变化由实际验证命令驱动，避免全量 `go mod tidy`。 |
| 2 · Dashboard 与运维 | 更新 Store 锁 API、Topom 启停、Admin 查锁/解锁与共享 CLI，接入 FE 动态发现、Admin 元数据命令和 Proxy 自动上线。 | 跑通第 7 节 A1–A4 的相关入口；API、CLI 与配置回显覆盖脱敏。 |
| 3 · 在线发现和退役 | 替换 `pkg/proxy/jodis.go` 与 `cluster_nodes.go` 的发现源；清除旧驱动和生产依赖、路径、配置、stats/model 中专属 Jodis 字段及对应 UI 引用。收窄不再有调用方的接口。 | A5 通过；默认配置启动普通 proxy 时无需连接 etcd。运行时依赖图不存在旧后端驱动。 |
| 4 · 部署与文档 | 从程序的 `--default-config` 再生两份默认 TOML；Dashboard 用 `go run ./cmd/dashboard --default-config`。更新教程、架构、能力边界、`example/`、`deploy/` 模板及渲染样例、Kubernetes、Ansible、验证脚本、发布说明与索引。Dockerfile 和 Docker 脚本遵守仓库单独授权要求；仍依赖旧服务的 Docker 入口在发布说明中列为未适配。 | 新部署示例只需 etcd、Codis 组件和应用连接入口。帮助列出新版选项，旧线使用者可按第 1 节的 SHA 重新编译。历史验证记录保留原环境与 SHA。 |
| 5 · 构建与签收 | 先跑受影响包，再运行 `make gotest`、`make` 和 `go list -deps ./cmd/... ./pkg/...`。给真实 etcd 集成测试记录可复现命令、版本及运行条件。 | 分别报告代码/本地回归、单成员集成和三成员 Linux 真机结论。无 Linux 环境时可交付实现和已有证据，真机门槛列为待验。 |

## 7. 交付验收表

| 编号 | 可复现场景 | 必须观察到的结果 |
| --- | --- | --- |
| A0 依赖调研 | 对比旧线、隔离候选与最终交付的模块图和六入口包图。 | 正式接入前有第 0 节要求的候选报告；交付时核对最终选版、产物和旧驱动退役结果，记录与候选的差异。 |
| A1 配置与入口 | 四个组件接同一启用认证的 etcd。分别覆盖 CLI 覆盖文件、错误凭据、缺证书、旧参数、FE 静态模式及三种 Proxy 自动上线入口。 | Dashboard 可建群、FE 可发现、Admin 可查询、Proxy 可注册。无效配置在相应入口报错，拒绝访问可辨认，错误与日志中没有测试密码。 |
| A2 建群与隔离 | 按第 5 节从空 product 建群；在同一 etcd 中预置另一个名称有公共前缀的 product，再正常重启 Dashboard。 | 首次预检对非空前缀报错；正常重启成功。两组读写、1024 个 slot 和启用的 ACL/QPS/Sentinel 配置符合预期，另一 product 的测试数据未变化。 |
| A3 锁与迟到请求 | 两 Dashboard 同时抢锁；进程 A 异常退出后按已观察的 revision 解锁，B 在同地址启动；再提交针对 A 的旧解锁请求。 | 同时至多一个实例持锁，A、B 的 owner ID 与 create revision 均不同。旧请求失败且 B 的记录完整。缺 revision、负 revision、缺确认参数均失败。 |
| A4 结果未知 | 测试替身在真实存储调用外围分别模拟提交前失败、锁已提交但响应丢失、删除已提交但响应丢失，以及回读也不可用。 | 抢锁仅在核实本实例 owner 后继续；删除只认观察过的 revision。回读不可用时返回未知；迟到的重试不会覆盖其他所有者，也不会重复触发拓扑动作。 |
| A5 混合 Proxy | 一台 `all+注册`、一台 `self+注册`，另测 `disabled+注册` 和注册关闭。覆盖正常退出、崩溃、同 token 重启及刷新失败。 | `all` 能看到所有已注册在线 peer；`self` 仅返回自身。记录服务端授予的 TTL、键消失时刻和下一次刷新结果；键消失后 peer 按第 4 节时限收敛。同 token 的旧进程关闭无法撤销新进程的 lease。 |
| A6 Linux 发布门槛 | 三成员 etcd 3.7.x：切换 leader、隔离一成员、失去多数派、恢复多数派。经已上线 proxy 连续执行已分配 slot 的读写，同时观察控制面。 | quorum 恢复后控制面可读写和重新发现在线节点。期间无空拓扑误判、无错误接管；Redis 后端健康且拓扑稳定时数据面仍可服务。管理请求按第 2 节三种结果归类。 |
| A7 构建与清理 | clean checkout 验证默认配置、帮助、构建、包测试和示例；检查生产 Go 依赖与活动部署入口。 | 旧 SDK、filesystem coordinator 和 Jodis 注册路径从新版生产入口退出。历史文档、拒绝旧参数的诊断与测试可保留旧名称。 |

交付说明列出 Codis SHA、etcd/server 与 client/v3 版本、实际命令及结果，并逐项标示 A0–A7 的通过、失败或待验。Linux 发布签收按仓库约定登记环境、结果与证据位置。[构建与发布指南](build_and_release_zh.md) · [真机验证索引](validation/README.md)
