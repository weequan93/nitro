// Copyright 2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md
package pathdbmigrate

import (
	"bytes"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"golang.org/x/sync/singleflight"
)

// archiveNodeReads is only used beneath the archive's read-only hash trie DB.
// The existing clean cache handles completed reads. This group only shares
// concurrent misses, which occur when adjacent transitions overlap. No results
// (including errors) are retained after the last in-flight call completes.
type archiveNodeReads struct {
	ethdb.Database
	enabled   bool
	flight    singleflight.Group
	requests  atomic.Uint64
	reads     atomic.Uint64
	readBytes atomic.Uint64
}

func (db *archiveNodeReads) Get(key []byte) ([]byte, error) {
	if len(key) != common.HashLength {
		return db.Database.Get(key)
	}
	db.requests.Add(1)
	read := func() ([]byte, error) {
		db.reads.Add(1)
		blob, err := db.Database.Get(key)
		db.readBytes.Add(uint64(len(blob)))
		return blob, err
	}
	if !db.enabled {
		return read()
	}
	value, err, shared := db.flight.Do(string(key), func() (interface{}, error) { return read() })
	if err != nil {
		return nil, err
	}
	blob := value.([]byte)
	if shared {
		// Trie decoding may retain slices. Each caller must own its byte buffer,
		// just as it would after an independent database Get.
		return bytes.Clone(blob), nil
	}
	return blob, nil
}

func (db *archiveNodeReads) report(block uint64) {
	log.Info("Archive trie backend reads", "block", block,
		"coalescing", db.enabled,
		"requests", db.requests.Load(), "backendReads", db.reads.Load(),
		"backendBytes", db.readBytes.Load())
}
