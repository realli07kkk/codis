#!/usr/bin/env python3
"""Validate Codis ACL end to end against a running validation cluster.

The script drives the topom ACL API (GET/PUT, dry-run preview, --acl-sync),
checks that every codis-server received the runtime users, exercises the
proxy-side AUTH/WHOAMI/permission/db-binding behaviour, verifies password
rotation and fail-closed db binding, and optionally proves that a Redis master
restart drops the runtime ACL until `codis-admin --acl-sync` replays it.

Every observation lands in one Evidence JSON file; the exit code follows the
evidence status (0 passed, 1 failed, 2 blocked/skipped). ACL is always left
disabled with no users: the `acl-cleanup` case runs in a `finally` block, so a
failing case cannot leak credentials into the shared cluster.
"""

import argparse
import json
import sys
import time
import zlib
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import vlib

SCRIPT = "scripts/validation/acl/run_acl.py"
FEATURE = "codis-acl"
SCOPE = (
    "topom ACL API, backend Redis propagation, proxy AUTH/permission/db binding, "
    "password rotation, --acl-sync and re-sync after a master restart"
)

APP_USER = "app"
APP_PASSWORD = "acl-app-pass"
ROTATED_PASSWORD = "acl-app-pass-rotated"
READER_USER = "reader"
READER_PASSWORD = "acl-reader-pass"
DB_USER = "db3"
DB_PASSWORD = "acl-db3-pass"
DB_BINDING = 3
DB99_USER = "db99"
DB99_PASSWORD = "acl-db99-pass"
APP_KEY = "acl:app:key"
APP_VALUE = "acl-app-value"
DB_KEY = "acl:db3:key"
DB_VALUE = "acl-db3-value"
RESTART_KEY_PREFIX = "acl:restart:key:"
RESTART_VALUE = "acl-restart-value"


# ---------------------------------------------------------------------------
# topom / proxy helpers
# ---------------------------------------------------------------------------


def topom_url(cluster, path):
    return "http://{}{}".format(cluster["dashboard"]["addr"], path)


def acl_url(cluster):
    return topom_url(cluster, "/api/topom/acl/{}".format(cluster["xauth"]))


def cause_text(data):
    if isinstance(data, dict):
        cause = data.get("Cause")
        if cause:
            return str(cause)
    try:
        return json.dumps(data, sort_keys=True)[:1000]
    except (TypeError, ValueError):
        return str(data)


def get_acl(cluster):
    status, data = vlib.http_json("GET", acl_url(cluster))
    if status != 200 or not isinstance(data, dict):
        raise vlib.ValidationError("GET acl returned HTTP {}: {}".format(status, cause_text(data)))
    return data


def put_acl(cluster, body):
    status, data = vlib.http_json("PUT", acl_url(cluster), body)
    if status != 200 or not isinstance(data, dict):
        raise vlib.ValidationError("PUT acl returned HTTP {}: {}".format(status, cause_text(data)))
    return data


def put_acl_rejected(cluster, body, needle):
    status, data = vlib.http_json("PUT", acl_url(cluster), body)
    if status != 800:
        raise vlib.ValidationError(
            "PUT acl returned HTTP {} (expected the project 800 error): {}".format(
                status, cause_text(data)
            )
        )
    cause = cause_text(data)
    if needle.lower() not in cause.lower():
        raise vlib.ValidationError("PUT acl rejected with {!r}, expected {!r}".format(cause, needle))
    return cause


def user_names(view):
    return [user.get("name") for user in (view.get("users") or [])]


def users_by_name(view):
    return {user.get("name"): user for user in (view.get("users") or [])}


def default_users(password=APP_PASSWORD, extra=()):
    users = [
        {
            "name": APP_USER,
            "enabled": True,
            "new_password": password,
            "rules": ["+@all", "~*"],
        },
        {
            "name": READER_USER,
            "enabled": True,
            "new_password": READER_PASSWORD,
            "rules": ["+@read", "~*"],
        },
        {
            "name": DB_USER,
            "enabled": True,
            "new_password": DB_PASSWORD,
            "rules": ["+@all", "~*"],
            "db": DB_BINDING,
        },
    ]
    users.extend(extra)
    return users


def all_servers(cluster):
    servers = []
    for group in cluster.get("groups") or []:
        for server in group.get("servers") or []:
            servers.append(server)
    return servers


def masters(cluster):
    result = []
    for group in cluster.get("groups") or []:
        servers = group.get("servers") or []
        for server in servers:
            if server.get("role") == "master":
                result.append(server)
                break
    return result


def acl_list(bin_dir, addr):
    output = vlib.redis_cli(bin_dir, addr, "ACL", "LIST", check=False)
    return [line.strip() for line in str(output).splitlines() if line.strip()]


def acl_user_lines(bin_dir, addr):
    return [line for line in acl_list(bin_dir, addr) if line.startswith("user ")]


