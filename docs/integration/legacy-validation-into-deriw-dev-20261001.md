# Legacy validation fixes merged into deriw-dev

Date: 2026-10-01. The merge preserves the existing `deriw-dev` execution rules and replay WASM while bringing over the native validation-witness fixes and recovery records.

## Inputs and conflict decisions

| Item | Commit or decision |
| --- | --- |
| Dev baseline | `4b01e334619c95118177651b54b2efffe09705f7` |
| Legacy source | `fec9c58a23a51863d12b31f8bf0121931fa746d1` |
| Common ancestor | `052e41aed2d8` |
| Blacklist admission implementation and tests | Keep dev's versions, including the DeriwOS union rule and authorized emergency removal |
| `go-ethereum` | Keep dev's pin `52064791abfa175fb23229c20c11dd84fba894cf` |
| Precompile interfaces | Keep dev's pin `76d5e0b3f2a3f34da602505824eef7b7735a97fa` |
| Build and packaged machines | Keep dev's `Dockerfile`, `Makefile`, module dependencies and existing machine list |

Dev already contains the native gasless estimation hook and newer estimation behavior. The legacy `go-ethereum` pin moves that hook into a file excluded from WASM. Applying that older pin would change dev's replay inputs, so the merge retains dev's dependency and hook placement.

## Imported native behavior

The only Go implementation changes relative to the dev baseline are these six files:

- `execution/gethexec/block_recorder.go`
- `execution/gethexec/block_recorder_test.go`
- `execution/gethexec/legacy_fee_preimages.go`
- `execution/gethexec/legacy_blacklist_preimages.go`
- `execution/gethexec/legacy_blacklist_preimages_test.go`
- `execution/gethexec/legacy_recording_hooks.go`

The opt-in `execution.recording-database.legacy-fee-account-preimages` setting supplements validation witnesses with legacy fee-account and blacklist trie reads, including scheduled retryable redeems. Its default remains false. These reads collect witnesses without changing account balances, blacklist admission rules or consensus execution code. Compatibility with a particular historical WASM still depends on replaying that history.

All tracked sources in `arbos`, `precompiles`, `gethhook`, `cmd/replay`, the other replay dependencies, Rust crates and the relevant dependency pins remain identical to the dev baseline. Dev's S3 endpoint addressing behavior is also retained.

## WASM preservation verification

Before and after the merge, the replay dependency graph contains 384 packages and 577 local input files with identical content hashes. `execution/gethexec` is outside that graph.

Both snapshots were built at `/case` with the same offline builder, generated precompile interfaces, compiler flags and WASM libraries. The interfaces were regenerated from dev's unchanged pinned Solidity sources after stale cached bindings caused the initial test run to fail. The successful comparison includes the repository's `remove_reference_types.sh` normalization step.

| Check | Before and after |
| --- | --- |
| Replay WASM SHA-256 | `a3fb524f629a5d09997a49aa6009111123c7624f225471f88f1e66e7d3a396c6` |
| Prover module root | `0x9865afd986cddb6b554611fb9baec59c59bbe454db62eeee6d979b0ceb11247b` |
| Binary comparison | Byte-identical |
| Compiler | Go 1.25.13, Linux arm64 host, `GOOS=wasip1 GOARCH=wasm CGO_ENABLED=0` |
| Builder image | `sha256:a14b2422bc67a2a7774a5a82be5acd21edf0805aa329b9464d918caa3eb2aa9f` |

This establishes that the merge leaves dev's WASM unchanged under the same build environment. This comparison root is specific to the local build environment. The release Dockerfile pins Go 1.25.9; no new release machine was published or substituted. The existing packaged machines, including the historical production machine `0x121d685e2fdb0e3291592d6b90bd70d503951335d19d96455448eb7a14d17421`, remain in the unchanged Dockerfile.

## Tests and archived records

The following checks passed without network access or production operations:

```sh
go test -count=1 ./execution/gethexec ./execution/nodeinterface ./arbos/... ./arbutil ./precompiles ./gethhook ./util/s3client
PYTHONDONTWRITEBYTECODE=1 python3 scripts/validator-recovery/tests/run-offline.py
PYTHONDONTWRITEBYTECODE=1 python3 docs/validator-recovery/catalog.py --verify
node --test scripts/safe-proposals/blacklist-calldata.test.mjs scripts/safe-proposals/safe-proposal.test.mjs
```

The Go checks cover the legacy witness proofs, scheduled redeems, dev blacklist admission, subaccounts, precompiles, RPC estimation and S3 addressing. The recovery suite passed 128 tests across 17 suites; the Safe helpers passed 14 tests. The catalogue verifies 407 indexed files and preserves all 317 moved archive files byte for byte.

The recovery catalogue's implementation references were refreshed for this merged branch. Historical reports, transaction packages, replay evidence and distribution payloads retain their original bytes and scope. See the [recovery record](../validator-recovery/README.md), [manual withdrawal guide](../manual-withdrawal/README.md) and [Safe transaction guide](../safe-transactions/README.md).

The merge does not perform a deployment, publish a WASM machine, change on-chain parameters or push the branch.
