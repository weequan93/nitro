#!/usr/bin/env python3
"""Pinned three-staker Anvil rehearsal; no Docker or production transactions."""
import argparse
import importlib.util
import json
import os
import sys
from pathlib import Path


def load(name, filename):
    spec = importlib.util.spec_from_file_location(name, Path(__file__).with_name(filename))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


atomic = load('atomic_three', 'rehearse-atomic-recovery.py')
two = load('two_three', 'rehearse-two-staker-rejoin.py')
r = load('span_three', 'replay-three-staker-span.py').r
m = atomic.m
STAKERS = [two.ACTIVE, two.DORMANT, '0x65fa6c1d1efc338dfe8f07d5de84b3e7b76cbc95']
CHAIN = 'c6c912454a2bc8ce7b33b895b716d8622c42d29058061026abc547a091173e75'
CANDIDATE_SHA = 'ea3c79ca64e06307d155f6da8338216ecb41793f286c573c700cc2ffdaf9a6a7'
ANCHOR = '0xc06fedabd183b222c0be7680a66359e259fa3587417d5664f6623e970c141a8d'


def read(path):
    return json.loads(path.read_text())


def verify(audit_path, directory):
    audit = read(audit_path)
    r.check_audit(audit)
    report = read(directory/'summary.json')
    expected = dict(version=1, auditSha256=r.digest(audit), wasmModuleRoot=r.ROOT,
                    firstMessage=r.START, lastMessage=r.END, startingState=r.A, endingState=r.B)
    m.require(read(directory/'manifest.json') == expected, 'Wrong replay manifest')
    m.require(report.get('status') == 'retained_span_replay_passed'
              and report.get('executionReplayed') is True
              and report.get('validatedMessagesTotal') == 10578
              and report.get('wasmModuleRoot') == r.ROOT
              and report.get('firstMessage') == r.START and report.get('lastMessage') == r.END
              and report.get('lastValidatedState') == r.B
              and report.get('recordChainSha256') == CHAIN, 'Wrong completed replay report')
    paths = sorted((directory/'messages').glob('*.json'))
    m.require(len(paths) == 10578, 'Missing replay records')
    md = r.digest(expected)
    chain, previous = md, r.A
    for msg, path in zip(range(r.START, r.END+1), paths):
        m.require(path.name == f'{msg}.json', 'Replay record gap')
        record = read(path)
        previous = r.check_record(record, msg, previous, chain, md)
        chain = r.digest(record)
    m.require(chain == CHAIN and previous == r.B, 'Replay chain or endpoint changed')
    return dict(recordsChecked=10578, recordChainSha256=chain,
                replayDirectory=str(directory.resolve()), auditFile=str(audit_path.resolve()))


def check_fixture(summary, authority):
    m.require(r.digest(summary) == CANDIDATE_SHA, 'Wrong pinned candidate')
    m.require(authority.get('parentBlock') == summary['parentBlock']
              and authority.get('parentBlockHash', '').lower() == ANCHOR, 'Wrong parent anchor')


class Reference(two.TwoStakerFork):
    governance_span = 10578

    def impersonate(self, address):
        two.m.LocalFork.impersonate(self, address)

    def submit_admin(self, label, signature, *args):
        if signature.startswith('forceCreateNode('):
            m.require(args[2].endswith(',10578)') and args[3] != m.ZERO,
                      'Expected span 10578 and exact node hash')
        self.captured.append((label, signature, args))
        two.m.LocalFork.admin(self, label, signature, *args)


def run(f, summary, authority, directory, refunds, evidence):
    check_fixture(summary, authority)
    m.require(refunds == STAKERS, 'Wrong three-account refund scope')
    m.require(f.number('stakerCount()') == 3, 'Expected three stakers')
    actual = {'0x'+f.call(m.ROLLUP, 'getStakerAddress(uint64)', i)[-40:].lower() for i in range(3)}
    m.require(actual == set(STAKERS), 'Unexpected live fork participants')
    for address in STAKERS:
        words = [int(v, 16) for v in two.words(f.call(m.ROLLUP, 'getStaker(address)', address), 5)]
        m.require(words[0] == 1000000000000 and words[2] == (158 if address == two.DORMANT else 43007)
                  and words[3] == 0 and words[4] == 1, 'Unexpected stake state')
        m.require(f.number('isZombie(address)', address) == 0
                  and f.number('isValidator(address)', address) == 1, 'Unexpected eligibility')
    Reference.candidate_summary = summary
    previous = atomic.ReferenceFork
    atomic.ReferenceFork = Reference
    try:
        report = atomic.rehearse(f, summary, authority, directory, refunds)
    finally:
        atomic.ReferenceFork = previous
    commitment = read(f.output/'reference/governance-commitment.json')
    m.require(commitment['numBlocks'] == 10578
              and commitment['expectedNodeHash'] == report['exactExpectedNodeHash']
              and report['recoveryNode'] == 43008 and report['paused'] is True,
              'Unexpected atomic recovery result')
    m.require(f.number('stakerCount()') == 0, 'Active stakes remain after refund')
    report.update(test='three_staker_atomic_span_10578', stakers=STAKERS,
        governanceNumBlocks=10578, exactGovernanceNodeHashChecked=True, spanEvidence=evidence,
        allThreeRefundCreditsTested=True, referenceRefundPaymentsTested=True,
        restakingTested=False, parentConfirmedAToBProven=False, productionNumBlocksApproved=False,
        freshExecutionReplayTested=False, productionRefundScopeChanged=False,
        dockerAccessed=False, nodeDatabaseAccessed=False,
        limitations='Historical fork only. Three accounts refunded in simulation. Reference refund '
        'payments are tested then reverted. Atomic success remains paused with refund credits. '
        'Safe owners are impersonated; no production signatures. 10578 is a positional span, '
        'not an A-to-B proof. Saved replay records checked; no fresh execution replay, restaking, '
        'Nitro restart or full withdrawal audit in this run.')
    return report


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('snapshot', type=Path)
    p.add_argument('--span-audit', type=Path, required=True)
    p.add_argument('--span-replay', type=Path, required=True)
    p.add_argument('--out', type=Path, required=True)
    a = p.parse_args()
    upstream = os.environ.get('ARCHIVE_RPC', '')
    if not upstream.startswith(('http://', 'https://')):
        p.error('Set ARCHIVE_RPC to an Arbitrum One archive HTTP(S) URL')
    check_fixture(read(a.snapshot/'summary.json'), read(a.snapshot/'authority.json'))
    print('Checking all 10578 saved replay records; no new execution replay.', flush=True)
    evidence = verify(a.span_audit, a.span_replay)
    print('PASS saved replay evidence; starting private three-account atomic simulation.', flush=True)
    old = sys.argv, m.LocalFork, m.rehearse
    try:
        sys.argv = [sys.argv[0], str(a.snapshot), '--parent-rpc', upstream, '--out', str(a.out)]
        for address in STAKERS:
            sys.argv += ['--refund-staker', address]
        m.LocalFork = atomic.safe.SafeFork
        m.rehearse = lambda f, s, auth, d, refs: run(f, s, auth, d, refs, evidence)
        return m.main()
    finally:
        sys.argv, m.LocalFork, m.rehearse = old


if __name__ == '__main__':
    raise SystemExit(main())
