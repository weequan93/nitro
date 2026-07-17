// Copyright 2023-2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md
package pathdbmigrate

import (
	"sync/atomic"
	"time"
)

type Stats struct {
	startUnixNano atomic.Int64
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

	archiveHistory          atomic.Bool
	archiveStartBlock       atomic.Uint64
	archiveEndBlock         atomic.Uint64
	archiveCurrentBlock     atomic.Uint64
	archiveProcessedBlocks  atomic.Uint64
	archiveAvailableBlocks  atomic.Uint64
	archiveSkippedBlocks    atomic.Uint64
	archiveTransitions      atomic.Uint64
	archiveStateID          atomic.Uint64
	archiveAccounts         atomic.Uint64
	archiveStorageSlots     atomic.Uint64
	archiveMissingPreimages atomic.Uint64
}

func (s *Stats) Reset() {
	s.startUnixNano.Store(time.Now().UnixNano())
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
	s.archiveHistory.Store(false)
	s.archiveStartBlock.Store(0)
	s.archiveEndBlock.Store(0)
	s.archiveCurrentBlock.Store(0)
	s.archiveProcessedBlocks.Store(0)
	s.archiveAvailableBlocks.Store(0)
	s.archiveSkippedBlocks.Store(0)
	s.archiveTransitions.Store(0)
	s.archiveStateID.Store(0)
	s.archiveAccounts.Store(0)
	s.archiveStorageSlots.Store(0)
	s.archiveMissingPreimages.Store(0)
}

type ArchiveHistoryProgress struct {
	StartBlock       uint64
	EndBlock         uint64
	CurrentBlock     uint64
	ProcessedBlocks  uint64
	AvailableBlocks  uint64
	SkippedBlocks    uint64
	Transitions      uint64
	StateID          uint64
	Accounts         uint64
	StorageSlots     uint64
	MissingPreimages uint64
}

func (s *Stats) resetArchiveHistory(start, end uint64) {
	s.Reset()
	s.archiveStartBlock.Store(start)
	s.archiveEndBlock.Store(end)
	s.archiveCurrentBlock.Store(start)
	s.archiveHistory.Store(true)
}

func (s *Stats) setArchiveHistoryProgress(block, stateID uint64, stats archiveHistoryStats) {
	s.archiveCurrentBlock.Store(block)
	s.archiveProcessedBlocks.Store(stats.blocks)
	s.archiveAvailableBlocks.Store(stats.availableBlocks)
	s.archiveSkippedBlocks.Store(stats.skippedBlocks)
	s.archiveTransitions.Store(stats.transitions)
	s.archiveStateID.Store(stateID)
	s.archiveAccounts.Store(stats.accounts)
	s.archiveStorageSlots.Store(stats.storageSlots)
	s.archiveMissingPreimages.Store(stats.missingPreimages)
}

func (s *Stats) ArchiveHistoryProgress() (ArchiveHistoryProgress, bool) {
	if !s.archiveHistory.Load() {
		return ArchiveHistoryProgress{}, false
	}
	return ArchiveHistoryProgress{
		StartBlock:       s.archiveStartBlock.Load(),
		EndBlock:         s.archiveEndBlock.Load(),
		CurrentBlock:     s.archiveCurrentBlock.Load(),
		ProcessedBlocks:  s.archiveProcessedBlocks.Load(),
		AvailableBlocks:  s.archiveAvailableBlocks.Load(),
		SkippedBlocks:    s.archiveSkippedBlocks.Load(),
		Transitions:      s.archiveTransitions.Load(),
		StateID:          s.archiveStateID.Load(),
		Accounts:         s.archiveAccounts.Load(),
		StorageSlots:     s.archiveStorageSlots.Load(),
		MissingPreimages: s.archiveMissingPreimages.Load(),
	}, true
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
	start := s.startUnixNano.Load()
	if start == 0 {
		return 0
	}
	return time.Since(time.Unix(0, start))
}
