# mdbx-migrate bad state root (block 10) - progress

## Current issue
- mdbx-migrate fails with wrong trie root at block 10 (block 9 passes).
- Example: computed_root 0x336e4706... vs expected 0xfdc8d6f5... (block 10).

## Key findings
- Storage for 0xA4b05FffffFffFFFFfFFfffFfffFFfffFfFfFFFf matches between src/dest at block 10.
  - tmp_diff_storage_hashes.go: src=308 dest=308 only_dest=0 only_src=0.
- Account encoding for A4b05 in writeset is empty (val=0101000000) even though src shows non-empty storage_root.
  - /tmp/trace_b10_writeset.log shows repeated writeset entries for a4b05... with val=0101000000.
  - src account at block 10 shows storage_root=0xa056e198... (non-empty).
- Other touched accounts (28c18..., 502ffd..., 67d586..., A0000E...) sometimes show delete/re-add in writeset; still mismatch occurs.

## Hypothesis
- Account empty/drop logic is clearing account encoding even when storage exists (or pending storage writes exist), producing a different state root.
- Likely in erigon/core/state/rw_v3.go (emptyAccountEncoding / drop / empty encoding guard).

## Files / logs
- /tmp/mdbx_0_10.log, /tmp/mdbx_0_9.log
- /tmp/badroot10.log, /tmp/badroot10_keep.log, /tmp/badroot10_keepempty.log
- /tmp/trace_b10_writeset.log
- /tmp/src_accounts_block10*.log, /tmp/dst_accounts_block10*.log
- /tmp/dst_storage_block10.log
- tmp_diff_storage_hashes.go, tmp_dump_geth.go, tmp_dump_accounts_domain.go

## Repro commands
- Build: go build -tags erigon -o target/bin/mdbx-migrate ./cmd/mdbx-migrate
- Run (block 10):
  target/bin/mdbx-migrate --source /tmp/nitro-src --dest /tmp/mdbx-check --mode full --verify consensus --start-block 0 --end-block 10
- Debug storage diff:
  ADDR=0xA4b05F... SOURCE=/tmp/nitro-src/l2chaindata DEST=/tmp/mdbx-check BLOCK=10 go run -tags erigon tmp_diff_storage_hashes.go
- Writeset grep:
  rg -n "writeset entry.*domain=accounts" /tmp/trace_b10_writeset.log | rg -i "a4b05f|a4b000|39d28|502f|67d586|a0000e"

## Next step
- Inspect erigon/core/state/rw_v3.go for empty account encoding/drop logic.
- Add guard: do not drop/empty-encode if storage exists or pending storage updates exist for the address.
- Rebuild mdbx-migrate and rerun block 10 to confirm root matches expected.
