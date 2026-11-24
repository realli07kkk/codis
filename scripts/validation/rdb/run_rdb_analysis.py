#!/usr/bin/env python3
"""Real-machine validation of RDB Analysis (remote fetch / upload / workspace).

Covers the Redis 8 Codis Server RDB HTTP export and the three dashboard inputs
that feed the same parser: ``remote-http:`` (dashboard pulls ``GET
/codis/rdb/latest`` from a server in the current product), ``upload:`` (browser
style multipart upload) and ``workspace:`` (a path under
``rdb_analysis_workspace``).

Evidence lands in one JSON file; every job created here is removed at the end
(also when a case fails). Exit code follows the evidence status: 0 passed,
1 failed, 2 blocked/skipped.
"""

import argparse
import hashlib
import json
import re
import secrets
import sys
import time
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import vlib

SCRIPT = "scripts/validation/rdb/run_rdb_analysis.py"
FEATURE = "rdb-analysis"
SCOPE = (
    "RDB HTTP export plus dashboard remote fetch / upload / workspace analysis on a "
    "local validation cluster; browser FE interaction, RDB parser version matrix and "
    "performance are out of scope"
)

TERMINAL_STATUSES = ("done", "error", "canceled")
EXPORT_PATH = "/codis/rdb/latest"
EXPORT_HEADER = "X-Codis-RDB-Auth"
DEFAULT_OPTIONS = {
    "top_n": 100,
    "max_depth": 3,
    "prefix_separators": [":"],
    "regex": "",
    "include_expired": False,
}
WORKSPACE_RE = re.compile(r'^\s*rdb_analysis_workspace\s*=\s*"([^"]*)"', re.M)
REMOTE_ENABLED_RE = re.compile(r"^\s*rdb_analysis_remote_fetch_enabled\s*=\s*true\s*$", re.M)
REMOTE_AUTH_RE = re.compile(r'^\s*rdb_analysis_remote_fetch_auth\s*=\s*"([^"]*)"', re.M)


# ---------------------------------------------------------------------------
# local helpers
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


def ping(server):
    try:
        return resp_cmd(server, "PING", timeout=3)
    except (OSError, vlib.RedisError):
        return None


def write_dataset(server, prefix):
    """Write one key of every type plus a 3-entry stream; return the key map."""
    keys = {
        "string": "{}:str".format(prefix),
        "hash": "{}:hash".format(prefix),
        "list": "{}:list".format(prefix),
        "set": "{}:set".format(prefix),
        "zset": "{}:zset".format(prefix),
        "stream": "{}:stream".format(prefix),
    }
    with vlib.Resp(server["host"], server["port"], timeout=10) as client:
        client.cmd("SET", keys["string"], "hello-rdb-analysis")
        client.cmd("HSET", keys["hash"], "field1", "value1", "field2", "value2")
        client.cmd("RPUSH", keys["list"], "a", "b", "c")
        client.cmd("SADD", keys["set"], "x", "y", "z")
        client.cmd("ZADD", keys["zset"], 1, "alpha", 2, "beta")
        for index in range(3):
            client.cmd(
                "XADD", keys["stream"], "*", "seq", str(index), "payload", "v{}".format(index)
            )
        exists = {name: client.cmd("EXISTS", key) for name, key in keys.items()}
        stream_len = client.cmd("XLEN", keys["stream"])
        dbsize = client.cmd("DBSIZE")
    return {"keys": keys, "exists": exists, "stream_len": stream_len, "dbsize": dbsize}


def save_rdb(server, wait_timeout=60):
    """SAVE the master and wait until no bgsave is in flight."""
    result = {"save": resp_cmd(server, "SAVE", timeout=wait_timeout)}
    deadline = time.time() + wait_timeout
    fields = {}
    while time.time() < deadline:
        fields = info_fields(server, "persistence")
        if fields.get("rdb_bgsave_in_progress") == "0":
            break
        time.sleep(0.2)
    result.update(
        {
            "rdb_bgsave_in_progress": fields.get("rdb_bgsave_in_progress"),
            "rdb_last_bgsave_status": fields.get("rdb_last_bgsave_status"),
            "rdb_last_save_time": fields.get("rdb_last_save_time"),
        }
    )
    return result


def export_rdb(server, secret, timeout=60):
    """GET /codis/rdb/latest and return ``(status, body, headers)``."""
    url = "http://{}:{}{}".format(server["host"], server["port"], EXPORT_PATH)
    headers = {EXPORT_HEADER: secret} if secret is not None else None
    return vlib.http_raw("GET", url, headers=headers, timeout=timeout)


