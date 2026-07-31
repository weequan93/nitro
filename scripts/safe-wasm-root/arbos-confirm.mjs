import {
  arbosPreflight,
  assertArbosSignerIsOwner,
  fail,
  loadArbosConfig,
  safeNativeAbi,
  sameAddress,
  signNativeSafeHash,
  signatureCount,
  validateAndEncodeRequestSignatures,
  validateArbosRequestEnvelope,
  validateArbosTransactionRecord,
} from './arbos-common.mjs'
import {
  arbosRequestPathFromArgs,
  readArbosRequest,
  updateArbosRequest,
} from './arbos-request-file.mjs'

async function main() {
  const requestPath = arbosRequestPathFromArgs()
  const config = loadArbosConfig({ needsSigner: true })
  const state = await arbosPreflight(config, { enforceMinimumLead: false })
  const owners = await assertArbosSignerIsOwner(state.client, config)

  const request = await readArbosRequest(requestPath)
  validateArbosRequestEnvelope(request, config, state)
  await validateArbosTransactionRecord(request.transaction, state.client, config)

  const threshold = Number(
    await state.client.readContract({
      address: config.safeAddress,
      abi: safeNativeAbi,
      functionName: 'getThreshold',
    }),
  )
  const existingSignatures = signatureCount(request)
  if (existingSignatures >= threshold) {
    throw new Error(
      `Safe threshold is already met (${existingSignatures}/${threshold}); execute with: npm run arbos-execute -- ${requestPath}`,
    )
  }
  if (
    request.signatures.some((signature) =>
      sameAddress(signature.owner, config.signerAddress),
    )
  ) {
    throw new Error(
      `Owner ${config.signerAddress} has already signed ${request.safeTxHash}`,
    )
  }

  const signature = await signNativeSafeHash(config, request.safeTxHash)
  request.signatures.push({
    owner: config.signerAddress,
    signature,
  })
  await validateAndEncodeRequestSignatures(
    request,
    owners,
    request.safeTxHash,
  )
  request.updatedAt = new Date().toISOString()
  await updateArbosRequest(requestPath, request)

  const signatures = signatureCount(request)
  console.log('ArbOS Safe request validated and signed')
  console.log(`  Safe tx hash: ${request.safeTxHash}`)
  console.log(`  Signer:       ${config.signerAddress}`)
  console.log(`  Signatures:   ${signatures}/${threshold}`)
  console.log(`  Request file: ${requestPath}`)
}

main().catch(fail)
