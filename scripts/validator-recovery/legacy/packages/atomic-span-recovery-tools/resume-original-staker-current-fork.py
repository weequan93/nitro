#!/usr/bin/env python3
"""Continue the specific paused 18549 Anvil: Safe resume, original EOA restake,
normal assertion/confirmation using the Watchtower-validated 64-message interval.
No production keys. Tests contract account reuse, not original validator signing.
Requires rehearse-safe-recovery.py and its existing mock dependency.
"""
import argparse
import importlib.util
import json
import subprocess
import time
import urllib.request
from pathlib import Path

spec=importlib.util.spec_from_file_location('safe_recovery',Path(__file__).with_name('rehearse-safe-recovery.py'))
safe=importlib.util.module_from_spec(spec);spec.loader.exec_module(safe)
m=safe.m
OLD='0x5cda45a9ae0e52f1d5110dc3819f6fb96bade33a'
ROOT='0x121d685e2fdb0e3291592d6b90bd70d503951335d19d96455448eb7a14d17421'
SENDROOT='0xac44727106df865ba77137bcea131330935688cf48174d77b2b41a65613b03cf'
BEFORE=dict(BlockHash='0x2654eb1fc5b10d89fa6e3a75b5ec06641aa9c2856b39c0c07468c113b2014189',SendRoot=SENDROOT,Batch=320188,PosInBatch=225,machineStatus=1)
AFTER=dict(BlockHash='0x6fef08565d967d5c57521d4784e9fe58355c60ecf306b8641d477ed5c1011605',SendRoot=SENDROOT,Batch=320189,PosInBatch=0,machineStatus=1)


def rpc_at(url,method,params):
    req=urllib.request.Request(url,json.dumps(dict(jsonrpc='2.0',id=1,method=method,params=params)).encode(),{'Content-Type':'application/json'})
    with urllib.request.urlopen(req,timeout=60) as r:obj=json.load(r)
    if 'error' in obj:raise ValueError(method+': '+str(obj['error']))
    return obj['result']


def keccak(data):
    return subprocess.check_output(['cast','keccak','0x'+data.hex()],text=True,timeout=30).strip()


def raw(value):return bytes.fromhex(value.removeprefix('0x'))


def global_hash(s):
    return keccak(b'Global state:'+raw(s['BlockHash'])+raw(s['SendRoot'])+
                  s['Batch'].to_bytes(8,'big')+s['PosInBatch'].to_bytes(8,'big'))


def state_hash(s,count):
    return keccak(raw(global_hash(s))+count.to_bytes(32,'big')+bytes([s['machineStatus']]))


def expected_hash(prev_hash,acc,count):
    segments=[keccak(b'Block state:'+raw(global_hash(s))) for s in (BEFORE,AFTER)]
    execution=keccak(bytes(32)+count.to_bytes(32,'big')+b''.join(raw(x) for x in segments))
    return keccak(b'\x00'+raw(prev_hash)+raw(execution)+raw(acc)+raw(ROOT))


