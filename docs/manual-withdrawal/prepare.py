#!/usr/bin/env python3
"""Check Deriw L3 withdrawals and prepare Outbox claims; never sign or broadcast."""

import argparse
from datetime import datetime, timezone
from decimal import Decimal, localcontext
import json
from pathlib import Path
import re
import shlex
import shutil
import subprocess
import sys

L3_RPC = "https://rpc.deriw.com"
PARENT_RPC = "https://arb1.arbitrum.io/rpc"
ROLLUP = "0xa113e2e9620a3bc088a681ebb2c234fdbeb85e21"
OUTBOX = "0x47da6c41d03ac0608924e86f61577df558114bd8"
ARBSYS = "0x0000000000000000000000000000000000000064"
NODE_INTERFACE = "0x00000000000000000000000000000000000000c8"
TOPIC = "0x3e7aafa77dbf186b7fd488006beff893744caa3c4f6f299e8a709fa2087374fc"
EXECUTE = "executeTransaction(bytes32[],uint256,address,address,uint256,uint256,uint256,uint256,bytes)"
ITEM = "calculateItemHash(address,address,uint256,uint256,uint256,uint256,bytes)"
ZERO = "0x" + "00" * 32
READ_METHODS = {"eth_chainId", "eth_getTransactionReceipt", "eth_getBlockByNumber", "eth_call"}


def require(condition, message):
    if not condition:
        raise ValueError(message)


def hexbytes(value, length=None):
    require(isinstance(value, str) and re.fullmatch(r"0x(?:[0-9a-fA-F]{2})*", value), "Invalid hex data")
    result = bytes.fromhex(value[2:])
    require(length is None or len(result) == length, "Unexpected hex length")
    return result


def word(data, index):
    require(len(data) >= (index + 1) * 32, "Truncated ABI word")
    return int.from_bytes(data[index * 32:(index + 1) * 32], "big")


