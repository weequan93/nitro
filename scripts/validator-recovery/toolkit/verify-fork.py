#!/usr/bin/env python3
"""Execute the exact prepared CALL list only on a newly owned loopback Anvil 31337."""
import argparse
import socket
import subprocess
import sys
import time
from pathlib import Path
from core import *
from recovery import recovery_post, safe_receipt, created_event

class Fork(RPC):
    def __init__(self,upstream,block,blockhash,out):
        super().__init__(upstream)
        require(int(self.rpc('eth_chainId',[]),16)==42161,'Fork source must be Arbitrum One')
        require(self.rpc('eth_getBlockByNumber',[block,False])['hash']==blockhash,'Source anchor changed')
        with socket.socket() as s:s.bind(('127.0.0.1',0));port=s.getsockname()[1]
        self.log=(out/'anvil.log').open('w');self.proc=subprocess.Popen(['anvil','--host','127.0.0.1','--port',str(port),'--chain-id','31337','--fork-chain-id','42161','--fork-url',upstream,'--fork-block-number',str(int(block,16)),'--no-storage-caching','--accounts','0'],stdout=self.log,stderr=subprocess.STDOUT)
        self.url=f'http://127.0.0.1:{port}';self.enabled=False;self.out=out
        try:
            for _ in range(120):
                require(self.proc.poll() is None,'Owned Anvil exited; inspect anvil.log (may contain upstream URL)')
                try:
                    if self.rpc('eth_chainId',[])=='0x7a69':break
                except ValueError:pass
                time.sleep(.5)
            else:raise ValueError('Anvil startup timeout')
            require('anvil' in self.rpc('web3_clientVersion',[]).lower(),'Wrong local client')
            require(self.rpc('eth_getBlockByNumber',[block,False])['hash']==blockhash,'Fork anchor differs')
            self.enabled=True
        except BaseException:self.close();raise
    def mutate(self,m,p):
        require(self.enabled and self.proc.poll() is None and self.url.startswith('http://127.0.0.1:'),'Owned fork unavailable')
        require(self.rpc('eth_chainId',[])=='0x7a69','Fork chain changed')
        require(m in {'anvil_impersonateAccount','anvil_setBalance','eth_sendTransaction','evm_snapshot','evm_revert','anvil_setCode'},'Mutation outside simulation scope')
        return self.request(m,p)
    def fund(self,a):
        self.mutate('anvil_impersonateAccount',[a]);self.mutate('anvil_setBalance',[a,hex(10**20)])
    def send(self,a,to,data):
        tx=dict(from_=a,to=to,data=data,gas=hex(15000000));tx['from']=tx.pop('from_')
        h=self.mutate('eth_sendTransaction',[tx])
        for _ in range(120):
            receipt=self.rpc('eth_getTransactionReceipt',[h])
            if receipt:break
            time.sleep(.1)
        if receipt:
            save(self.out/('receipt-'+h+'.json'),receipt)
            if int(receipt['status'],16)==0:
                save(self.out/'failed-trace.json',self.request('debug_traceTransaction',[h,{'tracer':'callTracer'}]))
        require(receipt is not None and int(receipt['status'],16)==1,'Fork outer transaction reverted')
        return receipt
    def close(self):
        self.enabled=False
        if self.proc.poll() is None:
            self.proc.terminate()
            try:self.proc.wait(timeout=15)
            except subprocess.TimeoutExpired:self.proc.kill();self.proc.wait(timeout=10)
        self.log.close()

def event_hash(receipt,name):
    topic=keccak((name+'(bytes32,uint256)').encode())
    ev=[x for x in receipt['logs'] if x['address'].lower()==SAFE and x['topics'][0].lower()==topic]
    return (ev[0]['topics'][1] if len(ev[0]['topics'])>1 else words(ev[0]['data'])[0]) if len(ev)==1 else None

