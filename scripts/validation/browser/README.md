# Codis FE 浏览器验证（Playwright）

针对 Codis dashboard FE（`cmd/fe`）的浏览器级回归测试。套件是纯 JavaScript
（CommonJS），没有构建步骤，只依赖 `@playwright/test`。它驱动由
`scripts/validation/env/setup_cluster.py` 拉起的真实 `codis-fe` +
`codis-dashboard` + Redis 集群。

验证环境：Playwright **1.65.1**、Chromium **159.0.8154.6**
（Playwright Chromium build 1255）。

## 目录结构

```
scripts/validation/browser/
├── README.md
├── package.json          # private; @playwright/test pinned to 1.65.1
├── playwright.config.js  # serial chromium project, list + json reporter
└── tests/
    ├── helpers.js          # context/env resolution, FE navigation, dashboard API, page errors
    ├── fe-smoke.spec.js    # Overview / Security / RDB Analysis / PITR pages
    ├── acl.spec.js         # add + delete an ACL user, API cross-check
    ├── qps-limit.spec.js   # proxy QPS limit 0 -> 3 -> 0
    ├── pitr.spec.js        # invalid ts, successful 9-step job, detail drawer, delete
    └── rdb-analysis.spec.js# upload analysis + remote-fetch analysis
```

## 前置条件

* Node.js 20+ 与 npm（Playwright 1.65.1 要求 Node 20+）。
* 一套带 FE 的运行中验证集群，例如：

  ```bash
  python3 scripts/validation/env/setup_cluster.py \
      --groups 2 --replicas 1 --proxies 2 --fe
  ```

  该脚本把 `cluster.json` 路径打印到 stderr。它使用仓库
  `bin/codis-*`、`bin/redis-cli` 和 `bin/redis-check-aof` 二进制，默认开启
  AOF/PITR 以及 RDB HTTP 导出/远程拉取。
* `bin/redis-cli`（或 `REDIS_CLI`）必须对运行测试的用户可执行。

## 安装

```bash
cd scripts/validation/browser
npm install
npx playwright install chromium
```

`npm install` 会生成 `package-lock.json`；请保留它，以保证固定版本的
Playwright 与浏览器构建可复现。`node_modules/`、`test-results/`
和 `playwright-report/` 已被 `scripts/validation/.gitignore` 忽略。

## 运行

推荐用法（从 cluster 文件推导 FE URL、product、dashboard 地址、xauth、RDB
文件和 PITR master）：

```bash
CODIS_CLUSTER=/path/to/cluster.json npx playwright test
```

单个 spec（每个 spec 都可独立运行并自行复位状态）：

```bash
CODIS_CLUSTER=/path/to/cluster.json npx playwright test tests/fe-smoke.spec.js
CODIS_CLUSTER=/path/to/cluster.json npx playwright test tests/acl.spec.js
CODIS_CLUSTER=/path/to/cluster.json npx playwright test tests/qps-limit.spec.js
CODIS_CLUSTER=/path/to/cluster.json npx playwright test tests/pitr.spec.js
CODIS_CLUSTER=/path/to/cluster.json npx playwright test tests/rdb-analysis.spec.js
```

npm 快捷命令：`npm test`、`npm run test:smoke`、`npm run test:acl`、
`npm run test:qps`、`npm run test:pitr`、`npm run test:rdb`。

JSON 报告写入 `test-results/results.json`；`list` reporter 把每个测试的
结果打印到终端。必须 `workers: 1`：所有 spec 共用同一个在线集群，并会改动
ACL/QPS/PITR/RDB 状态。

## 环境变量

`tests/helpers.js` 是读取这些变量的唯一位置。

| 变量 | 用途 | 默认值 |
| --- | --- | --- |
| `CODIS_CLUSTER` | `setup_cluster.py` 生成的 `cluster.json` 路径。推荐使用：可推导下方全部取值。 | 未设置 |
| `CODIS_FE_URL` | FE 基础 URL，例如 `http://127.0.0.1:51563`。 | `cluster.fe.url`，否则 `http://127.0.0.1:8080` |
| `CODIS_PRODUCT` | Codis product 名。 | `cluster.product`，否则 `codis-validation` |
| `CODIS_DASHBOARD_ADDR` | 直连 API 使用的 dashboard `host:port`。 | `cluster.dashboard.addr`，否则 `127.0.0.1:18080` |
| `CODIS_XAUTH` | Dashboard xauth token。 | product 匹配时为 `cluster.xauth`，否则 `sha256("Codis-XAuth-[<product>]")` 前 32 位 hex |
| `CODIS_RDB_FILE` | upload 用例使用的本地 RDB 文件。相对路径按当前工作目录解析。 | 第一个 Group Master 的 `<dir>/<dbfilename>`；文件不存在时执行一次 `redis-cli SAVE` |
| `CODIS_PITR_MASTER` | PITR 的目标 `host:port`。 | `cluster.json` 中第一个 `aof: true` 的 Group Master |
| `REDIS_CLI` | `redis-cli` 二进制。 | `<repo>/bin/redis-cli` |

