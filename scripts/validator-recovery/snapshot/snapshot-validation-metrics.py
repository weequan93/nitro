#!/usr/bin/env python3
"""Enable or disable local validation metrics; restart only snapshot-sync."""
import argparse
import fcntl
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile
import socket
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


def changed_config(c, disable=False):
    require(c['parent-chain']['id'] == 42161 and c['parent-chain']['connection']['url'] == 'http://10.1.2.16:8547', 'Unexpected parent configuration')
    n = c['node']
    require(n['staker']['enable'] is False, 'Staking must remain disabled')
    require(n['batch-poster']['enable'] is False and n['delayed-sequencer']['enable'] is False, 'Posting/sequencing must remain disabled')
    require(c['execution']['sequencer']['enable'] is False, 'Execution sequencing must remain disabled')
    v = n['block-validator']
    require(v['enable'] is True and v['current-module-root'] == ROOT and v['pending-upgrade-module-root'] == '', 'Unexpected validation configuration')
    require(all(type(v.get(k)) is int and v[k] == val for k, val in {'validation-sent-limit':8, 'forward-blocks':32, 'prerecorded-blocks':4}.items()), 'Unexpected validation tuning; no changes made')
    require(c.get('metrics') is disable, 'Unexpected metrics state; do not repeat')
    if disable:
        require(c.get('metrics-server', {}).get('addr') == '127.0.0.1' and c['metrics-server'].get('port') == 8360, 'Unexpected metrics endpoint')
    danger = v.get('dangerous', {})
    require(not danger.get('reset-block-validation', False), 'Validation reset configured')
    require(not danger.get('revalidation', {}).get('end-block', 0), 'Revalidation end configured')
    copy = json.loads(json.dumps(c))
    copy['metrics'] = not disable
    if not disable:
        copy.setdefault('metrics-server', {}).update({'addr':'127.0.0.1', 'port':8360})
    return copy


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument('--apply', action='store_true', help='Back up config and gracefully restart snapshot-sync with selected metrics settings')
    ap.add_argument('--disable', action='store_true', help='Disable metrics again; --apply required to mutate')
    args = ap.parse_args()
    require(not any(p.is_symlink() for p in (CONFIG, *CONFIG.parents)), 'Config path must not contain symlinks')
    with CONFIG.open('r+b') as conf:
        fcntl.flock(conf, fcntl.LOCK_EX | fcntl.LOCK_NB)
        original = conf.read()
        c = json.loads(original)
        updated = changed_config(c, args.disable)
        target = inspect(TARGET)
        protected = identity(inspect('validator-nitro-1'))
        require(target['Name'] == '/snapshot-sync' and target['Image'] == IMAGE and target['State']['Running'], 'Unexpected snapshot container identity/state')
        require(target['Config'].get('StopSignal', '') in ('', 'SIGTERM', 'SIGINT', '15', '2'), 'Unexpected non-graceful stop signal')
        mounts = {m['Destination']: m for m in target['Mounts']}
        require(mounts['/snapshot-sync.json']['Source'] == str(CONFIG) and not mounts['/snapshot-sync.json']['RW'], 'Unexpected config mount')
        require(mounts['/home/user/.arbitrum']['Source'] == '/data_mock/validator/config', 'Unexpected data mount')
        require(all(not (m['Source'] == '/data' or m['Source'].startswith('/data/')) for m in target['Mounts']), 'Protected /data mounted by snapshot container')
        argv = (target['Config'].get('Entrypoint') or []) + (target['Config'].get('Cmd') or [])
        require(not any(x.startswith('--metrics') for x in argv), 'Metrics CLI override present')
        require(target['HostConfig']['NetworkMode'] == 'host', 'Expected host network for loopback metrics')
        if not args.disable:
            with socket.socket() as probe:
                probe.bind(('127.0.0.1', 8360))
        ids = subprocess.check_output(['docker', 'ps', '-q'], text=True, timeout=30).split()
        for cid in ids:
            other = inspect(cid)
            if other['Id'] == target['Id']:
                continue
            for mount in other['Mounts']:
                src = Path(mount['Source'])
                data = Path('/data_mock/validator/config')
                require(not (src == data or src in data.parents or data in src.parents), 'Another running container shares snapshot data')
        report = dict(status='plan_only', container=TARGET, change={'metrics': {'before':c['metrics'], 'after':updated['metrics']}, 'metrics-server': {'before': {k:c.get('metrics-server', {}).get(k) for k in ('addr','port')}, 'after': {k:updated.get('metrics-server', {}).get(k) for k in ('addr','port')}}},
                      validationSettingsChanged=False, metricsEndpoint='http://127.0.0.1:8360/debug/metrics/prometheus',
                      configSha256=hashlib.sha256(original).hexdigest(), protectedBefore=protected,
                      validationReset=False, productionTransactionsSent=False, readyForProduction=False)
        if not args.apply:
            print(json.dumps(report, indent=2))
            return
        os.umask(0o077)
        out = Path(tempfile.mkdtemp(prefix='snapshot-validation-metrics-', dir=BASE))
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
            report.update(status='snapshot_started_metrics_check_pending', startedAt=after['State']['StartedAt'],
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
