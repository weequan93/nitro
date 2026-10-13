# Deriw Safe proposal helper

## Generate production blacklist calldata for the Safe UI

`blacklist-calldata.mjs` generates additions to the sender (`from`) and/or
recipient (`to`) blacklists. It only makes read-only RPC calls and writes an
optional local JSON file; it never signs, proposes, or broadcasts a transaction.
Use the dependencies installed with `npm ci` in this directory.

From the repository root, replace the quoted placeholder with the actual
address to blacklist:

```bash
node scripts/safe-proposals/blacklist-calldata.mjs \
  --address 'ADDRESS_TO_BLACKLIST' \
  --direction both \
  --out /tmp/deriw-blacklist.safe.json
```

Repeat `--address 'ANOTHER_ADDRESS'` for multiple addresses. Use `--direction
from` or `--direction to` for a single list. The default is `both`, producing
two calls per address. Duplicate addresses are removed. Existing output files
are not overwritten.

### Choose the ban type (DER-3180)

`--ban-flag 1` is the default and preserves the existing ban-all calldata.
Use `--ban-flag 2` to record an ERC20/USDT transfer ban after **DeriwOS 6**
is active:

```bash
node scripts/safe-proposals/blacklist-calldata.mjs \
  --address 'ADDRESS_TO_BLACKLIST' \
  --ban-flag 2 \
  --direction both \
  --out /tmp/deriw-transfer-ban.safe.json
```

Type `2` is metadata only: DeriwOS does not block token transfers for this type.
Each address has one type. Setting type `2` replaces type `1` in both direction
lists and removes the existing ban-all restriction, even when `--direction`
selects only one list. Setting type `1` restores ban-all behavior. The generated
JSON and terminal output describe the selected type and its replacement effect.

For type `2`, online verification reads the active DeriwOS version from the
public precompile at the same block used for simulations. It rejects versions
below `6`. `--offline` generates unverified calldata without checking activation.
The new flag-aware methods work through either `--route executor` or
`--route direct`.

After execution, call `getBlacklistBanFlag(address)` on the public precompile
and check it returns `2`. Use `isBlacklistTxFromWithFlag(address,2)` and/or
`isBlacklistTxToWithFlag(address,2)` for the relevant directions. Legacy boolean
getters report membership for any type; they do not indicate whether a transaction
will be blocked. Only stored type `1` triggers the general blacklist rejection. Values other than `1` and `2` are rejected by the
helper. See [the ban-type behavior and API](../../docs/decisions/0005-blacklist-ban-types.md).

### Direct Safe calls to the blacklist precompile

Use `--route direct` when the Safe itself is authorized as a blacklist owner
or chain owner. This targets `0x00000000000000000000000000000000000007EC`
and uses the blacklist calldata directly. For example, to reproduce the
supplied recipient-list addition:

```bash
node scripts/safe-proposals/blacklist-calldata.mjs \
  --address '0xe76a03e00b10528e079070d39b52ec5788f6f3a8' \
  --direction to \
  --route direct \
  --out /tmp/deriw-blacklist-direct-to.safe.json
```

For this route, the Safe UI **To** is the `0x…07EC` precompile, **Value** is
`0`, **Operation** is `CALL (0)`, and **Data** is the printed calldata. For ABI
entry use [deriw-blacklist-add.abi.json](./deriw-blacklist-add.abi.json) and select
`addBlacklistTxTo`. Use `--direction both` to generate both list additions.
Direct mode checks the Safe and simulates each call from it; it does not
require the executor role. Unauthorized direct calls fail simulation.
The separate `safe-proposal.mjs` service helper still only accepts executor
targets; use the generated Transaction Builder JSON in the Safe UI for direct
calls. The generator continues to default to `--route executor`.

### Default executor route

