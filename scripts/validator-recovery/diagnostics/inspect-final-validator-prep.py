#!/usr/bin/env python3
"""Read-only Docker preparation report. Does not open configs, keys or databases."""
import argparse
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import re
import subprocess

ROLES = {
    'current': '0x65fa6c1d1efc338dfe8f07d5de84b3e7b76cbc95',
    'cloud-a': '0x5cda45a9ae0e52f1d5110dc3819f6fb96bade33a',
    'cloud-b': '0x21d4ea822a07f737c5e69f7951d517e5f2974849',
}
OLD = 'sha256:becc38893aca04b22a4642377c24017c970a291c791c500758588c0a072063ae'
NEW = 'sha256:b3571c0d8d4cd4e826a48895ed558927c4b003ea14050f5be3fb5ab09c62148f'
FLAGS = ('node.staker.enable', 'node.staker.enable-fast-confirmation',
         'node.staker.use-smart-contract-wallet', 'node.block-validator.enable',
         'node.staker.dangerous.without-block-validator',
         'node.staker.dangerous.ignore-rollup-wasm-module-root')
STRATEGIES = {'Watchtower', 'Defensive', 'StakeLatest', 'ResolveNodes', 'MakeNodes'}


def docker(*args):
    # Fixed read-only subcommands only; never emit raw inspect/log contents.
    if args[0] not in ('inspect', 'logs', 'image') or (args[0] == 'image' and args[1] != 'inspect'):
        raise ValueError('Non-read-only Docker command')
    result = subprocess.run(['docker', *args], text=True, capture_output=True, timeout=45)
    if result.returncode:
        raise RuntimeError('Docker metadata unavailable')
    return result.stdout + ('\n' + result.stderr if args[0] == 'logs' else '')


def launch(argv):
    flags = {name: None for name in FLAGS}
    strategy = None
    for i, token in enumerate(argv):
        if not isinstance(token, str) or not token.startswith('--'):
            continue
        key, sep, value = token[2:].partition('=')
        if key in flags:
            # A standalone pflag boolean means true. A following word is not an equals value.
            flags[key] = True if not sep else {'true': True, 'false': False, '1': True,
                '0': False, 't': True, 'f': False}.get(value.lower(), 'unrecognized')
        if key == 'node.staker.strategy':
            value = value if sep else (argv[i+1] if i+1 < len(argv) else None)
            strategy = value if isinstance(value, str) and value in STRATEGIES else None
    return {'explicitBooleanFlags': flags, 'explicitStrategy': strategy,
            'effectiveConfigVerified': False,
            'note': 'Missing means unknown. Config files, environment, wrappers and defaults not resolved.'}


def identities(logs):
    result = []
    for line in logs.splitlines():
        if 'running as validator' not in line:
            continue
        row = {}
        for key in ('txSender', 'actingAsWallet'):
            match = re.search(r'\b'+key+r'=(0x[0-9a-fA-F]{40})(?![0-9a-fA-F])', line)
            row[key] = match.group(1).lower() if match else None
        if row not in result:
            result.append(row)
    return result[-10:]


def image_report(image):
    return {'id': image.get('Id'), 'repoDigests': image.get('RepoDigests') or [],
            'architecture': image.get('Architecture'), 'os': image.get('Os')}


def summarize(c):
    cfg = c.get('Config') or {}
    host = c.get('HostConfig') or {}
    labels = cfg.get('Labels') or {}
    return {'id': c['Id'], 'imageId': c['Image'], 'oldImageMatches': c['Image'] == OLD,
            'running': c['State']['Running'], 'startedAt': c['State']['StartedAt'],
            'restartPolicy': host.get('RestartPolicy'),
            'composeManaged': 'com.docker.compose.project' in labels,
            'swarmManaged': 'com.docker.swarm.service.id' in labels,
            'otherSupervisorsNotChecked': True,
            'network': host.get('NetworkMode'),
            'mountMetadataOnly': [{'source': item.get('Source'), 'destination': item.get('Destination'),
                'type': item.get('Type'), 'rw': item.get('RW')} for item in c.get('Mounts', [])],
            'launch': launch((cfg.get('Entrypoint') or []) + (cfg.get('Cmd') or [])),
            'environmentPresentNotResolved': bool(cfg.get('Env'))}


def safe_output(path):
    resolved = path.resolve()
    return not any(resolved == root or root in resolved.parents for root in
                   (Path('/data'), Path('/data_mock'), Path('/data_new/validator')))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--role', choices=ROLES, required=True)
    parser.add_argument('--container', default='validator-nitro-1')
    parser.add_argument('--out', type=Path, required=True)
    args = parser.parse_args()
    if not re.fullmatch('[a-zA-Z0-9][a-zA-Z0-9_.-]*', args.container):
        parser.error('Invalid container name')
    if not safe_output(args.out) or args.out.exists() or args.out.is_symlink():
        parser.error('Choose a new report outside protected data directories')
    report = {'checkedAt': datetime.now(timezone.utc).isoformat(), 'role': args.role,
              'expectedAddress': ROLES[args.role], 'containerName': args.container,
              'readyForProduction': False, 'signingTested': False, 'errors': [],
              'limitations': 'Docker metadata and sampled logs only. No config/keystore/database file '
              'reads, container exec/start/stop, image pull, signing or RPC transactions. '
              'Data compatibility, effective config and external supervisors remain unverified.'}
    try:
        c = json.loads(docker('inspect', args.container))[0]
        report['container'] = summarize(c)
        report['currentImage'] = image_report(json.loads(docker('image', 'inspect', c['Image']))[0])
        # Limit logs to this launch, then extract only public identities.
        observed = identities(docker('logs', '--since', c['State']['StartedAt'], '--tail', '10000', args.container))
        report['startupIdentitySamples'] = observed
        report['expectedIdentitySeen'] = any(row['txSender'] == ROLES[args.role]
            and row['actingAsWallet'] == ROLES[args.role] for row in observed) if observed else None
        after = json.loads(docker('inspect', args.container))[0]
        report['containerUnchangedDuringRead'] = all(c[k] == after[k] for k in ('Id', 'Image')) and all(
            c['State'][k] == after['State'][k] for k in ('Running', 'StartedAt'))
    except Exception as exc:
        report['errors'].append({'step': 'running_container', 'errorType': type(exc).__name__})
    try:
        new = json.loads(docker('image', 'inspect', NEW))[0]
        report['testedNewImage'] = dict(image_report(new), localImageFound=True, expectedId=NEW)
    except Exception:
        report['testedNewImage'] = {'localImageFound': None, 'expectedId': NEW,
            'note': 'Exact tested image could not be inspected; may be absent or Docker unavailable. No pull attempted.'}
    report['status'] = 'metadata_collected' if not report['errors'] else 'partial_metadata'
    fd = os.open(args.out, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, 'w') as out:
        json.dump(report, out, indent=2)
        out.write('\n')
    print(json.dumps(report, indent=2))
    print('OUTPUT', args.out.absolute())
    return 0 if not report['errors'] else 1


if __name__ == '__main__':
    raise SystemExit(main())
