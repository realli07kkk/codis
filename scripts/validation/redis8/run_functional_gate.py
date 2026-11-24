#!/usr/bin/env python3
"""Orchestrate the Redis 8 functional gates on a real machine.

Cases: build-all, gotest, tcl-codis, e2e-local and (opt-in) docker. Every case
is recorded in one Evidence JSON file; the exit code follows the evidence
status (0 passed, 1 failed, 2 blocked/skipped).
"""

import argparse
import json
import re
import shutil
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import vlib

REPO_FEATURE = "redis8-functional-gate"
SCOPE = "functional gates only; performance and formal cutover verdict are out of scope"

GROUPS = ("build", "gotest", "tcl", "e2e", "docker")

BUILD_TIMEOUT = 45 * 60
GOTEST_TIMEOUT = 45 * 60
TCL_TIMEOUT = 30 * 60
E2E_TIMEOUT = 30 * 60
DOCKER_BUILD_TIMEOUT = 30 * 60
DOCKER_RUN_TIMEOUT = 5 * 60

TCL_SUITES = (
    "unit/codis",
    "unit/codis_migration",
    "unit/codis_slotsrestore",
    "unit/codis_async_migration",
    "unit/codis_rdb_export",
)

TCL_INSTALL_HINT = (
    "tclsh not found; install Tcl first: "
    "`sudo apt-get install -y tcl` (Ubuntu 22.04) or `brew install tcl-tk` (macOS)"
)


def build_parser():
    parser = argparse.ArgumentParser(
        description="Run the Redis 8 Codis functional gates and write Evidence JSON."
    )
    parser.add_argument("--repo", help="repository root (default: auto-detected)")
    parser.add_argument(
        "--output",
        help="evidence path (default: <repo>/.runtime/realmachine/redis8-gate-<run-id>.json)",
    )
    parser.add_argument(
        "--only",
        action="append",
        choices=("all",) + GROUPS,
        help="run only these groups (repeatable); default: all",
    )
    parser.add_argument("--skip-build", action="store_true", help="do not run make build-all")
    parser.add_argument(
        "--docker",
        action="store_true",
        help="also run the docker image gate (skipped by default)",
    )
    parser.add_argument(
        "--keep-temp", action="store_true", help="pass --keep-temp through to the e2e script"
    )
    parser.add_argument(
        "--workdir",
        help="log directory (default: <repo>/.runtime/realmachine/redis8-gate-<run-id>/)",
    )
    return parser


def resolve_groups(values):
    selected = list(values or ["all"])
    if "all" in selected:
        return set(GROUPS)
    result = set()
    for value in selected:
        for part in str(value).split(","):
            part = part.strip()
            if part:
                result.add(part)
    return result


def record(case, result, log_path):
    case.set(
        command=" ".join(result["command"]),
        cwd=result["cwd"],
        returncode=result["returncode"],
        duration_sec=result["duration_sec"],
        log=str(log_path),
    )
    return result


def run_tcl(evidence, repo, log_path):
    with evidence.case("tcl-codis", "Redis 8 Codis Tcl suites") as case:
        tclsh = shutil.which("tclsh")
        case.set(tclsh=tclsh)
        if not tclsh:
            case.blocked(TCL_INSTALL_HINT)
            return
        redis_root = repo / "extern" / "redis-8.6.3"
        if not (redis_root / "runtest").is_file():
            case.blocked("{} is missing; unpack the Redis 8 sources first".format(redis_root / "runtest"))
            return
        command = ["./runtest"]
        for suite in TCL_SUITES:
            command.extend(["--single", suite])
        case.step("running the Codis Tcl suites", suites=list(TCL_SUITES), timeout_sec=TCL_TIMEOUT)
        result = record(
            case,
            vlib.run(
                command, cwd=redis_root, timeout=TCL_TIMEOUT, check=False, log_path=log_path
            ),
            log_path,
        )
        combined = result["stdout"] + result["stderr"]
        if result["returncode"] != 0:
            case.fail("runtest exited {}".format(result["returncode"]))
        elif "All tests passed" not in combined:
            case.fail("runtest output does not contain 'All tests passed'")


def run_e2e(evidence, repo, workdir, keep_temp):
    with evidence.case("e2e-local", "local Codis cluster e2e (scripts/redis8_local_validation.py)") as case:
        script = repo / "scripts" / "redis8_local_validation.py"
        if not script.is_file():
            case.blocked("{} is missing".format(script))
            return
        evidence_path = workdir / "e2e-local-evidence.json"
        log_path = workdir / "e2e-local.log"
        command = [
            "python3",
            "scripts/redis8_local_validation.py",
            "--only",
            "e2e",
            "--output",
            str(evidence_path),
        ]
        if keep_temp:
            command.append("--keep-temp")
        case.set(evidence=str(evidence_path))
        result = record(
            case,
            vlib.run(command, cwd=repo, timeout=E2E_TIMEOUT, check=False, log_path=log_path),
            log_path,
        )
        if result["returncode"] != 0:
            case.fail("e2e script exited {}".format(result["returncode"]))
            return
        if evidence_path.is_file():
            try:
                inner = json.loads(evidence_path.read_text(encoding="utf-8"))
                case.set(e2e_status=inner.get("status"), e2e_scenarios=len(inner.get("e2e") or []))
            except ValueError as err:
                case.step("e2e evidence is not valid JSON", error=str(err))


