

### Verify


```
rg -o "storagetrace.*block=[0-9]+.*key=0x[0-9a-f]+" ./mdbx-all-storagetrace.log \
  | sed -E 's/.*block=([0-9]+).*key=0x([0-9a-f]+).*/\1 \2/' \
  | sort -u > /tmp/mdbx-block-keys.txt

rm -f /tmp/mdbx-keys-*.txt
while read -r block key; do
  echo "$key" >> "/tmp/mdbx-keys-$block.txt"
done < /tmp/mdbx-block-keys.txt

for f in /tmp/mdbx-keys-*.txt; do
  rm -rf "/Users/super/Documents/coinw/dex/localchain-test/config/My Arbitrum L3 Chain/nitro-mdbx"
  b="${f##*/}"; b="${b#mdbx-keys-}"; b="${b%.txt}"
  if target/bin/mdbx-storage-diff \
      --source "/Users/super/Documents/coinw/dex/localchain-test/config/My Arbitrum L3 Chain/nitro" \
      --dest "/Users/super/Documents/coinw/dex/localchain-test/config/My Arbitrum L3 Chain/nitro-mdbx" \
      --keys "$f" \
      --block "$b" 2>/dev/null | rg -q "mismatch"
  then
    echo "first mismatch at block $b"
    break
  fi
done


```
 
 
rm -rf  /tmp/mdbx-debug10
target/bin/mdbx-migrate \
      --source "/Users/super/Documents/coinw/dex/localchain-test/config/My Arbitrum L3 Chain/nitro" --dest /tmp/mdbx-debug10 \
      --mode full --verify none --start-block 0 --end-block 1 \
      ERIGON_MDBX_MIGRATE_KEEP_EMPTY_ACCOUNTS=true \
      ERIGON_BAD_ROOT_DEBUG=1 \
      ERIGON_MDBX_MIGRATE_SKIP_UNWIND_ON_BAD_ROOT=true \
      ERIGON_MDBX_MIGRATE_DEBUG_BLOCK=1 \
      ERIGON_MDBX_MIGRATE_FLUSH_ON_BAD_ROOT=true

       rm -rf /tmp/mdbx-debug10
  cp -R "/Users/super/Documents/coinw/dex/localchain-test/config/My Arbitrum L3 Chain/nitro" /tmp/nitro-src
  ERIGON_MDBX_MIGRATE_KEEP_EMPTY_ACCOUNTS=true ERIGON_BAD_ROOT_DEBUG=1 ERIGON_MDBX_MIGRATE_SKIP_UNWIND_ON_BAD_ROOT=true ERIGON_MDBX_MIGRATE_DEBUG_BLOCK=1 ERIGON_MDBX_MIGRATE_FLUSH_ON_BAD_ROOT=true \
  GOCACHE=$PWD/.gocache/build GOMODCACHE=/tmp/go-mod-cache GOPROXY=off \
    target/bin/mdbx-migrate --source /tmp/nitro-src --dest /tmp/mdbx-debug10 \
    --mode full --verify none --start-block 0 --end-block 1


rm -rf  /tmp/mdbx-debug10
target/bin/mdbx-migrate \
      --source "/Users/super/Documents/coinw/dex/localchain-test/config/My Arbitrum L3 Chain/nitro" --dest /tmp/mdbx-debug10 \
      --mode full --verify none --start-block 0 --end-block 9 \
      ERIGON_MDBX_MIGRATE_KEEP_EMPTY_ACCOUNTS=true \
      ERIGON_BAD_ROOT_DEBUG=9 \
      ERIGON_MDBX_MIGRATE_SKIP_UNWIND_ON_BAD_ROOT=true \
      ERIGON_MDBX_MIGRATE_DEBUG_BLOCK=9 \
      ERIGON_MDBX_MIGRATE_FLUSH_ON_BAD_ROOT=true

