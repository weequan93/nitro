"""Offline orchestration checks using fake collectors, with no Docker/RPC."""

import argparse
from contextlib import ExitStack, redirect_stdout
import fcntl
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import signal
import tempfile
import threading
import time
import unittest
from unittest.mock import patch


spec = importlib.util.spec_from_file_location("longrun", Path(__file__).with_name("capture-sequencer-longrun.py"))
longrun = importlib.util.module_from_spec(spec)
spec.loader.exec_module(longrun)

FAKE = '''import argparse, json, os, signal, time
from pathlib import Path
p = argparse.ArgumentParser()
p.add_argument('--minutes', type=float)
p.add_argument('--out-dir', type=Path)
a, _ = p.parse_known_args()
stopped = False
def stop(*_):
    global stopped
    stopped = True
signal.signal(signal.SIGTERM, stop)
a.out_dir.mkdir(exist_ok=True)
(a.out_dir / 'started.json').write_text(json.dumps({'pid': os.getpid()}))
(a.out_dir / 'source-marker.txt').write_text('original')
end = time.monotonic() + a.minutes * 60
while not stopped and time.monotonic() < end:
    time.sleep(0.01)
(a.out_dir / 'ended.txt').write_text('signal' if stopped else 'duration')
'''


class LongrunTests(unittest.TestCase):
    def setUp(self):
        self.stack = ExitStack()
        self.addCleanup(self.stack.close)
        self.root = Path(self.stack.enter_context(tempfile.TemporaryDirectory())).resolve()
        self.source = self.root / "source.py"
        self.source.write_text(FAKE)
        self.args = argparse.Namespace(
            out_dir=self.root / "out", collector=self.source, hours=0.0005, segment_minutes=0.006,
            interval=1, context_interval=15, container="main-nitro-1", rpc_url="unused", metrics_url="unused",
            lock_file=self.root / "capture.lock", events_file=None, max_output_gib=1, min_free_gib=0)
        self.stack.enter_context(patch.object(longrun, "active_collectors", return_value=[]))
        self.handlers = {}
        def install(sig, handler):
            self.handlers[sig] = handler
            return signal.SIG_DFL
        self.stack.enter_context(patch.object(longrun.signal, "signal", side_effect=install))
        self.stack.enter_context(redirect_stdout(io.StringIO()))
        old_mask = os.umask(0o077)
        self.addCleanup(os.umask, old_mask)

    def status(self):
        return json.loads((self.args.out_dir / "status.json").read_text())

    def test_rotation_keeps_the_original_collector_code(self):
        def change_installed_source():
            end = time.monotonic() + 3
            while time.monotonic() < end:
                if (self.args.out_dir / "segment-0001" / "started.json").exists():
                    self.source.write_text(FAKE.replace("original", "updated"))
                    return
                time.sleep(0.01)
        thread = threading.Thread(target=change_installed_source, daemon=True)
        thread.start()
        self.assertEqual(longrun.run(self.args), 0)
        thread.join(timeout=3)
        self.assertEqual(self.status()["kind"], "complete")
        segments = sorted(self.args.out_dir.glob("segment-*"))
        self.assertGreaterEqual(len(segments), 2)
        self.assertTrue(all((p / "source-marker.txt").read_text() == "original" for p in segments))
        metadata = json.loads((self.args.out_dir / "run.json").read_text())
        self.assertEqual(metadata["collector_sha256"], hashlib.sha256(FAKE.encode()).hexdigest())
        with self.assertRaisesRegex(ValueError, "fresh directory"):
            longrun.run(self.args)

    def test_parent_stop_gracefully_stops_its_child_and_records_cancellation(self):
        self.args.hours = 1
        self.args.segment_minutes = 60
        def request_stop():
            end = time.monotonic() + 3
            while time.monotonic() < end:
                if (self.args.out_dir / "segment-0001" / "started.json").exists():
                    self.handlers[signal.SIGTERM](signal.SIGTERM, None)
                    return
                time.sleep(0.01)
        thread = threading.Thread(target=request_stop, daemon=True)
        thread.start()
        self.assertEqual(longrun.run(self.args), 0)
        thread.join(timeout=3)
        self.assertEqual(self.status()["kind"], "stopped")
        self.assertEqual(self.status()["reason"], "signal")
        segment = self.args.out_dir / "segment-0001"
        self.assertEqual((segment / "ended.txt").read_text(), "signal")
        child_pid = json.loads((segment / "started.json").read_text())["pid"]
        with self.assertRaises(ProcessLookupError):
            os.kill(child_pid, 0)

    def test_child_failure_and_unexpected_early_success_are_visible(self):
        for code in (0, 7):
            with self.subTest(code=code):
                self.args.out_dir = self.root / ("failure-%s" % code)
                self.args.hours = 1
                self.args.segment_minutes = 60
                self.source.write_text("raise SystemExit(%s)\n" % code)
                self.assertEqual(longrun.run(self.args), 1)
                self.assertEqual(self.status()["reason"], "segment_failed_or_ended_early")
                self.assertEqual(self.status()["segments_started"], 1)

    def test_live_budget_limit_stops_the_owned_child(self):
        self.args.hours = 1
        self.args.segment_minutes = 60
        with patch.object(longrun, "CHECK_INTERVAL", 0.05), \
             patch.object(longrun, "budget_reason", side_effect=[None, None, "output_size_limit"]):
            self.assertEqual(longrun.run(self.args), 1)
        self.assertEqual(self.status()["reason"], "output_size_limit")
        self.assertEqual((self.args.out_dir / "segment-0001" / "ended.txt").read_text(), "signal")

    def test_existing_collector_and_lock_refuse_duplicates(self):
        with patch.object(longrun, "active_collectors", return_value=[123]):
            with self.assertRaisesRegex(ValueError, "already running"):
                longrun.run(self.args)
        self.assertFalse((self.args.out_dir / "run.json").exists())
        with self.args.lock_file.open("a") as lock:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            with self.assertRaisesRegex(ValueError, "holds the lock"):
                longrun.run(self.args)

    def test_budget_is_checked_before_collecting(self):
        with patch.object(longrun, "budget_reason", return_value="free_space_limit"):
            with self.assertRaisesRegex(ValueError, "free_space_limit"):
                longrun.run(self.args)
        self.assertFalse((self.args.out_dir / "run.json").exists())

    def test_oversized_source_is_refused_before_starting(self):
        self.source.write_bytes(b"x" * (2 * 1024**2 + 1))
        with self.assertRaisesRegex(ValueError, "exceeds 2 MiB"):
            longrun.run(self.args)
        self.assertFalse((self.args.out_dir / "run.json").exists())


class DiscoveryTests(unittest.TestCase):
    def test_parent_reference_and_watcher_are_not_mistaken_for_collectors(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            programs = {
                1: ["python3", "-B", "capture-sequencer-context.py", "--out-dir", "x"],
                2: ["python3", "capture-sequencer-longrun.py", "--collector", "capture-sequencer-context.py"],
                3: ["python3", "watch-slow-sequencer-blocks.py"],
            }
            for pid, argv in programs.items():
                (root / str(pid)).mkdir()
                (root / str(pid) / "cmdline").write_bytes("\0".join(argv).encode())
            self.assertEqual(longrun.active_collectors(root), [1])


if __name__ == "__main__":
    unittest.main()
