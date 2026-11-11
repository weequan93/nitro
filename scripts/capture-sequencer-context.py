#!/usr/bin/env python3
"""Bounded, read-only resource and transaction sampling on a Linux node host.

Resources are sampled independently of RPC. Periodic latest blocks are workload
controls, not proven fast blocks: this script does not measure their execution
time. Join block numbers and UTC times with watch-slow-sequencer-blocks.py.
No replay, eth_call, trace, profile, configuration change or process control.
"""

import argparse
from datetime import datetime, timezone
import json
import math
import os
from pathlib import Path
import re
import signal
import subprocess
import threading
import time
import urllib.request


ORACLE = "0x4243bc718ae8a11c7c71cb39bebd314ab0925f6c"
SELECTOR = "0x71044186"
METRIC = re.compile(r"^([a-zA-Z_:][a-zA-Z0-9_:]*)\s*(?:\{([^}]*)\})?\s+([-+0-9.eE]+)(?:\s+\d+)?$")


def utc():
    return datetime.now(timezone.utc).isoformat()


def read_text(path, cap=1024 * 1024):
    try:
        with Path(path).open("rb") as stream:
            value = stream.read(cap + 1)
        return value.decode(errors="replace") if len(value) <= cap else None
    except OSError:
        return None


def named_numbers(text, multiplier=1):
    if text is None:
        return None
    result = {}
    for line in text.splitlines():
        fields = line.replace(":", " ").split()
        if multiplier == 1024 and (len(fields) < 3 or fields[2] != "kB"):
            continue
        if len(fields) >= 2 and fields[1].isdigit():
            result[fields[0]] = int(fields[1]) * multiplier
    return result


def diff(now, before, key):
    n, p = now.get(key), before.get(key)
    return n - p if isinstance(n, (int, float)) and isinstance(p, (int, float)) and n >= p else None


def pressure(path):
    value = read_text(path)
    if value is None:
        return None
    result = {}
    for line in value.splitlines():
        parts = line.split()
        result[parts[0]] = dict(item.split("=", 1) for item in parts[1:] if "=" in item)
    return result


def process_snapshot(pid):
    root = Path("/proc") / str(pid)
    text = read_text(root / "stat")
    if not text:
        return {"pid": pid, "available": False}
    fields = text.rsplit(") ", 1)[1].split()
    result = {
        "pid": pid, "available": True, "state": fields[0],
        "start_ticks": int(fields[19]), "cpu_ticks": int(fields[11]) + int(fields[12]),
        "major_faults": int(fields[9]), "rss_bytes": int(fields[21]) * os.sysconf("SC_PAGE_SIZE"),
        "io": named_numbers(read_text(root / "io")),
    }
    paths = read_text(root / "cgroup") or ""
    for line in paths.splitlines():
        if line.startswith("0::"):
            cg = Path("/sys/fs/cgroup") / line[3:].lstrip("/")
            result["cgroup"] = {
                "cpu_stat": named_numbers(read_text(cg / "cpu.stat")),
                "cpu_max": (read_text(cg / "cpu.max") or "").strip() or None,
                "memory_current": (read_text(cg / "memory.current") or "").strip() or None,
                "memory_max": (read_text(cg / "memory.max") or "").strip() or None,
                "memory_events": named_numbers(read_text(cg / "memory.events")),
            }
    return result


def process_delta(now, previous, seconds):
    out = dict(now)
    out["rates"] = None
    if (not now.get("available") or not previous.get("available") or
            now.get("start_ticks") != previous.get("start_ticks")):
        return out
    out["rates"] = {}
    for key, scale in (("cpu_ticks", os.sysconf("SC_CLK_TCK")), ("major_faults", 1)):
        delta = diff(now, previous, key)
        out["rates"]["cpu_cores" if key == "cpu_ticks" else "major_faults_s"] = (
            delta / scale / seconds if delta is not None else None)
    for key in ("read_bytes", "write_bytes"):
        delta = diff(now.get("io") or {}, previous.get("io") or {}, key)
        out["rates"][key + "_s"] = delta / seconds if delta is not None else None
    cg = (now.get("cgroup") or {}).get("cpu_stat") or {}
    old_cg = (previous.get("cgroup") or {}).get("cpu_stat") or {}
    for key in ("nr_throttled", "throttled_usec"):
        out["rates"][key + "_delta"] = diff(cg, old_cg, key)
    return out


def disk_snapshot():
    result = {}
    text = read_text("/proc/diskstats")
    if text is None:
        return None
    for line in text.splitlines():
        f = line.split()
        if len(f) < 14 or f[2].startswith(("loop", "ram", "zram")):
            continue
        # Report each whole device separately; never sum logical and parent devices.
        if (Path("/sys/class/block") / f[2] / "partition").exists():
            continue
        result[f[2]] = dict(zip(
            ("reads", "read_merges", "read_sectors", "read_ms", "writes", "write_merges",
             "write_sectors", "write_ms", "inflight", "busy_ms", "weighted_ms"),
            map(int, f[3:14])))
    return result