def lower_headers(headers):
    return {str(key).lower(): value for key, value in (headers or {}).items()}


def store_artifact(workdir, server, body):
    """Write the exported bytes to <workdir>/artifacts/dump-<port>.rdb."""
    artifacts = Path(workdir) / "artifacts"
    artifacts.mkdir(parents=True, exist_ok=True)
    target = artifacts / "dump-{}.rdb".format(server["port"])
    target.write_bytes(body)
    return {
        "path": str(target),
        "size": len(body),
        "sha256": hashlib.sha256(body).hexdigest(),
    }


def multipart_body(file_path, fields):
    """Build a multipart/form-data body with a random boundary."""
    boundary = "----codisvalidation" + secrets.token_hex(12)
    file_name = Path(file_path).name
    chunks = []
    for name, value in fields.items():
        chunks.append(("--{}\r\n".format(boundary)).encode("utf-8"))
        chunks.append(
            (
                'Content-Disposition: form-data; name="{}"\r\n\r\n{}\r\n'.format(name, value)
            ).encode("utf-8")
        )
    chunks.append(("--{}\r\n".format(boundary)).encode("utf-8"))
    chunks.append(
        (
            'Content-Disposition: form-data; name="file"; filename="{}"\r\n'
            "Content-Type: application/octet-stream\r\n\r\n".format(file_name)
        ).encode("utf-8")
    )
    chunks.append(Path(file_path).read_bytes())
    chunks.append(b"\r\n")
    chunks.append(("--{}--\r\n".format(boundary)).encode("utf-8"))
    return b"".join(chunks), boundary


def temp_files(workspace):
    """Return the dashboard temp files left under uploads/ and remote/."""
    names = []
    for sub in ("uploads", "remote"):
        directory = Path(workspace) / sub
        if not directory.is_dir():
            continue
        for entry in sorted(directory.iterdir()):
            if entry.is_file():
                names.append("{}/{}".format(sub, entry.name))
    return names


def slim_summary(entries, limit=10):
    return [
        {
            "name": entry.get("name"),
            "db": entry.get("db"),
            "size": entry.get("size"),
            "key_count": entry.get("key_count"),
        }
        for entry in (entries or [])[:limit]
    ]


def slim_keys(entries, limit=10):
    return [
        {
            "db": entry.get("db"),
            "key": entry.get("key"),
            "type": entry.get("type"),
            "size": entry.get("size"),
            "element_count": entry.get("element_count"),
        }
        for entry in (entries or [])[:limit]
    ]


def ensure_workspace_copy(ctx):
    """Copy the exported artifact under rdb_analysis_workspace; return (name, path)."""
    workspace = Path(ctx.workspace)
    workspace.mkdir(parents=True, exist_ok=True)
    name = "ws-{}".format(Path(ctx.artifact["path"]).name)
    target = workspace / name
    if ctx.workspace_file is None or not target.is_file():
        target.write_bytes(Path(ctx.artifact["path"]).read_bytes())
        ctx.workspace_file = target
    return name, target


def record_job(case, job, **extra):
    data = {
        "job_id": job.get("id"),
        "status": job.get("status"),
        "source": job.get("source"),
        "file_size": job.get("file_size"),
        "bytes_read": job.get("bytes_read"),
        "objects_read": job.get("objects_read"),
        "db_count": job.get("db_count"),
        "total_size": job.get("total_size"),
        "error": job.get("error"),
        "type_summary": slim_summary(job.get("type_summary")),
        "db_summary": slim_summary(job.get("db_summary")),
        "top_big_keys": slim_keys(job.get("top_big_keys")),
    }
    data.update(extra)
    case.set(**data)
    return data


def wait_job(api, job_id, timeout, poll=0.3):
    """Poll an RDB analysis job until it reaches a terminal status."""
    deadline = time.time() + timeout
    last = None
    while time.time() < deadline:
        status, job = api.get(job_id)
        if status != 200 or not isinstance(job, dict):
            raise vlib.ValidationError(
                "GET rdb-analysis job {} returned http {}: {}".format(job_id, status, job)
            )
        last = job
        if job.get("status") in TERMINAL_STATUSES:
            return job
        time.sleep(poll)
    raise vlib.ValidationError(
        "rdb analysis job {} stayed non-terminal for {}s (last status={})".format(
            job_id, timeout, (last or {}).get("status")
        )
    )


