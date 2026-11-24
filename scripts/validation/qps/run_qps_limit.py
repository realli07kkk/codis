#!/usr/bin/env python3
"""Validate the Codis product-level proxy QPS limit on a real cluster.

The script drives `GET/PUT /api/topom/proxy/qps-limit/<xauth>`, reads the
per-proxy token-bucket counters from `GET http://<admin>/proxy/stats`,
sends RESP bursts that must be rejected with "proxy qps limit exceeded",
checks token refill, negative-input rejection, disabling with limit=0, replay
after a proxy restart, and that AUTH/QUIT bypass the limiter.

Every observation lands in one Evidence JSON file; the exit code follows the
evidence status (0 passed, 1 failed, 2 blocked/skipped). The limit is always
reset to 0 in the `qps-cleanup` case, which also runs from a `finally` block
when an earlier case raises.
"""

import argparse
import json
import subprocess
import sys
import time
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import vlib

SCRIPT = "scripts/validation/qps/run_qps_limit.py"
FEATURE = "codis-proxy-qps-limit"
SCOPE = (
    "product-level proxy qps limit: topom API, per-proxy token bucket, burst/refill, "
    "disable, proxy restart replay and AUTH/QUIT exemption"
)

QPS_EXCEEDED = "qps limit exceeded"

# pkg/proxy/proxy_api.go exposes the xauth-free stats route at "/proxy/stats";
# "/api/proxy/stats" only exists as "/api/proxy/stats/{xauth}".
PROXY_STATS_PATH = "/proxy/stats"


# ---------------------------------------------------------------------------
# topom / proxy helpers
# ---------------------------------------------------------------------------


def topom_url(cluster, path):
    return "http://{}{}".format(cluster["dashboard"]["addr"], path)


def qps_url(cluster):
    return topom_url(cluster, "/api/topom/proxy/qps-limit/{}".format(cluster["xauth"]))


def cause_text(data):
    if isinstance(data, dict):
        cause = data.get("Cause")
        if cause:
            return str(cause)
    try:
        return json.dumps(data, sort_keys=True)[:1000]
    except (TypeError, ValueError):
        return str(data)


def get_qps(cluster):
    status, data = vlib.http_json("GET", qps_url(cluster))
    if status != 200 or not isinstance(data, dict):
        raise vlib.ValidationError(
            "GET qps-limit returned HTTP {}: {}".format(status, cause_text(data))
        )
    return data


def put_qps(cluster, limit):
    status, data = vlib.http_json("PUT", qps_url(cluster), {"limit": int(limit)})
    if status != 200 or not isinstance(data, dict):
        raise vlib.ValidationError(
            "PUT qps-limit returned HTTP {}: {}".format(status, cause_text(data))
        )
    return data


def put_qps_rejected(cluster, limit, needle):
    status, data = vlib.http_json("PUT", qps_url(cluster), {"limit": int(limit)})
    if status != 800:
        raise vlib.ValidationError(
            "PUT qps-limit returned HTTP {} (expected the project 800 error): {}".format(
                status, cause_text(data)
            )
        )
    cause = cause_text(data)
    if needle.lower() not in cause.lower():
        raise vlib.ValidationError(
            "PUT qps-limit rejected with {!r}, expected {!r}".format(cause, needle)
        )
    return cause


def proxy_stats(admin_addr, timeout=5):
    url = "http://{}{}".format(admin_addr, PROXY_STATS_PATH)
    status, data = vlib.http_json("GET", url, timeout=timeout)
    if status != 200 or not isinstance(data, dict):
        raise vlib.ValidationError("GET {} returned HTTP {}: {}".format(url, status, cause_text(data)))
    return data


def qps_counters(admin_addr):
    """Return the qps_limit section of the proxy stats, normalised."""
    stats = proxy_stats(admin_addr)
    qps = stats.get("qps_limit")
    present = isinstance(qps, dict)
    qps = qps if present else {}
    return {
        "online": bool(stats.get("online")),
        "present": present,
        "revision": qps.get("revision"),
        "limit": qps.get("limit"),
        "enabled": qps.get("enabled"),
        "accepted": int(qps.get("accepted") or 0),
        "rejected": int(qps.get("rejected") or 0),
    }


