# Codis Console 前端

本目录是 `codis-fe` 的完整静态前端，沿用 AngularJS 1.8、Highcharts 和现有 Go 反向代理，无额外前端构建步骤。业务页面通过 `Api` / `ClusterStore` 访问数据；只有显式添加 `?mock=1` 时才启用 `mock-api.js`，不会请求真实 Dashboard。

## 部署到真实集群

在仓库根目录构建并复制静态资源：

```bash
make codis-fe
```

沿用原有的 Dashboard 列表或 coordinator 配置启动，例如：

```bash
./bin/codis-fe \
  --dashboard-list=/path/to/dashboard-list.json \
  --listen=0.0.0.0:18080
```

也可将 `--dashboard-list` 替换为现有的 `--zookeeper`、`--etcd`、`--filesystem` 或 `--consul` 配置。打开 `http://<host>:18080/`。

开发时可直接使用源码目录，避免反复复制 assets：

```bash
go build -o /tmp/codis-fe ./cmd/fe
/tmp/codis-fe \
  --assets-dir="$PWD/cmd/fe/assets" \
  --dashboard-list=/path/to/dashboard-list.json \
  --listen=127.0.0.1:18080
```

## 内置 Mock 预览

无需 Dashboard 即可检查所有页面和交互：

```bash
cd cmd/fe/assets
python3 -m http.server 8765 --bind 127.0.0.1
```

访问：

```text
http://127.0.0.1:8765/?mock=1#/codis-demo/overview
```

可在地址中追加 `scenario` 参数检查异常状态，例如 `?mock=1&scenario=partial#/codis-demo/overview`：

| scenario | 用途 |
| --- | --- |
| `normal`（默认） | 正常、告警、异常资源及完整业务数据 |
| `empty` | 无资源空状态 |
| `error` | 首次加载和请求失败 |
| `forbidden` | 403 无权限状态 |
| `partial` | 部分指标缺失 |
| `submit-error` | 写入请求失败；表单保留输入，Rebalance 预览仍可用 |

Mock 包含 `codis-demo`、`codis-order`、`codis-cache` 三个产品，以及 Proxy、Group、Sentinel、Slots、ACL、QPS Limit、RDB Analysis 和 PITR 页面。新增、编辑、确认和任务轮询均产生可观察结果；Mock 变更只保存在当前页面运行态，刷新浏览器后重置。

## 状态与监控数据

- 筛选、排序、分页、侧栏和监控布局等偏好按产品保存到 `localStorage`；认证信息不会写入偏好或操作记录。
- 抽屉详情和编辑不会改变原列表的筛选、分页或滚动上下文。
- 真实模式的趋势图只使用当前浏览器会话从 Dashboard 采样得到的数据，不伪造历史监控；Sentinel 没有对应历史序列时显示空状态。
- 页面切换和组件销毁时会清理定时器与图表实例，并忽略迟到请求；容器尺寸变化后图表自动调整。

## 代码结构

- `dashboard-fe.js`：格式化、偏好、路由、真实 API 和集群状态。
- `shell.js`：顶部导航、可折叠侧栏与工作区。
- `ui.js`：指标卡、状态标签、筛选、表格、图表、抽屉、确认、Toast 和空状态。
- `css/main.css`：集中设计变量、组件外观、响应式和减少动效适配。
- `overview.js`、`resources.js`、`resource-detail.js`、`slots.js`、`acl.js`、`qps-limit.js`、`rdb-analysis.js`、`pitr.js`：业务页面。
- `mock-api.js`：可替换的 Mock 数据层。