# ---------------------------------------------------------------------------
# dashboard API
# ---------------------------------------------------------------------------


class RdbApi(object):
    """Thin client for the /api/topom/rdb-analysis/* endpoints."""

    def __init__(self, dashboard_addr, xauth):
        self.base = "http://{}".format(str(dashboard_addr).rstrip("/"))
        self.root = self.base + "/api/topom/rdb-analysis/"
        self.xauth = xauth

    def _token(self, token):
        return self.xauth if token is None else token

    def get(self, job_id, token=None):
        return vlib.http_json(
            "GET", "{}{}/{}".format(self.root, self._token(token), job_id)
        )

    def remote_fetch(self, server_addr, options, token=None):
        return vlib.http_json(
            "PUT",
            "{}remote-fetch/{}".format(self.root, self._token(token)),
            {"server_addr": server_addr, "options": dict(options)},
        )

    def start(self, path, options, token=None):
        return vlib.http_json(
            "PUT",
            "{}start/{}".format(self.root, self._token(token)),
            {"path": path, "options": dict(options)},
        )

    def upload(self, file_path, fields, token=None):
        body, boundary = multipart_body(file_path, fields)
        return vlib.http_json(
            "POST",
            "{}upload/{}".format(self.root, self._token(token)),
            body,
            {"Content-Type": "multipart/form-data; boundary={}".format(boundary)},
            timeout=300,
        )

    def cancel(self, job_id, token=None):
        return vlib.http_json(
            "PUT", "{}cancel/{}/{}".format(self.root, self._token(token), job_id)
        )

    def remove(self, job_id, token=None):
        return vlib.http_json(
            "PUT", "{}remove/{}/{}".format(self.root, self._token(token), job_id)
        )


# ---------------------------------------------------------------------------
# bookkeeping
# ---------------------------------------------------------------------------


