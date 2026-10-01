#!/usr/bin/env python3
"""Historical Anvil-only test: refund both known stakers, rejoin, ordinary confirm.

Requires the existing resume-original-staker-current-fork.py,
rehearse-safe-recovery.py and mock-validator-recovery.py dependencies.
Reads ARCHIVE_RPC from the environment. Never opens a node database or uses Docker.
"""
import argparse
import importlib.util
import json
import os
import shutil
import socket
import subprocess
import time
from pathlib import Path
from urllib.parse import urlsplit

spec = importlib.util.spec_from_file_location(
    'interval', Path(__file__).with_name('resume-original-staker-current-fork.py'))
interval = importlib.util.module_from_spec(spec)
spec.loader.exec_module(interval)
safe, m = interval.safe, interval.m
ACTIVE = interval.OLD
DORMANT = '0xd38e969ae2947019e0dec0e46937e6aff651834d'
STAKERS = [ACTIVE, DORMANT]
ANCHOR = '0xc4a99db9563e557c6544abe0e4299ae14f8c641a7c96e938178d2308b5c29c72'


def save(path, obj):
    path.write_text(json.dumps(obj, indent=2) + '\n')


def words(raw, count):
    m.require(isinstance(raw, str) and raw.startswith('0x') and len(raw) == 2 + count * 64,
              'Unexpected contract return ABI')
    bytes.fromhex(raw[2:])
    return ['0x' + raw[i:i+64] for i in range(2, len(raw), 64)]


def check_fixture(summary, authority, evidence):
    m.require(summary.get('status') == 'checkpoint_single_message_replayed_fork_test_still_required',
              'Unexpected candidate status')
    m.require(summary.get('parentBlock') == '0x1e58e750'
              and authority.get('parentBlockHash', '').lower() == ANCHOR,
              'This test only supports the fixed historical candidate')
    m.require(summary.get('confirmedNode') == 42883 and summary.get('bridgeInboxCount') == 320189,
              'Unexpected candidate node/inbox')
    m.require(summary.get('checkpoint') == {k: v for k, v in interval.BEFORE.items() if k != 'machineStatus'}
              and summary.get('newWasmRoot') == interval.ROOT
              and summary.get('checkpointMessage') == 126237291,
              'Wrong candidate checkpoint/root/message')
    m.require(summary.get('checkpointAheadOfConfirmed') and summary.get('checkpointBatchesAvailable'),
              'Candidate was not ahead/available at its historical anchor')
    m.require(evidence.get('status') == 'nitro_restart_rehearsal_passed'
              and evidence.get('gracefulRestartTested') is True
              and evidence.get('normalConfirmedNode') == 42887
              and evidence.get('numBlocks') == 64
              and evidence.get('automaticCreationTested') is True
              and evidence.get('automaticConfirmationTested') is True,
              'Expected the completed 64-message Nitro restart report')
    endpoint = evidence.get('validatedEndpoint', {})
    m.require(endpoint.get('GlobalState') == {k: v for k, v in interval.AFTER.items() if k != 'machineStatus'}
              and endpoint.get('WasmRoots') == [interval.ROOT], 'Wrong prior validated endpoint')


def verify_span_evidence(audit_path, replay_dir):
    """Validate the complete saved replay record chain, not just its PASS label."""
    spec = importlib.util.spec_from_file_location('span_replay',
        Path(__file__).with_name('replay-retained-message-span.py'))
    replay = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(replay)
    audit = json.loads(audit_path.read_text())
    replay.check_audit(audit)
    report = json.loads((replay_dir/'summary.json').read_text())
    manifest = json.loads((replay_dir/'manifest.json').read_text())
    expected = dict(version=1, auditSha256=replay.digest(audit), wasmModuleRoot=interval.ROOT,
        firstMessage=replay.START, lastMessage=replay.END, startingState=replay.A, endingState=replay.B)
    m.require(manifest == expected, 'Replay manifest does not match span audit')
    m.require(report.get('status') == 'retained_span_replay_passed'
              and report.get('executionReplayed') is True
              and report.get('validatedMessagesTotal') == 7838
              and report.get('wasmModuleRoot') == interval.ROOT
              and report.get('firstMessage') == replay.START and report.get('lastMessage') == replay.END
              and report.get('lastValidatedState') == replay.B, 'Incomplete or mismatched span replay')
    paths = sorted((replay_dir/'messages').glob('*.json'))
    m.require(len(paths) == 7838, 'Missing replay records')
    md = replay.digest(manifest)
    chain, previous = md, replay.A
    for msg, path in zip(range(replay.START, replay.END+1), paths):
        m.require(path.name == f'{msg}.json', 'Replay record gap')
        record = json.loads(path.read_text())
        previous = replay.check_record(record, msg, previous, chain, md)
        chain = replay.digest(record)
    m.require(previous == replay.B and chain == report['recordChainSha256'], 'Replay record chain differs')
    return dict(auditDirectory=str(audit_path.parent.resolve()), replayDirectory=str(replay_dir.resolve()),
                recordChainSha256=chain, recordsChecked=7838, positionalMessageSpan=7838,
                parentConfirmedAToBProven=False, productionNumBlocksApproved=False)


