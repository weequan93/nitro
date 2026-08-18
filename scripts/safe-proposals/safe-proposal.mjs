#!/usr/bin/env node

import { createHash } from 'node:crypto'
import {
  existsSync,
  readFileSync,
  writeFileSync
} from 'node:fs'
import { resolve } from 'node:path'
import { pathToFileURL } from 'node:url'

import SafeApiKit from '@safe-global/api-kit'
import Safe from '@safe-global/protocol-kit'
import {
  JsonRpcProvider,
  getAddress,
  recoverAddress
} from 'ethers'

const PREPARED_FORMAT = 'deriw.safe-proposal.v1'
const HEX_BYTES = /^0x(?:[0-9a-fA-F]{2})*$/
const ECDSA_SIGNATURE = /^0x[0-9a-fA-F]{130}$/
const ZERO_ADDRESS = '0x0000000000000000000000000000000000000000'
const L3_UPGRADE_EXECUTORS = Object.freeze({
  '18417507517': '0xB5B4d7f7a32D86fF3bc270B864c7c06CE6F0BD78',
  '2885': '0xAc3516eF1E3658887198D192Cb0D7EcA07604943',
  '2886': '0xC49f79CcdFbB3668400b7476A641268De81548b1'
})

function assert(condition, message) {
  if (!condition) throw new Error(message)
}

function requiredString(value, label) {
  assert(typeof value === 'string' && value.trim() !== '', `${label} is required`)
  return value.trim()
}

function normalizedAddress(value, label) {
  try {
    return getAddress(requiredString(value, label))
  } catch {
    throw new Error(`${label} must be a valid EVM address`)
  }
}

function normalizedChainId(value) {
  const text = String(value ?? '')
  assert(/^\d+$/.test(text), 'chainId must be a positive decimal integer')
  const chainId = BigInt(text)
  assert(chainId > 0n, 'chainId must be greater than zero')
  return chainId.toString()
}

function normalizedValue(value, label) {
  const text = String(value ?? '0')
  assert(/^\d+$/.test(text), `${label} must be a non-negative decimal integer`)
  return BigInt(text).toString()
}

function normalizedData(value, label) {
  const text = requiredString(value, label)
  assert(HEX_BYTES.test(text), `${label} must be 0x-prefixed, whole-byte hex`)
  return text
}

function normalizedInteger(value, label, minimum = 0) {
  const number = Number(value)
  assert(Number.isSafeInteger(number) && number >= minimum,
    `${label} must be an integer greater than or equal to ${minimum}`)
  return number
}

function normalizeContractNetworks(input, chainId, transactionCount) {
  assert(input && typeof input === 'object' && !Array.isArray(input),
    'contractNetworks must be an object when supplied')
  const network = input[chainId]
  assert(network && typeof network === 'object' && !Array.isArray(network),
    `contractNetworks must contain an entry for chain ID ${chainId}`)

  for (const [key, value] of Object.entries(network)) {
    if (key.endsWith('Address') && value !== undefined) {
      normalizedAddress(value, `contractNetworks[${chainId}].${key}`)
    }
  }
  if (transactionCount > 1) {
    normalizedAddress(
      network.multiSendCallOnlyAddress,
      `contractNetworks[${chainId}].multiSendCallOnlyAddress`
    )
  }
  return input
}

