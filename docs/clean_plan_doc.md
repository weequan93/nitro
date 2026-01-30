# MDBX/Erigon Implementation 

## Implementation Summary (Official Nitro Upgrade -> Erigon-style MDBX)
- Add MDBX as a supported `persistent.db-engine` with a dedicated `persistent.mdbx.*` config block; keep Pebble/geth untouched. (Complexity: Low, Risk: Low)
- Introduce `execution.backend=auto|geth|erigon` with strict auto-detect and clear migration-required errors. (Complexity: Low, Risk: Medium)
- Define the MDBX layout (l2chaindata state/history tables + arbitrumdata/wasm `arb_*` buckets) and finalize prefix-to-bucket mapping. (Complexity: Medium, Risk: Medium)
- Build `execution/erigonexec` to cover sequencing, recording, and RPC/trace using Erigon state/history + ArbOS IBS adapter. (Complexity: High, Risk: High)
- Coverage for archive, sequencer, validator, forwarder, and full-node modes in erigonexec + config defaults. (Complexity: Medium, Risk: Medium)
- Deliver `mdbx-migrate` to replay full history, copy arbitrum/wasm data, and support checkpoint/resume. (Complexity: High, Risk: High)
- Define snapshot/fast-sync strategy (if required) consistent with Erigon history/pruning behavior. (Complexity: Medium, Risk: High)
- Implement verification and parity checks (head hash/root, sampled ArbOS slots, arbitrum/wasm key/value parity). (Complexity: Medium, Risk: High)
- Add integration tests for parity and provide operator cutover/runbook guidance. (Complexity: Medium, Risk: Medium)


## Minimal Touchpoints (Upstream-Safe)
- `cmd/nitro/nitro.go`
- `cmd/nitro/init.go`
- `cmd/conf/database.go`
- `execution/backend/factory.go`
- `execution/erigonexec/*`
- `cmd/mdbx-migrate/*`
- `arbos`

## Milestone A: Plumbing Complete
Tasks
- Ensure backend auto-detect is stable and logs selected backend.
- Enforce mixed DB guardrails for `l2chaindata`, `arbitrumdata`, `wasm`.
- Keep geth defaults unchanged unless MDBX is present.



## Milestone B: MDBX Arbitrum Data
Tasks
- Finalize per-prefix bucket mapping for `arbitrumdata` (`arb_*`) and `wasm`.
- Add a merged iterator/adapter across `arb_*` buckets to preserve prefix scan behavior.
- Add migration verification for these buckets (basic + extended).


## Milestone C: Erigon Execution Core
Tasks
- Implement `execution.FullExecutionClient` in `execution/erigonexec/client.go`.
- Port `ExecutionRecorder` and `ExecutionSequencer` parity from `execution/gethexec/*`.
- Add ArbOS initialization compatibility for Erigon state.
- Implement Erigon debug/trace via history readers (no fallback).


## Milestone D: Full Migration (l2chaindata)
Tasks
- Implement `cmd/mdbx-migrate --mode=full` replay into Erigon MDBX.
- Add checkpoint/resume support for long replays.
- Enforce resume argument consistency (`--start-block`/`--end-block`) and allow `--workers` override.
- Expose `--verify-samples` to tune history sampling.
- Extend verification to compare head hash/state root, chain config bytes, and sampled ArbOS slot history reads.


## Milestone E: Production Readiness
Tasks
- Add metrics for MDBX map usage and read tx limiter.
- Add startup health checks for head hash, state root, and arb metadata availability.
- Document rollback procedure and operator runbook.


## Module Breakdown (Tasks and Touchpoints)
Backend selection/config
- `cmd/conf/database.go`, `cmd/nitro/backend_select.go`, `execution/backend/factory.go`
- Finalize auto-detect rules and clear logging.

MDBX adapters
- `execution/erigonexec/kvdb/kvdb_erigon.go`
- Ensure prefix iteration matches Pebble behavior; preserve `ethdb` semantics.

Erigon execution backend
- `execution/erigonexec/client.go`
- `execution/erigonexec/*` state/history readers and writers
- Provide parity for sequencing, recording, and sync status.

ArbOS init/state compatibility
- `execution/erigonexec/*` shim to initialize ArbOS state without touching geth path.

Migration tool
- `cmd/mdbx-migrate/migrate_erigon.go`, `cmd/mdbx-migrate/migrate.go`
- Implement `--mode=full` replay and verification.

## Sequence
1. Backup datadir.
2. Run full migration with extended verification.
3. Start Nitro with auto-detect (or `persistent.db-engine=mdbx`, `execution.backend=erigon`).
4. Validate RPC parity and metrics.
5. Rollback if needed by restoring Pebble datadir and setting `execution.backend=geth`.

## Ownership and Sequencing (Suggested)
Workstreams
- Execution backend: implement Erigon execution client parity (Milestone C).
- Storage/MDBX: bucket mapping + adapters + mixed DB guards (Milestone A/B).
- Migration tool: full replay + verify + resume (Milestone D).
- QA/Validation: fixture chain, parity tests, RPC checks (Milestones B-D).
- Release/Docs: runbook, rollback, config reference (Milestone E).

Estimated timeline 
- Milestone A: 1 week
- Milestone B: 1 week
- Milestone C: 3-5 weeks
- Milestone D: 2-4 weeks
- Milestone E: 2-3 weeks
- Milestone F: 1 week

## Detailed Task (Code-Level)
Milestone A (plumbing)
- Add backend selection log line in `cmd/nitro/backend_select.go`.
- Ensure mixed DB detection checks all three dirs and errors consistently.
- Keep defaults untouched when no MDBX data exists.

Milestone B (arb data + wasm)
- Implement merged iteration across `arb_*` buckets and validate prefix parity in `execution/erigonexec/kvdb/kvdb_erigon.go`.
- Validate all `arbitrumdata` prefixes are preserved as-is (no key rewriting).
- Add a small fixture migration test for `cmd/mdbx-migrate --mode=state`.

Milestone C (execution core)
- Implement `execution.FullExecutionClient` in `execution/erigonexec/client.go`:
  - `DigestMessage`, `Reorg`, `ResultAtPos`, `HeadMessageNumber` parity.
  - Sequencer controls (`SequenceDelayedMessage`, `NextDelayedMessageNumber`).
  - Validator recorder (`RecordBlockCreation`, `PrepareForRecord`, `MarkValid`).
- Provide ArbOS init support in `execution/erigonexec` without changing geth callers.
- Implement Erigon debug/trace via history readers (no fallback).

Milestone D (full migration)
- Implement `--mode=full` replay in `cmd/mdbx-migrate/migrate_erigon.go`.
- Add checkpoint/resume under `dest/l2chaindata/.mdbx-migrate`.
- Enforce resume guardrails (`--start-block`/`--end-block` must match checkpoint; `--workers` may change).
- Extend `verify` for head hash/state root, chain config bytes, and sampled ArbOS slot history reads.
- Define checkpoint phase names and expected ordering (`headers_imported`, `senders_done`, `execution_done`, `txlookup_done`, `copy_done`, `verify_done`).

Milestone E (Node type coverage)
- validator, relayer, poster, sequencer, full node, arhieve node

Milestone F (prod readiness)
- Add MDBX map-size/headroom metrics and read-tx limiter metrics.
- Add startup health checks (head/root readable, buckets open, no conversion canary).
- Document operator runbook: cutover, verification, and rollback.
- Add migration smoke tests (state-mode fixture, small full-mode replay).


