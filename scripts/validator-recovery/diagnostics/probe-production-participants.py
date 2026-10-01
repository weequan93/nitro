#!/usr/bin/env python3
"""Pinned read-only production staker, pending-node and Safe inventory. No signing."""
import argparse
import json
import os
import subprocess
import urllib.request
from pathlib import Path

SAFE = '0xfbb37c66372f7b40361fbc8c8a235ae92711399d'
EXTRA = ['0x5cda45a9ae0e52f1d5110dc3819f6fb96bade33a',
         '0xd38e969ae2947019e0dec0e46937e6aff651834d']
NODE_FIELDS = ['stateHash', 'challengeHash', 'confirmData', 'prevNum',
 'deadlineBlock', 'noChildConfirmedBeforeBlock', 'stakerCount', 'childStakerCount',
 'firstChildBlock', 'latestChildNumber', 'createdAtBlock', 'nodeHash']


def words(raw):
    data = raw.removeprefix('0x')
    if len(data) % 64:
        raise ValueError('Unexpected ABI length')
    return [data[i:i+64] for i in range(0, len(data), 64)]


def array(raw):
    w = words(raw)
    off = int(w[0], 16) // 32
    n = int(w[off], 16)
    if n > 1000 or off + 1 + n > len(w):
        raise ValueError('Unexpected array ABI')
    return ['0x'+v[-40:] for v in w[off+1:off+1+n]]


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--inventory', type=Path, required=True)
    p.add_argument('--parent-rpc', default=os.environ.get('ARCHIVE_RPC'))
    p.add_argument('--out', type=Path, required=True)
    a = p.parse_args()
    if not a.parent_rpc:
        p.error('Set ARCHIVE_RPC or --parent-rpc')
    if a.out.exists():
        p.error('Output already exists')
    inv = json.loads(a.inventory.read_text())
    tag, rollup = inv['parentBlock'], inv['rollup']
    report = {'parentBlock': tag, 'parentBlockHash': inv['parentBlockHash'],
              'rollup': rollup, 'safe': {'address': SAFE}, 'errors': [],
              'readyForProduction': False}
    def rpc(m, params):
        if m not in ('eth_chainId', 'eth_call', 'eth_getBlockByNumber',
                     'eth_getCode', 'eth_getStorageAt', 'web3_sha3'):
            raise ValueError('Read-only RPC allowlist')
        req = urllib.request.Request(a.parent_rpc, json.dumps(dict(
            jsonrpc='2.0', id=1, method=m, params=params)).encode(),
            {'Content-Type': 'application/json'})
        with urllib.request.urlopen(req, timeout=45) as r:
            obj = json.load(r)
        if 'error' in obj or obj.get('result') is None:
            raise ValueError(f'{m}: {obj.get("error", "null result")}')
        return obj['result']
    def call(to, sig, *args):
        data = subprocess.check_output(['cast', 'calldata', sig, *map(str, args)],
                                       text=True, timeout=20).strip()
        return rpc('eth_call', [{'to': to, 'data': data}, tag])
    def number(sig, *args):
        return int(call(rollup, sig, *args), 16)
    def optional(label, fn):
        try:
            return fn()
        except Exception as exc:
            report['errors'].append({'check': label, 'errorType': type(exc).__name__})
            return None
    if int(rpc('eth_chainId', []), 16) != 42161:
        raise ValueError('Requires real Arbitrum One chain42161, not Anvil')
    head = rpc('eth_getBlockByNumber', [tag, False])
    if head['hash'].lower() != inv['parentBlockHash'].lower():
        raise ValueError('Pinned parent hash differs')
    report['chainId'] = 42161
    for name, obj in inv['slots'].items():
        code = rpc('eth_getCode', [obj['address'], tag])
        if rpc('web3_sha3', [code]).lower() != obj['codeHash'].lower():
            raise ValueError('Bytecode mismatch: '+name)
    confirmed, created = number('latestConfirmed()'), number('latestNodeCreated()')
    first = number('firstUnresolvedNode()')
    report['nodes'] = {'confirmed': confirmed, 'created': created, 'firstUnresolved': first}
    if confirmed != inv['nodeNumbers']['latestConfirmed']:
        raise ValueError('Pinned confirmed node mismatch')
    if created-first > 1000:
        raise ValueError('Pending range exceeds inventory bound')
    node_ids = {confirmed} | set(range(first, created+1))
    report['stakers'] = []
    count = number('stakerCount()')
    if count > 1000:
        raise ValueError('Unexpected staker count')
    addresses = ['0x'+call(rollup, 'getStakerAddress(uint64)', i)[-40:]
                 for i in range(count)]
    for address in dict.fromkeys(addresses + EXTRA):
        w = words(call(rollup, 'getStaker(address)', address))
        if len(w) != 5:
            raise ValueError('Unexpected Staker ABI')
        values = [int(x, 16) for x in w]
        entry = dict(address=address, amountStakedWei=str(values[0]), index=values[1],
                     latestStakedNode=values[2], currentChallenge=values[3],
                     isStaked=bool(values[4]),
                     withdrawableWei=str(number('withdrawableFunds(address)', address)),
                     isZombie=bool(number('isZombie(address)', address)),
                     whitelisted=bool(number('isValidator(address)', address)))
        entry['requiresChallengeReview'] = values[3] != 0
        report['stakers'].append(entry)
        if values[4]:
            node_ids.add(values[2])
    report['nodeStorage'] = {}
    for n in sorted(node_ids):
        w = words(call(rollup, 'getNode(uint64)', n))
        if len(w) != 12:
            raise ValueError('Unexpected Node ABI')
        report['nodeStorage'][str(n)] = {key: ('0x'+v if i in (0,1,2,11) else int(v,16))
                                       for i,(key,v) in enumerate(zip(NODE_FIELDS,w))}
    s = report['safe']
    s['codeHash'] = rpc('web3_sha3', [rpc('eth_getCode', [SAFE, tag])])
    s['owners'] = optional('Safe owners', lambda: array(call(SAFE, 'getOwners()')))
    s['threshold'] = optional('Safe threshold', lambda: int(call(SAFE, 'getThreshold()'),16))
    s['nonce'] = optional('Safe nonce', lambda: int(call(SAFE, 'nonce()'),16))
    s['versionRaw'] = optional('Safe version', lambda: call(SAFE, 'VERSION()'))
    for label, slot in {
        'guard': '0x4a204f620c8c5ccdca3fd54d003badd85ba500436a431f0cbda4f558c93c34c8',
        'fallbackHandler': '0x6c9a6c4a39284e37ed1cf53d337577d14212a4870fb976a4366c693b939918d5'
    }.items():
        s[label] = optional(label, lambda slot=slot: '0x'+rpc('eth_getStorageAt',[SAFE,slot,tag])[-40:])
    def modules():
        cursor = '0x'+'0'*39+'1'
        found = []
        for _ in range(100):
            raw = call(SAFE, 'getModulesPaginated(address,uint256)', cursor, 50)
            page = array(raw)
            nxt = '0x'+words(raw)[1][-40:]
            found.extend(page)
            if int(nxt,16) == 1:
                return found
            if nxt == cursor or not page:
                raise ValueError('Invalid module pagination')
            cursor = nxt
        raise ValueError('Module pagination limit')
    s['modules'] = optional('Safe modules', modules)
    report['paused'] = bool(number('paused()'))
    report['wasmModuleRoot'] = call(rollup, 'wasmModuleRoot()')
    if rpc('eth_getBlockByNumber',[tag,False])['hash'].lower() != head['hash'].lower():
        raise ValueError('Pinned hash changed')
    report['status'] = 'inventory_complete' if not report['errors'] else 'inventory_incomplete'
    report['limitations'] = 'No full withdrawal audit, role-member enumeration, Safe identity/guard audit or production calldata generation.'
    with a.out.open('x') as f:
        json.dump(report, f, indent=2)
    print(json.dumps(report, indent=2))
    print('OUTPUT', a.out)


if __name__ == '__main__':
    main()
