# DER-2646 Security Re-audit

Review date: 2026-08-19  
Branch: `fix/blacklist-subaccount`  
Live environment checked: Deriw development L3 (`https://rpc.dev.deriw.com`)

## Post-review remediation status

The findings below describe the reviewed pre-remediation behavior. The current
working tree adds a separate DeriwOS 5 consensus boundary while preserving the
historical behavior through DeriwOS 4:

- SA-001 is remediated at DeriwOS 5 by signer-scoped EIP-712 digest replay
  keys, including checks for both legacy recovery-byte encodings.
- SA-002 is partially remediated at DeriwOS 5 by exact schema/domain checks,
  the fixed `0x07E9` verifying contract, canonical timestamps, a 600-second
  maximum age, 30-second future skew, and Grant/Revoke digest consumption.
  The signed `chainId` is deliberately not compared with the node chain ID and
  no signed monotonic nonce is added, so those two recommended controls remain
  accepted limitations.
- SA-003 is remediated at DeriwOS 5 by consistent one-to-one bind, revoke, and
  position-cleanup updates. Activation performs no global state scan; affected
  legacy inconsistencies are repaired when rebound or explicitly cleaned up.

All execution and validation nodes must support DeriwOS 5 before it is
scheduled, and signing clients must be updated before activation. These
working-tree changes are not evidence that DeriwOS 5 is deployed on the live
development chain.

## Executive summary

The ERC-20 `ArbSys` route matcher is structurally strong and no permissionless
call-stack bypass was found. On the live development chain, a direct arbitrary
call to `ArbSys.sendTxToL1` reverted, while direct `withdrawEth` succeeded as
intended by DeriwOS 3. A direct ETH withdrawal cannot carry ERC-20 gateway
calldata because `WithdrawEth` always passes an empty payload.

The branch should nevertheless not be released without fixing the sub-account
authorization issues below. The most important confirmed issue is that a
revoked grant can be replayed by changing the ECDSA recovery byte between its
equivalent `0/1` and `27/28` forms. Grants also accept caller-supplied EIP-712
domains without checking the current chain or precompile, and their timestamp
is not enforced. Together, these permit old or cross-environment authorizations
to become active on the wrong chain or after revocation.

The blacklist is working according to its documented top-level-only design,
including checking both a signed child and its effective parent. It is not a
full quarantine. Clean contracts can make nested calls involving listed
addresses, ERC-20 calldata is not inspected, and funding-only deposit and
retryable-submission transactions are exempt. This is an explicit DeriwOS 1
limitation, but it is a real bypass if the product requirement is to freeze all
asset movement involving a listed address.

The live Deriw router remains an upgradeable, unverified implementation whose
admin Safe is currently 1-of-4. ArbOS approves route addresses, not active
implementation code hashes. A single Safe key can therefore replace the router
implementation with code that still satisfies the address-only route check.

## Scope and method

The review covered:

- public sub-account grant/revoke verification and bidirectional relationship
  state;
- delegated transaction sender, nonce, gas purchase, and refund handling;
- consensus and admission blacklist enforcement;
- DeriwOS 3 `ArbSys` ETH-versus-raw-message classification;
- normalized ERC-20 route matching and live router upgrade authority;
- live development-chain policy state and read-only simulations.

This was a source and live-state review, not a complete audit of the deployed
Deriw router. Its implementation source is not verified on the explorer, so
authorization, replay protection, and cross-function reentrancy inside that
contract cannot be certified from bytecode alone.

## Findings overview

| ID | Severity | Finding | Release status |
|---|---|---|---|
| SA-001 | High | Equivalent ECDSA recovery-byte encoding bypasses grant replay protection | Blocker |
| SA-002 | High | Grant/revoke signatures do not enforce domain, schema, nonce, or expiry | Blocker |
| SA-003 | Medium | Parent/child maps can become inconsistent on child rebinding | Fix before release |
| SA-004 | Medium | Child buys gas but unused gas is refunded to the parent | Fix or explicitly specify |
| SA-005 | High, conditional | A delegated child receives unrestricted authority over every allowlisted target | Reduce and formally accept |
| BL-001 | High, conditional | The blacklist can be bypassed through indirect EVM and token interactions | Requirement decision needed |
| BL-002 | Medium | A blacklist owner can also schedule a future DeriwOS consensus upgrade | Fixed in working tree; release validation pending |
| RT-001 | High | Address-only route trust is bypassed by a 1-of-4 router implementation upgrade | Blocker for hardened release |
| RT-002 | Medium | The direct router raw-message path depends entirely on router and parent-receiver authentication | Verify end to end |

