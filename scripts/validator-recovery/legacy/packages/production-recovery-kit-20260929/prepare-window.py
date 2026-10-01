#!/usr/bin/env python3
"""Collect a pinned read-only inventory and a non-signable pause review draft.

No Docker, signing, transaction submission, impersonation or node database access.
"""
import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import urllib.request

BASE = Path(__file__).resolve().parent
ROLLUP = '0xa113e2e9620a3bc088a681ebb2c234fdbeb85e21'
SAFE = '0xfbb37c66372f7b40361fbc8c8a235ae92711399d'
EXECUTOR = '0x1333480e92de9511dc9bb01f70901ff3ee94f613'
OLD_ROOT = '0x767c9a47cced7ccc3bf419a7efdd9ffb0f23a5dba42f30f3de64f32e2f82c55f'
NEW_ROOT = '0x121d685e2fdb0e3291592d6b90bd70d503951335d19d96455448eb7a14d17421'
STAKERS = {
    '0x5cda45a9ae0e52f1d5110dc3819f6fb96bade33a',
    '0xd38e969ae2947019e0dec0e46937e6aff651834d',
    '0x65fa6c1d1efc338dfe8f07d5de84b3e7b76cbc95',
}
OWNERS = {
    '0xa0c2aed24f5474b2815b2ff61d0f5a01970217c3',
    '0xc60f0ed09edd696e60574f714cbd7cfec004dd70',
    '0xc63b7a2dacfa3aed4ea158f4f51ffbe020b9de4c',
    '0x09ad976b259d9174f4250f0244873c3bc876e2ce',
}
CODE_HASHES = {
    'admin': '0x8736329b580cfc0c0c39ee6700515e0bc51652afb614640db9e34a5d784933e8',
    'primaryImplementation': '0x8cf117fd02db7f12da04db8ac71d302b4a34dffbc17c629b4f7aa6cd5ffcacc5',
    'secondaryImplementation': '0x8bf14ad1722eceef8f77dfdffb79393a42662fdce14d957db1e323084a8145c1',
}
ZERO = '0x' + '00' * 20


class PreparationError(ValueError):
    pass


def require(value, label):
    if not value:
        raise PreparationError(label)


def safe_output(path):
    resolved = path.resolve()
    for root in ('/data', '/data_mock', '/data_new/validator'):
        protected = Path(root)
        require(resolved != protected and protected not in resolved.parents,
                'Output must be in the scripts/report directory, not a node directory')
    require(not path.exists() and not path.is_symlink(), 'Output already exists')


def rpc(url, method, params):
    require(method in {'eth_chainId', 'eth_call', 'eth_getBlockByNumber',
                       'eth_getCode', 'eth_getStorageAt', 'web3_sha3'}, 'Read-only RPC guard')
    request = urllib.request.Request(url, json.dumps({
        'jsonrpc': '2.0', 'id': 1, 'method': method, 'params': params,
    }).encode(), {'Content-Type': 'application/json'})
    with urllib.request.urlopen(request, timeout=45) as response:
        result = json.load(response)
    require('error' not in result and result.get('result') is not None,
            'RPC read failed: ' + method)
    return result['result']


def encode(signature, *args):
    result = subprocess.run(['cast', 'calldata', signature, *map(str, args)],
                            capture_output=True, text=True, timeout=30)
    require(result.returncode == 0, 'Local calldata encoding failed')
    data = result.stdout.strip()
    require(data.startswith('0x') and len(data) % 2 == 0, 'Invalid encoded calldata')
    bytes.fromhex(data[2:])
    return data


