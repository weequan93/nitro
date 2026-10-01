# DeriW storage and ETH spending — findings and conversation handoff

Prepared from the investigation and user-provided production measurements on **September 24, 2026**. This is the consolidated, corrected summary. Earlier reports contain superseded archive-mode assumptions; use this document for the current interpretation.

## Executive summary

The investigation identified two distinct on-chain changes:

1. **September 15: oracle workload migration.** DeriW moved price-update traffic from the old PriceFeed/FastPriceFeed contracts to a new signed PriceOracle. Its historical record allocates five new storage slots per valid token update, versus one historical slot in the old PriceFeed. Observed records grow by millions per day. Successful postings to Arbitrum One also increased, while sampled per-post fees were initially nearly unchanged.
2. **September 23: Arbitrum priority-tip collection activation.** Arbitrum One enabled `setCollectTips(true)`. The DeriW batch poster already specified a 0.05 gwei priority tip. Immediately adjacent posting transactions went from 0.02 to 0.07 gwei effective gas price with the base fee unchanged. This explains a separate later ETH-cost increase; it does not explain disk growth.

**Important correction:** the production node is **hash state scheme with `caching.archive=false`, always false**, according to the user's clarified configuration. The earlier explanation that this node was retaining every historical state because archive mode was enabled is withdrawn. Having historical blocks/transactions is different from retaining every historical execution state.

Production measurements locate the disk increase in the non-ancient execution database. A 30-minute sample grew by **1,347 MiB**, of which **98.6%** was outside `ancient`. If sustained, that is **63.14 GiB/day / 67.80 GB/day**. This is a short-window extrapolation, not a measured full-day average.

The strongest current operational hypothesis is **increased state churn interacting with the default 1 GiB dirty-trie cache and 30-minute minimum retention window**. Metrics show the cache near its limit, approximately 48 MiB/minute of capacity-triggered flushing, and zero recorded in-memory node reclamation. Hashdb can retain previously persisted nodes after they become obsolete; normal Pebble compaction does not perform chain-state reachability pruning. The mechanism is supported by source and runtime measurements, but the exact disk-byte attribution remains unmeasured.

A controlled increase to **4 GiB dirty-trie cache** was proposed. **No production configuration change or restart has been confirmed.** The user decided to observe another node first; its baseline results are pending.

## 1. Scope and infrastructure

- Requested window: before September 13 through September 24, 2026. Collected daily parent-batch comparisons cover September 9–24.
- DeriW RPC: https://rpc.deriw.com — chain ID **2886**.
- DeriW explorer: https://explorer.deriw.com.
- Parent chain: **Arbitrum One**, chain ID **42161**, RPC https://arb1.arbitrum.io/rpc.
- Rollup: `0xa113e2e9620a3bc088a681ebb2c234fdbeb85e21`.
- Sequencer inbox: `0xe79283a775f6a1de250cb3284e7ff3541ff7668a`.
- Observed batch poster: `0x34993e941d0b58b22a57a0c407589ae060d19e2d`.

Production node inspected through commands run by the user:

- Host: `deriw-prod-internal-priority-node-1`.
- Container: `node-nitro-1`.
- Image: `fuhua-container.tencentcloudcr.com/deriw/deriw:v1.3.1.2.v3.10.0-16c17ee3a9c4`.
- Container start: **2026-09-21 04:50:06 UTC**.
- Bind mount: `/data/node/config` → `/home/user/.arbitrum`.
- Chain directory: `/data/node/config/Deriw Chain/nitro`.
- Execution DB: `/data/node/config/Deriw Chain/nitro/l2chaindata`.
- Metrics endpoint confirmed reachable: `http://127.0.0.1:6070/debug/metrics/prometheus`.
- The assistant did not directly SSH into production; server results were supplied by the user.

## 2. On-chain changes and causal evidence

### Oracle migration

Old contracts:

- PriceFeed: `0x83ca1aa2bc20e41287154650e4161dc995278e1d`.
- FastPriceFeed: `0x43948b78477963d7b408a0e27ae168584c6e07a9`.

New contracts:

- PriceOracle proxy: `0x1461469b43ad78145048eea11cbf6ba97222d379`.
- Implementation: `0xf822b48544b783d2e1fd8d3ff395b959bb6168ad`.

