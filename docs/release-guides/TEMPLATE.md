# Release `<Deriw version>.<Nitro version>`

Copy this file into a new version directory and create `development.md`,
`testnet.md`, `staging.md`, and `production.md` beside it.

## Release manifest

| Item | Value |
| --- | --- |
| Release owner | `<name>` |
| Release status | `draft / candidate / approved / deployed / rolled back` |
| Nitro branch | `<branch>` |
| Nitro commit | `<40-character commit>` |
| go-ethereum commit | `<40-character commit>` |
| Other changed submodules | `<path and commit>` |
| Previous release | `<version>` |
| Image tag | `<registry/repository:immutable-tag>` |
| Image digest | `<registry/repository@sha256:...>` |
| Binary version output | `<nitro --version>` |
| Current WASM root | `<bytes32 or not applicable>` |
| Target WASM root | `<bytes32 or unchanged>` |
| Current ArbOS version | `<value>` |
| Target ArbOS version | `<value or unchanged>` |

## Scope

- User-visible changes:
- Consensus changes:
- RPC-only changes:
- Configuration changes:
- Database migrations:
- Parent-chain contract changes:
- Explicitly out of scope:

## Compatibility and risks

- Old/new node compatibility:
- Old/new WASM compatibility:
- Submodule compatibility:
- Database rollback behavior:
- Sequencer mixed-version behavior:
- DAC compatibility:
- Known warnings:

## Build and artifact verification

Document exact checkout, submodule initialization, test, build, image push,
digest capture, binary-version check, and machine-root check commands.

## Environment values

Do not put secrets in this table.

| Value | Development | Testnet | Staging | Production |
| --- | --- | --- | --- | --- |
| L2 chain ID | | | | |
| Parent chain ID | | | | |
| L2 RPC | | | | |
| Parent RPC secret reference | | | | |
| Rollup | | | | |
| UpgradeExecutor | | | | |
| Chain-owner Safe | | | | |
| Current WASM root | | | | |
| Target WASM root | | | | |
| Coordinator Redis secret reference | | | | |

Every environment runbook must contain:

1. Inventory and rollout order.
2. Preflight queries and expected results.
3. Configuration delta from the previous version.
4. Snapshot and rollback procedure.
5. Deployment commands.
6. Health and finalized-block comparison.
7. Release-specific functional tests.
8. Sequencer and poster procedure, when applicable.
9. Parent-chain contract version discovery and any required upgrade.
10. Optional governance operations, clearly separated from binary deployment.
11. Evidence and sign-off checklist.

## Go/no-go gates

- [ ] Working tree and submodules are clean.
- [ ] All pinned commits are reachable from their remotes.
- [ ] Required tests pass.
- [ ] Image is deployed by digest.
- [ ] Image version and WASM roots match this manifest.
- [ ] Current on-chain root is present in the image.
- [ ] Database/disk rollback has been rehearsed.
- [ ] Development validation passed.
- [ ] Testnet validation passed.
- [ ] Staging rehearsal passed.
- [ ] Production maintenance and rollback owners are present.

## Rollback criteria

List objective conditions that stop the rollout, such as finalized-block hash
divergence, validation failure, missing-machine errors, database failure,
sequencer split brain, stalled blocks, failed batch posting, DAC failure,
or unacceptable RPC error/latency changes.

Document separately whether rollback remains possible after each governance
operation. Do not assume an old binary is safe after protocol activation.

## Release evidence

- Change ticket:
- Test run:
- Image digest:
- Snapshot IDs:
- Host deployment timestamps:
- Safe transaction hashes:
- Monitoring dashboard/event:
- Final approvers:
