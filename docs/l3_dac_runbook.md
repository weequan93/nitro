# L3 DAC Runbook (nitro-testnode)

This runbook describes how to run an L3 testnode on top of an L2 AnyTrust (DAC) chain
using the `nitro-testnode` docker compose services.

Notes:
- `nitro-testnode` enables DAC at the L2 layer via `--l2-anytrust`.
- The generated L3 config sets `DataAvailabilityCommittee=false`, so L3 itself does not
  use a DAC unless you customize the L3 config and add matching DAC services.

## Environment (Erigon build tags)
Run this once before any of the commands below:
```bash
export NITRO_DOCKER_BUILD_ARGS="--build-arg GO_BUILD_TAGS=erigon --build-arg SKIP_FORGE_YUL=1"
```

## Build Docker images
From repo root:
```bash
export COMPOSE_PROJECT_NAME=nitro-testnode
cd nitro-testnode
./test-node.bash --build --dev --no-run
```

## Start L3 DAC (new)
From repo root:
```bash
export COMPOSE_PROJECT_NAME=nitro-testnode
cd nitro-testnode
./test-node.bash --init --dev --detach --l3node --l2-anytrust
```
Add `--archive` if you want archive-mode configs generated.

## Start L3 DAC (import data)
If you have existing DAS data, restore it into the DAC volumes before startup:
```bash
export COMPOSE_PROJECT_NAME=nitro-testnode
cd nitro-testnode

# Committee A
docker run --rm \
  -v "${COMPOSE_PROJECT_NAME}_das-committee-a-data:/data" \
  -v "/path/to/das-committee-a:/in" \
  alpine:3.19 sh -c "rm -rf /data/* && tar -C /in -cf - . | tar -C /data -xf - && chown -R 1000:1000 /data"

# Committee B
docker run --rm \
  -v "${COMPOSE_PROJECT_NAME}_das-committee-b-data:/data" \
  -v "/path/to/das-committee-b:/in" \
  alpine:3.19 sh -c "rm -rf /data/* && tar -C /in -cf - . | tar -C /data -xf - && chown -R 1000:1000 /data"

# Mirror
docker run --rm \
  -v "${COMPOSE_PROJECT_NAME}_das-mirror-data:/data" \
  -v "/path/to/das-mirror:/in" \
  alpine:3.19 sh -c "rm -rf /data/* && tar -C /in -cf - . | tar -C /data -xf - && chown -R 1000:1000 /data"
```

## L3 DAC Validator (committee members)
```bash
cd nitro-testnode
docker compose up -d das-committee-a das-committee-b
```
Optional mirror:
```bash
docker compose up -d das-mirror
```

## L3 Sequencer
The L3 sequencer runs as `l3node` and depends on the L2 sequencer:
```bash
cd nitro-testnode
docker compose up -d validation_node sequencer l3node
```

## L3 Validator
In `nitro-testnode`, the `l3node` config enables staker mode, so it also acts as the L3
validator. A dedicated L3 validator is not provided by default.

## L3 Archive Node
Generate archive-mode configs and start the node:
```bash
cd nitro-testnode
./test-node.bash --init --dev --detach --l3node --l2-anytrust --archive
docker compose up -d validation_node sequencer l3node
```
