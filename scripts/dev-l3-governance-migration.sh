#!/usr/bin/env bash

set -euo pipefail

readonly DEFAULT_RPC_URL="https://rpc.dev.deriw.com"
readonly DEV_CHAIN_ID="18417507517"
readonly UPGRADE_EXECUTOR="0xB5B4d7f7a32D86fF3bc270B864c7c06CE6F0BD78"
readonly EXECUTOR_ROLE="0xd8aa0f3194971a2a116679f7c2090f6939c8d4e01a2a8d7e41d55e5351469e63"
readonly ARB_OWNER="0x0000000000000000000000000000000000000070"
readonly ARB_OWNER_PUBLIC="0x000000000000000000000000000000000000006b"
readonly BLACKLIST_PUBLIC="0x00000000000000000000000000000000000007EB"

readonly SAFE_A="0x5f1B197A82fC1148A02Ea55B3BEF529f78D64151"
readonly SAFE_B="0x9Caa16915f33F5A122351d828B07F8758a53bdEa"

readonly CHAIN_OWNER_57="0x57F93d0dFa75206f61F2BcD41Cb61c499d48Fe17"
readonly CHAIN_OWNER_94="0x94A6713cbF5F589aB51570D0b4cd219792421af2"
readonly OLD_EXECUTOR_2C="0x2c57af3d21a13fd30d2bd396b308a6313ad2402e"
readonly OLD_EXECUTOR_9D="0x9D4130d6646Fde37C9EE9485a01E1f2Dd71476DA"

readonly RPC_URL="${L3_RPC_URL:-$DEFAULT_RPC_URL}"
readonly CONFIRMATIONS="${CONFIRMATIONS:-2}"
readonly OUT_DIR="${OUT_DIR:-dev-l3-governance-output}"

usage() {
  cat <<'EOF'
Development L3 governance migration

Usage:
  scripts/dev-l3-governance-migration.sh audit
  scripts/dev-l3-governance-migration.sh grant
  scripts/dev-l3-governance-migration.sh grant --broadcast
  SAFE_ADDRESS=0x... PROPOSER_ADDRESS=0x... \
    scripts/dev-l3-governance-migration.sh generate-test
  SAFE_ADDRESS=0x... PROPOSER_ADDRESS=0x... SAFE_TESTS_CONFIRMED=yes \
    scripts/dev-l3-governance-migration.sh generate-cleanup
  scripts/dev-l3-governance-migration.sh verify-final

Modes:
  audit             Read-only snapshot of chain owners, blacklist owners, and roles.
  grant             Print and simulate both initial Safe role grants. No transaction.
  grant --broadcast Send both grants using CURRENT_L3_EXECUTOR_ACCOUNT.
  generate-test     Write one Safe proposal manifest for a state-neutral path test.
  generate-cleanup  Write the five-call Safe cleanup batch; requires confirmed tests.
  verify-final      Require the approved final owner and executor state.

Environment:
  CURRENT_L3_EXECUTOR_ACCOUNT  Foundry keystore account for grant --broadcast.
  SAFE_ADDRESS                 SAFE_A or SAFE_B for generated manifests.
  PROPOSER_ADDRESS             Registered Safe delegate or Safe owner address.
  PROPOSAL_ORIGIN              Change-ticket description stored in the manifest.
  OUT_DIR                      Manifest output directory (default shown above).
  L3_RPC_URL                   Override only with an approved dev L3 RPC.
  CONFIRMATIONS                Confirmations for grant broadcasts (default 2).
  FORCE=1                      Permit overwriting an existing generated manifest.

The script never accepts or reads a raw private key. The retained blacklist owner
0x57F93d0dFa75206f61F2BcD41Cb61c499d48Fe17 is not removed.
EOF
}

die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || die "required command not found: $1"
}

