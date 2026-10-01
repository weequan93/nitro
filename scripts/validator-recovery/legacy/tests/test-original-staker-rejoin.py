#!/usr/bin/env python3
"""Replay a Nitro-created assertion from a refunded original account on a SECOND Anvil.
No keys, no production writes. Source fork is read-only; target must be fresh recovery.
This tests contract account reuse, NOT the original validator binary/signing setup.
"""
import argparse
import json
import subprocess
import time
import urllib.request
from pathlib import Path

ROLLUP = '0xa113e2e9620a3bc088a681ebb2c234fdbeb85e21'
OLD = '0x5cda45a9ae0e52f1d5110dc3819f6fb96bade33a'
TEST = '0xcf979e4a23adc38af8e23721b9ea250ba22ee81a'
ASSERTION = '(((bytes32[2],uint64[2]),uint8),((bytes32[2],uint64[2]),uint8),uint64)'
NEW = 'newStakeOnNewNode(' + ASSERTION + ',bytes32,uint256)'
CONFIRM = '0x8df0a4fb91686dd55d18aa5eaf4c34720a28802c46d0f5bbc402761502c28fc8'
BH = '0x44cb8e18375564e7453e1b51faa693ee96413fe166ccf3c6b36e3cc0cfa4e230'
SR = '0x71835632ed7edffa198596858626f76fcf82aff5bc69da94b22fd93950d74912'


def require(ok, message):
    if not ok:
        raise RuntimeError(message)


def encode(sig, *args):
    return subprocess.check_output(['cast', 'calldata', sig, *map(str, args)], text=True).strip()