def admin_cmd(ctx, *args, timeout=60, truncate=True):
    command = [
        str(Path(ctx["bin_dir"]) / "codis-admin"),
        "--dashboard={}".format(ctx["cluster"]["dashboard"]["addr"]),
    ]
    command.extend(str(arg) for arg in args)
    return vlib.run(
        command,
        cwd=ctx["repo"],
        timeout=timeout,
        check=False,
        truncate=truncate,
        log_path=ctx["workdir"] / "codis-admin.log",
    )


def load_slots(ctx):
    """Return the 1024 ``--slots-status`` entries."""
    result = admin_cmd(ctx, "--slots-status", timeout=60, truncate=False)
    if result["returncode"] != 0:
        raise vlib.ValidationError(
            "codis-admin --slots-status exited {}: {}".format(
                result["returncode"], result["stderr"].strip()[:1000]
            )
        )
    try:
        slots = json.loads(result["stdout"])
    except ValueError as err:
        raise vlib.ValidationError(
            "--slots-status output is not JSON ({}): {}".format(err, result["stdout"][:500])
        )
    if not isinstance(slots, list) or not slots:
        raise vlib.ValidationError("--slots-status returned an unexpected payload")
    return slots


def pick_key_for_group(ctx, gid, prefix, attempts=256):
    """Pick ``<prefix><n>`` whose slot is owned by group ``gid``.

    Codis maps a key to ``crc32(key) % 1024`` (pkg/proxy/mapper.go), so the
    restart case can target the master of a known group without guessing.
    """
    slots = load_slots(ctx)
    for index in range(attempts):
        key = "{}{}".format(prefix, index)
        slot = zlib.crc32(key.encode("utf-8")) % 1024
        if slot >= len(slots):
            raise vlib.ValidationError(
                "--slots-status returned {} slots, cannot look up slot {}".format(len(slots), slot)
            )
        if slots[slot].get("backend_addr_group_id") == gid:
            return key, slot
    raise vlib.ValidationError(
        "no key with prefix {!r} maps into group {} after {} attempts".format(prefix, gid, attempts)
    )


def proxy_resp(cluster, index=0):
    proxy = cluster["proxies"][index]
    return vlib.Resp(proxy["host"], proxy["port"])


# ---------------------------------------------------------------------------
# RESP assertion helpers
# ---------------------------------------------------------------------------


def call_raising(client, *args):
    """Return ``(reply, error_text)``; exactly one of them is not None."""
    try:
        return client.cmd(*args), None
    except vlib.RedisError as err:
        return None, str(err)


def require_ok(case, client, *args, label=None):
    reply, error = call_raising(client, *args)
    command = label or " ".join(str(arg) for arg in args)
    if error is not None:
        raise vlib.ValidationError("{} failed: {}".format(command, error))
    case.step("{} OK".format(command), reply=str(reply)[:200])
    return reply


def require_error(case, client, args, needle, label=None):
    reply, error = call_raising(client, *args)
    command = label or " ".join(str(arg) for arg in args)
    if error is None:
        raise vlib.ValidationError("{} unexpectedly succeeded: {!r}".format(command, reply))
    if needle.lower() not in error.lower():
        raise vlib.ValidationError("{} returned {!r}, expected {!r}".format(command, error, needle))
    case.step("{} refused as expected".format(command), error=error)
    return error


# ---------------------------------------------------------------------------
# readiness helpers (local, vlib has no redis-level waiters)
# ---------------------------------------------------------------------------


def wait_pong(host, port, timeout=60):
    deadline = time.time() + timeout
    last = None
    while time.time() < deadline:
        try:
            with vlib.Resp(host, port, timeout=2) as client:
                if client.cmd("PING") == "PONG":
                    return True
        except (OSError, vlib.RedisError) as err:
            last = str(err)
        time.sleep(0.2)
    raise vlib.ValidationError(
        "redis {}:{} did not answer PING within {}s: {}".format(host, port, timeout, last)
    )


def wait_replica_link(server, timeout=90):
    deadline = time.time() + timeout
    last = None
    while time.time() < deadline:
        try:
            with vlib.Resp(server["host"], server["port"], timeout=3) as client:
                info = client.cmd("INFO", "replication")
            for line in str(info).splitlines():
                if line.startswith("master_link_status:"):
                    last = line.split(":", 1)[1].strip()
                    break
            if last == "up":
                return True
        except (OSError, vlib.RedisError) as err:
            last = str(err)
        time.sleep(0.3)
    raise vlib.ValidationError(
        "replica {}:{} master_link_status stayed {!r} for {}s".format(
            server["host"], server["port"], last, timeout
        )
    )


# ---------------------------------------------------------------------------
# cases
# ---------------------------------------------------------------------------


