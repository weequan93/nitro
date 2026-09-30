import importlib.util
import json
import os
from pathlib import Path
import sys
import tempfile
import unittest
from types import SimpleNamespace
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import cutover as c
from test_cutover import source

spec = importlib.util.spec_from_file_location('prep_compose', Path(__file__).resolve().parents[1]/'prepare-compose.py')
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)


def key(address=c.ACCOUNT):
    return json.dumps({'address': address[2:], 'version': 3,
                       'crypto': {'cipher': 'aes-128-ctr', 'ciphertext': 'encrypted-test-fixture',
                                  'kdf': 'scrypt', 'mac': 'test-fixture'}}).encode()


class ComposePreparationTests(unittest.TestCase):
    def test_preserves_verified_config_and_da_changes_only_wallet_and_strategy(self):
        wt = c.watchtower_config(source())
        before = json.loads(json.dumps(wt))
        result = m.makenodes_config(wt)
        wallet = result['node']['staker'].pop('parent-chain-wallet')
        result['node']['staker']['strategy'] = 'Watchtower'
        self.assertEqual(result, before)
        self.assertEqual(wt, before)
        self.assertEqual(wallet['account'], c.ACCOUNT)
        self.assertNotIn('password', wallet)
        self.assertNotIn('private-key', wallet)

    def test_mounts_clean_db_only_and_ro_keys_interactive_no_restart(self):
        service = m.compose_config()['services']['validator']
        self.assertEqual(service['restart'], 'no')
        self.assertTrue(service['tty'] and service['stdin_open'])
        volumes = {v['target']:v for v in service['volumes']}
        self.assertEqual(volumes['/home/user/.arbitrum']['source'], '/data_mock/validator/config')
        self.assertTrue(volumes['/keys']['read_only'])
        self.assertTrue(all(not v['source'].startswith('/data/') for v in volumes.values()))

    def test_key_selection_rejects_duplicate_plaintext_symlink_and_hardlink(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp).resolve()
            p = root/'key'; p.write_bytes(key())
            (root/'unrelated').write_bytes(key('0x'+'12'*20))
            self.assertEqual(m.selected_keystore(root), key())
            duplicate = root/'duplicate'; duplicate.write_bytes(key())
            with self.assertRaises(ValueError): m.selected_keystore(root)
            duplicate.unlink(); duplicate.symlink_to(p)
            with self.assertRaises(ValueError): m.selected_keystore(root)
            duplicate.unlink(); os.link(p, duplicate)
            with self.assertRaises(ValueError): m.selected_keystore(root)
            duplicate.unlink()
            plain = json.loads(key()); plain['version'] = 1; p.write_text(json.dumps(plain))
            with self.assertRaises(ValueError): m.selected_keystore(root)

    def test_preparation_backs_up_config_copies_ciphertext_never_launches(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp).resolve(); d = root/'plan'; d.mkdir()
            dest = root/'validator'; cfg = dest/'config/nodeconfig.json'
            cfg.parent.mkdir(parents=True); cfg.write_bytes(b'old-config')
            compose = dest/'compose.recovered.json'; kd = dest/'keys/recovered-65fa'; kf = kd/'account.json'
            ks = root/'sourcekeys'; ks.mkdir(); (ks/'key').write_bytes(key())
            wt = c.watchtower_config(source()); (d/'watchtower.json').write_text(json.dumps(wt))
            (d/'launch.json').write_text(json.dumps({'recoveryPackageIdentity': 'package'}))
            out = root/'output'
            container = {'Id':'id', 'Image':c.IMAGE, 'State':{'Running':True,'StartedAt':'start'}}
            uid, gid = os.getuid(), os.getgid()
            commands = []
            def run(*args, **kwargs):
                commands.append(args)
                self.assertEqual(args[:2], ('docker', 'compose'))
                self.assertEqual(args[-2:], ('config','--quiet'))
                return ''
            with patch.multiple(m, DEST=dest, CONFIG=cfg, COMPOSE=compose, KEY_DIR=kd, KEY_FILE=kf, KEY_SOURCE=ks), \
                 patch.object(c, 'BASE', root), \
                 patch.object(c, 'checked_plan', return_value=(d, {'recoveryPackageIdentity':'package'}, {})), \
                 patch.object(m, 'check_runtime', return_value=(container,uid,gid)), \
                 patch.object(c, 'inspect', return_value=container), patch.object(c,'run',side_effect=run), \
                 patch.object(os, 'fchown'), patch.object(os, 'chown'), \
                 patch.object(os,'geteuid',return_value=0), patch('builtins.print'):
                m.prepare(SimpleNamespace(plan=d, apply=True, out=out))
            self.assertEqual((out/'nodeconfig.json.before').read_bytes(), b'old-config')
            self.assertEqual(kf.read_bytes(), key())
            self.assertEqual(kf.stat().st_mode & 0o777, 0o600)
            self.assertEqual(kd.stat().st_mode & 0o777, 0o700)
            report = json.loads((out/'summary.json').read_text())
            self.assertFalse(report['containersStarted'])
            self.assertTrue(report['keystoreCopyVerified'])
            self.assertFalse(report['decryptedAddressVerified'])
            self.assertEqual(len(commands), 1)


if __name__ == '__main__':
    unittest.main()
