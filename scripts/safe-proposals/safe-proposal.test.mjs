import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import test from 'node:test'

import {
  normalizeManifest,
  normalizePrepared
} from './safe-proposal.mjs'

const manifest = {
  name: 'Production L3 wave 2: max transaction gas',
  chainId: '2886',
  safeAddress: '0x2F996bC558818D33DE37aF36Bee7de24bA3Fc4dF',
  proposerAddress: '0x1111111111111111111111111111111111111111',
  upgradeExecutorAddress: '0xC49f79CcdFbB3668400b7476A641268De81548b1',
  origin: 'Deriw release CR-1234',
  transactions: [
    {
      to: '0xC49f79CcdFbB3668400b7476A641268De81548b1',
      value: '0',
      data: '0x1234',
      operation: 0,
      description: 'UpgradeExecutor.executeCall(ArbOwner, setMaxTxGasLimit(60000000))'
    }
  ]
}

function normalizedManifestHash(value) {
  const normalized = normalizeManifest(value)
  const json = JSON.stringify(normalized, null, 2) + '\n'
  return createHash('sha256').update(json).digest('hex')
}

test('normalizes a single-call proposal manifest', () => {
  const normalized = normalizeManifest(manifest)
  assert.equal(normalized.chainId, '2886')
  assert.equal(normalized.upgradeExecutorAddress, manifest.upgradeExecutorAddress)
  assert.equal(normalized.transactions[0].operation, 0)
  assert.equal(normalized.transactions[0].value, '0')
})

test('rejects direct governance targets', () => {
  assert.throws(
    () => normalizeManifest({
      ...manifest,
      transactions: [{
        ...manifest.transactions[0],
        to: '0x0000000000000000000000000000000000000070'
      }]
    }),
    /direct governance targets are forbidden/
  )
})

test('pins every known L3 UpgradeExecutor address by chain ID', () => {
  const environments = [
    ['18417507517', '0xB5B4d7f7a32D86fF3bc270B864c7c06CE6F0BD78'],
    ['2885', '0xAc3516eF1E3658887198D192Cb0D7EcA07604943'],
    ['2886', '0xC49f79CcdFbB3668400b7476A641268De81548b1']
  ]

  for (const [chainId, upgradeExecutorAddress] of environments) {
    const normalized = normalizeManifest({
      ...manifest,
      chainId,
      upgradeExecutorAddress,
      transactions: [{ ...manifest.transactions[0], to: upgradeExecutorAddress }]
    })
    assert.equal(normalized.upgradeExecutorAddress, upgradeExecutorAddress)

    assert.throws(
      () => normalizeManifest({
        ...manifest,
        chainId,
        upgradeExecutorAddress: '0x1111111111111111111111111111111111111111',
        transactions: [{
          ...manifest.transactions[0],
          to: '0x1111111111111111111111111111111111111111'
        }]
      }),
      /requires L3 UpgradeExecutor/
    )
  }
})

test('rejects delegatecall children', () => {
  assert.throws(
    () => normalizeManifest({
      ...manifest,
      transactions: [{ ...manifest.transactions[0], operation: 1 }]
    }),
    /DELEGATECALL is forbidden/
  )
})

test('requires operation to be the integer zero', () => {
  assert.throws(
    () => normalizeManifest({
      ...manifest,
      transactions: [{ ...manifest.transactions[0], operation: '0' }]
    }),
    /integer 0/
  )
})

test('requires an explicit acknowledgement for batches', () => {
  assert.throws(
    () => normalizeManifest({
      ...manifest,
      transactions: [manifest.transactions[0], manifest.transactions[0]]
    }),
    /batchSafetyAcknowledgement/
  )

  const normalized = normalizeManifest({
    ...manifest,
    transactions: [manifest.transactions[0], manifest.transactions[0]],
    batchSafetyAcknowledgement: true
  })
  assert.equal(normalized.transactions.length, 2)
})

test('rejects odd-length calldata and invalid values', () => {
  assert.throws(
    () => normalizeManifest({
      ...manifest,
      transactions: [{ ...manifest.transactions[0], data: '0x123' }]
    }),
    /whole-byte hex/
  )
  assert.throws(
    () => normalizeManifest({
      ...manifest,
      transactions: [{ ...manifest.transactions[0], value: '-1' }]
    }),
    /non-negative decimal integer/
  )
})

test('validates the prepared proposal envelope', () => {
  const prepared = normalizePrepared({
    format: 'deriw.safe-proposal.v1',
    manifest,
    manifestSha256: normalizedManifestHash(manifest),
    safeTxHash: `0x${'ab'.repeat(32)}`,
    safeTransactionData: { to: manifest.transactions[0].to },
    safe: {
      owners: ['0x2222222222222222222222222222222222222222'],
      threshold: 3,
      onChainNonce: 7,
      serviceNextNonce: 7,
      safeVersion: '1.4.1',
      proposerRole: 'delegate'
    }
  })
  assert.equal(prepared.safe.threshold, 3)
  assert.equal(prepared.safe.proposerRole, 'delegate')
})

test('rejects a changed prepared manifest', () => {
  assert.throws(
    () => normalizePrepared({
      format: 'deriw.safe-proposal.v1',
      manifest,
      manifestSha256: '00'.repeat(32),
      safeTxHash: `0x${'ab'.repeat(32)}`,
      safeTransactionData: { to: manifest.transactions[0].to },
      safe: {
        owners: ['0x2222222222222222222222222222222222222222'],
        threshold: 3,
        onChainNonce: 7,
        serviceNextNonce: 7,
        safeVersion: '1.4.1',
        proposerRole: 'delegate'
      }
    }),
    /checksum does not match/
  )
})
