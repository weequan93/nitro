#!/usr/bin/env python3
"""Join saved sequencer resources, rolling metrics and logged slow blocks.

Offline only: no Docker commands, RPC requests, profile or replay. Resources
outside logged slow intervals are a comparison group, not proven fast work.
"""

import argparse
from bisect import bisect_left
from collections import Counter, defaultdict
from datetime import datetime, timedelta, timezone
import json
import math
from pathlib import Path


SGT = timezone(timedelta(hours=8))


def number(value):
    return isinstance(value, (int, float)) and not isinstance(value, bool) and math.isfinite(value)


def stamp(value):
    if not isinstance(value, str):
        return None
    try:
        parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
        return parsed.timestamp() if parsed.tzinfo else None
    except ValueError:
        return None


def sgt(value):
    return datetime.fromtimestamp(value, SGT).isoformat() if value is not None else None


def snapshot(path):
    size = path.stat().st_size
    if size > 512 * 1024**2:
        raise ValueError(str(path) + " exceeds 512 MiB report input limit")
    rows, malformed, oversized = [], 0, 0
    with path.open("rb") as stream:
        remaining = size
        while remaining:
            line = stream.readline(min(remaining, 2 * 1024**2 + 1))
            if not line:
                break
            remaining -= len(line)
            if len(line) > 2 * 1024**2:
                oversized += 1
                while remaining and not line.endswith(b"\n"):
                    line = stream.readline(min(remaining, 2 * 1024**2 + 1))
                    if not line:
                        break
                    remaining -= len(line)
                continue
            try:
                obj = json.loads(line)
                if not isinstance(obj, dict):
                    raise ValueError("not an object")
                rows.append(obj)
            except (ValueError, UnicodeError):
                malformed += 1
    return rows, {"bytes": size, "malformed_or_partial": malformed, "oversized": oversized}


def stats(values):
    values = sorted(v for v in values if number(v))
    return {"n": len(values), "mean": round(sum(values) / len(values), 3) if values else None,
            "min": round(values[0], 3) if values else None,
            "max": round(values[-1], 3) if values else None}


def merge(intervals):
    result = []
    for start, end in sorted(intervals):
        if result and start <= result[-1][1]:
            result[-1] = (result[-1][0], max(end, result[-1][1]))
        else:
            result.append((start, end))
    return result


def overlaps(intervals, starts, start, end):
    index = bisect_left(starts, end) - 1
    return index >= 0 and intervals[index][1] > start


def flatten(row, previous):
    values = {}
    def put(key, value, divisor=1):
        if number(value):
            values[key] = value / divisor
    ticks, old_ticks = row.get("cpu_ticks"), (previous or {}).get("cpu_ticks")
    if isinstance(ticks, list) and isinstance(old_ticks, list) and len(ticks) == len(old_ticks) == 8:
        delta = [a - b for a, b in zip(ticks, old_ticks)]
        total = sum(delta)
        if total > 0 and min(delta) >= 0:
            put("host.cpu_busy_percent", (total - delta[3] - delta[4]) / total * 100)
            put("host.cpu_iowait_percent", delta[4] / total * 100)
            put("host.cpu_steal_percent", delta[7] / total * 100)
    mem = row.get("meminfo_bytes") or {}
    for key in ("MemAvailable", "SwapFree"):
        put("host." + key + "_GiB", mem.get(key), 1024**3)
    for resource in ("cpu", "memory", "io"):
        for kind in ("some", "full"):
            value = (((row.get("pressure") or {}).get(resource) or {}).get(kind) or {}).get("avg10")
            try:
                put("host." + resource + "_psi_" + kind + "_avg10", float(value))
            except (ValueError, TypeError):
                pass
    for proc in row.get("processes") or []:
        if not proc.get("available"):
            continue
        # Keep the two validators separate; PID reuse must not join processes.
        key = "process.%s.pid%s.start%s." % (proc.get("role"), proc.get("pid"), proc.get("start_ticks"))
        rates = proc.get("rates") or {}
        put(key + "cpu_cores", rates.get("cpu_cores"))
        put(key + "rss_GiB", proc.get("rss_bytes"), 1024**3)
        put(key + "state_D", 1 if proc.get("state") == "D" else 0)
        put(key + "major_faults_s", rates.get("major_faults_s"))
        for operation in ("read", "write"):
            put(key + operation + "_MiB_s", rates.get(operation + "_bytes_s"), 1024**2)
        seconds = row.get("interval_s")
        if number(seconds) and seconds > 0:
            for counter, unit in (("nr_throttled", "throttled_periods_s"), ("throttled_usec", "throttled_ms_s")):
                value = rates.get(counter + "_delta")
                if number(value):
                    put(key + unit, value / seconds / (1000 if counter == "throttled_usec" else 1))
    for device, counters in (row.get("devices") or {}).items():
        for key in ("read_await_ms", "write_await_ms", "busy_percent", "average_queue"):
            put("device." + device + "." + key, counters.get(key))
        for operation in ("read", "write"):
            put("device." + device + "." + operation + "_MiB_s", counters.get(operation + "_bytes_s"), 1024**2)
    return values