rm -rf  /tmp/mdbx-debug10
target/bin/mdbx-migrate \
      --source "/Users/super/Documents/coinw/dex/localchain-test/config/My Arbitrum L3 Chain/nitro" --dest /tmp/mdbx-debug10 \
      --mode full --verify none --start-block 0 --end-block 10 \
      ERIGON_MDBX_MIGRATE_KEEP_EMPTY_ACCOUNTS=true \
      ERIGON_BAD_ROOT_DEBUG=10 \
      ERIGON_MDBX_MIGRATE_SKIP_UNWIND_ON_BAD_ROOT=true \
      ERIGON_MDBX_MIGRATE_DEBUG_BLOCK=10 \
      ERIGON_MDBX_MIGRATE_FLUSH_ON_BAD_ROOT=true

       rm -rf /tmp/mdbx-debug10
  cp -R "/Users/super/Documents/coinw/dex/localchain-test/config/My Arbitrum L3 Chain/nitro" /tmp/nitro-src
  ERIGON_MDBX_MIGRATE_KEEP_EMPTY_ACCOUNTS=true ERIGON_BAD_ROOT_DEBUG=10 ERIGON_MDBX_MIGRATE_SKIP_UNWIND_ON_BAD_ROOT=true ERIGON_MDBX_MIGRATE_DEBUG_BLOCK=10 ERIGON_MDBX_MIGRATE_FLUSH_ON_BAD_ROOT=true \
  GOCACHE=$PWD/.gocache/build GOMODCACHE=/tmp/go-mod-cache GOPROXY=off \
    target/bin/mdbx-migrate --source /tmp/nitro-src --dest /tmp/mdbx-debug10 \
    --mode full --verify none --start-block 0 --end-block 10


rm -rf  /tmp/mdbx-debug10
target/bin/mdbx-migrate \
      --source /tmp/nitro-copy --dest /tmp/mdbx-debug10 \
      --mode full --verify none --start-block 0 --end-block 12 \
      ERIGON_MDBX_MIGRATE_KEEP_EMPTY_ACCOUNTS=true \
      ERIGON_BAD_ROOT_DEBUG=12 \
      ERIGON_MDBX_MIGRATE_SKIP_UNWIND_ON_BAD_ROOT=true \
      ERIGON_MDBX_MIGRATE_DEBUG_BLOCK=12 \
      ERIGON_MDBX_MIGRATE_FLUSH_ON_BAD_ROOT=true

       rm -rf /tmp/mdbx-debug10
  cp -R "/Users/super/Documents/coinw/dex/localchain-test/config/My Arbitrum L3 Chain/nitro" /tmp/nitro-src
  ERIGON_MDBX_MIGRATE_KEEP_EMPTY_ACCOUNTS=true ERIGON_BAD_ROOT_DEBUG=12 ERIGON_MDBX_MIGRATE_SKIP_UNWIND_ON_BAD_ROOT=true ERIGON_MDBX_MIGRATE_DEBUG_BLOCK=12 ERIGON_MDBX_MIGRATE_FLUSH_ON_BAD_ROOT=true \
  GOCACHE=$PWD/.gocache/build GOMODCACHE=/tmp/go-mod-cache GOPROXY=off \
    target/bin/mdbx-migrate --source /tmp/nitro-src --dest /tmp/mdbx-debug10 \
    --mode full --verify none --start-block 0 --end-block 12

rm -rf  /tmp/mdbx-debug10
target/bin/mdbx-migrate \
      --source /tmp/nitro-copy --dest /tmp/mdbx-debug10 \
      --mode full --verify none --start-block 0 --end-block 13 \
      ERIGON_MDBX_MIGRATE_KEEP_EMPTY_ACCOUNTS=true \
      ERIGON_BAD_ROOT_DEBUG=13 \
      ERIGON_MDBX_MIGRATE_SKIP_UNWIND_ON_BAD_ROOT=true \
      ERIGON_MDBX_MIGRATE_DEBUG_BLOCK=13 \
      ERIGON_MDBX_MIGRATE_FLUSH_ON_BAD_ROOT=true

            rm -rf /tmp/mdbx-debug10
  cp -R "/Users/super/Documents/coinw/dex/localchain-test/config/My Arbitrum L3 Chain/nitro" /tmp/nitro-src
  ERIGON_MDBX_MIGRATE_KEEP_EMPTY_ACCOUNTS=true ERIGON_BAD_ROOT_DEBUG=13 ERIGON_MDBX_MIGRATE_SKIP_UNWIND_ON_BAD_ROOT=true ERIGON_MDBX_MIGRATE_DEBUG_BLOCK=13 ERIGON_MDBX_MIGRATE_FLUSH_ON_BAD_ROOT=true \
  GOCACHE=$PWD/.gocache/build GOMODCACHE=/tmp/go-mod-cache GOPROXY=off \
    target/bin/mdbx-migrate --source /tmp/nitro-src --dest /tmp/mdbx-debug10 \
    --mode full --verify none --start-block 0 --end-block 13


      rm -rf "/Users/super/Documents/coinw/dex/localchain-test/config/My Arbitrum L3 Chain/nitro-mdbx"

