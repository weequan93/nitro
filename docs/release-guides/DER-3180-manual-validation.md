# DER-3180 — Code summary and development validation

## Scope and current evidence

Release source: `625e9205f` on `dev/feat-DER-3180` (feature commit `94a22d9f6`).
The operator reported development running ArbOS **60**, DeriwOS **6**, with no
pending upgrade. The parent root update transaction was
`0x3a9d80dd8de0ee04c5d05f78a5fb97fec42c5c6e0ab094d4e28ea439c2030898`.
The official WASM root is
`0x3691fa25daae0d0c32e86493e36d392ca544b0039575d134a84584c11d1f7a8d`.
The reported image is
`fuhua-container.tencentcloudcr.com/deriw/deriw:dev-v1.3.1.v3.10.0-625e9205fca5`.

Supplied validator logs show progress with this root. They do not identify the
DeriwOS 6 activation block or demonstrate replay of the flag tests below.
Development activation is evidenced; feature acceptance and post-activation
replay are still to be recorded. Test and production are not verified here.
The activation transaction hash and immutable image digest remain to be recorded.

## What changed

| Area | Result | Main files |
| --- | --- | --- |
| Storage | Two existing address lists, one shared `uint64` type per address; no extra list per flag | `arbos/blacklist/blacklist.go` |
| Types | `1 = BanFlagAll`; `2 = ERC20/USDT transfer metadata`; values are enums, not bitmasks | `arbos/blacklist/blacklist.go` |
| Compatibility | Historical unflagged members resolve to 1; unlisted addresses resolve to 0; legacy adds set 1 | `arbos/blacklist/blacklist.go`, `precompiles/DeriwBlacklist.go` |
| API | Flag-aware add/remove, filtered lists and membership, shared-flag getter; new selectors gated at DeriwOS 6 | `precompiles/DeriwBlacklist*.go`, `contracts/src/precompiles/DeriwBlacklist*.sol` |
| Admission | Reject only exact flag 1 for sender, effective parent, or explicit recipient | `execution/gethexec/blacklist_pre_checker.go` |
| Committed execution/replay | Same exact flag-1 decision; deterministic third storage read per unique checked address at version 6; historical gas preserved | `arbos/deriw_blacklist_consensus.go`, `arbos/tx_processor.go` |
| Recovery | Authorized legacy self-bound owners regain the same emergency removal admission path as execution | `execution/gethexec/blacklist_pre_checker.go` |
| Integration | Updated Solidity interfaces, explorer ABIs, Safe helper and regression coverage | See release note and design record |

`BanType` is the gas-metered storage getter. `BanTypeFree` resolves the same type
without charging through that getter; consensus explicitly charges the reads.
`LegacyBanTypeFree` preserves the old membership interpretation and two-read
cost for historical execution. The obsolete `IsQuarantinedFree` helper was removed.

### Rules the tester must understand

- Flag 2 is stored only. DeriwOS does not inspect token calldata or enforce an
  ERC20/USDT restriction for it. Otherwise-valid token transfers should work.
- The shared flag changes for **both** lists, but an add changes membership only
  by adding its requested direction. Existing membership in the other list stays.
- Under the existing consensus rule, membership in **either** list with flag 1
  blocks an address in **any checked top-level role**. A from-only entry can
  block receipt of an ordinary transaction; a to-only entry can block sending.
- Legacy `isBlacklistTxFrom/To` return membership for all types. `true` alone
  does not establish that DeriwOS will reject a transaction.
- Flag-aware removal requires the matching type and direction. The last removal
  clears the flag. A legacy removal ignores the type but still requires membership.
- Ban-all retains its existing scope: signed sender, effective subaccount parent,
  explicit top-level destination, and applicable original/aliased L1 senders.
  Nested calls and addresses embedded only in ERC20 calldata are outside this scope.
- Non-mutating simulation, funding-only deposit/retryable creation, internal
  transactions, and narrowly defined emergency removal keep their exceptions.

## 1. Test setup

Use development only and two disposable, funded, ordinary EOA accounts A and B.
They must have no subaccount relationship, gasless status or governance role.
Do not use a sequencer, Safe owner, fee account, executor, or system address as A/B.
Use encrypted Foundry keystore aliases for their transaction signing.