def check_inventory(authority, participants, phase):
    require(authority['rollup'].lower() == ROLLUP and participants['rollup'].lower() == ROLLUP,
            'Unexpected Rollup')
    for key in ('parentBlock', 'parentBlockHash'):
        require(authority[key].lower() == participants[key].lower(), 'Mixed snapshots')
    require(participants['chainId'] == 42161, 'Wrong parent chain')
    require(participants['status'] == 'inventory_complete' and not participants['errors'],
            'Incomplete participant inventory')
    require(participants['wasmModuleRoot'].lower() == OLD_ROOT, 'Root differs from pre-recovery baseline')
    require(participants['paused'] is (phase == 'paused'), 'Unexpected pause state')
    require(int(authority['calls']['paused()'], 16) == int(participants['paused']), 'Pause mismatch')
    require(authority['calls']['wasmModuleRoot()'].lower() == OLD_ROOT, 'Root mismatch')
    require(authority['adminContract']['address'].lower() == EXECUTOR, 'Wrong executor')
    for name, expected in CODE_HASHES.items():
        require(authority['slots'][name]['codeHash'].lower() == expected, 'Code drift: ' + name)
    require(authority['slots']['admin']['address'].lower() == EXECUTOR, 'Wrong proxy admin')
    impl = authority['adminContract']['proxySlots']['implementation']
    require(impl['codeHash'].lower() ==
            '0x0d88feac198ef1b50b99fddf06aa9f6b1050bfe7211d6f04173de9b6d8953bcb',
            'Executor implementation drift')
    role = authority['adminContract']['calls']['hasRole:EXECUTOR_ROLE()']
    require(role['authority'].lower() == SAFE and int(role['raw'], 16) == 1, 'Safe executor role absent')
    for signature, address in (
            ('bridge()', '0x53a7559d1e57e371f3d1e55fea97e9b6748418a3'),
            ('outbox()', '0x47da6c41d03ac0608924e86f61577df558114bd8')):
        require('0x' + authority['calls'][signature][-40:].lower() == address,
                'Bridge/Outbox identity drift')
    safe = participants['safe']
    require(safe['address'].lower() == SAFE and safe['threshold'] == 3 and
            len(safe['owners']) == 4 and {x.lower() for x in safe['owners']} == OWNERS,
            'Safe owners/threshold drift')
    require(safe['codeHash'].lower() ==
            '0xd7d408ebcd99b2b70be43e20253d6d92a8ea8fab29bd3be7f55b10032331fb4c',
            'Safe proxy code drift')
    require(safe['guard'].lower() == ZERO and safe['modules'] == [], 'Safe guard/modules drift')
    require(safe['fallbackHandler'].lower() == '0xfd0732dc9e303f09fcef3a7388ad10a83459ec99',
            'Safe fallback handler drift')
    require(type(safe['nonce']) is int and safe['nonce'] >= 0, 'Invalid Safe nonce')
    active = [s for s in participants['stakers'] if s['isStaked']]
    require(len(active) == 3 and {s['address'].lower() for s in active} == STAKERS,
            'Active staker scope changed')
    for staker in active:
        require(staker['currentChallenge'] == 0 and not staker['requiresChallengeReview'],
                'Active challenge needs review')
        require(staker['whitelisted'] and not staker['isZombie'] and
                int(staker['amountStakedWei']) == 10**12 and int(staker['withdrawableWei']) == 0,
                'Stake/whitelist/refund baseline changed')
    nodes = participants['nodes']
    require(nodes['confirmed'] == authority['nodeNumbers']['latestConfirmed'] and
            nodes['created'] == authority['nodeNumbers']['latestNodeCreated'], 'Node inventory mismatch')
    require(authority['confirmedNode']['number'] == nodes['confirmed'] and
            authority['confirmedNode']['after']['machineStatus'] == 1, 'Confirmed endpoint incomplete')
    require('error' not in authority['localChain'] and authority['localChain']['head'] > 0,
            'Local L3 unavailable')


def write(path, data):
    with path.open('x') as file:
        json.dump(data, file, indent=2)
        file.write('\n')


