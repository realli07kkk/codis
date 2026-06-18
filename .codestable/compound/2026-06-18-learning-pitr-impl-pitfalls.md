---
doc_type: learning
track: pitfall
slug: pitr-impl-pitfalls
status: current
created: 2026-06-18
source: feature/2026-06-18-pitr-server-flashback
component: [topom, admin, redis-utils, testing]
tags: [pitr, docopt, shutdown, exec, testing, darwin]
severity: high
---

# PITR 实现踩到的三个可复用坑

来源：`2026-06-18-pitr-server-flashback` 的 impl + 两轮 review。三个坑分属 CLI 库语义、Redis 协议、测试环境，但都是"实现带副作用的编排逻辑时会再撞一次"的类型，收进一份便于检索。

---

## 坑 1：docopt-go 的 absent boolean option 是 `false (bool)` 不是 `nil`

### 现象

`codis-admin` 新增 `--pitr-get` / `--pitr-cancel` / `--pitr-remove` flag 后，dispatch 用 `case d["--pitr-get"] != nil` 判断。结果默认的 `--dashboard=ADDR`（overview）路径落进了 PITR handler，然后因为缺 `--job` 报错——**所有不带 pitr flag 的 dashboard 命令都被截走**。

### 试过但没用

- 一开始以为是 dispatch case 顺序问题，调整 fallthrough 顺序没用——根因不在顺序。
- 怀疑 docopt usage grammar 写错（`--pitr-get --job=ID` 没标 required），但 grammar 本身能解析，问题在 dispatch 的类型判断。

### 真正原因

docopt-go 把**未出现的 boolean option 也放进返回 map，值为 `false`（bool 类型），不是 nil**。用 `!= nil` 判断永远为真，所以 default overview 路径命中了第一个 pitr case。

来源验证：docopt-go 文档 + 实测 `docopt.Parse(usage, ["--dashboard=ADDR"])` 返回 `--pitr-get = false (type bool)`。

### 下次怎么更早发现

- **dispatch 用 `.(bool)` 而非 `!= nil`**——boolean flag 永远是 bool 类型，type assertion 不会对 absent flag 误判。
- **写 admin dispatch 测试**：用真实 `docopt.Parse(adminUsage, argv)` 跑 overview 路径，断言所有子命令 flag 是 `false (bool)`。本次把 `adminUsage` 从 `func main()` 的 `const` 提升为包级 `var` 才让测试能引用它——这个提升是值得的固定动作。
- grep `cmd/admin/*.go` 里 `!= nil` 判 docopt flag 的写法本身就是 smell。

---

## 坑 2：`SHUTDOWN` 后的 error 无法区分 transport-close 和 redis-command-error，必须后置探活

### 现象

PITR 状态机的 shutdown step 最初写成"发送 `SHUTDOWN NOSAVE`，任何 error 都视为 server 已停（连接断开=成功）"。review 指出：如果 Redis 返回 NOAUTH / ERR unknown command 这类**command error**，server 其实还在线，后续会 snapshot/truncate 一个正在写的 AOF——**数据损坏**。

### 试过但没用

- 想用 go-redis 的 error 类型区分（`goredis.Error` = command error vs 其他 = transport）。但 `pkg/utils/redis` 的 `isRedisCommandError` 是**未导出**的，跨包用不了。
- 即便能区分 error 类型，仍然不可靠：网络层的 reset/EOF 在不同 go-redis 版本表现不一。

### 真正原因

`SHUTDOWN` 的语义是"server 收到后退出并关闭连接"，但从 client 视角，`Do("SHUTDOWN")` 返回的 error 既可能是"server 拒绝了命令（还活着）"，也可能是"server 退出导致连接断（已停）"——**两者在 error 层面无法可靠区分**。

### 解法（后置探活）

发送 SHUTDOWN 后，**用一条全新的连接做 PING 探活**：
- 能 PING 通 → server 还活着，SHUTDOWN 失败，job 直接 failed，绝不进 snapshot/truncate。
- 连不上 → server 已停，继续。

这是唯一可靠的判断方式，不依赖 error 类型分类，跨 go-redis 版本稳定。

```go
// 发送 SHUTDOWN（error 不可靠，忽略具体类型）
_, _ = c.Do("SHUTDOWN", "NOSAVE")
// 用新连接探活，server 还能答 → 失败
if serverStillUp(addr) { return errors.New("shutdown failed: server still up") }
```

### 下次怎么更早发现

- **任何"发命令让远端进程退出"的逻辑都要后置探活**——error 不可靠是这类操作的固有性质，不是 go-redis 的 bug。
- review 时看到"shutdown 后假定成功继续"的代码就该警觉，无论 error 怎么处理。

---

## 坑 3：darwin 上 `/bin/true` `/bin/false` 路径不固定，写 exec 测试要 LookPath

### 现象

PITR 的 truncate step 测试需要一个"exit 0 无副作用"的 stand-in for `redis-check-aof`，用了 `/bin/true`。在开发机（darwin）上 `exec.Command("/bin/true")` 报 `fork/exec /bin/true: no such file or directory`——**darwin 上 `/bin/true` 不一定存在**（实际在 `/usr/bin/true`，或根本不在标准路径）。

### 试过但没用

- 改成 `/usr/bin/true`——换台机器又可能不一样。
- 用 `exec.LookPath("true")`——能解决，但每个测试都写一遍啰嗦。

### 真正原因

darwin 和 linux 的标准 binary 路径布局不同；`/bin/true` 是 linux 习惯，darwin 上 `/bin` 很精简。本仓库开发平台是 darwin（`attention.md` 标注），但生产部署是 linux，测试要在 darwin 上跑得通。

### 解法（helper + 多路径 fallback）

写个 `trueBin(t)` / `falseBin(t)` helper：先试 `/usr/bin/true`、`/bin/true`，都没有就生成一个 `#!/bin/sh\nexit 0` 临时脚本。测试用 `m.redisCheckBin = trueBin(t)`。

```go
func trueBin(t *testing.T) string {
    for _, cand := range []string{"/usr/bin/true", "/bin/true"} {
        if st, err := os.Stat(cand); err == nil && !st.IsDir() { return cand }
    }
    p := filepath.Join(t.TempDir(), "true.sh")
    os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0755)
    return p
}
```

### 下次怎么更早发现

- **在 darwin 上写涉及外部 binary 的测试，永远不要硬编码 `/bin/xxx` 路径**。用 `exec.LookPath` 或多路径 fallback helper。
- 这条已登记为 attention.md 候选（见本次 accept 报告第 8 节）。
