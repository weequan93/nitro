import subprocess,json,urllib.request,datetime,concurrent.futures,pathlib,time
OUT=pathlib.Path('/tmp/deriw-cost-investigation')
OUT.mkdir(parents=True, exist_ok=True)
L3='https://rpc.deriw.com'; L2='https://arb1.arbitrum.io/rpc'
def rpc(url,method,params):
 for attempt in range(4):
  try:
   req=urllib.request.Request(url,json.dumps({'jsonrpc':'2.0','id':1,'method':method,'params':params}).encode(),{'Content-Type':'application/json'})
   data=json.loads(subprocess.check_output(['curl','-sS','--fail','--max-time','45',url,'-H','Content-Type: application/json','--data',req.data.decode()]))
   if 'error' in data: raise RuntimeError(data['error'])
   return data['result']
  except Exception:
   if attempt==3: raise
   time.sleep(1)
def block(url,n,full=False): return rpc(url,'eth_getBlockByNumber',[hex(n) if isinstance(n,int) else n,full])
def boundary(url,t):
 latest=block(url,'latest'); lo=0; hi=int(latest['number'],16); ht=int(latest['timestamp'],16); lt=int(block(url,0)['timestamp'],16)
 for i in range(40):
  if hi-lo<=1: return hi
  n=max(lo+1,min(hi-1,lo+int((t-lt)*(hi-lo)/(ht-lt))))
  n=(lo+hi)//2 if i>4 else n
  b=block(url,n); bt=int(b['timestamp'],16)
  if bt<t:lo,lt=n,bt
  else:hi,ht=n,bt
 raise RuntimeError('boundary')
if __name__=='__main__':
 inbox=rpc(L2,'eth_call',[{'to':'0xa113e2e9620a3bc088a681ebb2c234fdbeb85e21','data':'0xee35f327'},'latest'])
 print('inbox','0x'+inbox[-40:],flush=True); (OUT/'inbox.json').write_text(json.dumps(inbox))
 def get(d):
  t=int(datetime.datetime(2026,9,d,tzinfo=datetime.timezone.utc).timestamp())
  row={'day':d,'timestamp':t,'l3':boundary(L3,t),'l2':boundary(L2,t)}
  print(row,flush=True);return row
 with concurrent.futures.ThreadPoolExecutor(max_workers=4) as ex: rows=list(ex.map(get,range(9,25)))
 (OUT/'boundaries.json').write_text(json.dumps(rows,indent=2))
