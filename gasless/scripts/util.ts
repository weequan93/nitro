import { BigNumber, BigNumberish, Wallet } from "ethers";
import { arrayify, keccak256 } from "ethers/lib/utils";
import { ethers } from "hardhat";
import { Counter, EntryPoint, NitroPaymaster } from "../typechain-types";

let counter = 0;

export async function deployEntryPoint(
  l1PricerFundsPoolAddress: string,
  paymasterStake: BigNumberish,
  unstakeDelaySecs: BigNumberish
): Promise<EntryPoint> {
  const Contract = await ethers.getContractFactory("EntryPoint");
  const contract = await Contract.deploy(
    l1PricerFundsPoolAddress,
    paymasterStake,
    unstakeDelaySecs
  );
  return await contract.deployed();
}

export async function deployNitroPaymaster(
  entryPointAddress: string
): Promise<NitroPaymaster> {
  const Contract = await ethers.getContractFactory("NitroPaymaster");
  const contract = await Contract.deploy(entryPointAddress);
  return await contract.deployed();
}

export async function deployCounter(): Promise<Counter> {
  const Contract = await ethers.getContractFactory("Counter");
  const contract = await Contract.deploy();
  return await contract.deployed();
}

// create non-random account, so gas calculations are deterministic
export function createNonRandomWallet(): Wallet {
  const privateKey = keccak256(
    Buffer.from(arrayify(BigNumber.from(++counter)))
  );
  return new ethers.Wallet(privateKey, ethers.provider);
}