def summarize(samples):
    values = defaultdict(list)
    for row in samples:
        for key, value in row["values"].items():
            values[key].append(value)
    return {key: stats(value) for key, value in sorted(values.items())}


def slow_block_info(event, matching_samples):
    transactions = [t for t in event.get("transactions", []) if t.get("type") != "0x6a"]
    gas = [t.get("receipt_gas_used") for t in event.get("transactions", [])]
    resource_values = summarize(matching_samples)
    return {
        "end_sgt": sgt(event["end"]), "start_sgt": sgt(event["begin"]),
        "block": event.get("block_number"), "block_hash": event.get("block_hash"),
        "sequence_ms": round(event["sequence_elapsed_ms"], 3),
        "gas_used": sum(gas) if gas and all(number(v) for v in gas) else None,
        "user_transactions": len(transactions),
        "targets_and_selectors": dict(Counter(str(t.get("to")) + "/" + str(t.get("selector")) for t in transactions)),
        "input_bytes": [t.get("input_bytes") for t in transactions],
        "resource_samples": len(matching_samples),
        "resource_window_clipped": event["clipped"],
        "resource_values": {key: value for key, value in resource_values.items()
                            if key.startswith(("host.", "device.")) or
                            key.endswith(("cpu_cores", "read_MiB_s", "write_MiB_s", "throttled_ms_s"))},
    }