class JobRegistry(object):
    """Every analysis job this script created."""

    def __init__(self):
        self.items = []

    def track(self, job_id, case_id, source):
        self.items.append(
            {"id": job_id, "case": case_id, "source": source, "removed": False}
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

    def __init__(self, repo, cluster, workdir, run_id, timeout):
        self.repo = Path(repo)
        self.cluster = cluster
        self.workdir = Path(workdir)
        self.run_id = run_id
        self.timeout = int(timeout)
        self.product = cluster.get("product") or ""
        self.xauth = cluster.get("xauth") or vlib.xauth_for(self.product)
        self.features = cluster.get("features") or {}
        self.secret = self.features.get("rdb_export_auth") or ""
        dashboard = cluster.get("dashboard") or {}
        self.dashboard = dashboard
        self.api = (
            RdbApi(dashboard.get("addr"), self.xauth) if dashboard.get("addr") else None
        )
        self.conf_text = vlib.read_text(dashboard.get("conf")) if dashboard.get("conf") else ""
        self.workspace = None
        self.dbfilename = None
        self.master, self.replica = self._pick_targets()
        self.jobs = JobRegistry()
        self.artifact = None
        self.remote_job = None
        self.workspace_file = None
        self.temps_before = []
        self.precheck_ok = False
        self.precheck_reason = None

    def _pick_targets(self):
        groups = self.cluster.get("groups") or []
        master = None
        replica = None
        if groups:
            servers = groups[0].get("servers") or []
            if servers:
                master = servers[0]
                for server in servers[1:]:
                    if server.get("role") == "replica":
                        replica = server
                        break
        return master, replica

    def guard(self, case):
        if self.precheck_ok:
            return False
        case.blocked(self.precheck_reason or "rdb-precheck did not pass")
        return True


def remove_job(case, ctx, item, settle_timeout=30):
    """Cancel (when needed) and remove one tracked job."""
    result = {"id": item["id"], "case": item["case"], "source": item["source"]}
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
    if job.get("status") not in TERMINAL_STATUSES:
        cancel_status, cancel_body = ctx.api.cancel(item["id"])
        result["cancel_http_status"] = cancel_status
        result["cancel_body"] = (
            cancel_body if isinstance(cancel_body, str) else cause_of(cancel_body)
        )
        job = wait_job(ctx.api, item["id"], settle_timeout)
    result["job_status"] = job.get("status")
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
        "rdb-precheck",
        "RDB export, dashboard remote fetch and the analysis workspace are configured",
    ) as case:
        case.set(
            product=ctx.product,
            dashboard=ctx.dashboard.get("addr"),
            dashboard_conf=ctx.dashboard.get("conf"),
            features={
                "rdb_export": bool(ctx.features.get("rdb_export")),
                "rdb_export_auth_present": bool(ctx.secret),
            },
            master=ctx.master.get("addr") if ctx.master else None,
        )
        problems = []
        if ctx.api is None:
            problems.append("cluster.json has no dashboard.addr")
        if ctx.master is None:
            problems.append("cluster.json has no group master")
        if not ctx.features.get("rdb_export"):
            problems.append("cluster.json features.rdb_export is false")
        if not ctx.secret:
            problems.append("cluster.json features.rdb_export_auth is empty")

        remote_enabled = bool(REMOTE_ENABLED_RE.search(ctx.conf_text))
        auth_match = REMOTE_AUTH_RE.search(ctx.conf_text)
        workspace_match = WORKSPACE_RE.search(ctx.conf_text)
        ctx.workspace = workspace_match.group(1).strip() if workspace_match else None
        case.set(
            remote_fetch_enabled_in_conf=remote_enabled,
            remote_fetch_auth_in_conf=bool(auth_match and auth_match.group(1).strip()),
            workspace=ctx.workspace,
        )
        if not remote_enabled:
            problems.append("dashboard conf has no rdb_analysis_remote_fetch_enabled = true")
        if not (auth_match and auth_match.group(1).strip()):
            problems.append("dashboard conf has no rdb_analysis_remote_fetch_auth")
        if not ctx.workspace:
            problems.append("dashboard conf has no rdb_analysis_workspace")

        if ctx.master is not None:
            if ping(ctx.master) != "PONG":
                problems.append("master {} does not answer PING".format(ctx.master["addr"]))
            dbfilename = config_get(ctx.master, "dbfilename")
            ctx.dbfilename = dbfilename
            export_enabled = config_get(ctx.master, "codis-rdb-export-enabled")
            case.set(
                dbfilename=dbfilename,
                codis_rdb_export_enabled=export_enabled,
                dir=config_get(ctx.master, "dir"),
            )
            if not dbfilename:
                problems.append("master CONFIG GET dbfilename is empty")
            if str(export_enabled).lower() != "yes":
                problems.append(
                    "master CONFIG GET codis-rdb-export-enabled is {!r}".format(export_enabled)
                )

        if ctx.replica is not None:
            case.set(replica=ctx.replica["addr"], replica_rdb_export=ctx.replica.get("rdb_export"))

        if ctx.workspace:
            ctx.temps_before = temp_files(ctx.workspace)
            case.set(workspace_temp_files_before=ctx.temps_before)

        if problems:
            ctx.precheck_ok = False
            ctx.precheck_reason = "; ".join(problems)
            case.blocked(ctx.precheck_reason)
            return
        ctx.precheck_ok = True


def case_export_direct(evidence, ctx):
    with evidence.case(
        "rdb-export-direct",
        "GET /codis/rdb/latest streams the saved dbfilename and enforces X-Codis-RDB-Auth",
    ) as case:
        if ctx.guard(case):
            return
        save = save_rdb(ctx.master)
        case.set(save=save)
        require(save["save"] == "OK", "master SAVE returned {!r}".format(save["save"]))
        require(
            save["rdb_bgsave_in_progress"] == "0",
            "bgsave still in progress after SAVE: {}".format(save),
        )

        no_auth_status, no_auth_body, _ = export_rdb(ctx.master, None)
        wrong_status, wrong_body, _ = export_rdb(ctx.master, "wrong-secret")
        status, body, headers = export_rdb(ctx.master, ctx.secret)
        lower = lower_headers(headers)
        case.set(
            no_auth={"http_status": no_auth_status, "bytes": len(no_auth_body)},
            wrong_auth={"http_status": wrong_status, "bytes": len(wrong_body)},
            with_auth={
                "http_status": status,
                "bytes": len(body),
                "magic": body[:5].decode("utf-8", "replace"),
                "content_disposition": lower.get("content-disposition"),
                "mtime": lower.get("x-codis-rdb-mtime"),
                "content_length": lower.get("content-length"),
            },
        )
        require(
            no_auth_status == 403,
            "export without the auth header returned {}".format(no_auth_status),
        )
        require(
            wrong_status == 403,
            "export with a wrong auth header returned {}".format(wrong_status),
        )
        require(status == 200, "export with the correct auth header returned {}".format(status))
        require(
            body[:5] == b"REDIS",
            "exported body does not start with REDIS: {!r}".format(body[:16]),
        )
        require("content-disposition" in lower, "export response has no Content-Disposition header")
        require("x-codis-rdb-mtime" in lower, "export response has no X-Codis-RDB-Mtime header")
        disposition = str(lower.get("content-disposition"))
        expected_name = ctx.dbfilename or "dump-{}.rdb".format(ctx.master["port"])
        require(
            'filename="{}"'.format(expected_name) in disposition,
            "Content-Disposition {!r} does not name {}".format(disposition, expected_name),
        )

        ctx.artifact = store_artifact(ctx.workdir, ctx.master, body)
        case.set(artifact=ctx.artifact)
        require(
            Path(ctx.artifact["path"]).is_file(),
            "artifact {} was not written".format(ctx.artifact["path"]),
        )
        case.step(
            "auth guard verified and the export stored as an artifact",
            artifact=ctx.artifact["path"],
            size=ctx.artifact["size"],
        )


