"""Offline preparation checks; never controls Docker or any service."""

from contextlib import ExitStack, redirect_stdout
from copy import deepcopy
import importlib.util
import io
import json
import os
from pathlib import Path
import stat
import tempfile
import unittest
from unittest.mock import patch


spec = importlib.util.spec_from_file_location(
    "image_prepare", Path(__file__).with_name("prepare-rpc-subsecond-switch.py"))
trial = importlib.util.module_from_spec(spec)
spec.loader.exec_module(trial)


def help_text(clean=600, database=2048):
    return (" --execution.caching.trie-clean-cache int allowance (default %d)\n"
            " --execution.caching.database-cache int allowance (default %d)\n") % (clean, database)


class PreparationTests(unittest.TestCase):
    def setUp(self):
        self.stack = ExitStack()
        self.addCleanup(self.stack.close)
        self.root = Path(self.stack.enter_context(tempfile.TemporaryDirectory()))
        self.build = self.root / "build"
        self.build.mkdir()
        self.stack.enter_context(patch.object(trial, "BUILD", self.build))
        self.checker = trial.load_checker()
        self.ops = self.checker.ops
        self.config = {"persistent": {"pebble": {"compaction-output-bytes-per-second": 64 * 1024**2,
                                               "max-concurrent-compactions": 1}},
                       "rpc": {"stage-diagnostics-threshold": "1s"}}
        self.config_path = self.root / "node.json"
        self.config_path.write_text(json.dumps(self.config))
        self.compose = self.root / "compose.yaml"
        self.compose.write_text("services: {}\n")
        self.baseline = {"observed_utc": "first", "image": trial.BASE,
                         "settings": {k: {"value": None, "source": "image_default_unresolved"}
                                      for k in self.checker.KEYS},
                         "hashes": {"bound_json": self.ops.digest(self.config_path.read_bytes())},
                         "compose_files": [str(self.compose)]}
        for key, value in ((self.ops.KEY, 64 * 1024**2), (self.ops.CONCURRENCY_KEY, 1)):
            self.baseline["settings"][key] = {"value": value, "source": "json"}
        (self.build / "status.json").write_text(json.dumps({
            "state": "assembled_and_verified_not_deployed", "base_image": trial.BASE, "image": trial.TAG}))
        (self.build / "image-id.txt").write_text("IMAGE_ID=" + trial.CANDIDATE + "\n")
        (self.build / "runtime-before.json").write_text(json.dumps(self.baseline))
        self.model = {"services": {"nitro": {"image": "original:tag", "command": ["$literal"],
                                              "environment": {"SECRET": "not printed"}}}}
        self.info = {"Config": {"Labels": {"com.docker.compose.project.working_dir": str(self.root)}}}
        self.stack.enter_context(patch.object(self.checker, "observe", side_effect=lambda: deepcopy(self.baseline)))
        self.stack.enter_context(patch.object(self.checker, "compare", return_value=[]))
        self.tag_id = trial.CANDIDATE
        self.stack.enter_context(patch.object(self.ops, "inspect", side_effect=lambda kind, name:
                                             {"Id": self.tag_id} if kind == "image" else self.info))
        self.stack.enter_context(patch.object(self.ops, "native_argv", return_value=[]))
        self.stack.enter_context(patch.object(self.ops, "local_config", side_effect=lambda *a, **k:
                                             (self.config_path, self.config)))
        self.commands = []
        self.extra_change = False
        self.stack.enter_context(patch.object(self.ops, "run", side_effect=self.render))
        self.stack.enter_context(patch.object(trial, "image_help", return_value=help_text()))
        self.output = self.stack.enter_context(redirect_stdout(io.StringIO()))
        old_mask = os.umask(0o077)
        self.addCleanup(os.umask, old_mask)

    def render(self, command):
        self.commands.append(command)
        self.assertEqual(command[-3:], ["config", "--format", "json"])
        result = deepcopy(self.model)
        if any(s.endswith("image.compose.json") for s in command):
            result["services"]["nitro"]["image"] = trial.TAG
            if self.extra_change:
                result["services"]["nitro"]["command"] = ["changed"]
        return json.dumps(result)

    def prepare(self):
        return trial.prepare(self.root / "plans", self.checker)

    def test_plan_preserves_exact_inputs_and_has_no_service_operations(self):
        original = self.config_path.read_bytes()
        path = self.prepare()
        plan = json.loads(path.read_text())
        self.assertEqual(plan["state"], "prepared_no_service_change")
        self.assertEqual(plan["candidate_image"], trial.CANDIDATE)
        self.assertFalse(plan["activated"])
        self.assertEqual((path.parent / "nodeconfig.before.json").read_bytes(), original)
        self.assertEqual(self.config_path.read_bytes(), original)
        self.assertEqual((path.parent / "compose-before-00").read_bytes(), self.compose.read_bytes())
        self.assertEqual(stat.S_IMODE(path.stat().st_mode), 0o600)
        self.assertEqual(len(self.commands), 2)
        self.assertNotIn("not printed", self.output.getvalue())

    def test_changed_undeclared_default_is_refused(self):
        with patch.object(trial, "image_help", side_effect=[help_text(), help_text(clean=1200)]):
            with self.assertRaisesRegex(ValueError, "undeclared cache default"):
                self.prepare()
        self.assertEqual(self.commands, [])

    def test_tag_change_and_disabled_diagnostics_are_refused(self):
        self.tag_id = "sha256:changed"
        with self.assertRaisesRegex(ValueError, "tag changed"):
            self.prepare()
        self.tag_id = trial.CANDIDATE
        self.config["rpc"]["stage-diagnostics-threshold"] = "0s"
        self.config_path.write_text(json.dumps(self.config))
        self.baseline["hashes"]["bound_json"] = self.ops.digest(self.config_path.read_bytes())
        with self.assertRaisesRegex(ValueError, "positive RPC"):
            self.prepare()

    def test_other_compose_changes_do_not_produce_a_ready_plan(self):
        self.extra_change = True
        with self.assertRaisesRegex(ValueError, "more than the main image"):
            self.prepare()
        self.assertEqual(list((self.root / "plans").glob("*/plan.json")), [])

    def test_cache_default_parser_rejects_missing_or_duplicate_flags(self):
        self.assertEqual(trial.cache_defaults(help_text())[trial.CACHE_KEYS[0]], 600)
        for invalid in ("", help_text() + help_text()):
            with self.assertRaises(ValueError):
                trial.cache_defaults(invalid)


if __name__ == "__main__":
    unittest.main()
