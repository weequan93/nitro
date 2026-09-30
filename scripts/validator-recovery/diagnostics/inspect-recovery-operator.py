#!/usr/bin/env python3
"""Portable read-only inventory for a selected recovery validator operator (standard library only)."""
import argparse
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import re
import stat
import subprocess

EXPECTED = '0x5cda45a9ae0e52f1d5110dc3819f6fb96bade33a'
BOOLS = ('node.staker.enable', 'node.staker.use-smart-contract-wallet',
         'node.staker.only-create-wallet-contract', 'node.staker.enable-fast-confirmation',
         'node.staker.start-validation-from-staked', 'node.block-validator.enable',
         'node.block-validator.failure-is-fatal', 'node.sequencer',
         'node.batch-poster.enable', 'execution.sequencer.enable')


def get(obj, dotted):
    for key in dotted.split('.'):
        if not isinstance(obj, dict):
            return None
        obj = obj.get(key)
    return obj


def address(value):
    if not isinstance(value, str):
        return None
    value = value.removeprefix('0x')
    return '0x' + value.lower() if re.fullmatch('[0-9a-fA-F]{40}', value) else None


def config_report(c):
    wallet = get(c, 'node.staker.parent-chain-wallet') or {}
    if not isinstance(wallet, dict):
        wallet = {}
    strategy = get(c, 'node.staker.strategy')
    root = get(c, 'node.block-validator.current-module-root')
    external = 'node.staker.data-poster.external-signer'
    return {
        'source': 'operator_selected_config_file',
        'effectiveRuntimeConfigVerified': False,
        'flags': {k: get(c, k) if type(get(c, k)) is bool else None for k in BOOLS},
        'strategy': strategy if isinstance(strategy, str) and strategy in
                    {'Watchtower', 'Defensive', 'StakeLatest', 'ResolveNodes', 'MakeNodes'} else None,
        'parentChainId': get(c, 'parent-chain.id') if type(get(c, 'parent-chain.id')) is int else None,
        'parentUrlConfigured': bool(get(c, 'parent-chain.connection.url')),
        'currentModuleRoot': root if isinstance(root, str) and
            (root == 'current' or re.fullmatch('0x[0-9a-fA-F]{64}', root)) else None,
        'wallet': {'account': address(wallet.get('account')),
                   'keystoreConfigured': bool(wallet.get('pathname')),
                   'inlineKeyConfigured': bool(wallet.get('private-key')),
                   'passwordConfigured': bool(wallet.get('password')) and
                       wallet.get('password') != 'PASSWORD_NOT_SET',
                   'contractWalletAddress': address(get(c, 'node.staker.contract-wallet-address'))},
        'externalSigner': {'urlConfigured': bool(get(c, external + '.url')),
                           'address': address(get(c, external + '.address'))},
    }


def launch_report(argv):
    switches = {k: None for k in BOOLS}
    for token in argv:
        if not isinstance(token, str) or not token.startswith('--'):
            continue
        k, sep, v = token[2:].partition('=')
        if k in switches:
            switches[k] = True if not sep else (
                True if v.lower() in ('true', 't', '1') else
                False if v.lower() in ('false', 'f', '0') else 'unrecognized')
    return switches


def identities(logs):
    found = []
    for line in logs.splitlines():
        if 'running as validator' not in line:
            continue
        item = {}
        for key in ('txSender', 'actingAsWallet'):
            m = re.search(r'\b' + key + r'=(0x[0-9a-fA-F]{40})(?![0-9a-fA-F])', line)
            item[key] = address(m.group(1)) if m else None
        if item not in found:
            found.append(item)
    return found[-10:]


def docker(*args, include_stderr=False):
    p = subprocess.run(['docker', *args], capture_output=True, text=True, timeout=30)
    if p.returncode:
        raise RuntimeError('docker query failed')
    return p.stdout + ('\n' + p.stderr if include_stderr else '')


def read_selected_config(path):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd) as f:
        s = os.fstat(f.fileno())
        if not stat.S_ISREG(s.st_mode) or s.st_size > 4 * 1024 * 1024:
            raise ValueError('Not a bounded regular JSON configuration')
        return config_report(json.load(f))


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--expected-staker', required=True, help='Expected public staking address; never a private key')
    p.add_argument('--container', help='Exact existing Docker container name or ID; read-only')
    p.add_argument('--config', type=Path, help='Optional host path to node JSON config; NOT a key/password file')
    p.add_argument('--out', required=True, type=Path)
    a = p.parse_args()
    expected = address(a.expected_staker)
    if expected is None:
        p.error('Expected staker must be a 20-byte public address')
    if a.config and (a.config.resolve() == Path('/data') or Path('/data') in a.config.resolve().parents):
        p.error('/data is protected; omit --config and use Docker metadata only')
    if a.out.resolve() == Path('/data') or Path('/data') in a.out.resolve().parents:
        p.error('/data is protected; choose another output location')
    if a.container and not re.fullmatch('[a-zA-Z0-9][a-zA-Z0-9_.-]*', a.container):
        p.error('Invalid container name/ID')
    if a.out.exists() or a.out.is_symlink():
        p.error('Output exists; select a new report filename')
    report = {'checkedAt': datetime.now(timezone.utc).isoformat(),
              'expectedOriginalStaker': expected, 'readyForProduction': False,
              'signingTested': False, 'errors': [],
              'limitations': 'Config declarations and sampled historical logs only; '
              'defaults, environment and entrypoint overrides unresolved. '
              'No keystore read, unlock, signing, RPC calls, transaction or container mutation.'}
    try:
        # Listing never reads command strings, environment or database/key files.
        report['containers'] = [json.loads(line) for line in docker('ps', '-a', '--format',
            '{"name":{{json .Names}},"image":{{json .Image}},"status":{{json .Status}}}').splitlines() if line]
    except Exception as exc:
        report['errors'].append({'step': 'docker_list', 'errorType': type(exc).__name__})
    if a.container:
        try:
            c = json.loads(docker('inspect', a.container))[0]
            cfg = c.get('Config') or {}
            report['selectedContainer'] = {
                'name': a.container, 'imageId': c['Image'], 'running': c['State']['Running'],
                'startedAt': c['State'].get('StartedAt'),
                'network': c['HostConfig'].get('NetworkMode'),
                'launchBooleanSwitches': launch_report((cfg.get('Entrypoint') or []) + (cfg.get('Cmd') or [])),
                'environmentPresentNotResolved': bool(cfg.get('Env')),
            }
            report['selectedContainer']['historicalStartupIdentities'] = identities(
                docker('logs', '--tail', '5000', a.container, include_stderr=True))
        except Exception as exc:
            report['errors'].append({'step': 'selected_container', 'errorType': type(exc).__name__})
    observed = report.get('selectedContainer', {}).get('historicalStartupIdentities', [])
    report['expectedStakerSeenInHistoricalLogs'] = any(
        x.get('actingAsWallet') == expected for x in observed) if observed else None
    report['currentSignerVerified'] = False
    if a.config:
        try:
            report['config'] = read_selected_config(a.config)
        except Exception as exc:
            report['errors'].append({'step': 'selected_config', 'errorType': type(exc).__name__})
    report['status'] = 'partial_inventory' if report['errors'] else (
        'inventory_collected_signing_unverified' if a.container or a.config else 'container_selection_required')
    fd = os.open(a.out, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, 'w') as f:
        json.dump(report, f, indent=2)
    print(json.dumps(report, indent=2))
    print('OUTPUT', a.out.absolute())


if __name__ == '__main__':
    main()
