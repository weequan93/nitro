# DER-2646 DeriwOS — testnet progress

## Checkpoint

**Resume by waiting for and verifying router activation before DeriwOS 2.**
The September 15 live check confirmed DeriwOS 1, no scheduled DeriwOS upgrade,
and a pending router configuration due September 17 at 01:45:34 Singapore time.
The active route is still empty. Node rollout completion remains unverified.

| Item | Recorded value |
| --- | --- |
| Last checked | 2026-09-15 09:46:35 Asia/Singapore (01:46:35 UTC) |
| Environment | Testnet |
| RPC / chain ID | `https://rpc.test.deriw.com` / `2885` |
| Documentation/source branch | `deriw-dev` |
| Saved proposal commit | `686bd5513fdaed1d3c6809d0cbad225d674b833b` |
| Deployed source commit / image digest / release tag | Not verified |
| Parent Rollup WASM root / validator readiness | Not verified |
| Active internal ArbOS / DeriwOS | `60` / `1` |
| Scheduled DeriwOS upgrade | `(0, 0, 0)` — none |
| Active router configuration | Zero router/canonical addresses, empty gateways, revision `0` |
| Pending router configuration | Revision `1`; activation timestamp `1789580734` (2026-09-17 01:45:34 Asia/Singapore / 2026-09-16 17:45:34 UTC) |
| Observation block number / hash | `141473217` / `0xdd50a7d0ea494c484f927468219213dcdacf100d23d304f37710842dc325b52b` |
| Release owner / change ticket | Not recorded |

This record tracks the custom DeriwOS rollout. The
[v1.3.1.v3.10.0 guide](../release-guides/v1.3.1.v3.10.0/README.md)
targets `deriw-master-legacy` and is a separate release flow.

## Guide and steps

Use the [DeriwOS deployment runbook](../deriw-l3-environment-deployment-runbook.md)
for the full procedure. Its August source pins and environment observations
are historical; reconcile them with the chosen release and live state before
execution.

| Step | Status | Evidence / next action |
| --- | --- | --- |
| Node rollout, machine availability, parent contracts/root, validator health | Not verified | User reported an upgrade; record deployed commits/digests and verify runbook sections 2 and 10–12 before further activation |
| Internal ArbOS 60 | Verified active | Live `getDeriwOSVersion()` returned `(60, 1)`; activation transaction not recorded |
| Transaction gas limit | Not verified | Read current value; follow runbook section 14 if a change is needed |
| Testnet router schedule | Verified pending on-chain; transaction hash not recorded | Live pending addresses match both saved manifests, but the live activation timestamp differs from both; reconcile with Safe history |
| Active router configuration | Not active; revision `0` | Wait for scheduled activation, then verify revision `1` and the approved addresses under runbook section 15 before DeriwOS 2 |
| DeriwOS 1 | Verified active | Live version read; acceptance-test evidence not recorded |
| DeriwOS 2 | Not active; not scheduled | Verify prerequisites and DeriwOS 1 acceptance, then follow section 16 and verify route enforcement |
| DeriwOS 3 | Not active; not scheduled | After DeriwOS 2 acceptance, activate separately and verify direct ETH withdrawal and retained route restrictions |
| DeriwOS 4 | Not active; not scheduled | After DeriwOS 3 acceptance, activate separately and verify owner-only scheduling behavior |
| DeriwOS 5 | Not active; not scheduled | Verify compatible nodes and subaccount signing clients, then activate separately and run authorization tests |
| Final acceptance and soak | Not verified | Record results and sign-off before promotion |

### Saved router proposals

- [2026-09-07 11:24:23 UTC manifest](../../scripts/safe-proposals/router-testnet-20260907T112423Z.json)
- [2026-09-07 16:47:43 UTC manifest](../../scripts/safe-proposals/router-testnet-20260907T164743Z.json)

Both files were committed in `686bd5513` on September 10. The later file
includes custom Safe contract-network addresses and a different activation
timestamp. Neither file contains an execution receipt. A filename timestamp
or elapsed activation time does not establish that a proposal was executed.

### Pending route verified on-chain

