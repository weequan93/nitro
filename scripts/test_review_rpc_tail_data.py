"""Synthetic counter and parser fixtures, including deliberately fake URL credentials."""

import contextlib
from copy import deepcopy
import importlib.util
import io
import json
from pathlib import Path
import shutil
import tempfile
import unittest
from unittest.mock import patch


HERE = Path(__file__).parent
SPEC = importlib.util.spec_from_file_location("deep_read", HERE / "review-rpc-tail-data.py")
m = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(m)
START = "2026-01-01T00:00:00+00:00"
IDENTITY = {"id": "saved-container", "pid": 123, "start_ticks": 100, "image": "sha256:saved"}


def sample(seconds, *, success=None, failure=0):
    d = {k: 0 for k in m.BASE_KEYS + (m.DROP,)}
    sub = {f"rpc_diagnostics_{method}_{outcome}_{s}": 0
           for method in m.METHODS for outcome in m.OUTCOMES for s in m.SUFFIXES[1:7]}
    if success is None:
        success = [10, 9, 8, 7, 6, 5, 4, 2, 1, 0]
    for suffix, value in zip(m.SUFFIXES, success):
        key = f"rpc_diagnostics_eth_sendRawTransaction_success_{suffix}"
        (d if key in d else sub)[key] = value
    d["rpc_diagnostics_eth_sendRawTransaction_failure_completed"] = failure
    return {"time_utc": m.iso_utc(m.stamp(START) + seconds), "interval_s": 15,
            "identity": deepcopy(IDENTITY), "errors": [], "delta_unavailable": None,
            "subsecond_delta_unavailable": None, "deltas": d, "subsecond_deltas": sub}


def baseline():
    r = sample(0)
    r["interval_s"] = None
    r["delta_unavailable"] = "first_sample"
    return r


class CounterTests(unittest.TestCase):
    def test_every_ge1s_interval_is_retained_beyond_burst_limits(self):
        rows = [baseline()] + [sample(i * 15) for i in range(1, 6)]
        intervals, _ = m.scan_counters(rows, m.stamp(rows[-1]["time_utc"]))
        hot = m.select_hot(intervals)
        self.assertEqual(len(hot), 5)
        self.assertIn("eth_sendRawTransaction:any_ge1s", hot[-1]["selected_for"])

    def test_failure_tails_are_independent_of_success_bucket_coverage(self):
        r = sample(15, failure=2)
        for suffix, value in zip(m.SUFFIXES, [2, 2, 1, 1, 1, 1, 1, 1, 0, 0]):
            key = f"rpc_diagnostics_eth_sendRawTransaction_failure_{suffix}"
            (r["deltas"] if key in r["deltas"] else r["subsecond_deltas"])[key] = value
        r["subsecond_deltas"]["rpc_diagnostics_eth_sendRawTransaction_success_ge500ms"] = 999
        intervals, _ = m.scan_counters([baseline(), r], m.stamp(r["time_utc"]))
        totals = m.totals(intervals, m.METHODS[0])
        self.assertIsNone(totals["success_ge500ms"])
        self.assertEqual(totals["failure_ge500ms"], 1)
        self.assertEqual(totals["failure_ge1s"], 1)
        self.assertEqual(totals["failure_500_to_1000ms"], 0)
        self.assertEqual(totals["failure_ge500_valid_seconds"], 15)

    def test_frozen_cutoff_and_nested_thresholds(self):
        rows = [baseline(), sample(15, failure=2), sample(30, failure=99)]
        intervals, skipped = m.scan_counters(rows, m.stamp(rows[1]["time_utc"]))
        t = m.totals(intervals, m.METHODS[0])
        self.assertEqual(t["failure"], 2)
        self.assertEqual(t["success"], 10)
        self.assertEqual(t["success_ge500ms"], 4)
        self.assertEqual(t["success_500_to_1000ms"], 2)
        self.assertEqual(t["success_ge1s"], 2)
        self.assertEqual(t["ge500_valid_seconds"], 15)
        self.assertEqual(skipped, {"first_sample": 1})

    def test_inconsistent_bucket_does_not_become_zero_or_drop_failure(self):
        r = sample(15, failure=3)
        r["subsecond_deltas"]["rpc_diagnostics_eth_sendRawTransaction_success_ge500ms"] = 20
        intervals, _ = m.scan_counters([baseline(), r], m.stamp(r["time_utc"]))
        t = m.totals(intervals, m.METHODS[0])
        self.assertEqual(t["failure"], 3)
        self.assertIsNone(t["success_ge500ms"])
        self.assertEqual(t["ge500_valid_seconds"], 0)
        self.assertEqual(t["ge500_unavailable_intervals"], 1)

    def test_zero_tail_is_available(self):
        r = sample(15, success=[1] + [0] * 9)
        intervals, _ = m.scan_counters([baseline(), r], m.stamp(r["time_utc"]))
        self.assertEqual(m.totals(intervals, m.METHODS[0])["success_ge500ms"], 0)

    def test_identity_change_and_gap_are_excluded(self):
        changed = sample(15)
        changed["identity"]["pid"] = 999
        long_gap = sample(60)
        long_gap["interval_s"] = 45
        intervals, skipped = m.scan_counters([baseline(), changed, long_gap], m.stamp(long_gap["time_utc"]))
        self.assertFalse(intervals)
        self.assertEqual(skipped["adjacent_identity_or_time_unavailable"], 1)
        self.assertEqual(skipped["errors_or_invalid_interval"], 1)

    def test_reset_and_negative_counter_excluded(self):
        reset = sample(15)
        reset["delta_unavailable"] = "counter_reset"
        negative = sample(30)
        negative["deltas"][m.DROP] = -1
        intervals, skipped = m.scan_counters([baseline(), reset, negative], m.stamp(negative["time_utc"]))
        self.assertFalse(intervals)
        self.assertEqual(skipped["counter_reset"], 1)
        self.assertEqual(skipped["missing_or_invalid_delta"], 1)

    def test_multiple_reasons_share_a_single_hot_interval(self):
        r = sample(15, failure=4)
        intervals, _ = m.scan_counters([baseline(), r], m.stamp(r["time_utc"]))
        hot = m.select_hot(intervals)
        self.assertEqual(len(hot), 1)
        self.assertEqual(len(hot[0]["selected_for"]), 4)


