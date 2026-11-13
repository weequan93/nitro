#!/usr/bin/env python3
"""Check the applied RPC read image, then start a fresh complete capture phase."""

import argparse
from datetime import datetime, timedelta, timezone
import fcntl
import importlib.util
import json
import math
import os
from pathlib import Path
import signal
import subprocess
import sys
import time


DIRECTORY = Path(__file__).resolve().parent
RPC_URL = "http://127.0.0.1:1100"
METRICS_URL = "http://127.0.0.1:6070/debug/metrics/prometheus"


def module(name, filename):
    spec = importlib.util.spec_from_file_location(name, DIRECTORY / filename)
    result = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(result)
    return result


def emit(kind, **values):
    print(json.dumps({"kind": kind, "time_utc": datetime.now(timezone.utc).isoformat(), **values}), flush=True)


def unchanged(ops, lifecycle, target):
    info = ops.inspect("container", ops.CONTAINER)
    if lifecycle.identity(info) != target or not info["State"].get("Running") or info["State"].get("OOMKilled"):
        raise ValueError("Applied node instance changed; no new capture started")
    return info


def health(suite, rpc, ops, lifecycle, target, timeout):
    deadline, previous, progress = time.monotonic() + timeout, None, 0
    last_error = "RPC/metrics are not ready"

    def call(method):
        payload = json.loads(suite.fetch(RPC_URL, json.dumps(
            {"jsonrpc": "2.0", "id": 1, "method": method, "params": []}).encode()))
        if not isinstance(payload, dict) or payload.get("error") or "result" not in payload:
            raise ValueError("RPC check failed: " + method)
        return payload["result"]

    while True:
        unchanged(ops, lifecycle, target)
        try:
            value = call("eth_blockNumber")
            if not isinstance(value, str) or not value.startswith("0x") or int(value, 16) <= 0:
                raise ValueError("RPC head is invalid")
            head = int(value, 16)
            if call("eth_syncing") is not False:
                raise ValueError("Node is still syncing")
            if call("arb_checkPublisherHealth") is not None:
                raise ValueError("Publisher health result is unexpected")
            metrics = suite.fetch(METRICS_URL).decode()
            if not any(line.startswith("l2chaindata_compact_debt ") for line in metrics.splitlines()):
                raise ValueError("Database metrics are unavailable")
            values = rpc.parse(metrics)
            if any(key not in values for key in rpc.TRACKED_KEYS):
                raise ValueError("Subsecond RPC counters are unavailable")
            unchanged(ops, lifecycle, target)
            if previous is not None and head < previous:
                raise ValueError("RPC head went backwards")
            if previous is not None and head > previous:
                return {"heads": [previous, head], "head_advanced": True, "syncing": False,
                        "publisher_health_result": None, "subsecond_counters_available": True}
            previous, last_error = head, "Waiting for the RPC head to advance"
        except (OSError, ValueError) as error:
            previous, last_error = None, str(error)
        if time.monotonic() >= deadline:
            raise ValueError("Readiness timeout: " + last_error)
        if time.monotonic() >= progress:
            emit("waiting_for_readiness", reason=last_error)
            progress = time.monotonic() + 30
        time.sleep(min(2, max(0, deadline - time.monotonic())))


def stop_previous(suite, previous_dir, timeout=90):
    previous_dir = previous_dir.resolve()
    parents = []
    for row in suite.inventory():
        item = suite.monitor(row)
        if item and item["name"] == "capture-sequencer-suite.py":
            if Path(item["out_dir"]).resolve() != previous_dir:
                raise ValueError("Another suite is running outside the named previous phase")
            parents.append(item)
    if len(parents) > 1:
        raise ValueError("Multiple previous suite parents found; retain them for review")
    for row in parents:
        emit("stopping_previous_capture", pid=row["pid"], out_dir=row["out_dir"])
        suite.send(row, signal.SIGTERM)
    deadline = time.monotonic() + timeout
    while True:
        pending = [row for row in parents if (current := suite.process_info(row["pid"]))
                   and current["start_ticks"] == row["start_ticks"]]
        if not pending:
            return parents
        if time.monotonic() >= deadline:
            raise ValueError("Previous capture is still exiting; no new collectors started")
        time.sleep(0.2)


def phase_metadata(target, checked, warmup_minutes):
    checked_at = datetime.now(timezone.utc)
    started = datetime.fromisoformat(target["started_at"].replace("Z", "+00:00"))
    warmup_until = started + timedelta(minutes=warmup_minutes)
    return {"schema": 1, "image": target["image"], "node_identity": target,
            "checked_utc": checked_at.isoformat(), "health": checked,
            "warmup_exclude_first_minutes_after_node_start": warmup_minutes,
            "warmup_until_utc": warmup_until.isoformat(),
            "comparison_not_before_utc": max(checked_at, warmup_until).isoformat(),
            "note": "Time-based startup exclusion; compare workload and background load after this boundary."}


