# MDBX Full Migration Design (Milestone D)

## Goals
- Implement `cmd/mdbx-migrate --mode=full` to rebuild `l2chaindata` in Erigon MDBX with full history.
- Preserve all Arbitrum metadata and wasm data from Pebble.
- Provide resumable, verifiable, offline migration.

## Non-goals
- No online/in-place migration.
- No pruning (full history is required in `--mode=full`).
- No changes to runtime Nitro execution logic for the geth path.
 - Note: runtime can support pruned mode later, but `--mode=full` remains full-history for debug/trace parity.
- No geth-style init/reorg/blocks-reexecutor workflows when running the Erigon backend.

## Inputs and Outputs
Inputs
- Source Pebble/LevelDB datadir with `l2chaindata`, `arbitrumdata`, `wasm`.
- Chain config and genesis from source.
- Migration config (`--start-block`, `--end-block`, `--workers`, `--verify`, `--verify-samples`).

Outputs
- Destination MDBX datadir with:
  - `l2chaindata` in Erigon format (PlainState, History, ChangeSets, etc.).
  - `arbitrumdata` in per-prefix MDBX buckets (`arb_messages`, `arb_message_results`, `arb_block_hash_feed`, `arb_block_metadata_feed`, `arb_missing_block_metadata`, `arb_delayed_messages_legacy`, `arb_delayed_messages_rlp`, `arb_parent_chain_blocks`, `arb_sequencer_batches`, `arb_delayed_sequenced`, `arb_counters`).
  - `wasm` in MDBX bucket `arb_wasm`.

## High-Level Flow
1. Preflight checks.
2. Bootstrap MDBX chain DB (genesis, chain config).
3. Import headers/bodies/TD into MDBX.
4. Execute blocks to build state + history.
5. Copy `arbitrumdata` + `wasm`.
6. Verify parity and mark migration complete.

## Preflight Checks
- Ensure `--source` and `--dest` differ and exist.
- Ensure source uses Pebble/LevelDB (not already MDBX).
- Ensure destination DB dirs are empty.
- Read genesis hash and chain config from source.
- Optional: check available disk space (estimate = source size * 2).

Preflight failure examples
- Mixed Pebble/MDBX in source or dest.
- Destination already contains MDBX tables or Pebble markers.

## Bootstrap MDBX Chain DB
File: `cmd/mdbx-migrate/migrate_erigon.go`

Steps
- Open Erigon MDBX chain DB with history enabled.
- Write chain config and genesis data into MDBX tables.
- Initialize stage progress keys (stages start at 0).
- Create or reset migration checkpoint file.

Notes
- Use Erigon rawdb helpers to write chain config and canonical head.
- For now, avoid snapshots; use DB tables only.

Current implementation notes
- Chain config is bootstrapped if missing; genesis data comes from the header/body import (when `--start-block=0`).

## Header/Body/TD Import
Goal
- Populate MDBX with canonical headers, bodies, and total difficulty from Pebble.
- Receipts are rebuilt during execution; they are not imported in the current flow.

Source reads
- `rawdb.ReadCanonicalHash` to get canonical hash per block.
- `rawdb.ReadHeaderRLP`, `rawdb.ReadBodyRLP`, `rawdb.ReadTd` from source.

Destination writes
- Use Erigon rawdb writers to store headers/bodies/TD:
  - `erigon/db/rawdb.WriteHeader`
  - `erigon/db/rawdb.WriteCanonicalHash`
  - `erigon/db/rawdb.WriteBody`
  - `erigon/db/rawdb.WriteTd`

Checkpoint
- Record the last imported block number.

## State and History Replay
Goal
- Execute blocks in order to generate PlainState, History, ChangeSets, and trie hashes.

Approach
- Use Erigon execution stage with history enabled:
  - `erigon/execution/stagedsync/StageExecuteBlocksCfg`
  - `ExecBlockV3` or staged sync pipeline in a loop

Inputs
- Block reader that serves headers/bodies from MDBX.
- Consensus engine compatible with Nitro chain config.
- VM config with Nitro/ArbOS hooks (from Erigon execution backend).
- `wasm` access for stylus programs (adapter for MDBX `arb_wasm`).
- ArbOS init message (delayed message 0) to initialize ArbOS at genesis.

Execution loop
- For each block:
  - Ensure senders stage is populated (if required by Erigon).
  - Execute transactions and write state/history.
  - Commit in batches (every N blocks or MB size).

Init message source
- Read delayed message 0 from `arbitrumdata` (rlp-delayed or legacy prefix) and parse the init message.
- Set the init message in the execution worker before running block 0.

