"""Offline re-encoding and EIP-712 check of pasted production proposal imports."""
import json
import re
import sys
from pathlib import Path

BASE = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(BASE / 'toolkit'))
from core import SAFE, ROLLUP, EXEC, NEW, CREATE, REFUNDS, admin, tuple_state, envelope, safe_fields, keccak, raw, word

source = Path('/Users/super/.codex/attachments/70f1b5f5-ffd6-43cf-b132-7088a35cb55b/Pasted text.txt')
text = source.read_text()
docs = [json.JSONDecoder().raw_decode(text[m.start():])[0] for m in re.finditer(r'\{\s*"version"\s*:', text)]
assert len(docs) == 2
A = dict(BlockHash='0x42fea32f2e72edb32eb5a8fb0ec1760a2a26d41ccaf4b96712988be511f71962',
         SendRoot='0x9a0300bf873d9ce0ea8365ac9dd758c6023d13a883ed79e4027d6095838a4603', Batch=324089, PosInBatch=0)
B = dict(BlockHash='0x9f6ef87c6d5ca1b9e17cc94d4c79a9b64bcf32adccc7aad29c2d4dc8e478a74f',
         SendRoot=A['SendRoot'], Batch=324110, PosInBatch=122)
node_hash = '0x6c679a7d16a4f0e6768f320ff0491e512150902e53404ce8a68974e4aee0c779'
assertion = '(' + tuple_state(A) + ',' + tuple_state(B) + ',4467)'
expected = [
    [admin('forceRefundStaker(address[])', '['+','.join(REFUNDS)+']'),
     admin('setWasmModuleRoot(bytes32)', NEW),
     admin(CREATE, 43123, 324089, assertion, node_hash),
     admin('forceConfirmNode(uint64,bytes32,bytes32)', 43126, B['BlockHash'], B['SendRoot'])],
    [admin('resume()')]]
expected_hashes = ['0x0c4d1e1d549247dbb6bee12d248f0d4f20230a05beb4ecb3e987ef6bcbe9c7b9',
                   '0xd3d46cecef80826db10248900275c2a304f857ed71cc4d4d4b3d70ab8f4d343c']
domain = keccak(raw(keccak(b'EIP712Domain(uint256 chainId,address verifyingContract)')) + word(42161) + word(int(SAFE,16)))
typehash = keccak(b'SafeTx(address to,uint256 value,bytes data,uint8 operation,uint256 safeTxGas,uint256 baseGas,uint256 gasPrice,address gasToken,address refundReceiver,uint256 nonce)')
out = BASE / 'evidence/presign-window-20260929-154302-review'
out.mkdir(exist_ok=True)
reports = []
for kind, nonce, doc, txs, want in zip(('recovery','resume'), (11,12), docs, expected, expected_hashes):
    assert doc['chainId'] == '42161' and doc['meta']['createdFromSafeAddress'].lower() == SAFE
    assert doc['transactions'] == txs, 'Pasted calls differ from exact expected parameters'
    fields = safe_fields(envelope(txs), nonce)
    data = raw(typehash) + word(int(fields['to'],16)) + word(int(fields['value'])) + raw(keccak(raw(fields['data'])))
    data += b''.join(word(fields[k]) for k in ('operation','safeTxGas','baseGas','gasPrice'))
    data += word(int(fields['gasToken'],16)) + word(int(fields['refundReceiver'],16)) + word(nonce)
    got = keccak(b'\x19\x01' + raw(domain) + raw(keccak(data)))
    assert got == want, 'Safe EIP-712 hash differs'
    (out/(kind+'.safe-import.json')).write_text(json.dumps(doc,indent=2)+'\n')
    (out/(kind+'.safe-fields.json')).write_text(json.dumps(fields,indent=2)+'\n')
    reports.append(dict(kind=kind,nonce=nonce,to=fields['to'],operation=fields['operation'],
                        calls=len(txs),safeTxHash=got,exactCalldataMatched=True,eip712Matched=True))
report=dict(status='offline_pasted_calls_and_hashes_matched',chainId=42161,safe=SAFE,
    parentConfirmedNode=43123,parentConfirmedInboxMaxCount=324089,recoveryNode=43126,
    recoveryNodeHash=node_hash,checkpointBlock=127108720,governanceNumBlocks=4467,
    before=A,after=B,newWasmRoot=NEW,refundAccounts=REFUNDS,transactions=reports,
    parentConfirmedAToBProven=False,productionStateQueried=False,productionTransactionsSent=False,
    limitations='Pasted import content checked offline. Does not verify current state, complete remote package checksums, real UI fields/signatures or upstream nodeHash derivation inputs.')
(out/'review.json').write_text(json.dumps(report,indent=2)+'\n')
print(json.dumps(report,indent=2))
