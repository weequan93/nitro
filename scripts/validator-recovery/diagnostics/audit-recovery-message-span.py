#!/usr/bin/env python3
"""Read-only historical A-position/B message-span audit. No signing or chain mutation.

Requires cast and the existing resume-original-staker-current-fork.py dependencies.
Reads ARCHIVE_RPC from environment; local node defaults to snapshot-sync (8349).
Recording a validation input can consume node CPU/I/O; it does not replay WASM.
"""
import argparse
import importlib.util
import json
import os
import urllib.request
from pathlib import Path

spec = importlib.util.spec_from_file_location('interval', (Path(__file__).resolve().parent / '../legacy/tools/resume-original-staker-current-fork.py'))
interval = importlib.util.module_from_spec(spec)
spec.loader.exec_module(interval)
m = interval.m
ALLOWED = {'eth_chainId', 'eth_getBlockByNumber', 'eth_call',
           'arb_findBatchContainingBlock', 'arbdebug_validationInputsAt'}


def integer(value):
    return int(value, 16) if isinstance(value, str) and value.startswith('0x') else int(value)


def gs(value):
    return dict(BlockHash=value['BlockHash'].lower(), SendRoot=value['SendRoot'].lower(),
                Batch=integer(value['Batch']), PosInBatch=integer(value['PosInBatch']))


def position(value):
    return integer(value['Batch']), integer(value['PosInBatch'])


def rpc(url, method, params):
    m.require(method in ALLOWED, 'RPC method outside read-only audit scope')
    req = urllib.request.Request(url, json.dumps(dict(jsonrpc='2.0', id=1, method=method, params=params)).encode(),
                                 {'Content-Type': 'application/json'})
    with urllib.request.urlopen(req, timeout=600 if method == 'arbdebug_validationInputsAt' else 60) as response:
        data = json.load(response)
    m.require('error' not in data, 'RPC failed: ' + method)
    return data['result']


