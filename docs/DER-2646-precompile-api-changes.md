# DER-2646 Precompile API and ABI Changes

Date: 2026-08-19  
Source commit: `baaaccabb`  
Precompile-interface revision: `f87337228`

## Summary

DER-2646 adds eleven function selectors across four precompiles. It also changes
the versioned behavior of existing `ArbSys`, blacklist, and fee-account methods
without changing their ABI.

| Precompile | Address | New function | Selector | Access |
|---|---|---|---|---|
| `DeriwBlacklist` | `0x00000000000000000000000000000000000007EC` | `scheduleDeriwOSUpgrade(uint64,uint64)` | `0xcf3722a4` | Legacy DeriwOS 1-3 replay only; rejects version 4+ |
| `DeriwBlacklistPublic` | `0x00000000000000000000000000000000000007EB` | `getDeriwOSVersion()` | `0xb9c863dd` | Legacy public-view alias |
| `DeriwBlacklistPublic` | `0x00000000000000000000000000000000000007EB` | `getScheduledDeriwOSUpgrade()` | `0x051109a5` | Legacy public-view alias |
| `ArbOwner` | `0x0000000000000000000000000000000000000070` | `scheduleDeriwOSUpgrade(uint64,uint64)` | `0xcf3722a4` | Chain owner; ArbOS 60+ |
| `ArbOwner` | `0x0000000000000000000000000000000000000070` | `cancelScheduledDeriwOSUpgrade()` | `0x02d05f6c` | Chain owner; ArbOS 60+ |
| `ArbOwner` | `0x0000000000000000000000000000000000000070` | `scheduleDeriwRouterConfig(address,address,address[],uint64)` | `0x9fedd857` | Chain owner; ArbOS 60+ |
| `ArbOwner` | `0x0000000000000000000000000000000000000070` | `cancelScheduledDeriwRouterConfig()` | `0x3c8dcc51` | Chain owner; ArbOS 60+ |
| `ArbOwnerPublic` | `0x000000000000000000000000000000000000006B` | `getDeriwOSVersion()` | `0xb9c863dd` | Public view; ArbOS 60+ |
| `ArbOwnerPublic` | `0x000000000000000000000000000000000000006B` | `getScheduledDeriwOSUpgrade()` | `0x051109a5` | Public view; ArbOS 60+ |
| `ArbOwnerPublic` | `0x000000000000000000000000000000000000006B` | `getDeriwRouterConfig()` | `0x31a79d7e` | Public view; ArbOS 60+ |
| `ArbOwnerPublic` | `0x000000000000000000000000000000000000006B` | `getScheduledDeriwRouterConfig()` | `0xffb95254` | Public view; ArbOS 60+ |

Successful state-changing calls continue to use the pre-existing `OwnerActs`
event:

```solidity
event OwnerActs(bytes4 indexed method, address indexed owner, bytes data);
```

Event topic:

```text
0x3c9e6a772755407311e3b35b3ee56799df8f87395941b3a658eee9e08a67ebda
```

No new event signature was added.

## Solidity interface additions

### Legacy `DeriwBlacklist` compatibility — `0x07EC`

```solidity
interface IDeriwBlacklistDeriwOS {
    function scheduleDeriwOSUpgrade(
        uint64 newVersion,
        uint64 timestamp
    ) external;
}
```

Compatibility rules:

- This selector remains executable only so historical DeriwOS 1-3 transactions
  replay identically.
- It rejects `newVersion >= 4` before changing state, even when the caller is a
  blacklist owner or chain owner.
- New deployment transactions must use the same selector on `ArbOwner` at
  `0x70`.
- The active internal ArbOS version is stored with the schedule.
- The transition is applied at a block boundary once the L3 timestamp reaches
  `timestamp`.
