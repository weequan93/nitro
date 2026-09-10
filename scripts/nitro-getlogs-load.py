#!/usr/bin/env python3
"""Bounded eth_getLogs reproduction test; Python 3 standard library only.

Run against a dedicated test node, not a shared production endpoint.
Metrics use Docker's network namespace when --container is supplied.
No node configuration is changed and no GC or restart is triggered.
"""

import argparse
import collections
import concurrent.futures
import datetime
import http.client
import json
from pathlib import Path
import random
import socket
import subprocess
import tempfile
import threading
import time
import urllib.parse


GIB = 1024 ** 3


def timestamp():
    return datetime.datetime.now(datetime.timezone.utc).isoformat()


def fetch(url, payload=None, timeout=20, max_bytes=32 * 1024 * 1024):
    """Stream responses, retaining only a small preview to avoid client heap growth."""
    target = urllib.parse.urlsplit(url)
    if target.scheme not in ("http", "https") or not target.hostname:
        raise ValueError("URL must use http:// or https://")
    cls = http.client.HTTPSConnection if target.scheme == "https" else http.client.HTTPConnection
    conn = cls(target.hostname, target.port, timeout=timeout)
    body = json.dumps(payload).encode() if payload is not None else None
    path = urllib.parse.urlunsplit(("", "", target.path or "/", target.query, ""))
    started = time.monotonic()
    preview = bytearray()
    size = 0
    try:
        conn.request("POST" if body else "GET", path, body,
                     {"Content-Type": "application/json", "Connection": "close"})
        response = conn.getresponse()
        while True:
            remaining = timeout - (time.monotonic() - started)
            if remaining <= 0:
                raise TimeoutError("client deadline exceeded")
            if conn.sock is not None:
                conn.sock.settimeout(remaining)
            chunk = response.read1(min(65536, max_bytes - size + 1))
            if not chunk:
                break
            size += len(chunk)
            preview.extend(chunk[:max(0, 8192 - len(preview))])
            if size > max_bytes:
                return response.status, size, bytes(preview), True
        return response.status, size, bytes(preview), False
    finally:
        conn.close()


def rpc(url, method, params, request_id=1):
    print("Preflight: %s at %s" % (method, url), flush=True)
    try:
        status, _, body, limited = fetch(url, {
            "jsonrpc": "2.0", "method": method, "params": params, "id": request_id,
        })
        if status != 200 or limited:
            raise ValueError("HTTP status %s; body=%r" % (status, body[:256]))
        answer = json.loads(body)
        if "error" in answer:
            raise ValueError("JSON-RPC error: %s" % answer["error"])
        return answer["result"]
    except Exception as exc:
        raise ValueError("preflight %s at %s failed: %s. No eth_getLogs load was started."
                         % (method, url, exc)) from exc


def container_identity(name):
    result = subprocess.run(
        ["docker", "inspect", "-f", "{{.State.Pid}} {{.State.StartedAt}}", name],
        check=True, capture_output=True, text=True, timeout=5,
    )
    pid, started = result.stdout.strip().split(maxsplit=1)
    if int(pid) <= 0:
        raise ValueError("container is not running")
    return {"pid": int(pid), "started_at": started}