rpc_request() {
  local method="$1"
  local params_json="$2"
  local payload response error_message

  payload="$(jq -cn \
    --arg method "$method" \
    --argjson params "$params_json" \
    '{jsonrpc: "2.0", id: 1, method: $method, params: $params}')"
  response="$(curl -fsS \
    "$RPC_URL" \
    -H 'content-type: application/json' \
    --data "$payload")"

  error_message="$(printf '%s' "$response" | jq -r '.error.message // empty')"
  [ -z "$error_message" ] || die "RPC $method failed: $error_message"
  printf '%s' "$response" | jq -er '.result'
}

rpc_eth_call() {
  local to="$1"
  local data="$2"
  local params

  params="$(jq -cn \
    --arg to "$to" \
    --arg data "$data" \
    '[{to: $to, data: $data}, "latest"]')"
  rpc_request eth_call "$params"
}

rpc_eth_call_from() {
  local from="$1"
  local to="$2"
  local data="$3"
  local params

  params="$(jq -cn \
    --arg from "$from" \
    --arg to "$to" \
    --arg data "$data" \
    '[{from: $from, to: $to, data: $data}, "latest"]')"
  rpc_request eth_call "$params"
}

lower() {
  printf '%s' "$1" | tr '[:upper:]' '[:lower:]'
}

require_dev_chain() {
  local chain_hex chain_id
  chain_hex="$(rpc_request eth_chainId '[]')"
  chain_id="$(cast to-dec "$chain_hex")"
  [ "$chain_id" = "$DEV_CHAIN_ID" ] || die \
    "RPC chain ID is $chain_id; expected development L3 chain ID $DEV_CHAIN_ID"
}

has_executor_role() {
  local data result
  data="$(cast calldata \
    "hasRole(bytes32,address)" \
    "$EXECUTOR_ROLE" \
    "$1")"
  result="$(rpc_eth_call "$UPGRADE_EXECUTOR" "$data")"
  cast abi-decode "hasRole()(bool)" "$result"
}

require_approved_safe() {
  local candidate safe_a_lower safe_b_lower
  candidate="$(lower "$1")"
  safe_a_lower="$(lower "$SAFE_A")"
  safe_b_lower="$(lower "$SAFE_B")"
  if [ "$candidate" != "$safe_a_lower" ] && [ "$candidate" != "$safe_b_lower" ]; then
    die "SAFE_ADDRESS must be $SAFE_A or $SAFE_B"
  fi
}

require_manifest_inputs() {
  [ -n "${SAFE_ADDRESS:-}" ] || die "SAFE_ADDRESS is required"
  [ -n "${PROPOSER_ADDRESS:-}" ] || die "PROPOSER_ADDRESS is required"
  require_approved_safe "$SAFE_ADDRESS"
  require_command jq
}

write_manifest() {
  local output_path="$1"
  if [ -e "$output_path" ] && [ "${FORCE:-0}" != "1" ]; then
    die "$output_path already exists; review it or set FORCE=1 to replace it"
  fi
  mkdir -p "$OUT_DIR"
  jq '.' >"$output_path"
  printf 'Wrote %s\n' "$output_path"
}

audit() {
  local block_hex chain_owners_result blacklist_owners_result
  require_dev_chain

  printf 'RPC: %s\n' "$RPC_URL"
  block_hex="$(rpc_request eth_blockNumber '[]')"
  printf 'Block: %s\n' "$(cast to-dec "$block_hex")"
  chain_owners_result="$(rpc_eth_call "$ARB_OWNER_PUBLIC" "0x516b4e0f")"
  printf 'Chain owners: %s\n' \
    "$(cast abi-decode "getAllChainOwners()(address[])" "$chain_owners_result")"
  blacklist_owners_result="$(rpc_eth_call "$BLACKLIST_PUBLIC" "0xc3005d68")"
  printf 'Blacklist owners: %s\n' \
    "$(cast abi-decode "getAllBlacklistOwners()(address[])" "$blacklist_owners_result")"

  printf '\nEXECUTOR_ROLE members under review:\n'
  for address in \
    "$SAFE_A" \
    "$SAFE_B" \
    "$CHAIN_OWNER_94" \
    "$OLD_EXECUTOR_2C" \
    "$OLD_EXECUTOR_9D"
  do
    printf '  %s: ' "$address"
    has_executor_role "$address"
  done
}