- The legacy public getters at `0x07EB` remain read-only compatibility aliases.
- Replay compatibility means the legacy scheduler can still accept a target in
  the range 1-3 when that target is newer than the active DeriwOS version.
  Treat this as a temporary rollout condition: use `ArbOwner` for all new
  schedules and complete the sequential activation through DeriwOS 4.

### Legacy `DeriwBlacklistPublic` compatibility — `0x07EB`

```solidity
interface IDeriwBlacklistPublicDeriwOS {
    function getDeriwOSVersion()
        external
        view
        returns (
            uint64 arbOSVersion,
            uint64 deriwOSVersion
        );

    function getScheduledDeriwOSUpgrade()
        external
        view
        returns (
            uint64 newVersion,
            uint64 timestamp,
            uint64 scheduledAtArbOSVersion
        );
}
```

`getScheduledDeriwOSUpgrade()` returns `(0, 0, 0)` when no future transition is
pending, including after the scheduled version has activated.

### `ArbOwner` — `0x70`

```solidity
interface IArbOwnerDeriwGovernance {
    function scheduleDeriwOSUpgrade(
        uint64 newVersion,
        uint64 timestamp
    ) external;

    function cancelScheduledDeriwOSUpgrade() external;

    function scheduleDeriwRouterConfig(
        address router,
        address canonicalGatewayRouter,
        address[] calldata approvedTokenGateways,
        uint64 activationTimestamp
    ) external;

    function cancelScheduledDeriwRouterConfig() external;
}
```

Rules:

- Caller must be a chain owner. Operationally, the approved Safe calls the L3
  UpgradeExecutor, which calls `ArbOwner` as the chain owner.
- Available from internal ArbOS 60.
- `scheduleDeriwOSUpgrade` is the only endpoint allowed to schedule DeriwOS 4
  and later versions.
- `cancelScheduledDeriwOSUpgrade` clears the pending target, timestamp, and
  recorded ArbOS version without changing the active DeriwOS version.
- The DeriwOS scheduler does not enforce a minimum delay; the deployment
  runbook requires environment-specific review windows.
- Router `activationTimestamp` must be at least 604800 seconds (7 days) after
  the scheduling block timestamp.
- Router, canonical router, and gateways must be nonzero, distinct deployed
  contracts.
- At least one and at most 32 gateways must be supplied.
- The complete pending configuration replaces any prior pending configuration.
- A protected active, pending, or proposed route contract cannot schedule or
  cancel its own route configuration.
- Cancellation clears only the pending route; it does not change the active
  route.

### `ArbOwnerPublic` — `0x6B`

```solidity
interface IArbOwnerPublicDeriwGovernance {
    function getDeriwOSVersion()
        external
        view
        returns (uint64 arbOSVersion, uint64 deriwOSVersion);

    function getScheduledDeriwOSUpgrade()
        external
        view
        returns (
            uint64 newVersion,
            uint64 timestamp,
            uint64 scheduledAtArbOSVersion
        );

    function getDeriwRouterConfig()
        external
        view
        returns (
            address router,
            address canonicalGatewayRouter,
            address[] memory approvedTokenGateways,
            uint64 revision
        );

    function getScheduledDeriwRouterConfig()
        external
        view
        returns (
            address router,
            address canonicalGatewayRouter,
            address[] memory approvedTokenGateways,
            uint64 revision,
            uint64 activationTimestamp
        );
}
```

An uninitialized active route returns zero addresses, an empty gateway array,
and revision zero. No pending route returns zero addresses, an empty array,
revision zero, and timestamp zero.

## Minimal JSON ABI

### `DeriwBlacklist` additions

```json
[
  {
    "inputs": [
      { "internalType": "uint64", "name": "newVersion", "type": "uint64" },
      { "internalType": "uint64", "name": "timestamp", "type": "uint64" }
    ],
    "name": "scheduleDeriwOSUpgrade",
    "outputs": [],
    "stateMutability": "nonpayable",
    "type": "function"
  }
]
```

