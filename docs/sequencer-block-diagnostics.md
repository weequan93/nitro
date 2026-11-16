# Sequencer block latency diagnostics

Enable `execution.block-diagnostics` to investigate individual slow sequencing
operations. The feature is off by default and does not change the RPC stage
logging threshold. It uses the existing geth revision; no geth patch is needed.

The diagnostic boundary is `SequenceTransactions`, including acquisition of the
block-creation mutex. A monotonically increasing process-local `seq_id` joins
the log, runtime task, snapshot filename, and CPU profile labels. It is not an
RPC trace ID. One operation can include multiple transactions or retries.

## What is measured

| Observation | Meaning |
| --- | --- |
| `engine_lock_wait` | Wall time acquiring the block-creation mutex |
| `insert_retry_wait` | Chosen-sequencer/retry delay following an insertion conflict |
| `state_open`, `previous_block_read` | Opening state and obtaining the previous block |
| `produce_block` | Entire `ProduceBlockAdvanced` call |
| `arbos_setup` | Opening ArbOS, pricing/header setup and initial transaction preparation |
| `next_transaction` | Obtaining the next transaction from sequencing hooks |
| `transaction_attempt` | One attempted transaction, including failed/filtered attempts |
| `pre_filter`, `transaction_prepare` | Pre-execution filters, poster-cost/Brotli calculation and EVM setup |
| `apply_transaction` | EVM transaction application, result filtering and receipt creation |
| `post_filter` | Post-execution filters inside transaction application |
| `block_filter`, `finalize_block`, `assemble_block` | Block filters, final state/header work and assembly |
| `consensus_write`, `block_write` | Consensus publication and blockchain/state persistence |
| `state_readers.account/storage/code/code_size` | Calls crossing the StateDB reader boundary, with count, total wall and maximum wall |

These regions are nested and **must not be added together**. Reader work
includes cache access, decoding, trie/database access, scheduling and locks.
It is not physical disk time, and StateDB's own in-memory hits do not cross the
reader boundary. Cache-backed reads can consume CPU. Background prefetch and
commit workers are not charged as primary-goroutine reader work.

The reader wrapper preserves the original database, trie types, caches,
optional reader statistics and returned data/error ownership. StateDB copies
share observation; failed attempts and rollback work remain visible. There is
no EVM opcode tracer and no goroutine-to-thread pinning.

## Log observation

Add these arguments to the node's normal execution arguments in a candidate
configuration, then use the established graceful switch procedure:

```text
--execution.block-diagnostics.enable
--execution.block-diagnostics.threshold=500ms
```

Slow operations produce `Sequencer block diagnostics` with a JSON `timings`
field. It contains schema version, operation ID, UTC start, outcome, actual and
attempted block, gas, transaction count, stage timings and reader timings.
Failure/panic observations preserve the original return/panic behavior.

The execution path sends reports to a bounded queue of 32 entries after
releasing the block mutex. Logging and artifact writing run in an engine child
goroutine. Overload drops observations, counted by
`arb_blockdiag_reports_dropped`; it never waits for report consumption.
The context sampler now retains this metric. The slow-block watcher preserves
both text and JSON diagnostic records as `kind=block_diagnostics`, without
performing additional RPC requests. Use the updated watcher in a subsequent
capture; a running suite keeps its frozen tools.

Log-only observation measures reader/phase wall times. To separate lock waits,
scheduling and computation, use the bounded runtime capture below.

## Bounded runtime capture

Example additional arguments, using a private writable **container** path on
an existing persistent mount:

```text
--execution.block-diagnostics.capture-delay=5m
--execution.block-diagnostics.capture-duration=10m
--execution.block-diagnostics.output-dir=/diagnostics
--execution.block-diagnostics.cpu-profile
--execution.block-diagnostics.trace-window=10s
--execution.block-diagnostics.trace-max-mib=32
--execution.block-diagnostics.max-snapshots=3
--execution.block-diagnostics.snapshot-interval=1m
--execution.block-diagnostics.max-output-mib=256
```

There is one capture window per process start, capped at 30 minutes, with at
most ten snapshot attempts. Defaults retain up to three slow-block snapshots.
The worker stops capture at the duration or attempt limit and closes it during
normal node shutdown. Log observation continues afterward. Snapshot failures
also count toward the limit and are rate limited.