Checkpoint
- Record last executed block number and stage progress.
- Allow resume from checkpoint.

## Arbitrum Data and Wasm Copy
Goal
- Move `arbitrumdata` into per-prefix MDBX buckets and `wasm` into `arb_wasm`, without rewriting keys/values.

Method
- Use prefix-to-bucket mapping when copying (`m`→`arb_messages`, `r`→`arb_message_results`, `b`→`arb_block_hash_feed`, `t`→`arb_block_metadata_feed`, `x`→`arb_missing_block_metadata`, `d`→`arb_delayed_messages_legacy`, `e`→`arb_delayed_messages_rlp`, `p`→`arb_parent_chain_blocks`, `s`→`arb_sequencer_batches`, `a`→`arb_delayed_sequenced`, `_`→`arb_counters`).
- Provide a merged iterator/adapter so existing prefix scans see a single logical `arbitrumdata` view across buckets.
- Copy after l2 execution to simplify recovery (replay first, then metadata).

Block metadata notes
- Block metadata is stored under the `t` prefix in `arb_block_metadata_feed` (`blockMetadataInputFeedPrefix` in `arbnode/schema.go`).
- Missing block metadata markers use the `x` prefix in `arb_missing_block_metadata`.
- `arb_getRawBlockMetadata` reads the `t`-prefixed values; the data is present only if block metadata tracking is enabled on the source (via `node.transaction-streamer.track-block-metadata-from`).

## Verification
Modes
- `basic`: head hash/state root parity for `l2chaindata` (full mode) + key presence in `arbitrumdata`/`wasm`.
- `extended`:
  - Includes `basic`.
  - Sample a set of block numbers and verify ArbOS storage reads via history.
  - Compare chain config bytes stored in ArbOS (subspace `{7}`).
  - Compare `arbitrumdata`/`wasm` key/value pairs (merged view across `arb_*` buckets) and enforce destination key counts match source.
- `strict`:
  - Same as `extended`, but do not skip version-gated ArbOS slots.

Verification scope
- Head hash and state root comparisons always use the imported head.
- Verification can be time-consuming on large chains; sample size and mode control the cost.
- Default sample count: 20 blocks (tunable via a flag).
- Use `extended` for most migrations; reserve `strict` for full audits or high-risk upgrades.
- In `--mode=state`, verification always scans all keys (no sampling).

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

Exit codes (draft)
- 0: success
- 2: preflight failure
- 3: migration failure
- 4: verification failure

Implementation notes
- Compare canonical head hash using rawdb for source and MDBX for dest.
- For state root parity, read from header root values rather than recomputing from scratch.
- For history, use the Erigon history reader for dest (same path as Erigon RPC state reads) and the geth/state reader for source.

## Resume and Checkpointing
Checkpoint file
- Store JSON in `dest/l2chaindata/.mdbx-migrate/checkpoint.json`.

Fields
- `phase`
- `last_header_imported`
- `last_executed`
- `mode`
- `chain_id`
- `genesis_hash`
- `start_block`
- `end_block`

Behavior
- `--resume` continues from the last completed stage.
- Clear checkpoint on success.
- Refuse to resume if `chain_id` or `genesis_hash` mismatches the checkpoint.

Phase markers (suggested)
- `headers_imported`
- `senders_done`
- `execution_done`
- `txlookup_done` (required for full RPC support)
- `copy_done`
- `verify_done`

Phase -> stage progress mapping (draft)
- `headers_imported`: `stages.Headers`, `stages.BlockHashes`, `stages.Bodies` set to imported head.
- `senders_done`: `stages.Senders` set to imported head.
- `execution_done`: `stages.Execution` set to imported head (history enabled).
- `txlookup_done`: `stages.TxLookup` and `stages.Finish` set to imported head.

Example (draft)
```json
{
  "mode": "full",
  "chain_id": 42161,
  "genesis_hash": "0x...",
  "start_block": 0,
  "end_block": 0,
  "last_header_imported": 1234567,
  "last_executed": 1234567,
  "phase": "execution_done"
}
```

Resume logic (draft)
- If checkpoint is `headers_imported`, skip bootstrap/import and start at senders.
- If `senders_done`, skip senders and start execution.
- If `execution_done`, run txlookup, then copy/verify.
- If `txlookup_done`, skip txlookup and proceed to copy/verify.
- Keep `--start-block`/`--end-block` consistent with the initial run; resume should fail if they change.
- `--workers` can be adjusted on resume (applies to execution replay only).

