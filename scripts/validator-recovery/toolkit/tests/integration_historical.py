#!/usr/bin/env python3
"""Test-only historical fixture, not a production package generator or replay."""
import argparse
import sys
from pathlib import Path
sys.path.insert(0,str(Path(__file__).resolve().parents[1]))
from core import *
from recovery import write_package

def main():
    ap=argparse.ArgumentParser();ap.add_argument('--candidate',type=Path,required=True);ap.add_argument('--parent-rpc',required=True);ap.add_argument('--out',type=Path,required=True);a=ap.parse_args()
    c=read(a.candidate);c=c.get('report',c);r=RPC(a.parent_rpc);pre=inventory(r,c['parentBlock']);A=state(c['oldConfirmedState']);B=state(c['checkpoint']);count=c['confirmedInboxMaxCount'];span=10578
    validate_pre(pre,A,B,count,True);acc=r.call(BRIDGE,'sequencerInboxAccs(uint256)',B['Batch'],tag=pre['parentBlock']);txs,h=transactions(pre,A,B,span,count,acc)
    require(h=='0xd86bb1e3277e2cf5565f945d2954f670f4b4dc88df11f1d53359b706fd5f5da2','Historical commitment regression')
    d=output(a.out);p=dict(kind='recovery',purpose='simulation-only',pre=pre,before=A,after=B,beforeInboxCount=count,numBlocks=span,accumulator=acc,nodeHash=h,recoveryNode=pre['created']+1,nonce=pre['safeNonce']+1,transactions=txs,decisionMode='test-fixture',simulatePause=True,checkpointBlock=c['checkpointBlock'],limitations='Only historical contract fixture; saved replay records not present on this machine. No production package.')
    write_package(d,p,r);print('HISTORICAL TEST PACKAGE',d)
if __name__=='__main__':main()
