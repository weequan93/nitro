"""Offline lifecycle tests; fake collectors never contact Docker or Nitro."""

import argparse
from contextlib import ExitStack, redirect_stdout
import importlib.util
import io
import itertools
import json
import os
from pathlib import Path
import signal
import tempfile
import threading
import time
import unittest
from unittest.mock import patch


spec = importlib.util.spec_from_file_location(
    "suite", Path(__file__).with_name("capture-sequencer-suite.py"))
suite = importlib.util.module_from_spec(spec)
spec.loader.exec_module(suite)

FAKE = '''
if __name__ == "__main__":
    import argparse, json, os, signal, time
    from pathlib import Path
    p = argparse.ArgumentParser()
    p.add_argument("--out-dir", type=Path)
    p.add_argument("--hours", type=float, default=1)
    p.add_argument("--minutes", type=float, default=60)
    p.add_argument("--suite-dir", type=Path)
    a, _ = p.parse_known_args()
    a.out_dir.mkdir(parents=True, exist_ok=True)
    name = Path(__file__).name
    stopped = False
    def stop(*_):
        global stopped
        stopped = True
    signal.signal(signal.SIGTERM, stop)
    (a.out_dir / "started.json").write_text(json.dumps({"pid": os.getpid(), "name": name}))
    (a.out_dir / "source-marker").write_text("pinned-original")
    duration = (a.minutes * 60 if "cache-io" in name else
                a.hours * 3600 if "longrun" in name else 3600)
    if "rpc-diagnostics" in name:
        from datetime import datetime
        metadata = json.loads((a.suite_dir / "run.json").read_text())
        duration = max(0, datetime.fromisoformat(metadata["started_utc"]).timestamp() + metadata["hours"] * 3600 - time.time())
    if "cache-io" in name:
        CACHE_FAILURE
    end = time.monotonic() + duration
    while not stopped and time.monotonic() < end:
        time.sleep(0.01)
    (a.out_dir / "ended.txt").write_text("signal" if stopped else "duration")
'''.replace("CACHE_FAILURE", "pass")


