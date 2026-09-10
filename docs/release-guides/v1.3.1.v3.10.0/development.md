# v1.3.1.v3.10.0 development deployment

Target environment: development chain, including `rpc.dev.deriw.com`.

Follow the [shared procedure](common.md), with these environment-specific
gates.

## Required values

Record the development values in the change ticket:

| Value | Development value |
| --- | --- |
| L2 RPC | `https://rpc.dev.deriw.com` |
| L2 chain ID | Obtain with `cast chain-id` |
| Parent RPC | Obtain from deployment configuration |
| Rollup | Obtain from development chain-info |
| Chain-owner Safe | Verify on chain; do not infer from another environment |
| UpgradeExecutor | Verify on chain |
| Current WASM root | Query before deployment |
| Target WASM root | `0x121d685e2fdb0e3291592d6b90bd70d503951335d19d96455448eb7a14d17421` |

## Procedure

1. Deploy by digest to a non-sequencing development node.
2. Compare its finalized block hash with the active node.
3. Run all shared estimate-gas and blacklist tests.
4. Upgrade the standby sequencer.
5. Exercise a controlled coordinator handover.
6. Upgrade the former active sequencer.
7. Verify batch posting and AnyTrust retrieval.
8. Soak for at least one hour and record results.

Development may use disposable addresses for negative tests, but tests must
still remove blacklist entries afterwards. Never reuse development governance
addresses or calldata in testnet, staging, or production.

WASM-root and ArbOS operations are unnecessary for validating this release's
blacklist and gas-estimation fixes. If development is used to rehearse those
operations, record them as a separate protocol-upgrade test.
