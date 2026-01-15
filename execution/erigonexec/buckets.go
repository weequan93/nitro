package erigonexec

// MDBX bucket names for Arbitrum metadata and WASM storage.
const (
	BucketArbData                  = "arb_data" // fallback bucket for unknown prefixes
	BucketArbMessages              = "arb_messages"
	BucketArbMessageResults        = "arb_message_results"
	BucketArbBlockHashFeed         = "arb_block_hash_feed"
	BucketArbBlockMetadataFeed     = "arb_block_metadata_feed"
	BucketArbMissingBlockMetadata  = "arb_missing_block_metadata"
	BucketArbDelayedMessagesLegacy = "arb_delayed_messages_legacy"
	BucketArbDelayedMessagesRLP    = "arb_delayed_messages_rlp"
	BucketArbParentChainBlocks     = "arb_parent_chain_blocks"
	BucketArbSequencerBatches      = "arb_sequencer_batches"
	BucketArbDelayedSequenced      = "arb_delayed_sequenced"
	BucketArbCounters              = "arb_counters"
	BucketArbWasm                  = "arb_wasm"
)

var arbPrefixBuckets = map[byte]string{
	'm': BucketArbMessages,
	'r': BucketArbMessageResults,
	'b': BucketArbBlockHashFeed,
	't': BucketArbBlockMetadataFeed,
	'x': BucketArbMissingBlockMetadata,
	'd': BucketArbDelayedMessagesLegacy,
	'e': BucketArbDelayedMessagesRLP,
	'p': BucketArbParentChainBlocks,
	's': BucketArbSequencerBatches,
	'a': BucketArbDelayedSequenced,
	'_': BucketArbCounters,
}

var arbBucketList = []string{
	BucketArbMessages,
	BucketArbMessageResults,
	BucketArbBlockHashFeed,
	BucketArbBlockMetadataFeed,
	BucketArbMissingBlockMetadata,
	BucketArbDelayedMessagesLegacy,
	BucketArbDelayedMessagesRLP,
	BucketArbParentChainBlocks,
	BucketArbSequencerBatches,
	BucketArbDelayedSequenced,
	BucketArbCounters,
	BucketArbData,
}

func arbBucketForKey(key []byte) string {
	if len(key) == 0 {
		return BucketArbData
	}
	if bucket, ok := arbPrefixBuckets[key[0]]; ok {
		return bucket
	}
	return BucketArbData
}

func arbBuckets() []string {
	out := make([]string, len(arbBucketList))
	copy(out, arbBucketList)
	return out
}
