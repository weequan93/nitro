import {
  createPublicClient,
  encodeFunctionData,
  getAddress,
  http,
  keccak256,
  stringToHex,
} from 'viem'
import { privateKeyToAccount } from 'viem/accounts'

const DEFAULT_DERIW_CHAIN_ID = 2885n
const DEFAULT_DERIW_RPC = 'https://rpc.test.deriw.com'
const DEFAULT_DERIW_SAFE = '0xE5C8e6dAbE8dA8D90F0AE3d4543E930833A0e9Ec'
const DEFAULT_DERIW_UPGRADE_EXECUTOR =
  '0xAc3516eF1E3658887198D192Cb0D7EcA07604943'
const DEFAULT_EXPECTED_SAFE_OWNER =
  '0xa1698F44D70632BfE448804378DA373C55eE8476'

export const EXECUTOR_ROLE = keccak256(stringToHex('EXECUTOR_ROLE'))

export const accessControlAbi = [
  {
    type: 'function',
    name: 'revokeRole',
    stateMutability: 'nonpayable',
    inputs: [
      { name: 'role', type: 'bytes32' },
      { name: 'account', type: 'address' },
    ],
    outputs: [],
  },
  {
    type: 'function',
    name: 'grantRole',
    stateMutability: 'nonpayable',
    inputs: [
      { name: 'role', type: 'bytes32' },
      { name: 'account', type: 'address' },
    ],
    outputs: [],
  },
  {
    type: 'function',
    name: 'hasRole',
    stateMutability: 'view',
    inputs: [
      { name: 'role', type: 'bytes32' },
      { name: 'account', type: 'address' },
    ],
    outputs: [{ type: 'bool' }],
  },
  {
    type: 'function',
    name: 'executeCall',
    stateMutability: 'payable',
    inputs: [
      { name: 'target', type: 'address' },
      { name: 'targetCallData', type: 'bytes' },
    ],
    outputs: [],
  },
]

export const safeAccountAbi = [
  {
    type: 'function',
    name: 'getOwners',
    stateMutability: 'view',
    inputs: [],
    outputs: [{ name: '', type: 'address[]' }],
  },
  {
    type: 'function',
    name: 'getThreshold',
    stateMutability: 'view',
    inputs: [],
    outputs: [{ name: '', type: 'uint256' }],
  },
  {
    type: 'function',
    name: 'VERSION',
    stateMutability: 'view',
    inputs: [],
    outputs: [{ name: '', type: 'string' }],
  },
]

function required(name) {
  const value = process.env[name]?.trim()
  if (!value) {
    throw new Error(`Missing required environment variable ${name}`)
  }
  return value
}

export function checkedAddress(name, value) {
  if (!/^0x[0-9a-fA-F]{40}$/.test(value)) {
    throw new Error(`${name} must be a 20-byte 0x-prefixed address`)
  }
  return getAddress(value.toLowerCase())
}

function privateKey() {
  const raw = required('PK')
  const normalized = raw.startsWith('0x') ? raw : `0x${raw}`
  if (!/^0x[0-9a-fA-F]{64}$/.test(normalized)) {
    throw new Error('PK must be a 32-byte private key; it is never printed')
  }
  return normalized
}

function parseExpectedOwners(value) {
  const owners = value
    .split(',')
    .map((owner) => owner.trim())
    .filter(Boolean)
    .map((owner, index) =>
      checkedAddress(`EXPECTED_DERIW_SAFE_OWNERS entry ${index + 1}`, owner),
    )

  if (owners.length === 0) {
    throw new Error('EXPECTED_DERIW_SAFE_OWNERS must contain at least one address')
  }
  if (new Set(owners.map((owner) => owner.toLowerCase())).size !== owners.length) {
    throw new Error('EXPECTED_DERIW_SAFE_OWNERS must not contain duplicate addresses')
  }
  return owners
}

