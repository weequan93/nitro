# Safe transactions: blacklist and owners

[generate.py](generate.py) creates Safe Transaction Builder imports, a review document and a read-only check script. Generation is offline and never signs, submits a Safe proposal or broadcasts a transaction. Per-operation files go into ignored `generated/` directories.

## Choose the permission you mean

| Action | Call | Effect |
| --- | --- | --- |
| `blacklist-from` | `DeriwBlacklist.addBlacklistTxFrom(address)` | Add an address to the transaction sender blacklist. |
| `blacklist-to` | `DeriwBlacklist.addBlacklistTxTo(address)` | Add an address to the transaction destination blacklist. |
| `blacklist-owner` | `DeriwBlacklist.addBlacklistOwner(address)` | Grant permission to administer the blacklist. |
| `chain-owner` | `ArbOwner.addChainOwner(address)` | Grant broader ArbOS chain administration. |
| `safe-owner` | `Safe.addOwnerWithThreshold(address,uint256)` | Add a signer to that Safe and set its resulting signature threshold. |

These owner roles are separate. Adding a blacklist administrator does not make that account a Safe signer. Adding a Safe signer does not directly add the account to ArbOS's chain-owner list.

The Deriw precompile actions support chain IDs `2886` (mainnet, default) and `2885` (testnet). Safe signer changes accept an explicitly selected chain ID, including `42161` when changing an Arbitrum One Safe.

| Precompile | Address |
| --- | --- |
| DeriwBlacklist, privileged mutation | `0x00000000000000000000000000000000000007ec` |
| DeriwBlacklistPublic, public membership views | `0x00000000000000000000000000000000000007eb` |
| ArbOwner, privileged mutation | `0x0000000000000000000000000000000000000070` |
| ArbOwnerPublic, public chain-owner views | `0x000000000000000000000000000000000000006b` |

## Generate the import

Run from the repository root with Python 3 and Foundry `cast` available:

```bash
export PATH="$HOME/.foundry/bin:$PATH"
SAFE=0xYOUR_SAFE_ON_THE_SELECTED_CHAIN
ACCOUNT=0xADDRESS_TO_ADD
```

Replace the placeholders with full addresses. `--safe` is mandatory: this tool does not assume that the recovery governance Safe on Arbitrum One is a Deriw L3 administrator. A historical mainnet blacklist-owner draft used Deriw Safe `0x2f996bc558818d33de37af36bee7de24ba3fc4df`; check current authority before using it.

### Add a sender to the blacklist

```bash
python3 docs/safe-transactions/generate.py blacklist-from \
  --safe "$SAFE" --address "$ACCOUNT" --chain-id 2886
```

### Add a transaction destination to the blacklist

```bash
python3 docs/safe-transactions/generate.py blacklist-to \
  --safe "$SAFE" --address "$ACCOUNT" --chain-id 2886
```

Choose the appropriate direction. If both lists should contain the address, generate and review each action. For a batch in one list, repeat `--address` with distinct addresses.

### Add a blacklist administrator

```bash
python3 docs/safe-transactions/generate.py blacklist-owner \
  --safe "$SAFE" --address "$ACCOUNT" --chain-id 2886
```

The caller must already be a blacklist owner **or** a chain owner. For this direct call route, the caller is the executing Safe.

### Add a chain administrator

```bash
python3 docs/safe-transactions/generate.py chain-owner \
  --safe "$SAFE" --address "$ACCOUNT" --chain-id 2886
```

The executing Safe must already be an ArbOS chain owner.

### Add a Safe signer

```bash
python3 docs/safe-transactions/generate.py safe-owner \
  --safe "$SAFE" --address "$ACCOUNT" --chain-id 2886 --threshold 3
```

`--threshold` is the **resulting** threshold, not the number of existing signatures supplied for this operation. Set it to the current threshold to preserve that requirement, or to the explicitly reviewed new requirement. The new owner must not already be a signer, the zero/sentinel address or the Safe itself. The resulting threshold must fit the resulting owner count; the generated simulation and Safe UI must verify this against current state. This package contains one signer addition and calls the Safe itself.