def all_counters(cluster):
    return {
        proxy["addr"]: qps_counters(proxy["admin_addr"]) for proxy in cluster.get("proxies") or []
    }


def burst(host, port, count, timeout=5):
    """Send ``count`` PINGs on one connection and classify the replies."""
    accepted = 0
    rejected = 0
    unexpected = []
    started = time.monotonic()
    with vlib.Resp(host, port, timeout=timeout) as client:
        for _ in range(count):
            try:
                reply = client.cmd("PING")
            except vlib.RedisError as err:
                text = str(err)
                if QPS_EXCEEDED in text:
                    rejected += 1
                else:
                    unexpected.append(text)
                continue
            if reply == "PONG":
                accepted += 1
            else:
                unexpected.append("unexpected PING reply {!r}".format(reply))
    return {
        "count": count,
        "accepted": accepted,
        "rejected": rejected,
        "unexpected": unexpected,
        "duration_sec": round(time.monotonic() - started, 3),
    }


def wait_proxy_ready(admin_addr, revision, limit, timeout=120):
    """Wait until the proxy is online and replayed ``revision``/``limit``."""
    deadline = time.time() + timeout
    last = None
    while time.time() < deadline:
        try:
            stats = proxy_stats(admin_addr, timeout=5)
        except vlib.ValidationError as err:
            last = str(err)
            time.sleep(0.3)
            continue
        qps = stats.get("qps_limit")
        if (
            stats.get("online")
            and isinstance(qps, dict)
            and qps.get("revision") == revision
            and qps.get("limit") == limit
        ):
            return stats
        last = "online={!r} qps_limit={!r}".format(stats.get("online"), qps)
        time.sleep(0.3)
    raise vlib.ValidationError(
        "proxy admin {} did not replay revision={} limit={} within {}s: {}".format(
            admin_addr, revision, limit, timeout, last
        )
    )


def restart_proxy(ctx, case, index):
    """Stop a proxy recorded in cluster.json, start it again and record the new pid."""
    cluster = ctx["cluster"]
    proxy = cluster["proxies"][index]
    old_pid = proxy.get("pid")
    stopped = vlib.stop_pid(
        old_pid,
        "codis-proxy-{}".format(index),
        expect_substring=[proxy["conf"], str(ctx["repo"])],
    )
    case.step("proxy stopped", index=index, pid=old_pid, status=stopped.get("status"))
    if stopped.get("status") not in ("stopped", "not_running"):
        raise vlib.ValidationError(
            "cannot stop codis-proxy-{}: {}".format(index, json.dumps(stopped, sort_keys=True))
        )
    if not vlib.wait_port_closed(proxy["port"], host=proxy["host"], timeout=15):
        raise vlib.ValidationError("proxy port {} stayed open".format(proxy["port"]))
    log_path = Path(proxy["log"])
    log_path.parent.mkdir(parents=True, exist_ok=True)
    command = [
        str(Path(ctx["bin_dir"]) / "codis-proxy"),
        "-c",
        proxy["conf"],
        "--dashboard={}".format(cluster["dashboard"]["addr"]),
    ]
    with log_path.open("ab") as handle:
        handle.write(
            "# {} restart by {}\n".format(vlib.utc_now(), SCRIPT).encode("utf-8")
        )
        handle.flush()
        proc = subprocess.Popen(
            command,
            cwd=str(ctx["repo"]),
            stdin=subprocess.DEVNULL,
            stdout=handle,
            stderr=subprocess.STDOUT,
            start_new_session=True,
        )
    proxy["pid"] = proc.pid
    vlib.save_cluster(cluster, ctx["cluster_path"])
    case.step(
        "proxy restarted",
        index=index,
        pid=proc.pid,
        command=" ".join(command),
        log=str(log_path),
    )
    vlib.wait_tcp(proxy["admin_addr"], timeout=60)
    vlib.wait_tcp(proxy["addr"], timeout=60)
    return proc.pid


# ---------------------------------------------------------------------------
# cases
# ---------------------------------------------------------------------------


