#!/usr/bin/env python3
"""Owned historical Anvil: atomic recovery, resume, three refunds/rejoins, normal confirmation."""
import argparse
import importlib.util
import json
import os
import sys
from pathlib import Path

spec = importlib.util.spec_from_file_location('followup_rejoin', (Path(__file__).resolve().parent / 'replay-three-staker-followup.py'))
follow = importlib.util.module_from_spec(spec)
spec.loader.exec_module(follow)
w = follow.w
m, two, r = w.m, w.two, w.r
STAKERS = w.STAKERS
END_CHAIN = '10dc47fb6448c3aab4ef1b45460cd945a0993e093103225e444f4639ff1446d9'
END = dict(BlockHash='0xae4dc4e97d4f1eb5c4c01faa09cc3cc66a733471c42debfe245afcd5112a17cb',
           SendRoot='0xda325c2af6759775b183fab5c9dcf966968e0f46b2c15f283610511dfe103e3f',
           Batch=321868, PosInBatch=0)


def verify_followup(directory, summary, prior):
    result, manifest = w.read(directory/'summary.json'), w.read(directory/'manifest.json')
    expected = dict(version=1, parentBlock=summary['parentBlock'], parentBlockHash=w.ANCHOR,
        candidateSha256=r.digest(summary), atomicPassSha256=r.digest(prior),
        startingMessage=126693840, startingState=summary['checkpoint'],
        endingPosition=[321868, 0], wasmModuleRoot=r.ROOT)
    m.require(manifest == expected, 'Wrong B-to-C manifest')
    m.require(result.get('status') == 'followup_span_replay_passed'
        and result.get('executionReplayed') is True and result.get('numBlocks') == 48
        and result.get('validatedMessagesTotal') == 48
        and result.get('startMessage') == 126693840 and result.get('endMessage') == 126693888
        and result.get('startState') == summary['checkpoint'] and result.get('endState') == END
        and result.get('wasmModuleRoot') == r.ROOT
        and result.get('recordChainSha256') == END_CHAIN, 'Wrong B-to-C report')
    paths = sorted((directory/'messages').glob('*.json'))
    m.require(len(paths) == 48, 'Expected 48 replay records')
    old = r.START, r.END, r.A, r.B
    try:
        r.START, r.END, r.A, r.B = 126693841, 126693888, summary['checkpoint'], END
        md = r.digest(manifest)
        chain, previous = md, r.A
        for msg, path in zip(range(r.START, r.END+1), paths):
            m.require(path.name == f'{msg}.json', 'B-to-C record gap')
            record = w.read(path)
            previous = r.check_record(record, msg, previous, chain, md)
            chain = r.digest(record)
        m.require(previous == END and chain == END_CHAIN, 'B-to-C record chain changed')
    finally:
        r.START, r.END, r.A, r.B = old
    return dict(recordsChecked=48, recordChainSha256=chain, replayDirectory=str(directory.resolve()))


class Fork(two.TwoStakerFork):
    def membership(self):
        result = {}
        for address in STAKERS:
            a = [int(x, 16) for x in two.words(self.call(m.ROLLUP, 'getStaker(address)', address), 5)]
            result[address] = dict(amount=a[0], node=a[2], challenge=a[3], staked=a[4],
                credit=self.number('withdrawableFunds(address)', address),
                zombie=self.number('isZombie(address)', address),
                whitelisted=self.number('isValidator(address)', address))
        return result


def check_members(members, stakes):
    m.require(set(members) == set(STAKERS), 'Missing account')
    for address in STAKERS:
        m.require(members[address] == dict(amount=stakes[address], node=43009, challenge=0,
            staked=1, credit=0, zombie=0, whitelisted=1), 'Rejoined account state differs')


