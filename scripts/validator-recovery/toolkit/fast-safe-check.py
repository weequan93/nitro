#!/usr/bin/env python3
"""Read-only fast-confirmation Safe identity check; cannot enumerate offline signatures."""
import argparse
import sys
from core import *

def check(a):
    r=RPC(a.parent_rpc);require(int(r.rpc('eth_chainId',[]),16)==42161,'Wrong parent chain')
    block=r.rpc('eth_getBlockByNumber',['latest',False]);tag=block['number']
    def storage(k):return '0x'+r.rpc('eth_getStorageAt',[FAST,k,tag])[-40:].lower()
    require('0x'+r.call(ROLLUP,'anyTrustFastConfirmer()',tag=tag)[-40:]==FAST,'Fast confirmer changed')
    singleton='0x3e5c63644e683549055b9be8653de26e0b4cd36e'
    require(storage('0x0')==singleton,'Fast singleton changed')
    codes={FAST:'0xb89c1b3bdf2cf8827818646bce9a8f6e372885f8c55e5c07acbd307cb133b000',singleton:'0x21842597390c4c6e3c1239e434a682b054bd9548eee5e9b1d6a4482731023c0f'}
    for addr,h in codes.items():require(keccak(raw(r.rpc('eth_getCode',[addr,tag])))==h,'Fast code identity changed')
    ow=words(r.call(FAST,'getOwners()',tag=tag));require(len(ow)==5 and int(ow[0],16)==32 and int(ow[1],16)==3,'Fast owners ABI differs')
    require(sorted('0x'+v[-40:] for v in ow[2:])==sorted(ACTIVE) and r.num(FAST,'getThreshold()',tag=tag)==3,'Fast owners/threshold changed')
    require(storage(GUARD)==ADDR0 and storage(FALLBACK)=='0xf48f2b2d2a534e402487b3ee7c18c33aec0fe5e4','Fast guard/fallback changed')
    require(r.call(FAST,'getModulesPaginated(address,uint256)','0x'+'0'*39+'1',100,tag=tag)=='0x'+word(64).hex()+word(1).hex()+word(0).hex(),'Fast modules changed')
    require(r.rpc('eth_getBlockByNumber',[tag,False])['hash']==block['hash'],'Parent reorg')
    report=dict(status='fast_safe_identity_passed',parentBlock=tag,parentBlockHash=block['hash'],safe=FAST,owners=ACTIVE,threshold=3,nonce=r.num(FAST,'nonce()',tag=tag),readyForProduction=False,queueAndOfflineApprovalsReviewed=False,note='Coordinate pending fast-confirmation proposals/approved hashes separately. No signatures or transactions.')
    require(not a.out.exists(),'Output already exists');save(a.out,report);print(json.dumps(report,indent=2))
if __name__=='__main__':
    p=argparse.ArgumentParser(description=__doc__);p.add_argument('--parent-rpc',required=True);p.add_argument('--out',type=Path,required=True)
    try:check(p.parse_args())
    except Exception as e:print('STOP:',str(e) if isinstance(e,ValueError) else type(e).__name__,file=sys.stderr);raise SystemExit(1)
