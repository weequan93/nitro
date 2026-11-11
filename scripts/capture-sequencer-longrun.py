#!/usr/bin/env python3
"""Run bounded sequencer resource captures in separate hourly directories.

Only the owned collector child is stopped on cancellation/limits. The node,
its configuration and an existing slow-block watcher are never controlled.
"""

import argparse
from datetime import datetime, timezone
import fcntl
import hashlib
import json
import math
import os
from pathlib import Path
import shutil
import signal
import subprocess
import sys
import threading
import time


GIB = 1024**3
CHECK_INTERVAL = 60
COLLECTOR = Path(__file__).with_name("capture-sequencer-context.py")


def utc():
    return datetime.now(timezone.utc).isoformat()


def active_collectors(proc_root=Path("/proc")):
    found = []
    for path in proc_root.iterdir():
        if not path.name.isdigit():
            continue
        try:
            argv = (path / "cmdline").read_bytes().decode(errors="replace").split("\0")
        except OSError:
            continue
        program = next((a for a in argv[1:] if a.endswith(".py")), "")
        if Path(program).name == "capture-sequencer-context.py":
            found.append(int(path.name))
    return found


def output_bytes(root):
    return sum(p.lstat().st_size for p in root.rglob("*") if p.is_file() and not p.is_symlink())


def budget_reason(root, maximum, minimum_free):
    if output_bytes(root) >= maximum:
        return "output_size_limit"
    if shutil.disk_usage(root).free < minimum_free:
        return "free_space_limit"
    return None


def stop_child(child):
    if child is None or child.poll() is not None:
        return False
    child.terminate()
    try:
        child.wait(timeout=15)
        return False
    except subprocess.TimeoutExpired:
        child.kill()
        child.wait(timeout=5)
        return True


