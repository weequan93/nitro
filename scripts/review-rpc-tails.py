#!/usr/bin/env python3
"""Join saved >=500ms RPC counters, resources, publisher snapshots and log archives."""

import argparse
from datetime import datetime, timezone
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile


DIRECTORY = Path(__file__).resolve().parent
PUBLISH_STAGES = ("state_at", "precheck", "filter", "publisher", "queue_enqueue", "queue_wait",
                  "queue_pop", "nonce_retry_wait", "sequence", "block_execution", "consensus_write", "block_write")
PUBLISH_AUX = ("arb_txpublish_inflight", "arb_sequencer_queue_length", "arb_sequencer_noncefailurecache_size",
               "arb_sequencer_noncecache_rejected", "arb_sequencer_noncecache_hit", "arb_sequencer_noncecache_miss")


def module(name, filename):
    spec = importlib.util.spec_from_file_location(name, DIRECTORY / filename)
    value = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(value)
    return value


def emit(kind, value):
    print(kind, json.dumps(value, ensure_ascii=False, allow_nan=False), flush=True)


def require_single_instance(intervals):
    if len({json.dumps(row["identity"], sort_keys=True) for row in intervals}) != 1:
        raise ValueError("Split the review at the node identity change before joining archive traces")


def publisher_context(root, reader, hot, inputs):
    rows = []
    for path in sorted(root.glob("resources/segment-*/context.jsonl")):
        data, inputs[str(path.relative_to(root))] = reader.snapshot(path)
        rows.extend(r for r in data if reader.stamp(r.get("started_utc")) is not None)
    rows.sort(key=lambda r: reader.stamp(r["started_utc"]))
    result = []
    for interval in hot:
        selected = []
        seen = set()
        for target in (reader.stamp(interval["from_sgt"]) - 15,
                       reader.stamp(interval["to_sgt"]) + 15):
            if not rows:
                break
            row = min(rows, key=lambda r: abs(reader.stamp(r["started_utc"]) - target))
            when = reader.stamp(row["started_utc"])
            if when in seen or abs(when - target) > 20:
                continue
            seen.add(when)
            metrics = {} if row.get("metrics_error") else (row.get("metrics") or {})

            def value(key, divisor=1):
                number = metrics.get(key)
                return round(number / divisor, 3) if reader.number(number) else None

            block = row.get("block") or {}
            txs = block.get("transactions")
            selected.append({"sample_sgt": reader.sgt(when), "target_offset_s": round(when - target, 3),
                "metrics_error": row.get("metrics_error"),
                "rolling_stage_quantiles": {stage: {
                    "count": value("arb_txpublish_" + stage + "_count"),
                    **{label: value("arb_txpublish_" + stage + '{quantile="' + q + '"}', 1000)
                       for label, q in (("p50_ms", "0.5"), ("p99_ms", "0.99"), ("p999_ms", "0.999"))}}
                    for stage in PUBLISH_STAGES},
                "auxiliary": {key: value(key) for key in PUBLISH_AUX},
                "periodic_block": {"block": block.get("block_number"), "gas_used": block.get("block_gas_used"),
                    "sampled_transactions": len(txs) if isinstance(txs, list) else None,
                    "oracle_price_entries": [t["price_entries"] for t in txs or [] if t.get("price_entries") is not None],
                    "block_error": row.get("block_error")}})
        result.append({"from_sgt": interval["from_sgt"], "to_sgt": interval["to_sgt"], "samples": selected})
    return result


def archive_report(root, log_dir, output, data, archive):
    since, until = archive.stamp(data["window"]["from_sgt"]), archive.stamp(data["window"]["to_sgt"])
    reader = data["_reader"]
    events, quality = reader.snapshot(root / "slow-blocks/events.jsonl")
    anchors = {(row.get("method"), archive.integer(row.get("trace"))): archive.stamp(row.get("log_time_utc"))
               for row in events if row.get("kind") == "rpc_slow"
               and archive.integer(row.get("trace")) is not None
               and archive.stamp(row.get("log_time_utc")) is not None}
    snapshot, manifest = archive.snapshot_logs(log_dir, output)
    emit("LOG_SNAPSHOT", str(snapshot))
    records, scan_notes, scanned = archive.read_records(snapshot, manifest)
    offset, calibration = archive.infer_offset(records, anchors)
    failures, stages, notes = archive.analyze(records, offset, since, until)
    for item in manifest:
        times = [archive.timestamp(token, offset, (since + until) / 2)
                 for token in item.pop("boundary_tokens", [])]
        times = [t for t in times if t is not None]
        item["first_sgt"] = archive.sgt(min(times)) if times else None
        item["last_sgt"] = archive.sgt(max(times)) if times else None
    groups, senders = archive.summarize(failures, stages)
    comparison = []
    for method in archive.METHODS:
        expected = data["totals"][method]
        found_failure = sum(row["method"] == method for row in failures)
        entry = {"method": method, "counter_failures": expected["failure"],
                 "archive_failure_records": found_failure, "failure_counts_match": found_failure == expected["failure"]}
        for outcome in ("success", "failure"):
            found = sum(row["method"] == method and row["outcome"] == outcome and row["total_ms"] >= 1000
                        for row in stages)
            entry.update({"counter_" + outcome + "_ge1s": expected[outcome + "_ge1s"],
                          "archive_" + outcome + "_ge1s": found,
                          outcome + "_ge1s_counts_match": found == expected[outcome + "_ge1s"]})
        comparison.append(entry)
    return {"calibration": calibration, "input_files": manifest, "scanned_uncompressed_bytes": scanned,
            "scan_notes": scan_notes, "analysis_notes": notes, "saved_event_quality": quality,
            "counter_comparison": comparison, "failure_groups": groups, "nonce_senders": senders,
            "failure_records": failures, "stage_records": stages}


