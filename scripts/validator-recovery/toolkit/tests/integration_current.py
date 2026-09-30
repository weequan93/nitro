#!/usr/bin/env python3
"""Contract-only smoke fixture from current pending old-WASM assertions. NEVER production evidence."""
import argparse
import sys
from pathlib import Path
sys.path.insert(0,str(Path(__file__).resolve().parents[1]))
from core import *
from collect import ParentRPC,confirmed_state
from recovery import write_package

def main():
    ap=argparse.ArgumentParser();ap.add_argument('--parent-rpc',required=True);ap.add_argument('--out',type=Path,required=True);a=ap.parse_args()
    r=ParentRPC(a.parent_rpc);pre=inventory(r);A,count,event=confirmed_state(r,pre)
    require(pre['created']>pre['confirmed'] and pre['created']-pre['confirmed']<20,'Need finite pending chain for contract fixture')
    tip=dict(pre,confirmed=pre['created'],confirmedStorage=words(r.call(ROLLUP,'getNode(uint64)',pre['created'],tag=pre['parentBlock']),12))
    B,_,_=confirmed_state(r,tip);A=state(A);B=state(B);span=0;n=pre['created']
    while n>pre['confirmed']:
        node=words(r.call(ROLLUP,'getNode(uint64)',n,tag=pre['parentBlock']),12)
        _,_,ev=confirmed_state(r,dict(pre,confirmed=n,confirmedStorage=node));span+=int(words(ev['data'],15)[11],16);prev=int(node[3],16)
        require(pre['confirmed']<=prev<n,'Pending chain does not descend from confirmed');n=prev
    validate_pre(pre,A,B,count,True);needed=B['Batch']+bool(B['PosInBatch']);acc=r.call(BRIDGE,'sequencerInboxAccs(uint256)',needed-1,tag=pre['parentBlock'])
    txs,h=transactions(pre,A,B,span,count,acc);d=output(a.out)
    p=dict(kind='recovery',purpose='simulation-only',pre=pre,before=A,after=B,beforeInboxCount=count,numBlocks=span,accumulator=acc,nodeHash=h,recoveryNode=pre['created']+1,nonce=pre['safeNonce']+int(not pre['paused']),transactions=txs,decisionMode='test-fixture',simulatePause=not pre['paused'],limitations='Contract mechanics only: B comes from existing old-WASM pending assertions, not a proposed new-WASM recovery candidate. No WASM replay evidence. Never submit on production.')
    write_package(d,p,r);print('CONTRACT-ONLY TEST PACKAGE',d)
if __name__=='__main__':main()
