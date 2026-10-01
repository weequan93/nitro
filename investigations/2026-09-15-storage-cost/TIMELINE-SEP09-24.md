> **最新纠正：用户确认该节点 `archive=false`。** 此前基于“archive 节点”口述作出的“当前节点保留全部历史状态”判断撤回。保留全部区块不等于保留全部历史状态。下文旧分析中的 hash archive 归因已被此更正替代；链上记录数量、磁盘增长和 compaction 采样数据仍有效。用户进一步确认 nodeconfig caching.archive 一直为 false；后续按 hash 非归档节点分析，不再推定此前开启 archive。

# DeriW: September 9–24, 2026 storage and ETH spending

This extends and supersedes the timing and coverage of REPORT.md. Snapshot ends **September 24, 2026, 02:46:40 UTC / 10:46:40 Singapore**. September 24 is a partial day. “Storage” still has no supplied physical disk or DA billing metric, so the storage measurements here concern contract records and logical slots.

## Conclusion

There are **two independently identified changes**, not one:

1. **September 15, 09:58:25 UTC:** recurring updates move to a new signed PriceOracle that retains five slots per historical token-price record. This is the clear application-level source of sustained state growth. Larger update payloads coincide with more frequent AnyTrust postings; actual batch-poster size/compression settings remain unavailable.
2. **September 23, 17:20:03 UTC:** Arbitrum One enables priority-tip collection. DeriW's existing 0.05 gwei batch-poster tip starts being charged. A directly adjacent before/after pair changes from 0.02 to 0.07 gwei with unchanged 0.02 gwei base fee: **3.5× gas price**. This explains the additional sharp ETH increase into September 24.

## Exact transactions and changes

All times in this table are UTC; add eight hours for Singapore.