def read_metrics(args, identity):
    record = {"time": timestamp()}
    if identity:
        current = container_identity(args.container)
        if current != identity:
            raise ValueError("container identity changed; do not combine process lifetimes")
        record.update(identity)
        # PID is the main Nitro process for this deployment, not its validators.
        for line in Path("/proc/%d/status" % identity["pid"]).read_text().splitlines():
            if line.startswith(("VmRSS:", "VmSwap:")):
                key, value, _ = line.split()
                record["rss_gib" if key == "VmRSS:" else "swap_gib"] = int(value) / 1048576
    if args.no_metrics:
        return record
    if identity:
        raw = subprocess.run(
            ["nsenter", "-t", str(identity["pid"]), "-n", "curl", "-fsS",
             "--max-time", "4", args.metrics_url],
            check=True, capture_output=True, text=True, timeout=5,
        ).stdout
    else:
        # Metrics may be larger than the small preview used for query responses.
        target = urllib.parse.urlsplit(args.metrics_url)
        cls = http.client.HTTPSConnection if target.scheme == "https" else http.client.HTTPConnection
        connection = cls(target.hostname, target.port, timeout=4)
        try:
            connection.request("GET", (target.path or "/") + ("?" + target.query if target.query else ""))
            response = connection.getresponse()
            if response.status != 200:
                raise ValueError("metrics HTTP status %s" % response.status)
            raw = response.read(16 * 1024 * 1024)
        finally:
            connection.close()
    data = json.loads(raw)
    mem = data["memstats"]
    for field in ("HeapAlloc", "HeapInuse", "HeapIdle", "HeapReleased", "Sys", "NextGC"):
        record[field + "_GiB"] = mem[field] / GIB
    record["idle_unreleased_GiB"] = (mem["HeapIdle"] - mem["HeapReleased"]) / GIB
    record["NumGC"] = mem["NumGC"]
    record["goroutines"] = data.get("system/cpu/goroutines")
    return record


def arguments():
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument("--url", default="http://127.0.0.1:8449")
    p.add_argument("--container", help="Docker name; enables namespace metrics and main-process RSS (run as root)")
    p.add_argument("--metrics-url", default="http://127.0.0.1:6070/debug/metrics")
    p.add_argument("--no-metrics", action="store_true", help="explicitly allow running without Go metrics/heap guard")
    p.add_argument("--duration", type=float, default=120, help="seconds admitting requests (default 120)")
    p.add_argument("--max-requests", type=int, default=1000)
    p.add_argument("--concurrency", type=int, default=4)
    p.add_argument("--rps", type=float, default=4, help="total target requests/sec, not per worker; 0 = uncapped")
    p.add_argument("--timeout", type=float, default=20, help="client deadline/IO timeout; keep above server timeout")
    p.add_argument("--cooldown", type=float, default=30, help="observe metrics after all client workers finish")
    p.add_argument("--interval", type=float, default=5, help="metrics interval in seconds")
    p.add_argument("--from-block", type=lambda s: int(s, 0), help="inclusive decimal or 0x lower bound")
    p.add_argument("--to-block", type=lambda s: int(s, 0), help="inclusive upper bound; default head at test start")
    p.add_argument("--lookback", type=int, default=10000, help="default random block population below starting head")
    p.add_argument("--max-range", type=int, default=1, help="random query length 1..N blocks; default single block")
    p.add_argument("--address", help="optional 0x contract address; omitted = all addresses, like the original curl")
    p.add_argument("--topic", action="append", default=[], help="32-byte event topic; repeat for successive topic positions")
    p.add_argument("--seed", type=int, default=1)
    p.add_argument("--max-response-mib", type=int, default=32, help="client response byte cap (does not cap server memory)")
    p.add_argument("--stop-rss-gib", type=float, default=24, help="stop admission at main-process RSS; needs --container; 0 disables")
    p.add_argument("--stop-heap-gib", type=float, default=20, help="stop admission at HeapAlloc; 0 disables")
    p.add_argument("--stop-goroutines", type=int, default=2000, help="stop admission at this count; 0 disables")
    p.add_argument("--output", help="new output directory; must not already exist")
    args = p.parse_args()
    for name in ("duration", "max_requests", "concurrency", "timeout", "interval", "lookback", "max_range", "max_response_mib"):
        if getattr(args, name) <= 0:
            p.error("--%s must be positive" % name.replace("_", "-"))
    for name in ("rps", "cooldown", "stop_rss_gib", "stop_heap_gib", "stop_goroutines"):
        if getattr(args, name) < 0:
            p.error("--%s cannot be negative" % name.replace("_", "-"))
    if args.address and (len(args.address) != 42 or not args.address.startswith("0x") or
                         any(c not in "0123456789abcdefABCDEF" for c in args.address[2:])):
        p.error("--address must be a 20-byte 0x hexadecimal address")
    if len(args.topic) > 4 or any(len(t) != 66 or not t.startswith("0x") or
                                 any(c not in "0123456789abcdefABCDEF" for c in t[2:]) for t in args.topic):
        p.error("use at most four --topic values, each a 32-byte 0x hexadecimal topic")
    return args


