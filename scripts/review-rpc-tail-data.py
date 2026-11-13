#!/usr/bin/env python3
"""Read existing suite files and retained Docker logs; never call node RPC."""

import argparse
from collections import Counter, defaultdict
from datetime import datetime, timedelta, timezone
import importlib.util
import json
import math
import os
from pathlib import Path
import re
import subprocess
import tempfile
import threading


METHODS = ("eth_sendRawTransaction", "eth_getLogs")
OUTCOMES = ("success", "failure")
SUFFIXES = ("completed", "ge10ms", "ge25ms", "ge50ms", "ge100ms", "ge250ms",
            "ge500ms", "ge1s", "ge3s", "ge5s")
BASE_KEYS = tuple(f"rpc_diagnostics_{m}_{o}_{s}" for m in METHODS
                  for o in OUTCOMES for s in ("completed", "ge1s", "ge3s", "ge5s"))
DROP = "rpc_diagnostics_details_dropped"
SGT = timezone(timedelta(hours=8))
ANSI = re.compile(r"\x1b\[[0-9;]*m")
KV = re.compile(r'(?:^|\s)([A-Za-z][A-Za-z0-9_]*)=("(?:\\.|[^"\\])*"|\S+)')
STAGES = ("params", "callback", "encode", "handler_other", "resolve", "backend_setup",
          "sync_initial", "indexed_search", "result_filter", "unindexed_search", "sync_post",
          "consistency", "backend_close", "index_lock_wait", "index_lock_hold", "filter_rows_read",
          "block_pointer_access", "log_lookup", "receipts_access", "receipt_scan", "header_access",
          "block_logs_access", "body_access", "sync_register_wait", "sync_enqueue_wait", "sync_response_wait")
SHAPES = ("resolved_blocks_max", "unindexed_blocks_scanned", "result_count", "receipt_reads",
          "log_cache_hits", "log_cache_misses", "body_cache_hits", "body_cache_misses", "failure_kind")


def number(v):
    return type(v) in (int, float) and math.isfinite(v)


def stamp(v):
    if not isinstance(v, str):
        return None
    try:
        d = datetime.fromisoformat(v.replace("Z", "+00:00"))
        return d.timestamp() if d.tzinfo else None
    except ValueError:
        return None


def sgt(v):
    return datetime.fromtimestamp(v, SGT).isoformat() if v is not None else None


def iso_utc(v):
    return datetime.fromtimestamp(v, timezone.utc).isoformat()


def load_reader(root):
    spec = importlib.util.spec_from_file_location("saved_suite_reader", root / "tools/report-sequencer-context.py")
    reader = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(reader)
    return reader


def buckets(d, method, outcome):
    values = [d.get(f"rpc_diagnostics_{method}_{outcome}_{s}") for s in SUFFIXES]
    if (not all(type(v) is int and v >= 0 for v in values)
            or any(a < b for a, b in zip(values, values[1:]))):
        return None
    return [a - b for a, b in zip(values, values[1:] + [0])]