Resume decision table (draft)
| Checkpoint | Next action |
| --- | --- |
| headers_imported | run senders stage |
| senders_done | run execution stage |
| execution_done | run txlookup, then copy/verify |
| txlookup_done | copy/verify |
| copy_done | verify only |
| verify_done | finalize (clear checkpoint) |

Resume guard pseudocode (draft)
```text
if !resume:
  require dest is empty
else:
  ckpt = loadCheckpoint()
  require opts.chain_id == ckpt.chain_id
  require opts.genesis_hash == ckpt.genesis_hash
  require opts.start_block == ckpt.start_block
  require opts.end_block == ckpt.end_block
  require !destHasDataBeyond(ckpt.phase)
  next = phaseToAction(ckpt.phase)
```

Phase progression (draft)
```
bootstrap -> headers_imported -> senders_done -> execution_done
                       -> txlookup_done
                       -> copy_done -> verify_done -> clear_checkpoint
```

## Safety and Guardrails
- Abort if mixed Pebble/MDBX detected in source or destination.
- Write a “conversion in progress” canary before replay; remove on success.
- Refuse to start if chain config or genesis hash mismatch.
- Refuse to start the Erigon backend without existing MDBX data in `l2chaindata`, `arbitrumdata`, and `wasm`.
- Refuse to resume if destination already has data outside the checkpointed phase.
- On failure, keep the checkpoint and canary for diagnosis and resume.

## Performance Notes
- Use `ethdb.IdealBatchSize` for write batches.
- Avoid random access to source DB; iterate sequentially by block number.
- Allow `--workers` to control execution parallelism (Erigon exec workers).
- Disk usage may temporarily exceed 2x source size during replay; plan headroom accordingly.
- Batch commits can be based on block count or bytes; prefer bytes for predictable memory use.
- Higher `--workers` improves CPU-bound stages but can increase IO pressure; tune based on disk throughput.
- If unspecified, `--workers` should default to Erigon `ExecWorkerCount`.
- Use `--workers=1` for deterministic, single-threaded replay during debugging.

## Dependencies
- `-tags erigon` for access to Erigon MDBX and staged sync.
- Requires Erigon execution backend to supply:
  - consensus engine
  - block reader
  - VM configuration and ArbOS hooks
  - debug/trace RPC support via Erigon history readers (no fallback)
  - wasm access adapter
- Erigon builds use an ArbOS processing hook adapter to bridge Erigon EVM/state to Nitro's geth-based ArbOS logic:
  - `arbos.NewTxProcessorIBS` (files `arbos/tx_processor_erigon.go`, `arbos/tx_processor_erigon_state.go`).
  - Converts Erigon `types.Message` to geth `core.Message`, wraps Erigon IBS as a geth `vm.StateDB`,
    and routes WASM execution through ArbOS programs.
  - Uses unsafe access to set geth interpreter `readOnly`; keep it in sync with go-ethereum `EVMInterpreter` layout.

## Recommended Stage Pipeline (Draft)
- Import headers/bodies/TD and set stage progress:
  - `stages.Headers`, `stages.BlockHashes`, `stages.Bodies`
- Run senders recovery:
  - `stagedsync.StageSendersCfg` + `stagedsync.SpawnRecoverSendersStage`
  - advance `stages.Senders` progress to head
  - use standard Erigon sender recovery; keep Arbitrum business logic in execution/ArbOS (not in senders stage)
- Execute blocks with history enabled:
  - `stagedsync.StageExecuteBlocksCfg` + `stagedsync.ExecBlockV3`
  - advance `stages.Execution` progress to head
- Build tx lookup (required for full RPC support):
  - `stagedsync.StageTxLookupCfg` + `stagedsync.SpawnTxLookupStage`
  - advance `stages.TxLookup` and `stages.Finish`

Note: snapshots are optional for migration; use DB tables only to keep the path simple.

## Post-Execution: Copy, Verify, Finalize (Draft)
- Copy `arbitrumdata` and `wasm` after execution completes (ensures replay is stable before metadata copy).
- Run `verifyFull` using the selected mode (`basic|extended|strict`).
- Clear the checkpoint and conversion canary on success.
- Record `copy_done` and `verify_done` checkpoints between these steps for resumability.
- Verification runs after copy and before checkpoint removal.
- Phase artifacts (summary):
  - import: `l2chaindata` MDBX tables populated.
  - senders/execution/txlookup: `l2chaindata` stages and history tables advanced.
  - copy: `arbitrumdata`/`wasm` MDBX buckets populated.
  - verify: read-only checks across all DBs.

