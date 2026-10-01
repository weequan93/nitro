"""Deriw preparation primitives. Public clients have an explicit read-only RPC allowlist."""
import hashlib
import json
import os
import re
import subprocess
from datetime import datetime, timezone
from pathlib import Path
from urllib.parse import urlsplit

ROLLUP='0xa113e2e9620a3bc088a681ebb2c234fdbeb85e21'
EXEC='0x1333480e92de9511dc9bb01f70901ff3ee94f613'
SAFE='0xfbb37c66372f7b40361fbc8c8a235ae92711399d'
FAST='0x5e16561173ea0422549c3de12b68c8a7d3a76672'
BRIDGE='0x53a7559d1e57e371f3d1e55fea97e9b6748418a3'
OUTBOX='0x47da6c41d03ac0608924e86f61577df558114bd8'
MULTI='0x9641d764fc13c8b624c04430c7356c1c7c8102e2'
ACTIVE=['0x65fa6c1d1efc338dfe8f07d5de84b3e7b76cbc95','0x5cda45a9ae0e52f1d5110dc3819f6fb96bade33a','0x21d4ea822a07f737c5e69f7951d517e5f2974849']
RETIRED='0xd38e969ae2947019e0dec0e46937e6aff651834d'
REFUNDS=[ACTIVE[1],RETIRED,ACTIVE[0]]
OWNERS=sorted(['0xa0c2aed24f5474b2815b2ff61d0f5a01970217c3','0xc60f0ed09edd696e60574f714cbd7cfec004dd70','0xc63b7a2dacfa3aed4ea158f4f51ffbe020b9de4c','0x09ad976b259d9174f4250f0244873c3bc876e2ce'])
OLD='0x767c9a47cced7ccc3bf419a7efdd9ffb0f23a5dba42f30f3de64f32e2f82c55f'
NEW='0x121d685e2fdb0e3291592d6b90bd70d503951335d19d96455448eb7a14d17421'
ZERO='0x'+'00'*32
ADDR0='0x'+'00'*20
ASSERTION='(((bytes32[2],uint64[2]),uint8),((bytes32[2],uint64[2]),uint8),uint64)'
CREATE='forceCreateNode(uint64,uint256,'+ASSERTION+',bytes32)'
EXEC_TX='execTransaction(address,uint256,bytes,uint8,uint256,uint256,uint256,address,address,bytes)'
TX_HASH='getTransactionHash(address,uint256,bytes,uint8,uint256,uint256,uint256,address,address,uint256)'
CODES={SAFE:'0xd7d408ebcd99b2b70be43e20253d6d92a8ea8fab29bd3be7f55b10032331fb4c',
 EXEC:'0x8736329b580cfc0c0c39ee6700515e0bc51652afb614640db9e34a5d784933e8',
 MULTI:'0xecd5bd14a08c5d2122379900b2f272bdf107a7e92423c10dd5fe3254386c9939',
 '0x29fcb43b46531bca003ddc8fcb67ffe91900c762':'0xb1f926978a0f44a2c0ec8fe822418ae969bd8c3f18d61e5103100339894f81ff',
 '0x12b1389fbf261e781bdc3094d28636abfb03c5b3':'0x0d88feac198ef1b50b99fddf06aa9f6b1050bfe7211d6f04173de9b6d8953bcb',
 '0xf9725312bd91ccfa3ad797e78a8a10b6d692fcd6':'0x8cf117fd02db7f12da04db8ac71d302b4a34dffbc17c629b4f7aa6cd5ffcacc5',
 '0xf916bfe431b7a7aae083273f5b862e00a15d60f4':'0x8bf14ad1722eceef8f77dfdffb79393a42662fdce14d957db1e323084a8145c1'}
GUARD='0x4a204f620c8c5ccdca3fd54d003badd85ba500436a431f0cbda4f558c93c34c8'
FALLBACK='0x6c9a6c4a39284e37ed1cf53d337577d14212a4870fb976a4366c693b939918d5'

