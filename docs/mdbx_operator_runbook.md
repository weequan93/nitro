# MDBX Operator Runbook

## Scope
- Offline migration from Pebble to MDBX for Nitro with the Erigon backend.
- Full-history replay is required for debug/trace parity.

## Quickstart (Operational Commands)
### 0) Gather inputs
- `SOURCE_DIR`: existing Pebble datadir (parent that contains `l2chaindata/`, `arbitrumdata/`, `wasm/`).
- `DEST_DIR`: empty directory for MDBX output.
- `CONFIG`: your Nitro config file path (or CLI flags you already use).
- `L1_RPC`: your L1 RPC endpoint (and JWT path if required by your setup).

### 1) Stop the node and back up data
```bash
# Stop Nitro by your normal process, then verify it is not running.
ps aux | rg nitro || true

# Backup source data (example path; adjust for your setup).
cp -a "$SOURCE_DIR" "${SOURCE_DIR}.backup"
```

### 2) Build the migration tool (if not already built)
```bash
go build -tags erigon -o mdbx-migrate ./cmd/mdbx-migrate
```

### 3) Run full migration with verification
```bash
./mdbx-migrate \
  --source "$SOURCE_DIR" \
  --dest "$DEST_DIR" \
  --mode full \
  --verify extended \
  --verify-samples 20
```

### 4) Resume if interrupted
```bash
./mdbx-migrate \
  --dest "$DEST_DIR" \
  --mode full \
  --resume \
  --verify extended \
  --verify-samples 20
```

### 5) Start Nitro on MDBX/Erigon
Use your existing start command and add or update these flags:
```text
--persistent.db-engine=mdbx
--execution.backend=erigon
```
If you prefer auto-detect:
```text
--persistent.db-engine=auto
--execution.backend=auto
```
Point the datadir to `DEST_DIR` and keep your normal L1 RPC and auth/JWT settings.

### 6) Basic parity checks (example)
```bash
curl -s "$L2_RPC" -H 'content-type: application/json' \
  --data '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}'

curl -s "$L2_RPC" -H 'content-type: application/json' \
  --data '{"jsonrpc":"2.0","method":"eth_getBlockByNumber","params":["latest",false],"id":2}'
```

## Role-specific configs and commands (Erigon + MDBX)
### Common base (all roles)
Add these flags to your existing command or config:
```text
--execution.backend=erigon
--persistent.db-engine=mdbx
--persistent.chain=/path/to/datadir
--execution.erigon.prune-mode=archive
```
Notes:
- The Erigon backend requires existing MDBX data for `l2chaindata`, `arbitrumdata`, and `wasm`.
- Non-sequencer nodes must set `execution.forwarding-target` to the sequencer URL or `"null"` to disable tx forwarding.
- Set `execution.erigon.prune-mode` to `archive` for full historical RPC support; `full|minimal|blocks` will prune history and return "historical data pruned" errors for old blocks.

### 1) Sequencer
Minimum config deltas:
```text
--node.sequencer
--execution.sequencer.enable
--node.feed.output.enable=true
--node.feed.output.addr=0.0.0.0
--node.feed.output.port=9642
--node.seq-coordinator.enable=true            # if using redis coordinator
--node.seq-coordinator.my-url=ws://<host>:9642/feed
--node.seq-coordinator.redis-url=redis://<host>:6379/0
--node.batch-poster.enable=true               # if this node posts batches
--node.batch-poster.parent-chain-wallet.pathname=/path/to/keystore
--node.batch-poster.parent-chain-wallet.password=<pass>
--parent-chain.connection.url=<L1_RPC>
```
Example command (trim to your existing flags):
```bash
./nitro \
  --persistent.chain=/path/to/mdbx/datadir \
  --execution.backend=erigon \
  --persistent.db-engine=mdbx \
  --node.sequencer \
  --execution.sequencer.enable \
  --node.feed.output.enable=true \
  --node.feed.output.addr=0.0.0.0 \
  --node.feed.output.port=9642 \
  --node.seq-coordinator.enable=true \
  --node.seq-coordinator.my-url=ws://<host>:9642/feed \
  --node.seq-coordinator.redis-url=redis://<host>:6379/0 \
  --node.batch-poster.enable=true \
  --node.batch-poster.parent-chain-wallet.pathname=/path/to/keystore \
  --node.batch-poster.parent-chain-wallet.password=<pass> \
  --parent-chain.connection.url=<L1_RPC>
```
Conditions:
- Erigon does not support timeboost/express-lane; keep `execution.sequencer.dangerous.timeboost.enable=false`.
- If you run a dedicated batch-poster, disable `node.batch-poster.enable` here and run it elsewhere.

