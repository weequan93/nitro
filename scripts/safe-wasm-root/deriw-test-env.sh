#!/usr/bin/env sh

# Deriw Testnet L2 identity endpoint.
export L2_CHAIN_ID=2885
export L2_RPC='https://rpc.test.deriw.com'

# Governance contracts and Safe transactions live on the parent chain.
export CHAIN_ID=421614
export L1_RPC='https://rpc-arbitrum-sepolia.deriw.com'
export ROLLUP='0xb6a39f55E4C4397FE799BeDCc16fFa895950CFF9'
export UPGRADE_EXECUTOR='0x678815f2c63466f557024d8cce25baeeb4a23359'
export WASM_ROOT='0x121d685e2fdb0e3291592d6b90bd70d503951335d19d96455448eb7a14d17421'

# Direct Deriw Testnet governance used for ArbOS scheduling.
export DERIW_CHAIN_ID=2885
export DERIW_RPC='https://rpc.test.deriw.com'
export DERIW_SAFE='0xE5C8e6dAbE8dA8D90F0AE3d4543E930833A0e9Ec'
export DERIW_UPGRADE_EXECUTOR='0xAc3516eF1E3658887198D192Cb0D7EcA07604943'
export EXPECTED_DERIW_SAFE_OWNERS='0xa1698F44D70632BfE448804378DA373C55eE8476'
export EXPECTED_DERIW_SAFE_THRESHOLD=1
export ARBOS_TARGET_VERSION=60
export ARBOS_PUBLIC_VERSION_OFFSET=55
export MIN_ACTIVATION_LEAD_SECONDS=900
