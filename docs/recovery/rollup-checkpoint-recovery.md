# Rollup Checkpoint Recovery Runbook

This runbook documents the emergency recovery path used when the L1 rollup
confirmed assertions diverge from the current trusted sequencer chain.

It is an admin recovery procedure. It is not normal fraud-proof validation.
Use it only when you intentionally choose the current sequencer history as the
canonical history and cannot practically revalidate from the last matching L1
assertion.

## Situation

Symptoms:

```text
latest assertion ... not in chain:
globalstate not in chain: count ... hash <asserted> expected <local>
```

Meaning:

- L1 rollup latest confirmed node points to a `GlobalState`.
- The validator maps that `GlobalState` through `Batch` and `PosInBatch`.
- The local sequencer/validator chain has a different block hash/send root at
  that position.

In the incident this showed up after rollback attempts:

```text
latestConfirmed=35794 bad
latestConfirmed=35793 bad
latestConfirmed=35792 bad
```

Scanning older `NodeCreated` events showed no usable matching assertion in the
tested range.

## Important Concepts

Nitro assertions are checked with the full global state:

```text
GlobalState {
  BlockHash
  SendRoot
  Batch
  PosInBatch
}
```

`Batch` is the sequencer inbox batch number. It is not the L2 block number.
`PosInBatch` is the position inside that batch. Many L2 blocks can be inside one
batch.

The validator uses `Batch` and `PosInBatch` to find the exact message count in
the local sequencer stream, then checks:

```text
local block hash == GlobalState.BlockHash
local sendRoot   == GlobalState.SendRoot
```

If `Batch`/`PosInBatch` are wrong, the validator will still fail even if the
block hash is trusted.

## Recovery Strategy

There are two possible paths.

### Path A: Roll Back To A Matching L1 Assertion

Use this only if some older `NodeCreated` assertion exists in the current
sequencer chain.

1. Scan L1 `NodeCreated` events.
2. For each asserted `BlockHash`, check whether it exists in the trusted L2 RPC.
3. If found and send root matches, roll `latestConfirmed` back to that node.
4. Resume validator from that node.

Use:

```bash
PARENT_RPC="$PARENT_RPC" \
ROLLUP="$ROLLUP" \
LOCAL_L2_RPC=https://rpc.test.deriw.com \
START_NODE=35792 \
END_NODE=35000 \
node docs/recovery/scripts/scan-node-created-hashes.js
```

The scanner checks asserted block hashes directly with `eth_getBlockByHash`.
That is more reliable than estimating the L2 block number from the assertion's
batch/position fields.

### Path B: Admin Checkpoint Reset

Use this when L1 assertions have diverged too far and you trust the current
sequencer chain.

This creates a new synthetic confirmed node at `latestNodeCreated + 1` using a
recent trusted sequencer block as checkpoint.

The recovery implementation:

- creates a new `_nodes[newNodeNum]` entry
- emits a `NodeCreated` event so Nitro `LookupNode` can find it
- updates `latestConfirmed`, `firstUnresolvedNode`, and `latestNodeCreated`
- calls `outbox.updateSendRoot(sendRoot, blockHash)`
- leaves the rollup paused

After success, restore the original admin implementation immediately.

## Pre-Flight

Stop validators/stakers:

```bash
docker stop validator-nitro-1
```

Pause rollup:

```bash
UPGRADE_EXECUTOR=$(cast call --rpc-url "$PARENT_RPC" "$ROLLUP" 'owner()(address)')

PAUSE_DATA=$(cast calldata 'pause()')

cast send --rpc-url "$PARENT_RPC" --private-key "$OWNER_PK" "$UPGRADE_EXECUTOR" \
  'executeCall(address,bytes)' \
  "$ROLLUP" "$PAUSE_DATA" \
  --legacy \
  --gas-price 100000000
```

Verify:

```bash
cast call --rpc-url "$PARENT_RPC" "$ROLLUP" 'paused()(bool)'
cast call --rpc-url "$PARENT_RPC" "$ROLLUP" 'latestConfirmed()(uint64)'
cast call --rpc-url "$PARENT_RPC" "$ROLLUP" 'firstUnresolvedNode()(uint64)'
cast call --rpc-url "$PARENT_RPC" "$ROLLUP" 'latestNodeCreated()(uint64)'
cast call --rpc-url "$PARENT_RPC" "$ROLLUP" 'stakerCount()(uint64)'
cast call --rpc-url "$PARENT_RPC" "$ROLLUP" 'zombieCount()(uint256)'
```

