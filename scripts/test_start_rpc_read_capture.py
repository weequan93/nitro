"""Offline capture handoff and strict readiness checks; no node/service access."""

import argparse
from contextlib import ExitStack, redirect_stdout
from copy import deepcopy
from datetime import datetime
import importlib.util
import io
import json
from pathlib import Path
import signal
from types import SimpleNamespace
import tempfile
import unittest
from unittest.mock import Mock, patch


def load(name, filename):
    spec = importlib.util.spec_from_file_location(name, Path(__file__).with_name(filename))
    result = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(result)
    return result


start = load("read_capture_tests", "start-rpc-read-capture.py")
suite = load("read_capture_suite_tests", "capture-sequencer-suite.py")
lifecycle = load("read_capture_identity_tests", "switch-rpc-subsecond-image.py")
rpc = load("read_capture_counter_tests", "capture-rpc-diagnostics.py")


class ReadinessTests(unittest.TestCase):
    def setUp(self):
        self.stack = ExitStack()
        self.addCleanup(self.stack.close)
        self.stack.enter_context(redirect_stdout(io.StringIO()))
        self.info = {"Id": "candidate", "Image": "sha256:tested", "RestartCount": 0,
                     "State": {"Running": True, "OOMKilled": False,
                               "StartedAt": "2026-01-01T00:00:00Z", "Pid": 10}}
        self.target = lifecycle.identity(self.info)
        self.ops = SimpleNamespace(CONTAINER="main-nitro-1", inspect=lambda *a: deepcopy(self.info))
        self.head, self.syncing, self.publisher = 100, False, None
        self.advance, self.omit, self.error = True, None, False
        self.stack.enter_context(patch.object(suite, "fetch", side_effect=self.fetch))
        self.stack.enter_context(patch.object(start.time, "sleep"))
        self.ticks = iter(i / 10 for i in range(1000))
        self.stack.enter_context(patch.object(start.time, "monotonic", side_effect=lambda: next(self.ticks)))

    def fetch(self, url, body=None):
        if body is None:
            return ("l2chaindata_compact_debt 100\n" + "\n".join(
                key + " 0" for key in rpc.TRACKED_KEYS if key != self.omit)).encode()
        method = json.loads(body)["method"]
        if method == "eth_blockNumber":
            result = hex(self.head)
            if self.advance:
                self.head += 1
        elif method == "eth_syncing":
            result = self.syncing
        elif method == "arb_checkPublisherHealth":
            if self.error:
                return b'{"error":{"message":"unavailable"}}'
            result = self.publisher
        else:
            raise AssertionError("Unexpected RPC: " + method)
        return json.dumps({"result": result}).encode()

    def check(self):
        return start.health(suite, rpc, self.ops, lifecycle, self.target, 2)

    def test_requires_actual_head_advance_and_all_health_checks(self):
        health = self.check()
        self.assertEqual(health["heads"], [100, 101])
        self.assertTrue(health["head_advanced"])
        self.assertFalse(health["syncing"])
        self.assertTrue(health["subsecond_counters_available"])

    def test_stationary_head_is_not_ready(self):
        self.advance = False
        with self.assertRaisesRegex(ValueError, "timeout.*advance"):
            self.check()

    def test_syncing_error_and_missing_counters_are_not_ready(self):
        for mode in ("syncing", "publisher", "error", "missing"):
            self.syncing, self.publisher, self.error, self.omit = False, None, False, None
            if mode == "syncing":
                self.syncing = {"currentBlock": "0x1"}
            elif mode == "publisher":
                self.publisher = "unhealthy"
            elif mode == "error":
                self.error = True
            else:
                self.omit = rpc.SUBSECOND_KEYS[0]
            with self.subTest(mode=mode), self.assertRaisesRegex(ValueError, "timeout"):
                self.check()

    def test_changed_node_instance_is_rejected(self):
        self.info["State"]["StartedAt"] = "2026-01-01T01:00:00Z"
        with self.assertRaisesRegex(ValueError, "instance changed"):
            self.check()


