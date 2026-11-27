# Codis FAQ

### What is Codis?

Codis uses proxies to route Redis requests across backend groups. The Dashboard manages topology and slot migration. Backend Redis instances provide persistence and replication.

### Which coordinator backends does Codis support?

Only an externally deployed etcd 3.7.x. The control plane (topology, target configuration, the topom lock and the proxy online records) lives in etcd, while business keys stay in codis-server. All four components share `--etcd=URL[,URL...]` or `--etcd-config=FILE` (credentials, CA and mTLS certificates belong in the connection file), and the Dashboard's `coordinator_name` defaults to and only accepts `"etcd"` while `coordinator_addr` is only an address fallback. See the [tutorial](tutorial_en.md).

The ZooKeeper, Consul and filesystem coordinators were last supported at commit `c635ade0424a1448fb251f32b88d64ca7cf9a555`; check that out and build it if you need them. The new line has no legacy drivers, no dual write and no fallback switch, and explicitly using a removed option fails with that SHA. See the [etcd 3.7 release notes](etcd_3_7_release_notes_zh.md).

### Why does the Dashboard stop with "the topom lock is held by another instance"?

The lock at `/codis3/{product}/topom` is persistent (no lease attached) and only one Dashboard per product may hold it. On a conflict the log names the holder's `admin_addr`, `owner_id` and `create_revision`, and the Dashboard retries every 2 seconds (about 16 attempts). Check whether that Dashboard is still running: stop it normally and it releases the lock with the revision it acquired.

### How do I remove a lock left by a crashed Dashboard?

Isolate the old process and its auto-restart first, then read and remove the lock with the revision you observed:

```bash
codis-admin --etcd-config=etcd.toml --product=codis-demo --show-lock
codis-admin --etcd-config=etcd.toml --product=codis-demo --remove-lock --expected-revision=417 --confirm
```

`--show-lock` prints JSON (`product`, `admin_addr`, `owner_id`, `create_revision`, ...) or `null` when there is no lock. `--remove-lock` deletes only the given revision in a transaction: a missing or non-positive revision or a missing `--confirm` fails, and an absent lock, a revision mismatch, an unknown result or a refused request exit non-zero with distinct messages; a replacement lock found on read-back is kept. The Dashboard itself no longer offers `--remove-lock`.

### Does a short etcd outage make the proxy exit?

No. With `etcd_register_online` the proxy connects to etcd lazily in the background only after it is online and knows its token; failed registrations are retried with a bounded backoff, and going online plus request routing never wait for them. Slot routing is pushed by the Dashboard through its admin API and does not depend on etcd. A crashed proxy's online record disappears when its lease TTL (a fixed 30 seconds) expires, and `cluster_nodes_compat = "all"` drops the peer no later than the next successful refresh; that registration is not the Jodis protocol, which only applied to the old line (≤ `c635ade0424a1448fb251f32b88d64ca7cf9a555`).

Startup failures do exit: `--dashboard` auto-online tries 10 times 3 seconds apart and then prints `online proxy failed`, while `--etcd` auto-online gives up after 30 attempts and the Dashboard gives up after about 16 lock-conflict retries.

### Can I move an existing Redis or Twemproxy workload to Codis?

Check the application's Redis commands, multi-key operations, and authentication against the [proxy compatibility list](unsupported_cmds.md). For example, `CLIENT LIST` and `SLOTSINFO <backend-addr>` are available, while transactions and Pub/Sub subscription commands are restricted.

Use the FE Data Integration page or Dashboard API to create a [data migration task](data_migration_zh.md). The Dashboard launches a `codis-shake` worker that connects directly to the source and target. Check the task exit status and verify target data before switching clients.

### Where are the performance and validation results?

The [historical benchmark](benchmark.md) describes early Codis versions. The [Linux validation index](validation/README.md) tracks the current Redis 8 validation and performance baseline.
