#!/usr/bin/env python3
"""Prepare and gate a keyless Watchtower handover. Never enables MakeNodes."""
import argparse
import copy
import fcntl
import json
import os
from pathlib import Path
import socket
import subprocess
import sys
from urllib.parse import urlsplit

from core import (NEW, ROLLUP, RPC, b32, filehash, load_package, now,
                  position, read, require, save, state)
from collect import NodeRPC
from recovery import safe_receipt, recovery_post, created_event

BASE = Path('/data_new/scripts')
SOURCE = BASE / 'snapshot-sync.json'
DATA = Path('/data_mock/validator/config')
IMAGE = 'sha256:b3571c0d8d4cd4e826a48895ed558927c4b003ea14050f5be3fb5ab09c62148f'
PARENT = 'http://10.1.2.16:8547'
NODE = 'http://127.0.0.1:8349'
SNAPSHOT = 'snapshot-sync'
PROTECTED = 'validator-nitro-1'
TARGET = 'validator-recovered-watchtower'
ACCOUNT = '0x65fa6c1d1efc338dfe8f07d5de84b3e7b76cbc95'


def run(*args, timeout=60):
    r = subprocess.run(args, capture_output=True, text=True, timeout=timeout)
    require(r.returncode == 0, 'Command failed: ' + ' '.join(args[:2]))
    return r.stdout.strip()


def plain_path(path):
    path = Path(path)
    require(path.is_absolute() and '..' not in path.parts, 'Absolute normalized path required')
    require(not any(p.is_symlink() for p in (path, *path.parents)), 'Symlink path rejected')
    return path


def report_dir(path):
    path = plain_path(path)
    require(path.parent == BASE, 'Output must be a new immediate child of /data_new/scripts')
    path.mkdir(mode=0o755)
    os.chmod(path, 0o755)  # Nitro must traverse this directory; its config stays 0600.
    return path


def inspect(name):
    return json.loads(run('docker', 'inspect', name))[0]


def identity(c):
    return dict(id=c['Id'], image=c['Image'], running=c['State']['Running'],
                paused=c['State'].get('Paused', False), startedAt=c['State']['StartedAt'])


def overlapping(a, b):
    a, b = Path(a), Path(b)
    return a == b or a in b.parents or b in a.parents


def source_container(c):
    require(c['Name'] == '/' + SNAPSHOT and c['Image'] == IMAGE, 'Unexpected snapshot identity/image')
    require(c['State']['Running'] and not c['State'].get('Paused'), 'Snapshot must be running')
    mounts = {(m['Source'], m['Destination'], m['RW'], m['Type']) for m in c['Mounts']}
    require(mounts == {(str(DATA), '/home/user/.arbitrum', True, 'bind'),
                       (str(SOURCE), '/snapshot-sync.json', False, 'bind')}, 'Unexpected snapshot mounts')
    require(c['Config'].get('StopSignal', '') in ('', 'SIGTERM', 'SIGINT', '15', '2'), 'Unexpected stop signal')
    require(c['HostConfig']['NetworkMode'] == 'host', 'Expected host network')


def exclusive(containers, allowed):
    for c in containers:
        if not c['State']['Running'] or c['Id'] == allowed:
            continue
        for m in c.get('Mounts', []):
            if m.get('Source'):
                # Do not resolve or open other containers' paths (including /data).
                require(not overlapping(m['Source'], DATA),
                        'Another running container mounts the snapshot data: ' + c['Name'])


def containers():
    ids = run('docker', 'ps', '-aq').split()
    return json.loads(run('docker', 'inspect', *ids)) if ids else []


def no_old_nitro(c):
    if c['State']['Running']:
        names = run('docker', 'top', PROTECTED, '-eo', 'comm').splitlines()[1:]
        require(not any(Path(x.strip()).name.startswith('nitro') for x in names),
                'Old production container still has a Nitro process; coordinate its stop first')


