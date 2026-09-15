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