def require(ok, msg):
    if not ok: raise ValueError(msg)
def now(): return datetime.now(timezone.utc).isoformat()
def read(p): return json.loads(Path(p).read_text())
def digest(o): return hashlib.sha256(json.dumps(o,sort_keys=True,separators=(',',':')).encode()).hexdigest()
def filehash(p): return hashlib.sha256(Path(p).read_bytes()).hexdigest()
def save(p,o): Path(p).write_text(json.dumps(o,indent=2)+'\n')
def output(p):
    p=Path(p).resolve()
    require(not any(p==x or x in p.parents for x in [Path('/data'),Path('/data_mock'),Path('/data_new/validator')]),'Protected output path')
    p.mkdir(parents=True,exist_ok=False)
    return p

def cast(*args):
    r=subprocess.run(['cast',*map(str,args)],capture_output=True,text=True,timeout=60)
    require(r.returncode==0,'cast failed: '+str(args[0]))
    return r.stdout.strip()
def encode(sig,*args): return cast('calldata',sig,*args).lower()
def raw(v): return bytes.fromhex(v.removeprefix('0x'))
def keccak(v): return cast('keccak','0x'+v.hex()).lower()
def word(n): return int(n).to_bytes(32,'big')
def words(v,n=None):
    b=raw(v);require(len(b)%32==0 and (n is None or len(b)==32*n),'Unexpected ABI word length')
    return ['0x'+b[i:i+32].hex() for i in range(0,len(b),32)]
def address(v):
    require(re.fullmatch('0x[0-9a-fA-F]{40}',v) is not None,'Invalid address')
    return v.lower()
def b32(v):
    require(re.fullmatch('0x[0-9a-fA-F]{64}',v) is not None,'Invalid bytes32')
    return v.lower()
def state(s):
    x={k:b32(s[k]) for k in ('BlockHash','SendRoot')}
    for k in ('Batch','PosInBatch'):
        require(type(s[k]) is int and 0<=s[k]<2**64,'Invalid state position')
        x[k]=s[k]
    require(s.get('machineStatus',1)==1,'Only FINISHED states supported')
    return x

def position(s): return (s['Batch'],s['PosInBatch'])
def gh(s):
    return keccak(b'Global state:'+raw(s['BlockHash'])+raw(s['SendRoot'])+s['Batch'].to_bytes(8,'big')+s['PosInBatch'].to_bytes(8,'big'))
def sh(s,count): return keccak(raw(gh(s))+word(count)+b'\x01')
def nodehash(a,b,span,sibling,last,acc):
    execution=keccak(bytes(32)+word(span)+b''.join(raw(keccak(b'Block state:'+raw(gh(s)))) for s in (a,b)))
    return keccak(bytes([bool(sibling)])+raw(last)+raw(execution)+raw(acc)+raw(NEW))
def tuple_state(s): return f"(([{s['BlockHash']},{s['SendRoot']}],[{s['Batch']},{s['PosInBatch']}]),1)"
def admin(sig,*args): return dict(to=EXEC,value='0',data=encode('executeCall(address,bytes)',ROLLUP,encode(sig,*args)))
def pack(txs):
    return '0x'+b''.join(b'\x00'+raw(t['to'])+word(int(t['value']))+word(len(raw(t['data'])))+raw(t['data']) for t in txs).hex()
def envelope(txs):
    if len(txs)==1:return dict(**txs[0],operation=0)
    return dict(to=MULTI,value='0',data=encode('multiSend(bytes)',pack(txs)),operation=1)
def builder(txs,name):
    return dict(version='1.0',chainId='42161',createdAt=int(datetime.now().timestamp()*1000),
        meta=dict(name='REVIEW REQUIRED - '+name,description='No broadcast by toolkit. Compare exact Safe outer transaction before signing.',createdFromSafeAddress=SAFE,createdFromOwnerAddress=''),transactions=txs)
