# MDBX Migration and Erigon Storage Plan

## Goals
- Add MDBX as a first-class DB engine option for Nitro.
- Introduce an Erigon-backed execution/storage backend without modifying the existing geth-based path.
- Provide an offline migration tool that converts existing Pebble data into the new MDBX layout with full history.
- Keep the change isolated so future Nitro upgrades can keep the geth path intact.

## Non-goals
- No in-place live migration while a node is running.
- No partial history migrations for the primary path (full history is required).
- No changes to consensus rules or block formats.
- No geth-style init/reorg/blocks-reexecutor flows when using the Erigon backend.

## Decisions (Current Defaults)
- Backend selection: auto-detect selects `erigon` only when MDBX data exists for `l2chaindata`, `arbitrumdata`, and `wasm`. Partial MDBX data errors with a migration-required message. If Pebble data is present and no MDBX data exists, exit with a migration-required error. Keep `execution.backend` as an override for debugging.
- Empty datadir behavior: if no DBs exist, auto mode defaults to geth; explicit `execution.backend=erigon` requires MDBX data for `l2chaindata`, `arbitrumdata`, and `wasm`.
- MDBX layout: separate MDBX DBs for `l2chaindata`, `arbitrumdata`, and `wasm`, matching existing boundaries. Merging into a single MDBX DB is a future optimization.
- Arbitrum metadata layout: per-prefix `arb_*` buckets with a merged iterator to preserve prefix scans (Erigon-style tables).
- History policy: support both full-history and pruned modes (Erigon-style), but require full history for debug/trace parity.
- Migration verification: extended checks (head hash + state root + chain config bytes + sampled ArbOS slot history + arbitrumdata/wasm key/value parity).

## High-level Approach
- Add a backend selector with auto-detect (example: `execution.backend = auto|geth|erigon`, default `auto`).
- In `auto` mode, select Erigon if MDBX data exists, otherwise error with migration instructions if Pebble exists.
- Add `mdbx` to `persistent.db-engine` with an MDBX config section.
- Implement a new execution backend package (example: `execution/erigonexec`) that satisfies
  `execution.FullExecutionClient` using Erigon state/history.
- Add an offline migration tool (example: `cmd/mdbx-migrate`) to rebuild Erigon state/history from existing Pebble data.
- Keep geth execution and storage untouched; the Erigon backend is selected by auto-detect or override.

## Configuration Changes
- `persistent.db-engine`: add `mdbx` option; allow `auto`/empty to auto-detect.
- `persistent.mdbx.*`: new config section for MDBX options (page size, map size, growth step, flags, reader limits).
- `execution.backend`: new backend selector (default `auto`).
- When `execution.backend=erigon`, `persistent.db-engine` must be `mdbx` or `auto` (pebble/leveldb are rejected).
- Optional: `execution.erigon.*` for backend-specific tuning (history window, snapshot settings).
- Optional: `node.transaction-streamer.track-block-metadata-from` to persist block metadata used by `arb_getRawBlockMetadata`.

## MDBX Config Reference (Draft)
- `persistent.mdbx.page-size`: MDBX page size in bytes (`0` = default).
- `persistent.mdbx.map-size`: MDBX map size in bytes (`0` = default).
- `persistent.mdbx.growth-step`: MDBX map growth step in bytes (`0` = default).
- `persistent.mdbx.write-map`: enable MDBX writemap mode.
- `persistent.mdbx.no-sync`: disable fsync (unsafe, for testing only).
- `persistent.mdbx.max-readers`: max MDBX readers (`0` = default).

## Config Snippet (Example)
```text
# CLI flags
--persistent.db-engine=mdbx
--execution.backend=erigon
--persistent.mdbx.map-size=34359738368
--persistent.mdbx.max-readers=4096

# Config file
persistent.db-engine: mdbx
persistent.mdbx.map-size: 34359738368
persistent.mdbx.max-readers: 4096
execution.backend: erigon
```

## MDBX Layout
- Use Erigon-style tables for state and history in `l2chaindata`:
  - PlainState, HashedState, ChangeSets, History, IntermediateTrieHashes, Receipts, Headers, Bodies, etc.