def scan_counters(rows, until):
    valid, skipped = [], Counter()
    previous = None
    for r in rows:
        t, seconds = stamp(r.get("time_utc")), r.get("interval_s")
        d = r.get("deltas") or {}
        if t is None:
            skipped["invalid_time"] += 1
            previous = None
            continue
        if t > until:
            continue
        reason = r.get("delta_unavailable")
        if not reason and (r.get("errors") or not number(seconds) or not 0 < seconds <= 40):
            reason = "errors_or_invalid_interval"
        if not reason and (not r.get("identity") or previous is None
                           or previous.get("identity") != r["identity"]
                           or previous.get("errors") or stamp(previous.get("time_utc")) >= t):
            reason = "adjacent_identity_or_time_unavailable"
        if not reason and not all(type(d.get(k)) is int and d[k] >= 0 for k in BASE_KEYS + (DROP,)):
            reason = "missing_or_invalid_delta"
        if reason:
            skipped[str(reason)] += 1
        else:
            merged = dict(d, **(r.get("subsecond_deltas") or {}))
            data = {}
            for method in METHODS:
                prefix = f"rpc_diagnostics_{method}_"
                b = None if r.get("subsecond_delta_unavailable") else buckets(merged, method, "success")
                f = None if r.get("subsecond_delta_unavailable") else buckets(merged, method, "failure")
                data[method] = {
                    "success": d[prefix + "success_completed"],
                    "failure": d[prefix + "failure_completed"],
                    "success_ge500ms": sum(b[6:]) if b is not None else None,
                    "success_500_to_1000ms": b[6] if b is not None else None,
                    "success_ge1s": d[prefix + "success_ge1s"],
                    "success_ge3s": d[prefix + "success_ge3s"],
                    "success_ge5s": d[prefix + "success_ge5s"],
                    "failure_ge500ms": sum(f[6:]) if f is not None else None,
                    "failure_500_to_1000ms": f[6] if f is not None else None,
                    "failure_ge1s": d[prefix + "failure_ge1s"],
                    "failure_ge3s": d[prefix + "failure_ge3s"],
                    "failure_ge5s": d[prefix + "failure_ge5s"],
                }
            valid.append({"begin": t - seconds, "end": t, "seconds": seconds,
                          "identity": r["identity"], "data": data, "details_dropped": d[DROP]})
        previous = r
    return valid, dict(skipped)


def totals(intervals, method):
    result = {k: sum(r["data"][method][k] for r in intervals) if intervals else None
              for k in ("success", "failure", "success_ge1s", "success_ge3s", "success_ge5s",
                        "failure_ge1s", "failure_ge3s", "failure_ge5s")}
    accepted = [r for r in intervals if r["data"][method]["success_ge500ms"] is not None]
    for k in ("success_ge500ms", "success_500_to_1000ms"):
        result[k] = sum(r["data"][method][k] for r in accepted) if accepted else None
    result["base_valid_seconds"] = round(sum(r["seconds"] for r in intervals), 3)
    result["ge500_valid_seconds"] = round(sum(r["seconds"] for r in accepted), 3)
    result["ge500_unavailable_intervals"] = len(intervals) - len(accepted)
    failure_accepted = [r for r in intervals if r["data"][method]["failure_ge500ms"] is not None]
    for k in ("failure_ge500ms", "failure_500_to_1000ms"):
        result[k] = sum(r["data"][method][k] for r in failure_accepted) if failure_accepted else None
    result["failure_ge500_valid_seconds"] = round(sum(r["seconds"] for r in failure_accepted), 3)
    result["failure_ge500_unavailable_intervals"] = len(intervals) - len(failure_accepted)
    completed = (result["success"] or 0) + (result["failure"] or 0)
    covered_success = sum(r["data"][method]["success"] for r in accepted)
    result["failure_pct"] = round(100 * result["failure"] / completed, 6) if completed else None
    result["ge500_pct_of_covered_success"] = round(100 * result["success_ge500ms"] / covered_success, 6) if covered_success else None
    return result


def select_hot(intervals):
    selected = {}
    for method in METHODS:
        for key, limit in (("failure", 4), ("success_ge500ms", 4), ("success_ge1s", 2),
                           ("failure_ge500ms", 4)):
            candidates = [r for r in intervals if (r["data"][method][key] or 0) > 0]
            for r in sorted(candidates, key=lambda x: (-x["data"][method][key], x["end"]))[:limit]:
                label = (r["begin"], r["end"])
                item = selected.setdefault(label, {**r, "selected_for": [], "resources": defaultdict(list),
                                                   "resource_rows": 0, "identity_mismatches": 0, "cache_rows": []})
                item["selected_for"].append(method + ":" + key)
        # Include every >=1s interval, even if it was not one of the largest bursts.
        for r in intervals:
            if r["data"][method]["success_ge1s"] or r["data"][method]["failure_ge1s"]:
                label = (r["begin"], r["end"])
                item = selected.setdefault(label, {**r, "selected_for": [], "resources": defaultdict(list),
                                                   "resource_rows": 0, "identity_mismatches": 0, "cache_rows": []})
                reason = method + ":any_ge1s"
                if reason not in item["selected_for"]:
                    item["selected_for"].append(reason)
    return [selected[k] for k in sorted(selected)]


