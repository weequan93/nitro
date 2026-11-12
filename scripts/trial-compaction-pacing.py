#!/usr/bin/env python3
"""Prepare, apply, or roll back a single-setting main compaction trial.

Preparation is the default and does not change the running service or its
configuration. Apply/rollback briefly restart only the Compose nitro service.
All other settings, Compose inputs and image must match the prepared snapshot.
"""

import argparse
from copy import deepcopy
import csv
from datetime import datetime, timezone
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import shlex
import stat
import subprocess
import sys
import tempfile


CONTAINER = "main-nitro-1"
KEY = "persistent.pebble.compaction-output-bytes-per-second"
CONCURRENCY_KEY = "persistent.pebble.max-concurrent-compactions"


def now():
    return datetime.now(timezone.utc).isoformat()


def digest(data):
    return hashlib.sha256(data).hexdigest()


def read_bytes(path):
    with Path(path).open("rb") as stream:
        data = stream.read(2 * 1024**2 + 1)
    if len(data) > 2 * 1024**2:
        raise ValueError("Configuration/plan exceeds 2 MiB")
    return data


def run(command, timeout=15):
    result = subprocess.run(command, capture_output=True, text=True, timeout=timeout)
    if result.returncode:
        raise RuntimeError("Command failed (exit %s); command/config contents omitted" % result.returncode)
    return result.stdout


def inspect(kind, name):
    return json.loads(run(["docker", kind, "inspect", name]))[0]


def option(argv, name):
    found = []
    for i, arg in enumerate(argv):
        if arg == "--":
            break
        if arg == "--" + name:
            if i + 1 >= len(argv) or argv[i + 1].startswith("--"):
                raise ValueError("Missing value for " + name)
            found.append(argv[i + 1])
        elif arg.startswith("--" + name + "="):
            found.append(arg.split("=", 1)[1])
    return found


def lookup(config, key):
    if key in config:
        return config[key]
    value = config
    for part in key.split("."):
        if not isinstance(value, dict) or part not in value:
            return None
        value = value[part]
    return value


def candidate(config, rate, key=KEY):
    if not isinstance(config, dict) or type(rate) is not int or not 0 < rate < 2**63:
        raise ValueError("Require a JSON object and positive integer setting")
    if key not in (KEY, CONCURRENCY_KEY):
        raise ValueError("Unsupported trial setting")
    if key == CONCURRENCY_KEY:
        if rate != 1 or type(lookup(config, key)) is not int or lookup(config, key) != 2:
            raise ValueError("Concurrency trial requires an explicit JSON change from 2 to 1")
        if type(lookup(config, KEY)) is not int or lookup(config, KEY) != 64 * 1024**2:
            raise ValueError("Concurrency trial requires existing 64 MiB/s pacing")
    # Refuse partial dotted parents rather than guessing how koanf merges them.
    node = config
    parts = key.split(".")
    for depth, part in enumerate(parts[:-1]):
        if not isinstance(node, dict):
            raise ValueError("Ambiguous nested configuration")
        if any(".".join(parts[depth:end]) in node for end in range(depth + 2, len(parts))):
            raise ValueError("Partial dotted pacing parent requires separate review")
        if depth and ".".join(parts[depth:]) in node:
            raise ValueError("Partial dotted pacing key requires separate review")
        node = node.get(part, {})
    if isinstance(lookup(config, key), bool):
        raise ValueError("Pacing value cannot be a boolean")
    if key == KEY and lookup(config, key) not in (None, 0):
        raise ValueError("Preparation requires pacing absent or zero")
    changed = deepcopy(config)
    if key in config:
        if "persistent" in config:
            raise ValueError("Ambiguous flat/nested pacing configuration")
        changed[key] = rate
    else:
        node = changed
        for part in key.split(".")[:-1]:
            node = node.setdefault(part, {})
            if not isinstance(node, dict):
                raise ValueError("Ambiguous nested configuration")
        node[key.split(".")[-1]] = rate
    return changed


def runtime_fingerprint(info):
    config = info["Config"]
    return digest(json.dumps({
        "image": info["Image"], "command": config.get("Cmd"),
        "entrypoint": config.get("Entrypoint"), "env": config.get("Env"),
        "user": config.get("User"), "working_dir": config.get("WorkingDir"),
        "host_config": info.get("HostConfig"), "mounts": info.get("Mounts"),
    }, sort_keys=True).encode())


