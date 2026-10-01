#!/usr/bin/env python3
"""Package additive pre-sign files, pin existing v2 dependencies, never run RPC."""
import base64
import gzip
import hashlib
import json
from pathlib import Path

root=Path(__file__).resolve().parent.parent/'generated'
root.mkdir(parents=True,exist_ok=True)
kit=Path(__file__).resolve().parents[2]/'toolkit'
names=['presign.py','PRESIGN-RUNBOOK.md','tests/test_presign.py']
deps=['core.py','recovery.py','collect.py','replay-span.py','verify-fork.py']
payload={'files':{n:(kit/n).read_text() for n in names},
         'dependencies':{n:hashlib.sha256((kit/n).read_bytes()).hexdigest() for n in deps}}
blob=gzip.compress(json.dumps(payload,ensure_ascii=False).encode(),mtime=0)
sha=hashlib.sha256(blob).hexdigest()
encoded=base64.b64encode(blob).decode()
lines='\n'.join(encoded[i:i+88] for i in range(0,len(encoded),88))
script='''#!/usr/bin/env bash
# Add pre-sign preparation files only. No RPC, Docker, signing or broadcast.
(
set -euo pipefail
umask 077
cd /data_new/scripts
test "$(pwd -P)" = /data_new/scripts
STAGE=$(mktemp -d /data_new/scripts/presign-install.XXXXXX)
base64 -d > "$STAGE/addon.gz" <<'PAYLOAD'
'''+lines+'''
PAYLOAD
printf '%s  %s\\n' 'PAYLOAD_SHA256' "$STAGE/addon.gz" | sha256sum -c -
python3 - "$STAGE/addon.gz" <<'INSTALL'
import gzip, hashlib, json, os, sys
from pathlib import Path
blob=Path(sys.argv[1]).read_bytes()
if hashlib.sha256(blob).hexdigest()!='PAYLOAD_SHA256': raise SystemExit('Payload mismatch')
p=json.loads(gzip.decompress(blob))
base=Path('/data_new/scripts/production-recovery-toolkit-v2')
if not base.is_dir() or any(x.is_symlink() for x in (base,*base.parents)):
    raise SystemExit('Unexpected toolkit path')
for name,want in p['dependencies'].items():
    target=base/name
    if target.is_symlink() or hashlib.sha256(target.read_bytes()).hexdigest()!=want:
        raise SystemExit('Dependency mismatch: '+name)
for name,content in p['files'].items():
    target=base/name
    if any(x.is_symlink() for x in (target,*target.parents)):
        raise SystemExit('Symlink path rejected')
    if target.exists() and target.read_text()!=content:
        raise SystemExit('Existing different file: '+name)
for name,content in p['files'].items():
    target=base/name
    target.parent.mkdir(parents=True,exist_ok=True)
    if not target.exists():
        fd=os.open(target,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
        with os.fdopen(fd,'w') as f:
            f.write(content);f.flush();os.fsync(f.fileno())
    print('VERIFIED',name,hashlib.sha256(target.read_bytes()).hexdigest())
print('INSTALL VERIFIED; existing v2 files unchanged; no chain operations started')
INSTALL
)
'''
script=script.replace('PAYLOAD_SHA256',sha)
(root/'install-recovery-presign-addon.sh').write_text(script)
(root/'recovery-presign-addon.gz').write_bytes(blob)
print(json.dumps({'payloadSha256':sha,'installerBytes':len(script.encode()),'files':names},indent=2))
