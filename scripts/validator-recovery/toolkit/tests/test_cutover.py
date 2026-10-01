import json
from pathlib import Path
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch
sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import cutover as c


def source():
    return {'chain': {'info-json': json.dumps([{'chain-name': 'Deriw Chain',
            'chain-config': {'chainId': 2886}, 'parent-chain-id': 42161,
            'rollup': {'rollup': c.ROLLUP}}])},
            'parent-chain': {'id': 42161, 'connection': {'url': c.PARENT}},
            'node': {'staker': {'enable': False, 'parent-chain-wallet': {'password': 'DO NOT COPY'}},
                     'da': {'anytrust': {'enable': True, 'rest-aggregator': {
                         'enable': True, 'urls': ['https://da.example.invalid']}}},
                     'batch-poster': {'enable': False}, 'delayed-sequencer': {'enable': False},
                     'block-validator': {'enable': True, 'current-module-root': c.NEW,
                        'prerecorded-blocks': 16, 'validation-sent-limit': 8, 'forward-blocks': 32}},
            'execution': {'sequencer': {'enable': False}},
            'conf': {'env-prefix': 'UNTRUSTED', 'string': 'DO NOT COPY'}}


class CutoverTests(unittest.TestCase):
    def test_da_repair_preserves_bind_inode_and_starts_only_owned_watchtower(self):
        with tempfile.TemporaryDirectory() as tmp:
            base = Path(tmp).resolve(); d = base/'plan'; d.mkdir()
            srcfile = base/'snapshot.json'; srcfile.write_text(json.dumps(source()))
            cfgfile = d/'watchtower.json'; cfgfile.write_text(json.dumps(c.base_watchtower_config(source())))
            inode = cfgfile.stat().st_ino
            snap = {'Id': 'snapshot', 'Image': c.IMAGE, 'State': {'Running': False, 'StartedAt': 's'}}
            old = {'Id': 'old', 'Image': 'oldimage', 'State': {'Running': False, 'StartedAt': 'o'}}
            target = {'Id': 'target', 'Image': c.IMAGE, 'State': {'Running': False, 'StartedAt': 't'},
                      'Config': {'Entrypoint': ['/usr/local/bin/split-val-entry.sh'],
                                 'Cmd': ['--conf.file=/cutover.json', '--conf.env-prefix=']},
                      'HostConfig': {'NetworkMode': 'host', 'RestartPolicy': {'Name': 'no'}},
                      'Mounts': [dict(Source=str(c.DATA), Destination='/home/user/.arbitrum', RW=True, Type='bind'),
                                 dict(Source=str(cfgfile), Destination='/cutover.json', RW=False, Type='bind')]}
            plan = dict(packageDirectory=str(base/'package'), recoveryPackageIdentity='id',
                        sourceConfigSha256=c.filehash(srcfile), watchtowerConfigSha256=c.filehash(cfgfile),
                        snapshot={'id': 'snapshot'})
            launch = dict(containerId='target', recoveryPackageIdentity='id', recoveryTransaction='0x'+'11'*32)
            (d/'plan.json').write_text(json.dumps(plan)); (d/'launch.json').write_text(json.dumps(launch))
            commands = []
            def command(*args, **kwargs):
                commands.append(args)
                if args[:2] == ('docker', 'logs'):
                    return 'failed to create consensus node: rest-aggregator.enable must be set for reader mode'
                if args[0] == 'findmnt': return '/data_mock'
                self.assertEqual(args, ('docker', 'start', c.TARGET))
                target['State'].update(Running=True, StartedAt='new-start')
                return c.TARGET
            with patch.object(c, 'BASE', base), patch.object(c, 'SOURCE', srcfile), \
                 patch.object(c, 'package', return_value=({}, 'id')), \
                 patch.object(c, 'inspect', side_effect=lambda name: {c.SNAPSHOT:snap, c.PROTECTED:old, c.TARGET:target}[name]), \
                 patch.object(c, 'run', side_effect=command), patch.object(c, 'containers', return_value=[]), \
                 patch.object(c, 'recovery_gate', return_value={'paused': True}), \
                 patch.object(c.socket, 'socket'), patch('builtins.print'):
                c.repair_da(SimpleNamespace(plan=d, apply=True))
                c.checked_plan(d)
            self.assertEqual(cfgfile.stat().st_ino, inode)
            self.assertEqual(c.filehash(srcfile), plan['sourceConfigSha256'])
            self.assertEqual(json.loads((d/'launch.json').read_text())['startedAt'], 'new-start')
            self.assertEqual([x for x in commands if x[:2] == ('docker', 'start')], [('docker', 'start', c.TARGET)])
            self.assertFalse(snap['State']['Running'])
            self.assertFalse(old['State']['Running'])

    def test_modern_and_legacy_da_preserved_without_writer(self):
        modern = source()
        legacy = source()
        legacy['node']['data-availability'] = legacy['node'].pop('da')['anytrust']
        for src in (modern, legacy):
            cfg = c.watchtower_config(src)
            da = cfg['node']['da']['anytrust']
            self.assertTrue(da['enable'])
            self.assertTrue(da['rest-aggregator']['enable'])
            self.assertEqual(da['rest-aggregator']['urls'], ['https://da.example.invalid'])
            self.assertFalse(da['rpc-aggregator']['enable'])
            self.assertFalse(da['disable-signature-checking'])
            self.assertNotIn('key', da)

    def test_absent_disabled_or_writer_da_rejected(self):
        variants = [{}, {'rest-aggregator': {'enable': False}},
                    {'enable': False}, {'rpc-aggregator': {'enable': True}},
                    {'disable-signature-checking': True}]
        for value in variants:
            src = source(); src['node']['da']['anytrust'] = value
            with self.assertRaises(ValueError): c.watchtower_config(src)

    def test_da_endpoints_and_conflicts(self):
        src = source(); src['node']['da']['anytrust']['rest-aggregator']['urls'] = ['file:///data/key']
        with self.assertRaisesRegex(ValueError, 'URL'): c.watchtower_config(src)
        src = source(); src['node']['data-availability'] = {'enable': False}
        with self.assertRaisesRegex(ValueError, 'Conflicting'): c.watchtower_config(src)

    def test_da_fix_is_only_da_config_change(self):
        old = c.base_watchtower_config(source())
        fixed = c.watchtower_config(source())
        self.assertNotIn('da', old['node'])
        fixed['node'].pop('da')
        self.assertEqual(old, fixed)

    def test_keyless_config_and_limits(self):
        src = source()
        cfg = c.watchtower_config(src)
        staker = cfg['node']['staker']
        self.assertEqual(staker['strategy'], 'Watchtower')
        self.assertTrue(staker['start-validation-from-staked'])
        self.assertFalse(staker['enable-fast-confirmation'])
        self.assertNotIn('parent-chain-wallet', staker)
        self.assertNotIn('DO NOT COPY', json.dumps(cfg))
        self.assertNotIn('conf', cfg)
        self.assertEqual(cfg['node']['block-validator']['prerecorded-blocks'], 16)
        self.assertEqual(src, source())

    def test_wrong_parent_or_root_rejected(self):
        for edit in (lambda x: x['parent-chain'].update(id=31337),
                     lambda x: x['node']['block-validator'].update({'current-module-root': 'current'}),
                     lambda x: x['node']['staker'].update(enable=True)):
            src = source(); edit(src)
            with self.assertRaises(ValueError): c.watchtower_config(src)

    def test_dangerous_and_zero_limits_rejected(self):
        for change in ({'dangerous': {'reset-block-validation': True}},
                       {'dangerous': {'revalidation': {'start-block': 1}}},
                       {'validation-sent-limit': 0}, {'prerecorded-blocks': True}):
            src = source(); src['node']['block-validator'].update(change)
            with self.assertRaises(ValueError): c.watchtower_config(src)

    def test_wrong_chain_rollup_or_database_name_rejected(self):
        for key, val in [('chain-name', 'Other'), ('rollup', {'rollup': '0x0'}),
                         ('chain-config', {'chainId': 1})]:
            src = source(); info = json.loads(src['chain']['info-json']); info[0][key] = val
            src['chain']['info-json'] = json.dumps(info)
            with self.assertRaises(ValueError): c.watchtower_config(src)

    def test_writer_overlap_and_retained_stopped_container(self):
        other = {'Id': 'other', 'Name': '/other', 'State': {'Running': True},
                 'Mounts': [{'Source': '/data_mock'}]}
        with self.assertRaisesRegex(ValueError, 'Another running'): c.exclusive([other], 'snapshot')
        other['State']['Running'] = False
        c.exclusive([other], None)
        other['State']['Running'] = True
        other['Mounts'][0]['Source'] = '/data'
        c.exclusive([other], None)  # Lexical metadata only, no /data filesystem access.

    def test_tail_parent_does_not_hide_nitro_child(self):
        old = {'State': {'Running': True}}
        with patch.object(c, 'run', return_value='COMMAND\ntail\nnitro\nnitro-val'):
            with self.assertRaisesRegex(ValueError, 'Nitro process'): c.no_old_nitro(old)
        with patch.object(c, 'run', return_value='COMMAND\ntail'):
            c.no_old_nitro(old)

    def test_launch_has_only_clean_data_and_config(self):
        args = c.launch_args(Path('/data_new/scripts/cutover-unit'))
        mounts = [args[i+1] for i, v in enumerate(args) if v == '--mount']
        self.assertEqual(len(mounts), 2)
        self.assertIn('src=/data_mock/validator/config,', mounts[0])
        self.assertTrue(mounts[1].endswith(',readonly'))
        self.assertNotIn('--env', args)
        self.assertIn('--conf.env-prefix=', args)
        self.assertNotIn('/keys', ' '.join(args))
        self.assertIn('--restart=no', args)

    def test_output_paths_and_symlinks_rejected(self):
        for path in ['/data/x', '/data_mock/x', '/data_new/validator/x', '/tmp/../data/x']:
            with self.assertRaises(ValueError): c.report_dir(path)
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp).resolve(); (root/'link').symlink_to(root/'target')
            with self.assertRaises(ValueError): c.plain_path(root/'link')

    def test_missing_recovery_receipt_never_passes_gate(self):
        r = Mock(); r.rpc.return_value = hex(42161)
        with patch.object(c, 'safe_receipt', side_effect=ValueError('Receipt missing')):
            with self.assertRaisesRegex(ValueError, 'Receipt missing'):
                c.recovery_gate(r, {}, '0x'+'11'*32)

    def test_failed_live_gate_cannot_stop_snapshot(self):
        snap = {'Name': '/snapshot-sync', 'Id': 'snapshot', 'Image': c.IMAGE,
                'State': {'Running': True, 'StartedAt': 'start'}}
        old = dict(snap, Id='old', Name='/validator-nitro-1')
        with tempfile.TemporaryDirectory() as tmp:
            d = Path(tmp).resolve(); (d/'Deriw Chain'/'nitro').mkdir(parents=True)
            plan = {'snapshot': c.identity(snap)}
            def command(*args, **kwargs):
                self.assertNotEqual(args[:2], ('docker', 'stop'))
                self.assertNotEqual(args[:2], ('docker', 'run'))
                return '/data_mock'
            with patch.object(c, 'checked_plan', return_value=(d, plan, {})), \
                 patch.object(c, 'DATA', d), patch.object(c, 'inspect', side_effect=[snap, old]), \
                 patch.object(c, 'source_container'), patch.object(c, 'no_old_nitro'), \
                 patch.object(c, 'containers', return_value=[]), patch.object(c, 'run', side_effect=command), \
                 patch.object(c, 'recovery_gate', side_effect=ValueError('not recovered')):
                with self.assertRaisesRegex(ValueError, 'not recovered'):
                    c.start(SimpleNamespace(plan=d, recovery_tx='0x'+'11'*32))
            self.assertFalse((d/'launch.json').exists())

    def test_prepare_is_offline_and_does_not_mutate_containers(self):
        snap = {'Id': 'snapshot', 'Image': c.IMAGE,
                'State': {'Running': True, 'StartedAt': 'start'}, 'Mounts': []}
        with tempfile.TemporaryDirectory() as tmp:
            base = Path(tmp).resolve(); config = base/'snapshot.json'
            config.write_text(json.dumps(source()))
            def command(*args, **kwargs):
                self.assertEqual(args, ('docker', 'image', 'inspect', c.IMAGE))
                return json.dumps([{'Id': c.IMAGE}])
            with patch.object(c, 'BASE', base), patch.object(c, 'SOURCE', config), \
                 patch.object(c, 'package', return_value=({}, 'package-id')), \
                 patch.object(c, 'inspect', return_value=snap), patch.object(c, 'source_container'), \
                 patch.object(c, 'run', side_effect=command), \
                 patch.object(c, 'RPC', side_effect=AssertionError('No RPC during prepare')), \
                 patch('builtins.print'):
                c.prepare(SimpleNamespace(package=base/'recovery', out=base/'plan'))
            result = json.loads((base/'plan'/'plan.json').read_text())
            self.assertFalse(result['signingEnabled'])
            self.assertEqual(json.loads(config.read_text()), source())
            self.assertFalse(json.loads((base/'plan'/'makenodes-requirements.json').read_text())['runnable'])

    def test_gate_checks_receipt_and_current_state(self):
        r = Mock(); r.rpc.side_effect = [hex(42161), {'number': '0x9', 'hash': '0xbbb'}]
        rec = {'blockNumber': '0x8', 'blockHash': '0xaaa'}
        with patch.object(c, 'safe_receipt', return_value=rec), \
             patch.object(c, 'created_event') as event, patch.object(c, 'recovery_post') as post:
            c.recovery_gate(r, {}, '0x'+'11'*32)
            self.assertEqual([x.args[2] for x in post.call_args_list], ['0x8', '0x9'])
            event.assert_called_once_with(r, {}, rec)
        with patch.object(c, 'safe_receipt') as receipt:
            r.rpc.side_effect = [hex(31337)]
            with self.assertRaisesRegex(ValueError, 'parent chain'):
                c.recovery_gate(r, {}, '0x'+'11'*32)
            receipt.assert_not_called()

    def test_trusted_anchor_alone_is_not_fresh_progress(self):
        b = dict(BlockHash='0x'+'11'*32, SendRoot='0x'+'22'*32, Batch=1, PosInBatch=0)
        node = Mock(); node.rpc.return_value = dict(GlobalState=b, WasmRoots=[c.NEW])
        with patch.object(c, 'local_checkpoint'):
            with self.assertRaisesRegex(ValueError, 'beyond trusted B'):
                c.sample(node, {'after': b})

    def test_mixed_root_or_noncanonical_validation_rejected(self):
        b = dict(BlockHash='0x'+'11'*32, SendRoot='0x'+'22'*32, Batch=1, PosInBatch=0)
        end = dict(b, PosInBatch=1)
        node = Mock(); node.rpc.return_value = dict(GlobalState=end, WasmRoots=[c.NEW, '0xold'])
        with patch.object(c, 'local_checkpoint'):
            with self.assertRaisesRegex(ValueError, 'exclusively'): c.sample(node, {'after': b})
            node.rpc.side_effect = [dict(GlobalState=end, WasmRoots=[c.NEW]),
                                  dict(sendRoot=end['SendRoot'], number='0x2'), dict(hash='0xbad')]
            with self.assertRaisesRegex(ValueError, 'not canonical'): c.sample(node, {'after': b})


if __name__ == '__main__':
    unittest.main()
