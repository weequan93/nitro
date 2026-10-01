#!/usr/bin/env python3
"""Historical fork: spent preservation, natural unspent payment, duplicate rejection.
Only local owned Anvil receives transactions. L3 RPC is read-only. No Docker/DB access.
"""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import sys

BASE=Path(__file__).resolve().parent

def load(name, filename):
    spec=importlib.util.spec_from_file_location(name,BASE/filename)
    mod=importlib.util.module_from_spec(spec);spec.loader.exec_module(mod);return mod

j=load('withdrawal_three_rejoin','rehearse-three-staker-rejoin.py')
legacy=load('withdrawal_samples','rehearse-withdrawal-recovery.py')
w,m,safe=j.w,j.m,legacy.safe
BOX=legacy.BOX
TOPIC='0x3e7aafa77dbf186b7fd488006beff893744caa3c4f6f299e8a709fa2087374fc'
ARBSYS='0x0000000000000000000000000000000000000064'
NODEIF='0x00000000000000000000000000000000000000c8'
COUNT=8121


def readrpc(url,method,params):
    m.require(method in {'eth_chainId','eth_getBlockByNumber','eth_getLogs','eth_getTransactionReceipt','eth_call'},'Non-read RPC')
    return j.two.interval.rpc_at(url,method,params)


def check_runtime(report):
    m.require(report.get('status')=='nitro_restart_rehearsal_passed'
        and report.get('test')=='three_staker_atomic_nitro_restart_48'
        and report.get('normalConfirmedNode')==43009 and report.get('numBlocks')==48
        and report.get('governanceNumBlocks')==10578
        and report.get('testContainerStopped') is True and report.get('ownedForkStillRunning') is False
        and all(report.get(k) is True for k in ('atomicRecoveryTested','allThreeRefundCreditsTested',
            'originalRefundPaymentsTestedThisRun','automaticCreationTested','automaticConfirmationTested',
            'gracefulRestartTested','noAdditionalSenderNonceObserved','protectedContainersUnchanged')),
        'Runtime summary incomplete or not the expected completed run')
    m.require(report.get('validatedEndpoint')==dict(GlobalState=j.END,WasmRoots=[j.r.ROOT]),'Wrong runtime C/root')


def choose_unspent(flags):
    available=[i for i in range(COUNT) if flags[str(i)] is False]
    m.require(available,'All covered withdrawals already spent; no natural unclaimed payment sample. Do not alter spent storage.')
    return max(available)


def decode_event(event):
    topics=event['topics'];raw=bytes.fromhex(event['data'][2:])
    m.require(event['address'].lower()==ARBSYS and len(topics)==4 and topics[0].lower()==TOPIC,'Wrong withdrawal event')
    m.require(len(raw)>=224,'Short withdrawal event')
    word=lambda i:int.from_bytes(raw[i*32:(i+1)*32],'big')
    offset=word(5)
    m.require(offset==192 and len(raw)>=offset+32,'Unexpected payload offset')
    size=int.from_bytes(raw[offset:offset+32],'big')
    m.require(size<=len(raw)-offset-32,'Truncated withdrawal payload')
    payload=raw[offset+32:offset+32+size]
    m.require(len(payload)>=164 and payload[:4].hex()=='2e567b36' and word(4)==0,'Expected zero-ETH token withdrawal sample')
    return dict(index=int(topics[3],16),itemHash=topics[2].lower(),
        sender='0x'+raw[12:32].hex(),to='0x'+topics[1][-40:].lower(),
        l2Block=word(1),l1Block=word(2),timestamp=word(3),value=word(4),payload='0x'+payload.hex(),
        token='0x'+payload[16:36].hex(),recipient='0x'+payload[80:100].hex(),
        amount=int.from_bytes(payload[100:132],'big'),l3Tx=event['transactionHash'])