def recovery_node_hash(before, after, span, has_sibling, last_hash, acc, root):
    m.require(0 < span < 2**64, 'Invalid governance span')
    m.require(before.get('machineStatus', 1) == after.get('machineStatus', 1) == 1,
              'Only FINISHED endpoints supported')
    segment_hashes = [interval.keccak(b'Block state:' + interval.raw(interval.global_hash(s)))
                      for s in (before, after)]
    execution = interval.keccak(bytes(32) + span.to_bytes(32, 'big')
                               + b''.join(interval.raw(x) for x in segment_hashes))
    return interval.keccak(bytes([int(has_sibling)]) + interval.raw(last_hash)
                           + interval.raw(execution) + interval.raw(acc) + interval.raw(root))


class TwoStakerFork(safe.SafeFork):
    def admin(self, label, signature, *args):
        if signature.startswith('forceCreateNode(') and getattr(self, 'governance_span', 1) != 1:
            s = self.candidate_summary
            m.require(args[0] == s['confirmedNode'] and args[1] == s['confirmedInboxMaxCount'],
                      'Unexpected governance parent parameters')
            before, after = s['oldConfirmedState'], s['checkpoint']
            original = '(' + m.state_tuple(before) + ',' + m.state_tuple(after) + ',1)'
            m.require(args[2] == original and args[3] == m.ZERO, 'Unexpected baseline forceCreate arguments')
            prev = self.node(s['confirmedNode'])
            m.require(prev[0].lower() == interval.state_hash(before, s['confirmedInboxMaxCount']).lower(),
                      'Governance parent state commitment differs')
            sibling = int(prev[9], 16)
            last_hash = self.node(sibling)[11] if sibling else prev[11]
            bridge = '0x' + self.call(m.ROLLUP, 'bridge()')[-40:]
            required = int(after['Batch']) + int(int(after['PosInBatch']) > 0)
            m.require(required <= int(self.call(bridge, 'sequencerMessageCount()'), 16), 'Inbox not available')
            acc = self.call(bridge, 'sequencerInboxAccs(uint256)', required-1) if required else m.ZERO
            root = self.call(m.ROLLUP, 'wasmModuleRoot()')
            m.require(root.lower() == interval.ROOT, 'New WASM not installed on fork')
            expected = recovery_node_hash(before, after, self.governance_span, bool(sibling), last_hash, acc, root)
            assertion = '(' + m.state_tuple(before) + ',' + m.state_tuple(after) + ',' + str(self.governance_span) + ')'
            args = (args[0], args[1], assertion, expected)
            label = 'create trusted recovery assertion (positional span ' + str(self.governance_span) + ')'
            self.governance_commitment = dict(numBlocks=self.governance_span, hasSibling=bool(sibling),
                lastHash=last_hash, sequencerBatchAcc=acc, expectedNodeHash=expected,
                beforeState=before, afterState=after, wasmModuleRoot=root,
                interpretation='Positional interval convention under test, not parent A-to-B execution proof',
                readyForProduction=False)
            save(self.output/'governance-commitment.json', self.governance_commitment)
            self.submit_admin(label, signature, *args)
            actual = self.node(self.number('latestNodeCreated()'))[11]
            m.require(actual.lower() == expected.lower(), 'Deployed recovery node hash differs from calculated hash')
            self.exact_governance_hash_checked = True
            print('PASS positional-span recovery with exact expected node hash', flush=True)
            return
        self.submit_admin(label, signature, *args)

    def submit_admin(self, label, signature, *args):
        """Submit prepared arguments; reference rehearsals can capture this boundary."""
        super().admin(label, signature, *args)

    def node(self, number):
        return words(self.call(m.ROLLUP, 'getNode(uint64)', number), 12)

    def clock(self):
        return int(self.rpc('eth_call', [{'data': '0x4360005260206000f3'}, 'latest']), 16)

    def advance(self, goal):
        delta = max(0, goal - self.clock())
        m.require(delta <= 10000, 'Unexpected fork deadline gap')
        if delta:
            self.rpc('anvil_mine', [hex(delta)])
        m.require(self.clock() >= goal, 'EVM clock did not advance')

    def membership(self):
        result = {}
        for address in STAKERS:
            w = [int(v, 16) for v in words(self.call(m.ROLLUP, 'getStaker(address)', address), 5)]
            result[address] = dict(amountStakedWei=str(w[0]), index=w[1], latestStakedNode=w[2],
                currentChallenge=w[3], isStaked=bool(w[4]),
                withdrawableWei=str(self.number('withdrawableFunds(address)', address)),
                isZombie=bool(self.number('isZombie(address)', address)),
                whitelisted=bool(self.number('isValidator(address)', address)))
        return result

    def send_value(self, label, sender, data, value):
        tx = {'from': sender, 'to': m.ROLLUP, 'data': data, 'value': hex(value), 'gas': hex(2000000)}
        self.rpc('eth_call', [tx, 'latest'])
        txhash = self.rpc('eth_sendTransaction', [tx])
        receipt = None
        for _ in range(120):
            receipt = self.rpc('eth_getTransactionReceipt', [txhash])
            if receipt is not None:
                break
            time.sleep(.25)
        m.require(receipt and int(receipt['status'], 16) == 1, label + ' failed')
        self.steps.append(dict(step=label, forkTransaction=txhash, receipt=receipt))
        save(self.output/'steps.json', self.steps)
        save(self.output/(label + '.json'), dict(transaction=tx, receipt=receipt))
        print('PASS', label, txhash, flush=True)
        return txhash


