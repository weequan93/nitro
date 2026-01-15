# MDBX/Erigon Implementation Checklist

## Purpose
- Add MDBX as a first-class engine and wire an Erigon-backed execution/storage backend.
- Keep the existing geth path unchanged for upstream Nitro compatibility.
- Provide a safe, offline migration from Pebble to MDBX.

## Current Status
- `persistent.db-engine` supports `mdbx` and `auto`; `execution.backend` supports `auto|geth|erigon`.
- MDBX options config is wired and validated.
- Backend auto-detect, mixed-DB guards, and selection logging are in place.
- Auto-detect errors on partial MDBX datadirs (avoids creating empty DBs).
- `execution.backend=erigon` rejects `persistent.db-engine=pebble`/`leveldb` (MDBX only).
- `execution.backend=erigon` requires MDBX data in `l2chaindata`, `arbitrumdata`, and `wasm`.
- MDBX adapters exist for `arbitrumdata` and `wasm` buckets with per-prefix `arb_*` buckets, merged iteration, and `arb_data` as a fallback bucket.
- `cmd/mdbx-migrate --mode=state` migrates `arbitrumdata`/`wasm` only; `--mode=full` replays `l2chaindata` with checkpoint/resume and verification.
- Full verification samples are configurable via `--verify-samples` (used by full verify).
- Erigon execution client implements digest/reorg/recording and sequencing (uses erigon txpool internally; txpool RPC is stubbed); forwarding and seq-coordinator hooks are supported; timeboost/express-lane is supported.
- Startup health checks enforce readable head/root, no history pruning, required stage progress to head, and history data presence; MDBX map/read-tx metrics are wired; migration smoke tests exist (state fixture + opt-in full).
- Erigon build wires an ArbOS processing hook adapter (`arbos.NewTxProcessorIBS`) that bridges Erigon EVM/state to Nitro’s ArbOS implementation.
- Erigon startup validates ArbOS chain config and blocks geth-only init/reorg/blocks-reexecutor paths.

## Minimal Touchpoints (Upstream-Safe)
- `cmd/nitro/nitro.go`
- `cmd/nitro/init.go`
- `cmd/conf/database.go`
- `execution/backend/factory.go`
- `execution/erigonexec/*`
- `cmd/mdbx-migrate/*`

## Milestone A: Plumbing Complete
Tasks
- Ensure backend auto-detect is stable and logs selected backend.
- Enforce mixed DB guardrails for `l2chaindata`, `arbitrumdata`, `wasm`.
- Keep geth defaults unchanged unless MDBX is present.

Acceptance
- Pebble-only datadir starts on geth path unchanged.
- MDBX-only datadir (l2chaindata/arbitrumdata/wasm) selects Erigon backend.
- Mixed Pebble/MDBX datadir fails fast with a clear error.
- Partial MDBX datadir fails fast with a clear error.

## Milestone B: MDBX Arbitrum Data
Tasks
- Finalize per-prefix bucket mapping for `arbitrumdata` (`arb_*`) and `wasm`.
- Add a merged iterator/adapter across `arb_*` buckets to preserve prefix scan behavior.
- Add migration verification for these buckets (basic + extended).

Acceptance
- Existing Nitro reads/writes `arbitrumdata`/`wasm` via merged MDBX adapters without code changes.
- `mdbx-migrate --mode=state --verify extended` passes on a small fixture chain.

## Milestone C: Erigon Execution Core
Tasks
- Implement `execution.FullExecutionClient` in `execution/erigonexec/client.go`.
- Port `ExecutionRecorder` and `ExecutionSequencer` parity from `execution/gethexec/*`.
- Add ArbOS initialization compatibility for Erigon state.
- Implement Erigon debug/trace via history readers (no fallback).

Acceptance
- Erigon backend sequences blocks and produces identical head hash to geth on test fixtures.
- Validator/staker paths work with equivalent preimages/state proofs.

## Milestone D: Full Migration (l2chaindata)
Tasks
- Implement `cmd/mdbx-migrate --mode=full` replay into Erigon MDBX.
- Add checkpoint/resume support for long replays.
- Enforce resume argument consistency (`--start-block`/`--end-block`) and allow `--workers` override.
- Expose `--verify-samples` to tune history sampling.
- Extend verification to compare head hash/state root, chain config bytes, and sampled ArbOS slot history reads.

Acceptance
- Full migration completes on a test chain; verification matches head/root, chain config bytes, and ArbOS slot samples.
- Resume works after interruption without data loss; it rejects changed start/end ranges.

## Milestone E: Production Readiness
Tasks
- Add metrics for MDBX map usage and read tx limiter.
- Add startup health checks for head hash, state root, and arb metadata availability.
- Document rollback procedure and operator runbook.

Acceptance
- Staging cutover succeeds with clean metrics and no mixed-DB starts.
- Rollback procedure is tested and documented.

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

Docs and runbook
- `docs/mdbx_migration_plan.md`
- `docs/decisions/0002-mdbx-erigon-backend.md`
- `docs/mdbx_operator_runbook.md`

