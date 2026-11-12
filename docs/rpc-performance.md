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
| `--rpc.stage-diagnostics-threshold` | `0` (off) | Request-local getLogs stages, sendRaw outer timings, completion counts and 10/25/50/100/250/500ms/1/3/5s threshold counters. |
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

To collect resource context and workload controls alongside that watcher, run:

```bash
python3 -u /data/scripts/capture-sequencer-context.py \
  --out-dir /data/sequencer-context-UNIQUE --minutes 60
```

This Linux host script requires only Python's standard library and Docker read
access. It writes independent device I/O rates, node and migration process CPU,
physical I/O counters, major faults, host memory/PSI and cgroup v2 CPU/memory
limits and counters to `resources.jsonl`, normally once per second. Device
rates do not imply shared disks and must not be summed across storage layers.
Process rates use PID/start-time identity; missing or reset counters are null.
The process CPU unit is cores, where 1.0 means one fully used core.

An independent worker writes `context.jsonl` roughly every 15 seconds, retaining
selected metrics and one latest block with transaction targets, selectors,
input lengths, block gas used and decoded price counts for the known
PriceOracle `batchSetPrices` method. Sampled blocks are unclassified workload
controls, not proven fast blocks; their individual execution time is unknown.
Join their block hashes/numbers and UTC timestamps with the slow-block watcher
and rolling metrics. Metrics retain their original units (txpublish histograms
are microseconds); their counts are cumulative and quantiles are a rolling
one-minute window, not request-ID joins or additive per-request wait budgets.

The same metrics read also retains `state_snapshot_generation_*` counters to
separate snapshot generation activity from compaction activity. Duration
counters retain **nanoseconds**, while generated/recovered counters count
processed entries. Differences require a stable node process and non-reset
counters; elapsed stage time is neither CPU time nor physical I/O volume.
Snapshot log `slots` and `storage` include recovered existing entries, so they
do not establish newly created chain state or physical database size. The
snapshot ETA uses the account-hash cursor and can grow while one large account's
storage cursor advances. It is not a reliable completion deadline in that case.
Snapshot generation, bloom-bit cleanup and compaction can overlap; these counters
alone do not attribute disk I/O to a particular background task or prove its
effect on a transaction. The report below analyzes adjacent counter deltas from
the saved `context.jsonl` without querying the node again.

The collector performs no replay, trace, profile, `eth_call`, transaction send
or service changes, and exits automatically after the requested duration
(maximum six hours). HTTP reads have response caps and three-second socket
timeouts. Existing output files are refused to prevent mixing runs. Resource
samples continue independently of slow metrics or RPC responses. Validate the
collector offline with `python3 -B scripts/test_capture_sequencer_context.py`.

For a longer window, use the companion `scripts/capture-sequencer-longrun.py`
with the collector installed beside it:

```bash
nohup python3 -B -u /data/scripts/capture-sequencer-longrun.py \
  --hours 24 --segment-minutes 60 \
  --events-file /data/sequencer-context-paced-20261111-155957/slow-blocks/events.jsonl \
  --out-dir "$OUT" >"$OUT/stdout.log" 2>&1 &
```

Create a fresh `$OUT` directory first. This bounded Linux host job runs the
same collector in `segment-0001`, `segment-0002`, etc., keeping resource files
small enough for the report's per-file size limit. Defaults retain one-second
resource and 15-second context intervals. At the observed data rate, 24 hours
requires roughly 0.6 GB; actual size depends on sampled blocks and process
metadata. The wrapper copies and hashes the collector code before starting,
and writes `run.json`, `segments.jsonl` and atomic `status.json` updates.
Progress is also printed to the root `stdout.log` once per minute.

The wrapper defaults to 24 hours, accepts up to 168 hours, and starts children
of at most six hours each. It holds a host lock, checks for an existing resource
collector and refuses a reused output directory. It checks its output size and
free disk space once per minute, stopping the owned collector if output reaches
the default 2 GiB budget or free space falls below 2 GiB. Budget checks are
periodic, so these are stop thresholds, not exact byte caps. Existing data is
retained; no files are automatically deleted. These budgets cover the new run
directory, not a separate existing watcher directory.

