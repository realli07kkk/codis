#!/usr/bin/env python3
"""Bring up one Codis validation cluster and record it in cluster.json.

Started processes (codis-server / codis-dashboard / codis-proxy / codis-fe)
keep running after this script exits; stop them with
``scripts/validation/env/teardown_cluster.py``.
"""

import argparse
import json
import os
import secrets
import shutil
import sys
import time
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import vlib

REQUIRED_BINARIES = [
    "codis-admin",
    "codis-dashboard",
    "codis-proxy",
    "codis-server",
    "codis-fe",
    "redis-cli",
    "redis-check-aof",
]

SLOT_COUNT = 1024


# ---------------------------------------------------------------------------
# ports
# ---------------------------------------------------------------------------


class PortAllocator(object):
    """Hand out cluster ports, either automatically or from ``--port-base``."""

    def __init__(self, base=0):
        self._next = int(base) if base else 0
        self._used = set()

    def take(self):
        if self._next:
            for _ in range(4000):
                port = self._next
                self._next += 1
                if port in self._used:
                    continue
                if vlib.port_is_open(port):
                    vlib.log("port {} is already in use, skipping".format(port))
                    continue
                self._used.add(port)
                return port
            raise vlib.ValidationError("no free port found above the given --port-base")
        for _ in range(200):
            port = vlib.free_port()
            if port not in self._used:
                self._used.add(port)
                return port
        raise vlib.ValidationError("cannot allocate a free port")


# ---------------------------------------------------------------------------
# helpers
# ---------------------------------------------------------------------------


def admin_cmd(repo, admin_port, *args, timeout=30, check=True, truncate=True):
    command = [
        str(Path(repo) / "bin" / "codis-admin"),
        "--dashboard=127.0.0.1:{}".format(int(admin_port)),
    ]
    command.extend(str(a) for a in args)
    return vlib.run(command, cwd=repo, timeout=timeout, check=check, truncate=truncate)


def wait_redis_pong(host, port, timeout=40):
    deadline = time.time() + timeout
    last = None
    while time.time() < deadline:
        try:
            with vlib.Resp(host, port, timeout=3) as client:
                if client.cmd("PING") == "PONG":
                    return True
        except (OSError, vlib.RedisError) as err:
            last = str(err)
        time.sleep(0.2)
    raise vlib.ValidationError(
        "redis {}:{} did not answer PING within {}s: {}".format(host, port, timeout, last)
    )


def wait_replica_linked(host, port, timeout=90):
    deadline = time.time() + timeout
    last = None
    while time.time() < deadline:
        try:
            with vlib.Resp(host, port, timeout=3) as client:
                info = client.cmd("INFO", "replication")
            link = None
            for line in str(info).splitlines():
                if line.startswith("master_link_status:"):
                    link = line.split(":", 1)[1].strip()
                    break
            if link == "up":
                return True
            last = link
        except (OSError, vlib.RedisError) as err:
            last = str(err)
        time.sleep(0.3)
    raise vlib.ValidationError(
        "replica {}:{} master_link_status stayed {!r} for {}s".format(
            host, port, last, timeout
        )
    )


def wait_dashboard_ready(repo, admin_port, timeout=90):
    deadline = time.time() + timeout
    last = None
    while time.time() < deadline:
        result = admin_cmd(repo, admin_port, "--slots-status", timeout=15, check=False)
        if result["returncode"] == 0:
            return True
        last = result
        time.sleep(0.5)
    raise vlib.ValidationError(
        "codis-dashboard admin 127.0.0.1:{} never answered --slots-status: {}".format(
            admin_port, json.dumps(last, sort_keys=True)[:1000] if last else "no attempt"
        )
    )


def wait_slots_ready(repo, admin_port, timeout=90):
    """Wait until every one of the 1024 slots is assigned and unlocked."""
    deadline = time.time() + timeout
    last = None
    while time.time() < deadline:
        result = admin_cmd(
            repo, admin_port, "--slots-status", timeout=20, check=False, truncate=False
        )
        if result["returncode"] == 0:
            try:
                slots = json.loads(result["stdout"])
            except ValueError:
                slots = None
            if isinstance(slots, list) and len(slots) == SLOT_COUNT:
                bad = [
                    slot
                    for slot in slots
                    if slot.get("locked")
                    or not slot.get("backend_addr_group_id")
                    or slot.get("migrate_from")
                ]
                if not bad:
                    return slots
                last = "{} unassigned/locked slots, e.g. {}".format(
                    len(bad), json.dumps(bad[:2], sort_keys=True)
                )
            else:
                last = "unexpected --slots-status payload"
        else:
            last = "codis-admin exit {}".format(result["returncode"])
        time.sleep(0.5)
    raise vlib.ValidationError("slots never became ready: {}".format(last))


