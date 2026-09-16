# pathdb-migrate

`pathdb-migrate` is an offline migration helper for creating a pathdb execution
database from an existing hash-state execution database copy.

This tool is intentionally conservative:

- it never migrates in place;
- the source database is opened read-only;
- the destination must be a separate copy of the same chain database;
- only one selected state root is converted, normally `latest`;
- validators/stakers should not use pathdb unless Nitro explicitly supports it.

## Build on a Linux server with Docker

Run these commands in Bash from the Nitro repository root. Docker must be
installed and able to download images and dependencies. Host Go, Node.js, and
Foundry installations are not required. The repository requires Go 1.25 and
generated Solidity bindings; a plain Go container cannot build a fresh checkout
until those bindings exist.

### 1. Update the branch and submodules

```bash
git switch dev-migration-tool3 &&
git pull --ff-only &&
git submodule update --init --recursive
```

### 2. Build the contract artifacts

The contracts builder installs Foundry under `/root/.foundry/bin`. Explicitly
add that directory to `PATH`: sourcing `.bashrc` in a noninteractive Docker
build may leave `forge` unavailable. This command supplies the adjusted
Dockerfile through stdin without editing the repository's Dockerfile.

```bash
set -o pipefail

sed '/make build-solidity/i ENV PATH="/root/.foundry/bin:${PATH}"' Dockerfile |
docker build \
  --progress=plain \
  --target contracts-builder \
  -t nitro-migration-contracts:local \
  -f - .
```

Wait for this build to succeed before continuing. Rebuild this image after
updating contract sources or submodules so its artifacts match the checkout.

### 3. Generate bindings and build the executable

The following uses `printf` instead of a heredoc so indentation introduced when
pasting commands cannot turn a closing heredoc marker into a Dockerfile
instruction. Keep each quoted string on one line.

```bash
set -o pipefail

printf '%s\n' \
  'FROM nitro-migration-contracts:local AS contracts' \
  'FROM golang:1.25-bookworm AS builder' \
  'WORKDIR /workspace' \
  'COPY . .' \
  'COPY --from=contracts /workspace/ /workspace/' \
  'RUN go run ./solgen/gen.go' \
  'RUN go build -o /out/pathdb-migrate-updated ./cmd/pathdb-migrate' \
  'FROM scratch' \
  'COPY --from=builder /out/pathdb-migrate-updated /pathdb-migrate-updated' |
DOCKER_BUILDKIT=1 docker build \
  --progress=plain \
  --output type=local,dest=./migration-build \
  -f - .
```

The executable is exported to `./migration-build/pathdb-migrate-updated` for
the Docker builder's platform. These build steps do not open the chain databases.

### 4. Check the executable

```bash
./migration-build/pathdb-migrate-updated --help 2>&1 |
  grep -E 'max-transition-gap|spill-workers'
```

Both flags must appear. Use `./migration-build/pathdb-migrate-updated` in place
of `./pathdb-migrate` or `go run ./cmd/pathdb-migrate` in the examples below.
Checking out a branch does not update an existing executable.

### Build troubleshooting

- `go: command not found`: use the Docker workflow above; it supplies Go 1.25.
- Missing `solgen/go/precompilesgen` or `solgen/go/bridgegen`: these are local
  generated packages, not dependencies to install with `go get`. Complete the
  contract build and run the binding generator as shown above.
- `forge: No such file or directory`: use the explicit Foundry `PATH` in step 2.
- `sed: unterminated s command`: a pasted substitution may have been split
  across lines. Use the single-line `sed` insertion in step 2.
- `unknown instruction: DOCKERFILE`: an indented heredoc terminator became
  Dockerfile content. Use the `printf` command in step 3.

## Flow

1. Stop the source node cleanly.
2. Make a full filesystem copy of the execution database, including the ancient
   directory.
3. Run a dry traversal against the source:

   ```sh
   go run ./cmd/pathdb-migrate --src.chain-data /data/node/l2chaindata
   ```

4. Run the migration into the copied destination:

   ```sh
   go run ./cmd/pathdb-migrate \
     --src.chain-data /data/node/l2chaindata \
     --dst.chain-data /data/node-path/l2chaindata \
     --migrate \
     --verify
   ```

5. Optional: after successful verification, delete copied hashdb trie nodes and
   compact the copied destination:

   ```sh
   go run ./cmd/pathdb-migrate \
     --dst.chain-data /data/node-path/l2chaindata \
     --verify-only \
     --cleanup-legacy-hash-state \
     --compact
   ```

   For the strictest migrated output, also remove stale hashdb flat snapshot
   entries. The node will rebuild pathdb snapshots after startup:

   ```sh
   go run ./cmd/pathdb-migrate \
     --dst.chain-data /data/node-path/l2chaindata \
     --verify-only \
     --strict-cleanup \
     --compact
   ```

   The same cleanup can be appended to the migration command:

   ```sh
   go run ./cmd/pathdb-migrate \
     --src.chain-data /data/node/l2chaindata \
     --dst.chain-data /data/node-path/l2chaindata \
     --migrate \
     --verify \
     --strict-cleanup \
     --compact
   ```

