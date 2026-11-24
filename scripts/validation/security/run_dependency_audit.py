#!/usr/bin/env python3
"""Dependency and supply-chain audit for the Codis tree.

Cases: govulncheck, go-build, go-test, npm-audit, redis3-removed,
server-version. Result is one Evidence JSON document; exit code follows the
evidence status (0 passed, 1 failed, 2 blocked/skipped).
"""

import argparse
import json
import shutil
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import vlib

FEATURE = "dependency-security-audit"
SCOPE = "dependency vulnerability and packaging audit; no runtime load testing"

GO_BUILD_TIMEOUT = 30 * 60
GO_TEST_TIMEOUT = 45 * 60
GOVULNCHECK_TIMEOUT = 30 * 60
NPM_AUDIT_TIMEOUT = 15 * 60

GOVULNCHECK_HINT = (
    "govulncheck not installed; install it with: "
    "go install golang.org/x/vuln/cmd/govulncheck@latest"
)

NETWORK_ERROR_CODES = (
    "ENOTFOUND",
    "EAI_AGAIN",
    "ECONNREFUSED",
    "ECONNRESET",
    "ETIMEDOUT",
    "ENETUNREACH",
    "ERR_SOCKET_TIMEOUT",
)


def build_parser():
    parser = argparse.ArgumentParser(
        description="Run the dependency security audit and write Evidence JSON."
    )
    parser.add_argument("--repo", help="repository root (default: auto-detected)")
    parser.add_argument(
        "--output",
        help="evidence path (default: <repo>/.runtime/realmachine/security-audit-<run-id>.json)",
    )
    parser.add_argument("--skip-go-test", action="store_true", help="skip go test")
    parser.add_argument("--skip-npm", action="store_true", help="skip npm audit")
    return parser


def record(case, result, log_path):
    case.set(
        command=" ".join(result["command"]),
        cwd=result["cwd"],
        returncode=result["returncode"],
        duration_sec=result["duration_sec"],
        log=str(log_path),
    )
    return result


def case_govulncheck(evidence, repo, log_dir):
    with evidence.case("govulncheck", "govulncheck ./...") as case:
        binary = shutil.which("govulncheck")
        case.set(govulncheck=binary)
        if not binary:
            case.blocked(GOVULNCHECK_HINT)
            return
        log_path = log_dir / "govulncheck.log"
        result = record(
            case,
            vlib.run(
                [binary, "./..."],
                cwd=repo,
                timeout=GOVULNCHECK_TIMEOUT,
                check=False,
                log_path=log_path,
                truncate=False,
            ),
            log_path,
        )
        combined = result["stdout"] + result["stderr"]
        if "No vulnerabilities found" in combined:
            return
        case.set(output_tail=combined[-4000:] if combined else "")
        case.fail("govulncheck did not report 'No vulnerabilities found' (exit {})".format(result["returncode"]))


def case_go_build(evidence, repo, log_dir):
    with evidence.case("go-build", "go build ./...") as case:
        if not shutil.which("go"):
            case.blocked("go toolchain not found on PATH")
            return
        log_path = log_dir / "go-build.log"
        result = record(
            case,
            vlib.run(["go", "build", "./..."], cwd=repo, timeout=GO_BUILD_TIMEOUT, check=False, log_path=log_path),
            log_path,
        )
        if result["returncode"] != 0:
            case.fail("go build exited {}: {}".format(result["returncode"], result["stderr"].strip()[:1000]))


def case_go_test(evidence, repo, log_dir, skip):
    with evidence.case("go-test", "go test ./cmd/... ./pkg/...") as case:
        log_path = log_dir / "go-test.log"
        case.set(command="go test ./cmd/... ./pkg/...", log=str(log_path))
        if skip:
            case.skip("--skip-go-test")
            return
        if not shutil.which("go"):
            case.blocked("go toolchain not found on PATH")
            return
        result = record(
            case,
            vlib.run(
                ["go", "test", "./cmd/...", "./pkg/..."],
                cwd=repo,
                timeout=GO_TEST_TIMEOUT,
                check=False,
                log_path=log_path,
            ),
            log_path,
        )
        if result["returncode"] != 0:
            case.fail("go test exited {}: {}".format(result["returncode"], result["stderr"].strip()[:1000]))


