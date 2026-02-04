# Debug Memory: mdbx-migrate bad state root (block 10)

Date: 2026-02-03

## Repro
- Source DB: `/tmp/nitro-src/l2chaindata`
- Dest DB: `/tmp/mdbx-check` or `/tmp/mdbx-badroot10`
- Command:
  - `target/bin/mdbx-migrate --source /tmp/nitro-src --dest /tmp/mdbx-check --mode full --verify consensus --start-block 0 --end-block 10`
- Failure:
  - Block 10 wrong trie root.
  - Expected: `0xfdc8d6f54646f746e0ef9be4024277d968103db40cd9ad0b000849b62bb085a0`
  - Computed: `0x336e470641428fb77982bee17aa4c3e67cdd3cb10e90ae5ca90256ad304b2f3d`
  - Logs: `/tmp/mdbx_0_10.log`, `/tmp/badroot10_keep.log`, `/tmp/badroot10_keepempty.log`

## Key Evidence (A4b05 account)
- Writeset encodes A4b05 as short account (root empty):
  - `/tmp/trace_b10_writeset.log`
  - Lines: `writeset entry ... key=a4b05... val=0101000000`
- Source account has non-empty storage root:
  - `/tmp/src_accounts_block10.log` (storage_root `0xa056e198...`)
- Dest account root is empty:
  - `/tmp/dst_accounts_block10.log` (root `0x56e81f...`)
- Dest storage domain has entries for A4b05:
  - `/tmp/dst_storage_block10.log` (items=237)
- Storage hashes match (src vs dest) at block 10:
  - `ADDR=0xA4b05F... SOURCE=/tmp/nitro-src/l2chaindata DEST=/tmp/mdbx-badroot10 BLOCK=10 go run -tags erigon tmp_diff_storage_hashes.go`
  - Output: `only_dest=0 only_src=0`

## Observations
- End-block 9 run passes; A4b05 storage matches for blocks 1–9.
- Mismatch is not storage domain content; it is account encoding/root propagation.

## Code Change (applied)
- File: `erigon/core/state/intra_block_state.go`
- In `updateAccount`:
  - Added `keepEmpty := shouldKeepEmptyAccount(addr)`
  - Guarded nilAccount override to respect keep-empty:
    - `if !keepEmpty && sdb.nilAccounts != nil && ... { nilAccount = ... }`
- `ERIGON_MDBX_MIGRATE_KEEP_EMPTY_ACCOUNTS=true` did not fix bad root (computed root changed but still wrong).

## Debug Env Used
- `ERIGON_BAD_ROOT_DEBUG=1`
- `ERIGON_BAD_ROOT_DUMP_TOUCHED_ACCOUNTS=1`
- `ERIGON_BAD_ROOT_DUMP_STATE=1`
- `ERIGON_MDBX_MIGRATE_SKIP_UNWIND_ON_BAD_ROOT=1`
- `ERIGON_MDBX_MIGRATE_FLUSH_ON_BAD_ROOT=1`