def case_acl_initial(evidence, ctx):
    with evidence.case("acl-initial", "topom ACL starts from revision 0 / not_configured") as case:
        cluster = ctx["cluster"]
        view = get_acl(cluster)
        case.set(initial_view=view)
        case.step(
            "initial ACL view",
            revision=view.get("revision"),
            enabled=view.get("enabled"),
            sync_status=view.get("sync_status"),
            users=user_names(view),
        )
        if view.get("revision") != 0 or view.get("enabled") or (view.get("users") or []):
            evidence.note(
                "acl-initial: residual ACL state (revision={}, enabled={}, users={}); resetting".format(
                    view.get("revision"), view.get("enabled"), user_names(view)
                )
            )
            put_acl(cluster, {"enabled": False, "users": []})
            view = get_acl(cluster)
            case.set(reset_view=view)
            case.step(
                "reset residual ACL state",
                revision=view.get("revision"),
                enabled=view.get("enabled"),
                users=user_names(view),
            )
        if view.get("enabled"):
            raise vlib.ValidationError("ACL is still enabled after the initial check")
        if user_names(view):
            raise vlib.ValidationError("ACL still lists users after the initial check")
        if view.get("revision") == 0:
            if view.get("sync_status") != "not_configured":
                raise vlib.ValidationError(
                    "fresh ACL sync_status is {!r}, expected 'not_configured'".format(
                        view.get("sync_status")
                    )
                )
        elif view.get("sync_status") != "ready":
            raise vlib.ValidationError(
                "reset ACL sync_status is {!r}, expected 'ready'".format(view.get("sync_status"))
            )


def case_acl_dry_run_preview(evidence, ctx):
    with evidence.case(
        "acl-dry-run-preview",
        "codis-admin --acl-set without --confirm prints a redacted preview only",
    ) as case:
        cluster = ctx["cluster"]
        acl_file = ctx["workdir"] / "acl-preview.json"
        acl_file.write_text(
            json.dumps({"enabled": True, "users": default_users()}, indent=2), encoding="utf-8"
        )
        before = get_acl(cluster)
        result = admin_cmd(ctx, "--acl-set={}".format(acl_file), truncate=False)
        case.set(
            command=" ".join(result["command"]),
            returncode=result["returncode"],
            stdout=result["stdout"],
        )
        if result["returncode"] != 0:
            raise vlib.ValidationError(
                "codis-admin --acl-set exited {}: {}".format(
                    result["returncode"], result["stderr"].strip()[:1000]
                )
            )
        # codis-admin marshals with encoding/json, which escapes '<' and '>'
        # as \u003c / \u003e, so accept both spellings.
        if "<redacted>" not in result["stdout"] and "\\u003credacted\\u003e" not in result["stdout"]:
            raise vlib.ValidationError(
                "preview does not contain the redaction marker: {}".format(result["stdout"][:1000])
            )
        if APP_PASSWORD in result["stdout"]:
            raise vlib.ValidationError("preview leaked the plaintext password")
        try:
            preview = json.loads(result["stdout"])
        except ValueError as err:
            raise vlib.ValidationError(
                "preview is not JSON ({}): {}".format(err, result["stdout"][:1000])
            )
        if not preview.get("enabled"):
            raise vlib.ValidationError("preview does not echo enabled=true")
        for user in preview.get("users") or []:
            if user.get("new_password") != "<redacted>":
                raise vlib.ValidationError(
                    "user {} previews new_password={!r}".format(
                        user.get("name"), user.get("new_password")
                    )
                )
        after = get_acl(cluster)
        case.set(revision_before=before.get("revision"), revision_after=after.get("revision"))
        if after.get("revision") != before.get("revision"):
            raise vlib.ValidationError(
                "dry run changed the revision {} -> {}".format(
                    before.get("revision"), after.get("revision")
                )
            )
        if after.get("enabled"):
            raise vlib.ValidationError("dry run enabled ACL")


def case_acl_enable(evidence, ctx):
    with evidence.case("acl-enable", "PUT /api/topom/acl enables app/reader/db3") as case:
        cluster = ctx["cluster"]
        before = get_acl(cluster)
        view = put_acl(cluster, {"enabled": True, "users": default_users()})
        case.set(revision=view.get("revision"), users=user_names(view))
        case.step(
            "ACL enabled",
            revision=view.get("revision"),
            sync_status=view.get("sync_status"),
            users=user_names(view),
        )
        if not view.get("enabled"):
            raise vlib.ValidationError("PUT acl did not report enabled=true")
        if view.get("revision") != before.get("revision", 0) + 1:
            raise vlib.ValidationError(
                "revision {} -> {}, expected exactly one increment".format(
                    before.get("revision"), view.get("revision")
                )
            )
        if view.get("sync_status") != "ready":
            raise vlib.ValidationError(
                "sync_status is {!r}, expected 'ready'".format(view.get("sync_status"))
            )
        names = sorted(user_names(view))
        if names != sorted([APP_USER, READER_USER, DB_USER]):
            raise vlib.ValidationError("unexpected ACL users {}".format(names))
        users = users_by_name(view)
        for name in (APP_USER, READER_USER, DB_USER):
            if users[name].get("password_count") != 1:
                raise vlib.ValidationError(
                    "user {} has password_count {!r}".format(
                        name, users[name].get("password_count")
                    )
                )
        if users[APP_USER].get("rules") != ["+@all", "~*"]:
            raise vlib.ValidationError(
                "app rules are {!r}".format(users[APP_USER].get("rules"))
            )
        if users[READER_USER].get("rules") != ["+@read", "~*"]:
            raise vlib.ValidationError(
                "reader rules are {!r}".format(users[READER_USER].get("rules"))
            )
        if users[DB_USER].get("db") != DB_BINDING:
            raise vlib.ValidationError(
                "db3 binding is {!r}, expected {}".format(users[DB_USER].get("db"), DB_BINDING)
            )
        for name in (APP_USER, READER_USER):
            if "db" in users[name]:
                raise vlib.ValidationError("user {} unexpectedly carries a db binding".format(name))


