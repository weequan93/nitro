# Nitro eth_getLogs reproduction test

`nitro-getlogs-load.py` uses Python 3's standard library. Copy this one script to
the Linux host of a **dedicated test node**. Do not run high-load tests against
the public RPC or a shared production node. It does not change configuration,
restart Nitro, or force garbage collection.

## First run: random single blocks, all addresses

Run as root for Docker namespace metrics and `/proc` RSS access:

```bash
python3 nitro-getlogs-load.py \
  --container node-nitro-1 \
  --url http://127.0.0.1:8449 \
  --duration 120 --concurrency 4 --rps 4 \
  --lookback 10000 --max-range 1
```

The random block population is fixed at startup relative to that node's head;
it never selects future blocks. `--max-range 1` is the original single-block
query. `--seed 1` is the default. Use explicit bounds and the same seed for a
reproducible query sequence across builds:

```bash
python3 nitro-getlogs-load.py \
  --container node-nitro-1 \
  --from-block 120686357 --to-block 120686357 \
  --duration 60 --max-requests 100 --concurrency 2 --rps 2
```

## Exercise indexed matching

An unrestricted address/topic query can fall back to **unindexed** scanning even
when indexing is enabled (`ErrMatchAll`). To investigate the observed
`potentialMatches`/`mergeResults` problem, also test with a real, frequently
matching contract address or event topic from the failing workload:

```bash
python3 nitro-getlogs-load.py \
  --container node-nitro-1 \
  --address 0xYOUR_40_HEX_CHARACTER_CONTRACT_ADDRESS \
  --duration 120 --concurrency 4 --rps 4 \
  --lookback 10000 --max-range 100
```

Replace the address placeholder. Alternatively use `--topic 0x...` with an actual
32-byte topic; repeated `--topic` options represent successive topic positions,
not OR alternatives. Compare single-block queries first, then increase ranges.
Confirm indexing is enabled and covers the tested range. Matching real traffic
matters: a nonexistent address may not allocate the same candidate buffers.

## Increase load in separate stages

After the baseline and cooldown are healthy, use, for example:

```bash
python3 nitro-getlogs-load.py \
  --container node-nitro-1 \
  --duration 300 --max-requests 5000 \
  --concurrency 16 --rps 20 \
  --lookback 10000 --max-range 1
```

Add the same real address/topic filter to exercise that indexed workload. Change
one dimension at a time. `--rps` is the total target admission rate, not per
worker; achieved throughput can be lower when calls are slow. `--rps 0` removes
rate pacing, but concurrency, duration, and request count remain bounded.
Duration controls new admissions; in-flight client calls and cooldown extend the
total runtime. Do not run the next stage if server workers from the last remain
stuck. A client timeout does not prove its server work has finished.

## Metrics and safeguards

- Metrics are sampled every 5 seconds; 30 seconds of cooldown follow the last
  client completion. Adjust with `--interval` and `--cooldown`.
- `--container` checks the container PID/start time each sample, reads main-process
  RSS/swap, and runs `curl` in the container network namespace against
  `http://127.0.0.1:6070/debug/metrics`. The host needs Docker, nsenter and curl.
  Ensure `--url` is routed to this same Nitro container, not a load balancer.
- Without `--container`, metrics must be directly reachable via `--metrics-url`;
  process RSS and its stop threshold are unavailable.
- Metrics failure at startup aborts before load. Three consecutive monitoring
  failures during a run stop new requests; a container identity change does so
  immediately. `--no-metrics` explicitly disables Go metrics/heap safeguards.
- Defaults stop **new admissions** at 24 GiB main-process RSS, 20 GiB Go
  HeapAlloc, or 2,000 goroutines. Customize `--stop-rss-gib`, `--stop-heap-gib`,
  and `--stop-goroutines` to leave headroom below the host/container's actual
  limits. Zero disables an individual threshold. These are sampled guards,
  **not hard memory limits**, and cannot stop already-stuck server work.
- Ctrl-C stops admissions and waits for current client calls; a second Ctrl-C
  during cooldown skips the remaining observation period.
- Responses are streamed rather than kept in full. Each is capped at 32 MiB
  client-side (`--max-response-mib`); reaching that cap closes the connection but
  does not impose a server memory cap. The timeout is applied to socket I/O and
  checked while streaming, not an OS-enforced process kill deadline.

## Outputs

A fresh `/tmp/nitro-getlogs-*` directory is printed at startup (or provide a new
path via `--output`). Files:

- `config.json`: exact test parameters, chain ID, head, random bounds, process identity.
- `requests.jsonl`: request ID/filter, latency, HTTP status, response bytes,
  JSON-RPC errors, and log count for small validated responses. JSON-RPC errors
  are distinguished from HTTP errors and client timeouts. Responses over 8 KiB
  are reported as `http_200_large` without claiming full JSON validation.
- `metrics.jsonl`: timestamps, Go heap breakdown, GC count, goroutines, RSS,
  completed requests, outcomes and client in-flight count.
- `summary.json`: totals and latency percentiles for **all outcomes**, not just
  successful requests. Request-level logs permit separate success/error analysis.

Watch **HeapAlloc, RSS and goroutine trends**, especially during cooldown.
`Sys` includes runtime address space; `HeapReleased` is memory returned to the
OS. Rising Sys alone is not evidence of a retained-memory leak. HeapAlloc can
include garbage awaiting collection; this test deliberately does not force GC.
Other traffic and chain activity continue affecting the node's metrics, so use
an isolated test node. No load test is proof of a leak without profiles and
repeatable growth under a controlled workload.

## Local script tests (no real node)

```bash
python3 -B scripts/test_nitro_getlogs_load.py
```