ERIGON_BAD_ROOT_DEBUG=1 \
ERIGON_COMMIT_EACH_BLOCK=1 \
ERIGON_BAD_ROOT_ACCOUNTS=0x28c18bc63069e3581870904f32Dd34D9e3332cce,0x31c5a1C83265113bd089385d76dfe4D8A2577204,0xA4b000000000000000000073657175656e636572,0xA4B00000000000000000000000000000000000f6,0x00000000000000000000000000000000000A4B05,0xA4B05FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF \
ERIGON_MDBX_MIGRATE_DEBUG=1 \
MDBX_MIGRATE_DEBUG_TX_INDEX=2 \
MDBX_MIGRATE_DEBUG=1 \
ERIGON_MDBX_MIGRATE_DEBUG_BLOCK=10 \
MDBX_MIGRATE_DEBUG_BLOCK=10 \
target/bin/mdbx-migrate \
  --source "/Users/super/Documents/coinw/dex/localchain-test/config/My Arbitrum L3 Chain/nitro" \
  --dest "/Users/super/Documents/coinw/dex/localchain-test/config/My Arbitrum L3 Chain/nitro-mdbx" \
  --mode full \
  --verify basic \
  --start-block 0 \
  --end-block 10 | tee ./mdbx-10-debug.log

  rm -rf "/Users/super/Documents/coinw/dex/localchain-test/config/My Arbitrum L3 Chain/nitro-mdbx"

ERIGON_BAD_ROOT_DEBUG=1 \
ERIGON_COMMIT_EACH_BLOCK=1 \
ERIGON_BAD_ROOT_ACCOUNTS=0x28c18bc63069e3581870904f32Dd34D9e3332cce,0x31c5a1C83265113bd089385d76dfe4D8A2577204,0xA4b000000000000000000073657175656e636572,0xA4B00000000000000000000000000000000000f6,0x00000000000000000000000000000000000A4B05,0xA4B05FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF \
ERIGON_MDBX_MIGRATE_DEBUG=1 \
MDBX_MIGRATE_DEBUG_TX_INDEX=2 \
MDBX_MIGRATE_DEBUG=1 \
ERIGON_MDBX_MIGRATE_DEBUG_BLOCK=9 \
MDBX_MIGRATE_DEBUG_BLOCK=9 \
target/bin/mdbx-migrate \
  --source "/Users/super/Documents/coinw/dex/localchain-test/config/My Arbitrum L3 Chain/nitro" \
  --dest "/Users/super/Documents/coinw/dex/localchain-test/config/My Arbitrum L3 Chain/nitro-mdbx" \
  --mode full \
  --verify basic \
  --start-block 0 \
  --end-block 9 | tee ./mdbx-10-debug.log