def read_context(root, reader, hot, inputs):
    for path in sorted(root.glob("resources/segment-*/resources.jsonl")):
        rows, inputs[str(path.relative_to(root))] = reader.snapshot(path)
        rows = sorted((r for r in rows if r.get("kind") == "resources" and stamp(r.get("time_utc")) is not None),
                      key=lambda r: stamp(r["time_utc"]))
        for old, new in zip(rows, rows[1:]):
            a, b = stamp(old["time_utc"]), stamp(new["time_utc"])
            targets = [h for h in hot if a < h["end"] and b > h["begin"]]
            if not targets or not 0 < b - a <= 2.5:
                continue
            values = reader.flatten(new, old)
            values = {k: v for k, v in values.items() if k in (
                "host.cpu_busy_percent", "host.cpu_iowait_percent", "host.io_psi_some_avg10",
                "host.MemAvailable_GiB", "device.vdb.read_await_ms", "device.vdb.write_await_ms",
                "device.vdb.average_queue", "device.vdb.read_MiB_s", "device.vdb.write_MiB_s")
                or (k.startswith("process.") and k.endswith(("cpu_cores", "read_MiB_s", "write_MiB_s")))}
            main = [p for p in new.get("processes", []) if p.get("role") == "nitro" and p.get("available")]
            for h in targets:
                ident = h["identity"]
                if len(main) != 1 or (main[0].get("pid"), main[0].get("start_ticks")) != (ident.get("pid"), ident.get("start_ticks")):
                    h["identity_mismatches"] += 1
                    continue
                h["resource_rows"] += 1
                for k, v in values.items():
                    h["resources"][k].append(v)
    for path in sorted(root.glob("cache-io/segment-*/cache.jsonl")):
        rows, inputs[str(path.relative_to(root))] = reader.snapshot(path)
        for r in rows:
            t = stamp(r.get("time_utc"))
            if t is None:
                continue
            for h in hot:
                if h["begin"] - 15 <= t <= h["end"] + 15:
                    h["cache_rows"].append((t, r))


def context_output(h, reader):
    output = {"from_sgt": sgt(h["begin"]), "to_sgt": sgt(h["end"]),
              "selected_for": h["selected_for"], "rpc": h["data"],
              "resource_rows": h["resource_rows"], "identity_mismatches": h["identity_mismatches"],
              "resources": {k: reader.stats(v) for k, v in sorted(h["resources"].items())}}
    nearby = sorted(h["cache_rows"], key=lambda x: x[0])
    cache = [(t, r) for t, r in nearby if not r.get("errors") and r.get("identity") == h["identity"]]
    if not cache:
        output["cache_context"] = None
        return output
    def gauge(key, divisor=1):
        vals = [(r.get("metrics") or {}).get(key) for _, r in cache]
        vals = [v / divisor for v in vals if number(v)]
        return {"first": vals[0] if vals else None, "last": vals[-1] if vals else None, **reader.stats(vals)}
    counters = {}
    for key in ("l2chaindata_compact_writedelay_counter", "l2chaindata_compact_pacing_bypass",
                "l2chaindata_compact_pacing_output", "l2chaindata_compact_pacing_wait"):
        changes, covered = [], 0
        for (ta, a), (tb, b) in zip(nearby, nearby[1:]):
            va, vb = (a.get("metrics") or {}).get(key), (b.get("metrics") or {}).get(key)
            if (not a.get("errors") and not b.get("errors")
                    and a.get("identity") == b.get("identity") == h["identity"]
                    and 0 < tb - ta <= 40 and number(va) and number(vb) and 0 <= va <= vb):
                changes.append(vb - va)
                covered += tb - ta
        counters[key] = {"delta": sum(changes) if changes else None, "covered_seconds": round(covered, 3)}
    output["cache_context"] = {"from_sgt": sgt(cache[0][0]), "to_sgt": sgt(cache[-1][0]),
                               "unusable_nearby_samples": len(nearby) - len(cache),
                               "debt_GiB": gauge("l2chaindata_compact_debt", 1024**3),
                               "active_compactions": gauge("l2chaindata_compact_inprogress"), "counters": counters}
    return output