export function normalizeManifest(input) {
  assert(input && typeof input === 'object' && !Array.isArray(input), 'manifest must be a JSON object')

  const name = requiredString(input.name, 'name')
  const chainId = normalizedChainId(input.chainId)
  const safeAddress = normalizedAddress(input.safeAddress, 'safeAddress')
  const proposerAddress = normalizedAddress(input.proposerAddress, 'proposerAddress')
  const upgradeExecutorAddress = normalizedAddress(
    input.upgradeExecutorAddress,
    'upgradeExecutorAddress'
  )
  assert(upgradeExecutorAddress !== ZERO_ADDRESS,
    'upgradeExecutorAddress must not be the zero address')
  const knownL3UpgradeExecutor = L3_UPGRADE_EXECUTORS[chainId]
  if (knownL3UpgradeExecutor) {
    assert(addressesEqual(upgradeExecutorAddress, knownL3UpgradeExecutor),
      `chain ID ${chainId} requires L3 UpgradeExecutor ${knownL3UpgradeExecutor}`)
  }
  const origin = requiredString(input.origin, 'origin')
  assert(origin.length <= 200, 'origin must not exceed 200 characters')
  assert(Array.isArray(input.transactions) && input.transactions.length > 0, 'transactions must contain at least one call')

  const transactions = input.transactions.map((transaction, index) => {
    const label = `transactions[${index}]`
    assert(transaction && typeof transaction === 'object' && !Array.isArray(transaction), `${label} must be an object`)
    assert(transaction.operation === undefined || transaction.operation === 0,
      `${label}.operation must be the integer 0 (CALL); DELEGATECALL is forbidden`)
    const operation = 0

    const to = normalizedAddress(transaction.to, `${label}.to`)
    assert(to !== ZERO_ADDRESS, `${label}.to must not be the zero address`)
    assert(addressesEqual(to, upgradeExecutorAddress),
      `${label}.to must equal upgradeExecutorAddress ${upgradeExecutorAddress}; direct governance targets are forbidden`)

    return {
      to,
      value: normalizedValue(transaction.value, `${label}.value`),
      data: normalizedData(transaction.data, `${label}.data`),
      operation,
      description: requiredString(transaction.description, `${label}.description`)
    }
  })

  if (transactions.length > 1) {
    assert(input.batchSafetyAcknowledgement === true,
      'a multi-call proposal requires batchSafetyAcknowledgement: true')
  }

  const contractNetworks = input.contractNetworks === undefined
    ? undefined
    : normalizeContractNetworks(input.contractNetworks, chainId, transactions.length)

  return {
    name,
    chainId,
    safeAddress,
    proposerAddress,
    upgradeExecutorAddress,
    origin,
    transactions,
    ...(transactions.length > 1 ? { batchSafetyAcknowledgement: true } : {}),
    ...(contractNetworks !== undefined ? { contractNetworks } : {})
  }
}

function parseJsonFile(filename, label) {
  const absolute = resolve(filename)
  let parsed
  try {
    parsed = JSON.parse(readFileSync(absolute, 'utf8'))
  } catch (error) {
    throw new Error(`cannot read ${label} ${absolute}: ${error.message}`)
  }
  return { absolute, parsed }
}

function jsonStringify(value) {
  return JSON.stringify(value, (_, item) => typeof item === 'bigint' ? item.toString() : item, 2) + '\n'
}

function sha256Json(value) {
  return createHash('sha256').update(jsonStringify(value)).digest('hex')
}

function normalizedAddressList(values) {
  return values.map((value) => normalizedAddress(value, 'Safe owner'))
}

function addressesEqual(left, right) {
  return getAddress(left) === getAddress(right)
}

function sameAddressSet(left, right) {
  if (left.length !== right.length) return false
  const normalizedLeft = normalizedAddressList(left).sort()
  const normalizedRight = normalizedAddressList(right).sort()
  return normalizedLeft.every((value, index) => value === normalizedRight[index])
}

function parseArgs(argv) {
  const [command, ...rest] = argv
  const options = {}
  for (let index = 0; index < rest.length; index += 1) {
    const token = rest[index]
    assert(token.startsWith('--'), `unexpected argument: ${token}`)
    const key = token.slice(2)
    assert(key !== '', 'empty option name')
    if (key === 'allow-owner-proposer' || key === 'allow-pending-predecessors' || key === 'overwrite') {
      options[key] = true
      continue
    }
    assert(index + 1 < rest.length && !rest[index + 1].startsWith('--'), `--${key} requires a value`)
    options[key] = rest[index + 1]
    index += 1
  }
  return { command, options }
}

function requiredOption(options, key) {
  return requiredString(options[key], `--${key}`)
}

function assertAllowedOptions(options, allowed) {
  for (const key of Object.keys(options)) {
    assert(allowed.includes(key), `--${key} is not valid for this command`)
  }
}

function buildApiKit(chainId) {
  const txServiceUrl = process.env.SAFE_TX_SERVICE_URL?.trim()
  const apiKey = process.env.SAFE_API_KEY?.trim()
  assert(txServiceUrl || apiKey,
    'set SAFE_TX_SERVICE_URL for a custom Deriw service, or SAFE_API_KEY for the official Safe service')
  return new SafeApiKit({
    chainId: BigInt(chainId),
    ...(txServiceUrl ? { txServiceUrl } : { apiKey })
  })
}

async function connect(manifest) {
  const rpcUrl = requiredString(process.env.SAFE_RPC_URL, 'SAFE_RPC_URL')
  const provider = new JsonRpcProvider(rpcUrl)
  const network = await provider.getNetwork()
  assert(network.chainId.toString() === manifest.chainId,
    `RPC chain ID ${network.chainId} does not match manifest chain ID ${manifest.chainId}`)
  const code = await provider.getCode(manifest.safeAddress)
  assert(code !== '0x', `no contract code at Safe ${manifest.safeAddress}`)

  const protocolKit = await Safe.init({
    provider: rpcUrl,
    safeAddress: manifest.safeAddress,
    ...(manifest.contractNetworks ? { contractNetworks: manifest.contractNetworks } : {})
  })
  const apiKit = buildApiKit(manifest.chainId)
  return { protocolKit, apiKit }
}