## Dev Setup Notes
- For `-tags erigon` builds, wire local Erigon via `replace github.com/erigontech/erigon => ./erigon` (or use a `go work` file).
- If Erigon pulls `github.com/erigontech/nitro-erigon`, add `replace github.com/erigontech/nitro-erigon => .`.
- If `go-kzg-4844`/`gnark-crypto` API mismatches appear, pin `github.com/consensys/gnark-crypto` to `v0.12.1`.
- Generate contract bindings in `solgen/go/*` with `make .make/solgen` (requires `yarn` contract builds).
- Hardhat expects Node 14/16/18; use a supported Node or `YARN_IGNORE_ENGINES=1` for local builds.
- Full `go build ./...` needs `arbitrator.h`; run `make build-node-deps` to generate native headers and wasm libs.

## Validation Checklist
- `go test ./...`
- `go test -tags erigon ./...`
- Run `mdbx-migrate --mode=state --verify extended` on a small fixture.
- Run `mdbx-migrate --mode=full --verify extended` on a staged copy.
- RPC parity checks: `eth_blockNumber`, `eth_getBlockByNumber`, `arb_checkPublisherHealth`, `arb_getRawBlockMetadata` (if block metadata is tracked via `node.transaction-streamer.track-block-metadata-from`).

## Rollout Sequence
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

Critical path
- Milestone A must finish before B/C/D.
- Milestone B unblocks migration verification for arbitrumdata/wasm.
- Milestone C is required before production cutover.
- Milestone D is required for full history parity.
- Milestone E is required for production rollout.

Estimated timeline (placeholder)
- Milestone A: 1 week
- Milestone B: 1 week
- Milestone C: 3-5 weeks
- Milestone D: 2-4 weeks
- Milestone E: 1 week

## Detailed Task Checklist (Code-Level)
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

Milestone E (prod readiness)
- Add MDBX map-size/headroom metrics and read-tx limiter metrics.
- Add startup health checks (head/root readable, buckets open, no conversion canary).
- Document operator runbook: cutover, verification, and rollback.
- Add migration smoke tests (state-mode fixture, small full-mode replay).

## Decisions (Resolved)
- Decision: support both full-history and pruned modes (Erigon-style), but require full history for debug/trace parity.
- Decision: reuse Erigon full staged-sync pipeline for L2 replay; validate final state against Erigon RPC state (head hash/root + sampled ArbOS slots).
- Decision: full RPC support requires stages `Headers`, `BlockHashes`, `Bodies`, `Senders`, `Execution` (history enabled), `TxLookup`, and `Finish`.
- Decision: use `SpawnRecoverSendersStage` for L2 blocks; keep Arbitrum business logic out of sender recovery.
- Decision: follow Erigon's trace/debug RPC path (state-at-block/transaction via history reader; no fallback).
- Decision: follow Erigon-style per-prefix `arb_*` buckets with a merged iterator to preserve prefix scans.
- Decision: follow Erigon-style testing—use small fixture chains for unit tests and larger fixtures for migration parity, sized to cover ArbOS init, delayed messages, metadata prefixes, and at least one upgrade.

## Issue List (Suggested)
Milestone A
- [x] Add backend selection log line and document expected default behavior.
- [x] Tighten mixed-DB detection for `l2chaindata/arbitrumdata/wasm`.
- [x] Add a config doc snippet for `persistent.db-engine` and `persistent.mdbx.*`.

Milestone B
- [x] Validate merged iterator prefix parity across `arb_*` buckets in MDBX adapter.
- [x] Add a `cmd/mdbx-migrate --mode=state` fixture test.
- [x] Add verify summary output (counts, elapsed time).

Milestone C
- [x] Implement `execution.FullExecutionClient` methods in `execution/erigonexec/client.go`.
- [x] Port recorder parity (`RecordBlockCreation`, `PrepareForRecord`, `MarkValid`).
- [x] Port sequencer parity (`SequenceDelayedMessage`, `NextDelayedMessageNumber`).
- [x] Add ArbOS init compatibility wrapper.
- [x] Implement Erigon debug/trace via history readers (no fallback).

Milestone D
- [x] Implement `--mode=full` replay into MDBX.
- [x] Add checkpoint/resume support for long replays.
- [x] Enforce resume arg guardrails (`--start-block`/`--end-block` match checkpoint; `--workers` allowed to change).
- [x] Extend verification with head hash/state root parity, chain config bytes, and ArbOS slot history sampling.
- [x] Set and document checkpoint phase names and order (`headers_imported`, `senders_done`, `execution_done`, `txlookup_done`, `copy_done`, `verify_done`).

Milestone E
- [x] Add MDBX map-size/headroom metrics and read-tx limiter metrics.
- [x] Add startup health checks (head/root readable, buckets open, no conversion canary).
- [x] Document operator runbook: cutover, verification, and rollback.
- [x] Add migration smoke tests (state-mode fixture, small full-mode replay).