- Store Arbitrum-specific data in MDBX buckets in separate DBs:
  - `arbitrumdata` and `wasm` moved from Pebble to MDBX.
  - Use per-prefix `arb_*` buckets (Erigon-style tables) with a merged iterator to preserve existing prefix-scan semantics.
- Avoid Pebble once MDBX is enabled in config.

## Bucket Mapping (Draft)
| Source DB | Data | Current Key Prefix / DB | MDBX Bucket (Proposed) |
| --- | --- | --- | --- |
| l2chaindata | Headers/Bodies/Receipts | geth rawdb tables | Erigon default tables (Headers, BlockBodies, Receipts, etc.) |
| l2chaindata | State (current) | trie nodes / snapshots | PlainState + HashedState |
| l2chaindata | State history | geth archive snapshots | ChangeSets + History |
| l2chaindata | Trie hashes | trie nodes | IntermediateTrieHashes |
| arbitrumdata | Per-prefix arbitrumdata keys | `m`,`r`,`b`,`t`,`x`,`d`,`e`,`p`,`s`,`a`,`_...` | `arb_*` buckets (see mapping below) |
| wasm | Activated asm / wasm cache | wasm db | arb_wasm |

Per-prefix bucket mapping (arbitrumdata)
- `m` -> `arb_messages`
- `r` -> `arb_message_results`
- `b` -> `arb_block_hash_feed`
- `t` -> `arb_block_metadata_feed`
- `x` -> `arb_missing_block_metadata`
- `d` -> `arb_delayed_messages_legacy`
- `e` -> `arb_delayed_messages_rlp`
- `p` -> `arb_parent_chain_blocks`
- `s` -> `arb_sequencer_batches`
- `a` -> `arb_delayed_sequenced`
- `_` -> `arb_counters`

## New Execution Backend (Erigon)
Package: `execution/erigonexec`

Responsibilities:
- Provide `execution.FullExecutionClient` implementation.
- Use Erigon state readers/writers (PlainState + ChangeSets/History).
- Replace historical lookups with Erigon history readers (HistoryReaderV3).
- Integrate with ArbOS initialization via Erigon state APIs.

Integration:
- Update `cmd/nitro/nitro.go` and `cmd/nitro/init.go` to select backend based on `execution.backend`.
- Keep geth path unchanged to preserve future upgrade compatibility.
- Use `arbos.NewTxProcessorIBS` to bridge Erigon EVM/state to Nitro's ArbOS implementation in erigon builds.

## Build Notes
- Erigon MDBX wiring is gated behind the `erigon` build tag to avoid pulling the Erigon module in default builds.
- When building from this monorepo, add `replace github.com/erigontech/erigon => ./erigon` to the root `go.mod` or fetch Erigon from upstream.

## Migration Tool
Command: `cmd/mdbx-migrate`

Purpose:
- Convert existing Pebble data (`l2chaindata`, `arbitrumdata`, `wasm`) into MDBX with full history.

Inputs:
- Pebble data directories.
- Chain config (for validation).
- Migration options (resume checkpoints, verification level).

Preflight summary
- Source/dest differ, Pebble source only, empty dest, chain config/genesis read, disk space check.

Outputs:
- MDBX datadir with full Erigon layout and Arbitrum buckets.

Output paths
- Checkpoint file: `dest/l2chaindata/.mdbx-migrate/checkpoint.json`
- Conversion canary: `dest/<db>/UNFINISHED_MDBX_CONVERSION` (per DB)

DBs touched per phase (summary)
- Import/senders/execution/txlookup: `l2chaindata`
- Copy: `arbitrumdata`, `wasm`
- Verify: all three DBs (read-only)

Method:
1. Preflight checks: chain ID, genesis hash, schema versions.
2. Open Pebble DBs read-only.
3. Open MDBX datadir (new or empty).
4. Import canonical headers/bodies/TD into MDBX and set stage progress to the imported head (`--end-block` if set, else source head).
5. Run senders recovery (`stages.Senders`).
6. Execute blocks with history enabled (`stages.Execution`).
7. Build tx lookup (`stages.TxLookup`, `stages.Finish`) for full RPC parity.
8. Copy Arbitrum metadata and wasm data into MDBX buckets.
9. Verify head hash, state root, chain config bytes, sampled ArbOS slot history, and arbitrumdata/wasm parity.
10. Clear checkpoint and conversion canary on success.

