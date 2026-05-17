#!/usr/bin/env python3

import argparse
import datetime
import json
import os
import platform
import shutil
import signal
import socket
import subprocess
import sys
import tempfile
import time
from pathlib import Path


class ValidationError(Exception):
    pass


class ManagedProcess:
    def __init__(self, name, args, cwd, log_path):
        self.name = name
        self.args = [str(x) for x in args]
        self.cwd = str(cwd)
        self.log_path = Path(log_path)
        self.log_file = self.log_path.open("wb")
        self.proc = subprocess.Popen(
            self.args,
            cwd=self.cwd,
            stdout=self.log_file,
            stderr=subprocess.STDOUT,
        )

    def is_running(self):
        return self.proc.poll() is None

    def stop(self):
        if self.is_running():
            self.proc.terminate()
            try:
                self.proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                self.proc.kill()
                self.proc.wait(timeout=5)
        self.log_file.close()


def utc_now():
    return datetime.datetime.now(datetime.UTC).replace(microsecond=0).isoformat().replace("+00:00", "Z")


def repo_root_from_script():
    return Path(__file__).resolve().parents[1]


def short_text(value, limit=4000):
    if value is None:
        return ""
    text = str(value)
    if len(text) <= limit:
        return text
    return text[:limit] + "\n...[truncated]"


def run(args, cwd, timeout=30, check=True, input_text=None, stdout_limit=4000, stderr_limit=4000):
    proc = subprocess.run(
        [str(x) for x in args],
        cwd=str(cwd),
        input=input_text,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        timeout=timeout,
    )
    result = {
        "command": [str(x) for x in args],
        "cwd": str(cwd),
        "returncode": proc.returncode,
        "stdout": proc.stdout if stdout_limit is None else short_text(proc.stdout, stdout_limit),
        "stderr": proc.stderr if stderr_limit is None else short_text(proc.stderr, stderr_limit),
    }
    if check and proc.returncode != 0:
        raise ValidationError(json.dumps(result, indent=2, sort_keys=True))
    return result


def redis_cli(bin_dir, port, *args, db=None, timeout=15, check=True):
    cmd = [bin_dir / "redis-cli", "-h", "127.0.0.1", "-p", str(port), "--raw"]
    if db is not None:
        cmd.extend(["-n", str(db)])
    cmd.extend(args)
    return run(cmd, cwd=bin_dir.parent, timeout=timeout, check=check)


def redis_cli3(bin_dir, port, *args, db=None, timeout=15, check=True):
    cmd = [bin_dir / "redis-cli-redis3", "-h", "127.0.0.1", "-p", str(port), "--raw"]
    if db is not None:
        cmd.extend(["-n", str(db)])
    cmd.extend(args)
    return run(cmd, cwd=bin_dir.parent, timeout=timeout, check=check)


def free_port():
    sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    sock.bind(("127.0.0.1", 0))
    port = sock.getsockname()[1]
    sock.close()
    return port


def wait_tcp(port, timeout=20):
    deadline = time.time() + timeout
    last_error = None
    while time.time() < deadline:
        try:
            with socket.create_connection(("127.0.0.1", port), timeout=0.5):
                return
        except OSError as err:
            last_error = err
            time.sleep(0.2)
    raise ValidationError("port {} did not open: {}".format(port, last_error))


def wait_redis_ping(bin_dir, port, cli_func=redis_cli, timeout=20):
    deadline = time.time() + timeout
    last = None
    while time.time() < deadline:
        try:
            result = cli_func(bin_dir, port, "PING", timeout=5, check=False)
            if result["returncode"] == 0 and result["stdout"].strip() == "PONG":
                return
            last = result
        except Exception as err:
            last = str(err)
        time.sleep(0.3)
    raise ValidationError("redis ping failed on port {}: {}".format(port, short_text(last)))


def wait_admin_ready(bin_dir, admin_port, timeout=40):
    deadline = time.time() + timeout
    last = None
    while time.time() < deadline:
        result = admin_cmd(bin_dir, admin_port, "--slots-status", check=False, timeout=10)
        if result["returncode"] == 0:
            return
        last = result
        time.sleep(0.5)
    raise ValidationError("dashboard admin is not ready: {}".format(short_text(last)))


