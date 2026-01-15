#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TESTNODE_DIR="${TESTNODE_DIR:-$ROOT_DIR/nitro-testnode}"
COMPOSE_PROJECT_NAME="${COMPOSE_PROJECT_NAME:-nitro-testnode}"
export COMPOSE_PROJECT_NAME

L3_VOLUME="${L3_VOLUME:-${COMPOSE_PROJECT_NAME}_validator-data}"

TEST_ROOT="${TEST_ROOT:-$ROOT_DIR/.mdbx-arbitrum-tx-matrix}"
PEBBLE_DIR="${PEBBLE_DIR:-$TEST_ROOT/pebble-l3}"
MDBX_DIR="${MDBX_DIR:-$TEST_ROOT/mdbx-l3}"

MDBX_MIGRATE_BIN="${MDBX_MIGRATE_BIN:-$ROOT_DIR/target/bin/mdbx-migrate}"

# Override as needed (examples):
# TESTNODE_INIT_ARGS="--init-force --dev --detach --no-build"
TESTNODE_INIT_ARGS="${TESTNODE_INIT_ARGS:---init-force --dev --detach}"
ENABLE_ARCHIVE="${ENABLE_ARCHIVE:-1}"
MDBX_CHAIN_DIR="${MDBX_CHAIN_DIR:-local/nitro}"

ENABLE_L3="${ENABLE_L3:-1}"
ENABLE_DAC="${ENABLE_DAC:-1}"
ENABLE_L1_L2_TOKEN_BRIDGE="${ENABLE_L1_L2_TOKEN_BRIDGE:-0}"
ENABLE_L3_TOKEN_BRIDGE="${ENABLE_L3_TOKEN_BRIDGE:-0}"
ENABLE_L3_FEE_TOKEN="${ENABLE_L3_FEE_TOKEN:-0}"
L3_FEE_TOKEN_DECIMALS="${L3_FEE_TOKEN_DECIMALS:-18}"
REDUNDANT_SEQUENCERS="${REDUNDANT_SEQUENCERS:-1}"
BATCHPOSTERS="${BATCHPOSTERS:-1}"
# validator and l3node share validator-data volume in docker-compose; avoid enabling both without an override.
ENABLE_VALIDATOR="${ENABLE_VALIDATOR:-0}"
ALLOW_SHARED_VALIDATOR_VOLUME="${ALLOW_SHARED_VALIDATOR_VOLUME:-0}"
# forwarder is mapped to the relay service in nitro-testnode/docker-compose.yaml.
ENABLE_FORWARDER="${ENABLE_FORWARDER:-1}"

NITRO_DOCKER_BUILD_ARGS="${NITRO_DOCKER_BUILD_ARGS:-}"
if [[ "$NITRO_DOCKER_BUILD_ARGS" != *"SKIP_FORGE_YUL="* ]]; then
  NITRO_DOCKER_BUILD_ARGS="$NITRO_DOCKER_BUILD_ARGS --build-arg SKIP_FORGE_YUL=1"
fi
if [[ "$NITRO_DOCKER_BUILD_ARGS" != *"GO_BUILD_TAGS="* ]]; then
  NITRO_DOCKER_BUILD_ARGS="$NITRO_DOCKER_BUILD_ARGS --build-arg GO_BUILD_TAGS=erigon"
fi
export NITRO_DOCKER_BUILD_ARGS
export SKIP_FORGE_YUL="${SKIP_FORGE_YUL:-0}"

L1_RPC_URL="${L1_RPC_URL:-http://geth:8545}"
L2_RPC_URL="${L2_RPC_URL:-http://sequencer:8547}"
L3_RPC_URL="${L3_RPC_URL:-http://l3node:3347}"
L3_GAS_LIMIT="${L3_GAS_LIMIT:-100000}"
WAIT_FOR_SYNC_RETRIES="${WAIT_FOR_SYNC_RETRIES:-0}"
WAIT_FOR_SYNC_SLEEP="${WAIT_FOR_SYNC_SLEEP:-2}"

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

compose() {
  (cd "$TESTNODE_DIR" && docker compose "$@")
}

ensure_mdbx_migrate() {
  if [ -x "$MDBX_MIGRATE_BIN" ] && [ "${MDBX_MIGRATE_REBUILD:-}" != "1" ]; then
    return
  fi
  mkdir -p "$(dirname "$MDBX_MIGRATE_BIN")"
  (cd "$ROOT_DIR" && go build -tags erigon -o "$MDBX_MIGRATE_BIN" ./cmd/mdbx-migrate)
}

