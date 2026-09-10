# v1.3.1.v3.10.0 testnet deployment

Target environment: Deriw testnet, including `rpc.test.deriw.com`.

Follow [common.md](common.md). This deployment is the full rehearsal for
staging and production; do not skip snapshots, sequencer handover, validator
pending validation, or rollback testing.

## Required values

| Value | Testnet value |
| --- | --- |
| L2 RPC | `https://rpc.test.deriw.com` |
| L2 chain ID | Obtain with `cast chain-id` |
| Parent RPC | Obtain from testnet secret/configuration |
| Rollup | Obtain from testnet chain-info |
| UpgradeExecutor | Verify from testnet deployment records and on chain |
| Chain-owner Safe | Verify on chain |
| Coordinator Redis | Secret reference only |
| Current WASM root | Query before deployment |
| Target WASM root | `0x121d685e2fdb0e3291592d6b90bd70d503951335d19d96455448eb7a14d17421` |

## Rollout order

1. Backup/passive RPC node.
2. Secondary public/internal RPC nodes, one pool member at a time.
3. Validator with staker disabled for its restart.
4. Primary RPC nodes, one pool member at a time.
5. Batch poster.
6. Non-active sequencer.
7. Controlled handover, followed by the former active sequencer.

Keep `node.block-validator.enable=true`. Before an official root change use:

```json
{
  "node": {
    "block-validator": {
      "enable": true,
      "current-module-root": "current",
      "pending-upgrade-module-root": "0x121d685e2fdb0e3291592d6b90bd70d503951335d19d96455448eb7a14d17421"
    },
    "staker": {
      "enable": false
    }
  }
}
```

Remove `pending-upgrade-module-root` if the current official root already equals
the target. Re-enable the staker only after validation is healthy.

## Acceptance gate

- [ ] All RPC endpoints agree on a finalized block hash.
- [ ] Gas-estimation cases pass on every RPC pool.
- [ ] Both sequencers reject blacklisted transactions.
- [ ] Subaccount blacklist cases pass.
- [ ] Sequencer handover completes without stalled blocks.
- [ ] New batches appear on the parent chain.
- [ ] AnyTrust data remains retrievable.
- [ ] Validator completes at least 1,000 blocks without validation errors.
- [ ] One node rollback from the new image has been rehearsed.
- [ ] Testnet soaks for at least 24 hours.

Do not upgrade DAC binaries in the same rehearsal unless DAC migration is
explicitly in scope. v3.10 renamed `daserver` to `anytrustserver`, so DAC needs
its own command/config compatibility test.

