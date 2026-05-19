---
doc_type: refactor-design
refactor: 2026-05-18-coordinator-args-parse
status: approved
scope: 统一 cmd 下 coordinator CLI 参数解析，不改变 coordinator 后端能力或 docopt usage 语义
summary: 新增 cmd/internal/coordinator 解析函数并替换 dashboard/proxy/admin/fe 的重复 switch
---

# coordinator-args-parse refactor design

## 1. 本次范围

- 从 scan 勾选：#1 抽出 cmd coordinator 参数解析。
- 明确不做：不抽 `cmd/admin/main.go` 和其他入口的 usage 字符串；这些字符串包含命令特有语法，强行拼接会增加维护风险。
- 放行依据：用户本轮明确要求“使用 cs-refactor 解决”该唯一条目。
- 预估总工作量 / 风险：小到中；跨 4 个入口调用点，但新逻辑是纯解析函数。

## 2. 前置依赖

- 补刻画测试：在 `cmd/internal/coordinator` 中用 table test 固化当前 zookeeper、etcd、filesystem、consul 解析和 auth flag 行为。
- 调用方搜索：用 `rg -n -e "--zookeeper" -e "--consul" cmd pkg` 确认解析重复点只在 dashboard/proxy/admin/fe。

## 3. 执行顺序

### 步骤 1：新增共享解析函数和刻画测试

- 引用方法：M-L1-04 Characterization Test；M-L2-01 Extract Function
- 具体操作：新增 `cmd/internal/coordinator.Parse` / `MustParse`，集中处理 coordinator flag 顺序、地址解析、可选 auth flag，并新增 table tests 覆盖当前行为。
- 退出信号：`go test ./cmd/internal/coordinator` 通过。
- 验证责任：AI 自证。
- 回滚：删除新增包即可回到旧入口内解析。

### 步骤 2：替换 4 个入口调用点

- 引用方法：M-L2-01 Extract Function
- 具体操作：`cmd/dashboard/main.go`、`cmd/proxy/main.go`、`cmd/admin/admin.go`、`cmd/fe/main.go` 改为调用共享解析函数；dashboard 只在 CLI 显式给出 auth 时覆盖 `CoordinatorAuth`，保持现有 config auth 兼容语义。
- 退出信号：`go test ./cmd/...` 通过，且 grep 不再出现重复 coordinator switch 解析块。
- 验证责任：AI 自证。
- 回滚：还原 4 个入口调用点并删除新增 import。

### 步骤 3：全量回归与记录

- 引用方法：M-L1-01 Parallel Change
- 具体操作：运行项目 Go 测试入口，更新 checklist 和 apply notes。
- 退出信号：`make gotest` 通过；`python3 .codestable/tools/validate-yaml.py --file ... --yaml-only` 通过。
- 验证责任：AI 自证。
- 回滚：整体 revert 本 refactor 改动。

## 4. 风险与看点

- dashboard 原逻辑在 CLI 只覆盖 coordinator name/addr，不清空从配置文件读取的 auth；新 helper 需要用 `HasAuth` 区分显式 auth 和无 auth。
- proxy/admin/fe 原逻辑没有 coordinator 时的语义不同：proxy 允许无 coordinator，admin/fe 需要 panic；因此提供 `Parse` 和 `MustParse` 两个入口。
- `cmd/admin/main.go` 是 usage 文本，不是解析逻辑；本次不改它，避免把语法字符串抽象成难读的拼接模板。