grant_one() {
  local safe_address="$1"
  local account_name="$2"
  local sender="$3"
  local broadcast="$4"
  local inner outer current

  current="$(has_executor_role "$safe_address")"
  if [ "$current" = "true" ]; then
    printf 'Already granted: %s\n' "$safe_address"
    return
  fi

  inner="$(cast calldata \
    "grantRole(bytes32,address)" \
    "$EXECUTOR_ROLE" \
    "$safe_address")"
  outer="$(cast calldata \
    "executeCall(address,bytes)" \
    "$UPGRADE_EXECUTOR" \
    "$inner")"

  printf '\nSafe role grant target: %s\n' "$safe_address"
  printf 'UpgradeExecutor target: %s\n' "$UPGRADE_EXECUTOR"
  printf 'Outer calldata: %s\n' "$outer"

  rpc_eth_call_from "$sender" "$UPGRADE_EXECUTOR" "$outer" >/dev/null
  printf 'Simulation passed for %s\n' "$safe_address"

  if [ "$broadcast" != "true" ]; then
    return
  fi

  cast send \
    --rpc-url "$RPC_URL" \
    --chain "$DEV_CHAIN_ID" \
    --account "$account_name" \
    --confirmations "$CONFIRMATIONS" \
    "$UPGRADE_EXECUTOR" \
    "executeCall(address,bytes)" \
    "$UPGRADE_EXECUTOR" \
    "$inner"

  [ "$(has_executor_role "$safe_address")" = "true" ] || die \
    "grant transaction completed but role verification failed for $safe_address"
}

grant() {
  local broadcast="$1"
  local account_name sender

  require_dev_chain
  account_name="${CURRENT_L3_EXECUTOR_ACCOUNT:-}"
  [ -n "$account_name" ] || die "CURRENT_L3_EXECUTOR_ACCOUNT is required"
  sender="$(cast wallet address --account "$account_name")"
  [ "$(has_executor_role "$sender")" = "true" ] || die \
    "keystore address $sender does not currently have EXECUTOR_ROLE"

  printf 'Current migration executor: %s\n' "$sender"
  if [ "$broadcast" != "true" ]; then
    printf 'Dry run only. Re-run with grant --broadcast after reviewing both simulations.\n'
  fi

  grant_one "$SAFE_A" "$account_name" "$sender" "$broadcast"
  grant_one "$SAFE_B" "$account_name" "$sender" "$broadcast"

  if [ "$broadcast" = "true" ]; then
    printf '\nBoth approved Safes now have EXECUTOR_ROLE. Test the Safe selected for cleanup next.\n'
  fi
}

generate_test() {
  local inner outer output_path origin

  require_dev_chain
  require_manifest_inputs
  [ "$(has_executor_role "$SAFE_ADDRESS")" = "true" ] || die \
    "SAFE_ADDRESS does not yet have EXECUTOR_ROLE"

  inner="$(cast calldata "getAllChainOwners()")"
  outer="$(cast calldata \
    "executeCall(address,bytes)" \
    "$ARB_OWNER_PUBLIC" \
    "$inner")"
  origin="${PROPOSAL_ORIGIN:-Deriw development L3 executor migration Safe-path test}"
  output_path="${OUTPUT_FILE:-$OUT_DIR/dev-l3-safe-test-$(lower "$SAFE_ADDRESS").json}"

  jq -n \
    --arg safe "$SAFE_ADDRESS" \
    --arg proposer "$PROPOSER_ADDRESS" \
    --arg executor "$UPGRADE_EXECUTOR" \
    --arg origin "$origin" \
    --arg data "$outer" \
    '{
      name: "Development L3: verify Safe UpgradeExecutor path",
      chainId: "18417507517",
      safeAddress: $safe,
      proposerAddress: $proposer,
      upgradeExecutorAddress: $executor,
      origin: $origin,
      transactions: [{
        to: $executor,
        value: "0",
        data: $data,
        operation: 0,
        description: "UpgradeExecutor.executeCall(ArbOwnerPublic, getAllChainOwners())"
      }]
    }' | write_manifest "$output_path"
}

