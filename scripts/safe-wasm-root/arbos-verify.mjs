import {
  arbOwnerPublicAbi,
  arbSysAbi,
  arbosClient,
  fail,
  loadArbosConfig,
} from './arbos-common.mjs'
import {
  EXECUTOR_ROLE,
  accessControlAbi,
  safeAccountAbi,
} from './deriw-admin-common.mjs'

async function main() {
  const config = loadArbosConfig({ needsActivation: false })
  const client = arbosClient(config)
  const actualChainId = await client.getChainId()
  if (BigInt(actualChainId) !== config.chainId) {
    throw new Error(
      `Deriw RPC chain ID is ${actualChainId}, expected ${config.chainId}`,
    )
  }

  const [
    owners,
    threshold,
    safeCanExecute,
    scheduledUpgrade,
    publicVersion,
    latestBlock,
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
      address: config.upgradeExecutor,
      abi: accessControlAbi,
      functionName: 'hasRole',
      args: [EXECUTOR_ROLE, config.safeAddress],
    }),
    client.readContract({
      address: config.arbOwnerPublicAddress,
      abi: arbOwnerPublicAbi,
      functionName: 'getScheduledUpgrade',
    }),
    client.readContract({
      address: config.arbSysAddress,
      abi: arbSysAbi,
      functionName: 'arbOSVersion',
    }),
    client.getBlock({ blockTag: 'latest' }),
  ])

  const [scheduledVersion, scheduledTimestamp] = scheduledUpgrade
  const internalVersion = publicVersion - config.publicVersionOffset
  const expectedPublicVersion =
    config.targetVersion + config.publicVersionOffset
  const activated = publicVersion >= expectedPublicVersion

  console.log(`Deriw RPC:                 ${config.rpcUrl}`)
  console.log(`Deriw chain ID:            ${config.chainId}`)
  console.log(`Safe:                      ${config.safeAddress}`)
  console.log(`Safe owners:               ${owners.join(', ')}`)
  console.log(`Safe threshold:            ${threshold}`)
  console.log(`Safe has EXECUTOR_ROLE:    ${safeCanExecute}`)
  console.log(`UpgradeExecutor:           ${config.upgradeExecutor}`)
  console.log(`Current public version:    ${publicVersion}`)
  console.log(`Current internal version:  ${internalVersion}`)
  console.log(`Target internal version:   ${config.targetVersion}`)
  console.log(`Expected public version:   ${expectedPublicVersion}`)
  console.log(`Scheduled version:         ${scheduledVersion}`)
  console.log(`Scheduled timestamp:       ${scheduledTimestamp}`)
  if (scheduledTimestamp > 0n) {
    console.log(
      `Scheduled UTC:             ${new Date(Number(scheduledTimestamp) * 1000).toISOString()}`,
    )
  }
  console.log(`Latest block timestamp:    ${latestBlock.timestamp}`)
  console.log(`Target activated:          ${activated}`)
}

main().catch(fail)
