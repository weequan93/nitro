#!/usr/bin/env python3
"""Local fork only: recovery via Safe approveHash + execTransaction, no production keys.

Place beside mock-validator-recovery.py or beside its recovery-mock-tools directory.
Accepts the same command-line arguments as that script. numBlocks remains synthetic.
"""
import importlib.util
import json
import subprocess
from pathlib import Path
import urllib.request

BASE = Path(__file__).resolve().parent
PATH = BASE / 'recovery-mock-tools/mock-validator-recovery.py'
if not PATH.is_file():
    PATH = BASE / 'mock-validator-recovery.py'
spec = importlib.util.spec_from_file_location('recovery_mock', PATH)
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)
OriginalFork = m.LocalFork
original_rehearse = m.rehearse
ZERO_ADDR = '0x' + '00' * 20


def success_event(log, topic, txhash):
    if log.get('address', '').lower() != m.SAFE:
        return False
    topics = log.get('topics', [])
    if not topics or topics[0].lower() != topic.lower():
        return False
    # Accommodate both indexed and non-indexed txHash event layouts.
    actual = topics[1] if len(topics) > 1 else '0x' + log.get('data', '')[2:66]
    return actual.lower() == txhash.lower()


def addresses(raw):
    b = bytes.fromhex(raw[2:])
    offset = int.from_bytes(b[:32], 'big')
    n = int.from_bytes(b[offset:offset+32], 'big')
    m.require(0 < n <= 32 and len(b) >= offset+32+n*32, 'Unexpected owners ABI')
    return ['0x'+b[offset+32+i*32+12:offset+64+i*32].hex() for i in range(n)]


class SafeFork(OriginalFork):
    def impersonate(self, address):
        # Existing rehearsal calls this for Safe; never impersonate the Safe itself.
        if address.lower() != m.SAFE:
            super().impersonate(address)

    def admin(self, label, signature, *args):
        owners = sorted(addresses(self.call(m.SAFE, 'getOwners()')), key=lambda x: int(x,16))
        threshold = int(self.call(m.SAFE, 'getThreshold()'), 16)
        m.require(threshold == 3 and len(owners) == 4, 'Expected 3-of-4 Safe')
        nonce = int(self.call(m.SAFE, 'nonce()'), 16)
        data = m.encode('executeCall(address,bytes)', m.ROLLUP, m.encode(signature,*args))
        fields = [m.EXECUTOR, 0, data, 0, 0, 0, 0, ZERO_ADDR, ZERO_ADDR]
        txhash = self.call(m.SAFE,
            'getTransactionHash(address,uint256,bytes,uint8,uint256,uint256,uint256,address,address,uint256)',
            *fields, nonce)
        chosen = owners[:threshold]
        for owner in chosen:
            self.impersonate(owner)
            self.send('Safe owner approveHash: '+label, owner, m.SAFE,
                      m.encode('approveHash(bytes32)',txhash))
            m.require(int(self.call(m.SAFE,'approvedHashes(address,bytes32)',owner,txhash),16)==1,
                      'Owner approval missing')
        # Safe v=1 approved-hash signatures, sorted by numeric owner address.
        signatures = '0x'+''.join(owner[2:].rjust(64,'0')+'00'*32+'01' for owner in chosen)
        sig = 'execTransaction(address,uint256,bytes,uint8,uint256,uint256,uint256,address,address,bytes)'
        if not getattr(self, 'threshold_checked', False):
            short = '0x'+signatures[2:2+65*2*2]
            try:
                self.rpc('eth_call',[{'from':chosen[0],'to':m.SAFE,
                         'data':m.encode(sig,*fields,short)},'latest'])
            except ValueError as exc:
                m.require('GS020' in str(exc), 'Unexpected insufficient-signature failure: '+str(exc))
            else:
                raise ValueError('Safe unexpectedly accepted fewer than threshold signatures')
            self.threshold_checked = True
            print('PASS Safe rejects two signatures for threshold three',flush=True)
        self.send('Safe execTransaction: '+label,chosen[0],m.SAFE,m.encode(sig,*fields,signatures))
        receipt=self.steps[-1]['receipt']
        topic=subprocess.check_output(['cast','keccak','ExecutionSuccess(bytes32,uint256)'],
                                      text=True,timeout=30).strip()
        m.require(any(success_event(x,topic,txhash) for x in receipt['logs']),
                  'Safe ExecutionSuccess missing for transaction hash')
        m.require(int(self.call(m.SAFE,'nonce()'),16)==nonce+1,'Safe nonce did not advance exactly once')