def review(root, log_dir, until, examples=16, output_root=None):
    data_reader = module("tail_counter_reader", "review-rpc-tail-data.py")
    archive = module("tail_archive_reader", "review-rpc-tail-archives.py")
    reader = data_reader.load_reader(root)
    output_root = output_root or root.parent
    output_root.mkdir(parents=True, exist_ok=True)
    output = Path(tempfile.mkdtemp(prefix="rpc-tail-review-", dir=output_root))
    emit("REVIEW_DIR", str(output))
    started_utc = datetime.now(timezone.utc).isoformat()
    data = data_reader.build_report(root, until, read_logs=False)
    data["review_started_utc"] = started_utc
    phase_path = root / "phase.json"
    phase = json.loads(phase_path.read_text()) if phase_path.is_file() else {}
    cutoff = reader.stamp(phase.get("comparison_not_before_utc"))
    raw, _ = reader.snapshot(root / "rpc-counters/counters.jsonl")
    intervals, _ = data_reader.scan_counters(raw, until)
    require_single_instance(intervals)
    # The second bounded read must reproduce the same fixed observation window.
    if ({method: data_reader.totals(intervals, method) for method in data_reader.METHODS} != data["totals"]):
        raise ValueError("Counter inputs changed during review; retain this output and retry")
    post = [row for row in intervals if cutoff is not None and row["begin"] >= cutoff]
    data["post_warmup"] = {"requested_since_sgt": reader.sgt(cutoff),
                          "from_sgt": reader.sgt(post[0]["begin"]) if post else None,
                          "to_sgt": reader.sgt(post[-1]["end"]) if post else None,
                          "totals": {m: data_reader.totals(post, m) for m in data_reader.METHODS}}
    for interval in data["hot_intervals"]:
        interval["overlaps_startup_exclusion"] = (reader.stamp(interval["from_sgt"]) < cutoff
                                                 if cutoff is not None else None)
    data["publisher_context"] = publisher_context(root, reader, data["hot_intervals"], data["input_status"])
    # Preserve counters/context even if log copying, calibration or parsing cannot finish.
    (output / "data-report.json").write_text(json.dumps(data, ensure_ascii=False, indent=2, allow_nan=False) + "\n")
    data["_reader"] = reader
    logs = archive_report(root, log_dir, output, data, archive)
    del data["_reader"]
    report = {"data": data, "archives": logs,
        "review_started_utc": started_utc, "review_finished_utc": datetime.now(timezone.utc).isoformat(), "limits": [
        "Counters measure >=500ms; exactly 500ms cannot be separated from >500ms.",
        "The configured 1s stage threshold did not save individual 500-1000ms stage traces.",
        "Rolling publisher quantiles and nearby IO show context, not per-request phase attribution or causality.",
        "Stages overlap; do not sum callback, search and backend timings or subtract rolling quantiles.",
        "Log timestamps can lag completion; counter/log boundaries and detail drops can affect count agreement."]}
    path = output / "report.json"
    path.write_text(json.dumps(report, ensure_ascii=False, indent=2, allow_nan=False) + "\n")
    data_reader.print_report(data)
    emit("POST_WARMUP", data["post_warmup"])
    emit("LOG_CALIBRATION", logs["calibration"])
    emit("ARCHIVE_QUALITY", {"files": len(logs["input_files"]), "scanned_bytes": logs["scanned_uncompressed_bytes"],
         "scan_notes": logs["scan_notes"], "analysis_notes": logs["analysis_notes"],
         "file_errors": [{"name": f["name"], "error": f.get("error", f.get("scan_error"))}
                         for f in logs["input_files"] if f.get("error") or f.get("scan_error")]})
    for entry in logs["counter_comparison"]:
        emit("COUNTER_COMPARE", entry)
    for entry in logs["failure_groups"]:
        emit("FAILURE_GROUP", entry)
    for method in archive.METHODS:
        for outcome in ("success", "failure"):
            selected = sorted((r for r in logs["stage_records"] if r["method"] == method
                               and r["outcome"] == outcome and r["total_ms"] >= 500),
                              key=lambda r: -r["total_ms"])[:examples]
            for entry in selected:
                emit("SLOW_STAGE", entry)
    for entry in data["publisher_context"]:
        emit("PUBLISH_CONTEXT", entry)
    emit("LIMITS", report["limits"])
    emit("REPORT_JSON", str(path))
    return path


def main():
    os.umask(0o077)
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--suite-dir", type=Path, required=True)
    parser.add_argument("--log-dir", type=Path, required=True)
    parser.add_argument("--until", required=True, help="Fixed timezone-aware cutoff")
    parser.add_argument("--output-root", type=Path,
                        help="Parent for the review directory; defaults to the suite's parent, outside its capture budget")
    parser.add_argument("--examples", type=int, default=16)
    args = parser.parse_args()
    helpers = module("tail_review_arguments", "review-rpc-tail-data.py")
    until = helpers.stamp(args.until)
    if until is None or not 1 <= args.examples <= 100:
        parser.error("Require a timezone-aware cutoff and 1..100 examples per method/outcome")
    review(args.suite_dir.resolve(strict=True), args.log_dir.resolve(strict=True), until, args.examples,
           args.output_root.resolve() if args.output_root else None)


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, RuntimeError, subprocess.SubprocessError) as error:
        print("Saved RPC review stopped: " + str(error), file=sys.stderr)
        raise SystemExit(1)