def disk_delta(now, before, seconds):
    if now is None:
        return None
    result = {}
    for name, counters in now.items():
        old = (before or {}).get(name, {})
        delta = {k: diff(counters, old, k) for k in counters if k != "inflight"}
        def rate(key, multiplier=1):
            v = delta.get(key)
            return v * multiplier / seconds if v is not None else None
        def await_ms(operation):
            count, millis = delta.get(operation + "s"), delta.get(operation + "_ms")
            return millis / count if count and millis is not None else None
        result[name] = {
            "read_bytes_s": rate("read_sectors", 512), "write_bytes_s": rate("write_sectors", 512),
            "read_await_ms": await_ms("read"), "write_await_ms": await_ms("write"),
            "busy_percent": rate("busy_ms", 0.1), "average_queue": rate("weighted_ms", 0.001),
            "inflight": counters["inflight"],
        }
    return result


def discover(container):
    pids = {}
    docker_error = None
    try:
        top = subprocess.run(["docker", "top", container, "-eo", "pid,comm"],
                             capture_output=True, text=True, timeout=3, check=True)
        for line in top.stdout.splitlines()[1:]:
            parts = line.split()
            if len(parts) >= 2 and parts[0].isdigit() and "nitro" in parts[1]:
                pids[int(parts[0])] = parts[1]
    except (OSError, subprocess.SubprocessError) as error:
        docker_error = type(error).__name__
    for item in Path("/proc").iterdir():
        if item.name.isdigit():
            try:
                if Path(os.readlink(item / "exe")).name == "pathdb-migrate-pipeline":
                    pids[int(item.name)] = "pathdb-migrate-pipeline"
            except OSError:
                pass
    return pids, docker_error


