#!/usr/bin/env python3
"""Historical private Anvil only: Safe batch success and late-failure rollback.
Uses canonical Safe MultiSendCallOnly v1.4.1, pinned runtime code hash.
Depends on rehearse-safe-recovery.py and the existing mock script.
Synthetic recovery numBlocks=1 remains a limitation, never production calldata.
"""
import importlib.util
import json
import subprocess
from pathlib import Path

spec=importlib.util.spec_from_file_location('safe_recovery',Path(__file__).with_name('rehearse-safe-recovery.py'))
safe=importlib.util.module_from_spec(spec);spec.loader.exec_module(safe)
m=safe.m
BATCH='0x9641d764fc13c8b624c04430c7356c1c7c8102e2'
CODEHASH='0xecd5bd14a08c5d2122379900b2f272bdf107a7e92423c10dd5fe3254386c9939'
SOURCE='https://raw.githubusercontent.com/safe-global/safe-deployments/main/src/assets/v1.4.1/multi_send_call_only.json'


def keccak(hexdata):
    return subprocess.check_output(['cast','keccak',hexdata],text=True,timeout=30).strip()


def pack(calls):
    # Outer Safe operation is delegatecall; each inner operation is CALL.
    return '0x'+''.join('00'+m.EXECUTOR[2:]+'00'*32+
        format(len(bytes.fromhex(data[2:])),'064x')+data[2:] for data in calls)


class ReferenceFork(m.LocalFork):
    def admin(self,label,signature,*args):
        self.captured.append((label,signature,args))
        super().admin(label,signature,*args)


def execute_batch(f,label,calls,expect_success):
    owners=sorted(safe.addresses(f.call(m.SAFE,'getOwners()')),key=lambda x:int(x,16))
    m.require(len(owners)==4 and int(f.call(m.SAFE,'getThreshold()'),16)==3,'Unexpected Safe threshold')
    nonce=int(f.call(m.SAFE,'nonce()'),16)
    # Negative test deliberately supplies safeTxGas so Safe records ExecutionFailure
    # while the inner MultiSend transaction rolls back. Positive uses zero.
    fields=[BATCH,0,m.encode('multiSend(bytes)',pack(calls)),1,
            8000000 if not expect_success else 0,0,0,safe.ZERO_ADDR,safe.ZERO_ADDR]
    txhash=f.call(m.SAFE,'getTransactionHash(address,uint256,bytes,uint8,uint256,uint256,uint256,address,address,uint256)',*fields,nonce)
    for owner in owners[:3]:
        f.impersonate(owner)
        f.send(label+' owner approveHash',owner,m.SAFE,m.encode('approveHash(bytes32)',txhash))
    signatures='0x'+''.join(x[2:].rjust(64,'0')+'00'*32+'01' for x in owners[:3])
    data=m.encode('execTransaction(address,uint256,bytes,uint8,uint256,uint256,uint256,address,address,bytes)',*fields,signatures)
    (f.output/(label+'-fork-only.json')).write_text(json.dumps(dict(chainId=31337,
        safe=m.SAFE,to=BATCH,operation=1,value=0,nonce=nonce,safeTxGas=fields[4],
        safeTransactionHash=txhash,calldata=data,readyForProduction=False),indent=2))
    f.send(label,owners[0],m.SAFE,data)
    receipt=f.steps[-1]['receipt']
    event='ExecutionSuccess(bytes32,uint256)' if expect_success else 'ExecutionFailure(bytes32,uint256)'
    topic=keccak('0x'+event.encode().hex())
    m.require(any(safe.success_event(x,topic,txhash) for x in receipt['logs']),event+' not found')
    m.require(int(f.call(m.SAFE,'nonce()'),16)==nonce+1,'Unexpected Safe nonce')
    return f.steps[-1]['forkTransaction']


def snapshot_state(f,refunds,checkpoint,parent_node):
    box='0x'+f.call(m.ROLLUP,'outbox()')[-40:]
    return dict(confirmed=f.number('latestConfirmed()'),created=f.number('latestNodeCreated()'),
        unresolved=f.number('firstUnresolvedNode()'),paused=f.number('paused()'),
        wasm=f.call(m.ROLLUP,'wasmModuleRoot()'),outbox=box,
        checkpointRootMapping=f.call(box,'roots(bytes32)',checkpoint['SendRoot']),
        stakerCount=f.number('stakerCount()'),
        stakers={x:{'raw':f.call(m.ROLLUP,'getStaker(address)',x),
                    'credit':f.number('withdrawableFunds(address)',x),
                    'zombie':f.number('isZombie(address)',x)} for x in refunds},
        parentNodeRaw=f.call(m.ROLLUP,'getNode(uint64)',parent_node),
        latestNodeRaw=f.call(m.ROLLUP,'getNode(uint64)',f.number('latestNodeCreated()')),
        nextNodeRaw=f.call(m.ROLLUP,'getNode(uint64)',f.number('latestNodeCreated()')+1))


