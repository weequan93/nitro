#!/usr/bin/env python3
"""Snapshot and read only nitro.log/nitro-*.log.gz for a saved report window."""

import argparse
from collections import Counter, defaultdict
from datetime import datetime, timedelta, timezone
import gzip
import hashlib
import importlib.util
import json
import math
import os
from pathlib import Path
import re
import shutil
import tempfile

METHODS = ("eth_sendRawTransaction", "eth_getLogs")
SGT = timezone(timedelta(hours=8))
UTC = timezone.utc
ANSI = re.compile(r"\x1b\[[0-9;]*m")
TERMINAL = re.compile(r"^(?:TRACE|DEBUG|DBUG|INFO|WARN|ERROR|EROR|CRIT)\s*\[(\d{2}-\d{2}\|\d{2}:\d{2}:\d{2}(?:\.\d+)?)\]\s*(.*)$")
KV = re.compile(r'(?:^|\s)([A-Za-z][A-Za-z0-9_]*)=("(?:\\.|[^"\\])*"|\S+)')
STAGES = ("params", "callback", "encode", "handler_other", "resolve", "backend_setup", "sync_initial",
          "indexed_search", "result_filter", "unindexed_search", "sync_post", "consistency", "backend_close",
          "index_lock_wait", "index_lock_hold", "filter_rows_read", "block_pointer_access", "log_lookup",
          "receipts_access", "receipt_scan", "header_access", "block_logs_access", "body_access",
          "sync_register_wait", "sync_enqueue_wait", "sync_response_wait")
SHAPES = ("by_hash", "address_count", "topic_positions", "topic_alternatives", "resolved_blocks_max",
          "indexed_block_range_total", "unindexed_blocks_scanned", "candidate_count", "result_count",
          "receipt_reads", "receipt_cursor_hits", "log_cache_hits", "log_cache_misses", "body_cache_hits",
          "body_cache_misses", "failure_kind")


def stamp(v):
    try:
        d = datetime.fromisoformat(v.replace("Z", "+00:00"))
        return d.timestamp() if d.tzinfo else None
    except (ValueError, AttributeError):
        return None


def sgt(t):
    return datetime.fromtimestamp(t, SGT).isoformat() if t is not None else None


def numeric(v):
    try:
        n = float(str(v).replace(",", ""))
        return n if math.isfinite(n) and n >= 0 else None
    except (ValueError, TypeError):
        return None


def integer(v):
    text = str(v).replace(",", "")
    return int(text) if text.isdigit() and len(text) <= 20 else None


def fields(text):
    result = {}
    for k, v in KV.findall(text):
        try:
            result[k] = json.loads(v) if v.startswith('"') else v
        except ValueError:
            result[k] = v.strip('"')
    return result


def parse_line(raw):
    text = ANSI.sub("", raw.decode("utf-8", errors="replace")).strip()
    token, body, f = None, text, None
    parts = text.split(None, 1)
    if len(parts) == 2 and stamp(parts[0]) is not None:
        token, body = ("absolute", stamp(parts[0])), parts[1]
    if body.startswith("{"):
        try:
            f = json.loads(body)
        except ValueError:
            return None
        if not isinstance(f, dict):
            return None
        token = token or (("absolute", stamp(f.get("t", f.get("time")))))
        if token[1] is None:
            return None
        message = f.get("msg", f.get("message", ""))
    else:
        term = TERMINAL.match(body)
        if term:
            token, body = token or ("terminal", term[1]), term[2]
        if token is None:
            return None
        relevant = "RPC stage timings" in body or any("Served " + m in body for m in METHODS)
        if not relevant:
            return token, None, {}
        f = fields(body)
        message = "RPC stage timings" if "RPC stage timings" in body else next(
            ("Served " + m for m in METHODS if "Served " + m in body), "")
    return (token, message, f) if isinstance(message, str) else None


