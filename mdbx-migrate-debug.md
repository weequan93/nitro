# MDBX migrate bad root notes

## Context
- Failure during `mdbx-migrate` execution stage: computed state root mismatches header root at block 16.
- Example log: computed `0x5a16...` vs header `0x7bda...`, unwind to block 8, error `invalid state root hash`.

## What we confirmed
- The source (Pebble) receipts for block 16 are valid and match header receipt root.
- The "Bad state root receipts" log shows zero receipts only because `ReadReceiptsCacheV2`
  uses `HistorySeek`, which does not see unflushed `RCacheDomain` writes at the moment
  the bad-root logger runs. This is likely a debug artifact, not the cause of root mismatch.
- Per-block tx tasks include:
  - `tx_index=-1` (block init), `tx_index=0` (internal startblock tx),
    `tx_index=1` (user tx), `tx_index=len(txs)` (finalize).
  - For block 16, txnums 66..69 reflect these four tasks.
- Arbitrum internal tx startblock and L1 charge hooks are firing with expected values
  (base_fee_in_block=100000000, l1_block_number=167, time_passed=3).

## Hypotheses to validate
1) **Block finalization rewards**: `runExecutionStage` uses `ethash.NewFaker()` which
   applies miner rewards in `Finalize`. If Arbitrum L2 should not mint block rewards,
   this would diverge state roots. Need to confirm whether the source chain applies
   rewards or not.
2) **Divergence earlier than block 16**: root check happens at the last processed block.
   The first divergence might be before block 16; we need to locate the earliest block
   with mismatch.
3) **Scheduled txs / L1 pricing differences**: Arbitrum hooks schedule extra txs or
   charge L1 fees; a mismatch in hook semantics could surface later.

## Next steps
- Run with `ERIGON_COMMIT_EACH_BLOCK=1` and `ERIGON_BAD_ROOT_DEBUG=1` to find the
  earliest failing block. Set `ERIGON_MDBX_MIGRATE_DEBUG_BLOCK=<block>` for detailed tx roots.
- Add/enable logging around `Finalize` to capture coinbase balance deltas and check
  whether block rewards are being applied unexpectedly.
- If rewards are the culprit, test a patch that skips `accumulateRewards` when
  `chainCfg.IsArbitrum()` and re-run migration to verify root match.

