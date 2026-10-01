from probe import *
r=json.loads((OUT/'storage-counts.json').read_text());keys={t['token']:subprocess.check_output(['cast','keccak','0x'+t['token'][2:].zfill(64)+hex(11)[2:].zfill(64)],text=True).strip() for t in r[0]['tokens']}
for day in r:
 vals=day['tokens']
 for a in range(0,len(vals),20):
  items=vals[a:a+20];p=[{'jsonrpc':'2.0','id':i,'method':'eth_getStorageAt','params':['0x1461469b43ad78145048eea11cbf6ba97222d379',keys[t['token']],hex(day['block'])]} for i,t in enumerate(items)]
  raw=json.loads(subprocess.check_output(['curl','-sS','--fail','--max-time','45',L3,'-H','Content-Type: application/json','--data',json.dumps(p)]));lookup={x['id']:x for x in raw}
  for i,t in enumerate(items):assert int(lookup[i]['result'],16)==t['priceId'],(t,lookup[i])
 print('verified direct storage',day['label'],flush=True)
(OUT/'storage-verification.json').write_text(json.dumps({'mappingSlot':11,'uniqueKeys':len(set(keys.values())),'allGetterValuesMatchDirectStorage':True,'keys':keys},indent=2))
