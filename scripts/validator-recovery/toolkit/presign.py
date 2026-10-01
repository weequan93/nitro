#!/usr/bin/env python3
"""Prepare fixed consecutive Safe transactions; only owned forks may send transactions.

This is an additive entry point. Existing recovery.py execution gates are unchanged.
"""
import argparse
import importlib.util
import shutil
import sys
from types import SimpleNamespace
from core import *
import collect as collector
import recovery

DRIFT_FIELDS = ('routes', 'codeHashes', 'wasm', 'confirmed', 'created',
                'firstUnresolved', 'confirmedStorage', 'sibling', 'lastHash',
                'stakers', 'fastConfirmer')


def pause_package(path):
    p, identity = load_package(path)
    require(p['kind'] == 'pause' and p['purpose'] == 'production-review',
            'A reviewed production pause package is required')
    return p, identity


def signing_anchor(pre, pause):
    # This checks the signing baseline, not that pause has executed.
    require(pre['safeNonce'] == pause['nonce'], 'Pause nonce already consumed; use the post-pause workflow')
    require(not pre['paused'], 'Already paused; use the post-pause workflow')
    require(pre['routes'] == pause['pre']['routes'] and
            pre['codeHashes'] == pause['pre']['codeHashes'], 'Pause authority baseline changed')


def collect_before_pause(a):
    pause, identity = pause_package(a.pause)
    # Reuse only input collection with its pre-pause allowance. No simulation
    # transaction/package is converted into a production package.
    collector.collect(SimpleNamespace(parent_rpc=a.parent_rpc, node_rpc=a.node_rpc,
        block=a.block, lookback=a.lookback, simulation=True, out=a.out))
    pre = read(a.out / 'inventory.json')
    signing_anchor(pre, pause)
    save(a.out / 'presign-input.json', dict(schema=1, mode='fixed-pre-pause-input',
        pausePackageIdentity=identity, pauseNonce=pause['nonce'],
        inventorySha256=digest(pre),
        candidateSha256=digest(read(a.out / 'candidate/summary.json')),
        auditSha256=digest(read(a.out / 'audit/summary.json')),
        requiresExecutionRecheck=True, readyForProduction=False))
    print('PRE_SIGN_INPUT', a.out)


def write_presign(d, p, r):
    p['presignedSequence'] = True
    recovery.write_package(d, p, r)
    review = d / 'REVIEW.md'
    text = review.read_text().replace(
        'resume 必须在恢复回执和运行验收后单独生成。',
        'resume 已提前生成；仅在恢复回执及运行验收通过后执行。')
    text += '''
## 预签与执行分离

- 这是固定内容的预签包，不是执行许可。签名不会随链上状态自动更新。
- 执行前使用 presign.py check；预检只在链下生效，不阻止绕过预检直接执行。
- Safe nonce 只约束顺序，不保证前一笔成功；失败消耗 nonce 后禁止继续后续交易。
- 任何持有足够有效签名的人都可能执行；“操作员执行”由团队协调保证。
- 状态漂移导致不匹配时重新审阅、重建、重签；不能只修改 nonce。
'''
    review.write_text(text)
    seal(d)


