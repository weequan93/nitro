// Load on a trusted page with an EIP-1193 wallet, then explicitly call
// await claimWithdrawal(bundle) with the generated wallet-claim.json contents.
// Calling the function asks the wallet to submit a real Arbitrum One transaction.
async function claimWithdrawal(bundle) {
  const provider = window.ethereum;
  if (!provider) throw new Error("No EIP-1193 wallet provider");
  const outbox = "0x47da6c41d03ac0608924e86f61577df558114bd8";
  const request = bundle.transaction;
  const index = BigInt(bundle.index);
  if (index < 0n || index >= 2n ** 64n || !/^0x[0-9a-fA-F]{64}$/.test(bundle.root))
    throw new Error("Invalid withdrawal index/root");
  if (request.to.toLowerCase() !== outbox || BigInt(request.chainId) !== 42161n ||
      BigInt(request.value) !== 0n || !/^0x08635a95(?:[0-9a-fA-F]{2})+$/.test(request.data) ||
      BigInt("0x" + request.data.slice(74, 138)) !== index)
    throw new Error("Unexpected Outbox claim transaction");
  const checkChain = async () => {
    if (BigInt(await provider.request({method: "eth_chainId"})) !== 42161n)
      throw new Error("Switch wallet to Arbitrum One");
  };
  await checkChain();
  const [from] = await provider.request({method: "eth_requestAccounts"});
  if (!from) throw new Error("Select a gas-paying account");
  const spent = await provider.request({method: "eth_call", params: [{to: outbox,
    data: "0x5a129efe" + index.toString(16).padStart(64, "0")}, "latest"]});
  if (BigInt(spent) !== 0n) throw new Error("Already claimed; do not resend");
  // The complete executeTransaction call checks the actual proof and registered root.
  const tx = {from, to: outbox, value: "0x0", data: request.data, chainId: "0xa4b1"};
  await provider.request({method: "eth_call", params: [tx, "latest"]});
  const gas = BigInt(await provider.request({method: "eth_estimateGas", params: [tx]}));
  tx.gas = "0x" + ((gas * 120n + 99n) / 100n).toString(16);
  await checkChain();
  const [selected] = await provider.request({method: "eth_accounts"});
  if (!selected || selected.toLowerCase() !== from.toLowerCase())
    throw new Error("Selected wallet account changed");
  console.log("Review fixed withdrawal destination and payload:", bundle.details);
  const hash = await provider.request({method: "eth_sendTransaction", params: [tx]});
  console.log("Claim submitted; verify its successful receipt and isSpent:", hash);
  return hash;
}