## Detailed findings

### SA-001: Equivalent ECDSA recovery-byte encoding bypasses grant replay protection

- Severity: High
- Location: `precompiles/DeriwSubAccountPublic.go:20-36`,
  `arbutil/signature.go:32-45`, and `go-ethereum/common/types.go:58-63`
- Evidence: `GrantAccountControl` stores
  `common.BytesToHash(signatureUse)`. A signature is 65 bytes, while
  `BytesToHash` retains only its rightmost 32 bytes. The replay key therefore
  discards all of `r` and the first byte of `s`. More importantly,
  `ParseTypeDataNSignature` accepts `v=27/28` by converting it to `0/1`.
  Equivalent encodings of the same valid signature consequently produce two
  different replay keys before normalization.
- Impact: a child can use a valid grant with one `v` encoding, wait for the
  parent to revoke it, and submit the same authorization with the equivalent
  alternate encoding to regain parent authority without a new signature.
- Recommended fix: validate and canonicalize the signature first, validate a
  signed nonce, then key replay protection by the complete EIP-712 digest plus
  signer and nonce. Do not use a truncated raw signature as the replay key.
- Mitigation: disable new public grants and revoke existing relationships until
  the consensus fix is deployed.
- False-positive notes: this does not require ECDSA malleability in `s`; only
  the two recovery-byte conventions already accepted by the parser are needed.

### SA-002: Grant/revoke signatures do not enforce domain, schema, nonce, or expiry

- Severity: High
- Location: `arbutil/signature.go:12-30` and
  `precompiles/DeriwSubAccountPublic.go:45-85,89-106`
- Evidence: the generic parser hashes whatever EIP-712 domain and primary type
  the caller supplies. The public precompile never requires the live chain ID,
  verifying contract `0x00000000000000000000000000000000000007E9`,
  expected name/version, or an exact message schema. The timestamp checks are
  commented out, `SetString` success is ignored, and there is no signed nonce.
  Revoke validates only `Operation == "Revoke"` and the recovered signer.
- Impact: an authorization signed for dev, test, another contract, or an old
  session can be replayed on this chain. A stale revoke can also revoke a later
  relationship belonging to the same signer.
- Recommended fix: define one exact EIP-712 type for grants and one for revokes;
  bind chain ID, precompile address, parent, child, operation, monotonically
  increasing parent nonce, issued-at, and deadline. Reject unknown fields and
  malformed/noncanonical addresses. Consume the nonce before changing state.
- Mitigation: use environment-specific keys and do not reuse signatures across
  environments until the fix is active.
- False-positive notes: changing the domain invalidates a signature, but a
  genuine signature originally obtained under another accepted domain remains
  valid there and is currently accepted here as well.

### SA-003: Parent/child maps can become inconsistent on child rebinding

- Severity: Medium
- Location: `arbos/subAccount/subAccount.go:77-125` and
  `arbos/addressMap/addressMap.go:172-184`
- Evidence: `BindRelation` removes the old child of the new parent, but it does
  not remove or reject the existing parent of the new child. `AddressMap.Add`
  silently succeeds without overwriting an existing key. The operation can
  therefore add `parentB -> child` while leaving `child -> parentA`. Later
  revocation or position-based cleanup can remove the wrong reverse entry and
  leave another stale entry.
- Impact: relationship reads disagree, grants appear successful but do not act
  as expected, and one parent can disrupt a relationship associated with
  another parent. This creates authorization-integrity and recovery problems.
- Recommended fix: make rebinding atomic and explicitly enforce a one-to-one
  invariant. Either reject a child that already has a different parent or
  remove both sides of the old relationship before inserting both new sides.
  Add invariant tests after every bind, revoke, and cleanup path.
- Mitigation: prevent the same child from being approved by multiple parents.
- False-positive notes: exploitation requires valid signatures from the
  involved parents, but accidental reuse and stale state are sufficient to
  trigger the bug.

