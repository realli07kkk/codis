# redis8-rdb-http-export 验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-06-02
> 关联方案 doc：`.codestable/features/2026-06-01-redis8-rdb-http-export/redis8-rdb-http-export-design.md`

## 1. 接口契约核对

对照方案第 2.1 节名词层逐一核查：

**接口示例逐项核对**：

- [x] `GET /codis/rdb/latest HTTP/1.0|1.1` + `X-Codis-RDB-Auth`：实现中固定为 `CODIS_RDB_EXPORT_PATH` 和 `CODIS_RDB_EXPORT_AUTH_HEADER`，只接受精确 GET path 与 HTTP/1.0|1.1。
- [x] 成功响应：`codisRdbExportStartFileResponse` 生成 `200 OK`、`Content-Length`、`Content-Disposition`、`X-Codis-RDB-Mtime` 和 `Connection: close`，Tcl 测试校验 header 与 body。
- [x] 错误响应：精确 export request 内部返回 `403`、`404` 或 `400`，短 body，不回显 auth 或绝对路径。

**名词层“现状 -> 变化”逐项核对**：

- [x] RDB export config：`server.h` 新增 `codis_rdb_export_enabled` / `codis_rdb_export_auth`；`config.c` 注册 immutable config，auth 标记 sensitive，并在 enabled yes + empty auth 时启动失败。
- [x] HTTP request 契约：`networking.c` 在 RESP parser 前调用 `codisRdbExportTryHandle`；非精确 HTTP-like 请求回到 Redis 既有处理。
- [x] RDB candidate：`codisRdbExportOpenDbfilename` 只 snapshot 当前 `server.rdb_filename` basename，并执行 basename/suffix、`lstat`、`open` + `fstat` 和 RDB magic 校验。
- [x] Streaming state：`client.codis_rdb_export_state` 持有 fd、header、offset 和 buffer，完成或异常后统一清理。

**流程图核对**：

- [x] 图中节点均有代码落点：socket read hook 在 `networking.c`，IO-thread handoff 在 `iothread.c`，auth/candidate/open/header/stream/cleanup 在 `codis_rdb_export.c`。

## 2. 行为与决策核对

**需求摘要逐项验证**：

- [x] 默认关闭：`CONFIG GET` 默认值测试通过；默认配置下 HTTP export 返回 404，Redis `PING`、`SET/GET`、inline `GET` 仍正常。
- [x] 开启后需要非空 auth：启动失败测试覆盖 `--codis-rdb-export-enabled yes` 且 auth 为空。
- [x] 正确 auth 下载已有 RDB：Tcl 测试写入 `REDIS0001...` 文件后下载，body 与文件内容一致。
- [x] 错误 auth 返回 403 且不传文件：Tcl 测试覆盖缺失和错误 header。
- [x] 无有效候选返回 404，不生成快照：Tcl 测试覆盖 missing、symlink、非 RDB 和 `lastsave` 不变；`rg` 确认 helper 不调用 save/bgsave 符号。

**明确不做逐项核对**：

- [x] 未新增 Redis 命令或 command JSON：本次挂载点只在 config/networking/iothread/server/codis_rdb_export 和 Tcl 测试。
- [x] 未改 proxy/dashboard/FE/coordinator/Go API：反向 grep 命中均落在 Redis 8 server、Redis config、Makefile 和 feature doc。
- [x] 不支持路径参数、query auth、Range、压缩、限速或独立端口：代码只接受固定 path 和 header。

**关键决策落地**：

- [x] 复用 Redis 监听端口：无独立 HTTP server 或新端口配置。
- [x] 最小 HTTP 例外：只有完整精确 GET export request 被截获；POST guard 和 query-string 请求测试确认回到 Redis 既有语义。
- [x] 配置重启生效：两个配置均为 `IMMUTABLE_CONFIG`。
- [x] IO thread 边界：IO thread 只标记 `CLIENT_IO_PENDING_RDB_EXPORT` 并交回主线程；主线程执行 auth、配置读取、打开文件和安装 streaming state。

**挂载点反向核对**：

- [x] 清单内挂载点均存在：`.gitignore`、Redis Makefiles、`config.c`、`server.h`、`networking.c`、`iothread.c`、`codis_rdb_export.c`、两份 `redis.conf` 和 Tcl 测试。
- [x] 反向 grep：`codis_rdb_export|codis-rdb-export|CLIENT_IO_PENDING_RDB_EXPORT|/codis/rdb/latest|X-Codis-RDB-Auth|codisRdbExport` 命中均落在挂载点或 SDD 文档。
- [x] 拔除沙盘：移除 helper、hook、client/server 字段、config、Makefile object、配置模板和 Tcl 测试即可卸载能力；不会残留 Go 侧 API 或 Redis command JSON。

## 3. 验收场景核对

