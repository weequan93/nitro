import { ethers } from "hardhat";
import {
  Counter,
  Counter__factory,
  EntryPoint,
  EntryPoint__factory,
  NitroPaymaster,
  NitroPaymaster__factory,
} from "../typechain-types";
import { hexConcat, parseEther } from "ethers/lib/utils";
import { UserOperationStruct } from "../typechain-types/contracts/core/EntryPoint";
import { SignerWithAddress } from "@nomiclabs/hardhat-ethers/signers";
import { expect } from "chai";
import "@nomicfoundation/hardhat-chai-matchers";
import * as util from "../scripts/util";

describe("NitroPaymaster", function () {
  let counterCallData: string;
  let walletOwner: SignerWithAddress;
  let user: SignerWithAddress;
  let counter: Counter;
  let entryPoint: EntryPoint;
  let nitroPaymaster: NitroPaymaster;

  const stakeAmount = parseEther("0.11");
  const depositAmount = parseEther("0.01");
  const l1PricerFundsPoolAddress = "0xa4b00000000000000000000000000000000000f6";

  const entryPointAddr = "0xfE3B201e062d6f78dCaB056a1c3e8f28137160cd";
  const nitroPaymasterAddr = "0xa32a4C0E6fc8CF546727021DA8bc13975122695A";
  const counterAddr = "0xB94BCBEC76132814512665534E81c719Cd18fEbD";

  beforeEach(async function () {
    const [_walletOwner, _user] = await ethers.getSigners();
    walletOwner = _walletOwner;
    user = _user;

    console.log(
      `wallet owner: ${
        walletOwner.address
      }, balance: ${await walletOwner.getBalance()}`
    );
    console.log(`User: ${user.address}`);

    counter = Counter__factory.connect(counterAddr, user);
    // counter = await util.deployCounter();
    console.log(`Counter contract: ${counter.address}`);

    const testTx = await counter.populateTransaction.incrementCounter();
    counterCallData = testTx.data ?? "";

    entryPoint = EntryPoint__factory.connect(entryPointAddr, walletOwner);
    // entryPoint = await util.deployEntryPoint(l1PricerFundsPoolAddress, 1, 1);
    console.log(`EntryPoint contract: ${entryPoint.address}`);

    nitroPaymaster = NitroPaymaster__factory.connect(
      nitroPaymasterAddr,
      walletOwner
    );
    // nitroPaymaster = await util.deployNitroPaymaster(entryPoint.address);
    console.log(`Nitro Paymaster contract: ${nitroPaymaster.address}`);

    const deposit = await entryPoint.balanceOf(nitroPaymaster.address);
    if (deposit < depositAmount) {
      await nitroPaymaster.addStake(1, {
        value: stakeAmount,
      });
      console.log(`Add stake for paymaster: ${stakeAmount}`);
      await entryPoint.depositTo(nitroPaymaster.address, {
        value: depositAmount,
      });
      console.log(`Deposit to entrypoint for paymaster: ${depositAmount}`);
    }

    const balance = await entryPoint.balanceOf(nitroPaymaster.address);
    console.log(`Paymaster's deposit: ${balance}`);
    const stake = await entryPoint.getDepositInfo(nitroPaymaster.address);
    console.log(`Paymaster's stake: ${stake.staked}, ${stake.deposit}`);

    await nitroPaymaster.addAvailAddr(counter.address);
    console.log(
      `Add available target contract to paymaster: ${counter.address}`
    );
  });

  describe("#NitroPaymaster", () => {
    it("make call to a valid address", async () => {
      const zeroAddr = "0x" + "00".repeat(20);

      // Mock UserOp
      const userOp: UserOperationStruct = {
        callContract: counter.address,
        callData: counterCallData,
        callGasLimit: 100000,
        verificationGasLimit: 100000,
        preVerificationGas: 60000,
        maxFeePerGas: 100000000,
        maxPriorityFeePerGas: 0,
        paymasterAndData: hexConcat([nitroPaymaster.address, "0x1234"]),
      };

      const callGasLimit = await entryPoint
        .connect(zeroAddr)
        .callStatic.simulateCallContract(userOp);
      userOp.callGasLimit = callGasLimit;
      console.log(`callGasLimit: ${callGasLimit}`);

      const { preOpGas } = await entryPoint
        .connect(zeroAddr)
        .callStatic.simulateValidation(userOp, { gasPrice: 0 });
      const verificationGasLimit = preOpGas.mul(1);
      console.log(`verificationGasLimit: ${verificationGasLimit}`);

      userOp.verificationGasLimit = verificationGasLimit;
      const total = await entryPoint.estimateGas.handleOp(userOp, {
        gasPrice: 0,
      });
      userOp.preVerificationGas = total
        .sub(callGasLimit)
        .sub(verificationGasLimit);
      console.log(`userOp.preVerificationGas: ${userOp.preVerificationGas}`);

      // Print init state
      const initCount = await counter.getCount();
      console.log(`init count: ${initCount}`);
      const initDeposit = await entryPoint.balanceOf(nitroPaymaster.address);
      console.log(`Paymaster's init deposit: ${initDeposit}`);
      const initPaymasterBalance = await ethers.provider.getBalance(
        nitroPaymaster.address
      );
      console.log(`Paymaster's init balance: ${initPaymasterBalance}`);
      const initEntryPointBalance = await ethers.provider.getBalance(
        entryPoint.address
      );
      console.log(`EntryPoint's init balance: ${initEntryPointBalance}`);

      // Send tx
      const tx = await entryPoint
        .connect(user)
        .handleOp(userOp, { gasLimit: total, gasPrice: 0 });
      const receipt = await tx.wait();
      console.log(`tx.hash: ${receipt.transactionHash}`);
      expect(receipt.status).equal(1);

      // Check state changed
      const count = await counter.getCount();
      console.log(`count: ${count}`);
      expect(count.sub(initCount)).to.equal(1);

      const deposit = await entryPoint.balanceOf(nitroPaymaster.address);
      console.log(`NitroPaymaster's deposit: ${deposit}`);
      // todo: it seems hard to make the gas cost equals to the real income on boundler
      // we only check if the income is larger than the real cost
      // in another word, we charge more.
      expect(receipt.gasUsed.mul(userOp.maxFeePerGas as string)).to.lt(
        initDeposit.sub(deposit)
      );

      const paymasterBalance = await ethers.provider.getBalance(
        nitroPaymaster.address
      );
      const entryPointBalance = await ethers.provider.getBalance(
        entryPoint.address
      );
      console.log(`paymaster's balance: ${paymasterBalance}`);
      console.log(`entrypoint's balance: ${entryPointBalance}`);
    });
  });
});