### SA-004: Child buys gas but unused gas is refunded to the parent

- Severity: Medium
- Location: `go-ethereum/core/state_transition.go:472-520,675-689,843,988-1007`
- Evidence: `preCheck` and `buyGas` debit `st.msg.From` before sub-account
  substitution. The code then replaces `st.msg.From` with the parent. It never
  restores the original sender before `returnGas`, which credits unused gas to
  the now-effective parent.
- Impact: the child pays the maximum gas reservation and the parent receives
  unused gas. With a large gas limit, this transfers value unexpectedly and
  makes fee accounting differ from the signed transaction payer.
- Recommended fix: record a dedicated immutable gas-payer address and use it
  for both gas debit and unused-gas refund. Keep the effective execution sender
  separate.
- Mitigation: cap delegated transaction gas limits and document the current
  value transfer until fixed.
- False-positive notes: if transferring unused gas to the parent is intentional,
  it must be specified, tested, and exposed to the child before signing. It is
  not normal Ethereum gas-payer behavior.

### SA-005: A delegated child receives unrestricted authority over every allowlisted target

- Severity: High, conditional on the intended child trust model
- Location: `arbos/subAccount/subAccount.go:218-299` and
  `precompiles/DeriwSubAccount.go:40-60`
- Evidence: once a child-parent relationship exists, any call with arbitrary
  value and calldata to an `allowedAddress` executes as the parent. There is no
  selector, amount, token, recipient, rate, session, or expiry restriction.
  `AddAllowedAddress` does not require deployed code or pin a code hash. The
  live development set contained 873 allowed targets at review time.
- Impact: compromise of a child key can use every privileged function exposed
  by any allowlisted contract. An upgrade, arbitrary executor, or future code
  deployment at one listed address can expand authority without changing the
  sub-account policy.
- Recommended fix: replace the global address allowlist with per-target
  selector and asset limits, session expiry, spend limits, and code-hash or
  implementation controls. Remove stale targets and prohibit generic
  call/delegatecall executors.
- Mitigation: reduce the live list to the minimum reviewed targets and monitor
  code and proxy implementation changes.
- False-positive notes: this may be an accepted full-delegation model. If so,
  the UI and signed authorization must clearly state that the child can spend
  parent assets through every target in the live global list.

### BL-001: The blacklist can be bypassed through indirect EVM and token interactions

- Severity: High if the requirement is full quarantine; informational against
  the documented DeriwOS 1 top-level-only specification
- Location: `arbos/deriw_blacklist_consensus.go:45-109`,
  `gethhook/deriw_blacklist_consensus_test.go:209-339`, and
  `docs/decisions/0004-deriwos-consensus-blacklist.md:27-37`
- Evidence: consensus checks the signed sender, effective sub-account parent,
  and explicit top-level destination. It deliberately does not inspect nested
  calls, calldata token addresses or recipients, creation addresses,
  self-destruct beneficiaries, EIP-7702 targets, retryable refund addresses, or
  protocol-generated internal execution. Deposit funding and retryable ticket
  creation are exempt. Tests explicitly assert the nested-call, ERC-20
  calldata, creation-address, deposit, and retryable behavior.
- Impact: a clean EOA can call a clean helper which interacts with a listed
  address. Tokens can be transferred to a listed recipient because the token
  contract, not the recipient encoded in calldata, is the top-level target.
  Assets can also be funded through the allowed L1-originated paths.
- Recommended fix: first decide the product invariant. For a complete freeze,
  top-level transaction filtering is insufficient; token/gateway controls,
  receiver-specific enforcement, and carefully scoped internal-call policy are
  required. Do not claim that DeriwOS 1 is a full quarantine.
- Mitigation: document the narrow guarantee and pair it with contract/token
  controls for assets requiring a freeze.
- False-positive notes: no bypass was found for the implemented top-level
  invariant. Both the signed child and effective parent are checked.

### BL-002: A blacklist owner can also schedule a future DeriwOS consensus upgrade

- Severity: Medium
- Resolution status: fixed in the post-review working tree; native release
  builder and end-to-end activation tests remain required.
- Location: `precompiles/DeriwBlacklist.go`, `precompiles/ArbOwner.go`, and
  `arbos/arbosState/deriwos_version.go`
