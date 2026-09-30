#!/usr/bin/env bash
set -euo pipefail
# No private key, Safe signatures, or production transaction is used.
SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
PARENT_RPC=${PARENT_RPC:-http://10.1.2.16:8547}
NODE_RPC=${NODE_RPC:-http://127.0.0.1:8149}
INPUT=${1:?Usage: bash run-validator-recovery-mock.sh INPUT_JSON NEW_OUTPUT_DIRECTORY STAKER_ADDRESS}
OUTPUT=${2:?Specify a new output directory}
STAKER=${3:?Specify the staker to refund IN THE FORK ONLY}
for tool in python3 anvil cast; do
  command -v "$tool" >/dev/null || { echo "Missing prerequisite: $tool" >&2; exit 1; }
done
[[ -f "$INPUT" ]] || { echo "Input file not found: $INPUT" >&2; exit 1; }
[[ ! -e "$OUTPUT" ]] || { echo "Output exists; choose a new directory" >&2; exit 1; }
mkdir -- "$OUTPUT"
OUTPUT=$(cd -- "$OUTPUT" && pwd)
echo 'Stage 1: read-only checkpoint collection and one-message replay on 8149.'
python3 "$SCRIPT_DIR/prepare-recovery-fork.py" \
  --parent-rpc "$PARENT_RPC" --node-rpc "$NODE_RPC" \
  --input "$INPUT" --out "$OUTPUT/snapshot"
echo 'Stage 2: disposable local fork; no transaction goes to the parent RPC.'
python3 "$SCRIPT_DIR/mock-validator-recovery.py" "$OUTPUT/snapshot" \
  --parent-rpc "$PARENT_RPC" --refund-staker "$STAKER" --out "$OUTPUT/fork"
echo "Read $OUTPUT/fork/summary.json. A PASS is NOT production approval."
