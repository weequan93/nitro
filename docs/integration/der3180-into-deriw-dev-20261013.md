# DER-3180 merged into current deriw-dev — WASM comparison

Prepared 13 October 2026. Review branch: `codex/merge-der3180-into-dev`.

The merge combines current dev `18c0a97530a90cd22e49269d475072d2a015c904` with feature tip `726330ebc19e2f5e5af205a92a15ce3a4777b5a9`. It merges cleanly, supports DeriwOS 6, and preserves the legacy validation-witness helpers, scheduled-redeem recording, opt-in recorder flag, and S3 endpoint behavior. No production or staging branch was promoted, no chain write was performed, and no release tag was created.

## Before and after comparison

The correct baseline is the DER-3180 feature branch already used by the deployed dev binary. Comparing against the pre-merge dev branch's DeriwOS-5 implementation would include the intentional consensus change introducing 6.

The Linux Go 1.25.9 builds use identical `/workspace`, `/usr/local/go`, and `/root/go/pkg/mod` paths. Version-6 ABI bindings were regenerated from pinned Solidity sources with Solc 0.8.17. Unchanged core contract artifacts and native libraries were reused. The comparison generated the same replay graph: 384 packages and 577 local inputs. Neither `execution/gethexec` nor `util/s3client` is in that graph.

| Check | Result |
| --- | --- |
| Replay inputs, feature before vs merge after | Identical |
| Normalized replay WASM | Byte-identical, 27,808,436 bytes |
| Replay WASM SHA-256, both builds | `c4915584f1e1503cb910ce4817d95e74296ea02e198ffb1dada587ca5bd575e4` |
| Deployed dev's published replay WASM | Exact same bytes and SHA-256 |
| Native regressions | 8 packages passed: blacklist, ArbOS state/execution, precompiles, gethexec, hooks, node interface, S3 |
| Safe helper tests | 18 passed |

## Machine roots and limits of the comparison

Dev's current parent Rollup root is `0x3691fa25daae0d0c32e86493e36d392ca544b0039575d134a84584c11d1f7a8d`. Its published compressed machine was downloaded and the prover independently returned that exact root. The compressed-file SHA-256 is `e0c61a7f038103226f256c0d99cdb8e30298fd3d4ebf7c863e1cab82fc73bdde`.

A separate local full-machine generation using cached native prover/WASM libraries produces `0xbe23815995337d179de9443d865ba455b536597c1dafd2e1e03847bb4395dee8` both before and after; the compressed machines are byte-identical to each other. This root does not match the deployed dev root. Those cached build artifacts' release provenance was not established, so this is merge-preservation evidence under a fixed local environment, not a replacement release machine. Do not promote the local `be238` machine or change an on-chain root based on this run. Reuse the verified deployed machine, or reproduce the full pinned release builder and resolve the discrepancy before publishing a freshly generated machine.

Test's supplied Rollup and Inbox share bridge `0x0572018Db4caadA1A71C9df9C88Fd12379c1d152`. Test currently runs ArbOS 60 / DeriwOS 5 and its parent root is `0x184884e1eb9fefdc158f6c8ac912bb183bf3cf83f0090317e0bc4ac5860baa39`, different from dev's. The public artifact download returns HTTP 403; test's actual machine files and runtime packaging remain to be verified by its operator. A single new machine cannot equal both current roots. Retain each environment's historical/current machines and separately prove compatibility before activation or root changes.

## Outstanding release checks

- Verify the machine/root/checksums inside the actual dev and test validator images or immutable mounted bundles. The source check alone does not identify the running fleet.
- Obtain and retain test's `184884` machine bundle. The feature Dockerfile's extra packaged root is dev's `3691`; it does not prove test's bundle is included.
- If rebuilding the complete machine, use the full release toolchain, library sources, build paths, and parser/prover provenance; do not use the local cached reproduction root as the production target.
- Record final immutable node/validator digests and runtime machine availability. Rehearse current-root validation and activation-boundary replay for the final artifacts.
- Promote the same reviewed release only after those checks. Production currently uses `deriw-master-legacy`; merging code does not execute a Safe transaction or activate DeriwOS.

Raw findings and file hashes are in [the comparison evidence](der3180-into-deriw-dev-20261013.json). Temporary build snapshots and logs are retained at `/private/tmp/deriw3180-verification-20261013` for this session. They are not durable release storage. Archive the required artifacts/logs in the release evidence system before cleanup.