def hash_fields(t): return [t['to'],t['value'],t['data'],t['operation'],t['safeTxGas'],t['baseGas'],t['gasPrice'],t['gasToken'],t['refundReceiver']]
def safe_fields(e,nonce): return dict(**e,safeTxGas=0,baseGas=0,gasPrice=0,gasToken=ADDR0,refundReceiver=ADDR0,nonce=nonce)
def slot(label): return hex(int(keccak(label.encode()),16)-1)

class RPCRejected(ValueError):
    def __init__(self,method,detail):
        super().__init__("RPC rejected "+method);self.detail=detail

class RPC:
    READ={'eth_chainId','web3_clientVersion','eth_getBlockByNumber','eth_getBlockByHash','eth_call','eth_getCode','eth_getStorageAt','eth_getTransactionReceipt','eth_getTransactionByHash','eth_getBalance'}
    def __init__(self,url):
        require(urlsplit(url).scheme in ('http','https') and urlsplit(url).hostname,'HTTP(S) RPC required')
        self.url=url
    def request(self,method,params):
        # Payload on stdin; RPC credentials are never copied into reports or error strings.
        r=subprocess.run(['curl','--fail','-sS','--max-time',str(600 if method.startswith('arbdebug_') else 90),self.url,'-H','Content-Type: application/json','--data-binary','@-'],input=json.dumps(dict(jsonrpc='2.0',id=1,method=method,params=params)),capture_output=True,text=True,timeout=605 if method.startswith('arbdebug_') else 95)
        require(r.returncode==0,'RPC transport failed for '+method)
        obj=json.loads(r.stdout)
        if 'error' in obj:raise RPCRejected(method,obj['error'])
        return obj.get('result')
    def rpc(self,method,params):
        require(method in self.READ,'Public RPC mutation forbidden: '+method)
        return self.request(method,params)
    def call(self,to,sig,*args,tag='latest'):
        return self.rpc('eth_call',[dict(to=to,data=encode(sig,*args)),tag])
    def num(self,to,sig,*args,tag='latest'): return int(self.call(to,sig,*args,tag=tag),16)