def parse_log(line):
    clean = ANSI.sub("", line).strip()
    parts = clean.split(None, 1)
    if len(parts) != 2 or stamp(parts[0]) is None:
        return None
    t, payload = stamp(parts[0]), parts[1]
    if payload.startswith("{"):
        try:
            f = json.loads(payload)
            if not isinstance(f, dict):
                return None
        except ValueError:
            return None
        msg = f.get("msg", f.get("message", ""))
    else:
        f = {}
        for k, v in KV.findall(payload):
            try:
                f[k] = json.loads(v) if v.startswith('"') else v
            except ValueError:
                f[k] = v.strip('"')
        msg = "RPC stage timings" if "RPC stage timings" in payload else next(
            ("Served " + m for m in METHODS if "Served " + m in payload), "")
    return (t, msg, f) if isinstance(msg, str) else None


def normalize_error(value):
    text = str(value)[:2000]
    text = re.sub(r"[A-Za-z][A-Za-z0-9+.-]*://\S+", "<url>", text)
    text = re.sub(r"0x[0-9a-fA-F]+", "<hex>", text)
    text = re.sub(r"\b[0-9a-fA-F]{32,}\b", "<hex>", text)
    text = re.sub(r"\b\d+(?:[.,]\d+)*\b", "<n>", text)
    return " ".join(text.split())[:400]


def numeric(v):
    try:
        n = float(str(v).replace(",", ""))
        return n if math.isfinite(n) and n >= 0 else None
    except (ValueError, TypeError):
        return None


def duration_ms(v):
    text = str(v)
    tokens = list(re.finditer(r"([0-9]+(?:\.[0-9]+)?)(ns|us|µs|μs|ms|s|m|h)", text))
    if not tokens or "".join(m.group(0) for m in tokens) != text:
        return None
    units = {"ns": 1e-6, "us": .001, "µs": .001, "μs": .001, "ms": 1, "s": 1000, "m": 60000, "h": 3600000}
    return sum(float(m[1]) * units[m[2]] for m in tokens)


