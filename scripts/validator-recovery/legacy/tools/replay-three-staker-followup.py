#!/usr/bin/env python3
"""Replay B to the fixed parent inbox boundary for the next runtime rehearsal.
No transactions, Docker operations, or direct database access.
"""
import argparse
import json
import os
from pathlib import Path
import importlib.util

spec = importlib.util.spec_from_file_location('three_atomic_followup', (Path(__file__).resolve().parent / 'rehearse-three-staker-atomic.py'))
w = importlib.util.module_from_spec(spec)
spec.loader.exec_module(w)
r, m = w.r, w.m
r.ALLOWED.add('eth_call')
EXPECTED = '0xd86bb1e3277e2cf5565f945d2954f670f4b4dc88df11f1d53359b706fd5f5da2'


def check_pass(report):
    m.require(report.get('status') == 'contract_rehearsal_passed'
        and report.get('test') == 'three_staker_atomic_span_10578'
        and report.get('recoveryNode') == 43008 and report.get('paused') is True
        and report.get('governanceNumBlocks') == 10578
        and report.get('exactExpectedNodeHash') == EXPECTED
        and report.get('spanEvidence', {}).get('recordChainSha256') == w.CHAIN
        and report.get('stakers') == w.STAKERS
        and report.get('innerRollbackTested') is True
        and report.get('atomicSuccessTested') is True, 'Wrong atomic pass')


def collect(snapshot, prior_path, node_rpc, upstream, out):
    s = w.read(snapshot/'summary.json')
    inv = w.read(snapshot/'authority.json')
    w.check_fixture(s, inv)
    prior = w.read(prior_path)
    check_pass(prior)
    out.mkdir(parents=True, exist_ok=False)
    (out/'messages').mkdir()
    report = dict(status='running', readyForProduction=False, executionReplayed=False,
        parentConfirmedAToBProven=False, chainMutations=False, wasmModuleRoot=r.ROOT,
        parentBlock=s['parentBlock'], parentBlockHash=w.ANCHOR, recoveryNode=43008,
        candidateDirectory=str(snapshot.resolve()), atomicPass=str(prior_path.resolve()),
        startMessage=s['checkpointMessage'], startBlock=s['checkpointBlock'],
        startState=s['checkpoint'], validatedMessagesTotal=0,
        limitations='Fresh local B-to-C replay only. Does not prove parent A-to-B or production readiness.')
    def save(name, data):
        r.save(out/name, data)
    def parent(method, params):
        return r.rpc(upstream, method, params)
    def node(method, params):
        return r.rpc(node_rpc, method, params)
    def header(msg, state, previous=None):
        h = node('eth_getBlockByNumber', [hex(msg), False])
        r.check_header(h, msg, state, previous)
        return {k: h[k] for k in ('number', 'hash', 'parentHash', 'sendRoot')}
    started = r.time.monotonic()
    try:
        m.require(r.number(parent('eth_chainId', [])) == 42161, 'Wrong parent chain')
        m.require(r.number(node('eth_chainId', [])) == 2886, 'Wrong node chain')
        m.require(parent('eth_getBlockByNumber', [s['parentBlock'], False])['hash'].lower() == w.ANCHOR,
                  'Parent block changed')
        bridge = '0x'+parent('eth_call', [{'to':m.ROLLUP, 'data':m.encode('bridge()')}, s['parentBlock']])[-40:]
        count = int(parent('eth_call', [{'to':bridge, 'data':m.encode('sequencerMessageCount()')}, s['parentBlock']]), 16)
        m.require(count == s['bridgeInboxCount'] == 321868, 'Wrong fixed inbox count')
        m.require(s['checkpointBlock'] == s['checkpointMessage'], 'Unsupported message offset')
        previous = r.state(s['checkpoint'])
        save('start-header.json', header(s['checkpointMessage'], previous))
        manifest = dict(version=1, parentBlock=s['parentBlock'], parentBlockHash=w.ANCHOR,
            candidateSha256=r.digest(s), atomicPassSha256=r.digest(prior),
            startingMessage=s['checkpointMessage'], startingState=previous,
            endingPosition=[count, 0], wasmModuleRoot=r.ROOT)
        save('manifest.json', manifest)
        md = r.digest(manifest)
        chain = md
        report['clientVersionThisRun'] = node('web3_clientVersion', [])
        for msg in range(s['checkpointMessage']+1, s['checkpointMessage']+4097):
            report.update(currentMessage=msg, checkedAt=r.now())
            save('summary.json', report)
            entry = node('arbdebug_validationInputsAt', [hex(msg), 'amd64'])
            m.require(r.number(entry['Id']) == msg and r.state(entry['StartState']) == previous,
                      'Input continuity mismatch')
            end = r.state(entry['ExpectedEndState'])
            pos = (end['Batch'], end['PosInBatch'])
            m.require((previous['Batch'], previous['PosInBatch']) < pos <= (count, 0),
                      'Message outside fixed parent inbox or non-advancing')
            h = header(msg, end, previous)
            response = node('arbdebug_validateMessageNumber', [hex(msg), True, r.ROOT])
            m.require(response.get('valid') is True and r.state(response['globalstate']) == end,
                      'Explicit-root execution failed or end mismatch')
            header(msg, end, previous)
            record = dict(message=msg, wasmModuleRoot=r.ROOT, start=previous, end=end,
                recordedInputSha256=r.digest(entry), validationResponse=response, canonicalHeader=h,
                manifestSha256=md, previousRecordSha256=chain, checkedAt=r.now())
            save('messages/'+str(msg)+'.json', record)
            previous, chain = end, r.digest(record)
            report.update(validatedMessagesTotal=msg-s['checkpointMessage'], endMessage=msg,
                endBlock=msg, endState=end, recordChainSha256=chain)
            print('PASS', msg, 'messages', report['validatedMessagesTotal'], 'position', pos, flush=True)
            save('summary.json', report)
            if pos == (count, 0):
                break
        else:
            raise ValueError('Boundary not reached within 4096 messages')
        header(s['checkpointMessage'], r.state(s['checkpoint']))
        header(report['endMessage'], previous)
        m.require(parent('eth_getBlockByNumber', [s['parentBlock'], False])['hash'].lower() == w.ANCHOR,
                  'Parent anchor changed')
        report.update(status='followup_span_replay_passed', executionReplayed=True,
            numBlocks=report['validatedMessagesTotal'], fixedInboxBoundaryReached=True)
    except Exception as exc:
        report.update(status='stopped', errorType=type(exc).__name__)
        if isinstance(exc, ValueError):
            report['error'] = str(exc).replace(upstream, '<PARENT_RPC>').replace(node_rpc, '<NODE_RPC>')
    report.update(checkedAt=r.now(), elapsedSeconds=round(r.time.monotonic()-started, 2))
    save('summary.json', report)
    print(json.dumps(report, indent=2))
    print('OUTPUT', out)
    return 0 if report['status'] == 'followup_span_replay_passed' else 1


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('snapshot', type=Path)
    p.add_argument('--atomic-pass', required=True, type=Path)
    p.add_argument('--out', required=True, type=Path)
    p.add_argument('--node-rpc', default='http://127.0.0.1:8349')
    a = p.parse_args()
    upstream = os.environ.get('ARCHIVE_RPC', '')
    if not upstream.startswith(('http://', 'https://')):
        p.error('Set ARCHIVE_RPC to an Arbitrum One archive URL')
    return collect(a.snapshot, a.atomic_pass, a.node_rpc, upstream, a.out)


if __name__ == '__main__':
    raise SystemExit(main())