def wait_proxy_ping(host, port, timeout=60):
    """Return ``{"ok": bool, "error": str|None}``; only raises on timeout."""
    deadline = time.time() + timeout
    last = None
    while time.time() < deadline:
        try:
            with vlib.Resp(host, port, timeout=3) as client:
                reply = client.cmd("PING")
            if reply == "PONG":
                return {"ok": True, "error": None}
            last = "unexpected PING reply {!r}".format(reply)
        except vlib.RedisError as err:
            text = str(err)
            if "NOAUTH" in text.upper() or "AUTH" in text.upper():
                # ACL is enabled in the proxy config; unauthenticated PING is
                # refused. Record it instead of changing proxy semantics.
                return {"ok": False, "error": text}
            last = "redis error: {}".format(text)
        except OSError as err:
            last = str(err)
        time.sleep(0.3)
    raise vlib.ValidationError(
        "proxy {}:{} did not answer within {}s: {}".format(host, port, timeout, last)
    )


def _sh_quote(value):
    return "'" + str(value).replace("'", "'\\''") + "'"


def write_pitr_restart_script(path, repo, masters, default_probe="127.0.0.1"):
    """Render the PITR restart hook.

    codis-dashboard executes ``pitr_restart_command`` as ``sh -c <cmd>`` and
    passes no server identity (pkg/topom/topom_pitr_run.go, stepRestart), so the
    hook probes every master port and restarts whichever one is not answering.
    ``nohup ... >> log 2>&1 < /dev/null &`` keeps the new server out of the
    dashboard's output pipe so the dashboard call returns immediately.
    """
    lines = [
        "#!/bin/sh",
        "# Generated by scripts/validation/env/setup_cluster.py -- do not edit.",
        "#",
        "# codis-dashboard runs pitr_restart_command as `sh -c <cmd>` with no",
        "# server identity, so this hook probes every master port and restarts",
        "# the one that stopped answering PING.",
        "set -u",
        "REPO={}".format(_sh_quote(str(repo))),
        'CLI="$REPO/bin/redis-cli"',
        'SRV="$REPO/bin/codis-server"',
        'cd "$REPO" || exit 1',
        "",
        "restart_if_down() {",
        '    port="$1"',
        '    conf="$2"',
        '    log="$3"',
        '    probe="$4"',
        '    out=$("$CLI" -h "$probe" -p "$port" --raw PING 2>/dev/null)',
        '    if [ "$out" = "PONG" ]; then',
        "        return 0",
        "    fi",
        '    echo "[pitr-restart] $(date -u +%Y-%m-%dT%H:%M:%SZ) port $port is down, restarting $conf"',
        '    mkdir -p "$(dirname "$log")"',
        '    nohup "$SRV" "$conf" >> "$log" 2>&1 < /dev/null &',
        "    return 0",
        "}",
        "",
    ]
    for master in masters:
        probe = master.get("probe") or default_probe
        lines.append(
            "restart_if_down {} {} {} {}".format(
                int(master["port"]),
                _sh_quote(master["conf"]),
                _sh_quote(master["log"]),
                _sh_quote(probe),
            )
        )
    lines.extend(["", "exit 0", ""])
    target = Path(path)
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text("\n".join(lines), encoding="utf-8")
    os.chmod(str(target), 0o755)
    return target


def split_slots(total, groups):
    """Split ``total`` slots into ``groups`` contiguous [beg, end] ranges."""
    per = total // groups
    ranges = []
    for index in range(groups):
        beg = index * per
        end = (index + 1) * per - 1 if index < groups - 1 else total - 1
        ranges.append((beg, end))
    return ranges


# ---------------------------------------------------------------------------
# main
# ---------------------------------------------------------------------------