def main():
    args = arguments()
    identity = container_identity(args.container) if args.container else None
    head = int(rpc(args.url, "eth_blockNumber", []), 16)
    chain_id = rpc(args.url, "eth_chainId", [])
    upper = head if args.to_block is None else args.to_block
    lower = max(0, upper - args.lookback + 1) if args.from_block is None else args.from_block
    if not 0 <= lower <= upper <= head:
        raise ValueError("require 0 <= from-block <= to-block <= current node head (%d)" % head)
    # Fail closed before issuing load if monitoring is unavailable.
    try:
        baseline = read_metrics(args, identity)
    except Exception as exc:
        raise ValueError("metrics preflight at %s failed: %s. No eth_getLogs load was started."
                         % (args.metrics_url, exc)) from exc
    if args.output:
        output = Path(args.output)
        output.mkdir(parents=True, exist_ok=False)
    else:
        output = Path(tempfile.mkdtemp(prefix="nitro-getlogs-"))
    config = dict(vars(args), chain_id=chain_id, head=head, lower=lower, upper=upper,
                  identity=identity, started_at=timestamp())
    (output / "config.json").write_text(json.dumps(config, indent=2) + "\n")
    print("Output: %s\nTarget: %s chain=%s random blocks=%d..%d" % (output, args.url, chain_id, lower, upper), flush=True)
    if not identity:
        print("No --container: process RSS is unavailable; RSS stop threshold is inactive.", flush=True)
    print("Thresholds stop NEW requests only; they cannot kill stuck server work. Ctrl-C also stops admission.", flush=True)
    stop = threading.Event()
    monitor_done = threading.Event()
    lock = threading.Lock()
    state = {"issued": 0, "completed": 0, "outcomes": collections.Counter(), "reason": None}
    latencies = []
    started = time.monotonic()
    deadline = started + args.duration
    next_slot = [started]

    def halt(reason):
        with lock:
            if state["reason"] is None:
                state["reason"] = reason
                print("STOP admitting requests: " + reason, flush=True)
        stop.set()

    with (output / "requests.jsonl").open("w", buffering=1) as requests_file, \
            (output / "metrics.jsonl").open("w", buffering=1) as metrics_file:
        def sample(record):
            with lock:
                record.update(elapsed_s=round(time.monotonic() - started, 3),
                              issued=state["issued"], completed=state["completed"],
                              client_inflight=state["issued"] - state["completed"],
                              outcomes=dict(state["outcomes"]))
            metrics_file.write(json.dumps(record) + "\n")
            print(json.dumps(record), flush=True)
            for key, limit in (("rss_gib", args.stop_rss_gib), ("HeapAlloc_GiB", args.stop_heap_gib),
                               ("goroutines", args.stop_goroutines)):
                value = record.get(key)
                if limit and isinstance(value, (int, float)) and value >= limit:
                    halt("%s=%s reached threshold %s" % (key, value, limit))

        def monitor():
            failures = 0
            while not monitor_done.wait(args.interval):
                try:
                    sample(read_metrics(args, identity))
                    failures = 0
                except Exception as exc:
                    failures += 1
                    sample({"time": timestamp(), "metrics_error": str(exc)})
                    if failures >= 3 or "identity changed" in str(exc):
                        halt("monitoring unavailable or container restarted")

        def worker():
            while not stop.is_set():
                # Reserve only a rate slot, not a request; concurrency bounds this queue.
                with lock:
                    now = time.monotonic()
                    if now >= deadline or state["issued"] >= args.max_requests:
                        return
                    slot = max(now, next_slot[0])
                    if args.rps:
                        next_slot[0] = slot + 1 / args.rps
                if slot >= deadline or stop.wait(max(0, slot - time.monotonic())):
                    return
                with lock:
                    if stop.is_set() or time.monotonic() >= deadline or state["issued"] >= args.max_requests:
                        return
                    state["issued"] += 1
                    request_id = state["issued"]
                rng = random.Random(args.seed + request_id)
                length = rng.randint(1, min(args.max_range, upper - lower + 1))
                first = rng.randint(lower, upper - length + 1)
                query = {"fromBlock": hex(first), "toBlock": hex(first + length - 1)}
                if args.address:
                    query["address"] = args.address
                if args.topic:
                    query["topics"] = args.topic
                row = {"time": timestamp(), "id": request_id, "filter": query}
                begin = time.monotonic()
                try:
                    code, size, preview, limited = fetch(args.url, {
                        "jsonrpc": "2.0", "method": "eth_getLogs", "params": [query], "id": request_id,
                    }, args.timeout, args.max_response_mib * 1024 * 1024)
                    row.update(http_status=code, response_bytes=size)
                    if limited:
                        row["outcome"] = "response_limit"
                    elif code != 200:
                        row["outcome"] = "http_error"
                    elif size > len(preview):
                        row["outcome"] = "http_200_large"  # not fully JSON-validated
                    else:
                        answer = json.loads(preview)
                        if "error" in answer:
                            row.update(outcome="rpc_error", error=answer["error"])
                        elif isinstance(answer.get("result"), list):
                            row.update(outcome="ok", log_count=len(answer["result"]))
                        else:
                            row["outcome"] = "invalid_rpc_response"
                    if row["outcome"] not in ("ok", "http_200_large"):
                        row["preview"] = preview[:512].decode(errors="replace")
                except (TimeoutError, socket.timeout) as exc:
                    row.update(outcome="client_timeout", error=str(exc))
                except Exception as exc:
                    row.update(outcome="client_error", error=str(exc))
                row["latency_s"] = round(time.monotonic() - begin, 6)
                with lock:
                    requests_file.write(json.dumps(row) + "\n")
                    state["completed"] += 1
                    state["outcomes"][row["outcome"]] += 1
                    latencies.append(row["latency_s"])

        sample(baseline)
        thread = threading.Thread(target=monitor, daemon=True)
        thread.start()
        pool = concurrent.futures.ThreadPoolExecutor(max_workers=args.concurrency)
        futures = [pool.submit(worker) for _ in range(args.concurrency)]
        try:
            for future in futures:
                future.result()
        except KeyboardInterrupt:
            halt("operator interrupted; waiting for current client calls")
        finally:
            stop.set()
            pool.shutdown(wait=True)
        load_ended = time.monotonic()
        print("Client load finished. Observing cooldown for %ss; server work may still be active." % args.cooldown, flush=True)
        try:
            monitor_done.wait(args.cooldown)
        except KeyboardInterrupt:
            print("Cooldown interrupted.", flush=True)
        finally:
            monitor_done.set()
            thread.join()
        try:
            sample(read_metrics(args, identity))
        except Exception as exc:
            sample({"time": timestamp(), "metrics_error": str(exc)})
    ordered = sorted(latencies)

    def percentile(p):
        return ordered[min(len(ordered) - 1, int((len(ordered) - 1) * p))] if ordered else None

    summary = dict(state, outcomes=dict(state["outcomes"]),
                   load_wall_s=round(load_ended - started, 3),
                   completed_rps=round(state["completed"] / max(.001, load_ended - started), 3),
                   latency_p50_s=percentile(.5), latency_p95_s=percentile(.95),
                   latency_max_s=max(ordered) if ordered else None,
                   finished_at=timestamp(), output=str(output))
    (output / "summary.json").write_text(json.dumps(summary, indent=2) + "\n")
    print("SUMMARY\n" + json.dumps(summary, indent=2), flush=True)


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        raise SystemExit("ERROR: %s" % error)
