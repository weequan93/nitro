"""Offline runtime adapter guards; no Docker, RPC or node DB access."""
import importlib.util
import json
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('new', (Path(__file__).resolve().parent / '../tools/rehearse-nitro-span-restart.py'))
new = importlib.util.module_from_spec(spec)
spec.loader.exec_module(new)


class Checks(unittest.TestCase):
    def test_atomic_gate(self):
        report = json.loads((Path(__file__).parent/'../../evidence/reported-results/atomic-span-mock-20260928-084057.json').read_text())['report']
        new.check_atomic(report)
        for key, value in [('governanceNumBlocks', 1), ('atomicSuccessTested', False),
                           ('innerRollbackTested', False), ('exactExpectedNodeHash', '0x00')]:
            with self.subTest(key=key), self.assertRaises(ValueError):
                new.check_atomic(dict(report, **{key: value}))

    def test_previous_container_checks(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp).resolve()
            folder = root/'run'
            folder.mkdir()
            path = folder/'summary.json'
            report = dict(status='nitro_restart_rehearsal_passed', container='recovery-nitro-restart-check',
                gracefulRestartTested=True, noAdditionalSenderNonceObserved=True,
                testContainerStopped=True, ownedForkStillRunning=False, containerId='prior')
            path.write_text(json.dumps(report))
            c = dict(Id='prior', State={'Running': False}, Image=new.rt.IMAGE,
                Name='/recovery-nitro-restart-check', Mounts=[
                    dict(Destination='/home/user/.arbitrum', Source=str(new.rt.DATA)),
                    dict(Destination='/test.json', Source=str(folder/'config.json'))])
            with patch.object(new.rt, 'SCRIPTS', root):
                new.previous_restart(path, [c])
                for key, value in [('State', {'Running': True}), ('Name', '/validator-nitro-1'),
                                   ('Image', 'different'), ('Mounts', [])]:
                    with self.subTest(key=key), self.assertRaises(ValueError):
                        new.previous_restart(path, [dict(c, **{key: value})])

    def test_runtime_wiring_and_report(self):
        with tempfile.TemporaryDirectory() as temp:
            p = Path(temp)
            for name in ['summary.json', 'authority.json', 'prior.json', 'atomic.json']:
                (p/name).write_text('{}')
            saved = []
            def fake_preflight(*args, **kwargs):
                self.assertEqual(new.rt.TEST, 'recovery-nitro-span-restart')
                return 'checked'
            def fake_main():
                new.rt.TEST = 'recovery-nitro-restart-check'
                self.assertEqual(new.rt.preflight(), 'checked')
                f = object.__new__(new.rt.safe.SafeFork)
                self.assertEqual(f.governance_span, 7838)
                f.exact_governance_hash_checked = True
                f.governance_commitment = dict(expectedNodeHash=new.span.EXPECTED)
                new.rt.safe.original_rehearse(f)
                report = dict(status='nitro_restart_rehearsal_passed', readyForProduction=False)
                new.rt.save(p/'result'/'summary.json', report)
                self.assertEqual(report['governanceNumBlocks'], 7838)
                self.assertFalse(report['parentConfirmedAToBProven'])
                return 0
            argv = ['tool', str(p), '--previous-pass', str(p/'prior.json'),
                '--atomic-pass', str(p/'atomic.json'), '--span-audit', str(p/'audit'),
                '--span-replay', str(p/'replay'), '--out', str(p/'result')]
            original_fork = new.rt.safe.SafeFork
            with patch.object(new, 'check_atomic'), patch.object(new.two, 'check_fixture'), \
                 patch.object(new.two, 'verify_span_evidence', return_value=dict(recordChainSha256=new.span.RECORD_CHAIN)), \
                 patch.object(new.rt, 'preflight', fake_preflight), patch.object(new.rt, 'main', fake_main), \
                 patch.object(new.rt.safe, 'original_rehearse', return_value={}), \
                 patch.object(new.rt, 'save', side_effect=lambda path, value: saved.append(value)), \
                 patch.object(sys, 'argv', argv):
                self.assertEqual(new.main(), 0)
            self.assertIs(new.rt.safe.SafeFork, original_fork)
            self.assertEqual(saved[0]['test'], 'nitro_span_7838_restart')


if __name__ == '__main__':
    unittest.main()
