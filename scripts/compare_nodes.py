#!/usr/bin/env python3
import argparse
import json
import random
import sys
import urllib.request


def rpc(url, method, params, timeout):
    payload = {"jsonrpc": "2.0", "method": method, "params": params, "id": 1}
    data = json.dumps(payload).encode("utf-8")
    req = urllib.request.Request(url, data=data, headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=timeout) as resp:
        out = json.load(resp)
    if "error" in out:
        raise RuntimeError(f"{method} error: {out['error']}")
    return out["result"]


def hex_to_int(value):
    if value is None:
        return None
    if isinstance(value, int):
        return value
    return int(value, 16)


def normalize_block_tag(tag):
    if tag == "latest" or tag == "earliest" or tag == "pending":
        return tag
    return hex(int(tag))


def get_block(url, number, timeout):
    return rpc(url, "eth_getBlockByNumber", [hex(number), False], timeout)


def compare_field(label, a, b, mismatches):
    if a != b:
        mismatches.append(f"{label}: {a} != {b}")


def compare_blocks(url_a, url_b, start, end, step, samples, seed, timeout, mismatches):
    fields = [
        "hash",
        "stateRoot",
        "receiptsRoot",
        "transactionsRoot",
        "parentHash",
        "gasUsed",
    ]

    def check_block(num):
        block_a = get_block(url_a, num, timeout)
        block_b = get_block(url_b, num, timeout)
        if block_a is None or block_b is None:
            mismatches.append(f"block {num}: missing (a={block_a is None}, b={block_b is None})")
            return
        for field in fields:
            compare_field(f"block {num} {field}", block_a.get(field), block_b.get(field), mismatches)

    for num in range(start, end + 1, step):
        check_block(num)

    if samples > 0:
        rng = random.Random(seed)
        for num in rng.sample(range(start, end + 1), min(samples, end - start + 1)):
            check_block(num)


def compare_accounts(url_a, url_b, addresses, storage_keys, block_tag, timeout, mismatches):
    if not addresses:
        return
    tag = normalize_block_tag(block_tag)
    for addr in addresses:
        bal_a = rpc(url_a, "eth_getBalance", [addr, tag], timeout)
        bal_b = rpc(url_b, "eth_getBalance", [addr, tag], timeout)
        compare_field(f"balance {addr} @ {tag}", bal_a, bal_b, mismatches)

        nonce_a = rpc(url_a, "eth_getTransactionCount", [addr, tag], timeout)
        nonce_b = rpc(url_b, "eth_getTransactionCount", [addr, tag], timeout)
        compare_field(f"nonce {addr} @ {tag}", nonce_a, nonce_b, mismatches)

        code_a = rpc(url_a, "eth_getCode", [addr, tag], timeout)
        code_b = rpc(url_b, "eth_getCode", [addr, tag], timeout)
        compare_field(f"code {addr} @ {tag}", code_a, code_b, mismatches)

        for key in storage_keys:
            val_a = rpc(url_a, "eth_getStorageAt", [addr, key, tag], timeout)
            val_b = rpc(url_b, "eth_getStorageAt", [addr, key, tag], timeout)
            compare_field(f"storage {addr} {key} @ {tag}", val_a, val_b, mismatches)


def main():
    parser = argparse.ArgumentParser(
        description="Compare two Nitro RPC nodes for block and account parity."
    )
    parser.add_argument("--a", required=True, help="RPC URL for the original node")
    parser.add_argument("--b", required=True, help="RPC URL for the migrated node")
    parser.add_argument("--from", dest="start", type=int, default=None, help="start block number")
    parser.add_argument("--to", dest="end", type=int, default=None, help="end block number")
    parser.add_argument("--step", type=int, default=1000, help="step between blocks (default: 1000)")
    parser.add_argument("--samples", type=int, default=0, help="random samples within range")
    parser.add_argument("--seed", type=int, default=1337, help="random seed for samples")
    parser.add_argument("--addr", action="append", default=[], help="address to compare (repeatable)")
    parser.add_argument("--storage-key", action="append", default=[], help="storage key to compare (repeatable)")
    parser.add_argument(
        "--account-block",
        default="latest",
        help="block tag/number for account checks (default: latest)",
    )
    parser.add_argument("--timeout", type=int, default=30, help="RPC timeout seconds")
    args = parser.parse_args()

    mismatches = []

    chain_a = rpc(args.a, "eth_chainId", [], args.timeout)
    chain_b = rpc(args.b, "eth_chainId", [], args.timeout)
    compare_field("chainId", chain_a, chain_b, mismatches)

    head_a = hex_to_int(rpc(args.a, "eth_blockNumber", [], args.timeout))
    head_b = hex_to_int(rpc(args.b, "eth_blockNumber", [], args.timeout))
    if head_a != head_b:
        mismatches.append(f"head mismatch: a={head_a} b={head_b} (using min for block compare)")
    head = min(head_a, head_b)

    start = args.start if args.start is not None else 0
    end = args.end if args.end is not None else head
    if start > end:
        print(f"invalid range: start {start} > end {end}", file=sys.stderr)
        return 2

    compare_blocks(args.a, args.b, start, end, args.step, args.samples, args.seed, args.timeout, mismatches)
    compare_accounts(args.a, args.b, args.addr, args.storage_key, args.account_block, args.timeout, mismatches)

    if mismatches:
        print("MISMATCHES:")
        for m in mismatches:
            print(f"- {m}")
        return 1

    print("OK: nodes match for requested checks")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
