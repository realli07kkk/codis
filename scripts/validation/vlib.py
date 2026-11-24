#!/usr/bin/env python3
"""Shared helpers for the Codis real-machine validation scripts.

Pure standard library, Python 3.10 compatible, runs on Linux (Ubuntu 22.04)
targets and on macOS developer laptops.

This module is imported by the scripts under ``scripts/validation/``; it is not
meant to be executed directly.
"""

import contextlib
import datetime
import hashlib
import json
import os
import platform
import signal
import socket
import subprocess
import sys
import time
import urllib.error
import urllib.request
from pathlib import Path

TRUNCATE_LIMIT = 4000


class ValidationError(Exception):
    """Raised when a validation step cannot continue."""


class RedisError(Exception):
    """Error reply ("-ERR ...") returned by a RESP server."""


# ---------------------------------------------------------------------------
# logging / time
# ---------------------------------------------------------------------------


def utc_now():
    """Return an ISO-8601 UTC timestamp such as ``2026-11-24T12:00:00Z``."""
    return (
        datetime.datetime.now(datetime.timezone.utc)
        .replace(microsecond=0)
        .isoformat()
        .replace("+00:00", "Z")
    )


def new_run_id():
    """Return a sortable run id such as ``20261124T120000Z``."""
    return datetime.datetime.now(datetime.timezone.utc).strftime("%Y%m%dT%H%M%SZ")


def log(msg):
    """Write a timestamped line to stderr."""
    sys.stderr.write("[{}] {}\n".format(utc_now(), msg))
    sys.stderr.flush()


# ---------------------------------------------------------------------------
# repository / host metadata
# ---------------------------------------------------------------------------


def find_repo(explicit=None):
    """Locate the repository root.

    ``explicit`` wins; otherwise walk up from this file looking for a ``go.mod``
    declaring ``module github.com/CodisLabs/codis``.
    """
    if explicit:
        root = Path(explicit).expanduser().resolve()
        if not (root / "go.mod").is_file():
            raise ValidationError("no go.mod under --repo path: {}".format(root))
        return root

    here = Path(__file__).resolve()
    for candidate in [here] + list(here.parents):
        go_mod = candidate / "go.mod"
        if not go_mod.is_file():
            continue
        try:
            head = go_mod.read_text(encoding="utf-8", errors="replace")
        except OSError:
            continue
        if "module github.com/CodisLabs/codis" in head:
            return candidate
    raise ValidationError(
        "cannot locate the Codis repository root (no go.mod with "
        "'module github.com/CodisLabs/codis' above {})".format(here)
    )


def _git(repo, *args, timeout=30):
    proc = subprocess.run(
        ["git"] + [str(a) for a in args],
        cwd=str(repo),
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        timeout=timeout,
    )
    if proc.returncode != 0:
        raise ValidationError(
            "git {} failed: {}".format(
                " ".join(str(a) for a in args),
                proc.stderr.decode("utf-8", "replace").strip(),
            )
        )
    return proc.stdout.decode("utf-8", "replace")


def git_head(repo):
    """Return ``{"commit", "branch", "dirty", "dirty_files"}`` for ``repo``."""
    repo = Path(repo)
    try:
        commit = _git(repo, "rev-parse", "HEAD").strip()
        branch = _git(repo, "rev-parse", "--abbrev-ref", "HEAD").strip()
        porcelain = _git(repo, "status", "--porcelain")
    except (ValidationError, OSError, subprocess.SubprocessError) as err:
        return {
            "commit": None,
            "branch": None,
            "dirty": None,
            "dirty_files": [],
            "error": str(err),
        }
    dirty_files = [line for line in porcelain.splitlines() if line.strip()]
    return {
        "commit": commit,
        "branch": branch,
        "dirty": bool(dirty_files),
        "dirty_files": dirty_files[:20],
    }


def host_summary():
    """Return a small host descriptor used in every evidence file."""
    return {
        "platform": platform.platform(),
        "machine": platform.machine(),
        "python": sys.version.split()[0],
    }


