#!/usr/bin/env python3
"""Collect a recovery checkpoint for fork analysis. Read-only; no signing or transactions.

Requires probe-recovery-authority.py next to this script. A successful single-message
replay is not a proof of the transition from the divergent confirmed parent state.
"""
import argparse
import json
import subprocess
import sys
import urllib.request
from pathlib import Path

ROOT = '0x121d685e2fdb0e3291592d6b90bd70d503951335d19d96455448eb7a14d17421'
KEYS = ('BlockHash', 'SendRoot', 'Batch', 'PosInBatch')
ALLOWED = {'eth_chainId', 'eth_getBlockByNumber', 'eth_getBlockByHash',
           'eth_getCode', 'eth_call', 'web3_sha3', 'arbdebug_validationInputsAt',
           'arbdebug_validateMessageNumber'}


def rpc(url, method, params, timeout=600):
    if method not in ALLOWED:
        raise ValueError('RPC method not allowed: ' + method)
    req = urllib.request.Request(url, json.dumps(dict(jsonrpc='2.0', id=1,
        method=method, params=params)).encode(), {'Content-Type': 'application/json'})
    with urllib.request.urlopen(req, timeout=timeout) as response:
        obj = json.load(response)
    if 'error' in obj or obj.get('result') is None:
        raise ValueError(f'{method}: {obj}')
    return obj['result']


def state(value):
    return {k: value[k] for k in KEYS}


def position(value):
    return int(value['Batch']), int(value['PosInBatch'])


def decode_node(raw):
    if len(raw) != 2 + 12 * 64:
        raise ValueError('Unexpected getNode ABI; deployed source needs investigation')
    words = [raw[i:i+64] for i in range(2, len(raw), 64)]
    names = ('stateHash', 'challengeHash', 'confirmData', 'prevNum',
             'deadlineBlock', 'noChildConfirmedBeforeBlock', 'stakerCount',
             'childStakerCount', 'firstChildBlock', 'latestChildNumber',
             'createdAtBlock', 'nodeHash')
    return {name: ('0x'+word if i in (0, 1, 2, 11) else int(word,16))
            for i, (name, word) in enumerate(zip(names, words))}