def collect_logs(container, since, until, timeout=45):
    groups, stages = {}, defaultdict(list)
    stage_counts, notes = Counter(), Counter()
    first = last = None
    expired = threading.Event()
    scanned = 0
    with subprocess.Popen(["docker", "logs", "--timestamps", "--since", iso_utc(since),
                           "--until", iso_utc(until), container], stdout=subprocess.PIPE,
                          stderr=subprocess.STDOUT, text=True, errors="replace") as p:
        def expire():
            expired.set()
            if p.poll() is None:
                p.kill()  # Only this owned read-only Docker log client.
        timer = threading.Timer(timeout, expire)
        timer.start()
        try:
            while True:
                line = p.stdout.readline(2 * 1024**2 + 1)
                if not line:
                    break
                scanned += len(line.encode("utf-8", errors="replace"))
                if scanned > 256 * 1024**2:
                    notes["scan_byte_limit_reached"] += 1
                    p.kill()
                    break
                if not line.endswith("\n") and len(line) > 2 * 1024**2:
                    notes["oversized_line_scan_stopped"] += 1
                    p.kill()
                    break
                parsed = parse_log(line)
                if parsed is None:
                    continue
                t, msg, f = parsed
                if not since <= t <= until:
                    continue
                first, last = t if first is None else min(first, t), t if last is None else max(last, t)
                if msg.startswith("Served ") and msg[7:] in METHODS and "err" in f:
                    method, error = msg[7:], normalize_error(f["err"])
                    g = groups.setdefault((method, error), {"method": method, "error_group": error,
                        "records": 0, "first_sgt": sgt(t), "last_sgt": sgt(t), "duration_ms": [], "examples": []})
                    g["records"] += 1
                    g["last_sgt"] = sgt(t)
                    dur = duration_ms(f.get("duration"))
                    if dur is not None:
                        g["duration_ms"].append(dur)
                    if len(g["examples"]) < 3:
                        g["examples"].append({"time_sgt": sgt(t), "duration_ms": dur})
                if msg == "RPC stage timings" and f.get("method") in METHODS and f.get("outcome") in OUTCOMES:
                    key = (f["method"], f["outcome"])
                    stage_counts[key] += 1
                    total = numeric(f.get("total_ms"))
                    if total is None:
                        notes["stage_total_unparseable"] += 1
                        continue
                    if key[1] == "success" and total < 500:
                        continue
                    item = {"time_sgt": sgt(t), "method": key[0], "outcome": key[1], "total_ms": total}
                    for name in ("trace",) + tuple(s + "_ms" for s in STAGES) + SHAPES:
                        v = numeric(f.get(name))
                        if v is not None:
                            item[name] = int(v) if name == "trace" else v
                    stages[key].append(item)
                    stages[key].sort(key=lambda x: x["total_ms"], reverse=True)
                    del stages[key][8:]
        finally:
            timer.cancel()
            if p.poll() is None:
                try:
                    p.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    p.kill()
            p.wait(timeout=5)
    for g in groups.values():
        vals = g.pop("duration_ms")
        g["duration_ms"] = {"n": len(vals), "min": min(vals) if vals else None,
                            "max": max(vals) if vals else None}
    return {"container_id": container, "query_timed_out": expired.is_set(), "returncode": p.returncode,
            "scanned_bytes": scanned, "first_retained_log_sgt": sgt(first), "last_retained_log_sgt": sgt(last),
            "notes": dict(notes), "failure_log_groups": sorted(groups.values(), key=lambda g: -g["records"]),
            "stage_records": {m + ":" + o: n for (m, o), n in stage_counts.items()},
            "slow_stage_examples": [r for group in stages.values() for r in group]}


def build_report(root, until, read_logs=True):
    reader = load_reader(root)
    inputs = {}
    rows, inputs["rpc-counters/counters.jsonl"] = reader.snapshot(root / "rpc-counters/counters.jsonl")
    intervals, skipped = scan_counters(rows, until)
    if not intervals:
        raise ValueError("No valid counter intervals before the requested cutoff")
    hot = select_hot(intervals)
    read_context(root, reader, hot, inputs)
    events, inputs["slow-blocks/events.jsonl"] = reader.snapshot(root / "slow-blocks/events.jsonl")
    saved = defaultdict(list)
    for e in events:
        t = stamp(e.get("log_time_utc"))
        if (e.get("kind") == "rpc_slow" and e.get("method") in METHODS and t is not None
                and intervals[0]["begin"] <= t <= intervals[-1]["end"]
                and number(e.get("total_ms")) and (e.get("outcome") == "failure" or e["total_ms"] >= 500)):
            saved[e["method"]].append({"time_sgt": sgt(t), "trace": e.get("trace"),
                "outcome": e.get("outcome"), "total_ms": e["total_ms"], "callback_ms": e.get("callback_ms")})
    hourly = defaultdict(list)
    for r in intervals:
        hourly[datetime.fromtimestamp(r["end"], SGT).strftime("%Y-%m-%dT%H:00+08:00")].append(r)
    ids = sorted({r["identity"]["id"] for r in intervals})
    report = {
        "window": {"from_sgt": sgt(intervals[0]["begin"]), "to_sgt": sgt(intervals[-1]["end"]),
                   "requested_until_sgt": sgt(until), "valid_intervals": len(intervals), "skipped": skipped,
                   "details_dropped_delta_shared": sum(r["details_dropped"] for r in intervals), "container_ids": ids},
        "totals": {m: totals(intervals, m) for m in METHODS},
        "hourly": [{"interval_end_hour_sgt": h, "methods": {m: totals(rs, m) for m in METHODS}}
                   for h, rs in sorted(hourly.items())],
        "saved_slow_events": {m: {"records": len(saved[m]), "largest": sorted(saved[m], key=lambda r: -r["total_ms"])[:8]}
                              for m in METHODS},
        "hot_intervals": [context_output(h, reader) for h in hot],
        "input_status": inputs,
        "limits": [
            "ge500ms means >=500ms; the stored thresholds cannot isolate calls at exactly 500ms.",
            "Success tails and failures are separate; ge1s is already included in ge500ms.",
            "Counter windows are about 15 seconds and use scrape times, not individual request timestamps.",
            "Hours are grouped by interval end; intervals crossing hour boundaries are not split.",
            "Logs retain only available Docker records; rotation and threshold/rate-limited details cannot be reconstructed.",
            "At a 1s stage threshold, individual 500-1000ms stage details were not saved.",
            "Served errors, stage logs and saved slow events overlap; never add their counts as separate requests.",
            "Stage log times can lag completion; nested stage durations overlap and are not additive wait budgets.",
            "Hardware overlaps do not establish causality; device IO is shared and process rates are not per-RPC costs.",
            "Cache context extends 15s around each counter interval and reports its actual coverage; gaps are excluded.",
            "Failure groups redact addresses, URLs and numeric values; a failed RPC is not necessarily a node fault.",
        ],
    }
    report["retained_logs"] = []
    if read_logs:
        for container in ids:
            try:
                report["retained_logs"].append(collect_logs(container, intervals[0]["begin"], intervals[-1]["end"]))
            except (OSError, subprocess.SubprocessError) as exc:
                report["retained_logs"].append({"container_id": container, "unavailable": type(exc).__name__})
    return report