- [x] 默认配置不可用且 RESP 兼容：`unit/codis_rdb_export` 覆盖默认关闭、`PING`、`SET/GET` 和 inline `GET`。
- [x] enabled yes + empty auth 启动失败：`unit/codis_rdb_export` 覆盖并断言错误文本。
- [x] auth 错误返回 403：`unit/codis_rdb_export` 覆盖 missing/wrong auth。
- [x] 无有效 RDB 返回 404，不改变 `lastsave`：`unit/codis_rdb_export` 覆盖 missing、非 RDB、symlink 和 `lastsave`。
- [x] 成功下载：`unit/codis_rdb_export` 覆盖 `Content-Length`、`Content-Disposition`、`X-Codis-RDB-Mtime` 和 body 一致。
- [x] 只选择当前 `dbfilename`：测试覆盖其他 `.rdb`、fake、symlink 不会被传输。
- [x] 非 HTTP Redis 命令不被误拦截：测试覆盖 RESP / inline Redis 请求。
- [x] HTTP POST / query string 不导出：测试覆盖 POST guard 和 `GET /codis/rdb/latest?auth=...` fallback。
- [x] IO-thread 场景：`io-threads 2` 下 export 成功，并保持后续普通 `PING` 可用。
- [x] 编译与回归：`make codis-server`、`cd extern/redis-8.6.3 && ./runtest --single unit/codis_rdb_export`、`./runtest --single unit/protocol`、`./runtest --single unit/codis` 已通过；`git diff --check` 与 untracked 文件 diff check 已通过。

前端改动：无，未新增 dashboard/FE 页面，不需要浏览器验证。

## 4. 术语一致性

- `RDB HTTP export`：设计、架构和实现均指 Redis 8 Codis Server 本机固定 HTTP 下载口。
- `codis-rdb-export-enabled` / `codis-rdb-export-auth`：配置模板、`config.c`、测试和文档命名一致。
- `X-Codis-RDB-Auth`：实现、测试和设计一致；未使用 URL path/query 传密钥。
- `CLIENT_IO_PENDING_RDB_EXPORT` / `codis_rdb_export_state`：线程归属和 client state 命名一致。
- 防冲突：RDB Analysis 仍指 dashboard/topom 离线分析；本 feature 不复用该术语做下载接口。

## 5. 架构归并

- [x] `.codestable/architecture/ARCHITECTURE.md` 已更新 frontmatter 日期和 `rdb` tag。
- [x] 术语归并：新增 `RDB HTTP export`，说明它和 RDB Analysis、Redis 命令、dashboard API 的边界。
- [x] 动词骨架归并：写入 Redis 端口复用、parser 前 hook、IO-thread 主线程转交、主线程 auth/config/file/open/streaming 流程。
- [x] 流程级约束归并：写入默认关闭、header auth、只传当前 `dbfilename` 既有 RDB、不生成快照、不支持 Range/压缩/限速/独立端口等边界。
- [x] 代码锚点归并：补充 Redis Makefile、`codis_rdb_export.c`、`networking.c`、`iothread.c`、`server.h`、`config.c` 和 Tcl 测试。

## 6. requirement 回写

- [x] `requirement: redis-cluster-service` 指向 current req，本次新增用户可见运维能力，已更新 `.codestable/requirements/redis-cluster-service.md`。
- [x] 用户故事：补入“值班人员可从 Redis 8 Codis Server 拉取本机当前 `dbfilename` 的已有 RDB”。
- [x] 解决方式：补入固定路径、本机已有 RDB、header secret、不过 proxy/dashboard/coordinator、不触发持久化生成。
- [x] 实现进展：新增 2026-06-02 RDB HTTP export 记录。
- [x] 边界：补入不属于 RDB Analysis、不支持任意路径/Range/压缩/加密/限速/独立端口、配置重启生效和网络隔离要求。

## 7. roadmap 回写

- [x] 方案 frontmatter 没有 `roadmap` / `roadmap_item` 字段，本 feature 非 roadmap 起头，跳过 roadmap items 和主文档回写。

## 8. attention.md 候选盘点

- [x] 有候选，暂不写入 `attention.md`，待用户确认：
  - 候选 1：Redis 8 vendored Makefile 原本被 `.gitignore` 的 `makefile` 规则忽略；涉及 Redis 源码 object 接入的 feature，应优先让相关 Makefile 成为 tracked / unignored 构建输入，不能靠构建时 patch ignored 本地文件。

## 9. 遗留

- 后续优化点：按 design 建议，适合补一份运维指南，包含配置示例、curl 示例、内网/TLS 要求、密钥轮换需重启和不支持 Range/断点续传。
- 已知限制：首版无 Range、压缩、加密、限速、并发配额、独立 HTTP 端口或 dashboard/FE 管理面；TLS 专项未在本 feature 的 Tcl 测试中覆盖。
- 顺手发现：构建输入可审查性已通过 `.gitignore` exception 与 Redis Makefile track 修复；无未处理 SDD 偏差。