class LifecycleTests(unittest.TestCase):
    def setUp(self):
        self.stack = ExitStack()
        self.addCleanup(self.stack.close)
        self.root = Path(self.stack.enter_context(tempfile.TemporaryDirectory())).resolve()
        self.source = self.root / "source"
        self.source.mkdir()
        for name in suite.SOURCES:
            (self.source / name).write_text(FAKE)
        self.args = argparse.Namespace(out_dir=self.root / "out", hours=0.0015, segment_minutes=0.015,
            compactions=1, pacing_bytes_per_second=64 * 1024**2, device="vdb", ready_timeout=5,
            rpc_url="unused", metrics_url="unused", replace_existing=True,
            lock_file=self.root / "suite.lock", max_output_gib=1, min_free_gib=0)
        self.baseline = {"settings": {"concurrency": 1}, "image": "sha256:unchanged"}
        self.original_pin = suite.pin
        self.stack.enter_context(patch.object(suite, "pin",
            side_effect=lambda root: self.original_pin(root, self.source)))
        self.stack.enter_context(patch.object(suite, "ready", return_value={"heads": [100, 101]}))
        self.stack.enter_context(patch.object(suite, "validate_device", return_value="252:16"))
        self.check = self.stack.enter_context(patch.object(suite, "runtime_check", return_value=self.baseline))
        self.takeover = self.stack.enter_context(patch.object(suite, "takeover", return_value=[]))
        self.stack.enter_context(patch.object(suite, "inventory", return_value=[]))
        self.handlers = {}
        def install(sig, handler):
            self.handlers[sig] = handler
            return signal.SIG_DFL
        self.stack.enter_context(patch.object(suite.signal, "signal", side_effect=install))
        self.stack.enter_context(redirect_stdout(io.StringIO()))
        previous_mask = os.umask(0o077)
        self.addCleanup(os.umask, previous_mask)

    def status(self):
        return json.loads((self.args.out_dir / "status.json").read_text())

    def test_all_collectors_run_cache_rotates_and_sources_are_pinned(self):
        def mutate_source():
            end = time.monotonic() + 3
            marker = self.args.out_dir / "cache-io/segment-0001/started.json"
            while time.monotonic() < end:
                if marker.exists():
                    (self.source / "capture-main-cache-io.py").write_text(FAKE.replace(
                        "pinned-original", "updated-install"))
                    return
                time.sleep(0.01)
        thread = threading.Thread(target=mutate_source, daemon=True)
        thread.start()
        self.assertEqual(suite.run(self.args), 0)
        thread.join(timeout=3)
        self.assertEqual(self.status()["kind"], "complete")
        self.assertEqual((self.args.out_dir / "resources/ended.txt").read_text(), "duration")
        self.assertEqual((self.args.out_dir / "slow-blocks/ended.txt").read_text(), "signal")
        segments = sorted((self.args.out_dir / "cache-io").glob("segment-*"))
        self.assertGreaterEqual(len(segments), 2)
        self.assertTrue(all((p / "source-marker").read_text() == "pinned-original" for p in segments))
        self.assertEqual(json.loads((self.args.out_dir / "runtime-baseline.json").read_text()), self.baseline)
        self.takeover.assert_called_once_with(True)
        self.assertGreaterEqual(self.check.call_count, 3)
        with self.assertRaisesRegex(ValueError, "new directory"):
            suite.run(self.args)

    def test_sampler_failure_stops_the_other_owned_processes(self):
        self.args.hours = 1
        self.args.segment_minutes = 60
        (self.source / "capture-main-cache-io.py").write_text(
            FAKE.replace('if "cache-io" in name:\n        pass',
                         'if "cache-io" in name:\n        raise SystemExit(7)'))
        self.assertEqual(suite.run(self.args), 1)
        self.assertEqual(self.status()["kind"], "stopped")
        self.assertIn("Cache collector failed", self.status()["reason"])
        for folder in ("resources", "slow-blocks"):
            self.assertEqual((self.args.out_dir / folder / "ended.txt").read_text(), "signal")

    def test_optional_rpc_collector_is_owned_pinned_and_finishes_at_suite_deadline(self):
        self.args.rpc_counters = True
        self.assertEqual(suite.run(self.args), 0, self.status())
        self.assertEqual(self.status()["kind"], "complete")
        self.assertEqual(self.status()["children"]["rpc"]["returncode"], 0)
        self.assertEqual((self.args.out_dir / "rpc-counters/ended.txt").read_text(), "duration")
        self.assertTrue((self.args.out_dir / "tools/capture-rpc-diagnostics.py").exists())

    def test_optional_rpc_failure_stops_other_owned_collectors(self):
        self.args.rpc_counters = True
        (self.source / "capture-rpc-diagnostics.py").write_text(FAKE.replace(
            'if "rpc-diagnostics" in name:', 'if "rpc-diagnostics" in name:\n        raise SystemExit(7)'))
        self.assertEqual(suite.run(self.args), 1)
        self.assertIn("rpc collector exited", self.status()["reason"])
        self.assertEqual(self.status()["children"]["rpc"]["returncode"], 7)
        self.assertTrue(all(c["returncode"] is not None for c in self.status()["children"].values()))

    def test_signal_stops_all_owned_collectors(self):
        self.args.hours = 1
        self.args.segment_minutes = 60
        def request_stop():
            end = time.monotonic() + 5
            while time.monotonic() < end:
                if (self.args.out_dir / "cache-io/segment-0001/started.json").exists():
                    self.handlers[signal.SIGTERM](signal.SIGTERM, None)
                    return
                time.sleep(0.01)
        thread = threading.Thread(target=request_stop, daemon=True)
        thread.start()
        self.assertEqual(suite.run(self.args), 0)
        thread.join(timeout=5)
        self.assertEqual(self.status()["reason"], "signal")
        for folder in ("resources", "slow-blocks", "cache-io/segment-0001"):
            self.assertEqual((self.args.out_dir / folder / "ended.txt").read_text(), "signal")

    def test_runtime_mismatch_precedes_takeover_and_starts_nothing(self):
        self.check.side_effect = ValueError("Unexpected concurrency")
        with self.assertRaisesRegex(ValueError, "Unexpected concurrency"):
            suite.run(self.args)
        self.takeover.assert_not_called()
        self.assertFalse((self.args.out_dir / "run.json").exists())

    def test_disk_budget_stops_all_collectors(self):
        support = suite.module("actual_longrun_for_test", suite.DIRECTORY / "capture-sequencer-longrun.py")
        with patch.object(support, "budget_reason", side_effect=[None, "output_size_limit"]), \
             patch.object(suite, "module", side_effect=lambda name, path:
                          support if name == "suite_longrun" else object()):
            self.assertEqual(suite.run(self.args), 1)
        self.assertIn("output_size_limit", self.status()["reason"])
        self.assertTrue(all(child["returncode"] is not None for child in self.status()["children"].values()))

    def test_duplicate_suite_lock_does_not_stop_existing_monitors(self):
        import fcntl
        with self.args.lock_file.open("a") as lock:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            with self.assertRaisesRegex(ValueError, "suite is running"):
                suite.run(self.args)
        self.takeover.assert_not_called()


