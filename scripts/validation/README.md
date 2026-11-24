# Codis 真机验证脚本索引

`scripts/validation/` 存放 Codis 真机验证的可执行部分：环境采集、单机验证集群的搭建与清理、各能力场景脚本、Playwright 浏览器用例。它们与 [真机验证进度索引](../../doc/validation/README.md) 配套使用：脚本负责跑场景、留证据，结论和遗留项仍写回 `doc/validation/`。

约束：纯 Python 标准库、Python 3.10 兼容，Linux 真机（Ubuntu 22.04）与本地 macOS 都能直接运行；不引入 pytest 等额外依赖，不修改被测代码。

## 用途与边界

- 覆盖：Redis 8 功能 gate、依赖与供应链安全扫描、Codis ACL、Proxy QPS 限流、PITR 数据闪回、RDB Analysis、FE 浏览器冒烟。
- 不覆盖：性能压测、数据迁移控制台、codis-ha、Consul/ZooKeeper coordinator、Docker 部署（仅功能 gate 的 `--docker` 可选）、多平台发布。完整清单见[限制](#限制)。
- 脚本只做单机布局：dashboard、fe、proxy、codis-server 全部同机，coordinator 用 filesystem。ENV-01 那种双机布局没有脚本化。
- 证据是本地 JSON 文件，默认落在 `.runtime/`（已 gitignore）；入库的只有文档，以及文档里引用的证据路径。

## 目录结构

```text
scripts/validation/
├── README.md                    # 本文件：脚本总索引
├── vlib.py                      # 共享库（进程/RESP/HTTP/证据/配置生成），不直接运行
├── env/
│   ├── collect_env.py           # 采集 8 类环境字段 -> env-<run-id>.json
│   ├── setup_cluster.py         # 拉起单机验证集群 -> <workdir>/cluster.json
│   └── teardown_cluster.py      # 停进程（pidfile 兜底）、等端口释放、删 workdir -> cleanup.json
├── redis8/
│   └── run_functional_gate.py   # build-all / gotest / Redis 8 Codis Tcl / e2e 功能 gate
├── security/
│   └── run_dependency_audit.py  # govulncheck / go build / go test / npm audit / Redis 3 移除断言
├── acl/
│   └── run_acl.py               # Codis ACL，18 个 case
├── qps/
│   └── run_qps_limit.py         # Proxy QPS 限流，11 个 case
├── pitr/
│   └── run_pitr.py              # PITR 数据闪回，12 个 case
├── rdb/
│   └── run_rdb_analysis.py      # RDB Analysis，7 个 case
└── browser/
    ├── README.md                # Playwright 工程说明（中文）
    ├── package.json             # @playwright/test 固定 1.65.1
    ├── playwright.config.js     # 串行 chromium，list + json reporter
    └── tests/
        ├── helpers.js           # 上下文/环境变量解析、FE 导航、dashboard API、页面错误采集
        ├── fe-smoke.spec.js     # 4 个页面加载无 console error / HTTP >= 400
        ├── acl.spec.js          # 新增 + 删除 ACL 用户
        ├── qps-limit.spec.js    # 限流 0 -> 3 -> 0
        ├── pitr.spec.js         # 非法 ts、成功任务 9 步、不存在 server、删除
        └── rdb-analysis.spec.js # upload + remote fetch
```

`vlib.py` 被所有脚本 import，提供进程启停、RESP 客户端、HTTP、Evidence 文档、集群/配置生成等公共能力；单独执行没有意义，改动它等于改动全部脚本。

## 快速开始

前置：Python ≥ 3.10、git、Go 工具链与 make（`make build-all`）；浏览器用例另需 Node.js 20+（Playwright 1.65.1 的 `engines` 要求）与 Chromium。

### 真机（Linux）

```bash
cd <repo>
make build-all   # 产出 bin/codis-*、bin/redis-cli、bin/redis-check-aof；Linux 构建依赖见 doc/build_and_release_zh.md

python3 scripts/validation/env/collect_env.py
python3 scripts/validation/env/setup_cluster.py --groups 2 --replicas 1 --proxies 2 --fe
# setup_cluster.py 在 stderr 打印 cluster.json 路径，例如
#   /path/to/codis/.runtime/realmachine/20261124T120000Z/cluster.json

python3 scripts/validation/acl/run_acl.py --cluster <cluster.json>
python3 scripts/validation/qps/run_qps_limit.py --cluster <cluster.json>
python3 scripts/validation/pitr/run_pitr.py --cluster <cluster.json>
python3 scripts/validation/rdb/run_rdb_analysis.py --cluster <cluster.json>

python3 scripts/validation/env/teardown_cluster.py --cluster <cluster.json>
```

不依赖验证集群、可以单独执行的脚本：

```bash
python3 scripts/validation/redis8/run_functional_gate.py     # build-all / gotest / 5 组 Tcl / e2e；--docker 可选
python3 scripts/validation/security/run_dependency_audit.py  # govulncheck / go build+test / npm audit / Redis 3 移除断言
```

`run_functional_gate.py` 自己会跑 `make build-all`，并复用 [`../redis8_local_validation.py`](../redis8_local_validation.py) 跑 e2e；它的输出可以作为"构建 + 回归"的完整记录，需要复用已有 `bin/` 时加 `--skip-build`。

### 本地 macOS 冒烟

命令与上面完全一致。`make build-all` 在 macOS 产出 darwin 二进制，e2e 与各场景脚本同样可跑；但按[口径](../../doc/validation/README.md#口径)，macOS 结果只是本地回归，不能计入真机验证结论。

### 浏览器用例（Playwright）

```bash
cd scripts/validation/browser
npm install
npx playwright install chromium
CODIS_CLUSTER=<cluster.json> npx playwright test
```

`CODIS_CLUSTER` 会推导 FE URL、product、dashboard 地址、xauth、RDB 文件与 PITR master，只需对 `setup_cluster.py` 拉起的集群执行。工程细节、环境变量与每个 spec 的行为见 [browser/README.md](browser/README.md)。

## 运行约定

| 约定 | 内容 |
| --- | --- |
| 工作目录 | 默认 `<repo>/.runtime/realmachine/<run-id>/`；`<run-id>` 是 UTC 时间戳，形如 `20261124T120000Z`（`vlib.new_run_id()`）。`setup_cluster.py --workdir` 可覆盖 |
| 证据文件 | 默认 `<repo>/.runtime/realmachine/<脚本名>-<run-id>.json`，`--output` 可覆盖；各脚本默认名见下表 |
| 场景脚本参数 | `--cluster <cluster.json>` 必填；`--output <evidence.json>` 可选；`--repo` 只在仓库根自动探测失败时需要 |
| 完整参数 | 每个脚本都支持 `--help`，以它为准；例如 `run_acl.py --skip-restart`、`run_pitr.py --timeout`、`run_functional_gate.py --only / --skip-build` |
| 退出码 | 0 = 全部 case `passed`；1 = 有 `failed`（含脚本未捕获异常）；2 = 没有 failed，但有 `blocked` / `skipped` |
| 证据结构 | 顶层 `script` / `feature` / `scope` / `cluster` / `generated_at` / `host` / `head` / `status` / `notes` / `cases[]`；每个 case 有 `id` / `title` / `status` / `steps` / `data` / `error` / `reason` / `duration_sec` |
| 状态复位 | 场景脚本在 `finally` 中复位自己改动的状态（ACL 关闭、QPS 限流归 0、PITR/RDB 任务删除）；集群进程由 `teardown_cluster.py` 收尾 |

默认证据文件名：

| 脚本 | 默认路径（相对 `.runtime/realmachine/`） |
| --- | --- |
| `env/collect_env.py` | `env-<run-id>.json` |
| `env/setup_cluster.py` | `<run-id>/cluster.json`（工作目录内） |
| `env/teardown_cluster.py` | `<run-id>/cleanup.json` |
| `redis8/run_functional_gate.py` | `redis8-gate-<run-id>.json` |
| `security/run_dependency_audit.py` | `security-audit-<run-id>.json` |
| `acl/run_acl.py` | `run_acl-<run-id>.json` |
| `qps/run_qps_limit.py` | `run_qps_limit-<run-id>.json` |
| `pitr/run_pitr.py` | `run_pitr-<run-id>.json` |
| `rdb/run_rdb_analysis.py` | `run_rdb_analysis-<run-id>.json` |

`cluster.json` 的关键字段：`product`、`workdir`、`xauth`、`bin_dir`、`coordinator`、`dashboard`、`fe`、`proxies[]`、`groups[].servers[]`、`features{aof, rdb_export, pitr}`。`cleanup.json` 记录每个进程的停止结果、端口是否释放、workdir 是否删除，用于回填 ENV 的"已销毁"状态。场景脚本按 `features` 做整体前置检查：`--no-aof` 会让 PITR 脚本整体 blocked，`--no-rdb-export` 会让 RDB 脚本整体 blocked。

## 能力 ↔ 文档 ↔ 脚本总表

| 能力域 | 对应文档 | 真机脚本 | 浏览器用例 | 备注 |
| --- | --- | --- | --- | --- |
| 环境采集 · 集群搭建 · 清理 | [redis8_validation_zh.md](../../doc/redis8_validation_zh.md)（"必须记录的环境字段"）、[validation/README.md](../../doc/validation/README.md)（ENV 登记） | `env/collect_env.py`、`env/setup_cluster.py`、`env/teardown_cluster.py` | — | collect_env 覆盖 8 类字段；setup 默认开启 aof / rdb-export / pitr，filesystem coordinator |
| Redis 8 功能 gate | [redis8_validation_zh.md](../../doc/redis8_validation_zh.md)（验证 Gate、必跑命令） | `redis8/run_functional_gate.py` | — | build / gotest / 5 组 Tcl / e2e；`--docker` 可选；性能 gate 按要求未脚本化 |
| 依赖与供应链安全 | [redis8_validation_zh.md](../../doc/redis8_validation_zh.md)（Redis 3 移除）、[build_and_release_zh.md](../../doc/build_and_release_zh.md) | `security/run_dependency_audit.py` | — | govulncheck / go build / go test / npm audit / Redis 3 移除断言 / codis-server 版本 |
| Codis ACL | [codis_acl_zh.md](../../doc/codis_acl_zh.md) | `acl/run_acl.py` | [tests/acl.spec.js](browser/tests/acl.spec.js) | 18 个 case；浏览器覆盖新增 / 删除用户 |
| Proxy QPS 限流 | [proxy_qps_rate_limit_zh.md](../../doc/proxy_qps_rate_limit_zh.md) | `qps/run_qps_limit.py` | [tests/qps-limit.spec.js](browser/tests/qps-limit.spec.js) | 11 个 case；浏览器 0 -> 3 -> 0 |
| PITR 数据闪回 | [pitr_flashback_zh.md](../../doc/pitr_flashback_zh.md) | `pitr/run_pitr.py` | [tests/pitr.spec.js](browser/tests/pitr.spec.js) | 12 个 case；`pitr-cancel-in-truncate` 固定 skipped（S17） |
| RDB Analysis | [rdb_analysis_zh.md](../../doc/rdb_analysis_zh.md) | `rdb/run_rdb_analysis.py` | [tests/rdb-analysis.spec.js](browser/tests/rdb-analysis.spec.js) | 7 个 case；覆盖导出端、remote fetch、upload、workspace、拒绝类 |
| FE 浏览器冒烟 | [validation/README.md](../../doc/validation/README.md)（ENV-01 "FE 控制台重设计"）、[cmd/fe/assets/README.md](../../cmd/fe/assets/README.md) | —（环境由 `env/setup_cluster.py --fe` 提供） | [tests/fe-smoke.spec.js](browser/tests/fe-smoke.spec.js) | 4 个页面加载无 console error / HTTP >= 400 |

## 环境字段与证据

- `env/collect_env.py` 输出的 8 个顶层 section 与 `doc/redis8_validation_zh.md` 的"必须记录的环境字段"一一对应：`repo`（仓库版本）、`os`（操作系统）、`hardware`（硬件）、`toolchain`（构建工具链）、`backend_binary`（后端二进制）、`runtime_config`（运行配置）、`test_resources`（测试资源）、`cleanup`（清理结果）。
- `collect_env.py` 始终以 0 退出：单条探测失败只降级为 `unsupported-on-<platform>` 或 `error` 字段，不会中断采集；采完再判断缺哪些字段，缺项按"未验证"处理。
- 在 `doc/validation/README.md` 登记 ENV-xx 时，把 `env-<run-id>.json` 的路径（本地 `.runtime/` 证据）和关键字段写进"真机环境记录"；环境销毁后由 `cleanup.json` 的 `stopped` / `ports_released` / `workdir_removed` 支撑"已销毁"状态。
- 场景脚本的 evidence 里同时带 `head`（commit / branch / dirty）和 `host`（hostname / OS / arch 等），详细文档引用时把这两项和 `cluster.json` 路径一起写出，避免"结论对哪次 HEAD 负责"说不清。

## 实测与文档差异

下表是脚本在真机上实测到、与现有能力文档不一致或文档未写的行为。脚本按实测实现；对应能力文档的"真机验证脚本"一节也标注了这些差异。

| # | 能力 | 文档说法 | 实测行为 | 出现位置 |
| --- | --- | --- | --- | --- |
| 1 | ACL | 规则表只写 `>明文密码` 会被拒绝，未给文案 | 实际文案是 `acl user <name>: plain password token is not allowed`（不是 `plaintext ...`） | `acl/run_acl.py` 的 `acl-reject-plaintext` |
| 2 | ACL | 文档写 Redis 重启后命令返回 `NOPERM` | 重启后 runtime ACL 丢失，proxy 上该用户 `AUTH` 仍成功，但后续命令返回 `ERR handle response, backend conn failure, EOF`：proxy 为 ACL 用户单独建后端连接并以该用户认证，连 Redis 的 AUTH 就失败；`codis-admin --acl-sync` 后恢复 | `acl/run_acl.py` 的 `acl-redis-restart-resync` |
| 3 | QPS | 文档把 `proxy_sync_failed:<token>` 列为 `sync_status` 的取值之一 | `PUT /api/topom/proxy/qps-limit/{xauth}` 成功时一律返回 `sync_status="ready"`；`proxy_sync_failed:<token>` 只能通过随后的 GET 观测。另：proxy stats 无 xauth 路径是 `GET /proxy/stats`，带 xauth 才是 `/api/proxy/stats/{xauth}` | `qps/run_qps_limit.py` |
| 4 | QPS | 文档只列 `AUTH`、`QUIT`（和 admin HTTP API）不限流 | `AUTH` / `QUIT` 确实豁免，但 `PING`、`ACL WHOAMI`、`INFO`、`SELECT`、`CLIENT LIST` 等本地命令都会消耗预算并被限流 | `qps/run_qps_limit.py` 的 `qps-auth-quit-exempt`、`qps-burst-*` |
| 5 | PITR | 文档只写 `pitr_enabled=false` 时 `/api/topom/pitr/*` 返回 `pitr is disabled` | 实际是 HTTP 800 + `{"Cause":"pitr is disabled"}`，不是 503 | `pitr/run_pitr.py` 的 `pitr-precheck` 前置检查 |
| 6 | PITR | 文档未写 create 的返回码 | 非 master / 不存在的 server 不是 create 阶段 800：create 返回 200 + job_id，随后在 `validate` 步失败、`state=failed`。同步拒绝只有 `truncate_ts<=0`、锁冲突、并发上限、`redis-check-aof` 缺失 | `pitr/run_pitr.py` 的 `pitr-reject-non-master`、`pitr-reject-unknown-server` |
| 7 | PITR | 文档只要求 `truncate_ts` 落在最后一个 segment 的 `#TS:` 范围内 | `#TS:` 注解只在该秒有写入时才产生，`truncate_ts=int(time.time())` 在空闲 ≥ 1s 后会落在区间之外；脚本用 before → marker → ts → after → marker 构造合法点，或取扫描区间的 max | `pitr/run_pitr.py` 的 `pitr-happy-path` |
| 8 | RDB | 文档未说明非法 regex 的报错阶段 | 不是 create 阶段 800，而是 job `status=error`、`error="illegal regex expression: ..."`；导出端在首次 `SAVE` 之前返回 404 | `rdb/run_rdb_analysis.py` 的 `rdb-rejects`、`rdb-export-direct` |
| 9 | FE | 文档未记录（产品缺陷未修复） | `cmd/fe/assets/rdb-analysis.js` 的 `runJobAction()` remove 分支先清 `activeJobId`（约 L993），`.finally` 中的 `mutationIsCurrent()` 不再匹配（约 L1016-1018），`actionPending` 卡在 true："清除任务"后「新建分析」按钮持续 disabled，需刷新页面。浏览器用例通过每个用例新开页面规避 | `browser/tests/rdb-analysis.spec.js` |

## 限制

- 只覆盖单机布局（所有组件同机）；ENV-01 那种双机布局未脚本化。
- 性能压测按要求暂不固化：`redis-benchmark` 的参数与执行未入脚本，功能 gate 也不产出吞吐/延迟结论。
- 数据迁移控制台、codis-ha、Consul/ZooKeeper coordinator、Docker 部署（gate 脚本 `--docker` 可选）、多平台发布：尚无脚本。
- `pitr-cancel-in-truncate` 固定 skipped：S17 的取消语义未定稿，脚本不把"failed"当成稳定契约。
- 浏览器用例会真实创建/删除 ACL 用户、改限流、创建 PITR 任务（真实 `SHUTDOWN NOSAVE` 并截断 AOF）、创建 RDB 任务，只能对一次性验证集群运行，不能指向业务集群。
- 脚本只验证"功能与契约"，不做容量、稳定性或长稳压测；`head.dirty` 为 true 时证据只对工作区状态负责。

## 维护规则

1. 新增脚本必须同时改三处：本 README 的"能力 ↔ 文档 ↔ 脚本总表"加一行；对应能力文档加/更新 `## 真机验证脚本` 一节；`doc/validation/README.md` 的"脚本索引"同步。
2. 修改 `vlib.py` 后，先跑全部脚本的 `--help`（确认参数与 import 没坏），再完整跑一次 e2e（`setup_cluster.py` -> 任一场景脚本 -> `teardown_cluster.py`）。
3. 新增或改名 case 时保持 case id 稳定（详细文档和 ENV 记录会引用 case id）；默认证据文件名跟随脚本名，改名脚本要同步本 README 的默认证据文件表。
4. 浏览器 spec 变更时同步 [browser/README.md](browser/README.md) 的"每个 spec 做什么"和本 README 总表。
5. 脚本实测行为与文档冲突时以实测为准：在能力文档写实测值，并更新本 README 的"实测与文档差异"表；不要让脚本为迁就旧文案而断言错误行为。