```bash
L3_RPC="https://rpc.dev.deriw.com"
BL_PUBLIC="0x00000000000000000000000000000000000007eb"
BL_OWNER="0x00000000000000000000000000000000000007ec"
L3_SAFE="0x5f1B197A82fC1148A02Ea55B3BEF529f78D64151"
L3_EXECUTOR="0xB5B4d7f7a32D86fF3bc270B864c7c06CE6F0BD78"
A_ACCOUNT="<disposable A keystore alias>"
B_ACCOUNT="<disposable B keystore alias>"
A=$(cast wallet address --account "$A_ACCOUNT")
B=$(cast wallet address --account "$B_ACCOUNT")

cast chain-id --rpc-url "$L3_RPC"
cast call --rpc-url "$L3_RPC" "$BL_PUBLIC" \
  "getDeriwOSVersion()(uint64,uint64)"
cast call --rpc-url "$L3_RPC" "$BL_PUBLIC" \
  "getScheduledDeriwOSUpgrade()(uint64,uint64,uint64)"
```

Expected: chain `18417507517`, versions `60 / 6`, schedule `0 / 0 / 0`.
If the chain differs, stop. The L3 Safe differs from the Arbitrum Sepolia parent Safe.

Read helpers (output in order: shared flag, from, to, from-1, from-2, to-1, to-2):

```bash
bl_read() {
  cast call --rpc-url "$L3_RPC" "$BL_PUBLIC" "getBlacklistBanFlag(address)(uint64)" "$1"
  cast call --rpc-url "$L3_RPC" "$BL_PUBLIC" "isBlacklistTxFrom(address)(bool)" "$1"
  cast call --rpc-url "$L3_RPC" "$BL_PUBLIC" "isBlacklistTxTo(address)(bool)" "$1"
  cast call --rpc-url "$L3_RPC" "$BL_PUBLIC" "isBlacklistTxFromWithFlag(address,uint64)(bool)" "$1" 1
  cast call --rpc-url "$L3_RPC" "$BL_PUBLIC" "isBlacklistTxFromWithFlag(address,uint64)(bool)" "$1" 2
  cast call --rpc-url "$L3_RPC" "$BL_PUBLIC" "isBlacklistTxToWithFlag(address,uint64)(bool)" "$1" 1
  cast call --rpc-url "$L3_RPC" "$BL_PUBLIC" "isBlacklistTxToWithFlag(address,uint64)(bool)" "$1" 2
}
bl_read "$A"
bl_read "$B"
```

Both must initially return `0` and six `false` values. Fund both before banning
either, then establish successful 1-wei transfers A→B and B→A as the baseline.

## 2. How to apply each management operation

This helper **simulates and prints calldata only**; it never sends a transaction.
Use it for every operation in the matrix, one operation at a time.

```bash
bl_prepare() {
  local inner outer
  inner=$(cast calldata "$@") || return 1
  outer=$(cast calldata "executeCall(address,bytes)" "$BL_OWNER" "$inner") || return 1
  cast call --rpc-url "$L3_RPC" --from "$L3_SAFE" \
    "$L3_EXECUTOR" --data "$outer" || return 1
  echo "Safe: $L3_SAFE"
  echo "To: $L3_EXECUTOR"
  echo "Value: 0; operation: CALL; chain: 18417507517"
  echo "Data: $outer"
}

# Example: prepare the first positive write in the matrix.
bl_prepare "addBlacklistTxFromWithFlag(address,uint64)" "$A" 1
```

In `https://safe.dev.deriw.com`, use the L3 Safe above to create a custom
transaction with the printed destination/data, value 0 and operation CALL.
Sign/execute using an L3 Safe owner. The path is
`L3 Safe → L3 UpgradeExecutor.executeCall → blacklist owner precompile 0x07ec`.
The executor is a chain owner; the blacklist wrapper permits a chain owner or
blacklist owner. Merely owning the Safe does not authorize an EOA to call 0x07ec.

Wait for a successful Safe execution and check `OwnerActs` at 0x07ec with the
expected selector/arguments, then run `bl_read "$A"`. Receipt status 1 by itself
is insufficient if a wrapper could catch an inner failure. Check state too.
For an expected-revert row, the helper should fail at simulation; do not sign it.
Simulation is useful for API errors, but does not prove committed rollback.

