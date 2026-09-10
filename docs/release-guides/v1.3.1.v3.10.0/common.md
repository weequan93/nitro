# v1.3.1.v3.10.0 shared procedure

These steps apply to all environments. Environment guides define inventory,
rollout order, soak duration, and governance authorization.

## 1. Build the release artifact

```bash
cd /data/nitro

git fetch origin --tags
git switch deriw-master-legacy
git pull --ff-only
git submodule sync --recursive
git submodule update --init --recursive --force

git status --short
git rev-parse HEAD
git -C go-ethereum rev-parse HEAD
./scripts/check-submodules.sh --strict
```

Required revisions:

```text
Nitro:       7f7d4033d7f1c5e5f56fcc149f6ff60f388e459d
go-ethereum: e582a7d5fb4887cbd3b4c7bb4fe74d2a5b06bddc
```

Build with an immutable version. The first rollout intentionally retains the
already-tested `nitro-node-dev` target and its split legacy/new validator
entrypoint. Changing image targets must be a later, separate release.

```bash
export REGISTRY="fuhua-container.tencentcloudcr.com/deriw/deriw"
export VERSION="v1.3.1.v3.10.0-7f7d403"
export IMAGE="${REGISTRY}:${VERSION}"
export COMMIT="7f7d4033d7f1c5e5f56fcc149f6ff60f388e459d"
export BUILD_TIME="$(git show -s --date=iso-strict --format=%cd "$COMMIT")"

docker build . \
  --target nitro-node-dev \
  --build-arg version="$VERSION" \
  --build-arg datetime="$BUILD_TIME" \
  --build-arg modified=false \
  --tag "$IMAGE"
```

## 2. Verify, publish, and pin the artifact

```bash
docker run --rm --entrypoint /usr/local/bin/nitro "$IMAGE" --version

docker run --rm --entrypoint cat "$IMAGE" \
  /home/user/target/machines/latest/module-root.txt

docker run --rm --entrypoint sh "$IMAGE" -c \
  'find /home/user/target/machines /home/user/nitro-legacy/machines \
  -name module-root.txt -exec cat {} \;' | sort -u

docker push "$IMAGE"
docker pull "$IMAGE"
docker image inspect --format='{{range .RepoDigests}}{{println .}}{{end}}' \
  "$IMAGE"
```

Required output:

```text
Version: v1.3.1.v3.10.0-7f7d403
0x121d685e2fdb0e3291592d6b90bd70d503951335d19d96455448eb7a14d17421
```

Record the digest as `RELEASE_IMAGE` and use it in Compose:

```bash
export RELEASE_IMAGE="fuhua-container.tencentcloudcr.com/deriw/deriw@sha256:REPLACE_ME"
```

## 3. Capture chain preflight

Do not source private keys from this file. Fill environment-specific addresses
in the change ticket or an untracked, access-controlled shell session.

```bash
export L2_RPC="https://ENVIRONMENT-L2-RPC"
export PARENT_RPC="https://ENVIRONMENT-PARENT-RPC"
export ROLLUP="0xENVIRONMENT_ROLLUP"
export TARGET_WASM_ROOT="0x121d685e2fdb0e3291592d6b90bd70d503951335d19d96455448eb7a14d17421"

cast chain-id --rpc-url "$L2_RPC"
cast block-number --rpc-url "$L2_RPC"

cast call --rpc-url "$L2_RPC" \
  0x0000000000000000000000000000000000000064 \
  'arbOSVersion()(uint256)'

cast call --rpc-url "$L2_RPC" \
  0x000000000000000000000000000000000000006B \
  'getScheduledUpgrade()(uint64,uint64)'

cast call --rpc-url "$PARENT_RPC" "$ROLLUP" \
  'wasmModuleRoot()(bytes32)'
```

Confirm the current official root is present in the release image. Do not
modify a live chain's `InitialArbOSVersion` in chain-info JSON.

## 4. Back up one node

Before each role's first node, take a provider disk/database snapshot. The
v1.2.3-to-v3.10 jump is large, so an image rollback alone is not sufficient if
the database format changes.

```bash
cd /path/to/compose

export BACKUP_DIR="/data/deploy-backups/$(date +%Y%m%d-%H%M%S)"
mkdir -p "$BACKUP_DIR"
cp docker-compose.yaml "$BACKUP_DIR/"
cp nodeConfig.json "$BACKUP_DIR/"
docker compose config > "$BACKUP_DIR/compose.rendered.yaml"
docker compose images > "$BACKUP_DIR/images.txt"
docker compose ps > "$BACKUP_DIR/containers.txt"
sha256sum docker-compose.yaml nodeConfig.json
```

Record the snapshot ID, current image digest, block number, and config hashes.
Never run `docker compose down -v` during this procedure.

## 5. Deploy one node

Drain the node from traffic first. Change only the image unless the environment
guide explicitly requires a config delta.

```yaml
services:
  nitro:
    image: fuhua-container.tencentcloudcr.com/deriw/deriw@sha256:REPLACE_ME
```

```bash
docker pull "$RELEASE_IMAGE"
docker compose config
docker compose stop -t 90 nitro
docker compose up -d --no-deps nitro
docker compose ps
docker compose logs --since=10m nitro
```

Investigate any matching line before proceeding:

```bash
docker compose logs --since=10m nitro 2>&1 | \
  grep -Ei 'panic|fatal|validation.*fail|module.root|machine.*missing|database|migration|coordinator|anytrust|batch.poster'
```

## 6. Validate one node

```bash
export LOCAL_RPC="http://127.0.0.1:8449"

cast chain-id --rpc-url "$LOCAL_RPC"
cast block-number --rpc-url "$LOCAL_RPC"

curl -sS "$LOCAL_RPC" \
  -H 'content-type: application/json' \
  --data '{"jsonrpc":"2.0","id":1,"method":"eth_syncing","params":[]}'
```

Choose a finalized block and compare its hash with an unchanged node:

```bash
export CHECK_BLOCK="REPLACE_WITH_FINALIZED_BLOCK"
cast block "$CHECK_BLOCK" --rpc-url "$LOCAL_RPC" --json | jq -r '.hash'
cast block "$CHECK_BLOCK" --rpc-url "$REFERENCE_RPC" --json | jq -r '.hash'
```

Return the node to traffic only when hashes match, synchronization is healthy,
logs are clean, and environment-specific smoke tests pass.

## 7. Release-specific functional checks

Run against every upgraded RPC/sequencer path:

- Normal funded transaction and gas estimate.
- Gasless allowed estimate from an unfunded account.
- Unlisted-target estimate retaining normal fee/balance behavior.
- Disposable blacklisted sender transaction rejected by admission.
- Disposable blacklisted recipient transaction rejected by admission.
- Blacklisted parent/subaccount and child cases rejected.
- Normal parent/subaccount transaction accepted.
- Native-value, ERC-20, and contract-call cases.

`eth_call` alone does not test sequencer transaction admission. Remove all
disposable blacklist entries after the test.

## 8. Rollback

Before protocol activation:

1. Drain the failed node.
2. Stop the new container.
3. Restore the previous image digest.
4. Restore the database/disk snapshot if the old process cannot safely open the
   database.
5. Start the old container and compare finalized block hashes.
6. Resume traffic only after recovery is confirmed.

Stop the rollout on hash divergence, validation failure, missing machines,
database failure, sequencer split brain, stalled blocks, failed batch posting,
DAC failure, or a sustained RPC error/latency regression.

After a WASM-root or ArbOS activation, do not assume v1.2.3 binaries are safe.

