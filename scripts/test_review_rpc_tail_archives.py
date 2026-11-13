import gzip
import contextlib
import importlib.util
import io
import json
from pathlib import Path
import shutil
import sys
import tempfile
import unittest
from unittest.mock import patch


HERE = Path(__file__).parent
SPEC = importlib.util.spec_from_file_location("archive_reader", HERE / "review-rpc-tail-archives.py")
m = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(m)
SINCE, UNTIL = m.stamp("2026-11-12T15:00:00Z"), m.stamp("2026-11-12T16:00:00Z")


def record(line, file_index=0):
    raw = (line + "\n").encode()
    token, message, fields = m.parse_line(raw)
    return {"token": token, "message": message, "fields": fields, "file_index": file_index,
            "fingerprint": m.hashlib.sha256(raw).hexdigest()}


def stage(clock="15:00:10.000", trace=1):
    return f"WARN [11-12|{clock}] RPC stage timings method=eth_getLogs outcome=success trace={trace} total_ms=1234.567 callback_ms=1234.500 sync_initial_ms=1000.0"


class TimeTests(unittest.TestCase):
    def test_plaintext_utc_calibration(self):
        records = [record(stage()), record(stage("15:00:20.000", 2))]
        anchors = {("eth_getLogs", 1): SINCE + 10.001, ("eth_getLogs", 2): SINCE + 20.001}
        offset, info = m.infer_offset(records, anchors)
        self.assertEqual(offset, 0)
        self.assertTrue(info["resolved"])
        self.assertEqual(info["anchor_votes"], {"0": 2})

    def test_plaintext_sgt_calibration(self):
        records = [record(stage("23:00:10.000")), record(stage("23:00:20.000", 2))]
        anchors = {("eth_getLogs", 1): SINCE + 10, ("eth_getLogs", 2): SINCE + 20}
        offset, info = m.infer_offset(records, anchors)
        self.assertEqual(offset, 28800)
        self.assertTrue(info["resolved"])
        self.assertEqual(m.timestamp(records[0]["token"], offset, SINCE), SINCE + 10)

    def test_one_anchor_is_insufficient_and_unresolved_times_excluded(self):
        records = [record(stage())]
        offset, info = m.infer_offset(records, {("eth_getLogs", 1): SINCE + 10})
        self.assertIsNone(offset)
        self.assertFalse(info["resolved"])
        failures, stages, notes = m.analyze(records, offset, SINCE, UNTIL)
        self.assertFalse(failures or stages)
        self.assertEqual(notes["unresolved_time_rpc_records"], 1)

    def test_json_timestamp_requires_no_timezone_guess(self):
        payload = {"t": "2026-11-12T23:00:10.000+08:00", "msg": "RPC stage timings",
                   "method": "eth_getLogs", "outcome": "success", "trace": 1, "total_ms": 1200}
        records = [record(json.dumps(payload))]
        offset, info = m.infer_offset(records, {})
        self.assertTrue(info["resolved"])
        _, stages, _ = m.analyze(records, offset, SINCE, UNTIL)
        self.assertEqual(stages[0]["time_sgt"], "2026-11-12T23:00:10+08:00")

    def test_year_boundary_and_leap_day(self):
        ref = m.stamp("2027-01-01T00:00:00Z")
        self.assertEqual(m.local_naive("12-31|23:59:59.000", ref), ref - 1)
        ref = m.stamp("2024-02-29T12:00:00Z")
        self.assertEqual(m.local_naive("02-29|12:00:00.000", ref), ref)


class RecordTests(unittest.TestCase):
    def test_logfmt_errors_redaction_and_nonce_grouping(self):
        line = 'WARN [11-12|15:10:00.000] Served eth_sendRawTransaction reqid=1 duration=200µs err="nonce too low: address 0x123456, tx: 9 state: 11"'
        records = [record(line), record(line)]
        failures, stages, _ = m.analyze(records, 0, SINCE, UNTIL)
        groups, senders = m.summarize(failures, stages)
        self.assertEqual(groups[0]["records"], 2)
        self.assertNotIn("123456", json.dumps(failures))
        self.assertEqual(senders[0]["records"], 2)
        self.assertEqual(senders[0]["distinct_failed_nonces"], 1)
        self.assertEqual(senders[0]["nonce_gap_min"], 2)
        self.assertAlmostEqual(groups[0]["duration_ms"]["max"], .2)

    def test_cross_file_duplicates_keep_within_file_multiplicity(self):
        line = 'WARN [11-12|15:10:00.000] Served eth_sendRawTransaction reqid=1 duration=1ms err="known transaction"'
        records = [record(line), record(line), record(line, 1), record(line, 1)]
        failures, _, notes = m.analyze(records, 0, SINCE, UNTIL)
        self.assertEqual(len(failures), 2)
        self.assertEqual(notes["duplicate_records_across_files"], 2)

    def test_stage_trace_dedup_and_fixed_cutoff(self):
        records = [record(stage()), record(stage("15:00:10.001"), 1), record(stage("16:00:01.000", 2))]
        _, stages, notes = m.analyze(records, 0, SINCE, UNTIL)
        self.assertEqual(len(stages), 1)
        self.assertEqual(stages[0]["sync_initial_ms"], 1000)
        self.assertEqual(notes["duplicate_stage_traces"], 1)

    def test_unrelated_timestamped_line_needs_no_payload_parse(self):
        token, message, fields = m.parse_line(b'INFO [11-12|15:00:00.123] Background task=running\n')
        self.assertEqual(token, ("terminal", "11-12|15:00:00.123"))
        self.assertIsNone(message)
        self.assertEqual(fields, {})


