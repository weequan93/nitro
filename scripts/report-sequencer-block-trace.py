#!/usr/bin/env python3
"""Explain one sequencer block using its runtime trace and labeled CPU profile.

Offline only. Use Go >= the version which wrote the trace. No node RPCs or
service operations. Region totals overlap; scheduled-running wall is not CPU.
"""

import argparse
from collections import Counter, defaultdict
import gzip
import json
from pathlib import Path
import re
import subprocess
import tempfile


HEADER = re.compile(r"^M=(-?\d+) P=(-?\d+) G=(-?\d+) (\w+) Time=(\d+)(.*)$")
FIELD = re.compile(r'(\w+)=("(?:[^"\\]|\\.)*"|\S+)')
STATES = ("go_running_wall", "runnable_delay", "mutex_wait", "channel_select_wait",
          "other_wait", "syscall_or_cgo_wall", "unknown")


def events(stream):
    """Support parsed Go 1.25 (-d=1) and Go 1.26 (-d=parsed) event dumps."""
    current = None
    stack = None
    for line in stream:
        match = HEADER.match(line)
        if match:
            if current is not None:
                yield current
            _m, _p, goroutine, kind, timestamp, tail = match.groups()
            fields = {}
            for key, value in FIELD.findall(tail):
                fields[key] = json.loads(value) if value.startswith('"') else value
            current = {"kind": kind, "time": int(timestamp), "g": int(goroutine),
                       "fields": fields, "tail": tail, "stacks": defaultdict(list)}
            stack = None
        elif current is not None:
            if line.startswith(("Stack=", "TransitionStack=")):
                stack = line.strip().rstrip("=")
            elif stack and line.startswith("\t") and " @ " in line:
                current["stacks"][stack].append(line.strip().split(" @ ", 1)[0])
    if current is not None:
        yield current


def selected_task(stream, seq_id):
    begins, ends, labels = {}, {}, []
    for e in events(stream):
        f = e["fields"]
        if e["kind"] in ("TaskBegin", "TaskEnd") and f.get("Type") == "sequencer/block":
            target = begins if e["kind"] == "TaskBegin" else ends
            target[f["ID"]] = e
        elif e["kind"] == "Log" and f.get("Category") == "sequencer_id" and f.get("Message") == str(seq_id):
            labels.append(f.get("Task"))
    ids = set(labels)
    if len(ids) != 1:
        raise ValueError("sequencer ID missing or ambiguous in retained trace")
    task_id = ids.pop()
    begin, end = begins.get(task_id), ends.get(task_id)
    if begin is None or end is None:
        raise ValueError("incomplete TaskBegin/TaskEnd; flight recorder may have evicted the block")
    if begin["g"] != end["g"] or begin["time"] >= end["time"]:
        raise ValueError("inconsistent task boundaries")
    return task_id, begin["g"], begin["time"], end["time"]


def category(state, reason, stack):
    if state == "Running":
        return "go_running_wall"
    if state == "Runnable":
        return "runnable_delay"
    if state == "Syscall":
        return "syscall_or_cgo_wall"
    if state == "Waiting":
        if any(re.search(r"sync\.(?:\(\*(?:RW)?Mutex\)|runtime_Semacquire(?:RWMutex|Mutex))", s) for s in stack):
            return "mutex_wait"
        if "chan" in reason.lower() or "select" in reason.lower():
            return "channel_select_wait"
        return "other_wait"
    return "unknown"


def merge_intervals(intervals):
    merged = []
    for start, end in sorted(intervals):
        if merged and start <= merged[-1][1]:
            merged[-1] = (merged[-1][0], max(end, merged[-1][1]))
        else:
            merged.append((start, end))
    return merged


def wall_breakdown(segments, intervals):
    totals = {key: 0 for key in STATES}
    # Both inputs are sorted, disjoint unions. Avoid quadratic per-read scans.
    intervals = merge_intervals(intervals)
    i = j = 0
    while i < len(segments) and j < len(intervals):
        start, end, label, _cause = segments[i]
        lo, hi = intervals[j]
        totals[label] += max(0, min(end, hi) - max(start, lo))
        if end <= hi:
            i += 1
        else:
            j += 1
    return {key: round(value / 1e6, 6) for key, value in totals.items()}


