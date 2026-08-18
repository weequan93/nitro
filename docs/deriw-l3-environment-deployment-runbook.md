# Deriw L3 environment deployment and activation runbook

Last verified against live RPC state: 2026-08-19

Target branch: `fix/blacklist-subaccount`

Reviewed root commit: `73189f61f9bbd3b7f7397d6f9c2625f69fd49e04`

## 1. Purpose and release status

This runbook covers deployment of the combined Nitro changes for:

- consensus blacklist enforcement;
- gasless `eth_estimateGas` support; and
- router-only raw `ArbSys.sendTxToL1` and canonical ERC-20 enforcement, with
  direct native-ETH withdrawals restored by DeriwOS 3.

Deriw is an L3. In this document:

- **parent L2** means Arbitrum Sepolia for development/test and Arbitrum One
  for production;
- **L3** means the corresponding Deriw chain; and
- the L3 Rollup contract and its proving WASM module root live on the parent
  L2, while ArbOS/DeriwOS governance precompiles live on the Deriw L3.

Do not deploy the reviewed commit to production unchanged. The current
security review has three unresolved findings:

- a blacklist owner can schedule an immediate DeriwOS consensus upgrade;
- gasless estimation can change `BASEFEE`/`GASPRICE` execution semantics; and
- sender-only gasless allowlisting is not mirrored by estimation.

Development deployment may be used to reproduce and fix those findings, but
test and production promotion require a new approved commit and a repeat of
the checks in this runbook.

## 2. Non-negotiable sequence

Use this order for every environment:

1. Freeze and approve one immutable source commit and all submodule gitlinks.
2. Inventory the parent Rollup, parent UpgradeExecutor, L3 UpgradeExecutor,
   authorized signer, current WASM root, current versions, and route contracts.
3. Build the node binaries, proving machine, and runtime images from that exact
   commit.
4. Prove the runtime validator image contains both the current and proposed
   WASM machines.
5. Run the parent Nitro-contract version checker and, only when its reported
   upgrade path requires it, upgrade and verify the parent Inbox and
   SequencerInbox contracts.
6. Deploy passive RPC/full nodes, then validators/stakers, with no consensus
   activation transaction yet.
7. Run current-root and pending-root validation in parallel.
8. Change the Rollup WASM module root on the parent L2, if required.
9. Observe successful assertions/validation under the new official root while
   DeriwOS behavior remains unchanged.
10. Upgrade the batch poster and sequencer.
11. Upgrade the L3 to internal ArbOS 60 when it is below 60. Production
    currently requires this step; development and test do not.
12. Set and verify the L3 maximum transaction gas limit of `60,000,000` after
    ArbOS 60 is active. Skip the write when the live value already matches.
13. Stage and activate the environment's L3 router configuration when needed.
14. Schedule and verify DeriwOS 1 (consensus blacklist).
15. Schedule and verify DeriwOS 2 (router-only ArbSys sends).
16. Schedule and verify DeriwOS 3 (direct `withdrawEth` restored; raw messages
    and ERC-20 sends remain route-restricted).
17. Run the full acceptance suite and complete the soak period before promoting
    the same release to the next environment.

Never activate DeriwOS before all execution and validation roles run compatible
code. Never update the parent Rollup to a module root that is absent from any
active validator/staker image.

## 3. Environment and account matrix

The following L3 and parent-governance values were read from the live chains on
2026-08-18. Re-run the preflight queries before every release; a Safe URL is
only a user interface and does not itself grant on-chain authority.

| Item | Development | Test | Production |
|---|---|---|---|
| Parent L2 | Arbitrum Sepolia | Arbitrum Sepolia | Arbitrum One |
| Parent chain ID | `421614` | `421614` | `42161` |
| Parent UpgradeExecutor | `0x1b46Af3D21A13fd30D2BD396B308A6313aD22f1D` | `0x678815F2c63466f557024D8cCe25BaeeB4A23359` | `0x1333480e92de9511dc9BB01F70901ff3ee94f613` |
| Parent governance Safe | `0x4Ec94DD57A65C3E1C59929885a3d3612941B75c2` | `0x663C00bA160ff059223f9f56bf80b1aE89DAceDe` | `0xFbB37c66372f7B40361fBC8C8A235ae92711399D` |
| Parent signing threshold | 1-of-1 | 1-of-2 | 3-of-4 |
| Parent submission method | Safe owner submits `Safe.execTransaction` with `cast` | Safe owner submits `Safe.execTransaction` with `cast` | Proposer creates a Safe UI/API proposal; owners sign and execute |
| Parent Safe UI | No hosted service is required | No hosted service is required | Approved Arbitrum One Safe UI/service |
| L3 RPC | `https://rpc.dev.deriw.com` | `https://rpc.test.deriw.com` | `https://rpc.deriw.com` |
| L3 chain ID | `18417507517` | `2885` | `2886` |
| Live internal ArbOS | 60 (`ArbSys` returns 115) | 60 (`ArbSys` returns 115) | 32 (`ArbSys` returns 87) |
| Live max transaction gas | `32,000,000` | `60,000,000` | unavailable before ArbOS 50; migration initializes `32,000,000` |
| Live max block gas | `300,000,000` | `60,000,000` | `300,000,000` through the legacy getter |
| L3 Safe UI | `https://safe.dev.deriw.com` | `https://safe.test.deriw.com` | `https://safe.deriw.com` |
| L3 Safe Transaction Service | Obtain from approved Safe UI/operations configuration | Obtain from approved Safe UI/operations configuration | Obtain from the approved `safe.deriw.com` configuration |
| L3 UpgradeExecutor | `0xB5B4d7f7a32D86fF3bc270B864c7c06CE6F0BD78` | `0xAc3516eF1E3658887198D192Cb0D7EcA07604943` | `0xC49f79CcdFbB3668400b7476A641268De81548b1` |
| L3 governance signer | Safes `0x5f1B197A82fC1148A02Ea55B3BEF529f78D64151` and `0x9Caa16915f33F5A122351d828B07F8758a53bdEa` | Safes `0xE5C8e6dAbE8dA8D90F0AE3d4543E930833A0e9Ec` and `0x573a3d28B82627a4dB4C48686ee986FdDedc1017` | Safe `0x2F996bC558818D33DE37aF36Bee7de24bA3Fc4dF` |
| L3 signing threshold | `0x5f1B...4151`: 1-of-4; `0x9Caa...bdEa`: 1-of-3 | `0xE5C8...e9Ec`: 1-of-2; `0x573a...1017`: 2-of-4 | 3-of-4 |
| L3 governance path | Either approved Safe calls L3 UpgradeExecutor | Either approved Safe calls L3 UpgradeExecutor; parent UpgradeExecutor alias is retained | Safe calls L3 UpgradeExecutor |

### Privileged transaction submission rule

Code deployment and governance execution are different operations. An approved
EOA may deploy a reviewed action contract, but it must not execute a privileged
state change directly. Every release-governance transaction must use this
outer path:

```text
approved Safe
  -> environment UpgradeExecutor
  -> approved contract or precompile target
```

For a normal target call, the Safe transaction has `to = UpgradeExecutor`,
`value = 0`, `operation = CALL`, and data encoding
`executeCall(target, innerCalldata)`. The Nitro contract-upgrade action is the
documented exception in calldata shape: it uses
`UpgradeExecutor.execute(action, actionCalldata)` because that approved action
is executed in the UpgradeExecutor context. Its outer Safe target is still the
UpgradeExecutor.

Never submit a release transaction directly from an EOA to an UpgradeExecutor,
`ArbOwner`, `DeriwBlacklist`, a Rollup, a ProxyAdmin, or another privileged
target. Never impersonate a parent UpgradeExecutor alias on an L3; that alias
is reserved for authenticated cross-chain execution.

Observed development parent-governance state:

```text
Arbitrum Sepolia chain ID
  421614

Parent Safe
  0x4Ec94DD57A65C3E1C59929885a3d3612941B75c2
  version:   1.4.1
  owner:     0x94A6713cbF5F589aB51570D0b4cd219792421af2
  threshold: 1-of-1
  modules:   none
  guard:     none

Parent UpgradeExecutor
  0x1b46Af3D21A13fd30D2BD396B308A6313aD22f1D

EXECUTOR_ROLE
  Safe 0x4Ec94D...75c2: true
  EOA  0x94A671...1af2: false
```

The required development parent-governance path is:

```text
owner EOA 0x94A671...1af2
  -> parent Safe 0x4Ec94D...75c2
  -> parent UpgradeExecutor 0x1b46Af...2f1D
  -> approved parent target
```

The 1-of-1 threshold is an accepted development-only risk. It enforces the
Safe-to-UpgradeExecutor transaction path but does not reduce single-key
compromise risk. The owner EOA must remain removed from the parent
UpgradeExecutor's direct `EXECUTOR_ROLE`.

Development parent-governance migration evidence:

```text
Safe deployment
  0xbe7089a0611bb64ccdd93c8813393ed9d030bd084747846065a6bce22420d236
Safe EXECUTOR_ROLE grant
  0x7e5176de79094a0347d5085ecea4861cdc06e5541bf9eb97d3e248c3259d921e
Safe execution test
  0x3eb44a7cf2f924a819142a164a2eceec50fc99dd5793a61abe81e123c787b545
Direct EOA EXECUTOR_ROLE revocation
  0xae34f64ffedcc480c2a6ce83ed4b95ec1de872d823ed69d94f083c9f3ec9e1e8
```

Observed test parent-governance state:

```text
Arbitrum Sepolia chain ID
  421614

Parent Safe
  0x663C00bA160ff059223f9f56bf80b1aE89DAceDe
  version:   1.4.1
  owners:    0xa1698F44D70632BfE448804378DA373C55eE8476
             0x35b3ac4003e1AfeE7601C190DB4f039fCb1BbcB5
  threshold: 1-of-2

Parent UpgradeExecutor
  0x678815F2c63466f557024D8cCe25BaeeB4A23359

EXECUTOR_ROLE
  Safe 0x663C00...ceDe: true
  EOA  0xa1698F...8476: false
  EOA  0x35b3ac...bcB5: false
```

The required test parent path is `Safe 0x663C00...ceDe -> UpgradeExecutor
0x678815...3359 -> approved parent target`. Either Safe owner may submit the
1-of-2 Safe transaction, but neither owner may call the UpgradeExecutor
directly.

Observed production parent-governance state:

```text
Arbitrum One chain ID
  42161

Parent Safe
  0xFbB37c66372f7B40361fBC8C8A235ae92711399D
  version:   1.4.1
  owners:    0xA0c2aed24f5474B2815b2fF61D0f5a01970217C3
             0xc60f0Ed09edd696e60574F714cBd7CFeC004DD70
             0xc63b7a2DAcfa3aEd4ea158F4f51FfBE020B9DE4c
             0x09Ad976b259D9174F4250f0244873c3Bc876E2ce
  threshold: 3-of-4

Parent UpgradeExecutor
  0x1333480e92de9511dc9BB01F70901ff3ee94f613

EXECUTOR_ROLE
  Safe 0xFbB37c...399D: true
  legacy EOA 0x35b3ac...bcB5: false
```

Production operators with proposal-only permission create the transaction for
Safe `0xFbB37c...399D`; three owners approve it and the Safe calls
`0x133348...F613`. A proposal signature is not an owner confirmation and does
not execute the transaction.

Observed L3 chain-owner lists:

```text
Development
  0xB5B4d7f7a32D86fF3bc270B864c7c06CE6F0BD78  UpgradeExecutor

Test
  0xAc3516eF1E3658887198D192Cb0D7EcA07604943  UpgradeExecutor

Production
  0xC49f79CcdFbB3668400b7476A641268De81548b1  UpgradeExecutor
```

Observed L3 blacklist-owner lists:

```text
Development: 0x57F93d0dFa75206f61F2BcD41Cb61c499d48Fe17
Test:        empty
Production: 0x2F996bC558818D33DE37aF36Bee7de24bA3Fc4dF
```

Use the L3 UpgradeExecutor path for release governance. Direct EOA chain-owner
and `EXECUTOR_ROLE` permissions are migration-only authority and must be removed
after the approved cleanup Safe passes an on-chain execution test.
Development intentionally retains `0x57F9...Fe17` as its standalone blacklist
owner. That permission is not a deployment path: every transaction in this
runbook, including blacklist and DeriwOS actions, must still use an approved
Safe through the L3 UpgradeExecutor. Any future direct use of the retained
blacklist-owner EOA is an emergency/operations exception outside this runbook
and requires a separate approved procedure.

### Development L3 governance Safes

The approved development L3 `EXECUTOR_ROLE` members are exactly:

```text
0x5f1B197A82fC1148A02Ea55B3BEF529f78D64151  Safe 1.3.0, 1-of-4
0x9Caa16915f33F5A122351d828B07F8758a53bdEa  Safe 1.3.0, 1-of-3
```

At initial review block `144717262`, both addresses were deployed Safe 1.3.0
proxies with no modules or guard, and both returned `false` for the development
L3 UpgradeExecutor's `EXECUTOR_ROLE`:

```text
Safe A 0x5f1B...4151
  owners:
    0x0a35af329a67446e02e24982dc12918bdd4925c4
    0x57f93d0dfa75206f61f2bcd41cb61c499d48fe17
    0x09ad976b259d9174f4250f0244873c3bc876e2ce
    0x1dccff6aec56a2b58d08f3dd96e75491dcbd3a84
  threshold: 1-of-4
  nonce:     92

Safe B 0x9Caa...bdEa
  owners:
    0xa1698F44D70632BfE448804378DA373C55eE8476
    0xf98233de9e9f613f6052bc838ce49989e3e43b1f
  threshold: 2-of-2
  nonce:     3
```

Both grants were subsequently executed and independently verified active at
block `144723476`:

```text
Safe A grant
  block: 144723053
  tx:    0xc10ca9a95b502b08d92e6240213859f4fcd34a4e54010ad24ebfbc9004158438

Safe B grant
  block: 144723364
  tx:    0x6f5522a26addcd8ce6834e159f2853b2a1075965903e20e1f0e5c3ba145f6e3e
```

Safe B was reconfigured after the initial review. A fresh read at block
`144726120` returned:

```text
Safe B 0x9Caa...bdEa
  owners:
    0x94A6713cbF5F589aB51570D0b4cd219792421af2
    0xa1698F44D70632BfE448804378DA373C55eE8476
    0xf98233de9e9f613f6052bc838ce49989e3e43b1f
  threshold: 1-of-3
  modules:   none
  guard:     none
  nonce:     6
```

The Safe B UpgradeExecutor path test subsequently succeeded:

```text
Safe B path test
  block:        144727578
  tx:           0x5257d16e070d28de94a8cfc48e0948ed9379f01a32c8ead373b45df9ae6d1c3d
  caller:       0x94A6713cbF5F589aB51570D0b4cd219792421af2
  target:       ArbOwnerPublic 0x000000000000000000000000000000000000006b
  inner call:   getAllChainOwners()
  Safe nonce:   6 -> 7
  receipt:      success
```

Development L3 cleanup progress:

```text
Remove 0x57F9...Fe17 from ChainOwners
  block:             144728908
  tx:                0x1a842e1f0700df7305a6d74fca7fadbf96faa2f2d1b310acfb6674eb8bed5aea
  Safe B nonce:      7 -> 8
  isChainOwner:      false
  isBlacklistOwner:  true
  receipt:           success

Remove 0x94A6...1af2 from ChainOwners
  block:             144729456
  tx:                0xc2b8dba31192f5cfd0aca7ce572b89cee9f4666dfcb357927516e835887f7719
  Safe B nonce:      8 -> 9
  chain owners:      UpgradeExecutor only
  isChainOwner:      false
  receipt:           success

Revoke EXECUTOR_ROLE from 0x2c57...2402
  block:             144730617
  tx:                0xca089dc9af630bb4ef2fcb4da9f23a37ad0f2ca9ba9bdf52bc29b7945f8790c4
  Safe B nonce:      9 -> 10
  hasRole:           false
  receipt:           success

Revoke EXECUTOR_ROLE from 0x9D41...76DA
  block:             144731155
  tx:                0x49820e924299d3f6fbadaa09afad6662b03db64e96e21feadb3191f9c4be6d86
  Safe B nonce:      10 -> 11
  hasRole:           false
  receipt:           success

Revoke EXECUTOR_ROLE from 0x94A6...1af2
  block:             144731582
  tx:                0xbc38d94f2ca1377d6146c77471e8969ad189b6b1ed8dca3af0c56ad599d046fd
  Safe B nonce:      11 -> 12
  hasRole:           false
  receipt:           success

Final verification
  block:             144731692
  chain owners:      UpgradeExecutor only
  blacklist owners:  0x57F9...Fe17 only
  executors:         Safe A and Safe B only
  result:            PASS
```

Safe B is the tested cleanup path. Operations explicitly waived the Safe A
UpgradeExecutor path test for this development migration; Safe A remains an
authorized but untested executor. Safe A also administers the development Deriw
router. Safe A's 1-of-4 and Safe B's 1-of-3 thresholds are accepted
development-only availability policies and each remains a single-key
compromise risk.

The required steady-state permission graph is:

```text
Either approved L3 Safe
  -> L3 UpgradeExecutor 0xB5B4d7f7a32D86fF3bc270B864c7c06CE6F0BD78
  -> ArbOwner, DeriwBlacklist, or another approved L3 governance target

L3 chain owners
  -> UpgradeExecutor only

L3 UpgradeExecutor EXECUTOR_ROLE
  -> 0x5f1B197A82fC1148A02Ea55B3BEF529f78D64151
  -> 0x9Caa16915f33F5A122351d828B07F8758a53bdEa
```

Do not confuse either L3 Safe with the parent Safe `0x4Ec94D...75c2`.

#### Development L3 executor migration

**Completed on 2026-08-18; do not repeat the grant or cleanup transactions.**
For future release preflight, use only the read-only `audit` and `verify-final`
modes of
[`scripts/dev-l3-governance-migration.sh`](../scripts/dev-l3-governance-migration.sh).
The commands below are retained solely as migration evidence. They are not a
deployment procedure, and the former EOA executor has already been revoked.

The live UpgradeExecutor is:

```bash
export L3_RPC="https://rpc.dev.deriw.com"
export L3_CHAIN_ID="18417507517"
export L3_UPGRADE_EXECUTOR="0xB5B4d7f7a32D86fF3bc270B864c7c06CE6F0BD78"
export L3_SAFE_A="0x5f1B197A82fC1148A02Ea55B3BEF529f78D64151"
export L3_SAFE_B="0x9Caa16915f33F5A122351d828B07F8758a53bdEa"
export L3_EXECUTOR_ROLE="0xd8aa0f3194971a2a116679f7c2090f6939c8d4e01a2a8d7e41d55e5351469e63"
```

The initial Safe grants were completed through
`UpgradeExecutor.executeCall(UpgradeExecutor, grantRole(...))` by the former
executor before that EOA was revoked. Do not recreate or rebroadcast those
transactions. Future role changes must themselves be proposed by an already
approved Safe to `L3_UPGRADE_EXECUTOR` and must encode the role change inside
`executeCall`.

These read-only checks must continue to return `true`:

```bash
cast call --rpc-url "$L3_RPC" "$L3_UPGRADE_EXECUTOR" \
  "hasRole(bytes32,address)(bool)" "$L3_EXECUTOR_ROLE" "$L3_SAFE_A"

cast call --rpc-url "$L3_RPC" "$L3_UPGRADE_EXECUTOR" \
  "hasRole(bytes32,address)(bool)" "$L3_EXECUTOR_ROLE" "$L3_SAFE_B"
```

Submit a controlled transaction from the Safe selected to perform cleanup. A
state-neutral execution test can target the public chain-owner getter:

```bash
export SAFE_TEST_INNER_CALLDATA="$(cast calldata "getAllChainOwners()")"

cast calldata \
  "executeCall(address,bytes)" \
  0x000000000000000000000000000000000000006b \
  "$SAFE_TEST_INNER_CALLDATA"
```

For Safe B, enter the L3 UpgradeExecutor as `to`, `0` as `value`, and the
encoded `executeCall` as `data` in `https://safe.dev.deriw.com`. Collect one
valid owner signature under its live 1-of-3 threshold. Require a successful
receipt, a `TargetCallExecuted` event, and an increment of exactly one in the
Safe nonce. Safe B satisfied this gate in transaction `0x5257...1c3d`.

Only after this test passes may Safe B prepare the cleanup calldata:

```bash
export REMOVE_CHAIN_OWNER_57_CALLDATA="$(cast calldata \
  "removeChainOwner(address)" \
  0x57F93d0dFa75206f61F2BcD41Cb61c499d48Fe17)"

export REMOVE_CHAIN_OWNER_94_CALLDATA="$(cast calldata \
  "removeChainOwner(address)" \
  0x94A6713cbF5F589aB51570D0b4cd219792421af2)"

export REVOKE_EXECUTOR_2C_CALLDATA="$(cast calldata \
  "revokeRole(bytes32,address)" \
  "$L3_EXECUTOR_ROLE" \
  0x2c57af3d21a13fd30d2bd396b308a6313ad2402e)"

export REVOKE_EXECUTOR_9D_CALLDATA="$(cast calldata \
  "revokeRole(bytes32,address)" \
  "$L3_EXECUTOR_ROLE" \
  0x9D4130d6646Fde37C9EE9485a01E1f2Dd71476DA)"

export REVOKE_EXECUTOR_94_CALLDATA="$(cast calldata \
  "revokeRole(bytes32,address)" \
  "$L3_EXECUTOR_ROLE" \
  0x94A6713cbF5F589aB51570D0b4cd219792421af2)"
```

Create a Safe batch whose five entries all target `L3_UPGRADE_EXECUTOR`, use
value `0`, and contain these outer calls in this order:

```bash
# 1. Remove 0x57F9...Fe17 as a direct chain owner.
cast calldata "executeCall(address,bytes)" \
  0x0000000000000000000000000000000000000070 \
  "$REMOVE_CHAIN_OWNER_57_CALLDATA"

# 2. Remove 0x94A6...1af2 as a direct chain owner.
cast calldata "executeCall(address,bytes)" \
  0x0000000000000000000000000000000000000070 \
  "$REMOVE_CHAIN_OWNER_94_CALLDATA"

# 3. Revoke the old 0x2c57...2402 executor.
cast calldata "executeCall(address,bytes)" \
  "$L3_UPGRADE_EXECUTOR" \
  "$REVOKE_EXECUTOR_2C_CALLDATA"

# 4. Revoke the old 0x9D41...76DA executor.
cast calldata "executeCall(address,bytes)" \
  "$L3_UPGRADE_EXECUTOR" \
  "$REVOKE_EXECUTOR_9D_CALLDATA"

# 5. Revoke the migration EOA last.
cast calldata "executeCall(address,bytes)" \
  "$L3_UPGRADE_EXECUTOR" \
  "$REVOKE_EXECUTOR_94_CALLDATA"
```

Perform every removal through `Safe -> UpgradeExecutor.executeCall`; never call
the privileged precompile or `revokeRole` directly from an EOA. Decode every
batch entry before collecting signatures. The last call deliberately removes
the migration EOA only after all other cleanup calls have succeeded.

Final-state verification must prove exactly:

```text
Chain owners:
  0xB5B4d7f7a32D86fF3bc270B864c7c06CE6F0BD78

EXECUTOR_ROLE:
  0x5f1B197A82fC1148A02Ea55B3BEF529f78D64151
  0x9Caa16915f33F5A122351d828B07F8758a53bdEa

Blacklist owners:
  0x57F93d0dFa75206f61F2BcD41Cb61c499d48Fe17
```