def execute(f,txs,nonce,negative=False):
    t=safe_fields(envelope(txs),nonce)
    if negative:t['safeTxGas']=8000000
    require(f.num(SAFE,'nonce()')==nonce,'Simulation nonce changed')
    h=f.call(SAFE,TX_HASH,*hash_fields(t),nonce)
    sigs='0x'
    for a in OWNERS[:3]:
        f.fund(a);f.send(a,SAFE,encode('approveHash(bytes32)',h))
        sigs+=word(int(a,16)).hex()+'00'*32+'01'
    # Exercise threshold without spending a nonce.
    rejected=False
    try:f.rpc('eth_call',[{'from':OWNERS[0],'to':SAFE,'data':encode(EXEC_TX,*hash_fields(t),sigs[:-130])}, 'latest'])
    except RPCRejected as e:rejected='GS020' in str(e.detail)
    require(rejected,'Two-signature negative check failed')
    rec=f.send(OWNERS[0],SAFE,encode(EXEC_TX,*hash_fields(t),sigs))
    if negative:
        trace=f.request('debug_traceTransaction',[rec['transactionHash'],{'tracer':'callTracer'}]);save(f.out/'negative-trace.json',trace)
        def walk(v):
            yield v
            for c in v.get('calls',[]):yield from walk(c)
        require(any(c.get('to','').lower()==EXEC and c.get('input','').lower()==txs[-1]['data'] and c.get('error') for c in walk(trace)), 'Negative case failed before final confirmation call')
    expected='ExecutionFailure' if negative else 'ExecutionSuccess'
    require(event_hash(rec,expected)==h and event_hash(rec,'ExecutionSuccess' if negative else 'ExecutionFailure') is None,'Safe outcome/hash mismatch')
    require(f.num(SAFE,'nonce()')==nonce+1,'Safe nonce did not advance once')
    return dict(receipt=rec,safeTxHash=h,safeFields=t,chainId=31337,ownersImpersonated=True)

def checked_state(f,p):
    x={sig:f.call(ROLLUP,sig) for sig in ('paused()','wasmModuleRoot()','latestConfirmed()','latestNodeCreated()','firstUnresolvedNode()','stakerCount()','anyTrustFastConfirmer()')}
    if p['kind']=='recovery':
        for s in p['pre']['stakers']:
            for sig in ('getStaker(address)','withdrawableFunds(address)','isZombie(address)'):
                x[sig+s['address']]=f.call(ROLLUP,sig,s['address'])
        for n in {p['pre']['confirmed'],p['pre']['created'],p['recoveryNode']}:
            x['node'+str(n)]=f.call(ROLLUP,'getNode(uint64)',n)
        x['outbox']=f.call(OUTBOX,'roots(bytes32)',p['after']['SendRoot'])
    return x

