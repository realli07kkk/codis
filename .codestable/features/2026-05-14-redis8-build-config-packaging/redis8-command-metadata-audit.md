# Redis 8 Codis command metadata audit

## 1. 结论

- Redis 8 Codis command metadata 已以 `extern/redis-8.6.3/src/commands/*.json` 为源。
- `extern/redis-8.6.3/src/commands.def` 当前由生成器同步，执行 `make -C extern/redis-8.6.3/src commands.def` 返回 up to date。
- 本轮审计未发现需要在 packaging 阶段修正的 metadata；不改命令协议和命令实现。

## 2. 命令清单

| 命令 | arity | 函数 | flags | ACL | JSON |
|---|---:|---|---|---|---|
| `SLOTSCHECK` | 1 | `slotscheckCommand` | `READONLY` | `KEYSPACE` | `slotscheck.json` |
| `SLOTSDEL` | -2 | `slotsdelCommand` | `WRITE` | `KEYSPACE` | `slotsdel.json` |
| `SLOTSHASHKEY` | -1 | `slotshashkeyCommand` | `READONLY,FAST` | `KEYSPACE` | `slotshashkey.json` |
| `SLOTSINFO` | -1 | `slotsinfoCommand` | `READONLY,FAST` | `KEYSPACE` | `slotsinfo.json` |
| `SLOTSMGRT-ASYNC-CANCEL` | 1 | `slotsmgrtAsyncCancelCommand` | `FAST` | `KEYSPACE,DANGEROUS` | `slotsmgrt-async-cancel.json` |
| `SLOTSMGRT-ASYNC-FENCE` | 1 | `slotsmgrtAsyncFenceCommand` | `READONLY,NOSCRIPT,BLOCKING` | `KEYSPACE,DANGEROUS` | `slotsmgrt-async-fence.json` |
| `SLOTSMGRT-ASYNC-STATUS` | 1 | `slotsmgrtAsyncStatusCommand` | `FAST` | `KEYSPACE,DANGEROUS` | `slotsmgrt-async-status.json` |
| `SLOTSMGRT-EXEC-WRAPPER` | -3 | `slotsmgrtExecWrapperCommand` | `WRITE,DENYOOM` | `KEYSPACE,DANGEROUS` | `slotsmgrt-exec-wrapper.json` |
| `SLOTSMGRTONE-ASYNC-DUMP` | -4 | `slotsmgrtOneAsyncDumpCommand` | `READONLY,DENYOOM` | `KEYSPACE,DANGEROUS` | `slotsmgrtone-async-dump.json` |
| `SLOTSMGRTONE-ASYNC` | -7 | `slotsmgrtOneAsyncCommand` | `WRITE,NOSCRIPT,BLOCKING` | `KEYSPACE,DANGEROUS` | `slotsmgrtone-async.json` |
| `SLOTSMGRTONE` | 5 | `slotsmgrtoneCommand` | `WRITE` | `KEYSPACE,DANGEROUS` | `slotsmgrtone.json` |
| `SLOTSMGRTSLOT-ASYNC` | 8 | `slotsmgrtSlotAsyncCommand` | `WRITE,NOSCRIPT,BLOCKING` | `KEYSPACE,DANGEROUS` | `slotsmgrtslot-async.json` |
| `SLOTSMGRTSLOT` | 5 | `slotsmgrtslotCommand` | `WRITE` | `KEYSPACE,DANGEROUS` | `slotsmgrtslot.json` |
| `SLOTSMGRTTAGONE-ASYNC-DUMP` | -4 | `slotsmgrtTagOneAsyncDumpCommand` | `READONLY,DENYOOM` | `KEYSPACE,DANGEROUS` | `slotsmgrttagone-async-dump.json` |
| `SLOTSMGRTTAGONE-ASYNC` | -7 | `slotsmgrtTagOneAsyncCommand` | `WRITE,NOSCRIPT,BLOCKING` | `KEYSPACE,DANGEROUS` | `slotsmgrttagone-async.json` |
| `SLOTSMGRTTAGONE` | 5 | `slotsmgrttagoneCommand` | `WRITE` | `KEYSPACE,DANGEROUS` | `slotsmgrttagone.json` |
| `SLOTSMGRTTAGSLOT-ASYNC` | 8 | `slotsmgrtTagSlotAsyncCommand` | `WRITE,NOSCRIPT,BLOCKING` | `KEYSPACE,DANGEROUS` | `slotsmgrttagslot-async.json` |
| `SLOTSMGRTTAGSLOT` | 5 | `slotsmgrttagslotCommand` | `WRITE` | `KEYSPACE,DANGEROUS` | `slotsmgrttagslot.json` |
| `SLOTSRESTORE-ASYNC-ACK` | 3 | `slotsrestoreAsyncAckCommand` | `WRITE` | `KEYSPACE,DANGEROUS` | `slotsrestore-async-ack.json` |
| `SLOTSRESTORE-ASYNC-AUTH` | 2 | `slotsrestoreAsyncAuthCommand` | `NOSCRIPT,LOADING,STALE,FAST,NO_AUTH` | `CONNECTION` | `slotsrestore-async-auth.json` |
| `SLOTSRESTORE-ASYNC-AUTH2` | 3 | `slotsrestoreAsyncAuth2Command` | `NOSCRIPT,LOADING,STALE,FAST,NO_AUTH` | `CONNECTION` | `slotsrestore-async-auth2.json` |
| `SLOTSRESTORE-ASYNC-SELECT` | 2 | `slotsrestoreAsyncSelectCommand` | `LOADING,STALE,FAST` | `CONNECTION` | `slotsrestore-async-select.json` |
| `SLOTSRESTORE-ASYNC` | -2 | `slotsrestoreAsyncCommand` | `WRITE,DENYOOM` | `KEYSPACE,DANGEROUS` | `slotsrestore-async.json` |
| `SLOTSRESTORE` | -4 | `slotsrestoreCommand` | `WRITE,DENYOOM` | `KEYSPACE,DANGEROUS` | `slotsrestore.json` |
| `SLOTSSCAN` | -3 | `slotsscanCommand` | `READONLY` | `KEYSPACE` | `slotsscan.json` |

## 3. 验证命令

```bash
make -C extern/redis-8.6.3/src commands.def
git diff -- extern/redis-8.6.3/src/commands.def 'extern/redis-8.6.3/src/commands/*.json'
```

结果：`commands.def` up to date，命令 JSON 和 generated file 无 diff。