Cleanup on success
- Remove checkpoint and canary files after verify passes.

Progress logging (draft)
- Log phase start/end with `phase`, `from`, `to`, and elapsed time.
- Example: `phase=execution from=120000 to=150000 elapsed=12m34s`
- Suggested fields: `phase`, `head`, `from`, `to`, `elapsed`, `rate`.
- Rate can be computed as `(to-from)/elapsed` (blocks/sec).
- On resume, log the `checkpoint_phase` and last imported/executed values.

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

Example log lines (full mode, current format)
```text
phase=init source=/data/chain dest=/data/mdbx mode=full verify=extended verify_samples=20 resume=false start_block=0 end_block=0 workers=8
phase=preflight chain_id=42161 genesis_hash=0x...
phase=bootstrap status=done
phase=import from=0 to=... status=start
phase=import status=progress blocks=... last=...
phase=import status=done blocks=... head=...
phase=senders status=start to=...
phase=senders status=done head=...
phase=execution status=start to=...
phase=execution status=done head=...
phase=txlookup status=start to=...
phase=txlookup status=done head=...
phase=copy dataset=arbitrumdata keys=...
phase=copy dataset=wasm keys=...
phase=verify dataset=l2chaindata section=head_root head=...
phase=verify dataset=l2chaindata section=arbos_slots samples=20 start_block=0 end_block=...
phase=verify dataset=l2chaindata section=chain_config status=ok
phase=verify dataset=arbitrumdata mode=extended keys=...
phase=verify dataset=wasm mode=extended keys=...
phase=done status=ok
```

## Stage Progress Updates (Draft)
- After import: `stages.Headers`, `stages.BlockHashes`, `stages.Bodies` set to the imported head.
- After senders: `stages.Senders` set to the imported head.
- After execution: `stages.Execution` set to the imported head.
- For full RPC parity: `stages.TxLookup` and `stages.Finish` set to the imported head.

Imported head definition
- If `--end-block` is set, use that; otherwise use the source canonical head.

## Stage Config Inputs (Draft)
Senders stage (`StageSendersCfg`)
- `db`, `chainConfig`, `syncCfg`, `tmpdir`, `prune=none`, `blockReader`, `hd=nil`.

Execution stage (`StageExecuteBlocksCfg`)
- `db`, `chainConfig`, `engine`, `vm.Config` (ArbOS hooks), `dirs` (tmp + snap domain),
  `blockReader`, `genesis`, `syncCfg`, `arbitrumWasmDB`, `prune=none`, `batchSize`.
  - `dirs` should be a `datadir.Dirs` with `Tmp` and `SnapDomain` set (snapshots optional, but required field).

Tx lookup stage (`StageTxLookupCfg`, required for full RPC support)
- `db`, `prune=none`, `tmpdir`, `borConfig=nil`, `blockReader`.

History requirement
- The execution stage must run with history enabled to populate ChangeSets and History tables.

## Implementation Task List (File-Level)
Core migration logic
- `cmd/mdbx-migrate/migrate_erigon.go`: implement `--mode=full` with stepwise phases and checkpoints (headers → senders → execution → txlookup → copy).
- `cmd/mdbx-migrate/migrate.go`: extend preflight for disk space and add resume guardrails.
- `cmd/mdbx-migrate/checkpoint.go` (new): JSON checkpoint read/write helpers.

Header/body/receipt import
- `cmd/mdbx-migrate/import_full_erigon.go`: iterate canonical chain and write headers/bodies/TD into MDBX using Erigon rawdb APIs.
- Set `stages.Headers`, `stages.BlockHashes`, `stages.Bodies` progress after import.

Execution + history replay
- `cmd/mdbx-migrate/senders_full_erigon.go`: run senders stage to head.
- `cmd/mdbx-migrate/execute_full_erigon.go`: load genesis/init message and run execution + txlookup stages.
- `cmd/mdbx-migrate/migrate_full_erigon.go`: wire senders/execution/txlookup + checkpoint updates.

Wasm + arbitrumdata copy
- Copy `arbitrumdata` into per-prefix buckets using the prefix-to-bucket mapping; keep keys/values unchanged.
- Ensure all `arb_*` buckets and `arb_wasm` are populated last for easier retry.

Verification
- `cmd/mdbx-migrate/verify_full_erigon.go`: head hash/root, chain config bytes, and sampled ArbOS slot/history checks.
- `cmd/mdbx-migrate/verify_slots_erigon.go`: slot mapping helpers and storage readers.
- Add a final summary line (head hash, root, elapsed time).