## 3. Ordinary transaction probes

Run both after **each** successful matrix operation. Send sequentially and retain
receipts/errors. Explicit gas avoids relying on `eth_estimateGas` as a blacklist test.

```bash
cast send --rpc-url "$L3_RPC" --chain 18417507517 \
  --account "$A_ACCOUNT" --gas-limit 100000 --value 1wei "$B"

cast send --rpc-url "$L3_RPC" --chain 18417507517 \
  --account "$B_ACCOUNT" --gas-limit 100000 --value 1wei "$A"
```

- **Allow** means a mined successful receipt and the expected transfer, provided
  balances, fees, nonce, and other policies are valid. An unrelated error is not a pass.
- **Reject** means the RPC/sequencer reports blacklist rejection, commonly
  `sender / receiver blacklisted`, and does not include the transaction.
  Compare `cast nonce --rpc-url "$L3_RPC" "$A"` (or B) before/after and balances:
  admission rejection must not consume nonce/gas or transfer value.
- If an RPC forwards a transaction before eventual rejection, inspect inclusion
  and receipts on the sequencer; a transaction hash alone is not successful execution.
- `cast call`/`eth_call` may succeed for a banned address because non-mutating
  simulation is exempt. Do not count that as enforcement success or failure.

## 4. Core acceptance matrix

Each row starts from the previous row's state. `F/T` means legacy from/to membership.
At every row verify all seven read results and the expected filtered lists.
For every listed address, a flag-specific membership is true only when that
direction is present **and** its requested flag equals the shared flag.

| Step | Management operation passed to `bl_prepare` | Expected flag; F/T | A→B and B→A |
| --- | --- | --- | --- |
| 0 | No write; baseline | `0; false/false` | Allow both |
| 1 | `"addBlacklistTxFromWithFlag(address,uint64)" "$A" 1` | `1; true/false` | Reject both (union rule) |
| 2 | Repeat step 1 | Same; no duplicate list member | Reject both |
| 3 | `"addBlacklistTxToWithFlag(address,uint64)" "$A" 2` | `2; true/true`; both flag-1 memberships false, both flag-2 true | Allow both |
| 4 | `"removeBlacklistTxFromWithFlag(address,uint64)" "$A" 1` | Revert; remains `2; true/true` | Allow both |
| 5 | `"removeBlacklistTxFromWithFlag(address,uint64)" "$A" 2` | `2; false/true` | Allow both |
| 6 | `"addBlacklistTxToWithFlag(address,uint64)" "$A" 1` | `1; false/true` | Reject both |
| 7 | `"removeBlacklistTxToWithFlag(address,uint64)" "$A" 1` | `0; false/false` | Allow both |
| 8 | `"addBlacklistTxFromWithFlag(address,uint64)" "$A" 2` | `2; true/false`; old flag not reused | Allow both |
| 9 | `"addBlacklistTxTo(address)" "$A"` (legacy add) | `1; true/true`; shared type replaced | Reject both |
| 10 | `"addBlacklistTxFromWithFlag(address,uint64)" "$A" 2` | `2; true/true`; shared type replaced | Allow both |
| 11 | `"removeBlacklistTxFrom(address)" "$A"` (legacy removal) | `2; false/true` | Allow both |
| 12 | `"removeBlacklistTxTo(address)" "$A"` | `0; false/false` | Allow both |
| 13 | Repeat step 12 | Revert; remains unlisted | Allow both |

Repeat the sequence with From and To swapped, including the legacy APIs. This
checks both implementations, not just a single direction. Query full and filtered
lists; count A once when present and zero times when absent:

```bash
cast call --rpc-url "$L3_RPC" "$BL_PUBLIC" "getBlacklistTxFrom()(address[])"
cast call --rpc-url "$L3_RPC" "$BL_PUBLIC" "getBlacklistTxTo()(address[])"
for flag in 1 2; do
  cast call --rpc-url "$L3_RPC" "$BL_PUBLIC" "getBlacklistTxFromWithFlag(uint64)(address[])" "$flag"
  cast call --rpc-url "$L3_RPC" "$BL_PUBLIC" "getBlacklistTxToWithFlag(uint64)(address[])" "$flag"
done
```

Other existing entries may be returned: compare A specifically, not list emptiness.
Enumeration scans at most the first 65,536 entries; use individual getters for
authoritative membership if the list exceeds that bound.

