# Gasless Contracts

The implementation is heavily inspired by [gasless-contract](https://github.com/godwokenrises/gasless-contract).

# Start Orbit Chain

Follow [orbit-setup-script](https://github.com/OffchainLabs/orbit-setup-script)

Replace the nitro docker image with yours.

Build docker

```
cd nitro
make
make build
docker build . -t nitro-node
```

# Install Dependencies

```shell
yarn
```

# Build

```shell
yarn build
```

# Deploy

```shell
npx hardhat run scripts/deploy.ts --network orbit
```

# Replace `ENTRYPOINT_CONTRACT`

Replace `ENTRYPOINT_CONTRACT` in `arbutil/gasless.go` with the real one.

Rebuild the docker.

Todo: put it in the configuration.

# Test

```shell
npx hardhat test test/NitroPaymaster.ts --network orbit
```
