"""Read-only public Arbitrum One address-to-stake comparison. No keys or writes."""
import json
import subprocess
from pathlib import Path
from datetime import datetime, timezone

RPC='https://arb1.arbitrum.io/rpc'
ROLLUP='0xa113e2e9620a3bc088a681ebb2c234fdbeb85e21'
ADDRESSES=[
 '0x65fa6c1d1efc338dfe8f07d5de84b3e7b76cbc95',
 '0x5cda45a9ae0e52f1d5110dc3819f6fb96bade33a',
 '0x21d4ea822a07f737c5e69f7951d517e5f2974849',
 '0xd38e969ae2947019e0dec0e46937e6aff651834d']
CAST=str(Path.home()/'.foundry/bin/cast')

def rpc(method,params):
    assert method in {'eth_chainId','eth_getBlockByNumber','eth_call','eth_getCode'}
    p=subprocess.run(['curl','--fail','-sS','--max-time','30',RPC,'-H','Content-Type: application/json',
       '--data-binary',json.dumps({'jsonrpc':'2.0','id':1,'method':method,'params':params})],
       capture_output=True,text=True,check=True)
    obj=json.loads(p.stdout)
    if 'error' in obj or obj.get('result') is None:raise ValueError(str(obj.get('error','null')))
    return obj['result']
assert int(rpc('eth_chainId',[]),16)==42161
head=rpc('eth_getBlockByNumber',['latest',False]);tag=head['number']
def call(sig,*args):
    data=subprocess.check_output([CAST,'calldata',sig,*map(str,args)],text=True).strip()
    return rpc('eth_call',[{'to':ROLLUP,'data':data},tag])
def number(sig,*args):return int(call(sig,*args),16)
count=number('stakerCount()');assert count<100
active=['0x'+call('getStakerAddress(uint64)',i)[-40:] for i in range(count)]
entries=[]
for address in dict.fromkeys(ADDRESSES+active):
    raw=call('getStaker(address)',address)[2:];assert len(raw)==5*64
    words=[int(raw[i:i+64],16) for i in range(0,len(raw),64)]
    entries.append({'address':address,'hasCode':rpc('eth_getCode',[address,tag])!='0x',
      'amountStakedWei':str(words[0]),'index':words[1],'latestStakedNode':words[2],
      'currentChallenge':words[3],'isStaked':bool(words[4]),
      'whitelisted':bool(number('isValidator(address)',address)),
      'isZombie':bool(number('isZombie(address)',address)),
      'withdrawableWei':str(number('withdrawableFunds(address)',address))})
assert rpc('eth_getBlockByNumber',[tag,False])['hash']==head['hash']
result={'checkedAt':datetime.now(timezone.utc).isoformat(),'parentChainId':42161,
 'parentBlock':tag,'parentBlockHash':head['hash'],'rollup':ROLLUP,
 'activeStakerCount':count,'activeStakers':active,'addresses':entries,'transactionsSent':False}
p=Path(__file__).with_name('address-check-'+datetime.now(timezone.utc).strftime('%Y%m%d-%H%M%S')+'.json')
p.write_text(json.dumps(result,indent=2)+'\n');print(json.dumps(result,indent=2));print('OUTPUT',p)
