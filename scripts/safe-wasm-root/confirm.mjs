import {
  assertSignerIsOwner,
  fail,
  loadConfig,
  preflight,
  protocolKit,
  sameAddress,
  signatureCount,
  validateTransactionRecord,
} from './common.mjs'
import { readRequest, requestPathFromArgs, updateRequest } from './request-file.mjs'

async function main() {
  const requestPath = requestPathFromArgs()
  const config = loadConfig({ needsSigner: true })
  const safeKit = await protocolKit(config)

  await assertSignerIsOwner(safeKit, config)
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
  await validateTransactionRecord(request.transaction, safeKit, config)

  const threshold = await safeKit.getThreshold()
  const existingSignatures = signatureCount(request)
  if (existingSignatures >= threshold) {
    throw new Error(
      `Safe threshold is already met (${existingSignatures}/${threshold}); execute with: npm run execute -- ${requestPath}`,
    )
  }

  const alreadyConfirmed = request.signatures.some((signature) =>
    sameAddress(signature.owner, config.signerAddress),
  )
  if (alreadyConfirmed) {
    throw new Error(
      `Owner ${config.signerAddress} has already signed ${request.safeTxHash}`,
    )
  }

  const signature = await safeKit.signHash(request.safeTxHash)
  request.signatures.push({
    owner: config.signerAddress,
    signature: signature.data,
  })
  request.updatedAt = new Date().toISOString()
  await updateRequest(requestPath, request)

  const signatures = signatureCount(request)

  console.log('Safe request validated and signed')
  console.log(`  Safe tx hash: ${request.safeTxHash}`)
  console.log(`  Signer:       ${config.signerAddress}`)
  console.log(`  Signatures:   ${signatures}/${threshold}`)
  console.log(`  Request file: ${requestPath}`)
  if (signatures >= threshold) {
    console.log('')
    console.log(`Threshold reached. Execute with: npm run execute -- ${requestPath}`)
  } else {
    console.log('')
    console.log('Another Safe owner must sign this same request file.')
  }
}

main().catch(fail)
