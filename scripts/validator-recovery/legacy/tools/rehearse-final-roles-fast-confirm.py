#!/usr/bin/env python3
"""Historical owned Anvil: retire d38e, stake 65fa/5cda/21d4, test 3/3 fast Safe.

No Docker, node database, real signatures, production transactions or fresh replay.
Ordinary and fast confirmations use alternative branches of the same verified node.
"""
import argparse
import importlib.util
import os
from pathlib import Path
import subprocess
import sys

spec = importlib.util.spec_from_file_location('final_roles_base',
    (Path(__file__).resolve().parent / 'rehearse-three-staker-rejoin.py'))
j = importlib.util.module_from_spec(spec)
spec.loader.exec_module(j)
w, m, two = j.w, j.m, j.two
SAFE_HELPER = w.atomic.safe
FAST = '0x5e16561173ea0422549c3de12b68c8a7d3a76672'
RETIRED = '0xd38e969ae2947019e0dec0e46937e6aff651834d'
NEW = '0x21d4ea822a07f737c5e69f7951d517e5f2974849'
LIVE = ['0x65fa6c1d1efc338dfe8f07d5de84b3e7b76cbc95',
        '0x5cda45a9ae0e52f1d5110dc3819f6fb96bade33a', NEW]
REFUNDS = list(w.STAKERS)
ALL = list(dict.fromkeys(REFUNDS + LIVE))
ZERO_ADDR = '0x' + '00' * 20
EXEC_SIG = 'execTransaction(address,uint256,bytes,uint8,uint256,uint256,uint256,address,address,bytes)'


def keccak(data):
    return subprocess.check_output(['cast', 'keccak', data], text=True, timeout=30).strip()


class Fork(j.Fork):
    def impersonate(self, address):
        m.require(address.lower() != FAST, 'Do not impersonate fast-confirm Safe')
        super().impersonate(address)

    def membership(self):
        result = {}
        for address in ALL:
            values = [int(v, 16) for v in two.words(self.call(m.ROLLUP, 'getStaker(address)', address), 5)]
            result[address] = dict(amount=values[0], node=values[2], challenge=values[3], staked=values[4],
                credit=self.number('withdrawableFunds(address)', address),
                zombie=self.number('isZombie(address)', address),
                whitelisted=self.number('isValidator(address)', address))
        return result


def check_members(members, stakes):
    m.require(set(members) == set(ALL), 'Missing participant')
    for address in LIVE:
        m.require(members[address] == dict(amount=stakes[address], node=43009, challenge=0,
            staked=1, credit=0, zombie=0, whitelisted=1), 'Wrong live stake: ' + address)
    retired = members[RETIRED]
    m.require(all(retired[k] == 0 for k in ('amount', 'challenge', 'staked', 'credit', 'zombie')),
              'Retired account still has stake/credit/challenge/zombie')


def fast_identity(f):
    m.require('0x' + f.call(m.ROLLUP, 'anyTrustFastConfirmer()')[-40:].lower() == FAST,
              'Historical fast confirmer differs; do not change fork permissions to force a pass')
    owners = SAFE_HELPER.addresses(f.call(FAST, 'getOwners()'))
    m.require(len(owners) == 3 and set(owners) == set(LIVE)
              and int(f.call(FAST, 'getThreshold()'), 16) == 3, 'Expected actual fast Safe 3/3 owners')
    m.require(keccak(f.rpc('eth_getCode', [FAST, 'latest'])) ==
              '0xb89c1b3bdf2cf8827818646bce9a8f6e372885f8c55e5c07acbd307cb133b000',
              'Fast Safe proxy code differs from observed deployment')
    singleton = '0x' + f.rpc('eth_getStorageAt', [FAST, '0x0', 'latest'])[-40:]
    m.require(singleton == '0x3e5c63644e683549055b9be8653de26e0b4cd36e', 'Fast Safe implementation differs')
    m.require(keccak(f.rpc('eth_getCode', [singleton, 'latest'])) ==
              '0x21842597390c4c6e3c1239e434a682b054bd9548eee5e9b1d6a4482731023c0f',
              'Fast Safe implementation code differs')
    guard = f.rpc('eth_getStorageAt', [FAST,
        '0x4a204f620c8c5ccdca3fd54d003badd85ba500436a431f0cbda4f558c93c34c8', 'latest'])
    m.require(int(guard, 16) == 0, 'Unexpected fast Safe guard')
    modules = f.call(FAST, 'getModulesPaginated(address,uint256)', '0x' + '0'*39 + '1', 50)
    words = two.words(modules, 3)
    m.require(int(words[0], 16) == 64 and int(words[1], 16) == 1 and int(words[2], 16) == 0,
              'Unexpected fast Safe modules')
    return dict(address=FAST, owners=owners, threshold=3,
                nonce=int(f.call(FAST, 'nonce()'), 16), singleton=singleton)