Resume:
- Record checkpoints per phase (`headers_imported`, `senders_done`, `execution_done`, `txlookup_done`, `copy_done`, `verify_done`).
- On resume, refuse if chain ID/genesis hash differ from the checkpoint.
- Refuse to resume if destination has data beyond the checkpointed phase.
- Resume example: `mdbx-migrate --mode full --resume --verify extended`

Validation:
- Compare canonical head hash and state root with Pebble.
- Verify sampled ArbOS slot history and arbitrumdata/wasm key/value parity.
- If `--start-block` is set, sample only from `[start, imported_head]`.
- On verification failure, keep the checkpoint to allow `--resume` after fixes.

## Migration CLI Spec (Draft)
Command:
- `mdbx-migrate --source <chain-dir> --dest <chain-dir> --mode full --verify extended`

Flags:
- `--source`: path to Pebble chain directory (read-only).
- `--dest`: path for MDBX chain directory.
- `--mode`: `full` (replay with history) or `state` (no history, not recommended).
- `--resume`: continue from last checkpoint.
- `--start-block`, `--end-block`: limit replay range for testing.
- `--workers`: parallelism for execution stages.
- `--verify`: `basic`, `extended`, or `strict` (default `extended`). `strict` enforces all slots even across ArbOS versions.
- `--verify-samples`: number of sampled blocks for history checks (default 20).
- `--mdbx.*`: MDBX tuning options (page-size, map-size, growth-step, write-map, no-sync, max-readers).
  - `--verify-samples` applies to `--mode=full` history sampling; `--mode=state` always scans all keys.

Flag defaults (summary)
- `--verify=extended`
- `--verify-samples=20`
- `--workers=0` uses the default execution worker count.
- `--end-block=0` replays to head.

Verification modes (cheat sheet)
- `basic`: head hash/root + key presence.
- `extended`: `basic` + chain config bytes + sampled ArbOS slots + value parity; destination key counts must match source.
- `strict`: `extended` without version gating.

Mode selection (guidance)
- `basic`: quick smoke check after a successful replay.
- `extended`: default for cutover readiness and most operators.
- `strict`: audits or high-risk migrations where full slot parity is required.

Coverage vs runtime (rough)
| Mode | Coverage | Runtime |
| --- | --- | --- |
| basic | head/root + key presence | fast |
| extended | basic + sampled history + value parity | medium |
| strict | extended without version gating | slow |

Verification runtime
- `strict` can be significantly slower on large chains; reserve for audits.
- `--mode=state` verification scans every key, which can be slow on large datasets.

Resume behavior:
- On `--resume`, skip completed phases based on the checkpoint `phase`.
- Refuse to resume if `chain_id` or `genesis_hash` differs from the checkpoint.
- Checkpoint format example: see `docs/mdbx_migration_design.md`.
- Keep `--start-block`/`--end-block` consistent with the checkpointed run when resuming.
- `--workers` can change on resume (applies to execution replay only).
- If a checkpoint exists and `--resume` is not set, fail fast to avoid overwriting progress.

Operational caution
- Avoid `--mdbx.no-sync` for production migrations unless you accept higher crash risk.

Current implementation status:
- `--mode=state` migrates only `arbitrumdata` and `wasm` into MDBX; `l2chaindata` is untouched by design.
- `--mode=full` bootstraps chain config, imports canonical headers/bodies/TD, runs senders/execution/txlookup, copies `arbitrumdata`/`wasm`, and clears the checkpoint after successful verification.
- Full verification logic covers head hash/state root, chain config bytes, sampled ArbOS slots, and arbitrumdata/wasm parity.

Exit codes:
- 0: success.
- 2: preflight failure.
- 3: migration failure.
- 4: verification failure.

## CLI Examples (Common Scenarios)
- Full migration: `mdbx-migrate --source <pebble> --dest <mdbx> --mode full --verify extended`
- Resume after crash: `mdbx-migrate --mode full --resume --verify extended`
- Resume with higher parallelism: `mdbx-migrate --mode full --resume --workers 8 --verify extended`
- Test range (keep start/end consistent on resume): `mdbx-migrate --source <pebble> --dest <mdbx> --mode full --start-block 0 --end-block 50000 --verify basic`
- State-only buckets (full scan verification): `mdbx-migrate --source <pebble> --dest <mdbx> --mode state --verify extended`
- Audit run: `mdbx-migrate --mode full --resume --verify strict --verify-samples 100`