也接受 `CODIS_REPO` 作为仓库根覆盖；未设置时仓库取自 `cluster.json` 或
`helpers.js` 所在位置。`loadContext()` 还会暴露 `repo` 和 `rdbMaster`
（持有默认 `<dir>/<dbfilename>` RDB 文件的 Group Master），供 RDB spec 使用。

`CODIS_FE_URL` 是 `playwright.config.js` 中 `use.baseURL` 唯一使用的值。
helpers 始终用 `CODIS_CLUSTER`／上述变量拼绝对 URL，因此只给 cluster 也能
运行，无需改配置。

## FE 路由：`forward` 与 URL hash

已对照 `cmd/fe/assets/dashboard-fe.js` 与 `shell.js` 核实：

* 选中的 product/页面编码在 hash 中：`#/<product>/<page>`
  （`#/codis-validation/security`、`#/codis-validation/analysis`、
  `#/codis-validation/recovery`）。`Router.parse()` 只读
  `window.location.hash`。
* `?forward=<product>` **不**写入 localStorage，也不用于在页面上选择
  product。它由 `Api` 服务附加到每个 dashboard API 请求
  （`/api/topom/...?forward=<product>`），供 `codis-fe` 反向代理选择
  dashboard。
* 因此 `openFE(page, hashPath)` 导航到
  `${baseURL}/?forward=<product>#/<product><hashPath>`。查询串对 SPA 无害，
  只是记录代理目标；真正选择集群/页面的是 hash。

FE 选择器使用稳定 id 和按钮文本（没有 `data-testid` 属性）：
`#security-tab-acl`、`#security-panel-acl`、`#acl-user-name`、
`#acl-user-password`、`#acl-user-rules`、`#acl-more-settings`、`#acl-user-db`、
`#security-tab-qps`、`#qps-limit-input`、`#qps-limit-hint`、`#qps-limit-error`、
`#rdb-source-tab-upload`、`#rdb-source-tab-remote`、`#rdb-upload-file`、
`#rdb-remote-options`、`#rdb-remote-server`、`#analysis-result-panel-summary`、
`#pitr-master-options`、`#pitr-server-addr`、`#pitr-truncate-ts`，以及
`codis-data-table` 内的行操作 `详情 / 取消 / 删除` 和确认对话框
`.confirm-dialog .confirm-accept`。

## 每个 spec 做什么

### `fe-smoke.spec.js`

按顺序打开 Overview、Security & rate limit、RDB Analysis 和 PITR 页面。
每个测试断言预期 hash、页面容器和该页面的就绪标记，然后断言
`capturePageErrors()` 没有看到 `console.error`、没有 `pageerror`，也没有
状态码 `>= 400` 的 HTTP 响应（favicon 响应忽略）。

### `acl.spec.js`

* `beforeAll` 通过 dashboard API 删除可能残留的 `browser-test` 用户，并记录
  基线 ACL（`enabled`、用户名单）。
* 通过 FE 表单创建 `browser-test` / `Browser-Pass-1` / `+@read ~*`，展开
  `更多配置` 以证明 `#acl-user-db` 可达，保存，断言表格行，并通过 `feApi`
  （FE 反向代理）交叉核对 `GET /api/topom/acl/<xauth>`。期望
  `password_count = 1`、规则 `["+@read", "~*"]`。
* 通过 FE 确认对话框（`确认删除`）删除该行，断言表格和 API 都不再包含该
  用户，且 ACL 的 enabled 标志与其余用户不变。
* `afterAll` 再执行一次尽力而为的清理。全新集群上的最终状态是
  `enabled=false, users=[]`（协议上为 `null`）。

不需要"先启用 ACL"：FE 的 `saveUser()` 提交完整用户列表并保留当前
`enabled` 值（全新集群为 `false`），dashboard 接受这种提交。

### `qps-limit.spec.js`

`beforeAll`/`afterAll` 强制 `limit=0`，保证 spec 可重复。测试步骤：

