#!/usr/bin/env python3
"""Stream a saved Nitro log snapshot, including eth_call reverts and RPC errors.

Offline only: no RPC, Docker, service operations or copying live log files.
Clock calibration uses paired Docker/embedded timestamps from a supplied log.
"""

import argparse
from collections import Counter
from datetime import datetime, timezone
import gzip
import importlib.util
import json
import math
import os
from pathlib import Path
import re


spec = importlib.util.spec_from_file_location(
    "recovery_archive", Path(__file__).with_name("review-rpc-tail-archives.py"))
archive = importlib.util.module_from_spec(spec)
spec.loader.exec_module(archive)
METHOD = re.compile(r"\bServed ([A-Za-z][A-Za-z0-9_]{0,127})\b")
KEYWORDS = {
    "storage": re.compile(r"no space left|I/O error|read-only file system|corrupt", re.I),
    "sequencer": re.compile(r"not main sequencer|sequencer is not synced|cannot sequence|chosen sequencer|coordinator", re.I),
    "deadline": re.compile(r"abort deadline|context deadline exceeded|timeout", re.I),
    "fatal": re.compile(r"\b(?:fatal|panic|CRIT)\b", re.I),
}


def calibrate(path):
    if path.stat().st_size > 64 * 1024**2:
        raise ValueError("Docker clock input exceeds 64 MiB")
    votes, examples = Counter(), set()
    with path.open("rb") as stream:
        for raw in stream:
            if len(raw) > 1024**2:
                raise ValueError("Docker clock line exceeds 1 MiB")
            parts = archive.ANSI.sub("", raw.decode("utf-8", "replace")).split(None, 1)
            absolute = archive.stamp(parts[0]) if parts else None
            embedded = archive.TERMINAL.match(parts[1]) if len(parts) == 2 else None
            if absolute is None or embedded is None:
                continue
            difference = archive.local_naive(embedded[1], absolute) - absolute
            offset = round(difference / 900) * 900
            if -12 * 3600 <= offset <= 14 * 3600 and abs(difference - offset) <= 3:
                identity = (offset, embedded[1], parts[0])
                if identity not in examples:
                    examples.add(identity)
                    votes[offset] += 1
            if len(examples) >= 2000:
                break
    best, count = votes.most_common(1)[0] if votes else (None, 0)
    resolved = count >= 2 and count >= .9 * sum(votes.values())
    return best if resolved else None, {"resolved": resolved, "offset_seconds": best if resolved else None,
        "paired_timestamp_votes": dict(votes),
        "source": "Docker timestamp paired with the same line's embedded logger timestamp"}


def parse(raw):
    value = archive.parse_line(raw)
    if value is None:
        return None
    token, message, fields = value
    text = archive.ANSI.sub("", raw.decode("utf-8", "replace")).strip()
    if message is None and (match := METHOD.search(text)):
        message, fields = "Served " + match[1], archive.fields(text)
    return token, message, fields, text


