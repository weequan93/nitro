"""Offline deployment checks; no live Docker or RPC access."""

from contextlib import ExitStack, redirect_stdout
from copy import deepcopy
from datetime import datetime, timezone
import importlib.util
import io
import json
import os
from pathlib import Path
import stat
import tempfile
import time
import unittest
from unittest.mock import patch


spec = importlib.util.spec_from_file_location("runtime_check", Path(__file__).with_name("check-main-runtime.py"))
check = importlib.util.module_from_spec(spec)
spec.loader.exec_module(check)


class RuntimeTests(unittest.TestCase):
    def setUp(self):
        self.stack = ExitStack()
        self.addCleanup(self.stack.close)
        self.root = Path(self.stack.enter_context(tempfile.TemporaryDirectory())).resolve()
        self.config_path = self.root / "node.json"
        self.config = {"persistent": {"pebble": {"max-concurrent-compactions": 2,
                                               "compaction-output-bytes-per-second": 67108864}},
                       "secret": "do-not-print-this-value"}
        self.config_path.write_text(json.dumps(self.config))
        self.argv = ["/usr/local/bin/nitro", "--conf.file=/config/node.json"]
        self.info = {
            "Id": "container-one", "Image": "sha256:old", "RestartCount": 0,
            "State": {"Running": True, "OOMKilled": False,
                      "StartedAt": datetime.fromtimestamp(time.time() + 60, timezone.utc).isoformat()},
            "Config": {"Image": "repo:old", "Cmd": ["--conf.file=/config/node.json"],
                       "Env": ["SECRET=do-not-print-this-value"], "Labels": {
                           "com.docker.compose.project": "main", "com.docker.compose.service": "nitro",
                           "com.docker.compose.config-hash": "a" * 64,
                           "com.docker.compose.project.config_files": str(self.root / "compose.yaml"),
                           "com.docker.compose.project.working_dir": str(self.root)}},
            "HostConfig": {"NetworkMode": "main_default"},
            "Mounts": [{"Type": "bind", "Source": str(self.config_path), "Destination": "/config/node.json"}],
        }
        self.model = {"services": {"nitro": {"image": "repo:old", "command": ["--conf.file=/config/node.json"]}}}
        self.rendered_hash = "a" * 64
        self.calls = []
        self.stack.enter_context(patch.object(check.ops, "inspect", side_effect=self.inspect))
        self.stack.enter_context(patch.object(check.ops, "native_argv", side_effect=lambda: list(self.argv)))
        self.stack.enter_context(patch.object(check.ops, "run", side_effect=self.read_command))

    def inspect(self, kind, name):
        self.calls.append((kind, name))
        return deepcopy(self.info) if kind == "container" else {"Id": self.info["Image"]}

    def read_command(self, command, timeout=15):
        self.calls.append(command)
        if command[-3:] == ["config", "--hash", "nitro"]:
            return "nitro " + self.rendered_hash + "\n"
        if command[-3:] == ["config", "--format", "json"]:
            return json.dumps(self.model)
        self.fail("Unexpected operation: " + repr(command))

    def test_current_settings_and_unresolved_defaults_are_explicit(self):
        before = self.config_path.read_bytes()
        observed = check.observe()
        self.assertEqual(check.compare(observed), [])
        self.assertEqual(observed["settings"][check.ops.KEY], {"value": 67108864, "source": "json"})
        self.assertEqual(observed["settings"][check.KEYS[2]], {"value": None, "source": "image_default_unresolved"})
        self.assertNotIn("do-not-print-this-value", json.dumps(observed))
        self.assertEqual(self.config_path.read_bytes(), before)

    def test_lost_rate_and_cli_override_are_detected(self):
        baseline = check.observe()
        self.config["persistent"]["pebble"].pop("compaction-output-bytes-per-second")
        self.config_path.write_text(json.dumps(self.config))
        current = check.observe()
        failures = check.compare(current, baseline)
        self.assertTrue(any(check.ops.KEY in f for f in failures))
        self.argv.append("--" + check.ops.KEY + "=0")
        self.assertEqual(check.observe()["settings"][check.ops.KEY], {"value": 0, "source": "cli"})
        self.assertTrue(check.compare(check.observe()))

    def test_image_change_requires_verified_expected_id_and_preserves_other_inputs(self):
        baseline = check.observe()
        self.info["Image"] = "sha256:new"
        self.info["Config"]["Image"] = self.model["services"]["nitro"]["image"] = "repo:new"
        current = check.observe()
        self.assertTrue(check.compare(current, baseline))
        self.assertEqual(check.compare(current, baseline, expected_image="sha256:new"), [])
        self.assertTrue(check.compare(current, baseline, expected_image="sha256:wrong"))
        self.info["HostConfig"]["PortBindings"] = {"8449/tcp": [{"HostPort": "9999"}]}
        self.assertTrue(any("container_layout" in f for f in check.compare(check.observe(), baseline, "sha256:new")))

    def test_dropped_override_and_unrelated_json_edit_are_detected(self):
        baseline = check.observe()
        self.model["services"]["nitro"]["command"] = []
        self.assertTrue(any("compose_model" in f for f in check.compare(check.observe(), baseline)))
        self.model["services"]["nitro"]["command"] = self.info["Config"]["Cmd"]
        self.config["another-setting"] = 123
        self.config_path.write_text(json.dumps(self.config))
        self.assertTrue(any("bound_json" in f for f in check.compare(check.observe(), baseline)))

    def test_incomplete_baseline_is_refused(self):
        baseline = check.observe()
        current = deepcopy(baseline)
        baseline["hashes"].clear()
        with self.assertRaisesRegex(ValueError, "incomplete"):
            check.compare(current, baseline)

    def test_newer_json_and_compose_drift_refuse_a_baseline(self):
        self.rendered_hash = "b" * 64
        with self.assertRaisesRegex(ValueError, "Compose hash differs"):
            check.observe()
        self.rendered_hash = "a" * 64
        os.utime(self.config_path, (time.time() + 120, time.time() + 120))
        with self.assertRaisesRegex(ValueError, "newer than container start"):
            check.observe()

    def test_duplicate_cli_and_extra_loaders_are_refused(self):
        with self.assertRaisesRegex(ValueError, "Duplicate CLI"):
            check.selected({}, ["--" + check.ops.KEY + "=1", "--" + check.ops.KEY + "=2"], check.ops.KEY)
        self.argv.append("--conf.env-prefix=NODE")
        with self.assertRaisesRegex(ValueError, "configuration loader"):
            check.observe()

    def test_save_is_private_and_refuses_overwriting(self):
        path = self.root / "baseline.json"
        old_mask = os.umask(0o077)
        try:
            with patch("sys.argv", ["check", "--save", str(path)]), redirect_stdout(io.StringIO()):
                self.assertEqual(check.main(), 0)
                with self.assertRaises(FileExistsError):
                    check.main()
        finally:
            os.umask(old_mask)
        self.assertEqual(stat.S_IMODE(path.stat().st_mode), 0o600)
        self.assertNotIn("do-not-print-this-value", path.read_text())


if __name__ == "__main__":
    unittest.main()