The runtime's trace age and memory settings are **hints**, not hard limits.
High event volume can evict a block's beginning. Each file payload is capped at
64 MiB and all trace/CPU payloads share the configured budget. Capture requires
2 GiB free at startup and before snapshots. Small JSON metadata is additional.
Check `session.json` for snapshot errors and CPU write failure. CPU files are
complete only after capture stops; analyze the completed session.

Capture adds overhead, especially when emitting a region for every reader
call. Use a short window after warm-up and compare normal RPC/resource
counters before, during and after it. Do not use the old
`sequencer.enable-profiling` simultaneously. Initial configuration rejects the
combination; the legacy per-block profiler is bypassed under diagnostics after
hot reload. A process-wide profiler already in use prevents capture startup.

Artifacts are written into a private `sequencer-diagnostics-*` directory:

```text
session.json
cpu.pprof
seq-ID.trace
seq-ID.trace.json
```

Run the offline analyzer with Go at least as new as the recording runtime:

```bash
python3 -B scripts/report-sequencer-block-trace.py \
  /path/to/completed-session/seq-ID.trace > /path/to/block-report.json
```

The adjacent record and CPU profile are selected automatically; `--record`,
`--cpu` and `--go` override their locations. The analyzer does not contact a
node. It requires complete task and region boundaries. An evicted/truncated
task is unavailable, never an all-zero latency breakdown.

The report partitions the primary goroutine's task wall time into Go-running
wall, runnable delay, mutex wait, channel/select wait, other waits,
syscall/cgo wall and unknown. It shows this breakdown for each phase and the
union of state-reader regions, plus blocking stacks. Event header goroutine
IDs are not used as the target of unblock transitions.

Go-running wall is not exact CPU: it can include runtime/OS pauses.
Syscall/cgo wall can also contain computation. The separate CPU profile report
selects only samples labeled with the operation ID and reports flat and
cumulative function hotspots. The sampled estimate can be sparse and child
goroutines can inherit the label. No matching samples means unknown, not zero
computation. Do not use the total CPU of an unfiltered profile as block CPU.

For example, a synthetic test with a 20 ms state-reader mutex wait and an
80 ms busy loop was decoded as approximately 20 ms mutex waiting and 80 ms
Go-running wall, with matching CPU samples. This validates classification;
it is not evidence about production latency or instrumentation overhead there.

To attribute physical storage latency, inspect syscall/wait stacks within the
reader regions alongside the existing device queue/await, process I/O and PSI
capture. A shared device's await value alone cannot establish the cause of a
particular request. Never label `total - reader wall` as computation.

## Build and checks

The existing build helper accepts a fourth, full base-image SHA argument. Pin
it to the current reviewed image rather than a mutable tag. It checks the
running image still matches, preserves runtime layers/configuration, builds
and tests in a bounded worker, and only assembles a candidate; it does not
activate it. Keep build/runtime evidence on the host.

```bash
BUILD="$(mktemp -d /data/rpc-read-build.XXXXXX)"
bash scripts/build-rpc-read-concurrency-image.sh \
  /data/nitro "$BUILD" SOURCE_COMMIT_SHA BASE_IMAGE_SHA256
```

Validation includes race tests, state-copy/rollback reader semantics, real
EVM storage execution with pre/post/block filtering, block hash/state-root and
receipt equality, panic/mutex release, bounded report delivery, real runtime
snapshots and CPU gzip completion. Offline Python tests cover trace accounting,
unblock IDs, missing boundaries, CPU-label selection and log parsing.

```bash
go test -mod=readonly -race ./util/blockdiag
go test -mod=readonly -race ./execution/gethexec \
  -run '^Test(Sequencer.*Mutex|BlockDiagnosticsExecutionEquivalence)'
python3 -B scripts/test_report_sequencer_block_trace.py
python3 -B scripts/test_capture_sequencer_context.py
```

Set `BLOCKDIAG_FIXTURE_DIR` for the Go capture test to retain its synthetic
session. Set `BLOCKDIAG_TEST_TRACE` and optionally `BLOCKDIAG_TEST_GO` for the
Python test to validate that real session end to end.
