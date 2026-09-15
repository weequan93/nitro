# DER-3180 — Blacklist ban types: development, test, and production release

## Release status and manifest

**Status: draft release note; deployment and activation are not verified.**
Promote one pinned release through development, test, production rehearsal,
and production. Record execution evidence in the DER-3180 ticket and separate
[release progress records](../release-progress/README.md) for each environment.

| Item | Release value |
| --- | --- |
| Jira ID | `DER-3180` |
| Source branch | `dev/feat-DER-3180` |
| Release owner / final source commit | To be recorded after committing the reviewed changes |
| Submodule commits | Pin all gitlinks, including changed precompile interfaces and explorer ABIs |
| Node image digest / validator image digest | To be built and recorded from the final source commit |
| Current / target WASM module root | Record per environment; build the target machine from the same release source |
| Target DeriwOS version | `6` |
| ArbOS change | No new ArbOS version introduced by DER-3180; verify earlier rollout prerequisites separately |
| Deployment / activation transaction hashes | Pending, separately for each environment |

Use the [custom DeriwOS deployment runbook](../deriw-l3-environment-deployment-runbook.md)
for environment discovery, builds, machine packaging, governance, sequencer
handover, and snapshots. Re-query its historical values before use. The
`deriw-master-legacy` release image and its recorded machine root are not
DER-3180 release artifacts. A legacy builder image can supply build tools;
the released node and replay machine must contain this release's source.

## Changes included

- One shared ban type per address across the existing **from** and **to** lists.
  Adding another type replaces the previous type in both directions.
- `1 = BanFlagAll`: existing top-level blacklist rejection. Historical entries
  without a stored flag continue to mean type `1`.
- `2 = ERC20/USDT transfer ban`: stored and queryable metadata only. DeriwOS
  does not enforce transfer restrictions for this type.
- New owner-only `addBlacklistTxFromWithFlag` / `addBlacklistTxToWithFlag` and
  matching removal methods accept `uint64` types `1` and `2`. Other values revert.
- New `getBlacklistBanFlag` and flag-filtered list/membership queries are exposed
  through owner and public blacklist precompiles. New selectors require DeriwOS 6.
- Legacy adds set type `1`. Legacy membership/list queries include all types.
  Legacy removal removes the requested direction regardless of type. Removing
  the final membership clears the stored flag.
- Node execution and validator replay share the exact `BanFlagAll` comparison.
  Consensus charges one additional storage read per unique checked address from
  DeriwOS 6. Earlier consensus versions retain their existing gas accounting.
- Admission now permits authorized emergency removal for historical self-bound
  owners, matching execution. New flag-aware removal selectors do not receive
  the legacy emergency-removal exception.

Ban-all retains the existing top-level scope: sender, effective subaccount
parent, and explicit destination, including the existing L1 alias checks.
Nested calls and addresses embedded in ERC20 calldata are outside that scope.
Existing funding, internal-transaction, recovery, and simulation exceptions remain.
See the [design and review record](../decisions/0005-blacklist-ban-types.md).

## Environment rollout

These are configured chain identities; confirm them against each live RPC.
Record each environment's parent RPC, Rollup, governance Safe/executor, active
versions, current machine root, image digests, and snapshot IDs before rollout.

| Stage | RPC / expected chain ID | Release checks |
| --- | --- | --- |
| Development | `https://rpc.dev.deriw.com` / `18417507517` | Complete prior DeriwOS prerequisites; deploy and activate 6; run the full acceptance matrix with disposable accounts; rehearse cleanup and recovery |
| Test | `https://rpc.test.deriw.com` / `2885` | Promote the same candidate; verify environment-specific routes and governance; run acceptance and validator replay across activation; record soak results |
| Production rehearsal | Production-like isolated environment | Rehearse snapshots, traffic draining, sequencer handover, root change, activation, and recovery before production |
| Production | `https://rpc.deriw.com` / `2886` | Promote the tested candidate by digest; roll canaries and standbys first; activate only after readiness checks; use dedicated accounts for approved smoke tests |

The recorded testnet checkpoint for the earlier rollout is in
[DER-2646 testnet progress](../release-progress/deriwos-testnet.md). Re-read live
state; this note does not assume that DeriwOS 2–5 or their prerequisites have
already completed. A direct jump to 6 also activates earlier semantics and must
not be treated as a blacklist-only upgrade. Complete and verify the earlier
rollout steps before this feature's activation.

### Apply this order in each environment

1. Pin the source/submodule commits, run release checks, and build node and
   machine artifacts. Record immutable image digests and verify machine roots.
2. Record current versions and any scheduled upgrade. Take recoverable snapshots
   and assign the rollout and recovery owners.
