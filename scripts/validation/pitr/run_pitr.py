#!/usr/bin/env python3
"""Real-machine validation of PITR (point-in-time recovery / data flashback).

Drives the dashboard HTTP API against a cluster created by
``scripts/validation/env/setup_cluster.py``: builds a legal truncate point from
the live AOF (#TS annotations), runs the 9-step state machine, verifies the
recovered keyspace plus replica resync, exercises every synchronous rejection
and the per-server lock, and removes every job it created (also on failure).

Exit code follows the evidence status: 0 passed, 1 failed, 2 blocked/skipped.
"""

import argparse
import json
import re
import subprocess
import sys
import time
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import vlib

SCRIPT = "scripts/validation/pitr/run_pitr.py"
FEATURE = "pitr-flashback"
SCOPE = (
    "single Redis 8 Codis Server PITR orchestrated by codis-dashboard on a local "
    "validation cluster; cross-group consistency, proxy read-only switching, "
    "performance and FE browser interaction are out of scope"
)

STEP_ORDER = (
    "validate",
    "prereq",
    "feasibility",
    "shutdown",
    "snapshot",
    "truncate",
    "restart",
    "wait-load",
    "resync",
)
TERMINAL_STATES = ("succeeded", "failed", "cancelled")
RESYNC_TIMEOUT = 60
MANIFEST_LINE = re.compile(r"^file\s+(\S+)\s+seq\s+(\d+)\s+type\s+(\S+)(?:\s+\S+.*)?$")


# ---------------------------------------------------------------------------
# local helpers (kept here instead of vlib: PITR/RESP specifics)
# ---------------------------------------------------------------------------


def require(condition, message):
    """Raise ValidationError so the enclosing Evidence case turns failed."""
    if not condition:
        raise vlib.ValidationError(message)


def cause_of(data):
    """Return the dashboard error text of an HTTP 800 response."""
    if isinstance(data, dict):
        for key in ("Cause", "cause"):
            value = data.get(key)
            if value:
                return str(value)
        return json.dumps(data, sort_keys=True)[:500]
    return "" if data is None else str(data)


def resp_cmd(server, *args, timeout=10):
    with vlib.Resp(server["host"], server["port"], timeout=timeout) as client:
        return client.cmd(*args)


def config_get(server, name):
    reply = resp_cmd(server, "CONFIG", "GET", name)
    if isinstance(reply, list) and len(reply) >= 2:
        return reply[1]
    return None


def info_fields(server, section=None):
    text = resp_cmd(server, "INFO", section) if section else resp_cmd(server, "INFO")
    fields = {}
    for line in str(text).splitlines():
        if line.startswith("#") or ":" not in line:
            continue
        key, value = line.split(":", 1)
        fields[key.strip()] = value.strip()
    return fields


def dbsize(server):
    return int(resp_cmd(server, "DBSIZE"))


def ping(server):
    """Return the PING reply, or None when the server is unreachable."""
    try:
        return resp_cmd(server, "PING", timeout=3)
    except (OSError, vlib.RedisError):
        return None


def write_strings(server, keys, value="validation"):
    with vlib.Resp(server["host"], server["port"], timeout=10) as client:
        for key in keys:
            client.cmd("SET", key, value)


def aof_location(server):
    """Return ``(aof_dir, manifest_path)`` from CONFIG GET."""
    base = config_get(server, "dir")
    appenddirname = config_get(server, "appenddirname")
    appendfilename = config_get(server, "appendfilename")
    if not base or not appenddirname or not appendfilename:
        return None, None
    directory = Path(base) / appenddirname
    return directory, directory / (appendfilename + ".manifest")


def read_aof_segments(manifest_path):
    """Return the manifest segment filenames in evaluation order (BASE, INCRs)."""
    segments = []
    for line in vlib.read_text(manifest_path).splitlines():
        match = MANIFEST_LINE.match(line.strip())
        if match:
            segments.append((match.group(1), match.group(3)))
    return segments


def scan_aof_timestamps(path):
    """Return every ``#TS:<unix>`` annotation value found in an AOF segment."""
    stamps = []
    try:
        with Path(path).open("rb") as handle:
            for line in handle:
                if not line.startswith(b"#TS:"):
                    continue
                try:
                    stamps.append(int(line[4:].strip()))
                except ValueError:
                    continue
    except OSError:
        return []
    return stamps


def last_segment_info(server):
    """Locate the last AOF segment and its #TS range (what feasibility checks)."""
    directory, manifest = aof_location(server)
    if directory is None:
        raise vlib.ValidationError(
            "cannot resolve dir/appenddirname/appendfilename on {}".format(server["addr"])
        )
    segments = read_aof_segments(manifest)
    if not segments:
        raise vlib.ValidationError("aof manifest {} has no readable segments".format(manifest))
    name = segments[-1][0]
    stamps = scan_aof_timestamps(directory / name)
    return {
        "aof_dir": str(directory),
        "manifest": str(manifest),
        "segments": [segment[0] for segment in segments],
        "last_segment_file": name,
        "ts_count": len(stamps),
        "ts_min": min(stamps) if stamps else None,
        "ts_max": max(stamps) if stamps else None,
    }


