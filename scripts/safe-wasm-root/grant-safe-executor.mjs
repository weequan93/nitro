import { createWalletClient, http } from 'viem'
import { privateKeyToAccount } from 'viem/accounts'

import {
  EXECUTOR_ROLE,
  assertEnvironmentChains,
  fail,
  loadConfig,
  safeExecutorGrantData,
} from './common.mjs'

const accessControlAbi = [
  {
    type: 'function',
    name: 'hasRole',
    stateMutability: 'view',
    inputs: [
      { name: 'role', type: 'bytes32' },
      { name: 'account', type: 'address' },
    ],
    outputs: [{ type: 'bool' }],
  },
]

async function main() {
  const config = loadConfig({ needsSigner: true })
  const { client } = await assertEnvironmentChains(config)

  const [safeCode, signerCanExecute, safeCanExecute] = await Promise.all([
    client.getCode({ address: config.safeAddress }),
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

  if (!safeCode || safeCode === '0x') {
    throw new Error(`No Safe contract is deployed at ${config.safeAddress}`)
  }
  if (safeCanExecute) {
    console.log(`Safe ${config.safeAddress} already has EXECUTOR_ROLE`)
    return
  }
  if (!signerCanExecute) {
    throw new Error(
      `Signer ${config.signerAddress} does not have EXECUTOR_ROLE and cannot bootstrap the Safe`,
    )
  }

  const executeCallData = safeExecutorGrantData(config)

  await client.call({
    account: config.signerAddress,
    to: config.upgradeExecutor,
    data: executeCallData,
    value: 0n,
  })

  console.log('Safe executor-role grant simulated successfully')
  console.log(`  Existing executor: ${config.signerAddress}`)
  console.log(`  Safe:              ${config.safeAddress}`)
  console.log(`  UpgradeExecutor:   ${config.upgradeExecutor}`)
  console.log(`  EXECUTOR_ROLE:     ${EXECUTOR_ROLE}`)
  console.log(`  Calldata:          ${executeCallData}`)

  if (process.env.BROADCAST !== '1') {
    console.log('')
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
    throw new Error('Role-grant succeeded but Safe still lacks EXECUTOR_ROLE')
  }
  console.log(`Safe now has EXECUTOR_ROLE at block ${receipt.blockNumber}`)
}

main().catch(fail)
