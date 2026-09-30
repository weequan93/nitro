"""Offline pinned fixture / first-message / resume regression checks."""
import contextlib
import copy
import importlib.util
import io
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

BASE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location('three_span', BASE/'../tools/replay-three-staker-span.py')
w = importlib.util.module_from_spec(spec)
spec.loader.exec_module(w)
r = w.r
AUDIT = json.loads((BASE/'../../evidence/reported-results/three-staker-span-20260928-090354.json').read_text())['report']


class NewSpanTests(unittest.TestCase):
    def test_pin_rejects_changed_audit(self):
        changed = copy.deepcopy(AUDIT)
        changed['checkpointMessage'] -= 1
        with self.assertRaises(ValueError):
            w.check_audit(changed)

    def test_first_message_and_resume_use_new_interval(self):
        w.check_audit(AUDIT)
        calls = []
        def state(n):
            if n == r.START-1:
                return r.A
            if n == r.END:
                return r.B
            return dict(BlockHash='0x'+format(n, '064x'), SendRoot=r.SEND,
                        Batch=321809, PosInBatch=n-r.START+1)
        def rpc(url, method, params):
            if method == 'eth_chainId':
                return hex(2886)
            if method == 'web3_clientVersion':
                return 'offline fixture'
            n = int(params[0], 16)
            if method == 'eth_getBlockByNumber':
                return dict(number=hex(n), hash=state(n)['BlockHash'],
                            parentHash=state(n-1)['BlockHash'], sendRoot=r.SEND)
            if method == 'arbdebug_validationInputsAt':
                return dict(Id=n, StartState=state(n-1), ExpectedEndState=state(n))
            self.assertEqual(method, 'arbdebug_validateMessageNumber')
            self.assertEqual(params[1:], [True, r.ROOT])
            calls.append(n)
            return dict(valid=True, globalstate=state(n), latency='1ms')
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            audit = root/'audit.json'
            audit.write_text(json.dumps(AUDIT))
            args = ['replay', '--audit', str(audit), '--out', str(root/'run'), '--max-messages', '1']
            for extra in ([], ['--resume']):
                with patch('sys.argv', args+extra), patch.object(r, 'rpc', rpc), contextlib.redirect_stdout(io.StringIO()):
                    self.assertEqual(r.main(), 0)
            result = json.loads((root/'run/summary.json').read_text())
            self.assertEqual(calls, [126683263, 126683264])
            self.assertEqual(result['totalMessages'], 10578)
            self.assertEqual(result['validatedMessagesTotal'], 2)
            self.assertFalse(result['parentConfirmedAToBProven'])
            self.assertFalse(result['executionReplayed'])
            self.assertFalse(result['readyForProduction'])
            manifest = json.loads((root/'run/manifest.json').read_text())
            self.assertEqual(manifest['startingState'], r.A)
            self.assertEqual(manifest['endingState'], r.B)

if __name__ == '__main__':
    unittest.main()