# ---------------------------------------------------------------------------
# process execution
# ---------------------------------------------------------------------------


def _truncate(value):
    if value is None:
        return ""
    text = str(value)
    if len(text) <= TRUNCATE_LIMIT:
        return text
    return text[:TRUNCATE_LIMIT] + "\n...[truncated]"


def run(args, cwd=None, timeout=120, check=True, env=None, log_path=None, truncate=True):
    """Run a command and return a structured result dict.

    ``stdout``/``stderr`` are truncated to 4000 characters in the returned
    dict; ``log_path`` receives the full merged output when given. Pass
    ``truncate=False`` when the caller needs to parse the complete output
    (large JSON payloads such as ``--slots-status`` or ``npm audit --json``).
    """
    command = [str(a) for a in args]
    cwd_str = str(cwd) if cwd is not None else None
    child_env = None
    if env:
        child_env = dict(os.environ)
        child_env.update({str(k): str(v) for k, v in env.items()})

    started = time.monotonic()
    stdout = ""
    stderr = ""
    returncode = None
    timed_out = False
    try:
        proc = subprocess.run(
            command,
            cwd=cwd_str,
            env=child_env,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            timeout=timeout,
        )
        returncode = proc.returncode
        stdout = proc.stdout.decode("utf-8", "replace")
        stderr = proc.stderr.decode("utf-8", "replace")
    except subprocess.TimeoutExpired as err:
        timed_out = True
        returncode = -1
        stdout = _decode(err.stdout)
        stderr = _decode(err.stderr) + "\n[timeout after {}s, command killed]".format(timeout)
    except FileNotFoundError as err:
        returncode = -1
        stderr = "command not found: {}".format(err)
    except OSError as err:
        returncode = -1
        stderr = "failed to start command: {}".format(err)

    duration = time.monotonic() - started
    result = {
        "command": command,
        "cwd": cwd_str,
        "returncode": returncode,
        "stdout": _truncate(stdout) if truncate else stdout,
        "stderr": _truncate(stderr) if truncate else stderr,
        "duration_sec": round(duration, 3),
        "timed_out": timed_out,
    }

    if log_path:
        target = Path(log_path)
        target.parent.mkdir(parents=True, exist_ok=True)
        with target.open("a", encoding="utf-8") as handle:
            handle.write("$ {}\n".format(" ".join(command)))
            if stdout:
                handle.write(stdout)
                if not stdout.endswith("\n"):
                    handle.write("\n")
            if stderr:
                handle.write(stderr)
                if not stderr.endswith("\n"):
                    handle.write("\n")
            handle.write("[exit {} in {:.1f}s]\n".format(returncode, duration))

    if check and returncode != 0:
        raise ValidationError(json.dumps(result, indent=2, sort_keys=True))
    return result


def _decode(value):
    if value is None:
        return ""
    if isinstance(value, bytes):
        return value.decode("utf-8", "replace")
    return str(value)


def read_text(path, limit=None):
    """Read a text file, returning "" when it is missing."""
    try:
        text = Path(path).read_text(encoding="utf-8", errors="replace")
    except OSError:
        return ""
    if limit is not None and len(text) > limit:
        return text[:limit] + "...[truncated]"
    return text


def sha256_file(path):
    """Return the sha256 hex digest of a file, or None when unreadable."""
    digest = hashlib.sha256()
    try:
        with Path(path).open("rb") as handle:
            for chunk in iter(lambda: handle.read(1024 * 1024), b""):
                digest.update(chunk)
    except OSError:
        return None
    return digest.hexdigest()


def require_binaries(bin_dir, names):
    """Raise ValidationError listing every missing binary."""
    bin_dir = Path(bin_dir)
    missing = [name for name in names if not (bin_dir / name).is_file()]
    if missing:
        raise ValidationError(
            "missing binaries under {}: {}".format(bin_dir, ", ".join(missing))
        )
    return [bin_dir / name for name in names]


# ---------------------------------------------------------------------------
# networking helpers
# ---------------------------------------------------------------------------


