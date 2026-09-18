# Message 122408091: legacy replay witness gap

## Confirmed observations

The production experiment supplied 20 missing, hash-checked preimages to the
legacy replay. Replay then matched the complete expected end state. This proves
a witness gap for this message; it does not establish that the live validator
has resumed or that later messages will pass.

Three complete node payloads from the supplied JSON were independently decoded
and their Keccak hashes verified locally:

| Node hash | Decoded evidence |
| --- | --- |
| `59c7aa3718a5fe86bf24c335659f7695b18e8064826cb3b336f1dbb7e0ee7852` | Added account leaf, nonce 4, balance 592507810132818960 wei. Its 59-nibble compact-path suffix matches keccak(address `0xb2aab9c1402a51346467303349a3c2557b8599da`). |
| `0e834c821b894e93c15a8fb3e6489f007f6f2db6448d08564bf860bf0a05cb5b` | Added account leaf, nonce 0, balance 5176622206733810 wei. Its 59-nibble suffix matches keccak(L1PricerFundsPoolAddress `0xa4b00000000000000000000000000000000000f6`). |
| `c44143cc157b3dd11b238ac92f07293f55295da5d1fdc76a61682af90d2b5eb5` | Original storage leaf, decoded value `0xb2aab9c1402a51346467303349a3c2557b8599da`. Its compact-path suffix matches the secure key of ArbOS root storage offset 6, infraFeeAccountOffset. |

Both account leaves have empty storage roots and empty code hashes. The infra
slot is `0x15fed0451499512d95f3ec5a41c878b9de55f21878b5b4e190d4667ec709b406`;
its secure trie key is
`94b0ea710acc66b6ee178746922647c9a07d2e7b381b8404815a9bc2b44caeba`.
The full parent paths were not reconstructed locally because the pasted JSON
is truncated. Suffix matches are 236 bits, rather than full trie membership proofs.

## Source mechanism

Compared current `deriw-master-legacy` commit
`7f7d4033d7f1c5e5f56fcc149f6ff60f388e459d` with
`release/v1.2.3.v3.5.1` (`167a1cafb`, geth
`13558e1863ba7bdcefd7df426e26a3ebcdcfdc5e`).

In the older `arbos/tx_processor.go` EndTxHook, custom-price transactions zero
the base fee and compute cost, but infra/poster MintBalance calls are guarded
only by `!arbutil.IsGaslessTx(tx)`. For ordinary custom-price transactions that
are not that special gasless transaction type, these calls still execute.

Current EndTxHook guards those calls with `!p.SkipBaseFeeCheck(tx)`, which also
covers custom-price transactions. That skips the account accesses entirely.

In both geth versions, StateDB.AddBalance loads the account before checking
whether the amount is zero. MintBalance forwards zero amounts to AddBalance.
Consequently a zero mint can require account trie preimages even though the
balance of an existing nonempty account does not change.

The native block recorder collects preimages while executing current native
code. Skipped account accesses can therefore leave its witness insufficient
for a legacy WASM that still performs those accesses, despite identical final
state. The two account identifications strongly support this specific mechanism,
replacing the earlier hypothesis of exclusively missing whitelist storage.

## Limits and next validation

### Commit history follow-up

`git merge-base --is-ancestor release/v1.2.3.v3.5.1 HEAD` succeeds: the old
release is an ancestor of the current branch. Nevertheless, merge commit
`21ec862405cfa0e12e19c9202a74e04762b2de04` (2026-05-14, merging
dev-DER-2646 into integration-DER-2646-v3.10.0) changes the infra/poster guards
relative to its second parent from `!arbutil.IsGaslessTx(tx)` to `!isGasless`.
That variable includes `Pricer().IsCustomPriceTxCheck(tx)`. The changed guard
has no upgrade-version condition preserving the previous behavior.

Commit `034ef210a` (2026-05-18) then replaces that combined predicate with
`SkipBaseFeeCheck(tx)`. Current tx_processor.go lines 761, 776 and 789 retain
this behavior. Existing ArbOS >4 / <2 checks select infra fee availability
and poster destination, respectively; they do not gate this predicate change.

Dockerfile line 231 downloads the prebuilt legacy machine root 767c9a...;
it does not compile that legacy machine from the current checkout. Thus
preserving the correct legacy machine and preserving git ancestry do not
ensure that current native recording includes the legacy machine's read set.

The historical tag is source evidence; its identity with the deployed prebuilt
legacy WASM has not been independently established. All 20 added nodes have
not been assigned full trie paths, and a transaction-level legacy trace has
not been captured. Do not claim that this accounts for every missing node yet.

A candidate repair should preserve legacy-required reads when recording a
legacy witness, or restore the correct version-gated execution behavior after
reviewing consensus semantics. Do not globally restore zero AddBalance calls:
touching an empty account can affect state clearing. Do not hardcode the 20
node hashes or change the expected end state.