def base_watchtower_config(src):
    require(src['parent-chain']['id'] == 42161 and
            src['parent-chain']['connection']['url'] == PARENT, 'Wrong source parent')
    n = src['node']
    require(n['staker']['enable'] is False, 'Source staking must be disabled')
    require(n['batch-poster']['enable'] is False and n['delayed-sequencer']['enable'] is False
            and src['execution']['sequencer']['enable'] is False, 'Source must not sequence/post')
    bv = n['block-validator']
    require(bv.get('current-module-root') == NEW and bv.get('enable') is True, 'Wrong source validation root')
    dangerous = bv.get('dangerous', {})
    require(not dangerous or all(v in (False, 0, '', None, {}) for v in dangerous.values()),
            'Source has reset/revalidation controls; review separately')
    info = json.loads(src['chain']['info-json'])
    require(len(info) == 1 and info[0].get('chain-config', {}).get('chainId') == 2886
            and info[0].get('parent-chain-id') == 42161
            and info[0].get('chain-name') == 'Deriw Chain'
            and info[0].get('rollup', {}).get('rollup', '').lower() == ROLLUP, 'Wrong chain metadata')
    info[0]['parent-chain-is-arbitrum'] = True
    for k in ('sequencer-url', 'secondary-forwarding-target', 'feed-url', 'secondary-feed-url'):
        info[0][k] = ''
    # Construct the runtime config, rather than inheriting wallets, conf loaders or signing settings.
    c = dict(chain={'id': 2886, 'info-json': json.dumps(info)},
             **{'parent-chain': {'id': 42161, 'connection': {'url': PARENT}}})
    c['persistent'] = {'global-config': '/home/user/.arbitrum',
                       'chain': '/home/user/.arbitrum/Deriw Chain'}
    validation = {'enable': True, 'failure-is-fatal': True,
                  'current-module-root': NEW, 'pending-upgrade-module-root': ''}
    for key in ('prerecorded-blocks', 'validation-sent-limit', 'forward-blocks'):
        value = bv.get(key)
        require(type(value) is int and 0 < value <= 4096, 'Review source validation limit: ' + key)
        validation[key] = value
    c['node'] = {'sequencer': False, 'batch-poster': {'enable': False},
                 'delayed-sequencer': {'enable': False}, 'parent-chain-reader': {'enable': True},
                 'feed': {'input': {'url': [], 'secondary-url': []}, 'output': {'enable': False}},
                 'block-validator': validation,
                 'staker': {'enable': True, 'strategy': 'Watchtower',
                            'start-validation-from-staked': True,
                            'enable-fast-confirmation': False, 'use-smart-contract-wallet': False,
                            'only-create-wallet-contract': False,
                            'dangerous': {'without-block-validator': False,
                                          'ignore-rollup-wasm-module-root': False}}}
    c['execution'] = {'sequencer': {'enable': False}, 'forwarding-target': 'null',
                      'secondary-forwarding-target': [], 'caching': {'archive': True},
                      'recording-database': {'legacy-fee-account-preimages': True}}
    c['http'] = {'addr': '127.0.0.1', 'port': 8349, 'vhosts': ['localhost', '127.0.0.1'],
                 'corsdomain': [], 'api': ['eth', 'net', 'web3', 'arb', 'arbdebug']}
    c['ws'] = {'addr': '127.0.0.1', 'port': 8372, 'rpcprefix': '/ws'}
    c['metrics'] = False
    c['pprof'] = False
    c['file-logging'] = {'enable': False}
    c['validation'] = {'wasm': {'root-path': '/home/user/target/machines'}}
    return c


def anytrust_reader(src):
    n = src['node']
    legacy = n.get('data-availability', {})
    modern = n.get('da', {}).get('anytrust', {})
    require(not (legacy and modern) or legacy == modern, 'Conflicting legacy/new DA config; review required')
    original = modern or legacy
    require(isinstance(original, dict) and original, 'Source AnyTrust reader config missing')
    require(original.get('enable') is not False, 'Source AnyTrust is explicitly disabled')
    require(not original.get('rpc-aggregator', {}).get('enable'), 'Writer DA config rejected')
    require(not original.get('disable-signature-checking'), 'DA signature verification must remain enabled')
    rest = original.get('rest-aggregator', {})
    require(rest.get('enable') is True, 'Source REST aggregator must be enabled')
    require(not rest.get('sync-to-storage', {}).get('enable'), 'DA storage sync requires separate review')
    urls = rest.get('urls', [])
    online = rest.get('online-url-list', '')
    require(isinstance(urls, list) and isinstance(online, str) and (urls or online), 'DA reader endpoints missing')
    for url in urls + ([online] if online else []):
        require(isinstance(url, str) and urlsplit(url).scheme in ('http', 'https')
                and urlsplit(url).hostname, 'Invalid DA reader URL')
    allowed = {'enable', 'urls', 'online-url-list', 'online-url-list-fetch-interval',
               'strategy', 'strategy-update-interval', 'wait-before-try-next',
               'max-per-endpoint-stats', 'simple-explore-exploit-strategy', 'connection-wait'}
    require(not (set(rest) - allowed - {'sync-to-storage'}), 'Unknown DA reader setting')
    result = {'enable': True, 'rest-aggregator': {k: copy.deepcopy(v) for k, v in rest.items() if k in allowed},
              'rpc-aggregator': {'enable': False}, 'disable-signature-checking': False}
    for key in ('request-timeout', 'max-batch-size', 'panic-on-error'):
        if key in original:
            result[key] = copy.deepcopy(original[key])
    return result


