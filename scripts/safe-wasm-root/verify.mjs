import { fail, loadConfig, preflight } from './common.mjs'

async function main() {
  const config = loadConfig()
  const { currentRoot, safeCanExecute } = await preflight(config, {
    simulate: false,
    allowAlreadySet: true,
    requireExecutorRole: false,
  })

  console.log(`Deriw L2 RPC:      ${config.l2RpcUrl}`)
  console.log(`Deriw chain ID:    ${config.l2ChainId}`)
  console.log(`Parent RPC:        ${config.rpcUrl}`)
  console.log(`Parent chain ID:   ${config.chainId}`)
  console.log(`Safe:              ${config.safeAddress}`)
  console.log(`Has EXECUTOR_ROLE: ${safeCanExecute}`)
  console.log(`Rollup:            ${config.rollup}`)
  console.log(`Current WASM root: ${currentRoot}`)
  console.log(`Target WASM root:  ${config.wasmRoot}`)
  console.log(
    `Matches target:     ${currentRoot.toLowerCase() === config.wasmRoot}`,
  )
}

main().catch(fail)
