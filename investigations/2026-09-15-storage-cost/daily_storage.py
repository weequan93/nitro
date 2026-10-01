from probe import *
base=json.loads((OUT/'storage-counts.json').read_text());keys=json.loads((OUT/'storage-verification.json').read_text())['keys'];bounds=json.loads((OUT/'boundaries.json').read_text());rows=[]
for day in range(16,25):
 n=next(x['l3'] for x in bounds if x['day']==day);vals=[]
 if day==16:vals=base[0]['tokens']
 else:
  pairs=list(keys.items())
  for a in range(0,len(pairs),20):
   chunk=pairs[a:a+20];payload=[{'jsonrpc':'2.0','id':i,'method':'eth_getStorageAt','params':['0x1461469b43ad78145048eea11cbf6ba97222d379',key,hex(n)]} for i,(t,key) in enumerate(chunk)]
   raw=json.loads(subprocess.check_output(['curl','-sS','--fail','--max-time','45',L3,'-H','Content-Type: application/json','--data',json.dumps(payload)]));lookup={x['id']:x for x in raw}
   for i,(t,key) in enumerate(chunk):
    z=lookup[i]
    if 'error' in z:
     vals=None;break
    vals.append({'token':t,'priceId':int(z['result'],16)})
   if vals is None:break
  time.sleep(.15)
 if vals is None:
  print('unavailable',day,flush=True);continue
 r={'day':day,'block':n,'timestamp':next(x['timestamp'] for x in bounds if x['day']==day),'tokens':vals,'sumPriceIds':sum(x['priceId'] for x in vals)};rows.append(r);(OUT/'daily-storage.json').write_text(json.dumps(rows,indent=2));print(day,r['sumPriceIds'],flush=True)
(OUT/'daily-storage.json').write_text(json.dumps(rows,indent=2))