### 2) Validator (staker)
Minimum config deltas:
```text
--node.sequencer=false
--execution.sequencer.enable=false
--node.staker.enable=true
--node.staker.strategy=watchtower|defensive|stakeLatest|makeNodes
--node.staker.parent-chain-wallet.pathname=/path/to/keystore
--node.staker.parent-chain-wallet.password=<pass>
--parent-chain.connection.url=<L1_RPC>
--execution.forwarding-target="null"
```
Example command:
```bash
./nitro \
  --persistent.chain=/path/to/mdbx/datadir \
  --execution.backend=erigon \
  --persistent.db-engine=mdbx \
  --node.sequencer=false \
  --execution.sequencer.enable=false \
  --node.staker.enable=true \
  --node.staker.strategy=watchtower \
  --node.staker.parent-chain-wallet.pathname=/path/to/keystore \
  --node.staker.parent-chain-wallet.password=<pass> \
  --parent-chain.connection.url=<L1_RPC> \
  --execution.forwarding-target="null"
```
Conditions:
- For watchtower mode, no L1 tx posting happens unless `enable-fast-confirmation` is set.
- If you use a smart contract wallet, set `node.staker.use-smart-contract-wallet=true` and `node.staker.contract-wallet-address`.

### 3) Archival node (full history RPC)
Minimum config deltas:
```text
--node.sequencer=false
--execution.sequencer.enable=false
--execution.tx-lookup-limit=0
--node.message-pruner.enable=false
--execution.erigon.prune-mode=archive
--execution.forwarding-target="null"
```
Example command:
```bash
./nitro \
  --persistent.chain=/path/to/mdbx/datadir \
  --execution.backend=erigon \
  --persistent.db-engine=mdbx \
  --node.sequencer=false \
  --execution.sequencer.enable=false \
  --execution.tx-lookup-limit=0 \
  --node.message-pruner.enable=false \
  --execution.forwarding-target="null"
```
Conditions:
- Use full migration (`--mode full`) so history tables are populated for debug/trace.
- Consider disabling `node.feed.input` if you only need archival RPC and prefer L1-driven sync.

### 4) Forwarder node (tx forwarder)
Minimum config deltas:
```text
--node.sequencer=false
--execution.sequencer.enable=false
--execution.forwarding-target=http://<sequencer-rpc>
--execution.forwarder.redis-url=redis://<host>:6379/0     # optional, for dynamic target
--execution.forwarder.priority-node=true                   # optional, send priority txs
```
Example command:
```bash
./nitro \
  --persistent.chain=/path/to/mdbx/datadir \
  --execution.backend=erigon \
  --persistent.db-engine=mdbx \
  --node.sequencer=false \
  --execution.sequencer.enable=false \
  --execution.forwarding-target=http://<sequencer-rpc> \
  --execution.forwarder.redis-url=redis://<host>:6379/0
```
Conditions:
- Forwarder nodes are read+forward. They still need a fully migrated MDBX datadir.
- If you use redis-based coordination for sequencer selection, keep `execution.forwarding-target` set and let redis override when available.

## Migration by role
### 1) Sequencer migration
- Schedule downtime; stop sequencer + batch-poster so no L1 batches are created during migration.
- Run full migration to a new MDBX datadir.
- Switch `persistent.chain` to the MDBX datadir and start with `execution.backend=erigon`.

### 2) Validator migration
- Stop the validator/staker and ensure no L1 staking txs are pending.
- Run full migration to a new MDBX datadir.
- Start with `execution.backend=erigon` and `execution.forwarding-target="null"`.

### 3) Archival migration
- Stop the archival node and migrate to MDBX.
- Start with `execution.tx-lookup-limit=0` and `node.message-pruner.enable=false`.

### 4) Forwarder migration
- Stop the forwarder node and migrate to MDBX.
- Start with forwarding target or redis set.

## Test guide (end-to-end flow)
This uses the local `nitro-testnode` docker setup and the helper script `scripts/mdbx_flow_test.sh`.

### Prerequisites
- Docker + Docker Compose
- Go toolchain 1.24+ (used to build `mdbx-migrate`)
- Disk space for two full datadirs (Pebble + MDBX)

### What the script does (L3)
1) Starts a fresh local test chain with `--l3node` (L3 on top of L2).
2) Sends an L3 transaction via the test scripts.
3) Captures a block number + balance at that block for historical verification.
4) Stops containers and exports the L3 Pebble datadir to the host.
5) Runs `mdbx-migrate --mode full` with verification.
6) Imports MDBX data back into the L3 volume.
7) Updates `l3node_config.json` to `execution.backend=erigon` + `persistent.db-engine=mdbx`.
8) Restarts the L3 node and sends another L3 tx.
9) Re-queries the historical balance to confirm parity after migration.

