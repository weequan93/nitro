#!/usr/bin/env python3
"""Replay the collected candidate message via local arbdebug; never send transactions."""
import argparse
import hashlib
import json
from pathlib import Path
from datetime import datetime, timezone
import urllib.request

ROOT = '0x121d685e2fdb0e3291592d6b90bd70d503951335d19d96455448eb7a14d17421'

def state(s):
    return {k: s[k].lower() if k.endswith('Hash') or k == 'SendRoot' else int(s[k])
            for k in ('BlockHash', 'SendRoot', 'Batch', 'PosInBatch')}

def require(ok, message):
    if not ok:
        raise ValueError(message)

def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('candidate', type=Path)
    p.add_argument('--node-rpc', default='http://127.0.0.1:8349')
    p.add_argument('--out', required=True, type=Path)
    a = p.parse_args()
    a.out.mkdir(parents=True, exist_ok=False)
    def save(name, value):
        (a.out/name).write_text(json.dumps(value, indent=2))
    def rpc(method, params):
        require(method in ('eth_chainId', 'eth_getBlockByNumber', 'web3_clientVersion',
                           'arbdebug_validateMessageNumber'), 'RPC method not allowed')
        req = urllib.request.Request(a.node_rpc,
            json.dumps(dict(jsonrpc='2.0', id=1, method=method, params=params)).encode(),
            {'Content-Type': 'application/json'})
        with urllib.request.urlopen(req, timeout=600) as response:
            result = json.load(response)
        if 'error' in result:
            raise ValueError(str(result['error']))
        return result['result']
    report = {'readyForProduction': False, 'executionReplayed': False,
              'wasmModuleRoot': ROOT, 'candidateDirectory': str(a.candidate.resolve()),
              'note': 'One-message replay from local predecessor only. No parent-confirmed A-to-B execution proof; no production transaction.'}
    try:
        summary = json.loads((a.candidate/'summary.json').read_text())
        raw = (a.candidate/'candidate-input.json').read_bytes()
        entry = json.loads(raw)
        entry = entry.get('result', entry)
        require(summary['status'] == 'candidate_input_collected_replay_required', 'Candidate collection incomplete')
        require(summary.get('aheadOfConfirmed') is True and summary.get('batchesAvailable') is True,
                'Candidate was not ahead/available at collection time')
        require(int(rpc('eth_chainId', []), 16) == 2886, 'Wrong child chain')
        number, msg = int(summary['candidateBlock']), int(entry['Id'])
        expected = state(entry['ExpectedEndState'])
        require(expected == state(summary['checkpoint']), 'Saved checkpoint differs from input')
        require(msg == int(summary['candidateMessageUnverifiedUntilInputChecked']), 'Message index mismatch')
        def verify_block():
            block = rpc('eth_getBlockByNumber', [hex(number), False])
            require(block is not None, 'Candidate block missing')
            require(block['hash'].lower() == expected['BlockHash'] == summary['candidateHash'].lower(),
                    'Candidate hash changed')
            require(block['parentHash'].lower() == entry['StartState']['BlockHash'].lower(),
                    'Candidate predecessor differs from saved input')
            require(block.get('sendRoot', block.get('extraData', '')).lower() == expected['SendRoot'],
                    'Candidate send root mismatch')
        verify_block()
        report.update(candidateBlock=number, message=msg, checkpoint=expected,
                      clientVersion=rpc('web3_clientVersion', []),
                      savedInputSha256=hashlib.sha256(raw).hexdigest())
        print('Replaying message', msg, 'with explicit WASM root', ROOT, flush=True)
        result = rpc('arbdebug_validateMessageNumber', [hex(msg), True, ROOT])
        save('validation-response.json', result)
        require(result.get('valid') is True, 'Validation did not return valid=true')
        require(state(result['globalstate']) == expected, 'Replay end state differs from candidate')
        verify_block()
        report.update(executionReplayed=True, validatedMessagesThisRun=1,
                      status='candidate_single_message_replay_passed', latency=result.get('latency'),
                      inputSource='Node re-records the pinned message; saved input is used to check predecessor and expected end state.')
    except Exception as exc:
        report.update(status='stopped', errorType=type(exc).__name__, error=str(exc))
    report['checkedAt'] = datetime.now(timezone.utc).isoformat()
    save('summary.json', report)
    print(json.dumps(report, indent=2))
    print('OUTPUT', a.out)
    return 0 if report['executionReplayed'] else 1

if __name__ == '__main__':
    raise SystemExit(main())