### `DeriwBlacklistPublic` additions

```json
[
  {
    "inputs": [],
    "name": "getDeriwOSVersion",
    "outputs": [
      { "internalType": "uint64", "name": "arbOSVersion", "type": "uint64" },
      { "internalType": "uint64", "name": "deriwOSVersion", "type": "uint64" }
    ],
    "stateMutability": "view",
    "type": "function"
  },
  {
    "inputs": [],
    "name": "getScheduledDeriwOSUpgrade",
    "outputs": [
      { "internalType": "uint64", "name": "newVersion", "type": "uint64" },
      { "internalType": "uint64", "name": "timestamp", "type": "uint64" },
      {
        "internalType": "uint64",
        "name": "scheduledAtArbOSVersion",
        "type": "uint64"
      }
    ],
    "stateMutability": "view",
    "type": "function"
  }
]
```

### `ArbOwner` additions

```json
[
  {
    "inputs": [
      { "internalType": "uint64", "name": "newVersion", "type": "uint64" },
      { "internalType": "uint64", "name": "timestamp", "type": "uint64" }
    ],
    "name": "scheduleDeriwOSUpgrade",
    "outputs": [],
    "stateMutability": "nonpayable",
    "type": "function"
  },
  {
    "inputs": [],
    "name": "cancelScheduledDeriwOSUpgrade",
    "outputs": [],
    "stateMutability": "nonpayable",
    "type": "function"
  },
  {
    "inputs": [
      { "internalType": "address", "name": "router", "type": "address" },
      {
        "internalType": "address",
        "name": "canonicalGatewayRouter",
        "type": "address"
      },
      {
        "internalType": "address[]",
        "name": "approvedTokenGateways",
        "type": "address[]"
      },
      {
        "internalType": "uint64",
        "name": "activationTimestamp",
        "type": "uint64"
      }
    ],
    "name": "scheduleDeriwRouterConfig",
    "outputs": [],
    "stateMutability": "nonpayable",
    "type": "function"
  },
  {
    "inputs": [],
    "name": "cancelScheduledDeriwRouterConfig",
    "outputs": [],
    "stateMutability": "nonpayable",
    "type": "function"
  }
]
```

### `ArbOwnerPublic` additions

```json
[
  {
    "inputs": [],
    "name": "getDeriwOSVersion",
    "outputs": [
      { "internalType": "uint64", "name": "arbOSVersion", "type": "uint64" },
      { "internalType": "uint64", "name": "deriwOSVersion", "type": "uint64" }
    ],
    "stateMutability": "view",
    "type": "function"
  },
  {
    "inputs": [],
    "name": "getScheduledDeriwOSUpgrade",
    "outputs": [
      { "internalType": "uint64", "name": "newVersion", "type": "uint64" },
      { "internalType": "uint64", "name": "timestamp", "type": "uint64" },
      {
        "internalType": "uint64",
        "name": "scheduledAtArbOSVersion",
        "type": "uint64"
      }
    ],
    "stateMutability": "view",
    "type": "function"
  },
  {
    "inputs": [],
    "name": "getDeriwRouterConfig",
    "outputs": [
      { "internalType": "address", "name": "router", "type": "address" },
      {
        "internalType": "address",
        "name": "canonicalGatewayRouter",
        "type": "address"
      },
      {
        "internalType": "address[]",
        "name": "approvedTokenGateways",
        "type": "address[]"
      },
      { "internalType": "uint64", "name": "revision", "type": "uint64" }
    ],
    "stateMutability": "view",
    "type": "function"
  },
  {
    "inputs": [],
    "name": "getScheduledDeriwRouterConfig",
    "outputs": [
      { "internalType": "address", "name": "router", "type": "address" },
      {
        "internalType": "address",
        "name": "canonicalGatewayRouter",
        "type": "address"
      },
      {
        "internalType": "address[]",
        "name": "approvedTokenGateways",
        "type": "address[]"
      },
      { "internalType": "uint64", "name": "revision", "type": "uint64" },
      {
        "internalType": "uint64",
        "name": "activationTimestamp",
        "type": "uint64"
      }
    ],
    "stateMutability": "view",
    "type": "function"
  }
]
```

