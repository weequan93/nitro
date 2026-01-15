//go:build erigon
// +build erigon

package erigonexec

import (
	"fmt"
	"path/filepath"

	elog "github.com/erigontech/erigon-lib/log/v3"
	"github.com/erigontech/erigon/db/kv/dbcfg"
	"github.com/ethereum/go-ethereum/ethdb"

	"github.com/offchainlabs/nitro/execution/erigonexec/kvdb"
)

func OpenArbDB(chainDir string, opts MdbxOptions) (ethdb.Database, error) {
	logger := elog.New("component", "erigonexec")
	arbPath := filepath.Join(chainDir, "arbitrumdata")
	arbDB, err := openMdbxDB(dbcfg.ArbitrumDB, arbPath, opts, logger, arbTablesCfg())
	if err != nil {
		return nil, fmt.Errorf("open mdbx arb db: %w", err)
	}
	return kvdb.NewMerged(arbDB, kvdb.PrefixBuckets{
		Buckets:        arbBuckets(),
		DefaultBucket:  BucketArbData,
		PrefixToBucket: arbPrefixBuckets,
	}), nil
}

func OpenWasmDB(chainDir string, opts MdbxOptions) (ethdb.Database, error) {
	logger := elog.New("component", "erigonexec")
	wasmPath := filepath.Join(chainDir, "wasm")
	wasmDB, err := openMdbxDB(dbcfg.ArbWasmDB, wasmPath, opts, logger, wasmTablesCfg())
	if err != nil {
		return nil, fmt.Errorf("open mdbx wasm db: %w", err)
	}
	return kvdb.New(wasmDB, BucketArbWasm), nil
}
