#!/usr/bin/env bash
(
set -euo pipefail
export PATH="$HOME/.foundry/bin:$PATH"
cd /data_new/scripts
STAGE=$(mktemp -d /data_new/scripts/data-new-parent-install.XXXXXX)
cat > "$STAGE/prepare-data-new-parent-check.py" <<'PYTHON_FILE'
#!/usr/bin/env python3
"""Prepare a non-staking real-parent check of /data_new; --start explicitly opens it writable."""
import argparse
import json
import os
from pathlib import Path
import socket
import subprocess
import tempfile

BASE = Path('/data_new/scripts')
DATA = Path('/data_new/validator/config')
SOURCE = BASE / 'snapshot-sync.json'
NAME = 'data-new-parent-check'
IMAGE = 'sha256:b3571c0d8d4cd4e826a48895ed558927c4b003ea14050f5be3fb5ab09c62148f'
ROOT = '0x121d685e2fdb0e3291592d6b90bd70d503951335d19d96455448eb7a14d17421'
PARENT = 'http://10.1.2.16:8547'


def require(ok, message):
    if not ok:
        raise ValueError(message)


def run(*args):
    p = subprocess.run(args, capture_output=True, text=True, timeout=90)
    require(p.returncode == 0, 'Command failed; arguments/output withheld: ' + args[0])
    return p.stdout.strip()


def inspect(name):
    return json.loads(run('docker', 'inspect', name))[0]


def identity(c):
    return dict(id=c['Id'], image=c['Image'], running=c['State']['Running'], startedAt=c['State']['StartedAt'])


def prepare(c):
    c = json.loads(json.dumps(c))
    require(c['parent-chain']['id'] == 42161 and c['parent-chain']['connection']['url'] == PARENT, 'Unexpected source parent')
    info = json.loads(c['chain']['info-json'])
    require(isinstance(info, list) and info, 'Missing chain definitions')
    for item in info:
        require(item.get('parent-chain-id') == 42161, 'Source chain definition is not real parent')
        for k in ('sequencer-url','secondary-forwarding-target','feed-url','secondary-feed-url'):
            item[k] = ''
    c['chain']['info-json'] = json.dumps(info)
    c['chain'].pop('dev-wallet', None)
    n = c['node']
    n['staker'] = {'enable':False, 'only-create-wallet-contract':False}
    n['sequencer'] = False
    n['batch-poster'] = {'enable':False}
    n['delayed-sequencer'] = {'enable':False}
    n['feed'] = {'input':{'url':[], 'secondary-url':[]}, 'output':{'enable':False}}
    require(not n.get('seq-coordinator', {}).get('enable', False), 'Coordinator enabled in source')
    # Initialize/read the existing validation checkpoint, but intentionally hold
    # automatic validation submissions during this first parent-connectivity check.
    n['block-validator'] = {'enable':True, 'failure-is-fatal':True,
        'current-module-root':ROOT, 'pending-upgrade-module-root':'',
        'prerecorded-blocks':1, 'forward-blocks':0, 'validation-sent-limit':0}
    e = c['execution']
    e['sequencer'] = {'enable':False}
    e['forwarding-target'] = 'null'
    e['secondary-forwarding-target'] = []
    c['http'] = {'addr':'127.0.0.1','port':8249,'vhosts':['localhost','127.0.0.1'], 'corsdomain':[], 'api':['eth','net','web3','arb','arbdebug']}
    c['ws'] = {'addr':'127.0.0.1','port':8272,'rpcprefix':'/ws'}
    c['metrics'] = False
    c['pprof'] = False
    c.setdefault('file-logging', {})['enable'] = False
    return c


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument('--start', action='store_true', help='Start the prepared non-staking node; it writes ONLY the /data_new candidate and its own files')
    a = ap.parse_args()
    for p in (BASE, DATA, SOURCE, DATA/'Deriw Chain/nitro'):
        require(not any(x.is_symlink() for x in (p, *p.parents)), 'Unexpected symlink')
    require((DATA/'Deriw Chain/nitro').is_dir(), 'Candidate database missing')
    cfg = prepare(json.loads(SOURCE.read_text()))
    require(run('cast','chain-id','--rpc-url',PARENT,'--rpc-timeout','30') == '42161', 'Wrong real parent chain')
    protected = {n:identity(inspect(n)) for n in ('validator-nitro-1','snapshot-sync')}
    ids = run('docker','ps','-aq').split()
    for cid in ids:
        c = inspect(cid)
        require(c['Name'] != '/'+NAME, 'Check container already exists; inspect it instead of replacing it')
        if c['State']['Running']:
            for m in c['Mounts']:
                src = Path(m['Source'])
                require(not (src == DATA or src in DATA.parents or DATA in src.parents), 'Candidate data in use by another container')
    for host, port in (('127.0.0.1',8249),('127.0.0.1',8272),('127.0.0.10',52100),('127.0.0.10',52101)):
        with socket.socket() as sock:
            sock.bind((host,port))
    entry = run('docker','run','--rm','--network','none','--read-only','--entrypoint','/bin/cat',IMAGE,'/usr/local/bin/split-val-entry.sh')
    require(entry.count('52000') == 3 and entry.count('52001') == 3, 'Unexpected image entry script')
    entry = entry.replace('52000','52100').replace('52001','52101')+'\n'
    uid = int(run('docker','run','--rm','--network','none','--read-only','--entrypoint','/usr/bin/id',IMAGE,'-u'))
    gid = int(run('docker','run','--rm','--network','none','--read-only','--entrypoint','/usr/bin/id',IMAGE,'-g'))
    out = Path(tempfile.mkdtemp(prefix='data-new-parent-check-', dir=BASE))
    os.chown(out, uid, gid)
    for name, content in (('config.json',json.dumps(cfg,indent=2)+'\n'),('entry.sh',entry)):
        p = out/name
        with p.open('x') as f:
            f.write(content)
        os.chmod(p, 0o600)
        os.chown(p, uid, gid)
    report = dict(status='prepared_not_started', container=NAME, output=str(out), data=str(DATA),
        parentRpc=PARENT, parentChainId=42161, rpc='http://127.0.0.1:8249',
        stakingEnabled=False, automaticValidationSubmissionsEnabled=False,
        validatorServiceInitializedForCheckpointRead=True, candidateWillBeWrittenOnStart=True,
        resetRequested=False, protectedBefore=protected, productionReuseApproved=False)
    if a.start:
        require(all(identity(inspect(n)) == before for n,before in protected.items()), 'Protected container changed during preparation')
        cid = run('docker','run','-d','--name',NAME,'--restart=no','--network','host',
            '--log-opt','max-size=100m','--log-opt','max-file=3',
            '-v',str(DATA)+':/home/user/.arbitrum',
            '-v',str(out/'config.json')+':/check.json:ro',
            '-v',str(out/'entry.sh')+':/check-entry.sh:ro',
            '--entrypoint','/bin/bash',IMAGE,'/check-entry.sh','--conf.file','/check.json')
        report.update(status='started_runtime_checks_pending', containerId=cid,
            protectedAfter={n:identity(inspect(n)) for n in protected})
        report['protectedUnchanged'] = report['protectedAfter'] == protected
    (out/'summary.json').write_text(json.dumps(report,indent=2)+'\n')
    print(json.dumps(report,indent=2))


if __name__ == '__main__':
    main()
PYTHON_FILE
printf '%s  %s\n' '10e38e2b21206c87230ca974f46d39f4e1560270b380ceaff2ca10133df6933a' "$STAGE/prepare-data-new-parent-check.py" | sha256sum -c -
if [ -e prepare-data-new-parent-check.py ] || [ -L prepare-data-new-parent-check.py ]; then
  test ! -L prepare-data-new-parent-check.py
  cmp "$STAGE/prepare-data-new-parent-check.py" prepare-data-new-parent-check.py
else
  install -m 600 "$STAGE/prepare-data-new-parent-check.py" prepare-data-new-parent-check.py
fi
python3 prepare-data-new-parent-check.py
)
