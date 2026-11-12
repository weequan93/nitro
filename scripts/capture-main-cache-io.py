#!/usr/bin/env python3
"""Bounded cache-counter and device-IOPS sampling alongside an existing capture.

Requires capture-sequencer-context.py beside this file. Reads /proc, Docker
identity and the metrics endpoint; no node RPC, replay, profile or changes.
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
import shutil
import signal
import subprocess
import sys
import threading
import time


spec = importlib.util.spec_from_file_location(
    "context_support", Path(__file__).with_name("capture-sequencer-context.py"))
base = importlib.util.module_from_spec(spec)
spec.loader.exec_module(base)

CACHE_PAIRS = {
    "pebble_block": "l2chaindata_cache_block_",
    "pebble_table": "l2chaindata_cache_table_",
    "trie_clean": "hashdb_memcache_clean_",
    "trie_dirty": "hashdb_memcache_dirty_",
    "snapshot_clean_account": "state_snapshot_clean_account_",
    "snapshot_clean_storage": "state_snapshot_clean_storage_",
    "snapshot_dirty_account": "state_snapshot_dirty_account_",
    "snapshot_dirty_storage": "state_snapshot_dirty_storage_",
}


def emit(stream, value):
    stream.write(json.dumps(value, separators=(",", ":"), allow_nan=False) + "\n")
    stream.flush()


def identity(container):
    template = ('{"id":{{json .Id}},"image":{{json .Image}},"pid":{{.State.Pid}},'
                '"running":{{.State.Running}},"started":{{json .State.StartedAt}}}')
    result = subprocess.run(["docker", "inspect", "--format", template, container],
                            capture_output=True, text=True, timeout=3, check=True)
    obj = json.loads(result.stdout)
    if not obj.get("running") or not isinstance(obj.get("pid"), int) or obj["pid"] <= 0:
        raise ValueError("main_not_running")
    root = Path("/proc") / str(obj["pid"])
    if (base.read_text(root / "comm", 4096) or "").strip() != "nitro":
        raise ValueError("native_nitro_not_ready")
    text = base.read_text(root / "stat", 16384)
    if text is None:
        raise ValueError("native_identity_unavailable")
    obj["start_ticks"] = int(text.rsplit(") ", 1)[1].split()[19])
    obj.pop("running")
    return obj


def metrics(text):
    result = base.parse_metrics(text)
    for line in text.splitlines():
        match = base.METRIC.match(line)
        if not match:
            continue
        name, labels, value = match.groups()
        if labels or not name.startswith(("l2chaindata_cache_", "hashdb_memcache_clean_",
                                          "hashdb_memcache_dirty_", "state_snapshot_clean_",
                                          "state_snapshot_dirty_", "l2chaindata_tables_level0_")):
            continue
        number = float(value)
        if math.isfinite(number):
            result[name] = number
    return result


def cache_deltas(current, previous, seconds, max_gap=40):
    if previous is None:
        return {}, "first_sample"
    if not current.get("identity") or current["identity"] != previous.get("identity"):
        return {}, "identity_missing_or_changed"
    if current.get("errors") or previous.get("errors"):
        return {}, "sample_error"
    if not 0 < seconds <= max_gap:
        return {}, "sample_gap"
    results = {}
    for name, prefix in CACHE_PAIRS.items():
        changes = []
        for suffix in ("hit", "miss"):
            old = (previous.get("metrics") or {}).get(prefix + suffix)
            new = (current.get("metrics") or {}).get(prefix + suffix)
            valid = all(type(v) in (int, float) and math.isfinite(v) and v >= 0 for v in (old, new))
            changes.append(new - old if valid and new >= old else None)
        hits, misses = changes
        valid = hits is not None and misses is not None
        total = hits + misses if valid else 0
        results[name] = {"hits_delta": hits if valid else None, "misses_delta": misses if valid else None,
                         "hit_percent": hits / total * 100 if total else None,
                         "misses_s": misses / seconds if valid else None}
    return results, None


def io_delta(current, previous, seconds):
    if current is None or previous is None or seconds <= 0:
        return None
    keys = ("reads", "writes", "read_sectors", "write_sectors", "read_ms", "write_ms", "weighted_ms")
    if any(key not in current or key not in previous or current[key] < previous[key] for key in keys):
        return None
    result = base.disk_delta({"device": current}, {"device": previous}, seconds)["device"]
    for operation in ("read", "write"):
        count = current[operation + "s"] - previous[operation + "s"]
        sectors = current[operation + "_sectors"] - previous[operation + "_sectors"]
        result[operation + "_iops"] = count / seconds
        result[operation + "_request_KiB"] = sectors / 2 / count if count else None
    return result


def cache_loop(args, stop, stream, state):
    previous = previous_tick = None
    try:
        while not stop.is_set():
            tick = time.monotonic()
            event = {"time_utc": base.utc(), "identity": None, "metrics": {}, "errors": []}
            before = after = None
            try:
                before = identity(args.container)
            except (OSError, ValueError, IndexError, subprocess.SubprocessError) as error:
                event["errors"].append("identity_before:" + type(error).__name__)
            try:
                event["metrics"] = metrics(base.fetch(args.metrics_url, cap=2 * 1024**2).decode())
            except (OSError, ValueError, TypeError) as error:
                event["errors"].append("metrics:" + type(error).__name__)
            try:
                after = identity(args.container)
            except (OSError, ValueError, IndexError, subprocess.SubprocessError) as error:
                event["errors"].append("identity_after:" + type(error).__name__)
            if before is not None and before == after:
                event["identity"] = before
            else:
                event["errors"].append("identity_unverified")
            event["finished_utc"] = base.utc()
            seconds = tick - previous_tick if previous_tick is not None else 0
            event["interval_s"] = seconds if previous is not None else None
            event["cache_deltas"], event["delta_unavailable"] = cache_deltas(
                event, previous, seconds, max(40, args.metrics_interval * 2 + 10))
            if stop.is_set():
                break
            emit(stream, event)
            state["cache_samples"] += 1
            state["cache_error_samples"] += bool(event["errors"])
            previous, previous_tick = event, tick
            stop.wait(max(0, args.metrics_interval - (time.monotonic() - tick)))
    except Exception as error:
        state["worker_error"] = type(error).__name__
        stop.set()


def run(args):
    os.umask(0o077)
    root = args.out_dir.resolve()
    root.mkdir(parents=True, exist_ok=True)
    args.lock_file.parent.mkdir(parents=True, exist_ok=True)
    with args.lock_file.open("a") as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise ValueError("Cache/IO sampler already running") from None
        if any((root / name).exists() for name in ("run.json", "cache.jsonl", "io.jsonl")):
            raise ValueError("Use a fresh output directory")
        if shutil.disk_usage(root).free < 1024**3:
            raise ValueError("Require at least 1 GiB free space")
        metadata = {"started_utc": base.utc(), "pid": os.getpid(), "minutes": args.minutes,
                    "device": args.device, "interval_s": args.interval,
                    "metrics_interval_s": args.metrics_interval,
                    "support_sha256": hashlib.sha256(Path(base.__file__).read_bytes()).hexdigest(),
                    "boot_id": (base.read_text("/proc/sys/kernel/random/boot_id") or "").strip(),
                    "device_number": (base.read_text("/sys/class/block/" + args.device + "/dev") or "").strip(),
                    "note": "Cache counts cover the process workload, not a specific RPC. Device IOPS cover all users of the device."}
        (root / "run.json").write_text(json.dumps(metadata, indent=2) + "\n")
        stop = threading.Event()
        handlers = {}
        for sig in (signal.SIGINT, signal.SIGTERM):
            handlers[sig] = signal.signal(sig, lambda *_: stop.set())
        state = {"cache_samples": 0, "cache_error_samples": 0, "worker_error": None}
        worker = None
        count = 0
        reason = "duration_elapsed"
        deadline = time.monotonic() + args.minutes * 60
        try:
            with (root / "cache.jsonl").open("x") as cache_file, (root / "io.jsonl").open("x") as io_file:
                worker = threading.Thread(target=cache_loop, args=(args, stop, cache_file, state), daemon=True)
                worker.start()
                previous_tick = time.monotonic()
                previous = (base.disk_snapshot() or {}).get(args.device)
                heartbeat_at = budget_at = 0
                while not stop.is_set() and time.monotonic() < deadline:
                    tick = time.monotonic()
                    current = (base.disk_snapshot() or {}).get(args.device)
                    seconds = tick - previous_tick
                    emit(io_file, {"time_utc": base.utc(), "interval_s": seconds, "device": args.device,
                                   "raw": current, "rates": io_delta(current, previous, seconds),
                                   "unavailable": current is None})
                    previous, previous_tick = current, tick
                    count += 1
                    if tick >= heartbeat_at:
                        heartbeat = {"kind": "heartbeat", "time_utc": base.utc(), "io_samples": count,
                                     **state, "remaining_s": max(0, int(deadline - tick))}
                        temporary = root / ".status.tmp"
                        temporary.write_text(json.dumps(heartbeat, indent=2) + "\n")
                        temporary.replace(root / "status.json")
                        print(json.dumps(heartbeat), flush=True)
                        heartbeat_at = tick + 60
                    if tick >= budget_at:
                        size = sum(p.stat().st_size for p in root.iterdir() if p.is_file())
                        if size >= 128 * 1024**2 or shutil.disk_usage(root).free < 1024**3:
                            reason = "disk_budget"
                            break
                        budget_at = tick + 60
                    stop.wait(max(0, min(deadline - time.monotonic(), args.interval - (time.monotonic() - tick))))
                if stop.is_set() and time.monotonic() < deadline:
                    reason = "worker_error" if state["worker_error"] else "signal"
                stop.set()
                worker.join(timeout=15)
                if worker.is_alive():
                    reason = "metrics_worker_did_not_exit"
                if state["worker_error"]:
                    reason = "worker_error"
        finally:
            stop.set()
            if worker and worker.is_alive():
                worker.join(timeout=1)
            for sig, handler in handlers.items():
                signal.signal(sig, handler)
        record = {"kind": "complete" if reason == "duration_elapsed" else "stopped", "reason": reason,
                  "time_utc": base.utc(), "io_samples": count, **state}
        (root / "status.json").write_text(json.dumps(record, indent=2) + "\n")
        print(json.dumps(record), flush=True)
        return 0 if reason in ("duration_elapsed", "signal") else 1


def read_rows(path):
    size = path.stat().st_size
    if size > 128 * 1024**2:
        raise ValueError("Report input exceeds 128 MiB")
    rows, bad = [], 0
    with path.open("rb") as stream:
        remaining = size
        while remaining:
            line = stream.readline(min(remaining, 1024**2 + 1))
            remaining -= len(line)
            if not line:
                break
            if len(line) > 1024**2:
                raise ValueError("Report line exceeds 1 MiB")
            try:
                row = json.loads(line)
                if not isinstance(row, dict):
                    raise ValueError("not_object")
                rows.append(row)
            except (ValueError, UnicodeError):
                bad += 1
    return rows, bad


def report(root):
    caches, cache_bad = read_rows(root / "cache.jsonl")
    ios, io_bad = read_rows(root / "io.jsonl")
    pairs = {}
    for name in CACHE_PAIRS:
        hits = misses = seconds = 0
        valid = 0
        peak_misses = None
        least_hit = None
        attempted = 0
        for row in caches:
            delta = (row.get("cache_deltas") or {}).get(name) or {}
            h, m, s = delta.get("hits_delta"), delta.get("misses_delta"), row.get("interval_s")
            interval_valid = type(s) in (int, float) and math.isfinite(s) and s > 0
            attempted += interval_valid
            if all(type(v) in (int, float) and math.isfinite(v) and v >= 0 for v in (h, m)) and interval_valid:
                hits += h
                misses += m
                seconds += s
                valid += 1
                if peak_misses is None or m / s > peak_misses["misses_s"]:
                    peak_misses = {"interval_end_utc": row.get("time_utc"), "misses_s": m / s}
                if h + m and (least_hit is None or h / (h + m) * 100 < least_hit["hit_percent"]):
                    least_hit = {"interval_end_utc": row.get("time_utc"), "hit_percent": h / (h + m) * 100}
        pairs[name] = {"valid_intervals": valid, "covered_seconds": round(seconds, 3),
                       "unavailable_intervals": attempted - valid,
                       "hits_delta": hits if valid else None, "misses_delta": misses if valid else None,
                       "hit_percent": round(hits / (hits + misses) * 100, 3) if hits + misses else None,
                       "misses_s": round(misses / seconds, 3) if seconds else None,
                       "peak_miss_interval": peak_misses, "lowest_hit_interval": least_hit}
    device_stats = {}
    for key in ("read_iops", "write_iops", "read_request_KiB", "write_request_KiB", "read_await_ms", "write_await_ms",
                "average_queue", "read_bytes_s", "write_bytes_s"):
        # The first interval is only the setup overhead; omit it from summaries.
        values = [(row.get("rates") or {}).get(key) for row in ios[1:]]
        values = [v for v in values if type(v) in (int, float) and math.isfinite(v)]
        device_stats[key] = {"n": len(values), "mean": round(sum(values) / len(values), 3) if values else None,
                             "max": round(max(values), 3) if values else None}
    return {"cache_samples": len(caches), "io_samples": len(ios), "malformed_rows": cache_bad + io_bad,
            "first_cache_utc": caches[0].get("time_utc") if caches else None,
            "last_cache_utc": caches[-1].get("time_utc") if caches else None,
            "cache_samples_with_errors": sum(bool(r.get("errors")) for r in caches),
            "delta_unavailable_samples": sum(bool(r.get("delta_unavailable")) for r in caches),
            "device_unavailable_samples": sum(bool(r.get("unavailable")) for r in ios),
            "io_delta_unavailable_samples": sum(r.get("rates") is None for r in ios[1:]),
            "cache_intervals": pairs, "device_sample_stats": device_stats,
            "limits": ["Hit percentages use interval counts, not averaged percentages or process-lifetime totals.",
                       "Cache hits/misses are lookups, not physical I/O operations; page cache and read amplification also matter.",
                       "Device sample means are unweighted; IOPS are completed device operations, not database calls.",
                       "Metrics/device intervals are temporal context and are not joined to an individual RPC trace."]}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument("--out-dir", type=Path)
    mode.add_argument("--report", type=Path)
    parser.add_argument("--minutes", type=float, default=60)
    parser.add_argument("--interval", type=float, default=1)
    parser.add_argument("--metrics-interval", type=float, default=15)
    parser.add_argument("--device", default="vdb")
    parser.add_argument("--container", default="main-nitro-1")
    parser.add_argument("--metrics-url", default="http://127.0.0.1:6070/debug/metrics/prometheus")
    parser.add_argument("--lock-file", type=Path, default=Path("/run/lock/nitro-cache-io-capture.lock"))
    args = parser.parse_args()
    if args.report:
        print(json.dumps(report(args.report), indent=2, allow_nan=False))
        return 0
    if not all(math.isfinite(v) for v in (args.minutes, args.interval, args.metrics_interval)) or not (
            0 < args.minutes <= 360 and 1 <= args.interval <= 60 and 10 <= args.metrics_interval <= 300):
        parser.error("Require 0 < minutes <= 360, IO interval 1..60s and metrics interval 10..300s")
    if (not re.fullmatch(r"[A-Za-z0-9_-]+", args.device) or not Path("/proc/diskstats").exists() or
            (Path("/sys/class/block") / args.device / "partition").exists()):
        parser.error("Run on the Linux host with a valid whole-device name")
    return run(args)


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (OSError, ValueError) as error:
        print("Cache/IO capture stopped: " + str(error), file=sys.stderr)
        raise SystemExit(1)
