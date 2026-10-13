# DER-3180 merged into current deriw-dev — WASM comparison

Prepared 13 October 2026. Review branch: `codex/merge-der3180-into-dev`.

**Identity correction:** the supplied test Rollup reports chain ID 19005971302, while the public test RPC is chain 2885. The initial report associated that root with public test before checking `Rollup.chainId()`; that association is withdrawn. The repository-configured test Rollup `0xb6a39f55E4C4397FE799BeDCc16fFa895950CFF9` instead reports 2885 and root `0xb27443e2…`, matching the second container's latest metadata. Its public machine was independently computed to the same root. Runtime configuration and container file identity remain to be confirmed. Dev identity and the source/WASM comparison remain verified.

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

The public test RPC, chain 2885, runs ArbOS 60 / DeriwOS 5. The previously supplied Rollup and Inbox share bridge `0x0572018Db4caadA1A71C9df9C88Fd12379c1d152`, but that Rollup reports chain 19005971302. Its `184884` root is an archived upstream `consensus-v32` entry and is not established as chain 2885's current proving root. At Arbitrum Sepolia block 318712080, repository-configured test Rollup `0xb6a39f55E4C4397FE799BeDCc16fFa895950CFF9` reports chain 2885, root `0xb27443e2ace920ad81d5ce74afa048be7ba11638e035fcf7bc75faf32e960676`, Inbox `0xAcb00b245154679E37E478a752188574834fFc29`, and owner `0x678815F2c63466f557024D8CCE25BaeeB4A23359`. The downloaded public machine computes to `b274`; its compressed SHA-256 is `00d3c4cc307c05855e894f55687ccbddef190e80309311495fb6035056567767`. Its replay SHA-256 is `54a7b55f05f47742d2a7fa5aabd8c7387eb3f317064207cf5c80b70b6f1055db`, different from dev. Confirm actual runtime chain/Rollup settings and file hashes before any parent proposal. Retain applicable historical/current machines and separately prove compatibility before activation or root changes.

## Outstanding release checks

- Verify the machine/root/checksums inside the actual dev and test validator images or immutable mounted bundles. The source check alone does not identify the running fleet.
- Confirm runtime chain/Rollup configuration against the repository-configured chain-2885 Rollup and its independently verified public `b274` bundle. Operator outputs show first-container latest `3691`, second-container latest `b274`, and archived `184884` in both. They do not include container chain IDs, computed-root checks, or file checksums. The merge matches dev's replay; current test replay differs.
- If rebuilding the complete machine, use the full release toolchain, library sources, build paths, and parser/prover provenance; do not use the local cached reproduction root as the production target.
- Record final immutable node/validator digests and runtime machine availability. Rehearse current-root validation and activation-boundary replay for the final artifacts.
- Promote the same reviewed release only after those checks. Production currently uses `deriw-master-legacy`; merging code does not execute a Safe transaction or activate DeriwOS.

Raw findings and file hashes are in [the comparison evidence](der3180-into-deriw-dev-20261013.json). Temporary build snapshots and logs are retained at `/private/tmp/deriw3180-verification-20261013` for this session. They are not durable release storage. Archive the required artifacts/logs in the release evidence system before cleanup.
