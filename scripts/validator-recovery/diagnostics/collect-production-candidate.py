#!/usr/bin/env python3
"""Read-only candidate collection. Never signs or declares production readiness."""
import argparse
import json
import os
import subprocess
import sys
import urllib.request
from pathlib import Path


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--parent-rpc', default=os.environ.get('ARCHIVE_RPC'))
    p.add_argument('--node-rpc', default='https://rpc.deriw.com')
    p.add_argument('--input', required=True, type=Path)
    p.add_argument('--out', required=True, type=Path)
    p.add_argument('--lag-blocks', type=int, default=64)
    a = p.parse_args()
    if not a.parent_rpc or a.lag_blocks < 0:
        p.error('ARCHIVE_RPC required; lag must be nonnegative')
    a.out.mkdir(parents=True, exist_ok=False)
    def save(name, obj):
        (a.out/name).write_text(json.dumps(obj, indent=2))
    def rpc(url, method, params, timeout=40):
        req = urllib.request.Request(url, json.dumps(dict(jsonrpc='2.0', id=1,
            method=method, params=params)).encode(), {'Content-Type': 'application/json'})
        with urllib.request.urlopen(req, timeout=timeout) as r:
            obj = json.load(r)
        if 'error' in obj:
            raise ValueError(json.dumps(obj['error']))
        return obj.get('result')
    def child(m, params, timeout=40):
        return rpc(a.node_rpc, m, params, timeout)
    def parent(m, params):
        return rpc(a.parent_rpc, m, params)
    report = {'readyForProduction': False, 'executionReplayed': False}
    try:
        assert int(parent('eth_chainId', []),16) == 42161
        assert int(child('eth_chainId', []),16) == 2886
        probe = (Path(__file__).resolve().parent / 'probe-recovery-authority.py')
        with (a.out/'authority-probe.log').open('w') as log:
            subprocess.run([sys.executable, str(probe), '--parent-rpc', a.parent_rpc,
                '--node-rpc', a.node_rpc, '--authority',
                '0xFbB37c66372f7B40361fBC8C8A235ae92711399D',
                '--out', str(a.out/'authority.json')], stdout=log, stderr=log,
                check=True, timeout=900)
        inv = json.loads((a.out/'authority.json').read_text())
        report['parentBlock'] = inv['parentBlock']
        report['confirmedNode'] = inv['confirmedNode']
        anchor = json.loads(a.input.read_text())
        anchor = anchor.get('result', anchor)
        ab = child('eth_getBlockByHash', [anchor['ExpectedEndState']['BlockHash'], False])
        assert ab is not None, 'Incident anchor not found on retained chain'
        latest = child('eth_getBlockByNumber', ['latest', False])
        height = int(latest['number'],16)-a.lag_blocks
        assert height >= int(ab['number'],16)
        b = child('eth_getBlockByNumber', [hex(height), False])
        save('candidate-block.json', b)
        report.update(clientVersion=child('web3_clientVersion', []),
            head=int(latest['number'],16), candidateBlock=height,
            candidateHash=b['hash'], sendRoot=b.get('sendRoot', b.get('extraData')),
            sendCount=b.get('sendCount'))
        msg = int(anchor['Id']) + height-int(ab['number'],16)
        report['candidateMessageUnverifiedUntilInputChecked'] = msg
        bridge = '0x'+inv['calls']['bridge()'][-40:]
        data = subprocess.check_output(['cast','calldata','sequencerMessageCount()'],text=True).strip()
        count = int(parent('eth_call',[{'to':bridge,'data':data},inv['parentBlock']]),16)
        report['bridgeInboxCount'] = count
        outbox = '0x'+inv['calls']['outbox()'][-40:]
        data = subprocess.check_output(['cast','calldata','roots(bytes32)',report['sendRoot']],text=True).strip()
        report['candidateSendRootRegistration'] = parent('eth_call',[
            {'to':outbox,'data':data},inv['parentBlock']])
        try:
            entry = child('arbdebug_validationInputsAt', [hex(msg),'amd64'],600)
        except Exception as exc:
            report['status'] = 'candidate_header_only_validation_input_unavailable'
            report['inputErrorType'] = type(exc).__name__
            if isinstance(exc, ValueError):
                report['inputRpcError'] = str(exc)
        else:
            save('candidate-input.json',entry)
            after = entry['ExpectedEndState']
            assert int(entry['Id']) == msg
            assert after['BlockHash'].lower() == b['hash'].lower()
            assert entry['StartState']['BlockHash'].lower() == b['parentHash'].lower()
            assert after['SendRoot'].lower() == report['sendRoot'].lower()
            pos = lambda s:(int(s['Batch']),int(s['PosInBatch']))
            report['checkpoint'] = after
            report['aheadOfConfirmed'] = pos(after)>pos(inv['confirmedNode']['after'])
            report['batchesAvailable'] = int(after['Batch'])+(int(after['PosInBatch'])>0)<=count
            report['status'] = 'candidate_input_collected_replay_required'
        assert child('eth_getBlockByNumber',[hex(height),False])['hash'] == b['hash']
        assert parent('eth_getBlockByNumber',[inv['parentBlock'],False])['hash'] == inv['parentBlockHash']
    except Exception as exc:
        report['status'] = 'stopped'
        report['errorType'] = type(exc).__name__
        if isinstance(exc,(ValueError,AssertionError)):
            report['error'] = str(exc)
    report['note'] = 'Root registration is not full withdrawal audit; no A-to-B execution proof or production action.'
    save('summary.json',report)
    print(json.dumps(report,indent=2))
    print('OUTPUT',a.out)
    return int(report['status']=='stopped')


if __name__ == '__main__':
    raise SystemExit(main())
