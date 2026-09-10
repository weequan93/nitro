import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { Interface } from 'ethers'
import { buildCalls, buildTransactionBuilder, PRODUCTION, verifyCalls } from './blacklist-calldata.mjs'

const address = '0x1111111111111111111111111111111111111111'
// Decode with the repository's Solidity interface, independently of the generator ABI.
const source = readFileSync(new URL('../../contracts/src/precompiles/DeriwBlacklist.sol', import.meta.url), 'utf8')
const blacklist = new Interface(source.match(/function [^;]+;/g).map(line => line.slice(0, -1)))
const executor = new Interface(['function executeCall(address,bytes)', 'function EXECUTOR_ROLE() view returns (bytes32)', 'function hasRole(bytes32,address) view returns (bool)'])
const safe = new Interface(['function getOwners() view returns (address[])', 'function getThreshold() view returns (uint256)'])

test('both lists use nested executor CALLs and decode against the Solidity interface', () => {
  const calls = buildCalls([address, address])
  assert.equal(calls.length, 2)
  for (const [index, call] of calls.entries()) {
    assert.equal(call.to, PRODUCTION.executor)
    assert.equal(call.operation, 0)
    assert.equal(call.value, '0')
    const [target, data] = executor.decodeFunctionData('executeCall', call.data)
    assert.equal(target, PRODUCTION.blacklist)
    const decoded = blacklist.parseTransaction({ data })
    assert.equal(decoded.name, index === 0 ? 'addBlacklistTxFrom' : 'addBlacklistTxTo')
    assert.equal(decoded.args[0], address)
  }
  const exported = buildTransactionBuilder(calls)
  assert.equal(exported.chainId, '2886')
  assert.equal(exported.meta.createdFromSafeAddress, PRODUCTION.safe)
  assert.match(exported.meta.description, /UNVERIFIED/)
  assert.equal(exported.transactions[0].data, calls[0].data)
  assert.equal(exported.transactions[0].contractMethod, null)
})

test('directions and invalid inputs', () => {
  assert.equal(buildCalls([address], 'from')[0].method, 'addBlacklistTxFrom')
  assert.equal(buildCalls([address], 'to')[0].method, 'addBlacklistTxTo')
  assert.equal(buildCalls([address, '0x2222222222222222222222222222222222222222']).length, 4)
  assert.throws(() => buildCalls([]), /--address/)
  assert.throws(() => buildCalls([address], 'remove'), /--direction/)
  assert.throws(() => buildCalls([address], 'both', 'invalid'), /--route/)
  assert.throws(() => buildCalls(['invalid']))
  for (const endpoint of [PRODUCTION.safe, PRODUCTION.executor, PRODUCTION.blacklist]) {
    assert.throws(() => buildCalls([endpoint]), /Refusing/)
  }
})

function mockRpc({ chain = '0xb46', role = true, revert = false, code = '0x1234', target = PRODUCTION.executor } = {}) {
  const simulations = []
  return {
    simulations,
    rpc: async (method, params) => {
      if (method === 'eth_chainId') return chain
      if (method === 'eth_blockNumber') return '0x100'
      assert.equal(params[1], '0x100')
      if (target === PRODUCTION.blacklist) {
        assert.notEqual(method === 'eth_getCode' ? params[0] : params[0].to, PRODUCTION.executor)
      }
      if (method === 'eth_getCode') return code
      assert.equal(method, 'eth_call')
      const [tx] = params
      if (tx.from) {
        assert.equal(tx.from, PRODUCTION.safe)
        assert.equal(tx.to, target)
        assert.equal(tx.value, '0x0')
        simulations.push(tx)
        if (revert) throw new Error('execution reverted')
        return '0x'
      }
      const abi = tx.to === PRODUCTION.safe ? safe : executor
      const decoded = abi.parseTransaction(tx)
      const results = { EXECUTOR_ROLE: '0x' + '11'.repeat(32), hasRole: role, getOwners: [address], getThreshold: 1n }
      return abi.encodeFunctionResult(decoded.name, [results[decoded.name]])
    }
  }
}

test('verification simulates exact calls from Safe at one pinned block', async () => {
  const mock = mockRpc()
  const calls = buildCalls([address])
  const result = await verifyCalls(calls, mock.rpc)
  assert.equal(result.block, '256')
  assert.deepEqual(mock.simulations.map(tx => tx.data), calls.map(tx => tx.data))
})

test('direct route preserves supplied calldata and verifies Safe authorization by simulation', async () => {
  const calls = buildCalls(['0xe76a03e00b10528e079070d39b52ec5788f6f3a8'], 'to', 'direct')
  assert.equal(calls[0].to, PRODUCTION.blacklist)
  assert.equal(calls[0].operation, 0)
  assert.equal(calls[0].data, '0x9e01565a000000000000000000000000e76a03e00b10528e079070d39b52ec5788f6f3a8')
  assert.match(buildTransactionBuilder(calls).meta.description, /Safe -> DeriwBlacklist/)
  const mock = mockRpc({ target: PRODUCTION.blacklist, role: false })
  await verifyCalls(calls, mock.rpc)
  assert.equal(mock.simulations.length, 1)
  await assert.rejects(verifyCalls(calls, mockRpc({ target: PRODUCTION.blacklist, revert: true }).rpc), /simulation failed/)
})

test('verification fails closed on wrong chain, missing code, missing role, or revert', async () => {
  for (const [options, message] of [
    [{ chain: '0xb45' }, /Wrong chain/],
    [{ code: '0x' }, /No contract code/],
    [{ role: false }, /EXECUTOR_ROLE/],
    [{ revert: true }, /simulation failed/]
  ]) {
    await assert.rejects(verifyCalls(buildCalls([address]), mockRpc(options).rpc), message)
  }
})
