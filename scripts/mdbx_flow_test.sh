#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TESTNODE_DIR="${TESTNODE_DIR:-$ROOT_DIR/nitro-testnode}"
COMPOSE_PROJECT_NAME="${COMPOSE_PROJECT_NAME:-nitro-testnode}"
L3_VOLUME="${L3_VOLUME:-${COMPOSE_PROJECT_NAME}_validator-data}"

TEST_ROOT="${TEST_ROOT:-$ROOT_DIR/.mdbx-flow-test}"
PEBBLE_DIR="${PEBBLE_DIR:-$TEST_ROOT/pebble-l3}"
MDBX_DIR="${MDBX_DIR:-$TEST_ROOT/mdbx-l3}"

MDBX_MIGRATE_BIN="${MDBX_MIGRATE_BIN:-$ROOT_DIR/target/bin/mdbx-migrate}"

# Override as needed (examples):
# TESTNODE_INIT_ARGS="--init-force --dev --detach --no-build"
TESTNODE_INIT_ARGS="${TESTNODE_INIT_ARGS:---init-force --dev --detach}"
ENABLE_ARCHIVE="${ENABLE_ARCHIVE:-1}"
MDBX_CHAIN_DIR="${MDBX_CHAIN_DIR:-local/nitro}"
L3_DISABLE_VALIDATOR="${L3_DISABLE_VALIDATOR:-1}"

NITRO_DOCKER_BUILD_ARGS="${NITRO_DOCKER_BUILD_ARGS:-}"
if [[ "$NITRO_DOCKER_BUILD_ARGS" != *"SKIP_FORGE_YUL="* ]]; then
  NITRO_DOCKER_BUILD_ARGS="$NITRO_DOCKER_BUILD_ARGS --build-arg SKIP_FORGE_YUL=1"
fi
if [[ "$NITRO_DOCKER_BUILD_ARGS" != *"GO_BUILD_TAGS="* ]]; then
  NITRO_DOCKER_BUILD_ARGS="$NITRO_DOCKER_BUILD_ARGS --build-arg GO_BUILD_TAGS=erigon"
fi
export NITRO_DOCKER_BUILD_ARGS
export SKIP_FORGE_YUL="${SKIP_FORGE_YUL:-0}"

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
    "jq --arg chain \"$MDBX_CHAIN_DIR\" --arg config_name \"$config_name\" --argjson disable_validator \"$L3_DISABLE_VALIDATOR\" \
    '.execution.backend=\"erigon\" | .persistent[\"db-engine\"]=\"mdbx\" | .persistent[\"chain\"]=\$chain | .graphql.enable=false | (if \$disable_validator == 1 and \$config_name == \"l3node_config.json\" then .node.staker.dangerous[\"without-block-validator\"]=true else . end)' \
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
  local output
  output="$(compose run --rm scripts send-rpc --url "$L3_RPC_URL" --method "$method" --params "$params")"
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

require_cmd docker
require_cmd go

append_arg_if_missing() {
  local args="$1"
  local want="$2"
  case " $args " in
    *" $want "*) echo "$args" ;;
    *) echo "$args $want" ;;
  esac
}

start_chain() {
  local args="$TESTNODE_INIT_ARGS"
  args="$(append_arg_if_missing "$args" "--l3node")"
  if [ "$ENABLE_ARCHIVE" = "1" ]; then
    args="$(append_arg_if_missing "$args" "--archive")"
  fi
  (cd "$TESTNODE_DIR" && ./test-node.bash $args)
}

echo "== Step 1: start fresh chain (pebble, L3)"
start_chain
wait_for_sync "$L3_RPC_URL"
compose run --rm scripts send-l3 --ethamount 0.01 --to l3owner --wait --gas-limit "$L3_GAS_LIMIT"

echo "== Step 1b: capture historical reference (L3)"
L3_OWNER_ADDRESS="$(compose run --rm scripts print-address --account l3owner | tail -n 1)"
BLOCK_BEFORE="$(rpc_call eth_blockNumber)"
BALANCE_BEFORE="$(rpc_call eth_getBalance "[\"$L3_OWNER_ADDRESS\", \"$BLOCK_BEFORE\"]")"
if [ -z "$BLOCK_BEFORE" ] || [ -z "$BALANCE_BEFORE" ]; then
  echo "failed to capture historical reference data" >&2
  exit 1
fi

echo "== Step 2: stop containers"
cleanup_oneoff() {
  local ids
  ids="$(docker ps -aq --filter "label=com.docker.compose.project=$COMPOSE_PROJECT_NAME" --filter "label=com.docker.compose.oneoff=True")"
  if [ -n "$ids" ]; then
    docker rm -f $ids >/dev/null
  fi
}
cleanup_oneoff
compose down --remove-orphans

echo "== Step 3: export pebble data to host (L3)"
export_volume "$L3_VOLUME" "$PEBBLE_DIR"

echo "== Step 4: migrate pebble -> mdbx (full, L3)"
ensure_mdbx_migrate
rm -rf "$MDBX_DIR"
mkdir -p "$MDBX_DIR"
"$MDBX_MIGRATE_BIN" \
  --source "$PEBBLE_DIR" \
  --dest "$MDBX_DIR" \
  --mode full \
  --verify extended \
  --verify-samples 10

echo "== Step 5: import mdbx data back into L3 volume"
import_volume "$L3_VOLUME" "$MDBX_DIR"

echo "== Step 6: update l3node config for erigon + mdbx"
update_config_for_mdbx "l3node_config.json"

echo "== Step 7: restart L3 with erigon backend"
compose up -d geth redis sequencer validation_node l3node
compose up --wait redis
compose run --rm scripts redis-init
wait_for_sync "$L3_RPC_URL"
compose run --rm scripts send-l3 --ethamount 0.01 --to l3owner --wait --gas-limit "$L3_GAS_LIMIT"

echo "== Step 8: verify historical data after migration"
BALANCE_AFTER="$(rpc_call eth_getBalance "[\"$L3_OWNER_ADDRESS\", \"$BLOCK_BEFORE\"]")"
if [ -z "$BALANCE_AFTER" ]; then
  echo "failed to read historical balance after migration" >&2
  exit 1
fi
if [ "$BALANCE_BEFORE" != "$BALANCE_AFTER" ]; then
  echo "historical balance mismatch: before=$BALANCE_BEFORE after=$BALANCE_AFTER" >&2
  exit 1
fi

echo "== Done: L3 pebble -> mdbx migration flow exercised"