## 5. Invalid inputs and authorization

While A is unlisted, use the authorized helper to simulate each flag-aware add
and removal with `0`, `3`, and `18446744073709551615`. All must revert without
changing A. Flag-aware membership and filtered-list queries with these values
must also revert. Flag 0 is only a result of `getBlacklistBanFlag`, not a valid
argument to flag-aware methods.

Repeat invalid replacement tests while A has type 2: an error must not replace
its flag or alter either membership. Restore/clean up through the Safe afterward.

Unauthorized direct mutation example (B is a non-owner):

```bash
cast call --rpc-url "$L3_RPC" --from "$B" "$BL_OWNER" \
  "addBlacklistTxFromWithFlag(address,uint64)" "$A" 1
```

Expect authorization failure. Repeat for both add and both removal selectors.
Also try `executeCall` through the executor from B; it must reject the caller.
Check A's state before/after. For proof of committed unauthorized-write failure,
send one such direct call from B with explicit adequate gas and inspect its failed
receipt; ordinary gas/nonce changes are expected for a mined revert, while the
blacklist state remains unchanged. Public read queries must work from B.

## 6. ERC20 / USDT behavior and top-level scope

Use a known-good development test token, with a funded A and clean token contract.
Set A to flag 2 using step 8. First establish the same transfer works unlisted.

```bash
TOKEN="<development ERC20/USDT test token address>"
cast call --rpc-url "$L3_RPC" "$TOKEN" "balanceOf(address)(uint256)" "$A"
cast call --rpc-url "$L3_RPC" "$TOKEN" "balanceOf(address)(uint256)" "$B"
cast send --rpc-url "$L3_RPC" --chain 18417507517 \
  --account "$A_ACCOUNT" --gas-limit 300000 \
  "$TOKEN" "transfer(address,uint256)" "$B" 1
```

With flag 2, expect the token's normal successful transfer and balance changes.
With A changed to flag 1, the same signed-sender scenario must be rejected by the
blacklist. Change back to 2 and repeat: it must work again. An independent token
pause/blacklist/balance failure is not evidence about the DeriwOS flag.

Scope control: with A type 1, a clean B calling a clean token's `transfer(A,1)`
does not make A the top-level destination (the token contract is the destination).
DeriwOS should not reject solely because A appears in calldata. Token policy may
still reject. Test the same distinction for a clean proxy making a nested call
to A on an isolated fixture. DER-3180 does not introduce recursive quarantine.

## 7. Historical, recovery, and committed-execution cases

These are required for full coverage beyond the ordinary wallet matrix. Use
isolated fixtures where granting roles or constructing historical state is needed;
do not change live governance to manufacture a test case.

| Case | Setup and expected result |
| --- | --- |
| Historical entry | Use a recorded pre-6 entry untouched since activation. Getter now returns 1 and normal top-level transactions involving it are rejected. Do not replace it with a new add and call that a historical test. If no usable entry exists, replay a pre-6 fixture. |
| Effective parent | Use an existing permitted disposable subaccount relationship and a call that actually resolves to that parent. Parent type 1 rejects; type 2 permits. Repeat with the signed child flagged and a clean parent. An ordinary ETH transfer may not exercise delegation. |
| Protected address | Simulate a type-1 add against protected ArbOwner `0x0000000000000000000000000000000000000070`: reject. In an isolated fixture, store type 2 there, then attempt promotion to 1 through both legacy/new adds: reject and preserve type 2 and memberships. Do not mutate live system-address policy for this test. |
| Fee-account restrictions | In an isolated fixture, setting a fee account to a type-1 address must fail; type 2 must not fail due to the general blacklist. Restore fixture state. |
| Emergency removal | With a disposable authorized owner flagged 1, a direct zero-value canonical legacy removal to 0x07ec works. Test both legacy removal selectors. Flag-aware removal and unrelated methods do not receive this bypass. Unauthorized callers and truly delegated owners do not gain it. |
| Historical self-binding | On a fixture containing the historical owner→same-owner relationship, repeat emergency removal. Admission and execution must both allow it. Do not assume current subaccount APIs can create historical relationships. |
| Enclosing revert | In an isolated fixture, authorize a wrapper that successfully calls a flag update, then deliberately reverts. The entire membership/flag change rolls back. Include a control where the wrapper returns successfully and the update persists. |
| Out of gas | Execute additions, replacements, and final removals at insufficient gas in a fixture. No partial membership/flag mutation may persist. A matching adequate-gas control succeeds. |
| Delayed / retryable execution | Submit the normal L3 test payload through the development parent Inbox so execution reaches consensus independently of sequencer admission. Type 1 yields a failed L3 execution receipt, no payload effects, gas consumption and appropriate nonce advancement; type 2 allows the otherwise-valid payload. For retryables, inspect redemption execution, not just successful ticket creation. Repeat original/aliased sender and actual retry destination roles. |