def wait_proxy_ready(bin_dir, proxy_port, timeout=40):
    deadline = time.time() + timeout
    last = None
    while time.time() < deadline:
        result = redis_cli(bin_dir, proxy_port, "PING", timeout=5, check=False)
        if result["returncode"] == 0 and result["stdout"].strip() == "PONG":
            return
        last = result
        time.sleep(0.5)
    raise ValidationError("proxy is not ready: {}".format(short_text(last)))


def admin_cmd(bin_dir, admin_port, *args, timeout=30, check=True, stdout_limit=4000):
    cmd = [bin_dir / "codis-admin", "--dashboard=127.0.0.1:{}".format(admin_port)]
    cmd.extend(args)
    return run(cmd, cwd=bin_dir.parent, timeout=timeout, check=check, stdout_limit=stdout_limit)


def write_redis_conf(path, port, data_dir, codis_enabled=True):
    lines = [
        "port {}".format(port),
        "bind 127.0.0.1",
        "protected-mode no",
        "daemonize no",
        'save ""',
        "appendonly no",
        "dir {}".format(data_dir),
        "dbfilename dump-{}.rdb".format(port),
    ]
    if codis_enabled:
        lines.insert(4, "codis-enabled yes")
    Path(path).write_text("\n".join(lines) + "\n")


def write_dashboard_conf(path, admin_port, rootfs, product_name, migration_method):
    lines = [
        'coordinator_name = "filesystem"',
        'coordinator_addr = "{}"'.format(rootfs),
        'product_name = "{}"'.format(product_name),
        'product_auth = ""',
        'admin_addr = "127.0.0.1:{}"'.format(admin_port),
        'migration_method = "{}"'.format(migration_method),
        "migration_parallel_slots = 1",
        "migration_async_maxbulks = 200",
        'migration_async_maxbytes = "32mb"',
        "migration_async_numkeys = 100",
        'migration_timeout = "30s"',
        'sentinel_client_timeout = "5s"',
        "sentinel_quorum = 1",
        "sentinel_parallel_syncs = 1",
        'sentinel_down_after = "5s"',
        'sentinel_failover_timeout = "1m"',
        'sentinel_notification_script = ""',
        'sentinel_client_reconfig_script = ""',
    ]
    Path(path).write_text("\n".join(lines) + "\n")


def write_proxy_conf(path, admin_port, proxy_port, product_name):
    lines = [
        'product_name = "{}"'.format(product_name),
        'product_auth = ""',
        'session_auth = ""',
        'admin_addr = "127.0.0.1:{}"'.format(admin_port),
        'proto_type = "tcp4"',
        'proxy_addr = "127.0.0.1:{}"'.format(proxy_port),
        'proxy_datacenter = "local-mac"',
        "proxy_max_clients = 1000",
        'proxy_max_offheap_size = "0"',
        'proxy_heap_placeholder = "0"',
        'backend_ping_period = "1s"',
        'backend_recv_bufsize = "128kb"',
        'backend_recv_timeout = "30s"',
        'backend_send_bufsize = "128kb"',
        'backend_send_timeout = "30s"',
        "backend_max_pipeline = 1024",
        "backend_primary_only = false",
        "backend_primary_parallel = 1",
        "backend_replica_parallel = 1",
        'backend_keepalive_period = "75s"',
        "backend_number_databases = 16",
        'session_recv_bufsize = "128kb"',
        'session_recv_timeout = "30m"',
        'session_send_bufsize = "64kb"',
        'session_send_timeout = "30s"',
        "session_max_pipeline = 1024",
        'session_keepalive_period = "75s"',
        "session_break_on_failure = false",
    ]
    Path(path).write_text("\n".join(lines) + "\n")


def parse_slots_status(stdout):
    return json.loads(stdout)


def slot_action_done(slot):
    action = slot.get("action") or {}
    return (
        slot.get("backend_addr_group_id") == 2
        and not slot.get("migrate_from")
        and not slot.get("migrate_from_group_id")
        and not action.get("state")
        and not action.get("target_id")
    )


