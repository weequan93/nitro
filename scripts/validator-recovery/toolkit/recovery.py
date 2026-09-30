#!/usr/bin/env python3
"""Prepare/review Deriw recovery packages. Never signs or broadcasts to production."""
import argparse
import os
import sys
from pathlib import Path
from core import *


def write_package(d,p,r):
    p.update(schema=2,chainId=42161,checkedAt=now(),readyForProduction=False,broadcastEnabled=False,parentConfirmedAToBProven=False)
    p['envelope']=envelope(p['transactions']);p['safeFields']=safe_fields(p['envelope'],p['nonce'])
    # This hash belongs to the actual chain of the read-only input RPC. Simulation hashes are explicitly marked.
    p['safeHashChainId']=int(r.rpc('eth_chainId',[]),16)
    p['expectedSafeTxHash']=r.call(SAFE,TX_HASH,*hash_fields(p['safeFields']),p['nonce'],tag=p['pre']['parentBlock'])
    save(d/'package.json',p);save(d/'safe-import.json',builder(p['transactions'],p['kind']))
    (d/'REVIEW.md').write_text(f'''# {p['kind']}：需审阅，程序没有广播能力

- 用途：{p['purpose']}；RPC chainId：{p['safeHashChainId']}
- Safe：`{SAFE}`；Arbitrum One 42161；nonce `{p['nonce']}`
- 外层 to：`{p['envelope']['to']}`；operation `{p['envelope']['operation']}`；value 0
- safeTxGas/baseGas/gasPrice 均为 0；gasToken/refundReceiver 零地址。
- SafeTxHash：`{p['expectedSafeTxHash']}`，仅适用于上面的 chainId 和全部 Safe 字段。
- safe-import.json 是 Transaction Builder 的内层 CALL 列表。UI 必须构造 package.json 中完全相同的外层交易。
- UI 若选择不同 MultiSend 地址、包装层或 Safe gas 字段：不要签，重新生成并验证。外层普通 Ethereum gas limit 不属于 SafeTxHash。
- 历史分叉/模拟包不得用于生产。readyForProduction=false 不会自动变成批准。
- 精确节点 hash/prevNode 检查是合约已有约束；Safe nonce 防重复。其他前置检查在链下，不是原子链上 guard。
- Inbox 可以继续增长，新节点 stateHash 的 inboxMaxCount 应按 NodeCreated 实际事件核验；不要照抄模拟值。
- A→B 是受信任治理迁移。A′→B 重放和消息数量不能证明父链 A→B。
- 恢复包成功后仍暂停。resume 必须在恢复回执和运行验收后单独生成。
''')
    seal(d)


def build(a):
    r=RPC(a.parent_rpc);require(int(r.rpc('eth_chainId',[]),16)==42161,'Production parent must be 42161')
    c=read(a.candidate/'summary.json');audit=read(a.audit);decision=read(a.decision)
    A=state(c['oldConfirmedState']);B=state(c['checkpoint']);count=c['confirmedInboxMaxCount'];span=decision['numBlocks']
    require(type(span) is int and 0<span<2**64,'Invalid numBlocks')
    require(decision['candidateSha256']==digest(c) and decision['auditSha256']==digest(audit),'Decision bound to different input')
    require(decision['acknowledgesTrustedMigration'] is True and decision['acknowledgesNoParentAToBProof'] is True,'Trusted migration acknowledgement missing')
    require(decision['mode'] in ('review-only','approved-parameters') and isinstance(decision['reviewReference'],str) and decision['reviewReference'].strip(),'Review reference missing')
    require(c['parentBlock']==audit['parentBlock'] and c['confirmedNode']==audit['confirmedNode'],'Mixed candidate/audit anchors')
    pre=inventory(r,c['parentBlock']);require(pre['confirmed']==c['confirmedNode'],'Candidate parent confirmation changed')
    if (a.candidate/'authority.json').exists():require(read(a.candidate/'authority.json')['parentBlockHash'].lower()==pre['parentBlockHash'].lower(),'Candidate anchor hash mismatch')
    validate_pre(pre,A,B,count,a.simulation)
    print('Checking saved replay records...',flush=True)
    ev=evidence(audit,a.replay,A,B,span)
    require(c['checkpointMessage']==ev['manifest']['lastMessage'] and c['checkpointBlock']==c['checkpointMessage'],'Candidate message/block mismatch')
    node=RPC(a.node_rpc);require(int(node.rpc('eth_chainId',[]),16)==2886,'Wrong L3 chain')
    h=node.rpc('eth_getBlockByNumber',[hex(c['checkpointBlock']),False]);require(h and h['hash'].lower()==B['BlockHash'] and h['sendRoot'].lower()==B['SendRoot'],'B not canonical on supplied L3')
    needed=B['Batch']+bool(B['PosInBatch']);acc=r.call(BRIDGE,'sequencerInboxAccs(uint256)',needed-1,tag=pre['parentBlock']) if needed else ZERO
    txs,nh=transactions(pre,A,B,span,count,acc)
    d=output(a.out);save(d/'evidence.json',ev);save(d/'decision.json',decision);save(d/'candidate.json',c)
    p=dict(kind='recovery',purpose='simulation-only' if a.simulation else 'production-review',pre=pre,before=A,after=B,beforeInboxCount=count,numBlocks=span,accumulator=acc,nodeHash=nh,recoveryNode=pre['created']+1,nonce=pre['safeNonce']+int(a.simulation and not pre['paused']),transactions=txs,decisionMode=decision['mode'],evidenceSha256=digest(ev),simulatePause=bool(a.simulation and not pre['paused']),checkpointBlock=c['checkpointBlock'])
    write_package(d,p,r);print('PACKAGE',d)