def collect(script, args):
    # Discard helper console output: transport exceptions can contain authenticated URLs.
    result = subprocess.run([sys.executable, str(BASE / script), *args],
                            stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=1200)
    require(result.returncode == 0, 'Inventory helper failed: ' + script)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--parent-rpc', default=os.environ.get('RECOVERY_PARENT_RPC', 'http://10.1.2.16:8547'))
    parser.add_argument('--node-rpc', default='http://127.0.0.1:8349')
    parser.add_argument('--phase', choices=['before', 'paused'], default='before',
                        help='paused only reads a Rollup already paused by separately authorized governance')
    parser.add_argument('--out', required=True, type=Path)
    args = parser.parse_args()
    safe_output(args.out)
    args.out.mkdir(parents=True, mode=0o700)
    report = {'readyForProduction': False, 'broadcastEnabled': False,
              'checkedAt': datetime.now(timezone.utc).isoformat(), 'phase': args.phase,
              'status': 'collecting', 'productionNumBlocksApproved': False,
              'parentConfirmedAToBProven': False}
    stage = 'collect_inventory'
    try:
        inv, part = args.out / 'authority.json', args.out / 'participants.json'
        collect('probe-recovery-authority.py', ['--parent-rpc', args.parent_rpc, '--node-rpc', args.node_rpc,
                '--authority', SAFE, '--out', str(inv)])
        collect('probe-production-participants.py', ['--inventory', str(inv), '--parent-rpc', args.parent_rpc,
                '--out', str(part)])
        authority, participants = json.loads(inv.read_text()), json.loads(part.read_text())
        stage = 'check_baseline'
        check_inventory(authority, participants, args.phase)
        tag = authority['parentBlock']
        require(int(rpc(args.parent_rpc, 'eth_chainId', []), 16) == 42161, 'Wrong parent chain')
        # Safe proxy code alone does not identify the implementation. Record slot-zero identity.
        raw = rpc(args.parent_rpc, 'eth_getStorageAt', [SAFE, '0x0', tag])
        singleton = '0x' + raw[-40:]
        code = rpc(args.parent_rpc, 'eth_getCode', [singleton, tag])
        require(code != '0x', 'Safe singleton has no code')
        report['safeSingletonForReview'] = {'address': singleton,
            'codeHash': rpc(args.parent_rpc, 'web3_sha3', [code]), 'independentlyVerified': False}
        if args.phase == 'before':
            stage = 'pause_call_review'
            inner = encode('pause()')
            data = encode('executeCall(address,bytes)', ROLLUP, inner)
            result = rpc(args.parent_rpc, 'eth_call', [{'from': SAFE, 'to': EXECUTOR,
                                                       'value': '0x0', 'data': data}, tag])
            write(args.out / 'pause-review.json', {
                'purpose': 'Review only; no signatures, no Safe import package, no broadcast',
                'chainId': 42161, 'safe': SAFE, 'observedSafeNonce': participants['safe']['nonce'],
                'to': EXECUTOR, 'value': '0', 'operation': 0, 'data': data,
                'decoded': {'outer': 'executeCall(address,bytes)', 'target': ROLLUP,
                            'inner': 'pause()', 'innerData': inner},
                'pinnedAt': {'number': tag, 'hash': authority['parentBlockHash']},
                'innerCallSimulationPassed': True, 'innerCallReturn': result,
                'fullSafeExecutionSimulated': False, 'safeTxHashGenerated': False,
                'readyToSign': False, 'readyForProduction': False,
                'limitations': 'eth_call with Safe as from only checks the downstream call at a historical block. '
                    'It does not test signatures or guard hooks, hold state fixed, approve downtime, or stop validators.',
            })
        stage = 'final_anchor_check'
        require(rpc(args.parent_rpc, 'eth_getBlockByNumber', [tag, False])['hash'].lower() ==
                authority['parentBlockHash'].lower(), 'Pinned parent hash changed')
        report.update(status='preparation_inventory_passed', parentBlock=tag,
            parentBlockHash=authority['parentBlockHash'], nodes=participants['nodes'],
            safeNonceObserved=participants['safe']['nonce'], threshold=participants['safe']['threshold'],
            originalStakers=sorted(STAKERS), paused=participants['paused'],
            oldWasmRoot=OLD_ROOT, proposedNewWasmRoot=NEW_ROOT,
            finalRecoveryCalldataGenerated=False,
            remaining=['machine/address/container mapping and exact upgrade/data plan',
                       'Safe singleton identity and full pause transaction review',
                       'maintenance window and separate authorization to stop/pause',
                       'fresh A/B and exact guarded recovery package after pause',
                       'production signer and post-recovery runtime acceptance'])
    except Exception as exc:
        report.update(status='stopped', failedStage=stage, errorType=type(exc).__name__,
                      instruction='Do not stop validators or sign; inspect the collected reports. No chain mutation performed.')
        if isinstance(exc, PreparationError):
            report['reason'] = str(exc)
        # Do not echo raw exception strings: upstream endpoints may include secrets.
    write(args.out / 'summary.json', report)
    hashes = {p.name: hashlib.sha256(p.read_bytes()).hexdigest() for p in args.out.glob('*.json')}
    write(args.out / 'files-sha256.json', hashes)
    print(json.dumps(report, indent=2))
    print('OUTPUT', args.out.resolve())
    return 0 if report['status'] == 'preparation_inventory_passed' else 1


if __name__ == '__main__':
    raise SystemExit(main())