def verify(a):
    p,identity=load_package(a.package);d=output(a.out);f=None
    report=dict(status='stopped',packageIdentity=identity,checkedAt=now(),readyForProduction=False,productionSignaturesTested=False,productionTransactionsSent=False)
    try:
        f=Fork(a.parent_rpc,p['pre']['parentBlock'],p['pre']['parentBlockHash'],d)
        live=inventory(f);require(live==p['pre'],'Pinned inventory differs from package')
        shim=False
        if p['kind']=='recovery':
            try:
                f.call('0x0000000000000000000000000000000000000064','arbBlockNumber()')
            except RPCRejected:
                require(a.allow_arbsys_log_shim,'Anvil lacks ArbSys.arbBlockNumber; native-parent fidelity unavailable. Optional --allow-arbsys-log-shim is explicitly qualified test evidence only.')
                # Only a3b1b31d (arbBlockNumber) is accepted. Other selectors revert.
                # This supplies BLOCKNUMBER for log lookup; it does not implement ArbOS or L1/Arb block clocks.
                code='0x60003560e01c63a3b1b31d1460145760006000fd5b4360005260206000f3'
                f.mutate('anvil_setCode',['0x0000000000000000000000000000000000000064',code])
                f.call('0x0000000000000000000000000000000000000064','arbBlockNumber()')
                shim=True
        report['arbsysLogShimUsed']=shim
        report['nativeArbitrumExecutionTested']=False

        if p.get('simulatePause'):
            save(d/'simulation-pause.json',execute(f,[admin('pause()')],live['safeNonce']))
        if p['kind']=='recovery':
            require(f.num(ROLLUP,'paused()')==1,'Recovery fork is not paused')
            snap=f.mutate('evm_snapshot',[]);before=checked_state(f,p)
            bad=list(p['transactions']);bad[-1]=admin('forceConfirmNode(uint64,bytes32,bytes32)',p['recoveryNode'],ZERO,p['after']['SendRoot'])
            save(d/'negative.json',execute(f,bad,p['nonce'],True))
            require(checked_state(f,p)==before,'Inner rollback failed')
            require(f.mutate('evm_revert',[snap]) is True,'Snapshot rollback failed')
            report['lateFailureRollbackTested']=True
        result=execute(f,p['transactions'],p['nonce']);save(d/'success.json',result)
        require({k:result['safeFields'][k] for k in p['safeFields']}==p['safeFields'],'Positive Safe fields differ from imported package')
        receipt_package=dict(p,expectedSafeTxHash=result['safeTxHash'])
        safe_receipt(f,receipt_package,result['receipt']['transactionHash'],expected_chain=31337)
        report['receiptDecoderTested']=True
        if p['kind']=='recovery':
            report['post']=recovery_post(f,p)
            report['post']['actualInboxMaxCount']=created_event(f,p,result['receipt'])
        else:require(f.num(ROLLUP,'paused()')==int(p['kind']=='pause'),'Pause/resume result differs')
        if p['kind']=='recovery' and a.test_followups:
            snap=f.mutate('evm_snapshot',[])
            resume=execute(f,[admin('resume()')],f.num(SAFE,'nonce()'));save(d/'resume-test.json',resume)
            resume_p=dict(safeFields=resume['safeFields'],nonce=resume['safeFields']['nonce'],expectedSafeTxHash=resume['safeTxHash'])
            safe_receipt(f,resume_p,resume['receipt']['transactionHash'],expected_chain=31337)
            require(f.num(ROLLUP,'paused()')==0,'Resume failed')
            payments=[]
            for account in REFUNDS:
                f.fund(account);credit=f.num(ROLLUP,'withdrawableFunds(address)',account)
                balance=int(f.rpc('eth_getBalance',[account,'latest']),16)
                rec=f.send(account,ROLLUP,encode('withdrawStakerFunds()'))
                after=int(f.rpc('eth_getBalance',[account,'latest']),16)
                gas=int(rec['gasUsed'],16)*int(rec['effectiveGasPrice'],16)
                require(after==balance+credit-gas and f.num(ROLLUP,'withdrawableFunds(address)',account)==0,'Refund payment mismatch')
                payments.append(dict(account=account,credit=credit,transaction=rec['transactionHash']))
            save(d/'refund-test.json',payments)
            require(f.mutate('evm_revert',[snap]) is True,'Followup test rollback failed')
            recovery_post(f,p)
            report.update(resumeAndRefundPaymentsTested=True,followupTestsReverted=True)
        report.update(status='exact_package_fork_passed_with_arbsys_log_shim' if shim else 'exact_package_fork_passed',thresholdNegativeTested=True,exactSafeFieldsTested=True,exactImportTransactionsTested=True,hashDomain='Same fields; fork SafeTxHash uses chain31337, production uses42161',limitations='Fork owners approveHash; no production ECDSA. Recovery remains paused. No new WASM execution, runtime or full withdrawal audit in this test.')
    except Exception as e:report['error']=str(e) if isinstance(e,ValueError) else type(e).__name__
    finally:
        if f:f.close()
        report['ownedForkStopped']=True;save(d/'summary.json',report)
    print(json.dumps(report,indent=2));return 0 if report['status'] in ('exact_package_fork_passed','exact_package_fork_passed_with_arbsys_log_shim') else 1

def main():
    p=argparse.ArgumentParser(description=__doc__);p.add_argument('--package',type=Path,required=True);p.add_argument('--parent-rpc',default=os.environ.get('ARCHIVE_RPC'),required=not bool(os.environ.get('ARCHIVE_RPC')));p.add_argument('--out',type=Path,required=True);p.add_argument('--test-followups',action='store_true');p.add_argument('--allow-arbsys-log-shim',action='store_true',help='Explicitly qualified local test: replace only ArbSys block-number lookup, never production.')
    return verify(p.parse_args())
if __name__=='__main__':raise SystemExit(main())
