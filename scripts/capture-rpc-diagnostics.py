#!/usr/bin/env python3
"""Save RPC completion/latency counters until an existing suite finishes.

Supplement an already running, pinned suite without changing its tools or
restarting any process. The suite's PID/start time and deadline bound this
collector. RPC success means successful submission, not receipt status.
"""

import argparse
from collections import Counter
from datetime import datetime, timedelta, timezone
import fcntl
import hashlib
import importlib.util
import json
import math
import os
from pathlib import Path
import re
import shutil
import signal
import subprocess
import sys
import threading
import time


METHODS = ("eth_sendRawTransaction", "eth_getLogs")
OUTCOMES = ("success", "failure")
SUFFIXES = ("completed", "ge1s", "ge3s", "ge5s")
KEYS = tuple("rpc_diagnostics_%s_%s_%s" % (method, outcome, suffix)
             for method in METHODS for outcome in OUTCOMES for suffix in SUFFIXES)
DROP = "rpc_diagnostics_details_dropped"
ALL_KEYS = KEYS + (DROP,)


def utc():
    return datetime.now(timezone.utc).isoformat()


def module(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    result = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(result)
    return result


def save(path, obj):
    temporary = path.with_name("." + path.name + ".tmp")
    temporary.write_text(json.dumps(obj, indent=2) + "\n")
    temporary.replace(path)


def parse(text):
    values = {}
    for line in text.splitlines():
        parts = line.split()
        if len(parts) == 2 and parts[0] in ALL_KEYS and re.fullmatch(r"[0-9]+", parts[1]):
            values[parts[0]] = int(parts[1])
    return values


def deltas(current, previous, seconds, interval=15):
    if previous is None:
        return {}, "first_sample"
    if not current.get("identity") or current["identity"] != previous.get("identity"):
        return {}, "identity_missing_or_changed"
    if current.get("errors") or previous.get("errors"):
        return {}, "sample_error"
    if not 0 < seconds <= max(40, interval * 2 + 10):
        return {}, "sample_gap"
    before, after = previous["counters"], current["counters"]
    if any(key not in before or key not in after for key in ALL_KEYS):
        return {}, "missing_counters"
    if any(after[key] < before[key] for key in ALL_KEYS):
        return {}, "counter_reset"
    return {key: after[key] - before[key] for key in ALL_KEYS}, None


def observe(helper, url):
    row = {"time_utc": utc(), "identity": None, "counters": {}, "errors": []}
    identities = []
    for stage in ("before", "after"):
        try:
            identities.append(helper.identity("main-nitro-1"))
        except (OSError, ValueError, IndexError, subprocess.SubprocessError) as error:
            identities.append(None)
            row["errors"].append("identity_" + stage + ":" + type(error).__name__)
        if stage == "before":
            try:
                row["counters"] = parse(helper.base.fetch(url, cap=2 * 1024**2).decode())
            except (OSError, ValueError) as error:
                row["errors"].append("metrics:" + type(error).__name__)
    if identities[0] is not None and identities[0] == identities[1]:
        row["identity"] = identities[0]
    else:
        row["errors"].append("identity_unverified")
    row["missing_counters"] = [key for key in ALL_KEYS if key not in row["counters"]]
    if row["missing_counters"]:
        row["errors"].append("missing_counters")
    row["finished_utc"] = utc()
    return row


def owner(ops, root, metadata):
    row = ops.process_info(metadata["parent_pid"])
    if row is None:
        raise ValueError("Suite parent is not running")
    program = next((a for a in row["argv"][1:] if a.endswith(".py")), "")
    out = ops.argument(row["argv"], "--out-dir")
    if (Path(program).name != "capture-sequencer-suite.py" or not out or
            Path(out).resolve() != root):
        raise ValueError("Suite parent PID no longer belongs to this capture")
    return row


def same_owner(ops, original):
    row = ops.process_info(original["pid"])
    return row is not None and row["start_ticks"] == original["start_ticks"]


def run(args):
    os.umask(0o077)
    root = args.suite_dir.resolve()
    out = (args.out_dir or root / "rpc-counters").resolve()
    metadata = json.loads((root / "run.json").read_text())
    started = datetime.fromisoformat(metadata["started_utc"].replace("Z", "+00:00"))
    if started.tzinfo is None or not 0 < float(metadata["hours"]) <= 168:
        raise ValueError("Invalid suite deadline")
    until = started.timestamp() + float(metadata["hours"]) * 3600
    remaining = until - time.time()
    if remaining <= 0:
        raise ValueError("Suite deadline has already passed")
    ops = module("rpc_suite_owner", root / "tools/capture-sequencer-suite.py")
    parent = owner(ops, root, metadata)
    helper_path = root / "tools/capture-main-cache-io.py"
    helper = module("rpc_identity_helper", helper_path)
    out.mkdir(parents=True, exist_ok=True)
    if (out / "run.json").exists():
        raise ValueError("RPC capture output already contains a run")
    args.lock_file.parent.mkdir(parents=True, exist_ok=True)
    with args.lock_file.open("a") as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise ValueError("An RPC counter collector is already running") from None
        if shutil.disk_usage(out).free < args.min_free_gib * 1024**3:
            raise ValueError("Insufficient free space for RPC counters")
        source = Path(__file__).read_bytes()
        (out / "collector.py").write_bytes(source)
        save(out / "run.json", {
            "schema": 1, "started_utc": utc(), "pid": os.getpid(), "suite_dir": str(root),
            "suite_parent_pid": parent["pid"], "suite_parent_start_ticks": parent["start_ticks"],
            "planned_end_utc": datetime.fromtimestamp(until, timezone.utc).isoformat(),
            "interval_s": args.interval, "collector_sha256": hashlib.sha256(source).hexdigest(),
            "identity_helper_sha256": hashlib.sha256(helper_path.read_bytes()).hexdigest(),
            "note": "The first sample contains uptime counters. Only valid adjacent differences describe this new observation window.",
        })
        stop = threading.Event()
        handlers = {sig: signal.signal(sig, lambda *_: stop.set())
                    for sig in (signal.SIGTERM, signal.SIGINT)}
        deadline = time.monotonic() + max(0, until - time.time())
        previous = previous_tick = None
        next_sample = next_status = next_budget = 0
        samples = error_samples = 0
        reason, code = "duration_elapsed", 0
        try:
            with (out / "counters.jsonl").open("x") as stream:
                while not stop.is_set() and time.monotonic() < deadline:
                    if not same_owner(ops, parent):
                        reason = "suite_parent_exited_or_reused"
                        break
                    tick = time.monotonic()
                    if tick >= next_sample:
                        row = observe(helper, args.metrics_url)
                        seconds = tick - previous_tick if previous_tick is not None else 0
                        row["interval_s"] = seconds if previous_tick is not None else None
                        row["deltas"], row["delta_unavailable"] = deltas(row, previous, seconds, args.interval)
                        stream.write(json.dumps(row, separators=(",", ":"), allow_nan=False) + "\n")
                        stream.flush()
                        previous, previous_tick = row, tick
                        samples += 1
                        error_samples += bool(row["errors"])
                        next_sample = tick + args.interval
                        if tick >= next_status:
                            status = {"kind": "heartbeat", "time_utc": utc(), "pid": os.getpid(),
                                "samples": samples, "error_samples": error_samples,
                                "remaining_s": max(0, int(deadline - time.monotonic())),
                                "last_errors": row["errors"], "last_sample_utc": row["time_utc"],
                                "last_counters": row["counters"]}
                            save(out / "status.json", status)
                            print(json.dumps(status), flush=True)
                            next_status = tick + 60
                    if tick >= next_budget:
                        size = sum(p.stat().st_size for p in out.iterdir() if p.is_file())
                        if size >= 64 * 1024**2 or shutil.disk_usage(out).free < args.min_free_gib * 1024**3:
                            reason, code = "disk_budget", 1
                            break
                        next_budget = tick + 60
                    stop.wait(max(0, min(1, next_sample - time.monotonic(), deadline - time.monotonic())))
                if stop.is_set():
                    reason = "signal"
        except Exception as error:
            reason, code = type(error).__name__ + ":" + str(error), 1
        finally:
            status = {"kind": "complete" if reason == "duration_elapsed" else "stopped", "reason": reason,
                "time_utc": utc(), "pid": os.getpid(), "samples": samples, "error_samples": error_samples}
            save(out / "status.json", status)
            print(json.dumps(status), flush=True)
            for sig, handler in handlers.items():
                signal.signal(sig, handler)
        return code


def report(root):
    metadata = json.loads((root / "run.json").read_text())
    suite_dir = Path(metadata["suite_dir"])
    reader = module("rpc_report_reader", suite_dir / "tools/report-sequencer-context.py")
    rows, input_status = reader.snapshot(root / "counters.jsonl")
    valid = [r for r in rows if not r.get("delta_unavailable") and not r.get("errors")
             and type(r.get("interval_s")) in (int, float) and math.isfinite(r["interval_s"])
             and r["interval_s"] > 0 and all(
                 type((r.get("deltas") or {}).get(k)) is int and r["deltas"][k] >= 0 for k in ALL_KEYS)]
    total = {key: sum(r["deltas"][key] for r in valid) if valid else None for key in ALL_KEYS}
    methods = {}
    for method in METHODS:
        methods[method] = {outcome: {suffix: total[
            "rpc_diagnostics_%s_%s_%s" % (method, outcome, suffix)] for suffix in SUFFIXES}
            for outcome in OUTCOMES}
    return {
        "first_sample_utc": rows[0].get("time_utc") if rows else None,
        "last_sample_utc": rows[-1].get("time_utc") if rows else None,
        "samples": len(rows), "error_samples": sum(bool(r.get("errors")) for r in rows),
        "valid_intervals": len(valid),
        "valid_interval_seconds": round(sum(r["interval_s"] for r in valid), 3),
        "unavailable_intervals": dict(Counter(r.get("delta_unavailable") for r in rows if r.get("delta_unavailable"))),
        "method_deltas": methods, "details_dropped_delta": total[DROP], "input_status": input_status,
        "limits": [
            "Only valid adjacent differences are summed; missing/reset/error gaps are excluded, not counted as zero.",
            "These deltas start at this collector's first sample, not at the original suite start.",
            "Threshold counts are nested; do not add ge1s, ge3s and ge5s.",
            "Details dropped is shared by getLogs/sendRaw and does not count dropped transactions.",
            "RPC success is a submission result, not a successful transaction receipt.",
        ],
    }


def summarize_logs(lines, cutoff, watcher):
    """Split retained records by log timestamp, without inferring dropped calls."""
    groups = {name: {"stage_ge1s_records": 0, "stage_success": 0, "stage_failure": 0,
                    "stage_ge3s_records": 0, "stage_ge5s_records": 0,
                    "slowest_stage_records": [], "recent_served_errors": [],
                    "unparsed_sendraw_stage_lines": 0}
              for name in ("before_capture", "during_capture")}
    first = last = None
    for line in lines:
        stamp = watcher.parse_log_time(line)
        if stamp is None:
            continue
        when = datetime.fromisoformat(stamp.replace("Z", "+00:00"))
        first = when if first is None else min(first, when)
        last = when if last is None else max(last, when)
        group = groups["before_capture" if when < cutoff else "during_capture"]
        match = watcher.RPC_SLOW_RE.search(line)
        if ("RPC stage timings" in line and "method=eth_sendRawTransaction" in line
                and match is None):
            group["unparsed_sendraw_stage_lines"] += 1
        if match and match.group("method") == "eth_sendRawTransaction":
            duration = float(match.group("total"))
            if duration >= 1000:
                group["stage_ge1s_records"] += 1
                outcome = match.group("outcome")
                if outcome in OUTCOMES:
                    group["stage_" + outcome] += 1
                group["stage_ge3s_records"] += duration >= 3000
                group["stage_ge5s_records"] += duration >= 5000
                group["slowest_stage_records"].append({
                    "time_sgt": when.astimezone(timezone(timedelta(hours=8))).isoformat(),
                    "trace": match.group("trace"), "outcome": outcome, "total_ms": duration})
                group["slowest_stage_records"].sort(key=lambda r: r["total_ms"], reverse=True)
                del group["slowest_stage_records"][5:]
        if "Served eth_sendRawTransaction" in line:
            error = re.search(r'\berr=(?:"((?:[^"\\]|\\.)*)"|(\S+))', line)
            duration = re.search(r"\bduration=(\S+)", line)
            if error:
                group["recent_served_errors"].append({
                    "time_sgt": when.astimezone(timezone(timedelta(hours=8))).isoformat(),
                    "duration": duration.group(1) if duration else None,
                    "error": (error.group(1) if error.group(1) is not None else error.group(2))[:300]})
                del group["recent_served_errors"][:-5]
    return {"first_retained_log_utc": first.isoformat() if first else None,
            "last_retained_log_utc": last.isoformat() if last else None, **groups}


def inspect_logs(args):
    root = args.suite_dir.resolve()
    metadata = json.loads((root / "run.json").read_text())
    cutoff = datetime.fromisoformat(metadata["started_utc"].replace("Z", "+00:00"))
    helper = module("rpc_log_identity", root / "tools/capture-main-cache-io.py")
    identity = helper.identity("main-nitro-1")
    watcher = module("rpc_log_parser", root / "tools/watch-slow-sequencer-blocks.py")
    until = utc()
    timed_out = threading.Event()
    with subprocess.Popen(["docker", "logs", "--timestamps", "--since", identity["started"],
                           "--until", until, identity["id"]], stdout=subprocess.PIPE,
                          stderr=subprocess.STDOUT, text=True) as process:
        def expire():
            timed_out.set()
            process.kill()  # Only this owned, read-only Docker log client.
        timer = threading.Timer(45, expire)
        timer.daemon = True
        timer.start()
        try:
            summary = summarize_logs(process.stdout, cutoff, watcher)
        finally:
            timer.cancel()
            if process.poll() is None:
                try:
                    process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait(timeout=5)
    return {"node_started_utc": identity["started"], "capture_started_utc": metadata["started_utc"],
            "logs_until_utc": until, "query_timed_out": timed_out.is_set(),
            "docker_logs_returncode": process.returncode, **summary,
            "limits": ["Only retained Docker records are counted; rotated logs and dropped details cannot be recovered.",
                       "Stage records and Served errors are separate views; do not add them as distinct requests.",
                       "Asynchronous log timestamps may lag request completion.",
                       "The global details-dropped counter also includes getLogs.",
                       "A zero during-capture log count does not reconstruct the missing counter baseline."]}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument("--suite-dir", type=Path)
    mode.add_argument("--report", type=Path)
    parser.add_argument("--inspect-logs", action="store_true",
                        help="Read retained logs since node startup and split them at the suite start")
    parser.add_argument("--out-dir", type=Path)
    parser.add_argument("--interval", type=float, default=15)
    parser.add_argument("--metrics-url", default="http://127.0.0.1:6070/debug/metrics/prometheus")
    parser.add_argument("--lock-file", type=Path, default=Path("/run/lock/nitro-rpc-diagnostics-capture.lock"))
    parser.add_argument("--min-free-gib", type=float, default=1)
    args = parser.parse_args()
    if args.inspect_logs:
        if args.suite_dir is None:
            parser.error("--inspect-logs requires --suite-dir")
        result = inspect_logs(args)
        print(json.dumps(result, indent=2, allow_nan=False))
        return 1 if result["query_timed_out"] or result["docker_logs_returncode"] else 0
    if args.report:
        print(json.dumps(report(args.report.resolve()), indent=2, allow_nan=False))
        return 0
    if not math.isfinite(args.interval) or not 10 <= args.interval <= 60 or not (
            math.isfinite(args.min_free_gib) and args.min_free_gib >= 0):
        parser.error("Require interval 10..60s and a nonnegative free-space floor")
    if not Path("/proc/stat").exists():
        parser.error("Run on the Linux node host")
    return run(args)


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (OSError, ValueError, RuntimeError, subprocess.SubprocessError) as error:
        print("RPC counter capture stopped: " + str(error), file=sys.stderr)
        raise SystemExit(1)