Verify the two approved members return `true`, all three former executor EOAs
return `false`, and replay every `RoleGranted` and `RoleRevoked` event from the
UpgradeExecutor deployment block to ensure no undiscovered active executor
remains. Removing `0x57F9...Fe17` from the chain-owner set does not remove it
from the blacklist-owner set or Safe A's owner list. Revoking the direct
`EXECUTOR_ROLE` and chain-owner permissions of `0x94A6...1af2` does not remove
it from Safe B's owner list. These indirect Safe authorities are retained by
the approved development policy; changing either requires a separate Safe
governance decision.

### Test L3 governance Safes

The approved test L3 `EXECUTOR_ROLE` members are exactly:

```text
0xE5C8e6dAbE8dA8D90F0AE3d4543E930833A0e9Ec  Safe 1.3.0, 1-of-2
0x573a3d28B82627a4dB4C48686ee986FdDedc1017  Safe 1.3.0, 2-of-4
0x789915f2c63466F557024D8cCe25BAEEb4a2446a  aliased parent UpgradeExecutor
```

The alias maps to Arbitrum Sepolia UpgradeExecutor
`0x678815f2c63466f557024d8cce25baeeb4a23359` by adding the standard Arbitrum
address-alias offset. It is a required cross-chain governance path and must not
be removed as though it were an EOA.

The 1-of-2 Safe owners are `0x9D4130...76DA` and `0xa1698F...8476`; either key
can act through that Safe. Operations explicitly accepted this single-key test
environment policy. The 2-of-4 Safe owners are `0x57F93d...Fe17`,
`0x09ad97...e2ce`, `0x326760...3535`, and `0x1dccff...3a84`.

Both retained Safe paths have successful on-chain evidence:

```text
Safe 0x573a...1017 -> UpgradeExecutor
  block:    134790733
  tx:       0xa30a6fe274832b2a11e886f641339dc0013d5af596376537e6eac08b329d3f17
  receipt:  success

Safe 0xE5C8...e9Ec -> UpgradeExecutor -> ArbOwnerPublic.getAllChainOwners()
  block:       135381956
  tx:          0x55ff3b3b0bfab87e4a2cb64ec59190c4764bf3c78d206edba40a3bfcb63e4638
  Safe nonce:  5 -> 6
  receipt:     success
```

Test L3 governance cleanup evidence:

```text
Remove 0x9D41...76DA from ChainOwners
  block:       135382424
  tx:          0x11538db87d944c0ff8e514977b4c351143041fd491dfdae645ad63609c0dddc3
  Safe nonce:  6 -> 7

Remove 0xa169...8476 from ChainOwners
  block:       135382909
  tx:          0xd704fbb1cd5e5b493f7180e6c589d80666015278adb8fd160202f94e316a3211
  Safe nonce:  7 -> 8

Remove 0x573a...1017 from ChainOwners
  block:       135383109
  tx:          0x5c65c45e567f0d72c456f66839d73cef3a9a628d0ced44f18bdf23920b9fc4db
  Safe nonce:  8 -> 9

Revoke EXECUTOR_ROLE from 0x94A6...1af2
  block:       135383502
  tx:          0x98b6ed0f6740df96c9a8023ae78fbdc25bfa292dd11cb305dac786844a64a57e
  Safe nonce:  9 -> 10

Revoke EXECUTOR_ROLE from 0xa169...8476
  block:       135384502
  tx:          0x19267fdb6fcbc62427d71f2de93b42f4b259a129e84a3008169ee85577da947a
  Safe nonce:  10 -> 11

Final verification
  block:             135386906
  chain owners:      UpgradeExecutor only
  blacklist owners:  empty
  executors:         both approved Safes and the parent alias only
  result:            PASS
```

Removing the direct chain-owner and executor permissions of `0x9D41...76DA`
and `0xa169...8476` does not remove their indirect authority as owners of the
retained 1-of-2 Safe. Removing `0x573a...1017` from ChainOwners does not remove
its `EXECUTOR_ROLE`.

### Parent L2 inventory that operations must supply

The L3 RPC cannot authoritatively provide these parent-contract values. Fill
them from each signed chain deployment manifest and verify them on the parent
chain before proceeding:

| Variable | Development | Test | Production |
|---|---|---|---|
| `PARENT_CHAIN_RPC` | approved Arbitrum Sepolia RPC | approved Arbitrum Sepolia RPC | approved Arbitrum One RPC |
| `ROLLUP_ADDRESS` | `0xdDF009b4879EaFFa6a23782F9aA30F86cd2c64e6` | required | required |
| `INBOX_ADDRESS` | `0xce247621afFC14FaE7102a9E3024C7fF3694052A` | required | required |
| `SEQUENCER_INBOX_ADDRESS` | `0x2c331Ee06dcD1784eF21a371747feE5eB8035c2c` | required | required |
| `BRIDGE_ADDRESS` | `0x8E0031B770867771De9bA9259AC6E8666dffF61c` | required | required |
| `PROXY_ADMIN_ADDRESS` | `0xc3c88D09459BC57426849196146799311D9654BA` | required | required |
| `PARENT_UPGRADE_EXECUTOR_ADDRESS` | `0x1b46Af3D21A13fd30D2BD396B308A6313aD22f1D` | `0x678815F2c63466f557024D8cCe25BaeeB4A23359` | `0x1333480e92de9511dc9BB01F70901ff3ee94f613` |
| Parent signer/Safe | Safe `0x4Ec94DD57A65C3E1C59929885a3d3612941B75c2`, 1-of-1 | Safe `0x663C00bA160ff059223f9f56bf80b1aE89DAceDe`, 1-of-2 | Safe `0xFbB37c66372f7B40361fBC8C8A235ae92711399D`, 3-of-4 |
| Deployment manifest path/hash | required | required | required |

Stop if any required manifest value is unknown. Do not copy a Rollup,
UpgradeExecutor, or Safe address between environments. The parent Safes and
UpgradeExecutors above are verified values, but they do not replace the signed
deployment manifest for the Rollup, Inbox, Bridge, and ProxyAdmin.

### Proposer-only Safe Transaction Service workflow

Production release operators have proposal permission, not the Safe owner
threshold or on-chain execution authority. Register the operator EOA as a
delegate in the exact Safe Transaction Service used by the approved Safe UI.
A registered delegate can submit a trusted proposal for owners to inspect and
sign; the delegate signature does not count as an owner confirmation.