### Run it
```bash
chmod +x scripts/mdbx_flow_test.sh
./scripts/mdbx_flow_test.sh
```

### Common overrides
```bash
# Avoid rebuilding images if already built
TESTNODE_INIT_ARGS="--init-force --dev --detach --no-build" ./scripts/mdbx_flow_test.sh

# Set a custom L3 RPC target (default is http://l3node:3347)
L3_RPC_URL="http://127.0.0.1:3347" ./scripts/mdbx_flow_test.sh

# Use a custom output directory for test artifacts
TEST_ROOT="$PWD/.mdbx-flow-test" ./scripts/mdbx_flow_test.sh

# Use a custom mdbx-migrate binary location
MDBX_MIGRATE_BIN="$PWD/target/bin/mdbx-migrate" ./scripts/mdbx_flow_test.sh
```

### Notes
- The script uses `--init-force` by default, which wipes the testnode volumes for a clean chain.
- The script assumes the default `nitro-testnode` compose project name (`nitro-testnode`).
- The script migrates the L3 datadir stored in the `validator-data` volume and updates `l3node_config.json`.

### Next checks (optional)
- Smoke-build the erigon binaries: `go build -tags erigon ./cmd/nitro` and `go build -tags erigon ./cmd/mdbx-migrate`.
- Prune-mode behavior: start with `--execution.erigon.prune-mode=full` and call a historical debug/trace RPC; expect a "historical data pruned" error for old blocks.

## Preconditions
- Stop the node and confirm no Nitro process is running.
- Backup the full chain directory (`l2chaindata`, `arbitrumdata`, `wasm`).
- Ensure enough disk space for a full replay and MDBX map growth.

## Migration (Full)
1. Run migration with verification: `mdbx-migrate --source <pebble> --dest <mdbx> --mode full --verify extended`.
2. If interrupted, resume: `mdbx-migrate --mode full --resume --verify extended`.
3. Verify logs show `phase=verify` sections and `db_summary` with non-zero counts.

## Cutover
1. Point Nitro at the MDBX datadir.
2. Set config: `execution.backend=erigon` and `persistent.db-engine=mdbx` (or keep `auto` for both).
3. Start the node and confirm log line `Execution backend selected` shows `erigon`.
4. Check RPC parity and health: `eth_blockNumber`, `eth_getBlockByNumber`, and debug/trace endpoints.
5. Monitor metrics: `arb/mdbx/map/*` and `arb/mdbx/read_tx/*`.

## Rollback
1. Stop the node.
2. Restore the Pebble backup to the original location.
3. Set config: `execution.backend=geth` and `persistent.db-engine=pebble` (or `auto` with no MDBX data present).
4. Start the node and confirm it follows the geth path.

## Troubleshooting
- If a checkpoint exists, rerun with `--resume` and keep `--start-block`/`--end-block` unchanged.
- If `UNFINISHED_MDBX_CONVERSION` exists, do not start the node; resume or remove the destination data.
- Mixed MDBX/Pebble detection will block startup until the datadir is consistent.
- If startup fails with a history pruning error, re-run migration with full history and ensure the MDBX prune config is not set to prune history.
- If startup fails with a stage-behind error, rerun a full migration so `Headers`, `BlockHashes`, `Bodies`, `Senders`, `Execution`, `TxLookup`, and `Finish` reach head.
- If startup fails with a history-missing error, ensure the migration produced history (history snapshots or history tables) and rerun full migration if needed.

## Limitations
- Full-history MDBX is required for debug/trace parity; if history is pruned, the node will start with warnings and historical RPCs will return "historical data pruned" errors for old blocks.
- Timeboost/express-lane is not supported on the Erigon backend.
- GraphQL is only supported on the geth backend; disable `graphql.enable` when using Erigon.
- `eth_sendTransaction` is not supported; use `eth_sendRawTransaction`.
- `txpool` RPC APIs are stubbed on the Erigon backend (return "not supported").
- `persistent.ancient` (geth freezer) is not supported with the Erigon backend.
- `persistent.db-engine` must be `mdbx` (or `auto`); `pebble`/`leveldb` are rejected with the Erigon backend.
- `--init.*`, reorg options, and `blocks-reexecutor` are geth-only; use `mdbx-migrate` to produce MDBX data before starting Erigon.
- The Erigon backend requires existing MDBX data for `l2chaindata`, `arbitrumdata`, and `wasm`; empty or partial datadirs are not initialized.
- Auto-detect (`execution.backend=auto`) errors on partial MDBX data; ensure all three MDBX DBs are present.