def split_addr(addr):
    """Split ``host:port`` / ``[v6]:port`` / integer into ``(host, port)``."""
    if isinstance(addr, int):
        return "127.0.0.1", addr
    text = str(addr).strip()
    if text.isdigit():
        return "127.0.0.1", int(text)
    if text.startswith("["):
        host, _, port = text[1:].partition("]:")
        return host, int(port)
    host, _, port = text.rpartition(":")
    if not host:
        return "127.0.0.1", int(port)
    return host, int(port)


def free_port():
    """Reserve and release a loopback port, returning the number."""
    last_error = None
    for _ in range(20):
        sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        try:
            sock.bind(("127.0.0.1", 0))
            return sock.getsockname()[1]
        except OSError as err:
            last_error = err
        finally:
            sock.close()
    raise ValidationError("cannot allocate a free port: {}".format(last_error))


def port_is_open(port, host="127.0.0.1", timeout=0.5):
    try:
        with socket.create_connection((host, int(port)), timeout=timeout):
            return True
    except OSError:
        return False


def wait_tcp(addr, timeout=20):
    """Wait until a TCP connect to ``addr`` succeeds."""
    host, port = split_addr(addr)
    deadline = time.time() + timeout
    last_error = None
    while time.time() < deadline:
        try:
            with socket.create_connection((host, port), timeout=0.5):
                return True
        except OSError as err:
            last_error = err
            time.sleep(0.2)
    raise ValidationError(
        "tcp {}:{} did not open within {}s: {}".format(host, port, timeout, last_error)
    )


def wait_port_closed(port, host="127.0.0.1", timeout=10):
    """Wait until nothing listens on ``port``; True when released."""
    deadline = time.time() + timeout
    while time.time() < deadline:
        if not port_is_open(port, host=host):
            return True
        time.sleep(0.2)
    return not port_is_open(port, host=host)


def wait_http_ok(url, timeout=30):
    """Wait until a GET on ``url`` answers with a status below 400."""
    deadline = time.time() + timeout
    last = None
    while time.time() < deadline:
        try:
            status, _body, _headers = http_raw("GET", url, timeout=5)
            if status < 400:
                return status
            last = "http {}".format(status)
        except Exception as err:  # connection refused while starting up
            last = str(err)
        time.sleep(0.3)
    raise ValidationError("{} did not answer within {}s: {}".format(url, timeout, last))


# ---------------------------------------------------------------------------
# minimal RESP2 client
# ---------------------------------------------------------------------------


class Resp(object):
    """Tiny blocking RESP2 client.

    Supports ``+`` (simple string), ``-`` (error), ``:`` (integer), ``$`` (bulk
    string) and ``*`` (array). Used for QPS bursts and ACL probes so that the
    hot loop never forks ``redis-cli``.
    """

    def __init__(self, host, port, timeout=5):
        self.host = host
        self.port = int(port)
        self.timeout = timeout
        self.sock = socket.create_connection((self.host, self.port), timeout=timeout)
        self.sock.settimeout(timeout)
        self._file = self.sock.makefile("rb")

    # -- context manager ----------------------------------------------------
    def __enter__(self):
        return self

    def __exit__(self, exc_type, exc_value, traceback):
        self.close()
        return False

    def close(self):
        try:
            self._file.close()
        except OSError:
            pass
        try:
            self.sock.close()
        except OSError:
            pass

    # -- protocol -----------------------------------------------------------
    def cmd(self, *args):
        """Send one command and return the decoded reply."""
        payload = ["*{}".format(len(args)).encode("utf-8")]
        for arg in args:
            raw = arg if isinstance(arg, bytes) else str(arg).encode("utf-8")
            payload.append("${}".format(len(raw)).encode("utf-8"))
            payload.append(raw)
        self.sock.sendall(b"\r\n".join(payload) + b"\r\n")
        return self._read_reply()

    def _read_reply(self):
        line = self._readline()
        kind = line[0:1]
        body = line[1:]
        if kind == b"+":
            return body.decode("utf-8", "replace")
        if kind == b"-":
            raise RedisError(body.decode("utf-8", "replace"))
        if kind == b":":
            return int(body)
        if kind == b"$":
            length = int(body)
            if length < 0:
                return None
            data = self._read_exact(length + 2)
            return data[:-2].decode("utf-8", "replace")
        if kind == b"*":
            count = int(body)
            if count < 0:
                return None
            return [self._read_reply() for _ in range(count)]
        if kind == b"_":
            return None
        raise RedisError("unsupported RESP reply: {!r}".format(line[:32]))

    def _readline(self):
        line = self._file.readline()
        if not line:
            raise RedisError("connection closed by server")
        return line.rstrip(b"\r\n")

    def _read_exact(self, size):
        chunks = []
        remaining = size
        while remaining > 0:
            chunk = self._file.read(remaining)
            if not chunk:
                raise RedisError("connection closed by server")
            chunks.append(chunk)
            remaining -= len(chunk)
        return b"".join(chunks)


