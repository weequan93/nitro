#!/usr/bin/env python3
"""Read public keystore headers and selected Docker launch switches; never unlock/sign."""
import argparse
import datetime
import json
import os
import posixpath
from pathlib import Path
import re
import stat
import subprocess

EXPECTED = '0x5cda45a9ae0e52f1d5110dc3819f6fb96bade33a'
CONFIG = Path('/data_new/validator/config/nodeConfig.json')
KEYS = Path('/data_new/validator/keys')
BOOLS = (
    'node.staker.enable', 'node.staker.only-create-wallet-contract',
    'node.staker.use-smart-contract-wallet', 'node.staker.enable-fast-confirmation',
    'node.staker.parent-chain-wallet.only-create-key',
    'node.block-validator.enable', 'node.block-validator.failure-is-fatal',
)


def require(ok, message):
    if not ok:
        raise RuntimeError(message)


def address(value):
    if not isinstance(value, str):
        return None
    v = value.removeprefix('0x')
    return '0x' + v.lower() if re.fullmatch(r'[0-9a-fA-F]{40}', v) else None


def read_json(path):
    # Reject redirected paths, including symlinked ancestors; never follow a key symlink.
    require(path.resolve() == path, 'Redirected file refused')
    require(Path('/data_new') in path.parents, 'Path outside /data_new refused')
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd) as f:
        s = os.fstat(f.fileno())
        require(stat.S_ISREG(s.st_mode) and s.st_size <= 262144, 'Not a bounded regular file')
        return json.load(f)


def bool_switches(argv):
    result = {key: None for key in BOOLS}
    for token in argv:
        if not isinstance(token, str) or not token.startswith('--'):
            continue
        key, sep, value = token[2:].partition('=')
        if key not in result:
            continue
        # pflag bare boolean flags mean true; a separate following value is not consumed.
        if not sep:
            result[key] = True
        elif value.lower() in ('true', '1', 't'):
            result[key] = True
        elif value.lower() in ('false', '0', 'f'):
            result[key] = False
        else:
            result[key] = 'unrecognized_value'
    return result


def keystore_header(obj):
    if not isinstance(obj, dict):
        return None
    public = address(obj.get('address'))
    crypto = obj.get('crypto', obj.get('Crypto'))
    if obj.get('version') != 3 or not public or not isinstance(crypto, dict):
        return None
    return {'declaredAddress': public, 'matchesExpected': public == EXPECTED,
            'version': 3, 'encryptedPayloadPresent': bool(crypto.get('ciphertext')),
            'decryptedAddressVerified': False}


def collect():
    require(KEYS.resolve() == KEYS and KEYS.is_dir(), 'Expected keys directory missing or redirected')
    config = read_json(CONFIG)
    staker = config.get('node', {}).get('staker', {})
    wallet = staker.get('parent-chain-wallet', {})
    require(isinstance(wallet, dict), 'Unexpected wallet configuration type')
    proc = subprocess.run(['docker', 'inspect', 'validator-debug-nitro-1'],
                          capture_output=True, text=True, timeout=30)
    require(proc.returncode == 0, 'Docker inspection failed')
    container = json.loads(proc.stdout)[0]
    docker_config = container.get('Config') or {}
    argv = (docker_config.get('Entrypoint') or []) + (docker_config.get('Cmd') or [])
    key_mounts = [m for m in container.get('Mounts', []) if m.get('Destination') == '/keys']
    pathname = wallet.get('pathname')
    if isinstance(pathname, str) and pathname:
        pathname = posixpath.normpath(pathname)
    report = {
        'checkedAt': datetime.datetime.now(datetime.timezone.utc).isoformat(),
        'readyForProduction': False, 'expectedStaker': EXPECTED,
        'container': 'validator-debug-nitro-1',
        'containerRunning': container['State']['Running'],
        'imageId': container['Image'],
        'launchBooleanSwitches': bool_switches(argv),
        'keysMountMatchesScannedDirectory': len(key_mounts) == 1 and key_mounts[0].get('Source') == str(KEYS),
        'configuredWalletPathUsesKeysMount': (pathname == '/keys' or pathname.startswith('/keys/'))
            if isinstance(pathname, str) and pathname else None,
        'configuredAccount': address(wallet.get('account')),
        'walletPathOverriddenOnCLI': any(isinstance(x, str) and
            x.split('=', 1)[0] == '--node.staker.parent-chain-wallet.pathname' for x in argv),
        'walletAccountOverriddenOnCLI': any(isinstance(x, str) and
            x.split('=', 1)[0] == '--node.staker.parent-chain-wallet.account' for x in argv),
        'environmentPresentNotResolved': bool(docker_config.get('Env')),
        'keystores': [], 'unreadOrUnrecognizedFiles': 0,
        'scanComplete': True, 'signingTested': False,
        'limitations': 'Keystore address headers are declarations, not cryptographic signer verification. '
        'No password test, unlock, signature, transaction or container mutation. '
        'Entry script/environment/defaults not resolved. No /data file access.',
    }
    count = 0
    def walk_error(_):
        report['scanComplete'] = False
    for directory, dirs, files in os.walk(KEYS, followlinks=False, onerror=walk_error):
        base = Path(directory)
        depth = len(base.relative_to(KEYS).parts)
        kept = [d for d in dirs if not (base / d).is_symlink() and depth < 2]
        if len(kept) != len(dirs):
            report['scanComplete'] = False
        dirs[:] = kept
        for name in sorted(files):
            count += 1
            if count > 200:
                report['scanComplete'] = False
                break
            try:
                header = keystore_header(read_json(base / name))
                if header is None:
                    raise ValueError('Not a V3 keystore')
                report['keystores'].append(header)
            except Exception:
                report['unreadOrUnrecognizedFiles'] += 1
        if count > 200:
            break
    matches = sum(k['matchesExpected'] for k in report['keystores'])
    report['expectedAddressHeaderCount'] = matches
    report['status'] = 'expected_address_header_found_unlock_unverified' if matches else 'expected_address_not_found_in_readable_headers'
    return report


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--out', required=True, type=Path)
    a = p.parse_args()
    out = a.out.absolute()
    require(out.parent == Path('/data_new/scripts') and out.parent.resolve() == out.parent,
            'Output must be directly under /data_new/scripts without redirection')
    require(not out.exists() and not out.is_symlink(), 'Output already exists')
    try:
        result = collect()
    except Exception as exc:
        # Exception text may contain secret input; emit only its type.
        result = {'status': 'inspection_incomplete', 'errorType': type(exc).__name__,
                  'readyForProduction': False, 'signingTested': False}
    fd = os.open(out, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, 'w') as f:
        json.dump(result, f, indent=2)
    print(json.dumps(result, indent=2))
    print('OUTPUT', out)


if __name__ == '__main__':
    main()
