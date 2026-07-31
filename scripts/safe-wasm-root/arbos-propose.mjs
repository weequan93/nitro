import {
  arbosPreflight,
  assertArbosSignerIsOwner,
  createSingleCallSafeTransaction,
  expectedArbosTransaction,
  fail,
  getNativeSafeTransactionHash,
  loadArbosConfig,
  safeNativeAbi,
  signNativeSafeHash,
  simulateNativeSafeExecution,
  validateAndEncodeRequestSignatures,
} from './arbos-common.mjs'
import {
  createArbosRequest,
  defaultArbosRequestPath,
} from './arbos-request-file.mjs'

async function main() {
  const config = loadArbosConfig({ needsSigner: true })
  const state = await arbosPreflight(config)
  const owners = await assertArbosSignerIsOwner(state.client, config)
  const expected = expectedArbosTransaction(config)

  const [onChainNonceValue, thresholdValue] = await Promise.all([
    state.client.readContract({
      address: config.safeAddress,
      abi: safeNativeAbi,
      functionName: 'nonce',
    }),
    state.client.readContract({
      address: config.safeAddress,
      abi: safeNativeAbi,
      functionName: 'getThreshold',
    }),
  ])
  const onChainNonce = Number(onChainNonceValue)
  const threshold = Number(thresholdValue)
  const nonceText = process.env.SAFE_NONCE?.trim()
  if (nonceText && !/^[0-9]+$/.test(nonceText)) {
    throw new Error('SAFE_NONCE must be a non-negative integer')
  }
  const nonce = nonceText ? Number(nonceText) : onChainNonce

  const safeTransaction = createSingleCallSafeTransaction(expected, { nonce })
  const safeTxHash = await getNativeSafeTransactionHash(
    state.client,
    config,
    safeTransaction,
  )
  const senderSignature = await signNativeSafeHash(config, safeTxHash)
  const signatures = [
    {
      owner: config.signerAddress,
      signature: senderSignature,
    },
  ]
  const encodedSignatures = await validateAndEncodeRequestSignatures(
    { signatures },
    owners,
    safeTxHash,
  )
  await simulateNativeSafeExecution(
    state.client,
    config,
    safeTransaction,
    encodedSignatures,
    config.signerAddress,
  )
  const requestPath =
    process.env.REQUEST_FILE?.trim() ||
    defaultArbosRequestPath(safeTxHash)

  await createArbosRequest(requestPath, {
    chainId: config.chainId.toString(),
    safe: config.safeAddress,
    safeTxHash,
    createdAt: new Date().toISOString(),
    purpose: 'Schedule a Deriw ArbOS upgrade through the governance Safe',
    expected: {
      upgradeExecutor: config.upgradeExecutor,
      arbOwner: config.arbOwnerAddress,
      targetVersion: config.targetVersion.toString(),
      activationTimestamp: config.activationTimestamp.toString(),
      previousScheduledVersion: state.scheduledVersion.toString(),
      previousScheduledTimestamp: state.scheduledTimestamp.toString(),
      expectedPublicVersion: (
        config.targetVersion + config.publicVersionOffset
      ).toString(),
    },
    transaction: {
      ...safeTransaction.data,
      safeTxHash,
      safe: config.safeAddress,
      isExecuted: false,
    },
    signatures,
  })

  console.log('ArbOS Safe request created and signed')
  console.log(`  Safe:                    ${config.safeAddress}`)
  console.log(`  Proposer:                ${config.signerAddress}`)
  console.log(`  Safe tx hash:            ${safeTxHash}`)
  console.log(`  Nonce:                   ${nonce} (on-chain: ${onChainNonce})`)
  console.log(`  Threshold:               ${threshold}`)
  console.log(`  Current internal version:${state.internalVersion}`)
  console.log(`  Current public version:  ${state.publicVersion}`)
  console.log(`  Target internal version: ${config.targetVersion}`)
  console.log(
    `  Expected public version: ${config.targetVersion + config.publicVersionOffset}`,
  )
  console.log(`  Activation timestamp:    ${config.activationTimestamp}`)
  console.log(
    `  Activation UTC:          ${new Date(Number(config.activationTimestamp) * 1000).toISOString()}`,
  )
  console.log(`  UpgradeExecutor:         ${config.upgradeExecutor}`)
  console.log(
    `  Replaces schedule:       ${state.scheduledVersion} at ${state.scheduledTimestamp}`,
  )
  console.log(`  Request file:            ${requestPath}`)
  console.log('')
  if (threshold <= 1) {
    console.log(
      `Threshold reached. Execute with: npm run arbos-execute -- ${requestPath}`,
    )
  } else {
    console.log(
      `Next owner: npm run arbos-confirm -- ${requestPath}`,
    )
  }
}

main().catch(fail)
