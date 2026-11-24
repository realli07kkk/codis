# Codis RDB Analysis

Codis Dashboard 支持在浏览器中启动 RDB 文件分析任务，用于查看 RDB 中的内存占用概览、DB/类型分布、big keys、hot keys 和 prefix 聚合结果。

## 使用方式

1. 启动 `codis-dashboard` 和 `codis-fe`。
2. 在 FE 页面左侧选择 Codis product。
3. 在 `RDB Analysis` 区域选择一种输入：
   - `Upload`：从浏览器上传一个 RDB 文件。
   - `Workspace`：填写 dashboard 主机上 `rdb_analysis_workspace` 目录内的相对路径。
   - `Remote Server`：从当前 product 内某个 Redis 8 Codis Server 拉取该 server 本机已有的 RDB（见下文“远程拉取”）。
4. 设置 `TopN`、`Depth`、`Sep`、`Regex` 和是否包含已过期 key。
5. 启动任务后，页面会轮询 dashboard API 并展示进度和分析结果。

## Dashboard 配置

配置项在 `dashboard.toml`：

```toml
rdb_analysis_workspace = "/tmp/codis-rdb-analysis"
rdb_analysis_max_upload_size = "1gb"
rdb_analysis_max_concurrent_jobs = 1
rdb_analysis_max_jobs_retained = 16
rdb_analysis_max_top_n = 100
```

`Workspace` 输入只能读取 `rdb_analysis_workspace` 下的文件。API 返回中只展示安全摘要，不暴露 dashboard 主机上的绝对路径。

## 远程拉取

`Remote Server` 输入把 dashboard 和 Redis 8 Codis Server 的 RDB HTTP 导出接口接起来：dashboard 代表当前 product 向目标 server 的 Redis 端口发起 `GET /codis/rdb/latest`，把响应 body 流式写入受控临时文件，下载完整后再复用现有分析流程创建任务。Redis 8 Codis Server 侧的导出配置与限制见 [redis8_codis_server_zh.md](redis8_codis_server_zh.md)。

```toml
rdb_analysis_remote_fetch_enabled = false
rdb_analysis_remote_fetch_auth = ""
rdb_analysis_remote_fetch_timeout = "30m"
rdb_analysis_remote_fetch_max_concurrent = 1
```

- `rdb_analysis_remote_fetch_enabled`：默认关闭。FE 仍显示 `Remote Server` 选项，dashboard 会拒绝远程拉取请求；`Upload` 和 `Workspace` 可继续使用。
- `rdb_analysis_remote_fetch_auth`：dashboard 持久化的 Redis export 密钥，用于生成发往 Redis 的 `X-Codis-RDB-Auth` header。开启远程拉取时必须非空，否则 dashboard 配置校验失败；该字段不出现在 dashboard 配置的 JSON 输出中。
- `rdb_analysis_remote_fetch_timeout`：整个 HTTP 请求的超时，包含建连、等待响应和读取响应 body。
- `rdb_analysis_remote_fetch_max_concurrent`：并发拉取上限，与 `rdb_analysis_max_concurrent_jobs` 控制的解析并发相互独立。
- 文件大小上限复用 `rdb_analysis_max_upload_size`。

远程拉取的边界：

- `server_addr` 必须精确匹配当前 product 拓扑中的 server 地址，不接受任意 URL、scheme、path 或跨 product 目标。
- 目标 server 必须已开启 RDB HTTP 导出并配置了同样的密钥；导出返回 `403` 表示密钥不匹配或缺失，`404` 表示导出未开启或没有有效的 `dbfilename` 文件。
- dashboard 不跟随 redirect，避免把 `X-Codis-RDB-Auth` 发给其他 host；失败或取消时删除未完成的临时文件。
- 不执行 `SAVE`、`BGSAVE`，也不使用 `scp`、`rsync`、tus 或断点续传；需要新快照时先在 Redis 侧完成备份流程。
- 任务 `source` 记为 `remote-http:<server>/<filename>` 形式的安全摘要，不包含密钥和 dashboard 本地路径。
- `codis-admin --dashboard=ADDR --rdb-analysis-remote-fetch --server=ADDR` 是同一 dashboard API 的命令行入口，密钥同样只来自 dashboard 配置。

## RDB 解析器

分析任务使用 `third_party/rdb`：这是 `github.com/hdt3213/rdb v1.3.2` 的最小 fork，由根 `go.mod` 的 `replace` 指向本地目录，包路径保持 `github.com/hdt3213/rdb/parser` 和 `github.com/hdt3213/rdb/model`。

上游 v1.3.2 不能解析 Redis 8.6 生成的 `REDIS0013`（报 `cannot parse version: 13`），fork 在其基础上补齐：

- 接受 RDB version 13；
- 解码 `RDB_TYPE_STREAM_LISTPACKS_4`（类型 26）并消费 Redis 8.6 追加的 IDMP 元数据；
- 跳过 Redis 8 的 `RDB_OPCODE_KEY_META`（243）和 `RDB_OPCODE_SLOT_INFO`（244）；
- 为 stream 补齐 `GetElemCount()`，big key 表中的 stream 元素数不再显示为 0。

这些补丁是解析 Redis 8 RDB 的前提。升级上游版本时需要重新评估并重放它们，不能直接去掉本地 `replace`；`Upload`、`Workspace` 和 `Remote Server` 三个入口共用同一个解析器，因此 Redis 8 RDB 的解析行为在三条路径上一致。

## 边界

- 该能力只分析已有 RDB 文件，不会自动对后端 Redis 执行 `BGSAVE`；`Remote Server` 入口同样只下载本机已存在的 `dbfilename`，不生成新快照。
- 分析任务状态保存在当前 dashboard 进程内，不写入 coordinator；dashboard 重启后任务消失。
- 分析结果可能包含 key 名，因此 RDB analysis API 都要求 `xauth`。
- 首版不导出完整 JSON/AOF，也不把完整 key 列表返回给浏览器。
