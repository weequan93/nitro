#!/usr/bin/env python3
"""Read-only refund/runtime checks. Does not read keystores or send transactions."""
import argparse
import sys
from pathlib import Path
from core import *
from recovery import safe_receipt
from collect import NodeRPC

def check(a):
    p,identity=load_package(a.package);require(p['kind']=='recovery' and p['purpose']=='production-review','Production recovery package required')
    proof=read(a.acceptance/'summary.json');require(proof['status']=='receipt_and_block_state_passed' and proof['packageIdentity']==identity,'Acceptance does not match package')
    r=RPC(a.parent_rpc);require(int(r.rpc('eth_chainId',[]),16)==42161,'Wrong parent chain');safe_receipt(r,p,proof['transaction'])
    live=inventory(r);require(live['wasm']==NEW and live['confirmed']>=p['recoveryNode'],'Recovery state not active')
    w=words(r.call(ROLLUP,'getNode(uint64)',p['recoveryNode'],tag=live['parentBlock']),12);require(w[11]==p['nodeHash'],'Recovery node changed')
    if a.address:
        addr=address(a.address);require(addr in REFUNDS,'Not an original refund account')
        credit=r.num(ROLLUP,'withdrawableFunds(address)',addr,tag=live['parentBlock'])
        if a.require_credit:
            require(credit>0,'No refund credit remaining')
            require(not live['paused'],'Refund withdrawal requires resumed Rollup')
            require(r.num(ROLLUP,'isValidator(address)',addr,tag=live['parentBlock'])==1,'Refund account no longer whitelisted')
        if a.expect_empty:require(credit==0,'Refund credit remains')
        report=dict(status='refund_credit_checked',address=addr,creditWei=str(credit),paused=live['paused'],packageIdentity=identity)
        if a.transaction:
            rec=r.rpc('eth_getTransactionReceipt',[b32(a.transaction)]);tx=r.rpc('eth_getTransactionByHash',[a.transaction]);require(rec and tx and int(rec['status'],16)==1,'Withdrawal failed/missing')
            require(tx['from'].lower()==addr and tx['to'].lower()==ROLLUP and tx['input']==encode('withdrawStakerFunds()') and int(tx.get('value','0x0'),16)==0 and int(tx['chainId'],16)==42161,'Wrong withdrawal transaction')
            require(r.rpc('eth_getBlockByNumber',[rec['blockNumber'],False])['hash']==rec['blockHash'],'Withdrawal reorg')
            # Verify the Rollup credit-clearing event; block balances alone cannot attribute payment with concurrent txs.
            event=keccak(b'UserWithdrawableFundsUpdated(address,uint256,uint256)');ev=[l for l in rec['logs'] if l['address'].lower()==ROLLUP and l['topics'][0]==event and len(l['topics'])>1 and '0x'+l['topics'][1][-40:]==addr]
            require(len(ev)==1 and int(words(ev[0]['data'],2)[0],16)>0 and int(words(ev[0]['data'],2)[1],16)==0,'Credit-clearing event missing')
            report.update(status='withdrawal_receipt_checked',transaction=a.transaction,withdrawnCreditWei=str(int(words(ev[0]['data'])[0],16)))
    else:
        require(a.node_rpc,'Runtime status requires --node-rpc')
        node=NodeRPC(a.node_rpc);require(int(node.rpc('eth_chainId',[]),16)==2886,'Wrong L3 chain')
        validated=node.rpc('arb_latestValidated',[]);gs=state(validated['GlobalState']);require(NEW in [v.lower() for v in validated['WasmRoots']] and position(gs)>=position(p['after']),'Runtime not validated past B under new root')
        stakes={x:r.num(ROLLUP,'isStaked(address)',x,tag=live['parentBlock']) for x in ACTIVE}
        report=dict(status='runtime_snapshot_collected',validated=validated,finalAccountStakeFlags=stakes,retiredStakeFlag=r.num(ROLLUP,'isStaked(address)',RETIRED,tag=live['parentBlock']),confirmed=live['confirmed'],created=live['created'],paused=live['paused'],packageIdentity=identity,note='One observation only. Compare later observations/ordinary receipts; no claim of sustained validation or real signer success.')
    report.update(checkedAt=now(),parentBlock=live['parentBlock'],readyForProduction=False)
    if a.out:save(a.out,report)
    print(json.dumps(report,indent=2))
if __name__=='__main__':
    p=argparse.ArgumentParser(description=__doc__);p.add_argument('--package',type=Path,required=True);p.add_argument('--acceptance',type=Path,required=True);p.add_argument('--parent-rpc',required=True);p.add_argument('--address');p.add_argument('--require-credit',action='store_true');p.add_argument('--expect-empty',action='store_true');p.add_argument('--transaction');p.add_argument('--node-rpc');p.add_argument('--out',type=Path)
    try:check(p.parse_args())
    except Exception as e:print('STOP:',str(e) if isinstance(e,ValueError) else type(e).__name__,file=sys.stderr);raise SystemExit(1)