## Cutover Steps
1. Stop node.
2. Run `mdbx-migrate` to create MDBX datadir.
3. Update config (optional if using auto-detect defaults):
   - `persistent.db-engine = mdbx` (or leave auto-detect)
   - `execution.backend = erigon` (or leave auto-detect)
4. Start node and monitor sync/health checks.

## Operator Runbook (Draft)
1. Backup existing chain data directory.
2. Ensure no Nitro process is running and disk space is sufficient for a full replay.
3. Run migration with extended verification.
4. Confirm logs show head hash, state root, and ArbOS/arbitrumdata parity checks pass.
5. See `docs/mdbx_operator_runbook.md` for the full cutover/rollback procedure.
5. Start Nitro with MDBX backend auto-detected.
6. Validate RPC: `eth_blockNumber`, `arb_checkPublisherHealth`, `arb_getRawBlockMetadata` (if block metadata is tracked), and sample historical reads.
   - To populate block metadata, set `node.transaction-streamer.track-block-metadata-from` before or during the source node’s run so keys exist to migrate.
7. Monitor for MDBX map size warnings and adjust `persistent.mdbx.map-size` if needed.

Preflight sizing (guidance)
- Disk: plan for roughly 2x source data size during replay (history can inflate writes); if unsure, target 2.5x.
- Time: run a small range (e.g., 50k blocks) and compute blocks/sec; estimate total time as `total_blocks / rate`.
- Map size: set `persistent.mdbx.map-size` comfortably above expected final size; bump it if warnings appear.
- Parallelism: start with `--workers` near CPU cores, then reduce if IO saturation or memory pressure occurs.
- Sample count: verification time scales roughly linearly with `--verify-samples` in `--mode=full`.
- Sample size sizing (rule of thumb):
  - 20 for dev/test runs or small chains.
  - 50 for medium chains or first cutover.
  - 100+ for large chains or high-risk migrations.
- Fixture guidance (Erigon-style): use small fixtures for unit tests (hundreds to a few thousand blocks) and a larger fixture for migration parity that exercises ArbOS init, delayed messages, metadata prefixes, and at least one upgrade.

Expected log markers (per phase)
- Full-mode phases (import/senders/execution/txlookup) appear only once full migration is implemented.
- Init: run config (source/dest/mode/verify/flags).
- Preflight: chain ID and genesis hash (paths logged in init).
- Startup checks (node): `erigonexec startup checks status=ok head=... root=...`
- Resume: checkpoint phase and last imported/executed (only with `--resume`).
- Import: block count and imported head.
- Import may emit `status=start|progress|done` and `status=skip reason=checkpoint` when resuming.
- Senders: recovered sender progress.
- Execution: executed block range and rate.
- TxLookup: completed block range (required for full RPC support).
- Copy: arbitrumdata/wasm keys copied.
- Verify: head hash/root and ArbOS slot sampling summary (full mode); key/value scan counts (state mode).
- Finalize: canary removed (and checkpoint cleared in full mode).

Example log lines (state mode)
```text
phase=init source=/data/chain dest=/data/mdbx mode=state verify=extended verify_samples=20 resume=false start_block=0 end_block=0 workers=0
phase=preflight chain_id=42161 genesis_hash=0x...
phase=copy dataset=arbitrumdata status=start
phase=copy dataset=arbitrumdata keys=12345 bytes=67890
phase=finalize dataset=arbitrumdata canary=removed
phase=copy dataset=wasm status=start
phase=copy dataset=wasm keys=456 bytes=7890
phase=finalize dataset=wasm canary=removed
phase=verify dataset=arbitrumdata mode=extended keys=12345 bytes=67890
phase=verify dataset=wasm mode=extended keys=456 bytes=7890
phase=done status=ok
```