def prepare_truncate_point(server, run_id, tag):
    """Write before/marker/after keys and pick a ts inside the last segment.

    aof-timestamp-enabled only emits a ``#TS:`` line before the first write of a
    new second, so the marker writes plus the >=1.5s sleeps are what make the
    chosen second land between the before keys and the after keys.
    """
    prefix = "pitr:{}:{}".format(run_id, tag)
    before_keys = ["{}:before:{}".format(prefix, index) for index in range(5)]
    after_keys = ["{}:after:{}".format(prefix, index) for index in range(5)]
    marker_before = "{}:marker:before".format(prefix)
    marker_after = "{}:marker:after".format(prefix)

    write_strings(server, before_keys + [marker_before])
    time.sleep(1.6)
    write_strings(server, ["{}:flush".format(prefix)])
    truncate_ts = int(time.time())
    time.sleep(1.6)
    write_strings(server, after_keys)
    time.sleep(1.6)
    write_strings(server, [marker_after])
    time.sleep(1.2)

    return {
        "prefix": prefix,
        "before_keys": before_keys,
        "after_keys": after_keys,
        "markers": [marker_before, marker_after],
        "truncate_ts": truncate_ts,
        "last_segment": last_segment_info(server),
    }


def step_table(job):
    return [
        {"name": step.get("name"), "status": step.get("status"), "error": step.get("error")}
        for step in job.get("steps") or []
    ]


def steps_by_name(job):
    return {step.get("name"): step for step in job.get("steps") or []}


def wait_job(api, job_id, timeout, poll=0.25):
    """Poll a PITR job until it reaches a terminal state."""
    deadline = time.time() + timeout
    last = None
    while time.time() < deadline:
        status, job = api.get(job_id)
        if status != 200 or not isinstance(job, dict):
            raise vlib.ValidationError(
                "GET pitr job {} returned http {}: {}".format(job_id, status, job)
            )
        last = job
        if job.get("state") in TERMINAL_STATES:
            return job
        time.sleep(poll)
    raise vlib.ValidationError(
        "pitr job {} stayed non-terminal for {}s (last state={})".format(
            job_id, timeout, (last or {}).get("state")
        )
    )


def record_job(case, job, **extra):
    data = {
        "job_id": job.get("id"),
        "state": job.get("state"),
        "error": job.get("error"),
        "steps": step_table(job),
        "restart_kicked": job.get("restart_kicked"),
        "snapshot_dir": job.get("snapshot_dir"),
        "last_segment_ts_range": job.get("last_segment_ts_range"),
        "last_segment_file": job.get("last_segment_file"),
    }
    data.update(extra)
    case.set(**data)
    return job


def wait_replica_in_sync(master, replica, timeout=RESYNC_TIMEOUT):
    """Wait for link up + no full sync in progress + equal DBSIZE."""
    started = time.time()
    deadline = started + timeout
    last = {}
    while time.time() < deadline:
        try:
            fields = info_fields(replica, "replication")
            master_size = dbsize(master)
            replica_size = dbsize(replica)
        except (OSError, vlib.RedisError) as err:
            last = {"error": str(err)}
            time.sleep(0.5)
            continue
        last = {
            "master_link_status": fields.get("master_link_status"),
            "master_sync_in_progress": fields.get("master_sync_in_progress"),
            "master_host": fields.get("master_host"),
            "master_port": fields.get("master_port"),
            "dbsize_master": master_size,
            "dbsize_replica": replica_size,
        }
        if (
            last["master_link_status"] == "up"
            and last["master_sync_in_progress"] == "0"
            and last["master_host"] == master["host"]
            and str(last["master_port"]) == str(master["port"])
            and master_size == replica_size
        ):
            last["waited_sec"] = round(time.time() - started, 2)
            return last
        time.sleep(0.5)
    raise vlib.ValidationError(
        "replica {} did not resync within {}s: {}".format(
            replica["addr"], timeout, json.dumps(last, sort_keys=True)
        )
    )


def restart_server(repo, server, timeout=40):
    """Start a codis-server from its conf (dashboard never forks redis itself)."""
    bin_path = Path(repo) / "bin" / "codis-server"
    log_path = Path(server.get("log") or (Path(repo) / ".runtime" / "pitr-server.log"))
    log_path.parent.mkdir(parents=True, exist_ok=True)
    with log_path.open("ab") as handle:
        handle.write(
            "# {} PITR validation: server was down, restarting {}\n".format(
                vlib.utc_now(), server.get("conf")
            ).encode("utf-8")
        )
        handle.flush()
        subprocess.Popen(
            [str(bin_path), str(server["conf"])],
            cwd=str(repo),
            stdin=subprocess.DEVNULL,
            stdout=handle,
            stderr=subprocess.STDOUT,
            start_new_session=True,
        )
    vlib.wait_tcp(server["addr"], timeout=timeout)
    deadline = time.time() + timeout
    while time.time() < deadline:
        if ping(server) == "PONG":
            return True
        time.sleep(0.2)
    return False


def ensure_server_up(repo, server, timeout=40):
    """Return a small report; restart the server when a job left it down."""
    report = {"addr": server["addr"], "status": "up", "restarted": False}
    if ping(server) == "PONG":
        return report
    report["status"] = "down"
    report["restarted"] = restart_server(repo, server, timeout=timeout)
    report["status"] = "up_after_restart" if report["restarted"] else "down"
    return report


# ---------------------------------------------------------------------------
# dashboard API
# ---------------------------------------------------------------------------