SIGTERM/SIGINT to the recorded parent PID stops its current collector child
and prevents further segments. A failed/early child, timeout, cancellation or
budget stop produces a `stopped` status with its reason. Normal expiry produces
`complete`; every segment's timing and exit status are recorded separately.
Hourly rollover can leave short sampling gaps, so inspect actual coverage and
analyze each segment using the existing report with the watcher events path in
`run.json`. Node restart/counter gaps retain the report's existing exclusions.
The existing watcher remains a separate process: its file path is recorded but
its process is not stopped or restarted, and the wrapper does not establish
continuous watcher coverage. Keep tuning fixed during a comparison window.
The wrapper makes no node configuration, image, database or service changes.
Run `python3 -B scripts/test_capture_sequencer_longrun.py` for offline orchestration
checks using fake collectors.

To add cache and IOPS evidence while an existing capture continues, install
`scripts/capture-main-cache-io.py` beside `capture-sequencer-context.py` and run
it for a bounded window:

```bash
python3 -B -u /data/scripts/capture-main-cache-io.py \
  --minutes 60 --device vdb --out-dir /data/main-cache-io-UNIQUE
```

This independent Linux-host sampler writes completed device operations/s,
average request KiB, byte rates, read/write awaits and queue depth to
`io.jsonl` every second. `cache.jsonl` retains the existing diagnostic metrics
plus Pebble block/table, hashdb clean/dirty trie and snapshot clean/dirty
account/storage cache counters every 15 seconds. Each metrics sample checks
the same running native Nitro container/PID/start-time before and after the
HTTP read. Cache deltas require a stable identity, successful adjacent samples,
a bounded gap and nondecreasing counters. Missing/unused counters and zero
lookups produce null hit rates. Cache counters can also include background
work and other RPCs; a hit rate cannot be assigned to one transaction.

The sampler only adds metrics GETs and reads Docker identity plus kernel
statistics; it performs no node RPC or profile and changes no tuning/service
settings. The original long-run collector keeps using its pinned code and
files. A separate host lock prevents duplicate cache samplers. The extra run
normally writes several MiB/hour. Its own output threshold is 128 MiB, and it
requires 1 GiB free space; checks once per minute may overshoot those stop
thresholds. Completion, cancellation, budget stops and worker failures are
distinguished in `status.json` and the output log. SIGTERM to its parent stops
only this sampler.

Use `python3 -B /data/scripts/capture-main-cache-io.py --report /data/main-cache-io-UNIQUE`
to summarize saved samples during or after the run. The report computes hit
percentages from summed interval hit/miss counts, retains each cache's covered
seconds and unavailable intervals, and identifies peak miss-rate and lowest
hit-rate intervals. Those timestamps approximate metrics reads, with database
metric publication delay; correlate them with the slow watcher and existing
resources rather than treating them as per-request timing. Cache misses are
logical lookups, not physical IOPS. Device statistics include every user of
that device and do not establish the cloud volume's configured limits. Run
`python3 -B scripts/test_capture_main_cache_io.py` for offline checks.

The hashdb clean-cache miss meter counts successful database reads followed by
cache refills, rather than every failed clean-cache lookup. Its displayed
hit/(hit+miss) ratio is not an exhaustive clean-cache lookup probability.
Snapshot dirty-layer misses may immediately hit the clean memory cache.
Neither meter establishes physical reads.

For a fresh comparison phase with all three monitors, use
`capture-sequencer-suite.py`. It verifies the loaded tuning and RPC/metrics,
then, with `--replace-existing`, replaces recognized main monitoring processes
while retaining their files. It runs the resource collector, hourly cache/IO
segments and one slow-event watcher under a common 24-hour deadline, recording
the runtime baseline and pinning all source files. Normal expiry or a signal
stops its owned collectors and watcher. A failed collector, runtime mismatch
at a cache boundary or disk budget stop produces `stopped` and shuts down the
other owned monitors. See [the concurrency-1 procedure](main-compaction-one-20261112.md)
for pending-JSON restart verification, output paths and startup checks.

