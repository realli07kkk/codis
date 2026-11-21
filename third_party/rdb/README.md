# RDB parser fork

This directory is a minimal fork of `github.com/hdt3213/rdb v1.3.2` (MIT,
see `LICENSE`), vendored through the module replace in the repository root.

It adds Redis 8.6 RDB compatibility on top of the upstream v1.3.2 decoder:

- accept RDB version 13;
- decode `RDB_TYPE_STREAM_LISTPACKS_4` (type 26) and consume the IDMP metadata
  appended by Redis 8.6;
- skip Redis 8 `RDB_OPCODE_KEY_META` (243) module metadata;
- skip `RDB_OPCODE_SLOT_INFO` (244) in Redis RDBs as well as Valkey RDBs;
- expose stream element count for RDB analysis.

The package surface used by Codis remains `github.com/hdt3213/rdb/parser` and
`github.com/hdt3213/rdb/model`. The upstream project is
https://github.com/hdt3213/rdb.
