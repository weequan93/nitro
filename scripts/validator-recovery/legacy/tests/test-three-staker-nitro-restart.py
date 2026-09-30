import copy
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

BASE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location('three_runtime', BASE/'../tools/rehearse-three-staker-nitro-restart.py')
x = importlib.util.module_from_spec(spec)
spec.loader.exec_module(x)

class Tests(unittest.TestCase):
    def test_copy_failure_and_nonempty_verification_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            base=Path(tmp).resolve(); d=base/'run';d.mkdir();p=d/'summary.json'
            report=dict(status='copy_complete',source='/data_mock/validator/config/Deriw Chain/nitro',
                target=str(x.DATA/'Deriw Chain/nitro'),copiedSourceFrozen=True,testContainersStarted=False)
            p.write_text(json.dumps(report));(d/'verify.log').write_text('')
            with patch.object(x,'SCRIPTS',base):
                x.check_copy_report(p)
                (d/'verify.log').write_text('changed file')
                with self.assertRaises(ValueError):x.check_copy_report(p)
                (d/'verify.log').write_text('');report['status']='stopped';p.write_text(json.dumps(report))
                with self.assertRaises(ValueError):x.check_copy_report(p)

    def test_protected_container_restart_detected(self):
        items=[dict(Name='/'+name,Id=name,State=dict(Running=True,StartedAt='first'))
               for name in ('validator-nitro-1','snapshot-sync')]
        baseline=x.check_protected(items)
        items[0]['State']['StartedAt']='second'
        with self.assertRaises(ValueError):x.check_protected(items,baseline)
        items[0]['State']['StartedAt']='first';items[1]['State']['Running']=False
        with self.assertRaises(ValueError):x.check_protected(items,baseline)

    def test_config_sends_only_to_owned_fork_with_new_key(self):
        cfg=dict(node={'staker':{'parent-chain-wallet':{'path':'/keys'}}},execution={})
        result=x.runtime_config(cfg,'12'*32)
        self.assertEqual(result['parent-chain'],dict(id=31337,connection=dict(url=x.FORK)))
        self.assertEqual(result['node']['staker']['parent-chain-wallet'],{'private-key':'12'*32})
        self.assertFalse(result['node']['batch-poster']['enable'])
        self.assertFalse(result['execution']['sequencer']['enable'])
        self.assertIn('path',cfg['node']['staker']['parent-chain-wallet'])

    def test_signed_transaction_count_and_commitment_rejected_if_wrong(self):
        addr='0x'+'11'*20
        create_sig='newStakeOnNewNode('+x.m.ASSERTION+',bytes32,uint256)'
        def creation(count):
            assertion='('+x.m.state_tuple(x.interval.BEFORE)+','+x.m.state_tuple(x.interval.AFTER)+','+str(count)+')'
            return x.m.encode(create_sig,assertion,x.NORMAL_NODE_HASH,321868)
        confirm=x.m.encode('confirmNextNode(bytes32,bytes32)',x.interval.AFTER['BlockHash'],x.interval.SENDROOT)
        transactions={h:dict(hash=h,to=x.m.ROLLUP,chainId=hex(31337),r='0x1',s='0x2',input=data,**{'from':addr})
                      for h,data in [('0xcreate',creation(48)),('0xconfirm',confirm)]}
        class Fork:
            def rpc(self,method,args):
                if method=='eth_getLogs':return [dict(transactionHash=h) for h in transactions]
                if method=='eth_getTransactionByHash':return transactions[args[0]]
                if method=='eth_getTransactionReceipt':return dict(status='0x1')
                raise AssertionError(method)
        with tempfile.TemporaryDirectory() as tmp:
            out=Path(tmp)
            result=x.verify_transactions(Fork(),1,addr,x.NORMAL_NODE_HASH,out)
            self.assertEqual(set(result),{'creation','confirmation'})
            (out/'signed-transactions.json').unlink()
            transactions['0xcreate']['input']=creation(64)
            with self.assertRaises(ValueError):x.verify_transactions(Fork(),1,addr,x.NORMAL_NODE_HASH,out)
            transactions['0xcreate']['input']=creation(48)
            transactions['0xcreate']['chainId']=hex(42161)
            with self.assertRaises(ValueError):x.verify_transactions(Fork(),1,addr,x.NORMAL_NODE_HASH,out)

if __name__=='__main__':unittest.main()