def build_parser():
    parser = argparse.ArgumentParser(
        description="Start one Codis validation cluster and write cluster.json."
    )
    parser.add_argument("--repo", help="repository root (default: auto-detected)")
    parser.add_argument(
        "--workdir",
        help="working directory (default: <repo>/.runtime/realmachine/<run-id>)",
    )
    parser.add_argument("--product", default="codis-validation", help="Codis product name")
    parser.add_argument("--groups", type=int, default=2, help="number of server groups")
    parser.add_argument(
        "--replicas", type=int, default=1, help="replicas per group (0 disables replication)"
    )
    parser.add_argument("--proxies", type=int, default=2, help="number of codis-proxy processes")
    parser.add_argument("--fe", dest="fe", action="store_true", help="start codis-fe (default)")
    parser.add_argument("--no-fe", dest="fe", action="store_false", help="do not start codis-fe")
    parser.add_argument("--aof", dest="aof", action="store_true", help="enable AOF (default)")
    parser.add_argument(
        "--no-aof", dest="aof", action="store_false", help="disable AOF and PITR"
    )
    parser.add_argument(
        "--rdb-export",
        dest="rdb_export",
        action="store_true",
        help="enable RDB HTTP export on servers (default)",
    )
    parser.add_argument(
        "--no-rdb-export",
        dest="rdb_export",
        action="store_false",
        help="disable RDB HTTP export and dashboard remote fetch",
    )
    parser.add_argument("--bind", default="127.0.0.1", help="codis-server bind address")
    parser.add_argument(
        "--port-base",
        type=int,
        default=0,
        help="allocate ports sequentially from this number (0 = pick free ports)",
    )
    parser.add_argument(
        "--keep-on-failure",
        action="store_true",
        help="keep the workdir and write cluster.json with status=failed",
    )
    parser.add_argument("--output", help="cluster.json path (default: <workdir>/cluster.json)")
    parser.set_defaults(fe=True, aof=True, rdb_export=True)
    return parser