Acceptance requires regenerating the witness from the original state without
manual additions, replaying it against root
`0x767c9a47cced7ccc3bf419a7efdd9ffb0f23a5dba42f30f3de64f32e2f82c55f`,
matching the complete expected end state, and checking subsequent messages.
No production process was changed in this investigation.

## Opt-in recording candidate

`execution/gethexec/block_recorder.go` now supports the default-off option
`execution.recording-database.legacy-fee-account-preimages`. It reads the
initial infra fee account, network fee account and L1 fee pool through the
recording StateDB before block execution. This records their account trie
paths without minting, touching or creating accounts. Database read errors
are returned; the existing canonical block hash check remains in place.

This is a limited witness compatibility candidate, not version-gated legacy
execution. It does not cover a fee recipient changed during the block,
pre-ArbOS-2 coinbase poster destinations, or every possible difference in
legacy reads. It also cannot repair a true execution/end-state divergence.

### Isolated validation procedure

1. Keep the old production sequencer and progressing validator running.
   Record their actual image digests and the configured/on-chain WASM roots.
2. Build this checkout as a new immutable candidate image. The current `prod`
   tag does not contain this local change. Keep the test instance on its own
   cloned data directory with staking disabled.
3. Merge this option into the test instance's existing configuration:

   ```json
   {
     "execution": {
       "recording-database": {
         "legacy-fee-account-preimages": true
       }
     }
   }
   ```

   Equivalent CLI option:
   `--execution.recording-database.legacy-fee-account-preimages=true`.
4. Regenerate message 122408091's validation input from the original state
   using `arbdebug_validationInputsAt` with `["0x74bcc9b", "amd64"]`.
   Save as a new file; do not reuse the manually supplemented replay request.
5. Replay the fresh input with the legacy root listed above. Require no manual
   preimages and equality of BlockHash, SendRoot, Batch and PosInBatch.
6. Validate a historical interval and subsequent messages, including custom
   price transactions, reverted transactions, fee account changes, and the
   upgrade boundary. Compare roots to the old execution node. Any remaining
   missing preimage or end-state mismatch blocks promotion of this candidate.

Local validation (updated): a cached Go 1.25.5 toolchain was located. Gofmt
and `git diff --check` pass. The focused test passes with:

```sh
go test execution/gethexec/legacy_fee_preimages.go execution/gethexec/block_recorder_test.go -run '^TestLegacyFeeAccountPreimages$' -count=1
```

The test builds a persisted account trie, captures cold database reads, and
verifies account proofs using only the captured data. It covers funded,
absent and existing empty accounts, checks that the funded fixture initially
has an incomplete witness, and checks that supplementation does not change
account existence or the state root after finalization.

The full gethexec package compiled, but its test process panics during existing
gethhook initialization: `Precompile ArbOwnerPublic must implement
GetScheduledDeriwOSUpgrade`. This prevents running the full package tests in
the current checkout; the focused test avoids that unrelated initialization.
The production message replay has not yet been run with this candidate.

### Longer-term repair

Audit the fee/exemption changes against the source actually used to build
the active legacy WASM. Preserve its behavior below the real agreed upgrade
version and activate new semantics only at that version. Do not infer the
activation version from an image tag or arbitrarily choose ArbOS 60. Validate
the native execution and each intended WASM root using witnesses generated
by the shipped recorder across the upgrade boundary. Git ancestry alone is
not this compatibility test.

`diagnose-replay-preimages.py ORIGINAL.json REPLAY.json` can reconstruct the
available starting trie paths from complete local files and report ArbOS fee
settings and account leaf details. It is read-only and trusts the hash keys;
it does not perform replay or Keccak verification itself.


## Remaining blacklist witness gap (2026-09-16)

The supplied fresh input with the flag enabled contains 411 preimages; all are
byte-identical members of the 422-preimage successful manual replay input.
All Keccak keys in both inputs were checked against their values. All 11
remaining nodes are 17-element branch nodes reachable from the initial ArbOS
storage trie. The starting state root is
`69793d2ffba58ba94fc12ad8c9a38df1340d5e7b22615b5f51958ea8faa10ffc`,
and ArbOS version is 32.

Using the supplied block's two legacy transactions and the actual storage
mapping (subspace 12, sender set 1 / recipient set 2, address index 0), the
following four complete non-membership proofs account for exactly all 11 nodes:

| Transaction index | Check | Address | Secure trie key prefix | Missing nodes |
| --- | --- | --- | --- | --- |
| 1 | sender blacklist | `0x7bb99091f0d709e28c6d73949a56d123fee6108f` | `66eea661` | 3 |
| 1 | recipient blacklist | `0x83ca1aa2bc20e41287154650e4161dc995278e1d` | `c2026057` | 3 |
| 2 | sender blacklist | `0x342223b1bc7b96ed223b04d28dfa9a7254c44646` | `0ab2da79` | 3 |
| 2 | recipient blacklist | `0x43948b78477963d7b408a0e27ae168584c6e07a9` | `a3c90a7a` | 2 |

In historical geth commit `13558e1863ba7bdcefd7df426e26a3ebcdcfdc5e`,
`core/state_transition.go` calls `BlacklistState.IsBlacklistTxCheck` inside
`TransitionDb`. The current execution checks blacklist admission in
`execution/gethexec/blacklist_pre_checker.go`; ordinary replay does not execute
that admission policy. This explains a second legacy read-set gap independently
of the earlier fee-account reads. The exact deployed WASM's source identity
still has not been independently reproduced.

### Recorder extension

The existing `legacy-fee-account-preimages` flag also enables read-only
blacklist supplementation through recording-only sequencing hooks. The name
is retained for existing deployments. Before each input transaction, read its
recipient and signed-sender blacklist membership using a separate system
burner. Membership does not reject the transaction. Database/read errors fail
recording. The hook runs against the current recording state, so earlier
transactions' blacklist changes are visible. Parsing, non-discarding scheduling,
and the canonical block-hash check retain their existing behavior.

This extension changes only gethexec recording code, not ArbOS or WASM source.
It does not restore legacy blacklist enforcement in native consensus execution.
Scheduled redeems bypass this pre-transaction hook; the follow-up below adds
scheduling-time witness reads. Delegated sender differences and other legacy
execution differences still need interval testing. The earlier initial
fee-recipient limitations also remain.

### Validation of the extension

- Focused fee and blacklist witness tests pass. Blacklist tests cover absence
  and membership, nil recipient, complete proofs, and unchanged state root.
- The full gethexec test binary compiles. Running it remains blocked during
  existing precompile initialization; this run reports `Precompile ArbOwnerPublic
  must implement GetDeriwOSVersion`, before any test executes. The focused
  file-list tests avoid that initialization and pass.
- A local fixture experiment loaded the successful witness into a read-only
  source database and seeded the recorded proof with the fresh 411 entries.
  The actual Go blacklist helper, called for the two supplied transaction
  address pairs, made all 422 successful-witness entries available and left
  the initial state root unchanged. This tests witness reads, not full WASM
  execution or a production database's retention of those nodes.
- Full native block recording and legacy WASM replay against a rebuilt image
  remain required, followed by subsequent messages and historical intervals.


## Follow-up: scheduled redeem witnesses (message 123476067)

The supplied canonical block contains an internal transaction, a submit-retryable
transaction (`0x69`), and its automatic redeem (`0x68`). The redeem's ticket ID
matches the submit transaction hash. Its target is
`0x6121117fccecdd6dfa7b3230eacd4f53e12905db`, rather than the submit transaction's
`0x6e` target. The previous recording hook does not run for scheduled redeems.

`legacyRecordingHooks.PostTxFilter` now reads blacklist paths for every retry in
`ExecutionResult.ScheduledTxes`, using the retry's own sender and target. This
hook runs after both input transactions and redeems, covering automatic, manual,
and nested scheduling. Reads happen after the scheduling transaction's writes
and before queued redeems run. Later writes along these paths are recorded by
normal execution; extra reads do not enforce blacklist membership or alter state.
Multiple queued redeems may be prefetched before an earlier sibling executes.

This remains behind `execution.recording-database.legacy-fee-account-preimages`.
No ArbOS, go-ethereum, WASM source, or default execution behavior is changed.
Witness errors are retained and returned by the block-recording wrapper.

Focused tests exercise cold-trie absence and membership proofs through the
scheduled hook, with a parent sender and target different from the redeem's.
They require the recipient proof to be missing before supplementation, verify
both proofs afterward, and verify that the state root is unchanged.

The production input was truncated in the chat. The missing hash
`ae27410c55be70cf13c02b1c7cb4dc2800c1a5ffc4d2c93dd16a8d3dbf14e2fe`
has not yet been matched to a trie path here. A rebuilt native recorder plus
legacy WASM replay for message 123476067 must still confirm the expected hash
`0x71325337992f6a95c1d562622d373cc9513440a158a1aa57ec3a0b678063271c`.
The code coverage gap is confirmed; this is not a claim of successful production
replay or exhaustive legacy compatibility.

Local validation of this follow-up: focused fee/blacklist file-list tests passed,
including the scheduled absence/membership cases; `git diff --check` passed.
The full gethexec test binary compiled. Running it stopped before tests in
precompile initialization (`ArbOwnerPublic must implement
GetScheduledDeriwOSUpgrade`), using the locally generated ABI bindings.