The default RPC is `https://rpc.deriw.com`. Before producing output, the script
checks chain ID `2886`, Safe/executor bytecode, the Safe's executor role and
signing threshold, and simulates each exact executor call with `from` set to
the Safe at the same block. A failed check stops generation. `--rpc-url URL`
can select another production endpoint; the chain remains pinned. `--offline`
skips those checks and explicitly labels its output **UNVERIFIED**.

In `https://safe.deriw.com`, select production Safe
`0x2F996bC558818D33DE37aF36Bee7de24bA3Fc4dF`, then open **New transaction →
Transaction Builder** and import the generated JSON. Alternatively enable
**Custom data** and add each printed transaction using these fields:

| Field | Value |
|---|---|
| Safe / caller | `0x2F996bC558818D33DE37aF36Bee7de24bA3Fc4dF` |
| To / contract address | `0xC49f79CcdFbB3668400b7476A641268De81548b1` |
| Value | `0` |
| Operation for each executor call | `CALL (0)` |
| Data / hex encoded | The printed outer `Data (hex encoded)` for that entry |

The outer data is `executeCall(0x00000000000000000000000000000000000007EC,
innerData)`. For type `1`, the inner data calls `addBlacklistTxFrom(address)` or
`addBlacklistTxTo(address)`. Type `2` calls the corresponding
`WithFlag(address,2)` method. If using ABI entry instead of Custom data, select
`executeCall(address,bytes)`, put `0x00000000000000000000000000000000000007EC`
in `target`, and the printed **targetCallData** in `targetCallData`.
The address being blacklisted is inside the inner data; the Safe UI's **To**
field always contains the executor.

Minimal JSON ABIs for this workflow are included:

- [upgrade-executor-call.abi.json](./upgrade-executor-call.abi.json): paste this
  into the Safe ABI field for executor `0xC49f79CcdFbB3668400b7476A641268De81548b1`.
  Select `executeCall` and enter the `target` and `targetCallData` shown above.
- [deriw-blacklist-add.abi.json](./deriw-blacklist-add.abi.json): the legacy and flag-aware
  blacklist addition methods, for encoding/decoding the precompile calldata.

