// Copyright 2023-2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md
package pathdbmigrate

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/trie/trienode"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/ethereum/go-ethereum/triedb/pathdb"
	"github.com/holiman/uint256"
)

func TestConvertHashStateToPathState(t *testing.T) {
	src := rawdb.NewMemoryDatabase()
	dst := rawdb.NewMemoryDatabase()

	root, accountHash, storageRoot := buildHashState(t, src)
	copyDatabase(t, src, dst)

	code := []byte{0x60, 0x00}
	codeHash := crypto.Keccak256Hash(code)
	rawdb.WriteCode(dst, codeHash, code)
	nonTrieKey := crypto.Keccak256Hash([]byte("not trie"))
	if err := dst.Put(nonTrieKey.Bytes(), []byte("not a trie node")); err != nil {
		t.Fatal(err)
	}
	storageHash := crypto.Keccak256Hash([]byte("snapshot-slot"))
	rawdb.WriteAccountSnapshot(dst, accountHash, []byte("stale-account-snapshot"))
	rawdb.WriteStorageSnapshot(dst, accountHash, storageHash, []byte("stale-storage-snapshot"))

	config := DefaultConfig
	config.IdealBatchSize = 256
	migrator := NewMigrator(&config)
	migrator.stats.Reset()

	if !rawdb.HasLegacyTrieNode(dst, root) {
		t.Fatal("test setup missing legacy account trie root")
	}
	if err := migrator.convertState(context.Background(), src, dst, root, true); err != nil {
		t.Fatal(err)
	}
	if err := migrator.writePathMetadata(dst, root); err != nil {
		t.Fatal(err)
	}
	if scheme := rawdb.ReadStateScheme(dst); scheme != rawdb.PathScheme {
		t.Fatalf("unexpected destination state scheme: have %q want %q", scheme, rawdb.PathScheme)
	}
	if flag := rawdb.ReadSnapSyncStatusFlag(dst); flag != rawdb.StateSyncFinished {
		t.Fatalf("unexpected snap sync status flag: have %d want %d", flag, rawdb.StateSyncFinished)
	}
	if got := crypto.Keccak256Hash(rawdb.ReadAccountTrieNode(dst, nil)); got != root {
		t.Fatalf("root mismatch: have %s want %s", got, root)
	}
	if !rawdb.HasStorageTrieNode(dst, accountHash, nil) {
		t.Fatal("missing pathdb storage trie root")
	}
	if err := VerifyPathState(context.Background(), dst, root); err != nil {
		t.Fatal(err)
	}
	if err := migrator.cleanupLegacyHashState(context.Background(), dst); err != nil {
		t.Fatal(err)
	}
	if err := migrator.cleanupHashdbSnapshots(context.Background(), dst); err != nil {
		t.Fatal(err)
	}
	if rawdb.HasLegacyTrieNode(dst, root) {
		t.Fatal("legacy account trie root was not deleted")
	}
	if snap, err := rawdb.ReadAccountSnapshot(dst, accountHash); err != nil || len(snap) != 0 {
		t.Fatalf("account snapshot was not deleted: %x err %v", snap, err)
	}
	if snap, err := rawdb.ReadStorageSnapshot(dst, accountHash, storageHash); err != nil || len(snap) != 0 {
		t.Fatalf("storage snapshot was not deleted: %x err %v", snap, err)
	}
	if got := rawdb.ReadCode(dst, codeHash); string(got) != string(code) {
		t.Fatalf("code was changed by cleanup: have %x want %x", got, code)
	}
	if got, err := dst.Get(nonTrieKey.Bytes()); err != nil || string(got) != "not a trie node" {
		t.Fatalf("non-trie 32-byte key was changed by cleanup: have %x err %v", got, err)
	}
	if err := VerifyPathState(context.Background(), dst, root); err != nil {
		t.Fatal(err)
	}

	pathConfig := *pathdb.ReadOnly
	pathTrieDB := triedb.NewDatabase(dst, &triedb.Config{PathDB: &pathConfig})
	defer pathTrieDB.Close()
	storageTrie, err := trie.New(trie.StorageTrieID(root, accountHash, storageRoot), pathTrieDB)
	if err != nil {
		t.Fatal(err)
	}
	it := trie.NewIterator(storageTrie.MustNodeIterator(nil))
	if !it.Next() {
		t.Fatal("expected storage leaf")
	}
	if err := it.Err; err != nil {
		t.Fatal(err)
	}
}

