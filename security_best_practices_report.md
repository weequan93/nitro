# ArbSys Router-Only Security Review

Review date: 2026-08-17; combined-feature re-review: 2026-08-18; DeriwOS 3
policy update: 2026-08-19

Scope: Deriw development, test, and production L3 environments,
`ArbSys.SendTxToL1`, Nitro call-stack tracking, Deriw router and canonical
ERC-20 routes, upgrade/activation mechanics, and the canonical parent-chain
receiver.

## Executive summary

The pre-fix code did not restrict `ArbSys.SendTxToL1`. DeriwOS 2 introduced an
exact normalized route suffix, immediate-caller agreement, and uniqueness
checks. The updated policy deliberately restores only the distinct
`withdrawEth(address)` ABI entry point at DeriwOS 3. Raw `sendTxToL1` and
canonical ERC-20 gateway sends remain route-restricted.

The proposed control is not sufficient for end-to-end bridge safety. The live
router is an unverified, upgradeable implementation controlled by a 1-of-4
Safe. The current bytecode contains external calls and no `DELEGATECALL`, but a
shared reentrancy lock and complete authorization logic cannot be certified
without verified source. The canonical router and token gateway are also
upgradeable.

The repository already has an independent, state-backed DeriwOS consensus
version. Router-only sends activate as DeriwOS 2, and the narrow direct-ETH
exception activates as DeriwOS 3. This avoids silently changing an already
activated consensus version or unrelated Orbit-chain semantics. Development
and production have chain-ID-selected, compiled bootstrap routes that are
copied into consensus state exactly once. Afterward, a chain owner can stage a
complete atomic replacement through `ArbOwner`; it activates automatically at
a start-block boundary after a mandatory seven-day delay. The test chain has
no guessed bootstrap and must be configured through this delayed governance
path before DeriwOS 2 can activate.

Activation has an important availability consequence: canonical token-gateway
error-recovery withdrawals that originate directly inside the gateway do not
contain the Deriw router and will be rejected. Bridge operators must explicitly
accept that fail-closed behavior or redesign the recovery flow before
activation.

## Evidence snapshot

At the live head observed during review:

- Chain ID: `0x449c4dcbd` (`18417507517`).
- `ArbSys.arbOSVersion()`: `0x73` (`115`, internal ArbOS 60 plus 55).
- Direct `sendTxToL1` from `0x2222...2222`: success, result `492`.
- Direct zero-value `withdrawEth` from `0x2222...2222`: success, result `492`.
- Direct zero-amount default-gateway withdrawal: success, encoded result `492`.
- The new `ArbOwnerPublic.getDeriwRouterConfig()` selector reverted, confirming
  that the state-backed update mechanism is not yet deployed on the live chain.
- Deriw router slots 0/1/2: admin Safe / zero / implementation, matching the
  supplied addresses.
- Safe: four owners, threshold one, empty module list, zero guard slot.
- Canonical router/default gateway: `0x9fF6...4a5d` / `0x3fc1...7679`.
- Router implementation code hash:
  `0x0822922e29fbbda99cfbdfb21a3b449bbb1c520c15c7f7c174788641380e2c2b`.
- Opcode-aware scan of the router implementation found six `CALL` and two
  `STATICCALL` opcodes, with no `DELEGATECALL`, `CALLCODE`, `CREATE`, `CREATE2`,
  or `SELFDESTRUCT` in executable bytecode.
- The router and implementation are unverified on the Deriw explorer. The
  canonical gateway proxies and implementations are verified.
- A historical simulation of the cited signed ERC-20 withdrawal succeeded for
  both the original sender and an unrelated relayer, rejected an amount change
  with `signature err`, and rejected post-execution reuse with `signature err`.
- A direct call to the canonical parent gateway on Arbitrum Sepolia reverted
  with `NOT_FROM_BRIDGE`.