def case_qps_initial(evidence, ctx):
    with evidence.case("qps-initial", "topom qps limit starts from revision 0 / limit 0") as case:
        cluster = ctx["cluster"]
        view = get_qps(cluster)
        case.set(initial_view=view)
        case.step(
            "initial qps limit",
            revision=view.get("revision"),
            limit=view.get("limit"),
            enabled=view.get("enabled"),
            sync_status=view.get("sync_status"),
        )
        if view.get("revision") != 0 or view.get("limit") != 0 or view.get("enabled"):
            evidence.note(
                "qps-initial: residual qps limit (revision={}, limit={}); resetting to 0".format(
                    view.get("revision"), view.get("limit")
                )
            )
            put_qps(cluster, 0)
            view = get_qps(cluster)
            case.set(reset_view=view)
            case.step(
                "reset residual limit",
                revision=view.get("revision"),
                limit=view.get("limit"),
                enabled=view.get("enabled"),
            )
        if view.get("limit") != 0 or view.get("enabled"):
            raise vlib.ValidationError(
                "qps limit is still active: limit={!r} enabled={!r}".format(
                    view.get("limit"), view.get("enabled")
                )
            )
        if view.get("revision") == 0:
            if view.get("sync_status") != "not_configured":
                raise vlib.ValidationError(
                    "fresh sync_status is {!r}, expected 'not_configured'".format(
                        view.get("sync_status")
                    )
                )
        elif view.get("sync_status") != "ready":
            raise vlib.ValidationError(
                "reset sync_status is {!r}, expected 'ready'".format(view.get("sync_status"))
            )
        ctx["baseline"] = all_counters(cluster)
        case.set(proxy_baseline=ctx["baseline"])
        for addr, counters in sorted(ctx["baseline"].items()):
            case.step("proxy counter baseline", proxy=addr, **counters)


def case_qps_set_limit_5(evidence, ctx):
    with evidence.case("qps-set-limit-5", "PUT limit=5 bumps the revision and reports ready") as case:
        cluster = ctx["cluster"]
        before = get_qps(cluster)
        view = put_qps(cluster, 5)
        case.set(revision=view.get("revision"), limit=view.get("limit"))
        case.step(
            "limit applied",
            revision=view.get("revision"),
            limit=view.get("limit"),
            enabled=view.get("enabled"),
            sync_status=view.get("sync_status"),
        )
        if view.get("limit") != 5:
            raise vlib.ValidationError("PUT limit=5 returned limit={!r}".format(view.get("limit")))
        if not view.get("enabled"):
            raise vlib.ValidationError("PUT limit=5 did not report enabled=true")
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
        product = get_qps(cluster)
        if product.get("revision") != view.get("revision") or product.get("limit") != 5:
            raise vlib.ValidationError("GET after PUT does not echo the new revision/limit")
        ctx["revision"] = view.get("revision")


def case_qps_stats_propagated(evidence, ctx):
    with evidence.case(
        "qps-stats-propagated", "every proxy reports the product revision/limit"
    ) as case:
        cluster = ctx["cluster"]
        view = get_qps(cluster)
        revision = view.get("revision")
        baseline = ctx.get("baseline") or {}
        observed = {}
        problems = []
        for proxy in cluster.get("proxies") or []:
            counters = qps_counters(proxy["admin_addr"])
            observed[proxy["addr"]] = counters
            case.step("proxy stats", proxy=proxy["addr"], **counters)
            if not counters["present"]:
                problems.append("{}: qps_limit is absent".format(proxy["addr"]))
                continue
            if counters["revision"] != revision or counters["limit"] != 5:
                problems.append(
                    "{}: revision/limit {!r}/{!r} != {!r}/5".format(
                        proxy["addr"], counters["revision"], counters["limit"], revision
                    )
                )
            if not counters["enabled"]:
                problems.append("{}: qps_limit.enabled is false".format(proxy["addr"]))
            base = baseline.get(proxy["addr"]) or {}
            if (
                counters["accepted"] != int(base.get("accepted") or 0)
                or counters["rejected"] != int(base.get("rejected") or 0)
            ):
                problems.append(
                    "{}: counters moved before traffic accepted={} rejected={} (baseline {}/{})".format(
                        proxy["addr"],
                        counters["accepted"],
                        counters["rejected"],
                        base.get("accepted"),
                        base.get("rejected"),
                    )
                )
        case.set(proxies=observed)
        for addr, counters in sorted((baseline or {}).items()):
            if counters["accepted"] or counters["rejected"]:
                evidence.note(
                    "qps-stats-propagated: {} started with accepted={} rejected={} "
                    "(left over from an earlier run); deltas are asserted".format(
                        addr, counters["accepted"], counters["rejected"]
                    )
                )
        if problems:
            raise vlib.ValidationError("; ".join(problems))


