> **最新纠正：用户确认该节点 `archive=false`。** 此前基于“archive 节点”口述作出的“当前节点保留全部历史状态”判断撤回。保留全部区块不等于保留全部历史状态。下文旧分析中的 hash archive 归因已被此更正替代；链上记录数量、磁盘增长和 compaction 采样数据仍有效。用户进一步确认 nodeconfig caching.archive 一直为 false；后续按 hash 非归档节点分析，不再推定此前开启 archive。

> **Updated investigation:** see [September 9–24 timeline](TIMELINE-SEP09-24.md) for the exact oracle cutover, quantified record growth, and confirmed September 23 priority-fee activation.

# DeriW storage and parent-chain ETH cost investigation

Investigated September 24, 2026. Assumed the reported incident is September 15, **2026**. Dates and daily boundaries below are UTC; Singapore is UTC+8. “Storage” is provisionally interpreted as node database growth; no disk or DA billing metrics were provided.

## Finding

The strongest identified explanation is the September 15 migration from **PriceFeed + FastPriceFeed to a signed, historical PriceOracle**. It increases permanent state written per price record and coincides with substantially more batch postings to Arbitrum One. Transaction count decreases, so transaction count alone hides the change in workload.

The permanent-state mechanism is established by verified contract source and successful transaction events. The increase in batch frequency is measured from all SequencerBatchDelivered events on selected days. Larger oracle payloads are a strong explanation for that frequency increase, but production batch-poster configuration and compressed DA object sizes were unavailable, so configuration changes cannot be excluded.

## Contracts and timing