Review every entry, create the proposal, and have Safe owners sign and execute
through the UI. For multiple entries the UI constructs a MultiSend batch;
its outer Safe operation may be a delegatecall to MultiSend, while each child
is a CALL to the executor. The script simulates the child calls separately;
it does not simulate the full Safe batch, guards, signatures, or future state.
For type `1`, after execution, confirm both relevant getters (`isBlacklistTxFrom(address)`
and `isBlacklistTxTo(address)`) at the public precompile
`0x00000000000000000000000000000000000007EB` return `true`.
See the [Safe Transaction Builder guide](https://help.safe.global/articles/4180673514-transaction-builder).

The shell error `zsh: no matches found: data?` means prose with an unquoted `?`
was pasted into zsh and treated as a filename pattern. Run only the command
block above, replacing the quoted address placeholder.

## Prepare and submit signed proposals with the service helper

This helper lets a registered Safe Transaction Service delegate prepare and
submit a trusted proposal without receiving owner or executor authority. It
does not execute the Safe transaction and it never reads a private key.

Use Node.js `20.12.2` and install the pinned SDK versions:

```bash
cd scripts/safe-proposals
nvm use 20.12.2
npm ci
npm test
```

## 1. Confirm the proposer role

The proposer address must be either:

- a registered delegate for the target Safe in the same Transaction Service;
  or
- a Safe owner, with the explicit `--allow-owner-proposer` flag.

A delegate proposal adds no owner confirmation. An owner proposal does count
as that owner's confirmation. The helper checks the service registration,
on-chain owners, threshold, nonce, chain ID, and Safe bytecode.

## 2. Write one proposal manifest

Use one manifest for one Safe, one chain, and one verified deployment stage:

```json
{
  "name": "Production L3 wave 2: set max transaction gas",
  "chainId": "2886",
  "safeAddress": "0x2F996bC558818D33DE37aF36Bee7de24bA3Fc4dF",
  "proposerAddress": "0x1111111111111111111111111111111111111111",
  "upgradeExecutorAddress": "0xC49f79CcdFbB3668400b7476A641268De81548b1",
  "origin": "Deriw release CR-1234",
  "transactions": [
    {
      "to": "0xC49f79CcdFbB3668400b7476A641268De81548b1",
      "value": "0",
      "data": "0x<reviewed executeCall calldata>",
      "operation": 0,
      "description": "UpgradeExecutor.executeCall(ArbOwner, setMaxTxGasLimit(60000000))"
    }
  ]
}
```

Replace the example proposer and calldata. `operation` must be `0` (`CALL`).
Every transaction `to` must equal `upgradeExecutorAddress`; a manifest that
targets ArbOwner, a Deriw precompile, the Rollup, or another governed contract
directly is rejected. For Deriw L3 chain IDs `18417507517`, `2885`, and `2886`,
the helper also pins `upgradeExecutorAddress` to the reviewed environment value.
Parent-chain manifests must provide their separately verified parent
UpgradeExecutor address.

Current verified parent proposal pairs (2026-08-18) are:

| Environment | Chain | Safe | UpgradeExecutor |
|---|---:|---|---|
| Development parent | 421614 | `0x4Ec94DD57A65C3E1C59929885a3d3612941B75c2` | `0x1b46Af3D21A13fd30D2BD396B308A6313aD22f1D` |
| Test parent | 421614 | `0x663C00bA160ff059223f9f56bf80b1aE89DAceDe` | `0x678815F2c63466f557024D8cCe25BaeeB4A23359` |
| Production parent | 42161 | `0xFbB37c66372f7B40361fBC8C8A235ae92711399D` | `0x1333480e92de9511dc9BB01F70901ff3ee94f613` |

Re-query the role and Safe threshold before preparing a proposal. The outer
transaction always targets the listed UpgradeExecutor; the Rollup,
ProxyAdmin, action, or precompile appears only in nested calldata.

For a custom Deriw Safe deployment, add the Protocol Kit `contractNetworks`
object supplied by operations, including the verified Safe and
MultiSendCallOnly deployment addresses for that chain.

Multiple `transactions` are encoded using MultiSendCallOnly. A batch also
requires `"batchSafetyAcknowledgement": true`. Do not combine actions merely
to reduce signatures: every child must be independent, executable at the same
verified state gate, governed by the same Safe, and on the same chain.

## 3. Prepare and independently review

For a custom Deriw L3 Transaction Service:

```bash
export SAFE_RPC_URL="https://rpc.deriw.com"
export SAFE_TX_SERVICE_URL="<approved production L3 transaction-service URL>"

node safe-proposal.mjs prepare \
  --manifest proposal.json \
  --out proposal.prepared.json
```

For the official Safe service on Arbitrum One, omit
`SAFE_TX_SERVICE_URL` and set `SAFE_API_KEY` instead. The command only creates
the prepared file. It does not contact the service's proposal endpoint.

Give `proposal.prepared.json` and the independently decoded child calldata to
the change approver. Compare its Safe address, chain ID, nonce, owners,
threshold, transaction count, and `safeTxHash`.

By default, preparation refuses when the service's next nonce differs from the
on-chain nonce. Resolve existing pending proposals first. Only use
`--allow-pending-predecessors` when the change process explicitly approves a
queued nonce dependency.

## 4. Sign only the Safe transaction hash

The registered proposer signs the printed `safeTxHash` as raw 32-byte data:

```bash
cast wallet sign \
  --no-hash \
  --account <proposal-keystore-account> \
  <safeTxHash> > proposer.sig
```

Foundry also supports Ledger, Trezor, AWS KMS, and GCP KMS signers. Do not put a
production private key in the manifest, environment, command line, or this
directory.

## 5. Submit the proposal, not the execution

```bash
node safe-proposal.mjs submit \
  --prepared proposal.prepared.json \
  --signature-file proposer.sig \
  --confirm-safe 0x2F996bC558818D33DE37aF36Bee7de24bA3Fc4dF
```

Immediately open the approved Safe UI connected to the same Transaction
Service (`https://safe.deriw.com` for production L3) and verify that the
transaction appears with the expected nonce and Safe transaction hash. Send
the UI URL, Safe address, Safe transaction hash, change-ticket ID, and review
deadline to the approved signer channel. Transaction Service submission makes
the proposal available to sign; it does not guarantee that every signer was
notified. The Safe owners must inspect, sign to the configured threshold, and
execute it. The helper cannot collect their signatures or execute on-chain.

## Production proposal waves

Do not create one all-environments or all-stages batch. Prepare and submit the
next proposal only after the prior stage is executed and its postcondition is
verified:

1. Parent Safe: required Nitro contract upgrade; verify.
2. Parent Safe: Rollup WASM root update; verify new-root validation.
3. L3 Safe: schedule ArbOS 60; wait for activation and verify version 115.
4. L3 Safe: set max transaction gas to 60M; verify transaction and block limits.
5. L3 Safe: stage router configuration; wait for and verify activation.
6. L3 Safe: schedule DeriwOS 1; wait for and verify activation.
7. L3 Safe: schedule DeriwOS 2; wait for and verify activation.
8. L3 Safe: schedule DeriwOS 3; verify direct `withdrawEth` succeeds while raw
   `sendTxToL1` and ERC-20 route restrictions remain enforced.

The ArbOS schedule and 60M setter must never share a batch. Before ArbOS 50,
the setter changes the legacy block gas limit instead of the per-transaction
limit. Parent and L3 operations also use different chains and Safes.
Every proposal transaction must target its chain's verified UpgradeExecutor;
the governed contract or precompile appears only inside the reviewed nested
calldata. DeriwOS activation transactions remain separate proposals with
postcondition gates.





     node scripts/safe-proposals/blacklist-calldata.mjs \
    --address '0xbE184Ae2Bd172561ce18e375206066EB5D5136Bd' \
    --address '0x4a98D9770867BE6a62819284751049f2bfca95Ad' \
    --address '0x6349C86E0d4372681066B4Dad155C55E2aC96b69' \
    --address '0x3648ACcE9fa82c24ba6Dba396e9999C6DD0d5474' \
    --address '0x295452B8D1DFe2Da698F348F67293322DaeEf31D' \
    --address '0x34c29d726adacCc078861BecF78F67951986F22f' \
    --address '0x8e0d1A706Ed041F3e1dF0f63354a73C97877E3CF' \
    --address '0x82da8A15c7BFb44377480Bf27A54B834BC732eEE' \
    --address '0x70628234Ce9c175FF33C4b49Bf3FdaC5CDcc793a' \
    --address '0x19F1d21C64393efDDE96795239d68Bd72dC5a377' \
    --address '0xF6206Ada9A38a631C7e60A046aD21237C0E6425A' \
    --address '0x91eC5B77f44c2c031b7a6ADA9B744f419804FA20' \
    --address '0x01b58C29cD3E59605D72e9B5228E69F2b4B5D7ba' \
    --address '0x853DabC54B9CA0d1106919476A2B0F4Dbd6ff700' \
    --address '0xAA611f7529D8A1A4b102C7335914DbBE2797A4Aa' \
    --address '0x3F94073E092f06CDB8E7B708e40Dabf1Cb425018' \
    --address '0x6977756685BbFfF4b4941fE175A1c517F1659607' \
    --address '0x84d49f4Dcfb8D40cf6611CFC83B4c34DB4e4B303' \
    --address '0x516820C26620E0664d8727bA2e07f978d6F752Bf' \
    --address '0x9c7207aF7e7bfbcDfDf9a6D777775Dbb29E84ca7' \
    --direction both \
    --route direct \
    --out ./blacklist-to.safe.json