def case_qps_burst_limit_5(evidence, ctx):
    with evidence.case(
        "qps-burst-limit-5", "20 sequential PINGs on a limit=5 proxy yield 5/15"
    ) as case:
        cluster = ctx["cluster"]
        observed = {}
        problems = []
        for proxy in cluster.get("proxies") or []:
            before = qps_counters(proxy["admin_addr"])
            result = burst(proxy["host"], proxy["port"], 20)
            after = qps_counters(proxy["admin_addr"])
            observed[proxy["addr"]] = {
                "burst": result,
                "accepted_delta": after["accepted"] - before["accepted"],
                "rejected_delta": after["rejected"] - before["rejected"],
            }
            case.step("burst", proxy=proxy["addr"], **result)
            if result["unexpected"]:
                problems.append(
                    "{}: unexpected replies {}".format(proxy["addr"], result["unexpected"][:5])
                )
            if result["accepted"] != 5 or result["rejected"] != 15:
                problems.append(
                    "{}: accepted/rejected {}/{} != 5/15".format(
                        proxy["addr"], result["accepted"], result["rejected"]
                    )
                )
            if result["duration_sec"] >= 1.0:
                problems.append(
                    "{}: burst took {}s, the token bucket refilled during the burst".format(
                        proxy["addr"], result["duration_sec"]
                    )
                )
            if (
                after["accepted"] - before["accepted"] != result["accepted"]
                or after["rejected"] - before["rejected"] != result["rejected"]
            ):
                problems.append(
                    "{}: proxy counters do not match the replies (delta {}/{})".format(
                        proxy["addr"],
                        after["accepted"] - before["accepted"],
                        after["rejected"] - before["rejected"],
                    )
                )
        case.set(proxies=observed)
        if problems:
            raise vlib.ValidationError("; ".join(problems))


def case_qps_refill(evidence, ctx):
    with evidence.case("qps-refill", "after 1.2s the bucket refills to 5 tokens") as case:
        cluster = ctx["cluster"]
        time.sleep(1.2)
        observed = {}
        problems = []
        for proxy in cluster.get("proxies") or []:
            before = qps_counters(proxy["admin_addr"])
            result = burst(proxy["host"], proxy["port"], 5)
            after = qps_counters(proxy["admin_addr"])
            observed[proxy["addr"]] = {
                "burst": result,
                "accepted_delta": after["accepted"] - before["accepted"],
            }
            case.step("refilled burst", proxy=proxy["addr"], **result)
            if result["unexpected"] or result["rejected"]:
                problems.append(
                    "{}: refilled bucket still rejected: {}".format(
                        proxy["addr"], result["unexpected"] + [result["rejected"]]
                    )
                )
            if result["accepted"] != 5:
                problems.append(
                    "{}: accepted {} != 5 after a 1.2s refill".format(
                        proxy["addr"], result["accepted"]
                    )
                )
            if after["accepted"] - before["accepted"] != 5:
                problems.append(
                    "{}: accepted counter delta {} != 5".format(
                        proxy["addr"], after["accepted"] - before["accepted"]
                    )
                )
        case.set(proxies=observed)
        if problems:
            raise vlib.ValidationError("; ".join(problems))


def case_qps_reject_negative(evidence, ctx):
    with evidence.case(
        "qps-reject-negative", "PUT limit=-1 is rejected with HTTP 800 and changes nothing"
    ) as case:
        cluster = ctx["cluster"]
        before = get_qps(cluster)
        cause = put_qps_rejected(cluster, -1, "proxy qps limit must be >= 0")
        after = get_qps(cluster)
        case.set(cause=cause, revision=after.get("revision"), limit=after.get("limit"))
        case.step("negative limit rejected", cause=cause)
        if after.get("revision") != before.get("revision"):
            raise vlib.ValidationError(
                "a rejected PUT bumped the revision {} -> {}".format(
                    before.get("revision"), after.get("revision")
                )
            )
        if after.get("limit") != before.get("limit"):
            raise vlib.ValidationError(
                "a rejected PUT changed the limit {} -> {}".format(
                    before.get("limit"), after.get("limit")
                )
            )


