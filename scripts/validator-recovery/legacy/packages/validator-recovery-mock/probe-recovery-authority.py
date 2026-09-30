#!/usr/bin/env python3
"""Read-only Rollup proxy inventory. No keys, signing or transactions."""
import argparse
import json
import urllib.request
from pathlib import Path


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--parent-rpc', required=True)
    p.add_argument('--node-rpc', default='http://127.0.0.1:8149')
    p.add_argument('--rollup', default='0xA113e2E9620a3Bc088a681eBB2C234FDbeb85e21')
    p.add_argument('--authority', help='Public wallet or multisig address to check roles')
    p.add_argument('--out', required=True, type=Path)
    a = p.parse_args()
    if a.authority and (len(a.authority) != 42 or not a.authority.startswith('0x') or any(c not in '0123456789abcdefABCDEF' for c in a.authority[2:])):
        p.error('authority must be a 20-byte hex address')
    if a.out.exists():
        p.error('output already exists; choose a new filename')

    def rpc(method, params):
        req = urllib.request.Request(a.parent_rpc, json.dumps(dict(
            jsonrpc='2.0', id=1, method=method, params=params)).encode(),
            {'Content-Type': 'application/json'})
        with urllib.request.urlopen(req, timeout=30) as r:
            obj = json.load(r)
        if 'error' in obj or obj.get('result') is None:
            raise ValueError(str(obj))
        return obj['result']

    if int(rpc('eth_chainId', []), 16) != 42161:
        raise ValueError('Expected Arbitrum One chain 42161')
    head = rpc('eth_getBlockByNumber', ['latest', False])
    tag = head['number']
    report = dict(rollup=a.rollup, parentBlock=tag, parentBlockHash=head['hash'], slots={}, calls={})
    for name, label in [('admin', 'eip1967.proxy.admin'),
                        ('primaryImplementation', 'eip1967.proxy.implementation'),
                        ('secondaryImplementation', 'eip1967.proxy.implementation.secondary')]:
        slot = hex(int(rpc('web3_sha3', ['0x' + label.encode().hex()]), 16) - 1)
        raw = rpc('eth_getStorageAt', [a.rollup, slot, tag])
        address = '0x' + raw[-40:]
        code = rpc('eth_getCode', [address, tag])
        report['slots'][name] = dict(address=address, raw=raw,
            codeBytes=(len(code)-2)//2, codeHash=rpc('web3_sha3', [code]))
    for signature in ['wasmModuleRoot()', 'latestConfirmed()', 'latestNodeCreated()',
                      'paused()', 'bridge()', 'outbox()', 'challengeManager()']:
        try:
            selector = rpc('web3_sha3', ['0x' + signature.encode().hex()])[:10]
            report['calls'][signature] = rpc('eth_call', [{'to': a.rollup, 'data': selector}, tag])
        except ValueError as exc:
            report['calls'][signature] = {'error': str(exc)}
    for signature in ('latestConfirmed()', 'latestNodeCreated()'):
        value = report['calls'].get(signature)
        if isinstance(value, str):
            report.setdefault('nodeNumbers', {})[signature[:-2]] = int(value, 16)

    # Read the latest confirmed node at this snapshot, not an earlier incident node.
    try:
        number = report['nodeNumbers']['latestConfirmed']
        sig = 'getNodeCreationBlockForLogLookup(uint64)'
        selector = rpc('web3_sha3', ['0x' + sig.encode().hex()])[:10]
        creation = int(rpc('eth_call', [{'to':a.rollup,
                       'data':selector + f'{number:064x}'}, tag]), 16)
        signature = ('NodeCreated(uint64,bytes32,bytes32,bytes32,'
                     '(((bytes32[2],uint64[2]),uint8),'
                     '((bytes32[2],uint64[2]),uint8),uint64),'
                     'bytes32,bytes32,uint256)')
        topic = rpc('web3_sha3', ['0x' + signature.encode().hex()])
        logs = rpc('eth_getLogs', [{'address':a.rollup,
                   'fromBlock':hex(creation), 'toBlock':hex(creation),
                   'topics':[topic, '0x' + f'{number:064x}']}])
        if len(logs) != 1 or len(logs[0]['data'][2:]) != 15 * 64:
            raise ValueError('Unexpected legacy NodeCreated event; cannot decode')
        raw = logs[0]['data'][2:]
        words = [raw[i:i+64] for i in range(0, len(raw), 64)]
        def global_state(offset):
            return dict(BlockHash='0x'+words[offset],
                        SendRoot='0x'+words[offset+1],
                        Batch=int(words[offset+2],16),
                        PosInBatch=int(words[offset+3],16),
                        machineStatus=int(words[offset+4],16))
        report['confirmedNode'] = dict(number=number,
            creationBlock=creation, transactionHash=logs[0]['transactionHash'],
            before=global_state(1), after=global_state(6),
            inboxMaxCount=int(words[14],16),
            wasmModuleRoot='0x'+words[13])
    except Exception as exc:
        report['confirmedNode'] = {'error':str(exc)}

    def child_rpc(method, params):
        req = urllib.request.Request(a.node_rpc, json.dumps(dict(
            jsonrpc='2.0', id=1, method=method, params=params)).encode(),
            {'Content-Type':'application/json'})
        with urllib.request.urlopen(req, timeout=30) as r:
            obj = json.load(r)
        if 'error' in obj:
            raise ValueError(str(obj['error']))
        return obj['result']
    try:
        if int(child_rpc('eth_chainId', []),16) != 2886:
            raise ValueError('Expected Deriw chain 2886')
        local = dict(clientVersion=child_rpc('web3_clientVersion', []),
                     head=int(child_rpc('eth_blockNumber', []),16))
        endpoint = report['confirmedNode'].get('after')
        if endpoint:
            block = child_rpc('eth_getBlockByHash', [endpoint['BlockHash'],False])
            local['confirmedEndpointFound'] = block is not None
            if block is not None:
                canonical = child_rpc('eth_getBlockByNumber', [block['number'],False])
                local.update(endpointNumber=int(block['number'],16),
                    canonical=bool(canonical and canonical['hash'] == block['hash']),
                    sendRoot=block.get('sendRoot', block.get('extraData')))
                local['matchesAssertionSendRoot'] = local['sendRoot'] == endpoint['SendRoot']
        report['localChain'] = local
    except Exception as exc:
        report['localChain'] = {'error':str(exc)}
    admin = report['slots']['admin']['address']
    report['adminContract'] = {'address': admin, 'proxySlots': {}, 'calls': {}}
    for name, label in [('admin', 'eip1967.proxy.admin'),
                        ('implementation', 'eip1967.proxy.implementation')]:
        slot = hex(int(rpc('web3_sha3', ['0x' + label.encode().hex()]), 16) - 1)
        raw = rpc('eth_getStorageAt', [admin, slot, tag])
        address = '0x' + raw[-40:]
        code = rpc('eth_getCode', [address, tag])
        report['adminContract']['proxySlots'][name] = dict(address=address,
            codeBytes=(len(code)-2)//2, codeHash=rpc('web3_sha3', [code]))
    for signature in ['owner()', 'getOwners()', 'getThreshold()', 'ADMIN_ROLE()', 'EXECUTOR_ROLE()']:
        try:
            selector = rpc('web3_sha3', ['0x' + signature.encode().hex()])[:10]
            result = rpc('eth_call', [{'to': admin, 'data': selector}, tag])
            report['adminContract']['calls'][signature] = result
            if a.authority and signature in ('ADMIN_ROLE()', 'EXECUTOR_ROLE()') and len(result) == 66:
                selector = rpc('web3_sha3', ['0x' + b'hasRole(bytes32,address)'.hex()])[:10]
                data = selector + result[2:] + a.authority[2:].lower().zfill(64)
                role = rpc('eth_call', [{'to': admin, 'data': data}, tag])
                report['adminContract']['calls']['hasRole:' + signature] = dict(
                    authority=a.authority, raw=role)
        except ValueError as exc:
            report['adminContract']['calls'][signature] = {'error': str(exc)}
    report['note'] = 'Optional interface calls may revert. Successful calls alone do not verify contract identity.'
    check = rpc('eth_getBlockByNumber', [tag, False])
    if check['hash'] != head['hash']:
        raise ValueError('Parent block changed during collection; retry')
    a.out.write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps(report, indent=2))
    print('OUTPUT', a.out)


if __name__ == '__main__':
    main()
