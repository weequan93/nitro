#!/usr/bin/env python3
"""Collect a pinned candidate and locate local A-prime; no chain writes or WASM replay."""
import argparse
import sys
from pathlib import Path
from core import *

class NodeRPC(RPC):
    READ=RPC.READ|{'eth_blockNumber','arb_findBatchContainingBlock','arbdebug_validationInputsAt','arb_latestValidated'}
class ParentRPC(RPC):
    READ=RPC.READ|{'eth_getLogs'}
NODE_CREATED='NodeCreated(uint64,bytes32,bytes32,bytes32,'+ASSERTION+',bytes32,bytes32,uint256)'

def confirmed_state(r,pre):
    tag=pre['parentBlock'];n=pre['confirmed'];creation=r.num(ROLLUP,'getNodeCreationBlockForLogLookup(uint64)',n,tag=tag)
    logs=r.rpc('eth_getLogs',[dict(address=ROLLUP,fromBlock=hex(creation),toBlock=hex(creation),topics=[keccak(NODE_CREATED.encode()),'0x'+word(n).hex()])])
    require(len(logs)==1,'Confirmed NodeCreated event not unique');v=words(logs[0]['data'],15)
    A=dict(BlockHash=v[6],SendRoot=v[7],Batch=int(v[8],16),PosInBatch=int(v[9],16),machineStatus=int(v[10],16));count=int(v[14],16)
    require(A['machineStatus']==1 and sh(state(A),count)==pre['confirmedStorage'][0],'Confirmed event/storage mismatch')
    return A,count,logs[0]

def collect(a):
    r=ParentRPC(a.parent_rpc);node=NodeRPC(a.node_rpc)
    require(int(r.rpc('eth_chainId',[]),16)==42161 and int(node.rpc('eth_chainId',[]),16)==2886,'Wrong chains')
    pre=inventory(r);require(pre['paused'] or a.simulation,'Production collection requires confirmed pause')
    A,count,event=confirmed_state(r,pre);h=node.rpc('eth_getBlockByNumber',['latest',False]);height=a.block if a.block is not None else int(h['number'],16)-64
    require(height>=0,'Invalid B height')
    # This deployment has zero block/message offset. Verify each inferred input against actual headers; fail closed otherwise.
    entry=node.rpc('arbdebug_validationInputsAt',[hex(height),'amd64']);B=state(entry['ExpectedEndState']);header=node.rpc('eth_getBlockByNumber',[hex(height),False])
    require(int(entry['Id'])==height and header['hash'].lower()==B['BlockHash'] and header['sendRoot'].lower()==B['SendRoot'] and header['parentHash'].lower()==state(entry['StartState'])['BlockHash'],'Block/message mapping mismatch')
    validate_pre(pre,state(A),B,count,a.simulation)
    low=max(0,height-a.lookback);high=height
    def batch(n):
        x=node.rpc('arb_findBatchContainingBlock',[n]);return int(x,16) if isinstance(x,str) and x.startswith('0x') else int(x)
    require(batch(low)<A['Batch']<=batch(high),'A batch not bracketed; increase lookback or review empty batches')
    while high-low>1:
        mid=(low+high)//2
        if batch(mid)<A['Batch']:low=mid
        else:high=mid
    require(batch(high)==A['Batch'] and batch(high-1)<A['Batch'],'A batch empty/inconsistent')
    local=high-1+A['PosInBatch'];require(0<=local<height,'Invalid A-prime height')
    ai=node.rpc('arbdebug_validationInputsAt',[hex(local),'amd64']);AP=state(ai['ExpectedEndState']);ah=node.rpc('eth_getBlockByNumber',[hex(local),False])
    require(int(ai['Id'])==local and position(AP)==position(A) and ah['hash'].lower()==AP['BlockHash'] and ah['sendRoot'].lower()==AP['SendRoot'] and ah['parentHash'].lower()==state(ai['StartState'])['BlockHash'],'Local A-prime mismatch')
    for n,s in ((local,AP),(height,B)):require(node.rpc('eth_getBlockByNumber',[hex(n),False])['hash'].lower()==s['BlockHash'],'L3 reorg')
    require(r.rpc('eth_getBlockByNumber',[pre['parentBlock'],False])['hash']==pre['parentBlockHash'],'Parent reorg')
    d=output(a.out);cand=d/'candidate';cand.mkdir();audit=d/'audit';audit.mkdir()
    save(cand/'summary.json',dict(parentBlock=pre['parentBlock'],confirmedNode=pre['confirmed'],oldConfirmedState=A,confirmedInboxMaxCount=count,confirmedStateHashMatches=True,checkpointBlock=height,checkpointMessage=height,checkpoint=B,newWasmRoot=NEW,bridgeInboxCount=pre['inboxCount'],readyForProduction=False))
    save(cand/'authority.json',dict(parentBlock=pre['parentBlock'],parentBlockHash=pre['parentBlockHash'],rollup=ROLLUP));save(cand/'checkpoint-input.json',entry);save(cand/'checkpoint-block.json',header)
    save(d/'inventory.json',pre);save(d/'confirmed-event.json',event);save(audit/'local-input.json',ai)
    save(audit/'summary.json',dict(status='message_span_audited',parentBlock=pre['parentBlock'],confirmedNode=pre['confirmed'],parentConfirmedState=A,checkpoint=B,checkpointBlock=height,checkpointMessage=height,localStateAtConfirmedPosition=AP,localBlockAtConfirmedPosition=local,localMessageAtConfirmedPosition=local,localMessageCountAtConfirmedPosition=local+1,checkpointMessageCount=height+1,positionalMessageSpan=height-local,confirmedGlobalStateMatchesLocal=state(A)==AP,executionReplayed=False,parentConfirmedAToBProven=False,productionNumBlocksApproved=False,readyForProduction=False))
    print('COLLECTED',d,'SPAN',height-local)
if __name__=='__main__':
    p=argparse.ArgumentParser(description=__doc__);p.add_argument('--parent-rpc',required=True);p.add_argument('--node-rpc',default='http://127.0.0.1:8349');p.add_argument('--block',type=int);p.add_argument('--lookback',type=int,default=100000);p.add_argument('--simulation',action='store_true');p.add_argument('--out',type=Path,required=True)
    try:collect(p.parse_args())
    except Exception as e:print('STOP:',str(e) if isinstance(e,ValueError) else type(e).__name__,file=sys.stderr);raise SystemExit(1)
