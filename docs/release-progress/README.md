# Deriw release progress

Use this directory to record what has happened and where to resume a release.
Use [release guides](../release-guides/README.md) and the linked runbook for
instructions on how to perform each step.

## Active records

| Release work | Environment | Progress and next steps |
| --- | --- | --- |
| DER-2646 custom DeriwOS rollout, release tag not yet recorded | Testnet (`rpc.test.deriw.com`, chain `2885`) | [Testnet progress](deriwos-testnet.md) |

## Updating a record

1. Copy [TEMPLATE.md](TEMPLATE.md) for each release and environment. Keep
   development-chain progress separate from testnet, even when both use the
   `deriw-dev` source branch.
2. Record the source commit, deployed image digest, and WASM root when verified.
   A local branch or prepared proposal does not prove deployment or execution.
3. Update the checkpoint, step status, evidence, and next action after each
   deployment or governance operation. Distinguish prepared, submitted,
   executed, and verified states.
4. Append dated history; retain earlier observations. Include timezone and
   block number/hash when available. Re-query state before resuming.
5. Link to the guide instead of copying its transaction commands. Store only
   public transaction hashes and sanitized evidence or change-ticket links;
   keep credentials, private keys, and signatures out of Git.