export_volume() {
  local volume_name="$1"
  local out_dir="$2"
  rm -rf "$out_dir"
  mkdir -p "$out_dir"
  docker run --rm \
    -v "$volume_name":/data \
    -v "$out_dir":/out \
    alpine:3.19 \
    sh -c "tar -C /data -cf - . | tar -C /out -xf -"
}

import_volume() {
  local volume_name="$1"
  local in_dir="$2"
  docker run --rm \
    -v "$volume_name":/data \
    -v "$in_dir":/in \
    alpine:3.19 \
    sh -c "rm -rf /data/* && tar -C /in -cf - . | tar -C /data -xf - && chown -R 1000:1000 /data"
}

update_config_for_mdbx() {
  local config_name="$1"
  compose run --rm --entrypoint sh sequencer -c \
    "jq --arg chain \"$MDBX_CHAIN_DIR\" '.execution.backend=\"erigon\" | .persistent[\"db-engine\"]=\"mdbx\" | .persistent[\"chain\"]=\$chain | .graphql.enable=false' \
    /config/$config_name > /config/$config_name.tmp && \
    mv -f /config/$config_name.tmp /config/$config_name"
  if [ "$config_name" = "l3node_config.json" ]; then
    compose run --rm --entrypoint sh sequencer -c \
      "jq 'map(.\"has-genesis-state\" = true)' /config/l3_chain_info.json > /config/l3_chain_info.json.tmp && \
      mv -f /config/l3_chain_info.json.tmp /config/l3_chain_info.json"
  fi
}

rpc_call() {
  local method="$1"
  local params="${2:-[]}"
  local url="$3"
  local output
  output="$(compose run --rm scripts send-rpc --url "$url" --method "$method" --params "$params")"
  echo "$output" | tail -n 1
}

wait_for_sync() {
  local url="$1"
  local retries="$WAIT_FOR_SYNC_RETRIES"
  local delay="$WAIT_FOR_SYNC_SLEEP"
  local attempt=1

  while true; do
    if compose run --rm scripts wait-for-sync --url "$url"; then
      return 0
    fi
    if [ "$retries" -gt 0 ] && [ "$attempt" -ge "$retries" ]; then
      echo "wait-for-sync failed after $attempt attempts: $url" >&2
      return 1
    fi
    echo "wait-for-sync failed (attempt $attempt), retrying in ${delay}s: $url" >&2
    attempt=$((attempt + 1))
    sleep "$delay"
  done
}

append_arg_if_missing() {
  local args="$1"
  local want="$2"
  case " $args " in
    *" $want "*) echo "$args" ;;
    *) echo "$args $want" ;;
  esac
}

append_kv_arg_if_missing() {
  local args="$1"
  local key="$2"
  local value="$3"
  case " $args " in
    *" $key "*) echo "$args" ;;
    *) echo "$args $key $value" ;;
  esac
}

start_chain() {
  local args="$TESTNODE_INIT_ARGS"

  if [ "$ENABLE_L3" = "1" ]; then
    args="$(append_arg_if_missing "$args" "--l3node")"
  fi
  if [ "$ENABLE_DAC" = "1" ]; then
    args="$(append_arg_if_missing "$args" "--l2-anytrust")"
  fi
  if [ "$ENABLE_L1_L2_TOKEN_BRIDGE" = "1" ]; then
    args="$(append_arg_if_missing "$args" "--tokenbridge")"
  fi
  if [ "$ENABLE_L3_TOKEN_BRIDGE" = "1" ]; then
    args="$(append_arg_if_missing "$args" "--l3-token-bridge")"
  fi
  if [ "$ENABLE_L3_FEE_TOKEN" = "1" ]; then
    args="$(append_arg_if_missing "$args" "--l3-fee-token")"
    args="$(append_kv_arg_if_missing "$args" "--l3-fee-token-decimals" "$L3_FEE_TOKEN_DECIMALS")"
  fi
  if [ "$BATCHPOSTERS" -gt 0 ]; then
    args="$(append_kv_arg_if_missing "$args" "--batchposters" "$BATCHPOSTERS")"
  fi
  if [ "$REDUNDANT_SEQUENCERS" -gt 0 ]; then
    args="$(append_kv_arg_if_missing "$args" "--redundantsequencers" "$REDUNDANT_SEQUENCERS")"
  fi
  if [ "$ENABLE_VALIDATOR" = "1" ]; then
    args="$(append_arg_if_missing "$args" "--validate")"
  fi
  if [ "$ENABLE_ARCHIVE" = "1" ]; then
    args="$(append_arg_if_missing "$args" "--archive")"
  fi

  (cd "$TESTNODE_DIR" && ./test-node.bash $args)
}

get_l3_inbox_address() {
  compose run --rm --entrypoint sh rollupcreator -c \
    "jq -r '.\"sequencer-inbox\"' /config/l3deployment.json"
}

