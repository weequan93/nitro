# Main concurrency-1 comparison and complete monitoring

The 12 November baseline ran from 10:22:57 to 11:22:57 SGT with compaction
concurrency 2 and pacing 67,108,864 bytes/s. It completed 3,600 IO and 240 cache
samples without errors or an identity change. The watcher retained two
successful sendRaw requests of at least one second (maximum 1,136.143 ms),
zero retained failures of that duration and zero requests of at least five
seconds. Publisher calls increased by 22,012; this is an internal counter,
not a proven denominator for all RPC submissions.

Compaction debt first/min/max/last was 101.57/95.55/112.88/102.58 GiB, with
zero write-stall and pacing-bypass increments. Average read/write awaits were
0.331/0.980 ms; their independent sampled maxima were 5.610/10.508 ms.
Snapshot clean-storage hit ratio was 99.713%, and Pebble block-cache ratio
was 65.019%. The baseline does not prove that concurrency 1 improves latency.
The operator requested that comparison, retaining pacing, image and caches.

## Already edited JSON, original process still running

Install these scripts from the same revision beside one another:

- `trial-compaction-pacing.py`, `check-main-runtime.py`
- `capture-sequencer-suite.py`, `capture-sequencer-longrun.py`
- `capture-sequencer-context.py`, `capture-main-cache-io.py`
- `watch-slow-sequencer-blocks.py`, `report-sequencer-context.py`

For this recorded deployment the JSON was manually edited to concurrency 1
before restarting. The original loaded JSON is the pacing trial's candidate,
and its bytes must match the saved runtime baseline's bound-JSON hash:

```bash
python3 -B /data/scripts/trial-compaction-pacing.py \
  --compactions 1 --prepare-and-apply \
  --pending-baseline /data/deploy-backups/main-runtime-20261111-173334.json \
  --loaded-config /data/deploy-backups/main-pacing-sh6jpih4/candidate.json
```

The helper requires the exact original container ID/start time/image,
native-argument hash, container layout, Compose model and loaded-JSON hash.
The pending JSON must differ only in concurrency 2 to 1, with pacing already
64 MiB/s. A missing backup, intervening restart, other edit or deployment drift
stops before service changes. A private `main-compactions-*` plan backs up the
exact loaded JSON and pending candidate and prints apply/rollback commands.
Application stops only main/nitro, reinstalls the candidate and recreates the
same Compose service with no build or image pull.

After an activation error, inspect its private operation log and use the
printed rollback command. Rollback restores concurrency 2 with 64 MiB/s;
it does not restore the historical unpaced configuration. A declared
`started_health_pending` result does not establish RPC readiness.

For a concurrency-2 JSON that has not been edited, omit both pending-edit
arguments. For an already restarted concurrency-1 process, skip activation
and start the suite directly; preparing from the old baseline is deliberately
refused after an intervening restart.

## Start all three monitors

```bash
OUT="/data/sequencer-compaction1-24h-$(date +%Y%m%d-%H%M%S)"
mkdir -p "$OUT"
nohup python3 -B -u /data/scripts/capture-sequencer-suite.py \
  --hours 24 --segment-minutes 60 --compactions 1 \
  --replace-existing --device vdb --out-dir "$OUT" \
  >"$OUT/stdout.log" 2>&1 &
echo "SUITE_PID=$!"
echo "OUT=$OUT"
```

The suite waits for the RPC and database metrics (up to five minutes), verifies
that concurrency 1 and pacing 64 MiB/s are loaded, and pins the scripts. It
checks two positive nondecreasing heads, recording whether they advanced;
an idle chain is allowed. Only after these preflight checks does it stop
recognized previous main monitoring processes. It verifies PID/start ticks
before signals, includes the old longrun's current sampler, and stops only
the old watcher's direct `docker logs --follow` client. Main Nitro, validators,
migration, other containers and all previous output files are retained.

A new private `runtime-baseline.json` records this phase. The original
concurrency-2 baseline is retained. Sources and output paths are:

```text
OUT/
  stdout.log, status.json, journal.jsonl, run.json
  runtime-baseline.json, previous-monitors.json
  tools/
  resources/segment-NNNN/resources.jsonl
  resources/segment-NNNN/context.jsonl
  cache-io/segment-NNNN/io.jsonl
  cache-io/segment-NNNN/cache.jsonl
  slow-blocks/events.jsonl
  slow-blocks/stdout.log
```

The resource and cache collectors use one-second device/resource intervals
and fifteen-second metrics intervals, rotating hourly. The watcher retains
sendRaw records at least one second long and block/transaction details for
slow-block warnings. A common parent owns all three, stops them at 24 hours,
and stops the others if a collector fails. Unlike the earlier separate
watcher, this owned watcher also stops at the suite deadline.

