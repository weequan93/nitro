from probe import *
NEW='0x1461469b43ad78145048eea11cbf6ba97222d379'; tokens=json.loads((OUT/'observed-oracle-tokens.json').read_text());sel=subprocess.check_output(['cast','sig','getPriceId(address)'],text=True).strip(); rows=json.loads((OUT/'extended-boundaries.json').read_text())
out=[]
for name,n in [('sep16',next(x['l3'] for x in rows if x['day']==16)),('sep24_snapshot',rows[-1]['l3']-1)]:
 vals=[]
 for start in range(0,len(tokens),20):
  ts=tokens[start:start+20];payload=[{'jsonrpc':'2.0','id':i,'method':'eth_call','params':[{'to':NEW,'data':sel+t[2:].zfill(64)},hex(n)]} for i,t in enumerate(ts)]
  raw=json.loads(subprocess.check_output(['curl','-sS','--fail','--max-time','45',L3,'-H','Content-Type: application/json','--data',json.dumps(payload)]));lookup={x['id']:x for x in raw}
  for i,t in enumerate(ts):
   z=lookup[i]
   if 'error' in z: raise RuntimeError(z)
   vals.append({'token':t,'priceId':int(z['result'],16)})
 result={'label':name,'block':n,'timestamp':int(block(L3,n)['timestamp'],16),'tokens':vals,'sumPriceIds':sum(x['priceId'] for x in vals)};out.append(result);print(name,len(vals),result['sumPriceIds'],flush=True)
(OUT/'storage-counts.json').write_text(json.dumps(out,indent=2))
