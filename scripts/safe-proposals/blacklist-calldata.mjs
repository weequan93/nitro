#!/usr/bin/env node

import { writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { pathToFileURL } from 'node:url'
import { parseArgs } from 'node:util'
import { getAddress, Interface, ZeroAddress } from 'ethers'

export const PRODUCTION = Object.freeze({
  rpc: 'https://rpc.deriw.com',
  chainId: '2886',
  safe: '0x2F996bC558818D33DE37aF36Bee7de24bA3Fc4dF',
  executor: '0xC49f79CcdFbB3668400b7476A641268De81548b1',
  blacklist: getAddress('0x00000000000000000000000000000000000007ec')
})

const blacklistAbi = new Interface([
  'function addBlacklistTxFrom(address addr)',
  'function addBlacklistTxTo(address addr)'
])
const executorAbi = new Interface([
  'function executeCall(address target, bytes targetCallData) payable',
  'function EXECUTOR_ROLE() view returns (bytes32)',
  'function hasRole(bytes32 role, address account) view returns (bool)'
])
const safeAbi = new Interface([
  'function getOwners() view returns (address[])',
  'function getThreshold() view returns (uint256)'
])

function check(condition, message) {
  if (!condition) throw new Error(message)
}

export function buildCalls(addresses, direction = 'both', route = 'executor') {
  check(['from', 'to', 'both'].includes(direction), '--direction must be from, to, or both')
  check(['executor', 'direct'].includes(route), '--route must be executor or direct')
  check(addresses.length > 0, 'Supply at least one --address ADDRESS')
  const normalized = [...new Set(addresses.map(address => getAddress(address)))]
  for (const address of normalized) {
    check(![ZeroAddress, PRODUCTION.safe, PRODUCTION.executor, PRODUCTION.blacklist].includes(address),
      `Refusing to blacklist zero or a governance endpoint: ${address}`)
  }
  const methods = direction === 'both'
    ? ['addBlacklistTxFrom', 'addBlacklistTxTo']
    : [direction === 'from' ? 'addBlacklistTxFrom' : 'addBlacklistTxTo']
  return normalized.flatMap(address => methods.map(method => {
    const innerData = blacklistAbi.encodeFunctionData(method, [address])
    return {
      address,
      method,
      innerData,
      to: route === 'direct' ? PRODUCTION.blacklist : PRODUCTION.executor,
      value: '0',
      operation: 0,
      data: route === 'direct' ? innerData : executorAbi.encodeFunctionData('executeCall', [PRODUCTION.blacklist, innerData])
    }
  }))
}

export function buildTransactionBuilder(calls, verified = false) {
  const path = calls.every(call => call.to === PRODUCTION.blacklist)
    ? 'Safe -> DeriwBlacklist'
    : 'Safe -> UpgradeExecutor.executeCall -> DeriwBlacklist'
  return {
    version: '1.0',
    chainId: PRODUCTION.chainId,
    createdAt: Date.now(),
    meta: {
      name: 'Deriw production: add blacklist addresses',
      description: verified
        ? `${path}. Each child eth_call passed; full Safe batch/signatures were not simulated.`
        : `OFFLINE / UNVERIFIED. ${path}.`,
      txBuilderVersion: '1.16.5',
      createdFromSafeAddress: PRODUCTION.safe,
      createdFromOwnerAddress: ''
    },
    transactions: calls.map(({ to, value, data }) => ({
      to, value, data, contractMethod: null, contractInputsValues: null
    }))
  }
}

// Only read-only RPC methods are used. No wallet, key, signing, or submission.
export async function verifyCalls(calls, rpc) {
  const chainId = BigInt(await rpc('eth_chainId', []))
  check(chainId === BigInt(PRODUCTION.chainId), `Wrong chain: expected 2886, received ${chainId}`)
  const block = await rpc('eth_blockNumber', [])
  const usesExecutor = calls.some(call => call.to === PRODUCTION.executor)
  for (const address of usesExecutor ? [PRODUCTION.safe, PRODUCTION.executor] : [PRODUCTION.safe]) {
    check(await rpc('eth_getCode', [address, block]) !== '0x', `No contract code at ${address}`)
  }
  async function read(to, abi, method, args = []) {
    const data = abi.encodeFunctionData(method, args)
    return abi.decodeFunctionResult(method, await rpc('eth_call', [{ to, data }, block]))[0]
  }
  if (usesExecutor) {
    const role = await read(PRODUCTION.executor, executorAbi, 'EXECUTOR_ROLE')
    check(await read(PRODUCTION.executor, executorAbi, 'hasRole', [role, PRODUCTION.safe]),
      'Production Safe does not have EXECUTOR_ROLE')
  }
  const owners = await read(PRODUCTION.safe, safeAbi, 'getOwners')
  const threshold = await read(PRODUCTION.safe, safeAbi, 'getThreshold')
  check(threshold > 0n && threshold <= BigInt(owners.length), 'Invalid Safe signing threshold')
  for (const call of calls) {
    try {
      // Simulate the selected route with msg.sender = Safe. The precompile
      // checks authorization of the Safe (direct) or executor (wrapped).
      const result = await rpc('eth_call', [{
        from: PRODUCTION.safe, to: call.to, value: '0x0', data: call.data
      }, block])
      check(result === '0x', `Unexpected call return data: ${result}`)
    } catch (error) {
      throw new Error(`${call.method}(${call.address}) simulation failed: ${error.message}`)
    }
  }
  return { block: BigInt(block).toString(), threshold: threshold.toString(), owners: [...owners] }
}

async function main() {
  const { values } = parseArgs({ options: {
    address: { type: 'string', multiple: true },
    direction: { type: 'string', default: 'both' },
    route: { type: 'string', default: 'executor' },
    out: { type: 'string' },
    'rpc-url': { type: 'string', default: PRODUCTION.rpc },
    offline: { type: 'boolean', default: false },
    help: { type: 'boolean', short: 'h' }
  } })
  if (values.help) {
    console.log(`Usage: node blacklist-calldata.mjs --address ADDRESS [--address ADDRESS ...]
  [--direction from|to|both] [--route executor|direct] [--out blacklist.safe.json]
  [--rpc-url https://rpc.deriw.com] [--offline]

Defaults to both lists through the executor; verifies/simulates on production (chain 2886).
--route direct targets DeriwBlacklist directly from the Safe, which must be authorized.
--offline generates UNVERIFIED calldata without contacting RPC.
--out exports Safe Transaction Builder JSON; existing files are not overwritten.
Prints the fields for each CALL. Does not sign, propose, or broadcast.`)
    return
  }
  const calls = buildCalls(values.address ?? [], values.direction, values.route)
  let verification
  if (!values.offline) {
    let id = 0
    const rpc = async (method, params) => {
      const response = await fetch(values['rpc-url'], {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ jsonrpc: '2.0', id: ++id, method, params }),
        signal: AbortSignal.timeout(20_000)
      })
      check(response.ok, `RPC HTTP ${response.status}`)
      const body = await response.json()
      check(!body.error, `RPC ${method}: ${JSON.stringify(body.error)}`)
      check(body.result !== undefined, `RPC ${method}: missing result`)
      return body.result
    }
    verification = await verifyCalls(calls, rpc)
  }
  const batch = buildTransactionBuilder(calls, Boolean(verification))
  if (values.out) writeFileSync(values.out, JSON.stringify(batch, null, 2) + '\n', { flag: 'wx' })
  console.log(`Network: Deriw production (chain ${PRODUCTION.chainId})\nSafe: ${PRODUCTION.safe}`)
  console.log(verification
    ? `Verified at block ${verification.block}: ${verification.threshold}-of-${verification.owners.length} Safe; each ${values.route} call simulated successfully.`
    : 'OFFLINE / UNVERIFIED: no on-chain checks or simulations performed.')
  for (const [index, call] of calls.entries()) {
    console.log(`\nTransaction ${index + 1}: ${call.method}(${call.address})
To (contract address): ${call.to}
Value: ${call.value}
Operation: CALL (0)
Data (hex encoded): ${call.data}`)
    if (values.route === 'executor') console.log(`Decoded executor method: executeCall(address,bytes)
  target: ${PRODUCTION.blacklist}
  targetCallData: ${call.innerData}`)
  }
  if (values.out) console.log(`\nTransaction Builder JSON: ${resolve(values.out)}`)
  console.log('\nUse https://safe.deriw.com with the Safe above. For manual entry, add each transaction in Transaction Builder using Custom data. For JSON, import the exported file. Review every child; the UI wraps multiple calls in MultiSend. Full Safe execution, signatures, guards, and batch behavior were not simulated. No transaction was submitted.')
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  main().catch(error => {
    console.error(`Error: ${error.message}`)
    process.exitCode = 1
  })
}