def local_naive(token, reference):
    year = datetime.fromtimestamp(reference, UTC).year
    candidates = []
    for y in (year - 1, year, year + 1):
        try:
            candidates.append(datetime.fromisoformat(f"{y}-" + token.replace("|", "T")).replace(tzinfo=UTC).timestamp())
        except ValueError:
            continue
    if not candidates:
        raise ValueError("Invalid terminal date")
    return min(candidates, key=lambda t: abs(t - reference))


def infer_offset(records, anchors):
    votes = defaultdict(dict)
    for r in records:
        if r["message"] != "RPC stage timings" or r["token"][0] != "terminal":
            continue
        f = r["fields"]
        key = (f.get("method"), integer(f.get("trace")))
        expected = anchors.get(key)
        if expected is None:
            continue
        raw_time = local_naive(r["token"][1], expected)
        difference = raw_time - expected
        offset = round(difference / 900) * 900
        if -12 * 3600 <= offset <= 14 * 3600 and abs(difference - offset) <= 3:
            votes[offset][key] = round(difference - offset, 6)
    ranked = sorted(votes, key=lambda k: -len(votes[k]))
    count = sum(len(v) for v in votes.values())
    best = ranked[0] if ranked else None
    accepted = best is not None and len(votes[best]) >= 2 and len(votes[best]) >= .9 * count
    terminal_count = sum(r["token"][0] == "terminal" for r in records)
    return (best if accepted else None), {
        "method": "Matched stage method/trace against saved watcher timestamps; no filename-timezone assumption",
        "resolved": accepted or terminal_count == 0, "terminal_utc_offset_seconds": best if accepted else None,
        "terminal_rpc_records": terminal_count, "absolute_rpc_records": len(records) - terminal_count,
        "anchor_votes": {str(k): len(votes[k]) for k in ranked},
        "matched_anchor_examples": [{"method": m, "trace": trace, "residual_seconds": residual}
                                    for (m, trace), residual in list(votes.get(best, {}).items())[:5]],
    }


def timestamp(token, offset, reference):
    if token[0] == "absolute":
        return token[1]
    return local_naive(token[1], reference + offset) - offset if offset is not None else None


def duration_ms(v):
    text = str(v)
    matches = list(re.finditer(r"([0-9]+(?:\.[0-9]+)?)(ns|us|µs|μs|ms|s|m|h)", text))
    if not matches or "".join(m[0] for m in matches) != text:
        return None
    units = {"ns": 1e-6, "us": .001, "µs": .001, "μs": .001, "ms": 1, "s": 1000, "m": 60000, "h": 3600000}
    return sum(float(m[1]) * units[m[2]] for m in matches)


def error_group(value):
    value = re.sub(r"[A-Za-z][A-Za-z0-9+.-]*://\S+", "<url>", str(value)[:2000])
    value = re.sub(r"0x[0-9a-fA-F]+", "<hex>", value)
    value = re.sub(r"\b[0-9a-fA-F]{32,}\b", "<hex>", value)
    return " ".join(re.sub(r"\b\d+(?:[.,]\d+)*\b", "<n>", value).split())[:400]


def snapshot_logs(log_dir, output):
    paths = sorted(log_dir.glob("nitro-*.log.gz"))
    if (log_dir / "nitro.log").exists():
        paths.append(log_dir / "nitro.log")
    if not paths or len(paths) > 128:
        raise ValueError("Require 1..128 nitro.log/nitro-*.log.gz files")
    snapshots = output / "log-snapshot"
    snapshots.mkdir(mode=0o700)
    manifest, seen, budget = [], set(), 0
    for path in paths:
        item = {"name": path.name}
        manifest.append(item)
        if path.is_symlink():
            item["error"] = "symlink_skipped"
            continue
        try:
            with path.open("rb") as src:
                stat = os.fstat(src.fileno())
                key = (stat.st_dev, stat.st_ino)
                if key in seen:
                    item["error"] = "same_inode_skipped"
                    continue
                seen.add(key)
                budget += stat.st_size
                if budget > 2 * 1024**3:
                    raise ValueError("Snapshot inputs exceed 2 GiB")
                if shutil.disk_usage(output).free < stat.st_size + 3 * 1024**3:
                    raise ValueError("Snapshot would leave less than 3 GiB free")
                remaining, digest = stat.st_size, hashlib.sha256()
                with (snapshots / path.name).open("xb") as dst:
                    while remaining:
                        data = src.read(min(remaining, 1024**2))
                        if not data:
                            break
                        dst.write(data)
                        digest.update(data)
                        remaining -= len(data)
                item.update({"snapshot_bytes": stat.st_size - remaining, "expected_bytes": stat.st_size,
                             "sha256": digest.hexdigest(), "source_grew_during_copy": os.fstat(src.fileno()).st_size > stat.st_size})
                if remaining:
                    item["error"] = "source_short_read"
        except OSError as exc:
            item["error"] = type(exc).__name__
    return snapshots, manifest


