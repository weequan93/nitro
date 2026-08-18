#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 2 ]; then
    echo "Usage: $0 <machines-dir> <prover>" >&2
    exit 2
fi

MACHINES_DIR=$1
PROVER=$2

if [ ! -d "$MACHINES_DIR" ]; then
    echo "Error: machines directory does not exist: $MACHINES_DIR" >&2
    exit 1
fi

if [ ! -x "$PROVER" ]; then
    echo "Error: prover is not executable: $PROVER" >&2
    exit 1
fi

MACHINES_DIR=$(cd "$MACHINES_DIR" && pwd)
PROVER=$(cd "$(dirname "$PROVER")" && pwd)/$(basename "$PROVER")

shopt -s nullglob
machines=("$MACHINES_DIR"/*/)
if [ "${#machines[@]}" -eq 0 ]; then
    echo "Error: no machine directories found in $MACHINES_DIR" >&2
    exit 1
fi

for machine in "${machines[@]}"; do
    moduleRootFile="$machine/module-root.txt"
    machineFile="$machine/machine.v2.wavm.br"

    if [ ! -f "$moduleRootFile" ]; then
        echo "Error: module root file does not exist: $moduleRootFile" >&2
        exit 1
    fi

    if [ ! -f "$machineFile" ]; then
        echo "Error: machine file does not exist: $machineFile" >&2
        exit 1
    fi

    expectedWasmModuleRoot=$(cat "$moduleRootFile")
    actualWasmModuleRoot=$(cd "$machine" && "$PROVER" machine.v2.wavm.br --print-wasmmoduleroot)
    if [ "$expectedWasmModuleRoot" != "$actualWasmModuleRoot" ]; then
        echo "Error: Expected module root $expectedWasmModuleRoot but found $actualWasmModuleRoot in $machine" >&2
        exit 1
    fi

    echo "Verified module root $actualWasmModuleRoot in $machine"
done
