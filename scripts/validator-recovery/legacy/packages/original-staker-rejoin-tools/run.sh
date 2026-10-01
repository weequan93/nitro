#!/usr/bin/env bash
set -euo pipefail
export PATH="$HOME/.foundry/bin:$PATH"
SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
command -v python3 >/dev/null || { echo 'Missing python3'; exit 1; }
command -v cast >/dev/null || { echo 'Missing Foundry cast'; exit 1; }
OUTPUT="${1:-$PWD/original-staker-rejoin-$(date +%Y%m%d-%H%M%S)}"
exec python3 "$SCRIPT_DIR/test-original-staker-rejoin.py" --out "$OUTPUT"