Inspect startup and actual sample growth:

```bash
cat "$OUT/status.json"
tail -3 "$OUT/stdout.log"
cat "$OUT/runtime-baseline.json"
wc -l "$OUT/resources/segment-0001/resources.jsonl" \
      "$OUT/resources/segment-0001/context.jsonl" \
      "$OUT/cache-io/segment-0001/io.jsonl" \
      "$OUT/cache-io/segment-0001/cache.jsonl"
```

The wrapper enforces a 3 GiB aggregate output threshold and 2 GiB free-space
floor, checked once per minute. Component limits also apply; brief overshoot
is possible between checks. Sending SIGTERM to the recorded suite parent
stops its owned monitoring without controlling the Nitro service.

## Comparison

RPC readiness does not establish cache warmup or completed state snapshot
generation. Capture startup/background activity, then compare equivalent
workloads and separate background phases. Track retained successful and failed
slow submissions separately, block work, disk waits, cache ratios, pacing
output, bypass/stall increments and debt cycles. A counter reset is a boundary,
not zero work; compaction completed-job byte totals are not live throughput.

Report each cache segment with its pinned tool:

```bash
python3 -B "$OUT/tools/capture-main-cache-io.py" \
  --report "$OUT/cache-io/segment-0001"
python3 -B "$OUT/tools/report-sequencer-context.py" \
  --capture-dir "$OUT/resources/segment-0001" \
  --events-file "$OUT/slow-blocks/events.jsonl"
```

## RPC counter coverage and an empty watcher file

The original `78df45a72` capture keeps compaction and publisher metrics but
omits the independent `rpc_diagnostics_*` counters. Its watcher files contain
only retained slow details, and an empty file does not prove zero slow calls.
Detail logging is rate-limited; `details_dropped` is shared across getLogs and
sendRaw and counts missing detail records, not lost transactions. The
completion/1s/3s/5s counters are incremented before that limit.

On the 12 November check, uptime counters showed 45,354 successful submission
RPCs and 74 failures, with 111 calls at least one second and 43 at least five
seconds. They start at the 12:54 SGT node restart, before this suite's 13:21 SGT
start. Those values alone cannot locate slow calls inside this capture window.
Read retained Docker logs, split at the suite start, with:

```bash
python3 -B /data/scripts/capture-rpc-diagnostics.py \
  --suite-dir "$OUT" --inspect-logs
```

This mode queries only the current container's retained logs from its startup,
with a fixed end timestamp and a 45-second client timeout. It reports time
coverage, parse failures, before/during counts and examples. Rotated or dropped
records remain unavailable. Stage details and Served errors are separate views
of calls and must not be added as distinct transactions.

To supplement the running pinned suite without changing its files:

```bash
mkdir -p "$OUT/rpc-counters"
nohup python3 -B -u /data/scripts/capture-rpc-diagnostics.py \
  --suite-dir "$OUT" --out-dir "$OUT/rpc-counters" \
  >"$OUT/rpc-counters/stdout.log" 2>&1 &
echo "RPC_COUNTER_PID=$!"
```

The extra collector reads metrics and stable native identity every 15 seconds.
It writes raw uptime counters and adjacent deltas to `counters.jsonl`, with
missing/error/reset/gap intervals marked unavailable. Its PID/start-time owner
check stops it if this suite exits or its PID is reused. Its deadline is the
existing suite's deadline, not another 24 hours from installation. It sends no
signals, performs no service/configuration operations, and preserves both the
suite's pinned sources and prior data. Output is bounded to 64 MiB with a 1 GiB
free-space floor checked once per minute.

```bash
cat "$OUT/rpc-counters/status.json"
python3 -B /data/scripts/capture-rpc-diagnostics.py \
  --report "$OUT/rpc-counters"
```

Only accepted adjacent differences are summed in the report. The new first
sample cannot reconstruct earlier per-window totals. RPC outcome is the
submission result, not transaction receipt status. New context collectors
also retain `rpc_diagnostics_*` in their metrics; existing pinned collectors
retain their original code and need the supplement for ongoing counter history.

Offline validation uses fake collectors and mocked service operations:

```bash
python3 -B -m unittest discover -s scripts -p 'test_trial_compaction_pacing.py'
python3 -B -m unittest discover -s scripts -p 'test_capture_sequencer_suite.py'
python3 -B -m unittest discover -s scripts -p 'test_capture_rpc_diagnostics.py'
```
