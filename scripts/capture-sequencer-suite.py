#!/usr/bin/env python3
"""Supervise resources, hourly cache/IO samples and a slow-event watcher.

Run on the Linux host after the requested tuning is loaded. This script never
changes node settings or controls its service. --replace-existing stops only
recognized monitoring processes for main-nitro-1 and retains all old files.
"""

import argparse
from datetime import datetime, timezone
import fcntl
import hashlib
import importlib.util
import json
import math
import os
from pathlib import Path
import re
import signal
import subprocess
import sys
import threading
import time
import urllib.request


DIRECTORY = Path(__file__).resolve().parent
SOURCES = (
    "capture-sequencer-suite.py", "capture-sequencer-longrun.py",
    "capture-sequencer-context.py", "capture-main-cache-io.py",
    "watch-slow-sequencer-blocks.py", "report-sequencer-context.py",
    "check-main-runtime.py", "trial-compaction-pacing.py",
    "capture-rpc-diagnostics.py",
)
MONITORS = set(SOURCES[:5]) | {"capture-txpublish-diagnostics.py", "capture-rpc-diagnostics.py"}
CONTAINER = "main-nitro-1"
GIB = 1024**3


def utc():
    return datetime.now(timezone.utc).isoformat()


def module(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    result = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(result)
    return result


def save(path, value):
    temporary = path.with_name("." + path.name + ".tmp")
    temporary.write_text(json.dumps(value, indent=2) + "\n")
    temporary.replace(path)


def process_info(pid, proc_root=Path("/proc")):
    try:
        root = proc_root / str(pid)
        argv = (root / "cmdline").read_bytes().decode(errors="replace").rstrip("\0").split("\0")
        fields = (root / "stat").read_text().rsplit(") ", 1)[1].split()
        if not argv or not argv[0]:
            return None
        return {"pid": int(pid), "ppid": int(fields[1]), "start_ticks": int(fields[19]), "argv": argv}
    except (OSError, ValueError, IndexError):
        return None


def inventory(proc_root=Path("/proc")):
    result = []
    for path in proc_root.iterdir():
        if path.name.isdigit() and int(path.name) != os.getpid():
            row = process_info(path.name, proc_root)
            if row:
                result.append(row)
    return result


def argument(argv, key, default=None):
    values = []
    for i, item in enumerate(argv):
        if item == key and i + 1 < len(argv):
            values.append(argv[i + 1])
        elif item.startswith(key + "="):
            values.append(item.split("=", 1)[1])
    if len(values) > 1:
        raise ValueError("Duplicate monitoring argument: " + key)
    return values[0] if values else default


def monitor(row):
    argv = row["argv"]
    if not Path(argv[0]).name.startswith("python"):
        return None
    program = next((a for a in argv[1:] if a.endswith(".py")), "")
    name = Path(program).name
    if name not in MONITORS or argument(argv, "--container", CONTAINER) != CONTAINER:
        return None
    out = argument(argv, "--out-dir")
    if not out and name == "capture-rpc-diagnostics.py":
        owner_dir = argument(argv, "--suite-dir")
        out = str(Path(owner_dir) / "rpc-counters") if owner_dir else None
    if not out or not Path(out).is_absolute():
        raise ValueError("Recognized monitor has no absolute output directory")
    return {"pid": row["pid"], "start_ticks": row["start_ticks"], "name": name, "out_dir": out}


def send(row, sig):
    current = process_info(row["pid"])
    if current and current["start_ticks"] == row["start_ticks"]:
        try:
            os.kill(row["pid"], sig)
            return True
        except ProcessLookupError:
            pass
    return False


def takeover(replace):
    rows = inventory()
    old = [item for row in rows if (item := monitor(row))]
    if any(r["name"] == "capture-sequencer-suite.py" for r in old):
        raise ValueError("Another suite is running; its children were not stopped")
    if old and not replace:
        raise ValueError("Monitoring already running; --replace-existing is required for a fresh phase")
    # Terminating docker logs clients lets legacy watcher readline/wait calls
    # finish after SIGTERM. Only their verified direct children are selected.
    watchers = {r["pid"] for r in old if r["name"] == "watch-slow-sequencer-blocks.py"}
    clients = [r for r in rows if r["ppid"] in watchers
               and Path(r["argv"][0]).name == "docker"
               and r["argv"][1:2] == ["logs"] and "--follow" in r["argv"]
               and r["argv"][-1] == CONTAINER]
    for row in sorted(old, key=lambda r: r["name"] != "capture-sequencer-longrun.py"):
        send(row, signal.SIGTERM)
    for row in clients:
        send(row, signal.SIGTERM)
    deadline = time.monotonic() + 25
    pending = old + clients
    while pending and time.monotonic() < deadline:
        pending = [r for r in pending if (current := process_info(r["pid"]))
                   and current["start_ticks"] == r["start_ticks"]]
        if pending:
            time.sleep(0.2)
    for row in pending:
        send(row, signal.SIGKILL)
    deadline = time.monotonic() + 5
    while time.monotonic() < deadline:
        remaining = [item for row in inventory() if (item := monitor(row))]
        if not remaining:
            return old
        time.sleep(0.2)
    raise ValueError("Previous monitoring has not stopped; no new collectors started")


def pin(root, source=DIRECTORY):
    data = {}
    for name in SOURCES:
        content = (source / name).read_bytes()
        if len(content) > 2 * 1024**2:
            raise ValueError("Monitoring source exceeds 2 MiB")
        compile(content, name, "exec")
        data[name] = content
    tools = root / "tools"
    tools.mkdir()
    for name, content in data.items():
        (tools / name).write_bytes(content)
    return {name: hashlib.sha256(content).hexdigest() for name, content in data.items()}


def fetch(url, body=None):
    request = urllib.request.Request(url, data=body,
        headers={"Content-Type": "application/json"} if body else {})
    with urllib.request.urlopen(request, timeout=5) as response:
        data = response.read(2 * 1024**2 + 1)
    if len(data) > 2 * 1024**2:
        raise ValueError("Health response exceeds 2 MiB")
    return data


def ready(args):
    deadline = time.monotonic() + args.ready_timeout
    heads = []
    rpc_counters = (module("suite_rpc_readiness", DIRECTORY / "capture-rpc-diagnostics.py")
                    if getattr(args, "rpc_counters", False) else None)
    while True:
        try:
            payload = json.loads(fetch(args.rpc_url, json.dumps(
                {"jsonrpc": "2.0", "id": 1, "method": "eth_blockNumber", "params": []}).encode()))
            if payload.get("error") or not isinstance(payload.get("result"), str):
                raise ValueError("RPC head unavailable")
            head = int(payload["result"], 16)
            if head <= 0:
                raise ValueError("RPC head is zero")
            metrics = fetch(args.metrics_url).decode()
            if not any(line.startswith("l2chaindata_compact_debt ") for line in metrics.splitlines()):
                raise ValueError("Database metrics unavailable")
            if rpc_counters:
                values = rpc_counters.parse(metrics)
                if any(k not in values for k in rpc_counters.TRACKED_KEYS):
                    raise ValueError("Subsecond RPC counters unavailable")
            heads.append(head)
            if len(heads) >= 2:
                if heads[-1] < heads[-2]:
                    raise ValueError("RPC head went backwards")
                return {"time_utc": utc(), "heads": heads[-2:],
                        "head_advanced": heads[-1] > heads[-2]}
        except (OSError, ValueError):
            heads.clear()
            if time.monotonic() >= deadline:
                raise ValueError("RPC/metrics not ready within the startup timeout") from None
        if time.monotonic() >= deadline:
            raise ValueError("Readiness timeout")
        time.sleep(min(2, max(0, deadline - time.monotonic())))


def runtime_check(checker, args, baseline=None):
    current = checker.observe()
    failures = checker.compare(current, baseline=baseline, pacing=args.pacing_bytes_per_second,
                               compactions=args.compactions)
    if failures:
        raise ValueError("; ".join(failures))
    return current


def validate_device(name, sys_root=Path("/sys/class/block")):
    if not re.fullmatch(r"[A-Za-z0-9_-]+", name):
        raise ValueError("Invalid device name")
    device = sys_root / name
    if not (device / "dev").is_file() or (device / "partition").exists():
        raise ValueError("Capture device must be an available whole disk")
    return (device / "dev").read_text().strip()


def spawn(command, log):
    with log.open("x") as output:
        return subprocess.Popen(command, stdin=subprocess.DEVNULL, stdout=output,
                                stderr=subprocess.STDOUT, start_new_session=True)


def shutdown(children, root, natural=False):
    live = [p for p in children.values() if p is not None]
    terminated = set()
    if natural:
        deadline = time.monotonic() + 20
        while time.monotonic() < deadline and any(
                children.get(k) is not None and children[k].poll() is None for k in ("resources", "cache", "rpc")):
            time.sleep(0.2)
    for name, child in children.items():
        if child is None:
            continue
        if child.poll() is None:
            try:
                os.killpg(child.pid, signal.SIGTERM)
                terminated.add(name)
            except ProcessLookupError:
                pass
    deadline = time.monotonic() + 25
    while time.monotonic() < deadline and any(p.poll() is None for p in live):
        time.sleep(0.2)
    for child in live:
        if child.poll() is None:
            try:
                os.killpg(child.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
        child.wait(timeout=5)
    # The resource longrun creates a separate session for its sampler child.
    # Recover that child if its parent was killed before it could clean up.
    for row in inventory():
        item = monitor(row)
        if item:
            try:
                Path(item["out_dir"]).resolve().relative_to(root)
            except ValueError:
                continue
            send(item, signal.SIGTERM)
            deadline = time.monotonic() + 5
            while time.monotonic() < deadline:
                current = process_info(item["pid"])
                if not current or current["start_ticks"] != item["start_ticks"]:
                    break
                time.sleep(0.2)
            send(item, signal.SIGKILL)
    return terminated


def run(args):
    os.umask(0o077)
    root = args.out_dir.resolve()
    root.mkdir(parents=True, exist_ok=True)
    if any((root / name).exists() for name in ("run.json", "tools", "status.json")):
        raise ValueError("Output already contains a run; use a new directory")
    args.lock_file.parent.mkdir(parents=True, exist_ok=True)
    support = module("suite_longrun", DIRECTORY / "capture-sequencer-longrun.py")
    with args.lock_file.open("a") as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise ValueError("Another complete monitoring suite is running") from None
        hashes = pin(root)
        tools = root / "tools"
        save(root / "status.json", {"kind": "preflight", "time_utc": utc(), "parent_pid": os.getpid()})
        print(json.dumps({"kind": "preflight", "time_utc": utc(), "out_dir": str(root)}), flush=True)
        try:
            checker = module("suite_runtime", tools / "check-main-runtime.py")
            device_number = validate_device(args.device)
            health = ready(args)
            baseline = runtime_check(checker, args)
            if support.budget_reason(root, args.max_output_gib * GIB, args.min_free_gib * GIB):
                raise ValueError("Capture disk budget unavailable")
            # All source/config/health checks precede stopping the previous phase.
            old = takeover(args.replace_existing)
            save(root / "previous-monitors.json", old)
            baseline = runtime_check(checker, args, baseline)
        except Exception as error:
            save(root / "status.json", {"kind": "stopped", "reason": str(error),
                                       "time_utc": utc(), "parent_pid": os.getpid()})
            raise
        save(root / "runtime-baseline.json", baseline)
        (root / "slow-blocks").mkdir()
        (root / "slow-blocks" / "events.jsonl").touch()
        (root / "cache-io").mkdir()
        started = time.monotonic()
        deadline = started + args.hours * 3600
        save(root / "run.json", {"schema": 1, "started_utc": utc(), "parent_pid": os.getpid(),
            "hours": args.hours, "compactions": args.compactions,
            "pacing_bytes_per_second": args.pacing_bytes_per_second, "device": args.device,
            "device_number": device_number,
            "source_sha256": hashes, "health": health,
            "note": "Samples include background work after startup. Cache misses are not device IOPS."})
        stop = threading.Event()
        handlers = {sig: signal.signal(sig, lambda *_: stop.set())
                    for sig in (signal.SIGTERM, signal.SIGINT)}
        children = {}
        reason, code, count = "duration_elapsed", 0, 0
        cache_started = cache_duration = 0
        try:
            with (root / "journal.jsonl").open("x") as journal:
                def emit(kind, **extra):
                    event = {"kind": kind, "time_utc": utc(), "parent_pid": os.getpid(),
                        "remaining_s": max(0, int(deadline - time.monotonic())),
                        "cache_segment": count,
                        "children": {k: {"pid": p.pid, "returncode": p.poll()} for k, p in children.items()},
                        **extra}
                    journal.write(json.dumps(event) + "\n")
                    journal.flush()
                    save(root / "status.json", event)
                    print(json.dumps(event), flush=True)
                python = [sys.executable, "-B", "-u"]
                children["watcher"] = spawn(python + [str(tools / "watch-slow-sequencer-blocks.py"),
                    "--out-dir", str(root / "slow-blocks"), "--rpc-url", args.rpc_url,
                    "--since", utc()], root / "slow-blocks" / "stdout.log")
                children["resources"] = spawn(python + [str(tools / "capture-sequencer-longrun.py"),
                    "--hours", str((deadline - time.monotonic()) / 3600),
                    "--segment-minutes", str(args.segment_minutes),
                    "--out-dir", str(root / "resources"), "--rpc-url", args.rpc_url,
                    "--metrics-url", args.metrics_url,
                    "--events-file", str(root / "slow-blocks" / "events.jsonl"),
                    "--max-output-gib", str(args.max_output_gib), "--min-free-gib", str(args.min_free_gib)],
                    root / "resources.stdout.log")
                if getattr(args, "rpc_counters", False):
                    (root / "rpc-counters").mkdir()
                    children["rpc"] = spawn(python + [str(tools / "capture-rpc-diagnostics.py"),
                        "--suite-dir", str(root), "--out-dir", str(root / "rpc-counters"),
                        "--metrics-url", args.metrics_url, "--min-free-gib", str(args.min_free_gib)],
                        root / "rpc-counters" / "stdout.log")
                heartbeat = 0
                while not stop.is_set() and time.monotonic() < deadline:
                    current = time.monotonic()
                    for name in ("watcher", "resources") + (("rpc",) if "rpc" in children else ()):
                        if children[name].poll() is not None:
                            if name in ("resources", "rpc") and children[name].returncode == 0 and deadline - current <= 2:
                                continue
                            raise RuntimeError(name + " collector exited; inspect its stdout.log")
                    cache = children.get("cache")
                    if cache is None or cache.poll() is not None:
                        if cache is not None:
                            if cache.returncode != 0 or current - cache_started < cache_duration - 2:
                                raise RuntimeError("Cache collector failed or ended early")
                            emit("cache_segment_finished")
                            runtime_check(checker, args, baseline)
                        if deadline - time.monotonic() < 0.5:
                            break
                        count += 1
                        segment = root / "cache-io" / ("segment-%04d" % count)
                        segment.mkdir()
                        cache_started = time.monotonic()
                        cache_duration = min(args.segment_minutes * 60, deadline - cache_started)
                        children["cache"] = spawn(python + [str(tools / "capture-main-cache-io.py"),
                            "--minutes", str(cache_duration / 60), "--device", args.device,
                            "--metrics-url", args.metrics_url, "--out-dir", str(segment)],
                            segment / "stdout.log")
                        emit("cache_segment_started", segment_dir=str(segment))
                    if current >= heartbeat:
                        limit = support.budget_reason(root, args.max_output_gib * GIB, args.min_free_gib * GIB)
                        if limit:
                            raise RuntimeError("Capture stopped at " + limit)
                        emit("heartbeat")
                        heartbeat = current + 60
                    stop.wait(0.5)
                if stop.is_set():
                    reason = "signal"
        except Exception as error:
            reason, code = type(error).__name__ + ": " + str(error), 1
        finally:
            terminated = shutdown(children, root, natural=reason == "duration_elapsed")
            if reason == "duration_elapsed":
                if any(children.get(k) and (children[k].returncode != 0 or k in terminated)
                       for k in ("resources", "cache", "rpc")):
                    reason, code = "collector_failed_or_late_on_shutdown", 1
                elif "watcher" in children and (
                        "watcher" not in terminated or children["watcher"].returncode not in (0, -signal.SIGTERM)):
                    reason, code = "watcher_failed_or_exited_before_shutdown", 1
            save(root / "status.json", {"kind": "complete" if reason == "duration_elapsed" else "stopped",
                "reason": reason, "time_utc": utc(), "parent_pid": os.getpid(),
                "cache_segments_started": count,
                "children": {k: {"pid": p.pid, "returncode": p.returncode} for k, p in children.items()}})
            print((root / "status.json").read_text(), flush=True)
            for sig, handler in handlers.items():
                signal.signal(sig, handler)
        return code


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--out-dir", type=Path, required=True)
    parser.add_argument("--hours", type=float, default=24)
    parser.add_argument("--segment-minutes", type=float, default=60)
    parser.add_argument("--compactions", type=int, default=1)
    parser.add_argument("--pacing-bytes-per-second", type=int, default=64 * 1024**2)
    parser.add_argument("--device", default="vdb")
    parser.add_argument("--rpc-url", default="http://127.0.0.1:1100")
    parser.add_argument("--metrics-url", default="http://127.0.0.1:6070/debug/metrics/prometheus")
    parser.add_argument("--ready-timeout", type=float, default=300)
    parser.add_argument("--replace-existing", action="store_true")
    parser.add_argument("--rpc-counters", action="store_true",
                        help="Require subsecond metrics and supervise an RPC counter collector until this suite ends")
    parser.add_argument("--lock-file", type=Path, default=Path("/run/lock/nitro-sequencer-suite.lock"))
    parser.add_argument("--max-output-gib", type=float, default=3)
    parser.add_argument("--min-free-gib", type=float, default=2)
    args = parser.parse_args()
    if not all(math.isfinite(v) for v in (args.hours, args.segment_minutes, args.ready_timeout,
                                           args.max_output_gib, args.min_free_gib)) or not (
            0 < args.hours <= 168 and 1 <= args.segment_minutes <= 360 and
            args.ready_timeout >= 5 and args.compactions >= 1 and args.pacing_bytes_per_second >= 0 and
            args.max_output_gib > 0 and args.min_free_gib >= 0):
        parser.error("Invalid duration, tuning expectation or disk budget")
    if not Path("/proc/stat").exists():
        parser.error("Run on the Linux node host")
    return run(args)


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (OSError, ValueError, RuntimeError, subprocess.SubprocessError) as error:
        print("Suite stopped: " + str(error), file=sys.stderr)
        raise SystemExit(1)
