#!/bin/sh
set -eu

: "${PARENT_RPC:?set PARENT_RPC}"
: "${SAFE:?set SAFE}"
: "${NEW_OWNER:?set NEW_OWNER}"
: "${SAFE_OWNER_PK:?set SAFE_OWNER_PK}"

GAS_PRICE="${GAS_PRICE:-100000000}"
THRESHOLD="${THRESHOLD:-1}"
ZERO=0x0000000000000000000000000000000000000000
ZERO32=0000000000000000000000000000000000000000000000000000000000000000

SAFE_OWNER="$(cast wallet address --private-key "$SAFE_OWNER_PK")"

echo "SAFE=$SAFE"
echo "SAFE_OWNER=$SAFE_OWNER"
echo "NEW_OWNER=$NEW_OWNER"
echo "THRESHOLD=$THRESHOLD"

echo "current owner check:"
cast call --rpc-url "$PARENT_RPC" "$SAFE" 'isOwner(address)(bool)' "$SAFE_OWNER"

echo "new owner before:"
cast call --rpc-url "$PARENT_RPC" "$SAFE" 'isOwner(address)(bool)' "$NEW_OWNER"

TXDATA="$(cast calldata 'addOwnerWithThreshold(address,uint256)' "$NEW_OWNER" "$THRESHOLD")"
OWNER_NO0X="$(printf '%s' "$SAFE_OWNER" | sed 's/^0x//')"
OWNER_PADDED="$(printf '%064s' "$OWNER_NO0X" | tr ' ' '0')"
SIGS=0x${OWNER_PADDED}${ZERO32}01

cast send --rpc-url "$PARENT_RPC" --private-key "$SAFE_OWNER_PK" "$SAFE" \
  'execTransaction(address,uint256,bytes,uint8,uint256,uint256,uint256,address,address,bytes)' \
  "$SAFE" 0 "$TXDATA" 0 0 0 0 "$ZERO" "$ZERO" "$SIGS" \
  --legacy \
  --gas-price "$GAS_PRICE"

echo "new owner after:"
cast call --rpc-url "$PARENT_RPC" "$SAFE" 'isOwner(address)(bool)' "$NEW_OWNER"

echo "owners:"
cast call --rpc-url "$PARENT_RPC" "$SAFE" 'getOwners()(address[])'
