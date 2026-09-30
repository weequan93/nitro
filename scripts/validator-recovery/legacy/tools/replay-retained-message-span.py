#!/usr/bin/env python3
"""Sequential, resumable replay of the fixed local A-prime -> B interval.
No transactions, Docker operations, direct node database access or production approval.
Uses node recording and explicit-root execution RPCs; this consumes node CPU/I/O.
"""
import argparse
import fcntl
import hashlib
import json
import os
import time
import urllib.request
from datetime import datetime, timezone
from pathlib import Path

ROOT = '0x121d685e2fdb0e3291592d6b90bd70d503951335d19d96455448eb7a14d17421'
START = 126229454
END = 126237291
SEND = '0xac44727106df865ba77137bcea131330935688cf48174d77b2b41a65613b03cf'
A = dict(BlockHash='0x2de7415d0a7d5e4fe6339a83ffe2cb43da57d6cd4c7118e4eea584151f0451c2',
         SendRoot=SEND, Batch=320161, PosInBatch=0)
B = dict(BlockHash='0x2654eb1fc5b10d89fa6e3a75b5ec06641aa9c2856b39c0c07468c113b2014189',
         SendRoot=SEND, Batch=320188, PosInBatch=225)
ALLOWED = {'eth_chainId', 'web3_clientVersion', 'eth_getBlockByNumber',
           'arbdebug_validationInputsAt', 'arbdebug_validateMessageNumber'}


def require(ok, msg):
    if not ok:
        raise ValueError(msg)


def now():
    return datetime.now(timezone.utc).isoformat()


def number(value):
    return int(value, 16) if isinstance(value, str) and value.startswith('0x') else int(value)


def state(s):
    return dict(BlockHash=s['BlockHash'].lower(), SendRoot=s['SendRoot'].lower(),
                Batch=number(s['Batch']), PosInBatch=number(s['PosInBatch']))


def digest(obj):
    return hashlib.sha256(json.dumps(obj, sort_keys=True, separators=(',', ':')).encode()).hexdigest()


def save(path, obj):
    tmp = path.with_name(path.name + '.tmp')
    with tmp.open('w') as f:
        json.dump(obj, f, indent=2)
        f.write('\n')
        f.flush()
        os.fsync(f.fileno())
    os.replace(tmp, path)


def rpc(url, method, params):
    require(method in ALLOWED, 'RPC outside replay scope')
    req = urllib.request.Request(url, json.dumps(dict(jsonrpc='2.0', id=1, method=method,
        params=params)).encode(), {'Content-Type': 'application/json'})
    with urllib.request.urlopen(req, timeout=600) as f:
        obj = json.load(f)
    require('error' not in obj and obj.get('result') is not None, 'RPC failed: ' + method)
    return obj['result']


def check_audit(obj):
    require(obj.get('status') == 'message_span_audited' and obj.get('parentBlock') == '0x1e58e750'
            and obj.get('confirmedNode') == 42883, 'Wrong historical audit')
    require(obj.get('localMessageAtConfirmedPosition') == START-1
            and obj.get('localBlockAtConfirmedPosition') == START-1
            and obj.get('checkpointMessage') == END and obj.get('checkpointBlock') == END
            and obj.get('localMessageCountAtConfirmedPosition') == START
            and obj.get('checkpointMessageCount') == END+1
            and obj.get('positionalMessageSpan') == END-START+1, 'Unexpected interval/offset')
    require(state(obj['localStateAtConfirmedPosition']) == A and state(obj['checkpoint']) == B,
            'Audit endpoint mismatch')
    require(obj.get('confirmedGlobalStateMatchesLocal') is False,
            'Expected historical divergent parent/local states')


def check_header(header, height, end, predecessor=None):
    require(header and number(header['number']) == height
            and header['hash'].lower() == end['BlockHash']
            and header['sendRoot'].lower() == end['SendRoot'], 'Canonical block/header mismatch')
    if predecessor is not None:
        require(header['parentHash'].lower() == predecessor['BlockHash'], 'Block parent mismatch')


