#!/usr/bin/env python3
"""Read-only watcher for slow sequencer blocks and their transactions.

The watcher follows Docker logs, records slow eth_sendRawTransaction stage
lines, and when Nitro reports a slow sequenced block it reads that block and
its receipts through JSON-RPC. Calldata is intentionally summarized by byte
length, selector, and SHA-256; the full payload is never written.
"""

from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import json
import os
from pathlib import Path
import re
import signal
import subprocess
import sys
import time
import urllib.error
import urllib.request


LOG_TIME_RE = re.compile(r"^(?P<time>\d{4}-\d{2}-\d{2}T[^ ]+Z) ")
SLOW_BLOCK_RE = re.compile(
    r"took over (?P<limit>[0-9.]+) seconds to sequence a block\s+"
    r"elapsed=(?P<elapsed>[0-9.]+)s\s+numTxes=(?P<num_txs>[0-9]+)"
    r"\s+success=(?P<success>\w+)\s+l2Block=(?P<block>[0-9,]+)"
)
RPC_SLOW_RE = re.compile(
    r"RPC stage timings\s+method=(?P<method>\S+)\s+"
    r"outcome=(?P<outcome>\S+)\s+trace=(?P<trace>\S+)\s+"
    r"total_ms=(?P<total>[0-9.]+).*?callback_ms=(?P<callback>[0-9.]+)"
)


def utc_now() -> str:
    return dt.datetime.now(dt.timezone.utc).isoformat().replace("+00:00", "Z")


def parse_log_time(line: str) -> str | None:
    match = LOG_TIME_RE.match(line)
    return match.group("time") if match else None


def hex_int(value: object) -> int | None:
    if not isinstance(value, str):
        return None
    try:
        return int(value, 16)
    except ValueError:
        return None


def input_summary(value: object) -> dict[str, object]:
    if not isinstance(value, str) or not value.startswith("0x"):
        return {"input_bytes": None, "selector": None, "input_sha256": None}
    raw = bytes.fromhex(value[2:])
    return {
        "input_bytes": len(raw),
        "selector": value[:10] if len(raw) >= 4 else None,
        "input_sha256": hashlib.sha256(raw).hexdigest(),
    }


def rpc_call(url: str, method: str, params: list[object], timeout: float) -> object:
    body = json.dumps({"jsonrpc": "2.0", "id": 1, "method": method, "params": params}).encode()
    request = urllib.request.Request(
        url,
        data=body,
        headers={"content-type": "application/json"},
        method="POST",
    )
    with urllib.request.urlopen(request, timeout=timeout) as response:  # noqa: S310 - operator supplies a local RPC URL
        payload = json.loads(response.read())
    if payload.get("error") is not None:
        raise RuntimeError(f"{method}: {payload['error']}")
    return payload.get("result")


def rpc_batch(url: str, calls: list[tuple[int, str, list[object]]], timeout: float) -> dict[int, object]:
    body = json.dumps(
        [{"jsonrpc": "2.0", "id": ident, "method": method, "params": params} for ident, method, params in calls]
    ).encode()
    request = urllib.request.Request(
        url,
        data=body,
        headers={"content-type": "application/json"},
        method="POST",
    )
    with urllib.request.urlopen(request, timeout=timeout) as response:  # noqa: S310 - operator supplies a local RPC URL
        payload = json.loads(response.read())
    result: dict[int, object] = {}
    for item in payload if isinstance(payload, list) else []:
        if isinstance(item, dict) and isinstance(item.get("id"), int):
            result[item["id"]] = item.get("result")
    return result


def process_io_snapshot() -> list[dict[str, object]]:
    """Capture only matching migration process counters when visible."""
    snapshots: list[dict[str, object]] = []
    proc_root = Path("/proc")
    for entry in proc_root.iterdir():
        if not entry.name.isdigit():
            continue
        cmdline_path = entry / "cmdline"
        try:
            command = cmdline_path.read_bytes().replace(b"\0", b" ").decode("utf-8", "replace").strip()
        except OSError:
            continue
        if "pathdb-migrate-pipeline" not in command:
            continue
        values: dict[str, object] = {"pid": int(entry.name), "process": "pathdb-migrate-pipeline"}
        try:
            values["state"] = (entry / "stat").read_text().split(") ", 1)[1].split()[0]
        except (OSError, IndexError):
            values["state"] = None
        try:
            for line in (entry / "io").read_text().splitlines():
                key, _, value = line.partition(":")
                if key in {"read_bytes", "write_bytes", "syscr", "syscw"}:
                    values[key] = int(value.strip())
        except OSError:
            pass
        snapshots.append(values)
    return snapshots


def describe_block(block: object, receipts: dict[int, object], event: dict[str, object]) -> dict[str, object]:
    if not isinstance(block, dict):
        return {**event, "rpc_error": "eth_getBlockByNumber returned no block"}
    transactions = block.get("transactions")
    if not isinstance(transactions, list):
        transactions = []
    described: list[dict[str, object]] = []
    for index, tx in enumerate(transactions):
        if not isinstance(tx, dict):
            continue
        summary = input_summary(tx.get("input", tx.get("data")))
        if tx.get("to") is None:
            kind = "contract_creation"
        elif summary["input_bytes"] == 0:
            kind = "native_transfer_or_empty_call"
        else:
            kind = "contract_call"
        receipt = receipts.get(index)
        if not isinstance(receipt, dict):
            receipt = {}
        described.append(
            {
                "index": index,
                "hash": tx.get("hash"),
                "from": tx.get("from"),
                "to": tx.get("to"),
                "kind": kind,
                "nonce": hex_int(tx.get("nonce")),
                "type": tx.get("type"),
                "value": tx.get("value"),
                "gas": hex_int(tx.get("gas")),
                **summary,
                "receipt_status": receipt.get("status"),
                "receipt_gas_used": hex_int(receipt.get("gasUsed")),
                "receipt_logs": len(receipt.get("logs", [])) if isinstance(receipt.get("logs"), list) else None,
                "contract_address": receipt.get("contractAddress"),
            }
        )
    return {
        **event,
        "block_hash": block.get("hash"),
        "block_timestamp": block.get("timestamp"),
        "block_tx_count": len(transactions),
        "transactions": described,
        "process_io": process_io_snapshot(),
    }


