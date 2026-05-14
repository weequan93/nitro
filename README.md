<br />
<p align="center">
  <a href="https://deriw.com/">
    <picture>
      <source media="(prefers-color-scheme: dark)" srcset="/assets/deriw/logo_dark.png" width="280">
      <img alt="Deriw" src="/assets/deriw/logo_light.png" width="280">
    </picture>
  </a>
  <p align="center">
    <a href="https://deriw.com/"><strong>Deriw Chain Next Generation Ethereum L3 Decentralized
Perpetual Exchange »</strong></a>
    <br />
  </p>
</p>

## About Deriw Chain

<picture>
    <source media="(prefers-color-scheme: dark)" srcset="/assets/deriw/logo_dark.png" width="280">
    <img alt="Deriw" src="/assets/deriw/logo_light.png" width="140">
</picture>

Deriw Chain is the latest iteration of the Arbitrum technology. It is a fully integrated, complete
layer 3 optimistic rollup system, including fraud proofs, the sequencer, the token bridges,
advanced calldata compression, gas-less transaction, permissioned sub-account and more.

This repository, named Deriw Chain, is part of the Deriw infrastructure. It contains the codebase for various components, including:

Deriw Sequencer: Deriw Chain sequncer service
Deriw Node: Deriw Chain full node
Deriw DAC: Data Availability Committee (DAC) which providing data availability to Deriw Chain
Deriw Validator: Fault prove module of Deriw Chain

The Nitro stack is built on several innovations. At its core is a new prover, which can do Arbitrum’s classic
interactive fraud proofs over WASM code. That means the L2 Arbitrum engine can be written and compiled using
standard languages and tools, replacing the custom-designed language and compiler used in previous Arbitrum
versions. In normal execution,
validators and nodes run the Nitro engine compiled to native code, switching to WASM if a fraud proof is needed.
We compile the core of Geth, the EVM engine that practically defines the Ethereum standard, right into Arbitrum.
So the previous custom-built EVM emulator is replaced by Geth, the most popular and well-supported Ethereum client.

The last piece of the stack is a slimmed-down version of our ArbOS component, rewritten in Go, which provides the
rest of what’s needed to run an L2 chain: things like cross-chain communication, and a new and improved batching
and compression system to minimize L1 costs.

Essentially, Nitro runs Geth at layer 2 on top of Ethereum, and can prove fraud over the core engine of Geth
compiled to WASM.

Arbitrum One successfully migrated from the Classic Arbitrum stack onto Nitro on 8/31/22. (See [state migration](https://developer.arbitrum.io/migration/state-migration) and [dapp migration](https://developer.arbitrum.io/migration/dapp_migration) for more info).

## Building `nitro-private`

`nitro-private` routes a subset of submodules (`go-ethereum`, `wasmer`)
through `-private` forks. First-time setup:

```sh
git clone git@github.com:OffchainLabs/nitro-private.git   # no need for --recurse-submodules
cd nitro-private
make init-submodules
make check-submodules
```

See [`docs/private-submodules.md`](./docs/private-submodules.md) for the
full workflow, including branch switching, the pre-push guard hook, and
the CI counterpart.

## License

Nitro is currently licensed under a [Business Source License](./LICENSE.md), similar to our friends at Uniswap and Aave, with an "Additional Use Grant" to ensure that everyone can have full comfort using and running nodes on all public Arbitrum chains.

The Additional Use Grant also permits the deployment of the Nitro software, in a permissionless fashion and without cost, as a new blockchain provided that the chain settles to either Arbitrum One or Arbitrum Nova.

For those that prefer to deploy the Nitro software either directly on Ethereum (i.e. an L2) or have it settle to another Layer-2 on top of Ethereum, the [Arbitrum Expansion Program (the "AEP")](https://docs.arbitrum.foundation/aep/ArbitrumExpansionProgramTerms.pdf) was recently established. The AEP allows for the permissionless deployment in the aforementioned fashion provided that 10% of net revenue (as more fully described in the AEP) is contributed back to the Arbitrum community in accordance with the requirements of the AEP.

For the chain deployment information see [docs](https://docs.deriw.com/)

## Contact

Telegram - [Deriw](https://t.me/deriwfinance) [Deriw](https://t.me/deriwofficial)

X - [Deriw](https://x.com/DeriWOfficial)

Discord - [Deriw](https://discord.com/invite/deriwfinance)

Medium - [Deriw](https://medium.com/@DeriwFi)

Docs - [Deriw](https://docs.deriw.com/)
