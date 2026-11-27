# Codis Tutorial

Codis shards Redis data across backend groups. Applications connect to a Codis Proxy, which routes commands to the backend. Review the [proxy command compatibility list](unsupported_cmds.md) before moving a workload.

## Version boundary

The current mainline supports only an externally deployed etcd 3.7.x as the coordinator: the control-plane topology and target configuration live in etcd, while business keys stay in codis-server. The Go dependency is pinned to `go.etcd.io/etcd/client/v3 v3.7.4`. The new binaries contain no legacy drivers, no fallback switch, no dual write, and no legacy proxy registration protocol. Explicitly using a removed CLI/TOML option fails before any connection is attempted and names the last supporting commit, without echoing the supplied value.

The ZooKeeper, Consul and filesystem coordinators and the Jodis discovery mechanism were last supported at commit `c635ade0424a1448fb251f32b88d64ca7cf9a555`. Check out that SHA and follow its build guide (`make`) if you need them. See the [etcd 3.7 release notes](etcd_3_7_release_notes_zh.md) for the incompatible changes and upgrade notes.

## Components

- **Codis Server** embeds Redis 8.6.3. Codis mode enables 1024 slots and the slot migration commands.
- **Codis Proxy** serves the Redis protocol and applies routing updates from the Dashboard.
- **Codis Dashboard** manages topology, slot migrations, and configuration. It stores its metadata in etcd 3.7.x.
- **Codis Admin** runs management commands and supports coordinator operations such as reading and removing a stale Dashboard lock.
- **Codis FE** manages clusters through Dashboard APIs. It loads a static Dashboard list or discovers Dashboards through etcd.
- **Codis Shake** is the worker the Dashboard launches for [cross-cluster data migration](data_migration_zh.md).

## Getting Codis

### Compile source code