def build_sample(f,node,idx,summary):
    m.require(int(readrpc(node,'eth_chainId',[]),16)==2886,'Wrong L3 chain')
    height=summary['checkpointBlock']
    b=readrpc(node,'eth_getBlockByNumber',[hex(height),False])
    m.require(b and b['hash'].lower()==summary['checkpoint']['BlockHash']
        and b['sendRoot'].lower()==summary['checkpoint']['SendRoot'] and int(b['sendCount'],16)==COUNT,'Wrong L3 B header')
    lo,hi=0,height
    while lo<hi:
        mid=(lo+hi)//2
        h=readrpc(node,'eth_getBlockByNumber',[hex(mid),False])
        m.require(h and 'sendCount' in h,'Missing historical L3 header/sendCount')
        if int(h['sendCount'],16)>idx:hi=mid
        else:lo=mid+1
    logs=readrpc(node,'eth_getLogs',[dict(address=ARBSYS,fromBlock=hex(lo),toBlock=hex(lo),
        topics=[TOPIC,None,None,'0x'+format(idx,'064x')])])
    m.require(len(logs)==1,'Expected one indexed withdrawal event')
    item=decode_event(logs[0]);m.require(item['index']==idx,'Index mismatch')
    receipt=readrpc(node,'eth_getTransactionReceipt',[item['l3Tx']])
    header=readrpc(node,'eth_getBlockByNumber',[hex(lo),False])
    m.require(receipt and int(receipt['status'],16)==1 and receipt['blockHash']==header['hash']
        and logs[0]['blockHash']==header['hash'] and any(
            all(x.get(k)==logs[0].get(k) for k in ('address','topics','data','logIndex','transactionHash','blockHash'))
            for x in receipt['logs']),'Receipt/event not canonical')
    raw=readrpc(node,'eth_call',[dict(to=NODEIF,data=m.encode('constructOutboxProof(uint64,uint64)',COUNT,idx)),'latest'])
    data=bytes.fromhex(raw[2:]);m.require(len(data)>=128,'Short Merkle proof')
    root='0x'+data[32:64].hex();offset=int.from_bytes(data[64:96],'big')
    m.require(offset==96,'Unexpected Merkle proof offset')
    n=int.from_bytes(data[offset:offset+32],'big')
    m.require(0<n<256 and len(data)>=offset+32+n*32,'Malformed Merkle proof')
    proof=['0x'+data[offset+32+k*32:offset+64+k*32].hex() for k in range(n)]
    args=[item['sender'],item['to'],item['l2Block'],item['l1Block'],item['timestamp'],item['value'],item['payload']]
    computed=f.call(BOX,'calculateItemHash(address,address,uint256,uint256,uint256,uint256,bytes)',*args)
    m.require(computed.lower()==item['itemHash'],'Message item hash mismatch')
    pa='['+','.join(proof)+']'
    m.require(root==summary['checkpoint']['SendRoot'] and
        f.call(BOX,'calculateMerkleRoot(bytes32[],uint256,bytes32)',pa,idx,computed).lower()==root,'Wrong sample Merkle root')
    item.update(root=root,proof=proof,calldata=m.encode('executeTransaction(bytes32[],uint256,address,address,uint256,uint256,uint256,uint256,bytes)',pa,idx,*args))
    (f.output/'unspent-sample.json').write_text(json.dumps(item,indent=2))
    (f.output/'sample-receipt.json').write_text(json.dumps(receipt,indent=2))
    return item


def claims(f,phase,samples):
    old=legacy.SAMPLES
    try:
        legacy.SAMPLES=samples
        result=legacy.test_claims(f,phase)
    finally:legacy.SAMPLES=old
    positive=[r for r in result if r.get('executed')]
    m.require(len(positive)==1 and positive[0]['index']==samples[-1]['index'],'Unspent payment sample was not executed')
    return result


def compare_rows(before,after,changed_root):
    m.require(len(before)==len(after),'Withdrawal sample count changed')
    for a,b in zip(before,after):
        for key in ('index','root','alreadySpent','executed','balanceIncrease','recipient','token','duplicateRejected'):
            m.require(a.get(key)==b.get(key),'Withdrawal behavior changed: '+key)
        if a['root']!=changed_root:m.require(a['rootMapping']==b['rootMapping'],'Older registered root changed')


