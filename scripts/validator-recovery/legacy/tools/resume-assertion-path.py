#!/usr/bin/env python3
"""Validate a contiguous local message path with one WASM root, stopping at any mismatch.
Does not change staking, validator progress, chain configuration, or database state.
This cannot follow a divergent WASM state using witnesses from the original history.
"""
import argparse
import json
import urllib.request
from pathlib import Path

KEYS = ('BlockHash', 'SendRoot', 'Batch', 'PosInBatch')

def state(s):
    return {k: s[k] for k in KEYS}

def position(s):
    return int(s['Batch']), int(s['PosInBatch'])

def load_checkpoint(directory, assertion, root, start, target, parent_root):
    previous = json.loads((directory / 'summary.json').read_text())
    if (previous.get('assertion') != assertion or previous.get('root') != root
            or previous.get('start') != start
            or previous.get('parentExpectedEnd') != target
            or previous.get('parentAssertionRoot') != parent_root):
        raise ValueError('Checkpoint root or assertion boundaries differ')
    rows = [json.loads(line) for line in
            (directory / 'steps.jsonl').read_text().splitlines() if line.strip()]
    if not rows or len(rows) != previous.get('validatedMessages'):
        raise ValueError('Checkpoint steps/count inconsistent; refusing to skip messages')
    current = start
    last_message = None
    for row in rows:
        if (state(row['start']) != current
                or (last_message is not None and row['message'] != last_message + 1)
                or not position(current) < position(row['end']) <= position(target)):
            raise ValueError('Checkpoint steps are not a contiguous state path')
        current = state(row['end'])
        last_message = row['message']
    if current != previous.get('lastValidatedEnd'):
        raise ValueError('Checkpoint last state disagrees with summary')
    if position(current) >= position(target):
        raise ValueError('Checkpoint already reached assertion end; inspect its summary')
    return rows, current, last_message + 1

def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('directory', type=Path)
    p.add_argument('--node-rpc', default='http://127.0.0.1:8149')
    p.add_argument('--root', required=True)
    p.add_argument('--assertion', type=int, default=42560)
    p.add_argument('--first-message', type=int, default=125022161)
    p.add_argument('--max-messages', type=int, default=1,
                   help='default one-message preflight; increase explicitly for interval validation')
    p.add_argument('--fast', action='store_true', help='record first input only, then verify consecutive block headers and WASM results')
    p.add_argument('--timeout', type=int, default=600)
    p.add_argument('--out', type=Path, required=True)
    p.add_argument('--resume', type=Path, required=True,
                   help='Previous output directory containing summary.json and steps.jsonl')
    a = p.parse_args()
    if a.max_messages < 1:
        p.error('--max-messages must be positive')
    a.out.mkdir(parents=True, exist_ok=False)
    def rpc(method, params):
        request = urllib.request.Request(a.node_rpc, json.dumps({
            'jsonrpc':'2.0','id':1,'method':method,'params':params
        }).encode(), {'Content-Type':'application/json'})
        with urllib.request.urlopen(request, timeout=a.timeout) as response:
            result = json.load(response)
        if 'error' in result:
            raise ValueError(json.dumps(result['error']))
        if result.get('result') is None:
            raise ValueError(method + ' returned null')
        return result['result']
    def save(name, value):
        (a.out / name).write_text(json.dumps(value, indent=2)+'\n')
    report = {'assertion':a.assertion, 'root':a.root, 'validatedMessages':0,
              'completedLocalInterval':False, 'matchesParentAssertion':None}
    entry = None
    try:
        if int(rpc('eth_chainId', []),16) != 2886:
            raise ValueError('Expected Deriw chain 2886')
        report['clientVersion'] = rpc('web3_clientVersion', [])
        nodes = json.loads((a.directory / 'rollup-assertion-probe.json').read_text())['nodes']
        node = next(n for n in nodes if n['node'] == a.assertion)
        current, target = state(node['before']), state(node['after'])
        report.update({'start':current, 'parentExpectedEnd':target,
                       'parentAssertionRoot':node['wasmModuleRoot']})
        if position(current) >= position(target):
            raise ValueError('Unexpected assertion position order')
        prior_rows, current, first_message = load_checkpoint(
            a.resume, a.assertion, a.root, current, target, node['wasmModuleRoot'])
        report.update({'resumedFrom':str(a.resume.resolve()),
                       'previouslyValidatedMessages':len(prior_rows),
                       'validatedMessages':len(prior_rows),
                       'lastValidatedEnd':current,
                       'resumeMessage':first_message})
        print('RESUME', first_message, 'from position', position(current), flush=True)
        next_block_number = None
        with (a.out / 'steps.jsonl').open('w') as steps:
            for row in prior_rows:
                steps.write(json.dumps(row)+'\n')
            steps.flush()
            for msg in range(first_message, first_message + a.max_messages):
                entry = None
                report['checkingMessage'] = msg
                expected = None
                if not a.fast or next_block_number is None:
                    entry = rpc('arbdebug_validationInputsAt', [hex(msg),'amd64'])
                    if state(entry['StartState']) != current:
                        raise ValueError('StartState does not equal previous validated state; cannot continue')
                    expected = state(entry['ExpectedEndState'])
                header = None
                if a.fast:
                    if next_block_number is None:
                        header = rpc('eth_getBlockByHash', [expected['BlockHash'], False])
                        next_block_number = int(header['number'], 16)
                    else:
                        header = rpc('eth_getBlockByNumber', [hex(next_block_number), False])
                    if header['parentHash'].lower() != current['BlockHash'].lower():
                        raise ValueError('Block parent differs from previous validated state')
                    if int(header['number'],16) != next_block_number:
                        raise ValueError('Unexpected block number')
                # true selects an execution spawner; this still runs the selected WASM root.
                response = rpc('arbdebug_validateMessageNumber', [hex(msg), True, a.root])
                report['lastValidationResponse'] = response
                got = response.get('globalstate')
                if response.get('valid') is not True or not isinstance(got,dict) or (expected is not None and state(got) != expected):
                    raise ValueError('WASM result differs from local expected state; stopped at first mismatch')
                expected = state(got)
                if not position(current) < position(expected) <= position(target):
                    raise ValueError('Unexpected position progression or assertion boundary overshoot')
                if a.fast:
                    if expected['BlockHash'].lower() != header['hash'].lower():
                        raise ValueError('Validated hash does not match linked block header')
                    next_block_number += 1
                row = {'message':msg, 'start':current, 'end':expected}
                steps.write(json.dumps(row)+'\n')
                steps.flush()
                current = expected
                report['validatedMessages'] += 1
                report['lastValidatedEnd'] = current
                if msg == first_message or report['validatedMessages'] % 25 == 0:
                    print('PASS', msg, 'position', position(current), flush=True)
                if position(current) == position(target):
                    report['completedLocalInterval'] = True
                    report['matchesParentAssertion'] = current == target
                    report['status'] = 'interval_completed'
                    break
            else:
                report['status'] = 'message_limit_reached_interval_not_complete'
    except KeyboardInterrupt:
        report['status'] = 'interrupted_interval_not_complete'
    except Exception as exc:
        report['status'] = 'stopped'
        report['error'] = str(exc) if isinstance(exc,ValueError) else type(exc).__name__
        if entry is not None:
            save('stopped-validation-input.json', {'result':entry})
    save('summary.json', report)
    print(json.dumps(report,indent=2),flush=True)
    print('OUTPUT',a.out)
    return 1 if report['status'] == 'stopped' else 0

if __name__ == '__main__':
    raise SystemExit(main())
