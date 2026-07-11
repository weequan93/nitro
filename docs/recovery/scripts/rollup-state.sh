#!/bin/sh
set -eu

: "${PARENT_RPC:?set PARENT_RPC}"
: "${ROLLUP:?set ROLLUP}"

echo "rollup=$ROLLUP"
echo "paused=$(cast call --rpc-url "$PARENT_RPC" "$ROLLUP" 'paused()(bool)')"
echo "latestConfirmed=$(cast call --rpc-url "$PARENT_RPC" "$ROLLUP" 'latestConfirmed()(uint64)')"
echo "firstUnresolvedNode=$(cast call --rpc-url "$PARENT_RPC" "$ROLLUP" 'firstUnresolvedNode()(uint64)')"
echo "latestNodeCreated=$(cast call --rpc-url "$PARENT_RPC" "$ROLLUP" 'latestNodeCreated()(uint64)')"
echo "stakerCount=$(cast call --rpc-url "$PARENT_RPC" "$ROLLUP" 'stakerCount()(uint64)')"
echo "zombieCount=$(cast call --rpc-url "$PARENT_RPC" "$ROLLUP" 'zombieCount()(uint256)')"
echo "wasmModuleRoot=$(cast call --rpc-url "$PARENT_RPC" "$ROLLUP" 'wasmModuleRoot()(bytes32)')"
echo "owner=$(cast call --rpc-url "$PARENT_RPC" "$ROLLUP" 'owner()(address)')"

if [ "${OLD_STAKER:-}" ]; then
  echo "oldStaker=$OLD_STAKER"
  echo "old.isStaked=$(cast call --rpc-url "$PARENT_RPC" "$ROLLUP" 'isStaked(address)(bool)' "$OLD_STAKER")"
  echo "old.latestStakedNode=$(cast call --rpc-url "$PARENT_RPC" "$ROLLUP" 'latestStakedNode(address)(uint64)' "$OLD_STAKER")"
  echo "old.currentChallenge=$(cast call --rpc-url "$PARENT_RPC" "$ROLLUP" 'currentChallenge(address)(uint64)' "$OLD_STAKER")"
fi

if [ "${NEW_STAKER:-}" ]; then
  echo "newStaker=$NEW_STAKER"
  echo "new.isStaked=$(cast call --rpc-url "$PARENT_RPC" "$ROLLUP" 'isStaked(address)(bool)' "$NEW_STAKER")"
  echo "new.latestStakedNode=$(cast call --rpc-url "$PARENT_RPC" "$ROLLUP" 'latestStakedNode(address)(uint64)' "$NEW_STAKER")"
  echo "new.currentChallenge=$(cast call --rpc-url "$PARENT_RPC" "$ROLLUP" 'currentChallenge(address)(uint64)' "$NEW_STAKER")"
fi
