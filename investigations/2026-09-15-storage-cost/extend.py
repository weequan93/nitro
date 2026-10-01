from probe import *
INBOX='0xe79283a775f6a1de250cb3284e7ff3541ff7668a';TOPIC='0x7394f4a19a13c7b92b5bb71033245305946ef78452f7b4986ac1390b5df4ebd7'
rows=json.loads((OUT/'boundaries.json').read_text()); latest=block(L2,'latest'); stop=int(latest['timestamp'],16)
rows.append({'day':25,'timestamp':stop+1,'l2':int(latest['number'],16)+1,'l3':int(block(L3,'latest')['number'],16)+1})
(OUT/'extended-boundaries.json').write_text(json.dumps(rows,indent=2))
def day(i):
 a,b=rows[i:i+2];d=a['day'];path=OUT/f'batchlogs-{d}.json'
 if path.exists() and d!=24: logs=json.loads(path.read_text())
 else:
  logs=rpc(L2,'eth_getLogs',[{'address':INBOX,'topics':[TOPIC],'fromBlock':hex(a['l2']),'toBlock':hex(b['l2']-1)}]);path.write_text(json.dumps(logs))
 ls=[x for x in logs if x['topics'][0]==TOPIC]
 out=[]
 for k in range(min(24,len(ls))):
  e=ls[int((k+.5)*len(ls)/min(24,len(ls)))];tx=rpc(L2,'eth_getTransactionByHash',[e['transactionHash']]);r=rpc(L2,'eth_getTransactionReceipt',[e['transactionHash']]);bl=block(L2,int(e['blockNumber'],16));inp=tx['input'][2:];off=int(inp[72:136],16)*2+8
  out.append({'hash':e['transactionHash'],'block':int(e['blockNumber'],16),'timestamp':int(bl['timestamp'],16),'gas':int(r['gasUsed'],16),'price':int(r['effectiveGasPrice'],16),'baseFee':int(bl['baseFeePerGas'],16),'maxFee':int(tx.get('maxFeePerGas','0x0'),16),'maxPriority':int(tx.get('maxPriorityFeePerGas','0x0'),16),'feeETH':int(r['gasUsed'],16)*int(r['effectiveGasPrice'],16)/1e18,'dataBytes':int(inp[off:off+64],16),'header':inp[off+64:off+66],'from':tx['from'],'messages':int(inp[328:392],16)-int(inp[264:328],16)})
 result={'day':d,'start':a['timestamp'],'endExclusive':b['timestamp'],'partial':d==24,'batches':len(ls),'firstSeq':int(ls[0]['topics'][1],16) if ls else None,'lastSeq':int(ls[-1]['topics'][1],16) if ls else None,'samples':out};(OUT/f'extended-fees-{d}.json').write_text(json.dumps(result,indent=2));print(d,len(ls),sum(x['feeETH'] for x in out)/len(out),flush=True)
with concurrent.futures.ThreadPoolExecutor(max_workers=3) as ex:list(ex.map(day,range(16)))
