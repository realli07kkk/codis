#!/usr/bin/env python3
"""Collect the environment fields required by doc/redis8_validation_zh.md.

Writes one JSON document with the eight fixed top-level sections:
``repo``, ``os``, ``hardware``, ``toolchain``, ``backend_binary``,
``runtime_config``, ``test_resources``, ``cleanup``.

Always exits 0: individual probes degrade to ``"unsupported-on-<platform>"``
or an ``error`` field instead of aborting the collection.
"""

import argparse
import json
import os
import platform
import shutil
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import vlib

MAIN_BINARIES = [
    "codis-admin",
    "codis-dashboard",
    "codis-fe",
    "codis-proxy",
    "codis-server",
    "redis-check-aof",
]


def _first_line(text):
    for line in str(text or "").splitlines():
        if line.strip():
            return line.strip()
    return ""


def _cmd(args, cwd=None, timeout=30):
    result = vlib.run(args, cwd=cwd, timeout=timeout, check=False)
    return result["stdout"].strip(), result


def _unsupported(prefix="unsupported", platform_name=None):
    return "{}-on-{}".format(prefix, platform_name or sys.platform)


def _read_os_release():
    values = {}
    try:
        text = Path("/etc/os-release").read_text(encoding="utf-8", errors="replace")
    except OSError:
        return values
    for line in text.splitlines():
        if "=" not in line:
            continue
        key, _, value = line.partition("=")
        values[key.strip()] = value.strip().strip('"').strip("'")
    return values


# ---------------------------------------------------------------------------
# os
# ---------------------------------------------------------------------------


def collect_os():
    is_linux = sys.platform.startswith("linux")
    info = {
        "is_linux": is_linux,
        "sys_platform": sys.platform,
        "platform": platform.platform(),
        "machine": platform.machine(),
        "python": sys.version.split()[0],
        "kernel": _first_line(_cmd(["uname", "-r"])[0]) if shutil.which("uname") else None,
    }
    if is_linux:
        release = _read_os_release()
        info["pretty_name"] = release.get("PRETTY_NAME", _unsupported("unknown", "linux"))
        info["id"] = release.get("ID")
        info["version_id"] = release.get("VERSION_ID")
        info["kernel_name"] = _first_line(_cmd(["uname", "-s"])[0])
        info["architecture"] = _first_line(_cmd(["uname", "-m"])[0])
    else:
        version = _first_line(_cmd(["sw_vers", "-productVersion"])[0])
        info["pretty_name"] = "macOS {}".format(version) if version else platform.platform()
        info["product_version"] = version or None
        info["build_version"] = _first_line(_cmd(["sw_vers", "-buildVersion"])[0]) or None
    return info


# ---------------------------------------------------------------------------
# hardware
# ---------------------------------------------------------------------------


def _df(path):
    """Return filesystem facts for the filesystem holding ``path``."""
    path = str(path)
    result = {
        "path": path,
        "device": None,
        "fstype": None,
        "size": None,
        "available": None,
        "mount": None,
        "source": None,
    }
    args = ["df", "-P"]
    if sys.platform.startswith("linux"):
        args = ["df", "-PT"]
    text, _res = _cmd(args + [path])
    if not text:
        return result
    result["source"] = " ".join(args)
    lines = [line for line in text.splitlines() if line.strip()]
    if len(lines) < 2:
        return result
    fields = lines[-1].split()
    if len(fields) >= 7:
        result["device"] = fields[0]
        result["fstype"] = fields[1]
        result["size"] = fields[2]
        result["available"] = fields[4]
        result["mount"] = " ".join(fields[6:])
    elif len(fields) >= 6:
        result["device"] = fields[0]
        result["size"] = fields[1]
        result["available"] = fields[3]
        result["mount"] = " ".join(fields[5:])
    if not result["fstype"] and result["mount"]:
        result["fstype"] = _fstype_from_mount(result["mount"])
    return result