def case_remote_fetch(evidence, ctx):
    with evidence.case(
        "rdb-remote-fetch",
        "write one key per type, SAVE, fetch the RDB through the dashboard and assert the analysis",
    ) as case:
        if ctx.guard(case):
            return
        prefix = "rdb:{}".format(ctx.run_id)
        dataset = write_dataset(ctx.master, prefix)
        case.set(
            prefix=prefix,
            keys=dataset["keys"],
            key_exists=dataset["exists"],
            stream_len=dataset["stream_len"],
            dbsize=dataset["dbsize"],
        )
        require(
            dataset["stream_len"] == 3,
            "XLEN of the stream is {} not 3".format(dataset["stream_len"]),
        )
        require(
            all(value == 1 for value in dataset["exists"].values()),
            "not every dataset key exists: {}".format(dataset["exists"]),
        )

        save = save_rdb(ctx.master)
        case.set(save=save)
        require(save["save"] == "OK", "master SAVE returned {!r}".format(save["save"]))

        # The export endpoint serves whatever dbfilename SAVE wrote, so refresh
        # the artifact here: upload/workspace then analyse the exact same bytes
        # the dashboard pulls in this case.
        status, body, headers = export_rdb(ctx.master, ctx.secret)
        require(status == 200, "export before remote fetch returned {}".format(status))
        ctx.artifact = store_artifact(ctx.workdir, ctx.master, body)
        case.set(artifact=ctx.artifact)

        status, created = ctx.api.remote_fetch(ctx.master["addr"], DEFAULT_OPTIONS)
        require(
            status == 200,
            "remote fetch create returned http {}: {}".format(status, cause_of(created)),
        )
        job_id = created.get("id") if isinstance(created, dict) else None
        require(bool(job_id), "remote fetch returned no job id: {}".format(created))
        ctx.jobs.track(job_id, "rdb-remote-fetch", "remote-http")
        case.set(job_id=job_id)

        job = wait_job(ctx.api, job_id, ctx.timeout)
        record_job(case, job)
        require(
            job.get("status") == "done",
            "remote fetch job ended {} instead of done: {}".format(
                job.get("status"), job.get("error")
            ),
        )
        source = job.get("source") or ""
        require(
            source.startswith("remote-http:"),
            "job source {!r} does not start with remote-http:".format(source),
        )
        require(
            ctx.master["addr"] in source,
            "job source {!r} does not contain the target {}".format(source, ctx.master["addr"]),
        )
        require(
            job.get("file_size") == len(body),
            "job file_size {} does not match the exported {} bytes".format(
                job.get("file_size"), len(body)
            ),
        )
        require(
            job.get("objects_read", 0) >= 6,
            "objects_read={} is below the 6 written keys".format(job.get("objects_read")),
        )
        require(job.get("db_count", 0) >= 1, "db_count={} is below 1".format(job.get("db_count")))

        stream_key = dataset["keys"]["stream"]
        big_keys = job.get("top_big_keys") or []
        stream_entry = next((entry for entry in big_keys if entry.get("key") == stream_key), None)
        case.set(
            stream_key=stream_key,
            stream_in_big_keys=bool(stream_entry),
            stream_entry=stream_entry,
            top_big_key_count=len(big_keys),
            type_summary=slim_summary(job.get("type_summary")),
        )
        require(
            stream_entry is not None,
            "stream key {} is missing from top_big_keys (top_n={}): {}".format(
                stream_key, DEFAULT_OPTIONS["top_n"], slim_keys(big_keys, limit=20)
            ),
        )
        require(
            stream_entry.get("element_count") == 3,
            "stream {} element_count is {} not 3: {}".format(
                stream_key, stream_entry.get("element_count"), stream_entry
            ),
        )
        require(
            stream_entry.get("type") == "stream",
            "stream {} is reported as type {!r}".format(stream_key, stream_entry.get("type")),
        )
        ctx.remote_job = {
            "id": job_id,
            "objects_read": job.get("objects_read"),
            "file_size": job.get("file_size"),
        }
        case.step(
            "remote fetch analysed the live RDB",
            source=source,
            objects_read=job.get("objects_read"),
            file_size=job.get("file_size"),
            stream_element_count=stream_entry.get("element_count"),
        )