The live RPC does not expose `debug_traceTransaction`; route evidence for the
cited ERC-20 withdrawal is therefore based on verified canonical source,
decoded live events, proxy slots, and the recorded `L2ToL1Tx.caller`, not a live
call trace.

### Environment follow-up

- Development: chain ID `18417507517`; route addresses are the original audited
  router, canonical router, and default gateway.
- Test: chain ID `2885`. The development addresses have no code on this chain.
  Its full route triplet was not established to audit quality, so no bootstrap
  is compiled. Chain governance must first stage and activate a verified route;
  DeriwOS 2 fails closed until that state-backed route exists.
- Production: chain ID `2886`. A successful withdrawal at transaction
  `0x339832ea425b55735e4726fce66c607ebb5dff5d9a0ad4851f2ba1af334ccf13`
  showed the normalized route:
  `0x8fb358679749FD952Ea5f090b0eA3675722B08F5` →
  `0xb85b91A9362e296243360e83Cb0792a87Dc32712` →
  `0x6121117fCcEcdD6dFa7B3230Eacd4f53e12905Db`.
- Consensus configuration is not read from process environment variables or
  RPC hostnames. Both could differ among validators. Bootstrap selection uses
  only the chain ID stored in ArbOS state; all subsequent changes are authorized
  transactions recorded in consensus state.

## High severity

### RSTL1-001: `ArbSys.SendTxToL1` has no route authorization

- Rule ID: RSTL1-001
- Severity: High
- Location: `precompiles/ArbSys.go:112-117` and
  `precompiles/ArbSysRouter.go:60-115`
- Status: remediated in the checked-out source and refined by DeriwOS 3.
- Evidence: before this change the function began with `L1BlockNumber` lookup
  and had no caller or route check. Live direct simulations of both functions
  succeeded. The first operation is now an operation-aware,
  DeriwOS-versioned route guard. `WithdrawEth` remains guarded at DeriwOS 2 and
  receives its explicit entry-point-only exemption at DeriwOS 3.
- Impact: any L3 account or contract can bypass the Deriw router and create an
  outbound message. Exploitability against parent assets depends on the parent
  receiver, but the stated route-integrity invariant is absent.
- Fix applied: DeriwOS 2 requires an exact normalized direct or ERC-20 route,
  immediate-caller agreement, and unique protected addresses. Delegate/callcode
  frames are ignored, while equal ordinary frames remain visible. DeriwOS 3
  exempts direct native-ETH withdrawals without using value or empty calldata
  as a spoofable classifier.
- Mitigation: do not rely on sequencer admission filters; delay activation
  until all consensus roles and validator machines run the new binary.
- False positive notes: none. The behavior was confirmed against source and the
  live chain.

### RSTL1-002: A single Safe owner can replace trusted router code

- Rule ID: RSTL1-002
- Severity: High
- Location: live Deriw router proxy
  `0x32068069f13191B57c03Eee8531a8C82b26d12B9`
- Evidence: custom proxy slot 0 is the Safe, slot 2 is the active
  implementation, and live Safe calls returned four owners with threshold one,
  no modules, and no guard.
- Impact: compromise or malicious action by one owner can install code that
  still executes as the allowlisted router address. The consensus suffix then
  proves routing through the proxy, but not correct user authorization.
- Fix: raise the threshold (preferably 3-of-4), add a timelock, and make the
  router immutable or add separately reviewed implementation-address/code-hash
  pinning.
- Mitigation: monitor slot 2 and stop activation/operation on an unexpected
  implementation hash.
- False positive notes: a policy enforced outside the Safe was not visible;
  verify any operational controls separately.

### RSTL1-003: Router authorization and reentrancy safety are not certifiable

- Rule ID: RSTL1-003
- Severity: High
- Location: live router implementation
  `0x9b7d4de172fa7c5a7be48bb8ba9004c4ecfea78e`
