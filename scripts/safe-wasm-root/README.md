# Safe WASM root update

This directory creates, signs, executes, and verifies the following nested call
through a Safe:

```text
Safe
  -> UpgradeExecutor.executeCall(
       Rollup,
       Rollup.setWasmModuleRoot(WASM_ROOT)
     )
```

`rpc.test.deriw.com` is the Deriw L2 (chain ID `2885`). The Rollup,
UpgradeExecutor, and governance Safe live on its Arbitrum Sepolia parent (chain
ID `421614`), so the Safe transaction is signed and sent through
`rpc-arbitrum-sepolia.deriw.com`. Every operation validates both endpoints.

Arbitrum Sepolia has canonical Safe contracts, but it is not served by Safe
Global's hosted Transaction Service. Consequently, the request does not appear
in the Safe Web queue. These scripts exchange a portable JSON request and
execute it directly through the Safe contract. Do not configure a different
chain's Transaction Service.

The scripts refuse to proceed unless:

- both RPCs are connected to their configured chains;
- the Safe, UpgradeExecutor, and Rollup are deployed contracts;
- the Safe has `EXECUTOR_ROLE` on the UpgradeExecutor;
- the signer is a Safe owner when proposing or confirming;
- the request's Safe, chain, target, value, operation, calldata, nonce fields,
  and recomputed hash match the independently configured update;
- a direct `eth_call` simulation from the Safe succeeds; and
- the Safe threshold is met and the Safe validates the collected signatures
  before execution.

## One-time setup

Node.js 22 or newer is required.

```sh
cd scripts/safe-wasm-root
npm ci
```

## Environment

`SAFE_ADDRESS` is required and must be a Safe deployed on the parent chain.
Load the checked-in Deriw Testnet environment to replace values from any other
environment, then set the Safe:

```sh
source ./deriw-test-env.sh
export SAFE_ADDRESS='0xYourSafeAddress'
```

The environment file contains:

```sh
export L2_CHAIN_ID=2885
export L2_RPC='https://rpc.test.deriw.com'
export CHAIN_ID=421614
export L1_RPC='https://rpc-arbitrum-sepolia.deriw.com'
export ROLLUP=0xb6a39f55E4C4397FE799BeDCc16fFa895950CFF9
export UPGRADE_EXECUTOR=0x678815f2c63466f557024d8cce25baeeb4a23359
export WASM_ROOT=0x121d685e2fdb0e3291592d6b90bd70d503951335d19d96455448eb7a14d17421
```

The three contract values above already have those defaults in the scripts.

Enter the key needed for the current step without putting it in shell history:

```sh
read -s 'PK?Safe owner private key: '
export PK
echo
```

## Create the Safe (one time)

If the Safe does not exist yet, create it on the Arbitrum Sepolia parent chain.
The default configuration is a `1-of-1` Safe owned by
`0x35b3ac4003e1AfeE7601C190DB4f039fCb1BbcB5`. `PK` is the gas-paying deployer
key; the deployer does not become an owner unless its address is included in
`SAFE_OWNERS`.

```sh
source ./deriw-test-env.sh
export SAFE_OWNERS='0x35b3ac4003e1AfeE7601C190DB4f039fCb1BbcB5'
export SAFE_THRESHOLD=1

read -s 'PK?Arbitrum Sepolia deployer private key: '
export PK
echo

npm run create-safe
```

The first run only predicts the Safe address, prints the complete deployment
configuration and calldata, and simulates the deployment. After reviewing it,
broadcast the same deployment:

```sh
BROADCAST=1 npm run create-safe
```

Copy the printed `export SAFE_ADDRESS=...` command into the current shell. A
different deterministic address can be selected with an unsigned
`SAFE_SALT_NONCE`. For a multisig, provide comma-separated owners and a matching
threshold before the dry run:

```sh
export SAFE_OWNERS='0xOwner1,0xOwner2,0xOwner3'
export SAFE_THRESHOLD=2
export SAFE_SALT_NONCE=1
```

Never change owners, threshold, or salt between the dry run and broadcast.

## Authorize the Safe (one time)

The Safe must have `EXECUTOR_ROLE` on the UpgradeExecutor. Check readiness:

```sh
npm run verify
```

If it prints `Has EXECUTOR_ROLE: false`, an address that already has
`EXECUTOR_ROLE` must bootstrap the Safe once. Load that existing executor's key
into `PK` and run a dry run. For this deployment, the currently recorded
executor is `0xa1698F44D70632BfE448804378DA373C55eE8476`.

```sh
npm run grant-safe
```

Review the existing executor, Safe, UpgradeExecutor, role hash, and calldata.
Then broadcast the exact simulated call:

```sh
BROADCAST=1 npm run grant-safe
```

This grants the Safe full upgrade-executor authority. It does not revoke the
existing executor; role removal should be a separately reviewed Safe request
after the new Safe path has been tested.

## 1. Create the request

Load a Safe owner's key into `PK`. The proposer also contributes the first Safe
owner signature.

```sh
npm run propose
```

This creates `safe-request-0x....json` with mode `0600` and includes the
proposer's first signature. If another Safe transaction must execute first, set
`SAFE_NONCE` explicitly before creating the request; otherwise the on-chain Safe
nonce is used.

