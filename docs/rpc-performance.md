# RPC performance and optional diagnostics

The go-ethereum submodule provides two log-index optimizations. Matcher batches
with at least eight candidate values reuse a single block's receipts and an
incremental lookup cursor. Reuse requires the same chain view, complete index
range and block; decreasing candidate indices restart the cursor. Smaller
batches and other matcher backends retain the original lookup path.

The index writer also remembers empty base rows for one head group. The bitmap
uses 8 KiB with default parameters and is capped at 64 KiB. Every row writer
updates it using the entire combined group. Group/parameter changes, index
initialization/reset and failed writes prevent stale absence information from
being reused. Matchers continue reading the database. Neither optimization
depends on diagnostic flags or changes the database format.

Nitro's transaction prechecker skips its filtering simulation when the built-in
filter wrapper has no address checker. It resolves that condition per request;
installing a checker restores simulation, and unknown filter implementations
retain their existing checks. The simulated beacon used by tests protects its
blob bundle map during concurrent mining/reads and preserves bundles across
repeated initialization.

## Configuration

| Nitro option | Default | Effect when enabled |
| --- | --- | --- |
| `--rpc.slow-call-threshold` | `0` (off) | Successful slow HTTP/WS calls produce bounded timing warnings. |
| `--rpc.stage-diagnostics-threshold` | `0` (off) | Request-local getLogs stages, sendRaw outer timings, and completion/1s/3s/5s counters. |
| `--rpc.map-write-diagnostics` | `false` (off) | Detailed index-write timings and counters; also requires `--metrics`. |
| `--persistent.pebble.compaction-output-bytes-per-second` | `0` (off) | Paces compaction SST output for `l2chaindata`; negative rates are rejected. |

Slow-call and stage thresholds are independent; nonpositive durations disable
their features. Ordinary node metrics retain their existing `--metrics` control.
Enabling metrics or tracing an RPC does not enable detailed writer diagnostics.
The writer flag passes through node/backend configuration to each indexer and
is fixed for that indexer's lifetime.

With optional diagnostics off, no request trace, diagnostic output worker,
writer observation, per-row diagnostic clock calls, or diagnostic queue is
created. Writer diagnostic counters and output resources initialize lazily on
enabled observation emission. Small flag/atomic/nil checks and pre-existing
ordinary metrics/logging work remain.

Slow-call warnings omit params/results and bound request IDs (oversized IDs
become fingerprints). Stage and writer details use bounded asynchronous queues
and rate limits; suppressed or dropped details do not provide a complete request
or write history. Stage threshold counters count completed calls independently
of detail drops. Process-global and overlapping stage measurements must not be
summed into an individual request's causal wait budget.

When the stage threshold is positive, transaction publishing also records
microsecond histograms under `arb/txpublish/`: `state_at`, `precheck`, `filter`,
`publisher`, `forwarder_total`, `forwarder_target`, `queue_enqueue` and
`queue_wait`, `queue_pop`, `nonce_retry_wait`, `sequence`, `block_execution`,
`consensus_write` and `block_write`, plus the `inflight` gauge. These timings
contain no transaction payload or address labels. `publisher` is the full
underlying publisher call; the queue, execution and write values are nested
details and must not be added to it. `queue_pop` measures time from request
admission until the sequencer takes the item. `sequence` covers the complete
sequencer block-processing call, including lock acquisition. `block_execution`
measures transaction execution, `consensus_write` measures the sequencer
message write, and `block_write` measures the local block database write.
`nonce_retry_wait` records time spent waiting for a predecessor nonce when a
cached nonce failure is revived.
With the threshold disabled, these histograms do not receive observations.

For a read-only live view of slow sequenced blocks, run
`scripts/watch-slow-sequencer-blocks.py` on the node host. It follows the
container log, records slow `eth_sendRawTransaction` stage lines, and looks up
the reported block and receipts through JSON-RPC. Each transaction record
contains its hash, sender, target, nonce, type, value, gas, input byte length,
four-byte selector, input hash, receipt status, gas used, and log count. Full
calldata and opcode traces are intentionally omitted. The watcher also records
visible `pathdb-migrate-pipeline` process I/O counters when a slow block is
observed.

Compaction pacing shares a token bucket with a 64 KiB burst across chain-database
compaction output writers. It excludes foreground reads, WAL, manifest operations,
flushes, read-only opens, and WASM/consensus databases. Write stalls bypass waits;
close releases limiter waits before normal database shutdown. This limits SST
output writes, not device IOPS or total read/write traffic. A low rate can grow
compaction debt; enable it only with workload-specific observation.

## Validation

The earlier performance layers passed native server Linux/amd64 regression,
race, fuzz and benchmark gates before their controlled rollout. The latest
explicit diagnostic opt-in revision was checked locally with Go 1.25.14 on
Linux/amd64 in an isolated container with networking disabled:

- Fresh race-enabled processes passed both default-off resource checks, including
  metrics plus a previous RPC trace, and explicit writer opt-in without metrics.
- The targeted four-package race suite passed 33 top-level tests, and the full
  filtermaps regression passed 33 top-level entries, including fuzz seeds. The
  two process-global checks intentionally skip in combined runs and passed
  independently above.
- Arbitrum and Ethereum backend wiring compiled. All three tests using the exact
  Nitro RPC configuration files passed with the race detector; this was a
  standalone configuration check, not a new full Nitro/WASM build.
- Five alternating default-off write benchmark pairs preserved allocations
  (175 cached, 460,083 uncached) and passed the existing regression budgets.
  These in-memory fixtures do not establish production latency or throughput.

Correctness coverage includes unchanged query replies and database contents,
reorg/head truncation, cancellation, read-error retries, partial commits,
concurrent queries/index updates and bounded diagnostic output. Fresh-process
checks can be rerun separately from the go-ethereum directory:

```sh
go test -race -count=1 ./core/filtermaps -run '^TestMapWriteDiagnosticsNeverEnabledResources$'
go test -race -count=1 ./core/filtermaps -run '^TestMapWriteDiagnosticsMetricsOff$'
```

The latest diagnostic flag revision needs a separately built and verified image
before rollout. Existing deployment scripts and previously verified images do
not automatically include it.
