"""Local tests of rehearsal guards and postconditions; does not access any RPC."""
import importlib.util
import json
import tempfile
import unittest
from pathlib import Path

spec=importlib.util.spec_from_file_location('recovery',(Path(__file__).resolve().parent / '../tools/mock-validator-recovery.py'))
m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
A='0x'+'ab'*20
H='0x'+'11'*32
R='0x'+'22'*32
NEW='0x'+'33'*32
OUT='0x'+'44'*20


def word(n):return '0x'+format(n,'064x')


class Fake:
    def __init__(self,path,chain=31337,role=True,challenge=0,corrupt_spent=False):
        self.output=path;self.steps=[];self.chain=chain;self.role=role;self.challenge=challenge
        self.authorized=False;self.paused=0;self.confirmed=10;self.created=12;self.first=11
        self.staked=1;self.credit=0;self.balance=1000;self.root=R;self.zombie=0
        self.roots={};self.corrupt_spent=corrupt_spent
    def rpc(self,method,params):
        if method=='eth_chainId':return hex(self.chain)
        if method=='web3_clientVersion':return 'anvil/test'
        if method=='eth_getBlockByNumber':return {'hash':H}
        if method=='eth_getBalance':return hex(self.balance)
        if method=='eth_getCode':return '0xab'
        raise AssertionError(method)
    def call(self,to,sig,*args):
        if sig=='hasRole(bytes32,address)':return word(int(self.role))
        if sig=='getThreshold()':return word(3)
        if sig=='getNode(uint64)':return H+'00'*352
        if sig=='getStakerAddress(uint64)':return '0x'+A[2:].zfill(64)
        if sig=='getStaker(address)':return '0x'+''.join(format(x,'064x') for x in [100,0,12,self.challenge,1])
        if sig=='outbox()':return '0x'+OUT[2:].zfill(64)
        if sig=='roots(bytes32)':return self.roots.get(args[0],m.ZERO)
        if sig=='wasmModuleRoot()':return self.root
        if sig=='isSpent(uint256)':return word(0 if self.corrupt_spent and self.confirmed==13 else 1)
        raise AssertionError(sig)
    def number(self,sig,*args):
        return {'latestConfirmed()':self.confirmed,'latestNodeCreated()':self.created,
            'firstUnresolvedNode()':self.first,'stakeToken()':0,'stakerCount()':1,
            'withdrawableFunds(address)':self.credit,'isStaked(address)':self.staked,
            'isZombie(address)':self.zombie,'paused()':self.paused}[sig]
    def impersonate(self,address):
        if not self.authorized:raise AssertionError('unverified fork')
    def admin(self,label,sig,*args):
        self.steps.append({'step':label})
        if sig=='pause()':self.paused=1
        elif sig=='forceRefundStaker(address[])':self.credit=100;self.staked=0;self.zombie=1
        elif sig=='setWasmModuleRoot(bytes32)':self.root=args[0]
        elif sig.startswith('forceCreateNode'):self.created=13
        elif sig.startswith('forceConfirmNode'):self.confirmed=13;self.first=14;self.roots[args[2]]=args[1]
        elif sig=='resume()':self.paused=0
        else:raise AssertionError(sig)
    def send(self,label,sender,to,data):
        if label.startswith('withdraw'):
            self.balance+=self.credit-2;self.credit=0
        elif label=='remove old zombies':self.zombie=0
        else:raise AssertionError(label)
        self.steps.append({'receipt':{'gasUsed':'0x1','effectiveGasPrice':'0x2'}})


class RehearsalTests(unittest.TestCase):
    def run_case(self,**kwargs):
        with tempfile.TemporaryDirectory() as tmp:
            d=Path(tmp)
            (d/'deployed-code.json').write_text(json.dumps({'rollup':{'address':m.ROLLUP,'code':'0xab'}}))
            (d/'confirmed-node-storage.json').write_text(json.dumps({'stateHash':H}))
            state={'BlockHash':H,'SendRoot':R,'Batch':100,'PosInBatch':0}
            summary={'parentBlock':'0xa','confirmedNode':10,'oldConfirmedState':state,
                     'checkpoint':{**state,'Batch':101},'confirmedInboxMaxCount':102,'newWasmRoot':NEW}
            authority={'parentBlockHash':H,'rollup':m.ROLLUP,'slots':{'admin':{'address':m.EXECUTOR}}}
            f=Fake(d,**kwargs)
            return m.rehearse(f,summary,authority,d,[A])
    def test_success_not_production_ready(self):
        r=self.run_case()
        self.assertEqual(r['status'],'contract_rehearsal_passed')
        self.assertFalse(r['readyForProduction'])
        self.assertFalse(r['validatorRestartTested'])
    def test_wrong_chain(self):
        with self.assertRaisesRegex(ValueError,'isolated chain'):self.run_case(chain=42161)
    def test_missing_role(self):
        with self.assertRaisesRegex(ValueError,'EXECUTOR_ROLE'):self.run_case(role=False)
    def test_challenged_staker(self):
        with self.assertRaisesRegex(ValueError,'active challenge'):self.run_case(challenge=1)
    def test_spent_changes_fail(self):
        with self.assertRaisesRegex(ValueError,'Spent flag'):self.run_case(corrupt_spent=True)
    def test_local_rpc_mutation_gate(self):
        class Process:
            def poll(self):return None
        f=m.LocalFork(1,Process(),Path('.'))
        with self.assertRaisesRegex(ValueError,'identity'):
            f.rpc('eth_sendTransaction',[{}])


if __name__=='__main__':unittest.main()