1. 打开 Security → `Proxy 限流`，断言当前限额指标为 `0`。
2. 打开 `编辑限流`，检查 `#qps-limit-hint`，输入 `3`，用 `保存并同步` 保存。
3. 轮询 `GET /api/topom/proxy/qps-limit/<xauth>` 直到
   `{limit: 3, enabled: true, sync_status: "ready"}`，并检查页面显示
   `已启用` / `已同步`。
4. 用 `0` 重复，断言 `{limit: 0, enabled: false}` 且页面显示
   `未启用`。即使断言失败，`afterAll` 也会复位为 `0`。

### `pitr.spec.js`

`test.setTimeout(240s)` 为 180s 的 job 轮询留出余量。spec 要求 AOF/PITR
（`cluster.features.pitr`）；否则带明确原因 skip。

* **非法时间戳**：选择 master，输入 `0`，点击 `创建闪回任务`，断言客户端
  错误包含 `大于 0`、没有发出 `PUT /api/topom/pitr/create/` 请求、也没有
  弹出确认对话框。
* **成功任务**：写入 `pitr:browser:before`，等 1.6s，写入
  `pitr:browser:marker`，记录 `truncate_ts = floor(now/1000)`，再等 1.6s，
  写入 `pitr:browser:after`，再等 1.6s。通过 FE 创建任务（选择 master、
  `确认 SHUTDOWN 并创建`），轮询 API 到终态，期望 `succeeded` 且恰好 9 个
  `done` 步骤，用 `redis-cli` 检查 `before` 存在、`after` 已消失，打开
  `详情` 校验 9 行 done 步骤，然后用 `删除` → `确认删除任务` 删除任务，并
  断言列表与 API 都为空。
* **不存在的 server**：手工输入 `127.0.0.1:1`（FE 允许手填
  `server_addr`），确认，等待 `failed`，断言表格和详情抽屉里的错误都包含
  `doesn't exist`，然后删除。
* `afterAll` 取消/移除该 spec 创建的所有 job id，并删除测试 key。PITR 任务
  会向真实 master 发送 `SHUTDOWN NOSAVE`；只能对配置了
  `pitr_restart_command` 的一次性验证集群运行（`setup_cluster.py` 会生成
  该配置）。

### `rdb-analysis.spec.js`

`test.setTimeout(180s)`。两个独立测试，各自使用全新页面（避免 RDB
workspace 偏好跨用例泄漏）：

* **Upload**：打开 `新建分析`，把 `#rdb-upload-file` 设为 `ctx.rdbFile`，
  用 `开始分析` 保存，捕获 `POST /api/topom/rdb-analysis/upload/` 响应的
  id，轮询到 `status=done`，断言 `source` 以 `upload:` 开头、
  `objects_read` / `file_size` 均 `> 0`（API 与 Summary 表里的
  `objects_read` / `file_size` 行都要），然后用 `清除任务` → `确认清除`
  清除任务。
* **Remote fetch**：切到 `Remote Server` 标签页，等待集群 server 列表，
  在 `#rdb-remote-options` 选择第一个 master，断言 `#rdb-remote-server`
  已填充，启动分析，断言 `source` 以 `remote-http:` 开头，检查相同的
  Summary 正值，然后清除任务。若 `cluster.features.rdb_export` 为 false，
  带原因 skip；若 dashboard 返回 `800` + `remote fetch is disabled`，也带
  dashboard 的 `Cause` skip。
* `afterAll` 通过
  `PUT /api/topom/rdb-analysis/remove/<xauth>/<id>` 兜底移除所有跟踪中的
  job（也覆盖 running 状态）。
* 两个用例在 `清除任务` 成功后都**不刷新页面**，继续断言「新建分析」按钮在
  默认 `expect` 超时内恢复 enabled，并能再次打开「新建 RDB 分析」抽屉
  （回归 `actionPending` 未释放导致按钮卡 disabled 的缺陷）。

## 与 Python 验证脚本的关系

浏览器套件是 API 级 Python 套件的 FE 层补充：

* `scripts/validation/env/setup_cluster.py` 创建集群和 `cluster.json`；
  把该路径作为 `CODIS_CLUSTER` 传给每条命令。
* `scripts/validation/env/teardown_cluster.py --cluster <cluster.json>`
  在浏览器运行结束后停止集群（包括 PITR 重启过的 server，其 pid 会从
  pidfile 重新读取）。