def run(f,summary,authority,directory,refunds,span,following,node):
    w.check_fixture(summary,authority)
    m.require(f.rpc('eth_chainId',[])==hex(31337) and 'anvil' in f.rpc('web3_clientVersion',[]).lower(),'Wrong fork')
    m.require(f.rpc('eth_getBlockByNumber',[summary['parentBlock'],False])['hash'].lower()==w.ANCHOR,'Wrong fork anchor')
    m.require('0x'+f.call(m.ROLLUP,'outbox()')[-40:]==BOX,'Wrong Outbox')
    f.authorized=True
    print('Reading all 8121 covered spent flags before recovery.',flush=True)
    before_flags=safe.spent_flags(f,BOX,COUNT)
    (f.output/'all-spent-before.json').write_text(json.dumps(before_flags))
    idx=choose_unspent(before_flags)
    print('Natural unspent sample',idx,'; locating historical event and proof.',flush=True)
    positive=build_sample(f,node,idx,summary)
    spent=[x for x in legacy.SAMPLES if before_flags[str(x['index'])] is True]
    m.require(spent,'Expected an already-spent regression sample')
    samples=spent+[positive]
    before=claims(f,'before-recovery',samples)
    atomic_run=w.run
    def with_paused_check(*args):
        report=atomic_run(*args)
        flags=safe.spent_flags(f,BOX,COUNT)
        (f.output/'all-spent-paused.json').write_text(json.dumps(flags))
        m.require(flags==before_flags,'Spent flags changed after atomic recovery')
        paused=claims(f,'after-atomic-paused',samples)
        compare_rows(before,paused,summary['checkpoint']['SendRoot'])
        return report
    try:
        w.run=with_paused_check
        report=j.run(f,summary,authority,directory,refunds,span,following)
    finally:w.run=atomic_run
    final_flags=safe.spent_flags(f,BOX,COUNT)
    (f.output/'all-spent-final.json').write_text(json.dumps(final_flags))
    m.require(final_flags==before_flags,'Spent flags changed after ordinary confirmation')
    after=claims(f,'after-normal-confirmation',samples)
    compare_rows(before,after,summary['checkpoint']['SendRoot'])
    print('PASS all 8121 spent flags preserved; natural payment and duplicate rejection passed in all three phases.',flush=True)
    report.update(test='three_staker_withdrawal_regression',spentFlagsChecked=COUNT,
        allCoveredSpentFlagsPreserved=True,withdrawalSamples=[x['index'] for x in samples],naturalUnspentSample=idx,
        naturalPaymentTested=True,withdrawalPhases=['before-recovery','after-atomic-paused','after-normal-confirmation'],
        withdrawalTestTransactionsReverted=True,fullWithdrawalHistoryAudited=False,
        originalAccountRuntimeTested=False,productionSigningTested=False,dockerAccessed=False,nodeDatabaseAccessed=False)
    report['limitations']='Historical owned fork, Safe/original account impersonation. All 8121 covered spent flags compared; sample execution only, not a full withdrawal-message audit. Claims reverted after each sample. Uses prior replay evidence; no new full WASM replay. No parent-confirmed A-to-B proof or production approval.'
    return report


def main():
    p=argparse.ArgumentParser(description=__doc__);p.add_argument('snapshot',type=Path)
    for name in ('runtime-pass','atomic-pass','followup','span-audit','span-replay','out'):
        p.add_argument('--'+name,type=Path,required=True)
    p.add_argument('--node-rpc',default='http://127.0.0.1:8349')
    a=p.parse_args();upstream=os.environ.get('ARCHIVE_RPC','')
    m.require(upstream.startswith(('http://','https://')),'Set ARCHIVE_RPC')
    s,inv,prior=w.read(a.snapshot/'summary.json'),w.read(a.snapshot/'authority.json'),w.read(a.atomic_pass)
    w.check_fixture(s,inv);j.follow.check_pass(prior);check_runtime(w.read(a.runtime_pass))
    span=w.verify(a.span_audit,a.span_replay);following=j.verify_followup(a.followup,s,prior)
    old=sys.argv,m.LocalFork,m.rehearse
    try:
        sys.argv=[sys.argv[0],str(a.snapshot),'--parent-rpc',upstream,'--out',str(a.out)]
        for address in w.STAKERS:sys.argv+=['--refund-staker',address]
        m.LocalFork=j.Fork
        m.rehearse=lambda f,s,i,d,refs:run(f,s,i,d,refs,span,following,a.node_rpc)
        return m.main()
    finally:sys.argv,m.LocalFork,m.rehearse=old

if __name__=='__main__':raise SystemExit(main())
