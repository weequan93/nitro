//go:build erigon
// +build erigon

package erigonexec

import (
	"context"
	"path/filepath"
	"testing"

	elog "github.com/erigontech/erigon-lib/log/v3"
	"github.com/erigontech/erigon/db/kv/dbcfg"
	"github.com/stretchr/testify/require"

	"github.com/offchainlabs/nitro/execution/erigonexec/kvdb"
)

func TestArbPrefixBucketRouting(t *testing.T) {
	dir := t.TempDir()
	db, err := openMdbxDB(dbcfg.ArbitrumDB, filepath.Join(dir, "arbitrumdata"), MdbxOptions{}, elog.New(), arbTablesCfg())
	require.NoError(t, err)
	t.Cleanup(db.Close)

	merged := kvdb.NewMerged(db, kvdb.PrefixBuckets{
		Buckets:        arbBuckets(),
		DefaultBucket:  BucketArbData,
		PrefixToBucket: arbPrefixBuckets,
	})

	expected := map[byte]string{
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

	keys := [][]byte{
		[]byte("m1"),
		[]byte("r1"),
		[]byte("b1"),
		[]byte("t1"),
		[]byte("x1"),
		[]byte("d1"),
		[]byte("e1"),
		[]byte("p1"),
		[]byte("s1"),
		[]byte("a1"),
		[]byte("_1"),
		[]byte("z1"),
	}
	for _, key := range keys {
		require.NoError(t, merged.Put(key, []byte("v")))
	}

	tx, err := db.BeginRo(context.Background())
	require.NoError(t, err)
	defer tx.Rollback()

	for _, key := range keys {
		bucket := BucketArbData
		if len(key) > 0 {
			if mapped, ok := expected[key[0]]; ok {
				bucket = mapped
			}
		}
		value, err := tx.GetOne(bucket, key)
		require.NoError(t, err)
		require.Equal(t, []byte("v"), value)
	}
}