def _fstype_from_mount(mount_point):
    text, _res = _cmd(["mount"])
    needle = " on {} (".format(mount_point)
    for line in text.splitlines():
        index = line.find(needle)
        if index < 0:
            continue
        rest = line[index + len(needle) :]
        return rest.split(",")[0].split(")")[0].strip() or None
    return None


def collect_hardware(workdir):
    is_linux = sys.platform.startswith("linux")
    info = {}
    if is_linux:
        cpu_model = None
        try:
            for line in Path("/proc/cpuinfo").read_text(errors="replace").splitlines():
                if line.lower().startswith("model name"):
                    cpu_model = line.split(":", 1)[1].strip()
                    break
        except OSError:
            cpu_model = None
        info["cpu_model"] = cpu_model or _unsupported("unknown", "linux")
        info["cpu_cores"] = _first_line(_cmd(["nproc"])[0]) or os.cpu_count()
        memory_kb = None
        try:
            for line in Path("/proc/meminfo").read_text(errors="replace").splitlines():
                if line.startswith("MemTotal:"):
                    memory_kb = int(line.split()[1])
                    break
        except (OSError, ValueError, IndexError):
            memory_kb = None
        info["memory_bytes"] = memory_kb * 1024 if memory_kb else None
    else:
        info["cpu_model"] = (
            _first_line(_cmd(["sysctl", "-n", "machdep.cpu.brand_string"])[0]) or None
        )
        cores = _first_line(_cmd(["sysctl", "-n", "hw.ncpu"])[0])
        info["cpu_cores"] = int(cores) if cores.isdigit() else os.cpu_count()
        memory = _first_line(_cmd(["sysctl", "-n", "hw.memsize"])[0])
        info["memory_bytes"] = int(memory) if memory.isdigit() else None
    if info.get("memory_bytes"):
        info["memory_human"] = "{:.1f} GiB".format(info["memory_bytes"] / float(1024 ** 3))
    else:
        info["memory_human"] = None
    info["workdir_filesystem"] = _df(workdir)
    info["cpu_count_python"] = os.cpu_count()
    return info


# ---------------------------------------------------------------------------
# toolchain / binaries
# ---------------------------------------------------------------------------


def collect_toolchain(repo, bin_dir):
    versions = {
        "go": _first_line(_cmd(["go", "version"])[0])
        if shutil.which("go")
        else "not-installed",
        "gcc": _first_line(_cmd(["gcc", "--version"])[0])
        if shutil.which("gcc")
        else "not-installed",
        "make": _first_line(_cmd(["make", "--version"])[0])
        if shutil.which("make")
        else "not-installed",
        "python3": _first_line(_cmd(["python3", "--version"])[0])
        if shutil.which("python3")
        else "not-installed",
    }
    go_env = {}
    if shutil.which("go"):
        for key in ("GOOS", "GOARCH", "GOVERSION", "CGO_ENABLED", "GOFLAGS"):
            value, _res = _cmd(["go", "env", key], cwd=repo)
            go_env[key] = value
    return {
        "versions": versions,
        "go_env": go_env,
        "bin_version": vlib.read_text(bin_dir / "version").strip(),
        "bin_version_ldflags": vlib.read_text(bin_dir / "version.ldflags").strip(),
        "binaries": {
            name: {
                "path": str(bin_dir / name),
                "exists": (bin_dir / name).is_file(),
                "sha256": vlib.sha256_file(bin_dir / name),
            }
            for name in MAIN_BINARIES
        },
    }


def collect_backend_binary(repo, bin_dir):
    path = bin_dir / "codis-server"
    result = vlib.run([path, "--version"], cwd=repo, timeout=30, check=False)
    return {
        "path": str(path),
        "exists": path.is_file(),
        "sha256": vlib.sha256_file(path),
        "version": result["stdout"].strip(),
        "stderr": result["stderr"].strip(),
        "returncode": result["returncode"],
        "codis_server_redis8": str(bin_dir / "codis-server-redis8"),
    }