def wait_slot_migration(bin_dir, admin_port, slot_ids, timeout=60):
    deadline = time.time() + timeout
    last = None
    while time.time() < deadline:
        result = admin_cmd(bin_dir, admin_port, "--slots-status", timeout=15, stdout_limit=None)
        slots = parse_slots_status(result["stdout"])
        by_id = {slot["id"]: slot for slot in slots}
        target = [by_id[sid] for sid in slot_ids if sid in by_id]
        if len(target) == len(slot_ids) and all(slot_action_done(slot) for slot in target):
            return target
        last = target
        time.sleep(0.5)
    raise ValidationError("slot migration did not finish: {}".format(short_text(last)))


def require_binaries(bin_dir):
    names = [
        "codis-admin",
        "codis-dashboard",
        "codis-proxy",
        "codis-server",
        "codis-server-redis3",
        "redis-cli",
        "redis-cli-redis3",
    ]
    missing = [name for name in names if not (bin_dir / name).exists()]
    if missing:
        raise ValidationError("missing binaries: {}".format(", ".join(missing)))


def collect_versions(repo, bin_dir):
    commands = {
        "git_head": ["git", "rev-parse", "--short=12", "HEAD"],
        "codis_server": [bin_dir / "codis-server", "--version"],
        "codis_server_redis3": [bin_dir / "codis-server-redis3", "--version"],
        "codis_proxy": [bin_dir / "codis-proxy", "--version"],
        "codis_dashboard": [bin_dir / "codis-dashboard", "--version"],
    }
    versions = {}
    for name, cmd in commands.items():
        try:
            versions[name] = run(cmd, cwd=repo, timeout=10, check=False)
        except Exception as err:
            versions[name] = {"error": str(err)}
    versions["codis_admin"] = {"status": "codis-admin has no --version command in this tree"}
    return versions


def start_redis(processes, bin_path, conf, cwd, log_path, name):
    proc = ManagedProcess(name, [bin_path, conf], cwd, log_path)
    processes.append(proc)
    return proc


def start_dashboard(processes, bin_dir, conf, cwd, log_path):
    proc = ManagedProcess("codis-dashboard", [bin_dir / "codis-dashboard", "-c", conf], cwd, log_path)
    processes.append(proc)
    return proc


def start_proxy(processes, bin_dir, conf, admin_port, cwd, log_path):
    proc = ManagedProcess(
        "codis-proxy",
        [
            bin_dir / "codis-proxy",
            "-c",
            conf,
            "--dashboard=127.0.0.1:{}".format(admin_port),
        ],
        cwd,
        log_path,
    )
    processes.append(proc)
    return proc


def assert_redis_value(bin_dir, port, key, expected, db=0):
    result = redis_cli(bin_dir, port, "GET", key, db=db)
    actual = result["stdout"].strip()
    if actual != expected:
        raise ValidationError("GET {} on db {} returned {!r}, want {!r}".format(key, db, actual, expected))


def migrate_slot(bin_dir, admin_port, slot_id):
    admin_cmd(bin_dir, admin_port, "--slot-action", "--create", "--sid={}".format(slot_id), "--gid=2")
    wait_slot_migration(bin_dir, admin_port, [slot_id])