async function getProposerRole(apiKit, safeAddress, proposerAddress, owners) {
  if (owners.some((owner) => addressesEqual(owner, proposerAddress))) return 'owner'

  const response = await apiKit.getSafeDelegates({
    safeAddress,
    delegateAddress: proposerAddress
  })
  const delegates = Array.isArray(response) ? response : (response?.results ?? [])
  const now = Date.now()
  const isDelegate = delegates.some((entry) => {
    if (!entry?.delegate || !entry?.delegator) return false
    if (!addressesEqual(entry.delegate, proposerAddress)) return false
    if (!owners.some((owner) => addressesEqual(owner, entry.delegator))) return false
    if (!entry.expiryDate) return true
    const expiry = Date.parse(entry.expiryDate)
    return Number.isFinite(expiry) && expiry > now
  })
  return isDelegate ? 'delegate' : 'none'
}

async function readSafeState(protocolKit, apiKit, manifest) {
  const [owners, threshold, onChainNonce, safeVersion, serviceSafe, serviceNextNonce] = await Promise.all([
    protocolKit.getOwners(),
    protocolKit.getThreshold(),
    protocolKit.getNonce(),
    protocolKit.getContractVersion(),
    apiKit.getSafeInfo(manifest.safeAddress),
    apiKit.getNextNonce(manifest.safeAddress)
  ])

  const normalizedOwners = normalizedAddressList(owners)
  assert(addressesEqual(serviceSafe.address, manifest.safeAddress),
    'Safe Transaction Service returned a different Safe address')
  assert(Number(serviceSafe.threshold) === Number(threshold),
    'Safe Transaction Service threshold disagrees with on-chain Safe state')
  assert(Number(serviceSafe.nonce) === Number(onChainNonce),
    'Safe Transaction Service nonce disagrees with on-chain Safe state')
  assert(sameAddressSet(serviceSafe.owners, normalizedOwners),
    'Safe Transaction Service owners disagree with on-chain Safe state')

  const proposerRole = await getProposerRole(
    apiKit,
    manifest.safeAddress,
    manifest.proposerAddress,
    normalizedOwners
  )

  return {
    owners: normalizedOwners,
    threshold: Number(threshold),
    onChainNonce: Number(onChainNonce),
    serviceNextNonce: Number(serviceNextNonce),
    safeVersion,
    proposerRole
  }
}

function assertProposerRole(role, allowOwnerProposer) {
  assert(role !== 'none', 'proposerAddress is neither a current Safe owner nor a registered Transaction Service delegate')
  if (role === 'owner') {
    assert(allowOwnerProposer,
      'proposerAddress is a Safe owner; use a delegate, or pass --allow-owner-proposer knowing its signature counts as an owner confirmation')
  }
}

function sdkTransactions(manifest) {
  return manifest.transactions.map(({ to, value, data, operation }) => ({
    to,
    value,
    data,
    operation
  }))
}

function sameSafeTransactionData(left, right) {
  const fields = [
    'to',
    'value',
    'data',
    'operation',
    'safeTxGas',
    'baseGas',
    'gasPrice',
    'gasToken',
    'refundReceiver',
    'nonce'
  ]
  return fields.every((field) => String(left[field]) === String(right[field]))
}

async function getExistingTransaction(apiKit, safeTxHash) {
  try {
    return await apiKit.getTransaction(safeTxHash)
  } catch (error) {
    if (Number(error?.statusCode) === 404) return null
    throw error
  }
}

export function normalizePrepared(input) {
  assert(input && typeof input === 'object' && !Array.isArray(input), 'prepared proposal must be a JSON object')
  assert(input.format === PREPARED_FORMAT, `prepared proposal format must be ${PREPARED_FORMAT}`)
  const manifest = normalizeManifest(input.manifest)
  const safeTxHash = requiredString(input.safeTxHash, 'safeTxHash')
  assert(/^0x[0-9a-fA-F]{64}$/.test(safeTxHash), 'safeTxHash must be 32-byte hex')
  assert(input.safeTransactionData && typeof input.safeTransactionData === 'object', 'safeTransactionData is required')
  assert(input.safe && typeof input.safe === 'object', 'safe state snapshot is required')
  assert(requiredString(input.manifestSha256, 'manifestSha256') === sha256Json(manifest),
    'prepared manifest checksum does not match its normalized manifest')
  const proposerRole = requiredString(input.safe.proposerRole, 'safe.proposerRole')
  assert(proposerRole === 'delegate' || proposerRole === 'owner',
    'safe.proposerRole must be delegate or owner')

  return {
    ...input,
    manifest,
    safeTxHash,
    safe: {
      ...input.safe,
      owners: normalizedAddressList(input.safe.owners),
      threshold: normalizedInteger(input.safe.threshold, 'safe.threshold', 1),
      onChainNonce: normalizedInteger(input.safe.onChainNonce, 'safe.onChainNonce'),
      serviceNextNonce: normalizedInteger(input.safe.serviceNextNonce, 'safe.serviceNextNonce'),
      safeVersion: requiredString(input.safe.safeVersion, 'safe.safeVersion'),
      proposerRole
    }
  }
}

