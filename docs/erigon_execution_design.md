# Erigon Execution Backend Design (Milestone C)

## Goals
- Implement `execution.FullExecutionClient` on top of Erigon storage/state/history.
- Keep the existing geth path unchanged and upstream-friendly.
- Support sequencing, validation (recording), and RPC parity required by Nitro.

## Non-goals
- No changes to consensus rules, L2 block format, or ArbOS logic for the geth path.
- No live in-place migrations; migration remains an offline tool.
- No geth-style init/reorg/blocks-reexecutor workflows on the Erigon backend.

## Key Constraints
- Nitro’s execution logic uses geth types (`core`, `state.StateDB`, `types.Block`, `types.Receipt`).
- Erigon uses its own state/storage APIs and MDBX layout.
- We must avoid modifying geth execution core; add new erigon-tagged code or adapters instead.
- Erigon backend requires existing MDBX data in `l2chaindata`, `arbitrumdata`, and `wasm`; no geth-style init path.

## Component Map (Geth Path -> Erigon Path)
Execution engine
- Geth: `execution/gethexec/executionengine.go`
- Erigon: `execution/erigonexec/engine.go` (new)

Sequencer
- Geth: `execution/gethexec/sequencer.go`
- Erigon: `execution/erigonexec/sequencer.go` (new)

Recorder (validator/staker)
- Geth: `execution/gethexec/block_recorder.go`
- Erigon: `execution/erigonexec/block_recorder.go` (new)

RPC/debug/trace shims
- Geth: `execution/gethexec/node.go`, `execution/gethexec/api.go`
- Erigon: `execution/erigonexec/node.go`, `execution/erigonexec/api.go` (new)

Block metadata
- Geth: `execution/gethexec/blockmetadata.go`
- Erigon: `execution/erigonexec/blockmetadata.go` (new)

ArbOS initialization
- Geth: `arbos/arbosState/initialize.go`
- Erigon: `execution/erigonexec/arbos_init.go` (new, erigon-tagged)

## Architecture Overview
Databases
- `l2chaindata`: Erigon MDBX (headers, bodies, receipts, state, history).
- `arbitrumdata`: MDBX `arb_*` buckets (per-prefix) for Arbitrum metadata; a merged iterator preserves prefix scans and `arb_data` remains as a fallback bucket.
- `wasm`: MDBX bucket `arb_wasm`.

Execution pipeline
- Use Erigon staged-sync execution primitives (`erigon/execution/stagedsync/*`) to apply blocks.
- Use Erigon state/history readers (`erigon/execution/exec3`, `erigon/core/state`) for historical reads.

Block IO
- Use Erigon block readers/writers: `erigon/db/rawdb/blockio`, `erigon/eth/backend.go` setup flow.
- Provide a thin adapter for the subset of reads Nitro needs (header, block, receipts).

## Proposed Implementation Approach
### 1) Core Client Skeleton
File: `execution/erigonexec/client.go`
- Hold references to:
  - `kv.RwDB` for chain MDBX
  - `ethdb.Database` adapters for `arbitrumdata`/`wasm`
  - Erigon block reader/writer (`services.FullBlockReader`, `blockio.BlockWriter`)
  - Chain config and consensus engine
- Implement `Start/Stop` to open DBs and initialize Erigon services.
  - Initial skeleton now opens MDBX chain/arb/wasm DBs, reads chain config/genesis,
    and implements header-derived read-only helpers (head, result, ArbOS version, block<->message mapping).

### 2) Sequencing Path
File: `execution/erigonexec/sequencer.go`
- Port logic from `execution/gethexec/sequencer.go`:
  - `SequenceDelayedMessage`
  - `NextDelayedMessageNumber`
  - `MarkFeedStart`
  - `Synced`, `FullSyncProgressMap`
- Use Erigon’s execution stage to apply produced blocks and write receipts/state/history.
- Store Arbitrum results/metadata in `arbitrumdata` bucket unchanged.

### 3) Block Production and ArbOS
Files: `execution/erigonexec/engine.go`, `execution/erigonexec/arbos_init.go`
- Nitro’s block production uses geth `state.StateDB` (see `arbos/block_processor.go`).
- Add erigon-tagged ArbOS init and block production wrappers:
  - Duplicate ArbOS init logic using Erigon state APIs.
  - Keep geth path untouched.
  - Header helpers now exist to build Arbitrum headers and update send-root/L1 fields from ArbOS state when using Erigon IBS.
- Decision: use Erigon-native block production with the ArbOS IBS adapter (`arbos.NewTxProcessorIBS`), keeping Arbitrum logic intact while relying on Erigon state/history.

### 4) Recorder (Validator/Staker)
File: `execution/erigonexec/block_recorder.go`
- Geth uses `arbitrum.RecordingDatabase` with geth `state.StateDB` and block replay.
- For Erigon, leverage witness generation and history readers:
  - Use `erigon/execution/stagedsync/stage_witness.go` for witness and preimage generation.
  - Record preimages and user wasm data required by Nitro.
- Decision: use Erigon's witness pipeline and history readers for recorder parity.
- Ensure `RecordBlockCreation` and `PrepareForRecord` return identical proof data to geth.

### 5) RPC/Debug/Trace
Files: `execution/erigonexec/node.go`, `execution/erigonexec/api.go`
- Implement shims for RPCs used by Nitro:
  - block/receipt retrieval
  - debug trace calls
  - state queries
- Back these with Erigon’s block reader and state readers.
- Decision: require full RPC parity and follow Erigon's history-based debug/trace path (no fallback).

## Interfaces and Behavioral Parity
ExecutionClient methods
- `DigestMessage`: must produce identical `MessageResult` (block hash, send root).
- `Reorg`: must handle unwind/rewind via Erigon stages and update metadata.
- `HeadMessageNumber`: must map head block -> message index using same logic as geth path.

ExecutionRecorder methods
- `RecordBlockCreation`: must produce preimages and wasm data equivalent to geth.
- `PrepareForRecord`: must preload states for requested range.

ExecutionSequencer methods
- `SequenceDelayedMessage` and `NextDelayedMessageNumber` must preserve ordering and metadata.

## Testing and Acceptance
Unit/integration
- `go test -tags erigon ./...` with a small fixture chain.
- Ensure `DigestMessage` parity with geth on identical inputs.

Functional parity
- Compare head hash, receipts, and state root after replay.
- Validate sample historical reads (state at older block numbers).

## Decisions (Resolved)
- Use Erigon-native block production with the ArbOS IBS adapter (`arbos.NewTxProcessorIBS`); avoid a geth `state.StateDB` adapter.
- Use Erigon's witness stage for recording proofs and preimages.
- Require full RPC/debug parity and follow Erigon's history-based debug/trace path.
