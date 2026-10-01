#!/usr/bin/env bash
# Owner-operated only. Default is read-only. This is stake refund, NOT an L3 user cross-chain claim.
set -euo pipefail
MODE=${1:-check}
case "$MODE" in check|broadcast) ;; *) echo 'Usage: bash withdraw-refund.sh check|broadcast' >&2; exit 2;; esac
: "${PARENT_RPC:?Set Arbitrum One RPC}"
: "${RECOVERY_PACKAGE:?Set absolute recovery package directory}"
: "${RECOVERY_ACCEPTANCE:?Set absolute accepted recovery receipt directory}"
: "${OWNER_ADDRESS:?Set original refund account address}"
TOOL_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
python3 "$TOOL_DIR/owner-check.py" --package "$RECOVERY_PACKAGE" --acceptance "$RECOVERY_ACCEPTANCE" --parent-rpc "$PARENT_RPC" --address "$OWNER_ADDRESS" --require-credit
if [ "$MODE" = check ]; then
  echo 'READ-ONLY CHECK COMPLETE. No key accessed, no transaction sent.'
  exit 0
fi
: "${KEYSTORE:?Set one encrypted keystore FILE on this owner machine}"
test -f "$KEYSTORE"
# Do not mix inherited wallet/impersonation settings with the selected encrypted file.
unset ETH_PRIVATE_KEY ETH_FROM ETH_KEYSTORE ETH_KEYSTORE_ACCOUNT ETH_PASSWORD
ACTUAL=$(cast wallet address --keystore "$KEYSTORE")
test "$(printf '%s' "$ACTUAL" | tr '[:upper:]' '[:lower:]')" = "$(printf '%s' "$OWNER_ADDRESS" | tr '[:upper:]' '[:lower:]')"
printf 'Send original stake-refund withdrawal on Arbitrum One for %s? Type WITHDRAW: ' "$ACTUAL"
read -r ANSWER
test "$ANSWER" = WITHDRAW
# Recheck immediately after interactive signer unlock/confirmation.
python3 "$TOOL_DIR/owner-check.py" --package "$RECOVERY_PACKAGE" --acceptance "$RECOVERY_ACCEPTANCE" --parent-rpc "$PARENT_RPC" --address "$OWNER_ADDRESS" --require-credit
cast send 0xa113e2e9620a3bc088a681ebb2c234fdbeb85e21 'withdrawStakerFunds()' --rpc-url "$PARENT_RPC" --chain 42161 --keystore "$KEYSTORE"
