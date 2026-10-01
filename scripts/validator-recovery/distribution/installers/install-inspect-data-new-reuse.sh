#!/usr/bin/env bash
(
set -euo pipefail
export PATH="$HOME/.foundry/bin:$PATH"
cd /data_new/scripts
STAGE=$(mktemp -d /data_new/scripts/data-new-reuse-install.XXXXXX)
cat > "$STAGE/inspect-data-new-reuse.py" <<'PYTHON_FILE'
#!/usr/bin/env python3
"""Inventory prior /data_new use without opening databases or starting a node."""
import json
import re
import subprocess
from pathlib import Path

BASE = Path('/data_new/scripts')
DATA = Path('/data_new/validator/config')
DB = DATA / 'Deriw Chain/nitro'
PUBLIC_FIELDS = ('status', 'source', 'target', 'copiedSourceFrozen', 'verification',
                 'container', 'containerId', 'testContainerStopped', 'ownedForkStillRunning',
                 'runtimeEndpointChecked', 'validatedEndpoint', 'normalConfirmedNode',
                 'gracefulRestartTested', 'governanceNumBlocks', 'parentConfirmedAToBProven',
                 'freshFullSpanReplayTested', 'limitations')


def run(*args):
    return subprocess.run(args, text=True, capture_output=True, check=True, timeout=60).stdout


def overlap(a, b):
    return a == b or a in b.parents or b in a.parents


def main():
    for p in (BASE, DATA, DB):
        if any(x.is_symlink() for x in (p, *p.parents)):
            raise ValueError('Unexpected symlink in inspected paths')
    report = dict(status='inventory_only', productionReuseApproved=False, databaseOpened=False,
                  containersStarted=False, containers=[], evidence=[], warnings=[])
    ids = run('docker', 'ps', '-aq').split()
    for cid in ids:
        c = json.loads(run('docker', 'inspect', cid))[0]
        if not any(overlap(Path(m['Source']), DATA) for m in c.get('Mounts', []) if m.get('Source')):
            continue
        report['containers'].append(dict(name=c['Name'], id=c['Id'], imageId=c['Image'],
            running=c['State']['Running'], startedAt=c['State']['StartedAt'], finishedAt=c['State']['FinishedAt'],
            exitCode=c['State']['ExitCode'], mounts=[{'source':m.get('Source'), 'destination':m['Destination'], 'rw':m['RW']} for m in c['Mounts']]))
    report['runningDataUsers'] = [c['name'] for c in report['containers'] if c['running']]
    patterns = ('db-replacement-*/summary.json', 'three-staker-nitro-restart-*/summary.json',
                'nitro-restart-mock-*/summary.json', 'paused-watchtower-*/launch.json')
    for pattern in patterns:
        paths = sorted(BASE.glob(pattern), key=lambda p:p.stat().st_mtime, reverse=True)[:2]
        for p in paths:
            if any(x.is_symlink() for x in (p, *p.parents)):
                continue
            try:
                value = json.loads(p.read_text())
                report['evidence'].append(dict(file=str(p), fields={k:value[k] for k in PUBLIC_FIELDS if k in value}))
            except Exception as exc:
                report['warnings'].append(dict(file=str(p), errorType=type(exc).__name__))
    report['dbDirectoryPresent'] = DB.is_dir()
    report['directories'] = [{'name':n, 'present':(DB/n).is_dir()} for n in ('l2chaindata','arbitrumdata','wasm')]
    # Compare the most recent saved runtime endpoint to the clean sync RPC.
    endpoints = [x for x in report['evidence'] if 'validatedEndpoint' in x['fields']]
    if endpoints:
        endpoint = endpoints[0]
        gs = endpoint['fields']['validatedEndpoint'].get('GlobalState', {})
        h = gs.get('BlockHash', '')
        if re.fullmatch(r'0x[0-9a-fA-F]{64}', h):
            check = {'evidenceFile':endpoint['file'], 'savedEndpointHash':h}
            try:
                header = json.loads(run('cast','rpc','--rpc-url','http://127.0.0.1:8349','--rpc-timeout','30','eth_getBlockByHash',h,'false'))
                if isinstance(header, dict) and header.get('number'):
                    canonical = json.loads(run('cast','rpc','--rpc-url','http://127.0.0.1:8349','--rpc-timeout','30','eth_getBlockByNumber',header['number'],'false'))
                    check.update(blockNumber=int(header['number'],16), matchesCleanCanonicalHash=isinstance(canonical,dict) and canonical.get('hash','').lower()==h.lower(),
                                 matchesCleanSendRoot=isinstance(canonical,dict) and canonical.get('sendRoot','').lower()==gs.get('SendRoot','').lower())
                else:
                    check['foundOnCleanSync'] = False
            except Exception as exc:
                check['errorType'] = type(exc).__name__
            report['savedEndpointComparison'] = check
    report['note'] = 'Reports are historical evidence, not current database contents. No key/config file read, Pebble opening, reset, rollback or parent-chain switch. Matching a saved endpoint alone does not approve reuse.'
    print(json.dumps(report, indent=2))


if __name__ == '__main__':
    main()
PYTHON_FILE
printf '%s  %s\n' 'e33e366bb879a875c202b6823bfa0f4a6e5c93cdb0a6e519ea0e6db7975c9700' "$STAGE/inspect-data-new-reuse.py" | sha256sum -c -
if [ -e inspect-data-new-reuse.py ] || [ -L inspect-data-new-reuse.py ]; then
  test ! -L inspect-data-new-reuse.py
  cmp "$STAGE/inspect-data-new-reuse.py" inspect-data-new-reuse.py
else
  install -m 600 "$STAGE/inspect-data-new-reuse.py" inspect-data-new-reuse.py
fi
OUT=$(mktemp /data_new/scripts/data-new-reuse-report.XXXXXX)
python3 inspect-data-new-reuse.py | tee "$OUT"
printf 'OUTPUT %s\n' "$OUT"
)
