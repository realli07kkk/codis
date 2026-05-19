---
doc_type: refactor-scan
refactor: 2026-05-18-coordinator-args-parse
status: user-reviewed
scope: cmd/dashboard/main.go, cmd/proxy/main.go, cmd/admin/admin.go, cmd/fe/main.go, cmd/admin/main.go
summary: 发现 1 条结构类优化点；风险中；用户本轮已明确指定该项并要求解决
---

# coordinator-args-parse scan

## 总览

- 扫描范围：`cmd/dashboard/main.go`、`cmd/proxy/main.go`、`cmd/admin/admin.go`、`cmd/fe/main.go`、`cmd/admin/main.go`
- 发现 1 条优化点：结构 1 / 性能 0 / 可读性 0
- 按风险：低 0 / 中 1 / 高 0
- 建议先做：#1（重复逻辑集中、可用纯函数测试自证）
- 建议慎做 / 后做：不抽 docopt usage 字符串；`cmd/admin/main.go` 是 usage 文本，不是 switch-case 解析点
- 前置检查：无行为改动、非生成产物、范围 5 文件且低于 3000 行；CLI 解析缺少直接测试，已纳入设计步骤 1 用刻画测试补齐

## 条目

### #1 抽出 cmd coordinator 参数解析 ✓

- **位置**：`cmd/dashboard/main.go:92`、`cmd/proxy/main.go:139`、`cmd/admin/admin.go:47`、`cmd/fe/main.go:140`
- **分类**：结构
- **现状**：4 个解析点分别用 `switch` 判断 `--zookeeper`、`--etcd`、`--filesystem`、`--consul`，其中 3 处还重复处理 `--*-auth`。
- **问题**：同一 coordinator 类型判断重复 4 次；新增后端时需要在多个入口同步增删分支，已有 Consul 分支就是 4 份重复解析逻辑。
- **建议**：新增 `cmd/internal/coordinator` 解析包，集中返回 coordinator name、addr、auth 和 auth 是否由 CLI 显式给出；各入口只负责把结果写入本地 config 或创建 client。
- **建议映射的方法**：M-L2-01（Extract Function）
- **风险**：中（跨 4 个命令入口，但逻辑可抽成纯函数并用 table test 覆盖）
- **验证**：AI 自证（`go test ./cmd/internal/coordinator ./cmd/...`、`make gotest`、grep 旧重复 switch 残留）
- **范围**：约 90 行 / 5 文件
