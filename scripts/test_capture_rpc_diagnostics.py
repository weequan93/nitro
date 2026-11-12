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


def latency_deltas(durations_ms):
    values = {key: 0 for key in capture.TRACKED_KEYS}
    prefix = "rpc_diagnostics_eth_sendRawTransaction_success_"
    values[prefix + "completed"] = len(durations_ms)
    for suffix, bound in zip(capture.LATENCY_SUFFIXES[1:], capture.LATENCY_BOUNDS_MS[1:-1]):
        values[prefix + suffix] = sum(ms >= bound for ms in durations_ms)
    return values


def latency_row(values, when="2026-11-12T06:00:15Z", seconds=15):
    return {"time_utc": when, "interval_s": seconds, "errors": [], "delta_unavailable": None,
            "deltas": {key: values[key] for key in capture.ALL_KEYS},
            "subsecond_deltas": {key: values[key] for key in capture.SUBSECOND_KEYS},
            "subsecond_delta_unavailable": None}


class CounterTests(unittest.TestCase):
    def test_parse_keeps_exact_counters_and_missing_is_not_zero(self):
        text = "\n".join("%s %s" % item for item in counters(1).items())
        self.assertEqual(capture.parse(text), counters(1))
        key = capture.KEYS[0]
        self.assertNotIn(key, capture.parse(key + " NaN"))
        self.assertNotIn(key, capture.parse(key + " -1"))
        self.assertEqual(capture.parse("unrelated 10"), {})

    def test_optional_subsecond_counters_are_parsed_without_becoming_required(self):
        values = latency_deltas([5, 25, 500, 1000])
        self.assertEqual(capture.parse("\n".join("%s %s" % item for item in values.items())), values)
        old, new = sample(10), sample(15)
        self.assertIsNone(capture.deltas(new, old, 15)[1])
        self.assertEqual(capture.deltas(new, old, 15, keys=capture.SUBSECOND_KEYS)[1], "missing_counters")
        old["counters"].update({key: 20 for key in capture.SUBSECOND_KEYS})
        new["counters"].update({key: 25 for key in capture.SUBSECOND_KEYS})
        self.assertEqual(capture.deltas(new, old, 15, keys=capture.SUBSECOND_KEYS)[0],
                         {key: 5 for key in capture.SUBSECOND_KEYS})
        new["counters"][capture.SUBSECOND_KEYS[0]] = 19
        self.assertEqual(capture.deltas(new, old, 15, keys=capture.SUBSECOND_KEYS)[1], "counter_reset")
        self.assertIsNone(capture.deltas(new, old, 15)[1])

    def test_latency_buckets_include_lower_boundary_and_exclude_upper(self):
        durations = [0, 9.999, 10, 24.999, 25, 49.999, 50, 99.999, 100, 249.999,
                     250, 499.999, 500, 999.999, 1000, 2999.999, 3000, 4999.999, 5000, 12000]
        row = latency_row(latency_deltas(durations))
        result = capture.latency_report([row])["eth_sendRawTransaction"]
        self.assertEqual(result["success"]["completed"], 20)
        self.assertEqual([b["count"] for b in result["success"]["ranges_ms"]], [2] * 10)
        self.assertEqual([b["percent"] for b in result["success"]["ranges_ms"]], [10] * 10)
        self.assertTrue(result["failure"]["available"])
        self.assertEqual(result["failure"]["completed"], 0)
        self.assertTrue(all(b["percent"] is None for b in result["failure"]["ranges_ms"]))

    def test_old_capture_and_missing_optional_metrics_are_unavailable_not_zero(self):
        for reason in ("missing_counters", "counter_reset", None):
            row = latency_row(latency_deltas([5]))
            row["subsecond_delta_unavailable"] = reason
            if reason is None:
                row.pop("subsecond_deltas")
            result = capture.latency_report([row])["eth_sendRawTransaction"]["success"]
            self.assertFalse(result["available"])
            self.assertIsNone(result["completed"])
            self.assertTrue(all(b["count"] is None for b in result["ranges_ms"]))
            self.assertEqual(result["unavailable_intervals"], {reason or "not_recorded": 1})

    def test_inconsistent_scrape_is_excluded_only_from_affected_bucket_group(self):
        row = latency_row(latency_deltas([5]))
        row["subsecond_deltas"]["rpc_diagnostics_eth_sendRawTransaction_success_ge25ms"] = 1
        result = capture.latency_report([row])
        bad = result["eth_sendRawTransaction"]["success"]
        self.assertFalse(bad["available"])
        self.assertEqual(bad["unavailable_intervals"], {"missing_or_inconsistent_threshold_deltas": 1})
        self.assertTrue(result["eth_sendRawTransaction"]["failure"]["available"])
        self.assertTrue(result["eth_getLogs"]["success"]["available"])

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
            self.assertFalse(result["latency_buckets"]["eth_sendRawTransaction"]["success"]["available"])
            (root / "counters.jsonl").write_text(json.dumps(a) + "\n")
            with patch.object(capture, "module", return_value=reader):
                self.assertIsNone(capture.report(root)["method_deltas"]["eth_sendRawTransaction"]["success"]["completed"])

    def test_recent_report_uses_complete_intervals_and_separate_outcome_denominators(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            (root / "run.json").write_text(json.dumps({"suite_dir": str(root)}))
            rows = []
            values = latency_deltas([5, 25, 500, 1000])
            values["rpc_diagnostics_eth_sendRawTransaction_failure_completed"] = 1
            start = datetime.fromisoformat("2026-11-12T06:00:00+00:00").timestamp()
            for i in range(30):
                when = datetime.fromtimestamp(start + 15 * i, timezone.utc).isoformat()
                rows.append(latency_row(values, when))
            (root / "counters.jsonl").write_text("".join(json.dumps(row) + "\n" for row in rows))
            with patch.object(capture, "module", return_value=reader):
                result = capture.report(root, last_minutes=5)
            self.assertEqual(result["valid_intervals"], 20)
            self.assertEqual(result["valid_interval_seconds"], 300)
            self.assertEqual(result["last_sample_sgt"], "2026-11-12T14:07:15+08:00")
            groups = result["latency_buckets"]["eth_sendRawTransaction"]
            self.assertEqual(groups["success"]["completed"], 80)
            self.assertEqual(groups["failure"]["completed"], 20)
            self.assertEqual(groups["failure"]["ranges_ms"][0]["percent"], 100)
            self.assertEqual(groups["success"]["ranges_ms"][0]["percent"], 25)
            for invalid in (0, -1, float("nan"), float("inf"), 10081):
                with patch.object(capture, "module", return_value=reader):
                    with self.assertRaises(ValueError):
                        capture.report(root, last_minutes=invalid)

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
        self.assertEqual(self.status()["error_samples"], 0)
        self.kill.assert_not_called()
        result = capture.report(self.root / "rpc-counters")
        self.assertEqual(result["method_deltas"]["eth_sendRawTransaction"]["success"]["completed"], 2)
        self.assertEqual(result["valid_intervals"], 2)
        self.assertEqual((self.root / "run.json").read_bytes(), original_metadata)
        self.assertEqual((self.root / "tools/capture-main-cache-io.py").read_bytes(), original_helper)

    def test_new_image_subsecond_counters_survive_capture_and_report(self):
        traffic = latency_deltas([5, 50, 800])
        def fetch(*args, **kwargs):
            self.samples += 1
            return "\n".join("%s %s" % (key, 1000 + count * self.samples)
                             for key, count in traffic.items()).encode()
        self.helper.base.fetch = fetch
        self.assertEqual(capture.run(self.args), 0)
        self.assertEqual(self.status()["error_samples"], 0)
        result = capture.report(self.root / "rpc-counters")
        group = result["latency_buckets"]["eth_sendRawTransaction"]["success"]
        self.assertEqual(group["completed"], 6)
        self.assertEqual(group["valid_intervals"], 2)
        self.assertEqual([b["count"] for b in group["ranges_ms"]], [2, 0, 0, 2, 0, 0, 2, 0, 0, 0])

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
