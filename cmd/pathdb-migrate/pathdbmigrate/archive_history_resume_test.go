// Copyright 2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md
package pathdbmigrate

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/triedb"
)

func TestArchiveResumeAfterParallelFailure(t *testing.T) {
	src, dst := rawdb.NewMemoryDatabase(), rawdb.NewMemoryDatabase()
	defer src.Close()
	defer dst.Close()
	tdb := triedb.NewDatabase(src, triedb.HashDefaults)
	defer tdb.Close()
	oldRoot, newRoot := buildArchiveHashStatePair(t, src, tdb)
	writeCanonicalRootHeader(src, 0, oldRoot)
	writeCanonicalRootHeader(src, 1, newRoot)
	manifest := archiveMigrationManifest{Version: 1, Start: 0, End: 2, InitialRoot: oldRoot, EndRoot: oldRoot}
	dir := t.TempDir()
	freezer, err := rawdb.NewStateFreezer(dir, false, false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { freezer.Close() }()
	if _, _, err := prepareArchiveResume(context.Background(), src, dst, freezer, 0, manifest, true); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig
	cfg.Dst.ChainData = t.TempDir()
	cfg.ArchiveHistory.Workers = 2
	cfg.ArchiveHistory.MaxInFlight = 2
	cfg.ArchiveHistory.SpillDirectory = t.TempDir()
	m := NewMigrator(&cfg)
	// Block 2 is absent: record 1 is committed before the scanner error arrives.
	_, _, err = m.runArchiveHistoryParallel(context.Background(), src, dst, freezer, 0, 2, oldRoot, 0, true, 0, archiveHistoryStats{}, time.Now())
	if err == nil {
		t.Fatal("expected interrupted migration")
	}
	frozen, err := freezer.Ancients()
	if err != nil || frozen != 1 {
		t.Fatalf("retained records %d: %v", frozen, err)
	}
	if err := freezer.SyncAncient(); err != nil {
		t.Fatal(err)
	}
	if err := freezer.Close(); err != nil {
		t.Fatal(err)
	}
	freezer, err = rawdb.NewStateFreezer(dir, false, false)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate missing/stale KV mappings after a crash, including a lost tail.
	rawdb.WriteStateID(dst, newRoot, 99)
	stale := common.HexToHash("0xdead")
	rawdb.WriteStateID(dst, stale, 100)
	writeCanonicalRootHeader(src, 2, oldRoot)
	block, root, err := prepareArchiveResume(context.Background(), src, dst, freezer, frozen, manifest, true)
	if err != nil {
		t.Fatal(err)
	}
	if block != 1 || root != newRoot {
		t.Fatalf("wrong resume anchor %d %s", block, root)
	}
	if got := rawdb.ReadStateID(dst, newRoot); got == nil || *got != 1 {
		t.Fatalf("mapping not repaired: %v", got)
	}
	if rawdb.ReadStateID(dst, stale) != nil {
		t.Fatal("lost-tail mapping survived")
	}
	id, _, err := m.runArchiveHistoryParallel(context.Background(), src, dst, freezer, block, 2, root, block, true, frozen, archiveHistoryStats{}, time.Now())
	if err != nil || id != 2 {
		t.Fatalf("resume: ID %d, %v", id, err)
	}
	// Both retained records must match a clean computation, byte for byte.
	for id := uint64(1); id <= 2; id++ {
		parent, target := oldRoot, newRoot
		if id == 2 {
			parent, target = newRoot, oldRoot
		}
		want, err := computeArchiveTransition(context.Background(), src, tdb, archiveTransitionJob{block: id, anchorBlock: id - 1, parentRoot: parent, root: target}, cfg.ArchiveHistory, cfg.Dst.ChainData, nil)
		if err != nil {
			t.Fatal(err)
		}
		meta, ai, si, ad, sd, err := rawdb.ReadStateHistory(freezer, id)
		if err != nil {
			t.Fatal(err)
		}
		gotSections := [][]byte{meta, ai, si, ad, sd}
		wantSections := [][]byte{encodeArchiveHistoryMeta(parent, target, id), want.accountIndex, want.storageIndex, want.accountData, want.storageData}
		for i := range gotSections {
			if string(gotSections[i]) != string(wantSections[i]) {
				t.Fatalf("record %d section %d differs", id, i)
			}
		}
	}
	// Completed runs are idempotent: there are no new transitions to append.
	block, root, err = prepareArchiveResume(context.Background(), src, dst, freezer, 2, manifest, true)
	if err != nil || block != 2 || root != oldRoot {
		t.Fatalf("completed resume: %d %s %v", block, root, err)
	}
	changed := manifest
	changed.End++
	if _, _, err := prepareArchiveResume(context.Background(), src, dst, freezer, 2, changed, true); err == nil {
		t.Fatal("accepted changed range")
	}
	writeCanonicalRootHeader(src, 1, oldRoot)
	if _, _, err := prepareArchiveResume(context.Background(), src, dst, freezer, 2, manifest, true); err == nil {
		t.Fatal("accepted changed source")
	}
}

func TestArchiveResumeRejectsUnmarkedHistory(t *testing.T) {
	src, dst := rawdb.NewMemoryDatabase(), rawdb.NewMemoryDatabase()
	defer src.Close()
	defer dst.Close()
	f, err := rawdb.NewStateFreezer("", false, false)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	_, _, err = prepareArchiveResume(context.Background(), src, dst, f, 1, archiveMigrationManifest{Version: 1}, true)
	if err == nil || !strings.Contains(err.Error(), "manifest") {
		t.Fatalf("expected manifest rejection: %v", err)
	}
}

func TestArchiveResumeResetMutuallyExclusive(t *testing.T) {
	cfg := DefaultConfig
	cfg.ArchiveHistory.Enable = true
	cfg.ArchiveHistory.Resume = true
	cfg.ArchiveHistory.ResetHistory = true
	if err := cfg.Validate(); err == nil {
		t.Fatal("accepted resume with reset")
	}
}
