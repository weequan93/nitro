//go:build erigon
// +build erigon

package main

import (
	"context"
	"testing"

	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/stretchr/testify/require"

	"github.com/offchainlabs/nitro/util/dbutil"
)

func TestStateModeCopyAndVerify(t *testing.T) {
	ctx := context.Background()
	src := rawdb.NewMemoryDatabase()
	dst := rawdb.NewMemoryDatabase()

	require.NoError(t, src.Put([]byte("k1"), []byte("v1")))
	require.NoError(t, src.Put([]byte("k2"), []byte("v2")))

	stats, err := copyDatabase(ctx, src, dst)
	require.NoError(t, err)
	require.Equal(t, uint64(2), stats.Keys)
	require.NoError(t, dbutil.UnfinishedConversionCheck(dst))

	verifyStats, err := verifyDatabase(ctx, src, dst, "extended", "test")
	require.NoError(t, err)
	require.Equal(t, stats.Keys, verifyStats.Keys)
	require.Equal(t, stats.Bytes, verifyStats.Bytes)
}

func TestVerifyDatabaseDetectsMissingKey(t *testing.T) {
	ctx := context.Background()
	src := rawdb.NewMemoryDatabase()
	dst := rawdb.NewMemoryDatabase()

	require.NoError(t, src.Put([]byte("k1"), []byte("v1")))

	_, err := verifyDatabase(ctx, src, dst, "basic", "test")
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing key")
}

func TestVerifyDatabaseDetectsExtraKeys(t *testing.T) {
	ctx := context.Background()
	src := rawdb.NewMemoryDatabase()
	dst := rawdb.NewMemoryDatabase()

	require.NoError(t, src.Put([]byte("k1"), []byte("v1")))
	require.NoError(t, dst.Put([]byte("k1"), []byte("v1")))
	require.NoError(t, dst.Put([]byte("k2"), []byte("v2")))

	_, err := verifyDatabase(ctx, src, dst, "extended", "test")
	require.Error(t, err)
	require.Contains(t, err.Error(), "destination has extra keys")
}