def build_sequence(a):
    pause, pause_id = pause_package(a.pause)
    marker = read(a.window / 'presign-input.json')
    c = read(a.window / 'candidate/summary.json')
    audit = read(a.window / 'audit/summary.json')
    decision = read(a.decision)
    pre = read(a.window / 'inventory.json')
    require(marker['mode'] == 'fixed-pre-pause-input' and marker['pausePackageIdentity'] == pause_id,
            'Window belongs to a different pause package')
    require(marker['inventorySha256'] == digest(pre) and marker['candidateSha256'] == digest(c)
            and marker['auditSha256'] == digest(audit), 'Collected input changed')
    signing_anchor(pre, pause)
    require(decision['mode'] == 'approved-parameters', 'Concrete parameter review is required before signing')
    require(decision['candidateSha256'] == digest(c) and decision['auditSha256'] == digest(audit),
            'Decision belongs to different input')
    require(decision['acknowledgesTrustedMigration'] is True and
            decision['acknowledgesNoParentAToBProof'] is True and
            isinstance(decision['reviewReference'], str) and decision['reviewReference'].strip(),
            'Trusted migration review missing')
    A, B = state(c['oldConfirmedState']), state(c['checkpoint'])
    span, count = decision['numBlocks'], c['confirmedInboxMaxCount']
    require(type(span) is int and 0 < span < 2**64, 'Invalid span')
    require(c['parentBlock'] == audit['parentBlock'] == pre['parentBlock'] and
            c['confirmedNode'] == audit['confirmedNode'] == pre['confirmed'], 'Mixed anchors')
    validate_pre(pre, A, B, count, simulation=True)
    ev = evidence(audit, a.replay, A, B, span)
    require(c['checkpointBlock'] == c['checkpointMessage'] == ev['manifest']['lastMessage'],
            'Message/block mapping differs')
    r = RPC(a.parent_rpc)
    require(int(r.rpc('eth_chainId', []), 16) == 42161, 'Wrong parent chain')
    require(inventory(r, pre['parentBlock']) == pre, 'Pinned inventory changed')
    live = inventory(r)
    signing_anchor(live, pause)
    require(all(live[k] == pre[k] for k in DRIFT_FIELDS), 'State changed during preparation; recollect')
    node = RPC(a.node_rpc)
    require(int(node.rpc('eth_chainId', []), 16) == 2886, 'Wrong L3 chain')
    h = node.rpc('eth_getBlockByNumber', [hex(c['checkpointBlock']), False])
    require(h and h['hash'].lower() == B['BlockHash'] and h['sendRoot'].lower() == B['SendRoot'],
            'B is no longer canonical')
    needed = B['Batch'] + bool(B['PosInBatch'])
    acc = r.call(BRIDGE, 'sequencerInboxAccs(uint256)', needed-1, tag=pre['parentBlock']) if needed else ZERO
    txs, nh = transactions(pre, A, B, span, count, acc)
    d = output(a.out)
    shutil.copytree(a.pause, d / 'pause')
    rd = d / 'recovery'; rd.mkdir()
    save(rd / 'evidence.json', ev); save(rd / 'decision.json', decision); save(rd / 'candidate.json', c)
    p = dict(kind='recovery', purpose='production-review', pre=pre, before=A, after=B,
        beforeInboxCount=count, numBlocks=span, accumulator=acc, nodeHash=nh,
        recoveryNode=pre['created']+1, nonce=pause['nonce']+1, transactions=txs,
        decisionMode=decision['mode'], evidenceSha256=digest(ev), simulatePause=True,
        checkpointBlock=c['checkpointBlock'], pausePackageIdentity=pause_id)
    write_presign(rd, p, r)
    _, recovery_id = load_package(rd)
    sd = d / 'resume'; sd.mkdir()
    resume = dict(kind='resume', purpose='production-review', pre=pre,
        nonce=pause['nonce']+2, transactions=[admin('resume()')],
        recoveryPackageIdentity=recovery_id, requiresActualRecoveryReceipt=True,
        requiresRuntimeAcceptance=True)
    write_presign(sd, resume, r)
    packages = {name: load_package(d/name) for name in ('pause', 'recovery', 'resume')}
    save(d / 'sequence.json', dict(schema=1, packages={k:v[1] for k,v in packages.items()},
        nonces={k:v[0]['nonce'] for k,v in packages.items()},
        safeTxHashes={k:v[0]['expectedSafeTxHash'] for k,v in packages.items()},
        executionGates='off-chain-only', exclusiveExecutorEnforced=False,
        readyForProduction=False, productionTransactionsSent=False))
    print('PRE_SIGN_SEQUENCE', d)
    print(json.dumps(read(d / 'sequence.json'), indent=2))


def sequence(path):
    doc = read(path / 'sequence.json')
    require(doc['schema'] == 1, 'Unknown sequence schema')
    p = {}
    for kind in ('pause', 'recovery', 'resume'):
        package, identity = load_package(path / kind)
        require(identity == doc['packages'][kind] and package['kind'] == kind
                and package['purpose'] == 'production-review', 'Sequence package identity mismatch')
        require(package['nonce'] == doc['nonces'][kind] and
                package['expectedSafeTxHash'] == doc['safeTxHashes'][kind], 'Sequence fields mismatch')
        p[kind] = package
    n = p['pause']['nonce']
    require(p['recovery']['nonce'] == n+1 and p['resume']['nonce'] == n+2, 'Nonconsecutive nonces')
    require(p['recovery']['pausePackageIdentity'] == doc['packages']['pause'] and
            p['resume']['recoveryPackageIdentity'] == doc['packages']['recovery'], 'Dependencies differ')
    return p, doc