def check_labels(info, plan):
    labels = info["Config"].get("Labels") or {}
    for key, expected in plan["labels"].items():
        if labels.get(key) != expected:
            raise ValueError("Running Compose labels changed: " + key)


def compose_hash(prefix):
    output = run(prefix + ["config", "--hash", "nitro"])
    matches = re.findall(r"(?m)^nitro\s+([0-9a-f]{64})\s*$", output)
    if len(matches) != 1:
        raise ValueError("Cannot verify Compose service hash")
    return matches[0]


def native_argv():
    top = run(["docker", "top", CONTAINER, "-eo", "pid,comm"])
    pids = [line.split()[0] for line in top.splitlines()[1:]
            if len(line.split()) == 2 and line.split()[1] == "nitro"]
    if len(pids) != 1:
        raise ValueError("Require exactly one native main nitro process")
    return read_bytes("/proc/" + pids[0] + "/cmdline").decode().rstrip("\0").split("\0")


def local_config(info, argv, allow_cli_pacing=False):
    files = [path for value in option(argv, "conf.file") for path in next(csv.reader([value]))]
    if len(files) != 1 or not files[0].startswith("/"):
        raise ValueError("Require exactly one absolute --conf.file")
    blocked = ("conf.string", "conf.env-prefix", "conf.s3.secret-key")
    if not allow_cli_pacing:
        blocked += (KEY,)
    if any(option(argv, name) for name in blocked):
        raise ValueError("CLI pacing or other configuration loader requires separate review")
    target = Path(files[0])
    matches = []
    for mount in info.get("Mounts") or []:
        if mount.get("Type") != "bind":
            continue
        try:
            relative = target.relative_to(mount["Destination"])
        except ValueError:
            continue
        source = Path(mount["Source"]) / relative
        matches.append((len(Path(mount["Destination"]).parts), source))
    if not matches:
        raise ValueError("Node configuration is not an available host bind mount")
    path = max(matches, key=lambda entry: entry[0])[1].resolve(strict=True)
    if not path.is_file():
        raise ValueError("Require a regular bound configuration file")
    config = json.loads(read_bytes(path))
    if not isinstance(config, dict):
        raise ValueError("Node JSON must be an object")
    if any(lookup(config, key) for key in ("conf.file", "conf.string", "conf.env-prefix", "conf.s3.secret-key")):
        raise ValueError("JSON activates additional configuration loaders")
    return path, config


def save(path, obj):
    path.write_text(json.dumps(obj, indent=2) + "\n")
    path.chmod(0o600)


def service_run(command, log_path, timeout):
    with log_path.open("a") as output:
        result = subprocess.run(command, stdin=subprocess.DEVNULL, stdout=output,
                                stderr=subprocess.STDOUT, timeout=timeout)
    if result.returncode:
        raise RuntimeError("Service operation failed; inspect private log: " + str(log_path))


def compose_resources(prefix, info):
    """Reject common docker-update drift that recreation would silently reset."""
    service = json.loads(run(prefix + ["config", "--format", "json"]))["services"]["nitro"]
    limits = service.get("deploy", {}).get("resources", {}).get("limits", {})
    if service.get("scale", 1) != 1 or service.get("deploy", {}).get("replicas", 1) != 1:
        raise ValueError("Require exactly one main replica")
    checks = {
        "NanoCpus": round(float(service.get("cpus", limits.get("cpus", 0))) * 10**9),
        "CpuShares": service.get("cpu_shares", 0), "CpuPeriod": service.get("cpu_period", 0),
        "CpuQuota": service.get("cpu_quota", 0), "CpusetCpus": service.get("cpuset", ""),
        "Memory": int(service.get("mem_limit", limits.get("memory", 0))),
        "MemorySwap": int(service.get("memswap_limit", 0)),
        "MemoryReservation": int(service.get("mem_reservation", service.get("deploy", {}).get(
            "resources", {}).get("reservations", {}).get("memory", 0))),
    }
    host = info["HostConfig"]
    for key, value in checks.items():
        actual = host.get(key, "" if key == "CpusetCpus" else 0)
        # Docker assigns 2x memory as swap allowance when none is specified.
        if key == "MemorySwap" and not value and actual == 2 * checks["Memory"]:
            continue
        if actual != value:
            raise ValueError("Runtime resource differs from Compose: " + key)


def show(plan_path):
    script = str(Path(__file__).resolve())
    print("APPLY:", shlex.join(["python3", "-B", script, "--apply", str(plan_path)]))
    print("ROLLBACK:", shlex.join(["python3", "-B", script, "--rollback", str(plan_path)]))