def run_e2e_scenario(repo, bin_dir, work_root, migration_method, keep_temp):
    scenario_dir = work_root / "e2e-{}".format(migration_method)
    scenario_dir.mkdir(parents=True, exist_ok=True)
    rootfs = scenario_dir / "rootfs"
    data = scenario_dir / "data"
    logs = scenario_dir / "logs"
    conf = scenario_dir / "conf"
    for directory in (rootfs, data, logs, conf):
        directory.mkdir(parents=True, exist_ok=True)

    ports = {
        "redis_group_1": free_port(),
        "redis_group_2": free_port(),
        "dashboard_admin": free_port(),
        "proxy_admin": free_port(),
        "proxy": free_port(),
    }
    product_name = "redis8-validation-{}".format(migration_method.replace("-", ""))
    processes = []
    result = {
        "name": "e2e-{}".format(migration_method),
        "status": "running",
        "migration_method": migration_method,
        "ports": ports,
        "workdir": str(scenario_dir),
        "events": [],
    }

    try:
        redis1_conf = conf / "redis-group-1.conf"
        redis2_conf = conf / "redis-group-2.conf"
        write_redis_conf(redis1_conf, ports["redis_group_1"], data)
        write_redis_conf(redis2_conf, ports["redis_group_2"], data)
        start_redis(processes, bin_dir / "codis-server", redis1_conf, repo, logs / "redis-group-1.log", "redis8-group-1")
        start_redis(processes, bin_dir / "codis-server", redis2_conf, repo, logs / "redis-group-2.log", "redis8-group-2")
        wait_redis_ping(bin_dir, ports["redis_group_1"])
        wait_redis_ping(bin_dir, ports["redis_group_2"])
        result["events"].append("redis8 group masters are ready")

        dashboard_conf = conf / "dashboard.toml"
        write_dashboard_conf(dashboard_conf, ports["dashboard_admin"], rootfs, product_name, migration_method)
        start_dashboard(processes, bin_dir, dashboard_conf, repo, logs / "dashboard.log")
        wait_tcp(ports["dashboard_admin"])
        wait_admin_ready(bin_dir, ports["dashboard_admin"])
        result["events"].append("dashboard is ready")

        admin_cmd(bin_dir, ports["dashboard_admin"], "--create-group", "--gid=1")
        admin_cmd(
            bin_dir,
            ports["dashboard_admin"],
            "--group-add",
            "--gid=1",
            "--addr=127.0.0.1:{}".format(ports["redis_group_1"]),
            "--datacenter=local-mac",
        )
        admin_cmd(bin_dir, ports["dashboard_admin"], "--create-group", "--gid=2")
        admin_cmd(
            bin_dir,
            ports["dashboard_admin"],
            "--group-add",
            "--gid=2",
            "--addr=127.0.0.1:{}".format(ports["redis_group_2"]),
            "--datacenter=local-mac",
        )
        admin_cmd(bin_dir, ports["dashboard_admin"], "--slots-assign", "--beg=0", "--end=1023", "--gid=1", "--confirm")
        admin_cmd(bin_dir, ports["dashboard_admin"], "--slot-action", "--interval=0")
        result["events"].append("groups and initial slot map are configured")

        proxy_conf = conf / "proxy.toml"
        write_proxy_conf(proxy_conf, ports["proxy_admin"], ports["proxy"], product_name)
        start_proxy(processes, bin_dir, proxy_conf, ports["dashboard_admin"], repo, logs / "proxy.log")
        wait_tcp(ports["proxy_admin"])
        wait_proxy_ready(bin_dir, ports["proxy"])
        result["events"].append("proxy is online")

        keys = [
            {"key": "plain:{}:0".format(migration_method), "value": "value-plain-{}".format(migration_method), "db": 0},
            {"key": "{{cutover-{}}}:tagged".format(migration_method), "value": "value-tagged-{}".format(migration_method), "db": 0},
            {"key": "{{cutover-{}}}:db1".format(migration_method), "value": "value-db1-{}".format(migration_method), "db": 1},
        ]
        slot_ids = []
        for item in keys:
            redis_cli(bin_dir, ports["proxy"], "SET", item["key"], item["value"], db=item["db"])
            assert_redis_value(bin_dir, ports["proxy"], item["key"], item["value"], db=item["db"])
            slot_result = redis_cli(bin_dir, ports["redis_group_1"], "SLOTSHASHKEY", item["key"])
            item["slot"] = int(slot_result["stdout"].strip())
            if item["slot"] not in slot_ids:
                slot_ids.append(item["slot"])
        result["keys"] = keys
        result["slot_ids"] = slot_ids
        result["events"].append("proxy read/write checks passed before migration")

        for sid in slot_ids:
            migrate_slot(bin_dir, ports["dashboard_admin"], sid)
        for item in keys:
            assert_redis_value(bin_dir, ports["proxy"], item["key"], item["value"], db=item["db"])
        result["events"].append("proxy read/write checks passed after migration")

        target_checks = []
        for item in keys:
            source_exists = redis_cli(bin_dir, ports["redis_group_1"], "EXISTS", item["key"], db=item["db"])["stdout"].strip()
            target_exists = redis_cli(bin_dir, ports["redis_group_2"], "EXISTS", item["key"], db=item["db"])["stdout"].strip()
            target_checks.append(
                {
                    "key": item["key"],
                    "db": item["db"],
                    "slot": item["slot"],
                    "source_exists": source_exists,
                    "target_exists": target_exists,
                }
            )
            if target_exists != "1":
                raise ValidationError("target does not contain migrated key {}".format(item["key"]))
        result["target_checks"] = target_checks
        result["events"].append("source/target key placement checks passed")

        final_slots = wait_slot_migration(bin_dir, ports["dashboard_admin"], slot_ids, timeout=5)
        result["final_slots"] = final_slots
        result["status"] = "passed"
        return result
    except Exception as err:
        result["status"] = "failed"
        result["error"] = str(err)
        return result
    finally:
        cleanup = []
        for proc in reversed(processes):
            try:
                proc.stop()
                cleanup.append({"name": proc.name, "status": "stopped", "log": str(proc.log_path)})
            except Exception as err:
                cleanup.append({"name": proc.name, "status": "cleanup_failed", "error": str(err), "log": str(proc.log_path)})
        result["cleanup"] = cleanup
        if not keep_temp and result.get("status") == "passed":
            shutil.rmtree(scenario_dir, ignore_errors=True)
            result["workdir_removed"] = True


