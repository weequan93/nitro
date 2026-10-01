#!/usr/bin/env bash
# One explicit-root replay; no transaction, reset, container restart or config change.
(
set -euo pipefail
export PATH="$HOME/.foundry/bin:$PATH"
cd /data_new/scripts
OUT=$(mktemp -d /data_new/scripts/snapshot-next-validation.XXXXXX)
printf 'OUTPUT %s\n' "$OUT"
echo 'RECENT STATE PREPARATION LOGS'
docker logs --since 15m --tail 10000 snapshot-sync > "$OUT/recent.log" 2>&1
grep -Ei 'Setting up validation|prepareblocks|recording|low on memory|validator got error|validation failed' "$OUT/recent.log" | tail -n 30 || true
ROOT=0x121d685e2fdb0e3291592d6b90bd70d503951335d19d96455448eb7a14d17421
MESSAGE=$(cast to-hex 125022703)
test "$(cast chain-id --rpc-url http://127.0.0.1:8349 --rpc-timeout 30)" = 2886
echo 'Replaying only message 125022703 with explicit new WASM root (up to 300 seconds).'
date -u
if cast rpc --rpc-url http://127.0.0.1:8349 --rpc-timeout 300 arbdebug_validateMessageNumber "$MESSAGE" true "$ROOT" > "$OUT/replay.json" 2> "$OUT/replay-error.txt"; then
  cat "$OUT/replay.json"
  echo 'RPC completed; inspect Valid in the response. RPC success alone is not validation success.'
else
  echo 'Replay RPC failed or timed out; background validation status remains unknown.'
  cat "$OUT/replay-error.txt"
fi
date -u
cast rpc --rpc-url http://127.0.0.1:8349 --rpc-timeout 30 arb_latestValidated > "$OUT/latest-validated.json"
cat "$OUT/latest-validated.json"
echo 'An explicit one-message replay does not advance the background validation checkpoint.'
)