- Evidence: source and ABI are not verified. Executable bytecode contains six
  external `CALL` operations. The live signed ERC-20 path demonstrates
  tamper/replay rejection, but bytecode-only review cannot prove complete
  EIP-712 field binding or one shared reentrancy lock across every outbound
  entry point.
- Impact: a callback, incomplete signature schema, or upgrade can make an
  apparently approved route execute an unauthorized intent. Exact suffix
  matching does not replace router authorization or a shared lock.
- Fix: verify exact source and compiler settings; verify complete signed intent;
  add one namespaced shared lock across ETH, ERC-20, messaging, batch, and
  callback-reachable entries; preserve storage layout.
- Mitigation: keep the fee receiver as an EOA and allow only reviewed standard
  bridge tokens until the router is fixed, but do not treat those as permanent
  controls.
- False positive notes: a non-obvious bytecode-level lock may exist. Verified
  source and adversarial callback tests are required to resolve this finding.

### RSTL1-004: Router-only enforcement disables canonical gateway recovery sends

- Rule ID: RSTL1-004
- Severity: High
- Location: verified `L2ArbitrumGateway`/`L2ERC20Gateway` live implementation;
  proposed `ArbSys.SendTxToL1` policy
- Evidence: `finalizeInboundTransfer` and `handleNoContract` can call
  `triggerWithdrawal` directly for token deployment/address failures. Their
  normalized route ends in the gateway and contains neither the Deriw router
  nor the canonical router.
- Impact: after activation, affected deposits/retryables revert instead of
  emitting the automatic L3-to-parent refund, potentially delaying recovery of
  parent-chain escrowed funds.
- Fix: either explicitly accept and document the fail-closed recovery model, or
  redesign canonical recovery so it passes through an authenticated Deriw
  route. Do not silently allow `[gateway]`, because that recreates a direct
  gateway bypass for ordinary users.
- Mitigation: pause unsupported deposits and provide an operational recovery
  procedure before activation.
- False positive notes: this is not a suffix-check bug; it is a deliberate
  consequence of the strict policy.

## Medium severity

### RSTL1-005: A global ArbOS 61 gate would change unrelated chains

- Rule ID: RSTL1-005
- Severity: Medium
- Location: `arbos/deriwpolicy/router_only_sends.go:16-114` and
  `arbos/arbosState/deriwos_version.go:75-117`
- Status: resolved in the checked-out source.
- Evidence: global maximum supported ArbOS is 60, while the repository already
  implements independently scheduled DeriwOS consensus versions.
- Impact: hardcoding Deriw development addresses behind a global upstream
  version would brick or change outbound behavior on any other chain that uses
  the same binary/version.
- Fix applied: router-only sends are DeriwOS 2. A deterministic chain-ID
  resolver supplies only the one-time development/production bootstrap. The
  guard then reads that environment's active consensus-state route. An
  unconfigured chain cannot activate DeriwOS 2.
- Mitigation: deploy the compatible binary to every validator before using the
  new configuration precompile or activating DeriwOS 2.
- False positive notes: none for this repository architecture.

### RSTL1-006: Each environment gateway allowlist is intentionally brittle

- Rule ID: RSTL1-006
- Severity: Medium
- Location: live canonical routers,
  `arbos/arbosState/deriw_router_config.go:194-256`, and
  `precompiles/ArbOwner.go:101-144`
- Evidence: the live default route is `0x3fc1...7679`, but verified canonical
  router source permits the parent counterpart to change the default gateway
  and set token-specific gateways.
- Impact: a legitimate gateway configuration or upgrade not coordinated with a
  Deriw route update will make withdrawals fail closed at ArbSys.
- Fix applied: a chain owner can stage the router, canonical router, and full
  gateway list as one atomic replacement. The proposal validates nonzero and
  distinct contract addresses, allows at most 32 gateways, keeps the active
  route unchanged, and cannot activate for at least seven days.
- Mitigation: monitor `GatewaySet`/`DefaultGatewayUpdated`; verify proposed code,
  implementations, and calldata during the delay; coordinate activation times.