| Time | Change | Evidence transaction |
|---|---|---|
| Sep 14 02:58:38 | Deploy new oracle proxy `0x1461469b43ad78145048eea11cbf6ba97222d379` | [0x876a24a0…](https://explorer.deriw.com/tx/0x876a24a0d5ba9ce949843aaa2513fa93dff874c69950dc1fb5e2cb137e1df5b8) |
| Sep 14 02:58:39 | `setImplementation(0xf822b48544b783d2e1fd8d3ff395b959bb6168ad)` | [0xfa3cf4c9…](https://explorer.deriw.com/tx/0xfa3cf4c926bd9f40ed2471b787aac0d2f6fa05df4aecdd6c2bd81649c6ab4010) |
| Sep 14 07:34:08 | Initialize oracle with USDT and two signers | [0xf5532ddd…](https://explorer.deriw.com/tx/0xf5532ddd7b049806a854293deeb0d5985d7243e86268bff5a33b2550a9863503) |
| Sep 14 07:34:09 | Set DataReader `0x934b75a4f576738c1392a2af1bf8be1fbf52b53d` | [0x96bc49eb…](https://explorer.deriw.com/tx/0x96bc49eb3ea9c77cb9ec11e0e71a6f544d4b4e7c3726c2950ff420f66f1f8632) |
| Sep 14 07:34:10–13 | Authorize six updater addresses; example authorizes the first active updater `0x47aa875f16b6b746f1e17e9a928e4db71173615c` | [0x1c77f14a…](https://explorer.deriw.com/tx/0x1c77f14a826b860847366f5a0b4d80155114138595f2cf45f92f476f84d7116f) |
| Sep 15 09:57:38 | Last indexed update to old PriceFeed `0x83ca1aa2bc20e41287154650e4161dc995278e1d` | [0x47efb6a6…](https://explorer.deriw.com/tx/0x47efb6a6b52257002f18e5537309a8c053baf255b89fb9642114abd904b55776) |
| Sep 15 09:58:25 | First indexed `batchSetPrices` to new oracle; block 122496270 | [0xd813714a…](https://explorer.deriw.com/tx/0xd813714ae39cdd314b608dd4c44f8283eeb7d611f4b24b4c74a2252741a593c4) |
| Sep 23 17:20:03 | Arbitrum `ArbOwner.setCollectTips(true)`; parent block 508190474 | [0x81541d94…](https://arbiscan.io/tx/0x81541d94eb04c080ab55d0a589728353fb11ab63b9bd33591ae9db44fa261e95) |

The new oracle was a **new contract deployment and traffic migration**, not an implementation replacement of the old PriceFeed. Its indexed NewImplementation history contains only the initial September 14 assignment. Historical reads also confirm the same implementation on September 16 and September 23. Nothing in this evidence requires a Nitro software upgrade on September 15 to explain the contract-state expansion.

## Transactions responsible for recurring storage growth

The recurring transaction is `PriceOracle.batchSetPrices`, selector **0x71044186**. The activation transaction above succeeded, submitted **50 price entries plus two signatures**, used **6,948 calldata bytes**, emitted **50 SetPrice events**, and consumed **7,464,218 DeriW gas**. It creates 50 × 5 = **250 new historical slots**, in addition to updating counters and salt. These are child-chain execution gas units, not Arbitrum gas costs.

The next successful update, [0xe1ad025c…](https://explorer.deriw.com/tx/0xe1ad025c27e7449d2d1ed8dabbe6df6753af9176cf82376183038643d0fa3420), updates another 50 distinct tokens; the first two batches do not overlap in token coverage.

Compared with old PriceFeed:

| Per valid token update | Old PriceFeed | New PriceOracle |
|---|---|---|
| Historical record | one answer | token address, ask, bid, mid, timestamp |
| Newly allocated historical slots | 1 | 5 |
| Deletes historical records | No | No |
| Updates with unchanged prices create new record | Yes | Yes |
| Application signatures in new update ABI | Not in old PriceFeed method | Two in sampled calls |

The five-slot count is source-derived and corroborated by successful SetPrice events; it is not a claim that total node disk grew exactly fivefold. FastPriceFeed and database history add other baseline costs.

**Measured cumulative growth, not just a model:**

- Scope: **198 distinct index tokens observed in successful oracle events**; this is not guaranteed to be every configured token.
- Sep 16 boundary, block 122698309: **2,681,305** total price IDs.
- Sep 24 child-chain snapshot at 02:46:42 UTC, block 125350364: **32,187,894** total price IDs. See storage-counts.json for the authoritative snapshot block and timestamp.
- Difference: **29,506,589 price records**.
- At five historical slots each: **147,532,945 additional logical storage slots**.
- Their 32-byte logical values represent **4,721,054,240 bytes (4.72 GB / 4.40 GiB)** before keys, trie encoding, database indexes, and historical-state overhead. This is **not a physical disk estimate**.

Every getter result was independently checked with `eth_getStorageAt` on the **198 distinct `priceId` mapping keys (mapping slot 11)** at both blocks. This avoids double counting through DataReader aliases. The sums are a lower bound across the observed token set; storage key/value verification is saved in storage-verification.json. Ordinary pruning cannot remove the retained records because they remain live state.

## Entire daily comparison

Batch counts are exact successful SequencerBatchDelivered event counts. Fees are estimates from **24 batches distributed across each day's batch sequence**, multiplied by the exact count; not a complete wallet ledger. Explorer transaction counts are its current reported totals and can be revised as indexing progresses. The transaction API does not provide a completed September 24 total.

| September date (UTC) | Explorer transactions | Successful batches | Mean fee/batch (ETH) | Estimated total posting ETH |
|---|---:|---:|---:|---:|
| 9 | 1,034,291 | 1,180 | 0.000002937 | 0.003465 |
| 10 | 1,041,957 | 1,190 | 0.000002938 | 0.003496 |
| 11 | 1,020,072 | 1,171 | 0.000003232 | 0.003784 |
| 12 | 816,823 | 589 | 0.000002942 | 0.001733 |
| 13 | 876,803 | 681 | 0.000002935 | 0.001998 |
| 14 | 1,037,952 | 1,217 | 0.000002973 | 0.003618 |
| 15 | 888,893 | 1,623 | 0.000003002 | 0.004872 |
| 16 | 715,247 | 1,826 | 0.000002996 | 0.005470 |
| 17 | 679,104 | 1,678 | 0.000002951 | 0.004951 |
| 18 | 644,593 | 1,676 | 0.000002980 | 0.004995 |
| 19 | 667,416 | 1,219 | 0.000002970 | 0.003621 |
| 20 | 667,009 | 1,260 | 0.000002941 | 0.003706 |
| 21 | 616,019 | 1,646 | 0.000003039 | 0.005002 |
| 22 | 663,429 | 1,752 | 0.000002974 | 0.005211 |
| 23 | 788,514 | 1,866 | 0.000004800 | 0.008957 |
| 24 (partial) | — | 201 | 0.000010239 | 0.002058 |

Do not compare September 24's partial-day ETH total directly with a full day. Its mean fee per batch is the useful comparison. September 12–13 and 19–20 are weekends; their lower posting counts also make September 13 alone an unusually low baseline.

September 14 → 16: successful batch count +50.0%, sampled mean fee per batch +0.78%, estimated successful-posting spend +51.2%, while explorer transaction count falls about 31%. The new workload carries larger signed payloads even though fewer transactions are submitted.

All **384** distributed parent-batch samples carried 178-byte AnyTrust certificates, header 0x88. No sample shows full-calldata fallback; occasional unsampled fallback is not excluded. Compression ratio, DA object sizes, and the deployed batch-poster max-size/max-delay configuration were not available. Therefore the posting-frequency increase is confirmed, while the larger payloads are a strongly supported contributor rather than a fully isolated explanation of the batch policy.

## Why ETH jumped again September 23–24

Rechecked transaction path: the outer call is Safe `execTransaction` to `0xe128a8100d1b543de82d04450b3406d57abf553b`. It calls verified `TipCollectionToggler` at `0x4323dab775cc15e386fdac591a39420db6226a63` with `setCollectTips(true)`, which forwards to ArbOwner at `0x70`. The successful receipt contains both `OwnerActs` and `CollectTipsUpdated(true)`. This is an Arbitrum One network administration transaction, not a DeriW batch-posting transaction. Its effect concerns the later September 23–24 fee increase, not the September 15 storage migration.

The parent OwnerActs event at `0x0000000000000000000000000000000000000070` records method selector **0xa858dbe2**, `setCollectTips(true)`, in the activation transaction above. The [Arbitrum governance proposal](https://forum.arbitrum.foundation/t/constitutional-aip-transition-arbitrum-one-ordering-policy-to-priority-gas-auctions-pga/30942) documents this toggle mechanism; the transaction event and actual receipts establish this incident's activation timing and fee effect.

Directly adjacent DeriW batches around the change:

| | Before | After |
|---|---:|---:|
| UTC time, Sep 23 | 17:17:40 | 17:20:51 |
| Parent base fee | 0.020 gwei | 0.020 gwei |
| Poster maxPriorityFeePerGas | 0.050 gwei | 0.050 gwei |
| Receipt effectiveGasPrice | 0.020 gwei | 0.070 gwei |
| Receipt gas used | 152,193 | 146,614 |
| Actual fee | 0.000003043860 ETH | 0.000010262980 ETH |

Before: [0xf3bf247e…](https://arbiscan.io/tx/0xf3bf247e285e9c01a1f2cec3e79efb575d1b4ba5979940f6838df9a508005ef0). After: [0x806a3916…](https://arbiscan.io/tx/0x806a3916ce4d62cddb02f1faf1090f6c8b185b312113fc34566f4e08d32cfbe8).

The gas price rises **3.5×**; the actual fee rises about **3.37×** in this pair because the second batch uses slightly less gas. There is no base-fee increase in this pair. At the same batch rate, this alone would multiply posting spend by approximately 3.5.

The observed 0.05 gwei matches this repository's default `MinTipCapGwei` in arbnode/dataposter/data_poster.go. Its logic clamps suggested tips upward to that minimum. This does not prove production uses the default rather than an identical override. During this investigation the parent RPC returned `eth_maxPriorityFeePerGas = 0` and `eth_gasPrice = 20,000,000 wei`; a suggestion of zero is not a guarantee of inclusion latency under all conditions.

## Actions supported by the evidence

1. **Inspect the batch poster's `node.batch-poster.data-poster.min-tip-cap-gwei` first.** The observed 0.05 gwei is now a real payment rather than an ignored bid. Evaluate lowering the minimum with measured posting latency, backlog, replacement behavior, and current network conditions. At 0.02 base plus 0.05 tip, tips account for about 71% of the effective gas price. No production setting has been changed.
2. **Address oracle history retention for storage.** Check consumers of historical IDs before using bounded retention. Review unnecessary unchanged-price updates, redundant stored token addresses, derived mid prices, and safely packed values. Preserve settlement, freshness, signature requirements, and proxy storage compatibility.
3. **Check DA and batch configuration for posting frequency.** Measure compressed bytes per batch and compare actual configuration around September 15. The data identifies the new oracle workload, but does not prove the batch size/delay configuration stayed constant.
4. **Reconcile the actual expense wallet if its increase differs.** Counts and estimates here cover successful postings to the identified inbox. Failed transactions, validators, other wallets, transfers, and DA service charges are outside those totals.

## Files

- extended-fees.json: 384 batch receipt/transaction/header summaries.
- batchlogs-*.json.gz: complete daily successful-batch event evidence (some files also include other inbox events; filter on the SequencerBatchDelivered topic).
- oracle-first-transactions.json, oracle-activation-receipts.json, oracle-upgrades-indexer.json, old-feed-last.json: contract setup, activation, and cutover evidence.
- parent-tip-toggle.json and tip-before-after.json: priority-fee activation event and immediately adjacent receipts.
- storage-counts.json and storage-verification.json: record counts and direct mapping-key verification.
- Verified Solidity snapshots and collection scripts are adjacent to this report. Original REPORT.md remains as the initial narrower investigation; this document supersedes its tentative activation window and later-fee observation.

Limits: no direct server disk measurements, deployment configuration, or full wallet expenditure reconciliation. Public RPC history is subject to pruning and rate limits; a wide oracle-upgrade RPC log query timed out, so indexed explorer events were used for that specific history. No chain or node configuration was modified.