def fetch(url, payload=None, cap=8 * 1024 * 1024):
    data = json.dumps(payload).encode() if payload is not None else None
    request = urllib.request.Request(url, data=data, headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(request, timeout=3) as response:
        body = response.read(cap + 1)
    if len(body) > cap:
        raise ValueError("response_size_limit")
    return body


def rpc(url, method, params):
    if method != "eth_getBlockByNumber":
        raise ValueError("method_not_allowed")
    obj = json.loads(fetch(url, {"jsonrpc": "2.0", "id": 1, "method": method, "params": params}))
    if obj.get("error") is not None or not isinstance(obj.get("result"), dict):
        raise ValueError("block_unavailable")
    return obj["result"]


def quantity(value):
    try:
        return int(value, 16) if isinstance(value, str) else None
    except ValueError:
        return None


def workload(block):
    result = []
    for tx in block.get("transactions", []):
        if not isinstance(tx, dict):
            continue
        payload = tx.get("input")
        valid_input = isinstance(payload, str) and payload.startswith("0x") and len(payload) % 2 == 0
        item = {k: tx.get(k) for k in ("hash", "from", "to", "type")}
        item.update({"input_bytes": (len(payload) - 2) // 2 if valid_input else None,
                     "selector": payload[:10] if valid_input and len(payload) >= 10 else None})
        if valid_input and str(tx.get("to", "")).lower() == ORACLE and payload[:10].lower() == SELECTOR:
            try:
                raw = bytes.fromhex(payload[10:])
                offset = int.from_bytes(raw[:32], "big")
                if len(raw) < 224 or offset != 224 or len(raw) < offset + 32:
                    raise ValueError("invalid_abi")
                count = int.from_bytes(raw[offset:offset + 32], "big")
                if len(raw) < offset + 32 + count * 128:
                    raise ValueError("truncated_prices")
                item.update({"method": "batchSetPrices", "price_entries": count})
            except ValueError:
                item["price_entries"] = None
        result.append(item)
    return {"block_number": quantity(block.get("number")), "block_hash": block.get("hash"),
            "block_timestamp": quantity(block.get("timestamp")),
            "block_gas_used": quantity(block.get("gasUsed")), "transactions": result,
            "selection": "periodic_latest_unclassified", "sequence_elapsed_ms": None}


def parse_metrics(text):
    result = {}
    for line in text.splitlines():
        match = METRIC.match(line)
        if not match:
            continue
        name, labels, value = match.groups()
        if not name.startswith(("arb_txpublish_", "arb_sequencer_", "l2chaindata_compact_",
                                "state_snapshot_generation_")):
            continue
        if labels and labels not in ('quantile="0.5"', 'quantile="0.99"', 'quantile="0.999"'):
            continue
        value = float(value)
        if math.isfinite(value):
            result[name + ("{" + labels + "}" if labels else "")] = value
    return result


def emit(stream, event):
    stream.write(json.dumps(event, separators=(",", ":"), allow_nan=False) + "\n")
    stream.flush()


def network_loop(args, stop, stream):
    previous_hash = None
    while not stop.is_set():
        event = {"kind": "context", "started_utc": utc()}
        try:
            event["metrics"] = parse_metrics(fetch(args.metrics_url, cap=2 * 1024 * 1024).decode())
        except (OSError, ValueError, TypeError) as error:
            event["metrics_error"] = type(error).__name__
        if not stop.is_set():
            try:
                block = rpc(args.rpc_url, "eth_getBlockByNumber", ["latest", True])
                if block.get("hash") != previous_hash:
                    event["block"] = workload(block)
                    previous_hash = block.get("hash")
            except (OSError, ValueError, TypeError) as error:
                event["block_error"] = type(error).__name__
        event["finished_utc"] = utc()
        emit(stream, event)
        stop.wait(args.context_interval)


def run(args):
    os.umask(0o077)
    out = Path(args.out_dir)
    out.mkdir(parents=True, exist_ok=True)
    stop = threading.Event()
    for sig in (signal.SIGINT, signal.SIGTERM):
        signal.signal(sig, lambda *_: stop.set())
    with (out / "resources.jsonl").open("x") as resource_file, (out / "context.jsonl").open("x") as context_file:
        emit(resource_file, {"kind": "start", "time_utc": utc(), "minutes": args.minutes,
                             "interval_s": args.interval, "context_interval_s": args.context_interval,
                             "boot_id": (read_text("/proc/sys/kernel/random/boot_id") or "").strip(),
                             "note": "Independent devices; unknown is null. Sampled blocks are not proven fast."})
        worker = threading.Thread(target=network_loop, args=(args, stop, context_file))
        worker.start()
        deadline = time.monotonic() + args.minutes * 60
        previous_time = time.monotonic()
        previous_disks = disk_snapshot()
        previous_processes = {}
        pids = {}
        docker_error = None
        rediscover_at = heartbeat_at = 0
        samples = 0
        try:
            while not stop.is_set() and time.monotonic() < deadline:
                started = time.monotonic()
                if started >= rediscover_at:
                    pids, docker_error = discover(args.container)
                    rediscover_at = started + 30
                started = time.monotonic()
                seconds = started - previous_time
                processes = {pid: process_snapshot(pid) for pid in pids}
                disks = disk_snapshot()
                cpu = next((line.split()[1:9] for line in (read_text("/proc/stat") or "").splitlines()
                            if line.startswith("cpu ")), [])
                emit(resource_file, {
                    "kind": "resources", "time_utc": utc(), "interval_s": seconds,
                    "docker_top_error": docker_error,
                    "cpu_ticks": [int(v) for v in cpu] if cpu else None,
                    "meminfo_bytes": named_numbers(read_text("/proc/meminfo"), 1024),
                    "pressure": {k: pressure("/proc/pressure/" + k) for k in ("cpu", "memory", "io")},
                    "devices": disk_delta(disks, previous_disks, seconds),
                    "processes": [{"role": pids[pid], **process_delta(p, previous_processes.get(pid, {}), seconds)}
                                  for pid, p in processes.items()],
                })
                samples += 1
                previous_disks, previous_processes, previous_time = disks, processes, started
                if started >= heartbeat_at:
                    print(json.dumps({"time_utc": utc(), "resource_samples": samples,
                                      "processes": pids, "remaining_s": max(0, int(deadline - started))}), flush=True)
                    heartbeat_at = started + 60
                stop.wait(max(0, min(deadline - time.monotonic(), args.interval - (time.monotonic() - started))))
        finally:
            stop.set()
            worker.join()
            event = {"kind": "complete", "time_utc": utc(), "resource_samples": samples}
            emit(resource_file, event)
            print(json.dumps(event), flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--out-dir", required=True)
    parser.add_argument("--container", default="main-nitro-1")
    parser.add_argument("--rpc-url", default="http://127.0.0.1:1100")
    parser.add_argument("--metrics-url", default="http://127.0.0.1:6070/debug/metrics/prometheus")
    parser.add_argument("--minutes", type=float, default=60)
    parser.add_argument("--interval", type=float, default=1)
    parser.add_argument("--context-interval", type=float, default=15)
    args = parser.parse_args()
    if not Path("/proc/stat").exists():
        parser.error("Run on the Linux node host")
    if not all(math.isfinite(v) for v in (args.minutes, args.interval, args.context_interval)):
        parser.error("Timing arguments must be finite")
    if not (0 < args.minutes <= 360 and args.interval >= 1 and args.context_interval >= 10):
        parser.error("Require 0 < minutes <= 360, interval >= 1s, context-interval >= 10s")
    run(args)


if __name__ == "__main__":
    main()