def analyze_trace(stream, seq_id):
    task, goroutine, start, end = selected_task(stream, seq_id)
    stream.seek(0)
    transitions, region_stack, regions, region_errors = [], [], defaultdict(list), []
    for e in events(stream):
        f, timestamp = e["fields"], e["time"]
        if e["kind"] == "StateTransition":
            resource = re.search(r"Resource=Goroutine\((\d+)\)", e["tail"])
            target = f.get("GoID", resource.group(1) if resource else None)
            change = re.search(r"\b(\w+)->(\w+)\b", e["tail"])
            if target == str(goroutine) and change and start <= timestamp <= end:
                stack = e["stacks"].get("TransitionStack", []) or e["stacks"].get("Stack", [])
                transitions.append((timestamp, *change.groups(), f.get("Reason", ""), stack))
        if (e["kind"] in ("RegionBegin", "RegionEnd") and f.get("Task") == task
                and e["g"] == goroutine and f.get("Type", "").startswith("sequencer/")):
            name = f["Type"]
            if not start <= timestamp <= end:
                region_errors.append("region outside task: " + name)
            elif e["kind"] == "RegionBegin":
                region_stack.append((name, timestamp))
            elif not region_stack or region_stack[-1][0] != name:
                region_errors.append("unmatched RegionEnd: " + name)
            else:
                _, since = region_stack.pop()
                regions[name].append((since, timestamp))
    if region_stack or region_errors:
        raise ValueError("incomplete/nonnested region boundaries: " + str(region_errors or region_stack[:3]))
    # TaskBegin is emitted by the selected running goroutine, so its initial
    # state is known even if the preceding scheduler transition was evicted.
    previous, state, reason, stack = start, "Running", "", []
    segments, consistency = [], []
    for timestamp, before, after, why, frames in transitions:
        if before != state:
            consistency.append("state discontinuity at %d: %s/%s" % (timestamp, state, before))
            segments.append((previous, timestamp, "unknown", ("state discontinuity", ())))
        else:
            segments.append((previous, timestamp, category(state, reason, stack), (reason, tuple(stack[:8]))))
        previous, state, reason, stack = timestamp, after, why, frames
    segments.append((previous, end, category(state, reason, stack), (reason, tuple(stack[:8]))))
    reader_intervals = [span for name, spans in regions.items() if name.startswith("sequencer/state_reader/") for span in spans]
    waits = Counter()
    for lo, hi, label, cause in segments:
        if label in ("mutex_wait", "channel_select_wait", "other_wait", "syscall_or_cgo_wall"):
            waits[(label, *cause)] += hi - lo
    return {
        "available": True, "task_id": task, "goroutine": goroutine,
        "task_wall_ms": round((end - start) / 1e6, 6),
        "primary_goroutine_wall_ms": wall_breakdown(segments, [(start, end)]),
        "state_reader_union_wall_ms": wall_breakdown(segments, reader_intervals),
        "regions_nonadditive": {name.removeprefix("sequencer/"): {
            "calls": len(spans), "wall_ms": round(sum(hi - lo for lo, hi in spans) / 1e6, 6),
            "primary_goroutine_wall_ms": wall_breakdown(segments, spans)} for name, spans in sorted(regions.items())},
        "waiting_stacks": [{"category": label, "reason": why, "stack": frames,
                            "wall_ms": round(ns / 1e6, 6)} for (label, why, frames), ns in waits.most_common(12)],
        "consistency_errors": consistency,
    }


def varint(data, pos):
    result = 0
    for shift in range(0, 70, 7):
        if pos >= len(data):
            raise ValueError("truncated profile varint")
        byte = data[pos]
        pos += 1
        result |= (byte & 127) << shift
        if byte < 128:
            return result, pos
    raise ValueError("invalid profile varint")


def protobuf(data):
    pos = 0
    while pos < len(data):
        tag, pos = varint(data, pos)
        number, wire = tag >> 3, tag & 7
        if not number:
            raise ValueError("invalid profile field")
        if wire == 0:
            value, pos = varint(data, pos)
        elif wire in (1, 2, 5):
            size = {1: 8, 5: 4}.get(wire)
            if wire == 2:
                size, pos = varint(data, pos)
            if size > len(data) - pos:
                raise ValueError("truncated profile field")
            value, pos = data[pos:pos + size], pos + size
        else:
            raise ValueError("unsupported profile wire type")
        yield number, wire, value


def packed(value):
    if isinstance(value, int):
        return [value]
    result, pos = [], 0
    while pos < len(value):
        item, pos = varint(value, pos)
        result.append(item)
    return result


