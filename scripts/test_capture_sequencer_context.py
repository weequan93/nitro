"""Offline checks: python3 -B scripts/test_capture_sequencer_context.py."""

import argparse
from datetime import datetime
import importlib.util
import io
import json
import os
from pathlib import Path
import tempfile
import time
import unittest
from unittest import mock


spec = importlib.util.spec_from_file_location(
    "context", Path(__file__).with_name("capture-sequencer-context.py"))
context = importlib.util.module_from_spec(spec)
spec.loader.exec_module(context)


class ResourceContextTests(unittest.TestCase):
    def test_independent_disks_and_missing_or_reset_counters(self):
        previous = {"vdb": {"reads": 10, "read_sectors": 100, "read_ms": 20,
                             "writes": 0, "write_sectors": 20, "write_ms": 0,
                             "busy_ms": 0, "weighted_ms": 0}}
        now = {"vdb": {"reads": 12, "read_sectors": 108, "read_ms": 26,
                        "writes": 0, "write_sectors": 20, "write_ms": 0,
                        "busy_ms": 500, "weighted_ms": 2000, "inflight": 1},
               "vdc": {"reads": 1, "read_ms": 1, "inflight": 0}}
        result = context.disk_delta(now, previous, 2)
        self.assertEqual(result["vdb"]["read_bytes_s"], 2048)
        self.assertEqual(result["vdb"]["read_await_ms"], 3)
        self.assertEqual(result["vdb"]["busy_percent"], 25)
        self.assertEqual(result["vdb"]["average_queue"], 1)
        self.assertIsNone(result["vdb"]["write_await_ms"])
        self.assertIsNone(result["vdc"]["read_await_ms"])
        self.assertIsNone(context.disk_delta(None, previous, 1))
        now["vdb"]["read_sectors"] = 1
        self.assertIsNone(context.disk_delta(now, previous, 2)["vdb"]["read_bytes_s"])

    def test_pid_reuse_and_missing_io_never_appear_as_zero(self):
        old = {"available": True, "start_ticks": 1, "cpu_ticks": 10, "major_faults": 0}
        current = {"available": True, "start_ticks": 1,
                   "cpu_ticks": 10 + os.sysconf("SC_CLK_TCK"), "major_faults": 2}
        rates = context.process_delta(current, old, 2)["rates"]
        self.assertEqual(rates["cpu_cores"], 0.5)
        self.assertEqual(rates["major_faults_s"], 1)
        self.assertIsNone(rates["read_bytes_s"])
        self.assertIsNone(rates["nr_throttled_delta"])
        current["start_ticks"] = 2
        self.assertIsNone(context.process_delta(current, old, 2)["rates"])

    def test_oracle_batch_size_is_decoded_not_guessed_from_input_length(self):
        word = lambda n: n.to_bytes(32, "big")
        raw = word(224) + bytes(192) + word(50) + bytes(50 * 128)
        block = {"number": "0x10", "hash": "0xabc", "gasUsed": "0x64", "transactions": [
            {"to": context.ORACLE, "input": context.SELECTOR + raw.hex()},
            {"to": "0xother", "input": context.SELECTOR + raw.hex()},
            {"to": context.ORACLE, "input": context.SELECTOR + raw[:-1].hex()},
            {"to": context.ORACLE},
        ]}
        result = context.workload(block)
        self.assertEqual(result["transactions"][0]["price_entries"], 50)
        self.assertNotIn("price_entries", result["transactions"][1])
        self.assertIsNone(result["transactions"][2]["price_entries"])
        self.assertIsNone(result["transactions"][3]["input_bytes"])
        self.assertIsNone(result["sequence_elapsed_ms"])
        self.assertEqual(result["selection"], "periodic_latest_unclassified")
        self.assertNotIn("input", result["transactions"][0])

    def test_metrics_spaces_units_and_missing_values(self):
        result = context.parse_metrics(
            'arb_txpublish_sequence {quantile="0.99"} 123000\n'
            'arb_txpublish_sequence_count 45\n'
            'arb_txpublish_sequence {quantile="0.75"} 12\n'
            'secret_metric 99\n')
        self.assertEqual(result['arb_txpublish_sequence{quantile="0.99"}'], 123000)
        self.assertEqual(result["arb_txpublish_sequence_count"], 45)
        self.assertEqual(len(result), 2)
        self.assertNotIn("arb_txpublish_block_write_count", result)

    def test_rpc_allowlist_and_json_rpc_failure(self):
        with mock.patch.object(context, "fetch") as fetch:
            with self.assertRaisesRegex(ValueError, "method_not_allowed"):
                context.rpc("http://localhost", "eth_sendRawTransaction", [])
            fetch.assert_not_called()
            fetch.return_value = b'{"error":{"code":-1}}'
            with self.assertRaisesRegex(ValueError, "block_unavailable"):
                context.rpc("http://localhost", "eth_getBlockByNumber", ["latest", True])

    def test_slow_rpc_does_not_block_resource_sampling_and_job_finishes(self):
        def slow_rpc(*_):
            time.sleep(2.1)
            return {"number": "0x10", "hash": "0xabc", "transactions": []}
        with tempfile.TemporaryDirectory() as folder:
            args = argparse.Namespace(out_dir=folder, container="test", minutes=0.04,
                                      interval=1, context_interval=15, metrics_url="test", rpc_url="test")
            original_umask = os.umask(0o077)
            try:
                with mock.patch.object(context, "discover", return_value=({}, None)), \
                     mock.patch.object(context, "disk_snapshot", return_value={}), \
                     mock.patch.object(context, "read_text", return_value=""), \
                     mock.patch.object(context, "fetch", return_value=b""), \
                     mock.patch.object(context, "rpc", side_effect=slow_rpc), \
                     mock.patch.object(context.signal, "signal"), \
                     mock.patch("sys.stdout", new_callable=io.StringIO):
                    context.run(args)
            finally:
                os.umask(original_umask)
            resources = [json.loads(s) for s in (Path(folder) / "resources.jsonl").read_text().splitlines()]
            contexts = [json.loads(s) for s in (Path(folder) / "context.jsonl").read_text().splitlines()]
            samples = [r for r in resources if r["kind"] == "resources"]
            self.assertGreaterEqual(len(samples), 2)
            self.assertLess(datetime.fromisoformat(samples[1]["time_utc"]),
                            datetime.fromisoformat(contexts[0]["finished_utc"]))
            self.assertEqual(resources[-1]["kind"], "complete")


if __name__ == "__main__":
    unittest.main()