## Pseudo-code Flow (Draft)
```
runFullMigration(opts):
  preflight(opts)
  open source pebble dbs (l2chaindata, arbitrumdata, wasm)
  open dest mdbx dbs (l2chaindata, arbitrumdata, wasm)

  if !resume:
    bootstrap_mdbx_chain_db()
    import_headers_bodies_td()
    save_checkpoint("headers_imported")

  if resume and checkpoint < "senders_done":
    run_senders_stage()
    save_checkpoint("senders_done")

  if resume and checkpoint < "execution_done":
    run_execution_stage(history_enabled=true)
    save_checkpoint("execution_done")

  if resume and checkpoint < "txlookup_done":
    run_txlookup_stage()
    save_checkpoint("txlookup_done")

  copy_arbitrumdata_and_wasm()
  save_checkpoint("copy_done")
  verify(opts.verify)
  save_checkpoint("verify_done")
  clear_checkpoint()
```

## Phase Acceptance Criteria
Preflight
- Source has Pebble/LevelDB markers and valid genesis/chain config.
- Destination does not contain MDBX or Pebble markers.

Bootstrap
- Chain config and genesis written to MDBX.
- Stage progress keys initialized at 0.

Header/Body/TD Import
- Canonical headers/bodies/TD present in MDBX for all blocks in range.
- `stages.Headers`, `stages.BlockHashes`, `stages.Bodies` progress == imported head.

Senders Stage
- `stages.Senders` progress == imported head.
- BlockWithSenders reads succeed for sampled blocks.

Execution Stage (History)
- `stages.Execution` progress == imported head.
- State root at head matches source header root.
- Sampled historical state reads succeed via history reader.

Arbitrumdata/Wasm Copy
- Key count matches source (basic) or key/value parity (extended).

Verification
- Head hash and state root match between source and dest.
- Message count and latest metadata keys match (arb data).

## Suggested File Layout and Function Signatures
New files (cmd/mdbx-migrate)
- `checkpoint.go`
  - `func loadCheckpoint(path string) (*Checkpoint, error)`
  - `func saveCheckpoint(path string, c *Checkpoint) error`
  - `func clearCheckpoint(path string) error`
- `import_blocks.go`
  - `func importHeadersBodiesReceipts(ctx context.Context, src ethdb.Database, dst kv.RwDB, start, end uint64) (uint64, error)`
  - `func writeStageProgress(tx kv.RwTx, stage stages.SyncStage, block uint64) error`
- `replay_exec.go`
  - `func runSendersStage(ctx context.Context, cfg stagedsync.SendersCfg, start, end uint64) error`
  - `func runExecutionStage(ctx context.Context, cfg stagedsync.ExecuteBlockCfg, start, end uint64) error`
- `verify_full_erigon.go`
  - `func verifyFull(opts Options) error`
  - `func verifyHeadAndRoot(src ethdb.Database, dst kv.RoDB) (uint64, error)`
  - `func verifyArbosSlots(src ethdb.Database, dst kv.RoDB, head uint64, strict bool) error`
- `verify_slots_erigon.go`
  - `type slotReader interface { StorageAt(blockNum uint64, addr common.Address, slot common.Hash) (common.Hash, error) }`
  - `func readOffsets(reader slotReader, blockNum uint64, refs []slotRef) (map[string]common.Hash, error)`
  - `func readBytes(reader slotReader, blockNum uint64, subspace []byte) ([]byte, error)`

Core entry
- `migrate_erigon.go`
  - `func migrateFull(ctx context.Context, opts Options) error`
  - `func resumeFromCheckpoint(c *Checkpoint) bool`

## Verification Details (Draft)
Head and root
- Read canonical head hash from source (`rawdb.ReadHeadBlockHash`).
- Read header in source and destination for head number; compare hash/root.

Arbitrum metadata
- Use `verifyDatabase` to compare `arbitrumdata`/`wasm` key presence (`basic`) or value equality (`extended`/`strict`).
- Message count and latest message result are covered by value equality in `extended`/`strict`.

History sampling
- Choose deterministic samples (default N=20; override with `--verify-samples`).
- If `--start-block` is set, sample over `[start-block, imported_head]`.
- For each sampled block:
  - Read a small set of known keys (ArbOS metadata, system contract slots).
  - Use geth state reader on source and Erigon history reader on dest.
  - Compare values for equality.

Failure behavior
- Any mismatch fails migration with a clear error and sample context.
- Do not clear the checkpoint on failure; allow `--resume` to retry after fixes.
- Example error: `state root mismatch source=0x... dest=0x...`
- If only verification fails, rerun with `--resume` to avoid redoing replay.
- Resume example: `mdbx-migrate --mode full --resume --verify extended`