class PitrApi(object):
    """Thin client for the /api/topom/pitr/* endpoints."""

    def __init__(self, dashboard_addr, xauth):
        self.base = "http://{}".format(str(dashboard_addr).rstrip("/"))
        self.xauth = xauth

    def jobs(self, token=None):
        token = self.xauth if token is None else token
        return vlib.http_json("GET", self.base + "/api/topom/pitr/jobs/" + str(token))

    def get(self, job_id, token=None):
        token = self.xauth if token is None else token
        return vlib.http_json(
            "GET", self.base + "/api/topom/pitr/{}/{}".format(token, job_id)
        )

    def create(self, server_addr, truncate_ts, token=None):
        token = self.xauth if token is None else token
        return vlib.http_json(
            "PUT",
            self.base + "/api/topom/pitr/create/" + str(token),
            {"server_addr": server_addr, "truncate_ts": truncate_ts},
        )

    def cancel(self, job_id, token=None):
        token = self.xauth if token is None else token
        return vlib.http_json(
            "PUT", self.base + "/api/topom/pitr/cancel/{}/{}".format(token, job_id)
        )

    def remove(self, job_id, token=None):
        token = self.xauth if token is None else token
        return vlib.http_json(
            "PUT", self.base + "/api/topom/pitr/remove/{}/{}".format(token, job_id)
        )


# ---------------------------------------------------------------------------
# job bookkeeping
# ---------------------------------------------------------------------------


class JobRegistry(object):
    """Every job this script created, so cleanup can remove all of them."""

    def __init__(self):
        self.items = []

    def track(self, job_id, case_id, server, note=""):
        self.items.append(
            {
                "id": job_id,
                "case": case_id,
                "server": server["addr"] if server else None,
                "note": note,
                "removed": False,
            }
        )
        return job_id

    def mark_removed(self, job_id):
        for item in self.items:
            if item["id"] == job_id:
                item["removed"] = True

    def item_for(self, job_id):
        for item in self.items:
            if item["id"] == job_id:
                return item
        raise vlib.ValidationError("job {} is not tracked by this run".format(job_id))

    def open_items(self):
        return [item for item in self.items if not item["removed"]]

    def ids(self):
        return [item["id"] for item in self.items]


class Context(object):
    """Run-wide state shared by the cases."""

    def __init__(self, repo, cluster, run_id, timeout):
        self.repo = Path(repo)
        self.cluster = cluster
        self.run_id = run_id
        self.timeout = int(timeout)
        self.bin_dir = Path(cluster.get("bin_dir") or (self.repo / "bin"))
        self.workdir = Path(cluster.get("workdir") or (self.repo / ".runtime"))
        self.product = cluster.get("product") or ""
        self.xauth = cluster.get("xauth") or vlib.xauth_for(self.product)
        dashboard = cluster.get("dashboard") or {}
        self.dashboard = dashboard
        self.api = (
            PitrApi(dashboard.get("addr"), self.xauth) if dashboard.get("addr") else None
        )
        self.master, self.replica, self.other_master = self._pick_targets()
        self.jobs = JobRegistry()
        self.precheck_ok = False
        self.precheck_reason = None

    def _pick_targets(self):
        groups = self.cluster.get("groups") or []
        master = None
        replica = None
        other = None
        if groups:
            servers = groups[0].get("servers") or []
            if servers:
                master = servers[0]
                for server in servers[1:]:
                    if server.get("role") == "replica":
                        replica = server
                        break
        if len(groups) > 1:
            servers = groups[1].get("servers") or []
            if servers:
                other = servers[0]
        return master, replica, other

    def guard(self, case):
        """Block a case when the precheck did not pass."""
        if self.precheck_ok:
            return False
        case.blocked(self.precheck_reason or "pitr-precheck did not pass")
        return True


def remove_job(case, ctx, item, settle_timeout=30):
    """Cancel (when needed) and remove one tracked job; recorded in case."""
    result = {"id": item["id"], "case": item["case"], "server": item["server"]}
    if item["removed"]:
        result["status"] = "already_removed"
        return result
    status, job = ctx.api.get(item["id"])
    if status != 200:
        result["status"] = "missing"
        result["http_status"] = status
        result["error"] = cause_of(job)
        ctx.jobs.mark_removed(item["id"])
        return result
    if job.get("state") not in TERMINAL_STATES:
        cancel_status, cancel_body = ctx.api.cancel(item["id"])
        result["cancel_http_status"] = cancel_status
        result["cancel_body"] = (
            cancel_body if isinstance(cancel_body, str) else cause_of(cancel_body)
        )
        job = wait_job(ctx.api, item["id"], settle_timeout)
    result["state"] = job.get("state")
    status, body = ctx.api.remove(item["id"])
    result["http_status"] = status
    result["body"] = body if isinstance(body, str) else cause_of(body)
    if status == 200:
        result["status"] = "removed"
        ctx.jobs.mark_removed(item["id"])
    else:
        result["status"] = "remove_failed"
    return result


# ---------------------------------------------------------------------------
# cases
# ---------------------------------------------------------------------------


