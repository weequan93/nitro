# v1.3.1.v3.10.0 parent-chain contracts

This procedure discovers and, only when required, upgrades the Nitro contracts
deployed on the parent chain. It is separate from the node binary rollout,
WASM-root change, and ArbOS activation.

For the blacklist and estimate-gas fixes alone, collect the version report and
stop. Those fixes do not require a parent-chain contract upgrade.

## Placement in a full protocol upgrade

```text
Deploy all node binaries
    -> validate and soak
    -> discover deployed parent-contract versions
    -> upgrade parent contracts only if required
    -> verify batches and delayed messages
    -> change the WASM root only if required
    -> schedule ArbOS only if required
```

Never infer the deployed version from this repository's submodule pins. This
release contains `contracts` package `3.2.0-beta.0` and `contracts-legacy`
package `2.1.3`, but neither value proves what the production proxies use.

## 1. Pin the official upgrade tooling

The following official `arbitrum-chain-actions` revision was inspected for this
guide on 2026-09-03:

```text
5ff87aedf3ad581eeecaa2e4c9220248d8e2c263
```

Use a fresh checkout and do not run a moving `main` branch in production:

```bash
git clone https://github.com/OffchainLabs/arbitrum-chain-actions.git
cd arbitrum-chain-actions
git checkout 5ff87aedf3ad581eeecaa2e4c9220248d8e2c263
git submodule update --init --recursive

nvm use 20.12.2
yarn install --frozen-lockfile
forge --version
yarn build

git status --short
git rev-parse HEAD
```

Record the Node, Yarn, Forge, and chain-actions versions in the change ticket.
Use the same tool versions in testnet, staging, and production.

## 2. Required environment-specific values

Do not reuse development or testnet addresses. Do not commit `.env`, RPC
credentials, private keys, or Safe signatures.

```bash
export PARENT_CHAIN_RPC="https://PRODUCTION-PARENT-RPC"
export INBOX_ADDRESS="0xPRODUCTION_INBOX"
export ROLLUP_ADDRESS="0xPRODUCTION_ROLLUP"
export PROXY_ADMIN_ADDRESS="0xPRODUCTION_PROXY_ADMIN"
export PARENT_UPGRADE_EXECUTOR_ADDRESS="0xPRODUCTION_PARENT_UPGRADE_EXECUTOR"
export OWNER_SAFE="0xPRODUCTION_SAFE_WITH_EXECUTOR_PERMISSION"
```

Record the parent chain ID and prove that every address has code:

```bash
cast chain-id --rpc-url "$PARENT_CHAIN_RPC"

for address in \
  "$INBOX_ADDRESS" \
  "$ROLLUP_ADDRESS" \
  "$PROXY_ADMIN_ADDRESS" \
  "$PARENT_UPGRADE_EXECUTOR_ADDRESS"
do
  echo "$address"
  cast code --rpc-url "$PARENT_CHAIN_RPC" "$address" | cast keccak
done
```

Empty code or an unexpected parent chain ID is an immediate stop condition.

## 3. Discover the deployed versions

From the pinned `arbitrum-chain-actions` checkout:

```bash
INBOX_ADDRESS="$INBOX_ADDRESS" \
PARENT_CHAIN_RPC="$PARENT_CHAIN_RPC" \
yarn chain:contracts:version | tee parent-contract-versions.before.txt
```

The report must identify at least:

- Inbox or ERC20Inbox.
- Outbox.
- SequencerInbox.
- Bridge or ERC20Bridge.
- RollupProxy.
- RollupAdminLogic and RollupUserLogic.
- ChallengeManager, where applicable.
- A supported next upgrade action.

Use this decision gate:

| Version report | Action |
| --- | --- |
| Required components already report `v2.1.3` | Skip the `2.1.3` upgrade |
| Tool explicitly reports a supported path to `v2.1.3`, and the EIP-7702 condition below applies | Continue with this procedure |
| Tool requires `v2.1.2` first | Stop and create a separate `2.1.2` plan |
| Tool recommends `v3.1.0` or another version | Stop and use that version's audited guide; do not run `2.1.3` |
| Unknown, mixed, custom, or unsupported result | Stop and investigate the proxy implementations and storage history |

Nitro contracts `2.1.3` is a narrow patch for Inbox and SequencerInbox handling
of EIP-7702 callers. Use it only when the chain is not moving to `v3.1.0` before
its parent enables EIP-7702. If the parent is not enabling EIP-7702 and the
version tool does not require the patch, do not upgrade merely because this
node release uses Nitro v3.10.