def decision_template(a):
    c=read(a.candidate/'summary.json');au=read(a.audit)
    p=Path(a.out);require(not p.exists(),'Output already exists')
    save(p,dict(mode='review-only',candidateSha256=digest(c),auditSha256=digest(au),numBlocks=au['positionalMessageSpan'],acknowledgesTrustedMigration=False,acknowledgesNoParentAToBProof=False,reviewReference='填写具体审阅记录；两个 acknowledgement 在理解后改为 true'))
    print(p)


def check_drift(r,p):
    live=inventory(r)
    fields=('routes','codeHashes','wasm','confirmed','created','firstUnresolved','confirmedStorage','sibling','lastHash','stakers','fastConfirmer')
    require(all(live[k]==p['pre'][k] for k in fields),'Recovery preconditions drifted: regenerate/review')
    require(live['paused'] is True and live['safeNonce']==p['nonce'],'Pause/Safe nonce changed')
    needed=p['after']['Batch']+bool(p['after']['PosInBatch'])
    acc=r.call(BRIDGE,'sequencerInboxAccs(uint256)',needed-1,tag=live['parentBlock']) if needed else ZERO
    require(acc==p['accumulator'] and live['inboxCount']>=needed,'Inbox accumulator changed')
    return live


def preflight(a):
    p,identity=load_package(a.package);require(p['purpose']=='production-review','Simulation-only package')
    r=RPC(a.parent_rpc);require(int(r.rpc('eth_chainId',[]),16)==42161,'Wrong chain')
    if p['kind']=='recovery':live=check_drift(r,p)
    else:
        live=inventory(r);require(live['safeNonce']==p['nonce'] and live['paused']==(p['kind']=='resume'),'Pause/resume precondition changed')
        require(all(live[k]==p['pre'][k] for k in ('routes','codeHashes','wasm','confirmed','created','stakers')),'Followup state changed')
    actual=r.call(SAFE,TX_HASH,*hash_fields(p['safeFields']),p['nonce'],tag=live['parentBlock'])
    require(actual==p['expectedSafeTxHash'],'Safe hash changed')
    d=output(a.out);save(d/'summary.json',dict(status='read_only_preflight_passed',packageIdentity=identity,parentBlock=live['parentBlock'],parentBlockHash=live['parentBlockHash'],inboxCount=live['inboxCount'],expectedSafeTxHash=actual,readyForProduction=False,warning='Off-chain observation only, not an atomic guard; signatures and authority approval are separate.'))
    print('PASS',d)


def dynamic(data,index):
    b=raw(data);off=int.from_bytes(b[index*32:(index+1)*32],'big')
    require(off%32==0 and off+32<=len(b),'Invalid ABI offset')
    n=int.from_bytes(b[off:off+32],'big');require(off+32+n<=len(b),'Truncated ABI bytes')
    return '0x'+b[off+32:off+32+n].hex()


def safe_receipt(r,p,txhash,expected_chain=42161):
    receipt=r.rpc('eth_getTransactionReceipt',[txhash]);tx=r.rpc('eth_getTransactionByHash',[txhash])
    require(receipt and tx and receipt['transactionHash'].lower()==txhash.lower() and int(receipt['status'],16)==1,'Receipt missing/reverted')
    require(tx['to'].lower()==SAFE and int(tx.get('value','0x0'),16)==0 and int(tx['chainId'],16)==expected_chain,'Wrong transaction target/value/chain')
    require(tx['input'][:10]==encode(EXEC_TX,*hash_fields(p['safeFields']),'0x')[:10],'Not Safe execTransaction')
    args='0x'+tx['input'][10:];w=words('0x'+raw(args)[:320].hex(),10)
    t=dict(to='0x'+w[0][-40:],value=str(int(w[1],16)),data=dynamic(args,2),operation=int(w[3],16),safeTxGas=int(w[4],16),baseGas=int(w[5],16),gasPrice=int(w[6],16),gasToken='0x'+w[7][-40:],refundReceiver='0x'+w[8][-40:],nonce=p['nonce'])
    require(t==p['safeFields'],'Signed Safe fields differ from package')
    logs=[l for l in receipt['logs'] if l['address'].lower()==SAFE]
    success=keccak(b'ExecutionSuccess(bytes32,uint256)');failure=keccak(b'ExecutionFailure(bytes32,uint256)')
    require(not any(l['topics'][0].lower()==failure for l in logs),'Safe ExecutionFailure')
    found=[l for l in logs if l['topics'][0].lower()==success];require(len(found)==1,'Expected exactly one Safe success event')
    actual_hash=r.call(SAFE,TX_HASH,*hash_fields(t),t['nonce'],tag=receipt['blockNumber'])
    require(actual_hash==p['expectedSafeTxHash'] and (found[0]['topics'][1] if len(found[0]['topics'])>1 else words(found[0]['data'])[0])==actual_hash,'Safe success hash mismatch')
    event=keccak(b'SafeMultiSigTransaction(address,uint256,bytes,uint8,uint256,uint256,uint256,address,address,bytes,bytes)')
    detail=[l for l in logs if l['topics'][0].lower()==event];require(len(detail)==1,'SafeL2 transaction event missing')
    require(int(words(dynamic(detail[0]['data'],10))[0],16)==p['nonce'],'Executed nonce differs')
    block=r.rpc('eth_getBlockByNumber',[receipt['blockNumber'],False]);require(block['hash']==receipt['blockHash'],'Receipt is not canonical')
    return receipt


def recovery_post(r,p,tag='latest'):
    def num(sig,*args):return r.num(ROLLUP,sig,*args,tag=tag)
    require(num('paused()')==1 and r.call(ROLLUP,'wasmModuleRoot()',tag=tag)==NEW,'Recovery pause/root mismatch')
    n=p['recoveryNode'];require(num('latestConfirmed()')==num('latestNodeCreated()')==n and num('firstUnresolvedNode()')==n+1,'Recovery pointers mismatch')
    w=words(r.call(ROLLUP,'getNode(uint64)',n,tag=tag),12);require(w[11]==p['nodeHash'] and int(w[3],16)==p['pre']['confirmed'],'Recovery node commitment mismatch')
    require(r.call(OUTBOX,'roots(bytes32)',p['after']['SendRoot'],tag=tag)==p['after']['BlockHash'],'Outbox root registration mismatch')
    require(num('stakerCount()')==0,'Remaining active stake')
    credits={}
    for s in p['pre']['stakers']:
        credit=num('withdrawableFunds(address)',s['address']);require(credit==s['amount']+s['credit'] and num('isStaked(address)',s['address'])==0,'Refund credit mismatch')
        credits[s['address']]=credit
    return dict(recoveryNode=n,nodeHash=w[11],refundCredits=credits,paused=True)


def created_event(r,p,rec):
    sig=keccak(('NodeCreated(uint64,bytes32,bytes32,bytes32,'+ASSERTION+',bytes32,bytes32,uint256)').encode())
    ev=[l for l in rec['logs'] if l['address'].lower()==ROLLUP and l['topics'][0]==sig]
    require(len(ev)==1 and len(ev[0]['topics'])==4,'Exactly one NodeCreated event required')
    require(int(ev[0]['topics'][1],16)==p['recoveryNode'] and ev[0]['topics'][3]==p['nodeHash'],'NodeCreated identity mismatch')
    w=words(ev[0]['data'],15);count=int(w[14],16)
    require(w[13]==NEW and count>=p['after']['Batch']+bool(p['after']['PosInBatch']),'NodeCreated root/inbox mismatch')
    stored=words(r.call(ROLLUP,'getNode(uint64)',p['recoveryNode'],tag=rec['blockNumber']),12)
    require(stored[0]==sh(p['after'],count),'Recovery stateHash / event inbox count mismatch')
    return count


