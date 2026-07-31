import {
  createPublicClient,
  encodeFunctionData,
  getAddress,
  http,
  recoverMessageAddress,
} from 'viem'
import { privateKeyToAccount } from 'viem/accounts'

import { EXECUTOR_ROLE, accessControlAbi } from './deriw-admin-common.mjs'

const DEFAULT_CHAIN_ID = 2885n
const DEFAULT_RPC = 'https://rpc.test.deriw.com'
const DEFAULT_SAFE = '0xE5C8e6dAbE8dA8D90F0AE3d4543E930833A0e9Ec'
const DEFAULT_UPGRADE_EXECUTOR =
  '0xAc3516eF1E3658887198D192Cb0D7EcA07604943'
const DEFAULT_TARGET_VERSION = 60n
const DEFAULT_PUBLIC_VERSION_OFFSET = 55n
const DEFAULT_MINIMUM_LEAD_SECONDS = 900n
const ARB_OWNER_ADDRESS = '0x0000000000000000000000000000000000000070'
const ARB_OWNER_PUBLIC_ADDRESS =
  '0x000000000000000000000000000000000000006b'
const ARB_SYS_ADDRESS = '0x0000000000000000000000000000000000000064'
const ZERO_ADDRESS = '0x0000000000000000000000000000000000000000'
const UINT64_MAX = (1n << 64n) - 1n

const arbOwnerAbi = [
  {
    type: 'function',
    name: 'scheduleArbOSUpgrade',
    stateMutability: 'nonpayable',
    inputs: [
      { name: 'newVersion', type: 'uint64' },
      { name: 'timestamp', type: 'uint64' },
    ],
    outputs: [],
  },
]

export const arbOwnerPublicAbi = [
  {
    type: 'function',
    name: 'getScheduledUpgrade',
    stateMutability: 'view',
    inputs: [],
    outputs: [
      { name: 'version', type: 'uint64' },
      { name: 'timestamp', type: 'uint64' },
    ],
  },
]

export const arbSysAbi = [
  {
    type: 'function',
    name: 'arbOSVersion',
    stateMutability: 'view',
    inputs: [],
    outputs: [{ name: '', type: 'uint256' }],
  },
]