| UTC time | Event | Transaction |
|---|---|---|
| Sep 14 02:58:38 | Deploy new proxy | [0x876a24a0…](https://explorer.deriw.com/tx/0x876a24a0d5ba9ce949843aaa2513fa93dff874c69950dc1fb5e2cb137e1df5b8) |
| Sep 14 02:58:39 | Assign new implementation | [0xfa3cf4c9…](https://explorer.deriw.com/tx/0xfa3cf4c926bd9f40ed2471b787aac0d2f6fa05df4aecdd6c2bd81649c6ab4010) |
| Sep 15 09:57:38 | Last indexed old PriceFeed update | [0x47efb6a6…](https://explorer.deriw.com/tx/0x47efb6a6b52257002f18e5537309a8c053baf255b89fb9642114abd904b55776) |
| Sep 15 09:58:25 | First indexed new `batchSetPrices` | [0xd813714a…](https://explorer.deriw.com/tx/0xd813714ae39cdd314b608dd4c44f8283eeb7d611f4b24b4c74a2252741a593c4) |

The first new update occurred at **17:58:25 Singapore time**, submitted 50 price entries and two signatures, carried 6,948 calldata bytes, and emitted 50 successful SetPrice events. The new oracle was a new deployment and traffic migration, not an upgrade of the old PriceFeed proxy.

The old PriceFeed appends one historical answer slot while also updating existing fields. The new PriceOracle appends five slots per valid record: token address, ask, bid, mid, and timestamp. It also updates existing counters/salt. Records remain in current live state and are not deleted by this implementation. This growth occurs **even with archive disabled**.

For **198 observed distinct token keys**:

- Sep 16 boundary: **2,681,305** cumulative price IDs.
- Sep 24 02:46:42 UTC: **32,187,894** cumulative price IDs.
- Difference: **29,506,589 records**, corresponding to **147,532,945 historical slots**.
- The logical 32-byte values represent **4.72 GB**, before keys, trie/database encoding and other overhead. This is not physical disk usage.
- Getter values were independently verified against all 198 distinct underlying `priceId` storage mapping keys, avoiding token-alias double counting.

Available full-day increments across this token set include 3.98 million records on Sep 16 and 4.37 million on Sep 23. The set is not guaranteed to cover every token. Some daily historical RPC states were unavailable, so missing days were not fabricated.

### Parent posting frequency and fees

Successful daily batch counts come from complete SequencerBatchDelivered event sets. Fee totals below are **estimates**: exact count multiplied by the mean receipt fee from 24 distributed batch samples per day. They exclude failed transactions, other wallets, validator spending, transfers and DA service bills.

| September date, UTC | Successful batches | Estimated posting ETH |
|---|---:|---:|
| 9 | 1,180 | 0.003465 |
| 10 | 1,190 | 0.003496 |
| 11 | 1,171 | 0.003784 |
| 12 | 589 | 0.001733 |
| 13 | 681 | 0.001998 |
| 14 | 1,217 | 0.003618 |
| 15 | 1,623 | 0.004872 |
| 16 | 1,826 | 0.005470 |
| 17 | 1,678 | 0.004951 |
| 18 | 1,676 | 0.004995 |
| 19 | 1,219 | 0.003621 |
| 20 | 1,260 | 0.003706 |
| 21 | 1,646 | 0.005002 |
| 22 | 1,752 | 0.005211 |
| 23 | 1,866 | 0.008957 |
| 24 through 02:46:40 | 201 | 0.002058 |

Sep 14 → 16: batches increased approximately 50%, sample mean fee/batch increased 0.78%, and estimated successful-posting spend increased 51%. Explorer transaction count decreased about 31%. Larger signed payloads are a supported contributor to increased posting frequency, but compressed DA object sizes and historical deployed batch-size/delay settings were unavailable; the entire change is not isolated to payload size.

All **384** distributed parent-batch samples carried 178-byte AnyTrust certificates with header `0x88`. No sample shows full-calldata fallback; occasional unsampled fallback is not excluded.

### Separate priority-tip activation

At **Sep 23 17:20:03 UTC / Sep 24 01:20:03 Singapore**, [transaction 0x81541d94…](https://arbiscan.io/tx/0x81541d94eb04c080ab55d0a589728353fb11ab63b9bd33591ae9db44fa261e95) enabled Arbitrum tip collection.

The user challenged whether this was the wrong transaction. It was rechecked directly:

- Outer transaction is Safe `execTransaction` to `0xe128a8100d1b543de82d04450b3406d57abf553b`.
- It calls verified `TipCollectionToggler` at `0x4323dab775cc15e386fdac591a39420db6226a63`.
- That contract forwards `setCollectTips(true)` to ArbOwner at `0x70`.
- Successful receipt includes `OwnerActs` and `CollectTipsUpdated(true)`.

It is an **Arbitrum-wide administration transaction, not a transaction from the DeriW batch poster**. The [governance proposal](https://forum.arbitrum.foundation/t/constitutional-aip-transition-arbitrum-one-ordering-policy-to-priority-gas-auctions-pga/30942) describes this toggle mechanism; on-chain receipts establish the actual activation and fee effect here.

| Adjacent DeriW batch | Before | After |
|---|---:|---:|
| UTC time, Sep 23 | 17:17:40 | 17:20:51 |
| Base fee | 0.02 gwei | 0.02 gwei |
| Specified priority tip | 0.05 gwei | 0.05 gwei |
| Effective gas price | 0.02 gwei | 0.07 gwei |
| Actual receipt fee | 0.000003043860 ETH | 0.000010262980 ETH |

Before: [0xf3bf247e…](https://arbiscan.io/tx/0xf3bf247e285e9c01a1f2cec3e79efb575d1b4ba5979940f6838df9a508005ef0). After: [0x806a3916…](https://arbiscan.io/tx/0x806a3916ce4d62cddb02f1faf1090f6c8b185b312113fc34566f4e08d32cfbe8).

The gas-price effect is 3.5×. The specific receipt-fee effect is approximately 3.37× because gas used differs. The observed 0.05 gwei matches the repository's default minimum tip, but production's setting source was not independently inspected. No tip configuration was changed.

## 3. Disk chart and corrections

The user supplied a utilization chart for instance `ins-f8sifl9g`, covering Aug 13–Sep 24, with 96.17% at its last point. The user confirmed that **every abrupt utilization decrease was expansion by 300 GB**, not cleanup or pruning.

Consequences:

- Percentage slopes across different capacities cannot directly establish a physical GB/day ratio. The earlier unqualified 3–4× comparison was withdrawn.
- A last jump roughly 95% → 81% suggests a post-expansion capacity around 2 TB only as a rough screenshot calculation. Subsequent actual `df` is more authoritative.
- Actual host `df` showed `/data` at **2.3T total, 1.8T used, 384G available, 83%**. The mismatch with the screenshot's 96.17% was noted but not resolved with exact capacity/timestamp history.
- Docker overlay entries are views of the same filesystem and must not be added together.
- Earlier screenshot-based “1–2 days remaining” statements are superseded by the later filesystem reading and should not be treated as current headroom estimates.

## 4. Production disk measurements

Initial directory breakdown:

- `/data/node`: approximately 1,758 GiB, approximately 99% of `/data` allocated-file usage.
- `/data/containerd`: approximately 20 GiB.
- Docker/log directories were each below 1 GiB as rounded by `du -BG`.
- `arbitrumdata`: approximately 65.8 GiB.
- `l2chaindata`: approximately 1,691 GiB at that snapshot.
- `ancient/chain`: approximately 209.8 GiB.
- Non-ancient execution database: approximately 1,482 GiB.

Seven `du -m` measurements:

| UTC, Sep 24 | Ancient MiB | Total l2chaindata MiB |
|---|---:|---:|
| 05:16:16 | 214,868 | 1,732,125 |
| 05:21:16 | 214,871 | 1,732,473 |
| 05:26:16 | 214,876 | 1,732,587 |
| 05:31:16 | 214,880 | 1,732,888 |
| 05:36:16 | 214,880 | 1,733,029 |
| 05:41:16 | 214,883 | 1,733,287 |
| 05:46:16 | 214,887 | 1,733,472 |

30-minute net changes:

- Total: **1,347 MiB**.
- Ancient: **19 MiB**.
- Non-ancient: **1,328 MiB / 98.59% of the increase**.
- Extrapolated total: **63.14 GiB/day / 67.80 GB/day**.
- At that rate, decimal 300 GB covers about **4.4 days**; 300 GiB covers about 4.75 days.

These are live directory scans, not atomic snapshots. All sampled endpoints increase, but this does not exclude compaction fluctuations within or beyond the window. Non-ancient data includes indexes as well as trie state.

## 5. Database and runtime evidence

### Indexing and compaction

The two supplied `Log index head rendering` lines at 05:41:04 report `processed=1`, `remaining=0`, then `finished`. Source inspection shows firstblock/lastblock are the indexed range boundaries. This is **not evidence of rebuilding the whole approximately 10-million-block range**.

At the supplied metrics snapshot:

- `l2chaindata_disk_size = 1,592,290,349,952` bytes, approximately 1,483 GiB.
- Compaction debt, estimated debt, in-progress count, live compaction size, L0 tables/sublevels and write-delay count were all **zero**.
- Compaction output was approximately **4.68 TB cumulative I/O**, not additional current disk occupancy or reclaimable space.

No compaction backlog was observed at that instant. This does not exclude earlier compaction activity and **does not prove there are no obsolete blockchain-state keys on disk**. LSM compaction and chain-state reachability pruning are different operations.

### Effective configuration evidence

The user states caching has always been:

```json
"caching": {
  "archive": false
}
```

Adjacent RPC configuration has `gas-cap: 300000000`; this is not the dirty-state cache or state-retention setting.

Defaults were checked against the deployed-image commit **16c17ee3a9c4**, not merely the current checkout:

| Setting | Default unless otherwise overridden |
|---|---|
| state-scheme | hash |
| trie-dirty-cache | 1024 MiB |
| block-age | 30 minutes |
| block-count | 128 |
| trie-time-limit | 1 hour of accumulated block-processing time |

The referenced geth commit is `e582a7d5fb4887cbd3b4c7bb4fe74d2a5b06bddc`; its relevant flush/GC behavior was also checked. Additional command-line overrides have not been independently ruled out. The observed approximately 1 GiB cache supports the default cache-size interpretation.

### One-minute hashdb sample

| Metric | 06:14:12 UTC | 06:15:12 UTC |
|---|---:|---:|
| chain_triedb_size | 1,073,639,305 | 1,073,639,005 |
| flush_bytes | 224,845,240,347 | 224,895,554,424 |
| flush_nodes | 564,734,118 | 564,859,727 |
| gc_bytes / gc_nodes | 0 / 0 | 0 / 0 |
| commit_bytes / commit_nodes | 0 / 0 | 0 / 0 |
| l2chaindata_disk_size | 1,593,467,941,088 | 1,593,467,941,088 |

Interpretation:

- Capacity-triggered flushing added **50,314,077 bytes / 47.98 MiB**, involving **125,609 nodes**, in one minute.
- No reclaimed in-memory nodes were recorded. Zero GC counters do not mean the GC function was never called.
- Cache remains close to 1 GiB.
- Disk-size gauge did not change during this specific minute; logical flush bytes are not equal to physical net growth.
- Cache size divided by logical flush rate gives a rough **21-minute turnover scale**, not an exact residence time; their accounting includes different overheads.

Source behavior: hashdb `Cap` persists still-referenced dirty nodes to meet its memory limit. `Dereference` reclaims entries in the in-memory dirty map; if a node is already absent because it was persisted, it does not delete that node from disk. The full-node reclamation loop respects both block-count and minimum block-age. Thus an increased workload can cause capacity flushing before nodes become eligible for in-memory reclamation.

This supports the working diagnosis, but the proportion of persisted nodes that are now obsolete has not been measured. The old PriceFeed also modified existing fields, so “five new slots versus one” must not be treated as an exact disk-growth multiplier.

## 6. Proposed experiment, not yet performed

Memory supplied by the user:

- 30 GiB RAM total; 10 GiB used; 19 GiB available; approximately 604 MiB free.
- Docker `HostConfig.Memory = 0` (no explicit limit through this setting).
- Swap approximately 1.9 GiB occupied, but the five interval rows of `vmstat 1 6` showed **si=0, so=0**. No active swapping was observed in that short sample.

Proposed merge into the existing configuration:

```json
"caching": {
  "archive": false,
  "trie-dirty-cache": 4096
}
```

Keep other parameters unchanged for a single-variable comparison. Nominal cache increase is 3 GiB, not a cap on total process memory. The available-memory snapshot supports evaluating this experiment, but cannot guarantee headroom under all RPC peaks.

The proposal requires the normal graceful restart procedure in an acceptable service-interruption window. Keep the original configuration for rollback. No direct production mutation or restart was performed by the assistant, and the user has not reported applying it.

Success criteria after catching up and passing the retention window:

- GC node/byte counters begin recording reclamation, where applicable.
- Capacity-flush rate decreases under comparable workload.
- Sustained physical database growth decreases.
- Memory, swap activity, RPC service and sync remain healthy.

Observe approximately two hours or longer, after the cache has settled. A larger cache initially filling up can temporarily reduce flushes without proving a long-term improvement. Restarted cumulative counters require new delta baselines. This experiment does **not** automatically reclaim existing disk space.

The user plans a future path-state migration. Its scope and required historical-query retention must be decided separately; after the archive=false correction, do not assume full historical-state preservation is an already-settled requirement. Path storage may change the growth/retention behavior but will not stop the oracle from creating new live-state records. No migration savings percentage has been established.

## 7. Conversation progression and current handoff

1. User requested investigation of storage and ETH spending around Sep 15; clarified the year is 2026 and requested a wider pre-Sep-13-to-today window.
2. Public RPC/explorer investigation found the oracle migration, increased posting counts, and later tip-collection activation.
3. User challenged the tip-activation transaction; its multisig → toggler → ArbOwner path and actual posting-fee effect were reverified.
4. User supplied a disk-utilization chart; clarified its downward steps were +300 GB expansions. Percentage-based physical-growth comparisons were corrected.
5. User initially called the node archive and said it retained all historical data, then identified hash storage and a planned path migration.
6. Through user-run read-only commands, the investigation localized occupancy to l2chaindata and measured its 30-minute growth, followed by indexing and compaction metrics.
7. User corrected the configuration: **archive is false and has always been false**. All main reports were marked to withdraw the archive-mode attribution.
8. Hashdb flush/GC metrics and deployed-version defaults supported the cache/retention working hypothesis. Available-memory and swap samples supported evaluating a 4 GiB cache trial.
9. User chose to **observe another node first**. Baseline instructions were provided. No second-node results or cache-change results have been supplied yet.
10. Current request: export this findings-and-conversation summary.

### Pending second-node baseline

The user was given these read-only commands:

```bash
hostname
date -u
df -hT /data
free -h
docker ps --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}'
vmstat 1 6
```

Then a 30-minute metrics baseline:

```bash
for i in 1 2 3 4 5 6 7; do
  date -u '+%Y-%m-%dT%H:%M:%SZ'
  curl -fsS --max-time 10 \
    http://127.0.0.1:6070/debug/metrics/prometheus \
    | awk '
      $1 == "chain_triedb_size" ||
      $1 == "l2chaindata_disk_size" ||
      $1 == "l2chaindata_compact_debt" ||
      $1 == "l2chaindata_compact_inprogress" ||
      $1 ~ /^hashdb_memcache_(flush|gc|commit)_(bytes|nodes)$/ {
        print
      }
    '
  if [ "$i" -lt 7 ]; then sleep 300; fi
done | tee /tmp/deriw-other-node-baseline.txt
```

Also requested: that node's caching configuration and whether it is caught up. Keep it unchanged for the initial baseline; compare nodes under reasonably similar load and sync conditions. Metrics endpoints may differ on the second node.

## 8. Evidence files and limits

All relative links below refer to files alongside this summary:

- [Chinese disk investigation](DISK-CHART-ANALYSIS-ZH.md): sequential server evidence and explicit corrections.
- [September 9–24 timeline](TIMELINE-SEP09-24.md): on-chain transactions and daily costs; read its archive correction banner.
- [Extended batch samples](extended-fees.json) and `batchlogs-*.json.gz`: 384 samples and complete selected daily event sets.
- [Storage counts](storage-counts.json), [direct-key verification](storage-verification.json), [daily storage](daily-storage.json).
- [Hashdb runtime samples](hashdb-runtime-samples.json).
- [Tip activation](parent-tip-toggle.json), [rechecked receipt](tip-toggle-rechecked-receipt.json), [before/after batches](tip-before-after.json).
- `oracle-activation-receipts.json`, `oracle-first-transactions.json`, and verified Solidity snapshots.
- [Proposed caching fragment](caching-proposed-fragment.json): merge-only proposal, not a complete replacement configuration.

Unresolved: exact physical bytes attributable to oracle state versus obsolete persisted nodes/indexes; long-term daily disk average; complete expense-wallet reconciliation; all production configuration overrides; exact expansion history; second-node baseline; cache-trial outcome. Public historical state reads can fail due to pruning; missing observations were not substituted with invented values.

No chain transactions were sent, no database files were deleted, and no production settings were changed by the assistant. Local investigation reports, scripts and evidence files were created in the shared workspace.
