# Add MDBX and Erigon Backend With Auto-Detect

## Context and Problem Statement

We want to use the Erigon storage and history model while keeping Nitro compatible with future upstream updates.
The current execution path is geth-based and uses Pebble/LevelDB with a trie-backed schema. The Erigon layout is
incompatible, so we need a clean way to introduce it without breaking existing logic.

## Considered Options

* Keep geth + Pebble only (no Erigon layout)
* Replace Pebble with MDBX but keep the geth schema
* Add an Erigon backend with explicit config selection
* Add an Erigon backend with auto-detect and migration requirement

## Decision Outcome

Chosen option: "Add an Erigon backend with auto-detect and migration requirement", because it keeps the existing
geth path unchanged for upstream compatibility while enabling MDBX/Erigon as the default once migrated.

### Consequences

* Good, because existing geth-based logic remains intact and future Nitro merges stay low-risk.
* Good, because MDBX/Erigon is opt-in via migration and can be rolled out safely.
* Bad, because an offline migration tool is required before switching to MDBX.
* Bad, because two backends must be maintained until the Erigon path reaches parity.
* Good, because debug/trace RPCs will follow Erigon’s history-based execution path for consistency with Erigon state.
* Bad, because geth-only init/reorg/blocks-reexecutor workflows are not supported on the Erigon backend.
* Bad, because Erigon selection requires complete MDBX data (l2chaindata/arbitrumdata/wasm); partial MDBX datadirs fail fast.
