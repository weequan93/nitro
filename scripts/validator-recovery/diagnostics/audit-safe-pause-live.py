#!/usr/bin/env python3
"""Read-only public inventory plus an owned local 31337 fork pause rehearsal."""
import argparse
from datetime import datetime, timezone
import hashlib
import importlib.util
import json
from pathlib import Path
import socket
import subprocess
import time
import urllib.request
import urllib.error

BASE=Path(__file__).resolve().parent
spec=importlib.util.spec_from_file_location('pause_safe',BASE/'../legacy/tools/rehearse-safe-recovery.py')
safe=importlib.util.module_from_spec(spec);spec.loader.exec_module(safe)
m=safe.m
RPC='https://arb1.arbitrum.io/rpc'
OWNERS={'0xa0c2aed24f5474b2815b2ff61d0f5a01970217c3','0xc60f0ed09edd696e60574f714cbd7cfec004dd70',
        '0xc63b7a2dacfa3aed4ea158f4f51ffbe020b9de4c','0x09ad976b259d9174f4250f0244873c3bc876e2ce'}
GUARD='0x4a204f620c8c5ccdca3fd54d003badd85ba500436a431f0cbda4f558c93c34c8'
FALLBACK='0x6c9a6c4a39284e37ed1cf53d337577d14212a4870fb976a4366c693b939918d5'
EXPECTED_CODES={
 m.SAFE:'0xd7d408ebcd99b2b70be43e20253d6d92a8ea8fab29bd3be7f55b10032331fb4c',
 m.EXECUTOR:'0x8736329b580cfc0c0c39ee6700515e0bc51652afb614640db9e34a5d784933e8',
 '0x29fcb43b46531bca003ddc8fcb67ffe91900c762':'0xb1f926978a0f44a2c0ec8fe822418ae969bd8c3f18d61e5103100339894f81ff',
 '0x12b1389fbf261e781bdc3094d28636abfb03c5b3':'0x0d88feac198ef1b50b99fddf06aa9f6b1050bfe7211d6f04173de9b6d8953bcb',
 '0xf9725312bd91ccfa3ad797e78a8a10b6d692fcd6':'0x8cf117fd02db7f12da04db8ac71d302b4a34dffbc17c629b4f7aa6cd5ffcacc5',
 '0xf916bfe431b7a7aae083273f5b862e00a15d60f4':'0x8bf14ad1722eceef8f77dfdffb79393a42662fdce14d957db1e323084a8145c1',
}


