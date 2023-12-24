import { expect } from "chai";
import { ethers } from "hardhat";

describe("Counter", function () {
  async function deployCounter() {
    const Counter = await ethers.getContractFactory("Counter");
    const counter = await Counter.deploy();

    return counter;
  }

  it("inc", async function () {
    const counter = await deployCounter();
    await counter.incrementCounter();

    expect(await counter.getCount()).to.equal(1);
  });
});
