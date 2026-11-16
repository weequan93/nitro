"""Offline build binding and normal-exit lifecycle checks; no Docker access."""

from copy import deepcopy
import importlib.util
import json
from pathlib import Path
import unittest
from unittest.mock import patch


def load(name, filename):
    spec = importlib.util.spec_from_file_location(name, Path(__file__).with_name(filename))
    result = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(result)
    return result


read = load("read_switch_cases", "prepare-rpc-read-switch.py")
legacy = load("read_switch_lifecycle_cases", "test_switch_rpc_subsecond_image.py")


class ReadSwitchTests(unittest.TestCase):
    def setUp(self):
        self.life = legacy.SwitchTests()
        self.life.setUp()
        self.addCleanup(self.life.doCleanups)
        f = self.life.fixture
        self.f, self.stack, self.ops = f, f.stack, f.ops
        self.stack.enter_context(patch.object(read, "common", legacy.fixtures.trial))
        self.stack.enter_context(patch.object(read, "BUILD_PARENT", f.root.resolve()))
        self.build = f.root.resolve() / "rpc-read-build.Test01"
        (self.build / "payload").mkdir(parents=True)
        (self.build / "payload/nitro").write_bytes(b"native binary fixture")
        self.native = read.payload_digest(self.build / "payload/nitro")
        self.candidate = "sha256:" + "2" * 64
        self.tag = "deriw-rpc-read:" + read.SOURCE_REV[:10] + "-Test01"
        self.status = {"state": "assembled_and_verified_not_deployed", "stage": "image_verification",
                       "base_image": read.BASE, "image": self.tag, "source_revision": read.SOURCE_REV,
                       "geth_revision": read.GETH_REV, "activated": False}
        (self.build / "status.json").write_text(json.dumps(self.status))
        (self.build / "image-id.txt").write_text("IMAGE_ID=" + self.candidate + "\n")
        (self.build / "image-tag.txt").write_text(self.tag + "\n")
        (self.build / "native.sha256").write_text(self.native + "  " + str(self.build / "payload/nitro") + "\n")
        (self.build / "payload/checks.txt").write_text("RPC shared-read and diagnostics checks: PASS\n")
        (self.build / "version.txt").write_text("Version: rpc-read-" + read.SOURCE_REV[:10] + "\n")
        (self.build / "help.txt").write_text(
            "--persistent.pebble.compaction-output-bytes-per-second\n"
            "--persistent.pebble.max-concurrent-compactions\n--rpc.stage-diagnostics-threshold\n")
        for name, value in (("BASE", read.BASE), ("CANDIDATE", self.candidate), ("TAG", self.tag)):
            self.stack.enter_context(patch.object(legacy.fixtures.trial, name, value))
        f.tag_id = self.candidate
        f.info["Image"] = read.BASE
        f.baseline["image"] = read.BASE
        (self.build / "runtime-before.json").write_text(json.dumps(f.baseline))
        self.life.current = deepcopy(f.info)
        self.base_image = {"Id": read.BASE, "Os": "linux", "Architecture": "amd64",
                           "Config": {"Entrypoint": ["/usr/local/bin/split-val-entry.sh"], "Env": ["EXAMPLE=1"]},
                           "RootFS": {"Layers": ["base-layer"]}}
        self.image = deepcopy(self.base_image)
        self.image["Id"] = self.candidate
        self.image["Config"]["Labels"] = {"org.opencontainers.image.revision": read.SOURCE_REV}
        self.image["RootFS"]["Layers"].append("native-layer")
        self.image_native = self.native
        self.stack.enter_context(patch.object(self.ops, "inspect", side_effect=self.inspect))
        self.stack.enter_context(patch.object(self.ops, "run", side_effect=self.run_command))
        self.life.prep = read.BuildProfile(self.build, f.checker)
        self.life.path = read.common.prepare(f.root / "read-plans", f.checker, profile=self.life.prep)
        self.life.plan = json.loads(self.life.path.read_text())
        self.plan = self.life.plan

    def inspect(self, kind, name):
        if kind == "image":
            return deepcopy(self.image if name == self.tag else self.base_image)
        return self.life.inspect(kind, name)

    def run_command(self, command, timeout=15):
        if command[:2] == ["docker", "run"]:
            self.assertEqual(command, ["docker", "run", "--rm", "--network", "none", "--cpus", "1",
                                      "--memory", "1g", "--entrypoint", "/usr/bin/sha256sum",
                                      self.candidate, "/usr/local/bin/nitro"])
            return self.image_native + "  /usr/local/bin/nitro\n"
        return self.life.read(command, timeout)

    def test_plan_binds_build_and_only_prepares_the_image_change(self):
        self.assertEqual(self.plan["candidate_image"], self.candidate)
        self.assertEqual(self.plan["baseline"]["image"], read.BASE)
        self.assertEqual(self.plan["build_evidence"]["geth_revision"], read.GETH_REV)
        self.assertEqual(self.plan["build_evidence"]["native_sha256"], self.native)
        self.assertFalse(self.plan["activated"])
        self.assertEqual(self.life.signals + self.life.creates, [])
        self.assertEqual(self.plan["shutdown"], {"signal": "SIGTERM", "force_kill": False, "required_exit_code": 0})

    def test_unverified_build_or_wrong_pins_are_rejected(self):
        for key, value in (("state", "running"), ("geth_revision", "0" * 40),
                           ("source_revision", "0" * 40), ("base_image", "sha256:other"),
                           ("stage", "native_build_and_tests"), ("activated", True)):
            with self.subTest(key=key):
                status = dict(self.status, **{key: value})
                (self.build / "status.json").write_text(json.dumps(status))
                with self.assertRaisesRegex(ValueError, "not verified"):
                    read.BuildProfile(self.build, self.f.checker)
        self.assertEqual(self.life.signals, [])

    def test_changed_payload_and_image_binary_are_rejected_before_stop(self):
        self.image_native = "0" * 64
        with self.assertRaisesRegex(ValueError, "Image native binary"):
            self.life.switch()
        self.image_native = self.native
        (self.build / "payload/nitro").write_bytes(b"changed native")
        with self.assertRaisesRegex(ValueError, "Native payload"):
            self.life.switch()
        self.assertEqual(self.life.signals, [])

    def test_image_tag_config_and_layers_cannot_drift(self):
        original = deepcopy(self.image)
        for kind in ("tag", "config", "layer", "revision"):
            self.image = deepcopy(original)
            if kind == "tag":
                self.image["Id"] = "sha256:" + "3" * 64
            elif kind == "config":
                self.image["Config"]["Env"] = ["EXAMPLE=2"]
            elif kind == "layer":
                self.image["RootFS"]["Layers"][0] = "other-base-layer"
            else:
                self.image["Config"]["Labels"]["org.opencontainers.image.revision"] = "0" * 40
            with self.subTest(kind=kind), self.assertRaises(ValueError):
                self.life.switch()
        self.assertEqual(self.life.signals, [])

    def test_build_metadata_changes_invalidate_an_existing_plan(self):
        (self.build / "version.txt").write_text("Version: rpc-read-" + read.SOURCE_REV[:10] + "\nchanged\n")
        with self.assertRaisesRegex(ValueError, "evidence changed"):
            self.life.switch()
        self.assertEqual(self.life.signals, [])

    def test_fresh_profile_loader_cannot_refresh_an_existing_plan_silently(self):
        with patch.object(read.common, "load_checker", return_value=self.f.checker):
            self.life.prep = read.profile_for_plan(self.life.path)
            self.assertEqual(self.life.prep.CANDIDATE, self.candidate)
            with (self.build / "version.txt").open("a") as stream:
                stream.write("extra verification text\n")
            self.life.prep = read.profile_for_plan(self.life.path)
        with self.assertRaisesRegex(ValueError, "evidence changed"):
            self.life.switch()
        self.assertEqual(self.life.signals, [])

    def test_apply_rollback_and_repeated_apply_use_existing_graceful_lifecycle(self):
        config = self.f.config_path.read_bytes()
        self.life.switch()
        self.assertEqual(self.life.current["Image"], self.candidate)
        self.life.switch()
        self.assertEqual(len(self.life.signals), 1)
        self.life.switch(rollback=True)
        self.assertEqual(self.life.current["Image"], read.BASE)
        self.assertEqual(len(self.life.signals), 2)
        self.assertEqual(self.f.config_path.read_bytes(), config)

    def test_evidence_is_rechecked_after_normal_exit_before_recreation(self):
        original_stop = self.life.stop

        def stop_and_change_build(command, **kwargs):
            result = original_stop(command, **kwargs)
            (self.build / "payload/checks.txt").write_text("changed\n")
            return result

        with patch.object(legacy.switcher.subprocess, "Popen", side_effect=stop_and_change_build):
            with self.assertRaisesRegex(ValueError, "test result"):
                self.life.switch()
        self.assertFalse(self.life.current["State"]["Running"])
        self.assertEqual(self.life.creates, [])
        self.assertEqual(json.loads((self.life.path.parent / "apply.json").read_text())["state"],
                         "recreation_requested")

    def test_failed_recreation_retains_verified_rollback(self):
        self.life.fail_up = True
        with self.assertRaises(RuntimeError):
            self.life.switch()
        self.assertIsNone(self.life.current)
        self.life.fail_up = False
        self.life.switch(rollback=True)
        self.assertEqual(self.life.current["Image"], read.BASE)
        self.assertEqual(len(self.life.signals), 1)


if __name__ == "__main__":
    unittest.main()
