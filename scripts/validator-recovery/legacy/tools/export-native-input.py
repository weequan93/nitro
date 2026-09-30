#!/usr/bin/env python3
"""Export the saved single-batch, Keccak-only case for cmd/replay native mode."""
import argparse
import base64
import json
import shlex
import struct
from pathlib import Path
p=argparse.ArgumentParser(description=__doc__)
p.add_argument('input', type=Path)
p.add_argument('--out', type=Path, required=True)
a=p.parse_args()
o=json.loads(a.input.read_text()); e=o.get('result',o)
if not isinstance(e,dict) or 'StartState' not in e:
    p.error('not a validation input')
if set(e['PreimagesB64']) != {'0'} or len(e['BatchInfo']) != 1:
    p.error('this exporter supports only one batch and Keccak preimages')
if any(e.get('UserWasms',{}).values()):
    p.error('Stylus user WASMs are not supported by this exporter')
s=e['StartState']; batch=e['BatchInfo'][0]
if batch['Number'] != s['Batch']:
    p.error('batch does not match StartState')
decode=lambda v:base64.b64decode(v,validate=True)
preimages=[decode(v) for v in e['PreimagesB64']['0'].values()]
inbox=decode(batch['DataB64'])
a.out.mkdir(parents=True,exist_ok=False)
a.out=a.out.resolve()
(a.out/'inbox.bin').write_bytes(inbox)
with (a.out/'preimages.bin').open('wb') as f:
    for value in preimages:
        f.write(struct.pack('<Q',len(value)));f.write(value)
args=['--inbox-position',str(s['Batch']),'--position-within-message',str(s['PosInBatch']),
      '--last-block-hash',s['BlockHash'],'--inbox',str(a.out/'inbox.bin'),
      '--preimages',str(a.out/'preimages.bin')]
if e.get('HasDelayedMsg'):
    (a.out/'delayed.bin').write_bytes(decode(e['DelayedMsgB64']))
    args+=['--delayed-inbox-position',str(e['DelayedMsgNr']),'--delayed-inbox',str(a.out/'delayed.bin')]
(a.out/'args.json').write_text(json.dumps(args,indent=2)+'\n')
(a.out/'expected.json').write_text(json.dumps(e['ExpectedEndState'],indent=2)+'\n')
print('./replay-baseline '+shlex.join(args))
print('./replay-duplicate-error '+shlex.join(args))