def rehearse(f,summary,authority,directory,refunds):
    # First run the known sequential rehearsal inside an EVM snapshot to derive
    # the expected node hash, and verify all its existing pre/post conditions.
    m.require(f.rpc('eth_chainId',[])==hex(31337),'Not isolated fork')
    m.require('anvil' in f.rpc('web3_clientVersion',[]).lower(),'Not Anvil')
    block=f.rpc('eth_getBlockByNumber',[summary['parentBlock'],False])
    m.require(block and block['hash'].lower()==authority['parentBlockHash'].lower(),'Wrong anchor')
    actual=f.rpc('eth_getCode',[BATCH,'latest'])
    m.require(actual!='0x' and keccak(actual).lower()==CODEHASH,'MultiSendCallOnly runtime does not match official deployment')
    (f.output/'batch-component.json').write_text(json.dumps(dict(address=BATCH,codeHash=CODEHASH,source=SOURCE),indent=2))
    f.authorized=True
    snap=f.rpc('evm_snapshot',[])
    reference_dir=f.output/'reference';reference_dir.mkdir()
    ref=ReferenceFork(int(f.url.rsplit(':',1)[1]),f.process,reference_dir);ref.captured=[]
    reference=safe.original_rehearse(ref,summary,authority,directory,refunds)
    node=reference['recoveryNode']
    raw=f.call(m.ROLLUP,'getNode(uint64)',node)[2:]
    m.require(len(raw)==12*64,'Unexpected node ABI')
    expected_hash='0x'+raw[11*64:12*64]
    m.require(f.rpc('evm_revert',[snap]) is True,'Reference fork rollback failed')
    # Impersonation is Anvil metadata; explicitly turn off direct Safe impersonation.
    f.rpc('anvil_stopImpersonatingAccount',[m.SAFE])
    f.admin('separate governance pause','pause()')
    before=snapshot_state(f,refunds,summary['checkpoint'],summary['confirmedNode'])
    (f.output/'before-batch.json').write_text(json.dumps(before,indent=2))
    expected_signatures=['forceRefundStaker(address[])','setWasmModuleRoot(bytes32)',
        'forceCreateNode(uint64,uint256,'+m.ASSERTION+',bytes32)','forceConfirmNode(uint64,bytes32,bytes32)']
    ops=[(label,sig,list(args)) for label,sig,args in ref.captured if sig in expected_signatures]
    m.require([x[1] for x in ops]==expected_signatures,'Unexpected recovery operation order')
    ops[2][2][-1]=expected_hash  # Production-style exact guard, not the reference zero hash.
    calls=[m.encode('executeCall(address,bytes)',m.ROLLUP,m.encode(sig,*args)) for _,sig,args in ops]
    bad=[*calls]
    wrong_args=[node,m.ZERO,summary['checkpoint']['SendRoot']]
    bad[-1]=m.encode('executeCall(address,bytes)',m.ROLLUP,m.encode(expected_signatures[-1],*wrong_args))
    failure_tx=execute_batch(f,'late-failure-batch',bad,False)
    after_failure=snapshot_state(f,refunds,summary['checkpoint'],summary['confirmedNode'])
    m.require(before==after_failure,'Inner recovery state did not roll back')
    (f.output/'after-failure.json').write_text(json.dumps(after_failure,indent=2))
    print('PASS inner recovery changes rolled back after failed batch (Safe nonce consumed)',flush=True)
    success_tx=execute_batch(f,'successful-batch',calls,True)
    after=snapshot_state(f,refunds,summary['checkpoint'],summary['confirmedNode'])
    m.require(after['paused']==1 and after['confirmed']==node and after['created']==node,'Recovery/paused state mismatch')
    m.require(after['unresolved']==node+1 and after['wasm'].lower()==summary['newWasmRoot'].lower(),'Recovery pointer/root mismatch')
    m.require(after['outbox']==before['outbox'] and after['checkpointRootMapping'].lower()==summary['checkpoint']['BlockHash'].lower(),'Outbox mismatch')
    m.require(after['latestNodeRaw'][2+11*64:2+12*64].lower()==expected_hash[2:].lower(),'Recovery node hash mismatch')
    for addr in refunds:
        m.require(f.number('isStaked(address)',addr)==0,'Stake not removed')
        amount=int(before['stakers'][addr]['raw'][2:66],16)
        m.require(after['stakers'][addr]['credit']==before['stakers'][addr]['credit']+amount,'Refund credit mismatch')
    (f.output/'after-success.json').write_text(json.dumps(after,indent=2))
    return dict(status='contract_rehearsal_passed',test='atomic_safe_recovery',readyForProduction=False,
        recoveryNode=node,paused=True,batchAddress=BATCH,batchCodeHash=CODEHASH,
        exactExpectedNodeHash=expected_hash,innerRollbackTested=True,atomicSuccessTested=True,
        failureTransaction=failure_tx,successTransaction=success_tx,
        productionOwnerSignaturesTested=False,validatorRestartTested=False,fullWithdrawalHistoryAudited=False,
        limitations='Historical fork only; synthetic recovery numBlocks=1. Reference rehearsal uses direct Safe impersonation then reverts. Atomic tests use owner approvals and Safe delegatecall. Negative safeTxGas=8000000 records ExecutionFailure and consumes Safe nonce; positive safeTxGas=0. No production calldata or A-to-B proof.')


if __name__=='__main__':
    m.LocalFork=safe.SafeFork
    m.rehearse=rehearse
    raise SystemExit(m.main())