def watchtower_config(src):
    c = base_watchtower_config(src)
    c['node']['da'] = {'anytrust': anytrust_reader(src)}
    return c


def package(path):
    plain_path(path)
    p, ident = load_package(path)
    require(p['kind'] == 'recovery' and p['purpose'] == 'production-review', 'Production recovery package required')
    return p, ident


def local_checkpoint(node, p):
    require(int(node.rpc('eth_chainId', []), 16) == 2886, 'Wrong L3 chain')
    h = node.rpc('eth_getBlockByNumber', [hex(p['checkpointBlock']), False])
    require(h and h['hash'].lower() == p['after']['BlockHash'] and
            h['sendRoot'].lower() == p['after']['SendRoot'], 'Local B header differs from recovery package')


def recovery_gate(r, p, transaction):
    require(int(r.rpc('eth_chainId', []), 16) == 42161, 'Wrong parent chain')
    rec = safe_receipt(r, p, b32(transaction))
    recovery_post(r, p, rec['blockNumber'])
    created_event(r, p, rec)
    live = r.rpc('eth_getBlockByNumber', ['latest', False])
    recovery_post(r, p, live['number'])
    return dict(recoveryTransaction=transaction, receiptBlock=rec['blockNumber'],
                receiptBlockHash=rec['blockHash'], parentBlock=live['number'], parentBlockHash=live['hash'])


def prepare(a):
    _, pid = package(a.package)
    plain_path(SOURCE)
    c = read(SOURCE)
    config = watchtower_config(c)
    snap, old = inspect(SNAPSHOT), inspect(PROTECTED)
    source_container(snap)
    image = json.loads(run('docker', 'image', 'inspect', IMAGE))[0]
    require(image['Id'] == IMAGE, 'Prepared image missing')
    # Read mount metadata only, never the old validator config or keystore.
    old_mounts = [dict(source=m.get('Source'), destination=m['Destination'], rw=m['RW'])
                  for m in old['Mounts']]
    d = report_dir(a.out)
    save(d/'watchtower.json', config)
    report = dict(status='cutover_prepared_not_started', checkedAt=now(), packageDirectory=str(a.package),
                  recoveryPackageIdentity=pid, sourceConfigSha256=filehash(SOURCE),
                  watchtowerConfigSha256=filehash(d/'watchtower.json'),
                  snapshot=identity(snap), protectedAtPreparation=identity(old),
                  oldValidatorMounts=old_mounts, targetContainer=TARGET, image=IMAGE, data=str(DATA),
                  startValidationFromStaked=True, strategy='Watchtower', signingEnabled=False,
                  productionTransactionsSent=False, readyForProduction=False,
                  remaining=['Actual recovery receipt and paused new-root state',
                             'Watchtower launch and new-root progress beyond B',
                             'Operator keystore mount and password method for MakeNodes'])
    save(d/'plan.json', report)
    # Deliberately NOT a runnable Nitro configuration; no secret placeholders in a launch command.
    save(d/'makenodes-requirements.json', dict(account=ACCOUNT, desiredStrategy='MakeNodes',
         startValidationFromStaked=True, fastConfirmation=False, keystoreMountVerified=False,
         passwordMechanismVerified=False, signingTested=False, runnable=False))
    print(json.dumps(report, indent=2))
    print('OUTPUT', d)


def checked_plan(path):
    d = plain_path(path)
    require(d.parent == BASE, 'Plan must be under /data_new/scripts')
    plan = read(d/'plan.json')
    p, pid = package(Path(plan['packageDirectory']))
    require(pid == plan['recoveryPackageIdentity'], 'Recovery package changed')
    plain_path(SOURCE)
    require(filehash(SOURCE) == plan['sourceConfigSha256'], 'Source config changed; prepare a new plan')
    plain_path(d/'watchtower.json')
    require(read(d/'watchtower.json') == watchtower_config(read(SOURCE)) and
            filehash(d/'watchtower.json') == plan['watchtowerConfigSha256'], 'Prepared config changed')
    return d, plan, p