def cross_cli(bin_dir, redis_version):
    if redis_version == "redis3":
        return redis_cli3
    return redis_cli


def start_cross_redis(processes, repo, bin_dir, work_dir, redis_version, port, name):
    data_dir = work_dir / "data-{}-{}".format(redis_version, port)
    data_dir.mkdir(parents=True, exist_ok=True)
    conf = work_dir / "{}-{}.conf".format(redis_version, port)
    write_redis_conf(conf, port, data_dir, codis_enabled=(redis_version != "redis3"))
    binary = bin_dir / ("codis-server-redis3" if redis_version == "redis3" else "codis-server")
    log = work_dir / "{}-{}.log".format(redis_version, port)
    start_redis(processes, binary, conf, repo, log, name)
    wait_redis_ping(bin_dir, port, cli_func=cross_cli(bin_dir, redis_version))
    return conf


def prepare_sample(cli_func, bin_dir, port, sample):
    key = sample["key"]
    kind = sample["kind"]
    if kind == "string":
        cli_func(bin_dir, port, "SET", key, sample["value"])
    elif kind == "hash":
        cli_func(bin_dir, port, "HSET", key, "field", sample["value"])
    elif kind == "list":
        cli_func(bin_dir, port, "RPUSH", key, "one", "two", "three")
    elif kind == "zset":
        cli_func(bin_dir, port, "ZADD", key, "1", "one", "2", "two")
    else:
        raise ValidationError("unsupported sample kind {}".format(kind))


def assert_cross_sample(cli_func, bin_dir, port, sample):
    key = sample["key"]
    kind = sample["kind"]
    if kind == "string":
        actual = cli_func(bin_dir, port, "GET", key)["stdout"].strip()
        expected = sample["value"]
    elif kind == "hash":
        actual = cli_func(bin_dir, port, "HGET", key, "field")["stdout"].strip()
        expected = sample["value"]
    elif kind == "list":
        actual = cli_func(bin_dir, port, "LRANGE", key, "0", "-1")["stdout"].strip().splitlines()
        expected = ["one", "two", "three"]
    elif kind == "zset":
        actual = cli_func(bin_dir, port, "ZRANGE", key, "0", "-1", "WITHSCORES")["stdout"].strip().splitlines()
        expected = ["one", "1", "two", "2"]
    else:
        raise ValidationError("unsupported sample kind {}".format(kind))
    if actual != expected:
        raise ValidationError("sample {} mismatch: got {!r}, want {!r}".format(key, actual, expected))


def classify_migrate_result(result):
    output = (result.get("stdout") or "") + "\n" + (result.get("stderr") or "")
    if result.get("returncode") != 0 or "ERR" in output.upper() or "ERROR" in output.upper():
        return "observable_failure"
    text = result.get("stdout", "").strip()
    if text == "1":
        return "success"
    return "unsupported"


