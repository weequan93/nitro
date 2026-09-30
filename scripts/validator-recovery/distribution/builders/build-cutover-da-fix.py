#!/usr/bin/env python3
"""Package additive Watchtower cutover files, pin existing v2 dependencies, never run RPC."""
import base64
import gzip
import hashlib
import json
from pathlib import Path

root=Path(__file__).resolve().parent.parent/'generated'
root.mkdir(parents=True,exist_ok=True)
kit=Path(__file__).resolve().parents[2]/'toolkit'
names=['cutover.py','CUTOVER-RUNBOOK.md','tests/test_cutover.py']
deps=['core.py','recovery.py','collect.py','replay-span.py','verify-fork.py']
previous={'cutover.py': 'ba0f07a6a9ac3e0a57acfabbeefbf8757f20a411f52c5c53edecd2a411391e5c', 'CUTOVER-RUNBOOK.md': '1c3f6c38759e6d8e0cce4bddadd6c28bcfa69c0769f1f3628b2293e9ff2c55c6', 'tests/test_cutover.py': 'b7f08307a3117ac341196fd6ae4e01320efe9966c20858d8380ba7f2dd4384b0'}
payload={'previous':previous,'files':{n:(kit/n).read_text() for n in names},
         'dependencies':{n:hashlib.sha256((kit/n).read_bytes()).hexdigest() for n in deps}}
blob=gzip.compress(json.dumps(payload,ensure_ascii=False).encode(),mtime=0)
sha=hashlib.sha256(blob).hexdigest()
encoded=base64.b64encode(blob).decode()
lines='\n'.join(encoded[i:i+88] for i in range(0,len(encoded),88))
script='''#!/usr/bin/env bash
# Install cutover tools only; launching requires a separate explicit command. No RPC, Docker, signing or broadcast.
(
set -euo pipefail
umask 077
cd /data_new/scripts
test "$(pwd -P)" = /data_new/scripts
STAGE=$(mktemp -d /data_new/scripts/cutover-da-fix-install.XXXXXX)
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
    if target.exists() and target.read_text()!=content and hashlib.sha256(target.read_bytes()).hexdigest()!=p['previous'][name]:
        raise SystemExit('Unrecognized local edit: '+name)
for name,content in p['files'].items():
    target=base/name
    target.parent.mkdir(parents=True,exist_ok=True)
    if target.exists() and target.read_text()!=content:
        backup=target.with_name(target.name+'.before-da-fix-'+p['previous'][name][:12])
        if backup.is_symlink() or (backup.exists() and backup.read_bytes()!=target.read_bytes()):
            raise SystemExit('Backup conflict: '+name)
        if not backup.exists():
            fd=os.open(backup,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
            with os.fdopen(fd,'wb') as f: f.write(target.read_bytes())
        temp=target.with_name(target.name+'.da-fix-new')
        fd=os.open(temp,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
        with os.fdopen(fd,'w') as f:
            f.write(content);f.flush();os.fsync(f.fileno())
        os.replace(temp,target)
        print('BACKUP',backup.name)
    if not target.exists():
        fd=os.open(target,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
        with os.fdopen(fd,'w') as f:
            f.write(content);f.flush();os.fsync(f.fileno())
    print('VERIFIED',name,hashlib.sha256(target.read_bytes()).hexdigest())
print('INSTALL VERIFIED; DA repair tools updated; no container or chain operations started')
INSTALL
)
'''
script=script.replace('PAYLOAD_SHA256',sha)
(root/'install-recovery-cutover-da-fix.sh').write_text(script)
(root/'recovery-cutover-da-fix.gz').write_bytes(blob)
print(json.dumps({'payloadSha256':sha,'installerBytes':len(script.encode()),'files':names},indent=2))