def case_precheck(evidence, ctx):
    with evidence.case(
        "pitr-precheck", "PITR is enabled and the target master/replica meet the prerequisites"
    ) as case:
        features = ctx.cluster.get("features") or {}
        case.set(
            product=ctx.product,
            dashboard=ctx.dashboard.get("addr"),
            master=ctx.master.get("addr") if ctx.master else None,
            replica=ctx.replica.get("addr") if ctx.replica else None,
            other_master=ctx.other_master.get("addr") if ctx.other_master else None,
            features={"aof": bool(features.get("aof")), "pitr": bool(features.get("pitr"))},
            pitr_restart_command=features.get("pitr_restart_command") or "",
        )
        problems = []
        if ctx.api is None:
            problems.append("cluster.json has no dashboard.addr")
        if ctx.master is None:
            problems.append("cluster.json has no group master")
        if ctx.replica is None:
            problems.append("group 1 has no replica (PITR resync cannot be verified)")
        if not features.get("pitr"):
            problems.append("cluster.json features.pitr is false")
        if not features.get("aof"):
            problems.append("cluster.json features.aof is false")
        checker = ctx.bin_dir / "redis-check-aof"
        case.set(redis_check_aof=str(checker), redis_check_aof_exists=checker.is_file())
        if not checker.is_file():
            problems.append("{} is missing".format(checker))
        if ctx.master is not None and not ctx.master.get("aof", True):
            problems.append("group 1 master was started without AOF")

        if ctx.master is not None:
            appendonly = config_get(ctx.master, "appendonly")
            timestamps = config_get(ctx.master, "aof-timestamp-enabled")
            case.set(appendonly=appendonly, aof_timestamp_enabled=timestamps)
            if str(appendonly).lower() != "yes":
                problems.append('master CONFIG GET appendonly is {!r}'.format(appendonly))
            if str(timestamps).lower() != "yes":
                problems.append(
                    "master CONFIG GET aof-timestamp-enabled is {!r}".format(timestamps)
                )
            try:
                case.set(last_segment=last_segment_info(ctx.master))
            except (vlib.ValidationError, OSError, vlib.RedisError) as err:
                case.step("cannot read the AOF manifest before the run", error=str(err))

        if ctx.replica is not None:
            fields = info_fields(ctx.replica, "replication")
            link = fields.get("master_link_status")
            case.set(
                replica_link_status=link,
                replica_master="{host}:{port}".format(
                    host=fields.get("master_host"), port=fields.get("master_port")
                ),
            )
            if link != "up":
                problems.append("replica master_link_status is {!r}".format(link))

        if problems:
            ctx.precheck_ok = False
            ctx.precheck_reason = "; ".join(problems)
            case.blocked(ctx.precheck_reason)
            return
        ctx.precheck_ok = True


def case_happy_path(evidence, ctx):
    with evidence.case(
        "pitr-happy-path",
        "truncate to a live AOF timestamp, restart, and verify keyspace plus replica resync",
    ) as case:
        if ctx.guard(case):
            return
        point = prepare_truncate_point(ctx.master, ctx.run_id, "hp")
        segment = point["last_segment"]
        case.set(
            truncate_ts=point["truncate_ts"],
            before_keys=point["before_keys"],
            after_keys=point["after_keys"],
            markers=point["markers"],
            last_segment=segment,
        )
        require(
            segment["ts_min"] is not None and segment["ts_max"] is not None,
            "last AOF segment {} carries no #TS annotations".format(segment["last_segment_file"]),
        )
        require(
            segment["ts_min"] <= point["truncate_ts"] <= segment["ts_max"],
            "prepared truncate_ts={} is outside the last segment #TS range [{},{}]".format(
                point["truncate_ts"], segment["ts_min"], segment["ts_max"]
            ),
        )
        case.step(
            "wrote before/after keys around a #TS boundary",
            truncate_ts=point["truncate_ts"],
            ts_range=[segment["ts_min"], segment["ts_max"]],
        )

        status, body = ctx.api.create(ctx.master["addr"], point["truncate_ts"])
        require(status == 200, "pitr create returned http {}: {}".format(status, cause_of(body)))
        job_id = body.get("job_id") if isinstance(body, dict) else None
        require(bool(job_id), "pitr create returned no job_id: {}".format(body))
        ctx.jobs.track(job_id, "pitr-happy-path", ctx.master)
        case.set(job_id=job_id)

        job = wait_job(ctx.api, job_id, ctx.timeout)
        record_job(case, job)
        require(
            job.get("state") == "succeeded",
            "pitr job ended {} instead of succeeded: {}".format(
                job.get("state"), job.get("error")
            ),
        )
        table = step_table(job)
        require(
            [step["name"] for step in table] == list(STEP_ORDER),
            "unexpected step sequence: {}".format([step["name"] for step in table]),
        )
        not_done = [step["name"] for step in table if step["status"] != "done"]
        require(not not_done, "steps not done: {}".format(not_done))
        require(job.get("restart_kicked") is True, "restart_kicked is not true")
        job_range = job.get("last_segment_ts_range") or [None, None]
        require(
            job_range[0] is not None
            and job_range[0] <= point["truncate_ts"] <= job_range[1],
            "job last_segment_ts_range {} does not contain truncate_ts={}".format(
                job_range, point["truncate_ts"]
            ),
        )
        case.step(
            "all 9 steps done, restart_kicked=true",
            last_segment_ts_range=job_range,
            snapshot_dir=job.get("snapshot_dir"),
        )

        require(
            ping(ctx.master) == "PONG",
            "master {} does not answer PING after PITR".format(ctx.master["addr"]),
        )
        exists_before = resp_cmd(ctx.master, "EXISTS", *point["before_keys"])
        exists_after = resp_cmd(ctx.master, "EXISTS", *point["after_keys"])
        case.set(
            master_ping="PONG",
            exists_before=exists_before,
            exists_after=exists_after,
            appendonly_after=config_get(ctx.master, "appendonly"),
        )
        require(
            exists_before == len(point["before_keys"]),
            "expected {} surviving before keys, EXISTS returned {}".format(
                len(point["before_keys"]), exists_before
            ),
        )
        require(
            exists_after == 0,
            "expected the {} after keys to be truncated, EXISTS returned {}".format(
                len(point["after_keys"]), exists_after
            ),
        )

        resync = wait_replica_in_sync(ctx.master, ctx.replica)
        case.set(
            replica_resync=resync,
            dbsize={"master": resync["dbsize_master"], "replica": resync["dbsize_replica"]},
        )
        require(
            resync["dbsize_master"] == resync["dbsize_replica"],
            "master/replica DBSIZE differ after PITR: {}".format(resync),
        )
        case.step(
            "replica resynced",
            waited_sec=resync["waited_sec"],
            dbsize=resync["dbsize_master"],
            master_link_status=resync["master_link_status"],
            master_sync_in_progress=resync["master_sync_in_progress"],
        )
        removed = remove_job(case, ctx, ctx.jobs.item_for(job_id))
        case.step("removed the succeeded job", **removed)
        require(
            removed.get("status") == "removed",
            "the succeeded job could not be removed: {}".format(removed),
        )