The same read-only checks can be run with:

```bash
PARENT_RPC="$PARENT_RPC" \
ROLLUP="$ROLLUP" \
OLD_STAKER=0x94c3A588B17c62f7316a8962a110855c0A43EA8e \
NEW_STAKER=0xCC4799850299088637D3636fdBca4474300129E8 \
docs/recovery/scripts/rollup-state.sh
```

Save the original admin implementation:

```bash
IMPL_SLOT=0x360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc
ORIGINAL_IMPL_RAW=$(cast storage --rpc-url "$PARENT_RPC" "$ROLLUP" "$IMPL_SLOT")
ORIGINAL_IMPL=0x${ORIGINAL_IMPL_RAW:26}

echo "ORIGINAL_IMPL_RAW=$ORIGINAL_IMPL_RAW"
echo "ORIGINAL_IMPL=$ORIGINAL_IMPL"
```

## Choosing A Checkpoint

Pick a block from the trusted sequencer chain that every validator can already
read. Do not checkpoint ahead of the validator's local processed data.

Check validator logs:

```text
validator catching up to last valid
lastValid.Batch=245,719 lastValid.PosInBatch=519
batchCount=245,687
processedMsgCount=125058462
```

If checkpoint batch is above `batchCount`, the validator cannot verify it yet.
Choose an older checkpoint or wait for sync.

On a sequencer/full Nitro machine, compute checkpoint data:

```bash
LOCAL_L2_RPC=http://127.0.0.1:8449 CHECKPOINT_HEX=0x... \
  docs/recovery/scripts/checkpoint-position.sh
```

The checkpoint script only needs `sh`, `curl`, and `jq`. It calls the local
Nitro `NodeInterface` at `0x00000000000000000000000000000000000000c8` and uses
`findBatchContainingBlock(uint64)` to derive the sequencer inbox batch. It then
binary-searches the first block in that batch to compute `PosInBatch`.

Example output:

```text
CHECKPOINT_DEC=125082197
CHECKPOINT_HEX=0x7749a55
BLOCK_HASH_AT_CHECKPOINT=0x564f443d904cf7c9f34863a82239ef6bff8fda28e5000b7c16fe30c62abadfe6
GLOBALSTATE_BLOCK_HASH=0x...
SEND_ROOT=0xb5b71eb521f09ae50d27942c0dd4751f9732413e0ff80d3a8f5d630ee91833d4
BATCH=245719
POS_IN_BATCH=519
```

Use `GLOBALSTATE_BLOCK_HASH` for `emergencyConfirmSequencerCheckpoint`.
Nitro checks the global state's `BlockHash` against the block at
`messageCount - 1`, while `Batch` and `PosInBatch` identify `messageCount`.

## Deploy Minimal Recovery Implementation

Use the minimal implementation. The full reference implementation may exceed the
EVM contract size limit.

```bash
cd /Users/super/Documents/coinw/dex/nitro3/contracts

NEW_RECOVERY_IMPL=$(forge create \
  --root /Users/super/Documents/coinw/dex/nitro3/contracts \
  --rpc-url "$PARENT_RPC" \
  --private-key "$OWNER_PK" \
  src/rollup/RollupAdminLogicRecoveryMinimal.sol:RollupAdminLogicRecoveryMinimal \
  --legacy \
  --gas-price 100000000 \
  --broadcast \
  | awk '/Deployed to:/ {print $3}')

echo "NEW_RECOVERY_IMPL=$NEW_RECOVERY_IMPL"
```

Do not continue if the address is empty.

Never put private keys in these docs or scripts. Pass keys through environment
variables only in the shell that is executing the command.

## Execute Checkpoint Reset

Replace expected values and checkpoint values with the current verified values.

Example:

```bash
CHECKPOINT_DATA=$(cast calldata \
  'emergencyConfirmSequencerCheckpoint(uint64,uint64,uint64,bytes32,bytes32,uint64,uint64)' \
  35792 35793 35794 \
  "$GLOBALSTATE_BLOCK_HASH" \
  0xb5b71eb521f09ae50d27942c0dd4751f9732413e0ff80d3a8f5d630ee91833d4 \
  245719 519)

UPGRADE_DATA=$(cast calldata \
  'upgradeToAndCall(address,bytes)' \
  "$NEW_RECOVERY_IMPL" "$CHECKPOINT_DATA")

cast send --rpc-url "$PARENT_RPC" --private-key "$OWNER_PK" "$UPGRADE_EXECUTOR" \
  'executeCall(address,bytes)' \
  "$ROLLUP" "$UPGRADE_DATA" \
  --legacy \
  --gas-price 100000000
```

Expected after success if previous latest node was `35794`:

```text
latestConfirmed   = 35795
firstUnresolved   = 35796
latestNodeCreated = 35795
paused            = true
```

Verify:

```bash
cast call --rpc-url "$PARENT_RPC" "$ROLLUP" 'latestConfirmed()(uint64)'
cast call --rpc-url "$PARENT_RPC" "$ROLLUP" 'firstUnresolvedNode()(uint64)'
cast call --rpc-url "$PARENT_RPC" "$ROLLUP" 'latestNodeCreated()(uint64)'
cast call --rpc-url "$PARENT_RPC" "$ROLLUP" 'paused()(bool)'
cast call --rpc-url "$PARENT_RPC" "$ROLLUP" \
  'getNodeCreationBlockForLogLookup(uint64)(uint256)' 35795
```

## Restore Original Implementation

Restore immediately after the checkpoint reset succeeds:

```bash
RESTORE_DATA=$(cast calldata 'upgradeTo(address)' "$ORIGINAL_IMPL")

cast send --rpc-url "$PARENT_RPC" --private-key "$OWNER_PK" "$UPGRADE_EXECUTOR" \
  'executeCall(address,bytes)' \
  "$ROLLUP" "$RESTORE_DATA" \
  --legacy \
  --gas-price 100000000
```

Verify:

```bash
cast storage --rpc-url "$PARENT_RPC" "$ROLLUP" "$IMPL_SLOT"
echo "ORIGINAL_IMPL=$ORIGINAL_IMPL"
```

The implementation slot must end with the `ORIGINAL_IMPL` address without `0x`.

## Resume And Start Validator

Resume:

```bash
RESUME_DATA=$(cast calldata 'resume()')

cast send --rpc-url "$PARENT_RPC" --private-key "$OWNER_PK" "$UPGRADE_EXECUTOR" \
  'executeCall(address,bytes)' \
  "$ROLLUP" "$RESUME_DATA" \
  --legacy \
  --gas-price 100000000
```

Start validator with fast confirmation disabled for the first repair run:

```text
--node.staker.enable=true
--node.staker.enable-fast-confirmation=false
--node.staker.start-validation-from-staked=true
--node.block-validator.current-module-root=0x767c9a47cced7ccc3bf419a7efdd9ffb0f23a5dba42f30f3de64f32e2f82c55f
--node.block-validator.pending-upgrade-module-root=
```

If logs show:

```text
validator catching up to last valid
lastValid.Batch=<checkpoint batch>
batchCount=<lower batch>
```

then the checkpoint is ahead of this validator's local data. Either wait for the
validator to sync, or create a new checkpoint at an older block that the
validator already has.

## Notes

- Always restore the original implementation after recovery.
- Keep the rollup paused while deploying/upgrading/recovering.
- Do not use `--node.staker.dangerous.without-block-validator` during this
  recovery unless you intentionally want to bypass validation.
- Fast confirmation should be disabled until the new validator can validate
  forward cleanly from the checkpoint.

## Safe Owner Helper

If the fast-confirmation Safe is threshold `1`, add a new owner with:

```bash
PARENT_RPC="$PARENT_RPC" \
SAFE=0x0401E20216dc86bE781eEE04AB48315195dDE035 \
NEW_OWNER=0xCC4799850299088637D3636fdBca4474300129E8 \
SAFE_OWNER_PK="$SAFE_OWNER_PK" \
docs/recovery/scripts/safe-add-owner-threshold1.sh
```

This is only for a threshold-1 Safe where `SAFE_OWNER_PK` belongs to an
existing Safe owner.
