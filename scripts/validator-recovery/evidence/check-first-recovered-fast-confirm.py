import json,os,sys
from pathlib import Path
root=Path(__file__).resolve().parents[1]
sys.path.insert(0,str(root/'toolkit'))
os.environ['PATH']=str(Path.home()/'.foundry/bin')+os.pathsep+os.environ['PATH']
from core import *
r=RPC('https://arb1.arbitrum.io/rpc')
require(int(r.rpc('eth_chainId',[]),16)==42161,'Wrong chain')
head=r.rpc('eth_getBlockByNumber',['latest',False]);tag=head['number']
require('0x'+r.call(ROLLUP,'anyTrustFastConfirmer()',tag=tag)[-40:]==FAST,'Fast confirmer changed')
node=words(r.call(ROLLUP,'getNode(uint64)',43127,tag=tag),12)
nonce=r.num(FAST,'nonce()',tag=tag)
data=encode('fastConfirmNextNode(bytes32,bytes32,bytes32)','0x3d1b6da7f082beca6bd03d779a2a2cff75ef5fc6f8653957caf280097d21c312','0x9a0300bf873d9ce0ea8365ac9dd758c6023d13a883ed79e4027d6095838a4603',node[11])
safehash=r.call(FAST,TX_HASH,ROLLUP,0,data,0,0,0,0,ADDR0,ADDR0,nonce,tag=tag)
owners=words(r.call(FAST,'getOwners()',tag=tag));owners=['0x'+w[-40:] for w in owners[2:]]
report=dict(checkedAt=now(),parentBlock=int(tag,16),node=43127,nodeHash=node[11],fastSafe=FAST,fastSafeNonce=nonce,threshold=r.num(FAST,'getThreshold()',tag=tag),safeHash=safehash,approvals={a:r.num(FAST,'approvedHashes(address,bytes32)',a,safehash,tag=tag) for a in owners},latestConfirmed=r.num(ROLLUP,'latestConfirmed()',tag=tag),latestCreated=r.num(ROLLUP,'latestNodeCreated()',tag=tag),productionTransactionsSent=False)
(root/'evidence/first-recovered-fast-confirm.json').write_text(json.dumps(report,indent=2)+'\n')
print(json.dumps(report,indent=2))