6. Start a non-validator node from the destination copy with path state enabled,
   for example `--execution.caching.state-scheme=path`.

To verify an already converted destination without rerunning migration:

```sh
go run ./cmd/pathdb-migrate \
  --dst.chain-data /data/node-path/l2chaindata \
  --verify-only
```

## Find a state-root block offline

When a destination was migrated with `--block latest`, the source may later
advance. Use the read-only scanner to recover the canonical block whose header
contains the destination's state root; no RPC node is required:

```sh
go run ./cmd/pathdb-migrate \
  --src.chain-data /data/node/l2chaindata \
  --find-state-root <DESTINATION_STATE_ROOT> \
  --find-start-block 0 \
  --find-end-block latest
```

The scanner prints `Found state root` with the block number and block hash.
Use that number as `--archive-history.end-block`. It opens the source database
read-only; stop any node that owns the source database before running it.

To compare two offline databases block by block:

```sh
go run ./cmd/pathdb-migrate \
  --src.chain-data /data/source/l2chaindata \
  --dst.chain-data /data/destination/l2chaindata \
  --compare-source-destination \
  --compare-start-block 0 \
  --compare-end-block latest
```

The command reports the first canonical block-hash or state-root mismatch and
opens both databases read-only. On success it also prints the source and
destination head numbers and roots, the destination PathDB root, and whether
that PathDB root appeared in the compared canonical range. If `pathRootFound`
is false, rerun through `--compare-end-block latest` before treating the PathDB
as corrupt: the PathDB may simply represent a later block than the originally
chosen archive-history end block. It does not compare raw database files.

If an earlier process exited after migration finished but before clean shutdown,
the unfinished-conversion canary can be cleared after a successful verification:

```sh
go run ./cmd/pathdb-migrate \
  --dst.chain-data /data/node-path/l2chaindata \
  --verify-only \
  --ignore-unfinished-conversion
```

## Repair an orphaned current PathDB root

If the offline comparison reports `pathRootFound=false` through both database
heads, the destination PathDB root is not canonical. After making a full backup
of the destination, the trie layer can be replaced in place from an explicit
canonical source block:

```sh
go run ./cmd/pathdb-migrate \
  --src.chain-data /data/node/l2chaindata \
  --dst.chain-data /data/node-path/l2chaindata \
  --repair-path-state \
  --block 123600452 \
  --state-workers 4 \
  --state-max-inflight 8
```

This mode requires matching source and destination canonical headers at the
selected block, a hash-scheme source, a PathDB destination, and an empty PathDB
state-history freezer. If the current PathDB root already matches the selected
canonical root, repair first performs a full trie verification and exits without
rewriting when every descendant node is readable. If that verification fails,
repair falls through to the rewrite so missing or corrupt descendant nodes are
restored even though the root metadata matches. It writes an
unfinished-conversion canary before changing trie nodes, rewrites account and
storage nodes from the selected source root, updates PathDB root metadata,
performs another full trie verification, and removes the canary only after
success. It does not repair or preserve existing PathDB state history and
therefore refuses to run when history entries already exist.
Full verification logs account and storage traversal counters every 30 seconds;
these verification counters are distinct from the periodic migration counters.
After a previous full verification has already proved that descendants are
missing or corrupt, add `--force-repair-path-state` to the same repair command
to skip repeating that initial verification and begin the rewrite immediately.
If an interrupted repair left the conversion canary behind, restore the backup
or rerun the same repair with `--ignore-unfinished-conversion`; the canary is
still removed only after full verification succeeds.

Normal migration and PathDB repair can process independent account storage
tries concurrently. Account-trie traversal remains sequential, while each
storage worker uses its own bounded write batch. Start with four workers and an
in-flight limit of eight; increase one worker at a time only when storage has
spare random-read capacity and RSS remains safely below the host limit. These
flags are separate from `--archive-history.workers`.
The periodic state-copy report includes scheduled, completed and in-flight
storage jobs, active workers, nodes per second and MiB per second. A sustained
active-worker count below the configured worker count means account traversal
or storage latency is limiting scheduling; a full active-worker count with low
throughput usually means the disk is saturated.

## Resuming archive-history migration

Add `--archive-history.resume` to an archive-history command to start or resume
a matching migration. Keep the original `--archive-history.start-block`,
`--archive-history.end-block`, source chain, and `--archive-history.skip-missing-states`
setting. Worker counts, caches, spill settings, and the transition gap limit may
change between attempts. For example, retry with `--archive-history.workers 1`
if parallel processing discovers missing internal trie data.

