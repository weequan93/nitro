#!/usr/bin/env python3
"""Contract-only recovery rehearsal. Starts a private Anvil; NEVER signs production transactions.

Uses a synthetic numBlocks=1 for the trusted bridge between divergent histories.
No output from this program is a production Safe proposal or an execution proof.
"""
import argparse
import json
import shutil
import socket
import subprocess
import time
import urllib.request
from pathlib import Path

ROLLUP = '0xa113e2e9620a3bc088a681ebb2c234fdbeb85e21'
EXECUTOR = '0x1333480e92de9511dc9bb01f70901ff3ee94f613'
SAFE = '0xfbb37c66372f7b40361fbc8c8a235ae92711399d'
ROLE = '0xd8aa0f3194971a2a116679f7c2090f6939c8d4e01a2a8d7e41d55e5351469e63'
ZERO = '0x' + '00' * 32
ASSERTION = '(((bytes32[2],uint64[2]),uint8),((bytes32[2],uint64[2]),uint8),uint64)'


def encode(signature, *args):
    return subprocess.check_output(['cast', 'calldata', signature, *map(str, args)],
                                   text=True, timeout=30).strip()


def require(ok, reason):
    if not ok:
        raise ValueError(reason)


def state_tuple(s):
    return f"(([{s['BlockHash']},{s['SendRoot']}],[{s['Batch']},{s['PosInBatch']}]),{s.get('machineStatus', 1)})"


class LocalFork:
    def __init__(self, port, process, output):
        self.url = f'http://127.0.0.1:{port}'
        self.process = process
        self.output = output
        self.steps = []
        self.authorized = False

    def rpc(self, method, params):
        require(self.process.poll() is None, 'Owned Anvil process exited; refusing RPC')
        if method == 'eth_sendTransaction' or method.startswith('anvil_'):
            require(self.authorized, 'Fork identity has not been verified')
        req = urllib.request.Request(self.url, json.dumps(dict(jsonrpc='2.0', id=1,
            method=method, params=params)).encode(), {'Content-Type': 'application/json'})
        with urllib.request.urlopen(req, timeout=90) as response:
            obj = json.load(response)
        if 'error' in obj:
            raise ValueError(f'{method}: {obj["error"]}')
        return obj.get('result')

    def call(self, to, signature, *args):
        return self.rpc('eth_call', [{'to': to, 'data': encode(signature, *args)}, 'latest'])

    def number(self, signature, *args):
        return int(self.call(ROLLUP, signature, *args), 16)

    def impersonate(self, address):
        self.rpc('anvil_impersonateAccount', [address])
        # Only changes this disposable fork, to fund gas for the impersonated sender.
        self.rpc('anvil_setBalance', [address, hex(10**20)])

    def send(self, label, sender, target, data):
        tx = {'from': sender, 'to': target, 'data': data, 'gas': hex(15_000_000)}
        self.rpc('eth_call', [tx, 'latest'])
        tx_hash = self.rpc('eth_sendTransaction', [tx])
        receipt = None
        for _ in range(120):
            receipt = self.rpc('eth_getTransactionReceipt', [tx_hash])
            if receipt is not None:
                break
            time.sleep(0.25)
        require(receipt is not None and int(receipt['status'], 16) == 1, label + ' failed')
        self.steps.append({'step': label, 'forkTransaction': tx_hash, 'receipt': receipt})
        (self.output / 'steps.json').write_text(json.dumps(self.steps, indent=2))
        print('PASS', label, flush=True)

    def admin(self, label, signature, *args):
        inner = encode(signature, *args)
        self.send(label, SAFE, EXECUTOR, encode('executeCall(address,bytes)', ROLLUP, inner))