class ExistingFork(safe.SafeFork):
    def __init__(self,output):
        self.url='http://127.0.0.1:18549';self.output=output;self.steps=[];self.authorized=False
    def rpc(self,method,params):
        if method.startswith(('anvil_','evm_','eth_send')):
            m.require(self.authorized,'Fork mutations not authorized by preflight')
            m.require(rpc_at(self.url,'eth_chainId',[])==hex(31337),'Wrong chain')
            m.require('anvil' in rpc_at(self.url,'web3_clientVersion',[]).lower(),'Not Anvil')
            block=rpc_at(self.url,'eth_getBlockByNumber',['0x1e58e750',False])
            m.require(block and block['hash'].lower()=='0xc4a99db9563e557c6544abe0e4299ae14f8c641a7c96e938178d2308b5c29c72','Wrong fork anchor')
        return rpc_at(self.url,method,params)
    def node(self,n):
        data=self.call(m.ROLLUP,'getNode(uint64)',n)[2:]
        m.require(len(data)==12*64,'Unexpected Node ABI')
        return ['0x'+data[i:i+64] for i in range(0,len(data),64)]
    def clock(self):
        return int(self.rpc('eth_call',[{'data':'0x4360005260206000f3'},'latest']),16)
    def advance(self,goal):
        delta=max(0,goal-self.clock())
        m.require(delta<=10000,'Unexpected deadline gap')
        if delta:self.rpc('anvil_mine',[hex(delta)])
        m.require(self.clock()>=goal,'EVM clock did not advance')
    def send_value(self,label,data,value=0):
        tx={'from':OLD,'to':m.ROLLUP,'data':data,'value':hex(value),'gas':hex(2000000)}
        self.rpc('eth_call',[tx,'latest'])
        txhash=self.rpc('eth_sendTransaction',[tx])
        (self.output/(label+'-transaction.json')).write_text(json.dumps(dict(hash=txhash,transaction=tx),indent=2))
        for _ in range(120):
            receipt=self.rpc('eth_getTransactionReceipt',[txhash])
            if receipt is not None:break
            time.sleep(.25)
        m.require(receipt and int(receipt['status'],16)==1,label+' transaction failed')
        self.steps.append(dict(step=label,forkTransaction=txhash,receipt=receipt))
        (self.output/'steps.json').write_text(json.dumps(self.steps,indent=2))
        print('PASS',label,txhash,flush=True)
        return receipt


