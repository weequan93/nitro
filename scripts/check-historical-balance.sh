#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  scripts/check-historical-balance.sh --rpc URL

Optional:
  --trusted-rpc URL        Compare results against another RPC
  --addr ADDRESS           Address to query; auto-detects from latest block if omitted
  --blocks BLOCK[,BLOCK]   Blocks to test; auto-picks migration/recent/latest if omitted
  --all-blocks             Test every block from --start-block to --end-block
  --start-block BLOCK      First block for --all-blocks; defaults to --migration-block
  --end-block BLOCK        Last block for --all-blocks; defaults to latest
  --step N                 Block interval for --all-blocks; defaults to 1
  --progress-every N       Print progress every N checked blocks; defaults to 1000
  --continue-on-mismatch   Continue scanning after mismatches
  --migration-block BLOCK  Migration block used in automatic block list

Examples:
  scripts/check-historical-balance.sh \
    --rpc http://127.0.0.1:8547 \
    --trusted-rpc https://rpc.dev.deriw.com

  scripts/check-historical-balance.sh \
    --rpc http://127.0.0.1:8547 \
    --trusted-rpc https://rpc.dev.deriw.com \
    --addr 0xYourAddress \
    --blocks 115000000,116000000,latest

  scripts/check-historical-balance.sh \
    --rpc http://127.0.0.1:8547 \
    --trusted-rpc https://rpc.dev.deriw.com \
    --addr 0xYourAddress \
    --all-blocks \
    --start-block 115000000
EOF
}

rpc=""
trusted_rpc=""
addr=""
blocks=""
migration_block="${MIGRATION_BLOCK:-115000000}"
all_blocks=false
start_block=""
end_block="latest"
step=1
progress_every=1000
continue_on_mismatch=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --rpc)
      rpc="${2:-}"
      shift 2
      ;;
    --trusted-rpc)
      trusted_rpc="${2:-}"
      shift 2
      ;;
    --addr)
      addr="${2:-}"
      shift 2
      ;;
    --blocks)
      blocks="${2:-}"
      shift 2
      ;;
    --all-blocks)
      all_blocks=true
      shift
      ;;
    --start-block)
      start_block="${2:-}"
      shift 2
      ;;
    --end-block)
      end_block="${2:-}"
      shift 2
      ;;
    --step)
      step="${2:-}"
      shift 2
      ;;
    --progress-every)
      progress_every="${2:-}"
      shift 2
      ;;
    --continue-on-mismatch)
      continue_on_mismatch=true
      shift
      ;;
    --migration-block)
      migration_block="${2:-}"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [[ -z "$rpc" ]]; then
  usage >&2
  exit 2
fi

if ! command -v curl >/dev/null 2>&1; then
  echo "curl is required" >&2
  exit 2
fi

block_to_rpc_tag() {
  local block="$1"
  case "$block" in
    latest|earliest|pending|safe|finalized)
      printf '%s' "$block"
      ;;
    0x*)
      printf '%s' "$block"
      ;;
    ''|*[!0-9]*)
      echo "Invalid block: $block" >&2
      exit 2
      ;;
    *)
      printf '0x%x' "$block"
      ;;
  esac
}

require_decimal() {
  local name="$1"
  local value="$2"
  if [[ -z "$value" || "$value" == *[!0-9]* ]]; then
    echo "$name must be a decimal integer, got: $value" >&2
    exit 2
  fi
}

rpc_call() {
  local url="$1"
  local payload="$2"
  curl -sS "$url" \
    -H 'content-type: application/json' \
    --data "$payload"
}

hex_to_dec() {
  local hex="${1#0x}"
  printf '%d' "0x$hex"
}

json_get_result_or_error() {
  local response="$1"
  if [[ "$response" == *'"error"'* ]]; then
    printf 'ERROR %s' "$response"
    return 1
  fi
  local result
  result="$(printf '%s' "$response" | sed -n 's/.*"result"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"
  if [[ -z "$result" ]]; then
    printf 'ERROR unexpected response: %s' "$response"
    return 1
  fi
  printf '%s' "$result"
}

get_latest_block_number() {
  local url="$1"
  local payload
  local latest_hex
  payload='{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]}'
  latest_hex="$(json_get_result_or_error "$(rpc_call "$url" "$payload")")"
  hex_to_dec "$latest_hex"
}