def pending_original(info, argv, prefix, current, baseline_path, config_path):
    """Prove which JSON the unchanged original container had loaded."""
    baseline = json.loads(read_bytes(baseline_path))
    original = read_bytes(config_path)
    if (baseline.get("schema") != 1 or baseline.get("container_id") != info["Id"] or
            baseline.get("started_at") != info["State"]["StartedAt"] or
            baseline.get("image") != info["Image"]):
        raise ValueError("Pending edit requires the unchanged container from the saved runtime baseline")
    model = json.loads(run(prefix + ["config", "--format", "json"]))
    model["services"]["nitro"].pop("image", None)
    layout = deepcopy(info)
    layout["Image"] = "image_checked_separately"
    expected = {
        "bound_json": digest(original),
        "native_arguments": digest(json.dumps(argv).encode()),
        "container_layout_without_image": runtime_fingerprint(layout),
        "compose_model_without_main_image": digest(json.dumps(model, sort_keys=True).encode()),
    }
    if baseline.get("hashes") != expected:
        raise ValueError("Saved loaded JSON or deployment inputs do not match the runtime baseline")
    if candidate(json.loads(original), 1, CONCURRENCY_KEY) != current:
        raise ValueError("Pending JSON changes more than concurrency 2 to 1")
    return original


def prepare(root, rate, setting=KEY, pending_baseline=None, loaded_config=None):
    info = inspect("container", CONTAINER)
    if not info["State"].get("Running"):
        raise ValueError("Main is not running")
    labels = info["Config"].get("Labels") or {}
    if (labels.get("com.docker.compose.project"), labels.get("com.docker.compose.service")) != ("main", "nitro"):
        raise ValueError("Unexpected Compose project/service")
    files = labels.get("com.docker.compose.project.config_files", "").split(",")
    directory = labels.get("com.docker.compose.project.working_dir")
    if not directory or not Path(directory).is_absolute() or not all(Path(f).is_absolute() for f in files):
        raise ValueError("Require absolute original Compose files and directory")
    prefix = ["docker", "compose", "--project-directory", directory, "-p", "main"]
    for filename in files:
        prefix.extend(["-f", filename])
    expected_hash = compose_hash(prefix)
    if expected_hash != labels.get("com.docker.compose.config-hash"):
        raise ValueError("Compose files/environment differ from running service")
    compose_resources(prefix, info)
    if inspect("image", info["Config"]["Image"])["Id"] != info["Image"]:
        raise ValueError("Running image tag has changed")
    argv = native_argv()
    path, config = local_config(info, argv)
    source = read_bytes(path)
    if json.loads(source) != config:
        raise ValueError("Node configuration changed during preparation")
    pending = pending_baseline is not None or loaded_config is not None
    if pending and (setting != CONCURRENCY_KEY or pending_baseline is None or loaded_config is None):
        raise ValueError("Pending concurrency requires both runtime baseline and original loaded JSON")
    if setting == CONCURRENCY_KEY:
        if option(argv, CONCURRENCY_KEY):
            raise ValueError("CLI concurrency would override the JSON trial")
        started = datetime.fromisoformat(info["State"]["StartedAt"].replace("Z", "+00:00")).timestamp()
        if path.stat().st_mtime > started and not pending:
            raise ValueError("JSON was edited after container start; loaded settings are unverified")
    if pending:
        original = pending_original(info, argv, prefix, config, pending_baseline, loaded_config)
        replacement = source
        config = json.loads(original)
    else:
        original = source
        replacement = (json.dumps(candidate(config, rate, setting), indent=2) + "\n").encode()
    if read_bytes(path) != source:
        raise ValueError("Node configuration changed during preparation")
    root.mkdir(parents=True, exist_ok=True)
    work = Path(tempfile.mkdtemp(prefix="main-pacing-" if setting == KEY else "main-compactions-", dir=root))
    work.chmod(0o700)
    (work / "before.json").write_bytes(original)
    (work / "candidate.json").write_bytes(replacement)
    (work / "before.json").chmod(0o600)
    (work / "candidate.json").chmod(0o600)
    attrs = path.stat()
    plan = {
        "prepared_utc": now(), "source": str(path), "setting": setting, "value": rate,
        "pending_edit": pending,
        "before_sha": digest(original), "candidate_sha": digest(replacement),
        "uid": attrs.st_uid, "gid": attrs.st_gid, "mode": stat.S_IMODE(attrs.st_mode),
        "container_id": info["Id"], "started_at": info["State"]["StartedAt"],
        "runtime_fingerprint": runtime_fingerprint(info), "image": info["Image"],
        "native_arguments_sha": digest(json.dumps(argv).encode()),
        "labels": {key: labels[key] for key in (
            "com.docker.compose.project", "com.docker.compose.service", "com.docker.compose.config-hash",
            "com.docker.compose.project.config_files", "com.docker.compose.project.working_dir")},
        "image_ref": info["Config"]["Image"], "compose": prefix, "compose_hash": expected_hash,
        "inputs": {filename: digest(read_bytes(filename)) for filename in files},
    }
    if setting == KEY:
        plan["rate"] = rate  # Retain compatibility with existing pacing plans.
    plan_path = work / "plan.json"
    save(plan_path, plan)
    print(json.dumps({"state": "prepared_no_service_change", "plan": str(plan_path),
                      "image": info["Image"], "max_concurrent_compactions": lookup(config, "persistent.pebble.max-concurrent-compactions"),
                      "setting": setting, "before_value": lookup(config, setting), "after_value": rate,
                      "before_output_bytes_s": lookup(config, KEY) or 0,
                      "after_output_bytes_s": rate if setting == KEY else lookup(config, KEY)}))
    show(plan_path)
    return plan_path