def cpu_profile(path, seq_id):
    if path.stat().st_size > 64 << 20:
        raise ValueError("CPU profile exceeds 64 MiB")
    with gzip.open(path, "rb") as stream:
        data = stream.read((256 << 20) + 1)
    if len(data) > 256 << 20:
        raise ValueError("CPU profile expands beyond 256 MiB")
    strings, samples, sample_types, functions, locations = [], [], [], {}, {}
    for number, _wire, value in protobuf(data):
        if number == 6:
            strings.append(value.decode("utf-8", "replace"))
        elif number == 1:
            sample_types.append(dict((n, v) for n, _w, v in protobuf(value)))
        elif number == 2:
            samples.append(value)
        elif number == 5:
            f = dict((n, v) for n, _w, v in protobuf(value))
            functions[f[1]] = f.get(2, 0)
        elif number == 4:
            loc, lines = None, []
            for n, _w, v in protobuf(value):
                if n == 1:
                    loc = v
                elif n == 4:
                    lines.append(dict((i, x) for i, _w2, x in protobuf(v)).get(1))
            locations[loc] = lines
    cpu_index = next((i for i, t in enumerate(sample_types)
                      if strings[t[1]] == "cpu" and strings[t[2]] == "nanoseconds"), None)
    if cpu_index is None:
        raise ValueError("profile has no CPU nanosecond samples")
    cpu_ns, matching, flat, cumulative = 0, 0, Counter(), Counter()
    for value in samples:
        labels, values, locs = {}, [], []
        for n, _w, v in protobuf(value):
            if n == 1:
                locs.extend(packed(v))
            elif n == 2:
                values.extend(packed(v))
            elif n == 3:
                label = dict((i, x) for i, _w2, x in protobuf(v))
                if 1 in label and 2 in label:
                    labels[strings[label[1]]] = strings[label[2]]
        if labels.get("seq_id") != str(seq_id):
            continue
        if cpu_index >= len(values) or values[cpu_index] >= 1 << 63:
            raise ValueError("invalid CPU sample value")
        ns = values[cpu_index]
        cpu_ns += ns
        matching += 1
        frames = [strings[functions[func]] for loc in locs for func in locations.get(loc, []) if func in functions]
        if frames:
            flat[frames[0]] += ns
        for name in set(frames):
            cumulative[name] += ns
    return {"available": True, "matching_profile_entries": matching,
            "sampled_cpu_ms": round(cpu_ns / 1e6, 6) if matching else None,
            "top_flat": [{"function": name, "sampled_ms": ns / 1e6} for name, ns in flat.most_common(15)],
            "top_cumulative_nonadditive": [{"function": name, "sampled_ms": ns / 1e6} for name, ns in cumulative.most_common(15)],
            "note": "Sampled estimate; zero matching entries means unavailable, not zero CPU. Child goroutines may inherit labels."}


def dump_trace(go, path, output):
    # Some toolchain distributions build trace on demand through go run.
    attempts = ([go, "tool", "trace", "-d=parsed", str(path)],
                [go, "run", "cmd/trace", "-d=parsed", str(path)])
    for command in attempts:
        output.seek(0)
        output.truncate()
        result = subprocess.run(command, stdout=output, stderr=subprocess.PIPE, timeout=180, check=False)
        if result.returncode == 0:
            output.seek(0)
            return
        error = result.stderr.decode("utf-8", "replace")
        if 'no such tool "trace"' not in error:
            break
    raise ValueError("Go trace decoder failed: " + error[-2000:])


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("trace", type=Path)
    parser.add_argument("--record", type=Path, help="defaults to TRACE.json")
    parser.add_argument("--cpu", type=Path, help="defaults to adjacent cpu.pprof when present")
    parser.add_argument("--go", default="go", help="Go executable, at least the recording runtime version")
    args = parser.parse_args()
    record = json.loads((args.record or Path(str(args.trace) + ".json")).read_text())
    seq_id = record["seq_id"]
    result = {"schema": 1, "record": record, "limits": [
        "State-reader work includes caches, decoding, storage and locks; it is not physical disk time.",
        "Region sums overlap and exclude child goroutines. Do not subtract reader work to infer computation.",
        "Go running wall includes runtime/OS pauses; syscall/cgo wall may include CPU. Use labeled CPU samples for computation.",
        "Task wall excludes label/task setup and report overhead included in the diagnostic record.",
        "Flight recorder age and size are hints; incomplete tasks/regions are unavailable."]}
    try:
        if args.trace.stat().st_size > 64 << 20:
            raise ValueError("trace exceeds the capture's 64 MiB per-file limit")
        with tempfile.TemporaryFile(mode="w+", encoding="utf-8") as dump:
            dump_trace(args.go, args.trace, dump)
            result["runtime_trace"] = analyze_trace(dump, seq_id)
    except (OSError, ValueError, subprocess.SubprocessError) as error:
        result["runtime_trace"] = {"available": False, "error": str(error)}
    cpu = args.cpu or args.trace.parent / "cpu.pprof"
    try:
        result["cpu_profile"] = cpu_profile(cpu, seq_id)
    except (OSError, EOFError, ValueError, IndexError, KeyError) as error:
        result["cpu_profile"] = {"available": False, "error": str(error)}
    print(json.dumps(result, indent=2, ensure_ascii=False, allow_nan=False))
    return 0 if result["runtime_trace"]["available"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