export function loadDeriwAdminConfig({ needsSigner = false } = {}) {
  const chainIdText =
    process.env.DERIW_CHAIN_ID?.trim() || DEFAULT_DERIW_CHAIN_ID.toString()
  if (!/^[1-9][0-9]*$/.test(chainIdText)) {
    throw new Error('DERIW_CHAIN_ID must be a positive integer')
  }

  const thresholdText = process.env.EXPECTED_DERIW_SAFE_THRESHOLD?.trim() || '1'
  if (!/^[1-9][0-9]*$/.test(thresholdText)) {
    throw new Error('EXPECTED_DERIW_SAFE_THRESHOLD must be a positive integer')
  }
  const expectedThreshold = BigInt(thresholdText)

  const config = {
    chainId: BigInt(chainIdText),
    rpcUrl: process.env.DERIW_RPC?.trim() || DEFAULT_DERIW_RPC,
    safeAddress: checkedAddress(
      'DERIW_SAFE',
      process.env.DERIW_SAFE?.trim() || DEFAULT_DERIW_SAFE,
    ),
    upgradeExecutor: checkedAddress(
      'DERIW_UPGRADE_EXECUTOR',
      process.env.DERIW_UPGRADE_EXECUTOR?.trim() ||
        DEFAULT_DERIW_UPGRADE_EXECUTOR,
    ),
    expectedOwners: parseExpectedOwners(
      process.env.EXPECTED_DERIW_SAFE_OWNERS?.trim() ||
        DEFAULT_EXPECTED_SAFE_OWNER,
    ),
    expectedThreshold,
  }

  if (expectedThreshold > BigInt(config.expectedOwners.length)) {
    throw new Error(
      `EXPECTED_DERIW_SAFE_THRESHOLD ${expectedThreshold} exceeds the ` +
        `${config.expectedOwners.length} expected owner(s)`,
    )
  }

  if (needsSigner) {
    config.privateKey = privateKey()
    config.signerAddress = privateKeyToAccount(config.privateKey).address
  }
  return config
}

export function deriwPublicClient(config) {
  return createPublicClient({ transport: http(config.rpcUrl) })
}

export function deriwSafeGrantData(config) {
  const grantRoleData = encodeFunctionData({
    abi: accessControlAbi,
    functionName: 'grantRole',
    args: [EXECUTOR_ROLE, config.safeAddress],
  })
  return encodeFunctionData({
    abi: accessControlAbi,
    functionName: 'executeCall',
    args: [config.upgradeExecutor, grantRoleData],
  })
}

export function deriwExecutorRevokeTransaction(config, executorAddress) {
  const revokeRoleData = encodeFunctionData({
    abi: accessControlAbi,
    functionName: 'revokeRole',
    args: [EXECUTOR_ROLE, executorAddress],
  })
  const executeCallData = encodeFunctionData({
    abi: accessControlAbi,
    functionName: 'executeCall',
    args: [config.upgradeExecutor, revokeRoleData],
  })

  return {
    to: config.upgradeExecutor,
    value: '0',
    data: executeCallData,
    operation: 0,
  }
}

export function sameAddress(left, right) {
  return left.toLowerCase() === right.toLowerCase()
}

export function assertExactSafeConfiguration(
  actualOwners,
  actualThreshold,
  config,
) {
  const actual = new Set(actualOwners.map((owner) => owner.toLowerCase()))
  const expected = new Set(
    config.expectedOwners.map((owner) => owner.toLowerCase()),
  )
  const ownerSetMatches =
    actual.size === expected.size &&
    [...expected].every((owner) => actual.has(owner))

  if (!ownerSetMatches) {
    throw new Error(
      `Safe owners do not match: expected ${config.expectedOwners.join(', ')}, ` +
        `got ${actualOwners.join(', ')}`,
    )
  }
  if (actualThreshold !== config.expectedThreshold) {
    throw new Error(
      `Safe threshold does not match: expected ${config.expectedThreshold}, ` +
        `got ${actualThreshold}`,
    )
  }
}

export function fail(error) {
  const message = error instanceof Error ? error.message : String(error)
  console.error(`Error: ${message}`)
  process.exitCode = 1
}
