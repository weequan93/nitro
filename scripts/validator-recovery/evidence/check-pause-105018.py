"""Offline review of the user-reported pause fields. No RPC or signing."""
import json
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'toolkit'))
from core import admin, keccak, raw, word

safe = '0xfbb37c66372f7b40361fbc8c8a235ae92711399d'
tx = admin('pause()')
expected_data = '0xbca8c7b5000000000000000000000000a113e2e9620a3bc088a681ebb2c234fdbeb85e21000000000000000000000000000000000000000000000000000000000000004000000000000000000000000000000000000000000000000000000000000000048456cb5900000000000000000000000000000000000000000000000000000000'
assert tx['data'] == expected_data
domain = keccak(raw(keccak(b'EIP712Domain(uint256 chainId,address verifyingContract)')) + word(42161) + word(int(safe, 16)))
type_hash = keccak(b'SafeTx(address to,uint256 value,bytes data,uint8 operation,uint256 safeTxGas,uint256 baseGas,uint256 gasPrice,address gasToken,address refundReceiver,uint256 nonce)')
struct = keccak(raw(type_hash) + word(int(tx['to'], 16)) + word(0) + raw(keccak(raw(tx['data']))) + word(0) * 6 + word(10))
tx_hash = keccak(b'\x19\x01' + raw(domain) + raw(struct))
assert tx_hash == '0x95c613b5f67c0b1392088f9c4f3ac08f73041594faa3511098b201b74c64dadb'
print(json.dumps(dict(calldataMatches=True, safeTxHashMatches=True, chainId=42161, safe=safe, nonce=10, safeTxHash=tx_hash, productionStateQueried=False, productionTransactionsSent=False), indent=2))