def case_upload(evidence, ctx):
    with evidence.case(
        "rdb-upload", "multipart upload of the exported RDB matches the remote fetch result"
    ) as case:
        if ctx.guard(case):
            return
        require(ctx.artifact is not None, "no exported artifact is available to upload")
        require(
            ctx.remote_job is not None,
            "the remote fetch job is missing, objects_read cannot be compared",
        )
        artifact_path = Path(ctx.artifact["path"])
        require(artifact_path.is_file(), "artifact {} is gone".format(artifact_path))
        fields = {
            "top_n": str(DEFAULT_OPTIONS["top_n"]),
            "max_depth": str(DEFAULT_OPTIONS["max_depth"]),
            "include_expired": "false",
        }
        case.set(
            upload_file=str(artifact_path),
            upload_bytes=artifact_path.stat().st_size,
            form_fields=fields,
        )

        status, created = ctx.api.upload(artifact_path, fields)
        require(status == 200, "upload returned http {}: {}".format(status, cause_of(created)))
        job_id = created.get("id") if isinstance(created, dict) else None
        require(bool(job_id), "upload returned no job id: {}".format(created))
        ctx.jobs.track(job_id, "rdb-upload", "upload:{}".format(artifact_path.name))
        case.set(job_id=job_id)

        job = wait_job(ctx.api, job_id, ctx.timeout)
        record_job(case, job)
        require(
            job.get("status") == "done",
            "upload job ended {} instead of done: {}".format(job.get("status"), job.get("error")),
        )
        require(
            (job.get("source") or "") == "upload:{}".format(artifact_path.name),
            "upload job source {!r} is not upload:{}".format(job.get("source"), artifact_path.name),
        )
        require(
            job.get("file_size") == artifact_path.stat().st_size,
            "upload job file_size {} does not match {} bytes on disk".format(
                job.get("file_size"), artifact_path.stat().st_size
            ),
        )
        require(
            job.get("objects_read") == ctx.remote_job["objects_read"],
            "upload objects_read {} does not equal remote fetch {}".format(
                job.get("objects_read"), ctx.remote_job["objects_read"]
            ),
        )
        case.step(
            "upload result matches the remote fetch",
            objects_read=job.get("objects_read"),
            remote_objects_read=ctx.remote_job["objects_read"],
        )


def case_workspace(evidence, ctx):
    with evidence.case(
        "rdb-workspace",
        "workspace path under rdb_analysis_workspace matches the remote fetch result",
    ) as case:
        if ctx.guard(case):
            return
        require(ctx.workspace, "rdb_analysis_workspace is unknown")
        require(ctx.artifact is not None, "no exported artifact is available to copy")
        require(
            ctx.remote_job is not None,
            "the remote fetch job is missing, objects_read cannot be compared",
        )
        name, target = ensure_workspace_copy(ctx)
        case.set(
            workspace=str(Path(ctx.workspace)),
            workspace_file=str(target),
            workspace_relative=name,
        )

        status, created = ctx.api.start(name, DEFAULT_OPTIONS)
        require(
            status == 200,
            "workspace start returned http {}: {}".format(status, cause_of(created)),
        )
        job_id = created.get("id") if isinstance(created, dict) else None
        require(bool(job_id), "workspace start returned no job id: {}".format(created))
        ctx.jobs.track(job_id, "rdb-workspace", "workspace:{}".format(name))
        case.set(job_id=job_id)

        job = wait_job(ctx.api, job_id, ctx.timeout)
        record_job(case, job)
        require(
            job.get("status") == "done",
            "workspace job ended {} instead of done: {}".format(
                job.get("status"), job.get("error")
            ),
        )
        require(
            (job.get("source") or "") == "workspace:{}".format(name),
            "workspace job source {!r} is not workspace:{}".format(job.get("source"), name),
        )
        require(
            job.get("objects_read") == ctx.remote_job["objects_read"],
            "workspace objects_read {} does not equal remote fetch {}".format(
                job.get("objects_read"), ctx.remote_job["objects_read"]
            ),
        )
        case.step(
            "workspace result matches the remote fetch",
            objects_read=job.get("objects_read"),
            remote_objects_read=ctx.remote_job["objects_read"],
        )


