import {
  deployCounter,
  deployEntryPoint,
  deployNitroPaymaster,
} from "../scripts/util";
import { parseEther } from "ethers/lib/utils";

async function main() {
  const l1PricerFundsPoolAddress = "0xa4b00000000000000000000000000000000000f6"; // ?
  const entryPoint = await deployEntryPoint(l1PricerFundsPoolAddress, 1, 1);
  console.log(`Deploy EntryPoint contract: ${entryPoint.address}`);

  const nitroPaymaster = await deployNitroPaymaster(entryPoint.address);
  console.log(`Nitro Paymaster contract: ${nitroPaymaster.address}`);

  const depositAmount = parseEther("0.01");
  await entryPoint.depositTo(nitroPaymaster.address, {
    value: depositAmount,
  });
  console.log(`Deposit to entrypoint for nitro paymaster, ${depositAmount}`);
  const deposit = await entryPoint.balanceOf(nitroPaymaster.address);
  console.log(`Paymasetr's deposit: ${deposit}`);

  const stakeAmount = parseEther("0.011");
  await nitroPaymaster.addStake(99999999, {
    value: stakeAmount,
  });
  console.log(`Add stake for nitroPaymaster: ${stakeAmount}`);

  const counter = await deployCounter();
  console.log(`Counter contract: ${counter.address}`);

  await nitroPaymaster.addAvailAddr(counter.address);
  console.log(
    `Add available target contract to nitroPaymaster: ${counter.address}`
  );
}

// We recommend this pattern to be able to use async/await everywhere
// and properly handle errors.
main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
