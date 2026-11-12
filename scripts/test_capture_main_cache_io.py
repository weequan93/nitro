"""Offline cache/IO analysis checks; no Docker or node endpoint access."""

import argparse
import importlib.util
import io
import json
import os
from pathlib import Path
import tempfile
import time
from types import SimpleNamespace
import unittest
from unittest.mock import patch


spec = importlib.util.spec_from_file_location("cache_io", Path(__file__).with_name("capture-main-cache-io.py"))
capture = importlib.util.module_from_spec(spec)
spec.loader.exec_module(capture)


def disk():
    return {"reads": 100, "writes": 50, "read_sectors": 10000, "write_sectors": 2000,
            "read_ms": 500, "write_ms": 100, "weighted_ms": 1000, "busy_ms": 50, "inflight": 0}


def row(hits, misses, identity="instance-a"):
    return {"identity": identity, "errors": [], "metrics": {
        "l2chaindata_cache_block_hit": hits, "l2chaindata_cache_block_miss": misses}}


def args(root):
    return argparse.Namespace(out_dir=root / "capture", lock_file=root / "lock", container="main",
                              metrics_url="unused", device="vdb", minutes=0.04, interval=1, metrics_interval=15)


class CacheIOTests(unittest.TestCase):
    def test_identity_requires_a_running_native_nitro_process(self):
        selected = {"id": "container", "image": "sha256:unchanged", "pid": 42,
                    "running": True, "started": "2026-11-12T00:00:00Z"}
        fields = ["0"] * 22
        fields[0], fields[19] = "S", "12345"
        def text(path, _cap):
            return "nitro\n" if path.name == "comm" else "42 (nitro) " + " ".join(fields)
        with patch.object(capture.subprocess, "run", return_value=SimpleNamespace(stdout=json.dumps(selected))) as call, \
             patch.object(capture.base, "read_text", side_effect=text):
            result = capture.identity("main-nitro-1")
            self.assertEqual(result["start_ticks"], 12345)
            self.assertEqual(result["pid"], 42)
            self.assertEqual(call.call_args.args[0][:3], ["docker", "inspect", "--format"])
            self.assertNotIn(".Config", call.call_args.args[0][3])
        with patch.object(capture.subprocess, "run", return_value=SimpleNamespace(stdout=json.dumps(selected))), \
             patch.object(capture.base, "read_text", return_value="split-val-entry\n"):
            with self.assertRaisesRegex(ValueError, "not_ready"):
                capture.identity("main-nitro-1")

    def test_metrics_keep_cache_counts_and_existing_timer_units(self):
        result = capture.metrics(
            'l2chaindata_cache_block_hit 100\n'
            'l2chaindata_cache_block_miss 20\n'
            'state_snapshot_clean_storage_hit 50\n'
            'hashdb_memcache_clean_miss 4\n'
            'l2chaindata_tables_level0_sublevels 2\n'
            'arb_txpublish_sequence {quantile="0.999"} 123000\n'
            'secret_metric 9\n'
            'hashdb_memcache_clean_hit NaN\n')
        self.assertEqual(result["l2chaindata_cache_block_hit"], 100)
        self.assertEqual(result["hashdb_memcache_clean_miss"], 4)
        self.assertEqual(result['arb_txpublish_sequence{quantile="0.999"}'], 123000)
        self.assertNotIn("secret_metric", result)
        self.assertNotIn("hashdb_memcache_clean_hit", result)

    def test_disk_operations_sectors_and_milliseconds_have_distinct_units(self):
        previous = disk()
        current = dict(previous, reads=110, writes=54, read_sectors=10200, write_sectors=2160,
                       read_ms=520, write_ms=112, weighted_ms=1040, busy_ms=60)
        rates = capture.io_delta(current, previous, 2)
        self.assertEqual(rates["read_iops"], 5)
        self.assertEqual(rates["write_iops"], 2)
        self.assertEqual(rates["read_request_KiB"], 10)
        self.assertEqual(rates["write_request_KiB"], 20)
        self.assertEqual(rates["read_bytes_s"], 51200)
        self.assertEqual(rates["write_bytes_s"], 40960)
        self.assertEqual(rates["read_await_ms"], 2)
        self.assertEqual(rates["write_await_ms"], 3)
        self.assertAlmostEqual(rates["average_queue"], 0.02)
        self.assertEqual(capture.io_delta(previous, previous, 1)["read_iops"], 0)
        self.assertIsNone(capture.io_delta(previous, previous, 1)["read_request_KiB"])

    def test_missing_and_reset_device_counters_are_unavailable(self):
        self.assertIsNone(capture.io_delta(disk(), None, 1))
        current = dict(disk(), reads=1)
        self.assertIsNone(capture.io_delta(current, disk(), 1))
        self.assertIsNone(capture.io_delta(disk(), disk(), 0))

    def test_restart_errors_gaps_and_counter_reset_never_become_hits(self):
        previous, current = row(100, 10), row(200, 30)
        delta, reason = capture.cache_deltas(current, previous, 10)
        self.assertIsNone(reason)
        self.assertEqual(delta["pebble_block"]["hits_delta"], 100)
        self.assertEqual(delta["pebble_block"]["misses_delta"], 20)
        self.assertEqual(delta["pebble_block"]["misses_s"], 2)
        self.assertAlmostEqual(delta["pebble_block"]["hit_percent"], 100 / 120 * 100)
        self.assertIsNone(delta["trie_clean"]["hit_percent"])
        current["identity"] = "instance-b"
        self.assertEqual(capture.cache_deltas(current, previous, 10)[1], "identity_missing_or_changed")
        current["identity"] = previous["identity"]
        current["errors"] = ["timeout"]
        self.assertEqual(capture.cache_deltas(current, previous, 10)[1], "sample_error")
        current["errors"] = []
        self.assertEqual(capture.cache_deltas(current, previous, 50)[1], "sample_gap")
        current["metrics"]["l2chaindata_cache_block_hit"] = 1
        self.assertIsNone(capture.cache_deltas(current, previous, 10)[0]["pebble_block"]["hit_percent"])

    def test_zero_activity_is_not_a_hundred_percent_hit_rate(self):
        current = row(10, 5)
        delta, _ = capture.cache_deltas(current, current, 15)
        self.assertEqual(delta["pebble_block"]["hits_delta"], 0)
        self.assertEqual(delta["pebble_block"]["misses_delta"], 0)
        self.assertIsNone(delta["pebble_block"]["hit_percent"])

    def test_report_weights_counts_and_preserves_missing_data(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            caches = []
            for previous, current in ((row(0, 0), row(90, 10)), (row(90, 10), row(91, 19))):
                changes, _ = capture.cache_deltas(current, previous, 15)
                caches.append({"interval_s": 15, "cache_deltas": changes})
            (root / "cache.jsonl").write_text("".join(json.dumps(r) + "\n" for r in caches) + '{"partial":')
            (root / "io.jsonl").write_text(json.dumps({"rates": {"read_iops": 10000}}) + "\n" +
                                           json.dumps({"rates": {"read_iops": 5}}) + "\n")
            report = capture.report(root)
            pair = report["cache_intervals"]["pebble_block"]
            self.assertEqual(pair["hits_delta"], 91)
            self.assertEqual(pair["misses_delta"], 19)
            self.assertEqual(pair["hit_percent"], 82.727)  # Not the unweighted 50% mean.
            self.assertIsNone(report["cache_intervals"]["trie_clean"]["hit_percent"])
            self.assertEqual(report["device_sample_stats"]["read_iops"]["mean"], 5)
            self.assertEqual(report["malformed_rows"], 1)

    def test_slow_metrics_leave_io_sampling_independent_and_finish(self):
        def slow_fetch(*_args, **_kwargs):
            time.sleep(2.1)
            return b'l2chaindata_cache_block_hit 100\nl2chaindata_cache_block_miss 10\n'
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            old_mask = os.umask(0o077)
            try:
                with patch.object(capture, "identity", return_value={"id": "stable"}), \
                     patch.object(capture.base, "fetch", side_effect=slow_fetch), \
                     patch.object(capture.base, "disk_snapshot", return_value={"vdb": disk()}), \
                     patch.object(capture.base, "read_text", return_value=""), \
                     patch.object(capture.shutil, "disk_usage", return_value=SimpleNamespace(free=2**40)), \
                     patch.object(capture.signal, "signal"), patch("sys.stdout", new_callable=io.StringIO):
                    self.assertEqual(capture.run(args(root)), 0)
            finally:
                os.umask(old_mask)
            ios, _ = capture.read_rows(root / "capture" / "io.jsonl")
            caches, _ = capture.read_rows(root / "capture" / "cache.jsonl")
            self.assertGreaterEqual(len(ios), 2)
            self.assertLess(ios[1]["time_utc"], caches[0]["finished_utc"])
            self.assertEqual(json.loads((root / "capture" / "status.json").read_text())["kind"], "complete")
            with self.assertRaisesRegex(ValueError, "fresh output"):
                capture.run(args(root))

    def test_unexpected_worker_failure_is_not_reported_as_completed(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            old_mask = os.umask(0o077)
            try:
                with patch.object(capture, "identity", return_value={"id": "stable"}), \
                     patch.object(capture.base, "fetch", side_effect=RuntimeError("unexpected")), \
                     patch.object(capture.base, "disk_snapshot", return_value={"vdb": disk()}), \
                     patch.object(capture.base, "read_text", return_value=""), \
                     patch.object(capture.shutil, "disk_usage", return_value=SimpleNamespace(free=2**40)), \
                     patch.object(capture.signal, "signal"), patch("sys.stdout", new_callable=io.StringIO):
                    self.assertEqual(capture.run(args(root)), 1)
            finally:
                os.umask(old_mask)
            status = json.loads((root / "capture" / "status.json").read_text())
            self.assertEqual(status["kind"], "stopped")
            self.assertEqual(status["reason"], "worker_error")


if __name__ == "__main__":
    unittest.main()
