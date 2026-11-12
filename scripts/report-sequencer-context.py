#!/usr/bin/env python3
"""Join saved sequencer resources, rolling metrics and logged slow blocks.

Offline only: no Docker commands, RPC requests, profile or replay. Resources
outside logged slow intervals are a comparison group, not proven fast work.
"""

import argparse
from bisect import bisect_left, bisect_right
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


def background_activity(contexts, resources, events, rpc_events, resource_intervals):
    """Adjacent snapshot deltas with stable native-process/resource coverage.

    Metric read times are approximated by context start times. Stage durations
    are elapsed nanoseconds, not CPU time or a per-request causal budget.
    """
    points = sorted(resources, key=lambda r: stamp(r["time_utc"]))
    times = [stamp(r["time_utc"]) for r in points]
    contexts = sorted(contexts, key=lambda r: stamp(r["started_utc"]))
    prefix = "state_snapshot_generation_"
    keys = {k for r in contexts for k in (r.get("metrics") or {}) if k.startswith(prefix)}
    keys.update(("l2chaindata_compact_input", "l2chaindata_compact_output", "l2chaindata_compact_writedelay_counter"))
    totals = {k: {"valid_intervals": 0, "missing_intervals": 0, "reset_intervals": 0,
                  "delta_total": 0, "covered_seconds": 0} for k in keys}
    skipped = Counter()
    intervals = []
    for previous, current in zip(contexts, contexts[1:]):
        begin, end = stamp(previous["started_utc"]), stamp(current["started_utc"])
        seconds = end - begin
        if not 0 < seconds <= 30:
            skipped["nonpositive_or_over_30s_gap"] += 1
            continue
        lo, hi = bisect_right(times, begin) - 1, bisect_left(times, end)
        if lo < 0 or hi >= len(points):
            skipped["resource_boundary_unavailable"] += 1
            continue
        surrounding = points[lo:hi + 1]
        identities = []
        for row in surrounding:
            main = [p for p in row.get("processes") or [] if p.get("role") == "nitro" and p.get("available")]
            identities.append((main[0].get("pid"), main[0].get("start_ticks")) if len(main) == 1 else None)
        if any(identity is None or None in identity for identity in identities) or len(set(identities)) != 1:
            skipped["main_identity_missing_or_changed"] += 1
            continue
        if any(b - a > 2.5 for a, b in zip(times[lo:hi], times[lo + 1:hi + 1])):
            skipped["resource_gap_over_2_5s"] += 1
            continue
        old, new = previous.get("metrics") or {}, current.get("metrics") or {}
        if previous.get("metrics_error") or current.get("metrics_error"):
            skipped["metrics_read_error"] += 1
            continue
        delta = {}
        for key in keys:
            a, b = old.get(key), new.get(key)
            if not number(a) or not number(b):
                totals[key]["missing_intervals"] += 1
            elif b < a:
                totals[key]["reset_intervals"] += 1
            else:
                delta[key] = b - a
                totals[key]["valid_intervals"] += 1
                totals[key]["delta_total"] += b - a
                totals[key]["covered_seconds"] += seconds
        def change(key, divisor=1):
            value = delta.get(key)
            return value / divisor if value is not None else None
        def finite_value(key, divisor=1):
            value = new.get(key)
            return value / divisor if number(value) else None
        generated = change(prefix + "storage_generated")
        recovered = change(prefix + "storage_recovered")
        progress = (True if any(v is not None and v > 0 for v in (generated, recovered)) else
                    False if generated is not None and recovered is not None else None)
        compact_deltas = [change("l2chaindata_compact_" + kind) for kind in ("input", "output")]
        compact_gauges = [m.get("l2chaindata_compact_inprogress") for m in (old, new)]
        compact = (True if any(number(v) and v > 0 for v in compact_deltas + compact_gauges) else
                   False if all(number(v) and v == 0 for v in compact_deltas + compact_gauges) else None)
        intervals.append({
            "begin": begin, "end": end, "seconds": seconds,
            "snapshot_storage_progress": progress, "compaction_evidence": compact,
            "storage_generated_delta": generated, "storage_recovered_delta": recovered,
            "storage_prove_s": change(prefix + "duration_storage_prove", 1e9),
            "storage_trie_read_s": change(prefix + "duration_storage_trieread", 1e9),
            "storage_snapshot_read_s": change(prefix + "duration_storage_snapread", 1e9),
            "storage_write_s": change(prefix + "duration_storage_write", 1e9),
            "compaction_input_MiB_s": change("l2chaindata_compact_input", seconds * 1024**2),
            "compaction_output_MiB_s": change("l2chaindata_compact_output", seconds * 1024**2),
            "compactions_at_end": finite_value("l2chaindata_compact_inprogress"),
            "debt_GiB_at_end": finite_value("l2chaindata_compact_debt", 1024**3),
            "write_stalls_delta": change("l2chaindata_compact_writedelay_counter"),
        })
    for key, result in totals.items():
        divisor = 1e9 if key.startswith(prefix + "duration_") else 1024**2 if key in (
            "l2chaindata_compact_input", "l2chaindata_compact_output") else 1
        result["unit"] = "elapsed_stage_seconds" if divisor == 1e9 else "MiB" if divisor == 1024**2 else "count"
        if result["valid_intervals"]:
            result["delta_total"] = round(result["delta_total"] / divisor, 6)
            result["mean_rate_per_s"] = round(result["delta_total"] / result["covered_seconds"], 6)
        else:
            result["delta_total"] = result["mean_rate_per_s"] = None
        result["covered_seconds"] = round(result["covered_seconds"], 3)
    def interval_view(row):
        return {"start_sgt": sgt(row["begin"]), "end_sgt": sgt(row["end"]),
                **{k: round(v, 6) if type(v) is float else v for k, v in row.items() if k not in ("begin", "end")}}
    def nearby(begin, end):
        return [interval_view(r) for r in intervals if r["begin"] < end and r["end"] > begin]
    def rpc_resources(event):
        end = stamp(event["log_time_utc"])
        begin = end - event["total_ms"] / 1000
        rows = [r for r in resource_intervals if r["begin"] < end and r["end"] > begin]
        values = summarize(rows)
        return {"samples": len(rows), "window_clipped": begin < times[0] or end > times[-1],
                "values": {k: v for k, v in values.items() if
                           k.startswith("host.") or
                           k.startswith("device.") and not k.endswith("busy_percent") or
                           k.startswith("process.nitro.") and k.endswith((
                               "cpu_cores", "read_MiB_s", "write_MiB_s", "throttled_ms_s"))}}
    def gauge(key, divisor=1):
        values = [r["metrics"][key] / divisor for r in contexts if number((r.get("metrics") or {}).get(key))]
        return {"first": values[0] if values else None, "last": values[-1] if values else None, **stats(values)}
    combinations = []
    for progress in (True, False, None):
        for compact in (True, False, None):
            rows = [r for r in intervals if r["snapshot_storage_progress"] is progress and r["compaction_evidence"] is compact]
            if rows:
                combinations.append({"snapshot_storage_progress": progress, "compaction_evidence": compact,
                                     "intervals": len(rows), "seconds": round(sum(r["seconds"] for r in rows), 3)})
    return {
        "adjacent_context_pairs": max(0, len(contexts) - 1), "usable_intervals": len(intervals),
        "snapshot_metric_names": sum(k.startswith(prefix) for k in keys),
        "skipped_intervals": dict(skipped), "activity_combinations": combinations,
        "counters": dict(sorted(totals.items())),
        "debt_GiB": gauge("l2chaindata_compact_debt", 1024**3),
        "compactions": gauge("l2chaindata_compact_inprogress"),
        "slow_block_context": [{"block": e.get("block_number"), "sequence_ms": e["sequence_elapsed_ms"],
                                "intervals": nearby(e["begin"], e["end"])}
                               for e in sorted(events, key=lambda e: e["sequence_elapsed_ms"], reverse=True)[:3]],
        "slow_rpc_context": [{"end_sgt": sgt(stamp(e["log_time_utc"])), "outcome": e.get("outcome"),
                              "total_ms": e["total_ms"], "resources": rpc_resources(e), "intervals": nearby(
                                  stamp(e["log_time_utc"]) - e["total_ms"] / 1000, stamp(e["log_time_utc"]))}
                             for e in sorted((e for e in rpc_events if number(e.get("total_ms"))),
                                             key=lambda e: e["total_ms"], reverse=True)[:5]],
        "limits": [
            "Adjacent counter intervals use approximate metric-read times and require continuous stable main PID/start-time coverage.",
            "Full context intervals overlap slow requests/blocks; their deltas are not apportioned to those requests or proof of causality.",
            "No storage-counter advance or compaction evidence does not prove a background task stopped or completed.",
            "Stage durations can overlap; they are not CPU time, disk time, physical bytes, or additive wait budgets.",
            "Compaction input/output totals are booked at job completion; short-interval deltas are not instantaneous throughput. Pacing/output counts output writes as they occur.",
            "Compaction debt is a gauge, not a monotonic counter.",
        ],
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
        "background_activity": background_activity(contexts, resources, events, rpc_events, flat),
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
    background = report["background_activity"]
    print("BACKGROUND", json.dumps({k: v for k, v in background.items()
                                    if k not in ("slow_block_context", "slow_rpc_context")}, separators=(",", ":")))
    for block in background["slow_block_context"]:
        print("SLOW_BLOCK_BACKGROUND", json.dumps(block, separators=(",", ":")))
    for rpc in background["slow_rpc_context"]:
        print("SLOW_RPC_BACKGROUND", json.dumps(rpc, separators=(",", ":")))
    for key, groups in report["resource_comparison"].items():
        print("RESOURCE", json.dumps({"metric": key, **groups}, separators=(",", ":")))
    for block in report["slowest_blocks"]:
        print("SLOW_BLOCK", json.dumps({key: value for key, value in block.items() if key != "resource_values"},
                                       separators=(",", ":")))
    print("LIMITS", json.dumps(report["limits"], separators=(",", ":")))


if __name__ == "__main__":
    main()