func buildHashState(t *testing.T, db ethdb.Database) (common.Hash, common.Hash, common.Hash) {
	t.Helper()

	trieDB := triedb.NewDatabase(db, triedb.HashDefaults)
	defer trieDB.Close()

	address := common.HexToAddress("0x1234")
	accountHash := crypto.Keccak256Hash(address.Bytes())
	slotHash := crypto.Keccak256Hash(common.LeftPadBytes([]byte{1}, common.HashLength))

	storageTrie, err := trie.New(trie.StorageTrieID(types.EmptyRootHash, accountHash, types.EmptyRootHash), trieDB)
	if err != nil {
		t.Fatal(err)
	}
	storageValue, err := rlp.EncodeToBytes(uint256.NewInt(99))
	if err != nil {
		t.Fatal(err)
	}
	storageTrie.MustUpdate(slotHash.Bytes(), storageValue)
	storageRoot, storageNodes := storageTrie.Commit(false)
	if err := trieDB.Update(storageRoot, types.EmptyRootHash, 0, trienode.NewWithNodeSet(storageNodes), nil); err != nil {
		t.Fatal(err)
	}
	if err := trieDB.Commit(storageRoot, false); err != nil {
		t.Fatal(err)
	}

	account := types.StateAccount{
		Nonce:    7,
		Balance:  uint256.NewInt(123),
		Root:     storageRoot,
		CodeHash: types.EmptyCodeHash.Bytes(),
	}
	accountBlob, err := rlp.EncodeToBytes(&account)
	if err != nil {
		t.Fatal(err)
	}
	accountTrie, err := trie.New(trie.StateTrieID(types.EmptyRootHash), trieDB)
	if err != nil {
		t.Fatal(err)
	}
	accountTrie.MustUpdate(accountHash.Bytes(), accountBlob)
	root, accountNodes := accountTrie.Commit(false)
	if err := trieDB.Update(root, types.EmptyRootHash, 0, trienode.NewWithNodeSet(accountNodes), nil); err != nil {
		t.Fatal(err)
	}
	if err := trieDB.Commit(root, false); err != nil {
		t.Fatal(err)
	}
	return root, accountHash, storageRoot
}

func copyDatabase(t *testing.T, src ethdb.Database, dst ethdb.Database) {
	t.Helper()

	it := src.NewIterator(nil, nil)
	defer it.Release()
	batch := dst.NewBatch()
	for it.Next() {
		if err := batch.Put(common.CopyBytes(it.Key()), common.CopyBytes(it.Value())); err != nil {
			t.Fatal(err)
		}
	}
	if err := it.Error(); err != nil {
		t.Fatal(err)
	}
	if err := batch.Write(); err != nil {
		t.Fatal(err)
	}
}

func TestArchiveTrieRootAvailable(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	root := crypto.Keccak256Hash([]byte("retained-state-root"))

	if !archiveTrieRootAvailable(db, types.EmptyRootHash) {
		t.Fatal("empty root must always be available")
	}
	if archiveTrieRootAvailable(db, root) {
		t.Fatal("unstored root unexpectedly reported as available")
	}
	rawdb.WriteLegacyTrieNode(db, root, []byte{0x80})
	if !archiveTrieRootAvailable(db, root) {
		t.Fatal("stored root was not reported as available")
	}
}

func TestMissingArchiveStateClassification(t *testing.T) {
	if !isMissingArchiveState(fmt.Errorf("wrapped: %w", errArchiveStateUnavailable)) {
		t.Fatal("sentinel error was not classified as a missing archive state")
	}
	missing := &trie.MissingNodeError{NodeHash: crypto.Keccak256Hash([]byte("missing-node"))}
	if !isMissingArchiveState(fmt.Errorf("wrapped: %w", missing)) {
		t.Fatal("missing trie node was not classified as a missing archive state")
	}
	if isMissingArchiveState(errors.New("unrelated failure")) {
		t.Fatal("unrelated error was classified as a missing archive state")
	}
}

func TestArchiveCoverage(t *testing.T) {
	stats := archiveHistoryStats{availableBlocks: 3, skippedBlocks: 1}
	if got, want := archiveCoverage(stats), "75.00%"; got != want {
		t.Fatalf("unexpected coverage: have %q want %q", got, want)
	}
}

func TestArchiveHistoryProgressStats(t *testing.T) {
	var stats Stats
	stats.resetArchiveHistory(10, 20)
	stats.setArchiveHistoryProgress(14, 2, archiveHistoryStats{
		blocks:           4,
		availableBlocks:  3,
		skippedBlocks:    2,
		transitions:      2,
		accounts:         7,
		storageSlots:     11,
		missingPreimages: 1,
	})

	progress, ok := stats.ArchiveHistoryProgress()
	if !ok {
		t.Fatal("archive history progress mode was not enabled")
	}
	if progress.StartBlock != 10 || progress.EndBlock != 20 || progress.CurrentBlock != 14 {
		t.Fatalf("unexpected archive range progress: %+v", progress)
	}
	if progress.ProcessedBlocks != 4 || progress.AvailableBlocks != 3 || progress.SkippedBlocks != 2 {
		t.Fatalf("unexpected archive block counts: %+v", progress)
	}
	if progress.Transitions != 2 || progress.StateID != 2 {
		t.Fatalf("unexpected archive history counts: %+v", progress)
	}
	if progress.Accounts != 7 || progress.StorageSlots != 11 || progress.MissingPreimages != 1 {
		t.Fatalf("unexpected archive data counts: %+v", progress)
	}
	if stats.startUnixNano.Load() == 0 {
		t.Fatal("archive history start time was not initialized")
	}

	stats.Reset()
	if _, ok := stats.ArchiveHistoryProgress(); ok {
		t.Fatal("generic reset did not clear archive history progress mode")
	}
}