def case_qps_disable_limit_0(evidence, ctx):
    with evidence.case("qps-disable-limit-0", "limit=0 disables the limiter for all proxies") as case:
        cluster = ctx["cluster"]
        before = get_qps(cluster)
        view = put_qps(cluster, 0)
        case.set(revision=view.get("revision"), limit=view.get("limit"))
        case.step(
            "limit disabled",
            revision=view.get("revision"),
            limit=view.get("limit"),
            enabled=view.get("enabled"),
            sync_status=view.get("sync_status"),
        )
        if view.get("limit") != 0 or view.get("enabled"):
            raise vlib.ValidationError(
                "PUT limit=0 returned limit={!r} enabled={!r}".format(
                    view.get("limit"), view.get("enabled")
                )
            )
        if view.get("revision") != before.get("revision", 0) + 1:
            raise vlib.ValidationError("PUT limit=0 did not bump the revision exactly once")
        observed = {}
        problems = []
        for proxy in cluster.get("proxies") or []:
            before_counters = qps_counters(proxy["admin_addr"])
            result = burst(proxy["host"], proxy["port"], 20)
            after_counters = qps_counters(proxy["admin_addr"])
            observed[proxy["addr"]] = {
                "burst": result,
                "stats_present": after_counters["present"],
                "stats": after_counters,
            }
            case.step("unlimited burst", proxy=proxy["addr"], **result)
            if result["unexpected"] or result["rejected"] or result["accepted"] != 20:
                problems.append(
                    "{}: limit=0 still limited traffic {}".format(proxy["addr"], result)
                )
            if not after_counters["present"]:
                evidence.note(
                    "qps-disable-limit-0: {} hides the qps_limit field while the limit is 0".format(
                        proxy["addr"]
                    )
                )
            else:
                evidence.note(
                    "qps-disable-limit-0: {} still reports qps_limit with "
                    "enabled={!r} limit={!r} revision={!r} while the limit is 0".format(
                        proxy["addr"],
                        after_counters["enabled"],
                        after_counters["limit"],
                        after_counters["revision"],
                    )
                )
                if after_counters["enabled"]:
                    problems.append("{}: qps_limit.enabled is still true".format(proxy["addr"]))
            if after_counters["accepted"] != before_counters["accepted"]:
                evidence.note(
                    "qps-disable-limit-0: {} counts accepted={} even with limit=0 "
                    "(vlib observation, not asserted)".format(
                        proxy["addr"], after_counters["accepted"]
                    )
                )
        case.set(proxies=observed)
        if problems:
            raise vlib.ValidationError("; ".join(problems))


def case_qps_proxy_restart_replay(evidence, ctx):
    with evidence.case(
        "qps-proxy-restart-replay", "a restarted proxy replays limit=7 from the dashboard"
    ) as case:
        cluster = ctx["cluster"]
        view = put_qps(cluster, 7)
        revision = view.get("revision")
        case.step("limit=7 applied", revision=revision, limit=view.get("limit"))
        if view.get("limit") != 7:
            raise vlib.ValidationError("PUT limit=7 returned limit={!r}".format(view.get("limit")))
        restart_proxy(ctx, case, 0)
        stats = wait_proxy_ready(cluster["proxies"][0]["admin_addr"], revision, 7, timeout=120)
        qps = stats.get("qps_limit") or {}
        case.set(
            replayed_revision=qps.get("revision"),
            replayed_limit=qps.get("limit"),
            replayed_accepted=qps.get("accepted"),
            replayed_rejected=qps.get("rejected"),
        )
        case.step("restarted proxy replayed the limit", **qps)
        if qps.get("revision") != revision or qps.get("limit") != 7:
            raise vlib.ValidationError(
                "restarted proxy reports revision/limit {!r}/{!r}, expected {!r}/7".format(
                    qps.get("revision"), qps.get("limit"), revision
                )
            )
        if not qps.get("enabled"):
            raise vlib.ValidationError("restarted proxy reports qps_limit.enabled=false")
        if qps.get("accepted") != 0 or qps.get("rejected") != 0:
            raise vlib.ValidationError(
                "restarted proxy counters are not fresh: accepted={!r} rejected={!r}".format(
                    qps.get("accepted"), qps.get("rejected")
                )
            )
        product = get_qps(cluster)
        case.set(sync_status=product.get("sync_status"))
        if product.get("sync_status") != "ready":
            raise vlib.ValidationError(
                "product sync_status after the proxy restart is {!r}".format(
                    product.get("sync_status")
                )
            )
        if product.get("revision") != revision or product.get("limit") != 7:
            raise vlib.ValidationError("product view moved during the proxy restart")
        peer = qps_counters(cluster["proxies"][1]["admin_addr"])
        case.set(peer=peer)
        case.step("peer proxy stats", **peer)
        if peer["revision"] != revision or peer["limit"] != 7:
            raise vlib.ValidationError(
                "the untouched proxy reports revision/limit {!r}/{!r}".format(
                    peer["revision"], peer["limit"]
                )
            )