async function prepare(options) {
  assertAllowedOptions(options, [
    'manifest',
    'out',
    'allow-owner-proposer',
    'allow-pending-predecessors',
    'overwrite'
  ])
  const manifestFile = requiredOption(options, 'manifest')
  const outputFile = resolve(requiredOption(options, 'out'))
  const { absolute: manifestPath, parsed } = parseJsonFile(manifestFile, 'manifest')
  const manifest = normalizeManifest(parsed)
  if (existsSync(outputFile) && !options.overwrite) {
    throw new Error(`refusing to overwrite ${outputFile}; pass --overwrite only after reviewing the existing file`)
  }

  const { protocolKit, apiKit } = await connect(manifest)
  const safe = await readSafeState(protocolKit, apiKit, manifest)
  assertProposerRole(safe.proposerRole, options['allow-owner-proposer'])
  if (!options['allow-pending-predecessors']) {
    assert(safe.serviceNextNonce === safe.onChainNonce,
      `Transaction Service next nonce ${safe.serviceNextNonce} differs from on-chain nonce ${safe.onChainNonce}; resolve pending proposals or explicitly pass --allow-pending-predecessors`)
  }

  const safeTransaction = await protocolKit.createTransaction({
    transactions: sdkTransactions(manifest),
    onlyCalls: true,
    options: { nonce: safe.serviceNextNonce }
  })
  const safeTxHash = await protocolKit.getTransactionHash(safeTransaction)
  const prepared = {
    format: PREPARED_FORMAT,
    createdAt: new Date().toISOString(),
    manifestPath,
    manifestSha256: sha256Json(manifest),
    manifest,
    safe,
    safeTransactionData: safeTransaction.data,
    safeTxHash
  }

  writeFileSync(outputFile, jsonStringify(prepared), {
    flag: options.overwrite ? 'w' : 'wx',
    mode: 0o600
  })

  process.stdout.write(jsonStringify({
    status: 'prepared-not-submitted',
    outputFile,
    chainId: manifest.chainId,
    safeAddress: manifest.safeAddress,
    proposerAddress: manifest.proposerAddress,
    proposerRole: safe.proposerRole,
    threshold: safe.threshold,
    nonce: safe.serviceNextNonce,
    transactionCount: manifest.transactions.length,
    safeTxHash,
    signCommand: `cast wallet sign --no-hash --account <proposal-account> ${safeTxHash}`
  }))
}

function readSignature(filename) {
  const signature = readFileSync(resolve(filename), 'utf8').trim()
  assert(ECDSA_SIGNATURE.test(signature), 'signature file must contain exactly one 65-byte 0x-prefixed ECDSA signature')
  return signature
}