- DeriW RPC: https://rpc.deriw.com; chain ID 2886.
- Parent: Arbitrum One, chain ID 42161; https://arb1.arbitrum.io/rpc.
- Rollup: `0xa113e2e9620a3bc088a681ebb2c234fdbeb85e21`.
- Sequencer inbox: `0xe79283a775f6a1de250cb3284e7ff3541ff7668a`, independently obtained through Rollup.sequencerInbox().
- Observed batch poster: `0x34993e941d0b58b22a57a0c407589ae060d19e2d`.
- Old [PriceFeed](https://explorer.deriw.com/address/0x83ca1aa2bc20e41287154650e4161dc995278e1d?tab=contract): `0x83ca1aa2bc20e41287154650e4161dc995278e1d`.
- Old [FastPriceFeed](https://explorer.deriw.com/address/0x43948b78477963d7b408a0e27ae168584c6e07a9?tab=contract): `0x43948b78477963d7b408a0e27ae168584c6e07a9`.
- New [PriceOracle proxy](https://explorer.deriw.com/address/0x1461469b43ad78145048eea11cbf6ba97222d379?tab=contract): `0x1461469b43ad78145048eea11cbf6ba97222d379`.
- Verified [implementation](https://explorer.deriw.com/address/0xf822b48544b783d2e1fd8d3ff395b959bb6168ad?tab=contract): `0xf822b48544b783d2e1fd8d3ff395b959bb6168ad`. Historical proxy slot 2 reads at blocks 122698309 (September 16) and 124965573 (September 23) confirm this implementation, not just its present-day assignment.

Proxy deployment: September 14, 02:58:38 UTC, block 122075341, [transaction](https://explorer.deriw.com/tx/0x876a24a0d5ba9ce949843aaa2513fa93dff874c69950dc1fb5e2cb137e1df5b8).

The September 15 samples still use the old feeds at **07:46:40 UTC / 15:46:40 Singapore**, block 122466453, and use the new oracle at **10:49:42 UTC / 18:49:42 Singapore**, block 122508609. This brackets the sampled workload transition; it does not identify the first-ever oracle update or prove there was no overlap.

## Why persistent storage grows faster

Old PriceFeed._setLatestAnswer updates existing `roundId[token]` and `answer[token]` slots, and allocates **one new historical slot**: `answers[token][id]`.

New PriceOracle._processPrice increments `priceId[token]`, then allocates **five new historical slots per valid token update**:

1. `price[token][id].indexToken`
2. `price[token][id].priceAsk`
3. `price[token][id].priceBid`
4. `price[token][id].priceMid`
5. `lastUpdateTime[token][id]`

The records are never removed by this implementation. Each successful update advances the ID even if the prices are unchanged. `priceMid` is validated as `(ask + bid) / 2` and then stored; the token address is stored even though it is also the outer mapping key.

**This is 5× the new historical slots per equivalent token update, not a measured 5× increase in disk usage or total chain state.** Update frequency, token coverage, receipt/log history, trie overhead, database compaction, and archive settings affect actual bytes. Existing FastPriceFeed state/history also contributes to the baseline.

For scale, one million equivalent price updates create five million slots rather than one million: 160 MB rather than 32 MB of logical 32-byte values, before keys, trie/database encoding, and history overhead. These are not disk-size estimates.

Ordinary historical-state pruning cannot remove these records: they are still part of the current live contract state. Archive/path database choices may reduce additional overhead but do not remove this underlying growth.

## Why parent-chain ETH spending increases

Exact successful batch counts from all SequencerBatchDelivered logs:

| UTC day | DeriW explorer transaction count | Successful batches | Sample mean ETH / batch | Estimated ETH / day |
|---|---:|---:|---:|---:|
| Sep 11 | 1,020,069 | 1,171 | 0.000003232 | 0.003784 |
| Sep 14 | 1,037,963 | 1,217 | 0.000002973 | 0.003618 |
| Sep 15 | 888,893 | 1,623 | 0.000003002 | 0.004872 |
| Sep 16 | 715,225 | 1,826 | 0.000002996 | 0.005470 |
| Sep 17 | 679,102 | 1,678 | 0.000002951 | 0.004951 |

Daily transaction counts: [explorer API](https://explorer.deriw.com/api/v2/stats/charts/transactions). Successful batch sequence ranges: Sep 11 297585–298755; Sep 14 300026–301242; Sep 15 301243–302865; Sep 16 302866–304691; Sep 17 304692–306369. Each range is contiguous and contains no duplicate sequence number.

Fees are measured as receipt `gasUsed * effectiveGasPrice`. Each daily fee estimate is the exact batch count multiplied by the mean of **24 evenly distributed batch samples**; these are not complete wallet expenditure totals and exclude failed transactions and validator/other-wallet activity.

September 14 → 16:

- Successful batches: **+50.0%**.
- Sample mean fee per batch: **+0.78%**.
- Estimated successful-batch ETH spend: **+51.2%**.
- Explorer transaction count: **−31.1%**.
- Mean effective gas price in the samples: about **0.02012 → 0.02035 gwei**.

All 120 full-day batch samples carry **178-byte AnyTrust certificates with header 0x88**. This supports continued AnyTrust operation in the samples, not a widespread switch to posting full calldata. It does not rule out occasional fallback outside the samples.

The new oracle’s ABI carries four words per price entry, timestamp/salt/chain ID, and signature arrays. The sampled new-oracle transactions have two signatures. Signatures add changing, poorly compressible bytes. Eight distributed block samples on September 14 contain 5,756 oracle calldata bytes in total; eight on September 16 contain 19,104 bytes despite fewer oracle transactions. These small samples establish the workload difference but are not daily byte totals. The source-level format change and increased posting count are consistent with batches reaching their size limit sooner. Confirm actual compression, max-size, and max-delay settings before attributing the entire posting-frequency change to payload size.

Higher EVM storage gas on DeriW must not be multiplied directly by Arbitrum gas prices: execution/storage growth and parent batch-posting costs are different quantities.

A separate later observation: ten batches near September 24 00:00 UTC averaged approximately 0.00001021 ETH, compared with 0.000002950 near September 23 00:00 UTC. This is a limited later sample, not the cause demonstrated for September 15 and not a full-day estimate.

## Recommended next steps

1. Confirm whether “storage” means node disk, DA/S3 storage, or explorer database size, and compare the relevant bytes/day against the oracle cutover. If it is DA storage, the oracle calldata/signature expansion is relevant; the five-slot explanation specifically concerns execution state.
2. Identify which consumers require historical price IDs and how long they need them. Consider bounded history only after checking settlement/liquidation and other historical-read requirements.
3. Review storing redundant token addresses and derived mid prices; evaluate safe packing of timestamps/prices and a suitable retention policy. Any change must preserve proxy storage compatibility and oracle semantics.
4. Review redundant unchanged-price updates and required freshness guarantees. Keep the signature trust requirements intact; do not remove signatures merely to save data.
5. Compare actual batch-poster settings and DA bytes around the cutover. Avoid changing max-delay or max-size blindly: they affect finality/latency and data limits.
6. If observed wallet spending is much larger than the approximately 50% successful-batch increase measured here, reconcile the actual paying wallets, failed posting attempts, validator transactions, and later gas-price changes separately.

No node configuration or on-chain state was changed. No production filesystem metrics, deployment configuration, or complete wallet ledger was available, so the exact physical storage increase and total ETH increase remain unmeasured.

## Evidence and reproducibility

`evidence.json` contains block boundaries, the 120 full-day parent transaction samples, distributed child-chain transaction receipts/events, and historical implementation results. Verified Solidity sources are saved alongside this report. Scripts make read-only public RPC calls and write to `/tmp/deriw-cost-investigation`; run probe.py, batchdays.py, deeper.py, then fees.py to reproduce the core collection. Public RPC pruning/rate limits may prevent later reproduction of historical state reads.

The initial short-window probe used the first inbox event, which can be a spending-report event rather than SequencerBatchDelivered. Its sequence-based count estimates were discarded. The counts reported here use the complete daily SequencerBatchDelivered event set (topic `0x7394f4a19a13c7b92b5bb71033245305946ef78452f7b4986ac1390b5df4ebd7`).