* `scripts/validation/{acl,qps,pitr,rdb}/run_*.py` 驱动同一个 dashboard、改动
  同一份状态（ACL 用户、QPS 限额、PITR 任务、RDB 任务）。它们都会自行清理，
  但**不要**与浏览器套件并发运行：`workers: 1` 只在 Playwright 内部串行，
  管不到其他进程。在同一集群上按顺序跑 Python 套件与浏览器套件，或各给
  一套集群。
* `fe-smoke.spec.js` 是只读的；ACL/QPS/PITR/RDB spec 可重复执行，因为
  `afterAll` 会复位 ACL（移除 `browser-test`）、QPS（`0`）、PITR 任务
  （移除）和 RDB 任务（移除）。
* PITR spec 的成功用例会真实重启目标 Group Master（这是产品行为）。如果
  还排了 Python 的 `run_pitr.py` 或 `run_qps_limit.py`，顺序执行，并让每个
  套件先完成自己的复位阶段。

## 已知限制 / 产品观察

* **RDB 清除后同一页面里的「新建分析」保持可用（该缺陷已修复）。**
  `清除任务` 成功后 `runJobAction()` 的 `.finally` 曾用含 `activeJobId` 的
  `mutationIsCurrent()` 作守卫，而 remove 分支已先清空 `activeJobId`，
  `actionPending` 卡在 true，头部按钮一直 disabled 到刷新页面。现在
  `.finally` 只校验本次 mutation 仍是最新一次（`mutationOwnsSequence()`）；
  回归由 `rdb-analysis.spec.js` 覆盖：清除后不刷新页面，断言「新建分析」
  按钮恢复 enabled 并能再次打开抽屉。
* **PITR 天生具有破坏性。** 它发送 `SHUTDOWN NOSAVE`、截断 AOF，并依赖外部
  重启钩子。只能用于一次性集群。成功用例要求 `setup_cluster.py` 的默认
  配置（`--aof`、生成的 `pitr_restart_command`）。
* **RDB remote fetch 依赖 dashboard 配置。** `setup_cluster.py` 默认开启
  （`--rdb-export`）；用 `--no-rdb-export` 启动的集群会让 remote 测试 skip。
* **RDB 文件生成与种子 key。** `setup_cluster.py` 写入 `save ""`，所以全新
  集群没有 RDB 快照也没有 key。`ctx.rdbFile` 是惰性解析的（只读 spec 不碰
  Redis）；第一个访问它的 RDB 测试会在默认 RDB 文件不存在时写入一次性
  `codis-browser-validation:rdb-seed` key 并执行 `redis-cli SAVE`，然后
  删除在线 key。upload 测试还会用两个
  `codis-browser-validation:rdb-seed:*` key 刷新默认 RDB，使空集群上的
  `objects_read > 0` 断言有意义。remote 测试会让这些 key 保持在线直到
  `afterAll`（这样服务端 RDB 导出有对象），然后删除。把 `CODIS_RDB_FILE`
  指向带数据的 fixture，可让 upload 用例不依赖在线 master；remote-fetch
  用例仍然需要在线 key（因此自己种两个 key）。
* **ACL 状态。** 全新集群是 `enabled=false` 且没有用户。spec 会保持这个状态；
  如果对已有 ACL 用户的集群运行，它会保留这些用户的名称/规则，只移除
  `browser-test`。
* **QPS 状态。** QPS spec 会故意把集群级限额设为 `3` 几秒钟。不要在承载真实
  流量的集群上运行而不考虑这段临时限流。
* **选择器基于文本/id。** FE 没有 `data-testid`；测试匹配可见中文标签
  （`新增用户`、`保存用户`、`编辑限流`、`保存并同步`、`开始分析`、
  `清除任务`、`新建闪回任务`、`创建闪回任务`、`详情`、`删除` 等）。改 UI
  文案需要同步修改对应 spec。
* **超时划分。** 配置默认 90s；`pitr.spec.js` 和 `rdb-analysis.spec.js`
  在本地覆盖（`240s` / `180s`），因为它们文档化的 job 轮询预算可能超过
  90s。
* **错误状态 800。** Codis dashboard 用 HTTP `800` + JSON `{"Cause": "..."}`
  表示应用错误。`helpers.api()` 把它作为 `{status: 800, data: {Cause}}`
  返回而不抛异常；spec 在要求成功处断言 `200`，在可选功能处检查 `Cause`。
* **不重试。** `retries: 0` 是有意的：重试会在同一集群上重新执行破坏性的
  PITR/RDB/ACL 步骤，而没有明确的清理边界。
