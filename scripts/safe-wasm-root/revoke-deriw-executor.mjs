import Safe from '@safe-global/protocol-kit'

import {
  EXECUTOR_ROLE,
  accessControlAbi,
  assertExactSafeConfiguration,
  checkedAddress,
  deriwExecutorRevokeTransaction,
  deriwPublicClient,
  fail,
  loadDeriwAdminConfig,
  sameAddress,
} from './deriw-admin-common.mjs'

const DEFAULT_OLD_EXECUTOR = '0xa1698F44D70632BfE448804378DA373C55eE8476'

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
  const oldExecutor = checkedAddress(
    'OLD_DERIW_EXECUTOR',
    process.env.OLD_DERIW_EXECUTOR?.trim() || DEFAULT_OLD_EXECUTOR,
  )
  config.expectedOwners = [newOwner]
  config.expectedThreshold = 1n

  if (sameAddress(newOwner, oldExecutor)) {
    throw new Error('NEW_SAFE_OWNER must differ from OLD_DERIW_EXECUTOR')
  }
  if (!sameAddress(config.signerAddress, newOwner)) {
    throw new Error(
      `PK belongs to ${config.signerAddress}, but NEW_SAFE_OWNER is ${newOwner}`,
    )
  }

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
  const [owners, threshold, nonce, safeCanExecute, oldCanExecute] =
    await Promise.all([
      safeKit.getOwners(),
      safeKit.getThreshold(),
      safeKit.getNonce(),
      client.readContract({
        address: config.upgradeExecutor,
        abi: accessControlAbi,
        functionName: 'hasRole',
        args: [EXECUTOR_ROLE, config.safeAddress],
      }),
      client.readContract({
        address: config.upgradeExecutor,
        abi: accessControlAbi,
        functionName: 'hasRole',
        args: [EXECUTOR_ROLE, oldExecutor],
      }),
    ])

  assertExactSafeConfiguration(owners, BigInt(threshold), config)
  if (!safeCanExecute) {
    throw new Error(`Safe ${config.safeAddress} does not have EXECUTOR_ROLE`)
  }
  if (!oldCanExecute) {
    console.log(`Old executor ${oldExecutor} already lacks EXECUTOR_ROLE`)
    return
  }

  const transaction = deriwExecutorRevokeTransaction(config, oldExecutor)
  await client.call({
    account: config.safeAddress,
    to: transaction.to,
    data: transaction.data,
    value: 0n,
  })

  const safeTransaction = await safeKit.createTransaction({
    transactions: [transaction],
    options: { nonce },
  })
  const safeTxHash = await safeKit.getTransactionHash(safeTransaction)
  const signedTransaction = await safeKit.signTransaction(safeTransaction)
  if (!(await safeKit.isValidTransaction(signedTransaction))) {
    throw new Error('Safe rejected the executor-revocation transaction simulation')
  }

  console.log('Old Deriw executor revocation simulated successfully')
  console.log(`  Deriw chain ID:  ${config.chainId}`)
  console.log(`  Safe:            ${config.safeAddress}`)
  console.log(`  Safe owner:      ${newOwner}`)
  console.log(`  Safe threshold:  ${threshold}`)
  console.log(`  Old executor:    ${oldExecutor}`)
  console.log(`  UpgradeExecutor: ${config.upgradeExecutor}`)
  console.log(`  EXECUTOR_ROLE:   ${EXECUTOR_ROLE}`)
  console.log(`  Safe nonce:      ${nonce}`)
  console.log(`  Safe tx hash:    ${safeTxHash}`)
  console.log(`  Transaction to:  ${transaction.to}`)
  console.log(`  Value:           ${transaction.value}`)
  console.log(`  Calldata:        ${transaction.data}`)

  if (process.env.BROADCAST !== '1') {
    console.log('')
    console.log('Dry run only. Review the values, then run with BROADCAST=1.')
    return
  }

  const signerBalance = await client.getBalance({
    address: config.signerAddress,
  })
  if (signerBalance === 0n) {
    throw new Error(`New Safe owner ${config.signerAddress} has zero gas balance`)
  }

  const result = await safeKit.executeTransaction(signedTransaction)
  console.log(`Revocation submitted: ${result.hash}`)

  const receipt = await client.waitForTransactionReceipt({ hash: result.hash })
  if (receipt.status !== 'success') {
    throw new Error(`Executor revocation reverted: ${result.hash}`)
  }

  const [safeStillCanExecute, oldStillCanExecute] = await Promise.all([
    client.readContract({
      address: config.upgradeExecutor,
      abi: accessControlAbi,
      functionName: 'hasRole',
      args: [EXECUTOR_ROLE, config.safeAddress],
    }),
    client.readContract({
      address: config.upgradeExecutor,
      abi: accessControlAbi,
      functionName: 'hasRole',
      args: [EXECUTOR_ROLE, oldExecutor],
    }),
  ])
  if (!safeStillCanExecute || oldStillCanExecute) {
    throw new Error(
      `Unexpected roles after revocation: Safe=${safeStillCanExecute}, old executor=${oldStillCanExecute}`,
    )
  }

  console.log(`Old executor revoked and verified at block ${receipt.blockNumber}`)
}

main().catch(fail)
