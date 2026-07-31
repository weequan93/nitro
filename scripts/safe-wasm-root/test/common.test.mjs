import assert from 'node:assert/strict'
import test from 'node:test'

import {
  expectedMetaTransaction,
  loadConfig,
  parseSafeAccountConfig,
  safeExecutorGrantData,
  validateTransactionRecord,
} from '../common.mjs'
import {
  assertExactSafeConfiguration,
  checkedAddress,
  deriwExecutorRevokeTransaction,
  deriwSafeGrantData,
  loadDeriwAdminConfig,
} from '../deriw-admin-common.mjs'
import {
  createSingleCallSafeTransaction,
  expectedArbosTransaction,
  loadArbosConfig,
  signNativeSafeHash,
  validateAndEncodeRequestSignatures,
} from '../arbos-common.mjs'

process.env.L1_RPC = 'http://127.0.0.1:8545'
process.env.SAFE_ADDRESS = '0x0000000000000000000000000000000000000001'
process.env.ROLLUP = '0xb6a39f55E4C4397FE799BeDCc16fFa895950CFF9'
process.env.UPGRADE_EXECUTOR = '0x678815f2c63466f557024d8cce25baeeb4a23359'
process.env.DERIW_RPC = 'http://127.0.0.1:8545'
process.env.DERIW_CHAIN_ID = '2885'
process.env.DERIW_SAFE = '0xE5C8e6dAbE8dA8D90F0AE3d4543E930833A0e9Ec'
process.env.DERIW_UPGRADE_EXECUTOR =
  '0xAc3516eF1E3658887198D192Cb0D7EcA07604943'
process.env.ARBOS_TARGET_VERSION = '60'
process.env.ARBOS_PUBLIC_VERSION_OFFSET = '55'
process.env.ACTIVATION_TIMESTAMP = '1785474000'

const expectedCalldata =
  '0xbca8c7b5000000000000000000000000b6a39f55e4c4397fe799bedcc16ffa895950cff90000000000000000000000000000000000000000000000000000000000000040000000000000000000000000000000000000000000000000000000000000002489384960121d685e2fdb0e3291592d6b90bd70d503951335d19d96455448eb7a14d1742100000000000000000000000000000000000000000000000000000000'
const expectedGrantCalldata =
  '0xbca8c7b5000000000000000000000000678815f2c63466f557024d8cce25baeeb4a23359000000000000000000000000000000000000000000000000000000000000004000000000000000000000000000000000000000000000000000000000000000442f2ff15dd8aa0f3194971a2a116679f7c2090f6939c8d4e01a2a8d7e41d55e5351469e63000000000000000000000000000000000000000000000000000000000000000100000000000000000000000000000000000000000000000000000000'
const safeTxHash =
  '0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
const expectedArbosCalldata =
  '0xbca8c7b5000000000000000000000000000000000000000000000000000000000000007000000000000000000000000000000000000000000000000000000000000000400000000000000000000000000000000000000000000000000000000000000044e388b381000000000000000000000000000000000000000000000000000000000000003c000000000000000000000000000000000000000000000000000000006a6c2bd000000000000000000000000000000000000000000000000000000000'

function serviceTransaction(config, overrides = {}) {
  const expected = expectedMetaTransaction(config)
  return {
    safe: config.safeAddress,
    to: expected.to,
    value: expected.value,
    data: expected.data,
    operation: expected.operation,
    gasToken: '0x0000000000000000000000000000000000000000',
    safeTxGas: '0',
    baseGas: '0',
    gasPrice: '0',
    refundReceiver: '0x0000000000000000000000000000000000000000',
    nonce: '7',
    safeTxHash,
    isExecuted: false,
    signatures: [],
    ...overrides,
  }
}

const fakeSafeKit = {
  async createTransaction(args) {
    return args
  },
  async getTransactionHash() {
    return safeTxHash
  },
}

test('encodes the exact nested Rollup update calldata', () => {
  const config = loadConfig()
  const transaction = expectedMetaTransaction(config)

  assert.equal(transaction.to, config.upgradeExecutor)
  assert.equal(transaction.value, '0')
  assert.equal(transaction.operation, 0)
  assert.equal(transaction.data, expectedCalldata)
})

test('encodes the exact self-call that grants EXECUTOR_ROLE to the Safe', () => {
  assert.equal(safeExecutorGrantData(loadConfig()), expectedGrantCalldata)
})

test('parses and checks the Safe owner and threshold configuration', () => {
  const parsed = parseSafeAccountConfig(
    [
      '0x35b3ac4003e1AfeE7601C190DB4f039fCb1BbcB5',
      '0xa1698F44D70632BfE448804378DA373C55eE8476',
    ].join(','),
    '2',
  )

  assert.deepEqual(parsed, {
    owners: [
      '0x35b3ac4003e1AfeE7601C190DB4f039fCb1BbcB5',
      '0xa1698F44D70632BfE448804378DA373C55eE8476',
    ],
    threshold: 2,
  })
})