def case_npm_audit(evidence, repo, log_dir, skip):
    with evidence.case("npm-audit", "npm audit --json (cmd/fe/assets)") as case:
        assets = repo / "cmd" / "fe" / "assets"
        log_path = log_dir / "npm-audit.log"
        case.set(command="npm audit --json", cwd=str(assets), log=str(log_path))
        if skip:
            case.skip("--skip-npm")
            return
        npm = shutil.which("npm")
        case.set(npm=npm)
        if not npm:
            case.blocked("npm not found on PATH")
            return
        if not (assets / "package.json").is_file():
            case.blocked("{} is missing".format(assets / "package.json"))
            return
        result = record(
            case,
            vlib.run(
                [npm, "audit", "--json"],
                cwd=assets,
                timeout=NPM_AUDIT_TIMEOUT,
                check=False,
                log_path=log_path,
                truncate=False,
            ),
            log_path,
        )
        try:
            report = json.loads(result["stdout"])
        except ValueError:
            case.fail(
                "npm audit did not return JSON (exit {}): {}".format(
                    result["returncode"], (result["stderr"] or result["stdout"]).strip()[:1000]
                )
            )
            return
        error = report.get("error") if isinstance(report, dict) else None
        if error:
            code = str(error.get("code") or "")
            summary = str(error.get("summary") or error)
            if code in NETWORK_ERROR_CODES:
                case.blocked("npm audit cannot reach the registry: {}".format(summary))
            else:
                case.fail("npm audit error {}: {}".format(code or "<unknown>", summary))
            return
        vulnerabilities = {}
        if isinstance(report, dict):
            vulnerabilities = (report.get("metadata") or {}).get("vulnerabilities") or {}
        counts = {
            level: int(vulnerabilities.get(level) or 0)
            for level in ("info", "low", "moderate", "high", "critical", "total")
        }
        case.set(vulnerabilities=counts)
        advisories = []
        if isinstance(report, dict):
            for name, entry in (report.get("vulnerabilities") or {}).items():
                severity = (entry or {}).get("severity")
                if severity in ("high", "critical"):
                    advisories.append(
                        {
                            "package": name,
                            "severity": severity,
                            "range": (entry or {}).get("range"),
                            "direct": (entry or {}).get("isDirect"),
                        }
                    )
        case.set(advisories=sorted(advisories, key=lambda item: (item["severity"], item["package"])))
        if counts["critical"] > 0:
            case.fail(
                "npm audit reports {} critical vulnerabilit(ies): {}".format(
                    counts["critical"],
                    ", ".join(item["package"] for item in advisories if item["severity"] == "critical"),
                )
            )
            return
        if counts["high"] > 0:
            evidence.note(
                "npm audit reports {} high severity finding(s) accepted as known residual risk "
                "(AngularJS 1.8.3 is a pinned FE dependency and is not upgradeable in place): {}".format(
                    counts["high"], ", ".join(item["package"] for item in advisories)
                )
            )


def case_redis3_removed(evidence, repo, log_dir):
    with evidence.case("redis3-removed", "Redis 3 sources and build target are gone") as case:
        extern = repo / "extern"
        redis3_dirs = (
            sorted(path.name for path in extern.glob("redis-3*")) if extern.is_dir() else []
        )
        case.set(command="make -n codis-server-redis3", cwd=str(repo), logs=str(log_dir))
        case.set(redis3_dirs=redis3_dirs)
        log_path = log_dir / "redis3-removed.log"
        result = vlib.run(
            ["make", "-n", "codis-server-redis3"],
            cwd=repo,
            timeout=120,
            check=False,
            log_path=log_path,
        )
        record(case, result, log_path)
        combined = result["stdout"] + result["stderr"]
        if redis3_dirs:
            case.fail("extern/ still contains Redis 3 sources: {}".format(", ".join(redis3_dirs)))
        elif result["returncode"] == 0:
            case.fail("make -n codis-server-redis3 unexpectedly succeeded")
        elif "No rule" not in combined:
            case.fail(
                "make -n codis-server-redis3 failed without 'No rule': {}".format(
                    combined.strip()[:500]
                )
            )


def case_server_version(evidence, repo, log_dir):
    with evidence.case("server-version", "bin/codis-server is Redis 8.6") as case:
        binary = repo / "bin" / "codis-server"
        log_path = log_dir / "server-version.log"
        case.set(command="{} --version".format(binary), cwd=str(repo), log=str(log_path))
        result = vlib.run([binary, "--version"], cwd=repo, timeout=60, check=False, log_path=log_path)
        record(case, result, log_path)
        version = result["stdout"].strip()
        case.set(version=version)
        if result["returncode"] != 0:
            case.fail("bin/codis-server --version exited {}".format(result["returncode"]))
        elif "8.6" not in version:
            case.fail("bin/codis-server --version does not mention 8.6: {!r}".format(version))


def main(argv=None):
    args = build_parser().parse_args(argv)
    repo = vlib.find_repo(args.repo)
    run_id = vlib.new_run_id()
    output = (
        Path(args.output).expanduser().resolve()
        if args.output
        else repo / ".runtime" / "realmachine" / "security-audit-{}.json".format(run_id)
    )
    log_dir = repo / ".runtime" / "realmachine" / "dependency-audit-{}".format(run_id)

    evidence = vlib.Evidence(
        script="scripts/validation/security/run_dependency_audit.py",
        feature=FEATURE,
        scope=SCOPE,
    )
    evidence.attach_repo(repo)
    evidence.note("bin/version: {}".format(vlib.read_text(repo / "bin" / "version").strip().replace("\n", "; ")))
    evidence.note("log directory: {}".format(log_dir))

    def run_all():
        log_dir.mkdir(parents=True, exist_ok=True)
        case_govulncheck(evidence, repo, log_dir)
        case_go_build(evidence, repo, log_dir)
        case_go_test(evidence, repo, log_dir, args.skip_go_test)
        case_npm_audit(evidence, repo, log_dir, args.skip_npm)
        case_redis3_removed(evidence, repo, log_dir)
        case_server_version(evidence, repo, log_dir)
        return evidence.exit_code()

    return vlib.main_guard(evidence, run_all, output)


if __name__ == "__main__":
    sys.exit(main())
