#!/usr/bin/env node
"use strict";

const path = require("path");

function loadEthers() {
  try {
    return require("ethers");
  } catch (_) {
    const repoRoot = path.resolve(__dirname, "../../..");
    return require(path.join(repoRoot, "contracts/node_modules/ethers"));
  }
}

const { ethers } = loadEthers();

const required = ["PARENT_RPC", "ROLLUP", "LOCAL_L2_RPC"];
for (const name of required) {
  if (!process.env[name]) {
    console.error(`missing required env ${name}`);
    process.exit(1);
  }
}

const START_NODE = Number(process.env.START_NODE || "35792");
const END_NODE = Number(process.env.END_NODE || "35000");
const CONFIRM_SEND_ROOT = process.env.CONFIRM_SEND_ROOT !== "false";

const executionStateComponents = [
  {
    name: "globalState",
    type: "tuple",
    components: [
      { name: "bytes32Vals", type: "bytes32[2]" },
      { name: "u64Vals", type: "uint64[2]" },
    ],
  },
  { name: "machineStatus", type: "uint8" },
];

const rollupAbi = [
  "function getNodeCreationBlockForLogLookup(uint64 nodeNum) view returns (uint256)",
  {
    type: "event",
    name: "NodeCreated",
    anonymous: false,
    inputs: [
      { name: "nodeNum", type: "uint64", indexed: true },
      { name: "parentNodeHash", type: "bytes32", indexed: true },
      { name: "nodeHash", type: "bytes32", indexed: true },
      { name: "executionHash", type: "bytes32", indexed: false },
      {
        name: "assertion",
        type: "tuple",
        indexed: false,
        components: [
          { name: "beforeState", type: "tuple", components: executionStateComponents },
          { name: "afterState", type: "tuple", components: executionStateComponents },
          { name: "numBlocks", type: "uint64" },
        ],
      },
      { name: "afterInboxBatchAcc", type: "bytes32", indexed: false },
      { name: "wasmModuleRoot", type: "bytes32", indexed: false },
      { name: "inboxMaxCount", type: "uint256", indexed: false },
    ],
  },
];

function normalizeHex(value) {
  return (value || "").toLowerCase();
}

function asNumber(value) {
  return ethers.BigNumber.from(value).toNumber();
}

async function main() {
  const parentProvider = new ethers.providers.JsonRpcProvider(process.env.PARENT_RPC);
  const localProvider = new ethers.providers.JsonRpcProvider(process.env.LOCAL_L2_RPC);
  const iface = new ethers.utils.Interface(rollupAbi);
  const rollup = new ethers.Contract(process.env.ROLLUP, rollupAbi, parentProvider);
  const topic0 = iface.getEventTopic("NodeCreated");

  const step = START_NODE >= END_NODE ? -1 : 1;
  for (let nodeNum = START_NODE; step < 0 ? nodeNum >= END_NODE : nodeNum <= END_NODE; nodeNum += step) {
    const creationBlock = await rollup.getNodeCreationBlockForLogLookup(nodeNum);
    if (creationBlock.isZero()) {
      console.log(`${nodeNum} no creation block`);
      continue;
    }

    const nodeTopic = ethers.utils.hexZeroPad(ethers.utils.hexlify(nodeNum), 32);
    const logs = await parentProvider.getLogs({
      address: process.env.ROLLUP,
      fromBlock: creationBlock.toNumber(),
      toBlock: creationBlock.toNumber(),
      topics: [topic0, nodeTopic],
    });

    if (logs.length === 0) {
      console.log(`${nodeNum} no NodeCreated log at l1Block=${creationBlock.toString()}`);
      continue;
    }

    const parsed = iface.parseLog(logs[0]);
    const after = parsed.args.assertion.afterState.globalState;
    const blockHash = after.bytes32Vals[0];
    const sendRoot = after.bytes32Vals[1];
    const batch = asNumber(after.u64Vals[0]);
    const posInBatch = asNumber(after.u64Vals[1]);

    const block = await localProvider.send("eth_getBlockByHash", [blockHash, false]);
    if (!block) {
      console.log(`${nodeNum} missing hash=${blockHash} batch=${batch} pos=${posInBatch}`);
      continue;
    }

    const localSendRoot = normalizeHex(block.sendRoot || block.extraData);
    const sendRootOk = localSendRoot === normalizeHex(sendRoot);
    console.log(
      `${nodeNum} FOUND l2Block=${ethers.BigNumber.from(block.number).toString()} ` +
        `hash=${blockHash} batch=${batch} pos=${posInBatch} sendRootOk=${sendRootOk}`
    );

    if (!CONFIRM_SEND_ROOT || sendRootOk) {
      return;
    }
  }

  console.log(`No matching block hash found from ${START_NODE} to ${END_NODE}`);
  process.exitCode = 2;
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