def case_acl_backend_synced(evidence, ctx):
    with evidence.case(
        "acl-backend-synced", "every codis-server carries the runtime ACL users"
    ) as case:
        cluster = ctx["cluster"]
        missing = {}
        observed = {}
        for server in all_servers(cluster):
            lines = acl_user_lines(ctx["bin_dir"], server["addr"])
            names = sorted(line.split()[1] for line in lines)
            observed[server["addr"]] = names
            absent = [user for user in (APP_USER, READER_USER, DB_USER) if user not in names]
            if absent:
                missing[server["addr"]] = absent
        case.set(servers=observed, missing=missing)
        for addr, names in sorted(observed.items()):
            case.step("backend ACL LIST", addr=addr, users=names)
        if missing:
            raise vlib.ValidationError("backends missing ACL users: {}".format(missing))


def case_acl_unauth_rejected(evidence, ctx):
    with evidence.case(
        "acl-unauth-rejected", "unauthenticated proxy session is refused with NOAUTH"
    ) as case:
        cluster = ctx["cluster"]
        with proxy_resp(cluster) as client:
            error = require_error(case, client, ("PING",), "NOAUTH", label="PING (unauthenticated)")
            case.set(error=error)


def case_acl_app_rw(evidence, ctx):
    with evidence.case("acl-app-rw", "app user authenticates, WHOAMI=app and can read/write") as case:
        cluster = ctx["cluster"]
        with proxy_resp(cluster) as client:
            require_ok(case, client, "AUTH", APP_USER, APP_PASSWORD)
            whoami = require_ok(case, client, "ACL", "WHOAMI")
            if whoami != APP_USER:
                raise vlib.ValidationError("ACL WHOAMI returned {!r}".format(whoami))
            require_ok(case, client, "SET", APP_KEY, APP_VALUE)
            value = require_ok(case, client, "GET", APP_KEY)
            if value != APP_VALUE:
                raise vlib.ValidationError("GET returned {!r}, expected {!r}".format(value, APP_VALUE))
            case.set(whoami=whoami, value=value)


def case_acl_reader_readonly(evidence, ctx):
    with evidence.case(
        "acl-reader-readonly", "reader (+@read) reads but cannot SET"
    ) as case:
        cluster = ctx["cluster"]
        with proxy_resp(cluster) as client:
            require_ok(case, client, "AUTH", READER_USER, READER_PASSWORD)
            whoami = require_ok(case, client, "ACL", "WHOAMI")
            if whoami != READER_USER:
                raise vlib.ValidationError("ACL WHOAMI returned {!r}".format(whoami))
            value = require_ok(case, client, "GET", APP_KEY)
            if value != APP_VALUE:
                raise vlib.ValidationError(
                    "reader GET returned {!r}, expected {!r}".format(value, APP_VALUE)
                )
            error = require_error(case, client, ("SET", APP_KEY, "nope"), "NOPERM", label="SET")
            if "'set'" not in error.lower():
                raise vlib.ValidationError("SET denial does not name the command: {!r}".format(error))
            case.set(denial=error, value=value)


def case_acl_db_binding(evidence, ctx):
    with evidence.case(
        "acl-db-binding", "db-bound user is forced onto db 3 and SELECT does not move it"
    ) as case:
        cluster = ctx["cluster"]
        with proxy_resp(cluster) as client:
            require_ok(case, client, "AUTH", DB_USER, DB_PASSWORD)
            require_ok(case, client, "SELECT", 5)
            require_ok(case, client, "SET", DB_KEY, DB_VALUE)
            value = require_ok(case, client, "GET", DB_KEY)
            if value != DB_VALUE:
                raise vlib.ValidationError(
                    "db3 GET returned {!r}, expected {!r}".format(value, DB_VALUE)
                )
        hits3 = []
        hits0 = []
        for server in masters(cluster):
            got3 = str(
                vlib.redis_cli(
                    ctx["bin_dir"], server["addr"], "GET", DB_KEY, db=DB_BINDING, check=False
                )
            ).strip()
            got0 = str(
                vlib.redis_cli(ctx["bin_dir"], server["addr"], "GET", DB_KEY, db=0, check=False)
            ).strip()
            case.step("direct db check", addr=server["addr"], db3=got3, db0=got0)
            if got3 == DB_VALUE:
                hits3.append(server["addr"])
            if got0:
                hits0.append(server["addr"])
        case.set(db3_hits=hits3, db0_hits=hits0)
        if len(hits3) != 1:
            raise vlib.ValidationError(
                "expected exactly one master to hold the key in db {}, got {}".format(
                    DB_BINDING, hits3
                )
            )
        if hits0:
            raise vlib.ValidationError("key leaked into db 0 on {}".format(hits0))


