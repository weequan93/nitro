#!/usr/bin/env python3
"""Read-only recovery inventory. Never opens keystores or starts/stops a container."""
import argparse
import datetime
import json
import os
import re
import subprocess
from pathlib import Path

EXPECTED = '0x5cda45a9ae0e52f1d5110dc3819f6fb96bade33a'
CONFIGS = (
    '/data_new/validator/config/nodeConfig.json',
    '/data_new/scripts/snapshot-sync.json',
)
CONTAINERS = (
    'validator-debug-nitro-1', 'validator_new-nitro-1',
    'snapshot-sync', 'recovery-paused-watchtower', 'validator-nitro-1',
)
BOOL_FIELDS = (
    'node.staker.enable', 'node.staker.use-smart-contract-wallet',
    'node.staker.only-create-wallet-contract',
    'node.staker.enable-fast-confirmation',
    'node.staker.start-validation-from-staked',
    'node.staker.dangerous.without-block-validator',
    'node.staker.dangerous.ignore-rollup-wasm-module-root',
    'node.block-validator.enable', 'node.block-validator.failure-is-fatal',
    'node.sequencer', 'node.batch-poster.enable',
    'node.delayed-sequencer.enable', 'execution.sequencer.enable',
)
ADDRESS = re.compile(r'0x[0-9a-fA-F]{40}\Z')
ROOT = re.compile(r'0x[0-9a-fA-F]{64}\Z')
STRATEGIES = {'Watchtower', 'Defensive', 'StakeLatest', 'ResolveNodes', 'MakeNodes'}


def field(obj, name):
    for part in name.split('.'):
        if not isinstance(obj, dict) or part not in obj:
            return None
        obj = obj[part]
    return obj


def safe_address(value):
    return value.lower() if isinstance(value, str) and ADDRESS.fullmatch(value) else None


def config_summary(c):
    result = {'declaredFieldsOnly': True, 'missingMeansUnknownNotFalse': True}
    result['flags'] = {key: value if type(value) is bool else None
                       for key in BOOL_FIELDS for value in [field(c, key)]}
    strategy = field(c, 'node.staker.strategy')
    result['strategy'] = strategy if isinstance(strategy, str) and strategy in STRATEGIES else None
    chain = field(c, 'parent-chain.id')
    result['parentChainId'] = chain if type(chain) is int else None
    result['parentUrlConfigured'] = bool(field(c, 'parent-chain.connection.url'))
    # URL, passwords, keys, wallet path and arbitrary config strings are never emitted.
    wallet = field(c, 'node.staker.parent-chain-wallet')
    wallet = wallet if isinstance(wallet, dict) else {}
    account = safe_address(wallet.get('account'))
    contract = safe_address(field(c, 'node.staker.contract-wallet-address'))
    result['wallet'] = {
        'account': account, 'contractWalletAddress': contract,
        'accountMatchesExpected': account == EXPECTED if account else None,
        'inlinePrivateKeyPresent': bool(wallet.get('private-key')),
        'keystorePathConfigured': bool(wallet.get('pathname')),
        'passwordConfigured': bool(wallet.get('password')) and wallet.get('password') != 'PASSWORD_NOT_SET',
        'onlyCreateKey': wallet.get('only-create-key') if type(wallet.get('only-create-key')) is bool else None,
        'signerIdentityVerified': False,
    }
    for name in ('current-module-root', 'pending-upgrade-module-root'):
        value = field(c, 'node.block-validator.' + name)
        result[name] = (value if isinstance(value, str) and
                        (value in ('', 'current') or ROOT.fullmatch(value)) else None)
    return result


def command(*args):
    p = subprocess.run(args, capture_output=True, text=True, timeout=30)
    if p.returncode:
        raise RuntimeError('command failed')  # Do not expose stderr or command arguments.
    return p.stdout


