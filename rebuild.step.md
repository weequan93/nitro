### Rebuild

$ cd /Users/super/Documents/coinw/dex/nitro-erigon/erigon-nitro-deriw
$ docker build --no-cache . \
  --target nitro-node-dev \
  --tag quanquanah/nitro-node:erigon-mac \
  --build-arg GO_BUILD_TAGS=erigon \
  --build-arg SKIP_FORGE_YUL=1

$ rm -rf "/Users/super/Documents/coinw/dex/localchain-test/config-v1/My Arbitrum L3 Chain/nitro"
$ copy -R  /tmp/mdbx-debug10 
"/Users/super/Documents/coinw/dex/localchain-test/config-v1/My Arbitrum L3 Chain/nitro"
$ cd /Users/super/Documents/coinw/dex/localchain-test
$ docker compose up

observe the log and this is the output (this way log no longer will wrrtien at docker.migrated.log)