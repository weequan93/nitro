"""Offline counter/owner lifecycle checks. No node calls or service operations."""

import argparse
from contextlib import ExitStack, redirect_stdout
from datetime import datetime, timezone
import fcntl
import importlib.util
import io
import json
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch


SCRIPT = Path(__file__).with_name("capture-rpc-diagnostics.py")
spec = importlib.util.spec_from_file_location("rpc_capture", SCRIPT)
capture = importlib.util.module_from_spec(spec)
spec.loader.exec_module(capture)
spec = importlib.util.spec_from_file_location("rpc_reader", SCRIPT.with_name("report-sequencer-context.py"))
reader = importlib.util.module_from_spec(spec)
spec.loader.exec_module(reader)
spec = importlib.util.spec_from_file_location("rpc_watcher", SCRIPT.with_name("watch-slow-sequencer-blocks.py"))
watcher = importlib.util.module_from_spec(spec)
spec.loader.exec_module(watcher)


def counters(offset=0):
    return {key: i + offset for i, key in enumerate(capture.ALL_KEYS)}


def sample(offset=0):
    return {"identity": {"pid": 100, "start_ticks": 42}, "counters": counters(offset), "errors": []}


class CounterTests(unittest.TestCase):
    def test_parse_keeps_exact_counters_and_missing_is_not_zero(self):
        text = "\n".join("%s %s" % item for item in counters(1).items())
        self.assertEqual(capture.parse(text), counters(1))
        key = capture.KEYS[0]
        self.assertNotIn(key, capture.parse(key + " NaN"))
        self.assertNotIn(key, capture.parse(key + " -1"))
        self.assertEqual(capture.parse("unrelated 10"), {})

    def test_deltas_reject_restart_error_gap_missing_and_reset(self):
        old, new = sample(10), sample(15)
        self.assertEqual(capture.deltas(new, None, 15)[1], "first_sample")
        self.assertEqual(capture.deltas(new, old, 15)[0], {k: 5 for k in capture.ALL_KEYS})
        self.assertEqual(capture.deltas(new, old, 60)[1], "sample_gap")
        for kind in ("identity", "error", "missing", "reset"):
            with self.subTest(kind=kind):
                new = sample(15)
                if kind == "identity":
                    new["identity"]["start_ticks"] = 99
                elif kind == "error":
                    new["errors"] = ["metrics_timeout"]
                elif kind == "missing":
                    new["counters"].pop(capture.KEYS[0])
                else:
                    new["counters"][capture.DROP] = 0
                self.assertEqual(capture.deltas(new, old, 15)[0], {})

    def test_report_sums_differences_not_uptime_counts(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            (root / "run.json").write_text(json.dumps({"suite_dir": str(root)}))
            a, b = sample(1000), sample(1007)
            a.update(time_utc="2026-11-12T06:00:00Z", interval_s=None,
                     deltas={}, delta_unavailable="first_sample")
            delta, reason = capture.deltas(b, a, 15)
            b.update(time_utc="2026-11-12T06:00:15Z", interval_s=15,
                     deltas=delta, delta_unavailable=reason)
            (root / "counters.jsonl").write_text(json.dumps(a) + "\n" + json.dumps(b) + "\n{partial")
            with patch.object(capture, "module", return_value=reader):
                result = capture.report(root)
            self.assertEqual(result["valid_intervals"], 1)
            self.assertEqual(result["valid_interval_seconds"], 15)
            self.assertEqual(result["method_deltas"]["eth_sendRawTransaction"]["success"]["completed"], 7)
            self.assertEqual(result["details_dropped_delta"], 7)
            self.assertEqual(result["input_status"]["malformed_or_partial"], 1)
            (root / "counters.jsonl").write_text(json.dumps(a) + "\n")
            with patch.object(capture, "module", return_value=reader):
                self.assertIsNone(capture.report(root)["method_deltas"]["eth_sendRawTransaction"]["success"]["completed"])

    def test_retained_logs_are_split_at_capture_start(self):
        lines = [
            "2026-11-12T05:10:00.000000001Z WARN RPC stage timings method=eth_sendRawTransaction outcome=success trace=1 total_ms=6000 callback_ms=5999\n",
            "2026-11-12T05:30:00.000000001Z WARN RPC stage timings method=eth_sendRawTransaction outcome=failure trace=2 total_ms=1500 callback_ms=1499\n",
            "2026-11-12T05:30:00.000000002Z WARN Served eth_sendRawTransaction duration=1.5s err=\"nonce too high\"\n",
            "2026-11-12T05:31:00.000000001Z WARN RPC stage timings method=eth_sendRawTransaction outcome=success trace=3 total_ms=800 callback_ms=799\n",
        ]
        result = capture.summarize_logs(lines, datetime.fromisoformat("2026-11-12T05:21:30+00:00"), watcher)
        self.assertEqual(result["before_capture"]["stage_ge1s_records"], 1)
        self.assertEqual(result["before_capture"]["stage_ge5s_records"], 1)
        self.assertEqual(result["during_capture"]["stage_failure"], 1)
        self.assertEqual(result["during_capture"]["stage_ge5s_records"], 0)
        self.assertEqual(result["during_capture"]["recent_served_errors"][0]["error"], "nonce too high")
        self.assertEqual(result["during_capture"]["slowest_stage_records"][0]["time_sgt"], "2026-11-12T13:30:00+08:00")

    def test_unparseable_stage_lines_are_reported(self):
        line = "2026-11-12T05:30:00Z WARN RPC stage timings method=eth_sendRawTransaction unexpected_format\n"
        result = capture.summarize_logs([line], datetime.fromisoformat("2026-11-12T05:21:30+00:00"), watcher)
        self.assertEqual(result["during_capture"]["unparsed_sendraw_stage_lines"], 1)
        self.assertEqual(result["during_capture"]["stage_ge1s_records"], 0)


class LogInspectionTests(unittest.TestCase):
    class Client:
        def __init__(self):
            self.stdout = iter(["2026-11-12T05:30:00Z WARN Served eth_sendRawTransaction duration=2s err=EOF\n"])
            self.returncode = None
            self.kills = 0
        def __enter__(self):
            return self
        def __exit__(self, *_):
            self.wait()
        def poll(self):
            return self.returncode
        def wait(self, timeout=None):
            if self.returncode is None:
                self.returncode = 0
            return self.returncode
        def kill(self):
            self.kills += 1
            self.returncode = -9

    def test_finite_log_query_is_bound_to_current_container_and_timeout_is_visible(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "run.json").write_text(json.dumps({"started_utc": "2026-11-12T05:21:30Z"}))
            helper = SimpleNamespace(identity=lambda *_: {
                "id": "original-container", "started": "2026-11-12T04:54:30Z"})
            for timeout in (False, True):
                with self.subTest(timeout=timeout):
                    client = self.Client()
                    def timer(seconds, expire):
                        self.assertEqual(seconds, 45)
                        return SimpleNamespace(start=expire if timeout else lambda: None, cancel=lambda: None)
                    with patch.object(capture, "module", side_effect=lambda name, path:
                                      helper if name == "rpc_log_identity" else watcher), \
                         patch.object(capture.subprocess, "Popen", return_value=client) as launch, \
                         patch.object(capture.threading, "Timer", side_effect=timer):
                        result = capture.inspect_logs(SimpleNamespace(suite_dir=root))
                    args = launch.call_args.args[0]
                    self.assertEqual(args[-1], "original-container")
                    self.assertIn("--until", args)
                    self.assertNotIn("--follow", args)
                    self.assertEqual(result["query_timed_out"], timeout)
                    self.assertEqual(result["docker_logs_returncode"], -9 if timeout else 0)
                    self.assertEqual(client.kills, int(timeout))
                    self.assertEqual(result["during_capture"]["recent_served_errors"][0]["error"], "EOF")


class LifecycleTests(unittest.TestCase):
    def setUp(self):
        self.stack = ExitStack()
        self.addCleanup(self.stack.close)
        self.root = Path(self.stack.enter_context(tempfile.TemporaryDirectory())).resolve()
        (self.root / "tools").mkdir()
        (self.root / "tools/capture-main-cache-io.py").write_text("# pinned helper\n")
        self.started = datetime.now(timezone.utc).timestamp()
        self.metadata = {"parent_pid": 123, "started_utc": datetime.fromtimestamp(
            self.started, timezone.utc).isoformat(), "hours": 1}
        self.write_metadata()
        self.args = argparse.Namespace(suite_dir=self.root, out_dir=None, interval=0.01,
            metrics_url="unused", lock_file=self.root / "capture.lock", min_free_gib=0)
        self.samples = 0
        self.limit = 3
        self.reused = False
        self.parent = {"pid": 123, "start_ticks": 42,
            "argv": ["python3", "-B", "/data/scripts/capture-sequencer-suite.py",
                     "--out-dir", str(self.root)]}
        def process_info(pid):
            if self.samples < self.limit:
                return self.parent
            return dict(self.parent, start_ticks=99) if self.reused else None
        self.ops = SimpleNamespace(process_info=process_info,
            argument=lambda argv, key: argv[argv.index(key) + 1])
        def fetch(*args, **kwargs):
            self.samples += 1
            return "\n".join("%s %s" % item for item in counters(self.samples).items()).encode()
        self.helper = SimpleNamespace(identity=lambda *_: {"pid": 100, "start_ticks": 5},
            base=SimpleNamespace(fetch=fetch))
        def load(name, path):
            return self.ops if name == "rpc_suite_owner" else self.helper if name == "rpc_identity_helper" else reader
        self.stack.enter_context(patch.object(capture, "module", side_effect=load))
        self.stack.enter_context(patch.object(capture.signal, "signal", return_value=0))
        self.kill = self.stack.enter_context(patch.object(capture.os, "kill"))
        self.stack.enter_context(redirect_stdout(io.StringIO()))
        previous = capture.os.umask(0o077)
        self.addCleanup(capture.os.umask, previous)

    def write_metadata(self):
        (self.root / "run.json").write_text(json.dumps(self.metadata))

    def status(self):
        return json.loads((self.root / "rpc-counters/status.json").read_text())

    def test_parent_exit_stops_sampling_without_signalling_any_process(self):
        original_metadata = (self.root / "run.json").read_bytes()
        original_helper = (self.root / "tools/capture-main-cache-io.py").read_bytes()
        self.assertEqual(capture.run(self.args), 0)
        self.assertEqual(self.status()["reason"], "suite_parent_exited_or_reused")
        self.assertEqual(self.status()["samples"], 3)
        self.kill.assert_not_called()
        result = capture.report(self.root / "rpc-counters")
        self.assertEqual(result["method_deltas"]["eth_sendRawTransaction"]["success"]["completed"], 2)
        self.assertEqual(result["valid_intervals"], 2)
        self.assertEqual((self.root / "run.json").read_bytes(), original_metadata)
        self.assertEqual((self.root / "tools/capture-main-cache-io.py").read_bytes(), original_helper)

    def test_parent_pid_reuse_stops_capture(self):
        self.reused = True
        self.assertEqual(capture.run(self.args), 0)
        self.assertEqual(self.status()["reason"], "suite_parent_exited_or_reused")
        self.kill.assert_not_called()

    def test_deadline_is_the_existing_suite_deadline(self):
        self.limit = 100000
        self.metadata["started_utc"] = datetime.fromtimestamp(
            self.started - 3599.5, timezone.utc).isoformat()
        self.write_metadata()
        self.assertEqual(capture.run(self.args), 0)
        self.assertEqual(self.status()["kind"], "complete")
        saved = json.loads((self.root / "rpc-counters/run.json").read_text())
        self.assertAlmostEqual(datetime.fromisoformat(saved["planned_end_utc"]).timestamp(), self.started + 0.5, places=4)
        self.kill.assert_not_called()

    def test_missing_diagnostics_are_errors_not_zero_counts(self):
        self.helper.base.fetch = lambda *a, **k: self.missing_fetch()
        self.assertEqual(capture.run(self.args), 0)
        self.assertEqual(self.status()["error_samples"], 3)
        result = capture.report(self.root / "rpc-counters")
        self.assertEqual(result["valid_intervals"], 0)
        self.assertIsNone(result["method_deltas"]["eth_sendRawTransaction"]["success"]["completed"])

    def missing_fetch(self):
        self.samples += 1
        return b"other_metric 10\n"

    def test_duplicate_lock_refuses_another_capture(self):
        with self.args.lock_file.open("a") as lock:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            with self.assertRaisesRegex(ValueError, "already running"):
                capture.run(self.args)
        self.assertFalse((self.root / "rpc-counters/run.json").exists())

    def test_wrong_parent_and_expired_suite_are_refused(self):
        self.parent["argv"][2] = "/data/scripts/unrelated.py"
        with self.assertRaisesRegex(ValueError, "no longer belongs"):
            capture.run(self.args)
        self.parent["argv"][2] = "/data/scripts/capture-sequencer-suite.py"
        self.metadata["started_utc"] = "2020-01-01T00:00:00Z"
        self.write_metadata()
        with self.assertRaisesRegex(ValueError, "deadline has already passed"):
            capture.run(self.args)
        self.kill.assert_not_called()


if __name__ == "__main__":
    unittest.main()
