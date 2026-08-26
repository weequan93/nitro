// Copyright 2023-2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md
package pathdbmigrate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"math/big"
	"math/rand"
	"os"
	"testing"
	"time"

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

func TestRepairPathStateConfigRequiresExplicitBlock(t *testing.T) {
	config := DefaultConfig
	config.Src.ChainData = "/source"
	config.Dst.ChainData = "/destination"
	config.RepairPathState = true
	if err := config.Validate(); err == nil {
		t.Fatal("expected repair with latest block to fail validation")
	}
	config.Block = "123600452"
	if err := config.Validate(); err != nil {
		t.Fatalf("explicit repair block failed validation: %v", err)
	}
	config.Migrate = true
	if err := config.Validate(); err == nil {
		t.Fatal("expected repair combined with migration to fail validation")
	}
}

func TestEnsureSelectedStateStillCanonical(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	header := &types.Header{
		Number: big.NewInt(7),
		Root:   crypto.Keccak256Hash([]byte("selected-root")),
	}
	rawdb.WriteHeader(db, header)
	rawdb.WriteCanonicalHash(db, header.Hash(), header.Number.Uint64())
	selected := &selectedState{header: header, root: header.Root}
	if err := ensureSelectedStateStillCanonical(db, selected, "test"); err != nil {
		t.Fatalf("unchanged selected state failed canonicality check: %v", err)
	}

	replacement := &types.Header{
		Number: header.Number,
		Root:   crypto.Keccak256Hash([]byte("replacement-root")),
		Extra:  []byte("replacement"),
	}
	rawdb.WriteHeader(db, replacement)
	rawdb.WriteCanonicalHash(db, replacement.Hash(), replacement.Number.Uint64())
	if err := ensureSelectedStateStillCanonical(db, selected, "test"); err == nil {
		t.Fatal("expected changed canonical state to fail")
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

func TestSinglePassArchiveTrieDiff(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	trieDB := triedb.NewDatabase(db, triedb.HashDefaults)
	defer trieDB.Close()

	oldRoot, newRoot := buildArchiveHashStatePair(t, db, trieDB)
	oldTrie, err := trie.New(trie.TrieID(oldRoot), trieDB)
	if err != nil {
		t.Fatal(err)
	}
	newTrie, err := trie.New(trie.TrieID(newRoot), trieDB)
	if err != nil {
		t.Fatal(err)
	}

	addresses := []common.Address{
		common.HexToAddress("0x3000"), // updated
		common.HexToAddress("0x1000"), // deleted
		common.HexToAddress("0x2000"), // inserted
	}
	expected := make(map[common.Hash][]byte, len(addresses))
	for i, address := range addresses {
		hash := crypto.Keccak256Hash(address.Bytes())
		if i == 2 {
			expected[hash] = nil
			continue
		}
		blob, err := oldTrie.Get(hash.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		expected[hash] = common.CopyBytes(blob)
	}

	actual := make(map[common.Hash][]byte)
	err = forEachChangedLeaf(oldTrie, newTrie, func(key common.Hash, origin []byte) error {
		if _, exists := actual[key]; exists {
			t.Fatalf("changed leaf %s was emitted more than once", key)
		}
		actual[key] = common.CopyBytes(origin)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(actual) != len(expected) {
		t.Fatalf("unexpected changed leaf count: have %d want %d", len(actual), len(expected))
	}
	for key, want := range expected {
		got, ok := actual[key]
		if !ok {
			t.Fatalf("changed leaf %s was not emitted", key)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("wrong origin for %s: have %x want %x", key, got, want)
		}
	}

	sameOldTrie, err := trie.New(trie.TrieID(oldRoot), trieDB)
	if err != nil {
		t.Fatal(err)
	}
	unchanged := 0
	if err := forEachChangedLeaf(oldTrie, sameOldTrie, func(common.Hash, []byte) error {
		unchanged++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if unchanged != 0 {
		t.Fatalf("identical trie emitted %d changed leaves", unchanged)
	}
}

func TestSinglePassArchiveTrieDiffMatchesDirectionalReference(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	trieDB := triedb.NewDatabase(db, triedb.HashDefaults)
	defer trieDB.Close()

	rng := rand.New(rand.NewSource(1))
	randomHash := func() common.Hash {
		var hash common.Hash
		if _, err := rng.Read(hash[:]); err != nil {
			t.Fatal(err)
		}
		return hash
	}
	randomBlob := func() []byte {
		blob := make([]byte, 1+rng.Intn(96))
		if _, err := rng.Read(blob); err != nil {
			t.Fatal(err)
		}
		return blob
	}

	oldState := trie.NewEmpty(trieDB)
	var oldKeys []common.Hash
	for i := 0; i < 128; i++ {
		key := randomHash()
		oldKeys = append(oldKeys, key)
		oldState.MustUpdate(key.Bytes(), randomBlob())
	}
	oldRoot, oldNodes := oldState.Commit(false)
	if err := trieDB.Update(oldRoot, types.EmptyRootHash, 0, trienode.NewWithNodeSet(oldNodes), nil); err != nil {
		t.Fatal(err)
	}
	if err := trieDB.Commit(oldRoot, false); err != nil {
		t.Fatal(err)
	}

	newState, err := trie.New(trie.TrieID(oldRoot), trieDB)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range oldKeys[:32] {
		newState.MustUpdate(key.Bytes(), randomBlob())
	}
	for _, key := range oldKeys[32:64] {
		newState.MustDelete(key.Bytes())
	}
	for i := 0; i < 32; i++ {
		key := randomHash()
		newState.MustUpdate(key.Bytes(), randomBlob())
	}
	newRoot, newNodes := newState.Commit(false)
	if err := trieDB.Update(newRoot, oldRoot, 0, trienode.NewWithNodeSet(newNodes), nil); err != nil {
		t.Fatal(err)
	}
	if err := trieDB.Commit(newRoot, false); err != nil {
		t.Fatal(err)
	}

	openPair := func() (*trie.Trie, *trie.Trie) {
		oldTrie, err := trie.New(trie.TrieID(oldRoot), trieDB)
		if err != nil {
			t.Fatal(err)
		}
		newTrie, err := trie.New(trie.TrieID(newRoot), trieDB)
		if err != nil {
			t.Fatal(err)
		}
		return oldTrie, newTrie
	}
	oldTrie, newTrie := openPair()
	actual := make(map[common.Hash][]byte)
	if err := forEachChangedLeaf(oldTrie, newTrie, func(key common.Hash, origin []byte) error {
		if _, exists := actual[key]; exists {
			t.Fatalf("changed leaf %s was emitted more than once", key)
		}
		actual[key] = common.CopyBytes(origin)
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	expected := make(map[common.Hash][]byte)
	oldTrie, newTrie = openPair()
	oldIt, err := oldTrie.NodeIterator(nil)
	if err != nil {
		t.Fatal(err)
	}
	newIt, err := newTrie.NodeIterator(nil)
	if err != nil {
		t.Fatal(err)
	}
	forward, _ := trie.NewDifferenceIterator(oldIt, newIt)
	for forward.Next(true) {
		if forward.Leaf() {
			expected[common.BytesToHash(forward.LeafKey())] = nil
		}
	}
	if err := forward.Error(); err != nil {
		t.Fatal(err)
	}
	oldTrie, newTrie = openPair()
	newIt, err = newTrie.NodeIterator(nil)
	if err != nil {
		t.Fatal(err)
	}
	oldIt, err = oldTrie.NodeIterator(nil)
	if err != nil {
		t.Fatal(err)
	}
	reverse, _ := trie.NewDifferenceIterator(newIt, oldIt)
	for reverse.Next(true) {
		if reverse.Leaf() {
			expected[common.BytesToHash(reverse.LeafKey())] = common.CopyBytes(reverse.LeafBlob())
		}
	}
	if err := reverse.Error(); err != nil {
		t.Fatal(err)
	}

	if len(actual) != len(expected) {
		t.Fatalf("unexpected changed leaf count: have %d want %d", len(actual), len(expected))
	}
	for key, want := range expected {
		got, ok := actual[key]
		if !ok {
			t.Fatalf("changed leaf %s was not emitted", key)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("wrong origin for %s: have %x want %x", key, got, want)
		}
	}
}

func TestDiskBackedArchiveHistoryMatchesInMemoryEncoding(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	trieDB := triedb.NewDatabase(db, triedb.HashDefaults)
	defer trieDB.Close()

	oldRoot, newRoot := buildArchiveHashStatePair(t, db, trieDB)
	accounts, storages, accountCount, storageCount, err := archiveHistoryOrigins(db, trieDB, oldRoot, newRoot)
	if err != nil {
		t.Fatal(err)
	}
	accountIndex, storageIndex, accountData, storageData, err := encodeArchiveHistory(accounts, storages)
	if err != nil {
		t.Fatal(err)
	}
	expected := &encodedArchiveHistory{
		accountIndex: accountIndex,
		storageIndex: storageIndex,
		accountData:  accountData,
		storageData:  storageData,
		accounts:     accountCount,
		storageSlots: storageCount,
	}

	spillDirectory := t.TempDir()
	config := DefaultConfig.ArchiveHistory
	config.SpillDirectory = spillDirectory
	config.SpillCache = 16
	actual, err := archiveHistoryOriginsSpilled(context.Background(), db, trieDB, oldRoot, newRoot, config, "l2chaindata")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(spillDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("archive spill directory was not cleaned: %v", entries)
	}
	if expected.accounts != actual.accounts ||
		expected.storageSlots != actual.storageSlots ||
		!bytes.Equal(expected.accountIndex, actual.accountIndex) ||
		!bytes.Equal(expected.storageIndex, actual.storageIndex) ||
		!bytes.Equal(expected.accountData, actual.accountData) ||
		!bytes.Equal(expected.storageData, actual.storageData) {
		t.Fatalf(
			"disk-backed encoding differs: accounts %d/%d storage %d/%d indexes %x/%x storageIndexes %x/%x accountData %x/%x storageData %x/%x",
			expected.accounts, actual.accounts,
			expected.storageSlots, actual.storageSlots,
			expected.accountIndex, actual.accountIndex,
			expected.storageIndex, actual.storageIndex,
			expected.accountData, actual.accountData,
			expected.storageData, actual.storageData,
		)
	}
}

func TestCompletedArchiveResultSpillRoundTrip(t *testing.T) {
	expected := &encodedArchiveHistory{
		accountIndex: []byte("account-index"),
		storageIndex: []byte("storage-index"),
		accountData:  []byte("account-data"),
		storageData:  []byte("storage-data"),
		accounts:     3,
		storageSlots: 7,
	}
	spill, err := spillEncodedArchiveHistory(t.TempDir(), 42, expected)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := spill.load()
	if err != nil {
		t.Fatal(err)
	}
	if expected.accounts != actual.accounts ||
		expected.storageSlots != actual.storageSlots ||
		!bytes.Equal(expected.accountIndex, actual.accountIndex) ||
		!bytes.Equal(expected.storageIndex, actual.storageIndex) ||
		!bytes.Equal(expected.accountData, actual.accountData) ||
		!bytes.Equal(expected.storageData, actual.storageData) {
		t.Fatalf("completed archive result changed after spill: expected %+v actual %+v", expected, actual)
	}
	directory := spill.directory
	if err := spill.remove(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatalf("completed archive result spill was not removed: %v", err)
	}
}

func TestArchiveResultMemoryLimiter(t *testing.T) {
	limiter := newArchiveResultMemoryLimiter(1)
	if !limiter.tryAcquire(768 * 1024) {
		t.Fatal("initial archive result memory reservation failed")
	}
	if limiter.tryAcquire(512 * 1024) {
		t.Fatal("archive result memory limiter exceeded its budget")
	}
	limiter.release(768 * 1024)
	if !limiter.tryAcquire(512 * 1024) {
		t.Fatal("released archive result memory was not reusable")
	}
	limiter.release(512 * 1024)

	spillAll := newArchiveResultMemoryLimiter(0)
	if spillAll.tryAcquire(1) {
		t.Fatal("zero archive result memory limit retained a result")
	}
}

func TestArchiveHistoryRejectsOversizedSnappySectionBeforeAllocation(t *testing.T) {
	if err := validateArchiveHistorySectionSize("small", 1); err != nil {
		t.Fatalf("small archive history section was rejected: %v", err)
	}
	if err := validateArchiveHistorySectionSize("storage index", math.MaxUint32); err == nil {
		t.Fatal("oversized Snappy section was not rejected")
	}
	if _, err := encodeArchiveHistoryFromSpill(nil, archiveSpillStats{
		storageSlots: math.MaxUint32,
	}); err == nil {
		t.Fatal("oversized spilled history was not rejected before allocation")
	}
}

func TestParallelArchiveHistoryWritesTransitionsInBlockOrder(t *testing.T) {
	src := rawdb.NewMemoryDatabase()
	dst := rawdb.NewMemoryDatabase()
	trieDB := triedb.NewDatabase(src, triedb.HashDefaults)
	defer trieDB.Close()

	oldRoot, newRoot := buildArchiveHashStatePair(t, src, trieDB)
	changedAddress := common.HexToAddress("0x2000")
	changedHash := crypto.Keccak256Hash(changedAddress.Bytes())
	thirdRoot := commitArchiveAccountTrie(t, trieDB, newRoot, map[common.Hash]*types.StateAccount{
		changedHash: {
			Nonce:    5,
			Balance:  uint256.NewInt(500),
			Root:     types.EmptyRootHash,
			CodeHash: types.EmptyCodeHash.Bytes(),
		},
	}, nil)
	if err := trieDB.Commit(thirdRoot, false); err != nil {
		t.Fatalf("commit third account root %s: %v", thirdRoot, err)
	}

	writeCanonicalRootHeader(src, 0, oldRoot)
	writeCanonicalRootHeader(src, 1, newRoot)
	writeCanonicalRootHeader(src, 2, thirdRoot)

	freezer, err := rawdb.NewStateFreezer("", false, false)
	if err != nil {
		t.Fatal(err)
	}
	defer freezer.Close()

	config := DefaultConfig
	config.Dst.ChainData = t.TempDir()
	config.ArchiveHistory.Workers = 2
	config.ArchiveHistory.MaxInFlight = 2
	config.ArchiveHistory.ResultMemoryLimit = 0
	config.ArchiveHistory.SpillGap = 10000
	config.ArchiveHistory.SpillDirectory = t.TempDir()
	config.ArchiveHistory.ProgressEvery = 1
	migrator := NewMigrator(&config)
	migrator.stats.resetArchiveHistory(0, 2)

	stateID, stats, err := migrator.runArchiveHistoryParallel(
		context.Background(),
		src,
		dst,
		freezer,
		0,
		2,
		oldRoot,
		0,
		true,
		0,
		archiveHistoryStats{availableBlocks: 1},
		time.Now(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if stateID != 2 || stats.blocks != 2 || stats.transitions != 2 || stats.availableBlocks != 3 {
		t.Fatalf("unexpected parallel archive result: stateID=%d stats=%+v", stateID, stats)
	}
	if got := rawdb.ReadStateID(dst, oldRoot); got != nil {
		t.Fatalf("parallel helper unexpectedly wrote initial state ID: %v", *got)
	}
	if got := rawdb.ReadStateID(dst, newRoot); got == nil || *got != 1 {
		t.Fatalf("unexpected state ID for block 1 root: %v", got)
	}
	if got := rawdb.ReadStateID(dst, thirdRoot); got == nil || *got != 2 {
		t.Fatalf("unexpected state ID for block 2 root: %v", got)
	}
	if frozen, err := freezer.Ancients(); err != nil || frozen != 2 {
		t.Fatalf("unexpected freezer size: entries=%d err=%v", frozen, err)
	}
	metaOne, _, _, _, _, err := rawdb.ReadStateHistory(freezer, 1)
	if err != nil {
		t.Fatal(err)
	}
	metaTwo, _, _, _, _, err := rawdb.ReadStateHistory(freezer, 2)
	if err != nil {
		t.Fatal(err)
	}
	if want := encodeArchiveHistoryMeta(oldRoot, newRoot, 1); !bytes.Equal(metaOne, want) {
		t.Fatalf("first history record is out of order: have %x want %x", metaOne, want)
	}
	if want := encodeArchiveHistoryMeta(newRoot, thirdRoot, 2); !bytes.Equal(metaTwo, want) {
		t.Fatalf("second history record is out of order: have %x want %x", metaTwo, want)
	}
	if entries, err := os.ReadDir(config.ArchiveHistory.SpillDirectory); err != nil {
		t.Fatal(err)
	} else if len(entries) != 0 {
		t.Fatalf("parallel archive result spill directory was not cleaned: %v", entries)
	}
}

func writeCanonicalRootHeader(db ethdb.Database, number uint64, root common.Hash) {
	header := &types.Header{
		Number: new(big.Int).SetUint64(number),
		Root:   root,
	}
	rawdb.WriteHeader(db, header)
	rawdb.WriteCanonicalHash(db, header.Hash(), number)
}

func buildArchiveHashStatePair(t *testing.T, db ethdb.Database, trieDB *triedb.Database) (common.Hash, common.Hash) {
	t.Helper()

	addresses := []common.Address{
		common.HexToAddress("0x3000"),
		common.HexToAddress("0x1000"),
		common.HexToAddress("0x2000"),
	}
	accountHashes := make([]common.Hash, len(addresses))
	preimages := make(map[common.Hash][]byte, len(addresses))
	for i, address := range addresses {
		accountHashes[i] = crypto.Keccak256Hash(address.Bytes())
		preimages[accountHashes[i]] = common.CopyBytes(address.Bytes())
	}
	rawdb.WritePreimages(db, preimages)

	slotOne := crypto.Keccak256Hash(common.LeftPadBytes([]byte{1}, common.HashLength))
	slotTwo := crypto.Keccak256Hash(common.LeftPadBytes([]byte{2}, common.HashLength))
	oldStorage := commitArchiveStorageTrie(t, db, trieDB, types.EmptyRootHash, accountHashes[0], types.EmptyRootHash, map[common.Hash]uint64{
		slotOne: 11,
	})

	oldAccounts := map[common.Hash]*types.StateAccount{
		accountHashes[0]: {
			Nonce: 1, Balance: uint256.NewInt(100), Root: oldStorage, CodeHash: types.EmptyCodeHash.Bytes(),
		},
		accountHashes[1]: {
			Nonce: 2, Balance: uint256.NewInt(200), Root: types.EmptyRootHash, CodeHash: types.EmptyCodeHash.Bytes(),
		},
	}
	oldRoot := commitArchiveAccountTrie(t, trieDB, types.EmptyRootHash, oldAccounts, nil)
	if err := trieDB.Commit(oldRoot, false); err != nil {
		t.Fatalf("commit old account root %s: %v", oldRoot, err)
	}
	if !rawdb.HasLegacyTrieNode(db, oldStorage) {
		t.Fatalf("old storage root %s disappeared after account commit", oldStorage)
	}

	newStorage := commitArchiveStorageTrie(t, db, trieDB, oldRoot, accountHashes[0], oldStorage, map[common.Hash]uint64{
		slotOne: 22,
		slotTwo: 33,
	})

	newAccounts := map[common.Hash]*types.StateAccount{
		accountHashes[0]: {
			Nonce: 3, Balance: uint256.NewInt(300), Root: newStorage, CodeHash: types.EmptyCodeHash.Bytes(),
		},
		accountHashes[2]: {
			Nonce: 4, Balance: uint256.NewInt(400), Root: types.EmptyRootHash, CodeHash: types.EmptyCodeHash.Bytes(),
		},
	}
	newRoot := commitArchiveAccountTrie(t, trieDB, oldRoot, newAccounts, []common.Hash{accountHashes[1]})
	if err := trieDB.Commit(newRoot, false); err != nil {
		t.Fatalf("commit new account root %s: %v", newRoot, err)
	}
	return oldRoot, newRoot
}

func commitArchiveStorageTrie(
	t *testing.T,
	db ethdb.Database,
	trieDB *triedb.Database,
	stateRoot common.Hash,
	accountHash common.Hash,
	parentRoot common.Hash,
	values map[common.Hash]uint64,
) common.Hash {
	t.Helper()
	storageTrie, err := trie.New(trie.StorageTrieID(stateRoot, accountHash, parentRoot), trieDB)
	if err != nil {
		t.Fatalf("open storage trie root %s: %v", parentRoot, err)
	}
	for slot, value := range values {
		blob, err := rlp.EncodeToBytes(uint256.NewInt(value))
		if err != nil {
			t.Fatal(err)
		}
		storageTrie.MustUpdate(slot.Bytes(), blob)
	}
	root, nodes := storageTrie.Commit(false)
	if _, ok := nodes.HashSet()[root]; !ok {
		t.Fatalf("committed storage root %s is absent from node set", root)
	}
	// Persist each storage trie independently so both historical endpoints stay
	// available to the diff.
	if err := trieDB.Update(root, types.EmptyRootHash, 0, trienode.NewWithNodeSet(nodes), nil); err != nil {
		t.Fatal(err)
	}
	if err := trieDB.Commit(root, false); err != nil {
		t.Fatalf("commit storage trie root %s: %v", root, err)
	}
	if !rawdb.HasLegacyTrieNode(db, root) {
		t.Fatalf("committed storage trie root %s was not persisted", root)
	}
	return root
}

func commitArchiveAccountTrie(
	t *testing.T,
	trieDB *triedb.Database,
	parentRoot common.Hash,
	accounts map[common.Hash]*types.StateAccount,
	deleted []common.Hash,
) common.Hash {
	t.Helper()
	accountTrie, err := trie.New(trie.TrieID(parentRoot), trieDB)
	if err != nil {
		t.Fatalf("open account trie root %s: %v", parentRoot, err)
	}
	for accountHash, account := range accounts {
		blob, err := rlp.EncodeToBytes(account)
		if err != nil {
			t.Fatal(err)
		}
		accountTrie.MustUpdate(accountHash.Bytes(), blob)
	}
	for _, accountHash := range deleted {
		accountTrie.MustDelete(accountHash.Bytes())
	}
	// Collect account leaves so hashdb can link and flush the referenced
	// storage tries as part of the complete state root.
	root, nodes := accountTrie.Commit(true)
	if err := trieDB.Update(root, types.EmptyRootHash, 0, trienode.NewWithNodeSet(nodes), nil); err != nil {
		t.Fatal(err)
	}
	return root
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