def collect_runtime_config(repo, bin_dir, cluster_path):
    versions = {}
    for name in ("codis-dashboard", "codis-proxy"):
        binary = bin_dir / name
        if not binary.is_file():
            versions[name] = {"error": "missing binary"}
            continue
        result = vlib.run([binary, "--version"], cwd=repo, timeout=30, check=False)
        versions[name] = {
            "returncode": result["returncode"],
            "stdout": result["stdout"].strip(),
            "stderr": result["stderr"].strip(),
        }
    info = {
        "versions": versions,
        "cluster": None,
        "conf_files": [],
    }
    if not cluster_path:
        return info
    cluster = vlib.load_cluster(cluster_path)
    conf_files = []
    dashboard = cluster.get("dashboard") or {}
    if dashboard.get("conf"):
        conf_files.append({"role": "dashboard", "path": dashboard["conf"]})
    for proxy in cluster.get("proxies") or []:
        if proxy.get("conf"):
            conf_files.append({"role": "codis-proxy-{}".format(proxy.get("index")), "path": proxy["conf"]})
    for group in cluster.get("groups") or []:
        for server in group.get("servers") or []:
            if server.get("conf"):
                conf_files.append(
                    {
                        "role": "group-{}-{}".format(group.get("gid"), server.get("role")),
                        "path": server["conf"],
                    }
                )
    for entry in conf_files:
        entry["sha256"] = vlib.sha256_file(entry["path"])
    info["cluster"] = {
        "path": str(Path(cluster_path).resolve()),
        "run_id": cluster.get("run_id"),
        "product": cluster.get("product"),
        "status": cluster.get("status"),
        "workdir": cluster.get("workdir"),
        "features": cluster.get("features"),
        "dashboard_addr": dashboard.get("addr"),
        "proxy_addrs": [proxy.get("addr") for proxy in cluster.get("proxies") or []],
        "fe_url": (cluster.get("fe") or {}).get("url"),
    }
    info["conf_files"] = conf_files
    return info


def collect_test_resources(cluster_path):
    info = {"cluster": str(Path(cluster_path).resolve()) if cluster_path else None, "ports": [], "directories": {}}
    if not cluster_path:
        return info
    cluster = vlib.load_cluster(cluster_path)
    ports = []
    dashboard = cluster.get("dashboard") or {}
    if dashboard.get("port"):
        ports.append({"role": "dashboard-admin", "port": dashboard["port"]})
    fe = cluster.get("fe") or {}
    if fe.get("addr"):
        ports.append({"role": "codis-fe", "port": int(fe["addr"].rsplit(":", 1)[1])})
    for proxy in cluster.get("proxies") or []:
        ports.append({"role": "proxy-{}-admin".format(proxy.get("index")), "port": proxy.get("admin_port")})
        ports.append({"role": "proxy-{}".format(proxy.get("index")), "port": proxy.get("port")})
    for group in cluster.get("groups") or []:
        for server in group.get("servers") or []:
            ports.append(
                {
                    "role": "group-{}-{}".format(group.get("gid"), server.get("role")),
                    "port": server.get("port"),
                }
            )
    info["ports"] = ports
    workdir = cluster.get("workdir")
    info["directories"] = {
        "workdir": workdir,
        "coordinator": (cluster.get("coordinator") or {}).get("root"),
        "logs": str(Path(workdir) / "logs") if workdir else None,
        "conf": str(Path(workdir) / "conf") if workdir else None,
        "data": sorted(
            {
                server.get("dir")
                for group in cluster.get("groups") or []
                for server in group.get("servers") or []
                if server.get("dir")
            }
        ),
    }
    return info


# ---------------------------------------------------------------------------
# linux-only extras
# ---------------------------------------------------------------------------


