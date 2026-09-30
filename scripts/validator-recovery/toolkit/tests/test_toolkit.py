import importlib.util
import json
import sys
import tempfile
import unittest
from pathlib import Path
sys.path.insert(0,str(Path(__file__).resolve().parents[1]))
from core import *
from recovery import dynamic, write_package, safe_receipt

class Tests(unittest.TestCase):
    def test_node_commitment_known_fixture(self):
        f=read(Path(__file__).with_name('state-fixture.json'))
        self.assertEqual(sh(f['before'],f['inboxCount']),f['expectedStateHash'])
        self.assertNotEqual(sh(f['before'],f['inboxCount']+1),f['expectedStateHash'])
    def test_public_mutations_denied(self):
        r=RPC('http://127.0.0.1:1')
        for method in ('eth_sendTransaction','eth_sendRawTransaction','anvil_setStorageAt','evm_revert'):
            with self.assertRaisesRegex(ValueError,'forbidden'):r.rpc(method,[])
    def test_zero_unknown_state_rejected(self):
        with self.assertRaises(ValueError):state(dict(BlockHash=ZERO,SendRoot=ZERO,Batch=-1,PosInBatch=0))
        with self.assertRaises(ValueError):state(dict(BlockHash=ZERO,SendRoot=ZERO,Batch=1,PosInBatch=0,machineStatus=2))
    def test_dynamic_bounds(self):
        data='0x'+(word(32)+word(4)+b'abcd'+bytes(28)).hex()
        self.assertEqual(dynamic(data,0),'0x61626364')
        with self.assertRaises(ValueError):dynamic('0x'+(word(900)+word(4)).hex(),0)
    def test_builder_single_call_and_multi(self):
        tx=admin('pause()');self.assertEqual(envelope([tx])['operation'],0)
        self.assertEqual(envelope([tx,admin('resume()')])['operation'],1)
        self.assertTrue(pack([tx]).startswith('0x00'+EXEC[2:]))
        self.assertEqual(builder([tx],'pause')['transactions'],[tx])
    def test_pause_package_roundtrip_and_tamper(self):
        class Fake:
            def rpc(self,*_):return hex(42161)
            def call(self,*_,**__):return ZERO
        with tempfile.TemporaryDirectory() as tmp:
            d=Path(tmp);p=dict(kind='pause',purpose='production-review',pre={'parentBlock':'0x1'},nonce=10,transactions=[admin('pause()')])
            write_package(d,p,Fake());self.assertEqual(load_package(d)[0]['kind'],'pause')
            doc=read(d/'safe-import.json');doc['transactions'][0]['value']='1';save(d/'safe-import.json',doc)
            with self.assertRaisesRegex(ValueError,'modified'):load_package(d)
            seal(d)
            with self.assertRaisesRegex(ValueError,'differs'):load_package(d)
    def test_unsupported_kind_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            d=Path(tmp);save(d/'package.json',dict(schema=2,chainId=42161,kind='transfer'))
            seal(d)
            with self.assertRaises(ValueError):load_package(d)
    def test_protected_output(self):
        for p in ('/data/anything','/data_mock/a','/data_new/validator/config/test'):
            with self.assertRaisesRegex(ValueError,'Protected'):output(p)
    def test_safe_receipt_identity_and_failure(self):
        import copy
        f=read(Path(__file__).with_name('safe-receipt-fixture.json'))
        receipt=f['receipt'];fields=f['safeFields']
        package=dict(safeFields=fields,nonce=fields['nonce'],expectedSafeTxHash=f['safeTxHash'])
        class Fake:
            def __init__(self):self.receipt=copy.deepcopy(receipt)
            def rpc(self,m,args):
                if m=='eth_getTransactionReceipt':return self.receipt
                if m=='eth_getTransactionByHash':return dict(to=SAFE,value='0x0',chainId='0x7a69',input=encode(EXEC_TX,*hash_fields(fields),'0x'))
                if m=='eth_getBlockByNumber':return {'hash':receipt['blockHash']}
                raise AssertionError(m)
            def call(self,*args,**kwargs):return f['safeTxHash']
        client=Fake()
        self.assertEqual(safe_receipt(client,package,receipt['transactionHash'],expected_chain=31337),receipt)
        with self.assertRaisesRegex(ValueError,'chain'):safe_receipt(client,package,receipt['transactionHash'])
        modified=copy.deepcopy(package);modified['safeFields']['operation']=1
        with self.assertRaisesRegex(ValueError,'fields differ'):safe_receipt(client,modified,receipt['transactionHash'],expected_chain=31337)
        client.receipt['logs'].append(dict(address=SAFE,topics=[keccak(b'ExecutionFailure(bytes32,uint256)')],data='0x'+word(0).hex()*2))
        with self.assertRaisesRegex(ValueError,'ExecutionFailure'):safe_receipt(client,package,receipt['transactionHash'],expected_chain=31337)

    def test_replay_chain(self):
        A=dict(BlockHash='0x'+'11'*32,SendRoot=ZERO,Batch=1,PosInBatch=0)
        B=dict(BlockHash='0x'+'22'*32,SendRoot=ZERO,Batch=1,PosInBatch=1)
        au=dict(parentConfirmedState=A,localStateAtConfirmedPosition=A,checkpoint=B,localMessageAtConfirmedPosition=0,checkpointMessage=1,positionalMessageSpan=1)
        man=dict(firstMessage=1,lastMessage=1,startingState=A,endingState=B,auditSha256=digest(au),wasmModuleRoot=NEW)
        record=dict(message=1,wasmModuleRoot=NEW,manifestSha256=digest(man),previousRecordSha256=digest(man),start=A,end=B,validationResponse=dict(valid=True,globalstate=B),canonicalHeader=dict(number='0x1',hash=B['BlockHash'],parentHash=A['BlockHash'],sendRoot=ZERO),recordedInputSha256='00'*32)
        with tempfile.TemporaryDirectory() as tmp:
            d=Path(tmp);(d/'messages').mkdir();save(d/'manifest.json',man);save(d/'messages/1.json',record)
            save(d/'summary.json',dict(status='retained_span_replay_passed',executionReplayed=True,validatedMessagesTotal=1,recordChainSha256=digest(record),lastValidatedState=B))
            self.assertEqual(evidence(au,d,A,B,1)['recordsChecked'],1)
            record['validationResponse']['valid']=False;save(d/'messages/1.json',record)
            with self.assertRaisesRegex(ValueError,'result mismatch'):evidence(au,d,A,B,1)

if __name__=='__main__':unittest.main()