def launch_args(d):
    return ['docker', 'run', '-d', '--name', TARGET, '--restart=no', '--network=host',
            '--log-opt', 'max-size=100m', '--log-opt', 'max-file=3',
            '--mount', 'type=bind,src=' + str(DATA) + ',dst=/home/user/.arbitrum',
            '--mount', 'type=bind,src=' + str(d/'watchtower.json') + ',dst=/cutover.json,readonly',
            '--entrypoint', '/usr/local/bin/split-val-entry.sh', IMAGE,
            '--conf.file=/cutover.json', '--conf.env-prefix=']


def repair_da(a):
    """Repair only the first-version missing-DA launch; retain the bind-mounted inode."""
    d = plain_path(a.plan)
    require(d.parent == BASE, 'Plan must be under /data_new/scripts')
    for name in ('plan.json', 'launch.json', 'watchtower.json'):
        plain_path(d/name)
    plan, launch = read(d/'plan.json'), read(d/'launch.json')
    p, pid = package(Path(plan['packageDirectory']))
    require(pid == plan['recoveryPackageIdentity'] == launch['recoveryPackageIdentity'], 'Wrong recovery package')
    plain_path(SOURCE)
    require(filehash(SOURCE) == plan['sourceConfigSha256'], 'Source config changed; review required')
    src = read(SOURCE)
    before = read(d/'watchtower.json')
    require(before == base_watchtower_config(src) and
            filehash(d/'watchtower.json') == plan['watchtowerConfigSha256'], 'Not the original missing-DA config')
    after = watchtower_config(src)
    c, snap, old = inspect(TARGET), inspect(SNAPSHOT), inspect(PROTECTED)
    require(c['Id'] == launch.get('containerId') and c['Image'] == IMAGE and
            not c['State']['Running'] and not c['State'].get('Restarting'), 'Expected stopped owned Watchtower')
    require(snap['Id'] == plan['snapshot']['id'] and not snap['State']['Running'], 'Snapshot must remain stopped')
    require(c['Config']['Entrypoint'] == ['/usr/local/bin/split-val-entry.sh'] and
            c['Config']['Cmd'] == ['--conf.file=/cutover.json', '--conf.env-prefix='] and
            c['HostConfig']['NetworkMode'] == 'host' and
            c['HostConfig']['RestartPolicy']['Name'] == 'no', 'Unexpected Watchtower runtime settings')
    mounts = {(m['Source'], m['Destination'], m['RW'], m['Type']) for m in c['Mounts']}
    require(mounts == {(str(DATA), '/home/user/.arbitrum', True, 'bind'),
                       (str(d/'watchtower.json'), '/cutover.json', False, 'bind')}, 'Unexpected Watchtower mounts')
    logs = run('docker', 'logs', '--tail', '1000', TARGET)
    # Docker logs may arrive on stderr; collect both streams for this exact error check.
    if 'rest-aggregator.enable must be set for reader mode' not in logs:
        proc = subprocess.run(['docker', 'logs', '--tail', '1000', TARGET],
                              capture_output=True, text=True, timeout=60, check=True)
        logs = proc.stdout + proc.stderr
    require('rest-aggregator.enable must be set for reader mode' in logs, 'Expected DA startup error not found')
    plain_path(DATA)
    require(run('findmnt', '-n', '-o', 'TARGET', '-T', str(DATA)) == '/data_mock', 'Unexpected data mount')
    no_old_nitro(old)
    exclusive(containers(), None)
    report = dict(status='da_repair_plan', checkedAt=now(), container=TARGET, containerId=c['Id'],
                  configChange='Add original snapshot AnyTrust REST reader under node.da.anytrust',
                  sourceConfigUnchanged=True, productionTransactionsSent=False,
                  readyForProduction=False, protectedBefore=identity(old))
    if not a.apply:
        print(json.dumps(report, indent=2))
        return
    gate = recovery_gate(RPC(PARENT), p, launch['recoveryTransaction'])
    for host, port in [('127.0.0.1', 8349), ('127.0.0.1', 8372),
                       ('127.0.0.10', 52000), ('127.0.0.10', 52001)]:
        with socket.socket() as sock:
            sock.bind((host, port))
    out = report_dir(BASE/('watchtower-da-repair-' + now().replace(':', '').replace('+', '-')))
    for name in ('plan.json', 'launch.json', 'watchtower.json'):
        (out/(name+'.before')).write_bytes((d/name).read_bytes())
        os.chmod(out/(name+'.before'), 0o600)
    report.update(status='repair_backed_up', recoveryCheck=gate, backupDirectory=str(out))
    save(out/'summary.json', report)
    try:
        with (d/'watchtower.json').open('r+') as f:
            fcntl.flock(f, fcntl.LOCK_EX | fcntl.LOCK_NB)
            require(json.load(f) == before, 'Config changed during repair')
            f.seek(0); json.dump(after, f, indent=2); f.write('\n'); f.truncate(); f.flush(); os.fsync(f.fileno())
        plan['watchtowerConfigSha256'] = filehash(d/'watchtower.json')
        plan['daReaderRestoredFromSource'] = True
        save(d/'plan.json', plan)
        report['status'] = 'da_config_repaired'
        save(out/'summary.json', report)
        require(not inspect(SNAPSHOT)['State']['Running'] and not inspect(TARGET)['State']['Running'],
                'Container state changed during repair')
        require(identity(inspect(PROTECTED)) == identity(old), 'Protected container changed')
        exclusive(containers(), None)
        run('docker', 'start', TARGET)
        started = inspect(TARGET)
        require(started['Id'] == c['Id'], 'Watchtower identity changed')
        launch.update(status='watchtower_started_progress_pending', startedAt=started['State']['StartedAt'],
                      daRepairDirectory=str(out))
        save(d/'launch.json', launch)
        report.update(status='watchtower_restarted_progress_pending', startedAt=started['State']['StartedAt'])
    except Exception:
        report.update(failedAfterStage=report['status'], status='da_repair_stopped_for_review')
        raise
    finally:
        report['protectedUnchanged'] = identity(inspect(PROTECTED)) == identity(old)
        report['snapshotAutoRestarted'] = False
        save(out/'summary.json', report)
    print(json.dumps(report, indent=2))
    print('OUTPUT', out)