def read_records(folder, manifest):
    records, notes = [], Counter()
    total_bytes = 0
    for index, item in enumerate(manifest):
        if item.get("error"):
            continue
        path = folder / item["name"]
        item.update({"lines": 0, "rpc_records": 0, "timestamped_lines": 0})
        tokens = []
        try:
            with (gzip.open(path, "rb") if path.suffix == ".gz" else path.open("rb")) as stream:
                while True:
                    raw = stream.readline(1024**2 + 1)
                    if not raw:
                        break
                    total_bytes += len(raw)
                    if total_bytes > 4 * 1024**3:
                        raise ValueError("Uncompressed scan exceeds 4 GiB")
                    if len(raw) > 1024**2:
                        raise ValueError("Log line exceeds 1 MiB")
                    item["lines"] += 1
                    if not raw.endswith(b"\n"):
                        notes["partial_lines_skipped"] += 1
                        continue
                    parsed = parse_line(raw)
                    if parsed is None:
                        notes["unparsed_lines"] += 1
                        continue
                    token, message, f = parsed
                    item["timestamped_lines"] += 1
                    if not tokens:
                        tokens.append(token)
                    elif len(tokens) == 1:
                        tokens.append(token)
                    else:
                        tokens[-1] = token
                    if message == "RPC stage timings" or (message and message.startswith("Served ") and "err" in f):
                        item["rpc_records"] += 1
                        records.append({"file_index": index, "token": token, "message": message, "fields": f,
                                        "fingerprint": hashlib.sha256(raw).hexdigest()})
                        if len(records) > 200000:
                            raise ValueError("Relevant RPC records exceed 200000")
        except (OSError, EOFError) as exc:
            item["scan_error"] = type(exc).__name__
        item["boundary_tokens"] = tokens
    return records, dict(notes), total_bytes


def analyze(records, offset, since, until):
    reference = (since + until) / 2
    failures, stages, notes = [], [], Counter()
    max_previous = Counter()
    by_file = defaultdict(list)
    for r in records:
        by_file[r["file_index"]].append(r)
    seen_traces = {}
    for file_index in sorted(by_file):
        local_counts = Counter()
        for r in by_file[file_index]:
            t = timestamp(r["token"], offset, reference)
            if t is None:
                notes["unresolved_time_rpc_records"] += 1
                continue
            if not since <= t <= until:
                continue
            fp = r["fingerprint"]
            local_counts[fp] += 1
            if local_counts[fp] <= max_previous[fp]:
                notes["duplicate_records_across_files"] += 1
                continue
            f, message = r["fields"], r["message"]
            if message == "RPC stage timings":
                method, outcome, trace = f.get("method"), f.get("outcome"), integer(f.get("trace"))
                total = numeric(f.get("total_ms"))
                if method not in METHODS or outcome not in ("success", "failure") or trace is None or total is None:
                    notes["invalid_stage_records"] += 1
                    continue
                key = method, trace
                if key in seen_traces:
                    old_time, old_total = seen_traces[key]
                    notes["duplicate_stage_traces"] += 1
                    if abs(old_time - t) > 3 or old_total != total:
                        notes["conflicting_stage_traces"] += 1
                    continue
                seen_traces[key] = t, total
                item = {"time_sgt": sgt(t), "method": method, "outcome": outcome, "trace": trace, "total_ms": total}
                for name in tuple(s + suffix for s in STAGES for suffix in ("_ms", "_calls", "_max_ms")) + SHAPES:
                    value = numeric(f.get(name))
                    if value is not None:
                        item[name] = value
                stages.append(item)
            elif message[7:] in METHODS and "err" in f:
                error = str(f["err"])
                item = {"time_sgt": sgt(t), "method": message[7:], "error_group": error_group(error),
                        "duration_ms": duration_ms(f.get("duration"))}
                nonce = re.search(r"nonce too (?:low|high): address (0x[0-9a-fA-F]+), tx: (\d+) state: (\d+)", error)
                if nonce:
                    item.update({"sender_hash": hashlib.sha256(nonce[1].lower().encode()).hexdigest()[:12],
                                 "nonce_gap_state_minus_tx": int(nonce[3]) - int(nonce[2]),
                                 "failed_nonce_hash": hashlib.sha256((nonce[1].lower() + ":" + nonce[2]).encode()).hexdigest()[:16]})
                failures.append(item)
        max_previous |= local_counts
    return failures, stages, dict(notes)