def collect_linux_extras(repo):
    if not sys.platform.startswith("linux"):
        marker = _unsupported()
        return {
            "vm_overcommit_memory": marker,
            "transparent_hugepage_enabled": marker,
            "transparent_hugepage_defrag": marker,
            "jemalloc_generated_header": marker,
        }
    extras = {
        "vm_overcommit_memory": vlib.read_text("/proc/sys/vm/overcommit_memory").strip() or None,
        "transparent_hugepage_enabled": vlib.read_text(
            "/sys/kernel/mm/transparent_hugepage/enabled"
        ).strip()
        or None,
        "transparent_hugepage_defrag": vlib.read_text(
            "/sys/kernel/mm/transparent_hugepage/defrag"
        ).strip()
        or None,
    }
    headers = sorted(
        str(path.relative_to(repo))
        for path in (Path(repo) / "third_party" / "jemalloc-go").glob("**/jemalloc.h")
    )
    extras["jemalloc_generated_header"] = {
        "present": bool(headers),
        "paths": headers[:5],
    }
    return extras


# ---------------------------------------------------------------------------
# main
# ---------------------------------------------------------------------------


def build_parser():
    parser = argparse.ArgumentParser(
        description="Collect the real-machine environment fields into env.json."
    )
    parser.add_argument("--repo", help="repository root (default: auto-detected)")
    parser.add_argument(
        "--output", help="output path (default: <repo>/.runtime/realmachine/env-<run-id>.json)"
    )
    parser.add_argument("--cluster", help="optional cluster.json to describe")
    return parser


def main(argv=None):
    args = build_parser().parse_args(argv)
    repo = vlib.find_repo(args.repo)
    bin_dir = repo / "bin"
    run_id = vlib.new_run_id()
    output = (
        Path(args.output).expanduser().resolve()
        if args.output
        else repo / ".runtime" / "realmachine" / "env-{}.json".format(run_id)
    )
    cluster_path = str(Path(args.cluster).expanduser().resolve()) if args.cluster else None

    env = {
        "run_id": run_id,
        "generated_at": vlib.utc_now(),
        "repo": {
            "root": str(repo),
            "remote": _first_line(_cmd(["git", "remote", "-v"], cwd=repo)[0]) or None,
            "bin_dir": str(bin_dir),
        },
        "os": None,
        "hardware": None,
        "toolchain": None,
        "backend_binary": None,
        "runtime_config": None,
        "test_resources": None,
        "cleanup": None,
    }

    collectors = [
        ("repo.head", lambda: env["repo"].update(vlib.git_head(repo))),
        ("os", lambda: env.__setitem__("os", collect_os())),
        ("hardware", lambda: env.__setitem__("hardware", collect_hardware(repo))),
        ("toolchain", lambda: env.__setitem__("toolchain", collect_toolchain(repo, bin_dir))),
        (
            "backend_binary",
            lambda: env.__setitem__("backend_binary", collect_backend_binary(repo, bin_dir)),
        ),
        (
            "runtime_config",
            lambda: env.__setitem__(
                "runtime_config", collect_runtime_config(repo, bin_dir, cluster_path)
            ),
        ),
        (
            "test_resources",
            lambda: env.__setitem__("test_resources", collect_test_resources(cluster_path)),
        ),
    ]
    for name, fn in collectors:
        try:
            fn()
        except Exception as err:  # noqa: BLE001 - collection must always finish
            vlib.log("collector {} failed: {}: {}".format(name, type(err).__name__, err))
            env.setdefault("errors", []).append(
                {"collector": name, "error": "{}: {}".format(type(err).__name__, err)}
            )

    try:
        env["hardware"] = env["hardware"] or {}
        env["hardware"]["linux_extras"] = collect_linux_extras(repo)
    except Exception as err:  # noqa: BLE001
        vlib.log("collector linux_extras failed: {}".format(err))

    vlib.save_cluster(env, output)
    vlib.log("environment written to {}".format(output))
    vlib.log("top-level keys: {}".format(", ".join(sorted(env.keys()))))
    sys.stdout.write(
        json.dumps(
            {
                "output": str(output),
                "top_level_keys": sorted(env.keys()),
                "status": "ok",
            },
            indent=2,
            sort_keys=True,
        )
        + "\n"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