def assert_rejected(case, ctx, status, body, expected, server=None, job_label="reject"):
    """Accept a synchronous 800 rejection or an asynchronous job failure."""
    text = cause_of(body)
    if status == 800:
        require(
            expected in text,
            "http 800 cause {!r} does not contain {!r}".format(text, expected),
        )
        case.step("rejected synchronously by create", http_status=status, cause=text)
        return {"mode": "create-800", "cause": text}
    require(status == 200, "unexpected http {} from pitr create: {}".format(status, text))
    job_id = body.get("job_id") if isinstance(body, dict) else None
    require(bool(job_id), "pitr create returned no job_id: {}".format(body))
    ctx.jobs.track(job_id, case.id, server or ctx.master, job_label)
    job = wait_job(ctx.api, job_id, ctx.timeout)
    record_job(case, job)
    require(
        job.get("state") == "failed",
        "job ended {} instead of failed (error={})".format(job.get("state"), job.get("error")),
    )
    job_error = job.get("error") or ""
    require(
        expected in job_error,
        "job error {!r} does not contain {!r}".format(job_error, expected),
    )
    failed_steps = [step["name"] for step in step_table(job) if step["status"] == "failed"]
    case.step(
        "rejected asynchronously by the job",
        http_status=status,
        job_state=job.get("state"),
        job_error=job_error,
        failed_steps=failed_steps,
    )
    return {
        "mode": "job-failed",
        "cause": job_error,
        "job_id": job_id,
        "failed_steps": failed_steps,
    }


def case_reject_non_master(evidence, ctx):
    with evidence.case(
        "pitr-reject-non-master", "a group replica is refused as a PITR target"
    ) as case:
        if ctx.guard(case):
            return
        if ctx.replica is None:
            case.blocked("group 1 has no replica to target")
            return
        point = last_segment_info(ctx.master)
        ts = point["ts_max"]
        case.set(server=ctx.replica["addr"], truncate_ts=ts, target_role="replica")
        status, body = ctx.api.create(ctx.replica["addr"], ts)
        outcome = assert_rejected(
            case,
            ctx,
            status,
            body,
            "is not the master of group",
            server=ctx.replica,
            job_label="non-master",
        )
        case.set(rejection=outcome)
        require(
            ping(ctx.replica) == "PONG",
            "replica {} stopped answering PING".format(ctx.replica["addr"]),
        )
        case.set(replica_ping="PONG")
        if outcome.get("job_id"):
            removed = remove_job(case, ctx, ctx.jobs.item_for(outcome["job_id"]))
            case.step("removed the rejected job", **removed)


def case_reject_aof_off(evidence, ctx):
    with evidence.case(
        "pitr-reject-aof-off",
        "CONFIG SET appendonly no on another master makes the job fail before touching it",
    ) as case:
        if ctx.guard(case):
            return
        if ctx.other_master is None:
            case.blocked("cluster.json has only one group")
            return
        server = ctx.other_master
        case.set(server=server["addr"])
        try:
            resp_cmd(server, "CONFIG", "SET", "appendonly", "no")
            time.sleep(0.5)
            appendonly = config_get(server, "appendonly")
            persistence = info_fields(server, "persistence")
            case.set(appendonly=appendonly, aof_enabled=persistence.get("aof_enabled"))
            require(
                str(appendonly).lower() == "no",
                "CONFIG SET appendonly no did not take effect ({!r})".format(appendonly),
            )
            truncate_ts = int(time.time())
            case.set(truncate_ts=truncate_ts)
            status, body = ctx.api.create(server["addr"], truncate_ts)
            text = cause_of(body)
            if status == 800:
                require(
                    "appendonly" in text,
                    "http 800 cause {!r} does not mention appendonly".format(text),
                )
                case.set(rejection={"mode": "create-800", "cause": text})
                case.step("rejected synchronously", http_status=status, cause=text)
            else:
                require(
                    status == 200,
                    "unexpected http {} from pitr create: {}".format(status, text),
                )
                job_id = body.get("job_id")
                ctx.jobs.track(job_id, case.id, server, "aof-off")
                job = wait_job(ctx.api, job_id, ctx.timeout)
                record_job(case, job)
                require(
                    job.get("state") == "failed",
                    "job ended {} instead of failed".format(job.get("state")),
                )
                expected = 'appendonly="no" (expected yes)'
                require(
                    expected in (job.get("error") or ""),
                    "job error {!r} does not contain {!r}".format(job.get("error"), expected),
                )
                table = steps_by_name(job)
                require(
                    table.get("prereq", {}).get("status") == "failed",
                    "prereq step did not fail: {}".format(step_table(job)),
                )
                require(
                    table.get("shutdown") is None,
                    "shutdown ran even though prereq failed",
                )
                case.set(
                    rejection={
                        "mode": "job-failed",
                        "job_id": job_id,
                        "job_error": job.get("error"),
                        "failed_steps": [
                            s["name"] for s in step_table(job) if s["status"] == "failed"
                        ],
                    }
                )
                case.step(
                    "job failed in prereq while the server stayed untouched",
                    job_error=job.get("error"),
                )
                removed = remove_job(case, ctx, ctx.jobs.item_for(job_id))
                case.step("removed the rejected job", **removed)
            require(
                ping(server) == "PONG",
                "{} stopped answering PING".format(server["addr"]),
            )
            case.set(server_ping="PONG")
        finally:
            resp_cmd(server, "CONFIG", "SET", "appendonly", "yes")
            restored = None
            deadline = time.time() + 30
            while time.time() < deadline:
                fields = info_fields(server, "persistence")
                restored = fields.get("aof_enabled")
                if restored == "1":
                    break
                time.sleep(0.3)
            case.set(
                appendonly_restored=config_get(server, "appendonly"),
                aof_enabled_restored=restored,
            )
            require(
                restored == "1",
                "appendonly did not recover to aof_enabled:1 (got {!r})".format(restored),
            )