## Data Flow Diagram (Text)
```
Pebble/LevelDB (source)
  l2chaindata  -> import headers/bodies/TD -> MDBX l2chaindata
               -> senders stage -> execution stage (history)
  arbitrumdata -> copyDatabase -> MDBX arbitrumdata (per-prefix `arb_*` buckets)
  wasm         -> copyDatabase -> MDBX wasm (arb_wasm bucket)

MDBX (destination)
  l2chaindata: PlainState, History, ChangeSets, Headers, Bodies, Receipts, TxLookup
  arbitrumdata: `arb_messages`, `arb_message_results`, `arb_block_hash_feed`, `arb_block_metadata_feed`,
                `arb_missing_block_metadata`, `arb_delayed_messages_legacy`, `arb_delayed_messages_rlp`,
                `arb_parent_chain_blocks`, `arb_sequencer_batches`, `arb_delayed_sequenced`, `arb_counters`
  wasm: arb_wasm
```

## History Sample Keys (Exact ArbOS Storage Offsets)
ArbOS state is stored in a single storage account, not in the precompile addresses.
- Storage account: `0xA4B05FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF` (see `arbos/storage/storage.go`).
- Precompiles (e.g., 0x64, 0x6c, 0x70) are frontends; they do not hold the state directly.

Logical slot mapping (see `arbos/storage/storage.go`)
- Root key = `util.UintToHash(offset)`
- Subspace key = `keccak(parent.storageKey, subspaceID)`
- Physical slot = `Storage.mapAddress(logicalKey)`

Root offsets (arbosState)
- 0: versionOffset
- 1: upgradeVersionOffset
- 2: upgradeTimestampOffset
- 3: networkFeeAccountOffset
- 4: chainIdOffset
- 5: genesisBlockNumOffset
- 6: infraFeeAccountOffset
- 7: brotliCompressionLevelOffset

Subspaces (arbosState)
- l1PricingSubspace = [0]
- l2PricingSubspace = [1]
- retryablesSubspace = [2]
- addressTableSubspace = [3]
- chainOwnerSubspace = [4]
- sendMerkleSubspace = [5]
- blockhashesSubspace = [6]
- chainConfigSubspace = [7] (bytes)
- pricerSubspace = [8]
- gaslessSubspace = [9]
- subAccountSubspace = [10]
- programsSubspace = [11]
- blacklistSubspace = [12]

L1 pricing offsets (l1pricing subspace)
- 0: payRewardsToOffset
- 1: equilibrationUnitsOffset
- 2: inertiaOffset
- 3: perUnitRewardOffset
- 4: lastUpdateTimeOffset
- 5: fundsDueForRewardsOffset
- 6: unitsSinceOffset
- 7: pricePerUnitOffset
- 8: lastSurplusOffset
- 9: perBatchGasCostOffset
- 10: amortizedCostCapBipsOffset
- 11: l1FeesAvailableOffset

L2 pricing offsets (l2pricing subspace)
- 0: speedLimitPerSecondOffset
- 1: perBlockGasLimitOffset
- 2: baseFeeWeiOffset
- 3: minBaseFeeWeiOffset
- 4: gasBacklogOffset
- 5: pricingInertiaOffset
- 6: backlogToleranceOffset

Retryable offsets (retryables subspace, per retryable ID)
- 0: numTriesOffset
- 1: fromOffset
- 2: toOffset
- 3: callvalueOffset
- 4: beneficiaryOffset
- 5: timeoutOffset
- 6: timeoutWindowsLeftOffset

Suggested sample set (stable keys)
- Root: versionOffset, chainIdOffset, genesisBlockNumOffset, networkFeeAccountOffset, infraFeeAccountOffset.
- L1 pricing: pricePerUnitOffset, perUnitRewardOffset, lastUpdateTimeOffset, l1FeesAvailableOffset.
- L2 pricing: baseFeeWeiOffset, minBaseFeeWeiOffset, gasBacklogOffset, pricingInertiaOffset.
- Chain config bytes: chainConfigSubspace.

Version gating notes
- ArbOS < 2: `networkFeeAccountOffset` is zero; `lastSurplusOffset` may be zero.
- ArbOS < 3: `perBatchGasCostOffset` and `amortizedCostCapBipsOffset` may be zero.
- ArbOS < 10: `l1FeesAvailableOffset` may be zero; skip this slot if you want stricter checks.
- All offsets still exist as storage slots; comparing zeros is acceptable if both sides match.