def run(f, s, inv, directory, refunds, span, following):
    atomic = w.run(f, s, inv, directory, refunds, span)
    two.save(f.output/'atomic-stage.json', atomic)
    m.require(atomic['exactExpectedNodeHash'] == follow.EXPECTED, 'Recovery hash changed')
    m.require(all(f.rpc('eth_getCode', [x, 'latest']) == '0x' for x in STAKERS), 'Expected original EOAs')
    f.admin('resume after atomic recovery', 'resume()')
    for address in STAKERS:
        f.impersonate(address)
        credit = f.number('withdrawableFunds(address)', address)
        m.require(credit == 10**12, 'Unexpected refund credit')
        balance = int(f.rpc('eth_getBalance', [address, 'latest']), 16)
        f.send('withdraw after atomic '+address, address, m.ROLLUP, m.encode('withdrawStakerFunds()'))
        receipt = f.steps[-1]['receipt']
        fee = int(receipt['gasUsed'], 16)*int(receipt['effectiveGasPrice'], 16)
        m.require(int(f.rpc('eth_getBalance', [address, 'latest']), 16) == balance+credit-fee
            and f.number('withdrawableFunds(address)', address) == 0, 'Refund payment mismatch')
    f.send('cleanup after atomic', STAKERS[0], m.ROLLUP, m.encode('removeOldZombies(uint256)', 0))
    m.require(all(f.number('isZombie(address)', x) == 0 for x in STAKERS), 'Zombie remains')
    bridge = '0x'+f.call(m.ROLLUP, 'bridge()')[-40:]
    inbox = int(f.call(bridge, 'sequencerMessageCount()'), 16)
    m.require(inbox == 321868, 'Fixed inbox changed')
    before, after = dict(s['checkpoint'], machineStatus=1), dict(END, machineStatus=1)
    prev = f.node(43008)
    m.require(prev[0].lower() == two.interval.state_hash(before, inbox).lower()
        and int(prev[9], 16) == 0 and prev[11].lower() == follow.EXPECTED, 'Wrong recovery node')
    acc = f.call(bridge, 'sequencerInboxAccs(uint256)', inbox-1)
    expected = two.recovery_node_hash(before, after, 48, False, prev[11], acc, r.ROOT)
    assertion = '('+m.state_tuple(before)+','+m.state_tuple(after)+',48)'
    f.advance(int(prev[10], 16)+f.number('minimumAssertionPeriod()')+1)
    stakes, txs = {}, {}
    for i, address in enumerate(STAKERS):
        stakes[address] = f.number('currentRequiredStake()')
        m.require(0 < stakes[address] <= 10**18, 'Unexpected required stake')
        data = (m.encode('newStakeOnNewNode('+m.ASSERTION+',bytes32,uint256)', assertion, expected, inbox)
            if i == 0 else m.encode('newStakeOnExistingNode(uint64,bytes32)', 43009, expected))
        txs[address] = f.send_value('original-rejoin-'+str(i+1), address, data, stakes[address])
        m.require(f.number('latestNodeCreated()') == 43009, 'Unexpected node number')
    node = f.node(43009)
    m.require(int(node[3], 16) == 43008 and node[11].lower() == expected.lower()
        and node[0].lower() == two.interval.state_hash(after, inbox).lower(), 'Ordinary commitment mismatch')
    check_members(f.membership(), stakes)
    m.require(f.number('stakerCount()') == 3 and int(node[6], 16) == 3
        and int(f.node(43008)[7], 16) == 3, 'Wrong stake counts')
    print('PASS all three original accounts staked on node 43009', flush=True)
    f.advance(max(int(node[4], 16), int(f.node(43008)[5], 16))+1)
    txs['confirmation'] = f.send_value('third-account-normal-confirm', STAKERS[2],
        m.encode('confirmNextNode(bytes32,bytes32)', END['BlockHash'], END['SendRoot']), 0)
    m.require(f.number('latestConfirmed()') == f.number('latestNodeCreated()') == 43009
        and f.number('firstUnresolvedNode()') == 43010 and f.number('paused()') == 0
        and f.number('stakerCount()') == 3 and f.call(m.ROLLUP, 'wasmModuleRoot()').lower() == r.ROOT,
        'Wrong final Rollup state')
    check_members(f.membership(), stakes)
    box = '0x'+f.call(m.ROLLUP, 'outbox()')[-40:]
    m.require(box == '0x47da6c41d03ac0608924e86f61577df558114bd8'
        and f.call(box, 'roots(bytes32)', END['SendRoot']).lower() == END['BlockHash'], 'Wrong Outbox endpoint')
    two.save(f.output/'participants-final.json', f.membership())
    return dict(status='contract_rehearsal_passed', test='three_staker_atomic_rejoin_48',
        readyForProduction=False, stakers=STAKERS, recoveryNode=43008, normalConfirmedNode=43009,
        governanceNumBlocks=10578, normalAssertionNumBlocks=48, sameNodeStakeCount=3,
        allThreeRefundPaymentsTested=True, allThreeRestakingTested=True,
        atomicRecoveryThenRejoinTested=True, normalConfirmationTested=True,
        exactGovernanceNodeHash=follow.EXPECTED, normalNodeHash=expected, transactions=txs,
        spanEvidence=span, followupEvidence=following, parentConfirmedAToBProven=False,
        productionNumBlocksApproved=False, productionSigningTested=False,
        originalAccountRuntimeTested=False, validatorRestartTested=False,
        dockerAccessed=False, nodeDatabaseAccessed=False, productionRefundScopeChanged=False,
        limitations='Historical owned Anvil only. Safe owners and original EOAs impersonated; mock gas. '
        'Successful atomic recovery is resumed on the same fork, all three refunds paid and accounts '
        'rejoined to the ordinary 48-message node. Saved replay evidence checked, no fresh replay here. '
        'No Nitro runtime, production signatures, A-to-B proof, or full withdrawal audit.')


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('snapshot', type=Path)
    for name in ('atomic-pass', 'followup', 'span-audit', 'span-replay', 'out'):
        p.add_argument('--'+name, type=Path, required=True)
    a = p.parse_args()
    upstream = os.environ.get('ARCHIVE_RPC', '')
    if not upstream.startswith(('http://', 'https://')):
        p.error('Set ARCHIVE_RPC to an Arbitrum One archive URL')
    s, inv, prior = w.read(a.snapshot/'summary.json'), w.read(a.snapshot/'authority.json'), w.read(a.atomic_pass)
    w.check_fixture(s, inv)
    follow.check_pass(prior)
    span = w.verify(a.span_audit, a.span_replay)
    following = verify_followup(a.followup, s, prior)
    print('PASS saved 10578 + 48 records; starting atomic recovery and three-account rejoin.', flush=True)
    old = sys.argv, m.LocalFork, m.rehearse
    try:
        sys.argv = [sys.argv[0], str(a.snapshot), '--parent-rpc', upstream, '--out', str(a.out)]
        for address in STAKERS:
            sys.argv += ['--refund-staker', address]
        m.LocalFork = Fork
        m.rehearse = lambda f, s, inv, d, refs: run(f, s, inv, d, refs, span, following)
        return m.main()
    finally:
        sys.argv, m.LocalFork, m.rehearse = old


if __name__ == '__main__':
    raise SystemExit(main())