def build_report(resource_rows, context_rows, event_rows, guard=15, top=3):
    resources = sorted((row for row in resource_rows if row.get("kind") == "resources"
                        and stamp(row.get("time_utc")) is not None), key=lambda r: stamp(r["time_utc"]))
    if not resources:
        raise ValueError("No timestamped resources available")
    first, last = stamp(resources[0]["time_utc"]), stamp(resources[-1]["time_utc"])
    events = {}
    for event in event_rows:
        end = stamp(event.get("log_time_utc"))
        elapsed = event.get("sequence_elapsed_ms")
        if event.get("kind") != "slow_sequence_block" or end is None or not number(elapsed) or elapsed <= 0:
            continue
        begin = end - elapsed / 1000
        if begin < last and end > first:
            events[(event.get("block_number"), event.get("block_hash"))] = {
                **event, "begin": begin, "end": end, "clipped": begin < first or end > last}
    events = list(events.values())
    intervals = merge([(e["begin"], e["end"]) for e in events])
    starts = [a for a, _ in intervals]
    protected = merge([(a - guard, b + guard) for a, b in intervals])
    protected_starts = [a for a, _ in protected]
    groups = {"during_logged_slow_sequence": [], "outside_slow_sequence_guard": []}
    flat, previous = [], None
    for row in resources:
        end = stamp(row["time_utc"])
        # The first disk interval predates our first process sample; omit it.
        if previous is not None:
            begin = stamp(previous["time_utc"])
            sample = {"begin": begin, "end": end, "values": flatten(row, previous)}
            flat.append(sample)
            if overlaps(intervals, starts, begin, end):
                groups["during_logged_slow_sequence"].append(sample)
            elif not overlaps(protected, protected_starts, begin, end):
                groups["outside_slow_sequence_guard"].append(sample)
        previous = row
    summaries = {key: summarize(value) for key, value in groups.items()}
    keys = sorted(set().union(*(summary.keys() for summary in summaries.values())))
    contexts = [r for r in context_rows if (stamp(r.get("started_utc")) is not None and
                                           first <= stamp(r["started_utc"]) <= last)]
    snapshots = {}
    for stage in ("publisher", "queue_pop", "sequence", "block_execution", "block_write", "consensus_write", "nonce_retry_wait"):
        key = 'arb_txpublish_%s{quantile="0.999"}' % stage
        snapshots[stage + "_snapshot_p999_ms"] = stats([
            r["metrics"][key] / 1000 for r in contexts if number((r.get("metrics") or {}).get(key))])
    controls = {(r["block"].get("block_number"), r["block"].get("block_hash")): r["block"] for r in contexts
                if isinstance(r.get("block"), dict)}
    slow_keys = {(e.get("block_number"), e.get("block_hash")) for e in events}
    control_prices = [t["price_entries"] for block in controls.values() for t in block.get("transactions", [])
                      if number(t.get("price_entries"))]
    rpc_events = [e for e in event_rows if e.get("kind") == "rpc_slow" and e.get("method") == "eth_sendRawTransaction"
                  and stamp(e.get("log_time_utc")) is not None and first <= stamp(e["log_time_utc"]) <= last]
    gaps = [sample["end"] - sample["begin"] for sample in flat]
    requested = next((r.get("minutes") for r in resource_rows if r.get("kind") == "start"), None)
    return {
        "coverage": {
            "first_resource_sgt": sgt(first), "last_resource_sgt": sgt(last),
            "observed_minutes": round((last - first) / 60, 2), "requested_minutes": requested,
            "collector_final_record_present": any(r.get("kind") == "complete" for r in resource_rows),
            "resources": len(resources), "context_samples": len(contexts),
            "largest_resource_gap_s": round(max(gaps), 3) if gaps else None,
            "resource_gaps_over_2_5s": sum(g > 2.5 for g in gaps),
            "metrics_errors": sum(bool(r.get("metrics_error")) for r in contexts),
            "block_lookup_errors": sum(bool(r.get("block_error")) for r in contexts),
            "docker_top_errors": sum(bool(r.get("docker_top_error")) for r in resources),
            "logged_slow_blocks": len(events), "slow_rpc_log_records": len(rpc_events),
            "slow_rpc_failures": sum(e.get("outcome") == "failure" for e in rpc_events),
            "group_sample_counts": {key: len(rows) for key, rows in groups.items()}, "guard_s": guard,
        },
        "resource_comparison": {key: {group: summary.get(key, stats([])) for group, summary in summaries.items()} for key in keys},
        "metric_snapshots": snapshots,
        "workload_controls": {"sampled_blocks_unclassified": len(controls),
                              "also_in_logged_slow_blocks": len(set(controls) & slow_keys),
                              "block_gas_used": stats([block.get("block_gas_used") for block in controls.values()]),
                              "oracle_price_entries_per_tx": stats(control_prices)},
        "slowest_blocks": [slow_block_info(e, [s for s in flat if s["begin"] < e["end"] and s["end"] > e["begin"]])
                           for e in sorted(events, key=lambda e: e["sequence_elapsed_ms"], reverse=True)[:top]],
        "limits": [
            "Slow blocks depend on retained watcher logs; an absent warning does not prove a fast block.",
            "Resource groups are interval overlaps with logged sequencing; sample means are unweighted and not causal attribution.",
            "PSI avg10 and metric p999 snapshots have overlapping windows; do not add/subtract independent quantiles.",
            "Devices and per-PID cgroups remain separate; neither shared disks nor summed throttle budgets are inferred.",
            "Periodic latest blocks are unclassified controls with unknown individual execution times.",
        ],
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--capture-dir", type=Path, required=True)
    parser.add_argument("--events-file", type=Path, required=True)
    parser.add_argument("--guard-seconds", type=float, default=15)
    parser.add_argument("--top", type=int, default=3)
    parser.add_argument("--json", action="store_true", help="Print full JSON instead of compact JSON lines")
    args = parser.parse_args()
    if not math.isfinite(args.guard_seconds) or args.guard_seconds < 0 or not 0 <= args.top <= 20:
        parser.error("guard-seconds must be nonnegative; top must be 0..20")
    resource_rows, resource_status = snapshot(args.capture_dir / "resources.jsonl")
    context_rows, context_status = snapshot(args.capture_dir / "context.jsonl")
    event_rows, event_status = snapshot(args.events_file)
    report = build_report(resource_rows, context_rows, event_rows, args.guard_seconds, args.top)
    report["input_snapshots"] = {"resources": resource_status, "context": context_status, "events": event_status}
    if args.json:
        print(json.dumps(report, indent=2, allow_nan=False))
        return
    print("COVERAGE", json.dumps(report["coverage"], separators=(",", ":")))
    print("INPUTS", json.dumps(report["input_snapshots"], separators=(",", ":")))
    print("METRIC_SNAPSHOTS", json.dumps(report["metric_snapshots"], separators=(",", ":")))
    print("WORKLOAD_CONTROLS", json.dumps(report["workload_controls"], separators=(",", ":")))
    for key, groups in report["resource_comparison"].items():
        print("RESOURCE", json.dumps({"metric": key, **groups}, separators=(",", ":")))
    for block in report["slowest_blocks"]:
        print("SLOW_BLOCK", json.dumps({key: value for key, value in block.items() if key != "resource_values"},
                                       separators=(",", ":")))
    print("LIMITS", json.dumps(report["limits"], separators=(",", ":")))


if __name__ == "__main__":
    main()
