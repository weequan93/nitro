#!/usr/bin/env python3
"""Build a deterministic, checksum-verified preparation-only installer."""
import base64
import gzip
import hashlib
import io
import tarfile
from pathlib import Path
BASE=Path(__file__).resolve().parent.parent/'generated'
BASE.mkdir(parents=True,exist_ok=True)
ROOT=Path(__file__).resolve().parents[2]/'toolkit'
files=sorted(p for p in ROOT.rglob('*') if p.is_file() and '__pycache__' not in p.parts and p.name!='MANIFEST.sha256')
manifest_data=''.join(hashlib.sha256(p.read_bytes()).hexdigest()+'  '+p.relative_to(ROOT).as_posix()+'\n' for p in files).encode()
b=io.BytesIO()
with tarfile.open(fileobj=b,mode='w',format=tarfile.USTAR_FORMAT) as tf:
    for p in sorted(files):
        data=p.read_bytes();info=tarfile.TarInfo('production-recovery-toolkit-v2/'+p.relative_to(ROOT).as_posix())
        info.size=len(data);info.mode=0o600;info.uid=info.gid=0;info.mtime=0
        tf.addfile(info,io.BytesIO(data))
    info=tarfile.TarInfo('production-recovery-toolkit-v2/MANIFEST.sha256')
    info.size=len(manifest_data);info.mode=0o600;info.uid=info.gid=0;info.mtime=0
    tf.addfile(info,io.BytesIO(manifest_data))
blob=gzip.compress(b.getvalue(),mtime=0);archive=BASE/'production-recovery-toolkit-v2.tar.gz';archive.write_bytes(blob)
sha=hashlib.sha256(blob).hexdigest();manifest=hashlib.sha256(manifest_data).hexdigest()
encoded=base64.b64encode(blob).decode();payload='\n'.join(encoded[i:i+88] for i in range(0,len(encoded),88))
script='''#!/usr/bin/env bash
# Installs preparation files only; does not run recovery, Docker, keys or RPC.
(
set -euo pipefail
umask 077
cd /data_new/scripts
test "$(pwd -P)" = /data_new/scripts
STAGE=$(mktemp -d /data_new/scripts/recovery-v2-install.XXXXXX)
base64 -d > "$STAGE/toolkit.tar.gz" <<'PAYLOAD'
'''+payload+'''
PAYLOAD
printf '%s  %s\\n' 'ARCHIVE_HASH' "$STAGE/toolkit.tar.gz" | sha256sum -c -
python3 - "$STAGE" <<'PYINSTALL'
import hashlib, pathlib, sys, tarfile
stage=pathlib.Path(sys.argv[1])
with tarfile.open(stage/'toolkit.tar.gz','r:gz') as tf:
    for m in tf.getmembers():
        p=pathlib.PurePosixPath(m.name)
        if not m.isfile() or p.is_absolute() or '..' in p.parts or p.parts[0]!='production-recovery-toolkit-v2':
            raise SystemExit('Unsafe archive member')
        target=stage.joinpath(*p.parts);target.parent.mkdir(parents=True,exist_ok=True)
        target.write_bytes(tf.extractfile(m).read())
root=stage/'production-recovery-toolkit-v2'
assert hashlib.sha256((root/'MANIFEST.sha256').read_bytes()).hexdigest()=='MANIFEST_HASH', 'Manifest mismatch'
for line in (root/'MANIFEST.sha256').read_text().splitlines():
    h,name=line.split('  ',1)
    assert hashlib.sha256((root/name).read_bytes()).hexdigest()==h, 'File mismatch: '+name
print('PAYLOAD AND FILES VERIFIED')
PYINSTALL
DEST=/data_new/scripts/production-recovery-toolkit-v2
if [ -e "$DEST" ] || [ -L "$DEST" ]; then
  test -d "$DEST"
  test ! -L "$DEST"
  cmp "$DEST/MANIFEST.sha256" "$STAGE/production-recovery-toolkit-v2/MANIFEST.sha256"
  (cd "$DEST" && sha256sum -c MANIFEST.sha256)
  echo 'SAME VERSION ALREADY INSTALLED; nothing replaced'
else
  mv "$STAGE/production-recovery-toolkit-v2" "$DEST"
  echo "INSTALLED $DEST"
fi
echo 'Preparation files only; no simulation or production operation started.'
)
'''
script=script.replace('ARCHIVE_HASH',sha).replace('MANIFEST_HASH',manifest)
installer=BASE/'install-production-recovery-toolkit-v2.sh';installer.write_text(script)
print('FILES',len(files),'ARCHIVE_BYTES',len(blob),'INSTALLER_BYTES',len(script.encode()),'ARCHIVE_SHA256',sha)
