#!/usr/bin/env bash
(
set -euo pipefail
cd /data_new/scripts
STAGE=$(mktemp -d /data_new/scripts/recording-eight-install.XXXXXX)
cat > "$STAGE/tune-snapshot-recording-eight.py" <<'PYTHON_FILE'
#!/usr/bin/env python3
"""Plan/apply/rollback bounded validation preparation tuning; restart only snapshot-sync."""
import argparse
import fcntl
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile
from datetime import datetime, timezone

BASE = Path('/data_new/scripts')
CONFIG = BASE / 'snapshot-sync.json'
TARGET = 'snapshot-sync'
IMAGE = 'sha256:b3571c0d8d4cd4e826a48895ed558927c4b003ea14050f5be3fb5ab09c62148f'
ROOT = '0x121d685e2fdb0e3291592d6b90bd70d503951335d19d96455448eb7a14d17421'


def require(ok, message):
    if not ok:
        raise ValueError(message)


def inspect(name):
    return json.loads(subprocess.check_output(['docker', 'inspect', name], text=True, timeout=30))[0]


def identity(c):
    return {k: c[k] for k in ('Id', 'Image')} | {'Running': c['State']['Running'], 'StartedAt': c['State']['StartedAt']}


def changed_config(c, rollback=False):
    require(c['parent-chain']['id'] == 42161 and c['parent-chain']['connection']['url'] == 'http://10.1.2.16:8547', 'Unexpected parent configuration')
    n = c['node']
    require(n['staker']['enable'] is False, 'Staking must remain disabled')
    require(n['batch-poster']['enable'] is False and n['delayed-sequencer']['enable'] is False, 'Posting/sequencing must remain disabled')
    require(c['execution']['sequencer']['enable'] is False, 'Execution sequencing must remain disabled')
    v = n['block-validator']
    require(v['enable'] is True and v['current-module-root'] == ROOT and v['pending-upgrade-module-root'] == '', 'Unexpected validation configuration')
    require(type(v['validation-sent-limit']) is int and v['validation-sent-limit'] == 8, 'Expected existing sent limit 8')
    expected = {'forward-blocks': 32, 'prerecorded-blocks': 8} if rollback else {'forward-blocks': 32, 'prerecorded-blocks': 4}
    for key, value in expected.items():
        require(type(v.get(key)) is int and v[key] == value, 'Unexpected preparation setting: ' + key)
    danger = v.get('dangerous', {})
    require(not danger.get('reset-block-validation', False), 'Validation reset configured')
    require(not any(danger.get('revalidation', {}).get(k, 0) for k in ('start-block', 'end-block', 'quit-after-revalidation')), 'Revalidation override configured')
    copy = json.loads(json.dumps(c))
    copy['node']['block-validator']['prerecorded-blocks'] = 4 if rollback else 8
    return copy


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument('--apply', action='store_true', help='Back up config, gracefully stop snapshot-sync, apply selected settings and start it')
    ap.add_argument('--rollback', action='store_true', help='Restore prerecorded-blocks to 4; requires --apply to mutate')
    args = ap.parse_args()
    require(not any(p.is_symlink() for p in (CONFIG, *CONFIG.parents)), 'Config path must not contain symlinks')
    with CONFIG.open('r+b') as conf:
        fcntl.flock(conf, fcntl.LOCK_EX | fcntl.LOCK_NB)
        original = conf.read()
        c = json.loads(original)
        updated = changed_config(c, args.rollback)
        target = inspect(TARGET)
        protected = identity(inspect('validator-nitro-1'))
        require(target['Name'] == '/snapshot-sync' and target['Image'] == IMAGE and target['State']['Running'], 'Unexpected snapshot container identity/state')
        require(target['Config'].get('StopSignal', '') in ('', 'SIGTERM', 'SIGINT', '15', '2'), 'Unexpected non-graceful stop signal')
        mounts = {m['Destination']: m for m in target['Mounts']}
        require(mounts['/snapshot-sync.json']['Source'] == str(CONFIG) and not mounts['/snapshot-sync.json']['RW'], 'Unexpected config mount')
        require(mounts['/home/user/.arbitrum']['Source'] == '/data_mock/validator/config', 'Unexpected data mount')
        require(all(not (m['Source'] == '/data' or m['Source'].startswith('/data/')) for m in target['Mounts']), 'Protected /data mounted by snapshot container')
        argv = (target['Config'].get('Entrypoint') or []) + (target['Config'].get('Cmd') or [])
        require(not any(x.startswith('--node.block-validator.' + k) for x in argv for k in ('validation-sent-limit', 'forward-blocks', 'prerecorded-blocks')), 'CLI override present')
        ids = subprocess.check_output(['docker', 'ps', '-q'], text=True, timeout=30).split()
        for cid in ids:
            other = inspect(cid)
            if other['Id'] == target['Id']:
                continue
            for mount in other['Mounts']:
                src = Path(mount['Source'])
                data = Path('/data_mock/validator/config')
                require(not (src == data or src in data.parents or data in src.parents), 'Another running container shares snapshot data')
        report = dict(status='plan_only', container=TARGET, change={k: {'before': c['node']['block-validator'][k], 'after': updated['node']['block-validator'][k]} for k in ('prerecorded-blocks',)},
                      rollback=args.rollback, validationSentLimit=8,
                      configSha256=hashlib.sha256(original).hexdigest(), protectedBefore=protected,
                      validationReset=False, productionTransactionsSent=False, readyForProduction=False)
        if not args.apply:
            print(json.dumps(report, indent=2))
            return
        if not args.rollback:
            mem = dict(line.split(':', 1) for line in Path('/proc/meminfo').read_text().splitlines())
            require(int(mem['MemAvailable'].split()[0]) >= 6 * 1024 * 1024, 'Less than 6 GiB available; do not increase preparation yet')
        os.umask(0o077)
        out = Path(tempfile.mkdtemp(prefix='snapshot-recording-eight-', dir=BASE))
        report['output'] = str(out)
        (out / 'config-before.json').write_bytes(original)
        proposed = (json.dumps(updated, indent=2) + '\n').encode()
        (out / 'config-after.json').write_bytes(proposed)
        print('OUTPUT', out, flush=True)
        try:
            require(identity(inspect(TARGET)) == identity(target), 'Snapshot state changed during preparation')
            report['status'] = 'stopping_snapshot'
            (out / 'summary.json').write_text(json.dumps(report, indent=2))
            print('Gracefully stopping only snapshot-sync; waiting without a forced-kill timeout.', flush=True)
            subprocess.run(['docker', 'stop', '-t', '-1', TARGET], check=True)
            stopped = inspect(TARGET)
            require(stopped['Id'] == target['Id'] and not stopped['State']['Running'], 'Snapshot did not stop')
            conf.seek(0)
            require(conf.read() == original, 'Config changed concurrently')
            require(os.stat(CONFIG).st_ino == os.fstat(conf.fileno()).st_ino, 'Config file replaced concurrently')
            require(identity(inspect('validator-nitro-1')) == protected, 'Protected container state changed')
            # Preserve inode, permissions and ownership for the existing file bind mount.
            conf.seek(0)
            conf.write(proposed)
            conf.truncate()
            conf.flush()
            os.fsync(conf.fileno())
            report['status'] = 'config_written'
            (out / 'summary.json').write_text(json.dumps(report, indent=2))
            subprocess.run(['docker', 'start', TARGET], check=True)
            after = inspect(TARGET)
            require(after['Id'] == target['Id'] and after['State']['Running'], 'Snapshot not running after start')
            report.update(status='snapshot_started_validation_progress_pending', startedAt=after['State']['StartedAt'],
                          protectedAfter=identity(inspect('validator-nitro-1')))
            report['protectedUnchanged'] = report['protectedAfter'] == protected
            require(report['protectedUnchanged'], 'Protected container changed externally')
        except BaseException as exc:
            report.update(failedAfterStage=report['status'], status='stopped_for_review', errorType=type(exc).__name__)
            print('Fix interrupted; inspect summary. No automatic reset or additional restart.', flush=True)
            raise
        finally:
            (out / 'summary.json').write_text(json.dumps(report, indent=2))
        print(json.dumps(report, indent=2))


if __name__ == '__main__':
    main()
PYTHON_FILE
printf '%s  %s\n' 'a8bf1ffe0f2f40e40594a9e9766ae5b9040f25b278305afb0cfe3a4ec2253ad0' "$STAGE/tune-snapshot-recording-eight.py" | sha256sum -c -
if [ -e tune-snapshot-recording-eight.py ] || [ -L tune-snapshot-recording-eight.py ]; then
  test ! -L tune-snapshot-recording-eight.py
  cmp "$STAGE/tune-snapshot-recording-eight.py" tune-snapshot-recording-eight.py
else
  install -m 600 "$STAGE/tune-snapshot-recording-eight.py" tune-snapshot-recording-eight.py
fi
python3 tune-snapshot-recording-eight.py
)