3. Deploy passive RPC/full-node canaries, compare finalized block hashes, and
   verify sync and RPC health. Drain traffic before replacing serving nodes.
4. Update validator/staker infrastructure with both current and candidate machines.
   Verify candidate replay against the existing rules before changing the official
   root. Update the parent Rollup root through its governance flow when required,
   then verify validation under that root while DeriwOS remains unchanged.
5. Update batch posters and sequencers. Upgrade standbys first, perform a
   controlled coordinator handover, and verify a single active sequencer,
   continuous block production, batch posting, and data availability.
6. Confirm every execution and validation role is compatible. Complete any
   earlier DeriwOS prerequisites through their separate rollout steps.
7. Through the environment's authorized chain-owner governance route, call
   `ArbOwner.scheduleDeriwOSUpgrade(6, activationTimestamp)`. The blacklist
   precompile's legacy scheduler cannot schedule version 6. Verify the scheduled
   version and timestamp, then verify active DeriwOS `6` after activation.
8. Run the acceptance matrix, compare finalized hashes and receipts across nodes,
   verify validator progress, and record soak duration/results and release sign-off.
   Agree the soak duration before each stage; promote only after it passes.

## Acceptance matrix

Run destructive/negative cases with disposable accounts in development and test.
Production smoke tests use designated accounts and leave their final state recorded.

| Test | Expected result |
| --- | --- |
| Historical entry after activation | Query resolves to `1`; existing rejection remains |
| Add type `1` using either API | Flag query returns `1`; sender/parent/destination roles follow existing rejection rules |
| Replace `1 → 2` in one direction | Shared flag becomes `2`; other direction membership remains; general blacklist rejection stops |
| Replace `2 → 1`, including legacy add | Shared flag becomes `1`; rejection resumes |
| Remove one of two memberships | Other membership and shared type remain |
| Remove final membership, then re-add | Query returns `0` after removal; re-add uses the newly requested type |
| Invalid type / unauthorized writer | Call reverts without changing membership or flag |
| Protected address promoted to type `1` | Promotion rejected; existing metadata preserved |
| Out-of-gas update / enclosing revert | Both membership and flag changes roll back |
| Authorized legacy emergency removal | Recovery works, including historical self-bound owners; true delegation and unauthorized callers gain no bypass |
| Delayed/retryable execution | Type `1` produces the expected failed execution; type `2` does not trigger blacklist rejection |
| Native versus validator replay | Same final global state for pre-activation, activation, and post-activation test blocks |

An admission-rejected transaction has no receipt. To test committed blacklist
failure, use the delayed-message test path in development/test. A successful
`eth_call` alone does not establish committed blacklist behavior.

The [Safe calldata helper](../../scripts/safe-proposals/README.md) supports
`--ban-flag 1` and `--ban-flag 2`, but its chain, Safe, and executor constants
are production-specific. Do not use its generated proposals for development
or test; encode calls with those environments' verified governance configuration.

## Validation evidence and remaining release gates

Current-branch review passed all six affected native Go package suites and all
nine Safe helper tests. Compiled blacklist ABIs match the explorer ABI files.

```sh
go test ./arbos/blacklist ./arbos/arbosState ./arbos ./precompiles ./execution/gethexec ./gethhook -count=1
node --test scripts/safe-proposals/blacklist-calldata.test.mjs
git diff --check
```

Before release, record:

- [ ] Final committed source and submodule revisions, reachable from release remotes.
- [ ] Node and validator image digests, current/target roots, and machine verification.
- [ ] Feature-specific WASM replay across DeriwOS 6 activation. This is not yet verified.
- [ ] Development acceptance and soak evidence.
- [ ] Test acceptance, replay, and soak evidence.
- [ ] Production rehearsal and snapshot recovery evidence.
- [ ] Production canary health, rollout evidence, activation receipt, smoke tests, and sign-off.

## Stop and recovery criteria

Stop promotion on unexpected type-2 rejection, type-1 enforcement failure,
membership/flag inconsistency, unauthorized mutation, finalized-hash or replay
divergence, stalled blocks, failed batch posting, or missing machine artifacts.

Before activation, cancel an unwanted pending upgrade through authorized
chain-owner governance and verify cancellation. Binary rollback requires
compatibility with the active versions, official machine root, and database;
revert traffic to a compatible healthy node using the environment runbook.

After DeriwOS 6 activation, do not roll back to a pre-6 binary or legacy replay
machine. Use a compatible fix-forward release. Do not restore an old snapshot
as a unilateral chain-state rollback. For incorrect blacklist policy, authorized
owners can replace the type or remove memberships; quarantined owners retain
the exact legacy removal recovery path. Record every corrective transaction.