- False positive notes: no additional live `GatewaySet` route was identified in
  the available explorer/RPC data, but historical log indexing was incomplete.

### RSTL1-007: Canonical proxy upgrades remain in the trusted computing base

- Rule ID: RSTL1-007
- Severity: Medium
- Location: canonical router `0x9fF6...4a5d`, default gateway
  `0x3fc1...7679`, and parent gateway `0xdf94...f1d5`
- Evidence: each is an EIP-1967 proxy with nonzero implementation/admin slots.
- Impact: a malicious or vulnerable approved gateway implementation can still
  create an outbound message along an approved suffix or mishandle assets.
- Fix: document governance, timelocks, and upgrade review; optionally pin
  implementations/code hashes in a separately scoped design.
- Mitigation: monitor implementation slots and fail operationally on unexpected
  changes.
- False positive notes: governance controls for these proxy admins were not
  fully audited in this review.

### RSTL1-008: Route-list governance remains a security-critical authority

- Rule ID: RSTL1-008
- Severity: Medium
- Location: `precompiles/ArbOwner.go:65-144`,
  `precompiles/ArbOwnerPublic.go:20-50`, and
  `arbos/arbosState/deriw_router_config.go:18-275`
- Evidence: an account or contract in the existing ArbOS chain-owner set can
  schedule a full route replacement. The change is transparent and delayed,
  but it is intentionally authorized governance power.
- Impact: compromised chain-owner governance can eventually approve malicious
  route contracts. The suffix check would then enforce the malicious route.
- Fix applied: owner-only access, a fixed seven-day delay, atomic full-list
  replacement, nonzero/distinct/code-present checks, public active/pending
  getters, deterministic block-boundary activation, revision tracking, and a
  rule preventing active, pending, or proposed protected route contracts from
  modifying their own list.
- Mitigation: put chain ownership behind a high-threshold Safe plus timelock,
  monitor every proposal, and maintain an emergency response that can cancel a
  pending update before its activation boundary.
- False positive notes: a delay limits reaction time but cannot compensate for
  permanently compromised governance.

## Confirmed controls

- Nitro tracks acting/storage addresses with `Contract.Address()` and marks
  delegate/callcode frames explicitly.
- `WithdrawEth` funnels through `SendTxToL1`.
- The source guard runs before the pre-existing `L1BlockNumber` lookup, fails
  closed on missing context, preserves normal duplicate frames, requires the
  final normalized frame to equal `c.caller`, and rejects unknown gateways.
- DeriwOS 2 is independently versioned. Bootstrap routes are selected from the
  stored chain ID and copied once; environment address sets cannot
  cross-authorize one another.
- Active and pending route configurations live in a dedicated ArbOS subspace.
  Chain owners can schedule or cancel a full replacement, public callers can
  inspect both states, and mature proposals activate in the consensus
  start-block transaction.
- Route proposals enforce the seven-day minimum delay, 1-32 gateways, unique
  nonzero protected addresses, and deployed bytecode at scheduling time.
- The current router implementation has no executable `DELEGATECALL`.
- The current fee receiver has no code.
- The observed signed ERC-20 request rejects calldata tampering and replay and
  intentionally permits relayers.
- The canonical parent gateway rejects a direct EOA call before executing the
  withdrawal payload.

## Required activation gates

1. All nodes and validator machines support DeriwOS 2 and use identical code.
2. The new ArbOwner/ArbOwnerPublic methods and DeriwOS version scheduling are
   deployed and callable; they were not live during this review.
3. The active/pending values, code hashes, revision, and activation timestamp
   are independently verified after the governance transaction.
4. Router source is verified and shared-lock/authorization tests pass.
5. Safe threshold/timelock or implementation pinning is in place.
6. Canonical recovery-path behavior is explicitly resolved.
7. All active gateway routes are inventoried and allowlisted.
8. Pre-activation state-root replay and delayed-inbox tests pass.
9. Direct ArbSys/direct gateway simulations revert after activation, while
   approved ETH and ERC-20 routes succeed.