def review(snapshot_root, docker_log, since, until, max_bytes):
    offset, clock = calibrate(docker_log)
    manifest = json.loads((snapshot_root / "manifest.json").read_text())
    if not isinstance(manifest, list) or not 1 <= len(manifest) <= 128:
        raise ValueError("Require a snapshot manifest with 1..128 files")
    quality, groups, keywords = Counter(), {}, {}
    files, stages, stage_counts = [], [], Counter()
    used = 0
    for entry in manifest:
        name = entry["name"]
        if not isinstance(name, str) or Path(name).name != name:
            raise ValueError("Invalid snapshot filename")
        path = snapshot_root / "log-snapshot" / name
        info = {"name": name, "first_sgt": None, "last_sgt": None, "lines": 0}
        files.append(info)
        if entry.get("error") or path.is_symlink():
            info["error"] = entry.get("error") or "symlink_skipped"
            continue
        first = last = None
        try:
            with (gzip.open(path, "rb") if path.suffix == ".gz" else path.open("rb")) as stream:
                while raw := stream.readline(1024**2 + 1):
                    used += len(raw)
                    if used > max_bytes or len(raw) > 1024**2:
                        raise ValueError("Snapshot scan byte/line budget exceeded; inputs retained")
                    info["lines"] += 1
                    if not raw.endswith(b"\n"):
                        quality["partial_lines"] += 1
                        continue
                    parsed = parse(raw)
                    if parsed is None:
                        quality["unparsed_lines"] += 1
                        continue
                    token, message, fields, text = parsed
                    try:
                        timestamp = archive.timestamp(token, offset, (since + until) / 2)
                    except ValueError:
                        timestamp = None
                    if timestamp is None:
                        quality["unresolved_timestamp_lines"] += 1
                        continue
                    first = timestamp if first is None else min(first, timestamp)
                    last = timestamp if last is None else max(last, timestamp)
                    if not since <= timestamp <= until:
                        continue
                    quality["lines_in_window"] += 1
                    when = archive.sgt(timestamp)
                    for label, pattern in KEYWORDS.items():
                        if pattern.search(text):
                            group = keywords.setdefault(label, {"records": 0, "examples": []})
                            group["records"] += 1
                            if len(group["examples"]) < 5:
                                group["examples"].append({"time_sgt": when, "file": name,
                                    "line": info["lines"], "text": archive.error_group(text)})
                    if isinstance(message, str) and message.startswith("Served ") and "err" in fields:
                        key = (message[7:], archive.error_group(fields["err"]))
                        if key not in groups and len(groups) >= 4096:
                            raise ValueError("Error group cardinality exceeds 4096")
                        group = groups.setdefault(key, {"records": 0, "first_sgt": when, "last_sgt": when,
                            "duration_samples": 0, "min_ms": None, "max_ms": None})
                        group["records"] += 1
                        group["first_sgt"], group["last_sgt"] = min(group["first_sgt"], when), max(group["last_sgt"], when)
                        duration = archive.duration_ms(fields.get("duration"))
                        if duration is not None and math.isfinite(duration):
                            group["duration_samples"] += 1
                            group["min_ms"] = duration if group["min_ms"] is None else min(group["min_ms"], duration)
                            group["max_ms"] = duration if group["max_ms"] is None else max(group["max_ms"], duration)
                    elif message == "RPC stage timings":
                        duration = archive.numeric(fields.get("total_ms"))
                        method, outcome = fields.get("method"), fields.get("outcome")
                        if not isinstance(method, str) or outcome not in ("success", "failure") or duration is None:
                            quality["invalid_stage_lines"] += 1
                            continue
                        key = method, outcome
                        if key not in stage_counts and len(stage_counts) >= 4096:
                            raise ValueError("Stage group cardinality exceeds 4096")
                        stage_counts[key] += 1
                        stages.append({"time_sgt": when, "method": method, "outcome": outcome,
                            "total_ms": duration, "callback_ms": archive.numeric(fields.get("callback_ms"))})
                        stages.sort(key=lambda r: r["total_ms"], reverse=True)
                        del stages[10:]
        except (OSError, EOFError) as error:
            info["error"] = type(error).__name__
        info.update(first_sgt=archive.sgt(first), last_sgt=archive.sgt(last))
    return {"schema": 1, "requested_window": {"from_sgt": archive.sgt(since), "to_sgt": archive.sgt(until)},
        "clock": clock, "scanned_uncompressed_bytes": used, "quality": dict(quality), "files": files,
        "error_groups": sorted(({"method": method, "error_group": error, **value}
                                for (method, error), value in groups.items()), key=lambda r: -r["records"]),
        "stage_record_counts": [{"method": method, "outcome": outcome, "records": count}
                                for (method, outcome), count in sorted(stage_counts.items())],
        "slowest_stage_records": stages, "keyword_records": keywords,
        "limits": ["Counts describe retained log records, not complete request totals or failure rates.",
            "Served errors and stage records can describe the same request; do not add them.",
            "Clock calibration refers to the supplied Docker log; native text logs must use the same logger clock.",
            "File coverage alone does not prove all records were retained. Rotation overlaps are not deduplicated.",
            "Keyword matches are investigation clues; they are not root-cause attribution.",
            "No counter baseline is reconstructed; unlogged 500-1000ms requests cannot be recovered."]}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--snapshot-root", type=Path, required=True)
    parser.add_argument("--docker-log", type=Path, required=True)
    parser.add_argument("--since", required=True, help="Timezone-aware lower bound")
    parser.add_argument("--until", default=datetime.now(timezone.utc).isoformat())
    parser.add_argument("--max-scan-mib", type=int, default=512)
    args = parser.parse_args()
    since, until = archive.stamp(args.since), archive.stamp(args.until)
    if since is None or until is None or not 0 < until - since <= 7 * 86400 or not 1 <= args.max_scan_mib <= 4096:
        parser.error("Require a valid window of at most 7 days and a 1..4096 MiB scan budget")
    os.umask(0o077)
    result = review(args.snapshot_root, args.docker_log, since, until, args.max_scan_mib * 1024**2)
    output = args.snapshot_root / "recovery-report.json"
    output.write_text(json.dumps(result, ensure_ascii=False, indent=2, allow_nan=False) + "\n")
    for key in ("requested_window", "clock", "quality", "stage_record_counts", "keyword_records"):
        print(key.upper(), json.dumps(result[key], ensure_ascii=False))
    for item in result["files"]:
        print("FILE_COVERAGE", json.dumps(item, ensure_ascii=False))
    for item in result["error_groups"][:30]:
        print("ERROR_GROUP", json.dumps(item, ensure_ascii=False))
    for item in result["slowest_stage_records"]:
        print("SLOW_STAGE", json.dumps(item, ensure_ascii=False))
    print("LIMITS", json.dumps(result["limits"], ensure_ascii=False))
    print("REPORT_JSON", str(output))


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, EOFError) as error:
        raise SystemExit("Saved recovery review stopped: " + str(error))
