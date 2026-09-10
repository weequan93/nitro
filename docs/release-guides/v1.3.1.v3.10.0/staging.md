# v1.3.1.v3.10.0 staging deployment

Target environment: production-like Deriw staging.

Follow [common.md](common.md). Use the exact image digest intended for
production. A staging rebuild does not qualify, even from the same commit.

## Staging requirements

- Inventory every Nitro, validator, poster, sequencer, RPC, DAC, Redis, and load
  balancer component.
- Use staging-specific RPCs, addresses, wallets, Redis, Safe, and Rollup.
- Match production Compose entrypoints, ports, persistent-volume layout,
  resource limits, and shutdown grace periods.
- Verify the current staging root is included in the release image.
- Record the previous image digest and snapshot for every stateful role.

## Rehearsal

1. Upgrade backup/passive RPC nodes.
2. Upgrade one secondary member of every RPC pool.
3. Upgrade validator with staker disabled for restart.
4. Upgrade remaining RPC members without removing all members of any pool.
5. Pause, upgrade, and resume the poster; verify parent-chain receipts.
6. Upgrade the non-active sequencer.
7. Gracefully hand over coordinator ownership and upgrade the former leader.
8. Run the shared blacklist, subaccount, and estimate-gas test matrix.
9. Roll one non-sequencer node back, including snapshot restoration if needed.
10. Redeploy that node with the release digest.
11. Soak for at least 24 hours.

Before sequencer handover:

```bash
redis-cli -u "$REDIS_URL" GET coordinator.chosen
redis-cli -u "$REDIS_URL" GET coordinator.priorities
```

Every `node.seq-coordinator.my-url` must exactly match its priority entry and
must not contain trailing spaces. Use a Compose stop grace period of at least
90 seconds.

## Production approval evidence

- Exact image digest tested.
- Staging source revisions and submodule pins.
- Start/end block and matching finalized block hashes.
- Validator result across at least 1,000 blocks.
- Sequencer handover duration and logs.
- Poster transaction hashes.
- Blacklist/subaccount and estimate-gas results.
- Rollback time and result.
- Known warnings accepted for production.

Do not perform a staging WASM-root or ArbOS operation merely to deploy the two
binary fixes. If protocol activation is being rehearsed, execute it as a
separate approved change and document the point after which old binaries are
no longer safe.

