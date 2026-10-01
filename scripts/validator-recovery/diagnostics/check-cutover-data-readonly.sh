#!/usr/bin/env bash
# Read-only preparation evidence. No container or database changes.
(
set -euo pipefail
export PATH="$HOME/.foundry/bin:$PATH"
cd /data_new/scripts
REPORT=$(mktemp /data_new/scripts/cutover-data-check.XXXXXX)
exec > >(tee "$REPORT") 2>&1
printf 'REPORT %s\n' "$REPORT"
date -u
command -v cast >/dev/null

echo 'SNAPSHOT CONTAINER (metadata only)'
docker inspect --format 'name={{.Name}} running={{.State.Running}} started={{.State.StartedAt}} image={{.Image}}' snapshot-sync
docker inspect --format '{{range .Mounts}}source={{.Source}} destination={{.Destination}} rw={{.RW}}{{println}}{{end}}' snapshot-sync
echo 'PREPARED IMAGE (local metadata only)'
docker image inspect --format 'id={{.Id}} arch={{.Architecture}} digests={{json .RepoDigests}}' fuhua-container.tencentcloudcr.com/deriw/deriw:v1.3.1.2.v3.10.0-16c17ee3a9c4

echo 'LOCAL L3 CLIENT'
cast rpc --rpc-url http://127.0.0.1:8349 --rpc-timeout 30 web3_clientVersion
test "$(cast chain-id --rpc-url http://127.0.0.1:8349 --rpc-timeout 30)" = 2886
test "$(cast chain-id --rpc-url https://rpc.deriw.com --rpc-timeout 30)" = 2886
LOCAL_HEAD=$(cast block-number --rpc-url http://127.0.0.1:8349 --rpc-timeout 30)
PUBLIC_HEAD=$(cast block-number --rpc-url https://rpc.deriw.com --rpc-timeout 30)
[[ "$LOCAL_HEAD" =~ ^[0-9]+$ && "$PUBLIC_HEAD" =~ ^[0-9]+$ ]]
printf 'localHead=%s publicHead=%s gap=%s\n' "$LOCAL_HEAD" "$PUBLIC_HEAD" "$((PUBLIC_HEAD-LOCAL_HEAD))"
HEIGHT=$LOCAL_HEAD
if (( PUBLIC_HEAD < HEIGHT )); then HEIGHT=$PUBLIC_HEAD; fi
test "$HEIGHT" -gt 64
HEIGHT=$((HEIGHT-64))
LOCAL_HASH=$(cast block "$HEIGHT" --field hash --rpc-url http://127.0.0.1:8349 --rpc-timeout 30)
PUBLIC_HASH=$(cast block "$HEIGHT" --field hash --rpc-url https://rpc.deriw.com --rpc-timeout 30)
[[ "$LOCAL_HASH" =~ ^0x[0-9a-fA-F]{64}$ && "$PUBLIC_HASH" =~ ^0x[0-9a-fA-F]{64}$ ]]
printf 'height=%s\nlocalHash=%s\npublicHash=%s\n' "$HEIGHT" "$LOCAL_HASH" "$PUBLIC_HASH"
test "$LOCAL_HASH" = "$PUBLIC_HASH"
echo 'PASS sampled same-height block hash matches; not WASM validation or cutover approval.'
echo 'Data files, production keys and /data were not accessed. Effective config and parent connection still require review.'
)