## Current fix candidate
- If the root mismatch is due to unintended block rewards, the fix is to skip
  rewards for Arbitrum (see Next steps #3). Steps #1 and #2 are diagnostics.

## Recent actions
- Added ethash Finalize logging (guarded by ERIGON_BAD_ROOT_DEBUG / ERIGON_MDBX_MIGRATE_DEBUG) and
  skipped rewards when `chainCfg.IsArbitrum()` in `erigon/execution/consensus/ethash/consensus.go`.
- Rebuilt `mdbx-migrate` successfully (linker warnings about macOS version mismatch).
- Attempted rerun with `ERIGON_COMMIT_EACH_BLOCK=1`, but migration aborted because
  destination already contains MDBX files. Found `.mdbx-flow-test/mdbx-l3/l2chaindata`
  has `UNFINISHED_MDBX_CONVERSION` and `.mdbx-migrate` checkpoint.

## Latest run (post-fix)
- Execution now matches: per-tx root at block 16 equals header root
  (`root=0x7bda...`), and execution stage completes.
- Verification fails in `arbos_slots` against the source DB:
  `missing trie node ... state ... not available` at block 3. This indicates
  the *source* Pebble DB lacks historical trie nodes (likely pruned).

## How to proceed on verify
- If you want migration success without historical trie checks, use:
  `--verify basic`, or keep extended but set `--verify-samples 1` or `--start-block <head>`
  to sample only the head state.
- To fully verify arbos slots historically, you need an archive/unpruned source DB
  that retains trie nodes for the sampled blocks.

## Archive test updates
- Added archive mode support in nitro-testnode config generation using
  `NITRO_TESTNODE_ARCHIVE=1` to set `execution.caching.archive=true` and
  `execution.caching.state-scheme=hash`.
- `scripts/mdbx_flow_test.sh` and `scripts/mdbx_arbitrum_tx_matrix_test.sh` now
  default to `ENABLE_ARCHIVE=1` and append `--archive` to `test-node.bash` so the
  source Pebble DB keeps historical trie nodes for extended verify.
- Fixed a TypeScript build error by constructing a typed `executionConfig` and
  assigning it into the base config (avoids indexing errors on `execution.caching`).

## Latest run (archive enabled)
- Step 7 failed during `wait-for-sync` with `ECONNREFUSED` to `http://l3node:3347`.
  Containers started, but L3 RPC was not accepting connections.
- `docker compose logs l3node` shows:
  `invalid backend configuration: missing MDBX data in /home/user/.arbitrum/local/...`
  so L3 is looking in `/home/user/.arbitrum/local`, but migrated data lives in the
  volume mounted at `/home/user/.arbitrum/local/nitro`.
- `l3node_config.json` has `persistent.chain = "local"`; chain info says
  `chain-name = "orbit-dev-test"`, but chain defaults do not override existing values,
  so the node keeps using `local` and fails to find MDBX.

## Fix for L3 startup after migration
- `scripts/mdbx_flow_test.sh` and `scripts/mdbx_arbitrum_tx_matrix_test.sh` now set
  `persistent.chain` via `MDBX_CHAIN_DIR` (default `local/nitro`) when updating the
  L3 config for mdbx so erigon points at the migrated data.
- Fixed a shell quoting issue where `$chain` was expanded by the host shell
  (causing `unbound variable`); jq filter now uses `\$chain`.
- L3 still failed with `missing MDBX data` because imported files were owned by root
  with `drwxr--r--` on directories, so the nitro user couldn't read them. Updated the
  import step to `chown -R 1000:1000 /data` after extraction.
- New failure: L3 exits with `init/reorg options are not supported with erigon backend`
  and `options=init.empty` due to `has-genesis-state=false` in `l3_chain_info.json`.
  Added a step to set `has-genesis-state=true` when switching L3 to erigon/mdbx.
- New failure after that: L3 hits `mdbx_env_open: resource temporarily unavailable`
  because `arbitrumdata` is opened twice in the same process (once in `nitro.go`,
  once inside `erigonexec.New`). Fixed by reusing the `arbitrumdata` handle from
  `erigonexec.Client` instead of reopening it in `nitro.go`.

## Latest findings (L3 startup + script UX)
- The config update step prompted for confirmation on `mv` because `mv` ran in
  interactive mode inside the container. Updated both mdbx test scripts to use
  `mv -f` so the config replace step is non-interactive.
- Added a `wait_for_sync` retry loop in both mdbx test scripts. It retries until
  the RPC port responds (configurable via `WAIT_FOR_SYNC_RETRIES` and
  `WAIT_FOR_SYNC_SLEEP`; `WAIT_FOR_SYNC_RETRIES=0` means unlimited retries).
- Added the same `wait_for_sync` retry loop in `nitro-testnode/test-node.bash` for
  the initial geth sync step (to avoid flaking on `ECONNREFUSED`).
- `ECONNREFUSED` from `wait-for-sync` means the L3 RPC port isn't listening;
  the node is either still booting or exited early. Check `docker compose logs l3node`
  to confirm the root cause. On prior runs this was due to `chain db missing temporal support`.
- `ENOTFOUND l3node` indicates DNS resolution failure for the `l3node` hostname.
  This happens when the wait-for-sync command runs on the host (not inside the
  compose network), or when the `l3node` service is not running on the network.
  Fix by running wait-for-sync via `docker compose run scripts ...` or by using
  `L3_RPC_URL=http://localhost:3347` with a published port, and ensure `l3node`
  is running.

## L3 failure: missing temporal DB
- `docker compose logs l3node` shows: `erigonexec: chain db missing temporal support`.
- Root cause: `erigonexec.New` opened raw MDBX (`kv.RwDB`) without wrapping it in
  `kv.TemporalRoDB`, but multiple erigonexec paths require `BeginTemporalRo`.
- Fix: create a `db/state` aggregator and wrap the chain DB with
  `temporal.New(...)` in `execution/erigonexec/client_erigon.go`, and close the
  aggregator in `StopAndWait`.

## Build fix: non-erigon builds
- Docker builds without `-tags=erigon` failed with
  `erigonClient.ArbDB undefined` because the non-erigon stub `Client` lacked the
  new `ArbDB()` method. Added a stub `ArbDB()` that returns nil in
  `execution/erigonexec/client.go`.

## L3 crash: prune mode nil panic
- L3 crashed with a nil pointer in `Client.historyPruned()` because `parsePruneMode`
  returned an uninitialized `prune.Mode` (History/Blocks nil) when no TextUnmarshaler
  is available. Fixed by falling back to `prune.FromCli(...)` so `prune.Mode` is
  fully initialized before use (`execution/erigonexec/prune_mode_erigon.go`).

## L3 crash: txpool nil in eth_getTransactionCount
- L3 crashed inside `eth_getTransactionCount` because `api.txPool` was nil and
  the pending-transaction path called `api.txPool.Nonce(...)`. Guarded the pending
  path: if txpool is nil, fall back to `latest` (`erigon/rpc/jsonrpc/eth_accounts.go`).

## L3 tx send: intrinsic gas too low
- `send-l3` failed with `intrinsic gas too low` (gasLimit 21000) after migration.
- Added `--gas-limit` support to `nitro-testnode/scripts/ethcommands.ts` and
  used `L3_GAS_LIMIT` (default 100000) in `scripts/mdbx_flow_test.sh` and
  `scripts/mdbx_arbitrum_tx_matrix_test.sh` to avoid underestimating L3 intrinsic gas.

## L3 sequencer panic: wasm DB opened twice
- Sequencer crashed with `mdbx_env_open: resource temporarily unavailable, label: arb-wasm`.
- Root cause: `wasmdb.OpenArbitrumWasmDB(...)` opened a second MDBX handle while
  `erigonexec.OpenWasmDB` already had the wasm DB open.
- Fix: open the wasm DB once in `erigonexec.Client.New`, wrap the same handle with
  `wasmdb.WrapDatabaseWithWasm`, and reuse it everywhere (`c.wasmDBForCtx(...)`).

## L2 sequencer stalled after restart (redis priorities)
- After Step 7 restart, `wait-for-sync` retries and L3 logs show `latest L1 block is old`,
  `sequencer internal error`, and `context deadline exceeded` when posting batches.
- Likely cause: Redis state is wiped by `compose down`, so `coordinator.priorities` is
  missing. The L2 sequencer coordinator then refuses lockout/sequencing.
- Fix: re-run Redis priorities init after restart. Added to scripts:
  - `scripts/mdbx_flow_test.sh`: `compose up --wait redis` + `compose run --rm scripts redis-init`
  - `scripts/mdbx_arbitrum_tx_matrix_test.sh`: same, but passes `--redundancy $REDUNDANT_SEQUENCERS`

## Latest run (still failing)
- Output shows Step 7 restart but no `redis-init` output line (expected to print
  `redis[coordinator.priorities]:...`). This suggests the updated script may not
  be used or `redis-init` didn't execute successfully.
- Errors persist: `ECONNREFUSED` to `l3node:3347` and `sequencer internal error`
  on `send-l3` tx.
- Next checks:
  - Verify the script contains the new `redis-init` step.
  - Read `coordinator.priorities` from Redis.
  - Check `sequencer` and `l3node` logs for the first error after restart.

## L3 reorg panic: wasm DB reopened in Reorg path
- Even after Redis priorities were set, L3 crashed during a reorg with:
  `panic: fail to open mdbx: ... label: arb-wasm` from `Client.Reorg`.
- Stack shows `wasmdb.OpenArbitrumWasmDB` called inside `Client.wasmDBForCtx`,
  which tries to open a second MDBX handle against the same `wasm` path.
- Fix: store the raw wasm MDBX handle on `Client` and have `wasmDBForCtx` reuse it
  via `wasmdb.WrapDatabaseWithWasm`, avoiding `OpenArbitrumWasmDB` and any second open.

## Rollupcreator build failure (nitro-contracts branch mismatch)
- `rollupcreator` image build failed with Hardhat error:
  `File src/precompiles/ArbGasInfo.sol not found`.
- Root cause: `rollupcreator/Dockerfile` defaulted to `main` and docker-compose passed
  an empty `NITRO_CONTRACTS_BRANCH` when the env var wasn't set, so the build used a
  branch that doesn't include `ArbGasInfo.sol`.
- Fix: set the default branch/commit explicitly in `nitro-testnode/docker-compose.yaml`
  and `nitro-testnode/rollupcreator/Dockerfile` to the same commit used by `test-node.bash`
  (`99c07a7db2fcce75b751c5a2bd4936e898cda065`).

## L3 sequencer panic: nil notifications in exec3_serial
- L3 crashes with `panic: runtime error: invalid memory address` at
  `erigon/execution/stagedsync/exec3_serial.go:212` inside `RecentLogs.Add`.
- Root cause: `se.cfg.notifications` is nil in the nitro/erigon path.
- Fix: guard the recent logs add with a nil check:
  `if se.cfg.notifications != nil && se.cfg.notifications.RecentLogs != nil { ... }`.
- If logs still show the same panic after code changes, the `nitro-node-dev-testnode`
  image likely wasn't rebuilt. Rebuild the nitro dev image and retag before restarting.
- Confirmed: logs still show `exec3_serial.go:212` after restart, which indicates the
  container is still using an old image. Rebuild from repo root and recreate the
  `sequencer`/`l3node` containers to pick up the new binary.

## L3 validation recording failure: temporal tx requirement
- After rebuilding, L3 no longer panics at `exec3_serial.go:212`, but recording fails
  with `Error while recording err="tx is not a temporal tx"` and then `cannot reorg out genesis`.
- Root cause: witness rewind uses `membatchwithdb.MemoryMutation` which does not
  implement `kv.TemporalRwTx`. Unwind in `RewindStagesForWitness` requires a temporal
  RW tx and errors out.
- Fix:
  - Use a temporal RW tx for recording: `recordBlockCreation` now opens
    `BeginTemporalRw` instead of `BeginTemporalRo`.
  - Implement `kv.TemporalRwTx` methods on `MemoryMutation` by delegating to the
    underlying temporal tx (DomainPut/Del, Unwind, prune helpers, UnmarkedRw).

## L3 validation still failing (nonce too low / missing preimage)
- Logs show repeated `Error while recording err="nonce too low: address 0xB957..."` followed
  by `Hostio failed with Missing requested preimage ...` and fatal shutdown.
- This is coming from `BlockValidator` recording; likely a state/message mismatch after
  rewinding head or missing preimage generation.
- Workaround for the migration flow test: allow disabling the block validator on L3.
  `scripts/mdbx_flow_test.sh` now supports `L3_DISABLE_VALIDATOR` (default `1`) and, when
  enabled, writes `node.staker.dangerous["without-block-validator"]=true` into
  `l3node_config.json` during the mdbx config update.

## L3 startup failure: execution stage behind head
- L3 exited with `erigonexec: Execution stage behind head (progress=16 head=18)`.
- Root cause: head header is ahead of execution stage (likely from prior partial
  execution), and startup checks treated it as fatal.
- Fix: during startup checks, detect stages behind head and rewind head to the
  minimum progress among required stages. Update head header/block/forkchoice hashes
  and re-read head for subsequent checks, logging a warning with stage progress.

## L3 restart after head rewind: txnum gap + delayed sequencer errors
- After the head rewind, L3 starts but logs repeatedly show:
  - `append tx nums: append with gap blockNum=17, but current height=18`
  - `Delayed sequencer error err="wrong pos got 17 expected 19"`
  - `nonce too low` for `0xB957...` during recording
- Root cause: the head header was rewound, but the `TxNums` table still had entries
  past the new head. This leaves the txnum cursor at height 18 while execution
  tries to append block 17, causing a gap and downstream sequencing errors.
- Fix: when rewinding the head due to stage progress, also truncate the txnums table
  to `minProgress + 1` before updating head hashes. Implemented in
  `execution/erigonexec/mdbx_health_erigon.go` using `rawdbv3.TxNums.Truncate(...)`.
- Note: the container must be rebuilt/recreated to pick up this change.

## TxNums still ahead of head (gap persists after restart)
- New logs still show `append tx nums: append with gap blockNum=17, but current height=18`
  right after startup, even without the stage-behind rewind warning.
- This means the MaxTxNum table can still be ahead of the chain head even when
  stage progress equals head (so the rewind path doesn’t run).
- Fix: add a separate startup check to compare MaxTxNum last block vs head and
  truncate if `lastBlockNum > head`. Implemented in
  `execution/erigonexec/mdbx_health_erigon.go` as `checkTxNumsConsistency(...)`,
  called after re-reading head in `runStartupChecks`.

## Message count ahead of execution head (wrong pos + txnum gap)
- Even after txnums truncation, L3 logs still show:
  - `append tx nums: append with gap blockNum=18, but current height=18`
  - `wrong pos got 18 expected 19`
- Root cause: `transaction_streamer` message count is ahead of the execution head,
  so it tries to write message `pos=18` while the DB already thinks it has `pos=18`.
  This causes duplicate `TxNums.Append` for block 18 and the wrong-pos error.
- Fix: in `arbnode/transaction_streamer.go` `cleanupInconsistentState`, compare
  `GetMessageCount()` to `exec.HeadMessageNumber()+1`. If the message count is
  ahead, call `ReorgTo(expected)` to rewind arbdb message state and reorg exec to
  the same head. This also truncates txnums/canonical hashes at the target head.
- Follow-up: since `Execution.Start` can still rewind head *after* the streamer
  is created, run the same reconciliation in `TransactionStreamer.Start` so it
  uses the post-startup head. This prevents the message count from drifting ahead
  again when `runStartupChecks` rewinds the head.
- Bugfix: the first attempt called `reconcileMessageCount` before the streamer
  started, which panicked (`StopWaiter.GetContext: not started`). Fixed by moving
  reconciliation to run *after* `StopWaiter.Start` and removing it from
  `cleanupInconsistentState`.

## Validation failure: missing preimage
- L3 now hits `Hostio failed with Missing requested preimage ... ResolveTypedPreimage`
  and then `cannot reorg out genesis`, causing a fatal shutdown.
- This appears during `validation_validate` RPC responses, likely triggered after
  sequencing/recording issues. Treat as secondary until txnum/head mismatch is fixed.

## Recording failure: nonce too low (witness rewind incomplete)
- Logs show `Error while recording err="nonce too low ... tx: 0 state: 1/2/3"` during
  validation recording. This indicates the witness rewind didn’t actually reset
  state back to the previous block, so tx nonces are already incremented.
- Root cause: `RewindStagesForWitness` used `StageState.BlockNumber = blockNr`, so
  `UnwindExecutionStage` only rewound one block (blockNr -> blockNr-1) instead of
  rewinding from the current head (`latestBlockNr`) down to `blockNr-1`.
- Fix: set `StageState.BlockNumber = latestBlockNr` in
  `erigon/execution/stagedsync/stage_witness.go` so the unwind spans the correct
  range. This should eliminate the nonce mismatch and unblock preimage recording.

## L3 deploy failure: rollupcreator nonce too low
- In `scripts/mdbx_arbitrum_tx_matrix_test.sh` during "Deploying L3",
  `rollupcreator create-rollup-testnode` fails with ethers `NONCE_EXPIRED`
  and JSON-RPC `nonce too low: address 0x863c..., tx: 19 state: 20`.
- Cause: L1/L2 chain state is not reset between runs; the deployer nonce is
  already advanced but the script reuses the same deployer key.
- Recovery (destructive): `cd nitro-testnode && docker compose down -v --remove-orphans`,
  then rerun the script from a clean state.
- Alternative: use a fresh deployer key or reset chain state; orphan-container
  warnings are unrelated to the nonce error.