def replace_config(path, data, plan):
    fd, temporary = tempfile.mkstemp(prefix=".pacing-", dir=path.parent)
    try:
        with os.fdopen(fd, "wb") as output:
            output.write(data)
            output.flush()
            os.fchmod(output.fileno(), plan["mode"])
            os.fchown(output.fileno(), plan["uid"], plan["gid"])
            os.fsync(output.fileno())
        os.replace(temporary, path)
        directory_fd = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY)
        try:
            os.fsync(directory_fd)
        finally:
            os.close(directory_fd)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def activate(plan_path, rollback=False):
    plan = json.loads(read_bytes(plan_path))
    setting = plan.get("setting", KEY)
    value = plan.get("value", plan.get("rate"))
    path = Path(plan["source"])
    with (path.parent / ".nitro-compaction-pacing.lock").open("a") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        before = read_bytes(plan_path.parent / "before.json")
        proposed = read_bytes(plan_path.parent / "candidate.json")
        if digest(before) != plan["before_sha"] or digest(proposed) != plan["candidate_sha"]:
            raise ValueError("Prepared backup/candidate changed")
        if json.loads(proposed) != candidate(json.loads(before), value, setting):
            raise ValueError("Candidate changes more than the single trial setting")
        for filename, expected in plan["inputs"].items():
            if digest(read_bytes(filename)) != expected:
                raise ValueError("Compose input changed")
        # Recovery remains possible if Compose removed the old container and
        # failed before creating its replacement. Require our operation journal.
        containers = run(["docker", "container", "ls", "-aq", "--no-trunc", "--filter", "name=^/" + CONTAINER + "$"]).splitlines()
        info = inspect("container", CONTAINER) if containers else None
        journal_path = plan_path.parent / "apply.json"
        if info is None and not (rollback and journal_path.exists()):
            raise ValueError("Main container missing without a recorded trial operation")
        if info and runtime_fingerprint(info) != plan["runtime_fingerprint"]:
            raise ValueError("Main runtime changed; prepare a new trial")
        if inspect("image", plan["image_ref"])["Id"] != plan["image"]:
            raise ValueError("Image tag changed; no service operation performed")
        if compose_hash(plan["compose"]) != plan["compose_hash"]:
            raise ValueError("Rendered Compose configuration changed")
        if info:
            compose_resources(plan["compose"], info)
            check_labels(info, plan)
            if (not rollback and plan.get("native_arguments_sha") and
                    digest(json.dumps(native_argv()).encode()) != plan["native_arguments_sha"]):
                raise ValueError("Native Nitro arguments changed")
        current = digest(read_bytes(path))
        attrs = path.stat()
        if (attrs.st_uid, attrs.st_gid, stat.S_IMODE(attrs.st_mode)) != (plan["uid"], plan["gid"], plan["mode"]):
            raise ValueError("Live JSON owner/permissions changed")
        if not rollback and (info["Id"] != plan["container_id"] or
                             info["State"]["StartedAt"] != plan["started_at"]):
            raise ValueError("Main restarted since preparation")
        expected = ((plan["before_sha"], plan["candidate_sha"]) if rollback else
                    (plan["candidate_sha"],) if plan.get("pending_edit") else (plan["before_sha"],))
        if current not in expected:
            raise ValueError("Live JSON changed; refusing to overwrite other edits")
        original_instance = info and info["Id"] == plan["container_id"] and info["State"]["StartedAt"] == plan["started_at"]
        if rollback and current == plan["before_sha"] and original_instance and info["State"].get("Running") and not journal_path.exists():
            print("Original JSON already active; no service operation performed")
            return
        operation = "rollback" if rollback else "apply"
        save(plan_path.parent / (operation + ".json"), {"state": "restart_requested", "time_utc": now()})
        print("Briefly stopping main/nitro; only " + setting + " will change.", flush=True)
        log_path = plan_path.parent / (operation + ".compose.private.log")
        if info:
            service_run(plan["compose"] + ["stop", "-t", "120", "nitro"], log_path, 150)
        replace_config(path, before if rollback else proposed, plan)
        # Explicit force-recreate ensures fresh file bind mounts and JSON reload.
        service_run(plan["compose"] + ["up", "-d", "--no-deps", "--no-build", "--pull", "never", "--force-recreate", "nitro"], log_path, 180)
        observed = inspect("container", CONTAINER)
        check_labels(observed, plan)
        if (not observed["State"].get("Running") or observed["State"].get("OOMKilled") or
                runtime_fingerprint(observed) != plan["runtime_fingerprint"]):
            raise ValueError("Post-start runtime mismatch; use saved rollback")
        active = json.loads(before if rollback else proposed)
        record = {"state": "started_health_pending", "operation": operation, "time_utc": now(),
                  "setting": setting, "value": lookup(active, setting),
                  "output_bytes_s": lookup(active, KEY) or 0,
                  "max_concurrent_compactions": lookup(active, CONCURRENCY_KEY), "image": observed["Image"],
                  "started_at": observed["State"]["StartedAt"]}
        save(plan_path.parent / (operation + ".json"), record)
        print(json.dumps(record))
        print("Check RPC/head recovery, pacing counters, latency and debt before extending the trial.")
        show(plan_path)