def load_fork_module():
    spec = importlib.util.spec_from_file_location('owned_presign_fork', Path(__file__).with_name('verify-fork.py'))
    module = importlib.util.module_from_spec(spec); spec.loader.exec_module(module)
    return module


def verify_sequence(a):
    packages, doc = sequence(a.sequence)
    p = packages['recovery']; v = load_fork_module(); d = output(a.out); f = None
    report = dict(status='stopped', packages=doc['packages'], readyForProduction=False,
        productionTransactionsSent=False, productionSignaturesTested=False,
        nativeArbitrumExecutionTested=False)
    try:
        f = v.Fork(a.parent_rpc, p['pre']['parentBlock'], p['pre']['parentBlockHash'], d)
        require(inventory(f) == p['pre'], 'Fork inventory differs')
        f.call('0x0000000000000000000000000000000000000064', 'arbBlockNumber()')
        def run(kind):
            package = packages[kind]
            result = v.execute(f, package['transactions'], package['nonce'])
            require(result['safeFields'] == package['safeFields'], 'Fork Safe fields differ')
            recovery.safe_receipt(f, dict(package, expectedSafeTxHash=result['safeTxHash']),
                result['receipt']['transactionHash'], expected_chain=31337)
            save(d / (kind + '-result.json'), result)
            return result['receipt']
        run('pause'); require(f.num(ROLLUP, 'paused()') == 1, 'Pause failed')
        snap = f.mutate('evm_snapshot', []); before = v.checked_state(f, p)
        bad = list(p['transactions'])
        bad[-1] = admin('forceConfirmNode(uint64,bytes32,bytes32)', p['recoveryNode'], ZERO, p['after']['SendRoot'])
        save(d / 'negative.json', v.execute(f, bad, p['nonce'], negative=True))
        require(v.checked_state(f, p) == before, 'Inner rollback failed')
        # The failed Safe execution consumes a nonce: this is why nonce order
        # alone does not authorize executing the pre-signed resume.
        require(f.num(SAFE, 'nonce()') == packages['resume']['nonce'], 'Unexpected failure nonce')
        rejected = False
        try: recovery.recovery_post(f, p)
        except ValueError: rejected = True
        require(rejected, 'Failed recovery wrongly passed resume prerequisite')
        require(f.mutate('evm_revert', [snap]) is True, 'Fork snapshot restore failed')
        rec = run('recovery')
        report['recoveryPost'] = recovery.recovery_post(f, p)
        report['actualInboxMaxCount'] = recovery.created_event(f, p, rec)
        run('resume'); require(f.num(ROLLUP, 'paused()') == 0, 'Resume failed')
        report.update(status='exact_presigned_sequence_fork_passed',
            exactThreeTransactionsTested=True, lateFailureRollbackTested=True,
            resumePrerequisiteRejectsFailedRecovery=True, thresholdNegativeTested=True,
            limitations='Owned historical fork; impersonated approvals, fork hashes use chain31337. No production ECDSA, fresh WASM replay or runtime acceptance. Execution gates remain off-chain.')
    except Exception as e:
        report['error'] = str(e) if isinstance(e, ValueError) else type(e).__name__
    finally:
        if f: f.close()
        report['ownedForkStopped'] = True
        save(d / 'summary.json', report)
    print(json.dumps(report, indent=2))
    return 0 if report['status'] == 'exact_presigned_sequence_fork_passed' else 1


def require_receipt_order(pause_rec, recovery_rec):
    a = (int(pause_rec['blockNumber'], 16), int(pause_rec['transactionIndex'], 16))
    b = (int(recovery_rec['blockNumber'], 16), int(recovery_rec['transactionIndex'], 16))
    require(a < b, 'Recovery must execute after the reviewed pause')


