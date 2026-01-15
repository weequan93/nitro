# AGENTS

## Project
- Repo: erigon-nitro-deriw (Nitro + Erigon integration)

## Build / Test
- Add canonical build and test commands here.

## Common Commands
- Build mdbx-migrate:
  `go build -tags erigon -o target/bin/mdbx-migrate ./cmd/mdbx-migrate`
- Run mdbx migrate (full):
  `target/bin/mdbx-migrate --source "$PWD/.mdbx-flow-test/pebble-l3" --dest "$PWD/.mdbx-flow-test/mdbx-l3" --mode full --verify extended --verify-samples 10`
- Run mdbx flow test:
  `ERIGON_BAD_ROOT_DEBUG=1 MDBX_MIGRATE_REBUILD=1 ./scripts/mdbx_flow_test.sh`

## Env Vars
- `ERIGON_BAD_ROOT_DEBUG=1`
- `ERIGON_MDBX_MIGRATE_DEBUG=1`
- `ERIGON_MDBX_MIGRATE_DEBUG_BLOCK=<block_num>`

## Data Paths
- `.mdbx-flow-test/pebble-l3` (source)
- `.mdbx-flow-test/mdbx-l3` (dest)

## Notes
- Prefer the `ERIGON_` prefix for mdbx-migrate debug envs to avoid warnings.
- Remove `.mdbx-flow-test/mdbx-l3` only if you intend to re-run a full migration.