def print_report(report):
    print("WINDOW", json.dumps(report["window"], ensure_ascii=False))
    for method, values in report["totals"].items():
        print("TOTAL", json.dumps({"method": method, **values}, ensure_ascii=False))
    for h in report["hourly"]:
        print("HOUR", json.dumps(h, ensure_ascii=False))
    for log in report["retained_logs"]:
        print("LOG_COVERAGE", json.dumps({k: v for k, v in log.items() if k not in (
            "failure_log_groups", "slow_stage_examples")}, ensure_ascii=False))
        for g in log.get("failure_log_groups", []):
            print("FAILURE_GROUP", json.dumps(g, ensure_ascii=False))
        for stage in log.get("slow_stage_examples", []):
            print("SLOW_STAGE", json.dumps(stage, ensure_ascii=False))
    for method, events in report["saved_slow_events"].items():
        print("SAVED_SLOW", json.dumps({"method": method, **events}, ensure_ascii=False))
    for h in report["hot_intervals"]:
        compact = dict(h)
        compact["resources"] = {k: {"avg": v["mean"], "max": v["max"], "n": v["n"]}
                                for k, v in h["resources"].items()}
        print("HOT_INTERVAL", json.dumps(compact, ensure_ascii=False))
    print("INPUT_QUALITY", json.dumps({"files": len(report["input_status"]),
        "malformed_or_partial": sum(s["malformed_or_partial"] for s in report["input_status"].values()),
        "oversized": sum(s["oversized"] for s in report["input_status"].values())}, ensure_ascii=False))


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--suite-dir", type=Path, required=True)
    p.add_argument("--until", required=True, help="Fixed timezone-aware cutoff")
    p.add_argument("--skip-logs", action="store_true")
    args = p.parse_args()
    until = stamp(args.until)
    if until is None:
        p.error("--until requires a timezone-aware timestamp")
    os.umask(0o077)
    root = args.suite_dir.resolve()
    report = build_report(root, until, not args.skip_logs)
    print_report(report)
    output = Path(tempfile.mkdtemp(prefix="rpc-deep-review-", dir=root))
    (output / "report.json").write_text(json.dumps(report, ensure_ascii=False, indent=2, allow_nan=False) + "\n")
    print("REPORT_JSON", str(output / "report.json"))


if __name__ == "__main__":
    main()