- Evidence: every access-controlled method on the blacklist precompile is
  admitted for either a blacklist owner or chain owner. This includes
  `scheduleDeriwOSUpgrade`. The scheduling method requires a newer supported
  version but imposes no minimum delay. The live dev blacklist-owner set still
  contained the EOA `0x57F93d0dFa75206f61F2BcD41Cb61c499d48Fe17`.
- Impact: when a binary supporting a newer DeriwOS version is deployed, a
  compromised blacklist-owner EOA can schedule that consensus policy without
  the UpgradeExecutor/Safe governance path and can select an immediate
  timestamp.
- Recommended fix: give DeriwOS scheduling its own chain-owner-only wrapper and
  require a governance delay. Blacklist owners should only manage blacklist
  membership.
- Implemented fix: the canonical scheduler and cancellation API now live on
  chain-owner-only `ArbOwner` (`0x70`), while the canonical reads live on
  `ArbOwnerPublic` (`0x6B`). The legacy blacklist selector is retained for
  historical DeriwOS 1-3 replay but rejects DeriwOS 4 and every later version.
  DeriwOS 4 records the completed governance boundary. A protocol-enforced
  minimum scheduling delay remains a separate hardening item.
- Transition caveat: before DeriwOS 4 activation, replay compatibility still
  permits the legacy selector to schedule versions 1-3 when newer than the
  active version. The deployment runbook therefore requires a short,
  sequential rollout through version 4 using only the ArbOwner path.
- Mitigation: activate DeriwOS 4 through the approved Safe and UpgradeExecutor,
  verify the legacy blacklist endpoint rejects version 4, and use only the
  `ArbOwner` endpoint for every later schedule. Independently replace
  single-key blacklist owners where operationally practical.
- False-positive notes: the current live chain is already at the maximum
  version supported by its deployed binary, so this is a future-upgrade risk,
  not a current way to downgrade DeriwOS 3.

### RT-001: Address-only route trust is bypassed by a 1-of-4 router implementation upgrade

- Severity: High
- Location: `precompiles/ArbSysRouter.go:67-95`,
  `precompiles/ArbOwner.go:101-135`, and the live router proxy
- Evidence: ArbOS validates route addresses and deployed code at scheduling
  time, but it does not bind proxy implementation addresses or code hashes.
  Live proxy slot 2 pointed to
  `0x9b7d4de172fa7c5a7be48bb8ba9004c4ecfea78e`. The router admin Safe
  `0x5f1b197a82fc1148a02ea55b3bef529f78d64151` had four owners, threshold
  one, no enabled modules, and a zero guard slot. The current implementation's
  opcode-aware scan found no `DELEGATECALL`, `CALLCODE`, `CREATE`, `CREATE2`, or
  `SELFDESTRUCT`, but it is upgradeable and its source is unverified.
- Impact: compromise of any one Safe owner can install an implementation that
  calls `ArbSys` from the trusted router address, satisfying the route suffix
  regardless of user authorization or intended gateway path.
- Recommended fix: raise the Safe threshold, add an upgrade timelock, verify
  source, and either make the router immutable or make ArbOS validate an
  approved implementation/code hash. Apply equivalent controls to canonical
  router and token gateway proxies.
- Mitigation: monitor proxy implementation slots and pause outbound router
  functions on any unreviewed change.
- False-positive notes: this is not a permissionless call-stack bypass in the
  current implementation; it is a concrete single-key governance bypass of an
  address-only consensus policy.

### RT-002: The direct router raw-message path depends on router and parent-receiver authentication

- Severity: Medium
- Location: `precompiles/ArbSysRouter.go:76-93` and parent receiver contracts
- Evidence: the exact suffix `[Deriw router]` is authorized for raw
  `sendTxToL1`, while canonical ERC-20 sends require `[Deriw router, canonical
  gateway router, approved token gateway]`. ArbOS distinguishes the real
  `withdrawEth` ABI path, but it cannot infer that arbitrary raw parent calldata
  is or is not token-related.
- Impact: a public or incorrectly authorized router raw-message wrapper can
  create a parent message without traversing the L3 canonical token gateway.
  This should not release canonical tokens if the parent receiver correctly
  authenticates the Outbox and recorded L3 sender, but an unauthenticated
  receiver can be exploited.