def summarize(failures, stages):
    groups, senders = defaultdict(list), defaultdict(list)
    for r in failures:
        groups[r["method"], r["error_group"]].append(r)
        if r.get("sender_hash"):
            senders[r["sender_hash"]].append(r)
    error_groups = []
    for (method, error), rows in groups.items():
        durations = [r["duration_ms"] for r in rows if r["duration_ms"] is not None]
        error_groups.append({"method": method, "error_group": error, "records": len(rows),
            "first_sgt": min(r["time_sgt"] for r in rows), "last_sgt": max(r["time_sgt"] for r in rows),
            "duration_ms": {"n": len(durations), "min": min(durations) if durations else None,
                            "max": max(durations) if durations else None}})
    nonce_senders = []
    for sender, rows in senders.items():
        gaps = [r["nonce_gap_state_minus_tx"] for r in rows]
        nonce_senders.append({"sender_hash": sender, "records": len(rows),
            "distinct_failed_nonces": len({r["failed_nonce_hash"] for r in rows}),
            "nonce_gap_min": min(gaps), "nonce_gap_max": max(gaps),
            "error_groups": dict(Counter(r["error_group"] for r in rows)),
            "first_sgt": min(r["time_sgt"] for r in rows), "last_sgt": max(r["time_sgt"] for r in rows)})
    return sorted(error_groups, key=lambda r: -r["records"]), sorted(nonce_senders, key=lambda r: -r["records"])


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--suite-dir", type=Path, required=True)
    p.add_argument("--log-dir", type=Path, required=True)
    p.add_argument("--previous-report", type=Path, required=True)
    args = p.parse_args()
    os.umask(0o077)
    previous = json.loads(args.previous_report.read_text())
    since, until = stamp(previous["window"]["from_sgt"]), stamp(previous["window"]["to_sgt"])
    if since is None or until is None or not 0 < until - since <= 86400:
        raise ValueError("Invalid fixed report window")
    root = args.suite_dir.resolve()
    spec = importlib.util.spec_from_file_location("saved_reader", root / "tools/report-sequencer-context.py")
    reader = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(reader)
    events, event_quality = reader.snapshot(root / "slow-blocks/events.jsonl")
    anchors = {(e.get("method"), integer(e.get("trace"))): stamp(e.get("log_time_utc")) for e in events
               if e.get("kind") == "rpc_slow" and integer(e.get("trace")) is not None and stamp(e.get("log_time_utc")) is not None}
    output = Path(tempfile.mkdtemp(prefix="rpc-archive-review-", dir=root))
    snapshot, manifest = snapshot_logs(args.log_dir, output)
    print("SNAPSHOT", str(snapshot), flush=True)
    records, scan_notes, scanned = read_records(snapshot, manifest)
    offset, calibration = infer_offset(records, anchors)
    failures, stages, analysis_notes = analyze(records, offset, since, until)
    for f in manifest:
        times = [timestamp(t, offset, (since + until) / 2) for t in f.pop("boundary_tokens", [])]
        times = [t for t in times if t is not None]
        f["first_sgt"], f["last_sgt"] = sgt(min(times)) if times else None, sgt(max(times)) if times else None
    errors, senders = summarize(failures, stages)
    comparison = []
    for method in METHODS:
        expected = previous["totals"][method]
        found_failures = sum(r["method"] == method for r in failures)
        found_slow = sum(r["method"] == method and r["outcome"] == "success" and r["total_ms"] >= 1000 for r in stages)
        comparison.append({"method": method, "counter_failures": expected["failure"], "archive_failure_records": found_failures,
            "failure_counts_match": found_failures == expected["failure"], "counter_success_ge1s": expected["success_ge1s"],
            "archive_success_ge1s_records": found_slow, "ge1s_counts_match": found_slow == expected["success_ge1s"]})
    hourly = defaultdict(Counter)
    for r in failures:
        hourly[r["time_sgt"][:13]][r["error_group"]] += 1
    report = {"window": {"from_sgt": sgt(since), "to_sgt": sgt(until)}, "calibration": calibration,
        "input_files": manifest, "scanned_uncompressed_bytes": scanned, "scan_notes": scan_notes,
        "analysis_notes": analysis_notes, "saved_event_quality": event_quality, "counter_comparison": comparison,
        "failure_groups": errors, "nonce_senders": senders, "failure_hours": {h: dict(c) for h, c in sorted(hourly.items())},
        "failure_records": failures, "stage_records": stages,
        "limits": ["Plaintext timestamps require trace-based timezone calibration; unresolved times are excluded.",
                   "Matching counters checks aggregate agreement, not a one-to-one link to every RPC or transport.",
                   "Only archived stage records above the configured 1s threshold can be recovered; 500-1000ms details were not logged.",
                   "File logging can drop buffered records; coverage boundaries alone do not prove completeness.",
                   "Stages are nested/parallel; do not add stage times. Request success does not establish receipt success.",
                   "Sender hashes group addresses without printing them; duplicate nonce submissions do not alone identify the calling client."]}
    (output / "report.json").write_text(json.dumps(report, ensure_ascii=False, indent=2, allow_nan=False) + "\n")
    print("CALIBRATION", json.dumps(calibration, ensure_ascii=False))
    print("QUALITY", json.dumps({"files": len(manifest), "scanned_uncompressed_bytes": scanned,
        "scan_notes": scan_notes, "analysis_notes": analysis_notes,
        "file_errors": [{"name": f["name"], "error": f.get("error", f.get("scan_error"))}
                        for f in manifest if f.get("error") or f.get("scan_error")]}, ensure_ascii=False))
    for f in manifest:
        print("FILE_COVERAGE", json.dumps({k: f.get(k) for k in ("name", "first_sgt", "last_sgt", "rpc_records")}, ensure_ascii=False))
    for r in comparison:
        print("COUNTER_COMPARE", json.dumps(r, ensure_ascii=False))
    for r in errors:
        print("FAILURE_GROUP", json.dumps(r, ensure_ascii=False))
    for r in senders[:15]:
        print("NONCE_SENDER", json.dumps(r, ensure_ascii=False))
    for h, counts in sorted(hourly.items()):
        print("FAILURE_HOUR", json.dumps({"hour_sgt": h + ":00+08:00", "errors": dict(counts)}, ensure_ascii=False))
    for method in METHODS:
        selected = sorted((r for r in stages if r["method"] == method), key=lambda r: -r["total_ms"])[:8]
        for r in selected:
            print("ARCHIVE_STAGE", json.dumps(r, ensure_ascii=False))
    print("REPORT_JSON", str(output / "report.json"))


if __name__ == "__main__":
    main()