Use the chain's established delayed-message/retryable harness with its actual
Inbox, funding asset, alias and redemption configuration. A normal rejected
`cast send` cannot substitute for this test. A successful funding-only deposit or
ticket-creation receipt is expected even for a quarantined account; it is not proof
that the later transaction execution is allowed.

The source regression suites provide reproducible fixture coverage. Run in the
project's configured Go/native-library build environment:

```bash
go test ./arbos/blacklist ./arbos/arbosState ./arbos ./precompiles ./execution/gethexec ./gethhook -count=1
node --test scripts/safe-proposals/blacklist-calldata.test.mjs
```

Useful tests to inspect include `TestBlacklistAdmissionMatchesCommittedExecution`,
`TestDeriwBlacklistFlagUpdatesRollBackWithEVM`,
`TestDeriwBlacklistOutOfGasCannotLeavePartialFlagUpdate`, and
`TestBlacklistBanTypeSelectorsPreserveHistoricalGas`.
These tests cover native execution; they do not replace deployed WASM replay.

## 8. Validator and cross-node acceptance

1. Record the activation transaction/block and each successful governance/test
   transaction receipt (block number and hash), including delayed failed execution.
2. Confirm each RPC/full node reports the same block hash, state root and receipt
   for the same finalized block. Query blacklist state with `cast call --block N`
   at one fixed block across nodes; comparing changing `latest` states is ambiguous.
3. Identify the corresponding input-message/batch positions and verify the
   validator has processed through them using the release root. Validator
   `messageCount` is not directly interchangeable with an L3 block number.
4. Replay pre-activation, activation, and post-activation blocks with flag 1/2
   changes and payloads. Native execution and WASM must agree on final global
   state, including failed executions and storage/gas-dependent effects.
5. Preserve the full log interval; do not use only the last successful lines to
   conclude there were no failures. Inspect every validator, not just one node.

On the node host, replace the example interval to cover activation and testing:

```bash
docker logs --since 30m main-nitro-1 > /tmp/DER-3180-validator.log 2>&1
grep -Ei 'validated execution|module.?root' /tmp/DER-3180-validator.log | tail -20
grep -Ei 'error|failed|mismatch|diverg|fatal|panic' /tmp/DER-3180-validator.log
```

Classify every relevant validation error; `failure-is-fatal: false` means a running
container is not proof of validation health. Retain the complete interval even
when grep returns no matches. Batch posting, sync and block production must keep
advancing. Agree and record the soak duration before promotion.

## 9. Cleanup and sign-off

For each disposable address, query memberships and remove only directions still
present, using legacy removal through the authorized Safe. Final removal clears
the type; do not attempt to clear it by adding flag 0. Confirm flag 0, both legacy
memberships false, and normal transfers succeed. Restore any isolated fixture
roles/configuration and record token/native balance effects.

Record one evidence row per case:

| Case | Before flag/F/T | Operation tx hash | Test tx hash or exact admission error | Receipt block/hash | After flag/F/T | Validator coverage | Pass/fail |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Example | `0/false/false` | … | … | … | `1/true/false` | … | Pending |

Sign off only when core, negative, scope, fixture, and feature-specific replay
checks have evidence. Mark unavailable cases **not tested**, rather than passed.
Testing supports the stated top-level policy; it cannot establish an unrestricted
"no holes" claim, and it does not create token-calldata enforcement for flag 2.

Related: [release note](DER-3180-blacklist-ban-types.md),
[design and review](../decisions/0005-blacklist-ban-types.md), and
[environment runbook](../deriw-l3-environment-deployment-runbook.md).
