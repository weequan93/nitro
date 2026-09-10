# v1.3.1.v3.10.0 production deployment

This runbook upgrades production from `release/v1.2.3.v3.5.1` to the release
artifact pinned in [README.md](README.md). Complete development, testnet, and
staging first. Follow the [shared procedure](common.md) for commands used on
each node.

## Production inventory and order

| Order | Host | Address | Role | Traffic/action |
| ---: | --- | --- | --- | --- |
| 1 | `ins-r3arxjcy` | `10.1.4.16:2026` | public node backup | Keep drained; first canary |
| 2 | `ins-ph3x5xew` | `10.1.2.6:2026` | internal node backup | Keep drained; second canary |
| 3 | `ins-mes9lrjg` | `10.1.2.8:2026` | internal node 2 | Drain, deploy, restore |
| 4 | `ins-hzjanyco` | `10.1.2.15:2026` | public node 2 | Drain, deploy, restore |
| 5 | `ins-dq3hm30k` | `10.1.2.17:2026` | internal priority node 2 | Drain, deploy, restore |
| 6 | `ins-2rgv3dc6` | `10.1.4.10:2026` | validator | Disable staker for restart |
| 7 | `ins-jn7z61e4` | `10.1.4.7:2026` | internal node 1 | Drain, deploy, restore |
| 8 | `ins-noxoclc8` | `10.1.4.11:2026` | public node 1 | Drain, deploy, restore |
| 9 | `ins-f8sifl9q` | `10.1.4.13:2026` | internal priority node 1 | Drain, deploy, restore |
| 10 | `ins-7gmh8hse` | `10.1.2.13:2026` | batch poster | Pause, deploy, resume |
| 11 | standby of the following pair | | sequencer | Deploy non-active first |
| 12 | active of the following pair | | sequencer | Handover, then deploy |
| | `ins-hx0bleem` | `10.1.2.4:2026` | sequencer 2 | Determine state from Redis |
| | `ins-l26kkmkg` | `10.1.4.15:2026` | sequencer 1 | Determine state from Redis |
| Separate window | `ins-8c9d4g32` | `10.1.2.12:2026` | DAC mirror | Leave unchanged initially |
| Separate window | `ins-814j00ki` | `10.1.4.12:2026` | DAC server | Leave unchanged initially |

Never remove all members of the public, internal, or priority pool at once.

## Production preflight gate

Complete this table in the change ticket; do not use development or testnet
values.

| Item | Production value |
| --- | --- |
| L2 RPC and chain ID | `REQUIRED` |
| Parent RPC and chain ID | `REQUIRED` |
| Rollup | `REQUIRED` |
| UpgradeExecutor | `REQUIRED` |
| Chain-owner Safe | `REQUIRED` |
| Current official WASM root | `REQUIRED` |
| Current ArbOS version/schedule | `REQUIRED` |
| Release image digest | `REQUIRED` |
| Previous image digest | `REQUIRED` |
| Coordinator Redis secret reference | `REQUIRED` |
| Sequencer 1 exact `my-url` | `REQUIRED` |
| Sequencer 2 exact `my-url` | `REQUIRED` |
| Snapshot/change ticket | `REQUIRED` |

## Observed production preflight

Observed on 2026-09-03 at L2 block `118529105`:

| Check | Observed value | Interpretation |
| --- | --- | --- |
| L2 chain ID | `2886` | Production RPC must continue returning this value |
| `ArbSys.arbOSVersion()` | `87` | Exposed value `55 + 32`; internal ArbOS version is 32 |
| `getScheduledUpgrade()` | `0, 0` | No ArbOS upgrade is currently scheduled |
| Rollup `wasmModuleRoot()` | `0x767c9a47cced7ccc3bf419a7efdd9ffb0f23a5dba42f30f3de64f32e2f82c55f` | Current official/legacy root |
| Release target root | `0x121d685e2fdb0e3291592d6b90bd70d503951335d19d96455448eb7a14d17421` | Future v3.10 root; not active yet |

Both the current `0x767c...` root and target `0x121d...` root were found
in the verified release image. This is necessary for the split legacy/new
validator transition, but the validator still has to prove live current and
pending validation before any governance root change.

Go only when:

- Git and submodule revisions match the manifest.
- The registry digest returns the expected binary version and target root.
- The current official production root is present in the image's legacy/new
  machine directories.
- Staging used the same digest and passed the full rehearsal.
- Previous images and disk snapshots are available.
- Release, rollback, network, database, and Safe owners are present.

## Canary and RPC rollout

For each node, use the shared backup, deploy, log, synchronization, and
finalized-block comparison procedure. Initially change only the image digest.

Observe each backup canary for 30–60 minutes. Observe each normal RPC member for
at least 10–15 minutes before touching the next member. Check error rate,
latency, block height, peers, database logs, and a finalized block hash.

Gas estimation is RPC-local. During the rolling window, pools may contain old
and new behavior, so run the estimate test matrix against every pool member
before restoring it to traffic.

## Validator

Before restarting `ins-2rgv3dc6`, configure:

```json
{
  "node": {
    "staker": {
      "enable": false
    },
    "block-validator": {
      "enable": true,
      "current-module-root": "current",
      "pending-upgrade-module-root": ""
    }
  }
}
```

If the current official root differs from the target and a later root change is
approved, set the pending value to:

```text
0x121d685e2fdb0e3291592d6b90bd70d503951335d19d96455448eb7a14d17421
```

Do not set `current-module-root` to `latest` before governance changes the
official root. Require at least 1,000 validated blocks without missing-machine,
state, or module-root errors. For a binary-only soak, re-enable the staker after
current-root validation is healthy; disable it again for any later root-change
window.

## Batch poster

