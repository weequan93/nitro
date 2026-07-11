# Recovery Docs

This folder contains operational notes and helper scripts for the Deriw Nitro
rollup recovery work.

- `rollup-checkpoint-recovery.md`: emergency runbook for replacing a diverged
  L1 rollup assertion with a trusted sequencer checkpoint.
- `scripts/scan-node-created-hashes.js`: scans historical `NodeCreated` events
  and checks whether their asserted L2 block hashes exist on the trusted L2 RPC.
- `scripts/checkpoint-position.sh`: computes the `Batch` and `PosInBatch` for a
  trusted L2 checkpoint block using only `sh`, `curl`, and `jq`.
- `scripts/rollup-state.sh`: read-only rollup/staker state snapshot.
- `scripts/safe-add-owner-threshold1.sh`: adds an owner to a threshold-1 Safe
  using a current Safe owner key.

These commands are intended for operators who already control the rollup owner
or upgrade executor. Do not run the checkpoint reset path unless the team has
explicitly chosen the current sequencer history as canonical.