def case_reject_ts_zero(evidence, ctx):
    with evidence.case(
        "pitr-reject-ts-zero", "truncate_ts=0 is refused synchronously"
    ) as case:
        if ctx.guard(case):
            return
        before_jobs = ctx.api.jobs()
        status, body = ctx.api.create(ctx.master["addr"], 0)
        text = cause_of(body)
        case.set(http_status=status, cause=text, truncate_ts=0)
        require(status == 800, "expected http 800 for truncate_ts=0, got {}".format(status))
        require(
            "invalid truncate_ts" in text,
            "cause {!r} does not contain 'invalid truncate_ts'".format(text),
        )
        after_jobs = ctx.api.jobs()
        require(
            before_jobs[0] == 200 and after_jobs[0] == 200,
            "cannot list pitr jobs: {} / {}".format(before_jobs[0], after_jobs[0]),
        )
        require(
            len(after_jobs[1]) == len(before_jobs[1]),
            "truncate_ts=0 created a job: {}".format(after_jobs[1]),
        )
        require(ping(ctx.master) == "PONG", "master stopped answering PING")


def case_reject_ts_out_of_range(evidence, ctx):
    with evidence.case(
        "pitr-reject-ts-out-of-range",
        "truncate_ts=1 fails in feasibility while the server stays online",
    ) as case:
        if ctx.guard(case):
            return
        case.set(server=ctx.master["addr"], truncate_ts=1)
        status, body = ctx.api.create(ctx.master["addr"], 1)
        outcome = assert_rejected(
            case, ctx, status, body, "truncate point not in last aof segment", job_label="ts-1"
        )
        case.set(rejection=outcome)
        require(
            "feasibility" in (outcome.get("failed_steps") or []),
            "job did not fail in the feasibility step: {}".format(outcome),
        )
        require(
            ping(ctx.master) == "PONG",
            "master stopped answering PING after feasibility rejection",
        )
        case.set(master_ping="PONG", appendonly_after=config_get(ctx.master, "appendonly"))
        removed = remove_job(case, ctx, ctx.jobs.item_for(outcome["job_id"]))
        case.step("removed the rejected job", **removed)


def case_reject_unknown_server(evidence, ctx):
    with evidence.case(
        "pitr-reject-unknown-server", "a server outside the product is refused"
    ) as case:
        if ctx.guard(case):
            return
        unknown = "127.0.0.1:1"
        point = last_segment_info(ctx.master)
        case.set(server=unknown, truncate_ts=point["ts_max"])
        status, body = ctx.api.create(unknown, point["ts_max"])
        outcome = assert_rejected(
            case, ctx, status, body, "doesn't exist", server=None, job_label="unknown-server"
        )
        case.set(rejection=outcome)
        if outcome.get("job_id"):
            removed = remove_job(case, ctx, ctx.jobs.item_for(outcome["job_id"]))
            case.step("removed the rejected job", **removed)


def case_cancel_before(evidence, ctx, pending):
    with evidence.case(
        "pitr-cancel-before",
        "cancel right after create ends cancelled and keeps the server online (lock stays)",
    ) as case:
        if ctx.guard(case):
            return
        point = last_segment_info(ctx.master)
        truncate_ts = point["ts_max"]
        status, body = ctx.api.create(ctx.master["addr"], truncate_ts)
        require(status == 200, "pitr create returned http {}: {}".format(status, cause_of(body)))
        job_id = body.get("job_id")
        ctx.jobs.track(job_id, case.id, ctx.master, "cancel-before")
        cancel_status, cancel_body = ctx.api.cancel(job_id)
        case.set(
            job_id=job_id,
            truncate_ts=truncate_ts,
            last_segment=point,
            cancel_http_status=cancel_status,
            cancel_body=cancel_body if isinstance(cancel_body, str) else cause_of(cancel_body),
        )
        require(
            cancel_status == 200,
            "cancel returned http {}: {}".format(cancel_status, cause_of(cancel_body)),
        )
        job = wait_job(ctx.api, job_id, ctx.timeout)
        record_job(case, job)
        state = job.get("state")
        if state == "succeeded":
            case.skip(
                "cancel lost the race: the job already reached succeeded; "
                "the lock is still held until remove"
            )
            case.set(server_after="checked in pitr-lock-until-remove")
        else:
            require(
                state == "cancelled",
                "cancel-before ended {} instead of cancelled (error={})".format(
                    state, job.get("error")
                ),
            )
            failed = [step["name"] for step in step_table(job) if step["status"] == "failed"]
            require(not failed, "cancelled job still has failed steps: {}".format(failed))
            require(
                steps_by_name(job).get("shutdown") is None,
                "shutdown ran before the cancel landed: {}".format(step_table(job)),
            )
            require(ping(ctx.master) == "PONG", "master stopped answering PING after cancel-before")
            case.set(
                server_after="up",
                master_ping="PONG",
                steps=[step["name"] for step in step_table(job)],
            )
            case.step("cancelled before any side effect, master still online")
        pending["cancel_before_job"] = job_id