generate_cleanup() {
  local remove_57 remove_94 revoke_2c revoke_9d revoke_94
  local outer_remove_57 outer_remove_94 outer_revoke_2c outer_revoke_9d outer_revoke_94
  local output_path origin

  require_dev_chain
  require_manifest_inputs
  [ "${SAFE_TESTS_CONFIRMED:-no}" = "yes" ] || die \
    "set SAFE_TESTS_CONFIRMED=yes only after the approved cleanup Safe passes its on-chain test"
  [ "$(has_executor_role "$SAFE_A")" = "true" ] || die "$SAFE_A lacks EXECUTOR_ROLE"
  [ "$(has_executor_role "$SAFE_B")" = "true" ] || die "$SAFE_B lacks EXECUTOR_ROLE"

  remove_57="$(cast calldata "removeChainOwner(address)" "$CHAIN_OWNER_57")"
  remove_94="$(cast calldata "removeChainOwner(address)" "$CHAIN_OWNER_94")"
  revoke_2c="$(cast calldata "revokeRole(bytes32,address)" "$EXECUTOR_ROLE" "$OLD_EXECUTOR_2C")"
  revoke_9d="$(cast calldata "revokeRole(bytes32,address)" "$EXECUTOR_ROLE" "$OLD_EXECUTOR_9D")"
  revoke_94="$(cast calldata "revokeRole(bytes32,address)" "$EXECUTOR_ROLE" "$CHAIN_OWNER_94")"

  outer_remove_57="$(cast calldata "executeCall(address,bytes)" "$ARB_OWNER" "$remove_57")"
  outer_remove_94="$(cast calldata "executeCall(address,bytes)" "$ARB_OWNER" "$remove_94")"
  outer_revoke_2c="$(cast calldata "executeCall(address,bytes)" "$UPGRADE_EXECUTOR" "$revoke_2c")"
  outer_revoke_9d="$(cast calldata "executeCall(address,bytes)" "$UPGRADE_EXECUTOR" "$revoke_9d")"
  outer_revoke_94="$(cast calldata "executeCall(address,bytes)" "$UPGRADE_EXECUTOR" "$revoke_94")"

  origin="${PROPOSAL_ORIGIN:-Deriw development L3 governance permission cleanup}"
  output_path="${OUTPUT_FILE:-$OUT_DIR/dev-l3-governance-cleanup-$(lower "$SAFE_ADDRESS").json}"

  jq -n \
    --arg safe "$SAFE_ADDRESS" \
    --arg proposer "$PROPOSER_ADDRESS" \
    --arg executor "$UPGRADE_EXECUTOR" \
    --arg origin "$origin" \
    --arg remove57 "$outer_remove_57" \
    --arg remove94 "$outer_remove_94" \
    --arg revoke2c "$outer_revoke_2c" \
    --arg revoke9d "$outer_revoke_9d" \
    --arg revoke94 "$outer_revoke_94" \
    '{
      name: "Development L3: remove direct governance authority",
      chainId: "18417507517",
      safeAddress: $safe,
      proposerAddress: $proposer,
      upgradeExecutorAddress: $executor,
      origin: $origin,
      batchSafetyAcknowledgement: true,
      transactions: [
        {
          to: $executor,
          value: "0",
          data: $remove57,
          operation: 0,
          description: "UpgradeExecutor.executeCall(ArbOwner, removeChainOwner(0x57F9...Fe17))"
        },
        {
          to: $executor,
          value: "0",
          data: $remove94,
          operation: 0,
          description: "UpgradeExecutor.executeCall(ArbOwner, removeChainOwner(0x94A6...1af2))"
        },
        {
          to: $executor,
          value: "0",
          data: $revoke2c,
          operation: 0,
          description: "UpgradeExecutor.executeCall(self, revokeRole(EXECUTOR_ROLE, 0x2c57...2402))"
        },
        {
          to: $executor,
          value: "0",
          data: $revoke9d,
          operation: 0,
          description: "UpgradeExecutor.executeCall(self, revokeRole(EXECUTOR_ROLE, 0x9D41...76DA))"
        },
        {
          to: $executor,
          value: "0",
          data: $revoke94,
          operation: 0,
          description: "UpgradeExecutor.executeCall(self, revokeRole(EXECUTOR_ROLE, 0x94A6...1af2))"
        }
      ]
    }' | write_manifest "$output_path"
}

