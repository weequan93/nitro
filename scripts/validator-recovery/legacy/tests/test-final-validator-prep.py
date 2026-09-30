import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('prep', (Path(__file__).resolve().parent / '../../diagnostics/inspect-final-validator-prep.py'))
x = importlib.util.module_from_spec(spec)
spec.loader.exec_module(x)


class Tests(unittest.TestCase):
    def test_sensitive_fields_and_log_content_not_emitted(self):
        c = dict(Id='container', Image=x.OLD, State=dict(Running=True, StartedAt='time'),
            Config=dict(Env=['PASSWORD=SENSITIVE_SECRET'], Entrypoint=['sh', '-c', 'echo SENSITIVE_SECRET'],
                Cmd=['--node.staker.parent-chain-wallet.private-key=SENSITIVE_SECRET'],
                Labels={'unknown': 'SENSITIVE_SECRET'}), HostConfig={}, Mounts=[])
        text = json.dumps(x.summarize(c))
        self.assertNotIn('SENSITIVE_SECRET', text)
        self.assertIsNone(x.summarize(c)['launch']['explicitBooleanFlags']['node.staker.enable-fast-confirmation'])
        log = 'password=SENSITIVE_SECRET\nrunning as validator txSender='+x.ROLES['current']+' actingAsWallet='+x.ROLES['current']+' rpc=SENSITIVE_SECRET'
        self.assertEqual(x.identities(log), [dict(txSender=x.ROLES['current'], actingAsWallet=x.ROLES['current'])])

    def test_false_missing_and_malformed_flags_distinguished(self):
        flags=x.launch(['--node.staker.enable', '--node.staker.enable-fast-confirmation=false',
            '--node.block-validator.enable=garbage', '--node.staker.strategy', 'MakeNodes'])
        self.assertIs(flags['explicitBooleanFlags']['node.staker.enable'], True)
        self.assertIs(flags['explicitBooleanFlags']['node.staker.enable-fast-confirmation'], False)
        self.assertEqual(flags['explicitBooleanFlags']['node.block-validator.enable'], 'unrecognized')
        self.assertIsNone(flags['explicitBooleanFlags']['node.staker.use-smart-contract-wallet'])
        self.assertEqual(flags['explicitStrategy'], 'MakeNodes')

    def test_protected_output_including_symlink(self):
        for path in ('/data/report.json', '/data_mock/a', '/data_new/validator/a'):
            self.assertFalse(x.safe_output(Path(path)))
        with tempfile.TemporaryDirectory() as tmp:
            link=Path(tmp)/'linked'; link.symlink_to('/data')
            self.assertFalse(x.safe_output(link/'report.json'))
            self.assertTrue(x.safe_output(Path(tmp)/'report.json'))

    def test_mutation_command_rejected_before_subprocess(self):
        with patch.object(x.subprocess, 'run') as run:
            for args in (('stop','validator-nitro-1'), ('exec','validator-nitro-1','cat','/keys/a'), ('image','pull','prod')):
                with self.assertRaises(ValueError): x.docker(*args)
            run.assert_not_called()


if __name__ == '__main__': unittest.main()