Send the request file to the next owner through your normal authenticated
operations channel. The request contains no private key, but every owner must
still configure `SAFE_ADDRESS`, `ROLLUP`, `UPGRADE_EXECUTOR`, and `WASM_ROOT`
independently rather than trusting values in the file.

## 2. Validate and sign

On a second owner's machine, use the same non-secret environment configuration,
enter that owner's `PK`, and run:

```sh
npm run confirm -- ./safe-request-0xSafeTransactionHash.json
```

The script recomputes the Safe transaction hash and checks the complete nested
calldata before appending this owner's signature. Repeat with additional owners
if the threshold is greater than two, passing along the updated request file.

## 3. Execute

After the confirmation count reaches the threshold, any funded signer can pay
the gas to execute:

```sh
npm run execute -- ./safe-request-0xSafeTransactionHash.json
```

The gas-paying key does not need to be a Safe owner. The script asks the Safe to
validate the collected owner signatures, broadcasts the transaction, waits for
the receipt, and verifies `wasmModuleRoot()` afterward.

To perform a read-only check at any time:

```sh
npm run verify
```

Remove the private key from the current shell when finished:

```sh
unset PK
```

## Authorize the Deriw ArbOS administration Safe

ArbOS scheduling happens directly on Deriw Testnet, not through the parent-chain
Safe used for the WASM root. The Deriw Safe created through the custom Safe web
app is pinned in `deriw-test-env.sh`:

```text
Safe:            0xE5C8e6dAbE8dA8D90F0AE3d4543E930833A0e9Ec
Version:         1.3.0
Owner:           0xa1698F44D70632BfE448804378DA373C55eE8476
Threshold:       1
UpgradeExecutor: 0xAc3516eF1E3658887198D192Cb0D7EcA07604943
```

The existing executor grants `EXECUTOR_ROLE` to this Safe once. Enter the
existing executor key without placing it in shell history:

```sh
source ./deriw-test-env.sh
read -s 'PK?Existing Deriw executor private key: '
export PK
echo

npm run grant-deriw-safe
```

The first invocation validates the chain, deployed contracts, exact Safe owner
set and threshold, existing executor role, and transaction simulation. It does
not send anything. After reviewing all printed values:

```sh
BROADCAST=1 npm run grant-deriw-safe
unset PK
```

This does not make the Safe a direct ArbOS chain owner. The existing
UpgradeExecutor remains the chain owner, and the Safe receives authority to
call it.

## Recover an exposed Deriw owner/executor key

If the current `1-of-1` owner key is exposed, generate a new owner key offline
and set only its public address. First rotate the Safe owner using the current
owner key:

```sh
source ./deriw-test-env.sh
export NEW_SAFE_OWNER='0xNewPublicOwnerAddress'

read -s 'PK?Current Safe owner private key: '
export PK
echo

npm run rotate-deriw-owner
BROADCAST=1 npm run rotate-deriw-owner
unset PK
```

After verifying the owner rotation, enter the **new** owner's key and revoke the
old EOA's direct `EXECUTOR_ROLE` through the Safe:

```sh
read -s 'PK?New Safe owner private key: '
export PK
echo

npm run revoke-deriw-executor
BROADCAST=1 npm run revoke-deriw-executor
unset PK
```

Both commands are dry runs by default. They validate the chain, Safe owner,
threshold, Safe nonce, role state, signed Safe transaction, and simulation
before allowing a broadcast. The revocation command refuses to use the old
executor as the new Safe owner.

## Schedule ArbOS 60 through the Deriw Safe

All sequencers, validators, batch posters, and relay/forwarder nodes must run a
binary and machine image that supports internal ArbOS version `60` before
scheduling activation. The on-chain public `ArbSys.arbOSVersion()` value has an
offset of `55` in this build:

```text
Current public version 87  -> internal version 32
Target internal version 60 -> expected public version 115
```

Start with a read-only check:

```sh
source ./deriw-test-env.sh
npm run arbos-verify
```

Choose a Unix activation timestamp. The checked-in minimum lead is 15 minutes;
one hour leaves time to review and execute the Safe request:

```sh
export ACTIVATION_TIMESTAMP=$(($(date +%s) + 3600))
date -r "$ACTIVATION_TIMESTAMP"
```

Load the Safe owner's key without placing it in shell history, then create and
sign the request:

```sh
read -s 'PK?Deriw Safe owner private key: '
export PK
echo

npm run arbos-propose
```

The request is written as `safe-arbos-request-0x....json` with mode `0600`.
For the current `1-of-1` Safe, do not run `arbos-confirm`; the proposal already
contains the required owner signature. Execute the exact path printed by the
proposal:

```sh
npm run arbos-execute -- ./safe-arbos-request-0x....json
unset PK
```

For a future multisig, each additional owner independently sets the same
`ACTIVATION_TIMESTAMP`, enters their key, and signs the same request:

```sh
npm run arbos-confirm -- ./safe-arbos-request-0x....json
```

After execution, `arbos-verify` must show scheduled internal version `60` and
the selected timestamp. After that timestamp passes and the chain processes the
upgrade, it should show public version `115`.