def first_block_in_batch(batch_at, low, high, target):
    """Locate transition into a nonempty batch; fail rather than guess empty batches."""
    m.require(0 <= low < high and batch_at(low) < target <= batch_at(high),
              'Batch not bracketed; review lookback or node batch availability')
    while high - low > 1:
        mid = (low + high) // 2
        if batch_at(mid) < target:
            low = mid
        else:
            high = mid
    m.require(batch_at(high) == target and batch_at(high - 1) < target,
              'Target batch empty or batch mapping inconsistent')
    return high


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('snapshot', type=Path)
    p.add_argument('--node-rpc', default='http://127.0.0.1:8349')
    p.add_argument('--out', required=True, type=Path)
    p.add_argument('--lookback-blocks', type=int, default=100000)
    a = p.parse_args()
    upstream = os.environ.get('ARCHIVE_RPC', '')
    if not upstream.startswith(('http://', 'https://')):
        p.error('Set ARCHIVE_RPC in this shell')
    if not 1 <= a.lookback_blocks <= 1000000:
        p.error('lookback-blocks must be between 1 and 1000000')
    a.out.mkdir(parents=True, exist_ok=False)
    def save(name, obj):
        (a.out/name).write_text(json.dumps(obj, indent=2) + '\n')
    report = dict(status='stopped', readyForProduction=False, executionReplayed=False,
                  productionNumBlocksApproved=False, chainMutations=False)
    stage = 'load_candidate'
    try:
        s = json.loads((a.snapshot/'summary.json').read_text())
        inv = json.loads((a.snapshot/'authority.json').read_text())
        saved_input = json.loads((a.snapshot/'checkpoint-input.json').read_text())
        before, after = s['oldConfirmedState'], s['checkpoint']
        b_height, b_message = integer(s['checkpointBlock']), integer(s['checkpointMessage'])
        m.require(s['parentBlock'] == inv['parentBlock'] and s['confirmedStateHashMatches']
                  and inv['rollup'].lower() == m.ROLLUP, 'Inconsistent historical candidate')
        m.require(position(before) < position(after) and integer(before['machineStatus']) == 1,
                  'Expected forward FINISHED-state candidate')
        m.require(integer(saved_input['Id']) == b_message and gs(saved_input['ExpectedEndState']) == gs(after),
                  'Saved B input does not match candidate')
        report.update(parentBlock=s['parentBlock'], confirmedNode=s['confirmedNode'],
                      parentConfirmedState=before, checkpoint=after, checkpointBlock=b_height)
        stage = 'verify_pinned_parent_and_B'
        m.require(integer(rpc(upstream, 'eth_chainId', [])) == 42161, 'Wrong parent chain')
        m.require(integer(rpc(a.node_rpc, 'eth_chainId', [])) == 2886, 'Wrong local chain')
        parent = rpc(upstream, 'eth_getBlockByNumber', [s['parentBlock'], False])
        m.require(parent and parent['hash'].lower() == inv['parentBlockHash'].lower(), 'Parent anchor mismatch')
        storage = rpc(upstream, 'eth_call', [{'to': m.ROLLUP,
            'data': m.encode('getNode(uint64)', s['confirmedNode'])}, s['parentBlock']])
        m.require(len(storage) == 2 + 12*64 and ('0x' + storage[2:66]).lower()
                  == interval.state_hash(before, integer(s['confirmedInboxMaxCount'])).lower(),
                  'A state commitment differs from pinned parent storage')
        b = rpc(a.node_rpc, 'eth_getBlockByNumber', [hex(b_height), False])
        m.require(b and b['hash'].lower() == after['BlockHash'].lower()
                  and b['sendRoot'].lower() == after['SendRoot'].lower(), 'B not canonical locally')
        save('checkpoint-header.json', b)
        stage = 'locate_local_A_position'
        cache = {}
        def batch_at(height):
            if height not in cache:
                # Go uint64 argument is encoded as a JSON number for this API.
                cache[height] = integer(rpc(a.node_rpc, 'arb_findBatchContainingBlock', [height]))
                save('batch-lookup.json', cache)
            return cache[height]
        print('Locating local block at parent-confirmed batch position...', flush=True)
        first = first_block_in_batch(batch_at, max(0, b_height-a.lookback_blocks), b_height,
                                     integer(before['Batch']))
        a_height = first - 1 + integer(before['PosInBatch'])
        m.require(0 <= a_height < b_height, 'Invalid local A-position block')
        # Nitro has a fixed genesis offset between block number and message index.
        # Verify the inferred index with the returned input Id/end state below.
        a_message = b_message - (b_height - a_height)
        m.require(a_message >= 0, 'Negative message index')
        stage = 'record_local_A_position_input'
        print('Recording validation input at message', a_message, '(no WASM replay)...', flush=True)
        entry = rpc(a.node_rpc, 'arbdebug_validationInputsAt', [hex(a_message), 'amd64'])
        save('local-A-position-input.json', entry)
        end = gs(entry['ExpectedEndState'])
        m.require(integer(entry['Id']) == a_message and position(end) == position(before),
                  'Local input does not end at the exact parent-confirmed position')
        ah = rpc(a.node_rpc, 'eth_getBlockByNumber', [hex(a_height), False])
        m.require(ah and ah['hash'].lower() == end['BlockHash'] and ah['sendRoot'].lower() == end['SendRoot']
                  and ah['parentHash'].lower() == entry['StartState']['BlockHash'].lower(),
                  'Recorded input/header mismatch')
        save('local-A-position-header.json', ah)
        stage = 'recheck_and_classify'
        m.require(rpc(a.node_rpc, 'eth_getBlockByNumber', [hex(b_height), False])['hash'] == b['hash']
                  and rpc(a.node_rpc, 'eth_getBlockByNumber', [hex(a_height), False])['hash'] == ah['hash'],
                  'Local chain changed during audit')
        m.require(rpc(upstream, 'eth_getBlockByNumber', [s['parentBlock'], False])['hash'] == parent['hash'],
                  'Parent anchor changed during audit')
        same_hash = end['BlockHash'] == before['BlockHash'].lower()
        same_root = end['SendRoot'] == before['SendRoot'].lower()
        report.update(status='message_span_audited', localStateAtConfirmedPosition=end,
            localBlockAtConfirmedPosition=a_height, localMessageAtConfirmedPosition=a_message,
            localMessageCountAtConfirmedPosition=a_message+1, checkpointMessage=b_message,
            checkpointMessageCount=b_message+1, positionalMessageSpan=b_message-a_message,
            confirmedBlockHashMatchesLocal=same_hash, confirmedSendRootMatchesLocal=same_root,
            confirmedGlobalStateMatchesLocal=same_hash and same_root,
            classification='same_endpoint_still_requires_execution_validation' if same_hash and same_root
                           else 'parent_confirmed_state_differs_from_local_at_same_position',
            note='positionalMessageSpan measures the retained local message interval only. '
                 'It is not proof that the parent-confirmed A executes to B, not a production '
                 'numBlocks selection, and not approval of the synthetic value 1. '
                 'Historical candidate only; input recording is not WASM replay.')
    except Exception as exc:
        report.update(failedStage=stage, errorType=type(exc).__name__)
        if isinstance(exc, ValueError):
            report['error'] = str(exc).replace(upstream, '<ARCHIVE_RPC>').replace(a.node_rpc, '<NODE_RPC>')
    save('summary.json', report)
    print(json.dumps(report, indent=2))
    print('OUTPUT', a.out)
    return 0 if report['status'] == 'message_span_audited' else 1


if __name__ == '__main__':
    raise SystemExit(main())