def case_lock_until_remove(evidence, ctx, pending):
    with evidence.case(
        "pitr-lock-until-remove",
        "the per-server lock survives the terminal state and only remove releases it",
    ) as case:
        if ctx.guard(case):
            return
        prior_id = pending.get("cancel_before_job")
        require(bool(prior_id), "pitr-cancel-before produced no job to lock the server")
        status, jobs = ctx.api.jobs()
        require(status == 200, "cannot list pitr jobs: {}".format(cause_of(jobs)))
        listed = {job.get("id"): job for job in jobs or []}
        require(
            prior_id in listed,
            "the terminal job {} is no longer listed, cannot test the lock".format(prior_id),
        )
        case.set(prior_job=prior_id, prior_state=listed[prior_id].get("state"))

        point = last_segment_info(ctx.master)
        truncate_ts = point["ts_max"]
        case.set(truncate_ts=truncate_ts, last_segment=point)
        status, body = ctx.api.create(ctx.master["addr"], truncate_ts)
        text = cause_of(body)
        case.set(locked_http_status=status, locked_cause=text)
        require(
            status == 800,
            "expected http 800 while the terminal job holds the lock, got {}".format(status),
        )
        require(
            "is locked by pitr job" in text,
            "cause {!r} does not contain 'is locked by pitr job'".format(text),
        )
        require(prior_id in text, "lock cause {!r} does not name job {}".format(text, prior_id))
        case.step("second create refused while the terminal job holds the lock", cause=text)

        status, body = ctx.api.remove(prior_id)
        require(
            status == 200,
            "remove of {} returned http {}: {}".format(prior_id, status, cause_of(body)),
        )
        ctx.jobs.mark_removed(prior_id)
        case.step("removed the terminal job", http_status=status, body=body)

        status, body = ctx.api.create(ctx.master["addr"], truncate_ts)
        text = cause_of(body)
        case.set(
            released_http_status=status,
            released_body=body if isinstance(body, dict) else text,
        )
        require(status == 200, "create after remove returned http {}: {}".format(status, text))
        job_id = body.get("job_id")
        ctx.jobs.track(job_id, case.id, ctx.master, "lock-release")
        case.set(job_id=job_id)
        cancel_status, cancel_body = ctx.api.cancel(job_id)
        case.set(cancel_http_status=cancel_status)
        job = wait_job(ctx.api, job_id, ctx.timeout)
        record_job(case, job)
        require(
            job.get("state") in ("cancelled", "succeeded"),
            "lock-release probe job ended {}: {}".format(job.get("state"), job.get("error")),
        )
        case.step(
            "create succeeded after remove (lock released)",
            job_state=job.get("state"),
            cancel_http_status=cancel_status,
        )
        if job.get("state") == "succeeded":
            resync = wait_replica_in_sync(ctx.master, ctx.replica)
            case.set(replica_resync=resync)
        require(ping(ctx.master) == "PONG", "master stopped answering PING")
        case.set(master_ping="PONG")
        removed = remove_job(case, ctx, ctx.jobs.item_for(job_id))
        case.step("removed the probe job", **removed)


def case_cancel_in_truncate(evidence, ctx):
    with evidence.case(
        "pitr-cancel-in-truncate", "cancel while redis-check-aof truncates"
    ) as case:
        case.skip(
            "S17 (cancel-during-truncate semantics) is not finalized: today the job "
            "ends failed with the server down and the snapshot kept, while the "
            "acceptance semantics expect cancelled; the behaviour is not asserted here"
        )


def case_xauth_guard(evidence, ctx):
    with evidence.case(
        "pitr-xauth-guard", "the PITR API requires a valid xauth token"
    ) as case:
        if ctx.api is None:
            case.blocked("cluster.json has no dashboard")
            return
        no_auth_status, _ = ctx.api.jobs(token="")
        wrong_status, wrong_body = ctx.api.jobs(token="deadbeef")
        ok_status, ok_body = ctx.api.jobs()
        known_ids = [job.get("id") for job in ok_body] if isinstance(ok_body, list) else []
        case.set(
            no_xauth_http_status=no_auth_status,
            wrong_xauth_http_status=wrong_status,
            wrong_xauth_cause=cause_of(wrong_body),
            correct_xauth_http_status=ok_status,
            job_count=len(ok_body) if isinstance(ok_body, list) else None,
        )
        require(
            no_auth_status == 404,
            "GET pitr jobs without xauth returned {}".format(no_auth_status),
        )
        require(
            wrong_status == 800,
            "GET pitr jobs with a wrong xauth returned {}".format(wrong_status),
        )
        require(
            "invalid xauth" in cause_of(wrong_body),
            "wrong xauth cause {!r} does not contain 'invalid xauth'".format(cause_of(wrong_body)),
        )
        require(
            ok_status == 200,
            "GET pitr jobs with the correct xauth returned {}".format(ok_status),
        )
        require(
            isinstance(ok_body, list),
            "GET pitr jobs did not return a list: {}".format(ok_body),
        )

        status, body = ctx.api.create(ctx.master["addr"], 1, token="deadbeef")
        case.set(create_wrong_xauth_http_status=status, create_wrong_xauth_cause=cause_of(body))
        require(status == 800, "create with a wrong xauth returned {}".format(status))
        require(
            "invalid xauth" in cause_of(body),
            "create wrong-xauth cause {!r} does not contain 'invalid xauth'".format(cause_of(body)),
        )
        status, jobs = ctx.api.jobs()
        require(status == 200, "cannot re-list pitr jobs")
        after_ids = [job.get("id") for job in jobs or []]
        require(
            after_ids == known_ids,
            "a wrong-xauth create changed the job list: {} -> {}".format(known_ids, after_ids),
        )
        case.set(job_list_unchanged=True)


