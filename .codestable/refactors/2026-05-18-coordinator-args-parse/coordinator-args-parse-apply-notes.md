---
doc_type: refactor-apply-notes
refactor: 2026-05-18-coordinator-args-parse
---

# coordinator-args-parse apply notes

## 步骤 1: 新增共享解析函数和刻画测试

- 完成时间: 2026-05-18
- 改动文件: `cmd/internal/coordinator/args.go`, `cmd/internal/coordinator/args_test.go`
- 验证结果: `go test ./cmd/internal/coordinator` 通过
- 偏离: 无

## 步骤 2: 替换 4 个入口调用点

- 完成时间: 2026-05-18
- 改动文件: `cmd/dashboard/main.go`, `cmd/proxy/main.go`, `cmd/admin/admin.go`, `cmd/fe/main.go`
- 验证结果: `go test ./cmd/...` 通过；`rg -n "var coordinator struct|case d\\[\\\"--zookeeper\\\"\\] != nil|case d\\[\\\"--consul\\\"\\] != nil|coordinator\\.name|coordinator\\.addr|coordinator\\.auth" cmd` 无命中
- 偏离: `cmd/admin/main.go` 是 docopt usage 文本，不是解析 switch，按 design 保持不改

## 步骤 3: 全量回归与记录

- 完成时间: 2026-05-18
- 改动文件: `.codestable/refactors/2026-05-18-coordinator-args-parse/coordinator-args-parse-checklist.yaml`, `.codestable/refactors/2026-05-18-coordinator-args-parse/coordinator-args-parse-apply-notes.md`
- 验证结果: `make gotest` 通过；`python3 .codestable/tools/validate-yaml.py --file .codestable/refactors/2026-05-18-coordinator-args-parse/coordinator-args-parse-checklist.yaml --yaml-only` 通过；`git diff --check` 通过
- 偏离: 无

## 收尾审查

- 完成时间: 2026-05-19
- 改动文件: `.codestable/refactors/2026-05-18-coordinator-args-parse/coordinator-args-parse-apply-notes.md`
- 验证结果: 用户 review 结论为 Good taste，未发现 Blocker/Critical；copyright 年份按仓库统一文件头保持 2016，不单独改成 2026；`go test ./cmd/internal/coordinator ./cmd/...` 通过；`make gotest` 通过；`python3 .codestable/tools/validate-yaml.py --dir .codestable/refactors/2026-05-18-coordinator-args-parse` 通过；`python3 .codestable/tools/validate-yaml.py --file .codestable/refactors/2026-05-18-coordinator-args-parse/coordinator-args-parse-checklist.yaml --yaml-only` 通过；`git diff --check` 通过；旧重复 switch grep 无命中
- 偏离: 无