def run(args):
    os.umask(0o077)
    root = args.out_dir.resolve()
    root.mkdir(parents=True, exist_ok=True)
    with args.collector.open("rb") as stream:
        source = stream.read(2 * 1024**2 + 1)
    if len(source) > 2 * 1024**2:
        raise ValueError("Collector source exceeds 2 MiB")
    if (root / "run.json").exists():
        raise ValueError("Output directory already contains a run; use a fresh directory")
    args.lock_file.parent.mkdir(parents=True, exist_ok=True)
    with args.lock_file.open("a") as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise ValueError("Another long capture holds the lock") from None
        existing = active_collectors()
        if existing:
            raise ValueError("Resource collector already running: " + ",".join(map(str, existing)))
        if args.events_file and not args.events_file.is_file():
            raise ValueError("Existing watcher events file is unavailable")
        reason = budget_reason(root, args.max_output_gib * GIB, args.min_free_gib * GIB)
        if reason:
            raise ValueError("Capture budget unavailable: " + reason)
        tools_dir = root / "tools"
        tools_dir.mkdir(exist_ok=False)
        pinned = tools_dir / "capture-sequencer-context.py"
        pinned.write_bytes(source)
        metadata = {
            "schema": 1, "parent_pid": os.getpid(), "started_utc": utc(),
            "hours": args.hours, "segment_minutes": args.segment_minutes,
            "interval_s": args.interval, "context_interval_s": args.context_interval,
            "container": args.container, "collector_sha256": hashlib.sha256(source).hexdigest(),
            "events_file": str(args.events_file.resolve()) if args.events_file else None,
            "max_output_gib": args.max_output_gib, "min_free_gib": args.min_free_gib,
            "note": "Hourly boundaries may have gaps. A completed duration does not prove complete RPC or sample coverage.",
        }
        with (root / "run.json").open("x") as output:
            json.dump(metadata, output, indent=2)
            output.write("\n")
        stop = threading.Event()
        previous_handlers = {}
        def signal_stop(_signal, _frame):
            stop.set()
        for sig in (signal.SIGTERM, signal.SIGINT):
            previous_handlers[sig] = signal.signal(sig, signal_stop)
        started = time.monotonic()
        deadline = started + args.hours * 3600
        count = completed = 0
        child = None
        reason = "duration_elapsed"
        exit_code = 0
        def emit(stream, event):
            event = {"time_utc": utc(), "parent_pid": os.getpid(), **event}
            stream.write(json.dumps(event, separators=(",", ":")) + "\n")
            stream.flush()
            temporary = root / ".status.tmp"
            temporary.write_text(json.dumps(event, indent=2) + "\n")
            temporary.replace(root / "status.json")
            print(json.dumps(event, separators=(",", ":")), flush=True)
        try:
            with (root / "segments.jsonl").open("x") as journal:
                while time.monotonic() < deadline and not stop.is_set():
                    remaining = deadline - time.monotonic()
                    if remaining < 0.5:
                        stop.wait(remaining)
                        break
                    existing = active_collectors()
                    if existing:
                        reason, exit_code = "another_resource_collector", 1
                        break
                    reason = budget_reason(root, args.max_output_gib * GIB, args.min_free_gib * GIB)
                    if reason:
                        exit_code = 1
                        break
                    count += 1
                    segment = root / ("segment-%04d" % count)
                    segment.mkdir()
                    seconds = min(args.segment_minutes * 60, remaining)
                    command = [sys.executable, "-B", "-u", str(pinned), "--minutes", str(seconds / 60),
                               "--interval", str(args.interval), "--context-interval", str(args.context_interval),
                               "--container", args.container, "--rpc-url", args.rpc_url,
                               "--metrics-url", args.metrics_url, "--out-dir", str(segment)]
                    segment_start = time.monotonic()
                    reason = None
                    with (segment / "stdout.log").open("x") as output:
                        child = subprocess.Popen(command, stdin=subprocess.DEVNULL, stdout=output,
                                                 stderr=subprocess.STDOUT, start_new_session=True)
                        emit(journal, {"kind": "segment_started", "segment": count, "child_pid": child.pid,
                                       "segment_dir": str(segment), "planned_seconds": round(seconds, 3),
                                       "remaining_s": max(0, int(deadline - time.monotonic()))})
                        heartbeat_at = budget_at = time.monotonic() + CHECK_INTERVAL
                        while child.poll() is None:
                            current = time.monotonic()
                            if stop.is_set():
                                reason = "signal"
                                break
                            if current >= deadline:
                                reason = "duration_elapsed"
                                break
                            if current > segment_start + seconds + 20:
                                reason, exit_code = "segment_timeout", 1
                                break
                            if current >= budget_at:
                                reason = budget_reason(root, args.max_output_gib * GIB, args.min_free_gib * GIB)
                                budget_at = current + CHECK_INTERVAL
                                if reason:
                                    exit_code = 1
                                    break
                            if current >= heartbeat_at:
                                emit(journal, {"kind": "heartbeat", "segment": count, "child_pid": child.pid,
                                               "segment_dir": str(segment), "remaining_s": max(0, int(deadline - current))})
                                heartbeat_at = current + CHECK_INTERVAL
                            stop.wait(0.5)
                        forced = stop_child(child)
                        code = child.wait()
                        if reason == "duration_elapsed" and (forced or code != 0):
                            reason, exit_code = "collector_failed_on_shutdown", 1
                    elapsed = time.monotonic() - segment_start
                    normal = code == 0 and elapsed + min(2, seconds * 0.05) >= seconds
                    if reason is None and not normal:
                        reason, exit_code = "segment_failed_or_ended_early", 1
                    if normal:
                        completed += 1
                    emit(journal, {"kind": "segment_finished", "segment": count, "child_returncode": code,
                                   "elapsed_s": round(elapsed, 3), "full_duration_observed": normal,
                                   "forced_child_stop": forced, "stop_reason": reason})
                    child = None
                    if reason:
                        break
                if stop.is_set():
                    reason = "signal"
                elif time.monotonic() >= deadline and exit_code == 0:
                    reason = "duration_elapsed"
                emit(journal, {"kind": "complete" if reason == "duration_elapsed" else "stopped",
                               "reason": reason, "segments_started": count, "segments_completed": completed,
                               "elapsed_s": round(time.monotonic() - started, 3)})
        finally:
            stop_child(child)
            for sig, previous in previous_handlers.items():
                signal.signal(sig, previous)
        return exit_code


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--out-dir", type=Path, required=True)
    parser.add_argument("--hours", type=float, default=24)
    parser.add_argument("--segment-minutes", type=float, default=60)
    parser.add_argument("--interval", type=float, default=1)
    parser.add_argument("--context-interval", type=float, default=15)
    parser.add_argument("--collector", type=Path, default=COLLECTOR)
    parser.add_argument("--events-file", type=Path)
    parser.add_argument("--container", default="main-nitro-1")
    parser.add_argument("--rpc-url", default="http://127.0.0.1:1100")
    parser.add_argument("--metrics-url", default="http://127.0.0.1:6070/debug/metrics/prometheus")
    parser.add_argument("--lock-file", type=Path, default=Path("/run/lock/nitro-sequencer-capture.lock"))
    parser.add_argument("--max-output-gib", type=float, default=2)
    parser.add_argument("--min-free-gib", type=float, default=2)
    args = parser.parse_args()
    numeric = (args.hours, args.segment_minutes, args.interval, args.context_interval, args.max_output_gib, args.min_free_gib)
    if not all(math.isfinite(v) for v in numeric) or not (
            0 < args.hours <= 168 and 1 <= args.segment_minutes <= 360 and args.interval >= 1 and
            args.context_interval >= 10 and args.max_output_gib > 0 and args.min_free_gib >= 0):
        parser.error("Require hours 0..168, segment minutes 1..360, intervals >=1/10s and valid disk budgets")
    if not Path("/proc/stat").exists():
        parser.error("Run on the Linux node host")
    return run(args)


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (OSError, ValueError, subprocess.SubprocessError) as error:
        reason = type(error).__name__ if isinstance(error, subprocess.SubprocessError) else str(error)
        print("Long capture stopped: " + reason, file=sys.stderr)
        raise SystemExit(1)