export const safeNativeAbi = [
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
    name: 'nonce',
    stateMutability: 'view',
    inputs: [],
    outputs: [{ name: '', type: 'uint256' }],
  },
  {
    type: 'function',
    name: 'getTransactionHash',
    stateMutability: 'view',
    inputs: [
      { name: 'to', type: 'address' },
      { name: 'value', type: 'uint256' },
      { name: 'data', type: 'bytes' },
      { name: 'operation', type: 'uint8' },
      { name: 'safeTxGas', type: 'uint256' },
      { name: 'baseGas', type: 'uint256' },
      { name: 'gasPrice', type: 'uint256' },
      { name: 'gasToken', type: 'address' },
      { name: 'refundReceiver', type: 'address' },
      { name: '_nonce', type: 'uint256' },
    ],
    outputs: [{ name: '', type: 'bytes32' }],
  },
  {
    type: 'function',
    name: 'execTransaction',
    stateMutability: 'payable',
    inputs: [
      { name: 'to', type: 'address' },
      { name: 'value', type: 'uint256' },
      { name: 'data', type: 'bytes' },
      { name: 'operation', type: 'uint8' },
      { name: 'safeTxGas', type: 'uint256' },
      { name: 'baseGas', type: 'uint256' },
      { name: 'gasPrice', type: 'uint256' },
      { name: 'gasToken', type: 'address' },
      { name: 'refundReceiver', type: 'address' },
      { name: 'signatures', type: 'bytes' },
    ],
    outputs: [{ name: 'success', type: 'bool' }],
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

function uint(name, value, { allowZero = true, max } = {}) {
  if (!/^[0-9]+$/.test(value)) {
    throw new Error(`${name} must be an unsigned integer`)
  }
  const parsed = BigInt(value)
  if (!allowZero && parsed === 0n) {
    throw new Error(`${name} must be greater than zero`)
  }
  if (max !== undefined && parsed > max) {
    throw new Error(`${name} exceeds ${max}`)
  }
  return parsed
}

function privateKey() {
  const raw = required('PK')
  const normalized = raw.startsWith('0x') ? raw : `0x${raw}`
  if (!/^0x[0-9a-fA-F]{64}$/.test(normalized)) {
    throw new Error('PK must be a 32-byte private key; it is never printed')
  }
  return normalized
}

export function loadArbosConfig({
  needsSigner = false,
  needsActivation = true,
} = {}) {
  const chainId = uint(
    'DERIW_CHAIN_ID',
    process.env.DERIW_CHAIN_ID?.trim() || DEFAULT_CHAIN_ID.toString(),
    { allowZero: false },
  )
  const targetVersion = uint(
    'ARBOS_TARGET_VERSION',
    process.env.ARBOS_TARGET_VERSION?.trim() ||
      DEFAULT_TARGET_VERSION.toString(),
    { allowZero: false, max: UINT64_MAX },
  )
  const publicVersionOffset = uint(
    'ARBOS_PUBLIC_VERSION_OFFSET',
    process.env.ARBOS_PUBLIC_VERSION_OFFSET?.trim() ||
      DEFAULT_PUBLIC_VERSION_OFFSET.toString(),
  )
  const minimumLeadSeconds = uint(
    'MIN_ACTIVATION_LEAD_SECONDS',
    process.env.MIN_ACTIVATION_LEAD_SECONDS?.trim() ||
      DEFAULT_MINIMUM_LEAD_SECONDS.toString(),
  )

  const config = {
    chainId,
    rpcUrl: process.env.DERIW_RPC?.trim() || DEFAULT_RPC,
    safeAddress: address(
      'DERIW_SAFE',
      process.env.DERIW_SAFE?.trim() || DEFAULT_SAFE,
    ),
    upgradeExecutor: address(
      'DERIW_UPGRADE_EXECUTOR',
      process.env.DERIW_UPGRADE_EXECUTOR?.trim() ||
        DEFAULT_UPGRADE_EXECUTOR,
    ),
    arbOwnerAddress: ARB_OWNER_ADDRESS,
    arbOwnerPublicAddress: ARB_OWNER_PUBLIC_ADDRESS,
    arbSysAddress: ARB_SYS_ADDRESS,
    targetVersion,
    publicVersionOffset,
    minimumLeadSeconds,
    rescheduleExistingUpgrade:
      process.env.RESCHEDULE_EXISTING_UPGRADE?.trim() === '1',
  }

  const activationText = process.env.ACTIVATION_TIMESTAMP?.trim()
  if (needsActivation || activationText) {
    config.activationTimestamp = uint(
      'ACTIVATION_TIMESTAMP',
      activationText || required('ACTIVATION_TIMESTAMP'),
      { allowZero: false, max: UINT64_MAX },
    )
  }

  if (needsSigner) {
    config.privateKey = privateKey()
    config.signerAddress = privateKeyToAccount(config.privateKey).address
  }
  return config
}

export function arbosClient(config) {
  return createPublicClient({ transport: http(config.rpcUrl) })
}

export function expectedArbosTransaction(config) {
  const scheduleData = encodeFunctionData({
    abi: arbOwnerAbi,
    functionName: 'scheduleArbOSUpgrade',
    args: [config.targetVersion, config.activationTimestamp],
  })
  const executeCallData = encodeFunctionData({
    abi: accessControlAbi,
    functionName: 'executeCall',
    args: [config.arbOwnerAddress, scheduleData],
  })

  return {
    to: config.upgradeExecutor,
    value: '0',
    data: executeCallData,
    operation: 0,
  }
}

export function createSingleCallSafeTransaction(
  transaction,
  {
    nonce,
    safeTxGas = '0',
    baseGas = '0',
    gasPrice = '0',
    gasToken = ZERO_ADDRESS,
    refundReceiver = ZERO_ADDRESS,
  },
) {
  return {
    data: {
      to: transaction.to,
      value: String(transaction.value),
      data: transaction.data,
      operation: Number(transaction.operation),
      safeTxGas: String(safeTxGas),
      baseGas: String(baseGas),
      gasPrice: String(gasPrice),
      gasToken,
      refundReceiver,
      nonce: Number(nonce),
    },
  }
}

function safeTransactionArgs(transaction) {
  const data = transaction.data
  return [
    data.to,
    BigInt(data.value),
    data.data,
    Number(data.operation),
    BigInt(data.safeTxGas),
    BigInt(data.baseGas),
    BigInt(data.gasPrice),
    data.gasToken,
    data.refundReceiver,
  ]
}

export async function getNativeSafeTransactionHash(
  client,
  config,
  transaction,
) {
  return client.readContract({
    address: config.safeAddress,
    abi: safeNativeAbi,
    functionName: 'getTransactionHash',
    args: [...safeTransactionArgs(transaction), BigInt(transaction.data.nonce)],
  })
}

function toSafeEthSignSignature(signature) {
  let v = Number.parseInt(signature.slice(-2), 16)
  if (v === 0 || v === 1) {
    v += 27
  }
  if (v !== 27 && v !== 28) {
    throw new Error(`Unexpected signature v value ${v}`)
  }
  return `${signature.slice(0, -2)}${(v + 4).toString(16).padStart(2, '0')}`
}

function fromSafeEthSignSignature(signature) {
  const safeV = Number.parseInt(signature.slice(-2), 16)
  if (safeV !== 31 && safeV !== 32) {
    throw new Error(
      `Safe eth_sign signature must have v 31 or 32, got ${safeV}`,
    )
  }
  return `${signature.slice(0, -2)}${(safeV - 4).toString(16).padStart(2, '0')}`
}

export async function signNativeSafeHash(config, safeTxHash) {
  const account = privateKeyToAccount(config.privateKey)
  const signature = await account.signMessage({
    message: { raw: safeTxHash },
  })
  return toSafeEthSignSignature(signature)
}

export function encodeNativeSafeExecution(transaction, encodedSignatures) {
  return encodeFunctionData({
    abi: safeNativeAbi,
    functionName: 'execTransaction',
    args: [...safeTransactionArgs(transaction), encodedSignatures],
  })
}

export async function validateAndEncodeRequestSignatures(
  request,
  owners,
  safeTxHash,
) {
  const seen = new Set()
  const validated = []

  for (const entry of request.signatures) {
    if (!owners.some((owner) => sameAddress(owner, entry.owner))) {
      throw new Error(`Request contains a signature from non-owner ${entry.owner}`)
    }
    if (!/^0x[0-9a-fA-F]{130}$/.test(entry.signature || '')) {
      throw new Error(`Invalid EOA signature from ${entry.owner}`)
    }
    const ownerKey = entry.owner.toLowerCase()
    if (seen.has(ownerKey)) {
      throw new Error(`Duplicate signature from owner ${entry.owner}`)
    }
    seen.add(ownerKey)

    const standardSignature = fromSafeEthSignSignature(entry.signature)
    const recovered = await recoverMessageAddress({
      message: { raw: safeTxHash },
      signature: standardSignature,
    })
    if (!sameAddress(recovered, entry.owner)) {
      throw new Error(
        `Signature owner mismatch: entry says ${entry.owner}, recovered ${recovered}`,
      )
    }
    validated.push(entry)
  }

  validated.sort((left, right) =>
    left.owner.toLowerCase().localeCompare(right.owner.toLowerCase()),
  )
  return `0x${validated.map((entry) => entry.signature.slice(2)).join('')}`
}

export async function simulateNativeSafeExecution(
  client,
  config,
  transaction,
  encodedSignatures,
  sender,
) {
  const { result } = await client.simulateContract({
    account: sender,
    address: config.safeAddress,
    abi: safeNativeAbi,
    functionName: 'execTransaction',
    args: [...safeTransactionArgs(transaction), encodedSignatures],
  })
  if (!result) {
    throw new Error('Safe simulation returned execution failure')
  }
}

export async function assertArbosSignerIsOwner(client, config) {
  const owners = await client.readContract({
    address: config.safeAddress,
    abi: safeNativeAbi,
    functionName: 'getOwners',
  })
  if (!owners.some((owner) => sameAddress(owner, config.signerAddress))) {
    throw new Error(
      `Signer ${config.signerAddress} is not an owner of Safe ${config.safeAddress}`,
    )
  }
  return owners
}

export async function arbosPreflight(
  config,
  {
    enforceMinimumLead = true,
    simulate = true,
    allowMatchingSchedule = false,
  } = {},
) {
  const client = arbosClient(config)
  const actualChainId = await client.getChainId()
  if (BigInt(actualChainId) !== config.chainId) {
    throw new Error(
      `Deriw RPC chain ID is ${actualChainId}, expected ${config.chainId}`,
    )
  }

  const [safeCode, executorCode] = await Promise.all([
    client.getCode({ address: config.safeAddress }),
    client.getCode({ address: config.upgradeExecutor }),
  ])
  if (!safeCode || safeCode === '0x') {
    throw new Error(`No Safe contract is deployed at ${config.safeAddress}`)
  }
  if (!executorCode || executorCode === '0x') {
    throw new Error(
      `No UpgradeExecutor is deployed at ${config.upgradeExecutor}`,
    )
  }

  const [
    safeCanExecute,
    scheduledUpgrade,
    publicVersion,
    latestBlock,
  ] = await Promise.all([
    client.readContract({
      address: config.upgradeExecutor,
      abi: accessControlAbi,
      functionName: 'hasRole',
      args: [EXECUTOR_ROLE, config.safeAddress],
    }),
    client.readContract({
      address: config.arbOwnerPublicAddress,
      abi: arbOwnerPublicAbi,
      functionName: 'getScheduledUpgrade',
    }),
    client.readContract({
      address: config.arbSysAddress,
      abi: arbSysAbi,
      functionName: 'arbOSVersion',
    }),
    client.getBlock({ blockTag: 'latest' }),
  ])

  if (!safeCanExecute) {
    throw new Error(
      `Safe ${config.safeAddress} does not have EXECUTOR_ROLE on ${config.upgradeExecutor}`,
    )
  }
  if (publicVersion < config.publicVersionOffset) {
    throw new Error(
      `Public ArbOS version ${publicVersion} is below configured offset ${config.publicVersionOffset}`,
    )
  }

  const internalVersion = publicVersion - config.publicVersionOffset
  if (config.targetVersion <= internalVersion) {
    throw new Error(
      `Target internal ArbOS version ${config.targetVersion} is not newer than current ${internalVersion}`,
    )
  }

  const [scheduledVersion, scheduledTimestamp] = scheduledUpgrade
  const scheduleMatches =
    scheduledVersion === config.targetVersion &&
    scheduledTimestamp === config.activationTimestamp
  if (
    scheduledVersion !== 0n ||
    scheduledTimestamp !== 0n
  ) {
    if (
      !(allowMatchingSchedule && scheduleMatches) &&
      !config.rescheduleExistingUpgrade
    ) {
      throw new Error(
        `An ArbOS upgrade is already scheduled: version ${scheduledVersion}, timestamp ${scheduledTimestamp}; set RESCHEDULE_EXISTING_UPGRADE=1 only to intentionally replace it`,
      )
    }
  }

  if (!config.activationTimestamp) {
    throw new Error('ACTIVATION_TIMESTAMP is required for scheduling')
  }
  const requiredLead = enforceMinimumLead ? config.minimumLeadSeconds : 0n
  if (config.activationTimestamp <= latestBlock.timestamp + requiredLead) {
    throw new Error(
      `Activation ${config.activationTimestamp} must be more than ${requiredLead} seconds after latest block timestamp ${latestBlock.timestamp}`,
    )
  }

  if (simulate && scheduledVersion === 0n && scheduledTimestamp === 0n) {
    const expected = expectedArbosTransaction(config)
    await client.call({
      account: config.safeAddress,
      to: expected.to,
      data: expected.data,
      value: 0n,
    })
  }

  return {
    client,
    publicVersion,
    internalVersion,
    scheduledVersion,
    scheduledTimestamp,
    latestBlockTimestamp: latestBlock.timestamp,
    safeCanExecute,
  }
}

export async function validateArbosTransactionRecord(
  transaction,
  client,
  config,
) {
  const expected = expectedArbosTransaction(config)
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

  const reconstructed = createSingleCallSafeTransaction(expected, {
    safeTxGas: String(transaction.safeTxGas ?? 0),
    baseGas: String(transaction.baseGas ?? 0),
    gasPrice: String(transaction.gasPrice ?? 0),
    gasToken: transaction.gasToken || ZERO_ADDRESS,
    refundReceiver: transaction.refundReceiver || ZERO_ADDRESS,
    nonce: Number(transaction.nonce),
  })
  const recomputedHash = await getNativeSafeTransactionHash(
    client,
    config,
    reconstructed,
  )
  if (recomputedHash.toLowerCase() !== hash.toLowerCase()) {
    throw new Error(
      `Safe hash mismatch: recomputed ${recomputedHash}, request contains ${hash}`,
    )
  }
  return reconstructed
}

export function validateArbosRequestEnvelope(request, config, state) {
  if (request.chainId !== config.chainId.toString()) {
    throw new Error(`Chain ID mismatch: request contains ${request.chainId}`)
  }
  if (!sameAddress(request.safe, config.safeAddress)) {
    throw new Error(`Safe mismatch: request contains ${request.safe}`)
  }
  if (
    request.expected?.targetVersion !== config.targetVersion.toString() ||
    request.expected?.activationTimestamp !==
      config.activationTimestamp.toString() ||
    !sameAddress(request.expected?.upgradeExecutor, config.upgradeExecutor) ||
    !sameAddress(request.expected?.arbOwner, config.arbOwnerAddress)
  ) {
    throw new Error('Request upgrade parameters do not match the configured upgrade')
  }
  if (
    state &&
    (request.expected?.previousScheduledVersion !==
      state.scheduledVersion.toString() ||
      request.expected?.previousScheduledTimestamp !==
        state.scheduledTimestamp.toString())
  ) {
    throw new Error(
      `Scheduled upgrade changed after proposal: request expected version ${request.expected?.previousScheduledVersion} at ${request.expected?.previousScheduledTimestamp}, chain has ${state.scheduledVersion} at ${state.scheduledTimestamp}`,
    )
  }
  if (
    typeof request.safeTxHash !== 'string' ||
    typeof request.transaction?.safeTxHash !== 'string' ||
    request.safeTxHash.toLowerCase() !==
      request.transaction.safeTxHash.toLowerCase()
  ) {
    throw new Error('Request hash fields do not match')
  }
}

export function signatureCount(request) {
  return new Set(
    (request.signatures || [])
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
