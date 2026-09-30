#!/usr/bin/env python3
"""Prepare Compose/config and copy one encrypted keystore. Never starts a validator."""
import argparse
import copy
import fcntl
import hashlib
import json
import os
from pathlib import Path
import stat
import tempfile

import cutover as c

DEST = Path('/data_new/validator')
KEY_SOURCE = Path('/data/validator/keys')  # The only authorized /data content read.
CONFIG = DEST/'config/nodeconfig.json'
COMPOSE = DEST/'compose.recovered.json'
KEY_DIR = DEST/'keys/recovered-65fa'
KEY_FILE = KEY_DIR/'account.json'
NAME = 'validator-recovered-makenodes'


def digest(blob):
    return hashlib.sha256(blob).hexdigest()


def blob(path):
    c.plain_path(path)
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(fd, 'rb') as f:
        s = os.fstat(f.fileno())
        c.require(stat.S_ISREG(s.st_mode) and s.st_nlink == 1 and s.st_size <= 2**20,
                  'Expected a small regular file with no hard links: ' + str(path))
        return f.read(2**20 + 1)


def selected_keystore(directory):
    c.plain_path(directory)
    matches = []
    for path in sorted(directory.iterdir()):
        c.require(not path.is_symlink(), 'Symlink inside authorized keys directory rejected')
        if not path.is_file():
            continue
        raw = blob(path)
        try:
            key = json.loads(raw)
        except (ValueError, UnicodeError):
            continue
        if not isinstance(key, dict) or str(key.get('address', '')).lower().removeprefix('0x') != c.ACCOUNT[2:]:
            continue
        crypto = key.get('crypto', key.get('Crypto'))
        c.require(key.get('version') == 3 and isinstance(crypto, dict) and
                  all(isinstance(crypto.get(k), str) and crypto[k] for k in ('cipher', 'ciphertext', 'kdf', 'mac')),
                  'Expected encrypted V3 keystore for the selected account')
        c.require('private-key' not in key and 'privateKey' not in key, 'Unexpected plaintext key field')
        matches.append(raw)
    c.require(len(matches) == 1, 'Expected exactly one matching encrypted keystore; found ' + str(len(matches)))
    return matches[0]


def makenodes_config(watchtower):
    result = copy.deepcopy(watchtower)
    s = result['node']['staker']
    c.require(s['strategy'] == 'Watchtower' and s['enable'] is True and
              s['start-validation-from-staked'] is True and 'parent-chain-wallet' not in s,
              'Expected reviewed keyless Watchtower configuration')
    s['strategy'] = 'MakeNodes'
    s['parent-chain-wallet'] = {'pathname': '/keys', 'account': c.ACCOUNT, 'only-create-key': False}
    # No password: Nitro's PASSWORD_NOT_SET default prompts on terminal stdin.
    return result


def compose_config():
    return {'name': 'deriw-recovered', 'services': {'validator': {
        'profiles': ['manual-start'], 'container_name': NAME, 'image': c.IMAGE,
        'pull_policy': 'never', 'network_mode': 'host', 'restart': 'no',
        'stdin_open': True, 'tty': True,
        'entrypoint': ['/usr/local/bin/split-val-entry.sh'],
        'command': ['--conf.file=/nodeconfig.json', '--conf.env-prefix='],
        'logging': {'driver': 'json-file', 'options': {'max-size': '100m', 'max-file': '3'}},
        'volumes': [
            {'type': 'bind', 'source': str(c.DATA), 'target': '/home/user/.arbitrum',
             'bind': {'create_host_path': False}},
            {'type': 'bind', 'source': str(CONFIG), 'target': '/nodeconfig.json', 'read_only': True,
             'bind': {'create_host_path': False}},
            {'type': 'bind', 'source': str(KEY_DIR), 'target': '/keys', 'read_only': True,
             'bind': {'create_host_path': False}}]}}}


def encoded(value):
    return (json.dumps(value, indent=2) + '\n').encode()


def existing(path):
    c.plain_path(path)
    return blob(path) if path.exists() else None


def write_atomic(path, content, uid, gid):
    c.plain_path(path)
    fd, temp = tempfile.mkstemp(prefix='.recovery-prep-', dir=path.parent)
    try:
        with os.fdopen(fd, 'wb') as f:
            os.fchmod(f.fileno(), 0o600)
            os.fchown(f.fileno(), uid, gid)
            f.write(content); f.flush(); os.fsync(f.fileno())
        os.replace(temp, path)
    finally:
        if os.path.exists(temp):
            os.unlink(temp)


