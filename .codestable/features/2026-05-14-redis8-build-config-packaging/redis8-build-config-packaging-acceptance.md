---
doc_type: feature-acceptance
feature: 2026-05-14-redis8-build-config-packaging
status: current
accepted_at: 2026-05-14
summary: Redis 8 Codis Server 已成为默认构建、配置模板和包装入口；Redis 3 fallback 保留，cutover 验证继续后移。
tags: [redis, codis-server, redis8, build, packaging]
---

# redis8-build-config-packaging 验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-05-14
> 关联方案 doc：`.codestable/features/2026-05-14-redis8-build-config-packaging/redis8-build-config-packaging-design.md`

## 1. 接口契约核对

对照方案第 2.1 节名词层逐一核查：

**接口示例逐项核对**：
- [x] `make codis-server`：`./bin/codis-server --version` 输出 `Redis server v=8.6.3`，默认 server 构建来自 `extern/redis-8.6.3/`。
- [x] `make codis-server-redis3`：`./bin/codis-server-redis3 --version` 输出 `Redis server v=3.2.11`，fallback 产物使用 suffixed 名称，不覆盖 `bin/codis-server`。
- [x] `bin/codis-server config/redis.conf --port 0`：Unix socket smoke 返回 `CONFIG GET codis-enabled => yes`、`CONFIG GET cluster-enabled => no`，`SLOTSHASHKEY smoke-key => 36`。
- [x] `COMMAND INFO`：25 个 Codis slot / migration / restore / async 命令均可发现；metadata 审计记录在 `redis8-command-metadata-audit.md`。
- [x] `scripts/docker.sh server`：server 分支显式传 `config/redis.conf`，并在容器场景下传 `--protected-mode no --bind 0.0.0.0`。

**名词层“现状 → 变化”逐项核对**：
- [x] 默认 server 构建产物：`Makefile` 的 `codis-server` 目标已从 Redis 3 切到 Redis 8；`codis-server-redis8` 是默认产物的 thin alias。
- [x] Redis 3 fallback 构建：`codis-server-redis3`、`redis-cli-redis3`、`redis-benchmark-redis3`、`redis-sentinel-redis3` 保留。
- [x] Codis Server 配置模板：`config/redis.conf` 是 Redis 8 模板并含 `codis-enabled yes`；`cluster-enabled` 未被开启。
- [x] Command metadata 收口：`commands.def` 由 JSON 源生成，`make -C extern/redis-8.6.3/src commands.def` 返回 up to date。
- [x] 发布包装入口：`Dockerfile`、`scripts/docker.sh`、`example/server.py` 已围绕默认 Redis 8 Codis Server 适配；`kubernetes/codis-server.yaml` 已确认继续加载 tracked `config/redis.conf`。

**流程图核对**：
- [x] A/B/C：metadata 审计完成，无需修 JSON；`commands.def` 与 JSON 源无 diff。
- [x] D/E/F/G：默认 Makefile 构建、Redis 3 fallback、Redis 8 配置模板、Docker/example/Kubernetes 入口均有落点。
- [x] H/I：`make build-all`、Redis 8 Tcl smoke、Go 测试和手工 socket smoke 通过；`redis8-validation-cutover` 接收后续端到端工作。

## 2. 行为与决策核对

**需求摘要逐项验证**：
- [x] 默认 `codis-server` 产物来自 Redis 8：`make codis-server` + `--version` 已验证。
- [x] 默认配置启动即进入 Codis mode：socket smoke 验证 `codis-enabled=yes`、`cluster-enabled=no`。
- [x] 命令 metadata 与 JSON 生成源一致：`make -C extern/redis-8.6.3/src commands.def` + `git diff --exit-code` 通过。
- [x] 示例 / Docker / Kubernetes 不会以 `codis-enabled no` 启动 Redis 8：脚本和 manifest 静态核查通过。

