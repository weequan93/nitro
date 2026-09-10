# Deriw v1.3.1 / Nitro v3.10.0

Status: release candidate.

This release upgrades nodes from `release/v1.2.3.v3.5.1` and adds the
blacklist/subaccount admission fix and gas-estimation fix without changing the
v1.3.0 WASM module root.

## Release manifest

| Item | Value |
| --- | --- |
| Branch | `deriw-master-legacy` |
| Nitro commit | `7f7d4033d7f1c5e5f56fcc149f6ff60f388e459d` |
| go-ethereum commit | `e582a7d5fb4887cbd3b4c7bb4fe74d2a5b06bddc` |
| Base release | `release/v1.3.0.v3.10.0` (`052e41aed`) |
| Blacklist/subaccount change | `5acf409ec` |
| Estimate-gas change | `b15ef4d67` |
| Build-path fix | `4774ea936` |
| WASM-neutral estimate fix | `7f7d4033d` |
| Suggested release tag | `release/v1.3.1.v3.10.0` |
| Binary version | `v1.3.1.v3.10.0-7f7d403` |
| Target WASM root | `0x121d685e2fdb0e3291592d6b90bd70d503951335d19d96455448eb7a14d17421` |

The image built from this revision has been verified to return the target root
from `/home/user/target/machines/latest/module-root.txt`. Its root matches the
unmodified `release/v1.3.0.v3.10.0` baseline.

## Activation boundaries

- The blacklist admission fix becomes effective when a sequencer runs the new
  binary. Both sequencers must be upgraded before enforcement is guaranteed.
- The estimate-gas fix becomes effective on each upgraded RPC node. Mixed RPC
  pools may return different results during rollout.
- Neither fix requires a WASM-root change or ArbOS activation.
- A WASM-root change, ArbOS 60 activation, and parent-chain contract upgrade
  are optional, separately approved operations.
- This branch uses the standard `scheduleArbOSUpgrade(uint64,uint64)` flow. It
  does not use the custom DeriwOS5 scheduling calls.

## Guides

- [Shared build, deployment, validation, and rollback steps](common.md)
- [Parent-chain contract version and upgrade procedure](parent-contracts.md)
- [Development](development.md)
- [Testnet](testnet.md)
- [Staging](staging.md)
- [Production](production.md)
- [DER-2646 v3.10 precautions](../../DER-2646-v3.10.0-release-precautions.md)

Before using an environment guide, copy its value table into the change ticket
and fill every required address. Values must be obtained from that environment,
not from another guide.