def accept(a):
    p,identity=load_package(a.package);require(p['purpose']=='production-review','Cannot accept production receipt against simulation package')
    r=RPC(a.parent_rpc);require(int(r.rpc('eth_chainId',[]),16)==42161,'Wrong chain')
    rec=safe_receipt(r,p,b32(a.transaction));tag=rec['blockNumber'];post={}
    if p['kind']=='recovery':
        post=recovery_post(r,p,tag)
        post['actualInboxMaxCount']=created_event(r,p,rec)
    else:
        require(r.num(ROLLUP,'paused()',tag=tag)==int(p['kind']=='pause'),'Pause/resume state mismatch')
        post['paused']=p['kind']=='pause'
    d=output(a.out);save(d/'receipt.json',rec)
    save(d/'summary.json',dict(status='receipt_and_block_state_passed',kind=p['kind'],packageIdentity=identity,packageDirectory=str(a.package.resolve()),transaction=a.transaction,block=tag,blockHash=rec['blockHash'],post=post,checkedAt=now(),readyForProduction=False,limitations='State checked at receipt block end; not proof of subsequent validator health, finality or full withdrawal history.'))
    print('PASS receipt',d)


def followup(a):
    r=RPC(a.parent_rpc);require(int(r.rpc('eth_chainId',[]),16)==42161,'Wrong chain')
    pre=inventory(r);d=output(a.out)
    if a.kind=='pause':
        require(not pre['paused'] and pre['wasm']==OLD,'Pause state changed')
        p=dict(kind='pause',purpose='production-review',pre=pre,nonce=pre['safeNonce'],transactions=[admin('pause()')])
    else:
        require(a.package and a.acceptance,'resume requires recovery package and accepted receipt')
        parent,identity=load_package(a.package);acceptance=read(a.acceptance/'summary.json')
        require(parent['kind']=='recovery' and parent['purpose']=='production-review','Wrong recovery package')
        require(acceptance['status']=='receipt_and_block_state_passed' and acceptance['kind']=='recovery' and acceptance['packageIdentity']==identity,'Wrong recovery acceptance')
        safe_receipt(r,parent,acceptance['transaction']);recovery_post(r,parent,pre['parentBlock'])
        p=dict(kind='resume',purpose='production-review',pre=pre,nonce=pre['safeNonce'],transactions=[admin('resume()')],recoveryPackageIdentity=identity,recoveryReceipt=acceptance['transaction'])
    write_package(d,p,r);print('PACKAGE',d)


def main():
    p=argparse.ArgumentParser(description=__doc__);subs=p.add_subparsers(dest='command',required=True)
    b=subs.add_parser('build');b.add_argument('--candidate',type=Path,required=True);b.add_argument('--audit',type=Path,required=True);b.add_argument('--replay',type=Path,required=True);b.add_argument('--decision',type=Path,required=True);b.add_argument('--node-rpc',required=True);b.add_argument('--simulation',action='store_true');b.set_defaults(fn=build)
    t=subs.add_parser('decision-template');t.add_argument('--candidate',type=Path,required=True);t.add_argument('--audit',type=Path,required=True);t.set_defaults(fn=decision_template)
    for name,fn in [('preflight',preflight),('accept',accept)]:
        q=subs.add_parser(name);q.add_argument('--package',type=Path,required=True);q.set_defaults(fn=fn)
        if name=='accept':q.add_argument('--transaction',required=True)
    f=subs.add_parser('followup');f.add_argument('kind',choices=['pause','resume']);f.add_argument('--package',type=Path);f.add_argument('--acceptance',type=Path);f.set_defaults(fn=followup)
    for q in (b,subs.choices['preflight'],subs.choices['accept'],f):q.add_argument('--parent-rpc',default=os.environ.get('PARENT_RPC','http://10.1.2.16:8547'))
    for q in subs.choices.values():q.add_argument('--out',type=Path,required=True)
    a=p.parse_args()
    try:a.fn(a)
    except Exception as e:
        print('STOP:',str(e) if isinstance(e,ValueError) else type(e).__name__,file=sys.stderr);return 1
    return 0
if __name__=='__main__':raise SystemExit(main())