get_l2_rollup_address() {
  compose run --rm --entrypoint sh sequencer -c \
    "jq -r '.[0].rollup.rollup' /config/l2_chain_info.json"
}

get_l3_rollup_address() {
  compose run --rm --entrypoint sh sequencer -c \
    "jq -r '.[0].rollup.rollup' /config/l3_chain_info.json"
}

wait_for_l3_batch() {
  local from_block="$1"
  local inbox_addr="$2"
  local attempts=30
  local logs
  while [ "$attempts" -gt 0 ]; do
    logs="$(rpc_call eth_getLogs "[{\"fromBlock\":\"$from_block\",\"toBlock\":\"latest\",\"address\":\"$inbox_addr\"}]" "$L2_RPC_URL")"
    if [ "$logs" != "[]" ] && [ -n "$logs" ]; then
      return 0
    fi
    attempts=$((attempts - 1))
    sleep 2
  done
  echo "warning: no L3 batch logs found on L2" >&2
  return 0
}

run_l1_l2_suite() {
  echo "== L1 -> L2 suite"
  compose run --rm scripts bridge-funds --ethamount 1 --wait
}

run_l2_suite() {
  echo "== L2 suite"
  compose run --rm scripts send-l2 --ethamount 1 --from l2owner --to user_traffic_generator --wait
  compose run --rm scripts send-l2 --ethamount 0.01 --from l2owner --to user_traffic_generator --data 0xdeadbeef --wait

  local token_addr
  token_addr="$(compose run --rm scripts create-erc20 --deployer user_token_bridge_deployer --decimals 18 | tail -n 1 | awk '{ print $NF }')"
  compose run --rm scripts transfer-erc20 --token "$token_addr" --amount 100 --from user_token_bridge_deployer --to l2owner
}

run_l2_to_l3_suite() {
  echo "== L2 -> L3 suite"
  if [ "$ENABLE_L3_FEE_TOKEN" = "1" ]; then
    compose run --rm scripts bridge-native-token-to-l3 --amount 10 --from user_fee_token_deployer --wait
  else
    compose run --rm scripts bridge-to-l3 --ethamount 1 --from l2owner --wait
  fi
}

run_l3_suite() {
  echo "== L3 suite"
  compose run --rm scripts send-l3 --ethamount 1 --from l3owner --to l3sequencer --wait --gas-limit "$L3_GAS_LIMIT"
  compose run --rm scripts send-l3 --ethamount 0.01 --from l3sequencer --to l3owner --data 0xdeadbeef --wait --gas-limit "$L3_GAS_LIMIT"
}

verify_rollup_contracts() {
  local l2_rollup_addr
  local l3_rollup_addr
  l2_rollup_addr="$(get_l2_rollup_address)"
  l3_rollup_addr="$(get_l3_rollup_address)"

  if [ -z "$l2_rollup_addr" ] || [ "$l2_rollup_addr" = "null" ]; then
    echo "missing L2 rollup address" >&2
    exit 1
  fi
  if [ -z "$l3_rollup_addr" ] || [ "$l3_rollup_addr" = "null" ]; then
    echo "missing L3 rollup address" >&2
    exit 1
  fi

  local l2_rollup_code
  local l3_rollup_code
  l2_rollup_code="$(rpc_call eth_getCode "[\"$l2_rollup_addr\", \"latest\"]" "$L1_RPC_URL")"
  l3_rollup_code="$(rpc_call eth_getCode "[\"$l3_rollup_addr\", \"latest\"]" "$L2_RPC_URL")"

  if [ "$l2_rollup_code" = "0x" ] || [ -z "$l2_rollup_code" ]; then
    echo "L2 rollup contract has no code on L1" >&2
    exit 1
  fi
  if [ "$l3_rollup_code" = "0x" ] || [ -z "$l3_rollup_code" ]; then
    echo "L3 rollup contract has no code on L2" >&2
    exit 1
  fi
}

require_cmd docker
require_cmd go

RUN_VALIDATOR_AFTER=0
if [ "$ENABLE_VALIDATOR" = "1" ] && [ "$ENABLE_L3" = "1" ] && [ "$ALLOW_SHARED_VALIDATOR_VOLUME" != "1" ]; then
  RUN_VALIDATOR_AFTER=1
fi

echo "== Step 1: start chain (pebble, L2+L3)"
start_chain
wait_for_sync "$L2_RPC_URL"
wait_for_sync "$L3_RPC_URL"
if [ "$ENABLE_FORWARDER" = "1" ]; then
  compose up -d relay
fi

echo "== Step 2: verify rollup deployments"
verify_rollup_contracts

if [ "$ENABLE_DAC" = "1" ]; then
  compose run --rm --entrypoint sh sequencer -c "test -s /config/l2_das_keyset.hex"
fi

echo "== Step 3: transaction matrix (pebble)"
run_l1_l2_suite
run_l2_suite
run_l2_to_l3_suite

L3_OWNER_ADDRESS="$(compose run --rm scripts print-address --account l3owner | tail -n 1)"
L3_BLOCK_BEFORE="$(rpc_call eth_blockNumber "[]" "$L3_RPC_URL")"
L3_BALANCE_BEFORE="$(rpc_call eth_getBalance "[\"$L3_OWNER_ADDRESS\", \"$L3_BLOCK_BEFORE\"]" "$L3_RPC_URL")"

L2_BLOCK_BEFORE="$(rpc_call eth_blockNumber "[]" "$L2_RPC_URL")"
L3_INBOX_ADDR="$(get_l3_inbox_address)"
run_l3_suite
wait_for_l3_batch "$L2_BLOCK_BEFORE" "$L3_INBOX_ADDR"

echo "== Step 4: stop containers"
compose down

echo "== Step 5: export pebble data to host (L3)"
export_volume "$L3_VOLUME" "$PEBBLE_DIR"

echo "== Step 6: migrate pebble -> mdbx (full, L3)"
ensure_mdbx_migrate
rm -rf "$MDBX_DIR"
mkdir -p "$MDBX_DIR"
"$MDBX_MIGRATE_BIN" \
  --source "$PEBBLE_DIR" \
  --dest "$MDBX_DIR" \
  --mode full \
  --verify extended \
  --verify-samples 10

echo "== Step 7: import mdbx data back into L3 volume"
import_volume "$L3_VOLUME" "$MDBX_DIR"

echo "== Step 8: update l3node config for erigon + mdbx"
update_config_for_mdbx "l3node_config.json"

echo "== Step 9: restart L2+L3 with erigon backend"
NODES="geth redis sequencer validation_node l3node"
if [ "$ENABLE_VALIDATOR" = "1" ] && [ "$RUN_VALIDATOR_AFTER" -eq 0 ]; then
  NODES="$NODES validator"
fi
if [ "$BATCHPOSTERS" -gt 0 ]; then
  NODES="$NODES poster"
fi
if [ "$BATCHPOSTERS" -gt 1 ]; then
  NODES="$NODES poster_b"
fi
if [ "$BATCHPOSTERS" -gt 2 ]; then
  NODES="$NODES poster_c"
fi
if [ "$REDUNDANT_SEQUENCERS" -gt 0 ]; then
  NODES="$NODES sequencer_b"
fi
if [ "$REDUNDANT_SEQUENCERS" -gt 1 ]; then
  NODES="$NODES sequencer_c"
fi
if [ "$REDUNDANT_SEQUENCERS" -gt 2 ]; then
  NODES="$NODES sequencer_d"
fi
if [ "$ENABLE_DAC" = "1" ]; then
  NODES="$NODES das-committee-a das-committee-b das-mirror"
fi
if [ "$ENABLE_FORWARDER" = "1" ]; then
  NODES="$NODES relay"
fi
compose up -d $NODES
compose up --wait redis
compose run --rm scripts redis-init --redundancy "$REDUNDANT_SEQUENCERS"
wait_for_sync "$L2_RPC_URL"
wait_for_sync "$L3_RPC_URL"

echo "== Step 10: transaction matrix (mdbx)"
run_l1_l2_suite
run_l2_suite
run_l2_to_l3_suite

L2_BLOCK_AFTER="$(rpc_call eth_blockNumber "[]" "$L2_RPC_URL")"
run_l3_suite
wait_for_l3_batch "$L2_BLOCK_AFTER" "$L3_INBOX_ADDR"

echo "== Step 11: verify historical data after migration"
L3_BALANCE_AFTER="$(rpc_call eth_getBalance "[\"$L3_OWNER_ADDRESS\", \"$L3_BLOCK_BEFORE\"]" "$L3_RPC_URL")"
if [ -z "$L3_BALANCE_AFTER" ]; then
  echo "failed to read historical balance after migration" >&2
  exit 1
fi
if [ "$L3_BALANCE_BEFORE" != "$L3_BALANCE_AFTER" ]; then
  echo "historical balance mismatch: before=$L3_BALANCE_BEFORE after=$L3_BALANCE_AFTER" >&2
  exit 1
fi

if [ "$RUN_VALIDATOR_AFTER" -eq 1 ]; then
  echo "== Step 12: validator verification (separate phase)"
  compose stop l3node
  compose up -d validator
  wait_for_sync "http://validator:8547"
fi

echo "== Done: L3 MDBX migration + arbitrum tx matrix exercised"
