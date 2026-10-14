# Production DeriwOS bootstrap fix

This draft is based on accepted test source `12d8f6b32cbb`. It preserves production chain 2886's legacy behavior until governance supplies an explicit bootstrap timestamp. Existing dev/test behavior and already-active DeriwOS versions keep their deployed semantics.

The fix removes the added pending-version gas read from production's DeriwOS-0 owner methods when no custom upgrade is armed. A scheduled custom upgrade still enforces protected-address rules. The version-0 bootstrap configuration lookup is metered against the actual call budget, including its JSON work; zero-gas calls cannot scan the configuration. Already-active versions skip that probe and retain their existing gas accounting. New custom configuration, scheduling and query methods remain unknown on production until ArbOS 60 is active and the child-block timestamp reaches the configured boundary.

The boundary is an optional top-level numeric field, `deriwOSBootstrapTime`, in the complete serialized on-chain chain configuration. Missing, zero or invalid values keep production's version-0 custom APIs disabled. Governance uses the existing **`ArbOwner.setChainConfig(string)`** ABI through the production child UpgradeExecutor/Safe. Preserve every existing configuration field and add only this field. An old binary stores these bytes unchanged and ignores this field when parsing the known Ethereum settings. No new precompile is needed to prepare the boundary.

No actual timestamp is selected in this draft. Choose a future time only after the new artifact passes replay and canary checks. Update every node and proving worker and the parent root before that time. At the boundary, query `(60, 0)`, then schedule DeriwOS 1 through 5 with acceptance between versions, followed by 6 in a separate window. Preserve the bootstrap field until version 1 has activated; the stored active version then keeps the APIs available independently of that field.

## Local regression verification

The final local suite passes 11 packages with 415 passing tests/subtests. It covers the four legacy owner methods at their exact old success threshold and immediately below it, all 11 introduced API entry points before and at the bootstrap timestamp, the existing owner configuration/scheduling path, protected addresses after scheduling, malformed metadata, deployed dev/test behavior and metered handling of large configurations. The suite also exercises synthetic production ArbOS/DeriwOS migrations. Native tests use Darwin arm64 / Go 1.25.9 and reused native libraries; they do not substitute for Linux release artifacts or production-state replay.

The replay code compiled and normalized successfully in a pinned Linux Go 1.25.9 builder. Dockerfile checks passed. The full machine and deployable runtime image still require a clean build and qualification below. No production activation time is selected by these tests.

## Build and artifact requirements

A code patch is not a deployable production artifact. Commit the reviewed change and build from a clean, fully initialized Linux amd64 source tree. The new Docker target is **`nitro-node-validator-candidate`**. It builds the replay environment from this source and installs the computed machine under its named root, makes `latest` point to it, and preserves the current 121d and historical 3691 bundles and split-validator entrypoint. It refuses to overwrite an existing named machine. The old `nitro-node-validator` target still reuses published machines and must not be used to package this replay-code fix.

Record the source commit, native version, compiler/base-image/dependency identities, image registry digest, freshly computed root and all bundle hashes. Verify the new bundle inside the actual image with the prover, publish the bundle, and repeat native/WASM tests. Do not label it as 12d or reuse 3691 as its new root. Do not use a cached machine whose library provenance is unresolved.

## Required qualification

- Replay representative production state before the bootstrap marker, including old successful and reverted owner calls and formerly unknown selectors, against the current 121d machine.
- Validate before, at and after the marker using the new image/machine; compare blocks, receipts, gas, state roots and validation global states. Preview and execute the existing configuration ABI on an isolated state copy.
- Confirm dev/test and active DeriwOS histories remain compatible, then qualify the new machine against test's current 3691 root before a separate test root update.
- Rehearse production DeriwOS 0 to 1 through 5 and later 6, including bridge mappings, parent authentication, protected addresses and production subaccount clients.

The previously scheduled legacy ArbOS 60 activation is independent of this draft and retains its existing readiness checks. No production/test deployment, timestamp proposal, signature, root update or final release tag is created by this code change.
