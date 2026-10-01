from probe import *
TOGGLE=508190474
logs=json.loads((OUT/'batchlogs-23.json').read_text()); ls=[x for x in logs if x['topics'][0].startswith('0x7394f4')]
before=max((x for x in ls if int(x['blockNumber'],16)<TOGGLE),key=lambda x:int(x['blockNumber'],16));after=min((x for x in ls if int(x['blockNumber'],16)>TOGGLE),key=lambda x:int(x['blockNumber'],16));out=[]
for e in [before,after]:
 tx=rpc(L2,'eth_getTransactionByHash',[e['transactionHash']]);r=rpc(L2,'eth_getTransactionReceipt',[e['transactionHash']]);b=block(L2,int(e['blockNumber'],16));out.append({'transaction':tx,'receipt':r,'block':{k:b[k] for k in ['number','timestamp','baseFeePerGas']}})
 print(e['transactionHash'],int(b['timestamp'],16),int(r['effectiveGasPrice'],16),int(b['baseFeePerGas'],16),int(tx['maxPriorityFeePerGas'],16),int(r['gasUsed'],16),flush=True)
(OUT/'tip-before-after.json').write_text(json.dumps(out,indent=2))
j=json.loads((OUT/'oracle-first-transactions.json').read_text()); hashes=[x['hash'] for x in j['result'][:14] if not x['input'].startswith('0x6080')]; details=[]
for h in hashes:
 tx=rpc(L3,'eth_getTransactionByHash',[h]);r=rpc(L3,'eth_getTransactionReceipt',[h]);b=block(L3,int(tx['blockNumber'],16)); details.append({'transaction':tx,'receipt':r,'timestamp':int(b['timestamp'],16)})
(OUT/'oracle-activation-receipts.json').write_text(json.dumps(details,indent=2));print('saved oracle receipts',len(details),flush=True)
