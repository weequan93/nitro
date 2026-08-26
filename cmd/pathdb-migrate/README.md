# pathdb-migrate

`pathdb-migrate` is an offline migration helper for creating a pathdb execution
database from an existing hash-state execution database copy.

This tool is intentionally conservative:

- it never migrates in place;
- the source database is opened read-only;
- the destination must be a separate copy of the same chain database;
- only one selected state root is converted, normally `latest`;
- validators/stakers should not use pathdb unless Nitro explicitly supports it.

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
opens both databases read-only. It does not compare raw database files.

If an earlier process exited after migration finished but before clean shutdown,
the unfinished-conversion canary can be cleared after a successful verification:

```sh
go run ./cmd/pathdb-migrate \
  --dst.chain-data /data/node-path/l2chaindata \
  --verify-only \
  --ignore-unfinished-conversion
```

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
  --archive-history.spill-cache 64 \
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

Start with three or four workers and watch RSS. On storage with spare random
read capacity, increase workers one at a time while keeping max-inflight equal
to workers. Every worker still needs memory for its active trie diff, even
though completed-result memory is bounded. If parallel mode reports missing
trie data discovered inside a retained root, retry with one worker so
`--archive-history.skip-missing-states` can recover sequentially.

The spill directory must be on a filesystem with enough free space. Temporary
spill data is removed after the transition, and stale spill directories from an
interrupted run are removed on retry. If a failed run already wrote destination
history, use `--archive-history.reset-history` only on the disposable destination
copy before retrying. Disk spilling reduces migration memory; it cannot restore
state roots or trie nodes that are absent from the source hashdb. A history
section that cannot fit in the freezer's Snappy block is rejected with an error;
choose a later start block instead of bridging one very large missing-state gap.

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
- with `--cleanup-legacy-hash-state`, legacy hash-scheme trie-node keys are
  deleted from the destination after successful pathdb verification.
- with `--strict-cleanup`, stale snapshot account/storage flat-state entries
  copied from hashdb are also deleted after successful pathdb verification.
