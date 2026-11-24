# Codis FAQ

### What is Codis?

Codis uses proxies to route Redis requests across backend groups. The Dashboard manages topology and slot migration. Backend Redis instances provide persistence and replication.

### Which coordinator backends does Codis support?

ZooKeeper, Consul, and the filesystem. Choose a backend with `coordinator_name`. The generated Dashboard config uses the filesystem by default. See the [tutorial](tutorial_en.md).

### Can I move an existing Redis or Twemproxy workload to Codis?

Check the application's Redis commands, multi-key operations, and authentication against the [proxy compatibility list](unsupported_cmds.md). For example, `CLIENT LIST` and `SLOTSINFO <backend-addr>` are available, while transactions and Pub/Sub subscription commands are restricted.

Use the FE Data Integration page or Dashboard API to create a [data migration task](data_migration_zh.md). The Dashboard launches a `codis-shake` worker that connects directly to the source and target. Check the task exit status and verify target data before switching clients.

### Where are the performance and validation results?

The [historical benchmark](benchmark.md) describes early Codis versions. The [Linux validation index](validation/README.md) tracks the current Redis 8 validation and performance baseline.