The context sampler also retains `rpc_diagnostics_*` completion and latency
threshold counters. Slow-event log counts alone are incomplete: detail logging
is rate-limited, and `details_dropped` is shared by getLogs/sendRaw. For an
already running suite pinned before these counters were retained, install
`capture-rpc-diagnostics.py` and run it with `--suite-dir OUT`. It supplements
the existing capture until that suite exits or its original deadline passes;
it preserves pinned sources and makes no node/service changes. Its `--report`
mode sums accepted adjacent counter differences, and `--inspect-logs` splits
retained Docker records at the suite start. Missing earlier counter baselines
and rotated/dropped logs cannot be reconstructed.

Subsecond latency counts require a node image containing the new
`rpc_diagnostics_<method>_<outcome>_ge10ms`, `ge25ms`, `ge50ms`, `ge100ms`,
`ge250ms` and `ge500ms` counters. Updating the Python script alone cannot add
these metrics to an already running binary. Diagnostics must already be
enabled with a positive stage threshold; the threshold continues to control
detail logging only. Counters cover completed instrumented calls before any
log threshold, rate limit or output-queue check, with independent success and
failure denominators. No extra per-request details are logged.

The updated supplemental collector records these counters when available,
without treating their absence on older images as a base-sampling failure.
Its `status.json` includes `subsecond_counters_available`. The report marks
missing historical bucket data unavailable (`count: null`), not zero. It
subtracts adjacent cumulative threshold deltas into disjoint ranges:
`[0,10)`, `[10,25)`, `[25,50)`, `[50,100)`, `[100,250)`, `[250,500)`,
`[500,1000)`, `[1000,3000)`, `[3000,5000)` and `[5000,+inf)` milliseconds.
Each outcome includes its own valid-interval coverage, counts and percentages.
Counter scrapes are not atomic; if threshold deltas are inconsistent, that
interval is excluded from the affected outcome's bucket totals. Base RPC
counts retain their separate coverage. Restarts, counter resets, missing
samples and gaps are never counted as zero requests.

To inspect the latest five minutes of an existing capture with the updated
reporter, without changing its running collector:

```bash
python3 -B /data/scripts/capture-rpc-diagnostics.py \
  --report "$OUT/rpc-counters" --last-minutes 5 |
jq '{last_sample_sgt, sample_age_seconds, valid_interval_seconds,
     sendraw: .method_deltas.eth_sendRawTransaction,
     latency: .latency_buckets.eth_sendRawTransaction}'
```

This window ends at the latest saved sample and includes only complete
adjacent intervals within it. Check sample age before interpreting it as
current traffic. Durations measure server RPC handling, not client network
latency or transaction receipt time. Older five-minute captures can establish
the total under one second, but cannot reconstruct finer bins. The node image
and a collector that records the additional counters must both be updated to
collect new bucket data; retain existing captures as their own experiment
phase when changing the image.

`scripts/build-rpc-subsecond-image.sh` builds a candidate for the recorded
`e27ef7...` main image without activating it. Run as root on the Linux host,
passing the existing repository and a fresh `/data/rpc-subsecond-build.*`
directory. It pins Nitro `b7df6d1a17d33f304bee1ac553e00e3e35a27b93` and geth
`bf399f01dffc34785f6f1653a3ddaeed30d9d5fb` in a separate clone, builds the
native binary through the repository Dockerfile, and gates export on the RPC
diagnostic race tests, including subsecond boundaries. A dedicated BuildKit
worker uses two CPUs and 10 GiB memory, with its large cache bound under the
build directory on `/data`; preflight requires 12 GiB available memory and
50 GiB free there. It stops its build worker when done and retains its cache,
logs, source, native payload, runtime baseline and candidate image for review.

If the original build reached image assembly but failed at help verification,
the updated helper can resume just verification:

```bash
bash /path/to/updated-build-rpc-subsecond-image.sh --verify-only "$BUILD"
```

The original helper captured only stdout for `--help`; Nitro prints its option
listing on stderr. The updated helper captures both streams and requires the
probe to exit successfully. Resume checks the saved phase/source/base/image
identity, archives the previous status, and leaves the original build log
intact. It reuses the candidate by resolved image ID and also compares its
native binary checksum with the tested exported payload. Run the verification
under a separate log filename; no source checkout, compilation, image assembly
or worker startup is performed by this mode.

