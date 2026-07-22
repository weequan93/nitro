// Copyright 2023-2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md
package pathdbmigrate

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/ethdb"
	pebbledb "github.com/ethereum/go-ethereum/ethdb/pebble"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/triedb"
)

const (
	archiveSpillAccountPrefix = byte(0x01)
	archiveSpillStoragePrefix = byte(0x02)
	archiveSpillBatchSize     = 16 * 1024 * 1024
	archiveSpillLogEvery      = 100000
)

type encodedArchiveHistory struct {
	accountIndex []byte
	storageIndex []byte
	accountData  []byte
	storageData  []byte
	accounts     uint64
	storageSlots uint64
}

type archiveSpillStats struct {
	accounts         uint64
	storageSlots     uint64
	accountDataBytes uint64
	storageDataBytes uint64
}

type archiveSpillWriter struct {
	store ethdb.KeyValueStore
	batch ethdb.Batch
}

func newArchiveSpillWriter(store ethdb.KeyValueStore) *archiveSpillWriter {
	return &archiveSpillWriter{store: store, batch: store.NewBatchWithSize(archiveSpillBatchSize)}
}

func (w *archiveSpillWriter) put(key []byte, value []byte) error {
	if err := w.batch.Put(key, value); err != nil {
		if !errors.Is(err, ethdb.ErrBatchTooLarge) {
			return err
		}
		if err := w.flush(); err != nil {
			return err
		}
		if err := w.batch.Put(key, value); err != nil {
			return err
		}
	}
	if w.batch.ValueSize() >= archiveSpillBatchSize {
		return w.flush()
	}
	return nil
}

func (w *archiveSpillWriter) flush() error {
	if w.batch.ValueSize() == 0 {
		return nil
	}
	if err := w.batch.Write(); err != nil {
		return err
	}
	w.batch.Reset()
	return nil
}

type differenceLeafStream struct {
	it   trie.NodeIterator
	ok   bool
	key  common.Hash
	blob []byte
}

func newDifferenceLeafStream(base *trie.Trie, target *trie.Trie) (*differenceLeafStream, error) {
	baseIt, err := base.NodeIterator(nil)
	if err != nil {
		return nil, err
	}
	targetIt, err := target.NodeIterator(nil)
	if err != nil {
		return nil, err
	}
	diff, _ := trie.NewDifferenceIterator(baseIt, targetIt)
	stream := &differenceLeafStream{it: diff}
	if err := stream.advance(); err != nil {
		return nil, err
	}
	return stream, nil
}

func (s *differenceLeafStream) advance() error {
	for s.it.Next(true) {
		if !s.it.Leaf() {
			continue
		}
		key := s.it.LeafKey()
		if len(key) != common.HashLength {
			return fmt.Errorf("unexpected trie leaf key length %d", len(key))
		}
		s.key = common.BytesToHash(key)
		s.blob = common.CopyBytes(s.it.LeafBlob())
		s.ok = true
		return nil
	}
	s.ok = false
	s.blob = nil
	return s.it.Error()
}

// forEachChangedLeaf emits the union of changed leaf keys in lexical trie-key order.
// origin is the value in base, or nil when the leaf did not exist in base.
func forEachChangedLeaf(base *trie.Trie, target *trie.Trie, callback func(key common.Hash, origin []byte) error) error {
	forward, err := newDifferenceLeafStream(base, target)
	if err != nil {
		return err
	}
	reverse, err := newDifferenceLeafStream(target, base)
	if err != nil {
		return err
	}
	for forward.ok || reverse.ok {
		switch {
		case !reverse.ok || (forward.ok && forward.key.Cmp(reverse.key) < 0):
			if err := callback(forward.key, nil); err != nil {
				return err
			}
			if err := forward.advance(); err != nil {
				return err
			}
		case !forward.ok || reverse.key.Cmp(forward.key) < 0:
			if err := callback(reverse.key, reverse.blob); err != nil {
				return err
			}
			if err := reverse.advance(); err != nil {
				return err
			}
		default:
			if err := callback(forward.key, reverse.blob); err != nil {
				return err
			}
			if err := forward.advance(); err != nil {
				return err
			}
			if err := reverse.advance(); err != nil {
				return err
			}
		}
	}
	return nil
}

func archiveSpillAccountKey(address common.Address) []byte {
	key := make([]byte, 1+common.AddressLength)
	key[0] = archiveSpillAccountPrefix
	copy(key[1:], address.Bytes())
	return key
}

func archiveSpillStorageKey(address common.Address, slot common.Hash) []byte {
	key := make([]byte, 1+common.AddressLength+common.HashLength)
	key[0] = archiveSpillStoragePrefix
	copy(key[1:], address.Bytes())
	copy(key[1+common.AddressLength:], slot.Bytes())
	return key
}

