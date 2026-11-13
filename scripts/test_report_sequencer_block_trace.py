"""Offline trace accounting tests; optional real fixture via BLOCKDIAG_TEST_TRACE."""

import gzip
import importlib.util
import io
import json
import os
from pathlib import Path
import tempfile
import unittest

spec = importlib.util.spec_from_file_location("report", Path(__file__).with_name("report-sequencer-block-trace.py"))
report = importlib.util.module_from_spec(spec)
spec.loader.exec_module(report)
spec = importlib.util.spec_from_file_location("watcher", Path(__file__).with_name("watch-slow-sequencer-blocks.py"))
watcher = importlib.util.module_from_spec(spec)
spec.loader.exec_module(watcher)


def event(kind, ms, fields="", g=7, stack=""):
    return f"M=1 P=0 G={g} {kind} Time={int(ms * 1e6)} {fields}\n{stack}"


def fixture():
    return "".join([
        event("TaskBegin", 100, 'ID=1 Type="sequencer/block"'),
        event("Log", 101, 'Task=1 Category="sequencer_id" Message="42"'),
        event("RegionBegin", 102, 'Task=1 Type="sequencer/apply_transaction"'),
        event("RegionBegin", 105, 'Task=1 Type="sequencer/state_reader/storage"'),
        event("StateTransition", 110, 'GoID=7 Running->Waiting Reason="sync"',
              stack="TransitionStack=\n\tsync.(*RWMutex).RLock @ 0x123\n\tstate.Reader @ 0x456\n"),
        # Unblock event's header G is the unlocking goroutine, not the target.
        event("StateTransition", 130, 'GoID=7 Waiting->Runnable Reason=""', g=9),
        event("StateTransition", 140, 'GoID=7 Runnable->Running Reason=""', g=-1),
        event("RegionEnd", 145, 'Task=1 Type="sequencer/state_reader/storage"'),
        event("StateTransition", 150, 'Resource=Goroutine(7) Running->Syscall Reason=""'),
        event("StateTransition", 160, 'Resource=Goroutine(7) Syscall->Running Reason=""'),
        event("StateTransition", 170, 'GoID=7 Running->Waiting Reason="select"'),
        event("StateTransition", 180, 'GoID=7 Waiting->Runnable Reason=""', g=9),
        event("StateTransition", 185, 'GoID=7 Runnable->Running Reason=""', g=-1),
        event("RegionEnd", 195, 'Task=1 Type="sequencer/apply_transaction"'),
        event("TaskEnd", 200, 'ID=1 Type="sequencer/block"'),
    ])


def vint(value):
    out = bytearray()
    while value >= 128:
        out.append((value & 127) | 128)
        value >>= 7
    out.append(value)
    return bytes(out)


def field(number, value):
    if isinstance(value, int):
        return vint(number << 3) + vint(value)
    return vint(number << 3 | 2) + vint(len(value)) + value