test('rejects duplicate Safe owners', () => {
  assert.throws(
    () =>
      parseSafeAccountConfig(
        [
          '0x35b3ac4003e1AfeE7601C190DB4f039fCb1BbcB5',
          '0x35b3ac4003e1afee7601c190db4f039fcb1bbcb5',
        ].join(','),
        '1',
      ),
    /duplicate/,
  )
})

test('rejects a Safe threshold greater than its owner count', () => {
  assert.throws(
    () =>
      parseSafeAccountConfig(
        '0x35b3ac4003e1AfeE7601C190DB4f039fCb1BbcB5',
        '2',
      ),
    /exceeds/,
  )
})

test('accepts a request transaction only when its payload and hash match', async () => {
  const config = loadConfig()
  await assert.doesNotReject(
    validateTransactionRecord(serviceTransaction(config), fakeSafeKit, config),
  )
})

test('rejects modified calldata before signing', async () => {
  const config = loadConfig()
  const modified = `${expectedCalldata.slice(0, -1)}1`

  await assert.rejects(
    validateTransactionRecord(
      serviceTransaction(config, { data: modified }),
      fakeSafeKit,
      config,
    ),
    /Calldata mismatch/,
  )
})

test('rejects a transaction for another Safe', async () => {
  const config = loadConfig()

  await assert.rejects(
    validateTransactionRecord(
      serviceTransaction(config, {
        safe: '0x0000000000000000000000000000000000000002',
      }),
      fakeSafeKit,
      config,
    ),
    /Safe mismatch/,
  )
})

test('encodes the Deriw UpgradeExecutor self-call for the Deriw Safe', () => {
  const config = loadDeriwAdminConfig()
  const calldata = deriwSafeGrantData(config)

  assert.equal(calldata.slice(0, 10), '0xbca8c7b5')
  assert.match(calldata.toLowerCase(), /ac3516ef1e3658887198d192cb0d7eca07604943/)
  assert.match(calldata.toLowerCase(), /e5c8e6dabe8da8d90f0ae3d4543e930833a0e9ec/)
})

test('accepts only the pinned Deriw Safe owner set and threshold', () => {
  const config = loadDeriwAdminConfig()

  assert.doesNotThrow(() =>
    assertExactSafeConfiguration(
      ['0xa1698F44D70632BfE448804378DA373C55eE8476'],
      1n,
      config,
    ),
  )
  assert.throws(
    () =>
      assertExactSafeConfiguration(
        ['0x35b3ac4003e1AfeE7601C190DB4f039fCb1BbcB5'],
        1n,
        config,
      ),
    /owners do not match/,
  )
})

test('encodes the nested Deriw executor revocation through the Safe role', () => {
  const config = loadDeriwAdminConfig()
  const oldExecutor = checkedAddress(
    'OLD_DERIW_EXECUTOR',
    '0xa1698F44D70632BfE448804378DA373C55eE8476',
  )
  const transaction = deriwExecutorRevokeTransaction(config, oldExecutor)

  assert.equal(transaction.to, config.upgradeExecutor)
  assert.equal(transaction.value, '0')
  assert.equal(transaction.operation, 0)
  assert.equal(transaction.data.slice(0, 10), '0xbca8c7b5')
  assert.match(
    transaction.data.toLowerCase(),
    /a1698f44d70632bfe448804378da373c55ee8476/,
  )
})

test('encodes the exact direct Deriw Safe ArbOS scheduling call', () => {
  const config = loadArbosConfig()
  const transaction = expectedArbosTransaction(config)

  assert.equal(transaction.to, config.upgradeExecutor)
  assert.equal(transaction.value, '0')
  assert.equal(transaction.operation, 0)
  assert.equal(transaction.data, expectedArbosCalldata)
  assert.equal(config.targetVersion, 60n)
  assert.equal(config.targetVersion + config.publicVersionOffset, 115n)
})

test('builds a single-call Safe transaction without a MultiSend deployment', () => {
  const config = loadArbosConfig()
  const expected = expectedArbosTransaction(config)
  const transaction = createSingleCallSafeTransaction(expected, { nonce: 7 })

  assert.equal(transaction.data.to, config.upgradeExecutor)
  assert.equal(transaction.data.data, expectedArbosCalldata)
  assert.equal(transaction.data.operation, 0)
  assert.equal(transaction.data.nonce, 7)
  assert.equal(transaction.data.safeTxGas, '0')
})

test('creates and verifies a native Safe eth_sign signature', async () => {
  const previousKey = process.env.PK
  process.env.PK =
    '0x0000000000000000000000000000000000000000000000000000000000000001'
  try {
    const config = loadArbosConfig({ needsSigner: true })
    const hash =
      '0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
    const signature = await signNativeSafeHash(config, hash)

    assert.match(signature, /^0x[0-9a-f]{130}$/)
    assert.ok(['1f', '20'].includes(signature.slice(-2)))
    assert.equal(
      await validateAndEncodeRequestSignatures(
        {
          signatures: [
            { owner: config.signerAddress, signature },
          ],
        },
        [config.signerAddress],
        hash,
      ),
      signature,
    )
  } finally {
    if (previousKey === undefined) {
      delete process.env.PK
    } else {
      process.env.PK = previousKey
    }
  }
})
