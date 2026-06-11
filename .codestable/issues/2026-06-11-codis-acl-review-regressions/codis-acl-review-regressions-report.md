---
doc_type: issue-report
issue: 2026-06-11-codis-acl-review-regressions
status: confirmed
severity: P1
summary: Codis-managed Redis ACL 首版存在并发、运行态同步和授权状态回退问题
tags: [acl, proxy, topom, security, concurrency]
---

# Codis ACL Review Regressions Issue Report

## 1. 问题现象

对 `72125ca7fd2c6af515c2ec5a5bc704de277e8bf9` 的 ACL commit review 发现多处上线前必须修复的问题：

- ACL user-bound backend pool 在多 session 并发访问不同 backend addr 时可能并发写 map 并 panic。
- Redis Server 重启或新 server 加入 group 后，Codis 管理的 ACL 用户不会自动或显式重推，后端运行态 ACL 丢失会导致业务用户和服务账号认证失败。
- ACL 从 enabled 切到 disabled 后，已经 ACL 认证过的 session 会残留 legacy `authorized=true`，可能绕过 `session_auth`。
- 已认证 ACL session re-auth 其他账号失败时不计入 brute-force guard，低权限账号可无限尝试横向爆破其他账号。
- backend service user 通过 `ACL SETUSER >plain-password` 下发，明文会进入目标 Redis 命令流。

## 2. 复现步骤

1. 基于 ACL commit 启动 proxy/topom，开启 `codis_acl_enabled` 并配置 ACL 用户。
2. 多个客户端使用同一 ACL 用户并发访问多个后端 addr。
3. 触发 ACL revision 切换或 Redis Server 重启 / group add server。
4. 使用已认证 ACL session 尝试 disable ACL 后继续访问，或不断 re-auth 其他用户的错误密码。

复现频率：稳定。并发 map 写需要并发调度触发；其余路径按状态转换必现。

## 3. 期望 vs 实际

**期望行为**：ACL user pool 并发安全；Redis 侧 ACL 运行态有可恢复路径；ACL disable 不提升已有受限 session 权限；所有 AUTH 失败都进入暴力破解保护；服务账号下发不暴露明文密码。

**实际行为**：user pool 内部 map 无锁写入；ACL 只在 UpdateACL 时下发；disable ACL 后 legacy authorized 标志残留；ACL re-auth 失败在已认证 session 上不计失败；服务账号使用 `>` 明文 password token 下发。

## 4. 环境信息

- 涉及模块 / 功能：Codis-managed Redis ACL、proxy session auth、proxy backend pool、topom ACL rollout。
- 相关文件 / 函数：`pkg/proxy/backend.go`、`pkg/proxy/router.go`、`pkg/proxy/session_auth.go`、`pkg/topom/topom_acl.go`、`pkg/topom/topom_group.go`、`pkg/topom/topom_api.go`、`cmd/admin/*`。
- 运行环境：本地 dev / code review。
- 其他上下文：`go test ./pkg/proxy ./pkg/topom -run ACL` 在 review 时通过；`-race` 的已知 martini render 模板竞争与本 issue 无关。

## 5. 严重程度

**P1** — ACL 是安全边界能力；并发 panic、运行态 ACL 丢失和授权状态回退都会影响生产启用安全性，但 ACL 默认关闭，未启用集群不受影响。

## 备注

本 issue 只修必须修和生产前必修项。性能优化、连接池容量上限、helper 命名、空文件整理等 review 小项不纳入本次修复范围。
