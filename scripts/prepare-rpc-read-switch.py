#!/usr/bin/env python3
"""Prepare an image-only plan for the tested RPC shared-read candidate."""

import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import subprocess
import sys


BASE = "sha256:0ea40d56b156757fc121c864ba30775f7bdd8924a45dec5d59b8b6228cd50215"
SOURCE_REV = "a3955fae953a50ef54d8f1420be301ce869dcab6"
GETH_REV = "9557fc573cdde179c42c17c6e1927f00b4b23133"
BUILD_PARENT = Path("/data")
EVIDENCE_FILES = ("status.json", "runtime-before.json", "image-id.txt", "image-tag.txt",
                  "native.sha256", "payload/checks.txt", "version.txt", "help.txt")
IMAGE_CONFIG_KEYS = ("Entrypoint", "Cmd", "Env", "User", "WorkingDir", "ExposedPorts",
                     "Volumes", "StopSignal", "Healthcheck", "Shell")

spec = importlib.util.spec_from_file_location(
    "read_switch_common", Path(__file__).with_name("prepare-rpc-subsecond-switch.py"))
common = importlib.util.module_from_spec(spec)
spec.loader.exec_module(common)


def payload_digest(path):
    if path.is_symlink() or not path.is_file():
        raise ValueError("Tested native payload must be a regular file")
    value = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024**2), b""):
            value.update(chunk)
    return value.hexdigest()


def verified_build(build, checker):
    build = build.resolve(strict=True)
    if build.parent != BUILD_PARENT or not re.fullmatch(r"rpc-read-build\.[A-Za-z0-9]+", build.name):
        raise ValueError("Require a saved /data/rpc-read-build.* directory")
    ops = checker.ops
    data = {name: ops.read_bytes(build / name) for name in EVIDENCE_FILES}
    status = json.loads(data["status.json"])
    tag = "deriw-rpc-read:" + SOURCE_REV[:10] + "-" + build.name.split(".", 1)[1]
    expected = {"state": "assembled_and_verified_not_deployed", "stage": "image_verification",
                "base_image": BASE, "source_revision": SOURCE_REV, "geth_revision": GETH_REV,
                "image": tag, "activated": False}
    if any(status.get(key) != value for key, value in expected.items()):
        raise ValueError("Read-concurrency build is not verified or its pinned identity differs")
    candidate = data["image-id.txt"].decode().strip().removeprefix("IMAGE_ID=")
    if (not re.fullmatch(r"sha256:[0-9a-f]{64}", candidate) or candidate == BASE
            or data["image-id.txt"].decode().strip() != "IMAGE_ID=" + candidate
            or data["image-tag.txt"].decode().strip() != tag):
        raise ValueError("Saved candidate image ID/reference differs")
    native = payload_digest(build / "payload/nitro")
    if data["native.sha256"].decode().split() != [native, str(build / "payload/nitro")]:
        raise ValueError("Native payload differs from the tested build")
    if data["payload/checks.txt"].strip() != b"RPC shared-read and diagnostics checks: PASS":
        raise ValueError("Native test result is missing")
    if "rpc-read-" + SOURCE_REV[:10] not in data["version.txt"].decode():
        raise ValueError("Saved native version differs")
    help_text = data["help.txt"].decode()
    for flag in ("persistent.pebble.compaction-output-bytes-per-second",
                 "persistent.pebble.max-concurrent-compactions", "rpc.stage-diagnostics-threshold"):
        if "--" + flag not in help_text:
            raise ValueError("Saved image help is incomplete")
    base, image = ops.inspect("image", BASE), ops.inspect("image", tag)
    if (base["Id"] != BASE or image["Id"] != candidate
            or any(item.get("Os") != "linux" or item.get("Architecture") != "amd64"
                   for item in (base, image))):
        raise ValueError("Candidate/base image identity or platform changed")
    if any(base["Config"].get(key) != image["Config"].get(key) for key in IMAGE_CONFIG_KEYS):
        raise ValueError("Candidate runtime image configuration differs")
    old, new = base["RootFS"]["Layers"], image["RootFS"]["Layers"]
    if (not old or new[:len(old)] != old or len(new) != len(old) + 1
            or (image["Config"].get("Labels") or {}).get("org.opencontainers.image.revision") != SOURCE_REV):
        raise ValueError("Candidate base layers/source revision differ")
    checksum = ops.run([
        "docker", "run", "--rm", "--network", "none", "--cpus", "1", "--memory", "1g",
        "--entrypoint", "/usr/bin/sha256sum", candidate, "/usr/local/bin/nitro"], timeout=30).split()
    if checksum != [native, "/usr/local/bin/nitro"]:
        raise ValueError("Image native binary differs from the tested payload")
    evidence = {"profile": "rpc-read-concurrency-20261113", "source_revision": SOURCE_REV,
                "geth_revision": GETH_REV, "native_sha256": native,
                "files_sha256": {name: ops.digest(value) for name, value in data.items()}}
    return build, candidate, tag, evidence


class BuildProfile:
    BASE = BASE
    WORK_PREFIX = "main-rpc-read-"

    def __init__(self, build, checker=None):
        self.checker = checker or common.load_checker()
        self.BUILD, self.CANDIDATE, self.TAG, self.evidence = verified_build(build, self.checker)

    def load_checker(self):
        return self.checker

    def image_only_model(self, before, after):
        common.image_only_model(before, after, tag=self.TAG)

    def verify_build(self, plan):
        build, candidate, tag, evidence = verified_build(self.BUILD, self.checker)
        if (plan.get("build_dir") != str(build) or plan.get("candidate_image") != candidate
                or plan.get("candidate_ref") != tag or plan.get("build_evidence") != evidence
                or (candidate, tag, evidence) != (self.CANDIDATE, self.TAG, self.evidence)):
            raise ValueError("Build evidence changed since switch preparation; retain the old plan")


def profile_for_plan(path):
    checker = common.load_checker()
    plan = json.loads(checker.ops.read_bytes(path))
    if not isinstance(plan.get("build_dir"), str):
        raise ValueError("Plan does not name a verified read-concurrency build")
    return BuildProfile(Path(plan["build_dir"]), checker)


def prepare(build, root, checker=None):
    profile = BuildProfile(build, checker)
    return common.prepare(root, profile.checker, profile=profile)


def main():
    os.umask(0o077)
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--build", required=True, type=Path)
    parser.add_argument("--root", type=Path, default=Path("/data/deploy-backups"))
    args = parser.parse_args()
    prepare(args.build, args.root)


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, RuntimeError, subprocess.SubprocessError) as error:
        print("Read-concurrency preparation stopped: " + str(error), file=sys.stderr)
        raise SystemExit(1)
