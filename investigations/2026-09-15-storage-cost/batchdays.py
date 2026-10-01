from probe import *
INBOX='0xe79283a775f6a1de250cb3284e7ff3541ff7668a'
r=json.loads((OUT/'boundaries.json').read_text())
def get(i):
 a,b=r[i:i+2];logs=rpc(L2,'eth_getLogs',[{'address':INBOX,'fromBlock':hex(a['l2']),'toBlock':hex(b['l2']-1)}]); (OUT/f'batchlogs-{a["day"]}.json').write_text(json.dumps(logs));print(a['day'],len(logs),flush=True)
with concurrent.futures.ThreadPoolExecutor(max_workers=3) as ex:list(ex.map(get,[2,5,6,7,8]))