Build from your checkout of this repository. The mainline uses Go modules with `go 1.26.1` and `toolchain go1.27.4` in [go.mod](../go.mod). See the [build guide](build_and_release_zh.md#依赖工具链) for the C/C++ and autotools required by embedded Redis.

```bash
go version
make
```

`make` builds the binaries, including `codis-shake`, copies the FE assets to `bin/` and refreshes the default settings in `config/`. For older binaries from [GitHub Releases](https://github.com/CodisLabs/codis/releases), use the documentation from that release.

## Running Codis Cluster

### Prerequisites: etcd 3.7.x

Codis does not ship or start a coordinator. Deploy etcd 3.7.x outside the cluster first (a single member is enough for a local trial) and make sure its client port is reachable.

```bash
etcd --version
# expected: etcd Version: 3.7.x
```

Before the first cluster is created, run one prefix query on the chosen product namespace. It is a linearizable read; the empty output is the green light:

```bash
etcdctl get --prefix --keys-only /codis3/codis-demo/
```

Choose another product name when the prefix is not empty. This check only confirms the namespace for the deployer; concurrent Dashboards are always serialized by the persistent topom lock (see "Codis Admin").

### Start the demo cluster

Run these commands from the repository root. The demo uses `http://127.0.0.1:2379`, two codis-server instances, and a proxy that registers itself online and is brought online by the Dashboard. It requires ports 2379 (etcd), 6379 and 6479 (codis-server), 9090 (FE), 11080 (proxy admin), 18080 (Dashboard), and 19000 (proxy client) to be available. Keep the shell open so you can use the saved PIDs to stop the demo.

```bash
demo_dir=$(mktemp -d "${TMPDIR:-/tmp}/codis-demo.XXXXXX")
mkdir -p "$demo_dir/redis-a" "$demo_dir/redis-b"

cat > "$demo_dir/etcd.toml" <<EOF
endpoints = ["http://127.0.0.1:2379"]
dial_timeout = "5s"
request_timeout = "5s"
EOF

cat > "$demo_dir/dashboard.toml" <<EOF
coordinator_name = "etcd"
coordinator_addr = "http://127.0.0.1:2379"
admin_addr = "127.0.0.1:18080"
data_migration_workspace = "$demo_dir/migration"
rdb_analysis_workspace = "$demo_dir/rdb"
EOF

cat > "$demo_dir/proxy.toml" <<EOF
admin_addr = "127.0.0.1:11080"
proxy_addr = "127.0.0.1:19000"
etcd_register_online = true
EOF

./bin/codis-dashboard --config="$demo_dir/dashboard.toml" --etcd-config="$demo_dir/etcd.toml" \
  > "$demo_dir/dashboard.log" 2>&1 &
dashboard_pid=$!

./bin/codis-server config/redis.conf --dir "$demo_dir/redis-a" \
  --pidfile "$demo_dir/redis-a.pid" > "$demo_dir/redis-a.log" 2>&1 &
server_a_pid=$!

./bin/codis-server config/redis.conf --dir "$demo_dir/redis-b" --port 6479 \
  --pidfile "$demo_dir/redis-b.pid" > "$demo_dir/redis-b.log" 2>&1 &
server_b_pid=$!

./bin/codis-proxy --config="$demo_dir/proxy.toml" --dashboard=127.0.0.1:18080 \
  --etcd-config="$demo_dir/etcd.toml" > "$demo_dir/proxy.log" 2>&1 &
proxy_pid=$!

./bin/codis-fe --listen=127.0.0.1:9090 --etcd-config="$demo_dir/etcd.toml" \
  > "$demo_dir/fe.log" 2>&1 &
fe_pid=$!
```

`--dashboard` brings the proxy online through the Dashboard, while `--etcd-config` only supplies the etcd connection options; both can be combined. Once the proxy is online and knows its token it registers in the background, so an unreachable etcd never blocks going online. `codis-fe` uses the dynamic source: it enumerates the products under `/codis3` and reads each `topom` for the Dashboard address.

The generated `config/redis.conf` includes `codis-enabled yes`. The example writes the server logs to `$demo_dir/redis-*.log`. If you use `admin/codis-server-admin.sh start` with the default config, the server's standard output is captured in `log/codis-server.out`. The script's stop command expects `/tmp/redis_6379.pid`, which requires an explicit `pidfile` setting.

Wait for the Dashboard and both codis-server instances to become ready before running the next commands (the loop gives up after about 30 seconds):

```bash
for i in $(seq 1 60); do
  ./bin/codis-admin --dashboard=127.0.0.1:18080 --slots-status >/dev/null 2>&1 \
    && ./bin/redis-cli -h 127.0.0.1 -p 6379 PING >/dev/null 2>&1 \
    && ./bin/redis-cli -h 127.0.0.1 -p 6479 PING >/dev/null 2>&1 && break
  sleep 0.5
done
```

### Create two groups and initialize slots

Open `http://127.0.0.1:9090/` and select `codis-demo`. Check that the proxy is online, create Group 1 with `127.0.0.1:6379` and Group 2 with `127.0.0.1:6479` under `Group 与 Server`, then in `Slots 分片` choose `自动 Rebalance`, review the plan, and confirm it. You can also use the CLI once the Dashboard and Redis are ready.

```bash
./bin/codis-admin --dashboard=127.0.0.1:18080 --create-group --gid=1
./bin/codis-admin --dashboard=127.0.0.1:18080 --group-add --gid=1 --addr=127.0.0.1:6379
./bin/codis-admin --dashboard=127.0.0.1:18080 --create-group --gid=2
./bin/codis-admin --dashboard=127.0.0.1:18080 --group-add --gid=2 --addr=127.0.0.1:6479
./bin/codis-admin --dashboard=127.0.0.1:18080 --rebalance
./bin/codis-admin --dashboard=127.0.0.1:18080 --rebalance --confirm
```

The Dashboard processes slot actions in the background after you confirm Rebalance. Wait for all 1024 slots to be assigned to the two groups (512 each) and for the actions to finish in the Slots page. With the CLI, run `./bin/codis-admin --dashboard=127.0.0.1:18080 --slots-status` and check that each slot has `backend_addr_group_id` set to 1 or 2 and none has `locked: true`. The proxy needs a moment to apply the new slot mapping; retry once if an immediate write or read fails.

Then verify that both groups serve data. Use `SLOTSHASHKEY` to find two keys that map to different groups (their group can be read from `--slots-status`), then confirm both `SET` calls return `OK` and both `GET` calls return `ok`; finally read the keys directly from the owning group's server port to confirm the placement — the other server returns nothing.

```bash
./bin/redis-cli -h 127.0.0.1 -p 19000 SLOTSHASHKEY codis-demo:key-a codis-demo:key-b
./bin/redis-cli -h 127.0.0.1 -p 19000 SET codis-demo:key-a ok
./bin/redis-cli -h 127.0.0.1 -p 19000 SET codis-demo:key-b ok
./bin/redis-cli -h 127.0.0.1 -p 19000 GET codis-demo:key-a
./bin/redis-cli -h 127.0.0.1 -p 19000 GET codis-demo:key-b
```

### Stop the demo

```bash
kill "$fe_pid" "$proxy_pid" "$dashboard_pid" "$server_b_pid" "$server_a_pid"
wait "$fe_pid" "$proxy_pid" "$dashboard_pid" "$server_b_pid" "$server_a_pid"
```

After the processes exit, you can remove the temporary configuration, logs, and data in `$demo_dir`. A demo product namespace can be deleted as well (demo environments only):

```bash
etcdctl del --prefix /codis3/codis-demo/
```

## Configuration and deployment

### etcd connection options (shared by four components)

Dashboard, FE, Admin and Proxy share the same connection options:

- `--etcd=URL[,URL...]`: one or more HTTP(S) client URLs of the same cluster, comma separated. Empty entries, embedded credentials, file paths and mixed HTTP/HTTPS are rejected.
- `--etcd-config=FILE`: a standalone TOML connection file, parsed with the same rules by all four programs; each role can use its own credentials.

Top-level fields of the connection file:

| Field | Type and default | Rules |
|---|---|---|
| `endpoints` | string array; when absent, the address precedence applies | one or more HTTP(S) client URLs of the same cluster |
| `dial_timeout` | Go duration, default `"5s"` | used by the connection preflight and for fetching the auth token at construction time; the first lease renewal wait is this value + 1s; it does not bound the TCP dial |
| `request_timeout` | Go duration, default `"5s"` | total budget of one KV/Txn call, including the SDK retries inside that call |
| `username`, `password` | strings, default empty | set both or neither; a file holding a password is readable only by the runtime account, and the password must never appear in CLI, URLs, logs or config dumps |
| `ca_file`, `cert_file`, `key_file` | file paths, default empty | HTTPS uses the system trust store or an explicit CA and verifies the host name; the client certificate and key come in pairs; relative paths resolve against the connection file's directory |

Addresses resolve in this order: explicit `--etcd` → connection file `endpoints` → the Dashboard's `coordinator_addr` → `http://127.0.0.1:2379`. Every other connection field comes from the file or its default. An explicit empty address or an invalid file fails before a connection is attempted; nothing falls back silently.

Production HTTPS/mTLS connection file (each role uses its own certificates):

```toml
endpoints = ["https://etcd-a:2379", "https://etcd-b:2379", "https://etcd-c:2379"]
ca_file = "tls/ca.pem"
cert_file = "tls/client.pem"
key_file = "tls/client-key.pem"
```

Role credentials: the Dashboard reads and writes this product's persistent metadata; a registering Proxy writes its own online key; a dynamically discovering FE enumerates `/codis3` across products and reads the Dashboard addresses, so it only needs the read access that discovery requires (use a static FE when products must be strictly isolated); Admin is authorized per query or change.

Connecting runs a bounded preflight: all endpoints share one deadline (`dial_timeout`) and any reachable endpoint passes. TLS handshake, certificate validation or authentication failures return the reason (redacted) rather than a bare timeout. Entries that depend on etcd prove their role permissions with the first real operation: the Dashboard with the actual lock acquisition, the FE with the first discovery, Admin with its first read. The four commands are:

```bash
codis-dashboard --config=dashboard.toml --etcd-config=etcd.toml
codis-fe --listen=127.0.0.1:9090 --etcd-config=etcd.toml
codis-admin --dashboard-list --etcd-config=etcd.toml
codis-proxy --config=proxy.toml --dashboard=127.0.0.1:18080 --etcd-config=etcd.toml
```

### Codis Dashboard

Run each binary with `--help` for its startup flags. The Dashboard and Proxy support `--default-config`; regenerate the defaults with `./bin/codis-dashboard --default-config`. See [dashboard.toml](../config/dashboard.toml) and [proxy.toml](../config/proxy.toml) for the full defaults.

The Dashboard always uses etcd: `coordinator_name` defaults to and only accepts `"etcd"`, `coordinator_addr` is only an address fallback, and credentials plus TLS material belong in the connection file. The old `coordinator_auth` was removed and is rejected when present.

```bash
./bin/codis-dashboard --config=config/dashboard.toml
```

On startup the Dashboard acquires the product's persistent lock `/codis3/{product}/topom` (no lease attached, value carries a per-process random `owner_id`); admin writes and background topology work start only after that. On a conflict it logs the holder's `admin_addr`/`owner_id`/`create_revision` and retries every 2 seconds (about 16 attempts); a permission or authentication refusal exits immediately; an unknown result retries and reads the lock back instead of interpreting an unreadable state as a free lock. A normal stop releases the lock with the revision this process acquired; an abnormal exit leaves the lock for the operator to remove (see "Codis Admin" and the [FAQ](FAQ_en.md)). The Dashboard no longer offers `--remove-lock`.

Set matching `product_name` and `product_auth` on the Dashboard and Proxy. Configure client passwords through `session_auth` or [Codis ACL](codis_acl_zh.md), and backend service credentials through `backend_auth_username` and `backend_auth_password`.

### Codis Proxy

`admin_addr` is the admin HTTP address (default port 11080); `proxy_addr` is the client address (default port 19000). Automatic online entries are `--dashboard=ADDR` (through the Dashboard), `--etcd` (through the Dashboard address published in etcd) and `--fillslots=FILE`; at most one of them may be given. With none of them the proxy waits for the Dashboard to add it manually on the `Proxy 实例` page. The proxy routes client requests only after slot sync and going online.

```bash
./bin/codis-admin --dashboard=127.0.0.1:18080 --create-proxy --addr=127.0.0.1:11080
```

The TOML gains `etcd_register_online` (default `false`): after the Dashboard adds the proxy and it knows its token, it connects lazily in the background and publishes its online record; failures are retried with a bounded backoff and never block going online. `cluster_nodes_compat` stays `disabled`/`self`/`all` (`all` requires registration), and `cluster_nodes_refresh_period` is kept (default `30s`). See "Proxy availability and discovery" for the record and `CLUSTER NODES` semantics.

### Codis Server

```bash
./bin/codis-server config/redis.conf
```

Your own configuration must also include `codis-enabled yes`. codis-server does not depend on etcd.

### Codis FE (optional)

`--listen` is required. The Dashboard list has exactly one source: the static `--dashboard-list=FILE` or the dynamic etcd source, selected by giving `--etcd` or `--etcd-config` (giving both counts as one source). The dynamic source enumerates the products under `/codis3` and reads each `topom`, so the FE account needs those read permissions.

```bash
./bin/codis-admin --dashboard-list --etcd-config=etcd.toml > codis.json
./bin/codis-fe --dashboard-list=codis.json --listen=127.0.0.1:9090
```

The first dynamic refresh doubles as the connection preflight: a permission/authentication refusal exits immediately with the reason, while connectivity errors are retried 3 times 2 seconds apart before exiting. Afterwards a failed refresh keeps the last successful snapshot.

### Codis Admin

Normal topology operations go through the Dashboard API and keep their previous usage. Commands that access metadata directly require at least one shared option (`--etcd` or `--etcd-config`):

- `--show-lock --product=NAME` prints the product lock as JSON (at least `product`, `admin_addr`, `owner_id`, `create_revision`, plus `mod_revision` and `lease`); it prints `null` and exits successfully when there is no lock.
- `--remove-lock --product=NAME --expected-revision=N --confirm` deletes only that revision in a transaction. A missing revision, a non-positive revision or a missing `--confirm` fails; an absent lock, a revision mismatch, an unknown result or a refused request all exit non-zero with distinct messages, and a replacement lock found on read-back is always kept.
- `--dashboard-list` lists the Dashboard address of every product under `/codis3` (redirect it into a static FE list); `--config-dump` dumps this product's metadata and only emits the Codis 3.x format (the old `-1`, which read a Codis 1.x/ZK tree, was removed).
- `--config-restore=FILE --product=NAME --confirm` submits the file into the product namespace; without `--confirm` it only prints it for debugging.
- `--config-convert FILE` is a pure file conversion and connects to nothing.

An abnormal exit leaves the persistent lock behind. Isolate the old process and its auto-restart first, read the lock, then remove it with the revision you observed:

```bash
./bin/codis-admin --etcd-config=etcd.toml --product=codis-demo --show-lock
./bin/codis-admin --etcd-config=etcd.toml --product=codis-demo --remove-lock --expected-revision=417 --confirm
```

If a proxy exited abnormally and cannot be removed normally, confirm the process is gone and force-remove it from the Dashboard.

```bash
./bin/codis-admin --dashboard=127.0.0.1:18080 --remove-proxy --addr=127.0.0.1:11080 --force
```

### Deploy with Ansible

The playbook in `ansible/` has been adapted to the etcd-based line: set the etcd endpoints in `ansible/group_vars/all` (`etcd_endpoints`); credentials and mTLS are supplied through an `--etcd-config` connection file (see the playbook templates). For current builds, update the Server template using the generated `config/redis.conf` and ensure it includes `codis-enabled yes`. Set addresses and the etcd connection options in `ansible/group_vars/all` and inventory in `ansible/hosts` before running the playbook.

```bash
cd ansible
ansible-playbook -i hosts site.yml
```

### Docker (not adapted)

The repository's `Dockerfile`, `make docker` and `scripts/docker.sh` still package the old line and have not been adapted to etcd 3.7: their coordinator options and default configuration are unchanged, so starting them produces the removed-option error. Until that work is done, deploy with the `bin/` binaries or write your own image and supply `--etcd` / `--etcd-config` as described under "Configuration and deployment". The status is recorded in the [release notes](etcd_3_7_release_notes_zh.md).

## Proxy availability and discovery

A product can have multiple proxies, kept in sync by the Dashboard through its admin API.

### Online registration

With `etcd_register_online = true`, a proxy that is added (so its token is known) and online connects to etcd lazily in the background and maintains its online record:

- the key is `/codis3/{product}/online-proxies/{token}` and the value is JSON with `token` and `addr` (the address clients use to reach the proxy);
- each online instance grants its own lease with a fixed 30-second TTL; there are no extra tuning options. Retries of the same registration round reuse that lease and bind the key to it with a version-0 compare-and-swap;
- if the same token is already held by another lease, the record is neither overwritten nor claimed; wait for that process to stop or the lease to expire;
- registration failures retry in the background with a bounded backoff and never hold up going online;
- shutdown order: stop and wait for this instance's registration loop → revoke this instance's lease with a bounded call → close the client. A failed revoke (or a crash) is cleaned up by the server-side TTL; keys are never deleted by token.

In `all` mode each refresh cycle does one consistent range read of the online prefix and reuses the existing self-completion, deduplication, sorting and pseudo-slot assignment, skipping corrupt records. The first failed read serves only the proxy itself, later failures keep the last successful snapshot, and a successful empty list serves only itself. After the server-side key disappears, the peer is dropped no later than the next successful refresh, so the stale window is bounded by the lease TTL (30 seconds) plus one refresh period.

### CLUSTER NODES

`cluster_nodes_compat` combines with the registration switch as follows. It is not the Jodis protocol: the internal online records only serve Codis' own discovery, and ordinary Redis requests still read the local routing table.

| Mode | Registration off | Registration on |
|---|---|---|
| `disabled` | the disabled response, no discovery connection | publishes itself, other proxies can discover it; this proxy still disables the command |
| `self` | returns only itself, no discovery connection | publishes itself, this proxy still returns only itself |
| `all` | configuration validation fails and asks for registration | publishes itself, polls the product's online list and builds the response |

### Legacy note (≤ `c635ade0424a1448fb251f32b88d64ca7cf9a555` only)

Jodis was the discovery mechanism of the old line: clients read `/jodis/{product}/proxy-{token}` registrations from ZooKeeper (or emulated ephemeral nodes on Consul) to find proxies, configured by `jodis_name`, `jodis_addr`, `jodis_auth`, `jodis_timeout` and `jodis_compatible`, while `models.Proxy` carried a `jodis_path` field. The new line removes that registration protocol, the `jodis_*` options and the model field, replacing them with the etcd online registration above; existing Jodis clients and deployments should build that SHA. Likewise the old `--zookeeper` / `--filesystem` / `--consul` and `coordinator_auth` are replaced by the etcd connection options and are rejected with the same SHA.

### Redis backend HA

Redis Sentinel can perform backend failover. The Dashboard and proxies follow the detected master. The optional `codis-ha` tool polls the Dashboard API and can promote a replica. After a manual or `codis-ha` promotion, check the replication settings and progress of the rest of the group.

## Upgrading from Codis 2.x (old line only)

The historical Codis 2.x metadata upgrade reads the Codis 1.x ZooKeeper tree with `--config-dump -1`, which the new line removed, so that flow only applies to tools built from `c635ade0424a1448fb251f32b88d64ca7cf9a555`. The new line does not migrate existing metadata and makes no promise about in-place upgrades or cutover; create a new cluster as described above and move data with the [data migration console](data_migration_zh.md). The historical steps remain in the [Chinese tutorial](tutorial_zh.md#5-从codis-2x-升级仅旧线). For current Redis 8 deployments, see the [validation and cutover guide](redis8_validation_zh.md).