## Existing ABI with changed behavior

### `ArbSys` — `0x64`

No ArbSys selector or parameter type changed:

```solidity
interface IArbSysOutbound {
    function sendTxToL1(
        address destination,
        bytes calldata data
    ) external payable returns (uint256);

    function withdrawEth(
        address destination
    ) external payable returns (uint256);
}
```

```json
[
  {
    "inputs": [
      { "internalType": "address", "name": "destination", "type": "address" },
      { "internalType": "bytes", "name": "data", "type": "bytes" }
    ],
    "name": "sendTxToL1",
    "outputs": [
      { "internalType": "uint256", "name": "", "type": "uint256" }
    ],
    "stateMutability": "payable",
    "type": "function"
  },
  {
    "inputs": [
      { "internalType": "address", "name": "destination", "type": "address" }
    ],
    "name": "withdrawEth",
    "outputs": [
      { "internalType": "uint256", "name": "", "type": "uint256" }
    ],
    "stateMutability": "payable",
    "type": "function"
  }
]
```

Selectors and versioned behavior:

| Function | Selector | DeriwOS 0/1 | DeriwOS 2 | DeriwOS 3+ |
|---|---|---|---|---|
| `sendTxToL1(address,bytes)` | `0x928c169a` | Historical unrestricted behavior | Exact route required | Exact route required |
| `withdrawEth(address)` | `0x25e16063` | Historical unrestricted behavior | Exact direct-router route required | Direct call allowed |

Raw `sendTxToL1` remains route-restricted at DeriwOS 3 even when it carries ETH
and its `bytes` argument is empty. The implementation classifies the original
ABI method rather than inferring an operation from calldata or value.

### Existing blacklist methods

The following selectors are unchanged, but their validation/recovery behavior
changes once DeriwOS 1 is active or scheduled:

| Function | Selector | Changed behavior |
|---|---|---|
| `addBlacklistTxFrom(address)` | `0x328ce6ec` | Rejects protected system/protocol and current fee addresses |
| `addBlacklistTxTo(address)` | `0x9e01565a` | Rejects protected system/protocol and current fee addresses |
| `removeBlacklistTxFrom(address)` | `0xdae84349` | Exact direct zero-value recovery call remains available to an authorized quarantined owner |
| `removeBlacklistTxTo(address)` | `0x89e25c2a` | Exact direct zero-value recovery call remains available to an authorized quarantined owner |

### Existing ArbOwner fee-account methods

The ABI is unchanged, but these setters reject a quarantined replacement once
DeriwOS 1 is active or scheduled:

| Function | Selector |
|---|---|
| `setNetworkFeeAccount(address)` | `0xfcdde2b4` |
| `setInfraFeeAccount(address)` | `0x57f585db` |

## No precompile ABI change for gas estimation

The gasless estimation change modifies the JSON-RPC implementation of
`eth_estimateGas`. It does not add or alter a precompile function. For a
destination on the custom-price target allowlist, the estimator normalizes
legacy and EIP-1559 fee fields to zero before simulation.

## Canonical interface sources

- [`ArbOwner.sol`](../contracts-local/src/precompiles/ArbOwner.sol)
- [`ArbOwnerPublic.sol`](../contracts-local/src/precompiles/ArbOwnerPublic.sol)
- [`DeriwBlacklist.sol`](../contracts-local/src/precompiles/DeriwBlacklist.sol)
- [`DeriwBlacklistPublic.sol`](../contracts-local/src/precompiles/DeriwBlacklistPublic.sol)
- [`ArbSys.sol`](../contracts-local/src/precompiles/ArbSys.sol)