Example log lines (full mode, planned format)
```text
phase=init source=/data/chain dest=/data/mdbx mode=full verify=extended verify_samples=20 resume=false start_block=0 end_block=0 workers=8
phase=preflight chain_id=42161 genesis_hash=0x...
phase=resume checkpoint_phase=execution_done last_header_imported=123456 last_executed=123456
phase=import from=0 to=... status=start
phase=import status=progress blocks=... last=...
phase=import status=done blocks=... head=...
phase=senders head=...
phase=execution from=... to=... rate=... blocks_per_sec
phase=txlookup head=...
phase=copy dataset=arbitrumdata keys=...
phase=copy dataset=wasm keys=...
phase=verify dataset=l2chaindata section=head_root head=...
phase=verify dataset=l2chaindata section=arbos_slots samples=20 start_block=0 end_block=...
phase=verify dataset=l2chaindata section=chain_config status=ok
phase=verify dataset=arbitrumdata mode=extended keys=...
phase=verify dataset=wasm mode=extended keys=...
phase=done status=ok
```

Operator checklist (summary)
- Stop node and backup data.
- Run migration with `--verify extended` (or `strict` for full slot checks).
- Start Nitro with MDBX backend and confirm RPC parity.
  - Use `--verify-samples` to increase coverage if needed.
  - Use `strict` only when you need full audits or high-risk cutovers.
- Confirm MDBX metrics show healthy map headroom and no read-tx pressure.

Metrics and Health Checks (Draft)
Metrics
- `arb/mdbx/map/size_bytes`, `arb/mdbx/map/used_bytes`, `arb/mdbx/map/free_bytes`, `arb/mdbx/map/headroom_ratio`.
- `arb/mdbx/read_tx/active`, `arb/mdbx/read_tx/limit`, `arb/mdbx/read_tx/waits_total`.
- `arb/mdbx/page_faults_total`, `arb/mdbx/txn_restarts_total`.
- Alerting guidance: warn at headroom < 15%, critical at headroom < 5%.
- `read_tx/waits_total` increments when the MDBX read-tx limiter acquisition exceeds a small wait threshold (best-effort).
- `txn_restarts_total` increments on MDBX transaction errors that require a restart (`BAD_TXN`, `TXN_FULL`).

Startup health checks
- Head header and state root are readable.
- `arb_*` (arbitrumdata) and `arb_wasm` buckets open cleanly.
- `UNFINISHED_MDBX_CONVERSION` canary is absent.
- History pruning is disabled (full history required for debug/trace parity).
- `Headers`, `BlockHashes`, `Bodies`, `Senders`, `Execution`, `TxLookup`, and `Finish` stages have progressed to the current head.
- History data is present (history snapshots or history tables) when head > 0.
- Mixed Pebble/MDBX detection fails fast with a clear error.

Implementation touchpoints (draft)
- Metrics wiring: `execution/erigonexec/db_erigon.go` (MDBX env stats) plus a small metrics updater in `execution/erigonexec` or `cmd/nitro/nitro.go`.
- Startup checks: `cmd/nitro/nitro.go` for boot-time validation, `cmd/conf/database.go` for mixed DB detection, `execution/erigonexec/mdbx_health_erigon.go` for canary + history checks.
- Canary lifecycle: `cmd/mdbx-migrate/*` writes and removes `UNFINISHED_MDBX_CONVERSION`.

Rollback verification (checklist)
- Stop node and restore the Pebble datadir from backup.
- Set `persistent.db-engine=pebble` (or `auto`) and `execution.backend=geth`.
- Start Nitro and confirm the head hash/state root match your pre-migration snapshot.
- Re-run RPC spot checks: `eth_blockNumber`, `eth_getBlockByNumber`, `arb_checkPublisherHealth`, `arb_getRawBlockMetadata` (if block metadata is tracked).

Failure handling (summary)
- Inspect logs for the failing phase and block range.
- Keep checkpoint/canary and rerun with `--resume` after fixing the issue.
  - Example: `mdbx-migrate --mode full --resume --verify extended`

Verify mode decision flow (quick)
```text
Need only a sanity check? -> basic
Need standard cutover confidence? -> extended
Need full audit / high-risk change? -> strict
```

