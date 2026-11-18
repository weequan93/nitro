import contextlib
import importlib.util
import io
import json
from pathlib import Path
import tempfile
import threading
import time
from types import SimpleNamespace
import unittest
from unittest.mock import patch

SCRIPTS = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location('overnight', SCRIPTS / 'capture-rpc-overnight.py')
c = importlib.util.module_from_spec(spec)
spec.loader.exec_module(c)
IDENT = dict(id='node', image='image', started='2026-11-17T00:00:00Z', pid=42, start_ticks=100)


def sample(t, n=0, ident=None):
    return dict(time_utc=c.datetime.fromtimestamp(t, c.timezone.utc).isoformat(), monotonic_s=t,
        identity=dict(ident or IDENT), errors=[], metrics={
            **{f'rpc_diagnostics_{m}_{o}_{k}': n for m in c.METHODS for o in c.OUTCOMES for k in c.SUFFIXES},
            'rpc_diagnostics_details_dropped': n,
            'rpc_duration_eth_call_success_count': n * 10,
            'rpc_duration_eth_call_failure_count': 0})


class DeltaTests(unittest.TestCase):
    def test_valid_and_unavailable_intervals(self):
        old, new = sample(100), sample(105, 2)
        self.assertEqual(c.latency_delta(new, old, 5)['eth_getLogs:success']['counts']['ge500ms'], 2)
        for reason, edit in (
                ('counter_reset', lambda r: r['metrics'].update(rpc_diagnostics_eth_getLogs_success_completed=-1)),
                ('missing_counters', lambda r: r['metrics'].pop('rpc_diagnostics_eth_getLogs_success_ge1s')),
                ('inconsistent_threshold_deltas', lambda r: r['metrics'].update(rpc_diagnostics_eth_getLogs_success_ge5s=3)),
                ('sample_error', lambda r: r['errors'].append('fetch')),
                ('sample_gap', lambda r: r.update(monotonic_s=150)),
                ('identity_missing_or_changed', lambda r: r['identity'].update(started='new-start'))):
            with self.subTest(reason=reason):
                row = sample(105, 2)
                edit(row)
                self.assertEqual(c.latency_delta(row, old, 5)['eth_getLogs:success']['unavailable'], reason)
        old['errors'] = ['failed']
        self.assertIsNone(c.scalar_delta(new, old, 'rpc_duration_eth_call_success_count'))

    def test_restart_splits_instance_even_if_container_same(self):
        changed = dict(IDENT, started='new-start', start_ticks=101)
        self.assertNotEqual(c.instance_key(IDENT), c.instance_key(changed))


class CaptureTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = Path(self.tmp.name)
        self.args = SimpleNamespace(container='main', max_output_gib=1, min_free_gib=0,
            hours=24, metrics_interval=5, profiles=True, log_file=self.root / 'nitro.log')
        self.obj = c.Capture(self.args, self.root, SCRIPTS)

    def tearDown(self):
        self.obj.stop.set()
        self.tmp.cleanup()

    def test_profiles_have_independent_quotas_and_revalidate_pending(self):
        self.obj.state['profile_attempts:eth_sendRawTransaction'] = 16
        self.obj.trigger('eth_sendRawTransaction', {})
        self.assertIsNone(self.obj.pending)
        self.obj.trigger('eth_getLogs', {})
        self.assertEqual(self.obj.pending['method'], 'eth_getLogs')
        self.obj.last_profile['eth_getLogs'] = time.monotonic()
        self.obj.profile(self.obj.pending)
        self.assertFalse((self.root / 'profiles').exists())
        self.assertEqual(self.obj.state['profile_stale_or_ineligible_skips'], 1)

    def test_profile_baseline_and_incident_classification(self):
        class Child:
            returncode = 0
            def __init__(self, *args, **kw): kw['stdout'].write(b'fixture')
            def poll(self): return 0
        for n, classification in ((0, 'baseline_or_unavailable'), (1, 'incident_window')):
            with self.subTest(n=n):
                self.obj.last_profile.clear()
                self.obj.observe = unittest.mock.Mock(side_effect=[sample(100), sample(106, n)])
                with patch.object(c.subprocess, 'Popen', Child), contextlib.redirect_stdout(io.StringIO()):
                    self.obj.profile(dict(method='eth_getLogs', detected_monotonic_s=time.monotonic()))
                values = list(c.read_rows(self.root / 'profiles.jsonl'))
                self.assertEqual(values[-1]['classification'], classification)
                self.assertEqual(self.obj.store.reserved, 0)

    def test_profile_reservation_preserves_monitoring_space(self):
        store = c.Store(self.root, 150 * 1024**2, 0)
        with self.assertRaises(c.BudgetError): store.reserve_profile()
        store.emit('metrics.jsonl', {'sampling': 'continues'})
        self.assertTrue((self.root / 'metrics.jsonl').exists())
        with patch.object(c.shutil, 'disk_usage', return_value=SimpleNamespace(free=100)):
            store.minimum_free = 101
            with self.assertRaisesRegex(c.BudgetError, 'free_space_floor'): store.check()

    def raw(self):
        return (json.dumps(dict(t='2026-11-17T12:00:00Z', msg='RPC stage timings',
            method='eth_getLogs', outcome='success', trace='12', total_ms='600', consistency_ms='599',
            request_params_json=json.dumps([dict(address=['0x123'], topics=[['0x456'], None],
                fromBlock='0x1', toBlock='0x1', secret='omit')]),
            signed_transaction='omit', arbitrary_secret='omit')) + '\n').encode()

    def test_log_sanitization(self):
        self.obj.log_event(self.raw())
        row = next(c.read_rows(self.root / 'events.jsonl'))
        self.assertEqual(row['query']['topics'], [['0x456'], None])
        self.assertNotIn('omit', json.dumps(row))

    def test_log_keeps_queue_join_and_index_write_fields(self):
        self.obj.log_event(
            b"WARN [11-17|12:00:00.000] RPC stage timings method=eth_sendRawTransaction "
            b"outcome=success trace=12 total_ms=600 tx_publish_first_dequeue_ms=44 "
            b"tx_publish_last_seq_id=51\n")
        self.obj.log_event(
            b"WARN [11-17|12:00:00.000] Log index write timings write_seq=53 "
            b"write_started_unix_ns=1794916800000000123 wall_ms=1500 batch_commit_ms=850 unrelated=omit\n")
        tx, write = list(c.read_rows(self.root / "events.jsonl"))
        self.assertEqual(tx["tx_publish_last_seq_id"], 51)
        self.assertEqual(tx["tx_publish_first_dequeue_ms"], 44)
        self.assertEqual(write["kind"], "index_write")
        self.assertEqual(write["record"]["write_started_unix_ns"], 1794916800000000123)
        self.assertEqual(write["record"]["batch_commit_ms"], 850)
        self.assertNotIn("unrelated", write["record"])

    def wait_until(self, predicate):
        until = time.monotonic() + 3
        while not predicate():
            if time.monotonic() > until: self.fail('log follower did not advance')
            time.sleep(.02)

    def test_log_tail_oversize_malformed_rotation_and_truncate(self):
        path = self.args.log_file
        path.write_bytes(b'old data\n')
        worker = threading.Thread(target=self.obj.logs_loop)
        worker.start()
        try:
            time.sleep(.1)
            with path.open('ab') as f:
                f.write(b'x' * (1024**2 + 100) + b'\n')
                f.write(b'{"msg":"RPC stage timings","t":"invalid"}\n')
                f.write(self.raw())
            self.wait_until(lambda: self.obj.state['rpc_stage_records'] == 1)
            self.assertEqual(self.obj.state['oversized_log_lines_dropped'], 1)
            self.assertGreaterEqual(self.obj.state['log_parse_errors'], 1)
            path.rename(self.root / 'old.log')
            path.write_bytes(self.raw())
            self.wait_until(lambda: self.obj.state['rpc_stage_records'] == 2)
            path.write_bytes(b'')
            self.wait_until(lambda: self.obj.state['log_copytruncate_seen'] == 1)
            path.write_bytes(self.raw())
            self.wait_until(lambda: self.obj.state['rpc_stage_records'] == 3)
        finally:
            self.obj.stop.set()
            worker.join(timeout=3)
        self.assertFalse(worker.is_alive())

    def test_process_disappearance_is_a_sample_not_collector_failure(self):
        self.obj.current = IDENT
        self.obj.base.process_snapshot = unittest.mock.Mock(side_effect=IndexError('exited'))
        real_emit = self.obj.store.emit
        def emit(path, row):
            real_emit(path, row)
            self.obj.stop.set()
        self.obj.store.emit = emit
        self.obj.resources_loop()
        row = next(c.read_rows(self.root / 'resources.jsonl'))
        self.assertFalse(row['process']['available'])
        self.assertEqual(self.obj.state['process_snapshot_errors'], 1)


class ReportTests(unittest.TestCase):
    def test_report_restarts_coverage_and_eth_call(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            now = time.time()
            rows = [sample(now-20), sample(now-15, 2),
                sample(now-10, 0, dict(IDENT, started='restart', start_ticks=200)),
                sample(now-5, 1, dict(IDENT, started='restart', start_ticks=200))]
            for index, row in enumerate(rows):
                row['latency_deltas'] = c.latency_delta(row, rows[index-1] if index else None, 5)
            (root/'metrics.jsonl').write_text(''.join(json.dumps(r)+'\n' for r in rows))
            output = io.StringIO()
            with contextlib.redirect_stdout(output): c.report(root, 1)
            text = output.getvalue()
            self.assertEqual(text.count('INSTANCE '), 2)
            self.assertIn('ETH_CALL success completed=20', text)
            self.assertIn('ETH_CALL success completed=10', text)
            self.assertIn('HOT_INTERVAL', text)
            self.assertIn('identity_missing_or_changed', text)
            with contextlib.redirect_stdout(output := io.StringIO()): c.report(root, .0001)
            self.assertIn('NO_VALID_INTERVALS', output.getvalue())


if __name__ == '__main__':
    unittest.main()
