import { EthSafeSignature } from '@safe-global/protocol-kit'

import {
  fail,
  loadConfig,
  preflight,
  protocolKit,
  publicClient,
  sameAddress,
  signatureCount,
  validateTransactionRecord,
} from './common.mjs'
import { readRequest, requestPathFromArgs } from './request-file.mjs'

async function main() {
  const requestPath = requestPathFromArgs()
  const config = loadConfig({ needsSigner: true })
  const safeKit = await protocolKit(config)
  const client = publicClient(config)

  await preflight(config)

  const request = await readRequest(requestPath)
  if (request.chainId !== config.chainId.toString()) {
    throw new Error(`Chain ID mismatch: request contains ${request.chainId}`)
  }
  if (request.l2ChainId !== config.l2ChainId.toString()) {
    throw new Error(`L2 chain ID mismatch: request contains ${request.l2ChainId}`)
  }
  if (
    typeof request.safeTxHash !== 'string' ||
    typeof request.transaction.safeTxHash !== 'string' ||
    request.safeTxHash.toLowerCase() !==
      request.transaction.safeTxHash.toLowerCase()
  ) {
    throw new Error('Request hash fields do not match')
  }
  const safeTransaction = await validateTransactionRecord(
    request.transaction,
    safeKit,
    config,
  )

  const signatures = signatureCount(request)
  const threshold = await safeKit.getThreshold()
  if (signatures < threshold) {
    throw new Error(
      `Safe threshold is not met: ${signatures}/${threshold} signatures`,
    )
  }

  const owners = await safeKit.getOwners()
  for (const signature of request.signatures) {
    if (!owners.some((owner) => sameAddress(owner, signature.owner))) {
      throw new Error(`Request contains a signature from non-owner ${signature.owner}`)
    }
    if (!/^0x[0-9a-fA-F]{130}$/.test(signature.signature || '')) {
      throw new Error(`Invalid EOA signature from ${signature.owner}`)
    }
    safeTransaction.addSignature(
      new EthSafeSignature(signature.owner, signature.signature),
    )
  }

  const gasPayerBalance = await client.getBalance({
    address: config.signerAddress,
  })
  if (gasPayerBalance === 0n) {
    throw new Error(
      `Gas-paying signer ${config.signerAddress} has zero balance on parent chain ${config.chainId}; load PK for a funded account and retry the same request`,
    )
  }

  if (!(await safeKit.isValidTransaction(safeTransaction))) {
    throw new Error(
      `Safe rejected the collected signatures or transaction simulation; gas-paying signer ${config.signerAddress} has balance ${gasPayerBalance}`,
    )
  }

  const result = await safeKit.executeTransaction(safeTransaction)
  const executionHash = result.hash
  console.log(`Execution submitted: ${executionHash}`)

  const receipt = await client.waitForTransactionReceipt({
    hash: executionHash,
  })
  if (receipt.status !== 'success') {
    throw new Error(`Execution transaction reverted: ${executionHash}`)
  }

  const { currentRoot } = await preflight(config, {
    simulate: false,
    allowAlreadySet: true,
  })
  if (currentRoot.toLowerCase() !== config.wasmRoot) {
    throw new Error(
      `Execution succeeded but root is ${currentRoot}, expected ${config.wasmRoot}`,
    )
  }

  console.log('Safe request executed and verified')
  console.log(`  Block:          ${receipt.blockNumber}`)
  console.log(`  Execution hash: ${executionHash}`)
  console.log(`  WASM root:      ${currentRoot}`)
}

main().catch(fail)