resolve_block_number() {
  local url="$1"
  local block="$2"
  case "$block" in
    latest)
      get_latest_block_number "$url"
      ;;
    0x*)
      hex_to_dec "$block"
      ;;
    ''|*[!0-9]*)
      echo "Invalid block number: $block" >&2
      exit 2
      ;;
    *)
      printf '%s' "$block"
      ;;
  esac
}

unique_csv_blocks() {
  local item
  local out=""
  local seen=","
  for item in "$@"; do
    [[ -z "$item" ]] && continue
    if [[ "$seen" != *",$item,"* ]]; then
      out="${out:+$out,}$item"
      seen="$seen$item,"
    fi
  done
  printf '%s' "$out"
}

auto_blocks() {
  local latest
  local candidates=()
  latest="$(get_latest_block_number "$rpc")"

  if [[ "$migration_block" != *[!0-9]* && "$migration_block" -le "$latest" ]]; then
    candidates+=("$migration_block")
  fi
  if (( latest > 100000 )); then
    candidates+=("$((latest - 100000))")
  fi
  if (( latest > 1000 )); then
    candidates+=("$((latest - 1000))")
  fi
  candidates+=("latest")

  unique_csv_blocks "${candidates[@]}"
}

extract_first_address() {
  sed -n 's/.*"\(from\|miner\)"[[:space:]]*:[[:space:]]*"\(0x[0-9a-fA-F]\{40\}\)".*/\2/p' | head -n 1
}

auto_addr() {
  local payload
  local response
  local found

  payload='{"jsonrpc":"2.0","id":1,"method":"eth_getBlockByNumber","params":["latest",true]}'
  response="$(rpc_call "$rpc" "$payload")"
  found="$(printf '%s' "$response" | extract_first_address || true)"
  if [[ -n "$found" ]]; then
    printf '%s' "$found"
    return
  fi

  payload='{"jsonrpc":"2.0","id":1,"method":"eth_getBlockByNumber","params":["latest",false]}'
  response="$(rpc_call "$rpc" "$payload")"
  found="$(printf '%s' "$response" | extract_first_address || true)"
  if [[ -n "$found" ]]; then
    printf '%s' "$found"
    return
  fi

  printf '0x0000000000000000000000000000000000000000'
}

get_balance() {
  local url="$1"
  local block_tag="$2"
  local payload
  payload='{"jsonrpc":"2.0","id":1,"method":"eth_getBalance","params":["'"$addr"'","'"$block_tag"'"]}'
  json_get_result_or_error "$(rpc_call "$url" "$payload")"
}

get_block_header() {
  local url="$1"
  local block_tag="$2"
  local payload
  local response
  payload='{"jsonrpc":"2.0","id":1,"method":"eth_getBlockByNumber","params":["'"$block_tag"'",false]}'
  response="$(rpc_call "$url" "$payload")"
  if [[ "$response" == *'"error"'* ]]; then
    printf 'ERROR %s' "$response"
    return 1
  fi
  if [[ "$response" == *'"result":null'* || "$response" == *'"result": null'* ]]; then
    printf 'ERROR block not found: %s' "$response"
    return 1
  fi
  printf 'ok'
}

check_one_block() {
  local block="$1"
  local quiet="${2:-false}"
  local block_tag
  local balance
  local trusted_balance

  block_tag="$(block_to_rpc_tag "$block")"

  if [[ "$quiet" != true ]]; then
    printf 'block=%s tag=%s\n' "$block" "$block_tag"
  fi

  if ! balance="$(get_balance "$rpc" "$block_tag")"; then
    printf '  block=%s local balance: %s\n' "$block" "$balance"
    return 1
  fi

  if [[ -n "$trusted_rpc" ]]; then
    if ! trusted_balance="$(get_balance "$trusted_rpc" "$block_tag")"; then
      printf '  block=%s trusted balance: %s\n' "$block" "$trusted_balance"
      return 1
    fi

    if [[ "$balance" != "$trusted_balance" ]]; then
      printf '  block=%s compare: MISMATCH local=%s trusted=%s\n' \
        "$block" "$balance" "$trusted_balance"
      return 1
    fi
  fi

  if [[ "$quiet" != true ]]; then
    printf '  local balance: %s\n' "$balance"
    if [[ -n "$trusted_rpc" ]]; then
      printf '  trusted balance: %s\n' "$trusted_balance"
      printf '  compare: MATCH\n'
    fi
    printf '\n'
  fi

  return 0
}

