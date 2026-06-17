## Run full node from source


### Hardware

- 4 CPU cores
- 8 GB RAM
- 13T for running, 26T for migration process
- 100 Mbps download, 20 Mbps upload broadband connection


### Requred tools Linux: 

 
go version      # repo wants Go 1.25
node -v         # use 20.19+ or 22.12+
yarn -v
cargo --version
rustc --version
forge --version
 
If yarn missing:
 
corepack enable
corepack prepare yarn@1.22.22 --activate
 
cargo install --locked cbindgen --version 0.29.0


### Build executable

- pwd = /data

- $ git clone https://github.com/deriwfi/deriw-chain
- $ cd deriw-chain
- change branch to latest release (find tag release/v**.** with larger number)
- $ git status
- $ git submodule update --init --recursive
- $ make contracts
- $ make build

### Data migrate 

- example $CHAIN = /data/deriw-chain/config/Deriw Chain
- Download latest snashot at https://deriw-chain-public.oss-ap-southeast-1.aliyuncs.com/snapshot/Deriw%20Chain%20Testnet/archive.2026-06-12.tar.gz
- $ tar -xzf archive.2026-06-12.tar.gz -C "$CHAIN/nitro"
- structure need to be like
    /data/deriw-chain/config/Deriw Chain/
    └── nitro/
        └── l2chaindata/
            ├── CURRENT
            ├── MANIFEST-...
            ├── *.sst
            └── ancient/

### Configuration
- at /data/deriw-chain/config/nodeConfig.json
- 

## Run Build

- at /data/deriw-chain

-  $ ./target/bin/nitro \
    --conf.file /data/deriw-chain/config/nodeConfig.json \
    --persistent.chain "/data/deriw-chain/config/My Arbitrum L3 Chain"
