// Copyright 2023-2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md
package pathdbmigrate

import (
	"sync/atomic"
	"time"
)

type Stats struct {
	start         time.Time
	accountNodes  atomic.Uint64
	accountLeaves atomic.Uint64
	storageNodes  atomic.Uint64
	storageLeaves atomic.Uint64
	storageTries  atomic.Uint64
	bytes         atomic.Uint64
	batches       atomic.Uint64
	legacyNodes   atomic.Uint64
	legacyBytes   atomic.Uint64
	snapshotNodes atomic.Uint64
	snapshotBytes atomic.Uint64
}

func (s *Stats) Reset() {
	s.start = time.Now()
	s.accountNodes.Store(0)
	s.accountLeaves.Store(0)
	s.storageNodes.Store(0)
	s.storageLeaves.Store(0)
	s.storageTries.Store(0)
	s.bytes.Store(0)
	s.batches.Store(0)
	s.legacyNodes.Store(0)
	s.legacyBytes.Store(0)
	s.snapshotNodes.Store(0)
	s.snapshotBytes.Store(0)
}

func (s *Stats) AccountNodes() uint64 {
	return s.accountNodes.Load()
}

func (s *Stats) AccountLeaves() uint64 {
	return s.accountLeaves.Load()
}

func (s *Stats) StorageNodes() uint64 {
	return s.storageNodes.Load()
}

func (s *Stats) StorageLeaves() uint64 {
	return s.storageLeaves.Load()
}

func (s *Stats) StorageTries() uint64 {
	return s.storageTries.Load()
}

func (s *Stats) Bytes() uint64 {
	return s.bytes.Load()
}

func (s *Stats) Batches() uint64 {
	return s.batches.Load()
}

func (s *Stats) LegacyNodes() uint64 {
	return s.legacyNodes.Load()
}

func (s *Stats) LegacyBytes() uint64 {
	return s.legacyBytes.Load()
}

func (s *Stats) SnapshotNodes() uint64 {
	return s.snapshotNodes.Load()
}

func (s *Stats) SnapshotBytes() uint64 {
	return s.snapshotBytes.Load()
}

func (s *Stats) Elapsed() time.Duration {
	if s.start.IsZero() {
		return 0
	}
	return time.Since(s.start)
}
