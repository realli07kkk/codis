# Codis RDB Analysis

Codis Dashboard 支持在浏览器中启动 RDB 文件分析任务，用于查看 RDB 中的内存占用概览、DB/类型分布、big keys、hot keys 和 prefix 聚合结果。

## 使用方式

1. 启动 `codis-dashboard` 和 `codis-fe`。
2. 在 FE 页面左侧选择 Codis product。
3. 在 `RDB Analysis` 区域选择一种输入：
   - `Upload`：从浏览器上传一个 RDB 文件。
   - `Workspace`：填写 dashboard 主机上 `rdb_analysis_workspace` 目录内的相对路径。
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

## 边界

- 该能力只分析已有 RDB 文件，不会自动对后端 Redis 执行 `BGSAVE` 或从远端 Redis Server 拉取 RDB。
- 分析任务状态保存在当前 dashboard 进程内，不写入 coordinator；dashboard 重启后任务消失。
- 分析结果可能包含 key 名，因此 RDB analysis API 都要求 `xauth`。
- 首版不导出完整 JSON/AOF，也不把完整 key 列表返回给浏览器。