def case_qps_auth_quit_exempt(evidence, ctx):
    with evidence.case("qps-auth-quit-exempt", "AUTH and QUIT bypass the limiter") as case:
        cluster = ctx["cluster"]
        view = put_qps(cluster, 1)
        case.step("limit=1 applied", revision=view.get("revision"), limit=view.get("limit"))
        if view.get("limit") != 1:
            raise vlib.ValidationError("PUT limit=1 returned limit={!r}".format(view.get("limit")))
        proxy = cluster["proxies"][0]
        with vlib.Resp(proxy["host"], proxy["port"], timeout=5) as client:
            drain = burst(proxy["host"], proxy["port"], 3)
            case.step("bucket drained", **drain)
            if drain["accepted"] != 1 or drain["rejected"] != 2:
                raise vlib.ValidationError(
                    "draining a limit=1 bucket with 3 PINGs gave {}/{}".format(
                        drain["accepted"], drain["rejected"]
                    )
                )
            try:
                auth_reply = client.cmd("AUTH", "qps-exempt-password")
                auth_error = None
            except vlib.RedisError as err:
                auth_reply = None
                auth_error = str(err)
            case.set(auth_reply=auth_reply, auth_error=auth_error)
            case.step("AUTH under an exhausted bucket", reply=str(auth_reply), error=auth_error)
            if auth_error is None:
                raise vlib.ValidationError(
                    "AUTH unexpectedly succeeded: {!r}".format(auth_reply)
                )
            if QPS_EXCEEDED in auth_error:
                raise vlib.ValidationError("AUTH was throttled: {!r}".format(auth_error))
            if "no password is set" not in auth_error.lower():
                raise vlib.ValidationError(
                    "AUTH returned {!r}, expected the no-password text".format(auth_error)
                )
            try:
                quit_reply = client.cmd("QUIT")
                quit_error = None
            except vlib.RedisError as err:
                quit_reply = None
                quit_error = str(err)
            case.set(quit_reply=quit_reply, quit_error=quit_error)
            case.step("QUIT under an exhausted bucket", reply=str(quit_reply), error=quit_error)
            if quit_error is not None or quit_reply != "OK":
                raise vlib.ValidationError(
                    "QUIT returned reply={!r} error={!r}".format(quit_reply, quit_error)
                )


def case_qps_burst_limit_1(evidence, ctx):
    with evidence.case("qps-burst-limit-1", "10 sequential PINGs on a limit=1 proxy yield 1/9") as case:
        cluster = ctx["cluster"]
        proxy = cluster["proxies"][0]
        # The previous case drained proxy 0's bucket; a 1/s bucket needs ~1s to
        # hand back its single token, so start from a full bucket.
        time.sleep(1.2)
        before = qps_counters(proxy["admin_addr"])
        result = burst(proxy["host"], proxy["port"], 10)
        after = qps_counters(proxy["admin_addr"])
        case.set(burst=result, accepted_delta=after["accepted"] - before["accepted"])
        case.step("burst", **result)
        if result["unexpected"]:
            raise vlib.ValidationError("unexpected replies {}".format(result["unexpected"][:5]))
        if result["accepted"] != 1 or result["rejected"] != 9:
            raise vlib.ValidationError(
                "accepted/rejected {}/{} != 1/9".format(result["accepted"], result["rejected"])
            )
        if result["duration_sec"] >= 1.0:
            raise vlib.ValidationError("burst took {}s".format(result["duration_sec"]))
        if after["accepted"] - before["accepted"] != 1 or after["rejected"] - before["rejected"] != 9:
            raise vlib.ValidationError("proxy counters do not match the replies")


