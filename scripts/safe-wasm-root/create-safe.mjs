import Safe from '@safe-global/protocol-kit'
import { createWalletClient, getAddress, http } from 'viem'
import { privateKeyToAccount } from 'viem/accounts'

import {
  assertEnvironmentChains,
  fail,
  loadSafeDeploymentConfig,
  sameAddress,
} from './common.mjs'

function sameOwners(actualOwners, expectedOwners) {
  const actual = new Set(actualOwners.map((owner) => owner.toLowerCase()))
  const expected = new Set(expectedOwners.map((owner) => owner.toLowerCase()))
  return (
    actual.size === expected.size &&
    [...actual].every((owner) => expected.has(owner))
  )
}

async function verifySafe(config, safeAddress) {
  const safeKit = await Safe.init({
    provider: config.rpcUrl,
    safeAddress,
  })
  const [owners, threshold, version] = await Promise.all([
    safeKit.getOwners(),
    safeKit.getThreshold(),
    Promise.resolve(safeKit.getContractVersion()),
  ])

  if (!sameOwners(owners, config.owners)) {
    throw new Error(
      `Safe ${safeAddress} was deployed, but its owners do not match SAFE_OWNERS`,
    )
  }
  if (threshold !== config.threshold) {
    throw new Error(
      `Safe ${safeAddress} has threshold ${threshold}, expected ${config.threshold}`,
    )
  }
  if (version !== config.safeVersion) {
    throw new Error(
      `Safe ${safeAddress} is version ${version}, expected ${config.safeVersion}`,
    )
  }

  return { owners, threshold, version }
}

async function main() {
  const config = loadSafeDeploymentConfig({ needsSigner: true })
  const { client } = await assertEnvironmentChains(config)
  const predictedSafe = {
    safeAccountConfig: {
      owners: config.owners,
      threshold: config.threshold,
    },
    safeDeploymentConfig: {
      safeVersion: config.safeVersion,
      ...(config.saltNonce ? { saltNonce: config.saltNonce } : {}),
    },
  }
  const safeKit = await Safe.init({
    provider: config.rpcUrl,
    signer: config.privateKey,
    predictedSafe,
  })
  const safeAddress = getAddress(await safeKit.getAddress())
  const existingCode = await client.getCode({ address: safeAddress })

  console.log('Safe deployment configuration')
  console.log(`  Parent chain ID: ${config.chainId}`)
  console.log(`  Parent RPC:      ${config.rpcUrl}`)
  console.log(`  Deriw chain ID:  ${config.l2ChainId}`)
  console.log(`  Safe version:    ${config.safeVersion}`)
  console.log(`  Deployer:        ${config.signerAddress}`)
  console.log(`  Owners:          ${config.owners.join(', ')}`)
  console.log(`  Threshold:       ${config.threshold} of ${config.owners.length}`)
  if (config.saltNonce) {
    console.log(`  Salt nonce:      ${config.saltNonce}`)
  }
  console.log(`  Predicted Safe:  ${safeAddress}`)

  if (existingCode && existingCode !== '0x') {
    await verifySafe(config, safeAddress)
    console.log('')
    console.log('The Safe is already deployed with the expected configuration.')
    console.log(`export SAFE_ADDRESS='${safeAddress}'`)
    return
  }

  const deployment = await safeKit.createSafeDeploymentTransaction()
  const factory = getAddress(deployment.to)
  const value = BigInt(deployment.value)
  const simulation = {
    account: config.signerAddress,
    to: factory,
    data: deployment.data,
    value,
  }
  await client.call(simulation)
  const estimatedGas = await client.estimateGas(simulation)
  const deployerBalance = await client.getBalance({
    address: config.signerAddress,
  })

  console.log(`  Factory:         ${factory}`)
  console.log(`  Value:           ${value}`)
  console.log(`  Estimated gas:   ${estimatedGas}`)
  console.log(`  Deployer balance: ${deployerBalance}`)
  console.log(`  Calldata:        ${deployment.data}`)
  console.log('')
  console.log('Safe deployment simulated successfully.')

  if (process.env.BROADCAST !== '1') {
    console.log('Dry run only. Review the values, then run with BROADCAST=1.')
    return
  }

  const account = privateKeyToAccount(config.privateKey)
  if (!sameAddress(account.address, config.signerAddress)) {
    throw new Error('Internal signer-address mismatch')
  }
  const wallet = createWalletClient({
    account,
    transport: http(config.rpcUrl),
  })
  const hash = await wallet.sendTransaction({
    account,
    to: factory,
    data: deployment.data,
    value,
  })
  console.log(`Deployment submitted: ${hash}`)

  const receipt = await client.waitForTransactionReceipt({ hash })
  if (receipt.status !== 'success') {
    throw new Error(`Safe deployment reverted: ${hash}`)
  }
  const code = await client.getCode({ address: safeAddress })
  if (!code || code === '0x') {
    throw new Error(
      `Deployment succeeded but no contract exists at predicted Safe ${safeAddress}`,
    )
  }
  await verifySafe(config, safeAddress)

  console.log(`Safe deployed at block ${receipt.blockNumber}: ${safeAddress}`)
  console.log(`export SAFE_ADDRESS='${safeAddress}'`)
}

main().catch(fail)