class ParserTests(unittest.TestCase):
    def test_logfmt_error_and_duration(self):
        line = ('2026-11-13T00:00:00.123456789Z WARN Served eth_sendRawTransaction '
                'reqid=123 duration=37.5ms err="nonce too high: address 0x123abc, tx: 42 state: 41"\n')
        t, msg, f = m.parse_log(line)
        self.assertIsNotNone(t)
        self.assertEqual(msg, "Served eth_sendRawTransaction")
        self.assertEqual(m.duration_ms(f["duration"]), 37.5)
        self.assertEqual(m.normalize_error(f["err"]), "nonce too high: address <hex>, tx: <n> state: <n>")

    def test_json_stage_and_comma_grouped_numeric(self):
        payload = {"msg": "RPC stage timings", "method": "eth_getLogs", "trace": 123,
                   "outcome": "success", "total_ms": 1123.5, "callback_ms": 1123.0}
        _, msg, f = m.parse_log("2026-11-13T00:00:00Z " + json.dumps(payload))
        self.assertEqual(msg, "RPC stage timings")
        self.assertEqual(f["total_ms"], 1123.5)
        self.assertEqual(m.numeric("1,234,567"), 1234567)
        self.assertIsNone(m.numeric("NaN"))

    def test_error_redaction_and_duration_units(self):
        self.assertEqual(m.duration_ms("1m2.003s"), 62003)
        self.assertAlmostEqual(m.duration_ms("350µs"), .35)
        self.assertIsNone(m.duration_ms("garbage"))
        error = m.normalize_error("request https://example-user:EXAMPLE_TEST_PASSWORD@example.invalid/private failed for 0xabcdef nonce 1234")
        self.assertNotIn("EXAMPLE_TEST_PASSWORD", error)
        self.assertNotIn("abcdef", error)
        self.assertNotIn("1234", error)

    def test_non_rpc_and_invalid_json_message(self):
        self.assertIsNone(m.parse_log("not timestamped"))
        self.assertIsNone(m.parse_log('2026-11-13T00:00:00Z {"msg":123}'))

    def test_retained_log_scope_error_groups_and_stage_details(self):
        raw = '\n'.join([
            '2026-11-12T23:59:59Z WARN Served eth_sendRawTransaction duration=1ms err="before cutoff"',
            '2026-11-13T00:00:00Z INFO background task=example',
            '2026-11-13T00:00:01Z WARN Served eth_sendRawTransaction duration=37ms err="nonce too high: address 0x123 tx: 42"',
            '2026-11-13T00:00:02Z WARN Served eth_sendRawTransaction duration=12ms err="nonce too high: address 0x456 tx: 43"',
            '2026-11-13T00:00:03Z WARN RPC stage timings method=eth_getLogs outcome=success trace=1,234 total_ms=1123 callback_ms=1122 sync_response_wait_ms=1100',
            '2026-11-13T00:01:01Z WARN Served eth_sendRawTransaction duration=1ms err="after cutoff"',
        ]) + '\n'
        class FakeProcess:
            stdout = io.StringIO(raw)
            returncode = 0
            def __enter__(self):
                return self
            def __exit__(self, *args):
                return False
            def poll(self):
                return self.returncode
            def wait(self, timeout=None):
                return self.returncode
        with patch.object(m.subprocess, "Popen", return_value=FakeProcess()) as popen:
            result = m.collect_logs("saved-container", m.stamp("2026-11-13T00:00:00Z"),
                                    m.stamp("2026-11-13T00:01:00Z"))
        self.assertEqual(popen.call_args.args[0][-1], "saved-container")
        self.assertEqual(result["failure_log_groups"][0]["records"], 2)
        self.assertEqual(result["failure_log_groups"][0]["duration_ms"]["max"], 37)
        self.assertEqual(result["slow_stage_examples"][0]["trace"], 1234)
        self.assertEqual(result["slow_stage_examples"][0]["sync_response_wait_ms"], 1100)
        self.assertEqual(result["first_retained_log_sgt"], "2026-11-13T08:00:00+08:00")


