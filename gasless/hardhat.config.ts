import { HardhatUserConfig } from "hardhat/config";
import "@nomicfoundation/hardhat-toolbox";
import "hardhat-gas-reporter";
import "solidity-coverage";

require("hardhat-contract-sizer");

import "solidity-coverage";

const TEST_PK1 =
  "0xe9dca69cafab0e953d9ee596f51cbcb8cf20b4ae017d5a7547330aa3eb1886e1";
const TEST_PK2 =
  "0x2dc6374a2238e414e51874f514b0fa871f8ce0eb1e7ecaa0aed229312ffc91b0";

const config: HardhatUserConfig = {
  solidity: "0.8.19",
  networks: {
    orbit: {
      chainId: 57084336371,
      url: "http://127.0.0.1:8449",
      accounts: [TEST_PK1, TEST_PK2],
    },
  },
};

export default config;
