import Safe from '@safe-global/protocol-kit'

import {
  assertExactSafeConfiguration,
  checkedAddress,
  deriwPublicClient,
  fail,
  loadDeriwAdminConfig,
  sameAddress,
} from './deriw-admin-common.mjs'

function required(name) {
  const value = process.env[name]?.trim()
  if (!value) {
    throw new Error(`Missing required environment variable ${name}`)
  }
  return value
}

async function main() {
  const config = loadDeriwAdminConfig({ needsSigner: true })
  const newOwner = checkedAddress('NEW_SAFE_OWNER', required('NEW_SAFE_OWNER'))
  const client = deriwPublicClient(config)

  const actualChainId = await client.getChainId()
  if (BigInt(actualChainId) !== config.chainId) {
    throw new Error(
      `Deriw RPC chain ID is ${actualChainId}, expected ${config.chainId}`,
    )
  }

  const safeKit = await Safe.init({
    provider: config.rpcUrl,
    signer: config.privateKey,
    safeAddress: config.safeAddress,
  })
  const [owners, threshold, nonce] = await Promise.all([
    safeKit.getOwners(),
    safeKit.getThreshold(),
    safeKit.getNonce(),
  ])
  assertExactSafeConfiguration(owners, BigInt(threshold), config)

  if (threshold !== 1 || owners.length !== 1) {
    throw new Error(
      `Owner rotation script requires the current 1-of-1 Safe; got ${threshold}-of-${owners.length}`,
    )
  }
  const oldOwner = owners[0]
  if (!sameAddress(config.signerAddress, oldOwner)) {
    throw new Error(
      `Signer ${config.signerAddress} is not the current Safe owner ${oldOwner}`,
    )
  }
  if (sameAddress(newOwner, oldOwner)) {
    throw new Error('NEW_SAFE_OWNER is already the sole Safe owner')
  }

  const safeTransaction = await safeKit.createSwapOwnerTx({
    oldOwnerAddress: oldOwner,
    newOwnerAddress: newOwner,
  })
  const safeTxHash = await safeKit.getTransactionHash(safeTransaction)
  const signedTransaction = await safeKit.signTransaction(safeTransaction)

  if (!(await safeKit.isValidTransaction(signedTransaction))) {
    throw new Error('Safe rejected the owner-rotation transaction simulation')
  }

  console.log('Deriw Safe owner rotation simulated successfully')
  console.log(`  Deriw chain ID: ${config.chainId}`)
  console.log(`  Safe:           ${config.safeAddress}`)
  console.log(`  Old owner:      ${oldOwner}`)
  console.log(`  New owner:      ${newOwner}`)
  console.log(`  Threshold:      ${threshold}`)
  console.log(`  Safe nonce:     ${nonce}`)
  console.log(`  Safe tx hash:   ${safeTxHash}`)
  console.log(`  Transaction to: ${safeTransaction.data.to}`)
  console.log(`  Value:          ${safeTransaction.data.value}`)
  console.log(`  Calldata:       ${safeTransaction.data.data}`)

  if (process.env.BROADCAST !== '1') {
    console.log('')
    console.log('Dry run only. Review the values, then run with BROADCAST=1.')
    return
  }

  const signerBalance = await client.getBalance({
    address: config.signerAddress,
  })
  if (signerBalance === 0n) {
    throw new Error(`Current Safe owner ${config.signerAddress} has zero gas balance`)
  }

  const result = await safeKit.executeTransaction(signedTransaction)
  console.log(`Owner rotation submitted: ${result.hash}`)

  const receipt = await client.waitForTransactionReceipt({ hash: result.hash })
  if (receipt.status !== 'success') {
    throw new Error(`Owner rotation reverted: ${result.hash}`)
  }

  const [ownersAfter, thresholdAfter] = await Promise.all([
    safeKit.getOwners(),
    safeKit.getThreshold(),
  ])
  if (
    ownersAfter.length !== 1 ||
    !sameAddress(ownersAfter[0], newOwner) ||
    thresholdAfter !== 1
  ) {
    throw new Error(
      `Rotation executed but Safe is ${thresholdAfter}-of-${ownersAfter.length}: ${ownersAfter.join(', ')}`,
    )
  }

  console.log(`Safe owner rotated and verified at block ${receipt.blockNumber}`)
}

main().catch(fail)
