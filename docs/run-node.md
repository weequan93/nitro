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
```

{
  "chain": {
    "info-json": "[{\"chain-id\":2885,\"parent-chain-id\":421614,\"parent-chain-is-arbitrum\":true,\"chain-name\":\"Deriw Testnet\",\"chain-config\":{\"homesteadBlock\":0,\"daoForkBlock\":null,\"daoForkSupport\":true,\"eip150Block\":0,\"eip150Hash\":\"0x0000000000000000000000000000000000000000000000000000000000000000\",\"eip155Block\":0,\"eip158Block\":0,\"byzantiumBlock\":0,\"constantinopleBlock\":0,\"petersburgBlock\":0,\"istanbulBlock\":0,\"muirGlacierBlock\":0,\"berlinBlock\":0,\"londonBlock\":0,\"clique\":{\"period\":0,\"epoch\":0},\"arbitrum\":{\"EnableArbOS\":true,\"AllowDebugPrecompiles\":false,\"DataAvailabilityCommittee\":true,\"InitialArbOSVersion\":32,\"GenesisBlockNum\":0,\"MaxCodeSize\":24576,\"MaxInitCodeSize\":49152,\"InitialChainOwner\":\"0xa1698F44D70632BfE448804378DA373C55eE8476\"},\"chainId\":2885},\"rollup\":{\"bridge\":\"0x6d8726867A89908918F35D6985D7e628347FB59b\",\"inbox\":\"0xAcb00b245154679E37E478a752188574834fFc29\",\"sequencer-inbox\":\"0xFda8daF595b871E85a4C085D9F81eF0E42b62c14\",\"rollup\":\"0xb6a39f55E4C4397FE799BeDCc16fFa895950CFF9\",\"validator-utils\":\"0x7C100c97a54e2D309a194752Df2f66922A802be3\",\"validator-wallet-creator\":\"0xFAd2C6Cb969Ab7B18d78BD63e512b650bb70B570\",\"deployed-at\":120284320}}]",
    "name": "Deriw Testnet"
  },
  "parent-chain": {
    "connection": {
      "url": "https://rpc-arbitrum-sepolia.deriw.com"
    }
  },
  "http": {
    "addr": "0.0.0.0",
    "port": 8449,
    "vhosts": [
      "*"
    ],
    "corsdomain": [
      "*"
    ],
    "api": [
      "eth",
      "net",
      "web3",
      "arb",
      "debug",
      "txpool"
    ]
  },
  "node": {
    "sequencer": false,
    "delayed-sequencer": {
      "enable": false,
      "use-merge-finality": false,
      "finalize-distance": 1
    },
    "staker": {
      "enable": false
    },
    "data-availability": {
      "enable": true,
      "sequencer-inbox-address": "0xFda8daF595b871E85a4C085D9F81eF0E42b62c14",
      "parent-chain-node-url": "https://rpc-arbitrum-sepolia.deriw.com"
    }
  },
  "execution": {
    "forwarding-target": "null",
    "caching": {
      "archive": true
    }
  }
}
```

## Run Build

- at /data/deriw-chain

-  $ ./target/bin/nitro \
    --conf.file /data/deriw-chain/config/nodeConfig.json \
    --persistent.chain "/data/deriw-chain/config/My Arbitrum L3 Chain"