This build syncs a migration manifest before writing history, even when the
first run omits `--archive-history.resume`. On restart it validates all retained
history metadata against the canonical source headers and the parent/root chain,
repairs root-to-state-ID mappings, and continues after the last retained
transition. This scans metadata but does not recompute completed trie diffs.
Skipped blocks and unchanged roots after that transition may be scanned again.
Progress and coverage counters describe the resumed segment; the state ID
includes previously retained records. Retain logs from earlier segments to
assess total missing-state coverage.

The source and destination must remain offline and unchanged between attempts.
Resume rejects a different range or source, incompatible metadata, and history
created by older builds without a manifest. It cannot be combined with
`--archive-history.reset-history`. Do not reset history merely to bypass a resume
validation error. A completed matching run can also be resumed without appending
duplicate records. Resume validates metadata continuity, not a full replay of
every stored account/storage history payload.

## Archive history with missing states

Full archive-history migration can bridge retained hashdb states while skipping
state roots that are genuinely unavailable in the source. Large gaps use a
temporary disk-backed trie diff by default, avoiding an in-memory map of every
changed account and storage slot:

```sh
GOMEMLIMIT=40GiB GOMAXPROCS=8 ./pathdb-migrate \
  --src.chain-data /data/node/l2chaindata \
  --dst.chain-data /data/node-path/l2chaindata \
  --src.cache 256 \
  --dst.cache 256 \
  --archive-history.enable \
  --archive-history.start-block 0 \
  --archive-history.end-block 35000000 \
  --archive-history.skip-missing-states \
  --archive-history.workers 4 \
  --archive-history.max-inflight 4 \
  --archive-history.trie-clean-cache 4096 \
  --archive-history.result-memory-limit 256 \
  --archive-history.spill-gap 10000 \
  --archive-history.max-transition-gap 1000000 \
  --archive-history.spill-cache 64 \
  --archive-history.spill-workers 4 \
  --archive-history.spill-directory /data/node-path/pathdb-spill
```

On a 60 GiB host, this leaves headroom for the OS, Pebble, and per-worker trie
data. `--archive-history.workers 4` computes four independent block transitions
concurrently while committing freezer records and state IDs in block order.
`--archive-history.max-inflight` bounds
scheduled work and defaults to the worker count. Completed results share the
memory budget set by `--archive-history.result-memory-limit`; results that do
not fit spill to temporary files before being committed in order. Set the
result memory limit to `0` to spill every completed parallel result.
`--archive-history.trie-clean-cache` creates one shared hash-trie clean-node
cache for all workers, avoiding repeated Pebble lookups for nodes shared by
adjacent state roots. The value is in MiB; use `0` to disable it.

Archive diffs use the hash-first range iterator in the `go-ethereum` submodule.
It compares corresponding hashed children before loading their contents and
skips equal subtrees without those reads. The normal Geth iterator is unchanged.
Initial range seeking still resolves nodes. Missing nodes in changed branches
remain errors; equal skipped subtrees are not checked for completeness, so this
optimization is not a replacement for state verification.

This optimization needs both the migration source changes and the modified
`go-ethereum` submodule, followed by a rebuild. When publishing, commit/push the
submodule changes first, then update/commit the parent repository's submodule
pointer together with the migration changes. No new command-line flag or
history format is introduced; existing resume manifests and records remain
compatible. Keep the existing resume range and tuning settings for the first
production comparison. Synthetic read-count reductions do not predict an
equivalent blocks-per-second improvement.

For parallel runs, `--archive-history.coalesce-node-reads` is an experimental,
opt-in optimization (default false). Concurrent clean-cache misses for the same
hash share one backend read; completed results and errors are not retained as a
second cache. Shared results are copied so each trie decoder owns its buffer.
This helps only when workers request the same uncached node simultaneously;
otherwise its synchronization overhead may outweigh the savings.

Parallel progress also logs `Archive trie backend reads`: cumulative `requests`,
`backendReads`, and `backendBytes`. These count hash-trie backend Get calls after
the clean cache, not all trie accesses, cache hit rates, or physical disk I/O.
Compare deltas between progress lines: `(requests - backendReads)` estimates
coalesced calls, and `backendReads / blocks advanced` measures reads per block.
In-flight work can cross interval boundaries. Test with the flag off and on,
keeping other settings unchanged and allowing comparable warmup. Compare
sustained blocks/s as well as read counts; do not infer speedup from CPU-profile
percentages alone. The flag does not change the history format or resume range.

Large transitions use append-only per-worker spool files instead of inserting
every changed slot into a temporary key-value database. Storage tries within
one such transition are processed by `--archive-history.spill-workers`; this is
separate from `--archive-history.workers`, which controls independent block
transitions. Start with four spill workers and increase toward eight only when
the source disk still has random-read capacity. `--archive-history.spill-cache`
is the total buffering budget for the spool writers, capped at 16 MiB per
worker. When possible, put `--archive-history.spill-directory` on local NVMe
separate from the source chain database.