def spent_flags(f, outbox, count):
    result={}
    # Read every covered message in batches. Only the owned, already-authorized fork.
    for start in range(0,count,128):
        m.require(f.authorized and f.process.poll() is None,'Fork not authorized/running')
        indices=list(range(start,min(start+128,count)))
        selector=m.encode('isSpent(uint256)',0)[:10]
        body=[dict(jsonrpc='2.0',id=i,method='eth_call',params=[
            {'to':outbox,'data':selector+format(i,'064x')},'latest']) for i in indices]
        request=urllib.request.Request(f.url,json.dumps(body).encode(),{'Content-Type':'application/json'})
        with urllib.request.urlopen(request,timeout=120) as response:
            items=json.load(response)
        m.require(isinstance(items,list) and len(items)==len(indices),'Incomplete spent response')
        seen=set()
        for item in items:
            i=item.get('id')
            m.require(i in indices and i not in seen and 'result' in item,'Invalid spent response')
            seen.add(i)
            value=int(item['result'],16)
            m.require(value in (0,1),'Invalid boolean spent response')
            result[str(i)]=bool(value)
        if start % 1024 == 0 or start+128 >= count:
            print('Spent flags read:',len(result),'/',count,flush=True)
    return result


def rehearse(f, summary, authority, directory, refunds):
    # No mutations until identity has been independently checked.
    m.require(f.rpc('eth_chainId',[])==hex(31337),'Not isolated fork')
    m.require('anvil' in f.rpc('web3_clientVersion',[]).lower(),'Not Anvil')
    parent=f.rpc('eth_getBlockByNumber',[summary['parentBlock'],False])
    m.require(parent['hash'].lower()==authority['parentBlockHash'].lower(),'Wrong fork snapshot')
    f.authorized=True
    header=json.loads((directory/'checkpoint-block.json').read_text())
    count=int(header['sendCount'],16)
    m.require(0<count<=100000,'Unexpected withdrawal count; review scope')
    outbox='0x'+f.call(m.ROLLUP,'outbox()')[-40:]
    print('Reading spent flags for indices 0 through',count-1,flush=True)
    before=spent_flags(f,outbox,count)
    (f.output/'all-spent-before.json').write_text(json.dumps(before,indent=2))
    report=original_rehearse(f,summary,authority,directory,refunds)
    after=spent_flags(f,outbox,count)
    (f.output/'all-spent-after.json').write_text(json.dumps(after,indent=2))
    m.require(before==after,'Withdrawal spent flag changed')
    print('PASS all',count,'covered withdrawal spent flags preserved',flush=True)
    report.update(safeExecutionPathTested=True,safeThresholdNegativeTested=f.threshold_checked,
        safeAuthorizationMode='Fork-impersonated owners approveHash, then Safe execTransaction with v=1 approvals',
        safeSignaturesTested=False,productionOwnerSignaturesTested=False,
        spentFlagsChecked=count,fullWithdrawalHistoryAudited=False,readyForProduction=False,
        limitations='numBlocks=1 remains synthetic. Safe approved-hash path tested with fork owner impersonation; no production ECDSA signatures, no full withdrawal message/root audit, no validator restart or restaking in this run.')
    return report


if __name__=='__main__':
    m.LocalFork=SafeFork
    m.rehearse=rehearse
    raise SystemExit(m.main())
