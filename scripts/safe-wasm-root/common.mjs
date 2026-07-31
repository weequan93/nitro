import Safe from '@safe-global/protocol-kit'
import { OperationType } from '@safe-global/types-kit'
import {
  createPublicClient,
  encodeFunctionData,
  getAddress,
  http,
  keccak256,
  stringToHex,
} from 'viem'
import { privateKeyToAccount } from 'viem/accounts'

const DEFAULT_CHAIN_ID = 421614n
const DEFAULT_L2_CHAIN_ID = 2885n
const DEFAULT_L1_RPC = 'https://rpc-arbitrum-sepolia.deriw.com'
const DEFAULT_L2_RPC = 'https://rpc.test.deriw.com'
const DEFAULT_ROLLUP = '0xb6a39f55E4C4397FE799BeDCc16fFa895950CFF9'
const DEFAULT_WASM_ROOT =
  '0x121d685e2fdb0e3291592d6b90bd70d503951335d19d96455448eb7a14d17421'
const DEFAULT_UPGRADE_EXECUTOR = '0x678815f2c63466f557024d8cce25baeeb4a23359'
const DEFAULT_SAFE_OWNER = '0x35b3ac4003e1AfeE7601C190DB4f039fCb1BbcB5'
const ZERO_ADDRESS = '0x0000000000000000000000000000000000000000'
export const EXECUTOR_ROLE = keccak256(stringToHex('EXECUTOR_ROLE'))

const rollupAbi = [
  {
    type: 'function',
    name: 'setWasmModuleRoot',
    stateMutability: 'nonpayable',
    inputs: [{ name: 'newWasmModuleRoot', type: 'bytes32' }],
    outputs: [],
  },
  {
    type: 'function',
    name: 'wasmModuleRoot',
    stateMutability: 'view',
    inputs: [],
    outputs: [{ type: 'bytes32' }],
  },
]