If a single account dominates a disk-backed transition, try
`--archive-history.spill-partitions 16` with `--archive-history.spill-workers 4`.
The default partition count is `1` (disabled); supported counts are 1, 2, 4, 8,
and 16. Partitions divide the storage key space into disjoint ranges, allowing
multiple workers to process the same account. They share the existing bounded
worker pool and buffer budget, not a new pool per account. Results are merged
in key order, and overlapping/out-of-order slots are rejected. Partitioning adds
trie seeks and metadata overhead, so measure it on the real source before
increasing concurrency or enabling it for small transitions.

For the observed `7032713 -> 7033177` transition, use
`--archive-history.spill-gap 100 --archive-history.spill-partitions 16` to select
the disk-backed partitioned path. This does not change the requested history
range. Rebuild the binary and check its help for `spill-partitions` first.

Long traversals log `Archive storage range progress` with account address,
partition, visited iterator positions, skipped identical subtrees, changed slots,
and elapsed time. Account traversal progress is logged separately. Reports are
checked periodically while traversing; an individual blocking database read may
delay them. Worker errors are logged immediately and cancellation is checked
between traversal positions, even when no changed leaf is emitted. These are
work counters, not an estimate of the total remaining trie size. The top-level
block ETA remains unreliable until transitions finish.

`--archive-history.max-transition-gap` rejects a single synthesized history
record spanning more than the configured number of blocks before starting its
trie diff. This prevents a large missing-state gap from consuming days only to
produce a record that the freezer cannot represent. The error reports the exact
block to use as a new `--archive-history.start-block`. Set the limit to `0` only
when the large transition is intentional and known to have a small state diff.

Start with three or four workers and watch RSS. On storage with spare random
read capacity, increase workers one at a time while keeping max-inflight equal
to workers. Every worker still needs memory for its active trie diff, even
though completed-result memory is bounded. If parallel mode reports missing
trie data discovered inside a retained root, retry with one worker so
`--archive-history.skip-missing-states` can recover sequentially.

The spill directory must be on a filesystem with enough free space. Temporary
append-only spool data is removed after the transition, and stale spill
directories from an interrupted run are removed on retry. If a failed run
already wrote destination history, use `--archive-history.reset-history` only
on the disposable destination copy before retrying. Disk spilling reduces
migration memory; it cannot restore state roots or trie nodes that are absent
from the source hashdb. A history
section that cannot fit in the freezer's Snappy block is rejected as soon as the
growing disk-backed result crosses the format limit. The error reports the exact
later start block instead of requiring the rest of the oversized diff to finish.

## Safety Notes

- Keep the original hash database until the converted node has caught up and
  served normal traffic for a full validation window.
- Do not point `--src.chain-data` and `--dst.chain-data` at the same directory.
- If migration fails, discard the destination copy and create a fresh copy before
  retrying. The tool writes an unfinished-conversion canary to prevent accidental
  reuse of a partial conversion.
- Run cleanup only after verification. The cleanup deletes legacy hash-scheme
  trie nodes from the destination copy using `rawdb.IsLegacyTrieNode`; it does
  not delete prefixed contract code, pathdb trie nodes, blocks, receipts, or
  freezer files.
- `--strict-cleanup` also deletes stale hashdb flat snapshot account/storage
  entries from the destination copy. This can reduce size further, but the node
  must rebuild pathdb snapshots after startup.
- Deleting legacy trie keys does not immediately shrink Pebble files. Use
  `--compact` after cleanup, or let Pebble compact over time while the node runs.
- The converted pathdb starts from the selected state root. It does not create
  historical pathdb state diffs before that root, so deep reorg recovery across
  the migration point is not available from the converted copy alone.
- Contract code and chain/freezer data are not rewritten by this tool; they must
  already exist in the copied destination database.

## What It Writes

- path-based account trie nodes;
- path-based storage trie nodes for every non-empty storage trie;
- pathdb state metadata for the selected root;
- state sync status as finished;
- by default, stale snapshot root/generator metadata is discarded so snapshots
  can rebuild against the converted root.
- verification may initialize empty pathdb state-history freezer files under
  `ancient/state` when the source database was converted from hashdb.
- immediately before committing PathDB root metadata, migration rechecks that
  the selected source and destination block hashes and roots are still
  canonical; a database that moved or reorged during a long conversion fails
  closed and retains the unfinished-conversion canary.
- with `--cleanup-legacy-hash-state`, legacy hash-scheme trie-node keys are
  deleted from the destination after successful pathdb verification.
- with `--strict-cleanup`, stale snapshot account/storage flat-state entries
  copied from hashdb are also deleted after successful pathdb verification.
