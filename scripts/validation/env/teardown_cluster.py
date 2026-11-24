#!/usr/bin/env python3
"""Stop the processes started by setup_cluster.py and clean the workdir.

Only pids recorded in cluster.json are killed (plus the pid found in a
codis-server pidfile when the originally recorded pid already exited, e.g.
after a PITR restart).
"""

import argparse
import json
import shutil
import sys
import time
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import vlib


def build_parser():
    parser = argparse.ArgumentParser(
        description="Stop a Codis validation cluster and write cleanup.json."
    )
    parser.add_argument("--cluster", required=True, help="path to cluster.json")
    parser.add_argument("--keep-dir", action="store_true", help="keep the workdir on disk")
    parser.add_argument(
        "--output", help="cleanup.json path (default: <workdir>/cleanup.json)"
    )
    return parser


def stop_order(cluster):
    """Return ``[(name, pid, pidfile)]`` in proxy -> fe -> dashboard -> server order."""
    targets = []
    for proxy in reversed(cluster.get("proxies") or []):
        targets.append(
            ("codis-proxy-{}".format(proxy.get("index")), proxy.get("pid"), None)
        )
    fe = cluster.get("fe") or {}
    if fe:
        targets.append(("codis-fe", fe.get("pid"), None))
    dashboard = cluster.get("dashboard") or {}
    if dashboard:
        targets.append(("codis-dashboard", dashboard.get("pid"), None))
    for group in reversed(cluster.get("groups") or []):
        for server in reversed(group.get("servers") or []):
            targets.append(
                (
                    "codis-server:{}".format(server.get("port")),
                    server.get("pid"),
                    server.get("pidfile"),
                )
            )
    return targets


def cluster_ports(cluster):
    ports = []
    dashboard = cluster.get("dashboard") or {}
    if dashboard.get("port"):
        ports.append(int(dashboard["port"]))
    fe = cluster.get("fe") or {}
    if fe.get("addr"):
        ports.append(int(fe["addr"].rsplit(":", 1)[1]))
    for proxy in cluster.get("proxies") or []:
        for key in ("port", "admin_port"):
            if proxy.get(key):
                ports.append(int(proxy[key]))
    for group in cluster.get("groups") or []:
        for server in group.get("servers") or []:
            if server.get("port"):
                ports.append(int(server["port"]))
    seen = []
    for port in ports:
        if port not in seen:
            seen.append(port)
    return seen


def wait_ports_released(ports, timeout=10):
    """Return the ports that are still listening after ``timeout`` seconds."""
    deadline = time.time() + timeout
    pending = list(ports)
    while pending and time.time() < deadline:
        pending = [port for port in pending if vlib.port_is_open(port)]
        if pending:
            time.sleep(0.2)
    return [port for port in pending if vlib.port_is_open(port)]


def main(argv=None):
    args = build_parser().parse_args(argv)

    cluster_path = Path(args.cluster).expanduser().resolve()
    cluster = vlib.load_cluster(cluster_path)
    workdir = Path(cluster.get("workdir") or cluster_path.parent).expanduser().resolve()
    output = (
        Path(args.output).expanduser().resolve()
        if args.output
        else workdir / "cleanup.json"
    )

    repo = cluster.get("repo")
    guards = [str(workdir)]
    if repo:
        guards.append(str(repo))

    report = {
        "cluster": str(cluster_path),
        "generated_at": vlib.utc_now(),
        "stopped": [],
        "ports_released": False,
        "workdir_removed": False,
        "errors": [],
    }

    vlib.log("tearing down cluster {}".format(cluster_path))
    for name, pid, pidfile in stop_order(cluster):
        result = vlib.stop_pid(pid, name, expect_substring=guards)
        report["stopped"].append(result)
        vlib.log("{} pid={} -> {}".format(name, pid, result["status"]))
        if result["status"] in ("skipped_unexpected_command", "kill_failed"):
            report["errors"].append(
                {
                    "name": name,
                    "pid": pid,
                    "status": result["status"],
                    "command": result.get("command"),
                    "error": result.get("error"),
                }
            )
        if not pidfile:
            continue
        # A PITR restart replaces the process: pick the new pid from the pidfile.
        live_pid = vlib.read_pidfile(pidfile)
        if not live_pid or live_pid == pid:
            continue
        extra = vlib.stop_pid(
            live_pid, "{}:pidfile".format(name), expect_substring=guards
        )
        report["stopped"].append(extra)
        vlib.log("{} pidfile pid={} -> {}".format(name, live_pid, extra["status"]))
        if extra["status"] in ("skipped_unexpected_command", "kill_failed"):
            report["errors"].append(
                {
                    "name": extra["name"],
                    "pid": live_pid,
                    "status": extra["status"],
                    "command": extra.get("command"),
                    "error": extra.get("error"),
                }
            )

    ports = cluster_ports(cluster)
    still_open = wait_ports_released(ports, timeout=10)
    report["ports_released"] = not still_open
    if still_open:
        report["errors"].append(
            {"kind": "port_still_open", "ports": sorted(still_open)}
        )
        vlib.log("ports still listening: {}".format(sorted(still_open)))
    else:
        vlib.log("all {} cluster ports released".format(len(ports)))

    if args.keep_dir:
        report["workdir_removed"] = False
        vlib.log("keeping workdir {}".format(workdir))
    elif workdir.is_dir():
        try:
            shutil.rmtree(str(workdir))
            report["workdir_removed"] = True
            vlib.log("removed workdir {}".format(workdir))
        except OSError as err:
            report["errors"].append({"kind": "workdir_remove_failed", "error": str(err)})
            vlib.log("failed to remove {}: {}".format(workdir, err))
    else:
        report["workdir_removed"] = True

    escaped_output = None
    if not args.keep_dir and _is_inside(output, workdir):
        # The default output lives inside the workdir that was just removed;
        # keep a sibling copy so the evidence survives the cleanup.
        escaped_output = workdir.parent / "{}.cleanup.json".format(workdir.name)
    vlib.save_cluster(report, escaped_output or output)
    if escaped_output:
        vlib.log("cleanup report written to {}".format(escaped_output))
    else:
        vlib.log("cleanup report written to {}".format(output))

    sys.stdout.write(json.dumps(report, indent=2, sort_keys=True) + "\n")
    return 0 if not report["errors"] else 1


def _is_inside(path, directory):
    try:
        Path(path).resolve().relative_to(Path(directory).resolve())
        return True
    except ValueError:
        return False


if __name__ == "__main__":
    sys.exit(main())
