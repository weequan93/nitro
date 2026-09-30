import copy
import importlib.util
import json
from pathlib import Path
import unittest

BASE=Path(__file__).resolve().parent
spec=importlib.util.spec_from_file_location('three_withdrawals',BASE/'../tools/rehearse-three-staker-withdrawals.py')
x=importlib.util.module_from_spec(spec);spec.loader.exec_module(x)

class Tests(unittest.TestCase):
    def test_event_decodes_existing_verified_8120(self):
        receipt=json.loads((BASE/'fixtures/withdrawal-145d72e9/receipt.json').read_text())
        event=next(e for e in receipt['logs'] if e['address'].lower()==x.ARBSYS and e['topics'][0]==x.TOPIC)
        item=x.decode_event(event)
        self.assertEqual(item['index'],8120)
        self.assertEqual(item['amount'],11140000)
        self.assertEqual(item['recipient'],'0x8eb80b1c6845391ef2025e83c2b995ee31fd612e')
        bad=copy.deepcopy(event);bad['data']=bad['data'][:-200]
        with self.assertRaises(ValueError):x.decode_event(bad)

    def test_natural_unspent_only_no_storage_reset(self):
        flags={str(i):True for i in range(x.COUNT)}
        with self.assertRaises(ValueError):x.choose_unspent(flags)
        flags['123']=False;flags['8110']=False
        self.assertEqual(x.choose_unspent(flags),8110)

    def test_incomplete_runtime_summary_rejected(self):
        with self.assertRaises(ValueError):x.check_runtime({'automaticCreationTested':True})
        report=dict(status='nitro_restart_rehearsal_passed',test='three_staker_atomic_nitro_restart_48',
          normalConfirmedNode=43009,numBlocks=48,governanceNumBlocks=10578,testContainerStopped=True,
          ownedForkStillRunning=False,validatedEndpoint=dict(GlobalState=x.j.END,WasmRoots=[x.j.r.ROOT]))
        for k in ('atomicRecoveryTested','allThreeRefundCreditsTested','originalRefundPaymentsTestedThisRun',
          'automaticCreationTested','automaticConfirmationTested','gracefulRestartTested',
          'noAdditionalSenderNonceObserved','protectedContainersUnchanged'):report[k]=True
        x.check_runtime(report)
        report['ownedForkStillRunning']=True
        with self.assertRaises(ValueError):x.check_runtime(report)

    def test_root_mapping_exception_does_not_allow_payment_or_spent_changes(self):
        row=dict(index=1,root='candidate',rootMapping='A',alreadySpent=False,executed=True,
            balanceIncrease='11',recipient='alice',token='token',duplicateRejected=True)
        after=dict(row,rootMapping='B')
        x.compare_rows([row],[after],'candidate')
        with self.assertRaises(ValueError):x.compare_rows([row],[after],'different')
        for key,value in [('alreadySpent',True),('balanceIncrease','10'),('recipient','bob')]:
            bad=dict(after,**{key:value})
            with self.assertRaises(ValueError):x.compare_rows([row],[bad],'candidate')

if __name__=='__main__':unittest.main()
