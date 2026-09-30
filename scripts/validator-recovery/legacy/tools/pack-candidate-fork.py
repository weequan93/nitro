#!/usr/bin/env python3
"""Build a pinned fork snapshot from collected candidate and replay evidence; read-only RPCs."""
import argparse
import hashlib
import json
import os
import subprocess
import urllib.request
from pathlib import Path

def require(ok, reason):
    if not ok:
        raise ValueError(reason)

def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--candidate', required=True, type=Path)
    p.add_argument('--replay', required=True, type=Path)
    p.add_argument('--participants', required=True, type=Path)
    p.add_argument('--parent-rpc', default=os.environ.get('ARCHIVE_RPC'))
    p.add_argument('--node-rpc', default='http://127.0.0.1:8349')
    p.add_argument('--out', required=True, type=Path)
    a = p.parse_args()
    if not a.parent_rpc:
        p.error('ARCHIVE_RPC or --parent-rpc required')
    def load(path):
        return json.loads(path.read_text())
    def rpc(url, method, params):
        require(method in ('eth_chainId','eth_call','eth_getCode','eth_getBlockByNumber'), 'RPC not read-only')
        req = urllib.request.Request(url, json.dumps(dict(jsonrpc='2.0',id=1,method=method,params=params)).encode(),
                                     {'Content-Type':'application/json'})
        with urllib.request.urlopen(req, timeout=60) as r:
            obj = json.load(r)
        require('error' not in obj and obj.get('result') is not None, 'RPC failed: '+str(obj.get('error')))
        return obj['result']
    def cast(*args):
        return subprocess.check_output(['cast', *map(str,args)], text=True).strip()
    c=load(a.candidate/'summary.json'); inv=load(a.candidate/'authority.json')
    r=load(a.replay/'summary.json'); participants=load(a.participants)
    require(r['status']=='candidate_single_message_replay_passed' and r['executionReplayed'], 'Replay not passed')
    require(c['aheadOfConfirmed'] and c['batchesAvailable'], 'Candidate positions unsuitable')
    require(c['checkpoint']==r['checkpoint'] and c['candidateBlock']==r['candidateBlock'], 'Replay/candidate mismatch')
    require(hashlib.sha256((a.candidate/'candidate-input.json').read_bytes()).hexdigest()==r['savedInputSha256'], 'Input digest mismatch')
    response=load(a.replay/'validation-response.json')
    require(response['valid'] is True and response['globalstate']==c['checkpoint'], 'Replay response mismatch')
    tag=inv['parentBlock']
    require(tag==c['parentBlock']==participants['parentBlock'], 'Mixed parent snapshots')
    require(inv['parentBlockHash']==participants['parentBlockHash'], 'Parent hash mismatch')
    require(participants.get('status')=='inventory_complete' and not participants.get('errors'), 'Participants incomplete')
    require(int(rpc(a.parent_rpc,'eth_chainId',[]),16)==42161, 'Wrong parent chain')
    require(int(rpc(a.node_rpc,'eth_chainId',[]),16)==2886, 'Wrong child chain')
    require(rpc(a.parent_rpc,'eth_getBlockByNumber',[tag,False])['hash']==inv['parentBlockHash'], 'Parent reorg')
    require(rpc(a.node_rpc,'eth_getBlockByNumber',[hex(c['candidateBlock']),False])['hash']==c['candidateHash'], 'Candidate reorg')
    confirmed=inv['confirmedNode']; before=confirmed['after']
    ref=participants['nodeStorage'][str(confirmed['number'])]
    raw=rpc(a.parent_rpc,'eth_call',[{'to':inv['rollup'],'data':cast('calldata','getNode(uint64)',confirmed['number'])},tag])
    require(len(raw)==2+12*64 and raw[2:66].lower()==ref['stateHash'][2:].lower(), 'Node storage mismatch')
    gs=b'Global state:'+bytes.fromhex(before['BlockHash'][2:])+bytes.fromhex(before['SendRoot'][2:])+int(before['Batch']).to_bytes(8,'big')+int(before['PosInBatch']).to_bytes(8,'big')
    gh=cast('keccak','0x'+gs.hex())
    packed=bytes.fromhex(gh[2:])+int(confirmed['inboxMaxCount']).to_bytes(32,'big')+int(before['machineStatus']).to_bytes(1,'big')
    require(cast('keccak','0x'+packed.hex()).lower()==ref['stateHash'].lower(), 'Confirmed state commitment mismatch')
    codes={}
    addresses={k:v['address'] for k,v in inv['slots'].items()}
    addresses.update(rollup=inv['rollup'],adminImplementation=inv['adminContract']['proxySlots']['implementation']['address'])
    for name,address in addresses.items():
        code=rpc(a.parent_rpc,'eth_getCode',[address,tag])
        codes[name]={'address':address,'code':code,'codeHash':cast('keccak',code)}
        expected=inv['slots'].get(name,{}).get('codeHash')
        if expected:
            require(codes[name]['codeHash']==expected,'Bytecode identity mismatch')
    summary=dict(parentBlock=tag,confirmedNode=confirmed['number'],oldConfirmedState=before,
        confirmedInboxMaxCount=confirmed['inboxMaxCount'],confirmedStateHashMatches=True,
        localHead=c['head'],checkpointBlock=c['candidateBlock'],checkpointMessage=r['message'],
        checkpoint=c['checkpoint'],newWasmRoot=r['wasmModuleRoot'],bridgeInboxCount=c['bridgeInboxCount'],
        checkpointAheadOfConfirmed=True,checkpointBatchesAvailable=True,validatedMessagesThisRun=0,
        replayEvidence=str(a.replay),status='checkpoint_single_message_replayed_fork_test_still_required',
        readyForProduction=False,note='Uses prior single-message replay. No A-to-B execution proof; fixed historical fork only.')
    a.out.mkdir(parents=True,exist_ok=False)
    for name,value in [('summary.json',summary),('authority.json',inv),('participants.json',participants),
                       ('deployed-code.json',codes),('confirmed-node-storage.json',ref),
                       ('checkpoint-input.json',load(a.candidate/'candidate-input.json')),
                       ('checkpoint-block.json',load(a.candidate/'candidate-block.json')),
                       ('checkpoint-replay.json',response)]:
        (a.out/name).write_text(json.dumps(value,indent=2))
    print(json.dumps(summary,indent=2)); print('OUTPUT',a.out)

if __name__=='__main__':
    main()