Sampling algorithm
- For each sampled block number:
  - Read selected logical offsets via `storage.Storage` helpers or ArbOS state accessors.
  - Compare source (geth state reader) vs destination (Erigon history reader).
  - Use deterministic stride across the sampled range (e.g., `head * i / (count-1)`).
  - If `count=1`, sample only the imported head.
- Deterministic sampling improves reproducibility across reruns.

Sampling with `--start-block`
- When `--start-block` is set, sample only from `[start, imported_head]`.

## Appendix: Slot References
- Storage account: `arbos/storage/storage.go:70`
- Slot mapping (`mapAddress`): `arbos/storage/storage.go:107`
- Root offsets: `arbos/arbosState/arbosstate.go:176`
- Subspaces: `arbos/arbosState/arbosstate.go:189`
- L1 pricing offsets: `arbos/l1pricing/l1pricing.go:57`
- L2 pricing offsets: `arbos/l2pricing/l2pricing.go:23`
- Retryable offsets: `arbos/retryables/retryable.go:58`

## Verification Helper (Draft Go)
```go
// slotRef identifies a logical ArbOS storage slot.
type slotRef struct {
	Name     string
	Subspace []byte // nil for root
	Offset   uint64
}

var arbosStorageAccount = common.HexToAddress("0xA4B05FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF")

func storageKeyForSubspace(id []byte) []byte {
	if len(id) == 0 {
		return nil
	}
	return crypto.Keccak256(id)
}

func mapAddress(storageKey []byte, key common.Hash) common.Hash {
	keyBytes := key.Bytes()
	boundary := common.HashLength - 1
	prefix := crypto.Keccak256(storageKey, keyBytes[:boundary])
	mapped := append(prefix[:boundary], keyBytes[boundary])
	return common.BytesToHash(mapped)
}

func slotForOffset(storageKey []byte, offset uint64) common.Hash {
	return mapAddress(storageKey, util.UintToHash(offset))
}

type slotReader interface {
	StorageAt(blockNum uint64, addr common.Address, slot common.Hash) (common.Hash, error)
}

func readOffsets(reader slotReader, blockNum uint64, refs []slotRef) (map[string]common.Hash, error) {
	out := make(map[string]common.Hash, len(refs))
	for _, ref := range refs {
		subspaceKey := storageKeyForSubspace(ref.Subspace)
		slot := slotForOffset(subspaceKey, ref.Offset)
		val, err := reader.StorageAt(blockNum, arbosStorageAccount, slot)
		if err != nil {
			return nil, err
		}
		out[ref.Name] = val
	}
	return out, nil
}

// StorageBackedBytes layout: length at offset 0, then 32-byte chunks at 1..N.
func readBytes(reader slotReader, blockNum uint64, subspace []byte) ([]byte, error) {
	sizeSlot := slotForOffset(storageKeyForSubspace(subspace), 0)
	sizeHash, err := reader.StorageAt(blockNum, arbosStorageAccount, sizeSlot)
	if err != nil {
		return nil, err
	}
	size := sizeHash.Big().Uint64()
	out := make([]byte, 0, size)
	bytesLeft := size
	offset := uint64(1)
	for bytesLeft >= 32 {
		slot := slotForOffset(storageKeyForSubspace(subspace), offset)
		val, err := reader.StorageAt(blockNum, arbosStorageAccount, slot)
		if err != nil {
			return nil, err
		}
		out = append(out, val.Bytes()...)
		bytesLeft -= 32
		offset++
	}
	if bytesLeft > 0 {
		slot := slotForOffset(storageKeyForSubspace(subspace), offset)
		val, err := reader.StorageAt(blockNum, arbosStorageAccount, slot)
		if err != nil {
			return nil, err
		}
		out = append(out, val.Bytes()[32-bytesLeft:]...)
	}
	return out, nil
}
```

## SlotReader Implementations (Draft)
Source (Pebble/geth)
```go
type gethSlotReader struct {
	db ethdb.Database
}

func (r *gethSlotReader) StorageAt(blockNum uint64, addr common.Address, slot common.Hash) (common.Hash, error) {
	hash := rawdb.ReadCanonicalHash(r.db, blockNum)
	header := rawdb.ReadHeader(r.db, hash, blockNum)
	if header == nil {
		return common.Hash{}, fmt.Errorf("missing header %d", blockNum)
	}
	scheme, err := rawdb.ParseStateScheme("", r.db)
	if err != nil {
		return common.Hash{}, err
	}
	trieCfg := &triedb.Config{Preimages: false}
	if scheme == rawdb.HashScheme {
		trieCfg.HashDB = hashdb.Defaults
	} else {
		trieCfg.PathDB = pathdb.Defaults
	}
	stateDb := state.NewDatabaseWithConfig(r.db, trieCfg)
	statedb, err := state.New(header.Root, stateDb, nil)
	if err != nil {
		return common.Hash{}, err
	}
	return statedb.GetState(addr, slot), nil
}
```