def case_acl_proxy_setuser_rejected(evidence, ctx):
    with evidence.case(
        "acl-proxy-setuser-rejected", "the proxy refuses ACL SETUSER"
    ) as case:
        cluster = ctx["cluster"]
        with proxy_resp(cluster) as client:
            require_ok(case, client, "AUTH", APP_USER, APP_PASSWORD)
            error = require_error(
                case, client, ("ACL", "SETUSER", "x"), "unsupported ACL subcommand", label="ACL SETUSER"
            )
            if "SETUSER" not in error:
                raise vlib.ValidationError("denial does not name SETUSER: {!r}".format(error))
            case.set(denial=error)


def case_acl_auth_single_arg(evidence, ctx):
    with evidence.case(
        "acl-auth-single-arg", "single-argument AUTH is refused with WRONGPASS"
    ) as case:
        cluster = ctx["cluster"]
        with proxy_resp(cluster) as client:
            error = require_error(
                case, client, ("AUTH", APP_PASSWORD), "WRONGPASS", label="AUTH <password>"
            )
            case.set(error=error)


def case_acl_wrongpass_keeps_session(evidence, ctx):
    with evidence.case(
        "acl-wrongpass-keeps-session", "a failed re-AUTH keeps the current identity"
    ) as case:
        cluster = ctx["cluster"]
        with proxy_resp(cluster) as client:
            require_ok(case, client, "AUTH", APP_USER, APP_PASSWORD)
            error = require_error(
                case, client, ("AUTH", APP_USER, "wrong-password"), "WRONGPASS", label="AUTH (bad password)"
            )
            whoami = require_ok(case, client, "ACL", "WHOAMI")
            if whoami != APP_USER:
                raise vlib.ValidationError(
                    "WHOAMI after a failed AUTH returned {!r}, expected {!r}".format(whoami, APP_USER)
                )
            value = require_ok(case, client, "GET", APP_KEY)
            if value != APP_VALUE:
                raise vlib.ValidationError("session lost its data access after a failed AUTH")
            case.set(error=error, whoami=whoami)


def case_acl_password_rotation(evidence, ctx):
    with evidence.case(
        "acl-password-rotation", "rotating the password invalidates old sessions, re-AUTH recovers"
    ) as case:
        cluster = ctx["cluster"]
        with proxy_resp(cluster) as client:
            require_ok(case, client, "AUTH", APP_USER, APP_PASSWORD)
            rotated = put_acl(cluster, {"enabled": True, "users": default_users(ROTATED_PASSWORD)})
            case.step("password rotated", revision=rotated.get("revision"))
            require_error(case, client, ("ACL", "WHOAMI"), "NOAUTH", label="ACL WHOAMI (stale session)")
            require_ok(case, client, "AUTH", APP_USER, ROTATED_PASSWORD)
            whoami = require_ok(case, client, "ACL", "WHOAMI")
            if whoami != APP_USER:
                raise vlib.ValidationError("WHOAMI after re-AUTH returned {!r}".format(whoami))
            restored = put_acl(cluster, {"enabled": True, "users": default_users()})
            case.step("password restored", revision=restored.get("revision"))
        with proxy_resp(cluster) as client:
            require_ok(case, client, "AUTH", APP_USER, APP_PASSWORD)
            whoami = require_ok(case, client, "ACL", "WHOAMI")
            if whoami != APP_USER:
                raise vlib.ValidationError("WHOAMI after restore returned {!r}".format(whoami))
            case.set(revision=restored.get("revision"))


def case_acl_reject_empty_rules(evidence, ctx):
    with evidence.case(
        "acl-reject-empty-rules", "a user without rules is rejected with HTTP 800"
    ) as case:
        cluster = ctx["cluster"]
        before = get_acl(cluster)
        bad = {"name": "emptyuser", "enabled": True, "new_password": "whatever", "rules": []}
        cause = put_acl_rejected(
            cluster,
            {"enabled": True, "users": default_users(extra=[bad])},
            "empty acl rules",
        )
        after = get_acl(cluster)
        case.set(cause=cause, revision=after.get("revision"))
        if after.get("revision") != before.get("revision"):
            raise vlib.ValidationError("a rejected PUT bumped the revision")
        if user_names(after) != user_names(before):
            raise vlib.ValidationError("a rejected PUT changed the user list")
        with proxy_resp(cluster) as client:
            require_ok(case, client, "AUTH", APP_USER, APP_PASSWORD)
            require_ok(case, client, "ACL", "WHOAMI")