def inventory(r,tag='latest'):
    require(int(r.rpc('eth_chainId',[]),16) in (42161,31337),'Wrong parent chain')
    block=r.rpc('eth_getBlockByNumber',[tag,False]);require(block,'Missing anchor');tag=block['number']
    def call(to,sig,*args):return r.call(to,sig,*args,tag=tag)
    def num(to,sig,*args):return int(call(to,sig,*args),16)
    def storage(to,k):return '0x'+r.rpc('eth_getStorageAt',[to,k,tag])[-40:].lower()
    routes=dict(rollupAdmin=storage(ROLLUP,slot('eip1967.proxy.admin')),primary=storage(ROLLUP,slot('eip1967.proxy.implementation')),secondary=storage(ROLLUP,slot('eip1967.proxy.implementation.secondary')),executor=storage(EXEC,slot('eip1967.proxy.implementation')),singleton=storage(SAFE,'0x0'))
    require(routes==dict(rollupAdmin=EXEC,primary='0xf9725312bd91ccfa3ad797e78a8a10b6d692fcd6',secondary='0xf916bfe431b7a7aae083273f5b862e00a15d60f4',executor='0x12b1389fbf261e781bdc3094d28636abfb03c5b3',singleton='0x29fcb43b46531bca003ddc8fcb67ffe91900c762'),'Contract routes changed')
    codes={a:keccak(raw(r.rpc('eth_getCode',[a,tag]))) for a in CODES}
    require(codes==CODES,'Deployment code changed')
    ow=words(call(SAFE,'getOwners()'));require(len(ow)==6 and int(ow[0],16)==32 and int(ow[1],16)==4,'Safe owners ABI changed')
    require(sorted('0x'+x[-40:] for x in ow[2:])==OWNERS and num(SAFE,'getThreshold()')==3,'Governance owners/threshold changed')
    require(storage(SAFE,GUARD)==ADDR0 and storage(SAFE,FALLBACK)=='0xfd0732dc9e303f09fcef3a7388ad10a83459ec99','Guard/fallback changed')
    require(call(SAFE,'getModulesPaginated(address,uint256)','0x'+'0'*39+'1',100)=='0x'+word(64).hex()+word(1).hex()+word(0).hex(),'Safe modules changed')
    require(num(EXEC,'hasRole(bytes32,address)',call(EXEC,'EXECUTOR_ROLE()'),SAFE)==1,'Safe missing executor role')
    require('0x'+call(ROLLUP,'bridge()')[-40:]==BRIDGE and '0x'+call(ROLLUP,'outbox()')[-40:]==OUTBOX,'Bridge/Outbox changed')
    require('0x'+call(ROLLUP,'anyTrustFastConfirmer()')[-40:]==FAST,'Fast confirmer changed')
    require(num(ROLLUP,'stakeToken()')==0,'Only native stake supported')
    confirmed=num(ROLLUP,'latestConfirmed()');created=num(ROLLUP,'latestNodeCreated()')
    p=words(call(ROLLUP,'getNode(uint64)',confirmed),12);sibling=int(p[9],16)
    stakers=[];count=num(ROLLUP,'stakerCount()');require(count<=16,'Unexpected staker count')
    for i in range(count):
        a='0x'+call(ROLLUP,'getStakerAddress(uint64)',i)[-40:]; w=words(call(ROLLUP,'getStaker(address)',a),5)
        stakers.append(dict(address=a,amount=int(w[0],16),node=int(w[2],16),challenge=int(w[3],16),active=bool(int(w[4],16)),credit=num(ROLLUP,'withdrawableFunds(address)',a)))
    result=dict(parentBlock=tag,parentBlockHash=block['hash'],routes=routes,codeHashes=codes,safeNonce=num(SAFE,'nonce()'),paused=bool(num(ROLLUP,'paused()')),wasm=call(ROLLUP,'wasmModuleRoot()'),confirmed=confirmed,created=created,firstUnresolved=num(ROLLUP,'firstUnresolvedNode()'),confirmedStorage=p,sibling=sibling,lastHash=words(call(ROLLUP,'getNode(uint64)',sibling),12)[11] if sibling else p[11],stakers=stakers,inboxCount=num(BRIDGE,'sequencerMessageCount()'),fastConfirmer=FAST)
    require(r.rpc('eth_getBlockByNumber',[tag,False])['hash']==block['hash'],'Anchor reorg')
    return result

def evidence(audit,directory,a,b,span):
    d=Path(directory);report=read(d/'summary.json');man=read(d/'manifest.json')
    first=man['firstMessage'];last=man['lastMessage'];start=state(man['startingState'])
    require(report['status']=='retained_span_replay_passed' and report['executionReplayed'] is True,'Replay unfinished')
    require(man['auditSha256']==digest(audit) and man['wasmModuleRoot']==NEW,'Replay audit/root mismatch')
    require(last-first+1==span==audit['positionalMessageSpan'] and state(man['endingState'])==b,'Replay span/B mismatch')
    require(state(audit['parentConfirmedState'])==a and state(audit['localStateAtConfirmedPosition'])==start and state(audit['checkpoint'])==b,'Audit state mismatch')
    require(audit['localMessageAtConfirmedPosition']==first-1 and audit['checkpointMessage']==last,'Audit positions mismatch')
    require(position(start)==position(a),'Local A-prime is not at parent A position')
    paths=sorted((d/'messages').glob('*.json'));require(len(paths)==span,'Missing replay records')
    chain=digest(man);md=chain;prev=start
    for msg,p in zip(range(first,last+1),paths):
        require(p.name==f'{msg}.json','Replay record gap');v=read(p)
        require(v['message']==msg and v['wasmModuleRoot']==NEW and v['manifestSha256']==md and v['previousRecordSha256']==chain,'Replay chain mismatch')
        end=state(v['end']);response=v['validationResponse'];h=v['canonicalHeader']
        require(state(v['start'])==prev and response['valid'] is True and state(response['globalstate'])==end,'Replay execution result mismatch')
        require(int(h['number'],16)==msg and h['hash'].lower()==end['BlockHash'] and h['parentHash'].lower()==prev['BlockHash'] and h['sendRoot'].lower()==end['SendRoot'],'Replay header mismatch')
        require(position(prev)<position(end)<=position(b),'Replay position mismatch')
        b32('0x'+v['recordedInputSha256']);prev=end;chain=digest(v)
    require(prev==b and report['validatedMessagesTotal']==span and report['recordChainSha256']==chain and state(report['lastValidatedState'])==b,'Replay final mismatch')
    return dict(recordsChecked=span,recordChainSha256=chain,manifest=man,summary=report,audit=audit,localStart=start,parentConfirmedAToBProven=False)