Use the helper in [`scripts/safe-proposals`](../scripts/safe-proposals/README.md)
instead of copying a raw private key into a Forge script. It uses the official
Safe Protocol Kit and API Kit, builds batches with MultiSendCallOnly, and has
separate prepare and submit phases. Safe documents the corresponding
[delegate model](https://docs.safe.global/core-api/transaction-service-guides/delegates),
[proposal API](https://docs.safe.global/reference-sdk-api-kit/proposetransaction),
and [CALL-only batch creation](https://docs.safe.global/reference-sdk-protocol-kit/transactions/createtransaction).

The Safe web URL and Transaction Service API URL are different configuration
values. Do not assume that the API is the Safe UI URL with `/api` appended. For
the production L3, open `https://safe.deriw.com`, obtain its approved
Transaction Service URL from the deployed frontend/operations configuration,
and independently verify the chain ID is `2886` before setting
`SAFE_TX_SERVICE_URL`. For the Arbitrum One parent Safe, use the approved Safe
service with its API key or the organization's approved custom service.

Install and test once from the repository root:

```bash
cd scripts/safe-proposals
nvm use 20.12.2
npm ci
npm test
```

For each proposal wave, write a reviewed manifest as described in the helper
README, then prepare without posting it:

The manifest must set `upgradeExecutorAddress`, and every child transaction
must use that exact address as its outer `to`. The governed contract or
precompile is allowed only as the target encoded inside `execute` or
`executeCall` calldata. The helper rejects direct governance targets and pins
the reviewed UpgradeExecutor addresses for Deriw L3 chain IDs `18417507517`,
`2885`, and `2886`. This is a release-tool policy; it does not change ArbOS or
on-chain authorization.

```bash
export SAFE_RPC_URL="<RPC for the Safe's chain>"
export SAFE_TX_SERVICE_URL="<approved Transaction Service API URL>"

node safe-proposal.mjs prepare \
  --manifest proposal.json \
  --out proposal.prepared.json
```

For the official Arbitrum One service, omit `SAFE_TX_SERVICE_URL` and set
`SAFE_API_KEY` instead. The helper verifies the RPC chain ID, Safe bytecode,
owners, threshold, on-chain and service nonces, and the proposer's owner or
delegate role. It refuses outstanding predecessor proposals by default.

After a second reviewer decodes every child call and compares the prepared
Safe transaction hash, the registered proposer signs only that raw hash with a
keystore, hardware wallet, or managed signer:

```bash
cast wallet sign \
  --no-hash \
  --account <proposal-keystore-account> \
  <safeTxHash> > proposer.sig
```

Post the proposal to the service, without executing it:

```bash
node safe-proposal.mjs submit \
  --prepared proposal.prepared.json \
  --signature-file proposer.sig \
  --confirm-safe <exact-Safe-address>
```

For a production L3 proposal, open `https://safe.deriw.com` immediately after
submission and require the displayed Safe, chain ID, nonce, Safe transaction
hash, target, value, and decoded calldata to match the prepared file. Safe
owners then collect the live threshold and execute through the UI. Record the
proposal hash before requesting signatures and the on-chain hash after
execution. Send the UI URL, Safe address, Safe transaction hash, change-ticket
ID, and signing deadline through the approved signer channel; posting to the
Transaction Service does not by itself guarantee signer notification.

Do not put the complete deployment into one atomic batch. Prepare and submit
the next wave only after the preceding postcondition is verified:

| Wave | Chain and Safe | Proposal | Required gate before the next wave |
|---|---|---|---|
| P1 | Arbitrum One Safe `0xFbB37c...399D` | Required Nitro contract upgrade | Upstream verifier and version checker pass |
| P2 | Arbitrum One Safe `0xFbB37c...399D` | Rollup WASM module-root update | New root is official and validators remain healthy |
| L1 | Production L3 Safe `0x2F996b...c4dF` | Schedule internal ArbOS 60 | Activation occurs and `arbOSVersion()` returns `115` |
| L2 | Production L3 Safe `0x2F996b...c4dF` | Set max transaction gas to 60M | Tx limit is 60M and block limit remains 300M |
| L3 | Production L3 Safe `0x2F996b...c4dF` | Stage the reviewed router configuration | Scheduled and active routes match after the delay |
| L4 | Production L3 Safe `0x2F996b...c4dF` | Schedule DeriwOS 1 | Blacklist behavior passes after activation |
| L5 | Production L3 Safe `0x2F996b...c4dF` | Schedule DeriwOS 2 | Router-only positive and negative tests pass |
| L6 | Production L3 Safe `0x2F996b...c4dF` | Schedule DeriwOS 3 | Direct ETH works; raw messages and ERC-20 restrictions still pass |

P1 and P2 remain separate so the parent-contract upgrade can be verified
before changing the official machine root. L1 and L2 must never share a batch:
before ArbOS 50, `setMaxTxGasLimit` changes the legacy block gas limit rather
than the per-transaction limit. Parent and L3 transactions also cannot be in
one Safe batch because they use different chains and Safes. Only independent
calls on the same chain, governed by the same Safe, and executable at the same
already-verified gate may be included in one manifest; multi-call manifests
require `batchSafetyAcknowledgement: true`.

## 4. L3 precompile address reference

```text
ArbSys                 0x0000000000000000000000000000000000000064
ArbGasInfo             0x000000000000000000000000000000000000006c
ArbOwnerPublic         0x000000000000000000000000000000000000006b
ArbOwner               0x0000000000000000000000000000000000000070
DeriwGaslessPublic     0x00000000000000000000000000000000000007E7
DeriwGasless           0x00000000000000000000000000000000000007E8
DeriwSubAccountPublic  0x00000000000000000000000000000000000007E9
DeriwSubAccount        0x00000000000000000000000000000000000007EA
DeriwBlacklistPublic   0x00000000000000000000000000000000000007EB
DeriwBlacklist         0x00000000000000000000000000000000000007EC
```

## 5. Source checkout and release gate

Run from the Nitro repository root:

```bash
git fetch origin
git switch fix/blacklist-subaccount
git pull --ff-only origin fix/blacklist-subaccount
git submodule sync --recursive
git submodule update --init --recursive
git status --short
git submodule status --recursive
./scripts/check-submodules.sh --strict
git diff --check
```

For the reviewed package, the important revisions are:

```text
Nitro root              73189f61f9bbd3b7f7397d6f9c2625f69fd49e04
go-ethereum             52064791abfa175fb23229c20c11dd84fba894cf
precompile interfaces   f87337228eac115d92ff35ce37474b3e644b72d7
nitro-testnode          a8f5f3c33c11e7bc5dc0b8328acceeb03e847225
```

Production must use the later approved fix commit, not these hashes, unless the
open review findings have been formally accepted.

Run the feature tests before building:

```bash
go test -count=1 \
  ./arbos/deriwpolicy \
  ./arbos/arbosState \
  ./precompiles \
  ./gethhook \
  ./execution/gethexec \
  ./execution/nodeinterface

cd go-ethereum
go test -count=1 ./internal/ethapi ./core
cd ..
```

Record the root commit, recursive gitlinks, test output, builder identity,
image digest, and artifact checksums in the release ticket.

## 6. Decide whether a WASM update is required

| Change type | Parent Rollup WASM update |
|---|---|
| RPC formatting or `eth_estimateGas` logic only | Usually no |
| Node flags, metrics, logging, or non-consensus admission only | No |
| ArbOS state transition, precompile execution, transaction result, or state root | Yes |
| New DeriwOS consensus version | Yes |

This combined release changes consensus blacklist execution and
`ArbSys.SendTxToL1`; therefore it requires a newly built proving machine and a
parent-L2 Rollup WASM module-root update.

## 7. Build and verify the WASM machine

Build on the same Linux architecture used for the release validator. The
repository target that calculates and embeds `target/machines/latest` is
`nitro-node-dev`:

```bash
export RELEASE_TAG="deriw-router-blacklist-<approved-version>"
export CANDIDATE_IMAGE="registry.example/deriw/nitro-node-dev:${RELEASE_TAG}"
export RUNTIME_IMAGE="registry.example/deriw/nitro-node:<approved-version>"

docker buildx build \
  --platform linux/amd64 \
  --target nitro-node-dev \
  --tag "$CANDIDATE_IMAGE" \
  --load \
  .
```

Read and independently verify the root:

```bash
docker run --rm \
  --entrypoint cat \
  "$CANDIDATE_IMAGE" \
  /home/user/target/machines/latest/module-root.txt

docker run --rm \
  --entrypoint /usr/local/bin/prover \
  "$CANDIDATE_IMAGE" \
  /home/user/target/machines/latest/machine.v2.wavm.br \
  --print-wasmmoduleroot

docker run --rm \
  --entrypoint /home/user/validate-wasm-module-root.sh \
  "$CANDIDATE_IMAGE" \
  /home/user/target/machines \
  /usr/local/bin/prover \
  && echo "WASM module-root validation: PASS"
```

The first two commands print a module root and those values must agree. The
current validation script prints every verified machine/root pair, fails if no
machine directory exists, and exits nonzero on a missing artifact or mismatch.
Older candidate images may contain the earlier script, which is silent on
success; the trailing shell message makes that success explicit. Therefore the
third command must end with `WASM module-root validation: PASS`. Set the exact
root printed by the first two commands as `NEW_WASM_MODULE_ROOT`.

The same artifacts can be built locally when the full toolchain is available:

```bash
make build-replay-env
cat target/machines/latest/module-root.txt
./scripts/validate-wasm-module-root.sh target/machines target/bin/prover \
  && echo "WASM module-root validation: PASS"
```

### Runtime-image packaging warning

In the current Dockerfile, `nitro-node-dev` copies the newly calculated
`latest` machine into the image; the normal `nitro-node` target does not do so
automatically. Do not publish a production runtime image until one of these is
true:

- the approved machine artifacts are included in the production/validator
  image by the release pipeline; or
- the immutable machine directory is mounted and included in
  `--validation.wasm.allowed-wasm-module-roots`.

Verify the final runtime image, not only the build image:

```bash
docker run --rm --entrypoint find "$RUNTIME_IMAGE" \
  /home/user/target/machines -name module-root.txt -print -exec cat {} \;
```

The runtime image must contain the current on-chain root and the proposed root.
Do not deploy `nitro-node-dev` to production merely because it contains the new
machine; package the artifact into the approved hardened runtime image.

## 8. Pre-deployment live-state capture

Set environment variables without putting private keys into shell history:

```bash
export L3_RPC="<environment L3 RPC>"
export PARENT_CHAIN_RPC="<environment parent L2 RPC>"
export ROLLUP_ADDRESS="<verified environment Rollup>"
export INBOX_ADDRESS="<verified environment Inbox>"
export SEQUENCER_INBOX_ADDRESS="<verified environment SequencerInbox>"
export BRIDGE_ADDRESS="<verified environment Bridge>"
export PROXY_ADMIN_ADDRESS="<verified environment Rollup ProxyAdmin>"
export PARENT_UPGRADE_EXECUTOR_ADDRESS="<verified parent UpgradeExecutor>"
export L3_UPGRADE_EXECUTOR="<environment value from section 3>"
export L3_GOVERNANCE_SIGNER="<approved L3 Safe address>"
export PARENT_GOVERNANCE_SENDER="<parent Safe holding EXECUTOR_ROLE>"
export PARENT_SAFE_ADDRESS="<environment parent Safe>"
export PARENT_SAFE_OWNER="<development/test owner EOA submitting the Safe transaction>"
export PARENT_SAFE_OWNER_ACCOUNT="<Foundry keystore account for that Safe owner>"
export PARENT_DEPLOYER="<approved EOA used only if a new action must be deployed>"
export PARENT_DEPLOYER_ACCOUNT_NAME="<Foundry keystore account for the optional action deployer>"
```

For development, set the governance values exactly as follows. The parent and
L3 Safes are separate contracts on separate chains:

```bash
export L3_RPC="https://rpc.dev.deriw.com"
export PARENT_CHAIN_RPC="<approved Arbitrum Sepolia RPC>"
export ROLLUP_ADDRESS="0xdDF009b4879EaFFa6a23782F9aA30F86cd2c64e6"
export INBOX_ADDRESS="0xce247621afFC14FaE7102a9E3024C7fF3694052A"
export SEQUENCER_INBOX_ADDRESS="0x2c331Ee06dcD1784eF21a371747feE5eB8035c2c"
export BRIDGE_ADDRESS="0x8E0031B770867771De9bA9259AC6E8666dffF61c"
export PROXY_ADMIN_ADDRESS="0xc3c88D09459BC57426849196146799311D9654BA"
export PARENT_UPGRADE_EXECUTOR_ADDRESS="0x1b46Af3D21A13fd30D2BD396B308A6313aD22f1D"
export PARENT_GOVERNANCE_SENDER="0x4Ec94DD57A65C3E1C59929885a3d3612941B75c2"
export PARENT_SAFE_ADDRESS="0x4Ec94DD57A65C3E1C59929885a3d3612941B75c2"
export PARENT_SAFE_OWNER="0x94A6713cbF5F589aB51570D0b4cd219792421af2"
export L3_UPGRADE_EXECUTOR="0xB5B4d7f7a32D86fF3bc270B864c7c06CE6F0BD78"
export L3_SAFE_A="0x5f1B197A82fC1148A02Ea55B3BEF529f78D64151"
export L3_SAFE_B="0x9Caa16915f33F5A122351d828B07F8758a53bdEa"
```

For test, use these governance values and fill the parent Rollup-contract
addresses from the signed test deployment manifest:

```bash
export L3_RPC="https://rpc.test.deriw.com"
export PARENT_CHAIN_RPC="<approved Arbitrum Sepolia RPC>"
export PARENT_UPGRADE_EXECUTOR_ADDRESS="0x678815F2c63466f557024D8cCe25BaeeB4A23359"
export PARENT_GOVERNANCE_SENDER="0x663C00bA160ff059223f9f56bf80b1aE89DAceDe"
export PARENT_SAFE_ADDRESS="0x663C00bA160ff059223f9f56bf80b1aE89DAceDe"
export PARENT_SAFE_OWNER="<0xa1698F...8476 or 0x35b3ac...bcB5>"
export L3_UPGRADE_EXECUTOR="0xAc3516eF1E3658887198D192Cb0D7EcA07604943"
export L3_SAFE_A="0xE5C8e6dAbE8dA8D90F0AE3d4543E930833A0e9Ec"
export L3_SAFE_B="0x573a3d28B82627a4dB4C48686ee986FdDedc1017"
```

For production, use these governance values and fill the parent Rollup-contract
addresses from the signed production deployment manifest:

```bash
export L3_RPC="https://rpc.deriw.com"
export PARENT_CHAIN_RPC="<approved Arbitrum One RPC>"
export PARENT_UPGRADE_EXECUTOR_ADDRESS="0x1333480e92de9511dc9BB01F70901ff3ee94f613"
export PARENT_GOVERNANCE_SENDER="0xFbB37c66372f7B40361fBC8C8A235ae92711399D"
export PARENT_SAFE_ADDRESS="0xFbB37c66372f7B40361fBC8C8A235ae92711399D"
export L3_UPGRADE_EXECUTOR="0xC49f79CcdFbB3668400b7476A641268De81548b1"
export L3_SAFE_A="0x2F996bC558818D33DE37aF36Bee7de24bA3Fc4dF"
```

Capture the parent state:

```bash
cast chain-id --rpc-url "$PARENT_CHAIN_RPC"
cast code --rpc-url "$PARENT_CHAIN_RPC" "$ROLLUP_ADDRESS"
cast code --rpc-url "$PARENT_CHAIN_RPC" "$INBOX_ADDRESS"
cast code --rpc-url "$PARENT_CHAIN_RPC" "$SEQUENCER_INBOX_ADDRESS"
cast code --rpc-url "$PARENT_CHAIN_RPC" "$BRIDGE_ADDRESS"
cast code --rpc-url "$PARENT_CHAIN_RPC" "$PROXY_ADMIN_ADDRESS"
cast call --rpc-url "$PARENT_CHAIN_RPC" "$ROLLUP_ADDRESS" "owner()(address)"
cast call --rpc-url "$PARENT_CHAIN_RPC" "$ROLLUP_ADDRESS" "wasmModuleRoot()(bytes32)"
cast call --rpc-url "$PARENT_CHAIN_RPC" "$INBOX_ADDRESS" "maxDataSize()(uint256)"
cast call --rpc-url "$PARENT_CHAIN_RPC" "$INBOX_ADDRESS" "bridge()(address)"
cast call --rpc-url "$PARENT_CHAIN_RPC" "$INBOX_ADDRESS" "sequencerInbox()(address)"
cast call --rpc-url "$PARENT_CHAIN_RPC" "$PROXY_ADMIN_ADDRESS" "owner()(address)"
cast call --rpc-url "$PARENT_CHAIN_RPC" "$PARENT_UPGRADE_EXECUTOR_ADDRESS" "EXECUTOR_ROLE()(bytes32)"
```

Confirm the Rollup owner equals `PARENT_UPGRADE_EXECUTOR_ADDRESS`. Verify from
the signed deployment manifest that the same UpgradeExecutor owns the Rollup
ProxyAdmin. Then confirm the selected Safe has `EXECUTOR_ROLE`:

```bash
export PARENT_EXECUTOR_ROLE="$(cast call \
  --rpc-url "$PARENT_CHAIN_RPC" \
  "$PARENT_UPGRADE_EXECUTOR_ADDRESS" \
  "EXECUTOR_ROLE()(bytes32)")"

cast call \
  --rpc-url "$PARENT_CHAIN_RPC" \
  "$PARENT_UPGRADE_EXECUTOR_ADDRESS" \
  "hasRole(bytes32,address)(bool)" \
  "$PARENT_EXECUTOR_ROLE" \
  "$PARENT_GOVERNANCE_SENDER"
```

For every environment, also verify the Safe itself. For development and test,
prove that each owner EOA lacks a direct execution path. Safe 1.4.1 has no
public `getGuard()` function, so read the guard storage slot directly:

```bash
export SAFE_SENTINEL="0x0000000000000000000000000000000000000001"
export SAFE_GUARD_STORAGE_SLOT="0x4a204f620c8c5ccdca3fd54d003badd85ba500436a431f0cbda4f558c93c34c8"

cast code --rpc-url "$PARENT_CHAIN_RPC" "$PARENT_SAFE_ADDRESS"
cast call --rpc-url "$PARENT_CHAIN_RPC" "$PARENT_SAFE_ADDRESS" \
  "VERSION()(string)"
cast call --rpc-url "$PARENT_CHAIN_RPC" "$PARENT_SAFE_ADDRESS" \
  "getOwners()(address[])"
cast call --rpc-url "$PARENT_CHAIN_RPC" "$PARENT_SAFE_ADDRESS" \
  "getThreshold()(uint256)"
cast call --rpc-url "$PARENT_CHAIN_RPC" "$PARENT_SAFE_ADDRESS" \
  "getModulesPaginated(address,uint256)(address[],address)" \
  "$SAFE_SENTINEL" 100
cast storage --rpc-url "$PARENT_CHAIN_RPC" \
  "$PARENT_SAFE_ADDRESS" "$SAFE_GUARD_STORAGE_SLOT"

cast call --rpc-url "$PARENT_CHAIN_RPC" \
  "$PARENT_UPGRADE_EXECUTOR_ADDRESS" \
  "hasRole(bytes32,address)(bool)" \
  "$PARENT_EXECUTOR_ROLE" \
  "$PARENT_SAFE_ADDRESS"

if [ -n "${PARENT_SAFE_OWNER:-}" ]; then
  cast call --rpc-url "$PARENT_CHAIN_RPC" \
    "$PARENT_UPGRADE_EXECUTOR_ADDRESS" \
    "hasRole(bytes32,address)(bool)" \
    "$PARENT_EXECUTOR_ROLE" \
    "$PARENT_SAFE_OWNER"
fi

cast call --rpc-url "$PARENT_CHAIN_RPC" \
  "$PARENT_SAFE_ADDRESS" "nonce()(uint256)"
```

For a multi-owner Safe, repeat the owner-role query for every address returned
by `getOwners()`; each result must be `false`.

Expected parent results are:

| Environment | Safe version | Owners/threshold | Active `EXECUTOR_ROLE` member |
|---|---|---|---|
| Development | 1.4.1 | owner `0x94A671...1af2`, 1-of-1 | Safe `0x4Ec94D...75c2` only |
| Test | 1.4.1 | owners `0xa1698F...8476`, `0x35b3ac...bcB5`, 1-of-2 | Safe `0x663C00...ceDe` only |
| Production | 1.4.1 | four owners, 3-of-4 | Safe `0xFbB37c...399D` only |

Checking only the selected Safe and one EOA does not prove that no third
executor exists. Enumerate every `RoleGranted` and `RoleRevoked` event from the
UpgradeExecutor deployment block and run `hasRole` on every discovered member.

Capture the L3 state:

```bash
cast chain-id --rpc-url "$L3_RPC"
cast block-number --rpc-url "$L3_RPC"

cast call --rpc-url "$L3_RPC" \
  0x0000000000000000000000000000000000000064 \
  "arbOSVersion()(uint256)"

cast call --rpc-url "$L3_RPC" \
  0x000000000000000000000000000000000000006b \
  "getAllChainOwners()(address[])"

cast call --rpc-url "$L3_RPC" \
  "$L3_UPGRADE_EXECUTOR" \
  "EXECUTOR_ROLE()(bytes32)"
```

Confirm the L3 governance signer has `EXECUTOR_ROLE`:

```bash
export L3_EXECUTOR_ROLE="$(cast call \
  --rpc-url "$L3_RPC" \
  "$L3_UPGRADE_EXECUTOR" \
  "EXECUTOR_ROLE()(bytes32)")"

cast call \
  --rpc-url "$L3_RPC" \
  "$L3_UPGRADE_EXECUTOR" \
  "hasRole(bytes32,address)(bool)" \
  "$L3_EXECUTOR_ROLE" \
  "$L3_GOVERNANCE_SIGNER"
```

### 8.1 Submit an L3 governance write

Every later L3 write produces step-specific outer calldata that encodes
`UpgradeExecutor.executeCall(target, innerCalldata)`. Call that value
`L3_EXECUTOR_CALLDATA`. Submit it as a Safe transaction with exactly:

```text
Safe:       L3_GOVERNANCE_SIGNER
to:         L3_UPGRADE_EXECUTOR
value:      0
operation:  CALL (0), never DELEGATECALL
data:       L3_EXECUTOR_CALLDATA
```

Development may select either approved development Safe. Test may select
`0xE5C8...e9Ec` (1-of-2) or `0x573a...1017` (2-of-4). Production must use
`0x2F996b...c4dF` (3-of-4). Use the environment Safe UI or the proposal helper
for a normal multisignature flow.

When and only when the selected Safe's live threshold is `1`, a Safe owner may
submit the already reviewed transaction directly with `cast`. The EOA sends to
the Safe, not to the UpgradeExecutor:

```bash
export L3_CHAIN_ID="$(cast chain-id --rpc-url "$L3_RPC")"
export L3_SAFE_ADDRESS="$L3_GOVERNANCE_SIGNER"
export L3_SAFE_OWNER="<full address of the selected Safe owner>"
export L3_SAFE_OWNER_ACCOUNT="<encrypted Foundry keystore account>"
export ZERO_ADDRESS="0x0000000000000000000000000000000000000000"
export OWNER_R="$(cast abi-encode "f(address)" "$L3_SAFE_OWNER")"
export OWNER_APPROVED_SIGNATURE="$(cast concat-hex \
  "$OWNER_R" \
  "$(cast hash-zero)" \
  0x01)"

# Read-only simulation. It must return true.
cast call \
  --rpc-url "$L3_RPC" \
  --from "$L3_SAFE_OWNER" \
  "$L3_SAFE_ADDRESS" \
  "execTransaction(address,uint256,bytes,uint8,uint256,uint256,uint256,address,address,bytes)(bool)" \
  "$L3_UPGRADE_EXECUTOR" \
  0 \
  "$L3_EXECUTOR_CALLDATA" \
  0 \
  0 \
  0 \
  0 \
  "$ZERO_ADDRESS" \
  "$ZERO_ADDRESS" \
  "$OWNER_APPROVED_SIGNATURE"

export L3_SAFE_NONCE_BEFORE="$(cast call \
  --rpc-url "$L3_RPC" \
  "$L3_SAFE_ADDRESS" \
  "nonce()(uint256)")"

cast send \
  --rpc-url "$L3_RPC" \
  --chain "$L3_CHAIN_ID" \
  --account "$L3_SAFE_OWNER_ACCOUNT" \
  --confirmations 2 \
  "$L3_SAFE_ADDRESS" \
  "execTransaction(address,uint256,bytes,uint8,uint256,uint256,uint256,address,address,bytes)" \
  "$L3_UPGRADE_EXECUTOR" \
  0 \
  "$L3_EXECUTOR_CALLDATA" \
  0 \
  0 \
  0 \
  0 \
  "$ZERO_ADDRESS" \
  "$ZERO_ADDRESS" \
  "$OWNER_APPROVED_SIGNATURE"
```

Require receipt status `1`, Safe `ExecutionSuccess`, the expected
UpgradeExecutor event, and a Safe nonce increase of exactly one. Do not use
this threshold-1 shortcut for test Safe `0x573a...1017` or any production
Safe. Never replace `L3_SAFE_ADDRESS` with `L3_UPGRADE_EXECUTOR` in the
`cast send` command.

After compatible nodes are deployed, also capture:

```bash
cast call --rpc-url "$L3_RPC" \
  0x00000000000000000000000000000000000007EB \
  "getDeriwOSVersion()(uint64,uint64)"

cast call --rpc-url "$L3_RPC" \
  0x00000000000000000000000000000000000007EB \
  "getScheduledDeriwOSUpgrade()(uint64,uint64,uint64)"

cast call --rpc-url "$L3_RPC" \
  0x000000000000000000000000000000000000006b \
  "getDeriwRouterConfig()(address,address,address[],uint64)"

cast call --rpc-url "$L3_RPC" \
  0x000000000000000000000000000000000000006b \
  "getScheduledDeriwRouterConfig()(address,address,address[],uint64,uint64)"
```

Before compatible nodes are live, the last four new calls may revert. Do not
submit a transaction to a new precompile method while any consensus participant
still runs old code.

Take database snapshots and record the existing blacklist, gasless/pricer,
subaccount, chain-owner, fee-account, and route state before rollout.

## 9. Upgrade parent Nitro contracts when required

The L3 also depends on Nitro bridge and Rollup contracts deployed on its parent
L2. Check them separately from the L3 node/ArbOS version. The `2.1.3` action
described here patches only the parent `Inbox`/`ERC20Inbox` and
`SequencerInbox` for EIP-7702 callers. It does not install the Deriw node code,
change the WASM module root, activate ArbOS/DeriwOS, or set the L3 transaction
gas limit.

Do not run `2.1.3` just because production is old. First run the version
checker. The upstream action only supports its documented source-version
ranges and is neither required nor recommended when the chain will move to
Nitro contracts `3.1.0` or later before its parent enables EIP-7702. Follow
exactly the upgrade path printed by the checker and approved in the release
ticket.

### 9.1 Prepare the chain-actions toolchain

Use the official Offchain Labs repository and pin a reviewed commit. The
following commit was inspected for this runbook; replace it only with another
independently reviewed immutable revision:

```bash
git clone https://github.com/OffchainLabs/arbitrum-chain-actions.git
cd arbitrum-chain-actions
git checkout 5ff87aedf3ad581eeecaa2e4c9220248d8e2c263

nvm install 20.12.2
nvm use 20.12.2
yarn install --frozen-lockfile
yarn build
```

Node `20.12.2` is the Deriw deployment-tool version, not an upstream
repository requirement. Record `node --version`, `yarn --version`, the
chain-actions commit, and `yarn.lock` hash.

If Foundry is not installed on the isolated deployment workstation:

```bash
curl -L https://foundry.paradigm.xyz | bash
source ~/.zshenv
foundryup
forge --version
cast --version
```

For test and production, install the previously reviewed Foundry version
rather than silently accepting a new `foundryup` release on deployment day.

### 9.2 Check the deployed contract versions

From the chain-actions repository root:

```bash
INBOX_ADDRESS="$INBOX_ADDRESS" \
PARENT_CHAIN_RPC="$PARENT_CHAIN_RPC" \
yarn chain:contracts:version
```

Save the complete output. Stop when it reports an unsupported deployment,
requires an intermediate action such as `2.1.2`, or recommends a different
upgrade family. Never copy the development/test result into production.

For a `2.1.3` upgrade, Offchain Labs publishes these action contracts:

| Environment | Parent | Published `UPGRADE_ACTION_ADDRESS` |
|---|---|---|
| Development | Arbitrum Sepolia | `0x0E0Ee28333798F9aF0E76653beabC72F7477C287` |
| Test | Arbitrum Sepolia | `0x0E0Ee28333798F9aF0E76653beabC72F7477C287` |
| Production | Arbitrum One | `0xA350fE71079Aa86d48a8f2fDc600bbc6fa9CFE70` |

Treat these as candidates, not blindly trusted constants: verify the parent
chain ID, deployed bytecode, published source, and template getter values
before execution. The published Arbitrum Sepolia and Arbitrum One actions
inspected on 2026-08-18 both embed `MAX_DATA_SIZE=104857`.

Minimum action checks:

```bash
cast code --rpc-url "$PARENT_CHAIN_RPC" "$UPGRADE_ACTION_ADDRESS"
cast codehash --rpc-url "$PARENT_CHAIN_RPC" "$UPGRADE_ACTION_ADDRESS"

cast call --rpc-url "$PARENT_CHAIN_RPC" \
  "$UPGRADE_ACTION_ADDRESS" "newEthInboxImpl()(address)"
cast call --rpc-url "$PARENT_CHAIN_RPC" \
  "$UPGRADE_ACTION_ADDRESS" "newERC20InboxImpl()(address)"
cast call --rpc-url "$PARENT_CHAIN_RPC" \
  "$UPGRADE_ACTION_ADDRESS" "newEthSequencerInboxImpl()(address)"
cast call --rpc-url "$PARENT_CHAIN_RPC" \
  "$UPGRADE_ACTION_ADDRESS" "newERC20SequencerInboxImpl()(address)"
```

Read `maxDataSize()(uint256)` and the code hash on every returned template
address relevant to the chain. Compare all outputs with the reviewed
chain-actions source and release ticket.

### 9.3 Create the root `.env` file

`.env` must be a file in the chain-actions repository root. Do not run
`mkdir .env`, and do not place it inside the `2.1.3` script directory.

```bash
cp scripts/foundry/contract-upgrades/2.1.3/.env.sample .env
chmod 600 .env
```

Set these values for exactly one environment:

```dotenv
PARENT_CHAIN_RPC=<approved Arbitrum Sepolia or Arbitrum One RPC>
PARENT_CHAIN_IS_ARBITRUM=true
MAX_DATA_SIZE=104857
UPGRADE_ACTION_ADDRESS=<verified action for this parent chain>
INBOX_ADDRESS=<this environment's Inbox proxy>
PROXY_ADMIN_ADDRESS=<this environment's Rollup ProxyAdmin>
PARENT_UPGRADE_EXECUTOR_ADDRESS=<this environment's parent UpgradeExecutor>
```

`MAX_DATA_SIZE` is an Inbox/SequencerInbox constructor parameter, not a gas
limit. Before proceeding, `Inbox.maxDataSize()` must equal the configured
value, and the execute script must confirm it matches the selected action.
Do not change the variable merely to make a failing simulation pass.

Do not store any governance private key in `.env`. A raw `ETH_PRIVATE_KEY` on a
command line or in shell history is also prohibited. Use an encrypted Foundry
keystore, hardware/managed signer, or the environment-specific Safe flow below.
The current `2.1.3` execute script does not read an `EXECUTOR` environment
variable. In all environments, set `FOUNDRY_SENDER` to the parent Safe for the
read-only simulation. An owner keystore is used only to submit
`Safe.execTransaction` in the development/test cast flow; it must never be used
to broadcast the Forge governance script or to call the UpgradeExecutor
directly.

### 9.4 Simulate before requesting signatures

The recent chain-actions CLI separates deploy, execute, and verify. Because
the published action can normally be reused, `deploy` should be skipped after
its code and parameters are verified. Simulate execution without
`FOUNDRY_BROADCAST=true`:

```bash
FOUNDRY_SENDER="$PARENT_GOVERNANCE_SENDER" \
yarn cli -- contract-upgrades/2.1.3/execute
```

The simulation must show the verified parent UpgradeExecutor delegating to the
verified action, which upgrades only this environment's Inbox and
SequencerInbox through the verified ProxyAdmin. Stop on any ownership, role,
address, `MAX_DATA_SIZE`, or prerequisite mismatch.

Only when the published action cannot be used and a new deployment has been
separately approved, deploy it with the deployment signer and record the last
created contract as `UPGRADE_ACTION_ADDRESS`:

```bash
forge script \
  scripts/foundry/contract-upgrades/2.1.3/DeployNitroContracts2Point1Point3UpgradeAction.s.sol:DeployNitroContracts2Point1Point3UpgradeActionScript \
  --rpc-url "$PARENT_CHAIN_RPC" \
  --sender "$PARENT_DEPLOYER" \
  --account "$PARENT_DEPLOYER_ACCOUNT_NAME" \
  --broadcast \
  --slow \
  -vvv
```

Deploying an approved action contract is not a governance execution and does
not grant the deployer any authority over the Rollup or UpgradeExecutor.

### 9.5 Do not broadcast the Forge execution script

There is no direct-EOA execution path in development, test, or production. The
owner EOAs do not hold parent `EXECUTOR_ROLE`. Use the Forge execute command
only for the Safe-sender simulation in section 9.4, extract and review the
nested calldata, and submit that calldata through the parent Safe. A command
that combines the execution script with `--broadcast`, `--private-key`, or an
owner `--account` is prohibited.

Development and test use distinct deployment manifests and must not share an
Inbox, ProxyAdmin, action, or executor address merely because they share
Arbitrum Sepolia.

### 9.6 Build parent-Safe contract-upgrade calldata

For every environment, run the simulation in section 9.4 with
`PARENT_GOVERNANCE_SENDER` set to that environment's parent Safe. Then create
and independently compare the exact nested calldata:

```bash
export CONTRACT_UPGRADE_ACTION_CALLDATA="$(cast calldata \
  "perform(address,address)" \
  "$INBOX_ADDRESS" \
  "$PROXY_ADMIN_ADDRESS")"

export CONTRACT_UPGRADE_EXECUTOR_CALLDATA="$(cast calldata \
  "execute(address,bytes)" \
  "$UPGRADE_ACTION_ADDRESS" \
  "$CONTRACT_UPGRADE_ACTION_CALLDATA")"
```

The Safe transaction target is `PARENT_UPGRADE_EXECUTOR_ADDRESS`, its value is
`0`, its operation is `CALL`, and its data is
`CONTRACT_UPGRADE_EXECUTOR_CALLDATA`.

### 9.7 Development and test: execute through the parent Safe

The development and test parent Safes have no required hosted Transaction
Service. Submit the reviewed transaction to the Safe contract on Arbitrum
Sepolia from a Safe owner keystore. Development is 1-of-1 and test is 1-of-2,
so one owner approval currently satisfies each Safe. This owner-approved-hash
signature form is valid only because the caller is a Safe owner and the live
threshold is one; stop and use a normal multisignature Safe proposal flow if
the threshold changes.

```bash
export ZERO_ADDRESS="0x0000000000000000000000000000000000000000"
export OWNER_R="$(cast abi-encode "f(address)" "$PARENT_SAFE_OWNER")"
export OWNER_APPROVED_SIGNATURE="$(cast concat-hex \
  "$OWNER_R" \
  "$(cast hash-zero)" \
  0x01)"

cast call \
  --rpc-url "$PARENT_CHAIN_RPC" \
  --from "$PARENT_SAFE_OWNER" \
  "$PARENT_SAFE_ADDRESS" \
  "execTransaction(address,uint256,bytes,uint8,uint256,uint256,uint256,address,address,bytes)(bool)" \
  "$PARENT_UPGRADE_EXECUTOR_ADDRESS" \
  0 \
  "$CONTRACT_UPGRADE_EXECUTOR_CALLDATA" \
  0 \
  0 \
  0 \
  0 \
  "$ZERO_ADDRESS" \
  "$ZERO_ADDRESS" \
  "$OWNER_APPROVED_SIGNATURE"
```

The simulation must return `true`. Record the Safe nonce, then submit:

```bash
cast call --rpc-url "$PARENT_CHAIN_RPC" \
  "$PARENT_SAFE_ADDRESS" "nonce()(uint256)"

cast send \
  --rpc-url "$PARENT_CHAIN_RPC" \
  --chain 421614 \
  --account "$PARENT_SAFE_OWNER_ACCOUNT" \
  --confirmations 2 \
  "$PARENT_SAFE_ADDRESS" \
  "execTransaction(address,uint256,bytes,uint8,uint256,uint256,uint256,address,address,bytes)" \
  "$PARENT_UPGRADE_EXECUTOR_ADDRESS" \
  0 \
  "$CONTRACT_UPGRADE_EXECUTOR_CALLDATA" \
  0 \
  0 \
  0 \
  0 \
  "$ZERO_ADDRESS" \
  "$ZERO_ADDRESS" \
  "$OWNER_APPROVED_SIGNATURE"
```

Require receipt status `1`, a Safe `ExecutionSuccess` event, the expected
UpgradeExecutor event, and a Safe nonce increase of exactly one. Recheck that
the Safe has `EXECUTOR_ROLE` and every Safe owner lacks direct
`EXECUTOR_ROLE`.

### 9.8 Production: execute through the parent Safe

Prepare this as proposal wave P1 using the proposer-only workflow in section 3.
In the approved production parent Safe UI on Arbitrum One, select Safe
`0xFbB37c66372f7B40361fBC8C8A235ae92711399D`:

1. Create a custom transaction to `PARENT_UPGRADE_EXECUTOR_ADDRESS` with value
   `0` and data `CONTRACT_UPGRADE_EXECUTOR_CALLDATA`.
2. Confirm the decoder shows
   `UpgradeExecutor.execute(UPGRADE_ACTION_ADDRESS, perform(INBOX_ADDRESS,
   PROXY_ADMIN_ADDRESS))`.
3. Confirm the Safe has `EXECUTOR_ROLE` and the executor owns the ProxyAdmin.
4. Simulate, collect the live threshold, execute, and record both Safe and
   Arbitrum transaction hashes.

The proposer posts the transaction but does not approve or execute it. Collect
the live 3-of-4 owner threshold. Do not submit the Forge execute script with
`--private-key` or an EOA account for production.

### 9.9 Verify the parent upgrade

Run the upstream verifier and then the version checker again:

```bash
forge script \
  scripts/foundry/contract-upgrades/2.1.3/VerifyNitroContracts2Point1Point3Upgrade.s.sol:VerifyNitroContracts2Point1Point3Upgrade \
  --rpc-url "$PARENT_CHAIN_RPC" \
  -vvv

INBOX_ADDRESS="$INBOX_ADDRESS" \
PARENT_CHAIN_RPC="$PARENT_CHAIN_RPC" \
yarn chain:contracts:version
```

Record before/after implementation addresses and code hashes for the Inbox and
SequencerInbox. Continue to the WASM-root transaction only after the verifier,
version report, batch posting, delayed inbox, and parent bridge health checks
all pass.

## 10. Node rollout and pending validation

Use immutable image digests, never `latest`.

1. Deploy one passive RPC canary.
2. Compare its block hash, state root, receipts, and read APIs with the old RPC.
3. Roll passive RPC/full nodes.
4. Roll validators/stakers with both current and proposed machines available.
5. Configure pending validation of the proposed root.
6. Let both roots validate the same live L3 blocks and compare final global
   state.

Relevant validator configuration shape:

```text
--node.block-validator.enable=true
--node.block-validator.current-module-root=current
--node.block-validator.pending-upgrade-module-root=<NEW_WASM_MODULE_ROOT>
--validation.wasm.allowed-wasm-module-roots=<current-machine-dir>,<new-machine-dir>
```

Pending validation is a local check. It does not update the Rollup contract and
does not activate DeriwOS.

Stop if any validator reports a missing machine, different global state,
different module root, or replay divergence.

## 11. Update the WASM root on the parent L2

This is required for the combined release.

Prepare the inner Rollup calldata:

```bash
export ROOT_CALLDATA="$(cast calldata \
  "setWasmModuleRoot(bytes32)" \
  "$NEW_WASM_MODULE_ROOT")"

export ROOT_EXECUTOR_CALLDATA="$(cast calldata \
  "executeCall(address,bytes)" \
  "$ROLLUP_ADDRESS" \
  "$ROOT_CALLDATA")"
```

### Development and test parent L2: Safe submission

Use the same threshold-1 Safe procedure and checks described in section 9.7,
but set the Safe transaction data to `ROOT_EXECUTOR_CALLDATA`. Development uses
Safe `0x4Ec94D...75c2`; test uses Safe `0x663C00...ceDe`. Rebuild the
owner-approved signature for the selected Safe owner if it is not already
present in the current shell:

```bash
export ZERO_ADDRESS="0x0000000000000000000000000000000000000000"
export OWNER_R="$(cast abi-encode "f(address)" "$PARENT_SAFE_OWNER")"
export OWNER_APPROVED_SIGNATURE="$(cast concat-hex \
  "$OWNER_R" \
  "$(cast hash-zero)" \
  0x01)"

cast call \
  --rpc-url "$PARENT_CHAIN_RPC" \
  --from "$PARENT_SAFE_OWNER" \
  "$PARENT_SAFE_ADDRESS" \
  "execTransaction(address,uint256,bytes,uint8,uint256,uint256,uint256,address,address,bytes)(bool)" \
  "$PARENT_UPGRADE_EXECUTOR_ADDRESS" \
  0 \
  "$ROOT_EXECUTOR_CALLDATA" \
  0 \
  0 \
  0 \
  0 \
  "$ZERO_ADDRESS" \
  "$ZERO_ADDRESS" \
  "$OWNER_APPROVED_SIGNATURE"
```

The simulation must return `true`. Record the Safe nonce, submit from the owner
keystore, and require two confirmations:

```bash
cast call --rpc-url "$PARENT_CHAIN_RPC" \
  "$PARENT_SAFE_ADDRESS" "nonce()(uint256)"

cast send \
  --rpc-url "$PARENT_CHAIN_RPC" \
  --chain 421614 \
  --account "$PARENT_SAFE_OWNER_ACCOUNT" \
  --confirmations 2 \
  "$PARENT_SAFE_ADDRESS" \
  "execTransaction(address,uint256,bytes,uint8,uint256,uint256,uint256,address,address,bytes)" \
  "$PARENT_UPGRADE_EXECUTOR_ADDRESS" \
  0 \
  "$ROOT_EXECUTOR_CALLDATA" \
  0 \
  0 \
  0 \
  0 \
  "$ZERO_ADDRESS" \
  "$ZERO_ADDRESS" \
  "$OWNER_APPROVED_SIGNATURE"
```

Require receipt status `1`, Safe `ExecutionSuccess`, the expected
UpgradeExecutor event, and a Safe nonce increase of exactly one. The on-chain
path must be `Safe -> parent UpgradeExecutor.executeCall -> Rollup`; a direct
owner-EOA call to either the UpgradeExecutor or Rollup is prohibited.

### Production parent L2: Safe submission

Prepare this as proposal wave P2 only after P1 verification is complete. In
the approved production parent-chain Safe UI, select Safe
`0xFbB37c66372f7B40361fBC8C8A235ae92711399D`:

1. Select Arbitrum One and the verified parent governance Safe.
2. Create a custom transaction.
3. Set `to` to `PARENT_UPGRADE_EXECUTOR_ADDRESS`.
4. Set value to `0`.
5. Set data to `ROOT_EXECUTOR_CALLDATA`.
6. Confirm the decoder shows the parent UpgradeExecutor calling
   `ROLLUP_ADDRESS.setWasmModuleRoot(NEW_WASM_MODULE_ROOT)`.
7. Simulate, propose, collect the configured threshold, and execute.
8. Record the Safe transaction hash and Arbitrum transaction hash.

Verify after confirmation:

```bash
cast call --rpc-url "$PARENT_CHAIN_RPC" \
  "$ROLLUP_ADDRESS" \
  "wasmModuleRoot()(bytes32)"
```

It must equal `NEW_WASM_MODULE_ROOT`. Observe successful validation/assertions
under the new official root before upgrading the sequencer and batch poster.

## 12. Upgrade sequencer and batch poster

After passive nodes and validators are healthy and the new WASM root is
official:

1. Upgrade the standby sequencer, if present.
2. Fail over or stop block production according to the existing operations
   procedure.
3. Upgrade the active sequencer.
4. Confirm block production and feed publication.
5. Upgrade the batch poster.
6. Confirm batches arrive on the correct parent L2 inbox.
7. Confirm delayed-inbox processing and validator progress.

Do not submit DeriwOS activation during a sequencer failover or batch-poster
incident.

## 13. Upgrade to internal ArbOS 60 when required

The route-governance methods on `ArbOwner`/`ArbOwnerPublic` have minimum ArbOS
version 60. `ArbSys.arbOSVersion()` adds 55 to the internal version:

```text
ArbSys result 115 = internal ArbOS 60
ArbSys result 87  = internal ArbOS 32
```

Development and test already report 115 and must not schedule another ArbOS 60
upgrade. Production reported 87 on 2026-08-18 and must complete this section
before using the route-governance methods.

Only schedule ArbOS 60 after the new WASM root is official, every node role is
running the approved binary, and validation remains healthy.

Prepare the inner calldata:

```bash
export ARBOS60_TIMESTAMP="<approved future Unix timestamp>"
export ARBOS_CALLDATA="$(cast calldata \
  "scheduleArbOSUpgrade(uint64,uint64)" \
  60 \
  "$ARBOS60_TIMESTAMP")"
```

Encode the outer call and submit it through the environment's approved L3 Safe.
Production submits it as proposal wave L1 through the production L3 Safe at
`https://safe.deriw.com`:

```bash
export L3_EXECUTOR_CALLDATA="$(cast calldata \
  "executeCall(address,bytes)" \
  0x0000000000000000000000000000000000000070 \
  "$ARBOS_CALLDATA")"
```

The Safe transaction must have:

```text
to:     0xC49f79CcdFbB3668400b7476A641268De81548b1
value:  0
data:   encoded executeCall(ArbOwner, ARBOS_CALLDATA)
```

Read back the schedule:

```bash
cast call --rpc-url "$L3_RPC" \
  0x000000000000000000000000000000000000006b \
  "getScheduledUpgrade()(uint64,uint64)"
```

At the first block whose timestamp reaches the schedule, verify:

```bash
cast call --rpc-url "$L3_RPC" \
  0x0000000000000000000000000000000000000064 \
  "arbOSVersion()(uint256)"
```

The result must be `115`. Confirm node agreement, validator progress, batch
posting, delayed messages, and all preserved Deriw state before proceeding.

## 14. Set the maximum L3 transaction gas to 60M

This is a separate L3 ArbOwner governance action. It is unrelated to
`MAX_DATA_SIZE` in the parent contract-upgrade script.

The ordering is security-critical: do not call `setMaxTxGasLimit` while the
chain is below internal ArbOS 50. In this code, the same method changes the
legacy **block** gas limit below ArbOS 50 and changes the per-transaction limit
at ArbOS 50 and above. Production must therefore finish section 13 and show
`ArbSys.arbOSVersion() == 115` before this transaction.

Read both limits:

```bash
cast call --rpc-url "$L3_RPC" \
  0x000000000000000000000000000000000000006c \
  "getMaxTxGasLimit()(uint256)"

cast call --rpc-url "$L3_RPC" \
  0x000000000000000000000000000000000000006c \
  "getMaxBlockGasLimit()(uint256)"
```

As observed on 2026-08-18, development is `32,000,000 / 300,000,000`, test is
`60,000,000 / 60,000,000`, and production's pre-upgrade legacy block limit is
`300,000,000`. The ArbOS 50 migration initializes the new per-transaction
limit to `32,000,000`, so production is expected to need the following write
after it reaches ArbOS 60. Test already matches and must skip the write unless
the new live preflight proves otherwise.

Stop if the maximum block gas limit is below `60,000,000`; raising the
transaction cap alone would not provide the intended capacity. A block-limit
change is a separate governance decision and is outside this release unless
explicitly approved.

Prepare the inner calldata:

```bash
export MAX_TX_GAS_CALLDATA="$(cast calldata \
  "setMaxTxGasLimit(uint64)" \
  60000000)"
```

For every environment, generate the outer calldata and submit it through an
approved L3 Safe. Production uses proposal wave L2 at
`https://safe.deriw.com`, only after L1 has activated and returned version 115:

```bash
export L3_EXECUTOR_CALLDATA="$(cast calldata \
  "executeCall(address,bytes)" \
  0x0000000000000000000000000000000000000070 \
  "$MAX_TX_GAS_CALLDATA")"
```

The Safe transaction target is `L3_UPGRADE_EXECUTOR`, its value is `0`, and
its decoded inner call must be exactly
`ArbOwner.setMaxTxGasLimit(60000000)`. After confirmation, rerun both getters
and require transaction limit `60,000,000` and block limit at least
`60,000,000`.

## 15. Configure router-only routes

Development and production have compiled bootstrap routes. Test deliberately
has none.

| Environment | Router | Canonical router | Initial gateway |
|---|---|---|---|
| Development | `0x32068069f13191B57c03Eee8531a8C82b26d12B9` | `0x9fF6747040212f6C21fCe2E8ED0B7B05bA5B4a5d` | `0x3fc1626EE794Aa6CdE8d8987F4B67BC1bC217679` |
| Test | Must be verified and staged on-chain | Must be verified and staged on-chain | Complete verified list required |
| Production | `0x8fb358679749FD952Ea5f090b0eA3675722B08F5` | `0xb85b91A9362e296243360e83Cb0792a87Dc32712` | `0x6121117fCcEcdD6dFa7B3230Eacd4f53e12905Db` |

Before relying on a bootstrap, verify every proxy implementation, code hash,
gateway mapping, and parent receiver. If the live route differs, stage the
complete correct route at least seven days before DeriwOS 2.

Test must complete this section before DeriwOS 2 can activate.

Prepare the inner route calldata, with every approved gateway in the array:

```bash
export ROUTE_ACTIVATION_TIMESTAMP="<Unix time at least 604800 seconds ahead>"
export ROUTE_CALLDATA="$(cast calldata \
  "scheduleDeriwRouterConfig(address,address,address[],uint64)" \
  "$DERIW_ROUTER" \
  "$CANONICAL_GATEWAY_ROUTER" \
  "[$TOKEN_GATEWAYS]" \
  "$ROUTE_ACTIVATION_TIMESTAMP")"
```

### L3 Safe submission

Use an approved L3 Safe in every environment. Development may use either of its
two approved executor Safes. Test may use `0xE5C8...e9Ec` or
`0x573a...1017`, subject to its live threshold. Production submits proposal
wave L3 at `https://safe.deriw.com`:

```bash
export L3_EXECUTOR_CALLDATA="$(cast calldata \
  "executeCall(address,bytes)" \
  0x0000000000000000000000000000000000000070 \
  "$ROUTE_CALLDATA")"
```

In the environment's Safe UI, create a custom transaction with:

```text
to:     L3_UPGRADE_EXECUTOR
value:  0
data:   encoded executeCall(ArbOwner, ROUTE_CALLDATA)
```

Decode and compare every address and the timestamp before collecting
signatures.

Read the pending route immediately after execution, then read the active route
after its activation boundary:

```bash
cast call --rpc-url "$L3_RPC" \
  0x000000000000000000000000000000000000006b \
  "getScheduledDeriwRouterConfig()(address,address,address[],uint64,uint64)"

cast call --rpc-url "$L3_RPC" \
  0x000000000000000000000000000000000000006b \
  "getDeriwRouterConfig()(address,address,address[],uint64)"
```

The active revision must be nonzero and every route address must match the
approved release ticket before scheduling DeriwOS 2.

A pending route can be cancelled before activation by sending
`cancelScheduledDeriwRouterConfig()` through the same ArbOwner/UpgradeExecutor
path.

## 16. Activate DeriwOS 1, then DeriwOS 2, then DeriwOS 3

Do not activate versions 1, 2, and 3 in one jump for the first environment
rollout. Activate and verify each version before scheduling the next one.

Current code exposes scheduling on `DeriwBlacklist` at `0x07EC`. Development
retains its standalone blacklist owner on chain, but this runbook never uses it
directly. DeriwOS version scheduling is release governance and must always use
one of the approved environment Safes through the UpgradeExecutor path shown
here.

Recommended minimum operator windows, even if the current contract does not
enforce them:

```text
Development: at least 2 hours
Test:        at least 24 hours
Production:  at least 7 days
```

Prepare DeriwOS 1 calldata:

```bash
export DERIWOS1_TIMESTAMP="<approved future Unix timestamp>"
export DERIWOS_CALLDATA="$(cast calldata \
  "scheduleDeriwOSUpgrade(uint64,uint64)" \
  1 \
  "$DERIWOS1_TIMESTAMP")"
```

Encode the outer call for the approved environment Safe. Development may use
either approved development L3 Safe. Production prepares this as proposal wave
L4 at `https://safe.deriw.com`:

```bash
export L3_EXECUTOR_CALLDATA="$(cast calldata \
  "executeCall(address,bytes)" \
  0x00000000000000000000000000000000000007EC \
  "$DERIWOS_CALLDATA")"
```

Submit that data to `L3_UPGRADE_EXECUTOR` through the correct Safe UI, collect
the required threshold, and execute.

Read the schedule back and verify the timestamp, target version, and recorded
ArbOS version. At activation, confirm `getDeriwOSVersion()` returns DeriwOS 1,
then run the blacklist acceptance tests.

Only after DeriwOS 1 is stable, repeat the same process with target version `2`
and a new approved future timestamp. Production prepares that separate action
as proposal wave L5 at `https://safe.deriw.com`. For test, the active router
revision must already be nonzero. For dev/production, independently verify
either the compiled bootstrap or an active staged replacement.

After DeriwOS 2 activation, confirm:

```bash
cast call --rpc-url "$L3_RPC" \
  0x00000000000000000000000000000000000007EB \
  "getDeriwOSVersion()(uint64,uint64)"

cast call --rpc-url "$L3_RPC" \
  0x000000000000000000000000000000000000006b \
  "getDeriwRouterConfig()(address,address,address[],uint64)"
```

DeriwOS 2 also keeps direct `withdrawEth` route-restricted. Once those negative
and approved-route tests pass, schedule target version `3` as a separate Safe
transaction with a new approved future timestamp. DeriwOS 3 changes no router
configuration: it exempts only the `withdrawEth(address)` ABI entry point.
Raw `sendTxToL1(address,bytes)` and every ERC-20 gateway send remain subject to
the DeriwOS 2 exact-route policy. Production prepares this separate action as
proposal wave L6 at `https://safe.deriw.com`.

## 17. Acceptance tests

### Router-only ArbSys

This direct call must revert after DeriwOS 2 and must continue to revert after
DeriwOS 3:

```bash
cast call \
  --rpc-url "$L3_RPC" \
  --from 0x2222222222222222222222222222222222222222 \
  0x0000000000000000000000000000000000000064 \
  "sendTxToL1(address,bytes)(uint256)" \
  0x1111111111111111111111111111111111111111 \
  0x1234
```

After DeriwOS 3 activation, require `getDeriwOSVersion()` to return DeriwOS 3,
rerun the direct raw-send command above and require it still to revert, then
confirm a direct native-ETH withdrawal succeeds:

```bash
cast call \
  --rpc-url "$L3_RPC" \
  --from "$FUNDED_TEST_ACCOUNT" \
  --value "$TEST_ETH_WITHDRAWAL_VALUE" \
  0x0000000000000000000000000000000000000064 \
  "withdrawEth(address)(uint256)" \
  "$PARENT_ETH_RECIPIENT"
```

Use a funded account and an amount accepted by the environment. This is a
read-only simulation; complete acceptance still requires one small real ETH
withdrawal and one small real ERC-20 withdrawal.

Also verify:

- direct `withdrawEth` succeeds at DeriwOS 3;
- raw `sendTxToL1` still reverts at DeriwOS 3;
- approved router ETH withdrawal succeeds;
- approved router/canonical-router/gateway ERC-20 withdrawal succeeds;
- direct gateway and unknown gateway paths revert;
- delayed-inbox raw `sendTxToL1` calls revert;
- parent receivers authenticate the canonical outbox and recorded L3 sender;
- the documented canonical deposit-recovery behavior is accepted or replaced.

### Consensus blacklist

Verify on a disposable funded test account:

- sequencer admission rejects the intended quarantined transaction;
- forced/delayed inclusion reaches the same consensus result;
- failed no-op receipts consume the intended full gas and advance the expected
  nonce(s);
- protected protocol addresses cannot be blacklisted;
- deposits and retryable submissions preserve the documented funding behavior;
- direct authorized emergency removal remains available.

Never add an irreversible production address solely to test the blacklist.

### Gasless estimation

After the two estimate-gas findings are fixed, verify:

- target-allowlisted unfunded sender with omitted, legacy, and EIP-1559 fee
  fields;
- sender-only allowlisted unfunded account;
- a contract that observes `BASEFEE` and `GASPRICE` receives the same values in
  estimate and actual execution;
- non-allowlisted unfunded accounts still fail normally; and
- mixed RPC nodes do not return different results behind the load balancer.

### Gas-limit configuration

Verify `ArbSys.arbOSVersion()` returns `115`, then require:

- `ArbGasInfo.getMaxTxGasLimit()` returns exactly `60,000,000`;
- `ArbGasInfo.getMaxBlockGasLimit()` returns at least `60,000,000`;
- a controlled development/test transaction just below the intended boundary
  is accepted when otherwise valid; and
- a controlled transaction above the maximum is rejected for the expected gas
  limit reason without changing state.

### Operational health

Verify:

- sequencer block production;
- batch posting to the correct parent inbox;
- delayed inbox processing;
- validator/staker progress with no module-root errors;
- identical head/block/state results across RPC nodes;
- explorer indexing and new precompile ABIs; and
- monitoring alerts and rollback artifacts.

## 18. Promotion policy

Use a separate change ticket and transaction set for each environment.

### Development

1. Use parent Safe `0x4Ec94D...75c2` through parent UpgradeExecutor
   `0x1b46Af...2f1D` for every development parent-governance action, including
   the WASM-root transaction. The owner EOA submits `Safe.execTransaction` but
   must not hold direct `EXECUTOR_ROLE`.
2. Verify L3 UpgradeExecutor `0xB5B4...BD78` remains the sole chain owner and
   Safes `0x5f1B...4151` and `0x9Caa...bdEa` remain the only active
   `EXECUTOR_ROLE` members. The migration is complete; do not repeat its grants
   or cleanup calls.
3. Submit release writes from one of those Safes to the L3 UpgradeExecutor.
   Retain `0x57F9...Fe17` as the development blacklist owner, but do not use it
   as a direct deployment path.
4. Raise the live development transaction limit from 32M to 60M and verify the
   300M block limit is unchanged.
5. Exercise every negative and positive acceptance test through both approved
   L3 Safes.
6. Soak for at least 24 hours after DeriwOS 3 before approving test.

### Test

1. Use parent Safe `0x663C00...ceDe` through parent UpgradeExecutor
   `0x678815...3359` for every test parent-governance action, including the
   contract upgrade and WASM-root transaction. A Safe owner submits
   `Safe.execTransaction`; neither owner has direct `EXECUTOR_ROLE`.
2. Use either retained Safe `0xE5C8...e9Ec` or `0x573a...1017` through L3
   UpgradeExecutor `0xAc35...943`; retain parent UpgradeExecutor alias
   `0x7899...446a` for cross-chain governance.
3. Collect the selected Safe's live threshold: 1-of-2 for `0xE5C8...e9Ec` or
   2-of-4 for `0x573a...1017`.
4. Confirm the live test transaction and block limits remain 60M; skip an
   unnecessary setter transaction.
5. Stage and activate the complete test router route at least seven days before
   DeriwOS 2; test has no compiled bootstrap.
6. Activate and verify DeriwOS 3 only after DeriwOS 2 acceptance passes.
7. Repeat all tests using test deployments and soak for at least 72 hours.

### Production

1. Run the production parent-contract version checker and execute only its
   approved upgrade path through Arbitrum One Safe `0xFbB37c...399D` and parent
   UpgradeExecutor `0x133348...F613`.
2. Use the same 3-of-4 parent Safe path to update the production Rollup root.
3. Upgrade production from internal ArbOS 32 to 60 and verify
   `ArbSys.arbOSVersion()` returns 115.
4. Set the new per-transaction gas limit to 60M and verify the 300M block limit
   is unchanged.
5. Use L3 Safe `0x2F99...c4dF` through L3 UpgradeExecutor
   `0xC49f...48b1`.
6. Collect the live 3-of-4 threshold.
7. Require independent calldata decoding and sign-off from node, bridge, and
   security owners.
8. Use a minimum seven-day activation window and avoid other upgrades in that
   window.
9. Activate and verify DeriwOS 1, 2, and 3 as separate proposal waves.
10. Keep a staffed monitoring window through activation and the relevant
   assertion/challenge period.

## 19. Abort and rollback rules

Abort before any on-chain action if:

- the source tree or any submodule is dirty/unreachable;
- independently built module roots differ;
- the final runtime image lacks either required machine;
- any signer lacks the expected on-chain role;
- the Rollup owner or UpgradeExecutor differs from the deployment manifest;
- the parent version checker reports a different or unsupported upgrade path;
- the selected action, Inbox, ProxyAdmin, `MAX_DATA_SIZE`, or nested calldata
  differs from the reviewed parent-contract proposal;
- pending validation diverges;
- the L3 is below ArbOS 50 when the transaction-limit setter would execute;
- a route address, implementation, gateway list, or parent receiver is
  unverified; or
- an unrelated upgrade is already scheduled.

Before DeriwOS activation, move the schedule to a later timestamp if health
checks fail. Cancel a pending router replacement when its data is wrong.

After DeriwOS activation, do not attempt a protocol downgrade: the current
state logic only moves forward. Preserve pre-activation database snapshots and
old/new images for investigation, but never replay post-activation blocks with
an incompatible binary.

Changing the parent Rollup WASM root back is another governance action, not a
routine node rollback. It requires a separately reviewed incident plan that
accounts for assertions already created under each root.

Likewise, an Inbox or SequencerInbox implementation upgrade is not rolled back
by restarting nodes. Preserve the old implementation addresses and code hashes,
but use them only through a separately simulated and approved parent-governance
incident action that confirms storage compatibility and current ownership.

## 20. Release record

Attach all of the following to the environment change ticket:

- root commit and recursive gitlinks;
- test and security-review results;
- image names, immutable digests, and builder architecture;
- current/new WASM roots and artifact checksums;
- parent contract-version reports, selected action address/code hash, and
  before/after Inbox and SequencerInbox implementations;
- parent Rollup, Inbox, ProxyAdmin, and UpgradeExecutor addresses;
- L3 UpgradeExecutor and signer/Safe role proofs;
- before/after L3 transaction and block gas limits;
- route address, implementation, code-hash, and gateway inventory;
- proposed and decoded inner/outer calldata;
- Safe proposal IDs, signatures, execution hashes, and EOA transaction hashes;
- activation timestamps and blocks;
- before/after version and route query output;
- acceptance-test evidence; and
- incident owner, rollback artifacts, and monitoring window.
