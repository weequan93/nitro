#!/usr/bin/env python3
"""Enable on-demand diagnostics on the stopped snapshot-sync node, without staking."""
import json
import os
import subprocess
from datetime import datetime, timezone
from pathlib import Path

CONFIG = Path('/data_new/scripts/snapshot-sync.json')
DATA = '/data_mock/validator/config'
IMAGE = 'sha256:b3571c0d8d4cd4e826a48895ed558927c4b003ea14050f5be3fb5ab09c62148f'
ROOT = '0x121d685e2fdb0e3291592d6b90bd70d503951335d19d96455448eb7a14d17421'

def run(*args):
    return subprocess.check_output(args, text=True).strip()

def require(condition, reason):
    if not condition:
        raise SystemExit('STOP: ' + reason)

def main():
    container = json.loads(run('docker', 'inspect', 'snapshot-sync'))[0]
    require(not container['State']['Running'], 'stop snapshot-sync gracefully first')
    require(container['Image'] == IMAGE, 'unexpected image; configuration requires version review')
    mounts = container['Mounts']
    require(any(m['Source'] == str(CONFIG) and m['Destination'] == '/snapshot-sync.json'
                for m in mounts), 'unexpected config mount')
    require(any(m['Source'] == DATA and m['Destination'] == '/home/user/.arbitrum'
                for m in mounts), 'unexpected database mount')
    require(not CONFIG.is_symlink(), 'configuration is a symlink')
    original = CONFIG.read_text()
    c = json.loads(original)
    require(c['parent-chain']['id'] == 42161, 'parent chain must be 42161')
    require(c['parent-chain']['connection']['url'] == 'http://10.1.2.16:8547', 'unexpected parent RPC')
    require(c['http']['addr'] == '127.0.0.1' and c['http']['port'] == 8349, 'unexpected HTTP endpoint')
    n = c['node']
    require(n['staker']['enable'] is False, 'staker must stay disabled')
    require('parent-chain-wallet' not in n['staker'], 'unexpected wallet configuration')
    require(n['sequencer'] is False and n['batch-poster']['enable'] is False
            and n['delayed-sequencer']['enable'] is False, 'unexpected transaction-producing service')
    require(c['execution']['sequencer']['enable'] is False, 'execution sequencer enabled')
    latest = run('docker', 'run', '--rm', '--network', 'none', '--entrypoint', '/bin/cat',
                 IMAGE, '/home/user/target/machines/latest/module-root.txt')
    require(latest.lower() == ROOT, 'image latest machine does not match expected new root')
    bv = n.setdefault('block-validator', {})
    require(not bv.get('dangerous'), 'unexpected validation reset/revalidation configuration')
    bv.update({'enable': True, 'failure-is-fatal': True,
               'current-module-root': ROOT, 'pending-upgrade-module-root': '',
               'validation-sent-limit': 0, 'forward-blocks': 0, 'prerecorded-blocks': 1})
    c.setdefault('validation', {}).setdefault('wasm', {})['root-path'] = '/home/user/target/machines'
    require('arbdebug' in c['http']['api'], 'arbdebug must be enabled locally')
    backup = CONFIG.with_name(CONFIG.name + '.before-diagnostics-' +
        datetime.now(timezone.utc).strftime('%Y%m%d-%H%M%S'))
    fd = os.open(backup, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, 'w') as f:
        f.write(original)
    # Preserve inode and ownership: Docker already bind-mounts this file.
    with CONFIG.open('w') as f:
        json.dump(c, f, indent=2)
        f.write('\n')
        f.flush()
        os.fsync(f.fileno())
    print('CONFIG READY:', CONFIG)
    print('BACKUP:', backup)
    print('Staker/sequencer/batch poster remain disabled. No wallet added.')
    print('Validation service enabled; automatic validation submissions limited to zero.')
    print('At most bounded background preparation remains; on-demand RPC validation is available after startup.')
    print('Database not opened by this script. Start snapshot-sync and inspect its logs.')

if __name__ == '__main__':
    main()