class ProcessTests(unittest.TestCase):
    def row(self, pid, program, container=None, ppid=1):
        argv = ["python3", "-B", program, "--out-dir", "/data/old"]
        if container:
            argv += ["--container", container]
        return {"pid": pid, "ppid": ppid, "start_ticks": pid * 100, "argv": argv}

    def test_takeover_excludes_node_other_containers_and_argument_references(self):
        rows = [
            self.row(10, "capture-sequencer-longrun.py"),
            self.row(11, "/data/old/tools/capture-sequencer-context.py", ppid=10),
            self.row(12, "watch-slow-sequencer-blocks.py"),
            self.row(13, "capture-main-cache-io.py", container="another-node"),
            {"pid": 14, "ppid": 1, "start_ticks": 1400, "argv": ["nitro", "--conf.file", "/config/node.json"]},
            {"pid": 15, "ppid": 12, "start_ticks": 1500,
             "argv": ["docker", "logs", "--follow", "--timestamps", "--since", "10s", "main-nitro-1"]},
            self.row(16, "unrelated-tool.py"),
        ]
        rows[-1]["argv"] += ["--collector", "capture-sequencer-context.py"]
        active = {r["pid"]: r for r in rows}
        sent = []
        def send(row, sig):
            sent.append((row["pid"], sig))
            active.pop(row["pid"], None)
        with patch.object(suite, "inventory", side_effect=lambda: list(active.values())), \
             patch.object(suite, "process_info", side_effect=lambda pid: active.get(pid)), \
             patch.object(suite, "send", side_effect=send):
            old = suite.takeover(True)
        self.assertEqual({r["pid"] for r in old}, {10, 11, 12})
        self.assertEqual({pid for pid, _ in sent}, {10, 11, 12, 15})
        self.assertEqual(set(active), {13, 14, 16})

    def test_takeover_without_authorization_sends_no_signals(self):
        with patch.object(suite, "inventory", return_value=[self.row(10, "capture-sequencer-longrun.py")]), \
             patch.object(suite, "send") as send:
            with self.assertRaisesRegex(ValueError, "replace-existing"):
                suite.takeover(False)
        send.assert_not_called()

    def test_pid_reuse_never_receives_signal(self):
        row = self.row(10, "capture-main-cache-io.py")
        current = dict(row, start_ticks=9999)
        with patch.object(suite, "process_info", return_value=current), patch.object(suite.os, "kill") as kill:
            self.assertFalse(suite.send(row, signal.SIGTERM))
        kill.assert_not_called()

    def test_discovery_uses_the_program_not_later_script_arguments(self):
        row = self.row(10, "capture-sequencer-longrun.py")
        row["argv"] += ["--collector", "/tmp/capture-sequencer-context.py"]
        self.assertEqual(suite.monitor(row)["name"], "capture-sequencer-longrun.py")
        row["argv"][2] = "report-sequencer-context.py"
        self.assertIsNone(suite.monitor(row))

    def test_proc_identity_and_device_validation(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            process = root / "123"
            process.mkdir()
            (process / "cmdline").write_bytes(b"python3\0-B\0/data/capture-main-cache-io.py\0--out-dir\0/data/old\0")
            fields = ["S", "1"] + ["0"] * 17 + ["98765"]
            (process / "stat").write_text("123 (python name) " + " ".join(fields))
            observed = suite.process_info(123, root)
            self.assertEqual(observed["start_ticks"], 98765)
            self.assertEqual(observed["ppid"], 1)
            self.assertEqual(suite.monitor(observed)["name"], "capture-main-cache-io.py")
            device = root / "vdb"
            device.mkdir()
            (device / "dev").write_text("252:16\n")
            self.assertEqual(suite.validate_device("vdb", root), "252:16")
            (device / "partition").touch()
            with self.assertRaises(ValueError):
                suite.validate_device("vdb", root)
            with self.assertRaises(ValueError):
                suite.validate_device("missing", root)

    def test_rpc_readiness_requires_subsecond_counters(self):
        rpc = suite.module("test_rpc_metric_keys", suite.DIRECTORY / "capture-rpc-diagnostics.py")
        args = argparse.Namespace(ready_timeout=5, rpc_url="rpc", metrics_url="metrics", rpc_counters=True)
        for keys, accepted in ((rpc.ALL_KEYS, False), (rpc.TRACKED_KEYS, True)):
            with self.subTest(accepted=accepted):
                metrics = "l2chaindata_compact_debt 1\n" + "\n".join(k + " 0" for k in keys)
                def fetch(url, body=None):
                    return b'{"result":"0x64"}' if body else metrics.encode()
                with patch.object(suite, "fetch", side_effect=fetch), \
                     patch.object(suite.time, "monotonic", side_effect=itertools.count()), \
                     patch.object(suite.time, "sleep"):
                    if accepted:
                        self.assertEqual(suite.ready(args)["heads"], [100, 100])
                    else:
                        with self.assertRaises(ValueError):
                            suite.ready(args)


if __name__ == "__main__":
    unittest.main()