## Check, import and execute

Each generation prints `OUTPUT_DIRECTORY`, containing:

```text
safe-import.json     Transaction Builder call list
REVIEW.md            Chain, Safe, purpose, target and parameters
check-readonly.sh    Current authority/membership views and direct-call simulation
```

1. Run the check script with the RPC for the **same chain as the Safe**:

   ```bash
   bash OUTPUT_DIRECTORY/check-readonly.sh https://rpc.deriw.com
   ```

   For another network, supply that network's RPC. The script verifies chain ID, reads the Safe's owners/threshold/nonce, checks the required precompile authority and simulates each direct call as the Safe. Precompiles can have empty bytecode, so an `eth_getCode` check alone does not establish whether they exist.

2. In the official Safe Global UI, open the selected Safe on the selected network. Import `safe-import.json` through Transaction Builder. Compare the chain, Safe, target, zero value, decoded method and each address with `REVIEW.md`.
3. Review the actual Safe nonce, wrapping, gas fields and UI simulation before collecting signatures. This file is an inner CALL list; it does not contain a pre-approved outer Safe transaction or SafeTxHash. A single call uses CALL operation `0`; multiple calls may be wrapped by the UI in MultiSend. The precompile must see the authorized Safe as caller.
4. Collect the current required approvals and execute through the Safe UI at the intended time. Preparation does not execute the change.
5. Check the successful Safe execution event and query the relevant membership afterward. A mined outer transaction may still contain `ExecutionFailure`.

Example public post-checks:

```bash
RPC=https://rpc.deriw.com
cast call 0x00000000000000000000000000000000000007eb \
  'isBlacklistTxFrom(address)(bool)' "$ACCOUNT" --rpc-url "$RPC"
cast call 0x00000000000000000000000000000000000007eb \
  'isBlacklistTxTo(address)(bool)' "$ACCOUNT" --rpc-url "$RPC"
cast call 0x00000000000000000000000000000000000007eb \
  'isBlacklistOwner(address)(bool)' "$ACCOUNT" --rpc-url "$RPC"
cast call 0x000000000000000000000000000000000000006b \
  'isChainOwner(address)(bool)' "$ACCOUNT" --rpc-url "$RPC"
cast call "$SAFE" 'isOwner(address)(bool)' "$ACCOUNT" --rpc-url "$RPC"
cast call "$SAFE" 'getThreshold()(uint256)' --rpc-url "$RPC"
```

Choose the check corresponding to the action and the correct network RPC. The script's individual `eth_call` checks do not exercise Safe signatures, guards, an entire batch or finality.

## Executor-based deployments

The generator produces **direct calls**. The earlier Deriw testnet draft used `Safe → UpgradeExecutor → delegated MultiSendCallOnly → precompile`, so the precompile caller there is the executor. Its addresses and calldata cannot be copied into the mainnet direct-call method.

If the selected Safe fails the direct authority check and an executor owns the role, generate the separately reviewed executor route instead. This tool does not guess executor addresses, role membership or delegation. Historical drafts remain in `scripts/safe-wasm-root`; they are not current authority evidence.

## Source and offline checks

The methods and permission checks come from [DeriwBlacklist ABI](../../contracts/src/precompiles/DeriwBlacklist.sol), [its permission wrapper](../../precompiles/DeriwBlacklistWrapper.go), [ArbOwner ABI](../../contracts/src/precompiles/ArbOwner.sol) and [Safe OwnerManager](../../safe-smart-account/contracts/base/OwnerManager.sol).

```bash
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover \
  -s docs/safe-transactions/tests -v
```

For validator recovery records, see [the recovery guide](../validator-recovery/README.md). For L3 bridge claims, see [manual withdrawal](../manual-withdrawal/README.md).
