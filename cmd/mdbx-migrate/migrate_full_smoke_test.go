//go:build erigon
// +build erigon

package main

import (
	"os"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/offchainlabs/nitro/cmd/conf"
)

func TestFullMigrationSmoke(t *testing.T) {
	source := os.Getenv("MDBX_MIGRATE_FULL_SOURCE")
	if source == "" {
		t.Skip("MDBX_MIGRATE_FULL_SOURCE not set")
	}
	endBlock := uint64(0)
	if endStr := os.Getenv("MDBX_MIGRATE_FULL_END_BLOCK"); endStr != "" {
		parsed, err := strconv.ParseUint(endStr, 10, 64)
		require.NoError(t, err)
		endBlock = parsed
	}

	opts := Options{
		Source:        source,
		Dest:          t.TempDir(),
		Mode:          "full",
		Verify:        "basic",
		VerifySamples: 1,
		Resume:        false,
		StartBlock:    0,
		EndBlock:      endBlock,
		Workers:       1,
		Mdbx:          conf.MdbxConfigDefault,
	}

	require.NoError(t, run(opts))
}
