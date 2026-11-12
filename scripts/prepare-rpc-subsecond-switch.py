#!/usr/bin/env python3
"""Save an image-only main switch plan; preparation performs no service change."""

import argparse
from copy import deepcopy
import importlib.util
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile


BASE = "sha256:e27ef7fd4d2df39cd402c56d68d5a5d1010f2bd3954e994ece396bddf8d676af"
CANDIDATE = "sha256:0ea40d56b156757fc121c864ba30775f7bdd8924a45dec5d59b8b6228cd50215"
TAG = "deriw-rpc-subsecond:b7df6d1a1-EsEK2e"
BUILD = Path("/data/rpc-subsecond-build.EsEK2e")
CACHE_KEYS = ("execution.caching.trie-clean-cache", "execution.caching.database-cache")


def load_checker():
    spec = importlib.util.spec_from_file_location(
        "image_runtime", Path(__file__).with_name("check-main-runtime.py"))
    checker = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(checker)
    return checker


def cache_defaults(text):
    result = {}
    for key in CACHE_KEYS:
        lines = [s for s in text.splitlines() if re.match(r"\s*--" + re.escape(key) + r"\s+", s)]
        found = re.search(r"\(default ([0-9]+)\)\s*$", lines[0]) if len(lines) == 1 else None
        if found is None:
            raise ValueError("Cannot resolve image cache default: " + key)
        result[key] = int(found.group(1))
    return result


def image_help(image):
    completed = subprocess.run([
        "docker", "run", "--rm", "--network", "none", "--cpus", "1", "--memory", "1g",
        "--entrypoint", "/usr/local/bin/nitro", image, "--help"],
        stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, timeout=30, check=True)
    return completed.stdout


def image_only_model(before, after):
    expected = deepcopy(before)
    expected["services"]["nitro"]["image"] = TAG
    if after != expected:
        raise ValueError("Rendered Compose changes more than the main image")


def prepare(root, checker=None):
    checker = checker or load_checker()
    ops = checker.ops
    status = json.loads(ops.read_bytes(BUILD / "status.json"))
    if (status.get("state") != "assembled_and_verified_not_deployed"
            or status.get("base_image") != BASE or status.get("image") != TAG
            or (BUILD / "image-id.txt").read_text().strip() != "IMAGE_ID=" + CANDIDATE):
        raise ValueError("Saved image verification is incomplete or differs")
    before = checker.observe()
    recorded = json.loads(ops.read_bytes(BUILD / "runtime-before.json"))
    failures = checker.compare(before, recorded, compactions=1)
    if before["image"] != BASE or failures:
        raise ValueError("Runtime changed since image build: " + "; ".join(failures))
    if ops.inspect("image", TAG)["Id"] != CANDIDATE:
        raise ValueError("Candidate image tag changed")
    info = ops.inspect("container", ops.CONTAINER)
    argv = ops.native_argv()
    config_path, config = ops.local_config(info, argv, allow_cli_pacing=True)
    config_bytes = ops.read_bytes(config_path)
    if ops.digest(config_bytes) != before["hashes"]["bound_json"]:
        raise ValueError("Bound JSON changed during preparation")
    stage = ops.option(argv, "rpc.stage-diagnostics-threshold")
    if len(stage) > 1:
        raise ValueError("Multiple diagnostics threshold arguments")
    stage = stage[0] if stage else ops.lookup(config, "rpc.stage-diagnostics-threshold")
    if not ((type(stage) is int and stage > 0) or
            (isinstance(stage, str) and re.fullmatch(r"(?:[0-9]+(?:\.[0-9]+)?(?:ns|us|µs|μs|ms|s|m|h))+", stage)
             and any(c in "123456789" for c in stage))):
        raise ValueError("Require an existing positive RPC stage diagnostics threshold")
    defaults = {"before": cache_defaults(image_help(BASE)),
                "candidate": cache_defaults(image_help(CANDIDATE))}
    for key in CACHE_KEYS:
        if before["settings"][key]["value"] is None and defaults["before"][key] != defaults["candidate"][key]:
            raise ValueError("An undeclared cache default would change: " + key)
    labels = info["Config"]["Labels"]
    prefix = ["docker", "compose", "--project-directory", labels["com.docker.compose.project.working_dir"], "-p", "main"]
    files = before["compose_files"]
    for name in files:
        prefix += ["-f", name]
    model = json.loads(ops.run(prefix + ["config", "--format", "json"]))
    root.mkdir(parents=True, exist_ok=True)
    work = Path(tempfile.mkdtemp(prefix="main-rpc-subsecond-", dir=root))
    work.chmod(0o700)
    override = work / "image.compose.json"
    ops.save(override, {"services": {"nitro": {"image": TAG}}})
    candidate_prefix = prefix + ["-f", str(override)]
    rendered = json.loads(ops.run(candidate_prefix + ["config", "--format", "json"]))
    image_only_model(model, rendered)
    (work / "nodeconfig.before.json").write_bytes(config_bytes)
    inputs = {}
    for i, name in enumerate(files):
        data = ops.read_bytes(name)
        backup = work / ("compose-before-%02d" % i)
        backup.write_bytes(data)
        inputs[name] = {"sha256": ops.digest(data), "backup": str(backup)}
    after = checker.observe()
    if ({k: v for k, v in after.items() if k != "observed_utc"} !=
            {k: v for k, v in before.items() if k != "observed_utc"}):
        raise ValueError("Main runtime changed during preparation")
    plan = {"schema": 1, "state": "prepared_no_service_change", "prepared_utc": ops.now(),
            "build_dir": str(BUILD), "candidate_image": CANDIDATE, "candidate_ref": TAG,
            "baseline": before, "cache_defaults": defaults, "diagnostics_threshold": stage,
            "compose": prefix, "candidate_compose": candidate_prefix, "compose_inputs": inputs,
            "compose_model_sha256": ops.digest(json.dumps(model, sort_keys=True).encode()),
            "candidate_compose_sha256": ops.digest(json.dumps(rendered, sort_keys=True).encode()),
            "override": str(override), "override_sha256": ops.digest(ops.read_bytes(override)),
            "nodeconfig_path": str(config_path), "nodeconfig_sha256": ops.digest(config_bytes),
            "shutdown": {"signal": "SIGTERM", "force_kill": False, "required_exit_code": 0},
            "activated": False}
    path = work / "plan.json"
    ops.save(path, plan)
    print(json.dumps({"state": plan["state"], "plan": str(path), "base_image": before["image"],
                      "candidate_image": CANDIDATE, "compactions": 1, "pacing_bytes_per_second": 64 * 1024**2,
                      "cache_defaults": defaults, "diagnostics_threshold": stage,
                      "compose_files": files, "candidate_override": str(override)}, indent=2))
    return path


def main():
    os.umask(0o077)
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=Path("/data/deploy-backups"))
    args = parser.parse_args()
    prepare(args.root)


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, RuntimeError, subprocess.SubprocessError) as error:
        print("Image switch preparation stopped: " + str(error), file=sys.stderr)
        raise SystemExit(1)