class RPC:
    def __init__(self, port, writable=False):
        self.url = f'http://127.0.0.1:{port}'
        self.writable = writable

    def rpc(self, method, params):
        mutate = method.startswith('anvil_') or method.startswith('eth_send')
        require(not mutate or self.writable, 'Source is read-only')
        if mutate:
            require(self.rpc('eth_chainId', []) == hex(31337), 'Wrong chain')
            require('anvil' in self.rpc('web3_clientVersion', []).lower(), 'Not Anvil')
        req = urllib.request.Request(self.url, json.dumps(dict(jsonrpc='2.0', id=1,
            method=method, params=params)).encode(), {'Content-Type': 'application/json'})
        with urllib.request.urlopen(req, timeout=60) as r:
            obj = json.load(r)
        require('error' not in obj, f'{method}: {obj.get("error")}')
        return obj['result']

    def call(self, sig, *args):
        return self.rpc('eth_call', [{'to': ROLLUP, 'data': encode(sig, *args)}, 'latest'])

    def num(self, sig, *args):
        return int(self.call(sig, *args), 16)

    def node(self, n):
        raw = self.call('getNode(uint64)', n)[2:]
        require(len(raw) == 12 * 64, 'Unexpected Node ABI')
        return [raw[i:i+64] for i in range(0, len(raw), 64)]

    def clock(self):
        return int(self.rpc('eth_call', [{'data': '0x4360005260206000f3'}, 'latest']), 16)

    def advance(self, goal):
        delta = max(0, goal - self.clock())
        require(delta <= 10000, 'Unexpected clock gap')
        if delta:
            self.rpc('anvil_mine', [hex(delta)])
        require(self.clock() >= goal, 'EVM clock did not advance')


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--out', required=True, type=Path)
    a = p.parse_args()
    a.out.mkdir(parents=True, exist_ok=False)
    source, target = RPC(18547), RPC(18548, True)
    def save(name, obj):
        (a.out / name).write_text(json.dumps(obj, indent=2))
    for r in (source, target):
        require(r.rpc('eth_chainId', []) == hex(31337), 'Wrong chain')
        require('anvil' in r.rpc('web3_clientVersion', []).lower(), 'Not Anvil')
    require(source.num('latestConfirmed()') == 42686, 'Unexpected source state')
    require(target.num('latestConfirmed()') == 42685, 'Target must be fresh recovery')
    require(target.num('latestNodeCreated()') == 42685, 'Target has pending nodes')
    require(source.node(42685)[0] == target.node(42685)[0], 'Recovery state mismatch')
    require(source.node(42685)[11] == target.node(42685)[11], 'Recovery node hash mismatch')
    require(source.call('wasmModuleRoot()') == target.call('wasmModuleRoot()'), 'Root mismatch')
    require(target.num('stakeToken()') == 0, 'ETH stakes required')
    require(target.num('isStaked(address)', OLD) == 0, 'Original account still staked')
    require(target.num('isZombie(address)', OLD) == 0, 'Original account is a zombie')
    require(target.num('withdrawableFunds(address)', OLD) == 0, 'Refund not withdrawn')
    require(target.num('isValidator(address)', OLD) == 1, 'Original account not whitelisted')
    require(target.rpc('eth_getCode', [OLD, 'latest']) == '0x', 'Original account is not EOA')
    height = source.call('getNodeCreationBlockForLogLookup(uint64)', 42686)
    block = source.rpc('eth_getBlockByNumber', [hex(int(height, 16)), True])
    selector = subprocess.check_output(['cast', 'sig', NEW], text=True).strip()
    txs = [t for t in block['transactions'] if t['from'].lower() == TEST
           and (t.get('to') or '').lower() == ROLLUP and t['input'].startswith(selector)]
    require(len(txs) == 1, 'Expected one Nitro normal new-stake transaction')
    template = txs[0]
    rc = source.rpc('eth_getTransactionReceipt', [template['hash']])
    require(rc['status'] == '0x1', 'Source creation failed')
    confirmation = source.rpc('eth_getTransactionByHash', [CONFIRM])
    expected = encode('confirmNextNode(bytes32,bytes32)', BH, SR)
    require(confirmation['input'] == expected, 'Source confirmation mismatch')
    require(source.rpc('eth_getTransactionReceipt', [CONFIRM])['status'] == '0x1', 'Source confirm failed')
    save('source-creation.json', template)
    save('source-confirmation.json', confirmation)
    value = int(template['value'], 16)
    require(value >= target.num('currentRequiredStake()') and value <= 10**18, 'Unexpected stake amount')
    target.advance(int(target.node(42685)[10], 16) + target.num('minimumAssertionPeriod()') + 1)
    # This is mock ETH, not a transfer or proof of original account funding.
    target.rpc('anvil_setBalance', [OLD, hex(10**18)])
    target.rpc('anvil_impersonateAccount', [OLD])
    def send(label, data, amount=0):
        tx = {'from': OLD, 'to': ROLLUP, 'data': data, 'value': hex(amount), 'gas': hex(2000000)}
        target.rpc('eth_call', [tx, 'latest'])
        h = target.rpc('eth_sendTransaction', [tx])
        save(label + '-hash.json', {'hash': h})
        for _ in range(60):
            receipt = target.rpc('eth_getTransactionReceipt', [h])
            if receipt:
                save(label + '-receipt.json', receipt)
                require(receipt['status'] == '0x1', label + ' failed')
                print('PASS', label, h, flush=True)
                return
            time.sleep(0.5)
        raise RuntimeError('Receipt timeout; inspect saved hash before retrying')
    try:
        send('original-restake-create', template['input'], value)
        require(target.num('latestStakedNode(address)', OLD) == 42686, 'Restake node mismatch')
        node = target.node(42686)
        require(int(node[3], 16) == 42685, 'Wrong predecessor')
        require(node[0] == source.node(42686)[0] and node[2] == source.node(42686)[2], 'Endpoint mismatch')
        target.advance(max(int(node[4], 16), int(target.node(42685)[5], 16)) + 1)
        send('original-normal-confirm', expected)
        require(target.num('latestConfirmed()') == 42686, 'Not confirmed')
        save('summary.json', {'status': 'original_account_contract_rejoin_passed',
            'account': OLD, 'node': 42686, 'privateKeyUsed': False,
            'originalValidatorRuntimeTested': False, 'readyForProduction': False,
            'note': 'Reuses Nitro-generated calldata on a separate Anvil; impersonated original EOA.'})
    finally:
        target.rpc('anvil_stopImpersonatingAccount', [OLD])
    print('OUTPUT', a.out)


if __name__ == '__main__':
    main()