def run_cross_version_direction(repo, bin_dir, work_root, source_version, target_version, keep_temp):
    name = "{}-to-{}".format(source_version, target_version)
    scenario_dir = work_root / "cross-{}".format(name)
    scenario_dir.mkdir(parents=True, exist_ok=True)
    source_port = free_port()
    target_port = free_port()
    processes = []
    source_cli = cross_cli(bin_dir, source_version)
    target_cli = cross_cli(bin_dir, target_version)
    result = {
        "name": name,
        "status": "running",
        "source_version": source_version,
        "target_version": target_version,
        "source_port": source_port,
        "target_port": target_port,
        "workdir": str(scenario_dir),
        "samples": [],
    }
    samples = [
        {"kind": "string", "key": "{{cross-{}}}:string".format(name), "value": "value-string"},
        {"kind": "hash", "key": "{{cross-{}}}:hash".format(name), "value": "value-hash"},
        {"kind": "list", "key": "{{cross-{}}}:list".format(name), "value": ""},
        {"kind": "zset", "key": "{{cross-{}}}:zset".format(name), "value": ""},
    ]
    try:
        start_cross_redis(processes, repo, bin_dir, scenario_dir, source_version, source_port, "source")
        start_cross_redis(processes, repo, bin_dir, scenario_dir, target_version, target_port, "target")
        for sample in samples:
            source_cli(bin_dir, source_port, "FLUSHALL")
            target_cli(bin_dir, target_port, "FLUSHALL")
            prepare_sample(source_cli, bin_dir, source_port, sample)
            before_exists = source_cli(bin_dir, source_port, "EXISTS", sample["key"])["stdout"].strip()
            migrate = source_cli(
                bin_dir,
                source_port,
                "SLOTSMGRTTAGONE",
                "127.0.0.1",
                str(target_port),
                "3000",
                sample["key"],
                timeout=20,
                check=False,
            )
            classification = classify_migrate_result(migrate)
            source_exists = source_cli(bin_dir, source_port, "EXISTS", sample["key"], check=False)["stdout"].strip()
            target_exists = target_cli(bin_dir, target_port, "EXISTS", sample["key"], check=False)["stdout"].strip()
            sample_result = {
                "kind": sample["kind"],
                "key": sample["key"],
                "before_source_exists": before_exists,
                "classification": classification,
                "migrate": migrate,
                "after_source_exists": source_exists,
                "after_target_exists": target_exists,
            }
            if classification == "success":
                assert_cross_sample(target_cli, bin_dir, target_port, sample)
                if source_exists != "0":
                    raise ValidationError("source key was not removed after success: {}".format(sample["key"]))
            else:
                if source_exists != "1":
                    raise ValidationError("source key disappeared after failed migration: {}".format(sample["key"]))
            result["samples"].append(sample_result)
        classifications = sorted(set(sample["classification"] for sample in result["samples"]))
        result["classifications"] = classifications
        result["status"] = "passed"
        if classifications == ["success"]:
            result["direction_result"] = "success"
        elif "observable_failure" in classifications:
            result["direction_result"] = "observable_failure"
        else:
            result["direction_result"] = "unsupported"
        return result
    except Exception as err:
        result["status"] = "failed"
        result["error"] = str(err)
        return result
    finally:
        cleanup = []
        for proc in reversed(processes):
            try:
                proc.stop()
                cleanup.append({"name": proc.name, "status": "stopped", "log": str(proc.log_path)})
            except Exception as err:
                cleanup.append({"name": proc.name, "status": "cleanup_failed", "error": str(err), "log": str(proc.log_path)})
        result["cleanup"] = cleanup
        if not keep_temp and result.get("status") == "passed":
            shutil.rmtree(scenario_dir, ignore_errors=True)
            result["workdir_removed"] = True


