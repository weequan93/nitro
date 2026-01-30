
## Deriw-Erigon


docker build --no-cache . \
  --target nitro-node-dev \
  --tag quanquanah/nitro-node:erigon-mac \
  --build-arg GO_BUILD_TAGS=erigon \
  --build-arg SKIP_FORGE_YUL=1

go build -tags erigon -o target/bin/mdbx-migrate ./cmd/mdbx-migrate                                                         
go build -tags erigon -o target/bin/mdbx-storage-diff ./cmd/mdbx-storage-diff   





### Migration

rm -rf /tmp/mdbx-debug10
rm -rf /tmp/nitro-src
cp -R "/Users/super/Documents/coinw/dex/localchain-test/config/My Arbitrum L3 Chain/nitro" /tmp/nitro-src
ERIGON_MDBX_MIGRATE_ACCOUNTTRACE=1 \
ERIGON_MDBX_MIGRATE_ACCOUNTTRACE_ADDRS=0x1820a4b7618bde71dce8cdc73aab6c95905fad24 \
ERIGON_MDBX_MIGRATE_DEBUG=1 \
ERIGON_MDBX_MIGRATE_DEBUG_BLOCK=32 \
ERIGON_MDBX_MIGRATE_DEBUG_TX_INDEX=1 \
ERIGON_MDBX_MIGRATE_DEBUG_TX_DATA=1 \
ERIGON_MDBX_MIGRATE_DEBUG_WRITESET=1 \
ERIGON_MDBX_MIGRATE_DEBUG_WRITESET_MAX=400 \
ERIGON_MDBX_MIGRATE_STORAGETRACE=true \
ERIGON_MDBX_MIGRATE_STORAGETRACE_BLOCK=32 \
ERIGON_MDBX_MIGRATE_STORAGETRACE_TX_INDEX=1 \
ERIGON_BAD_ROOT_DEBUG=1 \
ERIGON_MDBX_MIGRATE_KEEP_EMPTY_ACCOUNTS=false \
ERIGON_MDBX_MIGRATE_SKIP_UNWIND_ON_BAD_ROOT=false  \
ERIGON_MDBX_MIGRATE_FLUSH_ON_BAD_ROOT=false \
ERIGON_MDBX_MIGRATE_ZOMBIE_DEBUG=false \
ERIGON_BAD_ROOT_DEBUG=false \
GOCACHE=$PWD/.gocache/build GOMODCACHE=/tmp/go-mod-cache GOPROXY=off \
  target/bin/mdbx-migrate --source /tmp/nitro-src --dest /tmp/mdbx-debug10 \
  --mode full --verify strict --start-block 0 --end-block 32


  rm -rf /tmp/mdbx-debug10
rm -rf /tmp/nitro-src
cp -R "/Users/super/Documents/coinw/dex/localchain-test/config/My Arbitrum L3 Chain/nitro" /tmp/nitro-src

ERIGON_BAD_ROOT_DEBUG=false \
GOCACHE=$PWD/.gocache/build GOMODCACHE=/tmp/go-mod-cache GOPROXY=off \
  target/bin/mdbx-migrate --source /tmp/nitro-src --dest /tmp/mdbx-debug10 \
  --mode full --verify strict --start-block 0 --end-block 32

### Verify Consensus

go build -tags erigon -o target/bin/mdbx-migrate ./cmd/mdbx-migrate

  rm -rf /tmp/mdbx-check /tmp/nitro-src
  cp -R "/Users/super/Documents/coinw/dex/localchain-test/config/My Arbitrum L3 Chain/nitro" /tmp/nitro-src

  target/bin/mdbx-migrate \
    --source /tmp/nitro-src \
    --dest /tmp/mdbx-check \
    --mode full \
    --verify consensus \
    --verify-samples 33 \
    --start-block 0 --end-block 32


    rm -rf /tmp/mdbx-check /tmp/nitro-src
  cp -R "/Users/super/Documents/coinw/dex/localchain/config/My Arbitrum L3 Chain/nitro" /tmp/nitro-src

  target/bin/mdbx-migrate \
    --source /tmp/nitro-src \
    --dest /tmp/mdbx-check \
    --mode full \
    --verify consensus \
    --verify-samples 38 \
    --start-block 0 --end-block 37

### Diff


DB_PATH=/tmp/mdbx-debug10/l2chaindata go run -tags erigon /tmp/list_keys.go | awk 'NF>=1{print $1}' > /tmp/dest_keys13.keys
  target/bin/mdbx-storage-diff --source /tmp/nitro-src --dest /tmp/mdbx-debug10 \
    --keys /tmp/dest_keys13.keys --block 14 --compare-accounts --compare-storage-root

go run -tags erigon /tmp/account_probe.go \
    --source /tmp/nitro-src --dest /tmp/mdbx-debug10 \
    --addr 0x502FFdAfd660AEDf4Ea7DB3D758999e154102a6c --block 10

  go run ./cmd/compare-nodes --a http://127.0.0.1:8547 --b http://127.0.0.1:8549

  Examples:

  # Range + step
  go run ./cmd/compare-nodes --a http://127.0.0.1:8449 --b http://127.0.0.1:8450 --from 0 --to 35 --step 1

  # Include account checks
  go run ./cmd/compare-nodes --a http://127.0.0.1:8449 --b http://127.0.0.1:8450 \
    --addr 0x28c18bc63069e3581870904f32Dd34D9e3332cce \
    --storage-key 0x0


rm -rf /tmp/mdbx-debug1 /tmp/nitro-src
  cp -R "/Users/super/Documents/coinw/dex/localchain-test/config/My Arbitrum L3 Chain/nitro" /tmp/nitro-src

  ERIGON_BAD_ROOT_DEBUG=1 \
  ERIGON_MDBX_MIGRATE_DEBUG=1 \
  ERIGON_MDBX_MIGRATE_DEBUG_BLOCK=1 \
  ERIGON_MDBX_MIGRATE_DEBUG_TX_INDEX=1 \
  ERIGON_MDBX_MIGRATE_DEBUG_TX_DATA=1 \
  target/bin/mdbx-migrate --source /tmp/nitro-src --dest /tmp/mdbx-debug1 \
    --mode full --verify none --start-block 0 --end-block 1 \

   rg "internal tx startblock|pricing update|l2pricing add to gas pool|arbos: l2 gas pool|gas_backlog" /tmp/mdbx-orig.log > /tmp/orig.filtered
  rg "internal tx startblock|pricing update|l2pricing add to gas pool|arbos: l2 gas pool|gas_backlog" /tmp/mdbx-mod.log  > /tmp/mod.filtered
  diff -u /tmp/orig.filtered /tmp/mod.filtered


### Run

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


## Pending
- init flag all not supported
