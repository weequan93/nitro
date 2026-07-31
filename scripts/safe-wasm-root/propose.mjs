import {
  assertSignerIsOwner,
  expectedMetaTransaction,
  fail,
  loadConfig,
  preflight,
  protocolKit,
} from './common.mjs'
import { createRequest, defaultRequestPath } from './request-file.mjs'

async function main() {
  const config = loadConfig({ needsSigner: true })
  const safeKit = await protocolKit(config)

  await assertSignerIsOwner(safeKit, config)
  const { currentRoot } = await preflight(config)
  const expected = expectedMetaTransaction(config)

  const onChainNonce = await safeKit.getNonce()
  const nonceText = process.env.SAFE_NONCE?.trim()
  if (nonceText && !/^[0-9]+$/.test(nonceText)) {
    throw new Error('SAFE_NONCE must be a non-negative integer')
  }
  const nonce = nonceText ? Number(nonceText) : onChainNonce

  const safeTransaction = await safeKit.createTransaction({
    transactions: [expected],
    options: { nonce },
  })
  const safeTxHash = await safeKit.getTransactionHash(safeTransaction)
  const senderSignature = await safeKit.signHash(safeTxHash)
  const threshold = await safeKit.getThreshold()
  const requestPath =
    process.env.REQUEST_FILE?.trim() || defaultRequestPath(safeTxHash)

  await createRequest(requestPath, {
    chainId: config.chainId.toString(),
    l2ChainId: config.l2ChainId.toString(),
    safe: config.safeAddress,
    safeTxHash,
    createdAt: new Date().toISOString(),
    purpose: 'Set Rollup WASM module root through UpgradeExecutor',
    expected: {
      rollup: config.rollup,
      upgradeExecutor: config.upgradeExecutor,
      wasmRoot: config.wasmRoot,
    },
    transaction: {
      ...safeTransaction.data,
      safeTxHash,
      safe: config.safeAddress,
      isExecuted: false,
    },
    signatures: [
      {
        owner: config.signerAddress,
        signature: senderSignature.data,
      },
    ],
  })

  console.log('Safe request created and signed')
  console.log(`  Safe:             ${config.safeAddress}`)
  console.log(`  Proposer:         ${config.signerAddress}`)
  console.log(`  Safe tx hash:     ${safeTxHash}`)
  console.log(`  Nonce:            ${nonce} (on-chain nonce: ${onChainNonce})`)
  console.log(`  Threshold:        ${threshold}`)
  console.log(`  Upgrade executor: ${config.upgradeExecutor}`)
  console.log(`  Rollup:           ${config.rollup}`)
  console.log(`  Current root:     ${currentRoot}`)
  console.log(`  Requested root:   ${config.wasmRoot}`)
  console.log(`  Request file:     ${requestPath}`)
  console.log('')
  if (threshold <= 1) {
    console.log(`Threshold reached. Execute with: npm run execute -- ${requestPath}`)
  } else {
    console.log(`Next owner: npm run confirm -- ${requestPath}`)
  }
}

main().catch(fail)