Common failure patterns (and fixes)
- Genesis/hash mismatch: verify `--source` points to the correct chain and rerun.
- Mixed DB detection: remove partial MDBX dir or rerun with a clean destination.
- Map size exhaustion: increase `persistent.mdbx.map-size` and rerun with `--resume`.
- Verification mismatch at a single block: rerun with `--verify strict` and increase `--verify-samples` or narrow `--start-block`/`--end-block` to localize.
- Crash mid-replay: check disk space, then rerun with `--resume` (start/end must match).

## Compatibility Strategy
- Keep the geth-based backend unchanged for upstream compatibility.
- Default to Erigon when MDBX data exists; require migration if Pebble data exists.
- Avoid altering existing interfaces; add a factory to choose backend.

## Risks and Mitigations
- Long migration time: provide resume checkpoints and progress reporting.
- Large disk usage: allow optional pruning and history window config in Erigon backend.
- Validation gaps: add post-migration verification steps and tooling.

## Compatibility Matrix (Geth Path vs Erigon Backend)
- Cross-chain/rollup core logic (arbnode, inbox, sequencer): should remain unchanged if the backend implements `execution.*` interfaces.
- Validator/staker proving:
  - Requires `ExecutionRecorder` parity (`RecordBlockCreation`, `PrepareForRecord`) and equivalent preimage/state access.
  - Current implementation is geth-specific (`execution/gethexec/block_recorder.go`).
- ArbOS initialization/state:
  - Current path uses geth `state.StateDB` (`arbos/arbosState/initialize.go`).
  - Erigon backend must provide compatible state init and storage APIs.
- RPC/debug/trace:
  - Current RPC backend uses geth chain/db types (`execution/gethexec/node.go`).
  - Decision: follow Erigon trace/debug path (history-based state-at-block/tx, no fallback).
- Message results and Arbitrum metadata:
  - Stored in `arbitrumdata` today; must be migrated and accessible via MDBX buckets.

## Interface Parity Checklist
- `ExecutionClient`:
  - `DigestMessage`, `Reorg`, `HeadMessageNumber`, `ResultAtPos`,
    `MessageIndexToBlockNumber`, `BlockNumberToMessageIndex`.
- `ExecutionRecorder`:
  - `PrepareForRecord`, `RecordBlockCreation` (must return equivalent preimages/state proofs),
    `MarkValid`.
- `ExecutionSequencer`:
  - `SequenceDelayedMessage`, `NextDelayedMessageNumber`, `MarkFeedStart`,
    `Synced`, `FullSyncProgressMap`.
- ArbOS state initialization:
  - Provide Erigon-compatible state init and storage APIs replacing geth `state.StateDB`.
- RPC/debug/trace:
  - Provide Erigon-backed debug/trace via history readers (no fallback).
- Arbitrum metadata:
  - Message results, block metadata, and pruner markers must be readable from MDBX.

## Milestones and Tasks
1. Config and plumbing
   - Add `mdbx` engine option and config structs.
   - Add backend selector and factory in `cmd/nitro`.
2. MDBX support
   - Implement MDBX opening and datadir wiring.
   - Add an MDBX-backed `ethdb.Database` adapter that merges per-prefix `arb_*` buckets for `arbitrumdata` (preserve prefix-scan semantics) plus `arb_wasm`.
   - Define and enforce per-prefix bucket mappings for Arbitrum data.
3. Erigon execution backend
   - Implement `execution/erigonexec` with state + history.
   - Wire ArbOS initialization and EVM hooks.
4. Migration tool
   - Implement `cmd/mdbx-migrate` with replay + copy + verify.
   - Add resume and checkpoint support.
5. Validation and docs
   - Add verification scripts and troubleshooting notes.
   - Document rollback to geth backend if needed.

## Related Docs for Review
- docs/decisions/0002-mdbx-erigon-backend.md
- docs/decisions/README.md
- docs/erigon_execution_design.md
- docs/mdbx_migration_design.md
- erigon/README.md
- erigon/db/kv/Readme.md
- erigon/node/interfaces/_docs/staged-sync.md
- erigon/docs/programmers_guide/db_faq.md
- erigon/docs/programmers_guide/db_walkthrough.MD
- erigon/docs/programmers_guide/dupsort.md
- erigon/docs/programmers_guide/guide.md