def check_joined(members, stake_amounts):
    for address in STAKERS:
        item = members[address]
        m.require(item['isStaked'] and item['latestStakedNode'] == 42887
                  and int(item['amountStakedWei']) == stake_amounts[address]
                  and item['currentChallenge'] == 0 and not item['isZombie']
                  and item['whitelisted'] and item['withdrawableWei'] == '0',
                  'Unexpected rejoined stake state: ' + address)


def run_test(f, summary, authority, snapshot, output):
    m.require(f.number('stakerCount()') == 2, 'Expected exactly two historical stakers')
    actual = {'0x' + f.call(m.ROLLUP, 'getStakerAddress(uint64)', i)[-40:] for i in range(2)}
    m.require(actual == set(STAKERS), 'Historical staker list changed')
    before = f.membership()
    m.require(before[DORMANT]['latestStakedNode'] == 158
              and before[ACTIVE]['latestStakedNode'] == 42885, 'Unexpected original stake positions')
    for member in before.values():
        m.require(member['isStaked'] and not member['isZombie'] and member['currentChallenge'] == 0
                  and member['whitelisted'] and int(member['amountStakedWei']) > 0,
                  'Original staker is not eligible for this scenario')
    code_present = {a: f.rpc('eth_getCode', [a, 'latest']) != '0x' for a in STAKERS}
    save(output/'participants-before.json', dict(stakers=before, addressHasCode=code_present))

    # This dependency checks the fork anchor/deployed code and authorizes mutations
    # only on our owned Anvil. It verifies each refund credit and actual ETH payment.
    recovery = safe.original_rehearse(f, summary, authority, snapshot, STAKERS)
    span = getattr(f, 'governance_span', 1)
    recovery['governanceNumBlocks'] = span
    recovery['limitations'] = ('Both staker addresses and Safe owners impersonated on owned fork; '
        'Safe uses approveHash/execTransaction, not production ECDSA. Governance numBlocks=' + str(span)
        + ' is a test convention, not an A-to-B execution proof.')
    recovery['safeExecutionPathTested'] = True
    save(output/'recovery-stage.json', recovery)
    m.require(recovery['recoveryNode'] == 42886 and f.number('stakerCount()') == 0,
              'Both old stakes were not removed')
    save(output/'participants-after-refund.json', f.membership())
    m.require(f.number('paused()') == 0, 'Expected resumed fork')
    bridge = '0x' + f.call(m.ROLLUP, 'bridge()')[-40:]
    inbox = int(f.call(bridge, 'sequencerMessageCount()'), 16)
    m.require(inbox == 320189, 'Historical inbox changed')
    previous = f.node(42886)
    m.require(previous[0].lower() == interval.state_hash(interval.BEFORE, inbox).lower()
              and int(previous[9], 16) == 0, 'Wrong recovery state or existing child')
    acc = f.call(bridge, 'sequencerInboxAccs(uint256)', inbox - 1)
    expected = interval.expected_hash(previous[11], acc, 64)
    assertion = '(' + m.state_tuple(interval.BEFORE) + ',' + m.state_tuple(interval.AFTER) + ',64)'
    f.advance(int(previous[10], 16) + f.number('minimumAssertionPeriod()') + 1)
    stakes, transactions = {}, {}
    stakes[ACTIVE] = f.number('currentRequiredStake()')
    m.require(0 < stakes[ACTIVE] <= 10**18, 'Unexpected required stake')
    transactions['creation'] = f.send_value('active-restake-create', ACTIVE,
        m.encode('newStakeOnNewNode(' + m.ASSERTION + ',bytes32,uint256)', assertion, expected, inbox),
        stakes[ACTIVE])
    m.require(f.number('latestNodeCreated()') == 42887, 'Unexpected created node')
    node = f.node(42887)
    m.require(int(node[3], 16) == 42886 and node[11].lower() == expected.lower()
              and node[0].lower() == interval.state_hash(interval.AFTER, inbox).lower(),
              'Ordinary assertion commitment mismatch')
    stakes[DORMANT] = f.number('currentRequiredStake()')
    m.require(0 < stakes[DORMANT] <= 10**18, 'Unexpected second required stake')
    transactions['secondStakerJoin'] = f.send_value('dormant-restake-existing', DORMANT,
        m.encode('newStakeOnExistingNode(uint64,bytes32)', 42887, expected), stakes[DORMANT])
    joined = f.membership()
    save(output/'participants-after-rejoin.json', joined)
    check_joined(joined, stakes)
    m.require(f.number('stakerCount()') == 2 and int(f.node(42887)[6], 16) == 2
              and int(f.node(42886)[7], 16) == 2, 'Wrong live/child staker counts')
    print('PASS both original addresses staked on the same ordinary node', flush=True)
    f.advance(max(int(node[4], 16), int(f.node(42886)[5], 16)) + 1)
    transactions['confirmation'] = f.send_value('dormant-normal-confirm', DORMANT,
        m.encode('confirmNextNode(bytes32,bytes32)', interval.AFTER['BlockHash'], interval.SENDROOT), 0)
    m.require(f.number('latestConfirmed()') == 42887 and f.number('latestNodeCreated()') == 42887
              and f.number('firstUnresolvedNode()') == 42888, 'Ordinary confirmation pointers differ')
    m.require(f.number('stakerCount()') == 2 and f.number('paused()') == 0
              and f.call(m.ROLLUP, 'wasmModuleRoot()').lower() == interval.ROOT,
              'Final staker count/pause/WASM state differs')
    check_joined(f.membership(), stakes)
    outbox = '0x' + f.call(m.ROLLUP, 'outbox()')[-40:]
    m.require(outbox == '0x47da6c41d03ac0608924e86f61577df558114bd8'
              and f.call(outbox, 'roots(bytes32)', interval.SENDROOT).lower() == interval.AFTER['BlockHash'],
              'Confirmed Outbox endpoint mismatch')
    save(output/'participants-final.json', f.membership())
    if span != 1:
        m.require(getattr(f, 'exact_governance_hash_checked', False), 'Exact governance hash check missing')
    return dict(status='two_staker_rejoin_passed' if span == 1 else 'governance_span_rejoin_passed', readyForProduction=False,
        stakers=STAKERS, historicalStakersCovered=2, addressHasCode=code_present,
        bothRefundPaymentsTested=True, bothRestakingTested=True, sameNodeStakeCount=2,
        safeExecutionPathTested=True, safeThresholdNegativeTested=f.threshold_checked,
        recoveryNode=42886, normalConfirmedNode=42887, normalAssertionNumBlocks=64,
        transactions=transactions, originalAccountRuntimeTested=False, productionSigningTested=False,
        governanceNumBlocks=span, productionNumBlocksApproved=False, parentConfirmedAToBProven=False,
        exactGovernanceNodeHashChecked=getattr(f, 'exact_governance_hash_checked', False),
        exactGovernanceNodeHash=getattr(f, 'governance_commitment', {}).get('expectedNodeHash'),
        freshExecutionReplayTested=False, productionRefundScopeChanged=False,
        limitations='Historical fork, both staker addresses and Safe owners impersonated; mock gas funds. '
        'Contract-address impersonation, if applicable, does not test its owner/call path. '
        'Reuses prior validated B-to-C endpoint evidence, not a fresh execution replay. '
        'Governance numBlocks convention remains unapproved for production; no A-to-B proof. '
        'No production signatures, original validator processes, or new full withdrawal audit.')


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('snapshot', type=Path)
    p.add_argument('--previous-pass', required=True, type=Path)
    p.add_argument('--governance-span', type=int, choices=(1, 7838), default=1)
    p.add_argument('--span-audit', type=Path, help='Required for the 7838 positional-span variant')
    p.add_argument('--span-replay', type=Path, help='Completed full retained-span replay directory')
    p.add_argument('--out', required=True, type=Path)
    a = p.parse_args()
    for tool in ('anvil', 'cast'):
        if not shutil.which(tool):
            p.error(tool + ' is required; add ~/.foundry/bin to PATH')
    upstream = os.environ.get('ARCHIVE_RPC', '')
    try:
        parsed = urlsplit(upstream)
        valid_url = parsed.scheme in ('http', 'https') and bool(parsed.hostname)
    except ValueError:
        valid_url = False
    if not valid_url:
        p.error('Set ARCHIVE_RPC to an HTTP(S) archive endpoint in this shell')
    summary = json.loads((a.snapshot/'summary.json').read_text())
    authority = json.loads((a.snapshot/'authority.json').read_text())
    evidence = json.loads(a.previous_pass.read_text())
    check_fixture(summary, authority, evidence)
    span_evidence = None
    if a.governance_span != 1:
        if not a.span_audit or not a.span_replay:
            p.error('7838 requires --span-audit and --span-replay')
        span_evidence = verify_span_evidence(a.span_audit, a.span_replay)
        print('PASS all 7838 saved replay records checked; using positional-span convention for fork only.', flush=True)
    elif a.span_audit or a.span_replay:
        p.error('Span evidence is only accepted with --governance-span 7838')
    a.out.mkdir(parents=True, exist_ok=False, mode=0o700)
    save(a.out/'prior-restart-evidence.json', evidence)
    with socket.socket() as sock:
        sock.bind(('127.0.0.1', 0))
        port = sock.getsockname()[1]
    process = None
    report = dict(status='stopped', readyForProduction=False)
    try:
        with (a.out/'anvil.log').open('w') as log:
            process = subprocess.Popen(['anvil', '--host', '127.0.0.1', '--port', str(port),
                '--chain-id', '31337', '--fork-chain-id', '42161', '--fork-url', upstream,
                '--fork-block-number', str(int(summary['parentBlock'], 16)),
                '--no-storage-caching', '--accounts', '0'], stdout=log, stderr=subprocess.STDOUT)
            f = TwoStakerFork(port, process, a.out)
            f.governance_span = a.governance_span
            f.candidate_summary = summary
            for _ in range(120):
                m.require(process.poll() is None, 'Owned Anvil exited')
                try:
                    f.rpc('web3_clientVersion', [])
                    break
                except (OSError, ValueError):
                    time.sleep(.25)
            else:
                raise TimeoutError('Anvil startup timed out')
            report = run_test(f, summary, authority, a.snapshot, a.out)
    except Exception as exc:
        report.update(status='stopped', errorType=type(exc).__name__,
                      error=str(exc).replace(upstream, '<ARCHIVE_RPC>'))
    except KeyboardInterrupt:
        report.update(status='interrupted')
    finally:
        if process is not None and process.poll() is None:
            process.terminate()
            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait(timeout=10)
        report.update(ownedForkStillRunning=process is not None and process.poll() is None,
            previousPass=str(a.previous_pass.resolve()), candidateDirectory=str(a.snapshot.resolve()),
            dockerAccessed=False, nodeDatabaseAccessed=False, productionRefundScopeChanged=False)
        if span_evidence:
            report['priorLocalSpanReplayEvidence'] = span_evidence
        save(a.out/'summary.json', report)
    print(json.dumps(report, indent=2))
    print('OUTPUT', a.out)
    return 0 if report['status'] in ('two_staker_rejoin_passed', 'governance_span_rejoin_passed') else 1


if __name__ == '__main__':
    raise SystemExit(main())
