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

If an earlier process exited after migration finished but before clean shutdown,
the unfinished-conversion canary can be cleared after a successful verification:

```sh
go run ./cmd/pathdb-migrate \
  --dst.chain-data /data/node-path/l2chaindata \
  --verify-only \
  --ignore-unfinished-conversion
```

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