- Recommended fix: if arbitrary messaging is not required, remove the direct
  raw path in a future DeriwOS version. Otherwise, bind complete intent in the
  router signature and verify the canonical Outbox plus expected recorded L3
  sender in every parent receiver.
- Mitigation: inventory every raw-message destination and test direct calls and
  wrong recorded-sender calls on the parent chain.
- False-positive notes: `WithdrawEth` itself is not this bypass; it always sends
  empty calldata and is intentionally unrestricted at DeriwOS 3.

## Controls that worked

- `SendTxToL1` performs the route check before outbound message state access.
- Direct raw calls from an arbitrary EOA revert on live dev.
- Direct `withdrawEth` succeeds on live dev as intentionally specified.
- Delegate/callcode frames are normalized away, while ordinary duplicate
  addresses remain visible.
- The final normalized frame must match the immediate precompile caller.
- Protected router, canonical-router, and gateway addresses must each occur
  exactly once in the accepted ERC-20 suffix.
- Unknown gateway addresses fail closed.
- Router configuration is consensus state, validates code presence, replaces
  the complete route atomically, and has a seven-day activation delay.
- The consensus blacklist checks both the original child and effective parent,
  so sub-account substitution does not bypass its defined top-level rule.

## Live evidence snapshot

- `ArbSys.arbOSVersion()`: `115` (internal ArbOS 60 plus Nitro's 55 offset).
- Active DeriwOS version: 3.
- Direct `sendTxToL1` from `0x2222...2222`: reverted.
- Direct zero-value `withdrawEth` from `0x2222...2222`: succeeded and returned
  message index `492` in read-only simulation.
- Active route revision: 1.
- Active router: `0x32068069f13191B57c03Eee8531a8C82b26d12B9`.
- Active canonical gateway router:
  `0x9fF6747040212f6C21fCe2E8ED0B7B05bA5B4a5d`.
- Active approved token gateway:
  `0x3fc1626EE794Aa6CdE8d8987F4B67BC1bC217679`.
- Router implementation:
  `0x9b7d4de172fa7c5a7be48bb8ba9004c4ecfea78e`.
- Router admin Safe: four owners, threshold one, no modules, zero guard slot.
- Live sub-account allowed-target count: 873.
- Live blacklist owner:
  `0x57F93d0dFa75206f61F2BcD41Cb61c499d48Fe17`.

Explorer references:

- [Deriw router](https://explorer.dev.deriw.com/address/0x32068069f13191B57c03Eee8531a8C82b26d12B9)
- [Router implementation](https://explorer.dev.deriw.com/address/0x9b7d4de172fa7c5a7be48bb8ba9004c4ecfea78e)
- [Router admin Safe](https://explorer.dev.deriw.com/address/0x5f1b197a82fc1148a02ea55b3bef529f78d64151)
- [Canonical gateway router](https://explorer.dev.deriw.com/address/0x9fF6747040212f6C21fCe2E8ED0B7B05bA5B4a5d)
- [Approved token gateway](https://explorer.dev.deriw.com/address/0x3fc1626EE794Aa6CdE8d8987F4B67BC1bC217679)

## Test status

After the DeriwOS 5 remediation, `go test ./arbutil ./arbos/subAccount
./arbos/arbosState ./precompiles -count=1` passed locally with Go 1.25.9. The
linker emitted existing warnings because the local `libstylus.a` was built for
macOS 26.2 while the linker targeted macOS 26.0; all four packages completed
successfully. The complete release-builder and end-to-end suites remain
required before deployment.

## Required release gates

1. Fix SA-001 through SA-004 and add adversarial regression tests.
2. Decide whether blacklist means top-level denial or complete asset freeze;
   document and test the chosen invariant.
3. Reduce and review the 873-address sub-account allowlist.
4. Move DeriwOS scheduling away from the blacklist-owner role.
5. Raise the router admin Safe threshold and add implementation pinning or a
   timelock.
6. Verify the deployed router source and audit every outbound entry point,
   signature field, nonce, external call, and shared reentrancy lock.
7. Verify every parent receiver authenticates canonical Outbox execution and
   the expected recorded L3 sender.
8. Run the full native Go, system, delayed-inbox, bridge, and upgrade tests in
   the release builder.
