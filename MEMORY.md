# Memory - mdbx-migrate bad state root (block 10)

## Symptom (before fix)
- `mdbx-migrate --verify consensus` failed at block 10.
- Expected root (from header): `0xfdc8d6f54646f746e0ef9be4024277d968103db40cd9ad0b000849b62bb085a0`
- Computed root: `0x336e470641428fb77982bee17aa4c3e67cdd3cb10e90ae5ca90256ad304b2f3d`
- End-block 9 passed; end-block 10 failed.

## Root cause identified (Feb 4, 2026)
- Source (geth/pebble) has **35 accounts** at block 10; dest had **34**.
- Missing account in dest:
  - Address hash missing: `0xf0cb29e506d6a7a9e806f34f56b41ec82e52194f3ae982077a870a3fd402c163`
  - Preimage (address): `0x502ffdafd660aedf4ea7db3d758999e154102a6c`
  - Source account state at block 10:
    - exists=true, nonce=0, balance=0, codehash=empty, storage_root=empty
- This empty account was dropped during migration (EIP-161 empty removal), causing the root mismatch.

## Fix applied
- **Keep empty account** `0x502ffdafd660aedf4ea7db3d758999e154102a6c` by default:
  - Added to `arbosKeepEmptyAccounts` in `erigon/core/state/rw_v3.go`.
- After rebuild and rerun to block 10, **root matches header** (no “Wrong trie root” warning).

## Verification (Feb 4, 2026)
- Command:
  `ERIGON_MDBX_MIGRATE_SKIP_UNWIND_ON_BAD_ROOT=true ERIGON_MDBX_MIGRATE_FLUSH_ON_BAD_ROOT=true target/bin/mdbx-migrate --source /tmp/nitro-src --dest /tmp/mdbx-check --mode full --verify consensus --start-block 0 --end-block 10`
- Result:
  - No bad root warnings.
  - Only remaining error: `verify head/root: head number mismatch source=32 dest=10` (expected because end-block=10).

## Supporting debug work
- Dest accounts list generated via `tmp_list_accounts_domain.go` (new helper).
- Compared dest accounts vs geth using `cmd/mdbx-storage-diff --compare-accounts`; no mismatches among 34 dest accounts.
- Identified missing account by comparing geth hashed keys vs keccak(dest addresses); recovered preimage via `rawdb.ReadPreimage`.

## Note on earlier hypothesis
- Account encoding v3 **does not store storage root**; `accounts.SerialiseV3` omits `Root`. So “fix empty root in account encoding” does not affect state root.

## Build warnings
- `go build` succeeds; only macOS linker warnings about libstylus built for newer macOS.

# Memory - mdbx-migrate bad state root (block 32)

## Symptom (Feb 4, 2026)
- `mdbx-migrate --verify extended` failed at block 32.
- Expected root: `0xa2367891323e1fd04b048d3e270f832643613ac8ecd96e88fe77c25f4abe6711`
- Computed root: `0xd68fbc84c274ac3d1327ac0e5b05f85058ed33eea0acba5042b6d0ad7b0631be`

## Root cause identified
- Source (geth/pebble) has **61 accounts** at block 32; dest had **58**.
- Missing accounts in dest (all empty, codehash empty, storage root empty):
  - `0xe5052b97618c9ff3025bdece4d9a5e9e229b64b3`
  - `0x8807ed26dbaae86b62d0b663d754ce66f9d373b8`
  - `0x571fb9e1003ebe9c99ad3c1a60797e19cb577e93`
- These were dropped by EIP-161 empty account removal.

## Fix applied
- Added the three addresses above to `arbosKeepEmptyAccounts` in `erigon/core/state/rw_v3.go`.
- Rebuilt and re-ran to block 32; **root now matches header** (no bad root warnings).

## Verification (Feb 4, 2026)
- Command:
  `ERIGON_MDBX_MIGRATE_SKIP_UNWIND_ON_BAD_ROOT=true ERIGON_MDBX_MIGRATE_FLUSH_ON_BAD_ROOT=true target/bin/mdbx-migrate --source /tmp/nitro-src --dest /tmp/mdbx-debug32 --mode full --verify consensus --start-block 0 --end-block 32`
- Result:
  - No bad root warnings.
  - Normal verify output; consensus verification skipped because exec already at end-block.