def container_summary(c, protected=False):
    config = c.get('Config') or {}
    result = {
        'imageId': c.get('Image'), 'running': c['State']['Running'],
        'status': c['State']['Status'], 'startedAt': c['State'].get('StartedAt'),
        'network': c['HostConfig'].get('NetworkMode'),
        'protected': protected,
    }
    if protected:
        return result
    # Only known Nitro flag names are reported, never their values or shell strings.
    known = set(BOOL_FIELDS) | {'conf.file', 'conf.env-prefix', 'parent-chain.id',
        'parent-chain.connection.url', 'node.staker.strategy',
        'node.staker.contract-wallet-address'}
    known |= {'node.staker.parent-chain-wallet.' + k for k in
              ('account', 'pathname', 'password', 'private-key', 'only-create-key')}
    argv = (config.get('Entrypoint') or []) + (config.get('Cmd') or [])
    result['knownLaunchFlagNames'] = sorted({a[2:].split('=', 1)[0] for a in argv
        if isinstance(a, str) and a.startswith('--') and a[2:].split('=', 1)[0] in known})
    result['environmentEntriesPresent'] = bool(config.get('Env'))
    result['launchSettingsNotResolved'] = True
    result['mounts'] = [{'source': m.get('Source'), 'destination': m.get('Destination'),
                         'writable': m.get('RW')} for m in c.get('Mounts', [])]
    return result


def observed_identities(log):
    result = []
    for line in log.splitlines():
        if 'running as validator' not in line:
            continue
        item = {}
        for label in ('txSender', 'actingAsWallet'):
            match = re.search(r'\b' + label + r'=(0x[0-9a-fA-F]{40})(?![0-9a-fA-F])', line)
            item[label] = match.group(1).lower() if match else None
        if item not in result:
            result.append(item)
    return result[-10:]


def read_config(path):
    resolved = Path(path).resolve()
    if Path('/data_new') not in resolved.parents:
        raise ValueError('config outside allowed area')
    if resolved.stat().st_size > 4 * 1024 * 1024:
        raise ValueError('config too large')
    return config_summary(json.loads(resolved.read_text()))


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--out', type=Path, required=True)
    a = p.parse_args()
    out = a.out.absolute()
    # Only create the explicitly requested report under the scripts directory.
    if out.parent.resolve() != Path('/data_new/scripts').resolve():
        p.error('Output must be in /data_new/scripts')
    if out.exists() or out.is_symlink():
        p.error('Output already exists')
    report = {
        'checkedAt': datetime.datetime.now(datetime.timezone.utc).isoformat(),
        'expectedOriginalStaker': EXPECTED, 'readyForProduction': False,
        'productionSigningTested': False, 'configs': {}, 'containers': {},
        'errors': [],
        'limitations': 'Declared config and historical startup identities only. '
        'CLI/environment/entrypoint overrides and defaults are not resolved. '
        'No private-key derivation, keystore unlock, signing, chain transaction or runtime acceptance test. '
        'Protected validator metadata only; /data files are not opened.',
    }
    for path in CONFIGS:
        try:
            report['configs'][path] = read_config(path)
        except Exception as exc:
            report['errors'].append({'source': path, 'errorType': type(exc).__name__})
    for name in CONTAINERS:
        try:
            item = json.loads(command('docker', 'inspect', name))[0]
            report['containers'][name] = container_summary(item, name == 'validator-nitro-1')
            if name == 'validator-debug-nitro-1':
                try:
                    # Docker can put logs on both streams. Keep all raw text private.
                    logs = subprocess.run(['docker', 'logs', '--tail', '5000', name],
                        capture_output=True, text=True, timeout=30)
                    if logs.returncode:
                        raise RuntimeError('logs unavailable')
                    report['containers'][name]['historicalStartupIdentities'] = observed_identities(
                        logs.stdout + '\n' + logs.stderr)
                except Exception as exc:
                    report['errors'].append({'source': name + ':logs', 'errorType': type(exc).__name__})
        except Exception as exc:
            report['errors'].append({'source': name, 'errorType': type(exc).__name__})
    report['status'] = 'inventory_partial' if report['errors'] else 'inventory_collected_runtime_unverified'
    fd = os.open(out, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, 'w') as f:
        json.dump(report, f, indent=2)
    print(json.dumps(report, indent=2))
    print('OUTPUT', out)


if __name__ == '__main__':
    main()