def run_docker(evidence, repo, workdir, enabled):
    with evidence.case("docker", "docker image build and smoke run") as case:
        if not enabled:
            case.skip("docker gate runs only with --docker")
            return
        docker_bin = shutil.which("docker")
        case.set(docker=docker_bin)
        if not docker_bin:
            case.blocked("docker CLI not found on PATH")
            return
        info = vlib.run([docker_bin, "info"], cwd=repo, timeout=120, check=False)
        if info["returncode"] != 0:
            case.blocked(
                "docker daemon is not available: {}".format(info["stderr"].strip()[:500])
            )
            return
        image = docker_image_name(repo)
        case.set(image=image)
        build_log = workdir / "docker-build.log"
        build = record(
            case,
            vlib.run(
                ["make", "docker"], cwd=repo, timeout=DOCKER_BUILD_TIMEOUT, check=False, log_path=build_log
            ),
            build_log,
        )
        if build["returncode"] != 0:
            case.fail("make docker exited {}".format(build["returncode"]))
            return
        case.step("make docker finished", returncode=build["returncode"], log=str(build_log))
        smoke_log = workdir / "docker-run.log"
        smoke = record(
            case,
            vlib.run(
                [docker_bin, "run", "--rm", image, "codis-server", "--version"],
                cwd=repo,
                timeout=DOCKER_RUN_TIMEOUT,
                check=False,
                log_path=smoke_log,
            ),
            smoke_log,
        )
        case.set(smoke_command="docker run --rm {} codis-server --version".format(image))
        if smoke["returncode"] != 0:
            case.fail("docker run exited {}".format(smoke["returncode"]))


def docker_image_name(repo):
    text = vlib.read_text(repo / "Makefile")
    match = re.search(r"docker build[^\n]*?-t\s+([^\s]+)", text)
    return match.group(1) if match else "codis-image"


def main(argv=None):
    args = build_parser().parse_args(argv)
    repo = vlib.find_repo(args.repo)
    bin_dir = repo / "bin"
    run_id = vlib.new_run_id()
    workdir = (
        Path(args.workdir).expanduser().resolve()
        if args.workdir
        else repo / ".runtime" / "realmachine" / "redis8-gate-{}".format(run_id)
    )
    output = (
        Path(args.output).expanduser().resolve()
        if args.output
        else repo / ".runtime" / "realmachine" / "redis8-gate-{}.json".format(run_id)
    )

    evidence = vlib.Evidence(
        script="scripts/validation/redis8/run_functional_gate.py",
        feature=REPO_FEATURE,
        scope=SCOPE,
    )
    evidence.attach_repo(repo)
    bin_version = vlib.read_text(bin_dir / "version").strip().replace("\n", "; ")
    evidence.note("bin/version: {}".format(bin_version or "<missing>"))
    evidence.note("log directory: {}".format(workdir))

    def run_all():
        workdir.mkdir(parents=True, exist_ok=True)
        selected = resolve_groups(args.only)
        evidence.note("selected groups: {}".format(", ".join(sorted(selected))))
        vlib.log("selected groups: {}".format(", ".join(sorted(selected))))

        if "build" in selected:
            with evidence.case("build-all", "make build-all") as case:
                log_path = workdir / "build-all.log"
                if args.skip_build:
                    case.set(command="make build-all", log=str(log_path))
                    case.skip("--skip-build")
                else:
                    result = record(
                        case,
                        vlib.run(
                            ["make", "build-all"],
                            cwd=repo,
                            timeout=BUILD_TIMEOUT,
                            check=False,
                            log_path=log_path,
                        ),
                        log_path,
                    )
                    if result["returncode"] != 0:
                        case.fail(
                            "make build-all exited {}: {}".format(
                                result["returncode"], result["stderr"].strip()[:1000]
                            )
                        )

        if "gotest" in selected:
            with evidence.case("gotest", "make gotest") as case:
                log_path = workdir / "gotest.log"
                result = record(
                    case,
                    vlib.run(
                        ["make", "gotest"],
                        cwd=repo,
                        timeout=GOTEST_TIMEOUT,
                        check=False,
                        log_path=log_path,
                    ),
                    log_path,
                )
                if result["returncode"] != 0:
                    case.fail(
                        "make gotest exited {}: {}".format(
                            result["returncode"], result["stderr"].strip()[:1000]
                        )
                    )

        if "tcl" in selected:
            run_tcl(evidence, repo, workdir / "tcl-codis.log")

        if "e2e" in selected:
            run_e2e(evidence, repo, workdir, args.keep_temp)

        if "docker" in selected:
            run_docker(evidence, repo, workdir, args.docker)

        return evidence.exit_code()

    return vlib.main_guard(evidence, run_all, output)


if __name__ == "__main__":
    sys.exit(main())