def case_qps_cleanup(evidence, ctx):
    with evidence.case("qps-cleanup", "limit reset to 0, limiter reported as disabled") as case:
        cluster = ctx["cluster"]
        view = put_qps(cluster, 0)
        case.set(revision=view.get("revision"), limit=view.get("limit"))
        case.step(
            "limit disabled",
            revision=view.get("revision"),
            limit=view.get("limit"),
            enabled=view.get("enabled"),
            sync_status=view.get("sync_status"),
        )
        if view.get("limit") != 0 or view.get("enabled"):
            raise vlib.ValidationError(
                "cleanup left limit={!r} enabled={!r}".format(
                    view.get("limit"), view.get("enabled")
                )
            )
        product = get_qps(cluster)
        case.set(product=product)
        if product.get("enabled") or product.get("limit") != 0:
            raise vlib.ValidationError("GET after cleanup still reports an active limit")
        observed = {}
        for proxy in cluster.get("proxies") or []:
            counters = qps_counters(proxy["admin_addr"])
            observed[proxy["addr"]] = counters
            case.step("proxy stats after cleanup", proxy=proxy["addr"], **counters)
            if counters["present"] and counters["enabled"]:
                raise vlib.ValidationError(
                    "proxy {} still reports enabled=true".format(proxy["addr"])
                )
            if counters["present"] and counters["limit"] != 0:
                raise vlib.ValidationError(
                    "proxy {} still reports limit={!r}".format(proxy["addr"], counters["limit"])
                )
        case.set(proxies=observed)


def ensure_cleanup(evidence, ctx):
    if ctx.get("cleanup_done"):
        return
    ctx["cleanup_done"] = True
    try:
        case_qps_cleanup(evidence, ctx)
    except Exception as err:  # noqa: BLE001 - cleanup must never mask the original failure
        evidence.note("qps-cleanup raised {}: {}".format(type(err).__name__, err))


# ---------------------------------------------------------------------------
# main
# ---------------------------------------------------------------------------


def build_parser():
    parser = argparse.ArgumentParser(
        description="Validate the Codis proxy QPS limit on a real cluster and write Evidence JSON."
    )
    parser.add_argument("--cluster", required=True, help="path to cluster.json (setup_cluster.py)")
    parser.add_argument(
        "--output",
        help="evidence path (default: <repo>/.runtime/realmachine/run_qps_limit-<run-id>.json)",
    )
    parser.add_argument("--repo", help="repository root (default: auto-detected)")
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
        else repo / ".runtime" / "realmachine" / "run_qps_limit-{}.json".format(run_id)
    )

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
    evidence.note("proxies: {}".format([proxy["addr"] for proxy in cluster.get("proxies") or []]))
    evidence.note(
        "proxy stats are read from {} (the xauth-free route); /api/proxy/stats only "
        "exists with a /{{xauth}} suffix".format(PROXY_STATS_PATH)
    )
    evidence.note("run id: {}".format(run_id))

    ctx = {
        "cluster": cluster,
        "cluster_path": cluster_path,
        "repo": repo,
        "bin_dir": bin_dir,
        "cleanup_done": False,
    }

    def run_all():
        try:
            case_qps_initial(evidence, ctx)
            case_qps_set_limit_5(evidence, ctx)
            case_qps_stats_propagated(evidence, ctx)
            case_qps_burst_limit_5(evidence, ctx)
            case_qps_refill(evidence, ctx)
            case_qps_reject_negative(evidence, ctx)
            case_qps_disable_limit_0(evidence, ctx)
            case_qps_proxy_restart_replay(evidence, ctx)
            case_qps_auth_quit_exempt(evidence, ctx)
            case_qps_burst_limit_1(evidence, ctx)
        finally:
            ensure_cleanup(evidence, ctx)
        return evidence.exit_code()

    return vlib.main_guard(evidence, run_all, output)


if __name__ == "__main__":
    sys.exit(main())
