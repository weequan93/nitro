"""Read-only verification of cleanup receipt and a pinned current parent state."""
import concurrent.futures
import json
import os
from pathlib import Path
import sys
root = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(root/'toolkit'))
os.environ['PATH'] = str(Path.home()/'.foundry/bin') + os.pathsep + os.environ['PATH']
from core import RPC, RPCRejected, encode, now, require, NEW, ROLLUP, SAFE, words
r = RPC('https://arb1.arbitrum.io/rpc')
txhash = '0x57531e3cfa42afae4b76fb9dd36f237174fca48222a130b14310e250b92087c6'
account = '0x65fa6c1d1efc338dfe8f07d5de84b3e7b76cbc95'
require(int(r.rpc('eth_chainId', []),16) == 42161, 'Wrong chain')
with concurrent.futures.ThreadPoolExecutor() as ex:
    recf = ex.submit(r.rpc, 'eth_getTransactionReceipt', [txhash])
    txf = ex.submit(r.rpc, 'eth_getTransactionByHash', [txhash])
    headf = ex.submit(r.rpc, 'eth_getBlockByNumber', ['latest',False])
    rec, tx, head = recf.result(), txf.result(), headf.result()
require(rec and int(rec['status'],16)==1, 'Receipt not successful')
require(tx['from'].lower()==account and tx['to'].lower()==ROLLUP and int(tx['value'],16)==0 and int(tx['chainId'],16)==42161, 'Wrong sender/target/value/chain')
require(tx['input'].lower()==encode('removeOldZombies(uint256)',0), 'Wrong calldata')
require(r.rpc('eth_getBlockByNumber',[rec['blockNumber'],False])['hash']==rec['blockHash'], 'Receipt not canonical')
tag = head['number']
addresses = [account, '0x5cda45a9ae0e52f1d5110dc3819f6fb96bade33a', '0x21d4ea822a07f737c5e69f7951d517e5f2974849', '0xd38e969ae2947019e0dec0e46937e6aff651834d']
queries = {name:(ROLLUP,sig,[]) for name,sig in [('paused','paused()'),('wasm','wasmModuleRoot()'),('zombieCount','zombieCount()'),('confirmed','latestConfirmed()'),('created','latestNodeCreated()'),('stakerCount','stakerCount()'),('requiredStake','currentRequiredStake()'),('stakeToken','stakeToken()'),('whitelistDisabled','validatorWhitelistDisabled()')]}
queries['safeNonce']=(SAFE,'nonce()',[])
queries['recoveryNode']=(ROLLUP,'getNode(uint64)',[43126])
for a in addresses:
    for field,sig in [('zombie','isZombie(address)'),('staked','isStaked(address)'),('whitelisted','isValidator(address)'),('refundCredit','withdrawableFunds(address)'),('stakeInfo','getStaker(address)')]:
        queries[a+':'+field]=(ROLLUP,sig,[a])
def query(item):
    name,(to,sig,args)=item
    try: return name,r.call(to,sig,*args,tag=tag)
    except RPCRejected as e: return name,{'error':e.detail}
with concurrent.futures.ThreadPoolExecutor(max_workers=6) as ex:
    results=dict(ex.map(query, queries.items()))
    balances=dict(zip(addresses,ex.map(lambda a:r.rpc('eth_getBalance',[a,tag]),addresses)))
def number(k):
    v=results[k]
    return v if isinstance(v,dict) else int(v,16)
report={'checkedAt':now(),'transaction':txhash,'receiptVerified':True,'function':'removeOldZombies(0)','sender':account,'receiptBlock':int(rec['blockNumber'],16),'receiptBlockHash':rec['blockHash'],'checkedParentBlock':int(tag,16),'checkedParentBlockHash':head['hash'],'gasUsed':int(rec['gasUsed'],16),'feeWei':int(rec['gasUsed'],16)*int(rec['effectiveGasPrice'],16),'state':{k:number(k) for k in ['paused','zombieCount','confirmed','created','stakerCount','requiredStake','stakeToken','whitelistDisabled','safeNonce']},'wasm':results['wasm'],'newWasmMatches':results['wasm']==NEW,'accounts':{},'productionTransactionsSent':False,'localRuntimeChecked':False}
for a in addresses:
    entry={field:number(a+':'+field) for field in ['zombie','staked','whitelisted','refundCredit']}
    entry['balanceWei']=int(balances[a],16)
    info=results[a+':stakeInfo']
    if not isinstance(info,dict):
        w=words(info,5);entry['latestStakedNode']=int(w[2],16);entry['currentChallenge']=int(w[3],16)
    report['accounts'][a]=entry
node=results['recoveryNode']
report['recoveryNodeHash']=node if isinstance(node,dict) else words(node,12)[11]
require(r.rpc('eth_getBlockByNumber',[tag,False])['hash']==head['hash'],'Pinned state reorged')
out=Path(__file__).resolve().parent
for name,value in [('receipt.json',rec),('transaction.json',tx),('raw-state.json',results),('summary.json',report)]:
    (out/name).write_text(json.dumps(value,indent=2)+'\n')
print(json.dumps(report,indent=2))
