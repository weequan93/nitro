"""Synthetic fixed-window end-to-end checks; no live RPC, Docker or server data."""

from contextlib import redirect_stdout
import importlib.util
import io
import json
from pathlib import Path
import shutil
import tempfile
import unittest


def load(name, filename):
    spec = importlib.util.spec_from_file_location(name, Path(__file__).with_name(filename))
    result = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(result)
    return result


review = load("combined_tail_review_test", "review-rpc-tails.py")
fixtures = load("combined_tail_fixture", "test_review_rpc_tail_data.py")
m = fixtures.m


class ReviewTests(unittest.TestCase):
    def test_restart_with_the_same_container_id_cannot_merge_trace_namespaces(self):
        first = {"id": "same-container", "pid": 10, "start_ticks": 100}
        second = {"id": "same-container", "pid": 20, "start_ticks": 200}
        with self.assertRaisesRegex(ValueError, "node identity change"):
            review.require_single_instance([{"identity": first}, {"identity": second}])
        review.require_single_instance([{"identity": first}, {"identity": dict(first)}])

    def test_fixed_counters_archives_publisher_context_and_post_warmup(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary).resolve()
            (root / "tools").mkdir()
            (root / "logs").mkdir()
            shutil.copyfile(Path(__file__).with_name("report-sequencer-context.py"),
                            root / "tools/report-sequencer-context.py")

            def write(name, rows):
                path = root / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text("".join(json.dumps(row) + "\n" for row in rows))

            base, start_time = fixtures.baseline(), m.stamp(fixtures.START)
            active = fixtures.sample(15, failure=2)
            for suffix, value in zip(m.SUFFIXES, [2, 2, 1, 1, 1, 1, 1, 1, 0, 0]):
                key = "rpc_diagnostics_eth_sendRawTransaction_failure_" + suffix
                (active["deltas"] if key in active["deltas"] else active["subsecond_deltas"])[key] = value
            write("rpc-counters/counters.jsonl", [base, active,
                fixtures.sample(30, success=[0] * 10), fixtures.sample(45, failure=99)])
            (root / "phase.json").write_text(json.dumps({
                "comparison_not_before_utc": m.iso_utc(start_time + 15)}))
            write("resources/segment-0001/resources.jsonl", [
                {"kind": "resources", "time_utc": m.iso_utc(start_time + second),
                 "interval_s": 1, "cpu_ticks": [second * 20, 0, second * 5, second * 40, second * 35, 0, 0, 0],
                 "meminfo_bytes": {"MemAvailable": 25 * 1024**3},
                 "processes": [{"role": "nitro", "available": True, "pid": 123, "start_ticks": 100,
                                "rates": {"cpu_cores": 5}, "rss_bytes": 6 * 1024**3}],
                 "devices": {"vdb": {"read_await_ms": .3, "average_queue": 2}}}
                for second in range(31)])
            write("resources/segment-0001/context.jsonl", [
                {"started_utc": m.iso_utc(start_time + second), "metrics_error": None,
                 "metrics": {'arb_txpublish_block_execution{quantile="0.999"}': 2000000,
                             "arb_txpublish_block_execution_count": 10},
                 "block": {"block_number": 100, "block_gas_used": 12345, "transactions": []}}
                for second in (0, 15, 30, 45)])
            write("cache-io/segment-0001/cache.jsonl", [
                {"time_utc": m.iso_utc(start_time + second), "identity": fixtures.IDENTITY, "errors": [],
                 "metrics": {"l2chaindata_compact_debt": 100 * 1024**3,
                             "l2chaindata_compact_inprogress": 0,
                             "l2chaindata_compact_writedelay_counter": 0,
                             "l2chaindata_compact_pacing_output": second * 1000000}}
                for second in (0, 15, 30)])
            write("slow-blocks/events.jsonl", [
                {"kind": "rpc_slow", "log_time_utc": m.iso_utc(start_time + second),
                 "method": "eth_sendRawTransaction", "outcome": outcome, "trace": trace,
                 "total_ms": duration, "callback_ms": duration - .01}
                for second, trace, duration, outcome in ((12, 1, 1200, "success"),
                    (13, 2, 3400, "success"), (14, 3, 1100, "failure"))])
            lines = [
                'WARN [01-01|00:00:10.000] Served eth_sendRawTransaction duration=20ms err="nonce too low: address 0x123456, tx: 1 state: 2"',
                'WARN [01-01|00:00:14.000] Served eth_sendRawTransaction duration=1.1s err="context deadline exceeded"',
            ]
            for second, trace, duration, outcome in ((12, 1, 1200, "success"),
                    (13, 2, 3400, "success"), (14, 3, 1100, "failure")):
                lines.append(f'WARN [01-01|00:00:{second:02d}.000] RPC stage timings method=eth_sendRawTransaction '
                             f'outcome={outcome} trace={trace} total_ms={duration} callback_ms={duration - .01}')
            (root / "logs/nitro.log").write_text("\n".join(lines) + "\n")
            original = (root / "rpc-counters/counters.jsonl").read_bytes()
            console = io.StringIO()
            with redirect_stdout(console):
                path = review.review(root, root / "logs", start_time + 30, output_root=root / "reviews")
            report = json.loads(path.read_text())
            totals = report["data"]["totals"]["eth_sendRawTransaction"]
            self.assertEqual(totals["success_ge500ms"], 4)
            self.assertEqual(totals["success_500_to_1000ms"], 2)
            self.assertEqual(totals["failure"], 2)
            self.assertEqual(totals["failure_ge500ms"], 1)
            self.assertEqual(report["data"]["post_warmup"]["totals"]["eth_sendRawTransaction"]["failure"], 0)
            logs = report["archives"]
            self.assertTrue(logs["calibration"]["resolved"])
            self.assertEqual(logs["calibration"]["terminal_utc_offset_seconds"], 0)
            send = next(row for row in logs["counter_comparison"] if row["method"] == "eth_sendRawTransaction")
            self.assertTrue(send["failure_counts_match"])
            self.assertTrue(send["success_ge1s_counts_match"])
            self.assertTrue(send["failure_ge1s_counts_match"])
            self.assertEqual(len(logs["stage_records"]), 3)
            self.assertEqual(len(logs["failure_groups"]), 2)
            contexts = report["data"]["publisher_context"]
            self.assertTrue(any(sample["rolling_stage_quantiles"]["block_execution"]["p999_ms"] == 2000
                                for interval in contexts for sample in interval["samples"]))
            self.assertEqual((root / "rpc-counters/counters.jsonl").read_bytes(), original)
            self.assertNotIn("0x123456", console.getvalue())
            self.assertIn("PUBLISH_CONTEXT", console.getvalue())
            self.assertIn("SLOW_STAGE", console.getvalue())


if __name__ == "__main__":
    unittest.main()