def rehearse(f, summary, authority, directory, refunds):
    require(f.rpc('eth_chainId', []) == hex(31337), 'Not isolated chain 31337')
    require('anvil' in f.rpc('web3_clientVersion', []).lower(), 'Not Anvil')
    block = f.rpc('eth_getBlockByNumber', [summary['parentBlock'], False])
    require(block and block['hash'].lower() == authority['parentBlockHash'].lower(),
            'Fork block hash differs from collected snapshot')
    for item in json.loads((directory / 'deployed-code.json').read_text()).values():
        actual = f.rpc('eth_getCode', [item['address'], 'latest'])
        require(actual.lower() == item['code'].lower(), 'Deployment bytecode changed: '+item['address'])
    require(f.number('latestConfirmed()') == summary['confirmedNode'], 'Confirmed node changed')
    require(authority['rollup'].lower() == ROLLUP, 'Wrong Rollup')
    require(authority['slots']['admin']['address'].lower() == EXECUTOR, 'Wrong proxy admin')
    require(int(f.call(EXECUTOR, 'hasRole(bytes32,address)', ROLE, SAFE),16) == 1,
            'Safe lacks EXECUTOR_ROLE at fork block')
    require(int(f.call(SAFE, 'getThreshold()'),16) == 3, 'Unexpected Safe threshold')
    stored = f.call(ROLLUP, 'getNode(uint64)', summary['confirmedNode'])
    reference = json.loads((directory / 'confirmed-node-storage.json').read_text())
    require(stored[2:66].lower() == reference['stateHash'][2:].lower(), 'Before-state storage mismatch')
    # Authorization applies only to the locally launched process, never upstream RPC.
    f.authorized = True
    stakers = []
    for i in range(f.number('stakerCount()')):
        address = '0x'+f.call(ROLLUP,'getStakerAddress(uint64)',i)[-40:]
        raw = f.call(ROLLUP, 'getStaker(address)', address)[2:]
        words = [int(raw[k:k+64],16) for k in range(0,len(raw),64)]
        require(len(words) == 5, 'Unexpected Staker ABI')
        stakers.append(dict(address=address, amount=words[0], node=words[2], challenge=words[3]))
    (f.output/'stakers-before.json').write_text(json.dumps(stakers,indent=2))
    selected = {s['address']:s for s in stakers if s['address'] in refunds}
    require(set(selected) == set(refunds), 'Refund list includes an address not currently staked')
    require(all(s['challenge'] == 0 for s in selected.values()), 'Selected staker has active challenge')
    require(f.number('stakeToken()') == 0, 'This rehearsal only supports ETH stakes')
    pending = {'confirmed':summary['confirmedNode'], 'created':f.number('latestNodeCreated()'),
               'firstUnresolved':f.number('firstUnresolvedNode()')}
    (f.output/'nodes-before.json').write_text(json.dumps(pending,indent=2))
    outbox = '0x'+f.call(ROLLUP,'outbox()')[-40:]
    roots = set([summary['oldConfirmedState']['SendRoot'], summary['checkpoint']['SendRoot']])
    roots.update(['0x51ec9054390fcf055ca472b4d8d489f04d0961f34cd6b21e4f80bcf9dd57014d',
                  '0xcbc6c4652176371b752a000b6138198e2e00a76039c176133f9aa299db52ae85'])
    prior_roots = {r:f.call(outbox,'roots(bytes32)',r) for r in roots}
    spent_before = {i:f.call(outbox,'isSpent(uint256)',i) for i in range(8111,8115)}
    credits = {s:f.number('withdrawableFunds(address)',s) for s in refunds}
    f.impersonate(SAFE)
    if not f.number('paused()'):
        f.admin('pause', 'pause()')
    f.admin('refund selected stakers', 'forceRefundStaker(address[])', '['+','.join(refunds)+']')
    for address,s in selected.items():
        require(f.number('isStaked(address)',address) == 0, 'Stake not removed')
        require(f.number('withdrawableFunds(address)',address) == credits[address]+s['amount'],
                'Refund credit mismatch')
    f.admin('set new WASM root', 'setWasmModuleRoot(bytes32)',summary['newWasmRoot'])
    before = state_tuple(summary['oldConfirmedState'])
    after = state_tuple(summary['checkpoint'])
    f.admin('create trusted recovery assertion (synthetic numBlocks=1)',
            'forceCreateNode(uint64,uint256,'+ASSERTION+',bytes32)',
            summary['confirmedNode'], summary['confirmedInboxMaxCount'], f'({before},{after},1)', ZERO)
    recovery = f.number('latestNodeCreated()')
    require(recovery == pending['created']+1, 'Unexpected new node number')
    f.admin('confirm recovery assertion','forceConfirmNode(uint64,bytes32,bytes32)',
            recovery,summary['checkpoint']['BlockHash'],summary['checkpoint']['SendRoot'])
    require(f.number('latestConfirmed()') == recovery, 'Recovery was not confirmed')
    require(f.number('firstUnresolvedNode()') == recovery+1, 'Unexpected unresolved pointer')
    require(f.call(ROLLUP,'wasmModuleRoot()').lower() == summary['newWasmRoot'].lower(), 'Wrong root')
    require('0x'+f.call(ROLLUP,'outbox()')[-40:] == outbox, 'Outbox changed')
    require(f.call(outbox,'roots(bytes32)',summary['checkpoint']['SendRoot']).lower()
            == summary['checkpoint']['BlockHash'].lower(), 'Checkpoint root not registered')
    for r,v in prior_roots.items():
        if r.lower() != summary['checkpoint']['SendRoot'].lower():
            require(f.call(outbox,'roots(bytes32)',r) == v, 'Existing Outbox root changed')
    for i,v in spent_before.items():
        require(f.call(outbox,'isSpent(uint256)',i) == v, 'Spent flag changed')
    f.admin('resume Rollup', 'resume()')
    # Check the actual refund payment path for each selected staker on this fork.
    for address in refunds:
        f.impersonate(address)
        amount = f.number('withdrawableFunds(address)',address)
        balance_before = int(f.rpc('eth_getBalance',[address,'latest']),16)
        f.send('withdraw refund '+address,address,ROLLUP,encode('withdrawStakerFunds()'))
        receipt = f.steps[-1]['receipt']
        fee = int(receipt['gasUsed'],16)*int(receipt['effectiveGasPrice'],16)
        balance_after = int(f.rpc('eth_getBalance',[address,'latest']),16)
        require(balance_after == balance_before+amount-fee, 'Refund ETH payment mismatch')
        require(f.number('withdrawableFunds(address)',address) == 0, 'Refund balance not cleared')
    f.send('remove old zombies',refunds[0],ROLLUP,encode('removeOldZombies(uint256)',0))
    for address in refunds:
        require(f.number('isZombie(address)',address) == 0, 'Zombie cleanup failed')
    return dict(status='contract_rehearsal_passed', recoveryNode=recovery,
        originalPendingNodes=pending, spentPreserved=spent_before,
        safeSignaturesTested=False, validatorRestartTested=False, restakingTested=False,
        fullWithdrawalHistoryAudited=False, readyForProduction=False,
        limitations='Safe is impersonated, numBlocks=1 is synthetic. Tests deployed contract acceptance and refund payment only; no A-to-B execution proof, no validator restart, no subsequent assertion or production Safe signatures.')


