# Deriw router-only send configuration

The node binary contains audited bootstrap routes in
`arbos/deriwpolicy/router_only_sends.go`. At DeriwOS 2 activation, ArbOS copies
the matching chain-ID route into consensus state exactly once. Later binary,
environment-variable, or RPC configuration changes do not change the active
route.

Each Deriw environment has independent state:

| Environment | RPC | Chain ID | Compiled bootstrap |
|---|---|---:|---|
| Development | `https://rpc.dev.deriw.com` | `18417507517` | Yes |
| Test | `https://rpc.test.deriw.com` | `2885` | No; configure on-chain after verifying deployments |
| Production | `https://rpc.deriw.com` | `2886` | Yes |

Only a chain owner can stage or cancel a change. The management precompile is
`ArbOwner` at `0x0000000000000000000000000000000000000070`. A staged change
replaces the router, canonical gateway router, and complete token-gateway list
as one unit. It becomes active automatically at a start-block boundary after a
minimum seven-day delay.

All validators must run the binary containing these precompile methods before
the first configuration transaction is submitted. Calling a new consensus
method while old validator code is still active can split consensus. Updating a
local node config, RPC hostname, or process environment never changes the
on-chain route.

## Read configuration

The public query precompile is `ArbOwnerPublic` at
`0x000000000000000000000000000000000000006b`.

```bash
cast call \
  --rpc-url "$DERIW_RPC_URL" \
  0x000000000000000000000000000000000000006b \
  "getDeriwRouterConfig()(address,address,address[],uint64)"

cast call \
  --rpc-url "$DERIW_RPC_URL" \
  0x000000000000000000000000000000000000006b \
  "getScheduledDeriwRouterConfig()(address,address,address[],uint64,uint64)"
```

A revision of zero means the corresponding configuration is absent.

## Stage a replacement

Set `DERIW_ACTIVATION_TIMESTAMP` to a Unix timestamp at least 604800 seconds
after the transaction's block timestamp. Every address must be nonzero,
distinct, and contain deployed code. The gateway array must contain between one
and 32 addresses.

```bash
cast send \
  --rpc-url "$DERIW_RPC_URL" \
  --private-key "$DERIW_CHAIN_OWNER_PRIVATE_KEY" \
  0x0000000000000000000000000000000000000070 \
  "scheduleDeriwRouterConfig(address,address,address[],uint64)" \
  "$DERIW_ROUTER" \
  "$DERIW_CANONICAL_GATEWAY_ROUTER" \
  "[$DERIW_TOKEN_GATEWAYS]" \
  "$DERIW_ACTIVATION_TIMESTAMP"
```

For production, submit the same calldata through the chain-owner governance
Safe or timelock instead of exporting an owner key. The transaction is bound to
one chain by normal Ethereum transaction signing, so it cannot update the other
environments.

## Cancel a staged replacement

Cancellation leaves the active route unchanged:

```bash
cast send \
  --rpc-url "$DERIW_RPC_URL" \
  --private-key "$DERIW_CHAIN_OWNER_PRIVATE_KEY" \
  0x0000000000000000000000000000000000000070 \
  "cancelScheduledDeriwRouterConfig()"
```

Before scheduling, verify proxy implementations, code hashes, gateway routing,
parent-chain receiver authentication, and the governance transaction calldata.
After activation, verify direct ArbSys calls revert and approved ETH/ERC-20
routes still succeed.