def main(argv=None):
    args = build_parser().parse_args(argv)

    repo = vlib.find_repo(args.repo)
    bin_dir = repo / "bin"
    run_id = vlib.new_run_id()
    workdir = (
        Path(args.workdir).expanduser().resolve()
        if args.workdir
        else repo / ".runtime" / "realmachine" / run_id
    )
    output = Path(args.output).expanduser().resolve() if args.output else workdir / "cluster.json"

    coordinator_dir = workdir / "coordinator"
    conf_dir = workdir / "conf"
    logs_dir = workdir / "logs"
    data_dir = workdir / "data"

    rdb_export_auth = secrets.token_hex(32) if args.rdb_export else ""

    cluster = {
        "schema": 1,
        "run_id": run_id,
        "product": args.product,
        "repo": str(repo),
        "workdir": str(workdir),
        "status": "failed",
        "error": None,
        "coordinator": {"type": "filesystem", "root": str(coordinator_dir)},
        "dashboard": None,
        "fe": None,
        "proxies": [],
        "groups": [],
        "features": {
            "aof": bool(args.aof),
            "rdb_export": bool(args.rdb_export),
            "rdb_export_auth": rdb_export_auth,
            "pitr": bool(args.aof),
            "pitr_restart_command": "",
        },
        "xauth": vlib.xauth_for(args.product),
        "bin_dir": str(bin_dir),
        "notes": [],
    }

    ports = PortAllocator(args.port_base)
    processes = []

    def start(name, command, log_path):
        proc = vlib.ManagedProcess(name, command, cwd=repo, log_path=log_path).start()
        processes.append(proc)
        return proc

    def stop_all():
        stopped = []
        for proc in reversed(processes):
            try:
                stopped.append(proc.stop())
            except Exception as err:  # noqa: BLE001 - cleanup must not mask the cause
                stopped.append({"name": proc.name, "status": "cleanup_failed", "error": str(err)})
        return stopped

    try:
        vlib.require_binaries(bin_dir, REQUIRED_BINARIES)
        if args.groups < 1:
            raise vlib.ValidationError("--groups must be >= 1")
        if args.replicas < 0:
            raise vlib.ValidationError("--replicas must be >= 0")
        if args.proxies < 0:
            raise vlib.ValidationError("--proxies must be >= 0")

        for directory in (coordinator_dir, conf_dir, logs_dir, data_dir):
            directory.mkdir(parents=True, exist_ok=True)

        vlib.log("workdir {}".format(workdir))
        vlib.log(
            "starting {} group(s) x (1 master + {} replica(s)), aof={} rdb_export={}".format(
                args.groups, args.replicas, args.aof, args.rdb_export
            )
        )

        replica_host = "127.0.0.1"
        if args.bind not in ("127.0.0.1", "0.0.0.0"):
            cluster["notes"].append(
                "codis-server binds {} but replicas replicate from 127.0.0.1; "
                "add 127.0.0.1 to the bind list if replication fails".format(args.bind)
            )

        # 1. codis-server instances -----------------------------------------
        for gid in range(1, args.groups + 1):
            group = {"gid": gid, "servers": []}
            master_port = ports.take()
            for index in range(args.replicas + 1):
                role = "master" if index == 0 else "replica"
                port = master_port if index == 0 else ports.take()
                server_data = data_dir / "redis-{}".format(port)
                server_data.mkdir(parents=True, exist_ok=True)
                conf = conf_dir / "redis-{}.conf".format(port)
                log_path = logs_dir / "redis-{}.log".format(port)
                pidfile = data_dir / "redis-{}.pid".format(port)
                vlib.render_redis_conf(
                    conf,
                    port,
                    server_data,
                    bind=args.bind,
                    pidfile=pidfile,
                    aof=args.aof,
                    rdb_export_auth=rdb_export_auth or None,
                    replicaof="{} {}".format(replica_host, master_port) if role == "replica" else None,
                )
                proc = start(
                    "codis-server:{}".format(port), [bin_dir / "codis-server", conf], log_path
                )
                server = {
                    "addr": "127.0.0.1:{}".format(port),
                    "host": "127.0.0.1",
                    "port": port,
                    "role": role,
                    "conf": str(conf),
                    "dir": str(server_data),
                    "pidfile": str(pidfile),
                    "log": str(log_path),
                    "pid": proc.pid,
                    "aof": bool(args.aof),
                    "rdb_export": bool(args.rdb_export),
                }
                group["servers"].append(server)
            cluster["groups"].append(group)

        for group in cluster["groups"]:
            for server in group["servers"]:
                wait_redis_pong(server["host"], server["port"])
        vlib.log("all codis-server instances answered PING")

        for group in cluster["groups"]:
            for server in group["servers"]:
                if server["role"] == "replica":
                    wait_replica_linked(server["host"], server["port"])
        vlib.log("replicas report master_link_status:up")

        # 2. dashboard -------------------------------------------------------
        dashboard_port = ports.take()
        masters = [
            {
                "port": group["servers"][0]["port"],
                "conf": group["servers"][0]["conf"],
                "log": group["servers"][0]["log"],
                "probe": "127.0.0.1" if args.bind == "0.0.0.0" else args.bind,
            }
            for group in cluster["groups"]
        ]
        restart_script = write_pitr_restart_script(conf_dir / "pitr-restart.sh", repo, masters)
        restart_command = "sh {}".format(restart_script)
        cluster["features"]["pitr_restart_command"] = restart_command

        dashboard_conf = conf_dir / "dashboard.toml"
        vlib.render_dashboard_conf(
            dashboard_conf,
            admin_port=dashboard_port,
            coordinator_dir=coordinator_dir,
            product=args.product,
            workdir=workdir,
            pitr_enabled=bool(args.aof),
            pitr_restart_command=restart_command,
            rdb_workspace=workdir / "rdb-analysis",
            rdb_remote_fetch_auth=rdb_export_auth or None,
        )
        dashboard_log = logs_dir / "dashboard.log"
        dashboard_proc = start(
            "codis-dashboard",
            [bin_dir / "codis-dashboard", "-c", dashboard_conf],
            dashboard_log,
        )
        cluster["dashboard"] = {
            "addr": "127.0.0.1:{}".format(dashboard_port),
            "host": "127.0.0.1",
            "port": dashboard_port,
            "conf": str(dashboard_conf),
            "log": str(dashboard_log),
            "pid": dashboard_proc.pid,
        }
        vlib.wait_tcp("127.0.0.1:{}".format(dashboard_port), timeout=60)
        wait_dashboard_ready(repo, dashboard_port)
        vlib.log("dashboard ready on 127.0.0.1:{}".format(dashboard_port))

        # 3. groups and slots ------------------------------------------------
        for group in cluster["groups"]:
            gid = group["gid"]
            admin_cmd(repo, dashboard_port, "--create-group", "--gid={}".format(gid))
            for server in group["servers"]:
                admin_cmd(
                    repo,
                    dashboard_port,
                    "--group-add",
                    "--gid={}".format(gid),
                    "--addr={}".format(server["addr"]),
                )
        for (beg, end), group in zip(split_slots(SLOT_COUNT, len(cluster["groups"])), cluster["groups"]):
            admin_cmd(
                repo,
                dashboard_port,
                "--slots-assign",
                "--beg={}".format(beg),
                "--end={}".format(end),
                "--gid={}".format(group["gid"]),
                "--confirm",
            )
        wait_slots_ready(repo, dashboard_port)
        vlib.log("groups created and all {} slots assigned".format(SLOT_COUNT))

        # 4. proxies ---------------------------------------------------------
        for index in range(args.proxies):
            admin_port = ports.take()
            proxy_port = ports.take()
            proxy_conf = conf_dir / "proxy-{}.toml".format(index)
            proxy_log = logs_dir / "proxy-{}.log".format(index)
            vlib.render_proxy_conf(
                proxy_conf,
                admin_port=admin_port,
                proxy_port=proxy_port,
                product=args.product,
                acl_enabled=True,
            )
            proc = start(
                "codis-proxy-{}".format(index),
                [
                    bin_dir / "codis-proxy",
                    "-c",
                    proxy_conf,
                    "--dashboard=127.0.0.1:{}".format(dashboard_port),
                ],
                proxy_log,
            )
            entry = {
                "index": index,
                "addr": "127.0.0.1:{}".format(proxy_port),
                "host": "127.0.0.1",
                "port": proxy_port,
                "admin_addr": "127.0.0.1:{}".format(admin_port),
                "admin_port": admin_port,
                "conf": str(proxy_conf),
                "log": str(proxy_log),
                "pid": proc.pid,
            }
            cluster["proxies"].append(entry)
            vlib.wait_tcp("127.0.0.1:{}".format(admin_port), timeout=60)
            probe = wait_proxy_ping("127.0.0.1", proxy_port)
            if probe["ok"]:
                vlib.log("proxy {} (admin {}) answered PING".format(proxy_port, admin_port))
            else:
                message = (
                    "proxy {} refused unauthenticated PING ({}); codis_acl_enabled=true is "
                    "kept as configured, downstream checks must authenticate".format(
                        proxy_port, probe["error"]
                    )
                )
                cluster["notes"].append(message)
                vlib.log(message)

        # 5. codis-fe --------------------------------------------------------
        if args.fe:
            fe_port = ports.take()
            assets_dir = bin_dir / "assets"
            if not assets_dir.is_dir():
                assets_dir = repo / "cmd" / "fe" / "assets"
            fe_log = logs_dir / "fe.log"
            fe_proc = start(
                "codis-fe",
                [
                    bin_dir / "codis-fe",
                    "--filesystem={}".format(coordinator_dir),
                    "--listen=127.0.0.1:{}".format(fe_port),
                    "--assets-dir={}".format(assets_dir),
                ],
                fe_log,
            )
            fe_url = "http://127.0.0.1:{}".format(fe_port)
            vlib.wait_http_ok(fe_url + "/list", timeout=60)
            status, body = vlib.http_json("GET", fe_url + "/list", timeout=10)
            text = body if isinstance(body, str) else json.dumps(body)
            if args.product not in text:
                raise vlib.ValidationError(
                    "codis-fe /list did not advertise product {!r}: {}".format(
                        args.product, text[:500]
                    )
                )
            cluster["fe"] = {
                "addr": "127.0.0.1:{}".format(fe_port),
                "url": fe_url,
                "log": str(fe_log),
                "pid": fe_proc.pid,
            }
            vlib.log("codis-fe ready at {}".format(fe_url))

        # 6. record ----------------------------------------------------------
        cluster["status"] = "ready"
        cluster["notes"].append(
            "pitr_restart_command runs `sh -c` with no server identity, so "
            "conf/pitr-restart.sh probes every master port and restarts the one "
            "that stopped answering PING"
        )
        cluster["notes"].append("dashboard/proxy/fe admin listeners bind 127.0.0.1 only")
        vlib.save_cluster(cluster, output)

        vlib.log("cluster ready: {}".format(output))
        vlib.log(
            "dashboard={} proxy={} fe={} servers={}".format(
                cluster["dashboard"]["addr"],
                ",".join(proxy["addr"] for proxy in cluster["proxies"]) or "-",
                cluster["fe"]["url"] if cluster["fe"] else "-",
                ",".join(
                    server["addr"] for group in cluster["groups"] for server in group["servers"]
                ),
            )
        )
        return 0

    except Exception as err:  # noqa: BLE001 - every failure must be recorded
        cluster["status"] = "failed"
        cluster["error"] = "{}: {}".format(type(err).__name__, err)
        vlib.log("setup failed: {}".format(cluster["error"]))
        stopped = stop_all()
        cluster["stopped"] = stopped
        if args.keep_on_failure:
            try:
                vlib.save_cluster(cluster, output)
                vlib.log("kept workdir {} and wrote {}".format(workdir, output))
            except Exception as write_err:  # noqa: BLE001
                vlib.log("failed to write {}: {}".format(output, write_err))
        else:
            shutil.rmtree(str(workdir), ignore_errors=True)
            vlib.log("removed workdir {}".format(workdir))
        return 1


if __name__ == "__main__":
    sys.exit(main())