def start(a):
    d, plan, p = checked_plan(a.plan)
    require(not (d/'launch.json').exists(), 'Launch report exists; inspect it before any retry')
    plain_path(DATA)
    require(plain_path(DATA/'Deriw Chain'/'nitro').is_dir(), 'Snapshot database directory missing')
    require(run('findmnt', '-n', '-o', 'TARGET', '-T', str(DATA)) == '/data_mock', 'Unexpected snapshot mount')
    snap, old = inspect(SNAPSHOT), inspect(PROTECTED)
    source_container(snap)
    require(identity(snap) == plan['snapshot'], 'Snapshot changed since preparation; prepare a new plan')
    no_old_nitro(old)
    all_c = containers()
    require(not any(c['Name'] == '/' + TARGET for c in all_c), 'Target container already exists')
    exclusive(all_c, snap['Id'])
    r = RPC(PARENT)
    gate = recovery_gate(r, p, a.recovery_tx)
    local_checkpoint(NodeRPC(NODE), p)
    report = dict(status='prechecks_passed', checkedAt=now(), recoveryPackageIdentity=plan['recoveryPackageIdentity'],
                  **gate, protectedBefore=identity(old), readyForProduction=False)
    # Preserve config owner so the image's non-root Nitro can read it; no database file reads.
    os.chown(d/'watchtower.json', SOURCE.stat().st_uid, SOURCE.stat().st_gid)
    os.chmod(d/'watchtower.json', 0o600)
    save(d/'launch.json', report)
    try:
        print('Gracefully stopping only snapshot-sync; no forced-kill timeout.', flush=True)
        run('docker', 'stop', '-t', '-1', SNAPSHOT, timeout=None)
        report['status'] = 'snapshot_stopped'
        save(d/'launch.json', report)
        require(not inspect(SNAPSHOT)['State']['Running'], 'Snapshot still running')
        require(identity(inspect(PROTECTED)) == identity(old), 'Protected container changed')
        no_old_nitro(inspect(PROTECTED))
        exclusive(containers(), None)
        for host, port in [('127.0.0.1', 8349), ('127.0.0.1', 8372),
                           ('127.0.0.10', 52000), ('127.0.0.10', 52001)]:
            with socket.socket() as sock:
                sock.bind((host, port))
        recovery_gate(r, p, a.recovery_tx)
        require(filehash(SOURCE) == plan['sourceConfigSha256'], 'Source config changed during handover')
        report['containerId'] = run(*launch_args(d))
        report['status'] = 'watchtower_started_progress_pending'
        report['container'] = TARGET
        report['startedAt'] = inspect(TARGET)['State']['StartedAt']
    except Exception:
        report['failedAfterStage'] = report['status']
        report['status'] = 'handover_stopped_for_review'
        raise
    finally:
        report['protectedAfter'] = identity(inspect(PROTECTED))
        report['protectedUnchanged'] = report['protectedBefore'] == report['protectedAfter']
        report['snapshotAutoRestarted'] = False
        save(d/'launch.json', report)
    print(json.dumps(report, indent=2))