def check_runtime(d, plan, launch):
    w = c.inspect(c.TARGET)
    c.require(w['Id'] == launch['containerId'] and w['Image'] == c.IMAGE and
              w['State']['Running'] and w['State']['StartedAt'] == launch['startedAt'],
              'Expected the same running recovered Watchtower')
    c.require(w['Config']['Entrypoint'] == ['/usr/local/bin/split-val-entry.sh'] and
              w['Config']['Cmd'] == ['--conf.file=/cutover.json', '--conf.env-prefix='] and
              w['HostConfig']['NetworkMode'] == 'host', 'Watchtower command/network changed')
    mounts = {(m['Source'], m['Destination'], m['RW'], m['Type']) for m in w['Mounts']}
    c.require(mounts == {(str(c.DATA), '/home/user/.arbitrum', True, 'bind'),
                         (str(d/'watchtower.json'), '/cutover.json', False, 'bind')},
              'Watchtower mounts differ')
    c.require(not c.inspect(c.SNAPSHOT)['State']['Running'], 'Snapshot must remain stopped')
    c.no_old_nitro(c.inspect(c.PROTECTED))
    all_c = c.containers()
    c.exclusive(all_c, w['Id'])
    c.require(not any(x['Name'] == '/' + NAME for x in all_c), 'Target container already exists')
    for container in all_c:
        if container['State']['Running']:
            for m in container.get('Mounts', []):
                for target in (CONFIG, COMPOSE, KEY_DIR):
                    c.require(not m.get('Source') or not c.overlapping(m['Source'], target),
                              'A running container uses a preparation target')
    uid = int(c.run('docker', 'exec', c.TARGET, 'id', '-u'))
    gid = int(c.run('docker', 'exec', c.TARGET, 'id', '-g'))
    return w, uid, gid


def prepare(a):
    c.require(os.geteuid() == 0, 'Run on the validator server as root')
    d, plan, _ = c.checked_plan(a.plan)
    c.plain_path(d/'launch.json')
    launch = c.read(d/'launch.json')
    c.require(launch['recoveryPackageIdentity'] == plan['recoveryPackageIdentity'], 'Launch/package mismatch')
    before, uid, gid = check_runtime(d, plan, launch)
    key = selected_keystore(KEY_SOURCE)
    cfg = encoded(makenodes_config(c.read(d/'watchtower.json')))
    compose = encoded(compose_config())
    for path in (DEST, CONFIG, COMPOSE, KEY_DIR, KEY_FILE):
        c.plain_path(path)
    old = {path: existing(path) for path in (CONFIG, COMPOSE, KEY_FILE)}
    c.require(old[COMPOSE] in (None, compose), 'Existing different compose.recovered.json; review it first')
    c.require(old[KEY_FILE] in (None, key), 'Existing different copied key; will not overwrite')
    if KEY_DIR.exists():
        c.require(all(p == KEY_FILE for p in KEY_DIR.iterdir()), 'Unexpected files in dedicated keys directory')
    report = {'status': 'compose_preparation_plan', 'checkedAt': c.now(),
              'account': c.ACCOUNT, 'planDirectory': str(d), 'database': str(c.DATA),
              'config': str(CONFIG), 'compose': str(COMPOSE), 'encryptedKeystore': str(KEY_FILE),
              'configSha256': digest(cfg), 'composeSha256': digest(compose), 'keystoreSha256': digest(key),
              'willBackupExistingConfig': old[CONFIG] is not None and old[CONFIG] != cfg,
              'runtimeUid': uid, 'runtimeGid': gid, 'strategyWhenStarted': 'MakeNodes',
              'recoveryPackageIdentity': plan['recoveryPackageIdentity'],
              'decryptedAddressVerified': False, 'passwordStored': False,
              'databaseCopied': False, 'containersStarted': False, 'productionTransactionsSent': False,
              'readyForProduction': False}
    if not a.apply:
        print(json.dumps(report, indent=2)); return
    out = c.report_dir(a.out)
    os.chmod(out, 0o700)
    c.save(out/'summary.json', report)
    if report['willBackupExistingConfig']:
        write_atomic(out/'nodeconfig.json.before', old[CONFIG], 0, 0)
    # Validate Compose using a temporary file; do not create or start containers.
    write_atomic(out/'compose.review.json', compose, 0, 0)
    c.run('docker', 'compose', '-f', str(out/'compose.review.json'), 'config', '--quiet')
    for path in (DEST, CONFIG.parent, KEY_DIR.parent):
        if not path.exists():
            path.mkdir(mode=0o755, parents=True)
            os.chmod(path, 0o755)
    if not KEY_DIR.exists():
        KEY_DIR.mkdir(mode=0o700)
    # Only this new dedicated key subdirectory changes owner, never /data or database trees.
    os.chown(KEY_DIR, uid, gid); os.chmod(KEY_DIR, 0o700)
    check_runtime(d, plan, launch)
    for path, content, owner, group in ((KEY_FILE, key, uid, gid),
                                        (CONFIG, cfg, uid, gid), (COMPOSE, compose, 0, 0)):
        c.require(existing(path) == old[path], 'Destination changed during preparation')
        write_atomic(path, content, owner, group)
    c.require(c.identity(c.inspect(c.TARGET)) == c.identity(before), 'Watchtower changed during preparation')
    report.update(status='compose_files_prepared_not_started', backupDirectory=str(out),
                  keystoreCopyVerified=blob(KEY_FILE) == key, watchtowerUnchanged=True,
                  remaining=['Manager verifies copied keystore unlock/address locally',
                             'Resume acceptance and fresh chain/validator checks',
                             'Exclusive DB handover; manager enters password for MakeNodes'])
    c.save(out/'summary.json', report)
    print(json.dumps(report, indent=2)); print('OUTPUT', out)


def main():
    os.umask(0o077)
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--plan', required=True, type=Path)
    parser.add_argument('--apply', action='store_true')
    parser.add_argument('--out', type=Path)
    a = parser.parse_args()
    c.require(not a.apply or a.out, '--apply requires a new --out directory')
    with c.plain_path(c.BASE/'snapshot-cutover.lock').open('a') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        prepare(a)


if __name__ == '__main__':
    main()
