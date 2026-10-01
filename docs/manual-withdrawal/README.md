# Deriw L3 manual withdrawal

This is the reusable method used for the individual withdrawal checks and claim scripts during the validator recovery. It replaces the root-level `withdrawal-*` case directories. Transaction hashes, proofs, calldata, amounts and status reports are generated per request; they are not maintained as source files.

## Files

| File | Purpose |
| --- | --- |
| [prepare.py](prepare.py) | Read withdrawal events, check claim status, verify a proof and prepare a claim. It never signs or broadcasts. |
| [claim-with-wallet.js](claim-with-wallet.js) | Optional browser wallet method; submission happens when `claimWithdrawal(bundle)` is called. |
| [tests/test_prepare.py](tests/test_prepare.py) | Offline event, proof and preparation checks. |
| `generated/` | Ignored per-request receipts, proofs, reports and claim files. |

Requires Python 3, `curl` and Foundry `cast`. Commands below run from the repository root. RPC URLs can be overridden with `--l3-rpc` and `--parent-rpc`.

| Deployment | Value |
| --- | --- |
| L3 | Deriw, chain ID `2886`, default RPC `https://rpc.deriw.com` |
| Parent | Arbitrum One, chain ID `42161`, default RPC `https://arb1.arbitrum.io/rpc` |
| Rollup | `0xa113e2e9620a3bc088a681ebb2c234fdbeb85e21` |
| Outbox | `0x47da6c41d03ac0608924e86f61577df558114bd8` |

The tool verifies both chain IDs and `Rollup.outbox()` against this deployment. These are L3 bridge claims, separate from [validator stake refunds](../../scripts/validator-recovery/toolkit/withdraw-refund.sh).

## 1. Check whether a withdrawal has been claimed

Set `L3_TX` to the original **L3 withdrawal transaction hash**, not a parent claim hash:

```bash
export PATH="$HOME/.foundry/bin:$PATH"
L3_TX=0xYOUR_L3_TRANSACTION_HASH
python3 docs/manual-withdrawal/prepare.py "$L3_TX" --status-only
```

Multiple hashes can be supplied with `--status-only`. Every `ArbSys.L2ToL1Tx` event in each receipt is checked. `claimed: true` means `Outbox.isSpent(index)` is true at the saved parent block. It does not identify the parent claim transaction or independently reconcile the recipient's token balance.

## 2. Prepare the claim

```bash
python3 docs/manual-withdrawal/prepare.py "$L3_TX"
```

The command prints an `OUTPUT` directory. For reproducible output placement, use `--out docs/manual-withdrawal/generated/my-check` with a new directory. If a receipt has several withdrawal events, choose one with `--index MESSAGE_INDEX`.

Preparation follows the original method:

1. Verify the successful L3 receipt and its block hash, then decode the sender, destination, message index, block numbers, timestamp, native value and payload.
2. Read that block's `sendCount` and `sendRoot`; request `NodeInterface.constructOutboxProof(sendCount, index)`.
3. Check the returned send hash against the event and `Outbox.calculateItemHash`; check the proof using `calculateMerkleRoot` and compare it with the block's send root.
4. At one saved parent block, check `isSpent` and `roots(sendRoot)`. If the root is registered, simulate the complete `executeTransaction` call.
5. Save the evidence, fixed calldata, wallet request and a CLI script. Recheck that the parent observation block's hash is unchanged.

| Result | Next action |
| --- | --- |
| `already_claimed` | No claim files are generated. |
| `root_not_confirmed` | Wait for a root covering the message to be confirmed; prepare again. |
| `claim_prepared` | Review the destination, native value, payload and any decoded token recipient/amount before sending. |
| `STOP` | Fix the reported receipt, proof, RPC or simulation problem before proceeding. |

This method requests the root at the withdrawal's own block. If that root never gets registered, a proof at a later confirmed accumulator size may be needed; this tool does not automatically choose that later root. RPC nodes must retain the history required for the receipt and proof.

Only `finalizeInboundTransfer` payloads are decoded into token/recipient/amount fields. Other message types keep their original payload and native value for direct review. Token display metadata is optional; the raw amount is retained. The transaction's outer `value` is zero: the proven native withdrawal value is an argument to the Outbox call.

## 3. Sign and submit locally

Choose one method. The gas-paying account may differ from the withdrawal recipient; it cannot change the proven recipient or message payload.

### CLI with an encrypted keystore

Replace `OUTPUT_DIRECTORY` with the path printed by preparation:

```bash
bash OUTPUT_DIRECTORY/claim-cli.sh \
  https://arb1.arbitrum.io/rpc \
  /absolute/path/to/encrypted-keystore.json
```

The administrator enters the password locally. With no second argument, the script uses `cast --interactive` for a hidden private-key prompt. Do not place a password or private key in the command, output directory or repository.

The generated script rechecks the chain, Rollup's Outbox, spent flag and root, then simulates the complete call again before `cast send`. Running it sends a real parent-chain transaction. A competing claim may still execute between these checks and submission.

### Browser wallet

Load [claim-with-wallet.js](claim-with-wallet.js) on a trusted page with your connected EIP-1193 wallet. Copy the generated `wallet-claim.json` into a local `bundle` variable, then run:

```javascript
await claimWithdrawal(bundle);
```

The helper checks Arbitrum One and the fixed Outbox transaction, rejects an already-spent message, simulates the full call, estimates gas and requests wallet submission. Review the wallet prompt. Returning a hash confirms submission only.

## 4. Confirm execution

Use the parent claim transaction hash returned by your chosen method:

```bash
PARENT_TX=0xYOUR_PARENT_CLAIM_TRANSACTION_HASH
cast receipt "$PARENT_TX" --rpc-url https://arb1.arbitrum.io/rpc
python3 docs/manual-withdrawal/prepare.py "$L3_TX" --status-only
```

Check a successful receipt, `isSpent=true`, and the recipient's actual transfer/balance. A submitted hash or successful `eth_call` alone is not proof of payment. These per-message checks are not a full withdrawal-history audit.

## Offline verification and source references

```bash
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover \
  -s docs/manual-withdrawal/tests -v
node --check docs/manual-withdrawal/claim-with-wallet.js
```

The ABI and proof method are grounded in this branch's [ArbSys event](../../contracts-legacy/src/precompiles/ArbSys.sol), [NodeInterface ABI](../../contracts-legacy/src/node-interface/NodeInterface.sol), [proof implementation](../../execution/nodeinterface/node_interface.go), and [Outbox implementation](../../contracts-legacy/src/bridge/AbsOutbox.sol).

Per-case root directories, dated status reports and one-off claim scripts were removed during the 2026-09-30 documentation cleanup. The existing receipt fixture required by the recovery tests remains under `scripts/validator-recovery/legacy/tests/fixtures`; no historical claim status is presented here as current.