Assembly inherits the exact running image's layers and replaces its main
native binary. It checks the candidate's version, inherited runtime
configuration and layer prefix, then rechecks main's deployment baseline.
Only `assembled_and_verified_not_deployed` indicates build and verification
completed. `status.json` records stage timestamps; `image-tag.txt` and
`image-id.txt` identify the candidate. There is no Compose edit, service
activation, database mount, node restart or monitoring change. Exclude build
load from latency comparisons, and review the candidate separately before a
deployment. The image must be enabled and the counter collector updated in a
new output directory before subsecond bucket data becomes available.

After the verified candidate `deriw-rpc-subsecond:b7df6d1a1-EsEK2e` was
reported as image ID
`sha256:0ea40d56b156757fc121c864ba30775f7bdd8924a45dec5d59b8b6228cd50215`,
`scripts/prepare-rpc-subsecond-switch.py` can save a switch plan. Install it
beside `check-main-runtime.py` and `trial-compaction-pacing.py`. Preparation
rechecks the build evidence/current runtime, compares undeclared cache
defaults using both images' help output, and renders the original Compose
inputs plus a main-image override. Only an otherwise identical model is
accepted. It backs up the bound JSON and Compose files into a private
`main-rpc-subsecond-*` directory, then writes `plan.json` with image identities,
input/model hashes and the reviewed SIGTERM/normal-exit requirement. Its
`prepared_no_service_change` result is preparation only; it performs no node
restart or monitoring takeover. Activation and a fresh capture phase remain
separate steps after reviewing that on-server plan.

`scripts/switch-rpc-subsecond-image.py --apply PLAN` applies the prepared plan;
`--rollback PLAN` restores its original Compose image. Both acquire the image
operation lock and existing pacing lock, verify inputs/references/runtime
layout, then request Docker SIGTERM with an unlimited stop timeout. A 300-second
observer limit does not force-kill Nitro: the stop client keeps waiting and
holds the inherited locks. Recreation requires exit code zero, the signal and
`Blockchain stopped` markers, and no detected close/flush/fatal error. Shutdown
logs and stopped-container evidence are saved privately. A failed `up` can be
retried or rolled back when its matching verified normal-exit evidence exists,
including if Compose already removed the old container. Original JSON and
Compose files are never rewritten; apply adds the prepared image override,
rollback uses the saved original Compose input list. Only main/nitro is
recreated. Success is `started_health_pending`, requiring RPC/head/metrics
checks before interpreting the new phase as healthy.

The suite's `--rpc-counters` option now requires all subsecond counters before
startup and supervises the RPC sampler beside resources, cache/IO and the slow
watcher. Its counters use the suite's original deadline and pinned source;
sampler failure stops the other owned collectors. Use a new output directory
after the image change, stop the previous suite by its verified process
identity, and launch the fresh suite with `--replace-existing --rpc-counters`.
Retain the original capture as the concurrency-1 phase before the image change.

After capture, join the saved files without new node requests:

```bash
python3 -B /data/scripts/report-sequencer-context.py \
  --capture-dir /data/sequencer-context-UNIQUE \
  --events-file /data/slow-sequencer-watch-UNIQUE/events.jsonl
```

The report selects slow sequencing intervals from their logged finish time and
elapsed duration, compares overlapping resource samples with samples outside a
15-second guard, and keeps device and process identities separate. The outside
group is not proven fast; the watcher only retains blocks above its warning
threshold. Sample statistics, PSI averages and rolling quantile snapshots do
not establish causality. Input files are bounded snapshots; malformed/partial
lines and missing counters are reported, not counted as zero. The coverage
record includes observed duration, largest resource gap, lookup errors and the
presence of the collector's final record (which alone does not prove the full
requested duration elapsed). `--json` includes resource details for the three
slowest logged blocks; the default emits compact JSON lines. Tests run with
`python3 -B scripts/test_report_sequencer_context.py`.