If the chain uses a custom native token and was originally deployed before
Nitro contracts `v2.0.0`, determine whether the `v2.1.2` ERC20Bridge storage
fix is required before `v2.1.3`.

## 4. Capture addresses, implementations, and ownership

Derive the Bridge and SequencerInbox from the production Inbox:

```bash
export BRIDGE_ADDRESS="$(cast call --rpc-url "$PARENT_CHAIN_RPC" \
  "$INBOX_ADDRESS" 'bridge()(address)')"

export SEQUENCER_INBOX_ADDRESS="$(cast call --rpc-url "$PARENT_CHAIN_RPC" \
  "$BRIDGE_ADDRESS" 'sequencerInbox()(address)')"

echo "$BRIDGE_ADDRESS"
echo "$SEQUENCER_INBOX_ADDRESS"
```

Record EIP-1967 implementation slots before execution:

```bash
export IMPLEMENTATION_SLOT="0x360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc"

cast storage --rpc-url "$PARENT_CHAIN_RPC" \
  "$INBOX_ADDRESS" "$IMPLEMENTATION_SLOT"

cast storage --rpc-url "$PARENT_CHAIN_RPC" \
  "$SEQUENCER_INBOX_ADDRESS" "$IMPLEMENTATION_SLOT"
```

Verify the ProxyAdmin owner and Safe executor permission. The exact ownership
interfaces must match the deployed contracts:

```bash
cast call --rpc-url "$PARENT_CHAIN_RPC" \
  "$PROXY_ADMIN_ADDRESS" 'owner()(address)'

export EXECUTOR_ROLE="$(cast call --rpc-url "$PARENT_CHAIN_RPC" \
  "$PARENT_UPGRADE_EXECUTOR_ADDRESS" 'EXECUTOR_ROLE()(bytes32)')"

cast call --rpc-url "$PARENT_CHAIN_RPC" \
  "$PARENT_UPGRADE_EXECUTOR_ADDRESS" \
  'hasRole(bytes32,address)(bool)' "$EXECUTOR_ROLE" "$OWNER_SAFE"
```

Expected executor result is `true`. Also confirm from deployment records and
on-chain inspection that the parent UpgradeExecutor owns or controls the
Rollup/ProxyAdmin path expected by the action. Stop if ownership differs.

## 5. Determine the existing maximum data size

Do not copy `104857` from another environment without checking it:

```bash
export MAX_DATA_SIZE="$(cast call --rpc-url "$PARENT_CHAIN_RPC" \
  "$INBOX_ADDRESS" 'maxDataSize()(uint256)')"

echo "$MAX_DATA_SIZE"
```

The official execution script checks that this value matches both the current
deployment and selected upgrade action. A mismatch must revert and must not be
bypassed.

## 6. Select or deploy the 2.1.3 action

First decide whether the parent chain is itself Arbitrum:

```bash
export PARENT_CHAIN_IS_ARBITRUM="true-or-false"
```

The official guide lists predeployed actions by parent chain. Do not select one
only by its address: verify the parent chain, bytecode, `MAX_DATA_SIZE`, and
constructor-created implementations. Record the evidence.

If no verified compatible action exists, deploy one from the pinned checkout.
Deployment does not execute the upgrade. Use the team's hardware wallet,
keystore, or controlled deployer workflow; do not paste a raw key into history.

Create an untracked `.env` in the chain-actions project root:

```dotenv
PARENT_CHAIN_RPC=
PARENT_CHAIN_IS_ARBITRUM=true
MAX_DATA_SIZE=
UPGRADE_ACTION_ADDRESS=
INBOX_ADDRESS=
PROXY_ADMIN_ADDRESS=
PARENT_UPGRADE_EXECUTOR_ADDRESS=
```

Protect and verify it:

```bash
chmod 600 .env
git status --short
```

Simulate deployment first by leaving `FOUNDRY_BROADCAST` unset:

```bash
unset FOUNDRY_BROADCAST
yarn cli -- contract-upgrades/2.1.3/deploy
```

After simulation review, deploy through the approved signing workflow:

```bash
export FOUNDRY_BROADCAST=true
yarn cli -- contract-upgrades/2.1.3/deploy
unset FOUNDRY_BROADCAST
```

Record the deployed action address, deployment transaction, bytecode hash, and
the four implementation addresses held by the action. Set the reviewed value:

```bash
export UPGRADE_ACTION_ADDRESS="0xREVIEWED_2_1_3_ACTION"
```

## 7. Simulate the upgrade

The `2.1.3` action upgrades only Inbox/ERC20Inbox and SequencerInbox. It does not
set the Rollup WASM root and does not schedule ArbOS.

Populate `.env` and run the official execute step without broadcasting:

```bash
unset FOUNDRY_BROADCAST
yarn cli -- contract-upgrades/2.1.3/execute
```

For a parent-chain Safe, independently generate the expected calldata:

```bash
export PERFORM_DATA="$(cast calldata \
  'perform(address,address)' \
  "$INBOX_ADDRESS" \
  "$PROXY_ADMIN_ADDRESS")"

export SAFE_DATA="$(cast calldata \
  'execute(address,bytes)' \
  "$UPGRADE_ACTION_ADDRESS" \
  "$PERFORM_DATA")"

echo "$PERFORM_DATA"
echo "$SAFE_DATA"
```

Simulate the exact call with the Safe as caller:

```bash
cast call --rpc-url "$PARENT_CHAIN_RPC" \
  --from "$OWNER_SAFE" \
  "$PARENT_UPGRADE_EXECUTOR_ADDRESS" \
  "$SAFE_DATA"
```

Require a successful fork/`eth_call` simulation and review the official
script's planned transactions. Confirm that the calldata, Inbox, ProxyAdmin,
action, executor, chain ID, and Safe match the change ticket.

## 8. Execute through the parent-chain Safe

Pause the batch poster immediately before execution. L2 sequencing may
continue, but monitor backlog and parent-chain finality.

Create exactly one Safe transaction:

```text
Chain:  verified production parent chain
To:     PARENT_UPGRADE_EXECUTOR_ADDRESS
Value:  0
Data:   SAFE_DATA
```

Every signer must decode and verify both nested calls:

```text
UpgradeExecutor.execute(UPGRADE_ACTION_ADDRESS, PERFORM_DATA)
NitroContracts2Point1Point3UpgradeAction.perform(INBOX_ADDRESS, PROXY_ADMIN_ADDRESS)
```

Do not use the Rollup `executeCall` calldata used by the WASM-root operation;
the contract-version action uses UpgradeExecutor `execute(address,bytes)`.

Wait for the environment's required parent-chain confirmations before
restarting the poster.

## 9. Verify after execution

Run the pinned official verifier and version report:

```bash
yarn cli -- contract-upgrades/2.1.3/verify

INBOX_ADDRESS="$INBOX_ADDRESS" \
PARENT_CHAIN_RPC="$PARENT_CHAIN_RPC" \
yarn chain:contracts:version | tee parent-contract-versions.after.txt
```

Record the new implementation slots:

```bash
cast storage --rpc-url "$PARENT_CHAIN_RPC" \
  "$INBOX_ADDRESS" "$IMPLEMENTATION_SLOT"

cast storage --rpc-url "$PARENT_CHAIN_RPC" \
  "$SEQUENCER_INBOX_ADDRESS" "$IMPLEMENTATION_SLOT"
```

Also verify:

- Inbox/ERC20Inbox reports `v2.1.3`.
- SequencerInbox reports `v2.1.3`.
- Other unchanged contracts retain their expected versions.
- `maxDataSize()` is unchanged.
- The Rollup WASM root is unchanged.
- L2 blocks continue and finalized hashes agree.
- The poster submits a new batch successfully.
- Delayed messages remain consumable.
- Inbox deposits/retryables work in a controlled smoke test.

Observe at least one successful parent-chain batch lifecycle before closing the
contract change.

## 10. Failure and rollback policy

The action executes atomically: a reverted Safe transaction should leave both
proxies unchanged. Verify this using implementation slots and the version tool.

After a successful upgrade, do not attempt an ad hoc downgrade. A contract
rollback requires a separately reviewed action, known previous implementation
bytecode, storage-layout compatibility proof, simulation, and Safe approval.
Prepare that rollback payload before the maintenance window if the change is
operationally mandatory.

If verification fails after a successful transaction:

1. Keep the batch poster paused if posting may be unsafe.
2. Preserve transaction, receipt, traces, implementation slots, and logs.
3. Do not proceed to WASM-root or ArbOS operations.
4. Escalate to the contract release and Safe owners.
5. Execute only the pre-reviewed rollback or remediation plan.

## Official references

- [OffchainLabs arbitrum-chain-actions](https://github.com/OffchainLabs/arbitrum-chain-actions/tree/5ff87aedf3ad581eeecaa2e4c9220248d8e2c263)
- [Nitro contracts 2.1.3 procedure](https://github.com/OffchainLabs/arbitrum-chain-actions/tree/5ff87aedf3ad581eeecaa2e4c9220248d8e2c263/scripts/foundry/contract-upgrades/2.1.3)
- [Nitro contracts v2.1.3 release](https://github.com/OffchainLabs/nitro-contracts/releases/tag/v2.1.3)