def launch(args, exec_function=None):
    suite = module("read_capture_suite", "capture-sequencer-suite.py")
    prep = module("read_capture_profile", "prepare-rpc-read-switch.py")
    lifecycle = module("read_capture_lifecycle", "switch-rpc-subsecond-image.py")
    rpc = module("read_capture_rpc", "capture-rpc-diagnostics.py")
    root = args.out_dir.resolve()
    root.mkdir(parents=True, exist_ok=True)
    if any((root / name).exists() for name in ("run.json", "tools", "status.json")):
        raise ValueError("Output already contains a capture; use a fresh directory")
    profile = prep.profile_for_plan(args.plan)
    checker = profile.load_checker()
    ops = checker.ops
    plan, info = lifecycle.verify_inputs(args.plan, profile, checker)
    if plan["candidate_image"] != args.expected_image or info["Image"] != args.expected_image:
        raise ValueError("Applied image differs from the expected verified candidate")
    applied = json.loads(ops.read_bytes(args.plan.parent / "apply.json"))
    target = lifecycle.identity(info)
    if applied.get("state") != "started_health_pending" or applied.get("target") != target:
        raise ValueError("Applied instance does not match the saved operation journal")
    unchanged(ops, lifecycle, target)
    lifecycle.signal_preflight(info)
    before = checker.observe()
    failures = checker.compare(before, plan["baseline"], expected_image=args.expected_image, compactions=1)
    if failures:
        raise ValueError("; ".join(failures))
    # Check capture sources, disk device and budgets before ending the old phase.
    for name in suite.SOURCES:
        content = ops.read_bytes(DIRECTORY / name)
        compile(content, name, "exec")
    suite.validate_device("vdb")
    support = module("read_capture_budget", "capture-sequencer-longrun.py")
    if support.budget_reason(root, 3 * 1024**3, 2 * 1024**3):
        raise ValueError("Capture disk budget is unavailable")
    checked = health(suite, rpc, ops, lifecycle, target, args.ready_timeout)
    old = stop_previous(suite, args.previous_suite)
    unchanged(ops, lifecycle, target)
    after = checker.observe()
    failures = checker.compare(after, before, expected_image=args.expected_image, compactions=1)
    if failures:
        raise ValueError("Runtime changed during capture handoff: " + "; ".join(failures))
    phase = phase_metadata(target, checked, args.warmup_minutes)
    phase["native_pid"] = info["State"]["Pid"]
    phase["pid1_is_native_nitro"] = True
    phase["previous_suite"] = str(args.previous_suite.resolve())
    phase["previous_suite_parents"] = old
    suite.save(root / "phase.json", phase)
    emit("health_verified", out_dir=str(root), **phase)
    command = [sys.executable, "-B", "-u", str(DIRECTORY / "capture-sequencer-suite.py"),
               "--hours", str(args.hours), "--segment-minutes", "60", "--compactions", "1",
               "--pacing-bytes-per-second", str(64 * 1024**2), "--ready-timeout", str(args.ready_timeout),
               "--replace-existing", "--rpc-counters", "--device", "vdb", "--out-dir", str(root)]
    emit("starting_complete_suite", out_dir=str(root))
    (exec_function or os.execv)(sys.executable, command)


def main():
    os.umask(0o077)
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--plan", type=Path, required=True)
    parser.add_argument("--out-dir", type=Path, required=True)
    parser.add_argument("--previous-suite", type=Path, required=True)
    parser.add_argument("--expected-image", required=True)
    parser.add_argument("--hours", type=float, default=24)
    parser.add_argument("--ready-timeout", type=float, default=1800)
    parser.add_argument("--warmup-minutes", type=float, default=30)
    args = parser.parse_args()
    if not (all(math.isfinite(v) for v in (args.hours, args.ready_timeout, args.warmup_minutes))
            and 0 < args.hours <= 168 and args.ready_timeout >= 5 and 0 <= args.warmup_minutes <= 120):
        parser.error("Invalid capture/readiness/warmup duration")
    args.plan = args.plan.resolve(strict=True)
    with Path("/run/lock/nitro-rpc-read-capture.lock").open("a") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        launch(args)


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, RuntimeError, subprocess.SubprocessError) as error:
        print("RPC read capture start stopped: " + str(error), file=sys.stderr)
        raise SystemExit(1)
