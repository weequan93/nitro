#!/usr/bin/env python3
"""Bounded overnight RPC evidence capture; --report performs offline review.

Does not reconfigure/restart nodes or other collectors. Profiles are optional,
post-trigger HTTP captures, not a recorder of the already-completed request.
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
import shutil
import signal
import subprocess
import sys
import threading
import time

GIB = 1024**3
METHODS = ("eth_getLogs", "eth_sendRawTransaction")
OUTCOMES = ("success", "failure")
SUFFIXES = ("completed", "ge500ms", "ge1s", "ge3s", "ge5s")
PREFIXES = ("rpc_diagnostics_", "rpc_duration_", "arb_txpublish_", "arb_sequencer_",
            "arb_blockdiag_", "filtermaps_maps_write_diagnostics_", "l2chaindata_",
            "hashdb_", "state_snapshot_", "system_")
SGT = timezone(timedelta(hours=8))


def utc():
    return datetime.now(timezone.utc).isoformat()


def stamp(value):
    return datetime.fromisoformat(value.replace("Z", "+00:00")).timestamp()


def load(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def latency_delta(row, old, interval):
    reason = None
    if old is None:
        reason = "first_sample"
    elif not row.get("identity") or row["identity"] != old.get("identity"):
        reason = "identity_missing_or_changed"
    elif row.get("errors") or old.get("errors"):
        reason = "sample_error"
    elif not 0 < row["monotonic_s"] - old["monotonic_s"] <= max(20, interval * 2 + 10):
        reason = "sample_gap"
    groups = {}
    for method in METHODS:
        for outcome in OUTCOMES:
            keys = [f"rpc_diagnostics_{method}_{outcome}_{s}" for s in SUFFIXES]
            error, values = reason, None
            before, after = (old or {}).get("metrics", {}), row.get("metrics", {})
            if error is None:
                if any(k not in before or k not in after for k in keys):
                    error = "missing_counters"
                elif any(after[k] < before[k] for k in keys):
                    error = "counter_reset"
                else:
                    values = [after[k] - before[k] for k in keys]
                    if any(a < b for a, b in zip(values, values[1:])):
                        error, values = "inconsistent_threshold_deltas", None
            groups[f"{method}:{outcome}"] = {"unavailable": error,
                "seconds": row["monotonic_s"] - old["monotonic_s"] if not error else None,
                "counts": dict(zip(SUFFIXES, values)) if values is not None else None}
    return groups


def scalar_delta(row, old, key, interval=5):
    if (not old or not row.get("identity") or row["identity"] != old.get("identity")
            or row.get("errors") or old.get("errors")):
        return None
    seconds = row["monotonic_s"] - old["monotonic_s"]
    a, b = old.get("metrics", {}).get(key), row.get("metrics", {}).get(key)
    if (0 < seconds <= max(20, interval * 2 + 10) and a is not None
            and b is not None and b >= a):
        return b - a
    return None


def instance_key(identity):
    return tuple((identity or {}).get(k) for k in ("id", "image", "started", "pid", "start_ticks"))


class BudgetError(Exception):
    pass


class Store:
    def __init__(self, root, maximum, minimum_free):
        self.root, self.maximum, self.minimum_free = root, maximum, minimum_free
        self.used = sum(p.stat().st_size for p in root.rglob("*") if p.is_file())
        self.reserved = 0
        self.lock = threading.RLock()
        self.sizes = {}

    def check(self, extra=0):
        if self.used + self.reserved + extra > self.maximum:
            raise BudgetError("output_budget")
        if shutil.disk_usage(self.root).free - self.reserved - extra < self.minimum_free:
            raise BudgetError("free_space_floor")

    def emit(self, relative, data):
        raw = (json.dumps(data, separators=(",", ":"), allow_nan=False) + "\n").encode()
        with self.lock:
            self.check(len(raw))
            with (self.root / relative).open("ab") as f:
                f.write(raw)
            self.used += len(raw)

    def save(self, relative, data):
        raw = (json.dumps(data, indent=2, allow_nan=False) + "\n").encode()
        with self.lock:
            extra = len(raw) - self.sizes.get(relative, 0)
            # Allow a small final status even when the data budget is exhausted.
            if relative != "status.json":
                self.check(max(0, extra))
            p = self.root / relative
            tmp = p.with_name("." + p.name + ".tmp")
            tmp.write_bytes(raw)
            tmp.replace(p)
            self.used += extra
            self.sizes[relative] = len(raw)

    def reserve_profile(self):
        amount = 74 * 1024**2
        with self.lock:
            self.check(amount + 128 * 1024**2)  # Leave room for light monitoring.
            self.reserved += amount
        return amount

    def finish_profile(self, folder, reserved):
        with self.lock:
            self.reserved -= reserved
            self.used += sum(p.stat().st_size for p in folder.iterdir() if p.is_file())


class Capture:
    def __init__(self, args, root, tools):
        self.args, self.root = args, root
        self.base = load("overnight_context", tools / "capture-sequencer-context.py")
        self.ident = load("overnight_identity", tools / "capture-main-cache-io.py")
        self.archive = load("overnight_archive", tools / "review-rpc-tail-archives.py")
        self.blocks = load("overnight_blocks", tools / "watch-slow-sequencer-blocks.py")
        self.store = Store(root, args.max_output_gib * GIB, args.min_free_gib * GIB)
        self.stop = threading.Event()
        self.guard = threading.RLock()
        self.state = Counter()
        self.current = None
        self.last_sample = None
        self.last_error = None
        self.reason = None
        self.pending = None
        self.last_profile = {}
        self.deadline = time.monotonic() + args.hours * 3600
        self.started = utc()

    def bump(self, key, n=1):
        with self.guard:
            self.state[key] += n

    def finish(self, reason):
        with self.guard:
            self.reason = self.reason or reason
        self.stop.set()

    def observe(self):
        row = {"time_utc": utc(), "monotonic_s": time.monotonic(),
               "identity": None, "metrics": {}, "errors": []}
        before = after = None
        try:
            before = self.ident.identity(self.args.container)
        except Exception as e:
            row["errors"].append("identity_before:" + type(e).__name__)
        try:
            text = self.base.fetch(self.args.metrics_url, cap=4 * 1024**2).decode()
            for line in text.splitlines():
                match = self.base.METRIC.match(line)
                if not match:
                    continue
                name, labels, value = match.groups()
                if name.startswith(PREFIXES):
                    n = float(value) if any(c in value for c in ".eE") else int(value)
                    if math.isfinite(n):
                        row["metrics"][name + ("{" + labels + "}" if labels else "")] = n
        except Exception as e:
            row["errors"].append("metrics:" + type(e).__name__)
        if not any(k.startswith("rpc_diagnostics_eth_") for k in row["metrics"]):
            row["errors"].append("rpc_counters_unavailable")
        try:
            after = self.ident.identity(self.args.container)
        except Exception as e:
            row["errors"].append("identity_after:" + type(e).__name__)
        if before is not None and before == after:
            row["identity"] = before
        else:
            row["errors"].append("identity_unverified")
        row["finished_utc"] = utc()
        return row

    def trigger(self, method, evidence):
        if not self.args.profiles:
            return
        now = time.monotonic()
        with self.guard:
            cap = 8 if method == "eth_getLogs" else 16
            cooldown = 300 if method == "eth_getLogs" else 1800
            if self.state["profile_attempts:" + method] >= cap:
                self.state["profile_quota_skips"] += 1
                return
            if now - self.last_profile.get(method, -1e30) < cooldown:
                self.state["profile_cooldown_skips"] += 1
                return
            if self.pending is None or method == "eth_getLogs":
                self.pending = {"method": method, "detected_utc": utc(), "detected_monotonic_s": now,
                                "evidence": evidence}

    def metrics_loop(self):
        previous = None
        while not self.stop.is_set():
            tick = time.monotonic()
            row = self.observe()
            row["latency_deltas"] = latency_delta(row, previous, self.args.metrics_interval)
            self.store.emit("metrics.jsonl", row)
            with self.guard:
                self.current = row["identity"]
                self.last_sample = row["time_utc"]
                self.last_error = row["errors"]
            self.bump("metric_samples")
            self.bump("metric_error_samples", bool(row["errors"]))
            self.bump("metric_unavailable_intervals", any(v["unavailable"] for v in row["latency_deltas"].values()))
            for method in METHODS:
                count = sum((row["latency_deltas"][f"{method}:{o}"]["counts"] or {}).get("ge500ms", 0) for o in OUTCOMES)
                if count:
                    self.trigger(method, {"kind": "counter_delta", "time_utc": row["time_utc"], "ge500ms": count})
            previous = row
            self.stop.wait(max(0, self.args.metrics_interval - (time.monotonic() - tick)))

    def resources_loop(self):
        old_disks = old_cpu = old_process = None
        old_tick = time.monotonic()
        while not self.stop.is_set():
            tick = time.monotonic()
            seconds = tick - old_tick
            disks = self.base.disk_snapshot()
            text = self.base.read_text("/proc/stat") or ""
            cpu = next(([int(n) for n in l.split()[1:9]] for l in text.splitlines() if l.startswith("cpu ")), None)
            rates = None
            if cpu and old_cpu and seconds > 0:
                delta = [n - o for n, o in zip(cpu, old_cpu)]
                total = sum(delta)
                if total > 0 and min(delta) >= 0:
                    rates = {"busy_percent": 100 * (total - delta[3] - delta[4] - delta[7]) / total,
                             "iowait_percent": 100 * delta[4] / total, "steal_percent": 100 * delta[7] / total}
            with self.guard:
                ident = self.current
            try:
                process = self.base.process_snapshot(ident["pid"]) if ident else {"available": False}
            except (OSError, ValueError, IndexError, KeyError) as e:
                process = {"available": False, "reason": "process_snapshot:" + type(e).__name__}
                self.bump("process_snapshot_errors")
            if ident and process.get("start_ticks") != ident["start_ticks"]:
                process = {"available": False, "reason": "process_identity_changed"}
            row = {"time_utc": utc(), "interval_s": seconds, "identity": ident,
                "host_cpu": rates, "load": self.base.load_snapshot(),
                "meminfo_bytes": self.base.named_numbers(self.base.read_text("/proc/meminfo"), 1024),
                "pressure": {k: self.base.pressure("/proc/pressure/" + k) for k in ("cpu", "memory", "io")},
                "filesystems": self.base.filesystem_snapshot(),
                "devices": self.base.disk_delta(disks, old_disks, seconds) if seconds > 0 else None,
                "process": self.base.process_delta(process, old_process or {}, seconds) if seconds > 0 else process}
            self.store.emit("resources.jsonl", row)
            self.bump("resource_samples")
            old_disks, old_cpu, old_process, old_tick = disks, cpu, process, tick
            self.stop.wait(max(0, 1 - (time.monotonic() - tick)))

    def log_event(self, raw):
        if not any(marker in raw for marker in
                   (b"RPC stage timings", b"Sequencer block diagnostics", b"Log index write timings")):
            return
        parsed = self.archive.parse_line(raw)
        if parsed is None:
            self.bump("log_parse_errors")
            return
        token, message, fields = parsed
        t = self.archive.timestamp(token, 0, time.time())
        row = {"observed_utc": utc(), "log_time_utc": datetime.fromtimestamp(t, timezone.utc).isoformat() if t else None}
        if message == "RPC stage timings" and fields.get("method") in METHODS:
            total = self.archive.numeric(fields.get("total_ms"))
            if total is None or total < 500:
                return
            row.update(kind="rpc_stage", method=fields["method"], outcome=fields.get("outcome"),
                       trace=self.archive.integer(fields.get("trace")), total_ms=total)
            with self.guard:
                row["observed_identity"] = self.current
            if isinstance(fields.get("failure_kind"), str):
                row["failure_kind"] = fields["failure_kind"][:100]
            stage_keys = tuple(s + suffix for s in self.archive.STAGES for suffix in ("_ms", "_calls", "_max_ms"))
            stage_keys += tuple(
                key for key in fields
                if (key.startswith("tx_publish_") or key.startswith("sync_temp_range_wait_"))
                and key.endswith(("_ms", "_calls", "_max_ms"))
            )
            for key in stage_keys + self.archive.SHAPES:
                n = self.archive.numeric(fields.get(key))
                if n is not None:
                    row[key] = n
            seq_id = self.archive.integer(fields.get("tx_publish_last_seq_id"))
            if seq_id:
                row["tx_publish_last_seq_id"] = seq_id
            if row["method"] == "eth_getLogs":
                row["query_truncated"] = fields.get("request_params_truncated")
                try:
                    params = json.loads(fields["request_params_json"])
                    if isinstance(params, list) and params and isinstance(params[0], dict):
                        row["query"] = {k: v for k, v in params[0].items() if k in ("fromBlock", "toBlock", "blockHash", "address", "topics")}
                except (KeyError, ValueError, TypeError):
                    pass
            self.store.emit("events.jsonl", row)
            self.bump("rpc_stage_records")
            self.trigger(row["method"], {k: row.get(k) for k in ("kind", "log_time_utc", "observed_utc", "trace", "total_ms")})
        elif message == "Log index write timings":
            record = {}
            for key in self.archive.INDEX_WRITE_FIELDS:
                parse = self.archive.numeric if key.endswith("_ms") else self.archive.integer
                value = parse(fields.get(key))
                if value is not None:
                    record[key] = value
            if record.get("write_seq") and "wall_ms" in record:
                with self.guard:
                    row["observed_identity"] = self.current
                row.update(kind="index_write", record=record)
                self.store.emit("events.jsonl", row)
                self.bump("index_write_records")
        elif b"Sequencer block diagnostics" in raw:
            record = self.blocks.parse_block_diagnostics(raw.decode(errors="replace"))
            if record:
                with self.guard:
                    row["observed_identity"] = self.current
                row.update(kind="block_stage", record=record)
                self.store.emit("events.jsonl", row)
                self.bump("block_stage_records")

    def logs_loop(self):
        stream, first, partial, dropping = None, True, b"", False
        try:
            while not self.stop.is_set():
                path = self.args.log_file
                if stream is None:
                    try:
                        stream = path.open("rb")
                        if first:
                            stream.seek(0, 2)
                            first = False
                    except OSError:
                        self.bump("log_open_errors")
                        self.stop.wait(2)
                        continue
                budget = 4 * 1024**2
                while budget > 0 and not self.stop.is_set():
                    raw = stream.readline(1024**2 + 1)
                    if not raw:
                        break
                    budget -= len(raw)
                    if dropping:
                        dropping = not raw.endswith(b"\n")
                        continue
                    raw = partial + raw
                    partial = b""
                    if len(raw) > 1024**2:
                        self.bump("oversized_log_lines_dropped")
                        dropping = not raw.endswith(b"\n")
                        continue
                    if not raw.endswith(b"\n"):
                        partial = raw
                        break
                    try:
                        self.log_event(raw)
                    except (ValueError, TypeError, KeyError, IndexError, OverflowError):
                        self.bump("log_parse_errors")
                try:
                    current = path.stat()
                    old = os.fstat(stream.fileno())
                    if stream.tell() >= old.st_size and (current.st_dev, current.st_ino) != (old.st_dev, old.st_ino):
                        stream.close()
                        stream, partial, dropping = None, b"", False
                        self.bump("log_rotations")
                    elif current.st_size < stream.tell() and (current.st_dev, current.st_ino) == (old.st_dev, old.st_ino):
                        stream.seek(0)
                        partial = b""
                        dropping = False
                        self.bump("log_copytruncate_seen")
                except OSError:
                    pass
                self.stop.wait(.25)
        finally:
            if stream:
                stream.close()

    def profile(self, trigger):
        method = trigger["method"]
        now = time.monotonic()
        with self.guard:
            cap = 8 if method == "eth_getLogs" else 16
            cooldown = 300 if method == "eth_getLogs" else 1800
            if (now - trigger["detected_monotonic_s"] > 45
                    or now - self.last_profile.get(method, -1e30) < cooldown
                    or self.state["profile_attempts:" + method] >= cap):
                self.state["profile_stale_or_ineligible_skips"] += 1
                return
        if self.deadline - time.monotonic() < 20:
            self.bump("profile_deadline_skips")
            return
        try:
            reserved = self.store.reserve_profile()
        except BudgetError:
            self.bump("profile_budget_skips")
            return
        with self.guard:
            self.state["profile_attempts:" + method] += 1
            self.last_profile[method] = time.monotonic()
            number = sum(self.state["profile_attempts:" + m] for m in METHODS)
        folder = self.root / "profiles" / ("capture-%03d" % number)
        folder.mkdir(parents=True)
        result = {"kind": "profile", "trigger": trigger, "path": str(folder), "started_utc": utc(),
                  "limit": "Post-trigger capture: only new slow completions in this bracket support incident correlation; this is not a pre-event recorder."}
        children = []
        try:
            before = self.observe()
            (folder / "before.json").write_text(json.dumps(before) + "\n")
            if before["errors"] or not before["identity"]:
                raise ValueError("profile_identity_unavailable")
            pid = before["identity"]["pid"]
            jobs = (("cpu", "profile", 8 * 1024**2, "cpu.pprof"),
                    ("trace", "trace", 64 * 1024**2, "runtime.trace"))
            for name, endpoint, cap, filename in jobs:
                output, errors = (folder / filename).open("wb"), (folder / (name + ".stderr")).open("wb")
                try:
                    cmd = ["nsenter", "--target", str(pid), "--net", "curl", "-fsS", "--connect-timeout", "2",
                           "--max-time", "20", "--max-filesize", str(cap),
                           "http://127.0.0.1:6071/debug/pprof/" + endpoint + "?seconds=5"]
                    child = subprocess.Popen(cmd, stdout=output, stderr=errors, stdin=subprocess.DEVNULL)
                    children.append((name, child, filename))
                finally:
                    output.close()
                    errors.close()
            timeout = time.monotonic() + 24
            while any(c.poll() is None for _, c, _ in children):
                if self.stop.wait(.2) or time.monotonic() >= timeout:
                    raise TimeoutError("profile_cancelled_or_timed_out")
            result["returncodes"] = {n: c.returncode for n, c, _ in children}
            if any(c.returncode != 0 or not (folder / f).stat().st_size for _, c, f in children):
                raise ValueError("profiler_HTTP_failed_or_busy")
            after = self.observe()
            (folder / "after.json").write_text(json.dumps(after) + "\n")
            groups = latency_delta(after, before, 10)
            result.update(latency_deltas=groups, finished_utc=utc())
            result["identity_valid"] = before["identity"] == after["identity"] and not after["errors"]
            result["matching_slow_completions"] = sum((groups[f"{method}:{o}"]["counts"] or {}).get("ge500ms", 0) for o in OUTCOMES)
            result["classification"] = "incident_window" if result["identity_valid"] and result["matching_slow_completions"] else "baseline_or_unavailable"
            self.bump("profiles_saved")
            self.bump("incident_profiles", result["classification"] == "incident_window")
        except Exception as e:
            result.update(classification="capture_failed", error=type(e).__name__ + ":" + str(e), finished_utc=utc())
            self.bump("profile_errors")
        finally:
            for _, child, _ in children:
                if child.poll() is None:
                    child.terminate()
                    try:
                        child.wait(timeout=2)
                    except subprocess.TimeoutExpired:
                        child.kill()
                        child.wait(timeout=2)
            (folder / "manifest.json").write_text(json.dumps(result, indent=2) + "\n")
            self.store.finish_profile(folder, reserved)
        self.store.emit("profiles.jsonl", result)
        print("PROFILE_RESULT", json.dumps(result), flush=True)

    def profiles_loop(self):
        while not self.stop.wait(.25):
            with self.guard:
                trigger, self.pending = self.pending, None
            if trigger:
                self.profile(trigger)

    def status(self, kind):
        with self.guard:
            row = {"kind": kind, "time_utc": utc(), "pid": os.getpid(), "reason": self.reason,
                   "started_utc": self.started, "remaining_s": max(0, int(self.deadline - time.monotonic())),
                   "last_sample_utc": self.last_sample, "last_errors": self.last_error,
                   "identity": self.current, "stats": dict(self.state),
                   "managed_output_bytes": self.store.used, "profile_reserved_bytes": self.store.reserved}
        self.store.save("status.json", row)
        print("STATUS", json.dumps(row), flush=True)

    def run(self):
        def guarded(name, fn):
            try:
                fn()
            except Exception as e:
                self.bump("worker_errors")
                self.finish(name + ":" + type(e).__name__ + ":" + str(e))
        self.status("running")
        threads = [threading.Thread(target=guarded, args=(name, fn), name=name)
                   for name, fn in (("metrics", self.metrics_loop), ("resources", self.resources_loop),
                                    ("logs", self.logs_loop), ("profiles", self.profiles_loop))]
        for t in threads:
            t.start()
        try:
            while not self.stop.wait(10):
                if time.monotonic() >= self.deadline:
                    self.finish("duration_elapsed")
                    break
                try:
                    with self.store.lock:
                        self.store.check()
                    self.status("heartbeat")
                except Exception as e:
                    self.finish(type(e).__name__ + ":" + str(e))
        finally:
            self.stop.set()
            for t in threads:
                t.join()
            try:
                self.status("complete" if self.reason == "duration_elapsed" else "stopped")
            except OSError as e:
                print("FINAL_STATUS_WRITE_FAILED", type(e).__name__, self.reason, flush=True)


def read_rows(path):
    if not path.exists():
        return
    with path.open() as f:
        for line in f:
            try:
                row = json.loads(line)
                if isinstance(row, dict):
                    yield row
            except ValueError:
                continue


def interval_context(row, previous):
    m = row["metrics"]
    result = {}
    for stage in ("queue_pop", "queue_wait", "block_execution", "sequence", "block_write", "consensus_write"):
        value = m.get('arb_txpublish_' + stage + '{quantile="0.99"}')
        result[stage + "_rolling_p99_ms_at_end"] = value / 1000 if value is not None else None
    for stage, name in (("scheduler", "system_cpu_schedlatency"), ("gc_pause", "system_memory_pauses")):
        value = m.get(name + '{quantile="0.99"}')
        result[stage + "_rolling_p99_ms_at_end"] = value / 1e6 if value is not None else None
    for name in ("l2chaindata_compact_debt", "l2chaindata_compact_inprogress", "l2chaindata_tables_level0_sublevel"):
        result[name] = m.get(name)
    seconds = row["monotonic_s"] - previous["monotonic_s"]
    for key in ("l2chaindata_compact_input", "l2chaindata_compact_output", "l2chaindata_disk_write"):
        delta = scalar_delta(row, previous, key)
        result[key + "_MiB_s"] = delta / seconds / 1024**2 if delta is not None else None
    for outcome in OUTCOMES:
        result["eth_call_" + outcome + "_completed_delta"] = scalar_delta(
            row, previous, "rpc_duration_eth_call_" + outcome + "_count")
    return result


def resource_values(row):
    result = {}
    for k, v in (row.get("host_cpu") or {}).items():
        result["host_" + k] = v
    process = row.get("process") or {}
    if process.get("available"):
        result["nitro_cpu_cores"] = (process.get("rates") or {}).get("cpu_cores")
    for dev, values in (row.get("devices") or {}).items():
        if dev not in ("vdb", "vdc"):
            continue
        for k in ("read_await_ms", "write_await_ms", "average_queue", "busy_percent"):
            result[dev + "_" + k] = values.get(k)
    return {k: v for k, v in result.items() if isinstance(v, (int, float)) and math.isfinite(v)}


def report(root, hours):
    end = time.time()
    cutoff = end - hours * 3600 if hours else 0
    totals, coverage, skips, instances = {}, Counter(), Counter(), set()
    latest, first, errors, dropped = None, None, 0, 0
    prior = None
    call_totals, call_coverage = Counter(), Counter()
    hot = {m: [] for m in METHODS}
    for row in read_rows(root / "metrics.jsonl"):
        t = stamp(row["time_utc"])
        latest = t if latest is None else max(latest, t)
        if not cutoff <= t <= end:
            prior = row
            continue
        first = t if first is None else min(first, t)
        errors += bool(row.get("errors"))
        ident = row.get("identity") or {}
        if ident:
            instances.add(instance_key(ident))
        for name, item in row.get("latency_deltas", {}).items():
            if item.get("unavailable"):
                skips[name + ":" + item["unavailable"]] += 1
                continue
            if t - item["seconds"] < cutoff:
                continue
            key = (instance_key(ident), name)
            total = totals.setdefault(key, dict.fromkeys(SUFFIXES, 0))
            for k in SUFFIXES:
                total[k] += item["counts"][k]
            coverage[key] += item["seconds"]
        if prior and prior.get("identity") == row.get("identity") and not row.get("errors") and not prior.get("errors"):
            dt = row["monotonic_s"] - prior["monotonic_s"]
            if 0 < dt <= 20 and stamp(prior["time_utc"]) >= cutoff:
                delta = scalar_delta(row, prior, "rpc_diagnostics_details_dropped")
                dropped += delta or 0
                for outcome in OUTCOMES:
                    delta = scalar_delta(row, prior, "rpc_duration_eth_call_" + outcome + "_count")
                    if delta is not None:
                        key = (instance_key(ident), outcome)
                        call_totals[key] += delta
                        call_coverage[key] += dt
                for method in METHODS:
                    counts = [row.get("latency_deltas", {}).get(f"{method}:{o}", {}).get("counts") for o in OUTCOMES]
                    valid_counts = [v for v in counts if v is not None]
                    slow = sum(v["ge500ms"] for v in valid_counts)
                    if slow:
                        hot[method].append({"method": method, "start_utc": prior["time_utc"], "end_utc": row["time_utc"],
                            "identity": ident, "counts_by_outcome": dict(zip(OUTCOMES, counts)), "ge500ms": slow,
                            "context_at_end": interval_context(row, prior)})
                        hot[method].sort(key=lambda v: -v["ge500ms"])
                        del hot[method][6:]
        prior = row
    status = json.loads((root / "status.json").read_text()) if (root / "status.json").exists() else {}
    print("CAPTURE", root)
    print("STATUS", json.dumps(status))
    print("WINDOW_SGT", datetime.fromtimestamp(cutoff or first or end, SGT).isoformat(), "->", datetime.fromtimestamp(end, SGT).isoformat())
    print("LATEST_SGT", datetime.fromtimestamp(latest, SGT).isoformat() if latest else None,
          "AGE_SECONDS", round(end - latest, 1) if latest else None)
    print("QUALITY", json.dumps({"error_samples": errors, "details_dropped_delta": dropped, "unavailable_intervals": dict(skips)}))
    if latest is None or end - latest > 60:
        print("STALE_DATA: latest metrics are absent or older than 60 seconds")
    if not totals:
        print("NO_VALID_INTERVALS: no RPC counts can be inferred for this requested window")
    for instance in sorted(instances):
        node, image, started, pid, ticks = instance
        print("INSTANCE", node, image, started, "pid=" + str(pid), "start_ticks=" + str(ticks))
        print("method\toutcome\tcompleted\t>=500ms\tpercent\t>=1s\t>=3s\t>=5s\tvalid_hours")
        for method in METHODS:
            for outcome in OUTCOMES:
                key = (instance, f"{method}:{outcome}")
                v = totals.get(key)
                if v is None:
                    print(method, outcome, "unavailable", sep="\t")
                    continue
                pct = "%.4f%%" % (100 * v["ge500ms"] / v["completed"]) if v["completed"] else "n/a"
                print(method, outcome, v["completed"], v["ge500ms"], pct, v["ge1s"], v["ge3s"], v["ge5s"], round(coverage[key] / 3600, 4), sep="\t")
        for outcome in OUTCOMES:
            key = (instance, outcome)
            seconds = call_coverage[key]
            print("ETH_CALL", outcome, "completed=" + str(call_totals[key] if seconds else "unavailable"),
                  "valid_hours=" + str(round(seconds / 3600, 4)))
    hot_windows = [v for values in hot.values() for v in values]
    for v in hot_windows:
        v["resource_sample_count"] = 0
        v["resource_sums"], v["resource_counts"], v["resource_max"] = Counter(), Counter(), {}
    for r in read_rows(root / "resources.jsonl") if hot_windows else ():
        t = stamp(r["time_utc"])
        for v in hot_windows:
            if (stamp(v["start_utc"]) - 1 <= t <= stamp(v["end_utc"]) + 1
                    and r.get("identity") == v["identity"]):
                v["resource_sample_count"] += 1
                for key, n in resource_values(r).items():
                    v["resource_sums"][key] += n
                    v["resource_counts"][key] += 1
                    v["resource_max"][key] = max(n, v["resource_max"].get(key, n))
    for v in hot_windows:
        sums, counts = v.pop("resource_sums"), v.pop("resource_counts")
        v["resource_sample_mean"] = {k: n / counts[k] for k, n in sums.items()}
        print("HOT_INTERVAL", json.dumps(v))
    profiles = [r for r in read_rows(root / "profiles.jsonl") if cutoff <= stamp(r["started_utc"]) <= end]
    print("PROFILES", json.dumps(dict(Counter(r.get("classification") for r in profiles))))
    for r in sorted(profiles, key=lambda r: -r.get("matching_slow_completions", 0))[:12]:
        print("PROFILE", json.dumps({k: r.get(k) for k in ("path", "trigger", "classification", "matching_slow_completions", "error")}))
    examples = {m: [] for m in METHODS}
    diagnostic_examples = {"block_stage": [], "index_write": []}
    diagnostic_counts = Counter()
    for r in read_rows(root / "events.jsonl"):
        if not cutoff <= stamp(r["observed_utc"]) <= end:
            continue
        if r.get("kind") == "rpc_stage" and r.get("method") in examples:
            values = examples[r["method"]]
            values.append(r)
            values.sort(key=lambda x: -x["total_ms"])
            del values[6:]
        elif r.get("kind") in diagnostic_examples:
            kind = r["kind"]
            values = diagnostic_examples[kind]
            diagnostic_counts[kind] += 1
            values.append(r)
            metric = "total_ms" if kind == "block_stage" else "wall_ms"
            values.sort(key=lambda x: -x["record"][metric])
            del values[6:]
    for values in examples.values():
        for r in values:
            print("RPC_EXAMPLE", json.dumps(r))
    for kind, label in (("block_stage", "BLOCK_DIAGNOSTIC"), ("index_write", "INDEX_WRITE")):
        print(label + "_RECORDS", diagnostic_counts[kind])
        for r in diagnostic_examples[kind]:
            print(label + "_EXAMPLE", json.dumps(r))
    print("LIMITS: profile captures begin after triggers; baseline/failed profiles cannot prove incident causes. Counter brackets include HTTP overhead. Rolling p99 snapshots are not whole-window p99 and cannot be added/subtracted. queue_wait includes waiting for execution results. CPU/reader/lock timings overlap. Missing data is not zero. File logging can drop details. Review identity and UTC windows before correlating.")


def main():
    p = argparse.ArgumentParser(description=__doc__)
    mode = p.add_mutually_exclusive_group(required=True)
    mode.add_argument("--out-dir", type=Path)
    mode.add_argument("--report", type=Path)
    p.add_argument("--last-hours", type=float)
    p.add_argument("--hours", type=float, default=24)
    p.add_argument("--metrics-interval", type=float, default=5)
    p.add_argument("--max-output-gib", type=float, default=4)
    p.add_argument("--min-free-gib", type=float, default=20)
    p.add_argument("--container", default="main-nitro-1")
    p.add_argument("--metrics-url", default="http://127.0.0.1:6070/debug/metrics/prometheus")
    p.add_argument("--log-file", type=Path, default=Path("/data/main/config/My Arbitrum L3 Chain/nitro.log"))
    p.add_argument("--helpers-dir", type=Path, default=Path("/data/nitro/scripts"))
    p.add_argument("--profiles", action=argparse.BooleanOptionalAction, default=True)
    p.add_argument("--lock-file", type=Path, default=Path("/run/lock/rpc-overnight-evidence.lock"))
    args = p.parse_args()
    os.umask(0o077)
    if args.last_hours is not None and (not math.isfinite(args.last_hours) or args.last_hours <= 0):
        p.error("--last-hours must be finite and positive")
    if args.report:
        return report(args.report.resolve(), args.last_hours)
    if not (math.isfinite(args.hours) and 0 < args.hours <= 72 and 2 <= args.metrics_interval <= 30
            and 1 <= args.max_output_gib <= 16 and math.isfinite(args.min_free_gib) and args.min_free_gib >= 2):
        p.error("Require hours 0..72, interval 2..30s, output 1..16GiB and free floor >=2GiB")
    if not Path("/proc/stat").exists():
        p.error("Run capture on the Linux node host")
    root = args.out_dir.resolve()
    root.mkdir(parents=True, exist_ok=True)
    if (root / "run.json").exists():
        p.error("Use a fresh output directory")
    args.lock_file.parent.mkdir(parents=True, exist_ok=True)
    with args.lock_file.open("a") as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            p.error("An overnight collector already owns the lock")
        if shutil.disk_usage(root).free < args.min_free_gib * GIB:
            p.error("Insufficient free disk space")
        tools = root / "tools"
        tools.mkdir(exist_ok=True)
        hashes = {}
        for name in ("capture-sequencer-context.py", "capture-main-cache-io.py", "review-rpc-tail-archives.py", "watch-slow-sequencer-blocks.py"):
            data = (args.helpers_dir / name).read_bytes()
            (tools / name).write_bytes(data)
            hashes[name] = hashlib.sha256(data).hexdigest()
        collector = Capture(args, root, tools)
        for sig in (signal.SIGTERM, signal.SIGINT):
            signal.signal(sig, lambda *_: collector.finish("signal"))
        planned = datetime.now(timezone.utc) + timedelta(hours=args.hours)
        collector.store.save("run.json", {"schema": 1, "pid": os.getpid(), "started_utc": collector.started,
            "planned_end_utc": planned.isoformat(), "planned_end_sgt": planned.astimezone(SGT).isoformat(),
            "hours": args.hours, "container": args.container, "metrics_interval_s": args.metrics_interval,
            "resource_interval_s": 1, "profiles": args.profiles, "profile_seconds": 5,
            "profile_quota": {"eth_getLogs": 8, "eth_sendRawTransaction": 16},
            "profile_cooldown_s": {"eth_getLogs": 300, "eth_sendRawTransaction": 1800},
            "max_output_gib": args.max_output_gib, "min_free_gib": args.min_free_gib,
            "helper_sha256": hashes, "collector_sha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
            "log_file": str(args.log_file), "terminal_log_utc_offset_s": 0,
            "limits": ["Future log tail only; multiple rapid rotations may leave gaps.", "Profiles are post-trigger, not pre-event recordings.", "Profile limits stop profiling only; light monitoring continues.", "No offline trace parsing runs on the production host overnight.", "Existing collectors and node settings are not modified."]})
        collector.run()


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, BudgetError) as e:
        print("CAPTURE_ERROR", type(e).__name__, str(e), file=sys.stderr, flush=True)
        raise SystemExit(1)