class CliOutput(str):
    """``redis-cli`` stdout that still carries the process result.

    It is a ``str`` so callers can compare/``.strip()`` it directly, while
    ``.returncode``/``.stderr``/``.result`` expose the underlying ``run()``
    metadata for readiness polling with ``check=False``.
    """

    def __new__(cls, text, result=None):
        obj = str.__new__(cls, text)
        obj.result = result or {}
        obj.returncode = obj.result.get("returncode")
        obj.stderr = obj.result.get("stderr", "")
        return obj


def redis_cli(bin_dir, addr, *args, db=None, user=None, password=None, timeout=15, check=True):
    """Invoke ``<repo>/bin/redis-cli`` and return its stdout as ``CliOutput``."""
    bin_dir = Path(bin_dir)
    host, port = split_addr(addr)
    command = [str(bin_dir / "redis-cli"), "-h", host, "-p", str(port), "--raw"]
    if user is not None or password is not None:
        command.append("--no-auth-warning")
        if user is not None:
            command.extend(["--user", str(user)])
        if password is not None:
            command.extend(["--pass", str(password)])
    if db is not None:
        command.extend(["-n", str(db)])
    command.extend(str(a) for a in args)
    result = run(command, cwd=bin_dir.parent, timeout=timeout, check=check)
    return CliOutput(result["stdout"], result)


# ---------------------------------------------------------------------------
# HTTP helpers
# ---------------------------------------------------------------------------


def _http_open(method, url, body=None, headers=None, timeout=15):
    request = urllib.request.Request(url, data=body, method=method.upper())
    for key, value in (headers or {}).items():
        request.add_header(key, value)
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            return response.getcode(), response.read(), dict(response.headers)
    except urllib.error.HTTPError as err:
        payload = b""
        try:
            payload = err.read()
        except Exception:
            payload = b""
        return err.code, payload, dict(err.headers or {})
    except urllib.error.URLError as err:
        raise ValidationError("{} {} failed: {}".format(method, url, err))


def http_json(method, url, body=None, headers=None, timeout=15):
    """Perform an HTTP call and return ``(status, data)``.

    ``body`` given as dict/list is JSON encoded and gets a
    ``Content-Type: application/json`` header. Non-2xx statuses are returned,
    never raised, because this project reports errors as HTTP 800.
    """
    hdrs = dict(headers or {})
    payload = None
    if body is not None:
        if isinstance(body, (dict, list)):
            payload = json.dumps(body).encode("utf-8")
            hdrs.setdefault("Content-Type", "application/json")
        elif isinstance(body, bytes):
            payload = body
        else:
            payload = str(body).encode("utf-8")
    status, raw, _headers = _http_open(method, url, payload, hdrs, timeout)
    text = raw.decode("utf-8", "replace")
    if not text.strip():
        return status, None
    try:
        return status, json.loads(text)
    except ValueError:
        return status, text


def http_raw(method, url, headers=None, timeout=15):
    """Perform an HTTP call and return ``(status, body_bytes, headers_dict)``."""
    return _http_open(method, url, None, dict(headers or {}), timeout)


