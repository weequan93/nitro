"""Synthetic recovery-log review checks; no server access or operational data."""

import gzip
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest


spec = importlib.util.spec_from_file_location("recovery_review", Path(__file__).with_name("review-rpc-recovery-snapshot.py"))
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)


class RecoveryReviewTests(unittest.TestCase):
    def fixture(self, root, lines, clock=True):
        (root / "log-snapshot").mkdir()
        # Deliberately misleading filename; coverage must come from content.
        name = "nitro-2026-01-01.log.gz"
        (root / "log-snapshot" / name).write_bytes(gzip.compress("\n".join(lines).encode() + b"\n"))
        (root / "manifest.json").write_text(json.dumps([{"name": name}]))
        docker = root / "docker.log"
        docker.write_text('2026-02-01T04:00:00.001Z INFO [02-01|12:00:00.000] ready\n'
                          '2026-02-01T04:00:01.001Z INFO [02-01|12:00:01.000] ready\n' if clock else "no paired timestamps\n")
        return docker

    def review(self, root, docker, budget=1024**2):
        return m.review(root, docker, m.archive.stamp("2026-02-01T03:59:59Z"),
                        m.archive.stamp("2026-02-01T04:01:00Z"), budget)

    def test_clock_coverage_all_rpc_errors_and_stage_overlap(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            docker = self.fixture(root, [
                'WARN [02-01|12:00:02.000] Served eth_call duration=6ms err="execution reverted: _period err"',
                'WARN [02-01|12:00:03.000] Served eth_call duration=9ms err="execution reverted: _period err"',
                'WARN [02-01|12:00:04.000] Served eth_estimateGas duration=2ms err="execution reverted"',
                'WARN [02-01|12:00:05.000] Served eth_sendRawTransaction duration=1.2s err="context deadline exceeded"',
                'WARN [02-01|12:00:05.000] RPC stage timings method=eth_sendRawTransaction outcome=failure trace=1 total_ms=1200 callback_ms=1199',
                'WARN [02-01|12:02:00.000] Served eth_call duration=4s err="outside window"'])
            value = self.review(root, docker)
            self.assertEqual(value["clock"]["offset_seconds"], 8 * 3600)
            self.assertEqual(value["files"][0]["first_sgt"], "2026-02-01T12:00:02+08:00")
            self.assertEqual(value["quality"]["lines_in_window"], 5)
            period = value["error_groups"][0]
            self.assertEqual((period["method"], period["records"], period["min_ms"], period["max_ms"]),
                             ("eth_call", 2, 6, 9))
            self.assertEqual(sum(g["records"] for g in value["error_groups"]), 4)
            self.assertEqual(value["stage_record_counts"][0]["records"], 1)
            self.assertEqual(value["keyword_records"]["deadline"]["records"], 1)

    def test_no_clock_excludes_terminal_records_but_accepts_absolute_json(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            docker = self.fixture(root, [
                'WARN [02-01|12:00:02.000] Served eth_call duration=1ms err="not calibrated"',
                json.dumps({"time": "2026-02-01T04:00:02Z", "msg": "Served eth_call", "err": "absolute", "duration": "2ms"})], clock=False)
            value = self.review(root, docker)
            self.assertFalse(value["clock"]["resolved"])
            self.assertEqual(value["quality"]["unresolved_timestamp_lines"], 1)
            self.assertEqual(value["error_groups"][0]["error_group"], "absolute")
            self.assertEqual(value["error_groups"][0]["records"], 1)

    def test_scan_limit_and_manifest_paths_are_rejected(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            docker = self.fixture(root, ['INFO [02-01|12:00:02.000] line'])
            with self.assertRaisesRegex(ValueError, "budget"):
                self.review(root, docker, budget=1)
            (root / "manifest.json").write_text(json.dumps([{"name": "../outside.log"}]))
            with self.assertRaisesRegex(ValueError, "filename"):
                self.review(root, docker)

    def test_copy_failures_and_missing_durations_remain_visible(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            docker = self.fixture(root, ['WARN [02-01|12:00:02.000] Served eth_call err="unknown duration"'])
            manifest = json.loads((root / "manifest.json").read_text())
            manifest.append({"name": "missing.log", "error": "source_short_read"})
            (root / "manifest.json").write_text(json.dumps(manifest))
            value = self.review(root, docker)
            self.assertEqual(value["files"][1]["error"], "source_short_read")
            group = value["error_groups"][0]
            self.assertEqual(group["duration_samples"], 0)
            self.assertIsNone(group["max_ms"])


if __name__ == "__main__":
    unittest.main()