def expect_revert(f, label, tx, reason):
    try:
        f.rpc('eth_call', [tx, 'latest'])
    except ValueError as exc:
        m.require(reason in str(exc), 'Unexpected rejection: ' + label)
    else:
        raise ValueError('Unexpected success: ' + label)
    print('PASS', label, flush=True)


def approval_signatures(owners):
    m.require(len(owners) == 3 and set(owners) == set(LIVE), 'Wrong fast Safe signers')
    return '0x' + ''.join(a[2:].rjust(64, '0') + '00'*32 + '01'
        for a in sorted(owners, key=lambda a: int(a, 16)))


def fast_success(receipt, topic, txhash):
    for log in receipt['logs']:
        if log['address'].lower() != FAST or not log['topics'] or log['topics'][0].lower() != topic.lower():
            continue
        event_hash = log['topics'][1] if len(log['topics']) > 1 else '0x' + log['data'][2:66]
        if event_hash.lower() == txhash.lower():
            return True
    return False


def snapshot_state(f):
    return dict(confirmed=f.number('latestConfirmed()'), created=f.number('latestNodeCreated()'),
                unresolved=f.number('firstUnresolvedNode()'), paused=f.number('paused()'),
                fastNonce=int(f.call(FAST, 'nonce()'), 16), members=f.membership(),
                node=f.node(43009))


