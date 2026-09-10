# Deriw Release Guides

This directory is the operational source of truth for Deriw node releases.
Every release has its own directory, and every deployment environment has its
own runbook.

## Layout

```text
docs/release-guides/
├── README.md
├── TEMPLATE.md
└── <deriw-version>.<nitro-version>/
    ├── README.md
    ├── common.md
    ├── development.md
    ├── testnet.md
    ├── staging.md
    └── production.md
```

Use these environment names consistently:

| File | Environment |
| --- | --- |
| `development.md` | Development chain, including `rpc.dev.deriw.com` |
| `testnet.md` | Public/internal testnet, including `rpc.test.deriw.com` |
| `staging.md` | Production-like rehearsal environment |
| `production.md` | Production chain |

## Release index

| Release | Source revision | Guides |
| --- | --- | --- |
| `v1.3.1.v3.10.0` | Nitro `7f7d4033d7f1c5e5f56fcc149f6ff60f388e459d` | [release overview](v1.3.1.v3.10.0/README.md) |

## Rules

1. Copy `TEMPLATE.md` when starting a new version.
2. Pin the Nitro repository commit, every changed submodule commit, image
   digest, and WASM module root.
3. Never deploy `latest` or a mutable image tag. Compose must use a registry
   digest (`repository@sha256:...`).
4. Keep addresses and configuration values separate for every environment.
   Never copy development or testnet addresses into production.
5. Do not store private keys, Redis credentials, JWT secrets, RPC credentials,
   or Safe signatures in this repository.
6. Treat binary rollout, WASM-root change, ArbOS activation, and parent-chain
   contract upgrades as separate change sets.
7. Record command output and transaction hashes in the deployment/change
   ticket. Do not commit sensitive operational output here.
8. A production guide must include canaries, traffic draining, snapshots,
   rollback criteria, sequencer handover, and post-deployment validation.

## Required release lifecycle

```text
Build and test
    -> development
    -> testnet
    -> staging rehearsal
    -> production canaries
    -> production rolling deployment
    -> soak
    -> optional protocol activation
    -> final release sign-off
```

An environment may be skipped only when the release owner records the reason
and accepts the additional risk in the change ticket.