Before stopping `ins-7gmh8hse`, record the latest posted batch, backlog, parent
transaction, wallet balance, and current image. Pause only the poster service,
deploy the digest, then confirm new parent-chain receipts and decreasing
backlog. Preserve existing secret injection; never place its key in shell
history or this guide.

## Sequencers

Do not assume sequencer 1 is active. Query the coordinator:

```bash
redis-cli -u "$REDIS_URL" GET coordinator.chosen
redis-cli -u "$REDIS_URL" GET coordinator.priorities
```

Confirm both `node.seq-coordinator.my-url` values are exact, routable, present
in `coordinator.priorities`, and contain no trailing spaces. Ensure Compose has
at least `stop_grace_period: 90s`.

1. Deploy the digest to the non-chosen sequencer.
2. Wait until it is synchronized and ready for coordinator ownership.
3. Gracefully stop the chosen sequencer.
4. Watch `coordinator.chosen` move to the upgraded sequencer.
5. Confirm block production and signed-transaction submission continue.
6. Deploy the former leader.
7. Restore the intended priority order and observe for at least 30 minutes.

Do not manually delete `coordinator.chosen` during a normal handover. Blacklist
enforcement is considered deployed only after both sequencers use the new
digest.

## DAC scope

Leave `ins-8c9d4g32` and `ins-814j00ki` unchanged for this rollout. These binary
fixes do not require a DAC upgrade, and v3.10 renames `daserver`/`datool` to
`anytrustserver`/`anytrusttool`. Migrate DAC in a separate window after checking
the keyset, quorum, signing role, entrypoint, and configuration compatibility.
Never stop both DAC machines together.

## Production acceptance and soak

- [ ] All Nitro services use the recorded digest except intentionally deferred
      DAC services.
- [ ] Every RPC pool agrees on finalized block hashes.
- [ ] Estimate-gas tests pass on every public/internal/priority path.
- [ ] Both sequencers reject disposable blacklist cases.
- [ ] Parent/child subaccount blacklist cases pass.
- [ ] Normal native, ERC-20, contract, and subaccount transactions pass.
- [ ] Validator completes at least 1,000 clean blocks.
- [ ] Sequencer handover completes without stalled production or split brain.
- [ ] Batch poster transactions succeed and backlog is healthy.
- [ ] AnyTrust data remains available.
- [ ] Monitoring shows no sustained error, latency, memory, disk, or peer
      regression.

Soak the binary-only release for at least 24 hours. The blacklist and
estimate-gas release is complete at this point; no governance transaction is
required.

## Parent-chain contract version and upgrade

The current node and ArbOS versions do not reveal the deployed parent-chain
contract versions. Run the [parent-contract procedure](parent-contracts.md)
after the binary rollout and before any required WASM-root or ArbOS change.

If the release scope is only the blacklist and estimate-gas fixes, record the
deployed contract-version report and skip the contract upgrade. If the version
tool identifies a required upgrade, execute it as a separate Safe change with
its own simulation, approval, pause, verification, and rollback plan.

## Optional protocol change: WASM root

Only continue if a separately approved change requires it and the current root
does not already equal the target. First prove pending validation on live
blocks. Verify production `ROLLUP`, `UPGRADE_EXECUTOR`, and chain-owner Safe
independently.

```bash
export TARGET_WASM_ROOT="0x121d685e2fdb0e3291592d6b90bd70d503951335d19d96455448eb7a14d17421"
export ROLLUP="0xPRODUCTION_ROLLUP"

export ROOT_CALL="$(cast calldata \
  'setWasmModuleRoot(bytes32)' "$TARGET_WASM_ROOT")"

export SAFE_DATA="$(cast calldata \
  'executeCall(address,bytes)' "$ROLLUP" "$ROOT_CALL")"

echo "$ROOT_CALL"
echo "$SAFE_DATA"
```

Create a Safe transaction with production UpgradeExecutor as target, zero
value, and `SAFE_DATA` as data. Do not use a raw private key. Verify afterwards:

```bash
cast call --rpc-url "$PARENT_RPC" "$ROLLUP" \
  'wasmModuleRoot()(bytes32)'
```

Skip this transaction if the official root is already the target.

## Optional protocol change: ArbOS 60

Schedule this only as another approved change after all nodes and validators
support it. This branch uses the standard calls:

```bash
cast call --rpc-url "$L2_RPC" \
  0x000000000000000000000000000000000000006B \
  'getScheduledUpgrade()(uint64,uint64)'

export ACTIVATION_TIMESTAMP="$(( $(date +%s) + 4*60*60 ))"
export ARBOS_CALL="$(cast calldata \
  'scheduleArbOSUpgrade(uint64,uint64)' 60 "$ACTIVATION_TIMESTAMP")"
```

The L2 transaction targets ArbOwner `0x70` directly only if the Safe is a chain
owner. Otherwise wrap it through the registered L2 UpgradeExecutor. After
activation, `ArbSys.arbOSVersion()` reports `115` for internal ArbOS 60. Do not
roll back to v1.2.3 after activation.

Do not use `scheduleDeriwOSUpgrade` for this legacy branch. Do not execute a
parent-chain proxy/contract upgrade merely to deploy the blacklist and
estimate-gas fixes.

## Final release record

After the soak, tag the tested source commit:

```bash
git tag -a release/v1.3.1.v3.10.0 \
  7f7d4033d7f1c5e5f56fcc149f6ff60f388e459d \
  -m 'Deriw Nitro v1.3.1 / Nitro v3.10.0 blacklist and estimate-gas fixes'

git push origin release/v1.3.1.v3.10.0
```

Record the source/submodule revisions, image and previous-image digests,
machine roots, config hashes, snapshots, deployment times, functional results,
sequencer handover, validator result, and any Safe transaction hashes in the
release ticket.
