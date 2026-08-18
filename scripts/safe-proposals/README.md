# Deriw Safe proposal helper

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
