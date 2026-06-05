# Zookeeper Go SDK 迁移说明

本文只面向直接 import Codis Go 包的开发者。通过 `codis-dashboard`、`codis-proxy`、`codis-admin`、`codis-fe` 或配置文件使用 Zookeeper coordinator 的用户，不需要因为本次 SDK 迁移修改运行期配置。

## 变化

Codis 的 Zookeeper Go SDK 从旧 module path:

```go
github.com/samuel/go-zookeeper/zk
```

迁移到维护中的 module path:

```go
github.com/go-zookeeper/zk
```

目标版本是 `github.com/go-zookeeper/zk v1.0.4`。

## 兼容性影响

`pkg/models/zk.Client.Do` 是 exported API，签名里暴露了第三方 SDK 的 `*zk.Conn`。Go 的命名类型身份绑定 import path，因此这次迁移会让该方法的回调参数类型从:

```go
func(conn *github.com/samuel/go-zookeeper/zk.Conn) error
```

变为:

```go
func(conn *github.com/go-zookeeper/zk.Conn) error
```

这对直接调用 `Client.Do` 且在回调签名里使用旧 SDK path 的外部 Go 代码是源码级 breaking change。

Codis 不保留旧 `Client.Do` 回调签名。原因是保留旧签名需要继续暴露旧 SDK、引入 unsafe 类型转换，或维护新旧两套 Zookeeper 连接；这些做法会让依赖迁移不再是清晰的单 SDK 边界。

## 迁移方式

如果没有直接 import `github.com/CodisLabs/codis/pkg/models/zk` 或没有调用 `Client.Do`，通常不需要修改代码。

如果直接调用了 `Client.Do`，把回调使用的 SDK import path 改为新 path:

```diff
- import oldzk "github.com/samuel/go-zookeeper/zk"
+ import gozk "github.com/go-zookeeper/zk"

- err := client.Do(func(conn *oldzk.Conn) error {
+ err := client.Do(func(conn *gozk.Conn) error {
    // raw zookeeper operation
    return nil
  })
```

如果代码只通过 `models.Client`、`models.NewClient("zk"|"zookeeper", ...)`、CLI 参数或配置文件使用 Zookeeper coordinator，外部语义不变。

## 验证

迁移后建议执行:

```bash
go test ./cmd/... ./pkg/...
```