class SnapshotTests(unittest.TestCase):
    def test_snapshot_scope_gzip_plaintext_and_partial_tail(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            logs, output = root / "logs", root / "output"
            logs.mkdir()
            output.mkdir()
            with gzip.open(logs / 'nitro-2026-11-12T16-00-00.000.log.gz', 'wb') as stream:
                stream.write((stage() + '\n').encode())
            (logs / 'nitro.log').write_text(stage('15:00:20.000', 2) + '\npartial tail')
            (logs / 'nodekey').write_text('never read this')
            (logs / 'access.log').write_text('not part of the scope')
            snapshots, manifest = m.snapshot_logs(logs, output)
            self.assertEqual({p.name for p in snapshots.iterdir()},
                             {'nitro-2026-11-12T16-00-00.000.log.gz', 'nitro.log'})
            records, notes, _ = m.read_records(snapshots, manifest)
            self.assertEqual(len(records), 2)
            self.assertEqual(notes['partial_lines_skipped'], 1)
            anchors = {('eth_getLogs', 1): SINCE + 10, ('eth_getLogs', 2): SINCE + 20}
            offset, info = m.infer_offset(records, anchors)
            self.assertTrue(info['resolved'])
            _, stages, _ = m.analyze(records, offset, SINCE, UNTIL)
            self.assertEqual(len(stages), 2)

    def test_corrupt_gzip_is_reported(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            path = root / 'nitro-bad.log.gz'
            path.write_bytes(b'invalid gzip')
            manifest = [{'name': path.name}]
            records, _, _ = m.read_records(root, manifest)
            self.assertFalse(records)
            self.assertEqual(manifest[0]['scan_error'], 'BadGzipFile')

    def test_complete_command_matches_counter_report(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            suite, logs = root / 'suite', root / 'logs'
            (suite / 'tools').mkdir(parents=True)
            (suite / 'slow-blocks').mkdir()
            logs.mkdir()
            shutil.copyfile(HERE / 'report-sequencer-context.py',
                            suite / 'tools/report-sequencer-context.py')
            events = [{'kind': 'rpc_slow', 'method': 'eth_getLogs', 'trace': str(i),
                       'log_time_utc': f'2026-11-12T15:00:{i * 10:02d}.001Z'} for i in (1, 2)]
            (suite / 'slow-blocks/events.jsonl').write_text(''.join(json.dumps(e) + '\n' for e in events))
            previous = {'window': {'from_sgt': m.sgt(SINCE), 'to_sgt': m.sgt(UNTIL)},
                        'totals': {'eth_sendRawTransaction': {'failure': 1, 'success_ge1s': 0},
                                   'eth_getLogs': {'failure': 0, 'success_ge1s': 2}}}
            (suite / 'previous.json').write_text(json.dumps(previous))
            (logs / 'nitro.log').write_text(stage() + '\n' + stage('15:00:20.000', 2) + '\n' +
                'WARN [11-12|15:10:00.000] Served eth_sendRawTransaction duration=1ms err="nonce too low: address 0x123, tx: 1 state: 2"\n')
            args = ['review-rpc-tail-archives.py', '--suite-dir', str(suite), '--log-dir', str(logs),
                    '--previous-report', str(suite / 'previous.json')]
            stream = io.StringIO()
            with patch.object(sys, 'argv', args), contextlib.redirect_stdout(stream):
                m.main()
            output = json.loads(next(suite.glob('rpc-archive-review-*/report.json')).read_text())
            self.assertTrue(output['calibration']['resolved'])
            self.assertTrue(all(r['failure_counts_match'] and r['ge1s_counts_match']
                                for r in output['counter_comparison']))
            self.assertIn('ARCHIVE_STAGE', stream.getvalue())


if __name__ == '__main__':
    unittest.main()