def check_record(record, message, previous, previous_digest, manifest_digest):
    require(record.get('message') == message and record.get('wasmModuleRoot') == ROOT
            and record.get('manifestSha256') == manifest_digest
            and record.get('previousRecordSha256') == previous_digest, 'Record identity/chain mismatch')
    require(state(record['start']) == previous, 'Recorded state continuity mismatch')
    end = state(record['end'])
    result = record['validationResponse']
    require(result.get('valid') is True and state(result['globalstate']) == end,
            'Recorded execution not valid or end state mismatch')
    check_header(record['canonicalHeader'], message, end, previous)
    require((end['Batch'], end['PosInBatch']) > (previous['Batch'], previous['PosInBatch']),
            'Recorded message position did not advance')
    require((end['Batch'], end['PosInBatch']) <= (B['Batch'], B['PosInBatch']), 'Recorded position passed B')
    if message == END:
        require(end == B, 'Final replay state differs from B')
    return end


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--audit', required=True, type=Path)
    p.add_argument('--out', required=True, type=Path)
    p.add_argument('--node-rpc', default='http://127.0.0.1:8349')
    p.add_argument('--resume', action='store_true')
    p.add_argument('--max-messages', type=int, default=0, help='Optional per-invocation limit; 0 runs all remaining')
    a = p.parse_args()
    if a.max_messages < 0:
        p.error('max-messages must be nonnegative')
    audit = json.loads(a.audit.read_text())
    check_audit(audit)
    manifest = dict(version=1, auditSha256=digest(audit), wasmModuleRoot=ROOT,
                    firstMessage=START, lastMessage=END, startingState=A, endingState=B)
    md = digest(manifest)
    if a.resume:
        require(a.out.is_dir(), 'Resume directory missing')
    else:
        a.out.mkdir(parents=True, exist_ok=False)
    lock = (a.out/'run.lock').open('a')
    try:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
    except BlockingIOError:
        lock.close()
        p.error('Another process is already using this output directory')
    try:
        if a.resume:
            require(json.loads((a.out/'manifest.json').read_text()) == manifest, 'Resume manifest/audit differs')
        else:
            save(a.out/'manifest.json', manifest)
            save(a.out/'audit-evidence.json', audit)
            (a.out/'messages').mkdir()
    except BaseException:
        lock.close()
        raise
    previous, chain, completed, this_run = A, md, 0, 0
    started = time.monotonic()
    report = dict(status='starting', readyForProduction=False, executionReplayed=False,
        parentConfirmedAToBProven=False, productionNumBlocksApproved=False,
        wasmModuleRoot=ROOT, firstMessage=START, lastMessage=END, totalMessages=END-START+1,
        evidenceMode='Sequential per-message node recording plus explicit-root execution RPC; '
                     'each actual replay result must equal the recorded end; adjacent states must match. '
                     'The execution RPC re-records its own input; it does not execute the saved input blob. '
                     'Full input blobs are not retained; states, input digest, response and block header are retained.',
        limitations='Local A-prime to B only; initial A-prime is a retained-chain checkpoint. '
                    'Not an A-to-B proof or production approval. No new full withdrawal audit.')
    def checkpoint():
        report.update(checkedAt=now(), validatedMessagesTotal=completed, validatedMessagesThisRun=this_run,
            nextMessage=START+completed if START+completed <= END else None,
            lastValidatedState=previous, recordChainSha256=chain,
            elapsedSecondsThisRun=round(time.monotonic()-started, 2))
        save(a.out/'summary.json', report)
    try:
        # Every committed record is checked; a partial .tmp record is never credited.
        paths = sorted((a.out/'messages').glob('*.json'))
        require(len(paths) <= END-START+1, 'Too many saved records')
        for index, path in enumerate(paths):
            msg = START+index
            require(path.name == str(msg)+'.json', 'Record gap or unexpected filename')
            record = json.loads(path.read_text())
            previous = check_record(record, msg, previous, chain, md)
            chain = digest(record)
            completed += 1
        require(number(rpc(a.node_rpc, 'eth_chainId', [])) == 2886, 'Wrong node chain')
        report['clientVersionThisRun'] = rpc(a.node_rpc, 'web3_clientVersion', [])
        for height, expected in [(START-1, A), (END, B), (START+completed-1, previous)]:
            check_header(rpc(a.node_rpc, 'eth_getBlockByNumber', [hex(height), False]), height, expected)
        report['status'] = 'running'
        checkpoint()
        print('REPLAY', completed, '/', END-START+1, 'already saved; explicit root', ROOT, flush=True)
        for msg in range(START+completed, END+1):
            if a.max_messages and this_run >= a.max_messages:
                report['status'] = 'batch_complete'
                break
            report['currentMessage'] = msg
            checkpoint()
            print('REPLAYING', msg, flush=True)
            entry = rpc(a.node_rpc, 'arbdebug_validationInputsAt', [hex(msg), 'amd64'])
            require(number(entry['Id']) == msg and state(entry['StartState']) == previous,
                    'Input Id/start state differs from preceding validated state')
            end = state(entry['ExpectedEndState'])
            header = rpc(a.node_rpc, 'eth_getBlockByNumber', [hex(msg), False])
            check_header(header, msg, end, previous)
            input_hash = digest(entry)
            del entry
            response = rpc(a.node_rpc, 'arbdebug_validateMessageNumber', [hex(msg), True, ROOT])
            record = dict(message=msg, wasmModuleRoot=ROOT, start=previous, end=end,
                recordedInputSha256=input_hash, validationResponse=response,
                canonicalHeader={k: header[k] for k in ('number', 'hash', 'parentHash', 'sendRoot')},
                manifestSha256=md, previousRecordSha256=chain, checkedAt=now())
            save(a.out/'last-attempt.json', record)
            new_previous = check_record(record, msg, previous, chain, md)
            check_header(rpc(a.node_rpc, 'eth_getBlockByNumber', [hex(msg), False]), msg, end, previous)
            save(a.out/'messages'/f'{msg}.json', record)
            previous, chain = new_previous, digest(record)
            completed += 1
            this_run += 1
            checkpoint()
            print('PASS', msg, 'progress', completed, '/', END-START+1,
                  'latency', response.get('latency'), flush=True)
        if completed == END-START+1:
            require(previous == B, 'Final state differs from B')
            for height, expected in [(START-1, A), (END, B)]:
                check_header(rpc(a.node_rpc, 'eth_getBlockByNumber', [hex(height), False]), height, expected)
            report.update(status='retained_span_replay_passed', executionReplayed=True)
    except KeyboardInterrupt:
        report['status'] = 'interrupted'
    except Exception as exc:
        report.update(status='stopped', errorType=type(exc).__name__)
        if isinstance(exc, ValueError):
            report['error'] = str(exc).replace(a.node_rpc, '<NODE_RPC>')
    checkpoint()
    print(json.dumps(report, indent=2))
    print('OUTPUT', a.out)
    lock.close()
    return 0 if report['status'] in ('retained_span_replay_passed', 'batch_complete') else 1


if __name__ == '__main__':
    raise SystemExit(main())