10. Parent receivers are inventoried; each authenticates the bridge/outbox and
   recorded L3 sender. Only the observed canonical gateway was live-tested here.
11. On test chain ID `2885`, governance first stages and activates the verified
    router, canonical gateway router, and complete gateway list; only then is
    DeriwOS 2 scheduled.

## Implementation verification

- Route-normalization and authorization tables cover empty/short paths,
  arbitrary prefixes, proxy delegate/callcode frames, caller mismatch, helpers,
  unknown/missing gateways, and every repeated protected address:
  `precompiles/ArbSysRouter_test.go:43-205`.
- Environment and state tests verify exact dev/prod bootstrap sets,
  cross-environment rejection, fail-closed test/unknown bootstraps, delayed
  scheduling, automatic activation, cancellation, revision changes, and
  governance configuration of test before DeriwOS 2.
- Owner-precompile tests cover contract-code checks and rejection when a
  protected route contract is also accidentally granted chain-owner status.
- The Solidity precompile interfaces compile successfully.
- `go test ./arbos/deriwpolicy ./arbos/arbosState ./precompiles`: pass.
- `go vet ./arbos/deriwpolicy ./arbos/arbosState ./precompiles`: pass.
- `go test ./arbos/... ./gethhook`: pass.
- `git diff --check`: pass.

These tests validate the consensus helper, version gate, and state-backed
governance flow in this checkout. They do not replace state-root replay,
delayed-inbox, deployed-router callback, governance-Safe, or post-activation
end-to-end tests. The control should not be activated until the outstanding
activation gates above are satisfied.

## Combined branch and submodule verification (2026-08-18)

### Verdict

The combined source at root commit
`befa9dc97192589649ea78d06b57f9e74c78f86a` contains the consensus blacklist,
ArbSys router-only restriction, and gasless `eth_estimateGas` changes. The root
merge graph is correct: its parents are the ArbSys/blacklist line at
`d8ad7ef2cd4b4d9dc179fe03e95f39e69eeb9248` and `fix/estimate-gas` at
`727d5440bfb90bfc2a165677fb889761e4a4e9ac`.

The initial verification found that two gitlink commits were not remotely
fetchable, the committed go-ethereum URL named the upstream repository, and the
test-node submodule contained uncommitted generated ABI updates. Those release
blockers were resolved on consistent `fix/blacklist-subaccount` branches before
the root packaging commit was created.

### SCM-001: Consensus-critical go-ethereum gitlink was not fetchable

- Rule ID: SCM-001
- Severity: High (release blocker)
- Status: resolved.
- Location: root gitlink `go-ethereum`; `.gitmodules:1-3`
- Original evidence: the root pointed to
  `cd17872f174a58c08c8dc84c1f4d04547b7f88e9`, a local merge of blacklist gas
  handling parent `f9b2467411` and estimate-gas parent `5bd9de018f`. The commit
  was not advertised by any branch or tag on the Deriw go-ethereum remote. A
  fetch by exact hash from a new empty repository failed with
  `upload-pack: not our ref`. In addition, `.gitmodules` named
  `https://github.com/OffchainLabs/go-ethereum.git`; only this workstation's
  local `.git/config` overrode it to the Deriw fork.
- Impact: a clean recursive clone, CI runner, or dev deployment cannot obtain
  the exact consensus/RPC source recorded by the published root branch.
- Resolution applied: `go-ethereum/fix/blacklist-subaccount` now points to
  `52064791ab`, which contains blacklist gas handling `f9b2467411`, the original
  estimate-gas commit `5bd9de018f`, and its published follow-up `ec2aa50276`.
  The root gitlink was advanced to that tip and `.gitmodules` now names
  `https://github.com/weequan93/go-ethereum.git`.
- False positive notes: none; remote advertisement and an empty-repository
  exact-hash fetch were both checked before remediation.

