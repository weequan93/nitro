# Main RPC pacing observations — 11 November 2026

Decision: retain the current 64 MiB/s L2-chain compaction-output pacing with
compaction concurrency 2. Leave cache declarations unchanged. The sampled
one-hour and subsequent two-hour paced windows show lower retained submission
latency and recurring debt drainage without write stalls or limiter bypass.
The paced windows contain smaller sampled Oracle batches; performance for the
original large-block workload remains unverified. The rate is not established
as an optimal capacity setting or a causal explanation of every improvement.

## Configuration and upgrade retention

- Main image ID: `sha256:e27ef7fd4d2df39cd402c56d68d5a5d1010f2bd3954e994ece396bddf8d676af`.
- Pacing: `persistent.pebble.compaction-output-bytes-per-second = 67108864`,
  explicitly in the bound JSON.
- Compaction concurrency: `persistent.pebble.max-concurrent-compactions = 2`,
  explicitly in the bound JSON.
- Trie-clean and database cache: undeclared; image defaults remain unresolved
  by the deployment checker. This is not the complete 5 November combination
  (64 MiB/s, concurrency 1, trie-clean cache 1536 MiB).
- Current pacing plan: `/data/deploy-backups/main-pacing-sh6jpih4/plan.json`.
- Saved deployment baseline: `/data/deploy-backups/main-runtime-20261111-173334.json`.
- Follow [the upgrade checks](main-upgrade-checks.md) to preserve the full
  Compose model and bound JSON through subsequent image updates.

The operator applied the current pacing setting at 15:26:17 SGT. The following
captures did not change image, concurrency or cache declarations. Their native
main PID/start-time remained stable within each accepted analysis interval.

## Observed windows

| Observation | Unpaced 13:50–14:50 SGT | Paced 15:59–16:59 SGT | Paced 17:35–19:35 SGT |
| --- | ---: | ---: | ---: |
| Resource samples | 3593 | 3593 | 7185 |
| Context samples inside resource coverage | 239 | 239 | 479 |
| Retained slow sequencing blocks (>5 seconds) | 61 | 0 | 0 |
| Retained slow sendRaw records | 329 | 1 | 1 |
| Longest retained sendRaw | 19.690 s | 2.493 s | 1.088 s |
| Failures among retained slow sendRaw records | 0 | 0 | 0 |
| Sampled block gas mean | 2.344 M | 1.523 M | 1.282 M |
| Sampled block gas maximum | 14.313 M | 7.158 M | 5.898 M |
| Sampled Oracle price entries per transaction, mean / maximum | 19.004 / 50 | 9.736 / 50 | 8.017 / 41 |
| Compaction debt range | 90.798–106.314 GiB | 91.450–107.781 GiB | 91.598–105.909 GiB |
| Write-stall counter increment | 0 | 0 | 0 |

All three resource windows have no recorded metrics, block-lookup or
Docker-top errors, no malformed input lines and no resource gap above
2.5 seconds. The final two-hour window covers 119.99 minutes; its largest
resource gap is 1.122 seconds. These quality checks do not establish complete
RPC log coverage. Slow records are not total request counts, and an absent
slow warning is not proof that every block was fast.

In the final two-hour window, mean main-device `vdb` read/write latency was
0.281/0.872 ms and mean device queue length was 1.552. Pacing output advanced
208.292 GiB with zero bypass increments. Its 4871.315 seconds of accumulated
wait are writer-operation elapsed time, not an equally long node pause.

Snapshot storage counters advanced in all 478 accepted adjacent context
intervals. Background compaction evidence appeared in 272 of them. Thus the
improvement was observed while snapshot work continued, rather than after
observed snapshot completion. Snapshot stage counters do not identify CPU
time, physical disk traffic or a per-request causal wait budget.

The debt lows returned repeatedly to approximately 92–94 GiB, including
91.77 GiB in the 19:05–19:15 subset. The final recorded value was 94.15 GiB.
There is no monotonic rise in this observed two-hour window. This does not
prove indefinite balance under a different transaction/state-write workload.

## Remaining validation

The supplementary periodic block sample contained no block at or above
12 million gas and no decoded Oracle batch of 50 entries in the final two-hour
window. These periodic latest-block samples are not an exhaustive transaction
history. Retain the existing slow-block watcher and revisit naturally occurring
large Oracle batches or renewed slow requests. Assess workload size, latency,
device waits, debt lows, stalls and bypass together before trying one additional
cache or concurrency setting. No synthetic transactions or replay were used.

The finite resource captures are complete; the slow-block watcher remains a
separate long-running process. Server evidence files:

- `/data/sequencer-context-snapshot-20261111-135028/report-snapshot.txt`
- `/data/sequencer-context-paced-20261111-155957/report-paced.txt`
- `/data/sequencer-context-paced-followup-20261111-173538/report-followup.txt`
- `/data/sequencer-context-paced-followup-20261111-173538/report-followup-extra.txt`
- Watcher events: `/data/sequencer-context-paced-20261111-155957/slow-blocks/events.jsonl`
