# Historical Safe transaction drafts

These three Transaction Builder imports were prepared during the September 2026 operational work. The directory retains its original name, although the saved calls concern blacklist administration and a DAS keyset.

For a new blacklist or owner operation, use the [reusable Safe transaction guide](../../docs/safe-transactions/README.md) and generate a fresh package for the intended Safe and chain.

## Saved calls

| Import | Chain / Safe | Decoded action |
| --- | --- | --- |
| [add-deriw-blacklist-owner.safe.json](add-deriw-blacklist-owner.safe.json) | Deriw testnet `2885`; `0xe5c8e6dabe8da8d90f0ae3d4543e930833a0e9ec` | UpgradeExecutor `execute(address,bytes)` → MultiSendCallOnly → `DeriwBlacklist.addBlacklistOwner(address)`. |
| [add-deriw-mainnet-blacklist-owner.safe.json](add-deriw-mainnet-blacklist-owner.safe.json) | Deriw mainnet `2886`; `0x2f996bc558818d33de37af36bee7de24ba3fc4df` | Direct CALL to `DeriwBlacklist.addBlacklistOwner(address)`. |
| [set-deriw-mainnet-keyset-h2.safe.json](set-deriw-mainnet-keyset-h2.safe.json) | Arbitrum One `42161`; `0xfbb37c66372f7b40361fbc8c8a235ae92711399d` | UpgradeExecutor `executeCall(address,bytes)` → SequencerInbox `setValidKeyset(bytes)`. |

Both blacklist drafts add administrator `0x26a5ccec74e4e193cb47350e473b85410ea02d82` at precompile `0x00000000000000000000000000000000000007ec`. They grant blacklist administration, rather than adding a blocked sender or destination.

The keyset draft targets SequencerInbox `0xe79283a775f6a1de250cb3284e7ff3541ff7668a`. Its serialized keyset contains `H=2`, `N=3`, so the AnyTrust requirement `N + 1 - H` is two signers. This encodes a specific committee trust assumption. Its hash is:

```text
0xcdec90dd41576909713570c83c8e5ebaeea9ca01d91972644a1aadb1b16f4430
```

## Review and execution status

Offline review on October 1, 2026 decoded every wrapper and reproduced each encoded call with local `cast`. It checked zero call values, the single packed CALL in the testnet MultiSend, the blacklist address argument, and the keyset serialization boundaries, distinct public-key bytes and hash. The hash calculation follows [SequencerInbox.sol](../../contracts/src/bridge/SequencerInbox.sol); serialization follows [AnyTrust utilities](../../daprovider/anytrust/util/util.go).

The BLS public-key validity proofs were not cryptographically verified in this review. No live Safe owners, roles, contract code or current keyset registration were queried. No execution receipt accompanies these imports, so this archive does not establish whether they were proposed, signed or executed.

Builder imports contain inner calls. They do not pin the full Safe outer transaction, nonce, gas fields, signatures or Safe transaction hash. The saved metadata describes the original preparation and does not establish current permissions. Check the current authority, contracts, keyset and exact outer Safe transaction before using any draft. This review did not submit or broadcast a transaction.
