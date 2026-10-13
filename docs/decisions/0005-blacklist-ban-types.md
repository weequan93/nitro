# DER-3180: One blacklist ban type per address

## Behavior

Each address has one ban type across the sender and recipient lists. A type is
an enum value, **not a bitmask**. If a future type represents several restrictions,
code must explicitly define and implement those restrictions.

| Value | Meaning | DeriwOS behavior |
| --- | --- | --- |
| `0` | No blacklist entry (query result only) | No ban |
| `1` | Ban all | Existing top-level blacklist enforcement |
| `2` | ERC20/USDT transfer ban | Stored and queryable only |

Only `1` and `2` are accepted by flag-aware list methods. Undefined values revert;
additional types require a future update. Type `2` does not inspect calldata,
identify token contracts, or reject transactions. Type `1` retains the existing
scope and exceptions described in [the consensus blacklist decision](0004-deriwos-consensus-blacklist.md).

## API

Existing add methods default to `BanFlagAll`. Existing list and membership
getters describe blacklist membership for any type; they are not enforcement
checks. Legacy removal removes the requested direction regardless of type.
New flag-aware methods are available from DeriwOS 6:

- Owner-only: `addBlacklistTxFromWithFlag(address,uint64)` and
  `addBlacklistTxToWithFlag(address,uint64)`.
- Owner-only: `removeBlacklistTxFromWithFlag(address,uint64)` and
  `removeBlacklistTxToWithFlag(address,uint64)`.
- Owner and public precompiles: `getBlacklistBanFlag(address)` returns the
  address's single type, or zero if no direction lists contain it.
- Owner and public precompiles: `getBlacklistTxFromWithFlag(uint64)`,
  `getBlacklistTxToWithFlag(uint64)`, `isBlacklistTxFromWithFlag(address,uint64)`,
  and `isBlacklistTxToWithFlag(address,uint64)` query exactly that type.
  Enumeration scans up to 65,536 addresses in the direction list and returns
  those whose stored flag matches the requested type.

Adding a different type overwrites the single stored flag shared by **both**
direction lists, preserving existing direction membership and adding the
requested direction. It does not move the address to another list.
Adding the same type is idempotent for an existing direction. Legacy add methods
also replace a transfer type with type `1` after activation.

Example: an address exists in both legacy lists (type `1`). Calling
`addBlacklistTxFromWithFlag(address, 2)` stores flag `2` and stops ban-all
enforcement while the address stays in both lists. Calling
`addBlacklistTxTo(address)` stores flag `1`, restoring enforcement. Flags are never combined.

Flag-aware removal only removes the requested direction and requires membership
with that exact stored type. The type stays present while the other direction still contains the
address; removing the final direction makes `getBlacklistBanFlag` return zero.
Quarantined owners retain the existing direct legacy removal recovery path;
flag-aware removal selectors do not gain a new execution exception.

Protected-system-address checks still apply whenever type `1` is set, including
promotion from type `2`. Transfer-only metadata does not quarantine protected
addresses. Existing owner authorization and `OwnerActs` events also apply to
new writes; the event calldata includes the selected type.

## Storage and enforcement

There are **two** blacklist address lists: sender and recipient, plus the
separate blacklist-owner administration list. The sender/recipient lists contain
addresses of all ban types. A new mapping in blacklist subspace `3` stores one
`uint64` ban flag per address. Flag-specific getters are filtered views over
these lists, not extra stored lists.

New additions explicitly store `1` or `2`. Historical entries without a stored
flag resolve to `BanFlagAll`, so no bulk migration is needed. An address absent
from both lists resolves to zero regardless of its storage slot. Removing the
last membership clears the flag. Updates and removals execute inside the
precompile transaction and roll back with the enclosing StateDB snapshot on
failure.

`preCheckBlacklist` reads the address's ban type and explicitly requires
`banFlag == blacklist.BanFlagAll` before returning `ErrTxBlacklist` for the
sender, delegated parent, or recipient. Consensus execution uses the same
comparison before producing `vm.ErrDeriwBlacklisted`. Legacy transaction/address
helper checks, fee-account checks, and activation protection also reject only
`BanFlagAll`. Other nonzero flags must be handled by their own policy code; they
are not interpreted as bitmasks or converted into ban-all.

DeriwOS 6 gates the new storage writes and enforcement reads. Consensus performs
three deterministic storage reads per unique checked address: sender membership,
recipient membership, and ban flag. DeriwOS 0–5 retain their historical
behavior and consensus gas accounting (two membership reads from version 1).
Governance schedules version 6 through ArbOwner after deploying compatible
node/validator binaries and rebuilding the validator machine.

## Safe calldata helper

The [Safe helper](../../scripts/safe-proposals/README.md) accepts `--ban-flag 1`
(default, legacy selectors) or `--ban-flag 2` (new flag-aware selectors).
Online generation verifies DeriwOS 6 activation for type `2` before simulating
the calls. Both direct Safe and UpgradeExecutor routes are supported. Generated
output identifies the selected type and the effect on any existing ban-all entry.

## Review and validation

The review found an existing admission/consensus mismatch for a historical
self-bound subaccount: `parent != nil` disabled emergency removal in admission,
even though execution kept the signed sender unchanged and allowed recovery.
Admission now compares the effective sender identity, matching consensus.
This fixes admission only; historical consensus behavior and gas remain unchanged.

Regression coverage compares the same signed transaction and state through
`PreCheckTx` and `core.ApplyTransaction` across 35 cases: DeriwOS 1 and 6,
unlisted/type-1/type-2 addresses, sender/parent/recipient roles, ordinary recovery,
self-bound recovery, delegated recovery, and unauthorized callers. Admission
rejection leaves gas and nonce untouched; committed blacklist rejection produces
a failed receipt and consumes gas. Those outcomes intentionally differ while
the blacklist decision agrees.

Additional execution tests verify that:

- Replacing a shared flag changes subsequent transaction enforcement while
  both direction memberships remain present.
- A successful precompile update rolls back when its enclosing contract reverts.
- Insufficient gas cannot leave partial additions, replacements, or final removals.
- Delayed/aliased L1 and retryable sender checks enforce type 1 and allow type 2.
- Historical selectors, authorization, protected addresses, final flag cleanup,
  and version-dependent storage gas retain their expected behavior.

Validation passed:

```sh
go test ./arbos/blacklist ./arbos/arbosState ./arbos ./precompiles ./execution/gethexec ./gethhook -count=1
node --test scripts/safe-proposals/blacklist-calldata.test.mjs
git diff --check
```

Go tests ran in an offline Linux container with Go 1.25.13 and native library
dependencies. All six package suites and all nine Node tests passed. Compiled
blacklist ABIs match the explorer ABI files; the new functions are also present
in the mirrored Solidity interfaces.

This validates native execution and the reviewed feature scope. It does not
establish validator/WASM replay equivalence or constitute a deployed-chain test.
The existing top-level scope, funding/internal-transaction exemptions, emergency
removal exception, and non-mutating simulation behavior remain intentional.
Nested calls and ERC20 calldata addresses are not covered by ban-all enforcement;
type 2 remains metadata only.
