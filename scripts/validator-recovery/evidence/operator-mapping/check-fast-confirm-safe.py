"""Read-only fast confirmer and Safe roles inventory; no private keys or transactions."""
import json
import subprocess
from pathlib import Path
from datetime import datetime, timezone
RPC='https://arb1.arbitrum.io/rpc'
ROLLUP='0xa113e2e9620a3bc088a681ebb2c234fdbeb85e21'
FAST='0x5e16561173ea0422549c3de12b68c8a7d3a76672'
GOV='0xfbb37c66372f7b40361fbc8c8a235ae92711399d'
EXEC='0x1333480e92de9511dc9bb01f70901ff3ee94f613'
CAST=str(Path.home()/'.foundry/bin/cast')

def cast(*args):return subprocess.check_output([CAST,*map(str,args)],text=True).strip()
def rpc(method,params):
    assert method in {'eth_chainId','eth_getBlockByNumber','eth_call','eth_getCode','eth_getStorageAt'}
    raw=subprocess.check_output(['curl','--fail','-sS','--max-time','30',RPC,'-H','Content-Type: application/json',
        '--data-binary',json.dumps({'jsonrpc':'2.0','id':1,'method':method,'params':params})],text=True)
    obj=json.loads(raw)
    if 'error' in obj or obj.get('result') is None:raise ValueError(str(obj.get('error','null')))
    return obj['result']
assert int(rpc('eth_chainId',[]),16)==42161
head=rpc('eth_getBlockByNumber',['latest',False]);tag=head['number']
def call(to,sig,*args):return rpc('eth_call',[{'to':to,'data':cast('calldata',sig,*args)},tag])
def words(raw):
    b=bytes.fromhex(raw[2:]);assert len(b)%32==0
    return [b[i:i+32] for i in range(0,len(b),32)]
def arr(raw):
    w=words(raw);off=int.from_bytes(w[0],'big');assert off%32==0
    off//=32;n=int.from_bytes(w[off],'big');assert n<=100 and len(w)>=off+1+n
    return ['0x'+x[12:].hex() for x in w[off+1:off+1+n]]
code=rpc('eth_getCode',[FAST,tag]);assert code!='0x'
configured='0x'+call(ROLLUP,'anyTrustFastConfirmer()')[-40:]
report={'checkedAt':datetime.now(timezone.utc).isoformat(),'chainId':42161,'parentBlock':tag,
 'parentBlockHash':head['hash'],'fastConfirmSafe':FAST,'governanceSafe':GOV,
 'configuredFastConfirmer':configured,'matchesConfiguredFastConfirmer':configured.lower()==FAST,
 'safeCodeHash':cast('keccak',code),'owners':arr(call(FAST,'getOwners()')),
 'threshold':int(call(FAST,'getThreshold()'),16),'nonce':int(call(FAST,'nonce()'),16),
 'versionRaw':call(FAST,'VERSION()'), 'paused':bool(int(call(ROLLUP,'paused()'),16)),
 'fastSafeIsStaked':bool(int(call(ROLLUP,'isStaked(address)',FAST),16)),
 'transactionsSent':False,'readyForProduction':False,'errors':[]}
for label,slot in {'guard':'0x4a204f620c8c5ccdca3fd54d003badd85ba500436a431f0cbda4f558c93c34c8',
 'fallbackHandler':'0x6c9a6c4a39284e37ed1cf53d337577d14212a4870fb976a4366c693b939918d5',
 'singleton':'0x0'}.items():
    address='0x'+rpc('eth_getStorageAt',[FAST,slot,tag])[-40:]
    report[label]=address
    if label=='singleton':report['singletonCodeHash']=cast('keccak',rpc('eth_getCode',[address,tag]))
cursor='0x'+'0'*39+'1';modules=[]
for _ in range(20):
    raw=call(FAST,'getModulesPaginated(address,uint256)',cursor,50)
    page=arr(raw);nxt='0x'+words(raw)[1][12:].hex();modules+=page
    if int(nxt,16)==1:break
    assert page and nxt!=cursor;cursor=nxt
else:raise ValueError('Too many module pages')
report['modules']=modules
for role in ('EXECUTOR_ROLE()','ADMIN_ROLE()'):
    try:
        rolehash=call(EXEC,role)
        report['upgradeExecutor_'+role]=bool(int(call(EXEC,'hasRole(bytes32,address)',rolehash,FAST),16))
    except Exception as exc:report['errors'].append({'check':role,'errorType':type(exc).__name__})
assert rpc('eth_getBlockByNumber',[tag,False])['hash']==head['hash']
p=Path(__file__).with_name('fast-confirm-safe-'+datetime.now(timezone.utc).strftime('%Y%m%d-%H%M%S')+'.json')
p.write_text(json.dumps(report,indent=2)+'\n');print(json.dumps(report,indent=2));print('OUTPUT',p)