def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('snapshot',type=Path)
    p.add_argument('--parent-rpc',required=True,help='Read-only upstream for Anvil fork')
    p.add_argument('--refund-staker',action='append',required=True,help='Explicit public staker address; repeat if needed')
    p.add_argument('--out',required=True,type=Path)
    a=p.parse_args()
    for tool in ('anvil','cast'):
        if not shutil.which(tool):p.error(tool+' is required')
    refunds=list(dict.fromkeys(x.lower() for x in a.refund_staker))
    for address in refunds:
        require(len(address)==42 and address.startswith('0x'),'Invalid staker address')
        bytes.fromhex(address[2:])
    summary=json.loads((a.snapshot/'summary.json').read_text())
    require(summary.get('status')=='checkpoint_single_message_replayed_fork_test_still_required',
            'Snapshot not ready for contract rehearsal; inspect collection summary')
    require(summary.get('checkpointAheadOfConfirmed') and summary.get('checkpointBatchesAvailable'),
            'Checkpoint positions unsuitable')
    authority=json.loads((a.snapshot/'authority.json').read_text())
    a.out.mkdir(parents=True,exist_ok=False)
    with socket.socket() as sock:
        sock.bind(('127.0.0.1',0));port=sock.getsockname()[1]
    process=None
    report={'status':'stopped','readyForProduction':False}
    try:
        with (a.out/'anvil.log').open('w') as log:
            process=subprocess.Popen(['anvil','--host','127.0.0.1','--port',str(port),
                '--chain-id','31337','--fork-chain-id','42161','--fork-url',a.parent_rpc,
                '--fork-block-number',str(int(summary['parentBlock'],16)),
                '--no-storage-caching','--accounts','0'],stdout=log,stderr=subprocess.STDOUT)
            f=LocalFork(port,process,a.out)
            for _ in range(120):
                require(process.poll() is None,'Anvil failed; see anvil.log')
                try:
                    f.rpc('web3_clientVersion',[]);break
                except (OSError,ValueError):time.sleep(0.25)
            else:raise TimeoutError('Anvil startup timed out')
            report=rehearse(f,summary,authority,a.snapshot,refunds)
    except Exception as exc:
        report['error']=str(exc)
    finally:
        if process is not None and process.poll() is None:
            process.terminate()
            try:process.wait(timeout=10)
            except subprocess.TimeoutExpired:process.kill();process.wait()
        (a.out/'summary.json').write_text(json.dumps(report,indent=2)+'\n')
    print(json.dumps(report,indent=2));print('OUTPUT',a.out)
    return 0 if report['status']=='contract_rehearsal_passed' else 1


if __name__=='__main__':
    raise SystemExit(main())