Destination (Erigon MDBX)
```go
type erigonSlotReader struct {
	tx          kv.TemporalTx
	txNumReader rawdbv3.TxNumsReader
}

func (r *erigonSlotReader) StorageAt(blockNum uint64, addr common.Address, slot common.Hash) (common.Hash, error) {
	reader, err := rpchelper.CreateHistoryStateReader(r.tx, blockNum+1, 0, r.txNumReader)
	if err != nil {
		return common.Hash{}, err
	}
	// Convert go-ethereum address/hash to Erigon types if needed.
	eaddr := ecommon.BytesToAddress(addr.Bytes())
	eslot := ecommon.BytesToHash(slot.Bytes())
	val, ok, err := reader.ReadAccountStorage(eaddr, eslot)
	if err != nil {
		return common.Hash{}, err
	}
	if !ok {
		return common.Hash{}, nil
	}
	return common.BytesToHash(val.Bytes()), nil
}
```

Notes
- Use `rawdb.ParseStateScheme("", db)` to honor stored state scheme (hash vs path).
- For Erigon history reads, `blockNum` is interpreted as “block number + 1” in `CreateHistoryStateReader`, consistent with Erigon RPC helpers.

## Verification Harness Outline (cmd/mdbx-migrate/verify_full_erigon.go)
```go
func verifyArbosSlots(source ethdb.Database, dest kv.RoDB, head uint64, strict bool) error {
	// Source slot reader.
	src := &gethSlotReader{db: source}

	// Destination slot reader.
	tx, err := dest.BeginRo(context.Background())
	if err != nil {
		return err
	}
	defer tx.Rollback()
	ttx, ok := tx.(kv.TemporalTx)
	if !ok {
		return fmt.Errorf("destination tx is not temporal")
	}
	dst := &erigonSlotReader{
		tx:          ttx,
		txNumReader: rawdbv3.TxNums,
	}

	refs := []slotRef{
		{Name: "arbosVersion", Offset: 0},
		{Name: "chainId", Offset: 4},
		{Name: "genesisBlockNum", Offset: 5},
		{Name: "networkFeeAccount", Offset: 3},
		{Name: "infraFeeAccount", Offset: 6},
		{Name: "l1PricePerUnit", Subspace: []byte{0}, Offset: 7},
		{Name: "l1FeesAvailable", Subspace: []byte{0}, Offset: 11},
		{Name: "l2BaseFee", Subspace: []byte{1}, Offset: 2},
		{Name: "l2GasBacklog", Subspace: []byte{1}, Offset: 4},
	}

	for _, blockNum := range sampleBlocks(head, 20) {
		left, err := readOffsets(src, blockNum, refs)
		if err != nil {
			return fmt.Errorf("source read block %d: %w", blockNum, err)
		}
		right, err := readOffsets(dst, blockNum, refs)
		if err != nil {
			return fmt.Errorf("dest read block %d: %w", blockNum, err)
		}
		leftVersion := left["arbosVersion"].Big().Uint64()
		rightVersion := right["arbosVersion"].Big().Uint64()
		if leftVersion != rightVersion {
			return fmt.Errorf("arbos version mismatch at block %d", blockNum)
		}
		if leftVersion < 10 && !strict {
			delete(left, "l1FeesAvailable")
			delete(right, "l1FeesAvailable")
		}
		if err := compareSlotMaps(blockNum, left, right); err != nil {
			return err
		}
	}
	return nil
}
```

Notes
- `compareSlotMaps` should compare `common.Hash` values per key.
- For chain config bytes, use `readBytes` with subspace `{7}` and compare equality.
## Decisions (Resolved)
- Decision: reuse Erigon's full staged-sync pipeline for full migration; ensure the final state matches the Erigon RPC state view (head hash/root + sampled history reads).
- Decision: full RPC support requires stages `Headers`, `BlockHashes`, `Bodies`, `Senders`, `Execution` (history enabled), `TxLookup`, and `Finish`.
- Decision: use `SpawnRecoverSendersStage` for L2 blocks; do not embed Arbitrum business logic into sender recovery.
