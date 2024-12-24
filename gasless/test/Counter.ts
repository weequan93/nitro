import { expect } from "chai";
import { ethers } from "hardhat";
import {
    Counter__factory,
  } from "../typechain-types";

describe("Counter", function () {
  async function deployCounter() {
    const Counter = await ethers.getContractFactory("Counter");
    const counter = await Counter.deploy();

    return counter;
  }

  it("inc", async function () {
    // const [a, signer] = await ethers.getSigners();
    // const counter = Counter__factory.connect("0xd5E9937897BD904Eb7736d7049Fd474a1141B244", signer);

    const counter = await deployCounter();
    console.log(counter.address);

    const initCount = await counter.getCount();

    const tx = await counter.incrementCounter();
    const res = await tx.wait();
    console.log(res.transactionHash);

    const count = await counter.getCount();
    expect(count.sub(initCount)).to.equal(1);
  });
});