def case_rejects(evidence, ctx):
    with evidence.case(
        "rdb-rejects",
        "invalid server_addr / workspace path / regex and unknown job ids are refused",
    ) as case:
        if ctx.guard(case):
            return
        checks = []

        for label, addr in (
            ("scheme", "http://{}".format(ctx.master["addr"])),
            ("path", "{}{}".format(ctx.master["addr"], EXPORT_PATH)),
            ("query", "{}?auth=secret".format(ctx.master["addr"])),
        ):
            status, body = ctx.api.remote_fetch(addr, DEFAULT_OPTIONS)
            text = cause_of(body)
            checks.append(
                {
                    "check": "server_addr-" + label,
                    "value": addr,
                    "http_status": status,
                    "cause": text,
                }
            )
            require(
                status == 800,
                "remote fetch with server_addr {} returned {}".format(addr, status),
            )
            require(
                "invalid rdb analysis remote fetch server_addr" in text,
                "server_addr {!r} cause {!r} is not the invalid-addr error".format(addr, text),
            )

        outside_addr = "127.0.0.1:1"
        status, body = ctx.api.remote_fetch(outside_addr, DEFAULT_OPTIONS)
        text = cause_of(body)
        checks.append(
            {
                "check": "server_addr-not-in-product",
                "value": outside_addr,
                "http_status": status,
                "cause": text,
            }
        )
        require(status == 800, "remote fetch for a foreign server returned {}".format(status))
        require(
            "is not in current product" in text,
            "foreign server cause {!r} does not contain 'is not in current product'".format(text),
        )

        for outside in ("/etc/passwd", "../outside.rdb"):
            status, body = ctx.api.start(outside, DEFAULT_OPTIONS)
            text = cause_of(body)
            checks.append(
                {
                    "check": "workspace-outside",
                    "value": outside,
                    "http_status": status,
                    "cause": text,
                }
            )
            require(status == 800, "workspace start with {!r} returned {}".format(outside, status))
            require(
                "rdb analysis path is outside workspace" in text,
                "path {!r} cause {!r} is not the outside-workspace error".format(outside, text),
            )

        # An illegal regex is compiled when the job starts parsing, so it shows up
        # either as a synchronous 800 or as a finished job with status=error.
        require(ctx.artifact is not None, "no exported artifact is available for the regex check")
        name, _target = ensure_workspace_copy(ctx)
        bad_options = dict(DEFAULT_OPTIONS)
        bad_options["regex"] = "(["
        status, body = ctx.api.start(name, bad_options)
        text = cause_of(body)
        regex_check = {"check": "illegal-regex", "http_status": status, "cause": text}
        if status == 800:
            require(
                "illegal regex expression" in text,
                "illegal regex cause {!r} does not contain 'illegal regex expression'".format(text),
            )
            regex_check["mode"] = "create-800"
        else:
            require(
                status == 200,
                "workspace start with an illegal regex returned {}".format(status),
            )
            job_id = body.get("id") if isinstance(body, dict) else None
            require(bool(job_id), "illegal regex job returned no id: {}".format(body))
            ctx.jobs.track(job_id, "rdb-rejects", "workspace:{}".format(name))
            job = wait_job(ctx.api, job_id, ctx.timeout)
            record_job(case, job)
            require(
                job.get("status") == "error",
                "job with an illegal regex ended {} instead of error".format(job.get("status")),
            )
            require(
                "illegal regex expression" in (job.get("error") or ""),
                "illegal regex job error {!r} does not contain 'illegal regex expression'".format(
                    job.get("error")
                ),
            )
            regex_check["mode"] = "job-error"
            regex_check["job_id"] = job_id
            regex_check["job_status"] = job.get("status")
            regex_check["job_error"] = job.get("error")
            removed = remove_job(case, ctx, ctx.jobs.item_for(job_id))
            regex_check["removed"] = removed.get("status")
        checks.append(regex_check)

        status, body = ctx.api.get("nonexistent-job-id")
        text = cause_of(body)
        checks.append({"check": "unknown-job", "http_status": status, "cause": text})
        require(status == 800, "GET of an unknown job id returned {}".format(status))
        require(
            "rdb analysis job" in text and "not found" in text,
            "unknown job cause {!r} does not match the not-found error".format(text),
        )

        case.set(checks=checks)
        case.step("all invalid inputs were refused", checks=len(checks))


