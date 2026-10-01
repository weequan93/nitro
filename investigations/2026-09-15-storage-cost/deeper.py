from probe import *
INBOX='0xe79283a775f6a1de250cb3284e7ff3541ff7668a'
NEW='0x1461469b43ad78145048eea11cbf6ba97222d379'; OLD='0x83ca1aa2bc20e41287154650e4161dc995278e1d'; OLD2='0x43948b78477963d7b408a0e27ae168584c6e07a9'
rows=json.loads((OUT/'boundaries.json').read_text())
def day(i):
 x,y=rows[i:i+2];d=x['day']; result={'day':d,'blocks':y['l3']-x['l3'],'samples':[]}
 for k in range(8):
  n=x['l3']+int((y['l3']-x['l3'])*(k+.5)/8); b=block(L3,n,True)
  recs=[]
  for tx in b['transactions']:
   if tx.get('to') not in [NEW,OLD,OLD2]:continue
   rec=rpc(L3,'eth_getTransactionReceipt',[tx['hash']]); inp=tx['input'][10:]; w=lambda a:int(inp[a*64:(a+1)*64],16)
   r={'hash':tx['hash'],'to':tx['to'],'inputBytes':(len(tx['input'])-2)//2,'selector':tx['input'][:10],'gas':int(rec['gasUsed'],16),'status':rec['status'],'logs':len(rec['logs']),'logBytes':sum((len(z['data'])-2)//2+32*len(z['topics']) for z in rec['logs']),'events':rec['logs']}
   if tx['to']==NEW:
    r['pricesCount']=w(w(0)//32);r['sigCount']=w(w(4)//32)
   recs.append(r)
  result['samples'].append({'block':n,'timestamp':int(b['timestamp'],16),'blockBytes':int(b['size'],16),'txCount':len(b['transactions']),'oracle':recs})
 (OUT/f'deeper-{d}.json').write_text(json.dumps(result,indent=2));print('done',d,flush=True)
with concurrent.futures.ThreadPoolExecutor(max_workers=4) as ex:list(ex.map(day,range(15)))