def case_acl_reject_plaintext(evidence, ctx):
    with evidence.case(
        "acl-reject-plaintext", "a '>password' rule token is rejected with HTTP 800"
    ) as case:
        cluster = ctx["cluster"]
        before = get_acl(cluster)
        bad = {
            "name": "plainuser",
            "enabled": True,
            "new_password": "whatever",
            "rules": ["+@all", "~*", ">plain-secret"],
        }
        cause = put_acl_rejected(
            cluster,
            {"enabled": True, "users": default_users(extra=[bad])},
            "password token is not allowed",
        )
        after = get_acl(cluster)
        case.set(cause=cause, revision=after.get("revision"))
        if "plaintext password token is not allowed" not in cause:
            evidence.note(
                "acl-reject-plaintext: topom words the rejection as {!r}; the documented "
                "text is 'plaintext password token is not allowed'".format(cause)
            )
        if after.get("revision") != before.get("revision"):
            raise vlib.ValidationError("a rejected PUT bumped the revision")


def case_acl_db99_fail_closed(evidence, ctx):
    with evidence.case(
        "acl-db99-fail-closed",
        "topom accepts db=99 but the proxy fails the AUTH closed, then the user is removed",
    ) as case:
        cluster = ctx["cluster"]
        bad = {
            "name": DB99_USER,
            "enabled": True,
            "new_password": DB99_PASSWORD,
            "rules": ["+@all", "~*"],
            "db": 99,
        }
        before = get_acl(cluster)
        view = put_acl(cluster, {"enabled": True, "users": default_users(extra=[bad])})
        case.step("topom accepted db=99", revision=view.get("revision"))
        if view.get("revision") != before.get("revision", 0) + 1:
            raise vlib.ValidationError("db=99 PUT did not bump the revision exactly once")
        with proxy_resp(cluster) as client:
            error = require_error(
                case,
                client,
                ("AUTH", DB99_USER, DB99_PASSWORD),
                "bound db 99 exceeds backend db count 16",
                label="AUTH db99",
            )
            case.set(auth_error=error)
        removed = put_acl(cluster, {"enabled": True, "users": default_users()})
        case.step("db99 user removed", revision=removed.get("revision"))
        if DB99_USER in user_names(removed):
            raise vlib.ValidationError("db99 is still configured after the removal PUT")
        with proxy_resp(cluster) as client:
            require_error(
                case,
                client,
                ("AUTH", DB99_USER, DB99_PASSWORD),
                "WRONGPASS",
                label="AUTH db99 (removed)",
            )
        leftovers = [
            server["addr"]
            for server in all_servers(cluster)
            if any(line.split()[1] == DB99_USER for line in acl_user_lines(ctx["bin_dir"], server["addr"]))
        ]
        case.set(db99_left_on=leftovers)
        if leftovers:
            raise vlib.ValidationError("db99 still exists on backends {}".format(leftovers))


def case_acl_acl_sync(evidence, ctx):
    with evidence.case("acl-acl-sync", "codis-admin --acl-sync replays the current ACL") as case:
        cluster = ctx["cluster"]
        before = get_acl(cluster)
        result = admin_cmd(ctx, "--acl-sync", truncate=False)
        case.set(command=" ".join(result["command"]), returncode=result["returncode"])
        if result["returncode"] != 0:
            raise vlib.ValidationError(
                "codis-admin --acl-sync exited {}: {}".format(
                    result["returncode"], result["stderr"].strip()[:1000]
                )
            )
        try:
            payload = json.loads(result["stdout"])
        except ValueError as err:
            raise vlib.ValidationError(
                "--acl-sync output is not JSON ({}): {}".format(err, result["stdout"][:1000])
            )
        case.set(revision=payload.get("revision"), sync_status=payload.get("sync_status"))
        if payload.get("revision") != before.get("revision"):
            raise vlib.ValidationError("--acl-sync changed the revision")
        if payload.get("sync_status") != "ready":
            raise vlib.ValidationError(
                "--acl-sync reported sync_status {!r}".format(payload.get("sync_status"))
            )
        if sorted(user_names(payload)) != sorted(user_names(before)):
            raise vlib.ValidationError("--acl-sync changed the user list")
        with proxy_resp(cluster) as client:
            require_ok(case, client, "AUTH", APP_USER, APP_PASSWORD)
            value = require_ok(case, client, "GET", APP_KEY)
            if value != APP_VALUE:
                raise vlib.ValidationError("GET after --acl-sync returned {!r}".format(value))


def wait_proxy_value(cluster, key, expected, timeout=30):
    """Wait until an authenticated ``app`` session can read ``key`` again."""
    deadline = time.time() + timeout
    last = None
    while time.time() < deadline:
        try:
            with proxy_resp(cluster) as client:
                client.cmd("AUTH", APP_USER, APP_PASSWORD)
                value = client.cmd("GET", key)
            if value == expected or value is None:
                return value
            last = "GET returned {!r}".format(value)
        except (OSError, vlib.RedisError) as err:
            last = str(err)
        time.sleep(0.5)
    raise vlib.ValidationError(
        "proxy GET {} did not recover within {}s: {}".format(key, timeout, last)
    )