rm -rf /tmp/mdbx-debug10
    rm -rf /tmp/nitro-src
  cp -R "/Users/super/Documents/coinw/dex/localchain-test/config/My Arbitrum L3 Chain/nitro" /tmp/nitro-src
  ERIGON_MDBX_MIGRATE_KEEP_EMPTY_ACCOUNTS=true ERIGON_BAD_ROOT_DEBUG=1 ERIGON_MDBX_MIGRATE_SKIP_UNWIND_ON_BAD_ROOT=true ERIGON_MDBX_MIGRATE_DEBUG_BLOCK=1 ERIGON_MDBX_MIGRATE_FLUSH_ON_BAD_ROOT=true \
  GOCACHE=$PWD/.gocache/build GOMODCACHE=/tmp/go-mod-cache GOPROXY=off \
    target/bin/mdbx-migrate --source /tmp/nitro-src --dest /tmp/mdbx-debug10 \
    --mode full --verify none --start-block 0 --end-block 1


    rm -rf /tmp/mdbx-debug10
    rm -rf /tmp/nitro-src
  cp -R "/Users/super/Documents/coinw/dex/localchain-test/config/My Arbitrum L3 Chain/nitro" /tmp/nitro-src
  ERIGON_MDBX_MIGRATE_ACCOUNTTRACE_ADDRS=0x817d11080d680ce6109c21427da4e4d324003b85 ERIGON_MDBX_MIGRATE_KEEP_EMPTY_ACCOUNTS=true ERIGON_BAD_ROOT_DEBUG=13 ERIGON_MDBX_MIGRATE_SKIP_UNWIND_ON_BAD_ROOT=true ERIGON_MDBX_MIGRATE_DEBUG_BLOCK=13 ERIGON_MDBX_MIGRATE_FLUSH_ON_BAD_ROOT=true \
  GOCACHE=$PWD/.gocache/build GOMODCACHE=/tmp/go-mod-cache GOPROXY=off \
    target/bin/mdbx-migrate --source /tmp/nitro-src --dest /tmp/mdbx-debug10 \
    --mode full --verify none --start-block 0 --end-block 13


  rm -rf /tmp/mdbx-debug10
  rm -rf /tmp/nitro-src
  cp -R "/Users/super/Documents/coinw/dex/localchain-test/config/My Arbitrum L3 Chain/nitro" /tmp/nitro-src
  ERIGON_MDBX_MIGRATE_ACCOUNTTRACE=1 \
  ERIGON_MDBX_MIGRATE_ACCOUNTTRACE_ADDRS=0x1820a4b7618bde71dce8cdc73aab6c95905fad24 \
  ERIGON_MDBX_MIGRATE_DEBUG=1 \
  ERIGON_MDBX_MIGRATE_DEBUG_BLOCK=1 \
  ERIGON_MDBX_MIGRATE_DEBUG_TX_DATA=1 \
  ERIGON_MDBX_MIGRATE_DEBUG_WRITESET=1 \
  ERIGON_MDBX_MIGRATE_DEBUG_WRITESET_MAX=200 \
  ERIGON_BAD_ROOT_DEBUG=1 \
  ERIGON_MDBX_MIGRATE_KEEP_EMPTY_ACCOUNTS=false \
  ERIGON_MDBX_MIGRATE_SKIP_UNWIND_ON_BAD_ROOT=true  \
  ERIGON_MDBX_MIGRATE_FLUSH_ON_BAD_ROOT=true \
  ERIGON_MDBX_MIGRATE_ZOMBIE_DEBUG=true \
  GOCACHE=$PWD/.gocache/build GOMODCACHE=/tmp/go-mod-cacGOPROXY=off \
    target/bin/mdbx-migrate --source /tmp/nitro-src --dest /tmp/mdbx-debug10 \
    --mode full --verify none --start-block 0 --end-block 1


  rm -rf /tmp/mdbx-debug10
  rm -rf /tmp/nitro-src
  cp -R "/Users/super/Documents/coinw/dex/localchain-test/config/My Arbitrum L3 Chain/nitro" /tmp/nitro-src
  ERIGON_MDBX_MIGRATE_ACCOUNTTRACE=1 \
  ERIGON_MDBX_MIGRATE_ACCOUNTTRACE_ADDRS=0x1820a4b7618bde71dce8cdc73aab6c95905fad24 \
  ERIGON_MDBX_MIGRATE_DEBUG=1 \
  ERIGON_MDBX_MIGRATE_DEBUG_BLOCK=9 \
  ERIGON_MDBX_MIGRATE_DEBUG_TX_INDEX=1 \
  ERIGON_MDBX_MIGRATE_DEBUG_TX_DATA=1 \
  ERIGON_MDBX_MIGRATE_DEBUG_WRITESET=1 \
  ERIGON_MDBX_MIGRATE_DEBUG_WRITESET_MAX=400 \
  ERIGON_MDBX_MIGRATE_STORAGETRACE=true \
  ERIGON_MDBX_MIGRATE_STORAGETRACE_BLOCK=9 \
  ERIGON_MDBX_MIGRATE_STORAGETRACE_TX_INDEX=1 \
  ERIGON_BAD_ROOT_DEBUG=9 \
  ERIGON_MDBX_MIGRATE_KEEP_EMPTY_ACCOUNTS=false \
  ERIGON_MDBX_MIGRATE_SKIP_UNWIND_ON_BAD_ROOT=true  \
  ERIGON_MDBX_MIGRATE_FLUSH_ON_BAD_ROOT=true \
  GOCACHE=$PWD/.gocache/build GOMODCACHE=/tmp/go-mod-cache GOPROXY=off \
    target/bin/mdbx-migrate --source /tmp/nitro-src --dest /tmp/mdbx-debug10 \
    --mode full --verify none --start-block 0 --end-block 9

  rm -rf /tmp/mdbx-debug10
  rm -rf /tmp/nitro-src
  cp -R "/Users/super/Documents/coinw/dex/localchain-test/config/My Arbitrum L3 Chain/nitro" /tmp/nitro-src
  ERIGON_MDBX_MIGRATE_ACCOUNTTRACE=1 \
  ERIGON_MDBX_MIGRATE_ACCOUNTTRACE_ADDRS=0x1820a4b7618bde71dce8cdc73aab6c95905fad24 \
  ERIGON_MDBX_MIGRATE_DEBUG=1 \
  ERIGON_MDBX_MIGRATE_DEBUG_BLOCK=10 \
  ERIGON_MDBX_MIGRATE_DEBUG_TX_INDEX=1 \
  ERIGON_MDBX_MIGRATE_DEBUG_TX_DATA=1 \
  ERIGON_MDBX_MIGRATE_DEBUG_WRITESET=1 \
  ERIGON_MDBX_MIGRATE_DEBUG_WRITESET_MAX=400 \
  ERIGON_MDBX_MIGRATE_STORAGETRACE=true \
  ERIGON_MDBX_MIGRATE_STORAGETRACE_BLOCK=10 \
  ERIGON_MDBX_MIGRATE_STORAGETRACE_TX_INDEX=1 \
  ERIGON_BAD_ROOT_DEBUG=1 \
  ERIGON_MDBX_MIGRATE_KEEP_EMPTY_ACCOUNTS=false \
  ERIGON_MDBX_MIGRATE_SKIP_UNWIND_ON_BAD_ROOT=true  \
  ERIGON_MDBX_MIGRATE_FLUSH_ON_BAD_ROOT=true \
  GOCACHE=$PWD/.gocache/build GOMODCACHE=/tmp/go-mod-cache GOPROXY=off \
    target/bin/mdbx-migrate --source /tmp/nitro-src --dest /tmp/mdbx-debug10 \
    --mode full --verify none --start-block 0 --end-block 10




    rm -rf /tmp/mdbx-debug10
    rm -rf /tmp/nitro-src
  cp -R "/Users/super/Documents/coinw/dex/localchain-test/config/My Arbitrum L3 Chain/nitro" /tmp/nitro-src
  ERIGON_MDBX_MIGRATE_KEEP_EMPTY_ACCOUNTS=true ERIGON_BAD_ROOT_DEBUG=1 ERIGON_MDBX_MIGRATE_SKIP_UNWIND_ON_BAD_ROOT=true ERIGON_MDBX_MIGRATE_DEBUG_BLOCK=1 ERIGON_MDBX_MIGRATE_FLUSH_ON_BAD_ROOT=true \
  GOCACHE=$PWD/.gocache/build GOMODCACHE=/tmp/go-mod-cache GOPROXY=off \
    target/bin/mdbx-migrate --source /tmp/nitro-src --dest /tmp/mdbx-debug10 \
    --mode full --verify none --start-block 0 --end-block 1


 DB_PATH=/tmp/mdbx-debug10/l2chaindata go run -tags erigon /tmp/list_keys.go | awk 'NF>=1{print $1}' > /tmp/dest_keys13.keys
  target/bin/mdbx-storage-diff --source /tmp/nitro-src --dest /tmp/mdbx-debug10 \
    --keys /tmp/dest_keys13.keys --block 14 --compare-accounts --compare-storage-root




  rm -rf /tmp/mdbx-debug10
  rm -rf /tmp/nitro-src
  cp -R "/Users/super/Documents/coinw/dex/localchain-test/config/My Arbitrum L3 Chain/nitro" /tmp/nitro-src
  ERIGON_MDBX_MIGRATE_ACCOUNTTRACE=1 \
  ERIGON_MDBX_MIGRATE_ACCOUNTTRACE_ADDRS=0x1820a4b7618bde71dce8cdc73aab6c95905fad24 \
  ERIGON_MDBX_MIGRATE_DEBUG=1 \
  ERIGON_MDBX_MIGRATE_DEBUG_BLOCK=13 \
  ERIGON_MDBX_MIGRATE_DEBUG_TX_INDEX=1 \
  ERIGON_MDBX_MIGRATE_DEBUG_TX_DATA=1 \
  ERIGON_MDBX_MIGRATE_DEBUG_WRITESET=1 \
  ERIGON_MDBX_MIGRATE_DEBUG_WRITESET_MAX=400 \
  ERIGON_MDBX_MIGRATE_STORAGETRACE=true \
  ERIGON_MDBX_MIGRATE_STORAGETRACE_BLOCK=13 \
  ERIGON_MDBX_MIGRATE_STORAGETRACE_TX_INDEX=1 \
  ERIGON_BAD_ROOT_DEBUG=1 \
  ERIGON_MDBX_MIGRATE_KEEP_EMPTY_ACCOUNTS=false \
  ERIGON_MDBX_MIGRATE_SKIP_UNWIND_ON_BAD_ROOT=true  \
  ERIGON_MDBX_MIGRATE_FLUSH_ON_BAD_ROOT=true \
  ERIGON_MDBX_MIGRATE_ZOMBIE_DEBUG=true \
  GOCACHE=$PWD/.gocache/build GOMODCACHE=/tmp/go-mod-cache GOPROXY=off \
    target/bin/mdbx-migrate --source /tmp/nitro-src --dest /tmp/mdbx-debug10 \
    --mode full --verify none --start-block 0 --end-block 13



  rm -rf /tmp/mdbx-debug10
  rm -rf /tmp/nitro-src
  cp -R "/Users/super/Documents/coinw/dex/localchain-test/config/My Arbitrum L3 Chain/nitro" /tmp/nitro-src
  ERIGON_MDBX_MIGRATE_ACCOUNTTRACE=1 \
  ERIGON_MDBX_MIGRATE_ACCOUNTTRACE_ADDRS=0x1820a4b7618bde71dce8cdc73aab6c95905fad24 \
  ERIGON_MDBX_MIGRATE_DEBUG=1 \
  ERIGON_MDBX_MIGRATE_DEBUG_BLOCK=12 \
  ERIGON_MDBX_MIGRATE_DEBUG_TX_INDEX=1 \
  ERIGON_MDBX_MIGRATE_DEBUG_TX_DATA=1 \
  ERIGON_MDBX_MIGRATE_DEBUG_WRITESET=1 \
  ERIGON_MDBX_MIGRATE_DEBUG_WRITESET_MAX=400 \
  ERIGON_MDBX_MIGRATE_STORAGETRACE=true \
  ERIGON_MDBX_MIGRATE_STORAGETRACE_BLOCK=12 \
  ERIGON_MDBX_MIGRATE_STORAGETRACE_TX_INDEX=1 \
  ERIGON_BAD_ROOT_DEBUG=12 \
  ERIGON_MDBX_MIGRATE_KEEP_EMPTY_ACCOUNTS=false \
  ERIGON_MDBX_MIGRATE_SKIP_UNWIND_ON_BAD_ROOT=true  \
  ERIGON_MDBX_MIGRATE_FLUSH_ON_BAD_ROOT=true \
  ERIGON_MDBX_MIGRATE_ZOMBIE_DEBUG=true \
  GOCACHE=$PWD/.gocache/build GOMODCACHE=/tmp/go-mod-cache GOPROXY=off \
    target/bin/mdbx-migrate --source /tmp/nitro-src --dest /tmp/mdbx-debug10 \
    --mode full --verify none --start-block 0 --end-block 12

   rm -rf /tmp/mdbx-debug10
  rm -rf /tmp/nitro-src
  cp -R "/Users/super/Documents/coinw/dex/localchain-test/config/My Arbitrum L3 Chain/nitro" /tmp/nitro-src
  ERIGON_MDBX_MIGRATE_ACCOUNTTRACE=1 \
  ERIGON_MDBX_MIGRATE_ACCOUNTTRACE_ADDRS=0x1820a4b7618bde71dce8cdc73aab6c95905fad24 \
  ERIGON_MDBX_MIGRATE_DEBUG=1 \
  ERIGON_MDBX_MIGRATE_DEBUG_BLOCK=14 \
  ERIGON_MDBX_MIGRATE_DEBUG_TX_INDEX=1 \
  ERIGON_MDBX_MIGRATE_DEBUG_TX_DATA=1 \
  ERIGON_MDBX_MIGRATE_DEBUG_WRITESET=1 \
  ERIGON_MDBX_MIGRATE_DEBUG_WRITESET_MAX=400 \
  ERIGON_MDBX_MIGRATE_STORAGETRACE=true \
  ERIGON_MDBX_MIGRATE_STORAGETRACE_BLOCK=14 \
  ERIGON_MDBX_MIGRATE_STORAGETRACE_TX_INDEX=1 \
  ERIGON_BAD_ROOT_DEBUG=1 \
  ERIGON_MDBX_MIGRATE_KEEP_EMPTY_ACCOUNTS=false \
  ERIGON_MDBX_MIGRATE_SKIP_UNWIND_ON_BAD_ROOT=true  \
  ERIGON_MDBX_MIGRATE_FLUSH_ON_BAD_ROOT=true \
  ERIGON_MDBX_MIGRATE_ZOMBIE_DEBUG=true \
  GOCACHE=$PWD/.gocache/build GOMODCACHE=/tmp/go-mod-cache GOPROXY=off \
    target/bin/mdbx-migrate --source /tmp/nitro-src --dest /tmp/mdbx-debug10 \
    --mode full --verify none --start-block 0 --end-block 14

   rm -rf /tmp/mdbx-debug10
  rm -rf /tmp/nitro-src
  cp -R "/Users/super/Documents/coinw/dex/localchain-test/config/My Arbitrum L3 Chain/nitro" /tmp/nitro-src
  ERIGON_MDBX_MIGRATE_ACCOUNTTRACE=1 \
  ERIGON_MDBX_MIGRATE_ACCOUNTTRACE_ADDRS=0x1820a4b7618bde71dce8cdc73aab6c95905fad24 \
  ERIGON_MDBX_MIGRATE_DEBUG=1 \
  ERIGON_MDBX_MIGRATE_DEBUG_BLOCK=15 \
  ERIGON_MDBX_MIGRATE_DEBUG_TX_INDEX=1 \
  ERIGON_MDBX_MIGRATE_DEBUG_TX_DATA=1 \
  ERIGON_MDBX_MIGRATE_DEBUG_WRITESET=1 \
  ERIGON_MDBX_MIGRATE_DEBUG_WRITESET_MAX=400 \
  ERIGON_MDBX_MIGRATE_STORAGETRACE=true \
  ERIGON_MDBX_MIGRATE_STORAGETRACE_BLOCK=15 \
  ERIGON_MDBX_MIGRATE_STORAGETRACE_TX_INDEX=1 \
  ERIGON_BAD_ROOT_DEBUG=1 \
  ERIGON_MDBX_MIGRATE_KEEP_EMPTY_ACCOUNTS=false \
  ERIGON_MDBX_MIGRATE_SKIP_UNWIND_ON_BAD_ROOT=true  \
  ERIGON_MDBX_MIGRATE_FLUSH_ON_BAD_ROOT=true \
  ERIGON_MDBX_MIGRATE_ZOMBIE_DEBUG=true \
  GOCACHE=$PWD/.gocache/build GOMODCACHE=/tmp/go-mod-cache GOPROXY=off \
    target/bin/mdbx-migrate --source /tmp/nitro-src --dest /tmp/mdbx-debug10 \
    --mode full --verify none --start-block 0 --end-block 15

       rm -rf /tmp/mdbx-debug10
  rm -rf /tmp/nitro-src
  cp -R "/Users/super/Documents/coinw/dex/localchain-test/config/My Arbitrum L3 Chain/nitro" /tmp/nitro-src
  ERIGON_MDBX_MIGRATE_ACCOUNTTRACE=1 \
  ERIGON_MDBX_MIGRATE_ACCOUNTTRACE_ADDRS=0x1820a4b7618bde71dce8cdc73aab6c95905fad24 \
  ERIGON_MDBX_MIGRATE_DEBUG=1 \
  ERIGON_MDBX_MIGRATE_DEBUG_BLOCK=28 \
  ERIGON_MDBX_MIGRATE_DEBUG_TX_INDEX=1 \
  ERIGON_MDBX_MIGRATE_DEBUG_TX_DATA=1 \
  ERIGON_MDBX_MIGRATE_DEBUG_WRITESET=1 \
  ERIGON_MDBX_MIGRATE_DEBUG_WRITESET_MAX=400 \
  ERIGON_MDBX_MIGRATE_STORAGETRACE=true \
  ERIGON_MDBX_MIGRATE_STORAGETRACE_BLOCK=28 \
  ERIGON_MDBX_MIGRATE_STORAGETRACE_TX_INDEX=1 \
  ERIGON_BAD_ROOT_DEBUG=1 \
  ERIGON_MDBX_MIGRATE_KEEP_EMPTY_ACCOUNTS=false \
  ERIGON_MDBX_MIGRATE_SKIP_UNWIND_ON_BAD_ROOT=true  \
  ERIGON_MDBX_MIGRATE_FLUSH_ON_BAD_ROOT=true \
  ERIGON_MDBX_MIGRATE_ZOMBIE_DEBUG=true \
  GOCACHE=$PWD/.gocache/build GOMODCACHE=/tmp/go-mod-cache GOPROXY=off \
    target/bin/mdbx-migrate --source /tmp/nitro-src --dest /tmp/mdbx-debug10 \
    --mode full --verify none --start-block 0 --end-block 28

go run -tags erigon /tmp/account_probe.go \
    --source /tmp/nitro-src --dest /tmp/mdbx-debug10 \
    --addr 0xe5052b97618c9ff3025bdece4d9a5e9e229b64b3 --block 13

go run -tags erigon /tmp/account_probe.go \
    --source /tmp/nitro-src --dest /tmp/mdbx-debug10 \
    --addr 0x502FFdAfd660AEDf4Ea7DB3D758999e154102a6c --block 10

go run -tags erigon /tmp/account_probe.go \
    --source /tmp/nitro-src --dest /tmp/mdbx-debug10 \
    --addr 0x6633629b0817d54fF9148218ec4ABb1cb920ea50 --block 7
    

go build -tags erigon -o target/bin/mdbx-migrate ./cmd/mdbx-migrate

rm -rf /tmp/mdbx-debug10
rm -rf /tmp/nitro-src
cp -R "/Users/super/Documents/coinw/dex/localchain-test/config/My Arbitrum L3 Chain/nitro" /tmp/nitro-src
rm -rf /tmp/mdbx-debug10
target/bin/mdbx-migrate --source /tmp/nitro-src --dest /tmp/mdbx-debug10 --mode full --verify extended --verify-samples 33

