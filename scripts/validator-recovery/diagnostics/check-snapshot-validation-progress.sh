#!/usr/bin/env bash
# Read-only: distinguish persisted validation metadata from active progress.
(
set -euo pipefail
export PATH="$HOME/.foundry/bin:$PATH"
cd /data_new/scripts
REPORT=$(mktemp /data_new/scripts/snapshot-validation-progress.XXXXXX)
exec > >(tee "$REPORT") 2>&1
printf 'REPORT %s\n' "$REPORT"
echo 'CONTAINER START / CONFIG MTIME'
docker inspect --format 'running={{.State.Running}} started={{.State.StartedAt}}' snapshot-sync
stat -c 'configModified=%y' /data_new/scripts/snapshot-sync.json
echo 'REPORTED VALIDATED BLOCK NUMBER'
cast block 0x82437bc6e68b60acfe216e2b72da92f7b7e54c1d1800abb2386384799087a028 --field number --rpc-url http://127.0.0.1:8349 --rpc-timeout 30 || echo 'Reported checkpoint block unavailable by hash'
for SAMPLE in 1 2; do
  printf '\nSAMPLE %s\n' "$SAMPLE"
  date -u
  cast block-number --rpc-url http://127.0.0.1:8349 --rpc-timeout 30
  cast rpc --rpc-url http://127.0.0.1:8349 --rpc-timeout 30 arb_latestValidated
  if [ "$SAMPLE" = 1 ]; then sleep 30; fi
done
echo 'STARTUP VALIDATION MARKERS (may include older starts)'
docker logs --tail 20000 snapshot-sync 2>&1 | grep -E 'BlockValidator initialized|validator chosen|block_validator: assume-valid|validation not supported' | tail -n 15 || true
echo 'RECENT VALIDATION PROGRESS / ERRORS'
docker logs --since 5m --tail 3000 snapshot-sync 2>&1 | grep -Ei 'validated execution|error while validating|validation failed|error trying to (create|record|send).*validation|validation.*(error|failed)|recording.*(error|failed)' | tail -n 30 || true
echo 'No configuration, container or database changes made.'
)
