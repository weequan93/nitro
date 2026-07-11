#!/bin/sh
set -eu

LOCAL_L2_RPC="${LOCAL_L2_RPC:-http://127.0.0.1:8449}"
CHECKPOINT_HEX="${CHECKPOINT_HEX:-}"
NODE_INTERFACE="${NODE_INTERFACE:-0x00000000000000000000000000000000000000c8}"

if [ -z "$CHECKPOINT_HEX" ]; then
  echo "usage: LOCAL_L2_RPC=http://127.0.0.1:8449 CHECKPOINT_HEX=0x... $0" >&2
  exit 1
fi

if ! command -v jq >/dev/null 2>&1; then
  echo "jq is required" >&2
  exit 1
fi

rpc() {
  payload="$1"
  curl -sS "$LOCAL_L2_RPC" \
    -H 'content-type: application/json' \
    -d "$payload"
}

rpc_result() {
  payload="$1"
  response="$(rpc "$payload")"
  error="$(printf '%s' "$response" | jq -r '.error.message // empty')"
  if [ -n "$error" ]; then
    echo "rpc error: $error" >&2
    echo "$response" >&2
    exit 1
  fi
  printf '%s' "$response" | jq -r '.result'
}

hex_to_dec() {
  hex="${1#0x}"
  printf '%d' "0x$hex"
}

call_find_batch() {
  block_dec="$1"
  arg="$(printf '%064x' "$block_dec")"
  result="$(rpc_result '{"jsonrpc":"2.0","id":1,"method":"eth_call","params":[{"to":"'"$NODE_INTERFACE"'","data":"0x81f1adaf'"$arg"'"},"latest"]}')"
  hex_to_dec "$result"
}

get_block() {
  block_hex="$1"
  rpc_result '{"jsonrpc":"2.0","id":1,"method":"eth_getBlockByNumber","params":["'"$block_hex"'",false]}'
}

CHECKPOINT_DEC="$(hex_to_dec "$CHECKPOINT_HEX")"
BLOCK_JSON="$(get_block "$CHECKPOINT_HEX")"

if [ "$BLOCK_JSON" = "null" ]; then
  echo "block not found: $CHECKPOINT_HEX" >&2
  exit 1
fi

BLOCK_HASH="$(printf '%s' "$BLOCK_JSON" | jq -r '.hash')"
SEND_ROOT="$(printf '%s' "$BLOCK_JSON" | jq -r '.sendRoot // .extraData')"
BATCH="$(call_find_batch "$CHECKPOINT_DEC")"

lo=0
hi="$CHECKPOINT_DEC"
while [ "$lo" -lt "$hi" ]; do
  mid=$(((lo + hi) / 2))
  mid_batch="$(call_find_batch "$mid")"
  if [ "$mid_batch" -ge "$BATCH" ]; then
    hi="$mid"
  else
    lo=$((mid + 1))
  fi
done

FIRST_BLOCK_IN_BATCH="$lo"
POS_IN_BATCH=$((CHECKPOINT_DEC - FIRST_BLOCK_IN_BATCH))
PREV_BLOCK_HEX="$(printf '0x%x' "$((CHECKPOINT_DEC - 1))")"
PREV_BLOCK_JSON="$(get_block "$PREV_BLOCK_HEX")"

if [ "$PREV_BLOCK_JSON" = "null" ]; then
  echo "previous block not found: $PREV_BLOCK_HEX" >&2
  exit 1
fi

GLOBALSTATE_BLOCK_HASH="$(printf '%s' "$PREV_BLOCK_JSON" | jq -r '.hash')"
NEXT_BATCH="$(call_find_batch "$((CHECKPOINT_DEC + 1))" 2>/dev/null || printf 'unavailable')"

cat <<EOF
CHECKPOINT_DEC=$CHECKPOINT_DEC
CHECKPOINT_HEX=$CHECKPOINT_HEX
BLOCK_HASH_AT_CHECKPOINT=$BLOCK_HASH
GLOBALSTATE_BLOCK_HASH=$GLOBALSTATE_BLOCK_HASH
SEND_ROOT=$SEND_ROOT
BATCH=$BATCH
POS_IN_BATCH=$POS_IN_BATCH
FIRST_BLOCK_IN_BATCH=$FIRST_BLOCK_IN_BATCH
NEXT_BATCH=$NEXT_BATCH
EOF