def decode_event(event):
    topics = event.get("topics", [])
    require(event.get("address", "").lower() == ARBSYS and len(topics) == 4
            and topics[0].lower() == TOPIC and not event.get("removed", False), "Invalid ArbSys event")
    destination = hexbytes(topics[1], 32)
    require(destination[:12] == bytes(12), "Invalid destination address")
    item_hash = "0x" + hexbytes(topics[2], 32).hex()
    index = int.from_bytes(hexbytes(topics[3], 32), "big")
    data = hexbytes(event["data"])
    require(len(data) >= 224 and data[:12] == bytes(12) and word(data, 5) == 192, "Invalid event ABI")
    size = word(data, 6)
    require(len(data) == 224 + ((size + 31) // 32) * 32, "Truncated or trailing event payload")
    require(not any(data[224 + size:]), "Nonzero ABI padding")
    require(index < 2**64, "Message index exceeds NodeInterface uint64")
    return {
        "index": index, "itemHash": item_hash, "sender": "0x" + data[12:32].hex(),
        "destination": "0x" + destination[12:].hex(), "l3Block": word(data, 1),
        "parentBlock": word(data, 2), "timestamp": word(data, 3), "value": str(word(data, 4)),
        "data": "0x" + data[224:224 + size].hex(),
    }


def decode_proof(value):
    data = hexbytes(value)
    require(len(data) >= 128 and word(data, 2) == 96, "Invalid proof ABI")
    length = word(data, 3)
    require(length <= 64 and len(data) == 128 + length * 32, "Invalid proof length")
    return {
        "send": "0x" + data[:32].hex(), "root": "0x" + data[32:64].hex(),
        "proof": ["0x" + data[128 + i * 32:160 + i * 32].hex() for i in range(length)],
    }


def message_args(message):
    return [message[key] for key in ("sender", "destination", "l3Block", "parentBlock", "timestamp", "value", "data")]


def gateway_details(message):
    data = hexbytes(message["data"])
    if data[:4].hex() != "2e567b36":
        return {"payloadKind": "other", "note": "Review destination, native value and payload directly."}
    require(len(data) >= 196, "Truncated finalizeInboundTransfer payload")
    body = data[4:]
    for slot in range(3):
        require(body[slot * 32:slot * 32 + 12] == bytes(12), "Invalid gateway address")
    offset = word(body, 4)
    require(offset == 160 and len(body) >= offset + 32, "Invalid gateway payload offset")
    size = word(body, 5)
    require(len(body) == offset + 32 + ((size + 31) // 32) * 32, "Invalid gateway payload length")
    return {"payloadKind": "finalizeInboundTransfer", "token": "0x" + body[12:32].hex(),
            "recipient": "0x" + body[76:96].hex(), "amountRaw": str(word(body, 3))}


class Client:
    def __init__(self):
        self.cast = shutil.which("cast")
        if not self.cast and (Path.home() / ".foundry/bin/cast").is_file():
            self.cast = str(Path.home() / ".foundry/bin/cast")
        require(self.cast is not None, "cast not found; add ~/.foundry/bin to PATH")

    def calldata(self, signature, *args):
        return subprocess.check_output([self.cast, "calldata", signature, *map(str, args)], text=True, timeout=30).strip().lower()

    def rpc(self, url, method, params):
        require(method in READ_METHODS, "Only read-only RPC methods are allowed")
        request = json.dumps({"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
        result = subprocess.run(["curl", "--fail", "--silent", "--show-error", "--max-time", "45",
                                 url, "-H", "Content-Type: application/json", "--data-binary", "@-"],
                                input=request, capture_output=True, text=True, timeout=50)
        require(result.returncode == 0, "RPC transport failed; check endpoint connectivity")
        response = json.loads(result.stdout)
        require("error" not in response, f"RPC {method} rejected: {response.get('error')}")
        return response["result"]

    def call(self, url, target, signature, *args, tag="latest"):
        return self.rpc(url, "eth_call", [{"to": target, "data": self.calldata(signature, *args)}, tag])


def load_messages(client, l3_rpc, tx):
    hexbytes(tx, 32)
    receipt = client.rpc(l3_rpc, "eth_getTransactionReceipt", [tx])
    require(receipt and receipt["transactionHash"].lower() == tx.lower()
            and int(receipt["status"], 16) == 1, "Missing or unsuccessful L3 receipt")
    block = client.rpc(l3_rpc, "eth_getBlockByNumber", [receipt["blockNumber"], False])
    require(block and block["hash"].lower() == receipt["blockHash"].lower(), "L3 receipt block changed")
    messages = [decode_event(event) for event in receipt["logs"]
                if event.get("address", "").lower() == ARBSYS and event.get("topics")
                and event["topics"][0].lower() == TOPIC]
    require(messages, "No L2ToL1Tx withdrawal event in this receipt")
    require(all(message["l3Block"] == int(receipt["blockNumber"], 16) for message in messages), "Event block mismatch")
    return receipt, block, messages


def is_spent(client, rpc, index, tag):
    value = int(client.call(rpc, OUTBOX, "isSpent(uint256)", index, tag=tag), 16)
    require(value in (0, 1), "Unexpected isSpent response")
    return bool(value)


def claim_script(index, root, args, details):
    label = json.dumps({"index": index, **details}, ensure_ascii=True)
    return f'''#!/usr/bin/env bash
set +x
set -euo pipefail
# A real parent-chain transaction is sent only when this generated script is run.
export PATH="$HOME/.foundry/bin:$PATH"
RPC="${{1:-{PARENT_RPC}}}"
KEYSTORE="${{2:-}}"
OUTBOX={OUTBOX}
test "$(cast chain-id --rpc-url "$RPC" --rpc-timeout 30)" = 42161
MAPPED=$(cast call {ROLLUP} 'outbox()(address)' --rpc-url "$RPC" --rpc-timeout 30)
test "$(printf '%s' "$MAPPED" | tr '[:upper:]' '[:lower:]')" = "$OUTBOX"
test "$(cast call "$OUTBOX" 'isSpent(uint256)(bool)' {index} --rpc-url "$RPC" --rpc-timeout 30)" = false
REGISTERED=$(cast call "$OUTBOX" 'roots(bytes32)(bytes32)' {root} --rpc-url "$RPC" --rpc-timeout 30)
test "$REGISTERED" != {ZERO} || {{ echo 'Root not confirmed; prepare again later.'; exit 1; }}
CLAIM_ARGS=(
{chr(10).join('  ' + shlex.quote(str(value)) for value in [EXECUTE, *args])}
)
cast call "$OUTBOX" "${{CLAIM_ARGS[@]}}" --rpc-url "$RPC" --rpc-timeout 30 >/dev/null
printf '%s\\n' {shlex.quote(label)}
echo 'Review the fixed destination/value/payload. This signer pays Arbitrum One gas.'
SIGNER=(--interactive)
if [ -n "$KEYSTORE" ]; then SIGNER=(--keystore "$KEYSTORE"); fi
unset ETH_PRIVATE_KEY ETH_FROM ETH_KEYSTORE ETH_KEYSTORE_ACCOUNT ETH_PASSWORD
exec cast send "$OUTBOX" "${{CLAIM_ARGS[@]}}" --rpc-url "$RPC" --chain 42161 --value 0 --rpc-timeout 30 --timeout 120 "${{SIGNER[@]}}"
'''


def prepare_claim(client, args, receipt, block, message, anchor):
    count = int(block["sendCount"], 16)
    require(message["index"] < count < 2**64, "Invalid sendCount for this message")
    proof = decode_proof(client.call(args.l3_rpc, NODE_INTERFACE, "constructOutboxProof(uint64,uint64)", count, message["index"]))
    hexbytes(block["sendRoot"], 32)
    require(proof["root"] == block["sendRoot"].lower(), "Proof root differs from L3 block sendRoot")
    proof_arg = "[" + ",".join(proof["proof"]) + "]"
    item = client.call(args.parent_rpc, OUTBOX, ITEM, *message_args(message), tag=anchor["number"]).lower()
    require(item == message["itemHash"] == proof["send"], "Message hash differs from event or proof")
    root = client.call(args.parent_rpc, OUTBOX, "calculateMerkleRoot(bytes32[],uint256,bytes32)", proof_arg, message["index"], item, tag=anchor["number"])
    require(root.lower() == proof["root"], "Merkle proof verification failed")
    registered = client.call(args.parent_rpc, OUTBOX, "roots(bytes32)", root, tag=anchor["number"])
    hexbytes(registered, 32)
    claim_args = [proof_arg, message["index"], *message_args(message)]
    calldata = client.calldata(EXECUTE, *claim_args)
    request = {"to": OUTBOX, "data": calldata, "value": "0x0", "chainId": "0xa4b1"}
    root_known = registered.lower() != ZERO
    simulation = client.rpc(args.parent_rpc, "eth_call", [{key: request[key] for key in ("to", "data", "value")}, anchor["number"]]) if root_known else None
    details = gateway_details(message)
    if "token" in details:
        try:
            decimals = int(client.call(args.parent_rpc, details["token"], "decimals()", tag=anchor["number"]), 16)
            require(0 <= decimals <= 255, "Invalid token decimals")
            with localcontext() as context:
                context.prec = 400
                details.update(decimals=decimals, amountFormatted=format(Decimal(details["amountRaw"]) / 10**decimals, "f"))
        except (ValueError, subprocess.SubprocessError):
            details["decimalsUnavailable"] = True
    return {"root": root, "rootKnown": root_known, "proofVerified": True,
            "simulation": simulation, "details": details}, {
        "receipt.json": receipt, "proof-block.json": block, "message.json": message,
        "proof.json": proof, "wallet-request.json": request,
        "wallet-claim.json": {"index": str(message["index"]), "root": root, "transaction": request, "details": details},
        "claim-calldata.txt": calldata + "\n",
        "claim-cli.sh": claim_script(message["index"], root, claim_args, details),
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("transaction", nargs="+", help="L3 transaction hash; multiple allowed with --status-only")
    parser.add_argument("--status-only", action="store_true", help="Check every withdrawal event without building proofs")
    parser.add_argument("--index", type=int, help="Select a message when the transaction emits several withdrawals")
    parser.add_argument("--l3-rpc", default=L3_RPC)
    parser.add_argument("--parent-rpc", default=PARENT_RPC)
    parser.add_argument("--out", type=Path, help="New output directory; default: ignored generated/ under this tool")
    args = parser.parse_args()
    require(args.status_only or len(args.transaction) == 1, "Use --status-only for multiple transactions")
    require(not args.status_only or args.index is None, "--index applies to claim preparation")
    client = Client()
    require(int(client.rpc(args.l3_rpc, "eth_chainId", []), 16) == 2886, "L3 chain must be 2886")
    require(int(client.rpc(args.parent_rpc, "eth_chainId", []), 16) == 42161, "Parent chain must be 42161")
    anchor = client.rpc(args.parent_rpc, "eth_getBlockByNumber", ["latest", False])
    mapped = client.call(args.parent_rpc, ROLLUP, "outbox()", tag=anchor["number"])
    require(hexbytes(mapped, 32) == bytes(12) + hexbytes(OUTBOX, 20), "Rollup Outbox differs from expected Deriw deployment")
    files = {}
    results = []
    for tx in args.transaction:
        receipt, block, messages = load_messages(client, args.l3_rpc, tx)
        row = {"l3Tx": tx.lower(), "withdrawals": []}
        if not args.status_only:
            messages = [message for message in messages if args.index is None or message["index"] == args.index]
            require(len(messages) == 1, "Choose exactly one withdrawal with --index")
        for message in messages:
            spent = is_spent(client, args.parent_rpc, message["index"], anchor["number"])
            result = {"index": message["index"], "claimed": spent, "status": "already_claimed" if spent else "unclaimed"}
            if not args.status_only and not spent:
                details, files = prepare_claim(client, args, receipt, block, message, anchor)
                result.update(details)
                result["status"] = "claim_prepared" if details["rootKnown"] else "root_not_confirmed"
            row["withdrawals"].append(result)
        results.append(row)
    current = client.rpc(args.parent_rpc, "eth_getBlockByNumber", [anchor["number"], False])
    require(current["hash"].lower() == anchor["hash"].lower(), "Parent observation block changed; repeat check")
    report = {"checkedAt": datetime.now(timezone.utc).isoformat(), "l3ChainId": 2886, "parentChainId": 42161,
              "parentBlock": anchor["number"], "parentBlockHash": anchor["hash"], "outbox": OUTBOX,
              "results": results, "transactionsSent": False}
    out = args.out or Path(__file__).resolve().parent / "generated" / (datetime.now(timezone.utc).strftime("%Y%m%d-%H%M%S-%f") + "-" + args.transaction[0][2:10])
    out.mkdir(parents=True, exist_ok=False)
    for name, value in {**files, "summary.json": report}.items():
        (out / name).write_text(value if isinstance(value, str) else json.dumps(value, indent=2) + "\n")
    print(json.dumps(report, indent=2))
    print("OUTPUT", out)


if __name__ == "__main__":
    try:
        main()
    except (ValueError, KeyError, OSError, subprocess.SubprocessError) as error:
        print("STOP:", error, file=sys.stderr)
        sys.exit(1)
