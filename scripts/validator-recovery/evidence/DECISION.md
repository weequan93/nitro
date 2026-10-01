# Recovery decision — 2026-09-23 investigation

## Recommendation

Given the requirement to retain the current sequencer history, investigate an
administrator-authorized checkpoint transition using the existing Rollup and
Outbox before considering a proxy upgrade or new Rollup deployment. This is a
candidate for a fork test, **not an approved or validated production procedure**.

The new WASM matches the saved local interval but does not match the parent
assertion endpoint. Merely changing validator configuration or the parent WASM
root cannot repair that historical commitment difference.

## Confirmed evidence

- The saved native single-variable experiment reproduced both incident hashes
  by changing duplicate-address handling from success to error. Preserve this
  as the regression case; a witness-only patch cannot explain away this result.
- New-WASM replay of 4556 local messages completed, ending at Batch 314227/0
  with hash b63c37…ff93a8; parent assertion 42560 ends at 58a792…b0ea0f.
- All four withdrawal messages 8111–8114 executed successfully through the
  existing Outbox. Their parent receipts include the expected USD₮ transfers.
  This establishes those payouts, not equivalence of all account state or all
  historical outbox messages.
- Live parent query observed latestConfirmed 42591, latestNodeCreated 42593,
  old WASM root 767c9a…82c55f, paused=false, stakerCount=2.
- A fast confirmer is configured, but confirmation transaction
  0x8a825e93fb1c23954a23838d22a90c3256245df56738821158d2119d22128d86
  calls ordinary confirmNextNode(bytes32,bytes32), selector 0x5eb405d5.
  Do not attribute this transaction to the fast-confirmation path.
- Its endpoint f4d3c1…3d7801 was not found by the public child RPC. This lookup
  alone is not a new proof of divergence; the completed interval above is the
  stronger evidence.

## Candidate transition to test

1. Capture a fixed parent snapshot and a retained-child checkpoint at or beyond
   the latest confirmed inbox position, within the available parent inbox.
2. Verify the deployed implementations and authorized execution path. In a
   private fork, pause Rollup assertion operations and inspect all stakers,
   pending nodes and challenges. Do not assume pausing Rollup pauses withdrawals
   or the sequencer.
3. Use the actual latestConfirmed afterState and inboxMaxCount as beforeState.
   Test forceCreateNode to a independently verified retained-chain checkpoint,
   followed by forceConfirmNode of that newly created node. Test the intended
   WASM-root update in that same controlled procedure.
4. This deliberately trusts a checkpoint; it is not a WASM proof of a transition
   between divergent histories and does not rewrite old confirmed nodes.
5. Preserve the existing Outbox and spent bitmap. Audit accepted roots and
   message-index/payload continuity across the divergent interval, not just the
   four withdrawals. Old roots remain usable; a new root does not revoke them.
6. Test validator startup from the recovery checkpoint and creation, validation
   and confirmation of subsequent assertions with the selected native/WASM pair.
   The legacy start-validation-from-staked path exists but requires exact version,
   stake, checkpoint and local-hash verification; it is not a generic skip flag.

## Code work must be separate

- Do not globally restore duplicate-address errors in the retained current
  history. That regenerates the conflicting history.
- Preserve current historical execution and introduce future authorization fixes
  at an explicit consensus-version boundary in both native execution and WASM.
- Historical error behavior, historical success/no-op behavior, and future
  one-to-one authorization semantics are three distinct eras. Determine actual
  activation history before implementing a historical compatibility rule.
- Require full EVM receipt/state/gas regression tests, boundary replay tests and
  native/WASM equivalence. Do not suppress block-hash comparisons or rewrite
  preimages to make a mismatch appear successful.

## Current authority issue

User supplied Safe 0x2F996bC558818D33DE37aF36Bee7de24bA3Fc4dF.
At pinned parent block 0x1e496f11 and again at latest:

- Arbitrum One chain ID verified as 42161.
- Safe eth_getCode returns 0x; Safe view calls return empty bytes, not decoded
  owners or threshold.
- UpgradeExecutor 0x1333480e92de9511dc9bb01f70901ff3ee94f613 reports
  hasRole(EXECUTOR_ROLE, suppliedAddress)=false.
- The same address has proxy contract code on Deriw L3. Its L3 deployment alone
  does not confer parent permissions. A cross-chain governance path, if intended,
  requires separate verification of the actual parent executor/relay authority.

No signing, parent mutation, production restart, or validator database modification
has been performed. Full deployed-bytecode/source matching and fork execution remain
outstanding. The existing prepare-recovery-fork.py collects evidence only.

## Authority resolved: replacement Safe

The user's replacement address 0xFbB37c66372f7B40361fBC8C8A235ae92711399D
was queried on Arbitrum One (eth_chainId=42161). Saved responses in
safe-fbb37-response.json show contract code, EXECUTOR_ROLE=true on the parent
UpgradeExecutor, four owners, threshold=3, VERSION=1.4.1 and nonce=10.
These are live-query observations, not reserved transaction parameters; recheck
nonce and permissions before building an actual proposal. This resolves the
direct role prerequisite, not the outstanding fork rehearsal or deployment
identity checks. No Safe proposal or transaction was submitted.
