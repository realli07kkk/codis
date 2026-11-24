# Codis Tutorial

Codis shards Redis data across backend groups. Applications connect to a Codis Proxy, which routes commands to the backend. Review the [proxy command compatibility list](unsupported_cmds.md) before moving a workload.

- **Codis Server** embeds Redis 8.6.3. Codis mode enables 1024 slots and the slot migration commands.
- **Codis Proxy** serves the Redis protocol and applies routing updates from the Dashboard.
- **Codis Dashboard** manages topology, slot migrations, and configuration. It stores metadata in ZooKeeper, Consul, or the filesystem.
- **Codis Admin** runs management commands and supports coordinator operations such as removing a stale Dashboard lock.
- **Codis FE** manages clusters through Dashboard APIs. It loads a static Dashboard list or discovers Dashboards through a coordinator.
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

Run these commands from the repository root. The sample uses a temporary filesystem coordinator and loopback listeners. It requires ports 6379, 9090, 11080, 18080, and 19000 to be available. Keep the shell open so you can use the saved PIDs to stop the demo.

```bash
demo_dir=$(mktemp -d "${TMPDIR:-/tmp}/codis-demo.XXXXXX")

cat > "$demo_dir/dashboard.toml" <<EOF
coordinator_name = "filesystem"
coordinator_addr = "$demo_dir/coordinator"
admin_addr = "127.0.0.1:18080"
data_migration_workspace = "$demo_dir/migration"
rdb_analysis_workspace = "$demo_dir/rdb"
EOF

cat > "$demo_dir/proxy.toml" <<EOF
admin_addr = "127.0.0.1:11080"
proxy_addr = "127.0.0.1:19000"
EOF

./bin/codis-dashboard --config="$demo_dir/dashboard.toml" \
  > "$demo_dir/dashboard.log" 2>&1 &
dashboard_pid=$!

./bin/codis-server config/redis.conf --dir "$demo_dir" \
  --pidfile "$demo_dir/redis.pid" > "$demo_dir/redis.log" 2>&1 &
server_pid=$!

./bin/codis-proxy --config="$demo_dir/proxy.toml" --dashboard=127.0.0.1:18080 \
  > "$demo_dir/proxy.log" 2>&1 &
proxy_pid=$!

./bin/codis-fe --filesystem="$demo_dir/coordinator" --listen=127.0.0.1:9090 \
  > "$demo_dir/fe.log" 2>&1 &
fe_pid=$!
```

The generated `config/redis.conf` includes `codis-enabled yes`. The example writes the server log to `$demo_dir/redis.log`. If you use `admin/codis-server-admin.sh start` with the default config, the server's standard output is captured in `log/codis-server.out`. The script's stop command expects `/tmp/redis_6379.pid`, which requires an explicit `pidfile` setting.

Wait for the Dashboard and codis-server to become ready before running the next
commands (the loop gives up after about 30 seconds):

```bash
for i in $(seq 1 60); do
  ./bin/codis-admin --dashboard=127.0.0.1:18080 --slots-status >/dev/null 2>&1 \
    && ./bin/redis-cli -h 127.0.0.1 -p 6379 PING >/dev/null 2>&1 && break
  sleep 0.5
done
```

### Add a group and initialize slots

Open `http://127.0.0.1:9090/` and select `codis-demo`. Check that the proxy is online, create Group 1 under `Group 与 Server`, and add `127.0.0.1:6379`. In `Slots 分片`, choose `自动 Rebalance`, review the plan, and confirm it. You can also use the CLI once the Dashboard and Redis are ready.

```bash
./bin/codis-admin --dashboard=127.0.0.1:18080 --create-group --gid=1
./bin/codis-admin --dashboard=127.0.0.1:18080 --group-add --gid=1 --addr=127.0.0.1:6379
./bin/codis-admin --dashboard=127.0.0.1:18080 --rebalance
./bin/codis-admin --dashboard=127.0.0.1:18080 --rebalance --confirm
```

The Dashboard processes slot actions in the background after you confirm Rebalance. Wait for all 1024 slots to be assigned to Group 1 and for the actions to finish in the Slots page. With the CLI, run `./bin/codis-admin --dashboard=127.0.0.1:18080 --slots-status` and check that each slot has `backend_addr_group_id` set to 1 and none has `locked: true`. The proxy needs a moment to apply the new slot mapping; retry once if an immediate write or read fails. Then verify writes and reads through port 19000. The expected responses are `OK` and `ok`.

```bash
./bin/redis-cli -h 127.0.0.1 -p 19000 SET codis-demo:key ok
./bin/redis-cli -h 127.0.0.1 -p 19000 GET codis-demo:key
```

### Stop the demo

```bash
kill "$fe_pid" "$proxy_pid" "$dashboard_pid" "$server_pid"
wait "$fe_pid" "$proxy_pid" "$dashboard_pid" "$server_pid"
```

After the processes exit, you can remove the temporary configuration, logs, and data in `$demo_dir`.

## Configuration and deployment

Run each binary with `--help` for its startup flags. The Dashboard and Proxy support `--default-config`. See [dashboard.toml](../config/dashboard.toml) and [proxy.toml](../config/proxy.toml) for the full defaults. Set matching `product_name` and `product_auth` on the Dashboard and Proxy. Configure client passwords through `session_auth` or [Codis ACL](codis_acl_zh.md), and backend service credentials through `backend_auth_username` and `backend_auth_password`.

### Deploy with Ansible

The playbook in `ansible/` is a legacy example. For current builds, update the Server template using the generated `config/redis.conf` and ensure it includes `codis-enabled yes`. Set addresses and the coordinator in `ansible/group_vars/all` and inventory in `ansible/hosts` before running the playbook.

```bash
cd ansible
ansible-playbook -i hosts site.yml
```

### Run the proxy in Docker

Replace `100.0.1.100` with a host address reachable by the Dashboard and clients.

```bash
make docker
docker run --name Codis-Proxy -d -p 29000:19000 -p 21080:11080 \
  -v "$PWD/config/proxy.toml:/codis/proxy.toml:ro" codis-image \
  codis-proxy --config=/codis/proxy.toml \
  --host-admin=100.0.1.100:21080 --host-proxy=100.0.1.100:29000
```

Register the proxy at `100.0.1.100:21080` and connect Redis clients to `100.0.1.100:29000`. See the [Docker examples](../scripts/docker.sh) for other components.

## HA

A product can have multiple proxies. The Java [Jodis](https://github.com/CodisLabs/jodis) client discovers proxies from ZooKeeper registrations. The proxy registration backend supports ZooKeeper and Consul and defaults to the path `/jodis/{product}/proxy-{token}`.

Redis Sentinel can perform backend failover. The Dashboard and proxies follow the detected master. The optional `codis-ha` tool polls the Dashboard API and can promote a replica. After a manual or `codis-ha` promotion, check the replication settings and progress of the rest of the group.

For the historical Codis 2.x metadata upgrade, see the [Chinese tutorial](tutorial_zh.md#5-从codis-2x-升级). For current Redis 8 deployments, see the [validation and cutover guide](redis8_validation_zh.md).
