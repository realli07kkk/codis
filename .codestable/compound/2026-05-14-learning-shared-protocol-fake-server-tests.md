---
doc_type: learning
track: knowledge
date: "2026-05-14"
slug: shared-protocol-fake-server-tests
component: test-infrastructure
tags: [go, test, redis, protocol, compatibility]
related_feature: 2026-05-14-redis8-go-component-adapters
---

# 跨包协议兼容测试应共享 fake server 基础设施

## 1. 背景

`redis8-go-component-adapters` 为 `pkg/utils/redis`、`pkg/proxy` 和 `pkg/topom` 补 Redis 8 兼容测试时，最开始在两个包里各写了一套几乎相同的 fake Redis server：都负责监听 `127.0.0.1:0`、解码 RESP array、记录 command、调用 handler、编码 RESP reply。

单个测试看起来都合理，但重复的 fake server 会让协议测试产生一致性幻觉：两个包验证的是“看起来相同”的 Redis 8 协议，实际却由两份独立演化的测试基础设施解释。后续一旦需要调整 RESP 解码、pipeline 行为、错误返回或 RESP3 兼容，很容易只改一份，另一份继续给出过时结论。

本次收口为 `pkg/proxy/redis/redistest`：共享 transport、RESP 编解码、command recording 和基础 response helper；各包测试只保留自己的命令 handler 和断言。

## 2. 指导原则

- 协议 fake server 的职责应停在 transport 层：监听、解码、记录命令、调用 handler、编码返回。
- 具体业务语义留在各测试包自己的 handler 里，例如 `INFO` 返回什么字段、`SLOTSMGRT-EXEC-WRAPPER` 返回哪个 code。
- 当两个以上包需要模拟同一底层协议时，优先抽共享测试 helper，而不是复制一个“稍微改名”的 fake server。
- 共享 helper 要保持小而稳定，不把某个测试场景的状态塞进通用 struct field。
- 对需要包内默认行为的旧 fake server，可以先支持 handler 覆盖模式，不必为了“统一”强行迁走所有历史测试。

## 3. 为什么重要

协议兼容测试的目标是验证同一套协议假设。如果 fake server 被复制到多个包，后续维护会出现三类问题：

- 修复漂移：某个 fake server 支持了新的 RESP 形状，另一个没有支持。
- 语义漂移：同一个 Redis 命令在不同 fake server 中返回不同 fixture，测试通过但验证的协议不一致。
- 审查噪音：每次新增场景都要重复读 transport 代码，真正的测试意图被样板代码淹没。

共享 helper 把这些风险压到一个位置。review 时只需要确认 handler 表达了测试意图，transport 行为由同一份实现负责。

## 4. 何时适用

适用：

- 多个包都需要模拟同一网络协议或同一外部服务，例如 Redis RESP、HTTP API、coordinator store。
- fake server 的连接、编解码、命令记录逻辑相同，差异只在每个测试期望的响应。
- 这些测试服务于兼容性验证，未来协议形状可能继续扩展。
- 重复代码已经开始出现“不同名字、同一实现”的迹象。

不适用：

- 测试只在单个包内使用，抽出去会制造额外 import 边界。
- fake server 需要访问包内未导出的状态，抽共享 helper 会迫使生产代码暴露接口。
- 每个测试的 transport 行为本身不同，例如需要特殊断连、半包、乱序或超时模拟；这种情况可以在共享 helper 上增可选 hook，而不是先抽象完整框架。

## 5. 示例

本次 Redis 8 Go component adapters 的最终形态：

- `pkg/proxy/redis/redistest.Server`：负责 TCP listen、RESP decode/encode、命令 deep copy、`CountCommand`、默认 `OK` 返回。
- `redistest.Array` / `Bulk` / `Int` / `String` / `Error` / `OK`：统一构造测试 RESP。
- `pkg/utils/redis/client_test.go`：只表达 Redis client parser 的期望，例如 `InfoFull()`、`SlotsInfo()`、`MigrateSlot()`、`SetMaster()`。
- `pkg/proxy/redis8_adapters_test.go`：只表达 proxy backend、session dispatch 和 exec wrapper 的期望。
- `pkg/topom/topom_stats_test.go`：保留包内 fake server，但新增 handler 覆盖模式，让 Redis 8-specific 行为由测试局部闭包表达，而不是塞进 `MigrationRemaining` / `KeyspaceInfo` 这类通用字段。

一个可复用的 review 检查点：看到两个测试包里都有 `net.Listen("tcp", "127.0.0.1:0")` + RESP decode/encode + handler callback + command recording 时，先问它们是不是同一个测试基础设施。如果是，应该抽共享 helper；如果不是，差异必须能说清楚。
