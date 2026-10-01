import importlib.util
import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

BASE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location('three_atomic', BASE/'../tools/rehearse-three-staker-atomic.py')
w = importlib.util.module_from_spec(spec)
spec.loader.exec_module(w)
S = json.loads((BASE/'../../evidence/reported-results/three-staker-fork-20260928-090354.json').read_text())['report']
AUTH = dict(parentBlock=S['parentBlock'], parentBlockHash=w.ANCHOR)


class Tests(unittest.TestCase):
    def test_wrong_candidate_and_anchor_rejected(self):
        w.check_fixture(S, AUTH)
        with self.assertRaises(ValueError):
            w.check_fixture(dict(S, checkpointMessage=126237291), AUTH)
        with self.assertRaises(ValueError):
            w.check_fixture(S, dict(AUTH, parentBlockHash=w.m.ZERO))

    def test_requires_full_saved_evidence_not_pass_label(self):
        audit = json.loads((BASE/'../../evidence/reported-results/three-staker-span-20260928-090354.json').read_text())['report']
        w.r.check_audit(audit)
        with tempfile.TemporaryDirectory() as directory:
            p = Path(directory)
            (p/'audit.json').write_text(json.dumps(audit))
            (p/'messages').mkdir()
            manifest = dict(version=1, auditSha256=w.r.digest(audit), wasmModuleRoot=w.r.ROOT,
                firstMessage=w.r.START, lastMessage=w.r.END, startingState=w.r.A, endingState=w.r.B)
            (p/'manifest.json').write_text(json.dumps(manifest))
            report = dict(status='retained_span_replay_passed', executionReplayed=True,
                validatedMessagesTotal=10578, wasmModuleRoot=w.r.ROOT, firstMessage=w.r.START,
                lastMessage=w.r.END, lastValidatedState=w.r.B, recordChainSha256=w.CHAIN)
            (p/'summary.json').write_text(json.dumps(report))
            with self.assertRaisesRegex(ValueError, 'Missing replay records'):
                w.verify(p/'audit.json', p)
            report['recordChainSha256'] = 'bad'
            (p/'summary.json').write_text(json.dumps(report))
            with self.assertRaisesRegex(ValueError, 'Wrong completed replay'):
                w.verify(p/'audit.json', p)

    def test_reference_rejects_synthetic_and_old_span(self):
        f = object.__new__(w.Reference)
        f.captured = []
        sig = 'forceCreateNode(uint64,uint256,'+w.m.ASSERTION+',bytes32)'
        for span in (1, 7838):
            with self.assertRaises(ValueError):
                f.submit_admin('wrong', sig, 43005, 321809, '(example,'+str(span)+')', '0x'+'11'*32)
        with patch.object(w.two.m.LocalFork, 'admin') as send:
            f.submit_admin('good', sig, 43005, 321809, '(example,10578)', '0x'+'11'*32)
            send.assert_called_once()

    def test_three_live_stakers_required_before_mutation(self):
        class Fork:
            def number(self, *args):
                return 2
        with patch.object(w.atomic, 'rehearse') as mutate:
            with self.assertRaisesRegex(ValueError, 'Expected three stakers'):
                w.run(Fork(), S, AUTH, BASE, w.STAKERS, {})
            mutate.assert_not_called()
        with self.assertRaisesRegex(ValueError, 'refund scope'):
            w.run(Fork(), S, AUTH, BASE, w.STAKERS[:2], {})

if __name__ == '__main__':
    unittest.main()
