MDBX Migrate Bad State Root Debug (Nitro + Erigon)

Context
- Repo: erigon-nitro-deriw
- Source DB: /tmp/nitro-src/l2chaindata
- Dest DB: /tmp/mdbx-check or /tmp/mdbx-badroot10
- Symptom: mdbx-migrate fails on block 10 with wrong state root.

Failing case
- Command:
  target/bin/mdbx-migrate --source /tmp/nitro-src --dest /tmp/mdbx-check \
    --mode full --verify consensus --start-block 0 --end-block 10
- Error example:
  expected_root=0xfdc8d6f54646f746e0ef9be4024277d968103db40cd9ad0b000849b62bb085a0
  computed_root=0x336e470641428fb77982bee17aa4c3e67cdd3cb10e90ae5ca90256ad304b2f3d
  block_hash=0x5f1726fab1e544b16d4122fac536557885f9c132cbd7ce5f2217eb0ef34d8911

What matches
- For addr 0xA4b05FffffFffFFFFfFFfffFfffFFfffFfFfFFFf:
  - Storage hashes match src vs dest at block 10 (308 items, only_dest=0 only_src=0).
  - Account storage_root in source is non-empty (example: 0xa056e198... at block 10).

What does not match
- Writeset shows account encoding for A4b05 uses empty root:
  - val=0101000000 (nonce=1, balance=0, codehash empty, root empty)
  - Seen in /tmp/trace_b10_writeset.log:
    writeset entry domain=accounts key=a4b05... val=0101000000 delete=false

Key evidence files
- /tmp/badroot10.log, /tmp/badroot10_keep.log, /tmp/badroot10_keepempty.log
- /tmp/trace_b10_writeset.log
- /tmp/src_accounts_block10.log / /tmp/dst_accounts_block10.log
- /tmp/dst_storage_block10.log

Hypothesis
- Account is serialized before its storage root is updated.
- In erigon/core/state/rw_v3.go, UpdateAccountData serializes immediately:
  value := accounts.SerialiseV3(account)
  WriteAccountStorage writes storage but does not update account.Root.
- Guard "empty-encoding-with-storage" only prevents delete; it does not fix encoding.

Next steps
- Trace where account.Root is set before serialization (state/triedb code paths).
- Patch to ensure account.Root is set (or re-serialized) after storage update.
- Verify by re-running mdbx-migrate 0..10 and comparing root to header.