status=0

if [[ "$all_blocks" == true && -n "$blocks" ]]; then
  echo "--all-blocks cannot be used together with --blocks" >&2
  exit 2
fi

require_decimal "--step" "$step"
require_decimal "--progress-every" "$progress_every"
if (( step < 1 )); then
  echo "--step must be >= 1" >&2
  exit 2
fi
if (( progress_every < 1 )); then
  echo "--progress-every must be >= 1" >&2
  exit 2
fi

if [[ "$all_blocks" != true && -z "$blocks" ]]; then
  blocks="$(auto_blocks)"
fi

if [[ -z "$addr" ]]; then
  addr="$(auto_addr)"
fi

IFS=',' read -r -a block_list <<< "$blocks"

printf 'RPC: %s\n' "$rpc"
if [[ -n "$trusted_rpc" ]]; then
  printf 'Trusted RPC: %s\n' "$trusted_rpc"
fi
printf 'Address: %s\n\n' "$addr"

if [[ "$all_blocks" == true ]]; then
  if [[ -z "$trusted_rpc" ]]; then
    echo "--all-blocks requires --trusted-rpc so results can be compared" >&2
    exit 2
  fi
  if [[ -z "$start_block" ]]; then
    start_block="$migration_block"
  fi

  start_dec="$(resolve_block_number "$rpc" "$start_block")"
  end_dec="$(resolve_block_number "$rpc" "$end_block")"

  if [[ "$end_block" == "latest" ]]; then
    trusted_latest="$(get_latest_block_number "$trusted_rpc")"
    if (( trusted_latest < end_dec )); then
      end_dec="$trusted_latest"
    fi
  fi

  if (( start_dec > end_dec )); then
    echo "start block $start_dec is greater than end block $end_dec" >&2
    exit 2
  fi

  printf 'Mode: all-blocks\n'
  printf 'Range: %s..%s step=%s\n' "$start_dec" "$end_dec" "$step"
  printf 'Stop on mismatch: %s\n\n' "$([[ "$continue_on_mismatch" == true ]] && echo false || echo true)"

  checked=0
  mismatches=0
  started_at="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
  printf 'Started: %s\n' "$started_at"

  block="$start_dec"
  while (( block <= end_dec )); do
    checked=$((checked + 1))
    if ! check_one_block "$block" true; then
      mismatches=$((mismatches + 1))
      status=1
      if [[ "$continue_on_mismatch" != true ]]; then
        printf 'Stopped at first failure. checked=%s mismatches=%s\n' "$checked" "$mismatches"
        exit "$status"
      fi
    fi

    if (( checked % progress_every == 0 )); then
      printf 'progress checked=%s current_block=%s mismatches=%s\n' \
        "$checked" "$block" "$mismatches"
    fi

    block=$((block + step))
  done

  printf 'Finished checked=%s mismatches=%s\n' "$checked" "$mismatches"
  exit "$status"
fi

IFS=',' read -r -a block_list <<< "$blocks"

for block in "${block_list[@]}"; do
  block="${block//[[:space:]]/}"
  block_tag="$(block_to_rpc_tag "$block")"

  printf 'block=%s tag=%s\n' "$block" "$block_tag"

  if ! header="$(get_block_header "$rpc" "$block_tag")"; then
    printf '  local block: %s\n\n' "$header"
    status=1
    continue
  fi
  printf '  local block: ok\n'

  if ! balance="$(get_balance "$rpc" "$block_tag")"; then
    printf '  local balance: %s\n\n' "$balance"
    status=1
    continue
  fi
  printf '  local balance: %s\n' "$balance"

  if [[ -n "$trusted_rpc" ]]; then
    if ! trusted_balance="$(get_balance "$trusted_rpc" "$block_tag")"; then
      printf '  trusted balance: %s\n\n' "$trusted_balance"
      status=1
      continue
    fi
    printf '  trusted balance: %s\n' "$trusted_balance"

    if [[ "$balance" == "$trusted_balance" ]]; then
      printf '  compare: MATCH\n'
    else
      printf '  compare: MISMATCH\n'
      status=1
    fi
  fi

  printf '\n'
done

exit "$status"