const upgradeExecutorAbi = [
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
    name: 'executeCall',
    stateMutability: 'payable',
    inputs: [
      { name: 'target', type: 'address' },
      { name: 'targetCallData', type: 'bytes' },
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
]

function required(name) {
  const value = process.env[name]?.trim()
  if (!value) {
    throw new Error(`Missing required environment variable ${name}`)
  }
  return value
}

function address(name, value) {
  if (!/^0x[0-9a-fA-F]{40}$/.test(value)) {
    throw new Error(`${name} must be a 20-byte 0x-prefixed address`)
  }
  return getAddress(value.toLowerCase())
}

function bytes32(name, value) {
  if (!/^0x[0-9a-fA-F]{64}$/.test(value)) {
    throw new Error(`${name} must be a 32-byte 0x-prefixed value`)
  }
  return value.toLowerCase()
}

function privateKey() {
  const raw = required('PK')
  const normalized = raw.startsWith('0x') ? raw : `0x${raw}`
  if (!/^0x[0-9a-fA-F]{64}$/.test(normalized)) {
    throw new Error('PK must be a 32-byte private key; it is never printed')
  }
  return normalized
}

function loadEnvironmentConfig({ needsSigner = false } = {}) {
  const chainIdText = process.env.CHAIN_ID?.trim() || DEFAULT_CHAIN_ID.toString()
  if (!/^[0-9]+$/.test(chainIdText)) {
    throw new Error('CHAIN_ID must be a positive integer')
  }
  const l2ChainIdText =
    process.env.L2_CHAIN_ID?.trim() || DEFAULT_L2_CHAIN_ID.toString()
  if (!/^[0-9]+$/.test(l2ChainIdText)) {
    throw new Error('L2_CHAIN_ID must be a positive integer')
  }

  const config = {
    chainId: BigInt(chainIdText),
    l2ChainId: BigInt(l2ChainIdText),
    rpcUrl: process.env.L1_RPC?.trim() || DEFAULT_L1_RPC,
    l2RpcUrl: process.env.L2_RPC?.trim() || DEFAULT_L2_RPC,
  }

  if (needsSigner) {
    config.privateKey = privateKey()
    config.signerAddress = privateKeyToAccount(config.privateKey).address
  }

  return config
}

export function parseSafeAccountConfig(
  ownersText = DEFAULT_SAFE_OWNER,
  thresholdText = '1',
) {
  const rawOwners = ownersText
    .split(',')
    .map((owner) => owner.trim())
    .filter(Boolean)
  if (rawOwners.length === 0) {
    throw new Error('SAFE_OWNERS must contain at least one address')
  }

  const owners = rawOwners.map((owner, index) =>
    address(`SAFE_OWNERS entry ${index + 1}`, owner),
  )
  if (new Set(owners.map((owner) => owner.toLowerCase())).size !== owners.length) {
    throw new Error('SAFE_OWNERS must not contain duplicate addresses')
  }
  if (!/^[1-9][0-9]*$/.test(thresholdText)) {
    throw new Error('SAFE_THRESHOLD must be a positive integer')
  }

  const threshold = Number(thresholdText)
  if (!Number.isSafeInteger(threshold) || threshold > owners.length) {
    throw new Error(
      `SAFE_THRESHOLD ${thresholdText} exceeds the ${owners.length} configured owner(s)`,
    )
  }

  return { owners, threshold }
}

export function loadSafeDeploymentConfig({ needsSigner = true } = {}) {
  const config = loadEnvironmentConfig({ needsSigner })
  const { owners, threshold } = parseSafeAccountConfig(
    process.env.SAFE_OWNERS?.trim() || DEFAULT_SAFE_OWNER,
    process.env.SAFE_THRESHOLD?.trim() || '1',
  )
  const saltNonce = process.env.SAFE_SALT_NONCE?.trim()
  if (saltNonce && !/^[0-9]+$/.test(saltNonce)) {
    throw new Error('SAFE_SALT_NONCE must be an unsigned integer')
  }

  return {
    ...config,
    owners,
    threshold,
    safeVersion: '1.4.1',
    ...(saltNonce ? { saltNonce } : {}),
  }
}

export function loadConfig({ needsSigner = false } = {}) {
  return {
    ...loadEnvironmentConfig({ needsSigner }),
    safeAddress: address('SAFE_ADDRESS', required('SAFE_ADDRESS')),
    rollup: address('ROLLUP', process.env.ROLLUP?.trim() || DEFAULT_ROLLUP),
    wasmRoot: bytes32('WASM_ROOT', process.env.WASM_ROOT?.trim() || DEFAULT_WASM_ROOT),
    upgradeExecutor: address(
      'UPGRADE_EXECUTOR',
      process.env.UPGRADE_EXECUTOR?.trim() || DEFAULT_UPGRADE_EXECUTOR,
    ),
  }
}

export function publicClient(config) {
  return createPublicClient({ transport: http(config.rpcUrl) })
}

export function l2PublicClient(config) {
  return createPublicClient({ transport: http(config.l2RpcUrl) })
}

export async function assertEnvironmentChains(config) {
  const client = publicClient(config)
  const l2Client = l2PublicClient(config)
  const [actualChainId, actualL2ChainId] = await Promise.all([
    client.getChainId(),
    l2Client.getChainId(),
  ])
  if (BigInt(actualChainId) !== config.chainId) {
    throw new Error(
      `Parent RPC chain ID is ${actualChainId}, but CHAIN_ID is ${config.chainId}`,
    )
  }
  if (BigInt(actualL2ChainId) !== config.l2ChainId) {
    throw new Error(
      `Deriw RPC chain ID is ${actualL2ChainId}, but L2_CHAIN_ID is ${config.l2ChainId}`,
    )
  }
  return { client, l2Client }
}

export async function protocolKit(config) {
  return Safe.init({
    provider: config.rpcUrl,
    signer: config.privateKey,
    safeAddress: config.safeAddress,
  })
}

export function expectedMetaTransaction(config) {
  const setRootData = encodeFunctionData({
    abi: rollupAbi,
    functionName: 'setWasmModuleRoot',
    args: [config.wasmRoot],
  })
  const executeCallData = encodeFunctionData({
    abi: upgradeExecutorAbi,
    functionName: 'executeCall',
    args: [config.rollup, setRootData],
  })

  return {
    to: config.upgradeExecutor,
    value: '0',
    data: executeCallData,
    operation: OperationType.Call,
  }
}

export function safeExecutorGrantData(config) {
  const grantRoleData = encodeFunctionData({
    abi: upgradeExecutorAbi,
    functionName: 'grantRole',
    args: [EXECUTOR_ROLE, config.safeAddress],
  })
  return encodeFunctionData({
    abi: upgradeExecutorAbi,
    functionName: 'executeCall',
    args: [config.upgradeExecutor, grantRoleData],
  })
}

export async function assertSignerIsOwner(safeKit, config) {
  const owners = await safeKit.getOwners()
  if (!owners.some((owner) => sameAddress(owner, config.signerAddress))) {
    throw new Error(
      `Signer ${config.signerAddress} is not an owner of Safe ${config.safeAddress}`,
    )
  }
  return owners
}

export async function preflight(
  config,
  {
    simulate = true,
    allowAlreadySet = false,
    requireExecutorRole = true,
  } = {},
) {
  const { client, l2Client } = await assertEnvironmentChains(config)

  const [safeCode, executorCode, rollupCode] = await Promise.all([
    client.getCode({ address: config.safeAddress }),
    client.getCode({ address: config.upgradeExecutor }),
    client.getCode({ address: config.rollup }),
  ])
  if (!safeCode || safeCode === '0x') {
    throw new Error(`No contract is deployed at SAFE_ADDRESS ${config.safeAddress}`)
  }
  if (!executorCode || executorCode === '0x') {
    throw new Error(`No contract is deployed at UPGRADE_EXECUTOR ${config.upgradeExecutor}`)
  }
  if (!rollupCode || rollupCode === '0x') {
    throw new Error(`No contract is deployed at ROLLUP ${config.rollup}`)
  }

  const [currentRoot, safeCanExecute] = await Promise.all([
    client.readContract({
      address: config.rollup,
      abi: rollupAbi,
      functionName: 'wasmModuleRoot',
    }),
    client.readContract({
      address: config.upgradeExecutor,
      abi: upgradeExecutorAbi,
      functionName: 'hasRole',
      args: [EXECUTOR_ROLE, config.safeAddress],
    }),
  ])

  if (requireExecutorRole && !safeCanExecute) {
    throw new Error(
      `Safe ${config.safeAddress} does not have EXECUTOR_ROLE on ${config.upgradeExecutor}`,
    )
  }
  if (!allowAlreadySet && currentRoot.toLowerCase() === config.wasmRoot) {
    throw new Error(`WASM root is already ${config.wasmRoot}; refusing a redundant transaction`)
  }

  if (simulate && currentRoot.toLowerCase() !== config.wasmRoot) {
    const expected = expectedMetaTransaction(config)
    await client.call({
      account: config.safeAddress,
      to: config.upgradeExecutor,
      data: expected.data,
      value: 0n,
    })
  }

  return { client, l2Client, currentRoot, safeCanExecute }
}

export async function validateTransactionRecord(transaction, safeKit, config) {
  const expected = expectedMetaTransaction(config)
  const hash = transaction.safeTxHash

  if (!/^0x[0-9a-fA-F]{64}$/.test(hash || '')) {
    throw new Error('Request contains an invalid safeTxHash')
  }
  if (transaction.isExecuted) {
    throw new Error(`Safe transaction ${hash} is already executed`)
  }
  if (!sameAddress(transaction.safe, config.safeAddress)) {
    throw new Error(`Safe mismatch: request contains ${transaction.safe}`)
  }
  if (!sameAddress(transaction.to, expected.to)) {
    throw new Error(`Target mismatch: expected ${expected.to}, got ${transaction.to}`)
  }
  if (String(transaction.value) !== expected.value) {
    throw new Error(`Value mismatch: expected 0, got ${transaction.value}`)
  }
  if (Number(transaction.operation) !== Number(expected.operation)) {
    throw new Error(`Operation mismatch: expected CALL (0), got ${transaction.operation}`)
  }
  if ((transaction.data || '').toLowerCase() !== expected.data.toLowerCase()) {
    throw new Error('Calldata mismatch; refusing to sign or execute this transaction')
  }
  if (!/^[0-9]+$/.test(String(transaction.nonce))) {
    throw new Error(`Invalid Safe nonce ${transaction.nonce}`)
  }

  const reconstructed = await safeKit.createTransaction({
    transactions: [expected],
    options: {
      safeTxGas: String(transaction.safeTxGas ?? 0),
      baseGas: String(transaction.baseGas ?? 0),
      gasPrice: String(transaction.gasPrice ?? 0),
      gasToken: transaction.gasToken || ZERO_ADDRESS,
      refundReceiver: transaction.refundReceiver || ZERO_ADDRESS,
      nonce: Number(transaction.nonce),
    },
  })
  const recomputedHash = await safeKit.getTransactionHash(reconstructed)
  if (recomputedHash.toLowerCase() !== hash.toLowerCase()) {
    throw new Error(
      `Safe hash mismatch: recomputed ${recomputedHash}, request contains ${hash}`,
    )
  }

  return reconstructed
}

export function signatureCount(transaction) {
  return new Set(
    (transaction.signatures || [])
      .filter((signature) => signature.signature)
      .map((signature) => signature.owner.toLowerCase()),
  ).size
}

export function sameAddress(left, right) {
  return typeof left === 'string' && typeof right === 'string'
    ? left.toLowerCase() === right.toLowerCase()
    : false
}

export function fail(error) {
  const message = error instanceof Error ? error.message : String(error)
  console.error(`Error: ${message}`)
  process.exitCode = 1
}