**明确不做逐项核对**：
- [x] 未修改 Redis Cluster `MOVED` / `ASK` / cluster bus 语义，未把 `cluster-enabled yes` 当作 Codis mode 前提。
- [x] 未新增 Redis 命令实现，未改 `SLOTSMGRT*` / `SLOTSRESTORE*` 返回协议。
- [x] 未修改 Go proxy/topom/admin 生产协议适配，也未改 proxy `mapper.go` allow-list。
- [x] 未修改 `go.mod` / `go.sum`，未新增 `vendor/`、`Godeps/` 或 `vendor/modules.txt`。
- [x] 未删除 `extern/redis-3.2.11/`，Redis 3 fallback 构建能力仍可用。
- [x] 本报告未把端到端灰度、性能基线、Redis 3 ↔ Redis 8 数据迁移兼容写成本 feature 已完成。

**关键决策落地**：
- [x] D1 默认 `codis-server` 切到 Redis 8：`Makefile:30` 的 `codis-server` 调用 `$(REDIS8_DIR)`。
- [x] D2 默认配置启用 `codis-enabled yes` 但不启用 Redis Cluster：`config/redis.conf` 和启动 smoke 一致。
- [x] D3 命令表以 JSON 为源：未手写 `commands.def` diff，metadata 审计单独落档。
- [x] D4 包装入口只做最小适配：Docker/example 仅补构建环境与 Codis mode 启动，不改 cutover 流程。

**挂载点反向核对（可卸载性）**：
- [x] 根 `Makefile`：默认 server、fallback 和 `codis-server-redis8` alias 均在清单内。
- [x] `config/redis.conf` / `config/sentinel.conf`：Redis 8 tracked 模板和 Sentinel 暴露面说明在清单内。
- [x] Redis command registry：只新增审计文档，源码 JSON / `commands.def` 无改动。
- [x] `Dockerfile` + `scripts/docker.sh`：构建环境和 server 启动入口在清单内。
- [x] `example/server.py` + `kubernetes/codis-server.yaml`：example 已改，Kubernetes 已核对无需改。
- [x] 反向 grep / diff：本次代码 diff 只覆盖上述挂载点；额外改动均为 CodeStable design/checklist/audit/acceptance、architecture、requirement、roadmap 工作流产物。
- [x] 拔除沙盘推演：反向移除 Makefile/config/Docker/example/scripts 改动后，默认发布物会回到 Redis 3，Redis 8 packaging 能力随之消失；无清单外运行时代码残留。

## 3. 验收场景核对

- [x] **S1**：`make codis-server` 后 `./bin/codis-server --version` 显示 `Redis server v=8.6.3`。
  - 证据来源：本地构建命令。
  - 结果：通过。
- [x] **S2**：Redis 3 fallback 构建目标通过，产物不覆盖 `bin/codis-server`。
  - 证据来源：`make codis-server-redis3` + `./bin/codis-server-redis3 --version`。
  - 结果：通过。
- [x] **S3**：`make build-all` 通过，默认完整构建包含 Redis 8 server 与 Go 二进制。
  - 证据来源：本地构建命令。
  - 结果：通过。
- [x] **S4**：用 `config/redis.conf` 启动后 `codis-enabled=yes`、`cluster-enabled=no`，slot 命令可用。
  - 证据来源：Unix socket smoke。
  - 结果：通过。
- [x] **S5**：`CONFIG REWRITE` 后仍保留 `codis-enabled yes`。
  - 证据来源：临时配置 copy 上执行 `CONFIG REWRITE` 后 `grep '^codis-enabled yes$'`。
  - 结果：通过。
- [x] **S6**：`COMMAND INFO` 查询全部 Codis slot / migration / restore / async 命令均可发现。
  - 证据来源：25 命令循环检查 + metadata audit。
  - 结果：通过。
- [x] **S7**：重新生成 `commands.def` 后与 JSON 源一致。
  - 证据来源：`make -C extern/redis-8.6.3/src commands.def` + `git diff --exit-code`。
  - 结果：通过。
- [x] **S8**：Dockerfile 不再使用 Go 1.8，静态检查证明构建环境不再卡在旧镜像。
  - 证据来源：`Dockerfile` 为 `golang:1.26.1`，`GO111MODULE on`；Docker daemon 当前不可连接，未运行 docker build。
  - 结果：通过静态门槛。
- [x] **S9**：`scripts/docker.sh server`、`example/server.py`、`kubernetes/codis-server.yaml` 不会以 `codis-enabled no` 启动 Redis 8。
  - 证据来源：`bash -n scripts/docker.sh`、`python3 -m py_compile example/server.py`、静态 grep。
  - 结果：通过。