def build_gate_definitions():
    return [
        {
            "name": "default-build",
            "command": "make build-all",
            "expected": "bin/codis-server is Redis 8 and Go binaries are rebuilt",
            "failure_owner": "packaging/build",
            "local_execution": "documented_only",
            "evidence": "runner metadata plus local shell log when executed manually",
        },
        {
            "name": "go-regression",
            "command": "make gotest",
            "expected": "go test ./cmd/... ./pkg/... passes",
            "failure_owner": "Go component",
            "local_execution": "documented_only",
            "evidence": "local shell log when executed manually",
        },
        {
            "name": "redis-tcl-codis-suite",
            "command": "cd extern/redis-8.6.3 && ./runtest --single unit/codis --single unit/codis_migration --single unit/codis_slotsrestore --single unit/codis_async_migration",
            "expected": "Redis 8 Codis Tcl suites pass",
            "failure_owner": "Redis 8 Codis server",
            "local_execution": "documented_only",
            "evidence": "Redis runtest log when executed manually",
        },
        {
            "name": "e2e-local-codis-cluster",
            "command": "python3 scripts/redis8_local_validation.py --only e2e --output <evidence.json>",
            "expected": "semi-async and sync local clusters pass proxy and migration checks",
            "failure_owner": "integration/topom/proxy/server",
            "local_execution": "runner",
            "evidence": "feature evidence JSON",
        },
        {
            "name": "cross-version-fragment-matrix",
            "command": "python3 scripts/redis8_local_validation.py --only cross-version --output <evidence.json>",
            "expected": "Redis 3/Redis 8 directions are success or observable failure with source preserved",
            "failure_owner": "RDB fragment compatibility",
            "local_execution": "runner",
            "evidence": "feature evidence JSON",
        },
        {
            "name": "linux-formal-handoff",
            "command": "review .codestable/features/2026-05-17-redis8-validation-cutover/redis8-linux-validation-handoff.md",
            "expected": "Linux e2e, benchmark, fork/RDB, replication, Docker/deploy checks are enumerated",
            "failure_owner": "validation planning",
            "local_execution": "documented_only",
            "evidence": "handoff markdown",
        },
        {
            "name": "cutover-runbook-draft",
            "command": "review .codestable/features/2026-05-17-redis8-validation-cutover/redis8-cutover-runbook-draft.md",
            "expected": "preflight/canary/ramp/full/rollback are covered",
            "failure_owner": "operations planning",
            "local_execution": "documented_only",
            "evidence": "runbook markdown",
        },
    ]


def parse_only(value):
    if value == "all":
        return {"e2e", "cross-version"}
    return {part.strip() for part in value.split(",") if part.strip()}


def main():
    parser = argparse.ArgumentParser(description="Run local non-performance Redis 8 Codis validation gates.")
    parser.add_argument("--repo", default=str(repo_root_from_script()), help="repository root")
    parser.add_argument("--output", help="write JSON evidence to this path")
    parser.add_argument("--only", default="all", help="all, e2e, cross-version, or comma-separated subset")
    parser.add_argument("--keep-temp", action="store_true", help="keep temporary workdirs")
    args = parser.parse_args()

    repo = Path(args.repo).resolve()
    bin_dir = repo / "bin"
    selected = parse_only(args.only)
    evidence = {
        "feature": "2026-05-17-redis8-validation-cutover",
        "generated_at": utc_now(),
        "host": {
            "platform": platform.platform(),
            "machine": platform.machine(),
            "python": sys.version.split()[0],
        },
        "scope": "local Mac non-performance validation only; benchmark and formal cutover verdict are excluded",
        "gate_definitions": build_gate_definitions(),
        "versions": {},
        "e2e": [],
        "cross_version": [],
    }

    work_root = Path(tempfile.mkdtemp(prefix="codis-redis8-validation-"))
    evidence["temporary_root"] = str(work_root)
    overall_ok = True
    try:
        require_binaries(bin_dir)
        evidence["versions"] = collect_versions(repo, bin_dir)

        if "e2e" in selected:
            for method in ("semi-async", "sync"):
                scenario = run_e2e_scenario(repo, bin_dir, work_root, method, args.keep_temp)
                evidence["e2e"].append(scenario)
                if scenario.get("status") != "passed":
                    overall_ok = False

        if "cross-version" in selected:
            for source, target in (("redis3", "redis8"), ("redis8", "redis3")):
                scenario = run_cross_version_direction(repo, bin_dir, work_root, source, target, args.keep_temp)
                evidence["cross_version"].append(scenario)
                if scenario.get("status") != "passed":
                    overall_ok = False

        evidence["status"] = "passed" if overall_ok else "failed"
    except Exception as err:
        evidence["status"] = "failed"
        evidence["error"] = str(err)
        overall_ok = False
    finally:
        if not args.keep_temp and evidence.get("status") == "passed":
            shutil.rmtree(work_root, ignore_errors=True)
            evidence["temporary_root_removed"] = True
        if args.output:
            output = Path(args.output)
            output.parent.mkdir(parents=True, exist_ok=True)
            output.write_text(json.dumps(evidence, indent=2, sort_keys=True) + "\n")

    print(json.dumps(evidence, indent=2, sort_keys=True))
    return 0 if overall_ok else 1


if __name__ == "__main__":
    # Keep child processes under Python's signal handling in interactive shells.
    signal.signal(signal.SIGTERM, signal.default_int_handler)
    sys.exit(main())
