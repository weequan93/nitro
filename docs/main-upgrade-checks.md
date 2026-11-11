# Main image upgrades and configuration retention

The 5 November 2026 tuning used seven Compose inputs, compaction pacing
67,108,864 bytes/s, compaction concurrency 1 and trie-clean cache 1536 MiB.
By the 11 November inspection only the base Compose input was active: pacing
was absent/default 0, concurrency was explicitly 2 and caches were undeclared.
The current trial restored pacing to 64 MiB/s in the bound JSON, retaining
concurrency 2 and the existing cache declarations. It restores the pacing
setting; the other 5 November tuning values have not been restored.

The first paced hour recorded fewer slow requests and no retained >5-second
sequencing warning, with lower sampled Oracle batch sizes than the unpaced
hour. Debt repeatedly drained to approximately 91–94 GiB and rose within
compaction cycles. Retain the current setting provisionally; verify naturally
occurring large batches and debt across further cycles before attributing the
whole latency change to pacing or changing cache/concurrency settings.

The subsequent two-hour paced window also recorded no >5-second sequencing
warning, one retained slow submission at 1.088 seconds, zero write-stall and
bypass increments, and recurring debt lows near 92–94 GiB. Its periodic samples
did not cover the original large-block workload. See the
[11 November observation record](main-rpc-pacing-20261111.md) for evidence,
configuration identity and remaining validation.

## Save a baseline before upgrading

Install both `scripts/check-main-runtime.py` and
`scripts/trial-compaction-pacing.py` beside each other on the Linux node host.
The check uses Docker inspect/top/Compose rendering and reads the single bound
node JSON. It performs no RPC, database open, service change or restart.

```bash
BASELINE="/data/deploy-backups/main-runtime-$(date +%Y%m%d-%H%M%S).json"
python3 -B /data/scripts/check-main-runtime.py --save "$BASELINE"
```

The current expected declarations are pacing 64 MiB/s and concurrency 2. The
snapshot records allowlisted numeric cache declarations, image identity and
hashes of the full JSON, native argv, container layout and rendered Compose
model. Full arguments, environment and config secrets are not stored. Existing
baseline files are refused. Save the printed baseline path with the release.

Undeclared cache settings remain `image_default_unresolved`; the tool cannot
prove their effective values or that a new image keeps the same defaults.
Review new-image defaults for those settings, or pin reviewed explicit values
in a separately validated change. A JSON file newer than the container start
is refused because its cold-loaded values cannot be confirmed by this check.

## Preserve inputs and verify after upgrading

Carry every original Compose file reported by the running container's
`com.docker.compose.project.config_files` label into the upgrade command, in the
same order, and retain the bound JSON. Keep the image change separate from
configuration tuning. Pin the verified release image digest and preserve a
rollback copy of the deployment inputs using the deployment procedure.

After the new image is running, resolve the verified release reference to its
local **Docker image ID**, then compare with the saved baseline:

```bash
EXPECTED_IMAGE_ID="$(docker image inspect --format '{{.Id}}' "$RELEASE_IMAGE")"
python3 -B /data/scripts/check-main-runtime.py \
  --baseline "$BASELINE" --expected-image "$EXPECTED_IMAGE_ID"
```

Only the main image field is excluded from the Compose/layout comparison; a
changed image requires its explicitly verified image ID. An added image-only
override is allowed when its resolved model otherwise matches. Lost tuning
overrides, other command/model changes, JSON edits, resources, mounts and ports
are reported as mismatches. Intentional changes need a new baseline after
validation; preserve the old baseline with the release record.

`declarations_and_inputs_match` verifies these declarations and fingerprints,
not RPC readiness, chain progress, new binary defaults or performance. Check
startup, heads, both validators, restart/OOM status and pacing/stall/debt metrics
separately, then compare similar transaction workloads after startup settles.
Run the offline checks with `python3 -B scripts/test_check_main_runtime.py` and
`python3 -B scripts/test_trial_compaction_pacing.py`.
