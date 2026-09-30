#!/usr/bin/env python3
"""Generate Safe Builder imports for blacklist additions and owner grants; offline only."""

import argparse
from datetime import datetime, timezone
import json
from pathlib import Path
import re
import shlex
import shutil
import subprocess
import sys

BLACKLIST = "0x00000000000000000000000000000000000007ec"
BLACKLIST_PUBLIC = "0x00000000000000000000000000000000000007eb"
CHAIN_OWNER = "0x0000000000000000000000000000000000000070"
CHAIN_OWNER_PUBLIC = "0x000000000000000000000000000000000000006b"
ACTIONS = {
    "blacklist-from": ("addBlacklistTxFrom", BLACKLIST, "addr", "isBlacklistTxFrom", "Block transactions sent by the address"),
    "blacklist-to": ("addBlacklistTxTo", BLACKLIST, "addr", "isBlacklistTxTo", "Block transactions addressed to the address"),
    "blacklist-owner": ("addBlacklistOwner", BLACKLIST, "newOwner", "isBlacklistOwner", "Grant blacklist administration"),
    "chain-owner": ("addChainOwner", CHAIN_OWNER, "newOwner", "isChainOwner", "Grant ArbOS chain administration"),
    "safe-owner": ("addOwnerWithThreshold", None, "owner", "isOwner", "Add a Safe signer and set the resulting threshold"),
}


def require(ok, message):
    if not ok:
        raise ValueError(message)


def address(value):
    require(isinstance(value, str) and re.fullmatch(r"0x[0-9a-fA-F]{40}", value), "Address must contain exactly 20 hex bytes")
    require(int(value, 16) != 0, "Zero address is not allowed")
    return value.lower()


def cast_path():
    path = shutil.which("cast")
    if not path and (Path.home() / ".foundry/bin/cast").is_file():
        path = str(Path.home() / ".foundry/bin/cast")
    require(path is not None, "cast not found; add ~/.foundry/bin to PATH")
    return path


def calldata(signature, *args):
    return subprocess.check_output([cast_path(), "calldata", signature, *map(str, args)], text=True, timeout=30).strip().lower()


def build(action, safe, addresses, chain_id=2886, threshold=None):
    require(action in ACTIONS, "Unsupported action")
    safe = address(safe)
    addresses = [address(value) for value in addresses]
    require(addresses and len(set(addresses)) == len(addresses), "Supply distinct target addresses")
    require(type(chain_id) is int and 0 < chain_id < 2**256, "Invalid chain ID")
    method, target, parameter, query, purpose = ACTIONS[action]
    if action == "safe-owner":
        require(len(addresses) == 1, "Add one Safe signer per generated package")
        require(addresses[0] != safe and int(addresses[0], 16) != 1, "Invalid Safe owner: Safe itself or sentinel")
        require(type(threshold) is int and 1 <= threshold < 2**256, "--threshold is required and must be positive")
        target = safe
    else:
        require(chain_id in (2885, 2886), "Deriw precompile actions require chain ID 2885 or 2886")
        require(threshold is None, "--threshold applies only to safe-owner")
    signature = method + ("(address,uint256)" if action == "safe-owner" else "(address)")
    transactions = []
    for account in addresses:
        values = {parameter: account}
        inputs = [{"internalType": "address", "name": parameter, "type": "address"}]
        args = [account]
        if action == "safe-owner":
            values["_threshold"] = str(threshold)
            inputs.append({"internalType": "uint256", "name": "_threshold", "type": "uint256"})
            args.append(threshold)
        transactions.append({"to": target, "value": "0", "data": calldata(signature, *args),
                             "contractMethod": {"inputs": inputs, "name": method, "payable": False},
                             "contractInputsValues": values})
    return {
        "version": "1.0", "chainId": str(chain_id), "createdAt": int(datetime.now(timezone.utc).timestamp() * 1000),
        "meta": {"name": "REVIEW REQUIRED - " + action,
                 "description": purpose + "; direct CALL list, zero value. Generated offline; check current authority and Safe fields before signing.",
                 "createdFromSafeAddress": safe, "createdFromOwnerAddress": ""},
        "transactions": transactions,
    }