async function submit(options) {
  assertAllowedOptions(options, [
    'prepared',
    'signature-file',
    'confirm-safe',
    'allow-owner-proposer'
  ])
  const preparedFile = requiredOption(options, 'prepared')
  const signatureFile = requiredOption(options, 'signature-file')
  const confirmSafe = normalizedAddress(requiredOption(options, 'confirm-safe'), '--confirm-safe')
  const { parsed } = parseJsonFile(preparedFile, 'prepared proposal')
  const prepared = normalizePrepared(parsed)
  const { manifest } = prepared
  assert(addressesEqual(confirmSafe, manifest.safeAddress),
    `--confirm-safe does not match prepared Safe ${manifest.safeAddress}`)

  const signature = readSignature(signatureFile)
  const recovered = getAddress(recoverAddress(prepared.safeTxHash, signature))
  assert(addressesEqual(recovered, manifest.proposerAddress),
    `signature recovered ${recovered}, expected proposer ${manifest.proposerAddress}`)

  const { protocolKit, apiKit } = await connect(manifest)
  const existing = await getExistingTransaction(apiKit, prepared.safeTxHash)
  const liveSafe = await readSafeState(protocolKit, apiKit, manifest)
  assertProposerRole(liveSafe.proposerRole, options['allow-owner-proposer'])
  assert(liveSafe.safeVersion === prepared.safe.safeVersion, 'Safe contract version changed after preparation')
  assert(liveSafe.threshold === prepared.safe.threshold, 'Safe threshold changed after preparation')
  assert(sameAddressSet(liveSafe.owners, prepared.safe.owners), 'Safe owners changed after preparation')
  assert(liveSafe.onChainNonce === prepared.safe.onChainNonce, 'on-chain Safe nonce changed after preparation')
  assert(liveSafe.proposerRole === prepared.safe.proposerRole, 'proposer role changed after preparation')
  if (!existing) {
    assert(liveSafe.serviceNextNonce === prepared.safe.serviceNextNonce,
      'Transaction Service next nonce changed after preparation; prepare a new proposal')
  }

  const safeTransaction = await protocolKit.createTransaction({
    transactions: sdkTransactions(manifest),
    onlyCalls: true,
    options: { nonce: prepared.safe.serviceNextNonce }
  })
  const rebuiltHash = await protocolKit.getTransactionHash(safeTransaction)
  assert(rebuiltHash.toLowerCase() === prepared.safeTxHash.toLowerCase(),
    'rebuilt Safe transaction hash differs from the prepared hash')
  assert(sameSafeTransactionData(safeTransaction.data, prepared.safeTransactionData),
    'rebuilt Safe transaction data differs from the prepared transaction data')

  if (existing) {
    assert(addressesEqual(existing.safe, manifest.safeAddress),
      'an existing transaction with this hash belongs to a different Safe')
    assert(Number(existing.nonce) === prepared.safe.serviceNextNonce,
      'the existing transaction nonce differs from the prepared nonce')
    process.stdout.write(jsonStringify({
      status: 'already-submitted-for-safe-owner-signatures',
      chainId: manifest.chainId,
      safeAddress: manifest.safeAddress,
      safeTxHash: prepared.safeTxHash,
      proposerAddress: manifest.proposerAddress,
      proposerRole: liveSafe.proposerRole,
      nonce: existing.nonce,
      confirmationsRequired: existing.confirmationsRequired
    }))
    return
  }

  await apiKit.proposeTransaction({
    safeAddress: manifest.safeAddress,
    safeTxHash: prepared.safeTxHash,
    safeTransactionData: safeTransaction.data,
    senderAddress: manifest.proposerAddress,
    senderSignature: signature,
    origin: manifest.origin
  })

  process.stdout.write(jsonStringify({
    status: 'submitted-for-safe-owner-signatures',
    chainId: manifest.chainId,
    safeAddress: manifest.safeAddress,
    safeTxHash: prepared.safeTxHash,
    proposerAddress: manifest.proposerAddress,
    proposerRole: liveSafe.proposerRole,
    ownerConfirmationsAddedByProposer: liveSafe.proposerRole === 'delegate' ? 0 : 1,
    nonce: prepared.safe.serviceNextNonce,
    confirmationsRequired: liveSafe.threshold
  }))
}

function printHelp() {
  process.stdout.write(`Deriw Safe proposal helper

Prepare without submitting:
  SAFE_RPC_URL=... SAFE_TX_SERVICE_URL=... node safe-proposal.mjs prepare \\
    --manifest proposal.json --out proposal.prepared.json

Sign the printed Safe transaction hash outside this script:
  cast wallet sign --no-hash --account <proposal-account> <safeTxHash> > proposer.sig

Submit the signed proposal to the Safe Transaction Service:
  SAFE_RPC_URL=... SAFE_TX_SERVICE_URL=... node safe-proposal.mjs submit \\
    --prepared proposal.prepared.json --signature-file proposer.sig \\
    --confirm-safe 0x...

Use SAFE_API_KEY instead of SAFE_TX_SERVICE_URL for an official Safe service.
Owner proposers require --allow-owner-proposer because their signature counts
as an owner confirmation. Pending predecessor proposals require the explicit
--allow-pending-predecessors flag during preparation.
`)
}

async function main() {
  const { command, options } = parseArgs(process.argv.slice(2))
  if (!command || command === 'help' || command === '--help') {
    printHelp()
    return
  }
  if (command === 'prepare') {
    await prepare(options)
    return
  }
  if (command === 'submit') {
    await submit(options)
    return
  }
  throw new Error(`unknown command: ${command}`)
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  main().catch((error) => {
    process.stderr.write(`error: ${error.message}\n`)
    process.exitCode = 1
  })
}
