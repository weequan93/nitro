# September 2026 storage and posting-cost investigation

Start with [SUMMARY-AND-HANDOFF.md](SUMMARY-AND-HANDOFF.md). It is the final corrected interpretation of the investigation and the user-provided production measurements as of September 24, 2026.

This directory archives a completed collection session. Its measurements describe that session; they are not current monitoring results or an instruction to change production settings.

## Reading order

| File | Purpose |
| --- | --- |
| [SUMMARY-AND-HANDOFF.md](SUMMARY-AND-HANDOFF.md) | Corrected findings, evidence limits and pending follow-up. |
| [TIMELINE-SEP09-24.md](TIMELINE-SEP09-24.md) | Oracle cutover, daily batch comparisons and priority-tip activation. September 24 is partial. |
| [DISK-CHART-ANALYSIS-ZH.md](DISK-CHART-ANALYSIS-ZH.md) | Sequential server observations, earlier hypotheses and their corrections. |
| [REPORT.md](REPORT.md) | Initial investigation with a narrower sampling window. Later findings supersede its tentative timing and archive assumptions. |

The production node's `caching.archive` was reported to have always been `false`. Earlier statements attributing growth to archive-mode retention were withdrawn. The confirmed oracle record growth and sampled disk growth remain evidence; the exact physical-byte attribution is unresolved. The proposed cache fragment is an experiment proposal, and no result confirming its application is included.

## Evidence

- `evidence.json` combines the initial boundaries, fee samples, oracle samples and historical implementation reads.
- `extended-boundaries.json`, `extended-fees.json` and `batchlogs-*.json.gz` retain the wider September 9–24 collection. Some inbox log files also contain spending-report events; filter by the full `SequencerBatchDelivered` topic before counting batches.
- `storage-counts.json`, `storage-verification.json`, `daily-storage.json`, `observed-oracle-tokens.json` and `matched-token-rates.json` retain the observed token set and record counts. Unavailable historical days are omitted.
- `oracle-*.json`, `old-feed-last.json` and the Solidity snapshots retain the source and transaction evidence for the oracle migration.
- `parent-tip-toggle.json`, `tip-*.json` and `hashdb-runtime-samples.json` retain the fee-activation and runtime observations.
- `caching-proposed-fragment.json` is a merge-only proposal, not a full node configuration or a record of deployment.

The extracted Solidity sources and JSON/gzip evidence are preserved as collected, including source formatting. They are reference snapshots, not contracts added to the build or deployment process.

## Collector scripts

The nine Python scripts are archived collectors from the original session. They make read-only RPC requests and write to `/tmp/deriw-cost-investigation`. They require Python 3 and `curl`; the storage-key collectors also require Foundry `cast` on `PATH`.

They contain fixed September 2026 dates, endpoint addresses and snapshot assumptions. In particular, `extend.py` appends the current chain head as its final boundary while labelling that row as day 25. Running it later would not reproduce the archived September 24 partial-day window. Review and adapt the dates and boundaries before a new collection.

| Script | Dependencies / output |
| --- | --- |
| `probe.py` | Creates scratch `boundaries.json` and resolves the inbox. |
| `batchdays.py` | Reads boundaries; collects selected daily inbox logs. |
| `deeper.py` | Reads boundaries; records distributed child-chain oracle samples. |
| `fees.py` | Reads selected uncompressed batch logs; records receipt fee samples. |
| `extend.py` | Extends boundaries; collects the wider batch/fee sample set. |
| `details.py` | Reads batch logs and the explorer transaction snapshot; records activation and before/after receipts. |
| `storage_counts.py` | Reads extended boundaries and the separately collected observed-token list. |
| `verify_storage.py` | Derives mapping keys and compares getter counts with direct storage reads. |
| `daily_storage.py` | Reads the counts, verified keys and boundaries; records available daily counts. |

Several explorer snapshots and aggregate files were collected or assembled separately. This archive does not include a single command that regenerates every file. Collectors expect their dependencies in the scratch directory; the compressed logs in this directory are not automatically unpacked there. Historical RPC pruning and rate limits may also prevent a later collection from completing.

## Commit review

Offline checks on October 1, 2026 verified:

- JSON parsing, gzip integrity and syntax for all nine Python collectors.
- 20,775 saved batch events against daily counts, contiguous sequence numbers and parent-block boundaries.
- All 384 extended fee samples against their saved events, fee arithmetic and rounded daily table entries; each sampled certificate is 178 bytes with header `0x88`.
- All 198 mapping-key derivations and the recorded token-count sums, plus the hashdb flush deltas.
- Relative Markdown links.

These checks use saved data and local hashing. They do not independently refetch receipts, establish current chain state, or measure a new production workload.