### SCM-002: ArbSys governance-interface gitlink was not fetchable

- Rule ID: SCM-002
- Severity: High (release blocker)
- Status: resolved.
- Location: root commit `d8ad7ef2`, gitlink
  `contracts-local/src/precompiles`
- Original evidence: the root pointed to
  `f87337228eac115d92ff35ce37474b3e644b72d7`, which adds the router-config
  methods to `ArbOwner.sol` and `ArbOwnerPublic.sol`. The Deriw precompile
  interface remote advertised `fix/blacklist-subaccount` only through its
  parent `b6b357c2`; it did not advertise `f8733722`. An exact-hash fetch from
  a new empty repository failed with `upload-pack: not our ref`.
- Impact: a clean build cannot reproduce the ABI inputs used by this checkout,
  even though the already-generated Go bindings present locally compile.
- Resolution applied: `nitro-precompile-interfaces/fix/blacklist-subaccount`
  now advertises `f87337228`; its parent `b6b357c2` carries the earlier
  DeriwOS/blacklist interfaces and `f87337228` adds the ArbSys route-governance
  interfaces.
- False positive notes: none; the exact remote fetch failed before remediation.

### SCM-003: Test-node explorer ABI changes were uncommitted

- Rule ID: SCM-003
- Severity: Medium (release packaging)
- Status: resolved.
- Location: `nitro-testnode/blockscout/init/data/ArbOwner.abi:1`,
  `ArbOwnerPublic.abi:1`, `DeriwBlacklist.abi:1`, and
  `DeriwBlacklistPublic.abi:1`
- Original evidence: the root recorded test-node commit `e96bfabd85`, while the
  nested blockscout checkout had four modified ABI files. They contained the
  DeriwOS scheduling/query and router-config scheduling/query methods and were
  not represented by either the nested or parent gitlink.
- Impact: the running local test stack may expose newer ABIs than a clean dev
  deployment, causing explorer/tooling behavior to differ despite identical
  root commits.
- Resolution applied: `blockscout/fix/blacklist-subaccount` publishes the four
  ABI updates at `6283e561a`; `nitro-testnode/fix/blacklist-subaccount` records
  that gitlink at `a8f5f3c`, and the root gitlink was advanced to that commit.
- False positive notes: these files do not alter Nitro consensus execution, but
  they do affect reproducibility and governance-method discoverability.

### Combined implementation checks

- Blacklist: DeriwOS 1 applies the union of the legacy from/to sets to the
  signed sender, effective subaccount parent, explicit top-level destination,
  and both aliased/unaliased L1 sender identities. It executes during state
  transition, so delayed normal transactions cannot bypass sequencer admission.
  The documented funding-only and protocol-internal transaction exclusions are
  intentional, and retryable execution is checked at its actual destination.
- ArbSys: DeriwOS 2 runs the route guard as the first operation in the shared
  send implementation. DeriwOS 3 identifies the original ABI operation and
  exempts only `withdrawEth`; raw `sendTxToL1` and ERC-20 sends remain guarded.
  The guard reads the active route from consensus state and enforces exact
  normalized suffixes, caller agreement, protected-address uniqueness, and an
  explicit gateway allowlist. Dev/prod bootstraps are chain-ID selected; test
  fails closed until governance stages a route. Route replacement is atomic
  and delayed seven days.
- Estimate gas: the RPC-only hook zeroes legacy and EIP-1559 fee fields before
  estimation for statically configured or on-chain target-allowlisted gasless
  contracts. It deliberately does not treat sender-only allowlisting as
  sufficient. It does not change transaction consensus pricing.
- The consolidated go-ethereum branch contains all feature parents without
  source conflicts: the blacklist failed-no-op gas accounting touches
  `core/state_transition.go` and `core/vm/errors.go`, while estimate-gas touches
  `core/arbitrum_hooks.go` and `internal/ethapi`. It also includes the later
  fee-variant normalization tests from `ec2aa50276`.