def readonly_check(action, package):
    safe = package["meta"]["createdFromSafeAddress"]
    chain_id = package["chainId"]
    method, target, parameter, query, _ = ACTIONS[action]
    lines = ["#!/usr/bin/env bash", "set -euo pipefail", 'export PATH="$HOME/.foundry/bin:$PATH"',
             'RPC="${1:?Pass the RPC for the Safe network}"',
             f'test "$(cast chain-id --rpc-url "$RPC" --rpc-timeout 30)" = {chain_id}',
             f"SAFE={safe}",
             'echo "Safe owners, threshold and current nonce"',
             'cast call "$SAFE" \'getOwners()(address[])\' --rpc-url "$RPC" --rpc-timeout 30',
             'cast call "$SAFE" \'getThreshold()(uint256)\' --rpc-url "$RPC" --rpc-timeout 30',
             'cast call "$SAFE" \'nonce()(uint256)\' --rpc-url "$RPC" --rpc-timeout 30']
    if action.startswith("blacklist-"):
        lines += [f'BLACKLIST_OWNER=$(cast call {BLACKLIST_PUBLIC} \'isBlacklistOwner(address)(bool)\' "$SAFE" --rpc-url "$RPC" --rpc-timeout 30)',
                  f'CHAIN_OWNER=$(cast call {CHAIN_OWNER_PUBLIC} \'isChainOwner(address)(bool)\' "$SAFE" --rpc-url "$RPC" --rpc-timeout 30)',
                  '[ "$BLACKLIST_OWNER" = true ] || [ "$CHAIN_OWNER" = true ] || { echo "Safe is not a direct blacklist authority"; exit 1; }']
        view_target = BLACKLIST_PUBLIC
    elif action == "chain-owner":
        lines += [f'test "$(cast call {CHAIN_OWNER_PUBLIC} \'isChainOwner(address)(bool)\' "$SAFE" --rpc-url "$RPC" --rpc-timeout 30)" = true']
        view_target = CHAIN_OWNER_PUBLIC
    else:
        view_target = safe
    for tx in package["transactions"]:
        account = tx["contractInputsValues"][parameter]
        lines += [f"echo {shlex.quote('Current ' + query + ' for ' + account)}",
                  f'cast call {view_target} \'{query}(address)(bool)\' {account} --rpc-url "$RPC" --rpc-timeout 30',
                  f'cast call {tx["to"]} --data {tx["data"]} --from "$SAFE" --rpc-url "$RPC" --rpc-timeout 30 >/dev/null']
    lines += ['echo "PASS direct calls simulated; Safe signatures, guard, wrapping and finality are not tested. No transaction sent."']
    return "\n".join(lines) + "\n"


def review(action, package):
    _, _, _, _, purpose = ACTIONS[action]
    lines = [f"# Safe transaction review: {action}", "", purpose + ".", "",
             f"- Safe: `{package['meta']['createdFromSafeAddress']}`", f"- Chain ID: `{package['chainId']}`",
             "- Import: `safe-import.json` (Transaction Builder CALL list). All values are zero.",
             "- Generated offline. No nonce, SafeTxHash or owner signatures have been bound to this file.", "",
             "| Target | Method | Parameters |", "| --- | --- | --- |"]
    for tx in package["transactions"]:
        lines.append(f"| `{tx['to']}` | `{tx['contractMethod']['name']}` | `{json.dumps(tx['contractInputsValues'])}` |")
    lines += ["", "Run `bash check-readonly.sh RPC_URL` against the Safe's own chain, then review the Safe UI transaction and simulation before signing.",
              "Single direct calls use CALL (operation 0). If the UI batches multiple calls, inspect its MultiSend wrapper; the precompile calls must see the authorized Safe as their caller.",
              "These files do not construct an UpgradeExecutor route. If the authority is an executor rather than this Safe, use its separately reviewed call path.",
              "After execution, check both the successful Safe execution and the corresponding public membership view. Receipt status alone is insufficient if the Safe emits ExecutionFailure.", ""]
    return "\n".join(lines)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=ACTIONS)
    parser.add_argument("--safe", required=True, help="Safe that will execute on the selected chain")
    parser.add_argument("--address", action="append", required=True, help="Address to add; repeat for blacklist or chain-owner batches")
    parser.add_argument("--chain-id", type=int, default=2886)
    parser.add_argument("--threshold", type=int, help="Resulting Safe threshold; required for safe-owner")
    parser.add_argument("--out", type=Path, help="New output directory, default: ignored generated/ under this tool")
    args = parser.parse_args()
    package = build(args.action, args.safe, args.address, args.chain_id, args.threshold)
    out = args.out or Path(__file__).resolve().parent / "generated" / (datetime.now(timezone.utc).strftime("%Y%m%d-%H%M%S-%f") + "-" + args.action)
    out.mkdir(parents=True, exist_ok=False)
    (out / "safe-import.json").write_text(json.dumps(package, indent=2) + "\n")
    (out / "REVIEW.md").write_text(review(args.action, package))
    (out / "check-readonly.sh").write_text(readonly_check(args.action, package))
    print(json.dumps({"status": "safe_import_generated_review_required", "action": args.action,
                      "chainId": package["chainId"], "safe": package["meta"]["createdFromSafeAddress"],
                      "calls": len(package["transactions"]), "rpcAccess": False, "transactionsSent": False}))
    print("OUTPUT", out)


if __name__ == "__main__":
    try:
        main()
    except (ValueError, OSError, subprocess.SubprocessError) as error:
        print("STOP:", error, file=sys.stderr)
        sys.exit(1)
