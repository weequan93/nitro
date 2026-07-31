import { createWalletClient, http } from 'viem'
import { privateKeyToAccount } from 'viem/accounts'

import {
  EXECUTOR_ROLE,
  accessControlAbi,
  assertExactSafeConfiguration,
  deriwPublicClient,
  deriwSafeGrantData,
  fail,
  loadDeriwAdminConfig,
  safeAccountAbi,
} from './deriw-admin-common.mjs'

async function main() {
  const config = loadDeriwAdminConfig({ needsSigner: true })
  const client = deriwPublicClient(config)

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
      `No UpgradeExecutor contract is deployed at ${config.upgradeExecutor}`,
    )
  }

  const [
    owners,
    threshold,
    safeVersion,
    signerCanExecute,
    safeCanExecute,
  ] = await Promise.all([
    client.readContract({
      address: config.safeAddress,
      abi: safeAccountAbi,
      functionName: 'getOwners',
    }),
    client.readContract({
      address: config.safeAddress,
      abi: safeAccountAbi,
      functionName: 'getThreshold',
    }),
    client.readContract({
      address: config.safeAddress,
      abi: safeAccountAbi,
      functionName: 'VERSION',
    }),
    client.readContract({
      address: config.upgradeExecutor,
      abi: accessControlAbi,
      functionName: 'hasRole',
      args: [EXECUTOR_ROLE, config.signerAddress],
    }),
    client.readContract({
      address: config.upgradeExecutor,
      abi: accessControlAbi,
      functionName: 'hasRole',
      args: [EXECUTOR_ROLE, config.safeAddress],
    }),
  ])

  assertExactSafeConfiguration(owners, threshold, config)

  console.log('Deriw Safe executor-role grant configuration')
  console.log(`  Deriw chain ID:    ${config.chainId}`)
  console.log(`  Deriw RPC:         ${config.rpcUrl}`)
  console.log(`  Safe:              ${config.safeAddress}`)
  console.log(`  Safe version:      ${safeVersion}`)
  console.log(`  Safe owners:       ${owners.join(', ')}`)
  console.log(`  Safe threshold:    ${threshold}`)
  console.log(`  Existing executor: ${config.signerAddress}`)
  console.log(`  UpgradeExecutor:   ${config.upgradeExecutor}`)
  console.log(`  EXECUTOR_ROLE:     ${EXECUTOR_ROLE}`)

  if (safeCanExecute) {
    console.log('Safe already has EXECUTOR_ROLE; no transaction is needed.')
    return
  }
  if (!signerCanExecute) {
    throw new Error(
      `Signer ${config.signerAddress} does not have EXECUTOR_ROLE and cannot bootstrap the Safe`,
    )
  }

  const executeCallData = deriwSafeGrantData(config)
  await client.call({
    account: config.signerAddress,
    to: config.upgradeExecutor,
    data: executeCallData,
    value: 0n,
  })

  console.log(`  Calldata:          ${executeCallData}`)
  console.log('')
  console.log('Safe executor-role grant simulated successfully.')

  if (process.env.BROADCAST !== '1') {
    console.log('Dry run only. Review the values, then run with BROADCAST=1.')
    return
  }

  const account = privateKeyToAccount(config.privateKey)
  const wallet = createWalletClient({
    account,
    transport: http(config.rpcUrl),
  })
  const hash = await wallet.sendTransaction({
    account,
    to: config.upgradeExecutor,
    data: executeCallData,
    value: 0n,
  })
  console.log(`Grant submitted: ${hash}`)

  const receipt = await client.waitForTransactionReceipt({ hash })
  if (receipt.status !== 'success') {
    throw new Error(`Role-grant transaction reverted: ${hash}`)
  }

  const granted = await client.readContract({
    address: config.upgradeExecutor,
    abi: accessControlAbi,
    functionName: 'hasRole',
    args: [EXECUTOR_ROLE, config.safeAddress],
  })
  if (!granted) {
    throw new Error('Grant succeeded but the Safe still lacks EXECUTOR_ROLE')
  }
  console.log(`Safe now has EXECUTOR_ROLE at block ${receipt.blockNumber}`)
}

main().catch(fail)
