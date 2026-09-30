"""Offline end-to-end resume/failure checks with deterministic RPC fixtures."""
import contextlib
import importlib.util
import io
import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('replay', (Path(__file__).resolve().parent / '../tools/replay-retained-message-span.py'))
r = importlib.util.module_from_spec(spec)
spec.loader.exec_module(r)


def gs(n):
    return dict(BlockHash='0x'+format(n, '064x'), SendRoot=r.SEND, Batch=100, PosInBatch=n)


class ReplayTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.base = Path(self.tmp.name)
        self.out = self.base/'run'
        self.audit = self.base/'audit.json'
        self.audit.write_text(json.dumps(dict(status='message_span_audited', parentBlock='0x1e58e750',
            confirmedNode=42883, localMessageAtConfirmedPosition=9, localBlockAtConfirmedPosition=9,
            checkpointMessage=12, checkpointBlock=12, localMessageCountAtConfirmedPosition=10,
            checkpointMessageCount=13, positionalMessageSpan=3, localStateAtConfirmedPosition=gs(9),
            checkpoint=gs(12), confirmedGlobalStateMatchesLocal=False)))
        self.calls = []
        self.failure = None
        for name, value in [('START', 10), ('END', 12), ('A', gs(9)), ('B', gs(12))]:
            p = patch.object(r, name, value)
            p.start()
            self.addCleanup(p.stop)

    def rpc(self, url, method, params):
        if method == 'eth_chainId':
            return hex(2886)
        if method == 'web3_clientVersion':
            return 'test-fixture'
        msg = int(params[0], 16)
        if method == 'eth_getBlockByNumber':
            return dict(number=hex(msg), hash=gs(msg)['BlockHash'], parentHash=gs(msg-1)['BlockHash'], sendRoot=r.SEND)
        if method == 'arbdebug_validationInputsAt':
            return dict(Id=msg, StartState=gs(msg-1), ExpectedEndState=gs(msg))
        if method == 'arbdebug_validateMessageNumber':
            self.assertEqual(params[1:], [True, r.ROOT])
            self.calls.append(msg)
            return dict(valid=msg != self.failure, globalstate=gs(msg), latency='1ms')
        raise AssertionError(method)

    def run_script(self, *extra):
        argv = ['replay', '--audit', str(self.audit), '--out', str(self.out), *extra]
        with patch('sys.argv', argv), patch.object(r, 'rpc', self.rpc), contextlib.redirect_stdout(io.StringIO()):
            code = r.main()
        return code, json.loads((self.out/'summary.json').read_text())

    def test_resume_skips_only_saved_valid_records(self):
        code, summary = self.run_script('--max-messages', '1')
        self.assertEqual((code, summary['status'], summary['validatedMessagesTotal']), (0, 'batch_complete', 1))
        self.assertFalse(summary['executionReplayed'])
        code, summary = self.run_script('--resume')
        self.assertEqual((code, summary['status'], self.calls), (0, 'retained_span_replay_passed', [10, 11, 12]))
        self.assertEqual(summary['validatedMessagesThisRun'], 2)
        self.assertFalse(summary['parentConfirmedAToBProven'])

    def test_failed_validation_is_not_committed(self):
        self.failure = 11
        code, summary = self.run_script()
        self.assertEqual((code, summary['status'], summary['nextMessage']), (1, 'stopped', 11))
        self.assertFalse((self.out/'messages/11.json').exists())
        self.failure = None
        code, summary = self.run_script('--resume')
        self.assertEqual(self.calls, [10, 11, 11, 12])
        self.assertEqual(summary['status'], 'retained_span_replay_passed')

    def test_gap_rejected_before_more_replay(self):
        self.run_script()
        (self.out/'messages/11.json').unlink()
        code, summary = self.run_script('--resume')
        self.assertEqual((code, summary['status']), (1, 'stopped'))
        self.assertEqual(self.calls, [10, 11, 12])

    def test_wrong_root_in_record_rejected(self):
        self.run_script('--max-messages', '1')
        path = self.out/'messages/10.json'
        record = json.loads(path.read_text())
        record['wasmModuleRoot'] = '0x'+'00'*32
        path.write_text(json.dumps(record))
        code, summary = self.run_script('--resume')
        self.assertEqual((code, summary['status']), (1, 'stopped'))
        self.assertEqual(self.calls, [10])

    def test_discontinuous_input_rejected(self):
        original = self.rpc
        def broken(url, method, params):
            result = original(url, method, params)
            if method == 'arbdebug_validationInputsAt':
                result['StartState'] = gs(8)
            return result
        with patch.object(self, 'rpc', broken):
            code, summary = self.run_script()
        self.assertEqual((code, summary['validatedMessagesTotal'], self.calls), (1, 0, []))

    def test_write_rpc_disallowed(self):
        with self.assertRaises(ValueError):
            r.rpc('http://unused.invalid', 'eth_sendTransaction', [])


if __name__ == '__main__':
    unittest.main()