def sample(node, p):
    local_checkpoint(node, p)
    val = node.rpc('arb_latestValidated', [])
    gs = state(val['GlobalState'])
    require([v.lower() for v in val['WasmRoots']] == [NEW], 'Latest validation not exclusively under new root')
    require(position(gs) > position(p['after']), 'Validation has not advanced beyond trusted B yet')
    h = node.rpc('eth_getBlockByHash', [gs['BlockHash'], False])
    require(h and h['sendRoot'].lower() == gs['SendRoot'], 'Validated header unavailable/mismatched')
    canonical = node.rpc('eth_getBlockByNumber', [h['number'], False])
    require(canonical and canonical['hash'].lower() == gs['BlockHash'], 'Validated block not canonical locally')
    return dict(validated=val, validatedBlock=int(h['number'], 16))


def check(a):
    d, plan, p = checked_plan(a.plan)
    launch = read(d/'launch.json')
    require(launch['status'] == 'watchtower_started_progress_pending', 'Successful launch report required')
    c = inspect(TARGET)
    require(c['Id'] == launch['containerId'] and c['State']['Running'] and
            c['State']['StartedAt'] == launch['startedAt'], 'Watchtower stopped/restarted')
    require(not inspect(SNAPSHOT)['State']['Running'], 'Snapshot also running')
    gate = recovery_gate(RPC(PARENT), p, launch['recoveryTransaction'])
    result = sample(NodeRPC(NODE), p)
    out = report_dir(a.out)
    report = dict(status='watchtower_progress_observed', checkedAt=now(), **gate, **result,
                  recoveryPackageIdentity=plan['recoveryPackageIdentity'], containerId=c['Id'],
                  readyForProduction=False, signingTested=False,
                  note='One new-root observation beyond trusted B. Not parent A-to-B proof or production signer acceptance.')
    save(out/'summary.json', report)
    print(json.dumps(report, indent=2))


def main():
    os.umask(0o077)
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest='command', required=True)
    prep = sub.add_parser('prepare', help='No RPC, container change, DB read or keystore access')
    prep.add_argument('--package', required=True, type=Path)
    prep.add_argument('--out', required=True, type=Path)
    prep.set_defaults(fn=prepare)
    launch = sub.add_parser('start-watchtower', help='After actual recovery execution only; gracefully hands over /data_mock')
    launch.add_argument('--plan', required=True, type=Path)
    launch.add_argument('--recovery-tx', required=True)
    launch.set_defaults(fn=start)
    checkp = sub.add_parser('check', help='Read-only progress observation after launch')
    checkp.add_argument('--plan', required=True, type=Path)
    checkp.add_argument('--out', required=True, type=Path)
    checkp.set_defaults(fn=check)
    repair = sub.add_parser('repair-da', help='Repair only a stopped Watchtower with the specific missing-DA error')
    repair.add_argument('--plan', required=True, type=Path)
    repair.add_argument('--apply', action='store_true')
    repair.set_defaults(fn=repair_da)
    a = parser.parse_args()
    if a.command in ('start-watchtower', 'repair-da'):
        lock = plain_path(BASE/'snapshot-cutover.lock')
        with lock.open('a') as f:
            fcntl.flock(f, fcntl.LOCK_EX | fcntl.LOCK_NB)
            a.fn(a)
    else:
        a.fn(a)


if __name__ == '__main__':
    try:
        main()
    except Exception as e:
        print('STOP:', str(e) if isinstance(e, ValueError) else type(e).__name__, file=sys.stderr)
        raise SystemExit(1)