def case_cleanup(evidence, ctx):
    """Remove every job this run created and leave the cluster healthy."""
    with evidence.case(
        "pitr-cleanup", "remove every PITR job created by this run and check server health"
    ) as case:
        results = []
        for item in ctx.jobs.open_items():
            try:
                result = remove_job(case, ctx, item)
            except Exception as err:  # noqa: BLE001 - cleanup must always finish
                result = {
                    "id": item["id"],
                    "case": item["case"],
                    "status": "error",
                    "error": str(err),
                }
            results.append(result)
            case.step("cleanup job {}".format(item["id"]), **result)
        case.set(removed=results)

        removable = ("removed", "already_removed", "missing")
        failures = [result for result in results if result.get("status") not in removable]
        remaining = []
        status, jobs = ctx.api.jobs() if ctx.api else (None, None)
        if status == 200 and isinstance(jobs, list):
            ours = set(ctx.jobs.ids())
            remaining = [job.get("id") for job in jobs if job.get("id") in ours]
        else:
            case.step("cannot re-list pitr jobs during cleanup", http_status=status)
        case.set(remaining_jobs=remaining)
        require(not failures, "jobs that could not be removed: {}".format(failures))
        require(not remaining, "jobs still registered after cleanup: {}".format(remaining))

        servers = []
        for server in (ctx.master, ctx.other_master):
            if not server:
                continue
            try:
                servers.append(ensure_server_up(ctx.repo, server))
            except Exception as err:  # noqa: BLE001
                servers.append({"addr": server["addr"], "status": "error", "error": str(err)})
        case.set(servers=servers)
        healthy = ("up", "up_after_restart")
        down = [server for server in servers if server.get("status") not in healthy]
        require(not down, "servers left down by the run: {}".format(down))
        case.step("all masters answer PING", servers=servers)


# ---------------------------------------------------------------------------
# main
# ---------------------------------------------------------------------------


def build_parser():
    parser = argparse.ArgumentParser(
        description="Real-machine validation of Codis PITR (data flashback)."
    )
    parser.add_argument("--cluster", required=True, help="path to cluster.json")
    parser.add_argument(
        "--output",
        help="evidence path (default: <repo>/.runtime/realmachine/run_pitr-<run-id>.json)",
    )
    parser.add_argument("--repo", help="repository root (default: auto-detected)")
    parser.add_argument(
        "--timeout",
        type=int,
        default=300,
        help="maximum seconds to poll a single PITR job (default: 300)",
    )
    return parser


def main(argv=None):
    args = build_parser().parse_args(argv)
    repo = vlib.find_repo(args.repo)
    cluster_path = Path(args.cluster).expanduser().resolve()
    cluster = vlib.load_cluster(cluster_path)
    run_id = vlib.new_run_id()
    output = (
        Path(args.output).expanduser().resolve()
        if args.output
        else repo / ".runtime" / "realmachine" / "run_pitr-{}.json".format(run_id)
    )

    evidence = vlib.Evidence(
        script=SCRIPT, feature=FEATURE, scope=SCOPE, cluster_path=cluster_path
    )
    evidence.attach_repo(repo)

    ctx = Context(repo=repo, cluster=cluster, run_id=run_id, timeout=args.timeout)
    evidence.note("cluster: {} (product={})".format(cluster_path, ctx.product))
    evidence.note("workdir: {}".format(ctx.workdir))
    evidence.note(
        "targets: master={} replica={} other_master={}".format(
            ctx.master.get("addr") if ctx.master else None,
            ctx.replica.get("addr") if ctx.replica else None,
            ctx.other_master.get("addr") if ctx.other_master else None,
        )
    )
    evidence.note(
        "truncate_ts is derived from the last AOF segment #TS annotations; "
        "non-master and unknown-server targets are rejected by the asynchronous "
        "validate step (http 200 + job failed) rather than by create"
    )

    pending = {}

    def run():
        try:
            case_precheck(evidence, ctx)
            case_happy_path(evidence, ctx)
            case_reject_non_master(evidence, ctx)
            case_reject_aof_off(evidence, ctx)
            case_reject_ts_zero(evidence, ctx)
            case_reject_ts_out_of_range(evidence, ctx)
            case_reject_unknown_server(evidence, ctx)
            case_cancel_before(evidence, ctx, pending)
            case_lock_until_remove(evidence, ctx, pending)
            case_cancel_in_truncate(evidence, ctx)
            case_xauth_guard(evidence, ctx)
        finally:
            case_cleanup(evidence, ctx)
        return evidence.exit_code()

    return vlib.main_guard(evidence, run, output)


if __name__ == "__main__":
    sys.exit(main())
