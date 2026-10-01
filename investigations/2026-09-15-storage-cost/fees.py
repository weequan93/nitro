from probe import *
def day(d):
 logs=json.loads((OUT/f'batchlogs-{d}.json').read_text());ls=[x for x in logs if x['topics'][0].startswith('0x7394f4')]; out=[]
 for i in range(24):
  e=ls[int((i+.5)*len(ls)/24)]; tx=rpc(L2,'eth_getTransactionByHash',[e['transactionHash']]);r=rpc(L2,'eth_getTransactionReceipt',[e['transactionHash']]); b=block(L2,int(e['blockNumber'],16));inp=tx['input'][2:];off=int(inp[72:136],16)*2+8; size=int(inp[off:off+64],16)
  out.append({'hash':e['transactionHash'],'timestamp':int(b['timestamp'],16),'gas':int(r['gasUsed'],16),'price':int(r['effectiveGasPrice'],16),'feeETH':int(r['gasUsed'],16)*int(r['effectiveGasPrice'],16)/1e18,'dataBytes':size,'header':inp[off+64:off+66],'from':tx['from']})
 result={'day':d,'batches':len(ls),'samples':out};(OUT/f'fees-{d}.json').write_text(json.dumps(result,indent=2));print(d,len(ls),sum(x['feeETH'] for x in out)/24,flush=True)
with concurrent.futures.ThreadPoolExecutor(max_workers=3) as ex:list(ex.map(day,[11,14,15,16,17]))
for d,n in [(14,122034784),(16,122698309),(23,124965573)]:
 try:print('impl',d,rpc(L3,'eth_getStorageAt',['0x1461469b43ad78145048eea11cbf6ba97222d379','0x2',hex(n)]),flush=True)
 except Exception as e:print('impl error',str(e),flush=True)