def collect(a, snapshot, save, parent, child):
    tag = snapshot['parentBlock']
    def call(address, signature, args=''):
        selector = parent('web3_sha3', ['0x'+signature.encode().hex()])[:10]
        return parent('eth_call', [{'to':address, 'data':selector+args}, tag])

    if int(child('eth_chainId', []),16) != 2886:
        raise ValueError('Expected child chain 2886')
    confirmed = snapshot['confirmedNode']
    if 'after' not in confirmed:
        raise ValueError('Confirmed node event unavailable: '+str(confirmed))
    rollup = snapshot['rollup']
    node = decode_node(call(rollup, 'getNode(uint64)', f"{confirmed['number']:064x}"))
    save('confirmed-node-storage.json', node)
    # NodeCreated's final field is needed for the exact before-state commitment.
    selector = parent('web3_sha3', ['0x'+b'getNodeCreationBlockForLogLookup(uint64)'.hex()])[:10]
    creation = int(parent('eth_call', [{'to':rollup, 'data':selector+f"{confirmed['number']:064x}"}, tag]),16)
    # Use the snapshot probe's event field, collected at the same pinned block.
    if creation != confirmed['creationBlock']:
        raise ValueError('Creation block disagrees with snapshot')
    inbox_max = confirmed.get('inboxMaxCount')
    if inbox_max is None:
        raise ValueError('Update probe-recovery-authority.py: inboxMaxCount is required')
    before = confirmed['after']
    packed = (b'Global state:' + bytes.fromhex(before['BlockHash'][2:])
              + bytes.fromhex(before['SendRoot'][2:])
              + int(before['Batch']).to_bytes(8,'big')
              + int(before['PosInBatch']).to_bytes(8,'big'))
    gs_hash = parent('web3_sha3', ['0x'+packed.hex()])
    packed = bytes.fromhex(gs_hash[2:]) + int(inbox_max).to_bytes(32,'big') + int(before['machineStatus']).to_bytes(1,'big')
    expected_state_hash = parent('web3_sha3', ['0x'+packed.hex()])
    if expected_state_hash.lower() != node['stateHash'].lower():
        raise ValueError('Event-derived stateHash does not match getNode; stop')

    # Preserve bytecode for implementation identity checks, not just its hash.
    codes = {}
    addresses = {k:v['address'] for k,v in snapshot['slots'].items()}
    addresses['rollup'] = rollup
    addresses['adminImplementation'] = snapshot['adminContract']['proxySlots']['implementation']['address']
    for name, address in addresses.items():
        code = parent('eth_getCode', [address,tag])
        codes[name] = {'address':address,'code':code,
                       'codeHash':parent('web3_sha3',[''+code])}
    save('deployed-code.json', codes)

    original = json.loads(a.input.read_text())
    original = original.get('result', original)
    anchor = child('eth_getBlockByHash', [original['ExpectedEndState']['BlockHash'],False])
    anchor_num = int(anchor['number'],16)
    head = child('eth_getBlockByNumber', ['latest',False])
    candidate_num = int(head['number'],16) - a.lag_blocks
    if candidate_num < anchor_num:
        raise ValueError('Local head is behind incident anchor')
    candidate = child('eth_getBlockByNumber', [hex(candidate_num),False])
    message = int(original['Id']) + candidate_num - anchor_num
    entry = child('arbdebug_validationInputsAt', [hex(message),'amd64'])
    save('checkpoint-input.json', entry)
    after = state(entry['ExpectedEndState'])
    if int(entry['Id']) != message or after['BlockHash'].lower() != candidate['hash'].lower():
        raise ValueError('Message/block mapping or canonical hash mismatch')
    if entry['StartState']['BlockHash'].lower() != candidate['parentHash'].lower():
        raise ValueError('Checkpoint input parent does not match local header')
    send_root = candidate.get('sendRoot', candidate.get('extraData'))
    if send_root.lower() != after['SendRoot'].lower():
        raise ValueError('Checkpoint sendRoot does not match header')
    save('checkpoint-block.json',candidate)
    bridge = '0x'+snapshot['calls']['bridge()'][-40:]
    inbox_count = int(call(bridge,'sequencerMessageCount()'),16)
    needed_batches = int(after['Batch']) + (int(after['PosInBatch']) > 0)
    report = dict(parentBlock=tag, confirmedNode=confirmed['number'],
        oldConfirmedState=before, confirmedInboxMaxCount=inbox_max,
        confirmedStateHashMatches=True, localHead=int(head['number'],16),
        checkpointBlock=candidate_num, checkpointMessage=message, checkpoint=after,
        newWasmRoot=a.root, bridgeInboxCount=inbox_count,
        checkpointAheadOfConfirmed=position(after)>position(before),
        checkpointBatchesAvailable=needed_batches<=inbox_count,
        status='checkpoint_collected', readyForProduction=False)
    if not report['checkpointAheadOfConfirmed']:
        report['status']='local_checkpoint_not_ahead_of_confirmed'
    elif not report['checkpointBatchesAvailable']:
        report['status']='checkpoint_batches_not_available_at_parent_snapshot'
    else:
        response = child('arbdebug_validateMessageNumber', [hex(message),True,a.root])
        save('checkpoint-replay.json',response)
        if response.get('valid') is not True or state(response['globalstate']) != after:
            raise ValueError('Checkpoint replay failed or differs from local state')
        report['status']='checkpoint_single_message_replayed_fork_test_still_required'
        report['validatedMessagesThisRun']=1
    # Detect child reorg and pinned-parent reorg after the potentially slow replay.
    check = child('eth_getBlockByNumber',[hex(candidate_num),False])
    if check['hash'].lower() != candidate['hash'].lower():
        raise ValueError('Child checkpoint reorganized during collection; retry')
    check = parent('eth_getBlockByNumber',[tag,False])
    if check['hash'].lower() != snapshot['parentBlockHash'].lower():
        raise ValueError('Parent snapshot reorganized during collection; retry')
    report['note']='No proof of execution from old confirmed state to new checkpoint; any such transition is a trusted governance recovery, not ordinary WASM validation.'
    return report


def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--parent-rpc',required=True)
    p.add_argument('--node-rpc',default='http://127.0.0.1:8149')
    p.add_argument('--input',required=True,type=Path)
    p.add_argument('--out',required=True,type=Path)
    p.add_argument('--root',default=ROOT)
    p.add_argument('--lag-blocks',type=int,default=64)
    a=p.parse_args()
    if a.lag_blocks<0: p.error('lag-blocks must be nonnegative')
    if len(a.root)!=66 or not a.root.startswith('0x'):
        p.error('root must be a 32-byte hex hash')
    try: bytes.fromhex(a.root[2:])
    except ValueError: p.error('root is not hex')
    if not a.input.is_file(): p.error('input file not found')
    a.out.mkdir(parents=True,exist_ok=False)
    def save(name,data): (a.out/name).write_text(json.dumps(data,indent=2)+'\n')
    try:
        probe=(Path(__file__).resolve().parent / '../../diagnostics/probe-recovery-authority.py')
        with (a.out/'authority-probe.log').open('w') as log:
            subprocess.run([sys.executable,str(probe),'--parent-rpc',a.parent_rpc,
                '--node-rpc',a.node_rpc,'--out',str(a.out/'authority.json')],
                stdout=log,stderr=subprocess.STDOUT,check=True,timeout=900)
        snapshot=json.loads((a.out/'authority.json').read_text())
        print('Collecting checkpoint and implementation bytecode...',flush=True)
        report=collect(a,snapshot,save,
            lambda m,p:rpc(a.parent_rpc,m,p),lambda m,p:rpc(a.node_rpc,m,p))
    except Exception as exc:
        report=dict(status='stopped',error=str(exc),readyForProduction=False)
    save('summary.json',report)
    print(json.dumps(report,indent=2))
    print('OUTPUT',a.out)
    return 1 if report['status']=='stopped' else 0


if __name__=='__main__':
    raise SystemExit(main())