| Role | Address |
| --- | --- |
| Deriw router | `0x675a17efd9e9ce8b7a769fb3df88f9c102190de0` |
| Canonical gateway router | `0x01574141de98d405c96c53b4a61e5c2d3893bfc3` |
| Approved token gateway | `0xfa5dd7ead86a1b5c95ff9b25d0e763d0f7df364f` |

At block `141473217`, `getScheduledDeriwRouterConfig()` returned these
addresses, revision `1`, and activation timestamp `1789580734`. This proves a
schedule exists on-chain, but does not identify its transaction or establish
bridge health. The live timestamp differs from both saved proposal timestamps
(`1789471457` and `1789429652`); use the live schedule for the checkpoint.

The configuration activates at the first block boundary whose timestamp meets
the schedule, unless cancelled or replaced. Router-only send enforcement is
separately gated on DeriwOS 2; it is not enabled at the observed DeriwOS 1.

## Resume here

1. Re-query versions and both scheduled/active router configurations using
   the read-only commands below. After September 17 at 01:45:34 Singapore time,
   verify the active revision and addresses and that the pending route cleared.
   Capture the observation time and block; the schedule could change meanwhile.
2. Reconcile both saved manifests with Safe proposal/execution history. Record
   the actual transaction hash and receipt if executed; do not resubmit solely
   because a manifest exists.
3. Verify the deployed release, validator readiness, gas limit, active route,
   and DeriwOS 1 acceptance evidence. Complete any missing prerequisites using
   the runbook.
4. Follow runbook section 16 for separate DeriwOS 2, 3, 4, and 5 activations.
   Use the approved testnet Safe → UpgradeExecutor → ArbOwner path; record
   each schedule, execution receipt, activation observation, and test result.

### Read-only checkpoint commands

```bash
date -u '+%Y-%m-%dT%H:%M:%SZ'
L3_RPC='https://rpc.test.deriw.com'
OWNER_PUBLIC='0x000000000000000000000000000000000000006b'
cast chain-id --rpc-url "$L3_RPC"
CHECKPOINT_BLOCK=$(cast block-number --rpc-url "$L3_RPC")
cast block --rpc-url "$L3_RPC" "$CHECKPOINT_BLOCK" --json
cast call --rpc-url "$L3_RPC" --block "$CHECKPOINT_BLOCK" "$OWNER_PUBLIC" \
  'getDeriwOSVersion()(uint64,uint64)'
cast call --rpc-url "$L3_RPC" --block "$CHECKPOINT_BLOCK" "$OWNER_PUBLIC" \
  'getScheduledDeriwOSUpgrade()(uint64,uint64,uint64)'
cast call --rpc-url "$L3_RPC" --block "$CHECKPOINT_BLOCK" "$OWNER_PUBLIC" \
  'getDeriwRouterConfig()(address,address,address[],uint64)'
cast call --rpc-url "$L3_RPC" --block "$CHECKPOINT_BLOCK" "$OWNER_PUBLIC" \
  'getScheduledDeriwRouterConfig()(address,address,address[],uint64,uint64)'
```

## History

| Date | Action or observation | Evidence | Follow-up |
| --- | --- | --- | --- |
| 2026-09-07 (UTC filenames) | Two router proposal manifests prepared | Saved JSON files above | Submission and execution remain unverified |
| 2026-09-10 (Asia/Singapore) | Proposal files and release guides committed on `deriw-dev` | `686bd5513` | Retain as preparation evidence |
| 2026-09-15 (Asia/Singapore) | Read-only live RPC check | `eth_chainId = 0xb45`; ArbOwnerPublic selector `0xb9c863dd` returned words `0x3c`, `0x1`; selector `0x051109a5` returned three zero words | Verify router state and rollout prerequisites before scheduling DeriwOS 2 |
| 2026-09-15 09:46:35 (Asia/Singapore) | Versions and router state read at fixed block `141473217` | ArbOS/DeriwOS `(60, 1)`; scheduled version `(0, 0, 0)`; active route revision `0`; pending route revision `1`, timestamp `1789580734`, addresses above | Wait for and verify router activation; reconcile the live timestamp with Safe execution history before DeriwOS 2 |