class IntegrationTests(unittest.TestCase):
    def test_saved_data_to_hot_context_and_render(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            (root / "tools").mkdir()
            shutil.copyfile(HERE / "report-sequencer-context.py",
                            root / "tools/report-sequencer-context.py")
            def write(name, rows):
                path = root / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text("".join(json.dumps(r) + "\n" for r in rows))
            write("rpc-counters/counters.jsonl", [baseline(), sample(15, failure=2), sample(30, failure=99)])
            resource = []
            for second in range(16):
                resource.append({"kind": "resources", "time_utc": m.iso_utc(m.stamp(START) + second),
                    "interval_s": 1, "cpu_ticks": [second * 20, 0, second * 5, second * 40, second * 35, 0, 0, 0],
                    "meminfo_bytes": {"MemAvailable": 25 * 1024**3},
                    "processes": [{"role": "nitro", "available": True, "pid": 123, "start_ticks": 100,
                                   "rates": {"cpu_cores": 5}, "rss_bytes": 6 * 1024**3}],
                    "devices": {"vdb": {"read_await_ms": .3, "average_queue": 2}}})
            write("resources/segment-0001/resources.jsonl", resource)
            write("cache-io/segment-0001/cache.jsonl", [
                {"time_utc": m.iso_utc(m.stamp(START) + second), "identity": IDENTITY, "errors": [],
                 "metrics": {"l2chaindata_compact_debt": 100 * 1024**3,
                             "l2chaindata_compact_inprogress": 1,
                             "l2chaindata_compact_writedelay_counter": 0}}
                for second in (0, 15, 30)])
            write("slow-blocks/events.jsonl", [{"kind": "rpc_slow", "log_time_utc": m.iso_utc(m.stamp(START) + 14),
                "method": m.METHODS[0], "outcome": "success", "trace": "123", "total_ms": 1123, "callback_ms": 1122}])
            report = m.build_report(root, m.stamp(START) + 15, read_logs=False)
            self.assertEqual(report["totals"][m.METHODS[0]]["failure"], 2)
            hot = report["hot_intervals"][0]
            self.assertEqual(hot["resource_rows"], 15)
            self.assertEqual(hot["resources"]["host.cpu_iowait_percent"]["mean"], 35)
            self.assertEqual(hot["cache_context"]["debt_GiB"]["first"], 100)
            self.assertEqual(report["saved_slow_events"][m.METHODS[0]]["records"], 1)
            out = io.StringIO()
            with contextlib.redirect_stdout(out):
                m.print_report(report)
            self.assertIn("HOT_INTERVAL", out.getvalue())
            json.dumps(report, allow_nan=False)


if __name__ == "__main__":
    unittest.main()