def check_execution(a):
    packages, doc = sequence(a.sequence)
    p = packages['recovery']; r = RPC(a.parent_rpc)
    require(int(r.rpc('eth_chainId', []), 16) == 42161, 'Wrong chain')
    proof = read(a.fork_pass / 'summary.json')
    require(proof['status'] == 'exact_presigned_sequence_fork_passed' and
            proof['packages'] == doc['packages'], 'Exact sequence fork pass required')
    if a.stage == 'pause':
        live = inventory(r)
        signing_anchor(live, packages['pause'])
        require(all(live[k] == p['pre'][k] for k in DRIFT_FIELDS), 'Prepared recovery baseline drifted')
    else:
        require(a.pause_tx, 'Actual pause execution transaction required for execution check')
        pause_rec = recovery.safe_receipt(r, packages['pause'], b32(a.pause_tx))
        require(r.num(ROLLUP, 'paused()', tag=pause_rec['blockNumber']) == 1, 'Pause post-state mismatch')
        if a.stage == 'recovery':
            live = recovery.check_drift(r, p)
            require(a.node_rpc, 'L3 RPC required to recheck B')
            node = RPC(a.node_rpc)
            require(int(node.rpc('eth_chainId', []), 16) == 2886, 'Wrong L3')
            h = node.rpc('eth_getBlockByNumber', [hex(p['checkpointBlock']), False])
            require(h and h['hash'].lower() == p['after']['BlockHash'] and
                    h['sendRoot'].lower() == p['after']['SendRoot'], 'B changed')
        else:
            require(a.recovery_tx, 'Actual recovery execution transaction required')
            rec = recovery.safe_receipt(r, p, b32(a.recovery_tx))
            require_receipt_order(pause_rec, rec)
            recovery.recovery_post(r, p, rec['blockNumber']); recovery.created_event(r, p, rec)
            live = inventory(r)
            recovery.recovery_post(r, p, live['parentBlock'])
            require(live['safeNonce'] == packages['resume']['nonce'], 'Resume nonce changed')
            require(live['routes'] == p['pre']['routes'] and live['codeHashes'] == p['pre']['codeHashes'],
                    'Authority baseline changed')
            require(a.runtime_review, 'Concrete runtime acceptance record required')
            review = read(a.runtime_review)
            require(review.get('recoveryPackageIdentity') == doc['packages']['recovery'] and
                    review.get('recoveryTransaction', '').lower() == a.recovery_tx.lower() and
                    review.get('accepted') is True and
                    isinstance(review.get('reviewReference'), str) and review['reviewReference'].strip(),
                    'Runtime acceptance missing or for different recovery')
            report_note = 'Operator runtime review is an attestation, not a cryptographic runtime proof.'
    selected = packages[a.stage]
    require(r.call(SAFE, TX_HASH, *hash_fields(selected['safeFields']), selected['nonce'],
                   tag=live['parentBlock']) == selected['expectedSafeTxHash'], 'Safe hash changed')
    d = output(a.out)
    save(d / 'summary.json', dict(status='presigned_stage_execution_check_passed', stage=a.stage,
        packages=doc['packages'], parentBlock=live['parentBlock'], parentBlockHash=live['parentBlockHash'],
        expectedSafeTxHash=selected['expectedSafeTxHash'], readyForProduction=False,
        note='Off-chain point-in-time check; cannot prevent another holder executing signatures.',
        runtimeReviewNote=report_note if a.stage == 'resume' else None))
    print('EXECUTION_CHECK', d)


def main():
    parser = argparse.ArgumentParser(description=__doc__); sub = parser.add_subparsers(dest='command', required=True)
    c = sub.add_parser('collect'); c.add_argument('--pause', type=Path, required=True)
    c.add_argument('--block', type=int); c.add_argument('--lookback', type=int, default=100000)
    c.set_defaults(fn=collect_before_pause)
    b = sub.add_parser('build'); b.add_argument('--pause', type=Path, required=True)
    b.add_argument('--window', type=Path, required=True); b.add_argument('--replay', type=Path, required=True)
    b.add_argument('--decision', type=Path, required=True); b.set_defaults(fn=build_sequence)
    v = sub.add_parser('verify'); v.add_argument('--sequence', type=Path, required=True); v.set_defaults(fn=verify_sequence)
    k = sub.add_parser('check'); k.add_argument('stage', choices=['pause','recovery','resume'])
    k.add_argument('--sequence', type=Path, required=True); k.add_argument('--fork-pass', type=Path, required=True)
    k.add_argument('--pause-tx'); k.add_argument('--recovery-tx'); k.add_argument('--runtime-review', type=Path)
    k.set_defaults(fn=check_execution)
    for cmd in (c, b, v, k):
        cmd.add_argument('--parent-rpc', required=True); cmd.add_argument('--out', type=Path, required=True)
    for cmd in (c,b): cmd.add_argument('--node-rpc', default='http://127.0.0.1:8349')
    k.add_argument('--node-rpc')
    a = parser.parse_args()
    try: return a.fn(a) or 0
    except Exception as e:
        print('STOP:', str(e) if isinstance(e, ValueError) else type(e).__name__, file=sys.stderr)
        return 1

if __name__ == '__main__': raise SystemExit(main())