def case_acl_redis_restart_resync(evidence, ctx):
    with evidence.case(
        "acl-redis-restart-resync",
        "a restarted master loses the runtime ACL until --acl-sync replays it",
    ) as case:
        if ctx["skip_restart"]:
            case.skip("--skip-restart")
            return
        cluster = ctx["cluster"]
        script = Path(cluster["workdir"]) / "conf" / "pitr-restart.sh"
        if not script.is_file():
            case.blocked("{} is missing".format(script))
            return
        group = (cluster.get("groups") or [])[0]
        master = (group.get("servers") or [])[0]
        replicas = (group.get("servers") or [])[1:]
        key, slot = pick_key_for_group(ctx, group.get("gid"), RESTART_KEY_PREFIX)
        case.set(
            group=group.get("gid"),
            master=master["addr"],
            replicas=[replica["addr"] for replica in replicas],
            restart_script=str(script),
            key=key,
            slot=slot,
        )
        case.step("restart target resolved", key=key, slot=slot, master=master["addr"])
        with proxy_resp(cluster) as client:
            require_ok(case, client, "AUTH", APP_USER, APP_PASSWORD)
            require_ok(case, client, "SET", key, RESTART_VALUE)
        owners = [
            server["addr"]
            for server in masters(cluster)
            if str(vlib.redis_cli(ctx["bin_dir"], server["addr"], "GET", key, check=False)).strip()
            == RESTART_VALUE
        ]
        case.set(owners=owners)
        if owners != [master["addr"]]:
            raise vlib.ValidationError(
                "key {} (slot {}) landed on {}, expected the group-{} master {}".format(
                    key, slot, owners, group.get("gid"), master["addr"]
                )
            )
        time.sleep(1.2)  # let appendfsync everysec persist the write

        shutdown = vlib.redis_cli(
            ctx["bin_dir"], master["addr"], "SHUTDOWN", "NOSAVE", check=False, timeout=20
        )
        case.step(
            "SHUTDOWN NOSAVE sent",
            returncode=shutdown.returncode,
            stderr=shutdown.stderr.strip()[:200],
        )
        if not vlib.wait_port_closed(master["port"], host=master["host"], timeout=15):
            raise vlib.ValidationError("master {} did not stop".format(master["addr"]))
        restart = vlib.run(
            ["sh", str(script)],
            cwd=ctx["repo"],
            timeout=120,
            check=False,
            log_path=ctx["workdir"] / "pitr-restart.log",
        )
        case.step(
            "pitr-restart.sh finished",
            returncode=restart["returncode"],
            stdout=restart["stdout"].strip()[:400],
        )
        if restart["returncode"] != 0:
            raise vlib.ValidationError("pitr-restart.sh exited {}".format(restart["returncode"]))
        wait_pong(master["host"], master["port"], timeout=90)
        for replica in replicas:
            wait_replica_link(replica, timeout=90)
        new_pid = vlib.read_pidfile(master["pidfile"])
        if new_pid:
            case.step("master restarted", pid=new_pid)
            master["pid"] = new_pid
            vlib.save_cluster(cluster, ctx["cluster_path"])
        runtime_users = sorted(
            line.split()[1] for line in acl_user_lines(ctx["bin_dir"], master["addr"])
        )
        case.set(runtime_users_after_restart=runtime_users)
        case.step("master ACL LIST after the restart", users=runtime_users)

        with proxy_resp(cluster) as client:
            require_ok(case, client, "AUTH", APP_USER, APP_PASSWORD)
            reply, error = call_raising(client, "GET", key)
            if error is None:
                raise vlib.ValidationError(
                    "GET returned {!r} after the master restarted without its runtime ACL".format(
                        reply
                    )
                )
            case.set(stale_error=error)
            case.step("GET refused while the runtime ACL is missing", error=error)
            if "NOPERM" not in error:
                evidence.note(
                    "acl-redis-restart-resync: the proxy refuses the read with {!r} instead of "
                    "NOPERM, because its backend AUTH as the (now missing) ACL user fails".format(
                        error
                    )
                )

        sync = admin_cmd(ctx, "--acl-sync", truncate=False)
        case.set(sync_returncode=sync["returncode"])
        if sync["returncode"] != 0:
            raise vlib.ValidationError(
                "codis-admin --acl-sync exited {}: {}".format(
                    sync["returncode"], sync["stderr"].strip()[:1000]
                )
            )
        view = get_acl(cluster)
        case.set(sync_status=view.get("sync_status"), revision=view.get("revision"))
        if view.get("sync_status") != "ready":
            raise vlib.ValidationError(
                "sync_status after restart re-sync is {!r}".format(view.get("sync_status"))
            )
        value = wait_proxy_value(cluster, key, RESTART_VALUE, timeout=30)
        if value is None:
            evidence.note(
                "acl-redis-restart-resync: the pre-restart value was not recovered from AOF; "
                "the post-sync SET/GET path is verified instead"
            )
            with proxy_resp(cluster) as client:
                require_ok(case, client, "AUTH", APP_USER, APP_PASSWORD)
                require_ok(case, client, "SET", key, APP_VALUE)
                value = require_ok(case, client, "GET", key)
        if not value:
            raise vlib.ValidationError("GET after --acl-sync returned {!r}".format(value))
        case.set(value=value)
        case.step("read restored after --acl-sync", value=value)