# ---------------------------------------------------------------------------
# cluster helpers
# ---------------------------------------------------------------------------


def xauth_for(product):
    """Return the dashboard xauth token for a product name."""
    return hashlib.sha256("Codis-XAuth-[{}]".format(product).encode("utf-8")).hexdigest()[:32]


def load_cluster(path):
    """Read a cluster.json file."""
    target = Path(path)
    if not target.is_file():
        raise ValidationError("cluster file not found: {}".format(target))
    try:
        data = json.loads(target.read_text(encoding="utf-8"))
    except ValueError as err:
        raise ValidationError("cluster file {} is not valid JSON: {}".format(target, err))
    if not isinstance(data, dict):
        raise ValidationError("cluster file {} must contain a JSON object".format(target))
    return data


def save_cluster(obj, path):
    """Write a JSON document (cluster.json / cleanup.json / env.json)."""
    target = Path(path)
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text(json.dumps(obj, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    return target


# ---------------------------------------------------------------------------
# managed child processes
# ---------------------------------------------------------------------------


class ManagedProcess(object):
    """A long-running child (codis-server/dashboard/proxy/fe) with a log file.

    stdout/stderr are merged into ``log_path`` (never a pipe, so the child can
    never block on a full pipe buffer).
    """

    def __init__(self, name, args, cwd, log_path):
        self.name = name
        self.args = [str(a) for a in args]
        self.cwd = str(cwd)
        self.log_path = Path(log_path)
        self.proc = None
        self._log_file = None

    def start(self):
        self.log_path.parent.mkdir(parents=True, exist_ok=True)
        self._log_file = self.log_path.open("ab")
        self._log_file.write(
            "# {} start {} (cwd={})\n".format(utc_now(), " ".join(self.args), self.cwd).encode(
                "utf-8"
            )
        )
        self._log_file.flush()
        self.proc = subprocess.Popen(
            self.args,
            cwd=self.cwd,
            stdin=subprocess.DEVNULL,
            stdout=self._log_file,
            stderr=subprocess.STDOUT,
        )
        return self

    @property
    def pid(self):
        return self.proc.pid if self.proc is not None else None

    def is_running(self):
        return self.proc is not None and self.proc.poll() is None

    def stop(self, timeout=5):
        """terminate -> wait ``timeout`` -> kill -> wait ``timeout``."""
        status = "not_running"
        error = None
        pid = self.pid
        if self.proc is not None and self.proc.poll() is None:
            try:
                self.proc.terminate()
            except OSError as err:
                error = str(err)
            try:
                self.proc.wait(timeout=timeout)
                status = "stopped"
            except subprocess.TimeoutExpired:
                try:
                    self.proc.kill()
                    self.proc.wait(timeout=timeout)
                    status = "stopped"
                except (OSError, subprocess.TimeoutExpired) as err:
                    status = "kill_failed"
                    error = str(err)
        if self._log_file is not None:
            try:
                self._log_file.flush()
                self._log_file.close()
            except OSError:
                pass
            self._log_file = None
        result = {"name": self.name, "pid": pid, "status": status, "log": str(self.log_path)}
        if error:
            result["error"] = error
        return result


def read_pidfile(path):
    """Return the pid stored in a pidfile, or None."""
    try:
        text = Path(path).read_text(encoding="utf-8", errors="replace").strip()
    except OSError:
        return None
    if not text:
        return None
    try:
        return int(text.split()[0])
    except (ValueError, IndexError):
        return None


def _ps_command(pid):
    try:
        proc = subprocess.run(
            ["ps", "-o", "command=", "-p", str(pid)],
            stdout=subprocess.PIPE,
            stderr=subprocess.DEVNULL,
            timeout=10,
        )
    except (OSError, subprocess.SubprocessError):
        return None
    if proc.returncode != 0:
        return None
    text = proc.stdout.decode("utf-8", "replace").strip()
    return text or None


def _pid_alive(pid):
    try:
        os.kill(int(pid), 0)
    except ProcessLookupError:
        return False
    except PermissionError:
        return True
    except OSError:
        return False
    return True


def _wait_pid_gone(pid, timeout):
    deadline = time.time() + timeout
    while time.time() < deadline:
        if not _pid_alive(pid):
            return True
        time.sleep(0.1)
    return not _pid_alive(pid)


def stop_pid(pid, name, expect_substring=None, timeout=5):
    """Stop a pid that this script did not start.

    ``expect_substring`` guards against pid reuse: the command line reported by
    ``ps -o command= -p <pid>`` must contain the substring. A list/tuple is
    accepted and matches when any element is contained.
    """
    result = {"name": name, "pid": pid, "status": "not_running"}
    if not pid:
        return result
    try:
        pid = int(pid)
    except (TypeError, ValueError):
        result["status"] = "not_running"
        result["error"] = "invalid pid {!r}".format(pid)
        return result
    command = _ps_command(pid)
    if command is None:
        return result
    if expect_substring:
        wanted = (
            [expect_substring]
            if isinstance(expect_substring, str)
            else [str(item) for item in expect_substring]
        )
        wanted = [item for item in wanted if item]
        if wanted and not any(item in command for item in wanted):
            result["status"] = "skipped_unexpected_command"
            result["command"] = command
            return result
    result["command"] = command
    try:
        os.kill(pid, signal.SIGTERM)
    except ProcessLookupError:
        result["status"] = "not_running"
        return result
    except OSError as err:
        result["status"] = "kill_failed"
        result["error"] = str(err)
        return result
    if _wait_pid_gone(pid, timeout):
        result["status"] = "stopped"
        return result
    try:
        os.kill(pid, signal.SIGKILL)
    except ProcessLookupError:
        result["status"] = "stopped"
        return result
    except OSError as err:
        result["status"] = "kill_failed"
        result["error"] = str(err)
        return result
    if _wait_pid_gone(pid, timeout):
        result["status"] = "stopped"
    else:
        result["status"] = "kill_failed"
        result["error"] = "process survived SIGKILL"
    return result


# ---------------------------------------------------------------------------
# evidence collection
# ---------------------------------------------------------------------------


def _error_text(err):
    if isinstance(err, BaseException):
        return "{}: {}".format(type(err).__name__, err)
    return str(err)


class Case(object):
    """One gate case inside an :class:`Evidence` document."""

    def __init__(self, case_id, title):
        self.id = case_id
        self.title = title
        self.status = "running"
        self.started_at = utc_now()
        self._started = time.monotonic()
        self.steps = []
        self.error = None
        self.reason = None
        self.data = {}

    def step(self, message, **data):
        entry = {"at": utc_now(), "message": str(message)}
        if data:
            entry["data"] = data
        self.steps.append(entry)
        log("  . {}: {}".format(self.id, message))
        return self

    def set(self, **data):
        self.data.update(data)
        return self

    def fail(self, err):
        self.status = "failed"
        self.error = _error_text(err)
        log("  x {} FAILED: {}".format(self.id, self.error))
        return self

    def blocked(self, reason):
        self.status = "blocked"
        self.reason = str(reason)
        log("  ! {} BLOCKED: {}".format(self.id, self.reason))
        return self

    def skip(self, reason):
        self.status = "skipped"
        self.reason = str(reason)
        log("  - {} SKIPPED: {}".format(self.id, self.reason))
        return self

    def to_dict(self):
        return {
            "id": self.id,
            "title": self.title,
            "status": self.status,
            "started_at": self.started_at,
            "duration_sec": round(time.monotonic() - self._started, 3),
            "steps": self.steps,
            "data": self.data,
            "error": self.error,
            "reason": self.reason,
        }


class Evidence(object):
    """A feature evidence document made of independent cases."""

    def __init__(self, script, feature, scope, cluster_path=None):
        self.script = script
        self.feature = feature
        self.scope = scope
        self.cluster_path = str(cluster_path) if cluster_path else None
        self.generated_at = utc_now()
        self.host = host_summary()
        self.head = None
        self.cases = []
        self.error = None
        self.notes = []
        self._forced_status = None

    # -- status -------------------------------------------------------------
    @property
    def status(self):
        if self._forced_status:
            return self._forced_status
        states = [case.status for case in self.cases]
        if "failed" in states:
            return "failed"
        if "blocked" in states:
            return "blocked"
        if "skipped" in states:
            return "skipped"
        return "passed"

    @status.setter
    def status(self, value):
        self._forced_status = value

    def exit_code(self):
        status = self.status
        if status == "passed":
            return 0
        if status == "failed":
            return 1
        return 2

    # -- cases --------------------------------------------------------------
    @contextlib.contextmanager
    def case(self, case_id, title):
        entry = Case(case_id, title)
        self.cases.append(entry)
        log("== case {}: {}".format(case_id, title))
        try:
            yield entry
        except Exception as err:  # a failing case must not abort the gate
            if entry.status == "running":
                entry.fail(err)
            else:
                entry.step("exception after terminal status", error=_error_text(err))
        finally:
            if entry.status == "running":
                entry.status = "passed"
                log("  + {} passed".format(case_id))

    def note(self, message):
        self.notes.append(str(message))
        log("note: {}".format(message))
        return self

    def attach_repo(self, repo):
        self.head = git_head(repo)
        return self

    # -- serialisation ------------------------------------------------------
    def to_dict(self):
        return {
            "script": self.script,
            "feature": self.feature,
            "scope": self.scope,
            "cluster": self.cluster_path,
            "generated_at": self.generated_at,
            "host": self.host,
            "head": self.head,
            "status": self.status,
            "error": self.error,
            "notes": self.notes,
            "cases": [case.to_dict() for case in self.cases],
        }

    def write(self, path):
        return save_cluster(self.to_dict(), path)


def main_guard(evidence, fn, output_path=None):
    """Run ``fn`` and guarantee that evidence lands on disk.

    ``KeyboardInterrupt`` and any uncaught exception are recorded as
    ``status=failed`` with the error text; the returned exit code is then 1.
    """
    code = None
    try:
        code = fn()
    except KeyboardInterrupt:
        evidence.status = "failed"
        evidence.error = "interrupted (KeyboardInterrupt)"
        log(evidence.error)
        code = 1
    except BaseException as err:  # noqa: BLE001 - evidence must always be written
        evidence.status = "failed"
        evidence.error = "uncaught {}: {}".format(type(err).__name__, err)
        log(evidence.error)
        code = 1
    if code is None:
        code = evidence.exit_code()
    if output_path:
        try:
            evidence.write(output_path)
            log("evidence written to {}".format(output_path))
        except Exception as err:  # noqa: BLE001
            log("failed to write evidence to {}: {}".format(output_path, err))
            if code == 0:
                code = 1
    return code


# ---------------------------------------------------------------------------
# config generators
# ---------------------------------------------------------------------------


def _toml_string(value):
    text = str(value)
    text = text.replace("\\", "\\\\")
    text = text.replace('"', '\\"')
    text = text.replace("\r", "\\r")
    text = text.replace("\n", "\\n")
    text = text.replace("\t", "\\t")
    return '"{}"'.format(text)


def _toml_bool(value):
    return "true" if value else "false"


def _toml_scalar(value):
    if isinstance(value, bool):
        return _toml_bool(value)
    if isinstance(value, (int, float)):
        return str(value)
    return _toml_string(value)


def _extra_lines(extra):
    if not extra:
        return []
    if isinstance(extra, dict):
        return ["{} = {}".format(key, _toml_scalar(value)) for key, value in extra.items()]
    return [str(item) for item in extra]


def _write_conf(path, lines):
    target = Path(path)
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text("\n".join(lines) + "\n", encoding="utf-8")
    return target


def render_redis_conf(
    path,
    port,
    data_dir,
    *,
    bind="127.0.0.1",
    pidfile=None,
    aof=False,
    rdb_export_auth=None,
    replicaof=None,
    extra=None,
):
    """Render a codis-server (Redis 8) config for a validation instance."""
    port = int(port)
    lines = [
        "port {}".format(port),
        "bind {}".format(bind),
        "protected-mode no",
        "codis-enabled yes",
        "daemonize no",
        'save ""',
        "dir {}".format(data_dir),
        "dbfilename dump-{}.rdb".format(port),
    ]
    if pidfile:
        lines.append("pidfile {}".format(pidfile))
    if aof:
        lines.extend(["appendonly yes", "appendfsync everysec", "aof-timestamp-enabled yes"])
    else:
        lines.append("appendonly no")
    if rdb_export_auth:
        lines.extend(
            [
                "codis-rdb-export-enabled yes",
                "codis-rdb-export-auth {}".format(_toml_string(rdb_export_auth)),
            ]
        )
    else:
        lines.append("codis-rdb-export-enabled no")
    if replicaof:
        if isinstance(replicaof, (tuple, list)):
            master_host, master_port = replicaof[0], replicaof[1]
        else:
            parts = str(replicaof).replace(":", " ").split()
            if len(parts) == 1:
                master_host, master_port = split_addr(parts[0])
            else:
                master_host, master_port = parts[0], parts[1]
        lines.append("replicaof {} {}".format(master_host, int(master_port)))
    lines.extend(_extra_lines(extra))
    return _write_conf(path, lines)


def render_dashboard_conf(
    path,
    *,
    admin_port,
    coordinator_dir,
    product,
    workdir,
    pitr_enabled=True,
    pitr_restart_command=None,
    rdb_workspace=None,
    rdb_remote_fetch_auth=None,
    extra=None,
):
    """Render a codis-dashboard TOML config."""
    workdir = Path(workdir)
    workspace = Path(rdb_workspace) if rdb_workspace else workdir / "rdb-analysis"
    remote_fetch = bool(rdb_remote_fetch_auth)
    lines = [
        'coordinator_name = "filesystem"',
        "coordinator_addr = {}".format(_toml_string(coordinator_dir)),
        "product_name = {}".format(_toml_string(product)),
        'product_auth = ""',
        "admin_addr = {}".format(_toml_string("127.0.0.1:{}".format(int(admin_port)))),
        'migration_method = "semi-async"',
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
        "rdb_analysis_workspace = {}".format(_toml_string(workspace)),
        "rdb_analysis_max_upload_size = \"1gb\"",
        "rdb_analysis_max_concurrent_jobs = 1",
        "rdb_analysis_max_jobs_retained = 16",
        "rdb_analysis_max_top_n = 100",
        "rdb_analysis_remote_fetch_enabled = {}".format(_toml_bool(remote_fetch)),
        "rdb_analysis_remote_fetch_auth = {}".format(
            _toml_string(rdb_remote_fetch_auth or "")
        ),
        'rdb_analysis_remote_fetch_timeout = "30m"',
        "rdb_analysis_remote_fetch_max_concurrent = 1",
        "pitr_enabled = {}".format(_toml_bool(pitr_enabled)),
        'pitr_redis_check_aof_bin = "bin/redis-check-aof"',
        "pitr_max_concurrent_jobs = 1",
        'pitr_job_timeout = "30m"',
        "pitr_restart_command = {}".format(_toml_string(pitr_restart_command or "")),
    ]
    lines.extend(_extra_lines(extra))
    return _write_conf(path, lines)


def render_proxy_conf(path, *, admin_port, proxy_port, product, acl_enabled=True, extra=None):
    """Render a codis-proxy TOML config."""
    lines = [
        "product_name = {}".format(_toml_string(product)),
        'product_auth = ""',
        'session_auth = ""',
        "admin_addr = {}".format(_toml_string("127.0.0.1:{}".format(int(admin_port)))),
        'proto_type = "tcp4"',
        "proxy_addr = {}".format(_toml_string("127.0.0.1:{}".format(int(proxy_port)))),
        'proxy_datacenter = "validation"',
        "codis_acl_enabled = {}".format(_toml_bool(acl_enabled)),
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
    lines.extend(_extra_lines(extra))
    return _write_conf(path, lines)