- [x] **S10**：Redis 8 Codis smoke 写入 key 后 slot 语义为 0..1023。
  - 证据来源：`SLOTSHASHKEY smoke-key => 36`、`SLOTSINFO 0 1024` 可观察对应 slot；`./runtest --single unit/codis` 通过。
  - 结果：通过。
- [x] **Go 回归**：`make gotest` 通过。

## 4. 术语一致性

- `正式 Redis 8 Codis Server 构建`：`Makefile`、architecture、requirement、roadmap 均表述为默认构建已切 Redis 8。
- `Redis 3 fallback 构建`：代码和文档统一使用 `codis-server-redis3` / `*-redis3` 后缀。
- `Codis Server 配置模板`：`config/redis.conf` 统一作为 tracked Redis 8 Codis mode 模板，`config/redis8.conf` 只由 alias 目标复制生成。
- `Command metadata 收口`：审计文档明确 JSON 为源，未出现手写 generated file 的实现路径。
- 防冲突：旧判断“Redis 8 Codis Server 仍是独立支线 / 默认仍构建 Redis 3”已从 architecture 和 requirement 当前态中移除。

## 5. 架构归并

- [x] `.codestable/architecture/ARCHITECTURE.md`：构建层现状已更新为默认 Redis 8、Redis 3 fallback、`codis-server-redis8` alias；配置模板、包装入口和 cutover 边界已写入。
- [x] 代码锚点：补充 `config/redis.conf` / `config/sentinel.conf`、`Dockerfile` / `scripts/docker.sh` / `example/server.py` / `kubernetes/codis-server.yaml`。
- [x] 已知约束：补充默认 Redis 8 与 Redis Cluster 不启用、Redis 3 fallback、端到端 cutover 后移的边界。

## 6. requirement 回写

- [x] `requirement: redis-cluster-service` 指向 current req，本次改变了能力实现进展，需要 update。
- [x] `.codestable/requirements/redis-cluster-service.md` 已更新“怎么解决”“实现进展”“边界”：默认 `codis-server` 已切到 Redis 8 Codis Server，Redis 3 fallback 保留，灰度 cutover / 性能基线 / 回滚仍属后续。
- [x] `requirements/VISION.md` 无需更新：能力 pitch 和 status 未变化。

## 7. roadmap 回写

- [x] design frontmatter 含 `roadmap: redis8-upgrade` + `roadmap_item: redis8-build-config-packaging`。
- [x] `.codestable/roadmap/redis8-upgrade/redis8-upgrade-items.yaml` 对应条目从 `in-progress` 改为 `done`，feature 指向 `2026-05-14-redis8-build-config-packaging`。
- [x] `.codestable/roadmap/redis8-upgrade/redis8-upgrade-roadmap.md` 第 5 节子 feature 清单已同步为 `done` 并补充 packaging 完成备注。
- [x] YAML/frontmatter 校验通过：architecture、requirement、roadmap 主文档和 items.yaml 均 valid。

## 8. attention.md 候选盘点

- 候选 1：当前本机 Docker daemon 不可连接，Redis 8 packaging 只能完成 Dockerfile / shell 静态检查，不能运行 `docker build`。如果这是长期环境事实，可通过 `cs-note` 记入 `.codestable/attention.md`。
- 已有条目覆盖：`make build-all` 会刷新配置文件、使用 `python3` 跑 `.codestable/tools/*.py`、Redis 8 源码固定在 `extern/redis-8.6.3/`，本次无需重复写入。

## 9. 遗留

- 后续优化点：`redis8-validation-cutover` 继续覆盖端到端迁移演练、性能基线、灰度切换和回滚策略。
- 已知限制：未运行 `docker build`，原因是 Docker daemon 不可连接；本 feature 仅以 Dockerfile 和 `scripts/docker.sh` 静态检查满足验收门槛。
- 已知限制：Redis 8 配置模板默认值相对 Redis 3 有行为变化，design 已记录 daemonize/save/logfile/pidfile/bind/repl-diskless-sync/Sentinel protected-mode 差异；运维迁移影响由 cutover 阶段继续评估。
- 实现阶段顺手发现：`codis-server-redis8` 曾是独立重复构建目标，已在验收前改成默认产物 alias。