func collectArchiveStorageOriginsToSpill(
	ctx context.Context,
	src ethdb.KeyValueReader,
	trieDB *triedb.Database,
	parentRoot common.Hash,
	root common.Hash,
	accountHash common.Hash,
	address common.Address,
	oldRoot common.Hash,
	newRoot common.Hash,
	writer *archiveSpillWriter,
	stats *archiveSpillStats,
) (uint32, error) {
	if !archiveTrieRootAvailable(src, oldRoot) {
		return 0, fmt.Errorf("%w: parent storage root %s", errArchiveStateUnavailable, oldRoot)
	}
	if !archiveTrieRootAvailable(src, newRoot) {
		return 0, fmt.Errorf("%w: storage root %s", errArchiveStateUnavailable, newRoot)
	}
	oldTrie, err := trie.New(trie.StorageTrieID(parentRoot, accountHash, oldRoot), trieDB)
	if err != nil {
		return 0, fmt.Errorf("open parent storage trie %s: %w", oldRoot, err)
	}
	newTrie, err := trie.New(trie.StorageTrieID(root, accountHash, newRoot), trieDB)
	if err != nil {
		return 0, fmt.Errorf("open storage trie %s: %w", newRoot, err)
	}
	var slots uint64
	err = forEachChangedLeaf(oldTrie, newTrie, func(slotHash common.Hash, origin []byte) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(origin) > math.MaxUint8 {
			return fmt.Errorf("origin storage slot %s for %s too large: %d bytes", slotHash, address, len(origin))
		}
		if err := writer.put(archiveSpillStorageKey(address, slotHash), origin); err != nil {
			return err
		}
		slots++
		stats.storageSlots++
		stats.storageDataBytes += uint64(len(origin))
		if stats.storageSlots%archiveSpillLogEvery == 0 {
			log.Info("Spilling archive storage changes", "accounts", stats.accounts, "storageSlots", stats.storageSlots)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	if slots > math.MaxUint32 {
		return 0, fmt.Errorf("too many changed storage slots for %s: %d", address, slots)
	}
	return uint32(slots), nil
}

func collectArchiveHistoryOriginsToSpill(
	ctx context.Context,
	src ethdb.KeyValueReader,
	trieDB *triedb.Database,
	parentRoot common.Hash,
	root common.Hash,
	store ethdb.KeyValueStore,
) (archiveSpillStats, error) {
	if !archiveTrieRootAvailable(src, parentRoot) {
		return archiveSpillStats{}, fmt.Errorf("%w: parent account root %s", errArchiveStateUnavailable, parentRoot)
	}
	if !archiveTrieRootAvailable(src, root) {
		return archiveSpillStats{}, fmt.Errorf("%w: account root %s", errArchiveStateUnavailable, root)
	}
	oldTrie, err := trie.New(trie.TrieID(parentRoot), trieDB)
	if err != nil {
		return archiveSpillStats{}, fmt.Errorf("open parent account trie %s: %w", parentRoot, err)
	}
	newTrie, err := trie.New(trie.TrieID(root), trieDB)
	if err != nil {
		return archiveSpillStats{}, fmt.Errorf("open account trie %s: %w", root, err)
	}
	writer := newArchiveSpillWriter(store)
	stats := archiveSpillStats{}
	err = forEachChangedLeaf(oldTrie, newTrie, func(accountHash common.Hash, oldBlob []byte) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		preimage := rawdb.ReadPreimage(src, accountHash)
		if len(preimage) != common.AddressLength {
			return fmt.Errorf("missing account preimage for hash %s; full archive history needs account preimages", accountHash)
		}
		address := common.BytesToAddress(preimage)
		oldAccount, oldOrigin, err := decodeArchiveAccountOrigin(accountHash, oldBlob)
		if err != nil {
			return err
		}
		if len(oldOrigin) > math.MaxUint8 {
			return fmt.Errorf("origin account for %s too large: %d bytes", address, len(oldOrigin))
		}
		newAccount, err := readArchiveAccount(newTrie, accountHash)
		if err != nil {
			return err
		}
		oldStorageRoot := accountStorageRoot(oldAccount)
		newStorageRoot := accountStorageRoot(newAccount)
		var slots uint32
		if oldStorageRoot != newStorageRoot {
			slots, err = collectArchiveStorageOriginsToSpill(
				ctx, src, trieDB, parentRoot, root, accountHash, address,
				oldStorageRoot, newStorageRoot, writer, &stats,
			)
			if err != nil {
				return fmt.Errorf("account %s storage history: %w", address, err)
			}
		}
		value := make([]byte, 4+len(oldOrigin))
		binary.BigEndian.PutUint32(value[:4], slots)
		copy(value[4:], oldOrigin)
		if err := writer.put(archiveSpillAccountKey(address), value); err != nil {
			return err
		}
		stats.accounts++
		stats.accountDataBytes += uint64(len(oldOrigin))
		if stats.accounts%archiveSpillLogEvery == 0 {
			log.Info("Spilling archive account changes", "accounts", stats.accounts, "storageSlots", stats.storageSlots)
		}
		return nil
	})
	if err != nil {
		return archiveSpillStats{}, err
	}
	if err := writer.flush(); err != nil {
		return archiveSpillStats{}, err
	}
	if stats.accounts == 0 {
		return archiveSpillStats{}, errors.New("state roots differ but no account changes were found")
	}
	return stats, nil
}

func checkedArchiveAllocation(count uint64, itemSize int, label string) ([]byte, error) {
	if count > uint64(math.MaxInt/itemSize) {
		return nil, fmt.Errorf("%s is too large: %d entries", label, count)
	}
	return make([]byte, int(count)*itemSize), nil
}

func checkedArchiveDataAllocation(size uint64, label string) ([]byte, error) {
	if size > math.MaxUint32 {
		return nil, fmt.Errorf("%s is too large: %d bytes", label, size)
	}
	return make([]byte, int(size)), nil
}

func encodeArchiveHistoryFromSpill(store ethdb.KeyValueStore, stats archiveSpillStats) (*encodedArchiveHistory, error) {
	if stats.storageSlots > math.MaxUint32 {
		return nil, fmt.Errorf("too many storage slots in archive history: %d", stats.storageSlots)
	}
	accountIndex, err := checkedArchiveAllocation(stats.accounts, accountIndexSize, "account index")
	if err != nil {
		return nil, err
	}
	storageIndex, err := checkedArchiveAllocation(stats.storageSlots, storageIndexSizeForArchiveHistory, "storage index")
	if err != nil {
		return nil, err
	}
	accountData, err := checkedArchiveDataAllocation(stats.accountDataBytes, "account data")
	if err != nil {
		return nil, err
	}
	storageData, err := checkedArchiveDataAllocation(stats.storageDataBytes, "storage data")
	if err != nil {
		return nil, err
	}

	accountIt := store.NewIterator([]byte{archiveSpillAccountPrefix}, nil)
	defer accountIt.Release()
	var accountNumber, accountDataOffset, storageOffset uint64
	for accountIt.Next() {
		key, value := accountIt.Key(), accountIt.Value()
		if len(key) != 1+common.AddressLength || len(value) < 4 {
			return nil, errors.New("corrupt archive account spill entry")
		}
		if accountNumber >= stats.accounts {
			return nil, errors.New("archive account spill contains more entries than expected")
		}
		origin := value[4:]
		slots := uint64(binary.BigEndian.Uint32(value[:4]))
		if storageOffset+slots > math.MaxUint32 {
			return nil, fmt.Errorf("too many storage slots in archive history: %d", storageOffset+slots)
		}
		entry := accountIndex[accountNumber*accountIndexSize : (accountNumber+1)*accountIndexSize]
		copy(entry[:common.AddressLength], key[1:])
		entry[common.AddressLength] = uint8(len(origin))
		binary.BigEndian.PutUint32(entry[common.AddressLength+1:common.AddressLength+5], uint32(accountDataOffset))
		binary.BigEndian.PutUint32(entry[common.AddressLength+5:common.AddressLength+9], uint32(storageOffset))
		binary.BigEndian.PutUint32(entry[common.AddressLength+9:common.AddressLength+13], uint32(slots))
		copy(accountData[accountDataOffset:], origin)
		accountDataOffset += uint64(len(origin))
		storageOffset += slots
		accountNumber++
	}
	if err := accountIt.Error(); err != nil {
		return nil, err
	}
	if accountNumber != stats.accounts || accountDataOffset != stats.accountDataBytes || storageOffset != stats.storageSlots {
		return nil, fmt.Errorf(
			"archive account spill size mismatch: accounts %d/%d accountData %d/%d storageSlots %d/%d",
			accountNumber, stats.accounts, accountDataOffset, stats.accountDataBytes, storageOffset, stats.storageSlots,
		)
	}

	storageIt := store.NewIterator([]byte{archiveSpillStoragePrefix}, nil)
	defer storageIt.Release()
	var storageNumber, storageDataOffset uint64
	for storageIt.Next() {
		key, origin := storageIt.Key(), storageIt.Value()
		if len(key) != 1+common.AddressLength+common.HashLength {
			return nil, errors.New("corrupt archive storage spill entry")
		}
		if storageNumber >= stats.storageSlots {
			return nil, errors.New("archive storage spill contains more entries than expected")
		}
		entry := storageIndex[storageNumber*storageIndexSizeForArchiveHistory : (storageNumber+1)*storageIndexSizeForArchiveHistory]
		copy(entry[:common.HashLength], key[1+common.AddressLength:])
		entry[common.HashLength] = uint8(len(origin))
		binary.BigEndian.PutUint32(entry[common.HashLength+1:common.HashLength+5], uint32(storageDataOffset))
		copy(storageData[storageDataOffset:], origin)
		storageDataOffset += uint64(len(origin))
		storageNumber++
	}
	if err := storageIt.Error(); err != nil {
		return nil, err
	}
	if storageNumber != stats.storageSlots || storageDataOffset != stats.storageDataBytes {
		return nil, fmt.Errorf(
			"archive storage spill size mismatch: slots %d/%d storageData %d/%d",
			storageNumber, stats.storageSlots, storageDataOffset, stats.storageDataBytes,
		)
	}
	return &encodedArchiveHistory{
		accountIndex: accountIndex,
		storageIndex: storageIndex,
		accountData:  accountData,
		storageData:  storageData,
		accounts:     stats.accounts,
		storageSlots: stats.storageSlots,
	}, nil
}

func archiveSpillDirectory(config ArchiveHistoryConfig, dstChainData string) (string, string) {
	parent := config.SpillDirectory
	if parent == "" {
		parent = filepath.Dir(dstChainData)
	}
	prefix := "." + filepath.Base(dstChainData) + ".pathdb-archive-spill-"
	return parent, prefix
}

func removeStaleArchiveSpills(parent string, prefix string) error {
	entries, err := os.ReadDir(parent)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), prefix) {
			if err := os.RemoveAll(filepath.Join(parent, entry.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

func archiveHistoryOriginsSpilled(
	ctx context.Context,
	src ethdb.KeyValueReader,
	trieDB *triedb.Database,
	parentRoot common.Hash,
	root common.Hash,
	config ArchiveHistoryConfig,
	dstChainData string,
) (*encodedArchiveHistory, error) {
	parent, prefix := archiveSpillDirectory(config, dstChainData)
	if err := os.MkdirAll(parent, 0755); err != nil {
		return nil, fmt.Errorf("create archive spill parent: %w", err)
	}
	if err := removeStaleArchiveSpills(parent, prefix); err != nil {
		return nil, fmt.Errorf("remove stale archive spill: %w", err)
	}
	directory, err := os.MkdirTemp(parent, prefix)
	if err != nil {
		return nil, fmt.Errorf("create archive spill directory: %w", err)
	}
	started := time.Now()
	log.Warn("Using disk-backed archive trie diff", "directory", directory, "parentRoot", parentRoot, "root", root, "cacheMB", config.SpillCache)

	maxCompactions := func() int { return 1 }
	store, err := pebbledb.New(directory, config.SpillCache, 64, "pathdb-migrate-archive-spill/", false, &pebbledb.ExtraOptions{
		MaxConcurrentCompactions: maxCompactions,
	})
	if err != nil {
		_ = os.RemoveAll(directory)
		return nil, fmt.Errorf("open archive spill database: %w", err)
	}
	stats, collectErr := collectArchiveHistoryOriginsToSpill(ctx, src, trieDB, parentRoot, root, store)
	if collectErr == nil {
		log.Info(
			"Archive trie diff spilled to disk",
			"accounts", stats.accounts,
			"storageSlots", stats.storageSlots,
			"accountDataBytes", stats.accountDataBytes,
			"storageDataBytes", stats.storageDataBytes,
			"elapsed", time.Since(started),
		)
	}
	var encoded *encodedArchiveHistory
	if collectErr == nil {
		encoded, collectErr = encodeArchiveHistoryFromSpill(store, stats)
	}
	closeErr := store.Close()
	removeErr := os.RemoveAll(directory)
	if collectErr != nil {
		return nil, collectErr
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close archive spill database: %w", closeErr)
	}
	if removeErr != nil {
		return nil, fmt.Errorf("remove archive spill database: %w", removeErr)
	}
	log.Info(
		"Disk-backed archive trie diff encoded",
		"accounts", encoded.accounts,
		"storageSlots", encoded.storageSlots,
		"accountIndexBytes", len(encoded.accountIndex),
		"storageIndexBytes", len(encoded.storageIndex),
		"accountDataBytes", len(encoded.accountData),
		"storageDataBytes", len(encoded.storageData),
		"elapsed", time.Since(started),
	)
	return encoded, nil
}
