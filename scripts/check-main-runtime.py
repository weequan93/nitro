#!/usr/bin/env python3
"""Save/check main deployment settings without changing or restarting the node.

Requires trial-compaction-pacing.py beside this script. Current CLI and bound
JSON declarations are checked; undeclared image defaults are not inferred.
No RPC, Docker exec, database access, service operation or configuration edit.
"""

import argparse
from copy import deepcopy
from datetime import datetime
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys


spec = importlib.util.spec_from_file_location(
    "pacing_support", Path(__file__).with_name("trial-compaction-pacing.py"))
ops = importlib.util.module_from_spec(spec)
spec.loader.exec_module(ops)

KEYS = (ops.KEY, "persistent.pebble.max-concurrent-compactions",
        "execution.caching.trie-clean-cache", "execution.caching.database-cache")


def selected(config, argv, key):
    declarations = ops.option(argv, key)
    if len(declarations) > 1:
        raise ValueError("Duplicate CLI declaration: " + key)
    if declarations:
        value, source = declarations[0], "cli"
    else:
        value = ops.lookup(config, key)
        source = "json" if value is not None else "image_default_unresolved"
    if source == "image_default_unresolved":
        return {"value": None, "source": source}
    if type(value) is int and value >= 0:
        return {"value": value, "source": source}
    if isinstance(value, str) and value.isdigit():
        return {"value": int(value), "source": source}
    raise ValueError("Unsupported tuning value: " + key)


def observe():
    info = ops.inspect("container", ops.CONTAINER)
    if not info["State"].get("Running") or info["State"].get("OOMKilled"):
        raise ValueError("Main is not running or reports OOMKilled")
    labels = info["Config"].get("Labels") or {}
    if (labels.get("com.docker.compose.project"), labels.get("com.docker.compose.service")) != ("main", "nitro"):
        raise ValueError("Unexpected Compose project/service")
    argv = ops.native_argv()
    path, config = ops.local_config(info, argv, allow_cli_pacing=True)
    started = datetime.fromisoformat(info["State"]["StartedAt"].replace("Z", "+00:00")).timestamp()
    if path.stat().st_mtime > started:
        raise ValueError("Bound JSON is newer than container start; loaded values are unverified")
    data = ops.read_bytes(path)
    if json.loads(data) != config:
        raise ValueError("JSON changed during inspection")
    files = labels.get("com.docker.compose.project.config_files", "").split(",")
    directory = labels.get("com.docker.compose.project.working_dir", "")
    if not Path(directory).is_absolute() or not all(Path(f).is_absolute() for f in files):
        raise ValueError("Original Compose inputs are unavailable")
    prefix = ["docker", "compose", "--project-directory", directory, "-p", "main"]
    for filename in files:
        prefix += ["-f", filename]
    if ops.compose_hash(prefix) != labels.get("com.docker.compose.config-hash"):
        raise ValueError("Rendered Compose hash differs from the running service")
    ops.compose_resources(prefix, info)
    if ops.inspect("image", info["Config"]["Image"])["Id"] != info["Image"]:
        raise ValueError("Image reference no longer points to the running image")
    model = json.loads(ops.run(prefix + ["config", "--format", "json"]))
    model["services"]["nitro"].pop("image", None)
    layout = deepcopy(info)
    layout["Image"] = "image_checked_separately"
    current = ops.inspect("container", ops.CONTAINER)
    if (current["Id"] != info["Id"] or current["State"]["StartedAt"] != info["State"]["StartedAt"] or
            ops.runtime_fingerprint(current) != ops.runtime_fingerprint(info)):
        raise ValueError("Main changed during inspection")
    return {
        "schema": 1, "observed_utc": ops.now(), "container_id": info["Id"],
        "started_at": info["State"]["StartedAt"], "image": info["Image"],
        "image_ref": info["Config"]["Image"], "restart_count": info.get("RestartCount"),
        "settings": {key: selected(config, argv, key) for key in KEYS},
        "hashes": {
            "bound_json": ops.digest(data),
            "native_arguments": ops.digest(json.dumps(argv).encode()),
            "container_layout_without_image": ops.runtime_fingerprint(layout),
            "compose_model_without_main_image": ops.digest(json.dumps(model, sort_keys=True).encode()),
        },
        "compose_files": files,
    }


def compare(current, baseline=None, expected_image=None, pacing=64 * 1024**2, compactions=2):
    failures = []
    for key, expected in ((ops.KEY, pacing), (KEYS[1], compactions)):
        if current["settings"][key]["value"] != expected:
            failures.append("Unexpected declaration: " + key)
    if expected_image and current["image"] != expected_image:
        failures.append("Running image does not match expected image ID")
    if baseline is not None:
        if baseline.get("schema") != 1:
            raise ValueError("Unsupported baseline schema")
        if (set(baseline.get("hashes") or {}) != set(current["hashes"]) or
                set(baseline.get("settings") or {}) != set(KEYS)):
            raise ValueError("Baseline is incomplete")
        if current["image"] != baseline["image"] and not expected_image:
            failures.append("Image changed; supply its verified image ID using --expected-image")
        for key in KEYS:
            if current["settings"][key] != baseline["settings"][key]:
                failures.append("Selected configuration changed: " + key)
        for key, value in baseline["hashes"].items():
            if current["hashes"].get(key) != value:
                failures.append("Deployment input changed: " + key)
    return failures


def main():
    os.umask(0o077)
    parser = argparse.ArgumentParser(description=__doc__)
    group = parser.add_mutually_exclusive_group()
    group.add_argument("--save", type=Path, help="Save a new private baseline; existing files are refused")
    group.add_argument("--baseline", type=Path, help="Compare with a saved baseline")
    parser.add_argument("--expected-image", help="Verified Docker image ID (sha256:...), required for a changed image")
    parser.add_argument("--pacing-bytes-per-second", type=int, default=64 * 1024**2)
    parser.add_argument("--compactions", type=int, default=2)
    args = parser.parse_args()
    if args.pacing_bytes_per_second < 0 or args.compactions < 1:
        parser.error("Pacing must be nonnegative and compactions must be positive")
    current = observe()
    baseline = json.loads(ops.read_bytes(args.baseline)) if args.baseline else None
    failures = compare(current, baseline, args.expected_image, args.pacing_bytes_per_second, args.compactions)
    result = {"status": "declarations_and_inputs_match" if not failures else "mismatch",
              "image": current["image"], "settings": current["settings"], "failures": failures,
              "limits": ["Undeclared image defaults are unresolved, including after an image upgrade.",
                         "This checks running processes/current declarations; RPC/head readiness and performance are separate checks."]}
    if args.save and not failures:
        args.save.parent.mkdir(parents=True, exist_ok=True)
        with args.save.open("x") as output:
            json.dump(current, output, indent=2)
            output.write("\n")
        result["baseline_saved"] = str(args.save.resolve())
    print(json.dumps(result, indent=2))
    return 1 if failures else 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (OSError, ValueError, RuntimeError, subprocess.SubprocessError) as error:
        reason = type(error).__name__ if isinstance(error, subprocess.SubprocessError) else str(error)
        print("Runtime check stopped: " + reason, file=sys.stderr)
        raise SystemExit(1)
