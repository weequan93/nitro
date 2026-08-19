# Version the DER Execution Blacklist with DeriwOS

## Context and Problem Statement

The legacy DER blacklist is an admission policy. A sequencer can reject a transaction, but a delayed message can still reach consensus execution. It also checks the signed subaccount without consistently checking the effective parent account.

Changing those rules under the existing ArbOS version would make historical replay ambiguous. DER also needs to record which upstream ArbOS version was active when a DER-specific consensus change was scheduled.

## Considered Options

- Keep admission-only filtering and accept bypass through non-sequencer paths.
- Tie the behavior directly to an upstream ArbOS version.
- Add an independent, state-backed DeriwOS version while reporting its ArbOS compatibility pair.

## Decision Outcome

Add an independent `DeriwOSVersion` to append-only ArbOS state.

- DeriwOS 0 preserves historical consensus execution exactly.
- DeriwOS 1 enables the consensus blacklist.
- Scheduling a DeriwOS upgrade records the active ArbOS version.
- Scheduling rejects a DeriwOS version newer than the running binary supports.
- Start-block processing applies an ArbOS upgrade first and a DeriwOS upgrade second.
- No block-header format is changed.
- Nodes refuse to start on an active DeriwOS version newer than they support.

Under DeriwOS 1, membership in either legacy list blocks an address in any checked top-level role. Consensus execution checks only:

- the signed transaction sender;
- the effective parent when a permitted subaccount transaction executes as its parent; and
- the explicit top-level transaction destination when `to` is present.

Funding-only deposits and retryable submissions are allowed so L1 funds are always credited and retryable tickets remain recoverable. The later `ArbitrumRetryTx` is an ordinary top-level L2 execution and applies the `from`/`to` rule to its aliased L2 sender, original L1 sender, and actual retry destination. For other aliased L1-originated L2 execution types, both sender identities are also checked. A violation produces a failed receipt, consumes the complete supplied gas limit without a refund, preserves no transaction execution effects, and advances the signed child nonce plus the effective parent nonce when delegation is active. Fee accounting remains allowed.

The check applies once to an externally originated top-level transaction. Protocol-generated `ArbitrumInternalTx` execution is not checked. The scope is intentionally not a full EVM quarantine: DeriwOS 1 does not inspect calldata, ERC-20 owners/recipients/spenders, nested `CALL` targets, `CALLCODE`, `DELEGATECALL`, `STATICCALL`, derived `CREATE` or `CREATE2` addresses, `SELFDESTRUCT` beneficiaries, EIP-7702 authorities/code targets, or retryable refund and beneficiary addresses. A clean top-level sender and destination can therefore interact with a blacklisted address indirectly. This limitation is accepted for DeriwOS 1 and must not be represented as bypass-proof enforcement.

Non-mutating RPC simulation remains available. Blacklist membership reads for consensus checks are charged deterministically: both legacy sets are read once for each unique checked address.

System/recovery safety rules are:

- Scheduling fails if any protected address is already present in either legacy list.
- From the moment DeriwOS 1 is scheduled, ArbOS state, system precompiles, protocol system contracts, the L1 pricing pool, the batch poster, and current network/infrastructure fee accounts cannot be newly blacklisted.
- From scheduling onward, fee-account setters reject a quarantined replacement.
- A quarantined authorized owner has one recovery path: a direct, zero-value call to the Deriw blacklist precompile with canonical calldata for exactly `removeBlacklistTxFrom(address)` or `removeBlacklistTxTo(address)`. Proxy, subaccount, multicall, malformed, and unrelated calls do not receive the exception.

### Consequences

This is a consensus and validator change. Every validator, sequencer, batch poster, full node, and replay environment must run a DeriwOS-1-capable binary before activation. The Nitro validator machine must be rebuilt and its module root validated before the on-chain upgrade is scheduled.

Admission filtering remains useful for avoiding knowingly failed transactions, but consensus independently enforces the same narrow top-level address rule. Historical databases remain readable because all new state fields are appended and default to zero.