class HandoffTests(unittest.TestCase):
    def setUp(self):
        self.stack = ExitStack()
        self.addCleanup(self.stack.close)
        self.root = Path(self.stack.enter_context(tempfile.TemporaryDirectory())).resolve()
        self.stack.enter_context(redirect_stdout(io.StringIO()))

    def row(self, path):
        return {"pid": 20, "ppid": 1, "start_ticks": 30,
                "argv": ["python3", "-B", "/data/scripts/capture-sequencer-suite.py", "--out-dir", str(path)]}

    def test_only_named_old_suite_parent_is_signalled(self):
        old = self.root / "old"
        row = self.row(old)
        with patch.object(suite, "inventory", return_value=[row]), \
             patch.object(suite, "send") as send, patch.object(suite, "process_info", return_value=None):
            parents = start.stop_previous(suite, old)
        send.assert_called_once_with(parents[0], signal.SIGTERM)
        self.assertEqual(parents[0]["pid"], 20)

    def test_unrelated_running_suite_is_preserved(self):
        with patch.object(suite, "inventory", return_value=[self.row(self.root / "other")]), \
             patch.object(suite, "send") as send:
            with self.assertRaisesRegex(ValueError, "outside the named"):
                start.stop_previous(suite, self.root / "old")
        send.assert_not_called()

    def test_slow_old_suite_exit_does_not_force_kill_it(self):
        old = self.root / "old"
        with patch.object(suite, "inventory", return_value=[self.row(old)]), \
             patch.object(suite, "send") as send, \
             patch.object(suite, "process_info", return_value={"start_ticks": 30}):
            with self.assertRaisesRegex(ValueError, "still exiting"):
                start.stop_previous(suite, old, timeout=0)
        self.assertEqual(send.call_count, 1)
        self.assertEqual(send.call_args.args[1], signal.SIGTERM)

    def test_warmup_boundary_is_anchored_to_node_start(self):
        target = {"id": "node", "image": "candidate", "started_at": "2026-01-01T00:00:00Z"}
        phase = start.phase_metadata(target, {"head_advanced": True}, 30)
        self.assertEqual(phase["warmup_until_utc"], "2026-01-01T00:30:00+00:00")
        self.assertGreaterEqual(datetime.fromisoformat(phase["comparison_not_before_utc"]),
                                datetime.fromisoformat(phase["warmup_until_utc"]))

    def test_successful_handoff_execs_complete_suite_and_preserves_old_files(self):
        args = argparse.Namespace(plan=self.root / "plan.json", out_dir=self.root / "out",
                                  expected_image="candidate", previous_suite=self.root / "old",
                                  ready_timeout=30, warmup_minutes=30, hours=24)
        args.previous_suite.mkdir()
        old_file = args.previous_suite / "counters.jsonl"
        old_file.write_text("retained\n")
        info = {"Id": "new", "Image": "candidate", "RestartCount": 0,
                "State": {"Running": True, "Pid": 10, "StartedAt": "2026-01-01T00:00:00Z"}}
        applied = {"state": "started_health_pending", "target": lifecycle.identity(info)}
        ops = SimpleNamespace(CONTAINER="main-nitro-1", inspect=Mock(return_value=info),
                              read_bytes=lambda p: json.dumps(applied).encode() if p.name == "apply.json" else b"# source\n")
        checker = SimpleNamespace(ops=ops, observe=Mock(return_value={"image": "candidate"}), compare=Mock(return_value=[]))
        profile = SimpleNamespace(load_checker=lambda: checker)
        plan = {"candidate_image": "candidate", "baseline": {"image": "base"}}
        fake_lifecycle = SimpleNamespace(identity=lifecycle.identity, verify_inputs=Mock(return_value=(plan, info)),
                                         signal_preflight=Mock())
        fake_suite = SimpleNamespace(SOURCES=("source.py",), validate_device=Mock(), save=suite.save)
        modules = {"capture-sequencer-suite.py": fake_suite, "prepare-rpc-read-switch.py":
                   SimpleNamespace(profile_for_plan=lambda path: profile),
                   "switch-rpc-subsecond-image.py": fake_lifecycle, "capture-rpc-diagnostics.py": object(),
                   "capture-sequencer-longrun.py": SimpleNamespace(budget_reason=lambda *a: None)}
        exec_function = Mock()
        with patch.object(start, "module", side_effect=lambda name, filename: modules[filename]), \
             patch.object(start, "health", return_value={"head_advanced": True}), \
             patch.object(start, "stop_previous", return_value=[]) as stop:
            start.launch(args, exec_function)
        stop.assert_called_once_with(fake_suite, args.previous_suite)
        self.assertEqual(old_file.read_text(), "retained\n")
        phase = json.loads((args.out_dir / "phase.json").read_text())
        self.assertTrue(phase["pid1_is_native_nitro"])
        command = exec_function.call_args.args[1]
        for flag in ("--rpc-counters", "--replace-existing", "--device", "--compactions"):
            self.assertIn(flag, command)
        self.assertEqual(command[command.index("--device") + 1], "vdb")
        self.assertEqual(command[command.index("--compactions") + 1], "1")


if __name__ == "__main__":
    unittest.main()
