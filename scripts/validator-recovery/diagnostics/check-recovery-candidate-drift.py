#!/usr/bin/env python3
"""Read-only comparison of a rehearsed checkpoint with fresh parent-chain authority data."""
import argparse
import json
import subprocess
import sys
import urllib.request
from pathlib import Path
from datetime import datetime, timezone


def position(state):
    return int(state['Batch']), int(state['PosInBatch'])


def rpc(url, method, params):
    req = urllib.request.Request(url,json.dumps(dict(jsonrpc='2.0',id=1,
        method=method,params=params)).encode(),{'Content-Type':'application/json'})
    with urllib.request.urlopen(req,timeout=45) as response:
        data=json.load(response)
    if 'error' in data:
        raise ValueError(str(data['error']))
    return data['result']


def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--candidate',required=True,type=Path)
    p.add_argument('--parent-rpc',required=True)
    p.add_argument('--node-rpc',default='http://127.0.0.1:8349')
    p.add_argument('--out',required=True,type=Path)
    a=p.parse_args()
    a.out.mkdir(parents=True,exist_ok=False)
    report={'readyForProduction':False,'checkedAt':datetime.now(timezone.utc).isoformat()}
    try:
        candidate=json.loads((a.candidate/'summary.json').read_text())
        checkpoint=candidate['checkpoint']
        height=candidate.get('candidateBlock',candidate.get('checkpointBlock'))
        if height is None:
            raise ValueError('Candidate block missing')
        if int(rpc(a.parent_rpc,'eth_chainId',[]),16)!=42161:
            raise ValueError('Wrong parent chain')
        if int(rpc(a.node_rpc,'eth_chainId',[]),16)!=2886:
            raise ValueError('Wrong L3 chain')
        probe=(Path(__file__).resolve().parent / 'probe-recovery-authority.py')
        with (a.out/'probe.log').open('w') as log:
            result=subprocess.run([sys.executable,str(probe),'--parent-rpc',a.parent_rpc,
                '--node-rpc',a.node_rpc,'--authority','0xFbB37c66372f7B40361fBC8C8A235ae92711399D',
                '--out',str(a.out/'authority.json')],stdout=log,stderr=log,timeout=900)
        if result.returncode:
            raise ValueError('Authority probe failed; inspect probe.log')
        authority=json.loads((a.out/'authority.json').read_text())
        confirmed=authority['confirmedNode']['after']
        local=rpc(a.node_rpc,'eth_getBlockByNumber',[hex(int(height)),False])
        retained=bool(local and local['hash'].lower()==checkpoint['BlockHash'].lower()
                      and local['sendRoot'].lower()==checkpoint['SendRoot'].lower())
        ahead=position(checkpoint)>position(confirmed)
        old_parent=candidate['parentBlock']
        report.update(parentBlock=authority['parentBlock'],parentBlockHash=authority['parentBlockHash'],
            originalParentBlock=old_parent,confirmedNode=authority['nodeNumbers']['latestConfirmed'],
            latestNodeCreated=authority['nodeNumbers']['latestNodeCreated'],
            currentConfirmedState=confirmed,candidateBlock=height,checkpoint=checkpoint,
            candidateStillOnLocalChain=retained,candidateAheadOfCurrentConfirmed=ahead,
            parentChanged=authority['parentBlock']!=old_parent,
            status=('candidate_chain_mismatch' if not retained else
                    'candidate_not_ahead_refresh_required' if not ahead else
                    'candidate_ahead_but_fresh_rehearsal_state_required'),
            note='Position comparison only. No A-to-B execution proof, signatures or chain mutations. Historical mock results remain historical evidence.')
    except Exception as exc:
        report.update(status='check_failed',error=str(exc))
    (a.out/'summary.json').write_text(json.dumps(report,indent=2)+'\n')
    print(json.dumps(report,indent=2))
    print('OUTPUT',a.out)
    return 1 if report['status']=='check_failed' else 0


if __name__=='__main__':
    raise SystemExit(main())