verify_final() {
  local failures=0 chain_owners blacklist_owners
  local chain_owners_result blacklist_owners_result

  require_dev_chain
  chain_owners_result="$(rpc_eth_call "$ARB_OWNER_PUBLIC" "0x516b4e0f")"
  chain_owners="$(cast abi-decode "getAllChainOwners()(address[])" "$chain_owners_result")"
  blacklist_owners_result="$(rpc_eth_call "$BLACKLIST_PUBLIC" "0xc3005d68")"
  blacklist_owners="$(cast abi-decode "getAllBlacklistOwners()(address[])" "$blacklist_owners_result")"

  printf 'Chain owners: %s\n' "$chain_owners"
  printf 'Blacklist owners: %s\n' "$blacklist_owners"

  if [ "$(lower "$(printf '%s' "$chain_owners" | tr -d '[:space:]')")" != \
    "[$(lower "$UPGRADE_EXECUTOR")]" ]; then
    printf 'FAIL: UpgradeExecutor is not the sole chain owner.\n' >&2
    failures=$((failures + 1))
  fi

  if [ "$(lower "$(printf '%s' "$blacklist_owners" | tr -d '[:space:]')")" != \
    "[$(lower "$CHAIN_OWNER_57")]" ]; then
    printf 'FAIL: retained blacklist owner differs from policy.\n' >&2
    failures=$((failures + 1))
  fi

  for address in "$SAFE_A" "$SAFE_B"; do
    if [ "$(has_executor_role "$address")" != "true" ]; then
      printf 'FAIL: approved Safe lacks EXECUTOR_ROLE: %s\n' "$address" >&2
      failures=$((failures + 1))
    fi
  done

  for address in "$CHAIN_OWNER_94" "$OLD_EXECUTOR_2C" "$OLD_EXECUTOR_9D"; do
    if [ "$(has_executor_role "$address")" != "false" ]; then
      printf 'FAIL: former EOA still has EXECUTOR_ROLE: %s\n' "$address" >&2
      failures=$((failures + 1))
    fi
  done

  [ "$failures" -eq 0 ] || die "$failures final-state verification check(s) failed"
  printf 'PASS: development L3 governance matches the approved final state.\n'
}

main() {
  local mode="${1:-help}"
  local option="${2:-}"

  require_command cast
  require_command curl
  require_command jq

  case "$mode" in
    audit)
      [ -z "$option" ] || die "audit accepts no additional argument"
      audit
      ;;
    grant)
      if [ -z "$option" ]; then
        grant false
      elif [ "$option" = "--broadcast" ]; then
        grant true
      else
        die "grant accepts only --broadcast"
      fi
      ;;
    generate-test)
      [ -z "$option" ] || die "generate-test accepts no additional argument"
      generate_test
      ;;
    generate-cleanup)
      [ -z "$option" ] || die "generate-cleanup accepts no additional argument"
      generate_cleanup
      ;;
    verify-final)
      [ -z "$option" ] || die "verify-final accepts no additional argument"
      verify_final
      ;;
    help|-h|--help)
      usage
      ;;
    *)
      usage >&2
      die "unknown mode: $mode"
      ;;
  esac
}

main "$@"