def transactions(pre,a,b,span,count,acc):
    h=nodehash(a,b,span,pre['sibling'],pre['lastHash'],acc)
    assertion='('+tuple_state(a)+','+tuple_state(b)+','+str(span)+')'
    txs=[admin('forceRefundStaker(address[])','['+','.join(REFUNDS)+']'),admin('setWasmModuleRoot(bytes32)',NEW),admin(CREATE,pre['confirmed'],count,assertion,h),admin('forceConfirmNode(uint64,bytes32,bytes32)',pre['created']+1,b['BlockHash'],b['SendRoot'])]
    return txs,h

def validate_pre(pre,a,b,count,simulation=False):
    require(pre['paused'] or simulation,'Production package requires confirmed pause')
    require(pre['wasm']==OLD,'Expected old WASM')
    require(set(s['address'] for s in pre['stakers'])==set(REFUNDS),'Stake set changed; review refund scope')
    require(all(s['active'] and s['amount']>0 and s['challenge']==0 for s in pre['stakers']),'Invalid stake/challenge state')
    require(sh(a,count)==pre['confirmedStorage'][0],'A does not match parent confirmed commitment')
    require(position(b)>position(a),'B must be ahead of confirmed A')
    require(b['Batch']+bool(b['PosInBatch'])<=pre['inboxCount'],'B batches unavailable')

def seal(d):
    save(d/'checksums.json',{p.name:filehash(p) for p in sorted(d.iterdir()) if p.is_file() and p.name!='checksums.json'})
def load_package(d):
    d=Path(d);checks=read(d/'checksums.json')
    require(all('/' not in k and k not in ('.','..','checksums.json') for k in checks),'Bad package filename')
    require({p.name for p in d.iterdir() if p.is_file() and p.name!='checksums.json'}==set(checks),'Package file set changed')
    for name,h in checks.items():require(filehash(d/name)==h,'Package modified: '+name)
    p=read(d/'package.json');require(p['schema']==2 and p['chainId']==42161,'Wrong package schema/chain')
    if p['kind']=='recovery':
        txs,h=transactions(p['pre'],p['before'],p['after'],p['numBlocks'],p['beforeInboxCount'],p['accumulator'])
        require(p['transactions']==txs and p['nodeHash']==h,'Package calldata/commitment mismatch')
    else: require(p['kind'] in ('pause','resume') and p['transactions']==[admin(p['kind']+'()')],'Invalid followup call')
    require(p['envelope']==envelope(p['transactions']) and p['safeFields']==safe_fields(p['envelope'],p['nonce']),'Safe envelope mismatch')
    doc=read(d/'safe-import.json');require(doc['chainId']=='42161' and doc['meta']['createdFromSafeAddress'].lower()==SAFE and doc['transactions']==p['transactions'],'Safe import differs')
    return p,digest(checks)