def main():
    os.umask(0o077)
    parser = argparse.ArgumentParser(description=__doc__)
    group = parser.add_mutually_exclusive_group()
    group.add_argument("--apply", type=Path)
    group.add_argument("--rollback", type=Path)
    group.add_argument("--prepare-and-apply", action="store_true",
                       help="Save and print the verified plan, then apply it")
    parser.add_argument("--root", type=Path, default=Path("/data/deploy-backups"))
    parser.add_argument("--rate", type=int, default=64 * 1024**2)
    parser.add_argument("--compactions", type=int, choices=(1,),
                        help="Prepare a 2-to-1 concurrency trial while retaining 64 MiB/s pacing")
    parser.add_argument("--pending-baseline", type=Path,
                        help="Saved runtime baseline proving the original container, for an un-restarted JSON edit")
    parser.add_argument("--loaded-config", type=Path,
                        help="Exact original loaded JSON backup whose hash matches --pending-baseline")
    args = parser.parse_args()
    if not 0 < args.rate < 2**63:
        parser.error("rate must be a positive signed 64-bit integer")
    if (args.pending_baseline or args.loaded_config) and (
            args.compactions is None or args.apply or args.rollback):
        parser.error("Pending-edit inputs are only for preparing --compactions 1")
    if args.compactions is not None and args.rate != 64 * 1024**2:
        parser.error("Concurrency trial retains 64 MiB/s; do not supply another --rate")
    if args.apply or args.rollback:
        activate((args.apply or args.rollback).resolve(), rollback=bool(args.rollback))
    else:
        plan = prepare(args.root.resolve(), args.compactions if args.compactions is not None else args.rate,
                       CONCURRENCY_KEY if args.compactions is not None else KEY,
                       args.pending_baseline, args.loaded_config)
        if args.prepare_and_apply:
            activate(plan)


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, RuntimeError, subprocess.SubprocessError) as error:
        reason = str(error) if isinstance(error, (ValueError, RuntimeError)) else type(error).__name__
        print("ERROR: " + reason, file=sys.stderr)
        print("Trial stopped. Review the saved plan and private operation log (if created); "
              "no automatic rollback. Use the printed rollback command after a failed activation.", file=sys.stderr)
        raise SystemExit(1)