class Watcher:
    def __init__(self, args: argparse.Namespace) -> None:
        self.args = args
        self.stop = False
        self.seen_blocks: set[int] = set()
        self.seen_traces: set[str] = set()
        self.out_dir = Path(args.out_dir)
        self.out_dir.mkdir(parents=True, exist_ok=True)
        self.events = (self.out_dir / "events.jsonl").open("a", encoding="utf-8")
        self.stdout = (self.out_dir / "watcher.log").open("a", encoding="utf-8")

    def close(self) -> None:
        self.events.close()
        self.stdout.close()

    def emit(self, event: dict[str, object]) -> None:
        line = json.dumps(event, separators=(",", ":"), ensure_ascii=False)
        self.events.write(line + "\n")
        self.events.flush()
        self.stdout.write(line + "\n")
        self.stdout.flush()
        print(line, flush=True)

    def fetch_block(self, number: int, event: dict[str, object]) -> None:
        last_error: str | None = None
        for attempt in range(self.args.rpc_retries):
            try:
                block = rpc_call(self.args.rpc_url, "eth_getBlockByNumber", [hex(number), True], self.args.rpc_timeout)
                transactions = block.get("transactions", []) if isinstance(block, dict) else []
                calls = [
                    (index + 1, "eth_getTransactionReceipt", [tx.get("hash")])
                    for index, tx in enumerate(transactions)
                    if isinstance(tx, dict) and tx.get("hash")
                ]
                receipts = rpc_batch(self.args.rpc_url, calls, self.args.rpc_timeout) if calls else {}
                by_index = {ident - 1: receipt for ident, receipt in receipts.items()}
                self.emit(describe_block(block, by_index, event))
                return
            except (OSError, RuntimeError, ValueError, KeyError) as error:
                last_error = str(error)
                time.sleep(self.args.rpc_retry_delay)
        self.emit({**event, "rpc_error": last_error or "RPC lookup failed"})

    def handle_line(self, line: str) -> None:
        log_time = parse_log_time(line) or utc_now()
        rpc_match = RPC_SLOW_RE.search(line)
        if rpc_match and float(rpc_match.group("total")) >= self.args.rpc_threshold_ms:
            trace = rpc_match.group("trace")
            if trace not in self.seen_traces:
                self.seen_traces.add(trace)
                self.emit(
                    {
                        "kind": "rpc_slow",
                        "log_time_utc": log_time,
                        "method": rpc_match.group("method"),
                        "outcome": rpc_match.group("outcome"),
                        "trace": trace,
                        "total_ms": float(rpc_match.group("total")),
                        "callback_ms": float(rpc_match.group("callback")),
                    }
                )
        block_match = SLOW_BLOCK_RE.search(line)
        if not block_match:
            return
        block_number = int(block_match.group("block").replace(",", ""))
        if block_number in self.seen_blocks:
            return
        self.seen_blocks.add(block_number)
        self.fetch_block(
            block_number,
            {
                "kind": "slow_sequence_block",
                "log_time_utc": log_time,
                "block_number": block_number,
                "sequence_elapsed_ms": float(block_match.group("elapsed")) * 1000,
                "log_threshold_seconds": float(block_match.group("limit")),
                "log_num_txs": int(block_match.group("num_txs")),
                "log_success": block_match.group("success") == "true",
            },
        )

    def run(self) -> None:
        since = self.args.since
        while not self.stop:
            command = [
                "docker",
                "logs",
                "--follow",
                "--timestamps",
                "--since",
                since,
                self.args.container,
            ]
            try:
                with subprocess.Popen(command, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, bufsize=1) as process:
                    if process.stdout is not None:
                        for line in process.stdout:
                            if self.stop:
                                break
                            self.handle_line(line.rstrip("\n"))
                since = "1s"
            except OSError as error:
                self.emit({"kind": "watcher_error", "time_utc": utc_now(), "error": str(error)})
            if not self.stop:
                time.sleep(2)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--container", default="main-nitro-1")
    parser.add_argument("--rpc-url", default="http://127.0.0.1:1100")
    parser.add_argument("--out-dir", required=True)
    parser.add_argument("--since", default="10s", help="Docker log lookback on first connection")
    parser.add_argument("--rpc-threshold-ms", type=float, default=1000.0)
    parser.add_argument("--rpc-timeout", type=float, default=5.0)
    parser.add_argument("--rpc-retries", type=int, default=5)
    parser.add_argument("--rpc-retry-delay", type=float, default=0.5)
    args = parser.parse_args()
    watcher = Watcher(args)

    def stop_handler(_signum: int, _frame: object) -> None:
        watcher.stop = True

    signal.signal(signal.SIGINT, stop_handler)
    signal.signal(signal.SIGTERM, stop_handler)
    try:
        watcher.run()
    finally:
        watcher.close()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