### Combined verification run

- `go test ./arbos/... ./precompiles ./gethhook`: pass.
- `go test ./execution/nodeinterface ./execution/gethexec`: pass.
- In go-ethereum, `go test ./internal/ethapi ./core`: pass.
- Nitro vet on the scoped consensus, precompile, execution, and hook packages:
  pass.
- go-ethereum vet on `./core ./internal/ethapi`: pass.
- `git diff --check`: pass.
- Linker warnings report that the existing `libstylus.a` was built for macOS
  26.2 while this run linked for macOS 26.0; the tests still passed.

These checks support correctness of the scoped implementation, but they do not
remove the existing live-router, governance, canonical recovery-route, or
post-activation end-to-end findings above.

## Combined-feature security re-review (2026-08-18)

### Re-review verdict

The packaged branch and every nested submodule are clean and pinned to commits
advertised by their `fix/blacklist-subaccount` remotes. Focused Nitro and
go-ethereum tests pass from a cold test cache. The branch is nevertheless not
ready to activate unchanged: the re-review found one consensus-upgrade control
issue and two gasless-estimation correctness gaps that are not covered by the
current tests.

The blacklist and ArbSys route-normalization implementations otherwise match
their documented narrow invariants. This conclusion does not certify the code
as bug-free and does not resolve the live router/governance/recovery findings
earlier in this report.

### CMB-001: Blacklist owners can schedule an immediate consensus upgrade

- Rule ID: CMB-001
- Severity: High
- Location: `precompiles/DeriwBlacklistWrapper.go:58-72`,
  `precompiles/DeriwBlacklist.go:129-132`, and
  `arbos/arbosState/deriwos_version.go:137-153`
- Evidence: every access-controlled `DeriwBlacklist` method accepts either a
  blacklist owner or a chain owner. `ScheduleDeriwOSUpgrade` is exposed through
  that wrapper, and the state method validates the target version and current
  configuration but imposes no minimum activation delay. A timestamp at or
  before the current block time therefore activates at the next start-block
  transition. It also compares the new target only with the active version, so
  a caller can replace an existing pending version/timestamp.
- Impact: a lower-scope blacklist operator can activate consensus blacklist or
  router-only semantics without chain-owner approval and without an enforced
  validator/user review window. Premature activation can cause a chain halt or
  split when validator binaries differ, and can disrupt bridge recovery or
  outbound routes even when all nodes agree.
- Fix: move consensus-version scheduling to `ArbOwner`, authorize chain owners
  only, enforce a minimum delay, and define explicit cancel/reschedule rules.
  Add wrapper-level authorization and boundary tests, including a blacklist
  owner rejection and past/current timestamp rejection.
- Mitigation: until fixed, do not grant `DeriwBlacklist` ownership to an
  operational key with less trust than chain governance and do not activate
  DeriwOS on a public environment.
- False positive notes: if blacklist owners are intentionally identical to
  chain owners, the privilege-expansion part is operationally reduced, but the
  lack of an enforced activation delay remains.

### CMB-002: Gasless estimation changes `BASEFEE` and `GASPRICE` semantics

- Rule ID: CMB-002
- Severity: Medium
- Location: `go-ethereum/internal/ethapi/api.go:959-970`,
  `go-ethereum/eth/gasestimator/gasestimator.go:262-267`, and
  `arbos/tx_processor.go:938-950`
- Evidence: for an allowlisted target, `DoEstimateGas` overwrites legacy and
  EIP-1559 fee fields with zero. The estimator responds to a zero effective gas
  price by lowering the EVM block base fee to zero. Normal consensus execution
  skips fee payment for gasless transactions but does not rewrite the block
  base fee this way; Nitro's `GASPRICE` hook normally derives its value from the
  real base fee when tips are not collected.
- Impact: contracts that branch, calculate, or revert based on `BASEFEE` or
  `GASPRICE` can produce a different result during `eth_estimateGas` than when
  the transaction is executed. This can yield a false success, false revert, or
  materially wrong gas estimate.
