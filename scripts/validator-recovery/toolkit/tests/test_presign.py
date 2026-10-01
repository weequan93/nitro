import copy
import json
import sys
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch
sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import presign as s
from core import *


class PresignTests(unittest.TestCase):
    def test_nonce_consumed_rejected(self):
        with self.assertRaisesRegex(ValueError, 'nonce already consumed'):
            s.signing_anchor(dict(safeNonce=11), dict(nonce=10))

    def test_authority_change_rejected(self):
        with self.assertRaisesRegex(ValueError, 'baseline changed'):
            s.signing_anchor(dict(safeNonce=10, paused=False, routes={'a':2}, codeHashes={}),
                             dict(nonce=10, pre=dict(routes={'a':1}, codeHashes={})))

    def test_receipt_order(self):
        a = dict(blockNumber='0x20', transactionIndex='0x1')
        b = dict(blockNumber='0x20', transactionIndex='0x2')
        s.require_receipt_order(a, b)
        for bad in (a, dict(blockNumber='0x19', transactionIndex='0xff')):
            with self.assertRaises(ValueError): s.require_receipt_order(a, bad)

    def prepare(self, root):
        A = dict(BlockHash='0x'+'11'*32, SendRoot=ZERO, Batch=1, PosInBatch=0)
        B = dict(BlockHash='0x'+'22'*32, SendRoot=ZERO, Batch=1, PosInBatch=1)
        pre = dict(parentBlock='0x100', parentBlockHash='0x'+'aa'*32,
            safeNonce=10, paused=False, routes={}, codeHashes={}, wasm=OLD,
            confirmed=5, created=7, firstUnresolved=6,
            confirmedStorage=[sh(A, 1)]+[ZERO]*11, sibling=6, lastHash='0x'+'33'*32,
            stakers=[dict(address=a, active=True, amount=10, challenge=0, credit=0) for a in REFUNDS],
            inboxCount=2, fastConfirmer=FAST)
        window = root/'window'; window.mkdir(); (window/'candidate').mkdir(); (window/'audit').mkdir()
        c = dict(parentBlock=pre['parentBlock'], confirmedNode=5, oldConfirmedState=A,
            checkpoint=B, confirmedInboxMaxCount=1, checkpointBlock=1, checkpointMessage=1)
        audit = dict(parentBlock=pre['parentBlock'], confirmedNode=5,
            parentConfirmedState=A, localStateAtConfirmedPosition=A, checkpoint=B,
            localMessageAtConfirmedPosition=0, checkpointMessage=1, positionalMessageSpan=1)
        save(window/'candidate/summary.json', c); save(window/'audit/summary.json', audit)
        save(window/'inventory.json', pre)
        class FakeRPC:
            def __init__(self, url): pass
            def rpc(self, method, params):
                if method == 'eth_chainId': return hex(2886 if getattr(self, 'node', False) else 42161)
                if method == 'eth_getBlockByNumber': return dict(hash=B['BlockHash'], sendRoot=ZERO)
                raise AssertionError(method)
            def call(self, *args, **kw): return '0x'+'44'*32
        pause = root/'pause'; pause.mkdir()
        s.recovery.write_package(pause, dict(kind='pause', purpose='production-review',
            pre=pre, nonce=10, transactions=[admin('pause()')]), FakeRPC('parent'))
        pause_id = load_package(pause)[1]
        save(window/'presign-input.json', dict(mode='fixed-pre-pause-input',
            pausePackageIdentity=pause_id, inventorySha256=digest(pre),
            candidateSha256=digest(c), auditSha256=digest(audit)))
        decision = root/'decision.json'
        save(decision, dict(mode='approved-parameters', candidateSha256=digest(c),
            auditSha256=digest(audit), numBlocks=1, acknowledgesTrustedMigration=True,
            acknowledgesNoParentAToBProof=True, reviewReference='UNIT TEST ONLY'))
        replay=root/'replay'; replay.mkdir(); (replay/'messages').mkdir()
        man=dict(firstMessage=1, lastMessage=1, startingState=A, endingState=B,
            auditSha256=digest(audit), wasmModuleRoot=NEW)
        record=dict(message=1, wasmModuleRoot=NEW, manifestSha256=digest(man),
            previousRecordSha256=digest(man), start=A, end=B,
            validationResponse=dict(valid=True, globalstate=B), recordedInputSha256='00'*32,
            canonicalHeader=dict(number='0x1', hash=B['BlockHash'], parentHash=A['BlockHash'], sendRoot=ZERO))
        save(replay/'manifest.json',man); save(replay/'messages/1.json',record)
        save(replay/'summary.json',dict(status='retained_span_replay_passed',executionReplayed=True,
            validatedMessagesTotal=1,recordChainSha256=digest(record),lastValidatedState=B))
        def client(url):
            obj=FakeRPC(url); obj.node=(url=='node'); return obj
        args=SimpleNamespace(pause=pause,window=window,replay=replay,decision=decision,
            parent_rpc='parent',node_rpc='node',out=root/'sequence')
        return args,pre,client

    def build(self, args, pre, client):
        with patch.object(s, 'RPC', side_effect=client), patch.object(s, 'inventory', return_value=pre):
            s.build_sequence(args)

    def test_unpaused_build_and_future_nonces(self):
        with tempfile.TemporaryDirectory() as tmp:
            args, pre, client = self.prepare(Path(tmp))
            self.build(args,pre,client)
            packages, doc=s.sequence(args.out)
            self.assertEqual(doc['nonces'],dict(pause=10,recovery=11,resume=12))
            self.assertEqual(packages['recovery']['pre']['paused'],False)
            self.assertEqual(packages['recovery']['safeFields']['operation'],1)
            self.assertEqual(packages['resume']['safeFields']['operation'],0)
            self.assertTrue(packages['resume']['requiresActualRecoveryReceipt'])
            self.assertEqual(len(packages['recovery']['transactions']),4)
            self.assertEqual(packages['recovery']['transactions'][-1],
                admin('forceConfirmNode(uint64,bytes32,bytes32)',8,'0x'+'22'*32,ZERO))

    def test_old_simulation_pause_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            args,pre,client=self.prepare(Path(tmp))
            p=read(args.pause/'package.json');p['purpose']='simulation-only';save(args.pause/'package.json',p);seal(args.pause)
            with self.assertRaisesRegex(ValueError,'reviewed production pause'):
                self.build(args,pre,client)

    def test_unapproved_decision_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            args,pre,client=self.prepare(Path(tmp))
            d=read(args.decision);d['mode']='review-only';save(args.decision,d)
            with self.assertRaisesRegex(ValueError,'parameter review'):
                self.build(args,pre,client)
            self.assertFalse(args.out.exists())

    def test_incomplete_replay_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            args,pre,client=self.prepare(Path(tmp))
            d=read(args.replay/'summary.json');d['status']='running';save(args.replay/'summary.json',d)
            with self.assertRaisesRegex(ValueError,'Replay unfinished'):
                self.build(args,pre,client)

    def test_live_drift_rejected_before_output(self):
        with tempfile.TemporaryDirectory() as tmp:
            args,pre,client=self.prepare(Path(tmp))
            live=copy.deepcopy(pre);live['created']+=1
            with patch.object(s,'RPC',side_effect=client),patch.object(s,'inventory',side_effect=[pre,live]):
                with self.assertRaisesRegex(ValueError,'State changed'):
                    s.build_sequence(args)
            self.assertFalse(args.out.exists())

    def test_dependency_tampering_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            args,pre,client=self.prepare(Path(tmp));self.build(args,pre,client)
            resume=args.out/'resume';p=read(resume/'package.json')
            p['recoveryPackageIdentity']='00'*32;save(resume/'package.json',p);seal(resume)
            doc=read(args.out/'sequence.json');doc['packages']['resume']=load_package(resume)[1]
            save(args.out/'sequence.json',doc)
            with self.assertRaisesRegex(ValueError,'Dependencies differ'):s.sequence(args.out)

    def test_recovery_check_requires_actual_pause_transaction(self):
        with tempfile.TemporaryDirectory() as tmp:
            args,pre,client=self.prepare(Path(tmp));self.build(args,pre,client)
            packages,doc=s.sequence(args.out)
            passed=Path(tmp)/'fork';passed.mkdir()
            save(passed/'summary.json',dict(status='exact_presigned_sequence_fork_passed',packages=doc['packages']))
            check=SimpleNamespace(sequence=args.out,parent_rpc='parent',fork_pass=passed,
                stage='recovery',pause_tx=None,out=Path(tmp)/'check')
            with patch.object(s,'RPC',side_effect=client):
                with self.assertRaisesRegex(ValueError,'Actual pause execution'):
                    s.check_execution(check)
            self.assertFalse(check.out.exists())

if __name__=='__main__':unittest.main()
