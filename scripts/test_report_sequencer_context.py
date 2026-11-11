"""Offline checks: python3 -B scripts/test_report_sequencer_context.py."""

import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


SCRIPT = Path(__file__).with_name("report-sequencer-context.py")
spec = importlib.util.spec_from_file_location("report_context", SCRIPT)
reporter = importlib.util.module_from_spec(spec)
spec.loader.exec_module(reporter)


def timestamp(second):
    return "2026-11-11T02:30:%02d+00:00" % second


def resource(second):
    return {"kind": "resources", "time_utc": timestamp(second), "interval_s": 1,
            "cpu_ticks": [second * 10, 0, 0, second * 10, 0, 0, 0, 0],
            "devices": {"vdb": {"read_await_ms": 2 if second in (12, 13) else 1}},
            "processes": [{"role": "nitro", "pid": 123, "start_ticks": 50, "available": True,
                           "rates": {"cpu_cores": 8 if second in (12, 13) else 2}}]}


def slow_event(second=13):
    return {"kind": "slow_sequence_block", "log_time_utc": timestamp(second),
            "sequence_elapsed_ms": 2000, "block_number": 10, "block_hash": "0xabc",
            "transactions": [{"to": "oracle", "selector": "0x71044186", "receipt_gas_used": 50}]}


class ReportTests(unittest.TestCase):
    def test_exact_sequence_intervals_guard_and_dedup(self):
        resources = [resource(s) for s in range(10, 16)]
        event = slow_event()
        report = reporter.build_report(resources, [], [event, event, slow_event(2)], guard=1)
        counts = report["coverage"]["group_sample_counts"]
        self.assertEqual(counts["during_logged_slow_sequence"], 2)
        self.assertEqual(counts["outside_slow_sequence_guard"], 1)
        self.assertEqual(report["coverage"]["logged_slow_blocks"], 1)
        self.assertFalse(report["coverage"]["collector_final_record_present"])
        cpu = report["resource_comparison"]["process.nitro.pid123.start50.cpu_cores"]
        self.assertEqual(cpu["during_logged_slow_sequence"]["mean"], 8)
        self.assertEqual(cpu["outside_slow_sequence_guard"]["mean"], 2)
        self.assertEqual(report["slowest_blocks"][0]["gas_used"], 50)

    def test_missing_deltas_separate_devices_and_processes(self):
        row = resource(12)
        row["processes"] += [{"role": "nitro-val", "pid": 124, "start_ticks": 40,
                              "available": True, "rates": {"cpu_cores": 3}}]
        row["devices"]["vdc"] = {"read_await_ms": None, "read_bytes_s": 0}
        values = reporter.flatten(row, resource(11))
        self.assertEqual(values["process.nitro.pid123.start50.cpu_cores"], 8)
        self.assertEqual(values["process.nitro-val.pid124.start40.cpu_cores"], 3)
        self.assertNotIn("device.vdc.read_await_ms", values)
        self.assertEqual(values["device.vdc.read_MiB_s"], 0)
        self.assertNotIn("process.nitro.pid123.start50.throttled_ms_s", values)
        self.assertEqual(values["host.cpu_busy_percent"], 50)
        row["cpu_ticks"] = [0] * 8  # Counter reset must not become 100% idle.
        self.assertNotIn("host.cpu_busy_percent", reporter.flatten(row, resource(11)))

    def test_unclassified_controls_not_declared_fast_and_missing_receipts(self):
        event = slow_event()
        event["transactions"] += [{"to": "oracle", "receipt_gas_used": None}]
        contexts = [{"started_utc": timestamp(12), "metrics": {
            'arb_txpublish_sequence{quantile="0.999"}': 1500000}, "block": {
                "block_number": 10, "block_hash": "0xabc", "block_gas_used": 50,
                "transactions": [{"price_entries": 34}]}}]
        report = reporter.build_report([resource(s) for s in range(10, 16)], contexts, [event])
        self.assertIsNone(report["slowest_blocks"][0]["gas_used"])
        self.assertEqual(report["workload_controls"]["also_in_logged_slow_blocks"], 1)
        self.assertNotIn("fast_blocks", report["workload_controls"])
        self.assertEqual(report["metric_snapshots"]["sequence_snapshot_p999_ms"]["max"], 1500)
        self.assertEqual(report["metric_snapshots"]["block_execution_snapshot_p999_ms"]["n"], 0)

    def test_cli_saved_only_handles_partial_line_and_reports_final_record(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            rows = [{"kind": "start", "minutes": 1}] + [resource(s) for s in range(10, 16)] + [{"kind": "complete"}]
            (root / "resources.jsonl").write_text("".join(json.dumps(r) + "\n" for r in rows))
            (root / "context.jsonl").write_text('{"unfinished":')
            (root / "events.jsonl").write_text(json.dumps(slow_event()) + "\n")
            result = subprocess.run([sys.executable, "-B", str(SCRIPT), "--capture-dir", folder,
                                     "--events-file", str(root / "events.jsonl"), "--json"],
                                    capture_output=True, text=True, timeout=10)
            self.assertEqual(result.returncode, 0, result.stderr)
            report = json.loads(result.stdout)
            self.assertTrue(report["coverage"]["collector_final_record_present"])
            self.assertEqual(report["input_snapshots"]["context"]["malformed_or_partial"], 1)
            self.assertEqual(report["coverage"]["resources"], 6)

    def test_boundary_sequence_is_marked_clipped(self):
        event = slow_event(11)  # Starts before resource coverage.
        report = reporter.build_report([resource(s) for s in range(10, 16)], [], [event])
        self.assertTrue(report["slowest_blocks"][0]["resource_window_clipped"])
        self.assertEqual(report["slowest_blocks"][0]["resource_samples"], 1)


if __name__ == "__main__":
    unittest.main()