- Fix: preserve the caller's fee fields and EVM fee context. Add an explicit
  estimator option that bypasses only the sender-balance gas-affordability cap
  when the exact message satisfies the consensus gasless predicate.
- Mitigation: do not rely on the modified estimator for fee-opcode-sensitive
  contracts until fixed; simulate with funded accounts and representative fee
  fields as a cross-check.
- False positive notes: ordinary transfers and contracts that never inspect
  fee opcodes are unaffected, which is why the current tests pass.

### CMB-003: Sender-only gasless accounts are omitted from estimation policy

- Rule ID: CMB-003
- Severity: Medium
- Location: `execution/nodeinterface/virtual_contracts.go:88-106`,
  `arbos/pricer/pricer.go:51-55`, and `arbos/tx_processor.go:964-977`
- Evidence: the RPC hook receives only `to` and queries only `TxToAddrs`.
  Consensus execution treats a transaction as custom-price/gasless when either
  the sender or target is allowlisted. The node-interface test explicitly
  asserts that sender allowlisting does not affect the RPC hook.
- Impact: an unfunded sender that is validly gasless by the consensus sender
  allowlist can still receive an insufficient-funds error from
  `eth_estimateGas`, depending on supplied fee fields. Wallets and gas-station
  integrations therefore see inconsistent admission and estimation behavior.
- Fix: pass the complete estimation message (at least `from`, `to`, and the
  transaction form) to the hook and mirror the same consensus predicate used by
  `SkipBaseFeeCheck`; combine this with the balance-cap-only fix in CMB-002.
- Mitigation: target-allowlist each intended gas-station destination or fund
  sender accounts used for estimation until fixed.
- False positive notes: this is not a bug only if the public RPC feature is
  deliberately specified as target-only rather than as estimation support for
  all consensus gasless transactions. That narrower contract should then be
  named and documented explicitly.

### Live environment check

Read-only checks at the 2026-08-18 heads returned:

| Environment | Chain ID | Block | `ArbSys.arbOSVersion()` | Direct `sendTxToL1` |
|---|---:|---:|---:|---:|
| Development | `18417507517` | `144480848` | `115` | success, `492` |
| Test | `2885` | `135158172` | `115` | success, `637` |
| Production | `2886` | `113232458` | `87` | success, `6641` |

On all three environments, `getDeriwOSVersion()` and
`getDeriwRouterConfig()` reverted, confirming that the new precompile/state
surface is not deployed there. No live environment currently enforces the
router-only invariant.

### Re-review verification run

- Root commit: `73189f61f9bbd3b7f7397d6f9c2625f69fd49e04`.
- go-ethereum gitlink: `52064791abfa175fb23229c20c11dd84fba894cf`.
- Precompile-interface gitlink:
  `f87337228eac115d92ff35ce37474b3e644b72d7`.
- Test-node gitlink: `a8f5f3c33c11e7bc5dc0b8328acceeb03e847225`.
- All recursive submodule worktrees: clean.
- `git diff --check`: pass.
- Nitro: `go test -count=1 ./arbos/deriwpolicy ./arbos/arbosState
  ./precompiles ./gethhook ./execution/gethexec ./execution/nodeinterface`:
  pass.
- go-ethereum: `go test -count=1 ./internal/ethapi ./core`: pass.
- Build warnings remain limited to the existing `libstylus.a` macOS 26.2 versus
  26.0 linker-version mismatch.

### Release-tooling governance guard

The Safe proposal helper requires every proposal transaction to target the
manifest's `upgradeExecutorAddress` and rejects direct governed-contract or
precompile targets. It pins the reviewed UpgradeExecutor address for all three
Deriw L3 chain IDs. Its nine unit tests pass, including wrong-executor and
direct-target rejection. This is an operational safeguard only and does not
remediate CMB-001 in consensus execution.
