#!/usr/bin/env python3
"""Apply/roll back a prepared main image switch after a verified normal exit."""

import argparse
from copy import deepcopy
import fcntl
import importlib.util
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import time


DIRECTORY = Path(__file__).resolve().parent
OBSERVE_SECONDS = 300


def module(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    result = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(result)
    return result


def layout(info, ops):
    value = deepcopy(info)
    value["Image"] = "image_checked_separately"
    return ops.runtime_fingerprint(value)


def identity(info):
    return {"id": info["Id"], "started_at": info["State"]["StartedAt"],
            "restarts": info.get("RestartCount", 0), "image": info["Image"]}


def signal_preflight(info, proc_root=Path("/proc")):
    if (proc_root / str(info["State"]["Pid"]) / "comm").read_text().strip() != "nitro":
        raise ValueError("Require native Nitro as PID 1 before graceful shutdown")


def review_shutdown(info, text):
    state = info["State"]
    if (state.get("Running") or state.get("Restarting") or state.get("OOMKilled")
            or state.get("Dead") or state.get("Error") or state.get("ExitCode") != 0
            or not state.get("FinishedAt") or state["FinishedAt"].startswith("0001-")):
        raise ValueError("Main did not exit normally; container retained without recreation")
    clean = re.sub(r"\x1b\[[0-9;]*m", "", text).lower()
    if "shutting down because of sigint" not in clean or "blockchain stopped" not in clean:
        raise ValueError("Normal signal/blockchain shutdown markers missing; no recreation")
    patterns = (
        r"failed to close database on shutdown", r"error on stack close", r"backend stop.*err=",
        r"dangling trie nodes after full cleanup", r"\b(?:crit|critical|panic|fatal)\b",
        r"(?:failed|error|cannot|could not|unable to).*?(?:close|flush|commit|journal|persist|sync|write).*?(?:database|trie|state|db)",
        r"(?:database|trie|state|db).*?(?:close|flush|commit|journal|persist|sync|write).*?(?:failed|error)",
    )
    if any(re.search(pattern, clean) for pattern in patterns):
        raise ValueError("Shutdown close/flush/fatal error found; no recreation")


def graceful_stop(info, record, journal, ops, lock_fd):
    process = None
    if "stop_requested_utc" not in record:
        record.update(state="waiting_for_normal_exit", stop_requested_utc=ops.now(), source=identity(info))
        ops.save(journal, record)
        with journal.with_suffix(".stop.private.log").open("ab") as output:
            process = subprocess.Popen([
                "docker", "container", "stop", "--signal", "SIGTERM", "--timeout", "-1", info["Id"]],
                stdin=subprocess.DEVNULL, stdout=output, stderr=subprocess.STDOUT,
                start_new_session=True, pass_fds=(lock_fd,) if isinstance(lock_fd, int) else tuple(lock_fd))
    deadline, progress = time.monotonic() + OBSERVE_SECONDS, time.monotonic() + 30
    while True:
        observed = ops.inspect("container", ops.CONTAINER)
        if identity(observed) != record["source"]:
            raise ValueError("Container identity changed during shutdown")
        if not observed["State"]["Running"]:
            break
        if process is not None and process.poll() is not None:
            raise ValueError("Stop client exited while main is running; no forced signal or recreation")
        if time.monotonic() >= deadline:
            raise ValueError("Still waiting for normal exit; unlimited stop continues without force-kill. Retain this plan.")
        if time.monotonic() >= progress:
            print("Waiting for Nitro normal exit; no forced signal or recreation.", flush=True)
            progress = time.monotonic() + 30
        time.sleep(1)
    if process is not None and process.wait(timeout=10):
        raise ValueError("Stop client returned an error; no recreation")
    result = subprocess.run(["docker", "logs", "--timestamps", "--since", record["stop_requested_utc"], info["Id"]],
                            stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                            text=True, check=True, timeout=30)
    text = result.stdout
    evidence = journal.with_suffix(".shutdown.private.log")
    evidence.write_text(text)
    if evidence.stat().st_size > 2 * 1024**2:
        raise ValueError("Shutdown evidence exceeds 2 MiB; review it before recreating")
    review_shutdown(observed, text)
    snapshot = journal.with_suffix(".stopped.private.json")
    ops.save(snapshot, observed)
    record.update(state="normal_exit_verified", shutdown_log_sha256=ops.digest(evidence.read_bytes()),
                  stopped_snapshot_sha256=ops.digest(snapshot.read_bytes()))
    ops.save(journal, record)


def verified_stop(record, journal, ops, current=None):
    snapshot = journal.with_suffix(".stopped.private.json")
    log = journal.with_suffix(".shutdown.private.log")
    if (ops.digest(ops.read_bytes(snapshot)) != record.get("stopped_snapshot_sha256")
            or ops.digest(ops.read_bytes(log)) != record.get("shutdown_log_sha256")):
        raise ValueError("Saved normal-exit evidence changed")
    observed = json.loads(ops.read_bytes(snapshot))
    if identity(observed) != record.get("source") or (current and identity(current) != identity(observed)):
        raise ValueError("Stopped container identity differs from verified evidence")
    review_shutdown(current or observed, log.read_text())


def verify_inputs(path, prep, checker, allow_missing=False):
    ops = checker.ops
    plan = json.loads(ops.read_bytes(path))
    if (plan.get("schema") != 1 or plan.get("state") != "prepared_no_service_change"
            or plan.get("candidate_image") != prep.CANDIDATE or plan.get("candidate_ref") != prep.TAG
            or plan["baseline"]["image"] != prep.BASE
            or plan.get("shutdown") != {"signal": "SIGTERM", "force_kill": False, "required_exit_code": 0}):
        raise ValueError("Unsupported switch plan or shutdown policy")
    if hasattr(prep, "verify_build"):
        prep.verify_build(plan)
    override = Path(plan["override"])
    if (ops.digest(ops.read_bytes(override)) != plan["override_sha256"]
            or json.loads(ops.read_bytes(override)) != {"services": {"nitro": {"image": prep.TAG}}}):
        raise ValueError("Image-only override changed")
    if (ops.digest(ops.read_bytes(plan["nodeconfig_path"])) != plan["nodeconfig_sha256"]
            or ops.digest(ops.read_bytes(path.parent / "nodeconfig.before.json")) != plan["nodeconfig_sha256"]
            or plan["nodeconfig_sha256"] != plan["baseline"]["hashes"]["bound_json"]):
        raise ValueError("Node JSON or saved backup changed")
    for name, saved in plan["compose_inputs"].items():
        if any(ops.digest(ops.read_bytes(p)) != saved["sha256"] for p in (name, saved["backup"])):
            raise ValueError("Original Compose input or backup changed")
    ids = ops.run(["docker", "container", "ls", "-aq", "--no-trunc", "--filter", "name=^/" + ops.CONTAINER + "$"]).splitlines()
    if len(ids) > 1 or (not ids and not allow_missing):
        raise ValueError("Main container missing without owned normal-exit evidence")
    info = ops.inspect("container", ops.CONTAINER) if ids else None
    labels = info["Config"]["Labels"] if info else None
    directory = labels["com.docker.compose.project.working_dir"] if labels else plan["compose"][3]
    expected_prefix = ["docker", "compose", "--project-directory", directory, "-p", "main"]
    for filename in plan["baseline"]["compose_files"]:
        expected_prefix += ["-f", filename]
    if labels and (labels.get("com.docker.compose.project"), labels.get("com.docker.compose.service")) != ("main", "nitro"):
        raise ValueError("Unexpected Compose project/service")
    if (plan["compose"] != expected_prefix or plan["candidate_compose"] != expected_prefix + ["-f", str(override)]
            or set(plan["compose_inputs"]) != set(plan["baseline"]["compose_files"])):
        raise ValueError("Compose prefix/input set differs")
    before = json.loads(ops.run(plan["compose"] + ["config", "--format", "json"]))
    after = json.loads(ops.run(plan["candidate_compose"] + ["config", "--format", "json"]))
    prep.image_only_model(before, after)
    for model, key in ((before, "compose_model_sha256"), (after, "candidate_compose_sha256")):
        if ops.digest(json.dumps(model, sort_keys=True).encode()) != plan[key]:
            raise ValueError("Rendered Compose/environment changed")
    if (ops.inspect("image", prep.TAG)["Id"] != prep.CANDIDATE
            or ops.inspect("image", plan["baseline"]["image_ref"])["Id"] != prep.BASE):
        raise ValueError("Candidate or rollback image reference changed")
    if info is None:
        return plan, None
    if layout(info, ops) != plan["baseline"]["hashes"]["container_layout_without_image"]:
        raise ValueError("Container layout/resources changed")
    active_files = labels.get("com.docker.compose.project.config_files", "").split(",")
    original_files = plan["baseline"]["compose_files"]
    if (info["Image"], active_files) not in (
            (prep.BASE, original_files), (prep.CANDIDATE, original_files + [str(override)])):
        raise ValueError("Running image/Compose inputs are outside this trial")
    active_prefix = plan["compose"] if info["Image"] == prep.BASE else plan["candidate_compose"]
    if ops.compose_hash(active_prefix) != labels.get("com.docker.compose.config-hash"):
        raise ValueError("Active Compose hash differs")
    ops.compose_resources(active_prefix, info)
    return plan, info


def switch(path, rollback=False, prep=None, checker=None, lock_fd=None):
    prep = prep or module("subsecond_plan", DIRECTORY / "prepare-rpc-subsecond-switch.py")
    checker = checker or prep.load_checker()
    ops = checker.ops
    operation = "rollback" if rollback else "apply"
    journal = path.parent / (operation + ".json")
    record = json.loads(ops.read_bytes(journal)) if journal.exists() else {"operation": operation}
    recovery_journal, recovery = journal, record
    if rollback and not journal.exists() and (path.parent / "apply.json").exists():
        recovery_journal = path.parent / "apply.json"
        recovery = json.loads(ops.read_bytes(recovery_journal))
    can_recover = recovery.get("state") == "recreation_requested"
    if can_recover:
        verified_stop(recovery, recovery_journal, ops)
    plan, info = verify_inputs(path, prep, checker, allow_missing=can_recover)
    target = prep.BASE if rollback else prep.CANDIDATE
    target_prefix = plan["compose"] if rollback else plan["candidate_compose"]
    if info and info["Image"] == target and info["State"]["Running"]:
        if not rollback and not journal.exists():
            raise ValueError("Candidate is active without this plan's operation journal")
        if record.get("target") and record["target"] != identity(info):
            raise ValueError("Previously started instance changed; review before another operation")
        if not rollback:
            record.update(state="started_health_pending", target=identity(info), time_utc=ops.now())
            ops.save(journal, record)
        print("Requested image is already running; no service operation performed")
        return
    recovered = can_recover and (info is None or not info["State"]["Running"])
    if recovered:
        verified_stop(recovery, recovery_journal, ops, info)
        record.update(recovery, operation=operation)
    elif "source" in record:
        if record["source"] != identity(info):
            raise ValueError("Operation source identity changed; retain journal for review")
    else:
        if not info["State"]["Running"]:
            raise ValueError("Main is stopped without this operation's shutdown evidence")
        if not rollback and (info["Id"] != plan["baseline"]["container_id"]
                             or info["State"]["StartedAt"] != plan["baseline"]["started_at"]):
            raise ValueError("Main restarted since switch preparation")
        current = checker.observe()
        failures = checker.compare(current, plan["baseline"], expected_image=info["Image"], compactions=1)
        if failures:
            raise ValueError("; ".join(failures))
        signal_preflight(info)
    if not recovered:
        graceful_stop(info, record, journal, ops, lock_fd)
    else:
        # Copy the verified evidence to this operation before any recreation.
        for suffix in (".shutdown.private.log", ".stopped.private.json"):
            data = recovery_journal.with_suffix(suffix).read_bytes()
            journal.with_suffix(suffix).write_bytes(data)
    record.update(state="recreation_requested", requested_image=target)
    ops.save(journal, record)
    # Image IDs/references and all inputs are checked again after the potentially long shutdown.
    verify_inputs(path, prep, checker, allow_missing=recovered)
    ops.service_run(target_prefix + ["up", "-d", "--no-deps", "--no-build", "--pull", "never", "--force-recreate", "nitro"],
                    path.parent / (operation + ".compose.private.log"), 180)
    observed = ops.inspect("container", ops.CONTAINER)
    if (observed["Image"] != target or not observed["State"]["Running"] or observed["State"].get("OOMKilled")
            or layout(observed, ops) != plan["baseline"]["hashes"]["container_layout_without_image"]):
        raise ValueError("Started container differs; retain operation logs and use saved rollback")
    expected_files = plan["baseline"]["compose_files"] + ([] if rollback else [plan["override"]])
    labels = observed["Config"]["Labels"]
    if (labels.get("com.docker.compose.project.config_files", "").split(",") != expected_files
            or labels.get("com.docker.compose.config-hash") != ops.compose_hash(target_prefix)):
        raise ValueError("Started Compose inputs/hash differ; retain operation logs")
    record.update(state="started_health_pending", time_utc=ops.now(), target=identity(observed))
    ops.save(journal, record)
    print(json.dumps({"state": record["state"], "operation": operation, "image": target,
                      "started_at": observed["State"]["StartedAt"], "compactions": 1,
                      "pacing_bytes_per_second": 64 * 1024**2}, indent=2), flush=True)


def main(prep_loader=None):
    os.umask(0o077)
    parser = argparse.ArgumentParser(description=__doc__)
    choice = parser.add_mutually_exclusive_group(required=True)
    choice.add_argument("--apply", type=Path)
    choice.add_argument("--rollback", type=Path)
    args = parser.parse_args()
    path = (args.apply or args.rollback).resolve(strict=True)
    config_path = Path(json.loads(path.read_text())["nodeconfig_path"])
    with (Path("/run/lock/nitro-rpc-subsecond-switch.lock").open("a") as lock,
          (config_path.parent / ".nitro-compaction-pacing.lock").open("a") as pacing_lock):
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        fcntl.flock(pacing_lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        prep = prep_loader(path) if prep_loader else None
        switch(path, rollback=args.rollback is not None, prep=prep,
               lock_fd=(lock.fileno(), pacing_lock.fileno()))


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, RuntimeError, subprocess.SubprocessError) as error:
        print("Image switch stopped: " + str(error), file=sys.stderr)
        raise SystemExit(1)