def write(path,obj): path.write_text(json.dumps(obj,indent=2)+'\n')
def keccak(data): return subprocess.check_output(['cast','keccak',data],text=True,timeout=30).strip()
def slot(label): return hex(int(keccak('0x'+label.encode().hex()),16)-1)
def rpc(method,params):
    m.require(method in {'eth_chainId','eth_getBlockByNumber','eth_call','eth_getCode','eth_getStorageAt'},'Public RPC mutation forbidden')
    raw=subprocess.check_output(['curl','--fail','-sS','--max-time','30',RPC,'-H','Content-Type: application/json',
        '--data-binary',json.dumps(dict(jsonrpc='2.0',id=1,method=method,params=params))],text=True,timeout=35)
    obj=json.loads(raw)
    if 'error' in obj or obj.get('result') is None: raise ValueError(str(obj.get('error','null response')))
    return obj['result']


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--out',type=Path,required=True)
    a=parser.parse_args();a.out.mkdir(parents=True,exist_ok=False)
    report=dict(checkedAt=datetime.now(timezone.utc).isoformat(),readyToSign=False,readyForProduction=False,
                productionTransactionsSent=False,productionSignaturesTested=False)
    process=None
    try:
        batchpath=BASE/'../evidence/safe-ui-prep-20260929/DRAFT-pause.safe.json'
        batch=json.loads(batchpath.read_text());data=m.encode('executeCall(address,bytes)',m.ROLLUP,m.encode('pause()'))
        m.require(batch['chainId']=='42161' and batch['meta']['createdFromSafeAddress'].lower()==m.SAFE,'Wrong draft network/Safe')
        m.require(batch['transactions']==[dict(to=m.EXECUTOR,value='0',data=data)],'Draft does not contain exactly the intended pause')
        report['draftSha256']=hashlib.sha256(batchpath.read_bytes()).hexdigest()
        m.require(int(rpc('eth_chainId',[]),16)==42161,'Wrong parent chain')
        head=rpc('eth_getBlockByNumber',['latest',False]);tag=head['number']
        report.update(parentBlock=tag,parentBlockHash=head['hash'])
        def call(to,sig,*args): return rpc('eth_call',[{'to':to,'data':m.encode(sig,*args)},tag])
        def storage(to,s): return '0x'+rpc('eth_getStorageAt',[to,s,tag])[-40:]
        routes={
            'rollupAdmin':storage(m.ROLLUP,slot('eip1967.proxy.admin')),
            'rollupPrimary':storage(m.ROLLUP,slot('eip1967.proxy.implementation')),
            'rollupSecondary':storage(m.ROLLUP,slot('eip1967.proxy.implementation.secondary')),
            'executorImplementation':storage(m.EXECUTOR,slot('eip1967.proxy.implementation')),
            'safeSingleton':storage(m.SAFE,'0x0')}
        m.require(routes==dict(rollupAdmin=m.EXECUTOR,rollupPrimary='0xf9725312bd91ccfa3ad797e78a8a10b6d692fcd6',
            rollupSecondary='0xf916bfe431b7a7aae083273f5b862e00a15d60f4',executorImplementation='0x12b1389fbf261e781bdc3094d28636abfb03c5b3',
            safeSingleton='0x29fcb43b46531bca003ddc8fcb67ffe91900c762'),'Authority routing changed')
        report['routes']=routes
        codes={address:keccak(rpc('eth_getCode',[address,tag])) for address in EXPECTED_CODES}
        m.require(codes==EXPECTED_CODES,'Deployed code baseline changed');report['codeHashes']=codes
        owners=safe.addresses(call(m.SAFE,'getOwners()'));threshold=int(call(m.SAFE,'getThreshold()'),16)
        m.require(set(owners)==OWNERS and len(owners)==4 and threshold==3,'Safe owners/threshold changed')
        guard=storage(m.SAFE,GUARD);fallback=storage(m.SAFE,FALLBACK)
        mods=call(m.SAFE,'getModulesPaginated(address,uint256)','0x'+'0'*39+'1',50)
        m.require(mods=='0x'+format(64,'064x')+format(1,'064x')+'0'*64,'Unexpected modules')
        m.require(int(guard,16)==0 and fallback=='0xfd0732dc9e303f09fcef3a7388ad10a83459ec99','Safe guard/fallback changed')
        role=call(m.EXECUTOR,'EXECUTOR_ROLE()')
        m.require(int(call(m.EXECUTOR,'hasRole(bytes32,address)',role,m.SAFE),16)==1,'Safe lacks execution authority')
        nonce=int(call(m.SAFE,'nonce()'),16)
        report['safe']=dict(address=m.SAFE,owners=owners,threshold=threshold,nonce=nonce,guard=guard,fallbackHandler=fallback,modules=[])
        report['liveState']={sig:call(m.ROLLUP,sig) for sig in ('paused()','wasmModuleRoot()','latestConfirmed()','latestNodeCreated()','stakerCount()','anyTrustFastConfirmer()')}
        m.require(int(report['liveState']['paused()'],16)==0,'Rollup already paused')
        report['stakers']=[]
        count=int(report['liveState']['stakerCount()'],16);m.require(count<=20,'Unexpected staker count')
        for i in range(count):
            addr='0x'+call(m.ROLLUP,'getStakerAddress(uint64)',i)[-40:]
            raw=call(m.ROLLUP,'getStaker(address)',addr)
            words=[int(raw[k:k+64],16) for k in range(2,len(raw),64)]
            m.require(len(words)==5 and words[3]==0,'Staker challenge needs review')
            report['stakers'].append(dict(address=addr,raw=raw))
        report['downstreamEthCall']=rpc('eth_call',[{'from':m.SAFE,'to':m.EXECUTOR,'data':data,'value':'0x0'},tag])
        m.require(rpc('eth_getBlockByNumber',[tag,False])['hash']==head['hash'],'Anchor changed')
        write(a.out/'live-inventory.json',report)
        print('PASS live code/authority/Safe/paused-state checks and downstream eth_call',flush=True)
        checksum=subprocess.check_output(['cast','to-check-sum-address',m.SAFE],text=True).strip()
        url='https://safe-transaction-arbitrum.safe.global/api/v1/safes/'+checksum+'/multisig-transactions/?executed=false&nonce__gte='+str(nonce)+'&limit=100'
        try:
            raw=subprocess.check_output(['curl','-sS','-L','--proto','=https','--proto-redir','=https',
                '--max-redirs','3','--max-time','30','-w','\n%{http_code}',url],text=True,timeout=35)
            body,status=raw.rsplit('\n',1)
            if status!='200':raise urllib.error.HTTPError(url,int(status),'Service unavailable',{},None)
            queue=json.loads(body)
            report['serviceQueue']=dict(available=True,count=queue.get('count'),nextPage=bool(queue.get('next')),
                results=[{k:item.get(k) for k in ('nonce','safeTxHash','to','value','operation','data','isExecuted','confirmationsRequired')} for item in queue.get('results',[])],
                note='Service queue only; does not cover unpublished/offline signatures or all pending transactions.')
        except Exception as exc:
            report['serviceQueue']=dict(available=False,errorType=type(exc).__name__,httpStatus=getattr(exc,'code',None),note='Queue unknown; verify in Safe UI.')
        with socket.socket() as sock: sock.bind(('127.0.0.1',0));port=sock.getsockname()[1]
        with (a.out/'anvil.log').open('w') as log:
            process=subprocess.Popen(['anvil','--host','127.0.0.1','--port',str(port),'--chain-id','31337','--fork-chain-id','42161',
                '--fork-url',RPC,'--fork-block-number',str(int(tag,16)),'--no-storage-caching','--accounts','0'],stdout=log,stderr=subprocess.STDOUT)
        f=safe.SafeFork(port,process,a.out)
        for _ in range(120):
            m.require(process.poll() is None,'Owned fork exited')
            try:
                if f.rpc('eth_chainId',[])=='0x7a69':break
            except Exception:time.sleep(.5)
        else: raise ValueError('Fork startup timed out')
        m.require('anvil' in f.rpc('web3_clientVersion',[]).lower(),'Not Anvil')
        m.require(f.rpc('eth_getBlockByNumber',[tag,False])['hash']==head['hash'],'Fork anchor differs')
        def state():
            result={sig:f.call(m.ROLLUP,sig) for sig in ('wasmModuleRoot()','latestConfirmed()','latestNodeCreated()','firstUnresolvedNode()','stakerCount()','outbox()','anyTrustFastConfirmer()')}
            for item in report['stakers']:
                for sig in ('getStaker(address)','withdrawableFunds(address)','isZombie(address)'):
                    result[sig+item['address']]=f.call(m.ROLLUP,sig,item['address'])
            return result
        before=state();m.require(f.number('paused()')==0,'Fork paused unexpectedly')
        f.authorized=True
        f.admin('exact single-call pause draft','pause()')
        m.require(f.number('paused()')==1 and state()==before,'Pause did not preserve checked state')
        m.require(int(f.call(m.SAFE,'nonce()'),16)==nonce+1,'Safe nonce mismatch')
        receipt=f.steps[-1]['receipt']
        report['forkTest']=dict(chainId=31337,status='full_safe_pause_passed',twoSignaturesRejected=f.threshold_checked,
            safeExecutionSuccessChecked=True,pausedAfter=True,checkedStatePreserved=True,transaction=f.steps[-1]['forkTransaction'],
            gasUsed=int(receipt['gasUsed'],16),authorization='Impersonated owners approveHash then Safe execTransaction; Safe itself not impersonated',
            productionEcdsaSignaturesTested=False,productionSafeTxHashGenerated=False)
        report['status']='pause_review_and_fork_passed'
    except Exception as exc:
        report.update(status='review_stopped',errorType=type(exc).__name__,error=str(exc))
    finally:
        if process is not None:
            process.terminate()
            try: process.wait(timeout=10)
            except subprocess.TimeoutExpired: process.kill();process.wait()
        report['ownedForkRunning']=False
        report['finishedAt']=datetime.now(timezone.utc).isoformat()
        write(a.out/'summary.json',report)
        print(json.dumps(report,indent=2));print('OUTPUT',a.out)
    return 0 if report['status']=='pause_review_and_fork_passed' else 1


if __name__=='__main__': raise SystemExit(main())
