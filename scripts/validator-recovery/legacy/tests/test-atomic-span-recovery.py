"""Offline checks for parameter capture, evidence guards and result handling."""
import importlib.util
import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('span', (Path(__file__).resolve().parent / '../tools/rehearse-atomic-span-recovery.py'))
span = importlib.util.module_from_spec(spec)
spec.loader.exec_module(span)


class Checks(unittest.TestCase):
    def test_evidence_rejects_old_span_and_wrong_record_chain(self):
        prior = json.loads((Path(__file__).parent/'../../evidence'/'reported-results'/
            'governance-span-mock-20260928-081940.json').read_text())['report']
        summary = dict(parentBlock='0x1e58e750', confirmedNode=42883, confirmedInboxMaxCount=320161,
            bridgeInboxCount=320189, checkpointMessage=126237291, newWasmRoot=span.two.interval.ROOT,
            checkpoint={k: v for k, v in span.two.interval.BEFORE.items() if k != 'machineStatus'})
        authority = dict(parentBlockHash=span.two.ANCHOR)
        evidence = dict(recordsChecked=7838, recordChainSha256=span.RECORD_CHAIN)
        span.check_evidence(summary, authority, prior, evidence)
        for key, value in [('governanceNumBlocks', 1), ('exactGovernanceNodeHash', span.m.ZERO),
                           ('status', 'stopped'), ('bothRestakingTested', False)]:
            with self.subTest(key=key), self.assertRaises(ValueError):
                span.check_evidence(summary, authority, dict(prior, **{key: value}), evidence)
        with self.assertRaises(ValueError):
            span.check_evidence(summary, authority, prior, dict(evidence, recordChainSha256='bad'))

    def test_reference_captures_encoded_7838_and_exact_hash(self):
        f = object.__new__(span.SpanReferenceFork)
        f.captured = []
        m = span.m
        assertion = '(' + m.state_tuple(span.two.interval.BEFORE) + ',' + m.state_tuple(span.two.interval.AFTER) + ',7838)'
        sig = 'forceCreateNode(uint64,uint256,' + m.ASSERTION + ',bytes32)'
        with patch.object(span.two.m.LocalFork, 'admin') as submit:
            f.submit_admin('reference', sig, 42883, 320161, assertion, span.EXPECTED)
            submit.assert_called_once()
        _, actual_sig, args = f.captured[0]
        encoded = m.encode(actual_sig, *args)
        words = span.two.words('0x' + encoded[10:], 14)
        self.assertEqual(int(words[12], 16), 7838)
        self.assertEqual(words[13], span.EXPECTED)
        for bad_assertion, bad_hash in [(assertion.replace(',7838)', ',1)'), span.EXPECTED),
                                         (assertion, m.ZERO)]:
            with self.assertRaises(ValueError):
                f.submit_admin('bad', sig, 42883, 320161, bad_assertion, bad_hash)

    def test_run_keeps_pending_production_and_restores_reference_factory(self):
        with tempfile.TemporaryDirectory() as tmp:
            class Fork:
                output = Path(tmp)
            original = span.atomic.ReferenceFork
            def fake_atomic(f, summary, authority, directory, refunds):
                self.assertIs(span.atomic.ReferenceFork, span.SpanReferenceFork)
                self.assertEqual(span.SpanReferenceFork.candidate_summary, summary)
                (f.output/'reference').mkdir()
                (f.output/'reference'/'governance-commitment.json').write_text(json.dumps(
                    dict(numBlocks=7838, expectedNodeHash=span.EXPECTED)))
                return dict(status='contract_rehearsal_passed', readyForProduction=False,
                    exactExpectedNodeHash=span.EXPECTED, paused=True, innerRollbackTested=True)
            with patch.object(span.atomic, 'rehearse', fake_atomic):
                result = span.run(Fork(), {}, {}, Path(tmp), [span.two.ACTIVE], {}, Path('prior.json'))
            self.assertIs(span.atomic.ReferenceFork, original)
            self.assertEqual(result['governanceNumBlocks'], 7838)
            self.assertFalse(result['productionNumBlocksApproved'])
            self.assertFalse(result['parentConfirmedAToBProven'])
            with patch.object(span.atomic, 'rehearse', side_effect=ValueError('failed batch')):
                with self.assertRaises(ValueError):
                    span.run(Fork(), {}, {}, Path(tmp), [span.two.ACTIVE], {}, Path('prior.json'))
            self.assertIs(span.atomic.ReferenceFork, original)


if __name__ == '__main__':
    unittest.main()