class TraceTests(unittest.TestCase):
    def test_disjoint_wall_accounting_and_nested_reader_overlap(self):
        value = report.analyze_trace(io.StringIO(fixture()), 42)
        expected = {"go_running_wall": 45, "runnable_delay": 15, "mutex_wait": 20,
                    "channel_select_wait": 10, "syscall_or_cgo_wall": 10,
                    "other_wait": 0, "unknown": 0}
        self.assertEqual(value["primary_goroutine_wall_ms"], expected)
        reader = value["state_reader_union_wall_ms"]
        self.assertEqual((reader["go_running_wall"], reader["mutex_wait"], reader["runnable_delay"]), (10, 20, 10))
        self.assertEqual(sum(expected.values()), value["task_wall_ms"])
        self.assertEqual(value["consistency_errors"], [])
        self.assertEqual(value["waiting_stacks"][0]["stack"][0], "sync.(*RWMutex).RLock")

    def test_evicted_tasks_missing_regions_and_ambiguous_ids_are_unavailable(self):
        for data in (fixture().split("\n", 1)[1], fixture().rsplit("M=", 1)[0],
                     fixture().replace('RegionEnd Time=145000000', 'Metric Time=145000000'),
                     fixture() + event("Log", 205, 'Task=2 Category="sequencer_id" Message="42"')):
            with self.assertRaises(ValueError):
                report.analyze_trace(io.StringIO(data), 42)

    def test_no_transition_means_running_only_for_complete_tasks(self):
        data = "".join(e for e in fixture().splitlines(True) if "StateTransition" not in e
                       and not e.startswith(("TransitionStack", "\t")))
        value = report.analyze_trace(io.StringIO(data), 42)
        self.assertEqual(value["primary_goroutine_wall_ms"]["go_running_wall"], 100)

    def test_cpu_totals_select_labels_and_never_use_unfiltered_total(self):
        strings = ["", "cpu", "nanoseconds", "seq_id", "42", "other", "compute", "samples", "count"]
        profile = b"".join(field(6, s.encode()) for s in strings)
        profile += field(1, field(1, 7) + field(2, 8)) + field(1, field(1, 1) + field(2, 2))
        profile += field(5, field(1, 10) + field(2, 6))
        profile += field(4, field(1, 20) + field(4, field(1, 10)))
        for label, ns in [(4, 30_000_000), (5, 900_000_000)]:
            sample = field(1, vint(20)) + field(2, vint(3) + vint(ns))
            sample += field(3, field(1, 3) + field(2, label))
            profile += field(2, sample)
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "cpu.pprof"
            path.write_bytes(gzip.compress(profile))
            value = report.cpu_profile(path, 42)
            self.assertEqual(value["sampled_cpu_ms"], 30)
            self.assertEqual(value["top_flat"], [{"function": "compute", "sampled_ms": 30}])
            self.assertIsNone(report.cpu_profile(path, 999)["sampled_cpu_ms"])
            path.write_bytes(gzip.compress(b"\x12\xff"))
            with self.assertRaises(ValueError):
                report.cpu_profile(path, 42)

    def test_text_and_json_diagnostic_logs_and_malformed_records(self):
        record = {"schema": 1, "seq_id": 42, "total_ms": 550.5, "started_utc": "2026-01-01T00:00:00Z",
                  "stages": {}, "state_readers": {}}
        encoded = json.dumps(record)
        text = "2026-01-01T00:00:01Z WARN Sequencer block diagnostics seq=42 timings=" + json.dumps(encoded)
        structured = "2026-01-01T00:00:01Z " + json.dumps({"msg": "Sequencer block diagnostics", "timings": encoded})
        self.assertEqual(watcher.parse_block_diagnostics(text), record)
        self.assertEqual(watcher.parse_block_diagnostics(structured), record)
        self.assertIsNone(watcher.parse_block_diagnostics(text[:-10]))
        self.assertIsNone(watcher.parse_block_diagnostics("Sequencer block diagnostics timings=\"null\""))

    @unittest.skipUnless(os.getenv("BLOCKDIAG_TEST_TRACE"), "real runtime fixture not requested")
    def test_real_runtime_mutex_fixture_and_labeled_cpu(self):
        path = Path(os.environ["BLOCKDIAG_TEST_TRACE"])
        record = json.loads(Path(str(path) + ".json").read_text())
        with tempfile.TemporaryFile(mode="w+", encoding="utf-8") as dump:
            report.dump_trace(os.getenv("BLOCKDIAG_TEST_GO", "go"), path, dump)
            value = report.analyze_trace(dump, record["seq_id"])
        wall = value["primary_goroutine_wall_ms"]
        self.assertGreater(wall["mutex_wait"], 10)
        self.assertGreater(wall["go_running_wall"], 50)
        self.assertAlmostEqual(sum(wall.values()), value["task_wall_ms"], places=4)
        self.assertEqual(value["consistency_errors"], [])
        self.assertGreater(value["state_reader_union_wall_ms"]["mutex_wait"], 10)
        cpu = report.cpu_profile(path.parent / "cpu.pprof", record["seq_id"])
        self.assertGreater(cpu["sampled_cpu_ms"], 10)


if __name__ == "__main__":
    unittest.main()