def case_cleanup(evidence, ctx):
    """Remove every job this run created and drop the workspace copy."""
    with evidence.case(
        "rdb-cleanup", "remove every RDB analysis job created by this run"
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

        failures = [
            result
            for result in results
            if result.get("status") not in ("removed", "already_removed", "missing")
        ]
        require(not failures, "jobs that could not be removed: {}".format(failures))

        gone = []
        for job_id in ctx.jobs.ids():
            status, body = ctx.api.get(job_id) if ctx.api else (None, None)
            gone.append(
                {
                    "id": job_id,
                    "http_status": status,
                    "cause": cause_of(body),
                    "not_found": status == 800 and "not found" in cause_of(body),
                }
            )
        case.set(verified_removed=gone)
        missing = [entry for entry in gone if not entry["not_found"]]
        require(not missing, "jobs still readable after remove: {}".format(missing))

        workspace_files = []
        if ctx.workspace_file is not None:
            try:
                if ctx.workspace_file.is_file():
                    ctx.workspace_file.unlink()
                workspace_files.append(
                    {
                        "path": str(ctx.workspace_file),
                        "removed": not ctx.workspace_file.exists(),
                    }
                )
            except OSError as err:
                workspace_files.append({"path": str(ctx.workspace_file), "error": str(err)})
        temps_after = temp_files(ctx.workspace) if ctx.workspace else []
        case.set(
            workspace_files=workspace_files,
            workspace_temp_files_before=ctx.temps_before,
            workspace_temp_files_after=temps_after,
        )
        require(
            len(temps_after) <= len(ctx.temps_before),
            "dashboard temp files leaked: before={} after={}".format(
                ctx.temps_before, temps_after
            ),
        )
        if ctx.master is not None:
            case.set(master_ping=ping(ctx.master))
            require(
                ping(ctx.master) == "PONG",
                "master {} stopped answering PING".format(ctx.master["addr"]),
            )
        case.step("workspace copy and dashboard temp files are clean", temp_files=temps_after)


# ---------------------------------------------------------------------------
# main
# ---------------------------------------------------------------------------


def build_parser():
    parser = argparse.ArgumentParser(
        description=(
            "Real-machine validation of Codis RDB Analysis "
            "(remote fetch / upload / workspace)."
        ),
    )
    parser.add_argument("--cluster", required=True, help="path to cluster.json")
    parser.add_argument(
        "--output",
        help="evidence path (default: <repo>/.runtime/realmachine/run_rdb_analysis-<run-id>.json)",
    )
    parser.add_argument("--repo", help="repository root (default: auto-detected)")
    parser.add_argument(
        "--timeout",
        type=int,
        default=300,
        help="maximum seconds to poll a single analysis job (default: 300)",
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
        else repo / ".runtime" / "realmachine" / "run_rdb_analysis-{}.json".format(run_id)
    )
    workdir = Path(cluster.get("workdir") or (repo / ".runtime")).expanduser().resolve()

    evidence = vlib.Evidence(
        script=SCRIPT, feature=FEATURE, scope=SCOPE, cluster_path=cluster_path
    )
    evidence.attach_repo(repo)

    ctx = Context(repo=repo, cluster=cluster, workdir=workdir, run_id=run_id, timeout=args.timeout)
    evidence.note("cluster: {} (product={})".format(cluster_path, ctx.product))
    evidence.note("workdir: {}".format(workdir))
    evidence.note(
        "targets: master={} replica={}".format(
            ctx.master.get("addr") if ctx.master else None,
            ctx.replica.get("addr") if ctx.replica else None,
        )
    )
    evidence.note(
        "the export endpoint serves the existing dbfilename only (no SAVE/ BGSAVE); "
        "remote-fetch/upload/workspace all parse the same artifact so objects_read "
        "must match"
    )

    def run():
        try:
            case_precheck(evidence, ctx)
            case_export_direct(evidence, ctx)
            case_remote_fetch(evidence, ctx)
            case_upload(evidence, ctx)
            case_workspace(evidence, ctx)
            case_rejects(evidence, ctx)
        finally:
            case_cleanup(evidence, ctx)
        return evidence.exit_code()

    return vlib.main_guard(evidence, run, output)


if __name__ == "__main__":
    sys.exit(main())