def run(f, s, inv, directory, refunds, span, following):
    identity = fast_identity(f)  # Read only; owned-fork authorization happens inside w.run.
    m.require(refunds == REFUNDS and NEW not in refunds and RETIRED not in LIVE, 'Wrong role sets')
    m.require(f.number('isStaked(address)', NEW) == 0 and
              f.number('withdrawableFunds(address)', NEW) == 0 and
              f.number('isValidator(address)', NEW) == 1, 'New staker baseline differs')
    atomic = w.run(f, s, inv, directory, refunds, span)
    two.save(f.output/'atomic-stage.json', atomic)
    m.require(atomic['exactExpectedNodeHash'] == j.follow.EXPECTED, 'Recovery hash changed')
    m.require(all(f.rpc('eth_getCode', [a, 'latest']) == '0x' for a in ALL), 'Expected EOA participants')
    f.admin('resume final-role recovery', 'resume()')
    for address in REFUNDS:
        f.impersonate(address)
        credit = f.number('withdrawableFunds(address)', address)
        m.require(credit == 10**12, 'Unexpected historical refund credit')
        balance = int(f.rpc('eth_getBalance', [address, 'latest']), 16)
        f.send('withdraw-original-'+address, address, m.ROLLUP, m.encode('withdrawStakerFunds()'))
        receipt = f.steps[-1]['receipt']
        fee = int(receipt['gasUsed'], 16) * int(receipt['effectiveGasPrice'], 16)
        m.require(int(f.rpc('eth_getBalance', [address, 'latest']), 16) == balance + credit - fee
                  and f.number('withdrawableFunds(address)', address) == 0, 'Refund payment mismatch')
    f.send('cleanup-original-zombies', LIVE[0], m.ROLLUP, m.encode('removeOldZombies(uint256)', 0))
    m.require(all(f.number('isZombie(address)', a) == 0 for a in ALL), 'Zombie remains')
    f.impersonate(NEW)  # Fork-only gas/stake funding, not a real wallet unlock.
    m.require(f.number('stakeToken()') == 0, 'Only historical ETH stakes supported')
    bridge = '0x' + f.call(m.ROLLUP, 'bridge()')[-40:]
    inbox = int(f.call(bridge, 'sequencerMessageCount()'), 16)
    m.require(inbox == 321868, 'Pinned inbox differs')
    before, after = dict(s['checkpoint'], machineStatus=1), dict(j.END, machineStatus=1)
    prev = f.node(43008)
    m.require(prev[0].lower() == two.interval.state_hash(before, inbox).lower()
              and int(prev[9], 16) == 0 and prev[11].lower() == j.follow.EXPECTED, 'Wrong recovery node')
    acc = f.call(bridge, 'sequencerInboxAccs(uint256)', inbox-1)
    expected = two.recovery_node_hash(before, after, 48, False, prev[11], acc, j.r.ROOT)
    assertion = '('+m.state_tuple(before)+','+m.state_tuple(after)+',48)'
    f.advance(int(prev[10], 16) + f.number('minimumAssertionPeriod()') + 1)
    stakes, txs = {}, {}
    for i, address in enumerate(LIVE):
        stakes[address] = f.number('currentRequiredStake()')
        m.require(0 < stakes[address] <= 10**18, 'Unexpected required stake')
        data = (m.encode('newStakeOnNewNode('+m.ASSERTION+',bytes32,uint256)', assertion, expected, inbox)
                if i == 0 else m.encode('newStakeOnExistingNode(uint64,bytes32)', 43009, expected))
        txs[address] = f.send_value('live-account-stake-'+str(i+1), address, data, stakes[address])
    node = f.node(43009)
    m.require(int(node[3], 16) == 43008 and node[11].lower() == expected.lower()
              and node[0].lower() == two.interval.state_hash(after, inbox).lower(), 'Ordinary commitment differs')
    check_members(f.membership(), stakes)
    m.require(f.number('stakerCount()') == 3 and int(node[6], 16) == 3
              and int(f.node(43008)[7], 16) == 3, 'Wrong stake counts')
    print('PASS 65fa/5cda/21d4 staked together; d38e refunded and retired', flush=True)

    # Prove ordinary confirmation on one branch, then restore the pending node.
    snap = f.rpc('evm_snapshot', [])
    pending = snapshot_state(f)
    deadline = max(int(node[4], 16), int(f.node(43008)[5], 16))
    f.advance(deadline + 1)
    txs['ordinaryConfirmationRevertedBranch'] = f.send_value('normal-confirm-new-account', NEW,
        m.encode('confirmNextNode(bytes32,bytes32)', j.END['BlockHash'], j.END['SendRoot']), 0)
    m.require(f.number('latestConfirmed()') == 43009, 'Ordinary confirmation missing')
    m.require(f.rpc('evm_revert', [snap]) is True, 'Unable to restore pending branch')
    m.require(snapshot_state(f) == pending, 'Pending branch was not restored')
    print('PASS ordinary confirmation; reverted that branch for fast-confirm test', flush=True)

    # Same pending node, governed pause. Owners can approve while Rollup is paused,
    # but the Rollup confirmation must remain blocked, including via Safe.
    f.admin('pause before fast-confirm boundary test', 'pause()')
    fast_data = m.encode('fastConfirmNextNode(bytes32,bytes32,bytes32)',
                         j.END['BlockHash'], j.END['SendRoot'], expected)
    fields = [m.ROLLUP, 0, fast_data, 0, 0, 0, 0, ZERO_ADDR, ZERO_ADDR]
    nonce = int(f.call(FAST, 'nonce()'), 16)
    txhash = f.call(FAST,
        'getTransactionHash(address,uint256,bytes,uint8,uint256,uint256,uint256,address,address,uint256)',
        *fields, nonce)
    owners = sorted(LIVE, key=lambda a: int(a, 16))
    for owner in owners:
        f.send('fast-owner-approve-'+owner, owner, FAST, m.encode('approveHash(bytes32)', txhash))
        m.require(int(f.call(FAST, 'approvedHashes(address,bytes32)', owner, txhash), 16) == 1,
                  'Fast owner approval missing')
    signatures = approval_signatures(owners)
    full = m.encode(EXEC_SIG, *fields, signatures)
    short = m.encode(EXEC_SIG, *fields, '0x'+signatures[2:2+65*2*2])
    expect_revert(f, 'fast Safe rejects two approvals for 3/3',
                  {'from': owners[0], 'to': FAST, 'data': short, 'gas': hex(15000000)}, 'GS020')
    expect_revert(f, 'paused Rollup rejects fast-confirm call',
                  {'from': FAST, 'to': m.ROLLUP, 'data': fast_data}, 'Pausable: paused')
    expect_revert(f, 'paused fast confirmation rejected through full Safe call',
                  {'from': owners[0], 'to': FAST, 'data': full, 'gas': hex(15000000)}, 'GS013')
    m.require(int(f.call(FAST, 'nonce()'), 16) == nonce and f.number('latestConfirmed()') == 43008,
              'Unexpected fast nonce/confirmation change')
    f.admin('resume before fast confirmation', 'resume()')
    m.require(f.clock() < deadline, 'Deadline already reached; cannot prove early confirmation')
    ordinary_reason = 'BEFORE_DEADLINE' if f.clock() < int(node[4], 16) else 'CHILD_TOO_RECENT'
    expect_revert(f, 'ordinary confirmation still awaits deadline',
                  {'from': NEW, 'to': m.ROLLUP, 'data': m.encode('confirmNextNode(bytes32,bytes32)',
                      j.END['BlockHash'], j.END['SendRoot'])}, ordinary_reason)
    f.send('fast Safe confirms through 3/3 owner approvals', owners[0], FAST, full)
    receipt = f.steps[-1]['receipt']
    topic = keccak('0x' + b'ExecutionSuccess(bytes32,uint256)'.hex())
    # success_event in the old helper is bound to the governance Safe; compare fast Safe explicitly.
    m.require(fast_success(receipt, topic, txhash), 'Fast Safe ExecutionSuccess missing')
    # EVM BLOCK uses the fork's underlying clock, which can differ from RPC block number.
    m.require(f.clock() < deadline, 'Fast confirmation did not precede ordinary deadline')
    m.require(int(f.call(FAST, 'nonce()'), 16) == nonce+1 and f.number('latestConfirmed()') == 43009
              and f.number('firstUnresolvedNode()') == 43010 and f.number('paused()') == 0,
              'Wrong state after fast confirmation')
    check_members(f.membership(), stakes)
    m.require(f.call(m.ROLLUP, 'wasmModuleRoot()').lower() == j.r.ROOT, 'Final WASM root differs')
    box = '0x'+f.call(m.ROLLUP, 'outbox()')[-40:]
    m.require(box == '0x47da6c41d03ac0608924e86f61577df558114bd8'
              and f.call(box, 'roots(bytes32)', j.END['SendRoot']).lower() == j.END['BlockHash'],
              'Wrong final Outbox endpoint')
    two.save(f.output/'participants-final.json', f.membership())
    txs['fastConfirmation'] = f.steps[-1]['forkTransaction']
    return dict(status='contract_rehearsal_passed', test='final_roles_and_fast_confirmation',
        readyForProduction=False, refundAccounts=REFUNDS, postRecoveryStakers=LIVE, retiredAccount=RETIRED,
        recoveryNode=43008, ordinaryNode=43009, governanceNumBlocks=10578, normalAssertionNumBlocks=48,
        allOriginalRefundPaymentsTested=True, retiredAccountNotRestaked=True,
        newCloudBStakeTested=True, sameNodeStakeCount=3, ordinaryConfirmationTested=True,
        ordinaryConfirmationBranchReverted=True, fastConfirmationTested=True,
        fastSafeIdentity=identity, fastSafeTransactionHash=txhash, fastSafeNonceBefore=nonce,
        fastSafeNonceAfter=nonce+1, fastSafeThresholdNegativeTested=True,
        fastConfirmationPausedCallRejected=True, fastSafePausedCallRejected=True,
        approvalsSurvivePauseResumeTested=True, fastConfirmedBeforeOrdinaryDeadline=True,
        ordinaryConfirmationBeforeDeadlineRejected=True,
        finalForkEvmClock=f.clock(), ordinaryDeadline=deadline, transactions=txs,
        spanEvidence=span, followupEvidence=following, productionSignaturesTested=False,
        originalAccountRuntimeTested=False, automaticFastConfirmationTested=False,
        freshExecutionReplayTested=False, parentConfirmedAToBProven=False,
        productionNumBlocksApproved=False, fullWithdrawalHistoryAudited=False,
        dockerAccessed=False, nodeDatabaseAccessed=False,
        limitations='Owned historical fork only; original EOAs and governance/fast Safe owners impersonated. '
        'Fast Safe itself is never impersonated for a transaction. Approved-hash path, not production ECDSA. '
        'Ordinary confirmation is rolled back before the fast-confirm branch. Paused rejection via eth_call. '
        'Saved 10578+48 replay records reused; no fresh replay or Nitro automatic fast-confirm runtime. '
        'Governance span is positional, not an A-to-B proof. No production action.')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('snapshot', type=Path)
    for name in ('atomic-pass', 'followup', 'span-audit', 'span-replay', 'out'):
        parser.add_argument('--'+name, type=Path, required=True)
    args = parser.parse_args()
    upstream = os.environ.get('ARCHIVE_RPC', '')
    if not upstream.startswith(('http://', 'https://')):
        parser.error('Set ARCHIVE_RPC to an Arbitrum One archive URL')
    out = args.out.resolve()
    for root in (Path('/data'), Path('/data_mock'), Path('/data_new/validator')):
        m.require(out != root and root not in out.parents, 'Protected output path')
    s, inv, prior = w.read(args.snapshot/'summary.json'), w.read(args.snapshot/'authority.json'), w.read(args.atomic_pass)
    w.check_fixture(s, inv)
    j.follow.check_pass(prior)
    print('Checking saved 10578 + 48 records; no new replay, Docker or database copy.', flush=True)
    span = w.verify(args.span_audit, args.span_replay)
    following = j.verify_followup(args.followup, s, prior)
    old = sys.argv, m.LocalFork, m.rehearse
    try:
        sys.argv = [sys.argv[0], str(args.snapshot), '--parent-rpc', upstream, '--out', str(args.out)]
        for address in REFUNDS:
            sys.argv += ['--refund-staker', address]
        m.LocalFork = Fork
        m.rehearse = lambda f, s, inv, d, refs: run(f, s, inv, d, refs, span, following)
        return m.main()
    finally:
        sys.argv, m.LocalFork, m.rehearse = old


if __name__ == '__main__':
    raise SystemExit(main())
