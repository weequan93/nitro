import { createWalletClient, http } from 'viem'
import { privateKeyToAccount } from 'viem/accounts'

import {
  arbosPreflight,
  encodeNativeSafeExecution,
  fail,
  loadArbosConfig,
  safeNativeAbi,
  signatureCount,
  simulateNativeSafeExecution,
  validateAndEncodeRequestSignatures,
  validateArbosRequestEnvelope,
  validateArbosTransactionRecord,
} from './arbos-common.mjs'
import {
  arbosRequestPathFromArgs,
  readArbosRequest,
} from './arbos-request-file.mjs'

async function main() {
  const requestPath = arbosRequestPathFromArgs()
  const config = loadArbosConfig({ needsSigner: true })
  const state = await arbosPreflight(config, {
    enforceMinimumLead: false,
  })

  const request = await readArbosRequest(requestPath)
  validateArbosRequestEnvelope(request, config, state)
  const safeTransaction = await validateArbosTransactionRecord(
    request.transaction,
    state.client,
    config,
  )

  const signatures = signatureCount(request)
  const [thresholdValue, owners] = await Promise.all([
    state.client.readContract({
      address: config.safeAddress,
      abi: safeNativeAbi,
      functionName: 'getThreshold',
    }),
    state.client.readContract({
      address: config.safeAddress,
      abi: safeNativeAbi,
      functionName: 'getOwners',
    }),
  ])
  const threshold = Number(thresholdValue)
  if (signatures < threshold) {
    throw new Error(
      `Safe threshold is not met: ${signatures}/${threshold} signatures`,
    )
  }

  const encodedSignatures = await validateAndEncodeRequestSignatures(
    request,
    owners,
    request.safeTxHash,
  )

  const gasPayerBalance = await state.client.getBalance({
    address: config.signerAddress,
  })
  if (gasPayerBalance === 0n) {
    throw new Error(
      `Gas-paying signer ${config.signerAddress} has zero Deriw balance`,
    )
  }
  await simulateNativeSafeExecution(
    state.client,
    config,
    safeTransaction,
    encodedSignatures,
    config.signerAddress,
  )

  const account = privateKeyToAccount(config.privateKey)
  const wallet = createWalletClient({
    account,
    transport: http(config.rpcUrl),
  })
  const executionData = encodeNativeSafeExecution(
    safeTransaction,
    encodedSignatures,
  )
  const executionHash = await wallet.sendTransaction({
    account,
    to: config.safeAddress,
    data: executionData,
    value: 0n,
  })
  console.log(`ArbOS scheduling submitted: ${executionHash}`)

  const receipt = await state.client.waitForTransactionReceipt({
    hash: executionHash,
  })
  if (receipt.status !== 'success') {
    throw new Error(`ArbOS scheduling reverted: ${executionHash}`)
  }

  const [scheduledVersion, scheduledTimestamp] =
    await state.client.readContract({
      address: config.arbOwnerPublicAddress,
      abi: [
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
      ],
      functionName: 'getScheduledUpgrade',
    })
  if (
    scheduledVersion !== config.targetVersion ||
    scheduledTimestamp !== config.activationTimestamp
  ) {
    throw new Error(
      `Execution succeeded but scheduled upgrade is ${scheduledVersion} at ${scheduledTimestamp}`,
    )
  }

  console.log('ArbOS upgrade scheduled and verified')
  console.log(`  Block:                ${receipt.blockNumber}`)
  console.log(`  Execution hash:       ${executionHash}`)
  console.log(`  Internal version:     ${scheduledVersion}`)
  console.log(`  Activation timestamp: ${scheduledTimestamp}`)
  console.log(
    `  Activation UTC:       ${new Date(Number(scheduledTimestamp) * 1000).toISOString()}`,
  )
}

main().catch(fail)
