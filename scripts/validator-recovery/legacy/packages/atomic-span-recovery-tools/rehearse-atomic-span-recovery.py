#!/usr/bin/env python3
"""Fixed historical Anvil: atomic recovery with the audited 7838 position span.

Reads saved replay records, not node databases. Never starts/stops Docker.
No production signing, RPC writes, or A-to-B execution proof.
"""
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


atomic = load('atomic', 'rehearse-atomic-recovery.py')
two = load('two', 'rehearse-two-staker-rejoin.py')
m = atomic.m
EXPECTED = '0x1c44f95dd83a8e24c4794528d57b32b053a516204151418fce05e8a75e670b7b'
RECORD_CHAIN = 'dfdc67af2b21cb262d39a297e7517c482cba94219c5aa19bbe6ea1fd8f482435'


def check_evidence(summary, authority, prior, evidence):
    m.require(summary.get('parentBlock') == '0x1e58e750'
              and authority.get('parentBlockHash', '').lower() == two.ANCHOR,
              'Wrong historical anchor')
    m.require(summary.get('confirmedNode') == 42883
              and summary.get('confirmedInboxMaxCount') == 320161
              and summary.get('bridgeInboxCount') == 320189
              and summary.get('checkpointMessage') == 126237291
              and summary.get('newWasmRoot') == two.interval.ROOT
              and summary.get('checkpoint') == {
                  k: v for k, v in two.interval.BEFORE.items() if k != 'machineStatus'},
              'Wrong historical fixture')
    m.require(prior.get('status') == 'governance_span_rejoin_passed'
              and prior.get('governanceNumBlocks') == 7838
              and prior.get('exactGovernanceNodeHashChecked') is True
              and prior.get('exactGovernanceNodeHash', '').lower() == EXPECTED
              and prior.get('recoveryNode') == 42886
              and prior.get('normalConfirmedNode') == 42887
              and prior.get('bothRestakingTested') is True
              and prior.get('normalAssertionNumBlocks') == 64,
              'Expected completed 7838 governance/rejoin report')
    # The historical hash is also recomputed from live fork inputs below.
    m.require(evidence.get('recordsChecked') == 7838
              and evidence.get('recordChainSha256') == RECORD_CHAIN,
              'Wrong replay record chain')


class SpanReferenceFork(two.TwoStakerFork):
    """Reference only: capture transformed calldata, then roll back the EVM."""
    candidate_summary = None
    governance_span = 7838

    def impersonate(self, address):
        # The reference uses the existing direct Safe impersonation flow.
        # The real batch test below instead uses three owners' approveHash.
        two.m.LocalFork.impersonate(self, address)

    def submit_admin(self, label, signature, *args):
        if signature.startswith('forceCreateNode('):
            m.require(args[2].endswith(',7838)') and args[3].lower() == EXPECTED,
                      'Wrong span or calculated expected hash')
        self.captured.append((label, signature, args))
        two.m.LocalFork.admin(self, label, signature, *args)


def run(f, summary, authority, directory, refunds, evidence, prior_path):
    m.require(refunds == [two.ACTIVE], 'This atomic scenario refunds only the active staker')
    SpanReferenceFork.candidate_summary = summary
    previous = atomic.ReferenceFork
    atomic.ReferenceFork = SpanReferenceFork
    try:
        report = atomic.rehearse(f, summary, authority, directory, refunds)
    finally:
        atomic.ReferenceFork = previous
    commitment = json.loads((f.output/'reference'/'governance-commitment.json').read_text())
    m.require(commitment['numBlocks'] == 7838
              and commitment['expectedNodeHash'].lower() == EXPECTED
              and report['exactExpectedNodeHash'].lower() == EXPECTED,
              'Atomic result does not match 7838 reference')
    report.update(test='atomic_safe_recovery_span_7838', governanceNumBlocks=7838,
        exactGovernanceNodeHashChecked=True, spanEvidence=evidence,
        previousGovernancePass=str(prior_path.resolve()),
        parentConfirmedAToBProven=False, productionNumBlocksApproved=False,
        freshExecutionReplayTested=False, productionRefundScopeChanged=False,
        dockerAccessed=False, nodeDatabaseAccessed=False,
        limitations='Historical fork only. Governance 7838 is a positional-span convention, '
            'not a parent-confirmed A-to-B execution proof. Saved replay records are checked; '
            'no fresh WASM replay. Reference uses direct Safe impersonation and is reverted. '
            'Atomic batches use impersonated owners approveHash and Safe delegatecall. '
            'Negative batch records ExecutionFailure and consumes the Safe nonce while inner '
            'changes roll back. Success remains paused; no runtime restart, production '
            'signatures, production calldata approval or full withdrawal history audit.')
    return report


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('snapshot', type=Path)
    p.add_argument('--span-pass', type=Path, required=True)
    p.add_argument('--span-audit', type=Path, required=True)
    p.add_argument('--span-replay', type=Path, required=True)
    p.add_argument('--out', type=Path, required=True)
    a = p.parse_args()
    upstream = os.environ.get('ARCHIVE_RPC', '')
    if not upstream.startswith(('http://', 'https://')):
        p.error('Set ARCHIVE_RPC to an Arbitrum One archive HTTP(S) URL')
    summary = json.loads((a.snapshot/'summary.json').read_text())
    authority = json.loads((a.snapshot/'authority.json').read_text())
    prior = json.loads(a.span_pass.read_text())
    print('Checking all 7838 saved replay records (no execution replay).', flush=True)
    evidence = two.verify_span_evidence(a.span_audit, a.span_replay)
    check_evidence(summary, authority, prior, evidence)
    print('PASS saved span evidence; testing atomic recovery and late-failure rollback.', flush=True)
    # Reuse the owned-process runner and all its deployment/anchor guards.
    # No keep-alive: this test closes its private Anvil on completion/failure.
    old_argv, old_fork, old_rehearse = sys.argv, m.LocalFork, m.rehearse
    try:
        sys.argv = [sys.argv[0], str(a.snapshot), '--parent-rpc', upstream,
                    '--refund-staker', two.ACTIVE, '--out', str(a.out)]
        m.LocalFork = atomic.safe.SafeFork
        m.rehearse = lambda f, s, auth, d, refs: run(f, s, auth, d, refs, evidence, a.span_pass)
        return m.main()
    finally:
        sys.argv, m.LocalFork, m.rehearse = old_argv, old_fork, old_rehearse


if __name__ == '__main__':
    raise SystemExit(main())