`BACKGROUND` adds snapshot stage durations in seconds, generated/recovered
entry deltas, logical compaction input/output rates, debt and activity-interval
counts. Counter deltas require the same native main PID/start-time identity
throughout the surrounding resource samples, resource gaps no greater than
2.5 seconds, adjacent context starts at most 30 seconds apart and successful
metrics reads. Missing/reset counters are reported separately; valid totals sum
only accepted intervals and their covered seconds, excluding startup cumulative
counts. A resource-boundary interval may be unavailable. Debt is reported as a
gauge; it must not be treated as a monotonic counter.
Pebble books compaction input/output totals when jobs finish. A short interval
can therefore include a large job's completed byte count from work performed
earlier; those apparent rates do not measure instantaneous disk throughput or
prove a pacing violation. Use `compact/pacing/output` for output-write
accounting and kernel device metrics for physical I/O.

`SLOW_BLOCK_BACKGROUND` and `SLOW_RPC_BACKGROUND` show full context intervals
overlapping the three slowest logged blocks and five slowest retained sendRaw
records. The RPC details also summarize overlapping resource samples. These are
temporal context, not request-ID joins or per-request background-time budgets.
Context start times approximate metric-read times. No snapshot storage-counter
advance or compaction evidence does not establish that a background task
finished; compare the available interval combinations and workload before drawing
conclusions. The snapshot counters cannot establish bloom-bit cleanup status.

Compaction pacing shares a token bucket with a 64 KiB burst across chain-database
compaction output writers. It excludes foreground reads, WAL, manifest operations,
flushes, read-only opens, and WASM/consensus databases. Write stalls bypass waits;
close releases limiter waits before normal database shutdown. This limits SST
output writes, not device IOPS or total read/write traffic. A low rate can grow
compaction debt; enable it only with workload-specific observation.

For a single-setting main trial using the existing image, install
`scripts/trial-compaction-pacing.py` on the Linux node host and prepare a plan:

```bash
python3 -B /data/scripts/trial-compaction-pacing.py
```

Preparation reads the current main/nitro container, verifies its original
Compose inputs and service hash, and maps its single `--conf.file` to a host
bind mount. It saves exact JSON backup bytes, a candidate and a plan in a private
directory under `/data/deploy-backups`. Only the candidate's compaction-output
rate changes from absent/zero to 67,108,864 bytes/s (64 MiB/s); existing
concurrency, caches and image stay as configured. This starting rate is an
experiment, not a measured optimum. Additional config loaders and ambiguous
JSON layouts require separate review. Preparation does not edit the live JSON
or stop the node. Backups contain the original private configuration; do not
paste their contents into a support conversation.

The helper prints separate `--apply /absolute/plan.json` and
`--rollback /absolute/plan.json` commands. Apply verifies that the prepared
inputs, image, container and runtime still match, stops only main/nitro with a
120-second grace period, atomically updates the bound JSON, then recreates the
same service with its full original Compose file list, without a build or pull.
This briefly interrupts sequencing. Editing the host JSON persists the setting
across later starts using the original Compose files. Rollback restores the
exact original JSON and recreates that same service. Subsequent unrelated
configuration/runtime changes are refused rather than overwritten.

A service operation error leaves its output in a private log next to the plan;
there is no automatic rollback. The saved rollback also handles failure after
Compose removes the old container. `started_health_pending` confirms container
creation only. Check startup logs, RPC/head progress, restart/OOM status and
`l2chaindata_compact_pacing_*` counters, allow startup/cache warming to settle,
then capture a fresh hour with the context collector and existing slow-block
watcher. Compare similar transaction sizes/gas along with execution time,
main-device read/write latency and queue depth. Check debt across compaction
cycles, L0 and cumulative write-stall changes; sustained debt growth or worse
head/submission progress warrants rollback. Lower latency during a lighter
workload or just after restart does not establish a pacing improvement. Run
`python3 -B scripts/test_trial_compaction_pacing.py` for offline deployment
checks; these use mocked Docker responses and perform no live rollout.

Before an image upgrade, save and later compare the current deployment with
`scripts/check-main-runtime.py`. Follow [the main upgrade checks](main-upgrade-checks.md)
to retain every Compose input and the bound JSON, distinguish image IDs from
registry digests, and detect lost tuning or other deployment differences.

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