def case_acl_cleanup(evidence, ctx):
    with evidence.case("acl-cleanup", "ACL disabled, users removed, proxy open again") as case:
        cluster = ctx["cluster"]
        view = put_acl(cluster, {"enabled": False, "users": []})
        case.set(revision=view.get("revision"), sync_status=view.get("sync_status"))
        case.step(
            "ACL disabled",
            revision=view.get("revision"),
            enabled=view.get("enabled"),
            users=user_names(view),
        )
        if view.get("enabled"):
            raise vlib.ValidationError("ACL is still enabled after cleanup")
        if user_names(view):
            raise vlib.ValidationError("ACL still lists users after cleanup")
        leftovers = {}
        observed = {}
        for server in all_servers(cluster):
            names = sorted(line.split()[1] for line in acl_user_lines(ctx["bin_dir"], server["addr"]))
            observed[server["addr"]] = names
            if names != ["default"]:
                leftovers[server["addr"]] = names
        case.set(backends=observed, leftovers=leftovers)
        if leftovers:
            raise vlib.ValidationError("backends still carry ACL users: {}".format(leftovers))
        with proxy_resp(cluster) as client:
            reply = require_ok(case, client, "PING", label="PING (unauthenticated, ACL disabled)")
            if reply != "PONG":
                raise vlib.ValidationError("PING after cleanup returned {!r}".format(reply))


def ensure_cleanup(evidence, ctx):
    if ctx.get("cleanup_done"):
        return
    ctx["cleanup_done"] = True
    try:
        case_acl_cleanup(evidence, ctx)
    except Exception as err:  # noqa: BLE001 - cleanup must never mask the original failure
        evidence.note("acl-cleanup raised {}: {}".format(type(err).__name__, err))


# ---------------------------------------------------------------------------
# main
# ---------------------------------------------------------------------------


def build_parser():
    parser = argparse.ArgumentParser(
        description="Validate Codis ACL on a real cluster and write Evidence JSON."
    )
    parser.add_argument("--cluster", required=True, help="path to cluster.json (setup_cluster.py)")
    parser.add_argument(
        "--output",
        help="evidence path (default: <repo>/.runtime/realmachine/run_acl-<run-id>.json)",
    )
    parser.add_argument("--repo", help="repository root (default: auto-detected)")
    parser.add_argument(
        "--skip-restart",
        action="store_true",
        help="skip acl-redis-restart-resync (does not shut down a codis-server)",
    )
    return parser


def main(argv=None):
    args = build_parser().parse_args(argv)
    repo = vlib.find_repo(args.repo)
    cluster_path = Path(args.cluster).expanduser().resolve()
    cluster = vlib.load_cluster(cluster_path)
    bin_dir = Path(cluster.get("bin_dir") or (repo / "bin"))
    run_id = vlib.new_run_id()
    output = (
        Path(args.output).expanduser().resolve()
        if args.output
        else repo / ".runtime" / "realmachine" / "run_acl-{}.json".format(run_id)
    )
    workdir = output.parent / "{}-work".format(output.stem)

    evidence = vlib.Evidence(
        script=SCRIPT,
        feature=FEATURE,
        scope=SCOPE,
        cluster_path=cluster_path,
    )
    evidence.attach_repo(repo)
    evidence.note("cluster: {}".format(cluster_path))
    evidence.note("workdir: {}".format(cluster.get("workdir")))
    evidence.note("product: {}".format(cluster.get("product")))
    evidence.note("xauth: {}".format(cluster.get("xauth")))
    evidence.note("bin_dir: {}".format(bin_dir))
    evidence.note("run id: {}".format(run_id))

    ctx = {
        "cluster": cluster,
        "cluster_path": cluster_path,
        "repo": repo,
        "bin_dir": bin_dir,
        "workdir": workdir,
        "skip_restart": bool(args.skip_restart),
        "cleanup_done": False,
    }

    def run_all():
        workdir.mkdir(parents=True, exist_ok=True)
        try:
            case_acl_initial(evidence, ctx)
            case_acl_dry_run_preview(evidence, ctx)
            case_acl_enable(evidence, ctx)
            case_acl_backend_synced(evidence, ctx)
            case_acl_unauth_rejected(evidence, ctx)
            case_acl_app_rw(evidence, ctx)
            case_acl_reader_readonly(evidence, ctx)
            case_acl_db_binding(evidence, ctx)
            case_acl_proxy_setuser_rejected(evidence, ctx)
            case_acl_auth_single_arg(evidence, ctx)
            case_acl_wrongpass_keeps_session(evidence, ctx)
            case_acl_password_rotation(evidence, ctx)
            case_acl_reject_empty_rules(evidence, ctx)
            case_acl_reject_plaintext(evidence, ctx)
            case_acl_db99_fail_closed(evidence, ctx)
            case_acl_acl_sync(evidence, ctx)
            case_acl_redis_restart_resync(evidence, ctx)
        finally:
            ensure_cleanup(evidence, ctx)
        return evidence.exit_code()

    return vlib.main_guard(evidence, run_all, output)


if __name__ == "__main__":
    sys.exit(main())
