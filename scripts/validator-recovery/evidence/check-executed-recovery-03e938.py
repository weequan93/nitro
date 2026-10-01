"""Read-only public RPC verification against the locally reviewed Safe fields."""
import json
import os
from pathlib import Path
import sys
root=Path(__file__).resolve().parents[1]
sys.path.insert(0,str(root/'toolkit'))
os.environ['PATH']=str(Path.home()/'.foundry/bin')+os.pathsep+os.environ['PATH']
from core import *
from recovery import safe_receipt, recovery_post, created_event
review_dir=root/'evidence/presign-window-20260929-154302-review'
review=read(review_dir/'review.json')
fields=read(review_dir/'recovery.safe-fields.json')
p=dict(safeFields=fields,nonce=11,expectedSafeTxHash=review['transactions'][0]['safeTxHash'],
       recoveryNode=review['recoveryNode'],nodeHash=review['recoveryNodeHash'],after=review['after'],
       pre=dict(confirmed=review['parentConfirmedNode'],stakers=[dict(address=a,amount=10**12,credit=0) for a in review['refundAccounts']]))
tx='0x03e938966a6b480e3b907b153e173bd3576b7b84c88a2e2387810441fc7d249e'
r=RPC('https://arb1.arbitrum.io/rpc')
require(int(r.rpc('eth_chainId',[]),16)==42161,'Wrong chain')
rec=safe_receipt(r,p,tx)
post_at_receipt=recovery_post(r,p,rec['blockNumber'])
actual_inbox=created_event(r,p,rec)
head=r.rpc('eth_getBlockByNumber',['latest',False])
post=recovery_post(r,p,head['number'])
nonce=r.num(SAFE,'nonce()',tag=head['number'])
require(nonce==12,'Unexpected current Safe nonce')
report=dict(status='public_rpc_recovery_receipt_and_state_matched',checkedAt=now(),transaction=tx,
            executedSafeNonce=11,expectedSafeTxHash=p['expectedSafeTxHash'],receiptBlock=int(rec['blockNumber'],16),
            receiptBlockHash=rec['blockHash'],checkedParentBlock=int(head['number'],16),
            currentSafeNonce=nonce,newWasmRoot=NEW,actualInboxMaxCount=actual_inbox,
            receiptPost=post_at_receipt,currentPost=post,readyForProduction=False,
            runtimeCutoverVerified=False,productionTransactionsSent=False,
            limitations='Compared with locally reviewed fields and expected three refund credits, not the complete server package file set. No local L3 RPC or runtime access; server package acceptance and runtime checks remain.')
out=root/'evidence/executed-recovery-03e938'
out.mkdir(exist_ok=True)
save(out/'receipt.json',rec);save(out/'summary.json',report)
print(json.dumps(report,indent=2))
