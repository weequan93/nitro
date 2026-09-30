import contextlib
import importlib.util
import io
import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

BASE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location('followup', BASE/'../tools/replay-three-staker-followup.py')
w = importlib.util.module_from_spec(spec)
spec.loader.exec_module(w)
S = json.loads((BASE/'../../evidence/reported-results/three-staker-fork-20260928-090354.json').read_text())['report']
PASS = json.loads((BASE/'../../evidence/reported-results/three-staker-atomic-mock-20260928-133005.json').read_text())['report']


class Tests(unittest.TestCase):
    def run_case(self, valid=True, batch=321868):
        b = S['checkpointMessage']
        end = dict(S['checkpoint'], BlockHash='0x'+'11'*32, Batch=batch, PosInBatch=0)
        calls = []
        def rpc(url, method, params):
            if method == 'eth_chainId':
                return hex(42161 if url == 'parent' else 2886)
            if method == 'web3_clientVersion':
                return 'test'
            if method == 'eth_call':
                return '0x'+('0'*24+'22'*20 if params[0]['data']=='bridge()' else format(321868, '064x'))
            if method == 'eth_getBlockByNumber':
                if url == 'parent':
                    return dict(hash=w.w.ANCHOR)
                n = int(params[0],16)
                self.assertIn(n, (b,b+1))
                state = S['checkpoint'] if n == b else end
                return dict(number=hex(n),hash=state['BlockHash'],sendRoot=state['SendRoot'],parentHash=S['checkpoint']['BlockHash'])
            if method == 'arbdebug_validationInputsAt':
                return dict(Id=b+1,StartState=S['checkpoint'],ExpectedEndState=end)
            self.assertEqual(method,'arbdebug_validateMessageNumber')
            self.assertEqual(params,[hex(b+1),True,w.r.ROOT])
            calls.append(params)
            return dict(valid=valid,globalstate=end)
        with tempfile.TemporaryDirectory() as directory:
            p=Path(directory); snap=p/'candidate';snap.mkdir()
            (snap/'summary.json').write_text(json.dumps(S))
            (snap/'authority.json').write_text(json.dumps(dict(parentBlock=S['parentBlock'],parentBlockHash=w.w.ANCHOR)))
            (p/'pass.json').write_text(json.dumps(PASS))
            with patch.object(w.r,'rpc',rpc),patch.object(w.m,'encode',lambda sig: sig),contextlib.redirect_stdout(io.StringIO()):
                code=w.collect(snap,p/'pass.json','node','parent',p/'out')
            result=json.loads((p/'out/summary.json').read_text())
            records=list((p/'out/messages').glob('*.json'))
            return code,result,len(records),calls

    def test_stops_exactly_at_fixed_boundary(self):
        code,result,count,calls=self.run_case()
        self.assertEqual((code,result['status'],count,len(calls)),(0,'followup_span_replay_passed',1,1))
        self.assertEqual(result['numBlocks'],1)
        self.assertFalse(result['parentConfirmedAToBProven'])
        self.assertFalse(result['readyForProduction'])

    def test_beyond_parent_inbox_rejected_before_replay(self):
        code,result,count,calls=self.run_case(batch=321869)
        self.assertEqual((code,count,len(calls)),(1,0,0))
        self.assertFalse(result['executionReplayed'])

    def test_failed_execution_not_counted(self):
        code,result,count,calls=self.run_case(valid=False)
        self.assertEqual((code,count,result['validatedMessagesTotal']),(1,0,0))

    def test_old_atomic_result_rejected(self):
        w.check_pass(PASS)
        with self.assertRaises(ValueError):
            w.check_pass(dict(PASS,governanceNumBlocks=7838))

if __name__=='__main__':
    unittest.main()
