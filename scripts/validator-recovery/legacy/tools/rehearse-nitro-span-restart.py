#!/usr/bin/env python3
"""7838 governance + Nitro signing and graceful restart, historical fork only."""
import argparse
import json
import sys
from pathlib import Path
import importlib.util


def load(name, file):
    spec = importlib.util.spec_from_file_location(name, Path(__file__).with_name(file))
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


rt = load('runtime', 'rehearse-nitro-autosubmit.py')
span = load('span', 'rehearse-atomic-span-recovery.py')
two = span.two
TEST = 'recovery-nitro-span-restart'


def check_atomic(report):
    rt.m.require(report.get('status') == 'contract_rehearsal_passed'
        and report.get('test') == 'atomic_safe_recovery_span_7838'
        and report.get('governanceNumBlocks') == 7838
        and report.get('exactExpectedNodeHash') == span.EXPECTED
        and all(report.get(k) is True for k in
                ['innerRollbackTested', 'atomicSuccessTested', 'paused']),
        'Expected the completed 7838 atomic report')
    rt.m.require(report.get('spanEvidence', {}).get('recordChainSha256') == span.RECORD_CHAIN,
                 'Wrong atomic replay evidence')


def previous_restart(path, items):
    path = path.absolute()
    rt.m.require(path.resolve() == path and path.parent.parent == rt.SCRIPTS,
                 'Previous report must be under /data_new/scripts')
    report = json.loads(path.read_text())
    rt.m.require(report.get('status') == 'nitro_restart_rehearsal_passed'
        and report.get('container') == 'recovery-nitro-restart-check'
        and report.get('gracefulRestartTested') is True
        and report.get('noAdditionalSenderNonceObserved') is True
        and report.get('testContainerStopped') is True
        and report.get('ownedForkStillRunning') is False,
        'Expected the completed previous restart report')
    c = next((x for x in items if x.get('Id') == report.get('containerId')), None)
    rt.m.require(c and not c['State']['Running'] and c['Image'] == rt.IMAGE
        and c['Name'].lstrip('/') == report['container'], 'Prior container identity/state changed')
    mounts = {x['Destination']: x.get('Source') for x in c.get('Mounts', [])}
    rt.m.require(mounts.get('/home/user/.arbitrum') == str(rt.DATA)
        and mounts.get('/test.json') == str(path.parent/'config.json'), 'Prior mounts differ')


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('snapshot', type=Path)
    for flag in ['atomic-pass', 'previous-pass', 'span-audit', 'span-replay', 'out']:
        p.add_argument('--'+flag, type=Path, required=True)
    a = p.parse_args()
    summary = json.loads((a.snapshot/'summary.json').read_text())
    authority = json.loads((a.snapshot/'authority.json').read_text())
    prior = json.loads(a.previous_pass.read_text())
    check_atomic(json.loads(a.atomic_pass.read_text()))
    two.check_fixture(summary, authority, prior)
    print('Checking saved span records; no 7838-message execution rerun.', flush=True)
    evidence = two.verify_span_evidence(a.span_audit, a.span_replay)
    rt.m.require(evidence['recordChainSha256'] == span.RECORD_CHAIN, 'Unexpected replay chain')

    class SpanFork(two.TwoStakerFork):
        governance_span = 7838
        candidate_summary = summary

    original_preflight = rt.preflight
    original_recovery = rt.safe.original_rehearse
    original_save = rt.save
    completed = {}

    def preflight(*args, **kwargs):
        # main() chooses the old name first; replace it BEFORE inspecting containers.
        rt.TEST = TEST
        return original_preflight(*args, **kwargs)

    def recover(f, *args):
        result = original_recovery(f, *args)
        rt.m.require(f.exact_governance_hash_checked
            and f.governance_commitment['expectedNodeHash'] == span.EXPECTED,
            '7838 recovery commitment mismatch')
        completed.update(governanceNumBlocks=7838, exactGovernanceNodeHashChecked=True,
                         exactGovernanceNodeHash=span.EXPECTED)
        return result

    def save(path, value):
        if path.name == 'summary.json':
            value.update(completed)
            value.update(test='nitro_span_7838_restart', priorAtomicPass=str(a.atomic_pass.resolve()),
                spanEvidence=evidence, productionNumBlocksApproved=False,
                parentConfirmedAToBProven=False, freshExecutionReplayTested=False,
                limitations='Historical fork only; 7838 is a positional-span convention. '
                    'This runtime test uses sequential Safe recovery; the atomic batch was tested '
                    'separately. New disposable signer, not original account. Retained validation '
                    'progress may be reused; no fresh full B-to-C replay. Graceful restart on the '
                    'same running fork, not crash recovery or new-batch liveness. No A-to-B proof '
                    'or production action.')
        original_save(path, value)

    # Reuse all existing DB, port, image, signer and owned-fork protections.
    old = (rt.preflight, rt.previous_pass_evidence, rt.safe.SafeFork,
           rt.safe.original_rehearse, rt.save, sys.argv)
    try:
        rt.preflight, rt.previous_pass_evidence = preflight, previous_restart
        rt.safe.SafeFork, rt.safe.original_rehearse, rt.save = SpanFork, recover, save
        sys.argv = [sys.argv[0], str(a.snapshot), '--previous-pass', str(a.previous_pass),
                    '--restart-check', '--out', str(a.out)]
        return rt.main()
    finally:
        (rt.preflight, rt.previous_pass_evidence, rt.safe.SafeFork,
         rt.safe.original_rehearse, rt.save, sys.argv) = old


if __name__ == '__main__':
    raise SystemExit(main())