def main():
    p=argparse.ArgumentParser(description=__doc__);p.add_argument('--out',required=True,type=Path)
    a=p.parse_args();a.out.mkdir(parents=True,exist_ok=False)
    f=ExistingFork(a.out)
    report=dict(status='stopped',readyForProduction=False,originalValidatorRuntimeTested=False,privateKeyUsed=False)
    try:
        m.require(f.rpc('eth_chainId',[])==hex(31337),'Wrong chain')
        m.require('anvil' in f.rpc('web3_clientVersion',[]).lower(),'Not Anvil')
        m.require(f.number('paused()')==1,'Expected paused fork; do not blindly rerun a partial test')
        m.require(f.number('latestConfirmed()')==42886 and f.number('latestNodeCreated()')==42886,'Unexpected recovery/pending state')
        m.require(f.number('firstUnresolvedNode()')==42887,'Unexpected unresolved pointer')
        m.require(f.call(m.ROLLUP,'wasmModuleRoot()').lower()==ROOT,'Wrong WASM')
        m.require(f.number('stakeToken()')==0 and f.number('isStaked(address)',OLD)==0,'Unexpected original stake state')
        m.require(f.number('isValidator(address)',OLD)==1,'Original account not a validator')
        m.require(f.rpc('eth_getCode',[OLD,'latest'])=='0x','Original account not EOA')
        m.require(f.number('withdrawableFunds(address)',OLD)==10**12,'Unexpected refund credit')
        latest=rpc_at('http://127.0.0.1:8249','arb_latestValidated',[])
        m.require(latest['GlobalState']=={k:v for k,v in AFTER.items() if k!='machineStatus'},'Unexpected Watchtower validated endpoint')
        m.require([x.lower() for x in latest['WasmRoots']]==[ROOT],'Endpoint not validated with new root')
        # Fixed interval established by captured Watchtower message-count logs:
        # start 126237292, finish 126237356. Verify both canonical block identities.
        for height,state in [(126237291,BEFORE),(126237355,AFTER)]:
            block=rpc_at('http://127.0.0.1:8249','eth_getBlockByNumber',[hex(height),False])
            m.require(block and block['hash'].lower()==state['BlockHash'] and block['sendRoot'].lower()==state['SendRoot'],'Canonical checkpoint mismatch')
        bridge='0x'+f.call(m.ROLLUP,'bridge()')[-40:]
        inbox=int(f.call(bridge,'sequencerMessageCount()'),16)
        m.require(inbox==320189,'Unexpected fork inbox; 64-message endpoint may be insufficient')
        prev=f.node(42886)
        m.require(prev[0].lower()==state_hash(BEFORE,inbox).lower(),'Recovery before-state commitment mismatch')
        m.require(int(prev[9],16)==0,'Recovery node already has a child')
        acc=f.call(bridge,'sequencerInboxAccs(uint256)',320188)
        expected=expected_hash(prev[11],acc,64)
        assertion='('+m.state_tuple(BEFORE)+','+m.state_tuple(AFTER)+',64)'
        creation=m.encode('newStakeOnNewNode('+m.ASSERTION+',bytes32,uint256)',assertion,expected,inbox)
        (a.out/'validated-interval.json').write_text(json.dumps(dict(before=BEFORE,after=AFTER,
            startingMessageCount=126237292,endingMessageCount=126237356,numBlocks=64,
            expectedNodeHash=expected,latestValidated=latest),indent=2))
        f.authorized=True
        f.admin('resume after paused Watchtower validation','resume()')
        m.require(f.number('paused()')==0,'Resume failed')
        f.impersonate(OLD)
        credit=f.number('withdrawableFunds(address)',OLD)
        balance=int(f.rpc('eth_getBalance',[OLD,'latest']),16)
        receipt=f.send_value('withdraw-original-refund',m.encode('withdrawStakerFunds()'))
        gas=int(receipt['gasUsed'],16)*int(receipt['effectiveGasPrice'],16)
        m.require(int(f.rpc('eth_getBalance',[OLD,'latest']),16)==balance+credit-gas,'Refund balance delta mismatch')
        m.require(f.number('withdrawableFunds(address)',OLD)==0,'Refund not cleared')
        f.send_value('remove-old-zombies',m.encode('removeOldZombies(uint256)',0))
        m.require(f.number('isZombie(address)',OLD)==0,'Original account still zombie')
        f.advance(int(prev[10],16)+f.number('minimumAssertionPeriod()')+1)
        stake=f.number('currentRequiredStake()');m.require(0<stake<=10**18,'Unexpected required stake')
        f.send_value('original-restake-normal-create',creation,stake)
        m.require(f.number('latestNodeCreated()')==42887 and f.number('latestStakedNode(address)',OLD)==42887,'New node/stake mismatch')
        node=f.node(42887)
        m.require(int(node[3],16)==42886 and node[11].lower()==expected.lower(),'Wrong child node commitment')
        m.require(node[0].lower()==state_hash(AFTER,inbox).lower(),'Wrong endpoint state hash')
        f.advance(max(int(node[4],16),int(f.node(42886)[5],16))+1)
        f.send_value('original-normal-confirm',m.encode('confirmNextNode(bytes32,bytes32)',AFTER['BlockHash'],SENDROOT))
        m.require(f.number('latestConfirmed()')==42887,'Normal confirmation did not advance')
        outbox='0x'+f.call(m.ROLLUP,'outbox()')[-40:]
        m.require(outbox=='0x47da6c41d03ac0608924e86f61577df558114bd8','Outbox changed')
        m.require(f.call(outbox,'roots(bytes32)',SENDROOT).lower()==AFTER['BlockHash'],'Outbox endpoint not updated')
        report.update(status='paused_resume_original_rejoin_passed',recoveryNode=42886,normalConfirmedNode=42887,
            normalAssertionNumBlocks=64,account=OLD,paused=False,watchtowerValidatedEndpoint=latest,
            safeResumeExecutionTested=True,refundPaymentTested=True,restakingTested=True,normalConfirmationTested=True,
            note='Existing historical Anvil only. Mock owner/EOA authorization and mock gas funds. Normal assertion uses the observed 64-message Watchtower interval. Does not approve synthetic numBlocks=1 in the earlier trusted recovery assertion; no production signatures or original validator runtime tested.')
    except Exception as exc:
        report['error']=str(exc)
    finally:
        if f.authorized:
            try:f.rpc('anvil_stopImpersonatingAccount',[OLD])
            except Exception:pass
        (a.out/'summary.json').write_text(json.dumps(report,indent=2)+'\n')
    print(json.dumps(report,indent=2));print('OUTPUT',a.out)
    return 0 if report['status']=='paused_resume_original_rejoin_passed' else 1


if __name__=='__main__':raise SystemExit(main())
