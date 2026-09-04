// Copyright 2023-2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md
package pathdbmigrate

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/triedb"
)

const (
	archiveSpillLogEvery       = 100000
	archiveSpoolMinBufferSize  = 64 * 1024
	archiveSpoolMaxBufferSize  = 16 * 1024 * 1024
	archiveSpoolReadBufferSize = 1024 * 1024
	archiveSpoolSlotHeaderSize = common.HashLength + 1
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

type archiveStorageSpoolRef struct {
	path   string
	offset uint64
	size   uint64
	slots  uint32
}

type archiveSpoolAccount struct {
	address common.Address
	origin  []byte
	storage archiveStorageSpoolRef
}

type archiveStorageSpoolJob struct {
	parentRoot  common.Hash
	root        common.Hash
	accountHash common.Hash
	address     common.Address
	origin      []byte
	oldRoot     common.Hash
	newRoot     common.Hash
}

type archiveStorageSpoolResult struct {
	account archiveSpoolAccount
	stats   archiveSpillStats
}

type archiveStorageSpool struct {
	path   string
	file   *os.File
	writer *bufio.Writer
	offset uint64
}

func validateArchiveSpillStats(stats archiveSpillStats) error {
	if stats.storageSlots > math.MaxUint32 {
		return fmt.Errorf("too many storage slots in archive history: %d", stats.storageSlots)
	}
	if stats.accounts > math.MaxUint64/accountIndexSize {
		return fmt.Errorf("account index is too large: %d entries", stats.accounts)
	}
	if stats.storageSlots > math.MaxUint64/storageIndexSizeForArchiveHistory {
		return fmt.Errorf("storage index is too large: %d entries", stats.storageSlots)
	}
	sections := []struct {
		name string
		size uint64
	}{
		{name: "account index", size: stats.accounts * accountIndexSize},
		{name: "storage index", size: stats.storageSlots * storageIndexSizeForArchiveHistory},
		{name: "account data", size: stats.accountDataBytes},
		{name: "storage data", size: stats.storageDataBytes},
	}
	for _, section := range sections {
		if err := validateArchiveHistorySectionSize(section.name, section.size); err != nil {
			return err
		}
	}
	return nil
}

func validateArchiveStorageSpillStats(stats archiveSpillStats) error {
	if stats.storageSlots > math.MaxUint64/storageIndexSizeForArchiveHistory {
		return fmt.Errorf("storage index is too large: %d entries", stats.storageSlots)
	}
	if err := validateArchiveHistorySectionSize("storage index", stats.storageSlots*storageIndexSizeForArchiveHistory); err != nil {
		return err
	}
	return validateArchiveHistorySectionSize("storage data", stats.storageDataBytes)
}

func archiveSpoolBufferSize(totalMB int, workers int) int {
	size := totalMB * 1024 * 1024 / workers
	if size < archiveSpoolMinBufferSize {
		return archiveSpoolMinBufferSize
	}
	if size > archiveSpoolMaxBufferSize {
		return archiveSpoolMaxBufferSize
	}
	return size
}

func newArchiveStorageSpool(directory string, worker int, bufferSize int) (*archiveStorageSpool, error) {
	path := filepath.Join(directory, fmt.Sprintf("storage-%03d.spool", worker))
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	return &archiveStorageSpool{
		path:   path,
		file:   file,
		writer: bufio.NewWriterSize(file, bufferSize),
	}, nil
}

func (s *archiveStorageSpool) writeSlot(slot common.Hash, origin []byte) error {
	var header [archiveSpoolSlotHeaderSize]byte
	copy(header[:common.HashLength], slot.Bytes())
	header[common.HashLength] = byte(len(origin))
	if _, err := s.writer.Write(header[:]); err != nil {
		return err
	}
	if len(origin) != 0 {
		if _, err := s.writer.Write(origin); err != nil {
			return err
		}
	}
	s.offset += uint64(len(header) + len(origin))
	return nil
}

func (s *archiveStorageSpool) close() error {
	return errors.Join(s.writer.Flush(), s.file.Close())
}

type archiveTrieCursor struct {
	it trie.NodeIterator
	ok bool
}

func newArchiveTrieCursor(tr *trie.Trie) (*archiveTrieCursor, error) {
	it, err := tr.NodeIterator(nil)
	if err != nil {
		return nil, err
	}
	cursor := &archiveTrieCursor{it: it}
	if err := cursor.advance(true); err != nil {
		return nil, err
	}
	return cursor, nil
}

func (c *archiveTrieCursor) advance(descend bool) error {
	c.ok = c.it.Next(descend)
	if !c.ok {
		return c.it.Error()
	}
	return nil
}

func archiveCursorLeaf(cursor *archiveTrieCursor) (common.Hash, []byte, error) {
	key := cursor.it.LeafKey()
	if len(key) != common.HashLength {
		return common.Hash{}, nil, fmt.Errorf("unexpected trie leaf key length %d", len(key))
	}
	return common.BytesToHash(key), cursor.it.LeafBlob(), nil
}

func emitArchiveCursorLeaf(
	cursor *archiveTrieCursor,
	origin bool,
	callback func(key common.Hash, origin []byte) error,
) error {
	key, blob, err := archiveCursorLeaf(cursor)
	if err != nil {
		return err
	}
	if !origin {
		blob = nil
	} else {
		blob = common.CopyBytes(blob)
	}
	return callback(key, blob)
}

// forEachChangedLeaf traverses both secure tries once, emits every changed leaf,
// and skips identical hashed subtrees. Origin is the value in base, or nil when
// the leaf did not exist in base.
func forEachChangedLeaf(base *trie.Trie, target *trie.Trie, callback func(key common.Hash, origin []byte) error) error {
	baseCursor, err := newArchiveTrieCursor(base)
	if err != nil {
		return err
	}
	targetCursor, err := newArchiveTrieCursor(target)
	if err != nil {
		return err
	}
	advanceBoth := func(descend bool) error {
		baseErr := baseCursor.advance(descend)
		targetErr := targetCursor.advance(descend)
		if baseErr != nil {
			return baseErr
		}
		return targetErr
	}
	for baseCursor.ok || targetCursor.ok {
		switch {
		case !baseCursor.ok:
			if targetCursor.it.Leaf() {
				if err := emitArchiveCursorLeaf(targetCursor, false, callback); err != nil {
					return err
				}
			}
			if err := targetCursor.advance(true); err != nil {
				return err
			}
			continue
		case !targetCursor.ok:
			if baseCursor.it.Leaf() {
				if err := emitArchiveCursorLeaf(baseCursor, true, callback); err != nil {
					return err
				}
			}
			if err := baseCursor.advance(true); err != nil {
				return err
			}
			continue
		}

		pathCmp := bytes.Compare(baseCursor.it.Path(), targetCursor.it.Path())
		if pathCmp < 0 {
			if baseCursor.it.Leaf() {
				if err := emitArchiveCursorLeaf(baseCursor, true, callback); err != nil {
					return err
				}
			}
			if err := baseCursor.advance(true); err != nil {
				return err
			}
			continue
		}
		if pathCmp > 0 {
			if targetCursor.it.Leaf() {
				if err := emitArchiveCursorLeaf(targetCursor, false, callback); err != nil {
					return err
				}
			}
			if err := targetCursor.advance(true); err != nil {
				return err
			}
			continue
		}

		baseLeaf := baseCursor.it.Leaf()
		targetLeaf := targetCursor.it.Leaf()
		switch {
		case baseLeaf && targetLeaf:
			baseKey, baseBlob, err := archiveCursorLeaf(baseCursor)
			if err != nil {
				return err
			}
			targetKey, targetBlob, err := archiveCursorLeaf(targetCursor)
			if err != nil {
				return err
			}
			if baseKey != targetKey {
				return fmt.Errorf("trie leaves at the same path have different keys: %s and %s", baseKey, targetKey)
			}
			if !bytes.Equal(baseBlob, targetBlob) {
				if err := callback(baseKey, common.CopyBytes(baseBlob)); err != nil {
					return err
				}
			}
			if err := advanceBoth(true); err != nil {
				return err
			}
		case baseLeaf:
			if err := emitArchiveCursorLeaf(baseCursor, true, callback); err != nil {
				return err
			}
			if err := baseCursor.advance(true); err != nil {
				return err
			}
		case targetLeaf:
			if err := emitArchiveCursorLeaf(targetCursor, false, callback); err != nil {
				return err
			}
			if err := targetCursor.advance(true); err != nil {
				return err
			}
		default:
			baseHash := baseCursor.it.Hash()
			targetHash := targetCursor.it.Hash()
			descend := baseHash == (common.Hash{}) || baseHash != targetHash
			if err := advanceBoth(descend); err != nil {
				return err
			}
		}
	}
	return nil
}

func collectArchiveStorageOriginsToSpool(
	ctx context.Context,
	src ethdb.KeyValueReader,
	trieDB *triedb.Database,
	job archiveStorageSpoolJob,
	spool *archiveStorageSpool,
) (archiveStorageSpoolRef, archiveSpillStats, error) {
	if !archiveTrieRootAvailable(src, job.oldRoot) {
		return archiveStorageSpoolRef{}, archiveSpillStats{}, fmt.Errorf("%w: parent storage root %s", errArchiveStateUnavailable, job.oldRoot)
	}
	if !archiveTrieRootAvailable(src, job.newRoot) {
		return archiveStorageSpoolRef{}, archiveSpillStats{}, fmt.Errorf("%w: storage root %s", errArchiveStateUnavailable, job.newRoot)
	}
	oldTrie, err := trie.New(trie.StorageTrieID(job.parentRoot, job.accountHash, job.oldRoot), trieDB)
	if err != nil {
		return archiveStorageSpoolRef{}, archiveSpillStats{}, fmt.Errorf("open parent storage trie %s: %w", job.oldRoot, err)
	}
	newTrie, err := trie.New(trie.StorageTrieID(job.root, job.accountHash, job.newRoot), trieDB)
	if err != nil {
		return archiveStorageSpoolRef{}, archiveSpillStats{}, fmt.Errorf("open storage trie %s: %w", job.newRoot, err)
	}
	start := spool.offset
	stats := archiveSpillStats{}
	err = forEachChangedLeaf(oldTrie, newTrie, func(slotHash common.Hash, origin []byte) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(origin) > math.MaxUint8 {
			return fmt.Errorf("origin storage slot %s for %s too large: %d bytes", slotHash, job.address, len(origin))
		}
		stats.storageSlots++
		stats.storageDataBytes += uint64(len(origin))
		if err := validateArchiveStorageSpillStats(stats); err != nil {
			return err
		}
		if err := spool.writeSlot(slotHash, origin); err != nil {
			return err
		}
		if stats.storageSlots%archiveSpillLogEvery == 0 {
			log.Info(
				"Spooling archive account storage",
				"address", job.address,
				"accountSlots", stats.storageSlots,
			)
		}
		return nil
	})
	if err != nil {
		return archiveStorageSpoolRef{}, archiveSpillStats{}, err
	}
	if stats.storageSlots > math.MaxUint32 {
		return archiveStorageSpoolRef{}, archiveSpillStats{}, fmt.Errorf("too many changed storage slots for %s: %d", job.address, stats.storageSlots)
	}
	return archiveStorageSpoolRef{
		path:   spool.path,
		offset: start,
		size:   spool.offset - start,
		slots:  uint32(stats.storageSlots),
	}, stats, nil
}

func collectArchiveHistoryOriginsToSpill(
	ctx context.Context,
	src ethdb.KeyValueReader,
	trieDB *triedb.Database,
	parentRoot common.Hash,
	root common.Hash,
	directory string,
	workers int,
	bufferMB int,
) ([]archiveSpoolAccount, archiveSpillStats, error) {
	if !archiveTrieRootAvailable(src, parentRoot) {
		return nil, archiveSpillStats{}, fmt.Errorf("%w: parent account root %s", errArchiveStateUnavailable, parentRoot)
	}
	if !archiveTrieRootAvailable(src, root) {
		return nil, archiveSpillStats{}, fmt.Errorf("%w: account root %s", errArchiveStateUnavailable, root)
	}
	oldTrie, err := trie.New(trie.TrieID(parentRoot), trieDB)
	if err != nil {
		return nil, archiveSpillStats{}, fmt.Errorf("open parent account trie %s: %w", parentRoot, err)
	}
	newTrie, err := trie.New(trie.TrieID(root), trieDB)
	if err != nil {
		return nil, archiveSpillStats{}, fmt.Errorf("open account trie %s: %w", root, err)
	}
	if workers < 1 {
		workers = 1
	}
	bufferSize := archiveSpoolBufferSize(bufferMB, workers)
	spools := make([]*archiveStorageSpool, 0, workers)
	for worker := 0; worker < workers; worker++ {
		spool, err := newArchiveStorageSpool(directory, worker, bufferSize)
		if err != nil {
			for _, opened := range spools {
				_ = opened.close()
			}
			return nil, archiveSpillStats{}, fmt.Errorf("create archive storage spool: %w", err)
		}
		spools = append(spools, spool)
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan archiveStorageSpoolJob, workers*2)
	results := make(chan archiveStorageSpoolResult, workers*2)
	firstErr := make(chan error, 1)
	reportError := func(err error) {
		if err == nil {
			return
		}
		select {
		case firstErr <- err:
			cancel()
		default:
		}
	}

	var workerWG sync.WaitGroup
	for _, spool := range spools {
		workerWG.Add(1)
		go func(spool *archiveStorageSpool) {
			defer workerWG.Done()
			defer func() { reportError(spool.close()) }()
			for {
				select {
				case job, ok := <-jobs:
					if !ok {
						return
					}
					result := archiveStorageSpoolResult{
						account: archiveSpoolAccount{address: job.address, origin: job.origin},
						stats: archiveSpillStats{
							accounts:         1,
							accountDataBytes: uint64(len(job.origin)),
						},
					}
					if job.oldRoot != job.newRoot {
						storage, storageStats, err := collectArchiveStorageOriginsToSpool(runCtx, src, trieDB, job, spool)
						if err != nil {
							reportError(fmt.Errorf("account %s storage history: %w", job.address, err))
							return
						}
						result.account.storage = storage
						result.stats.storageSlots = storageStats.storageSlots
						result.stats.storageDataBytes = storageStats.storageDataBytes
					}
					select {
					case results <- result:
					case <-runCtx.Done():
						return
					}
				case <-runCtx.Done():
					return
				}
			}
		}(spool)
	}

	var scannerWG sync.WaitGroup
	scannerWG.Add(1)
	go func() {
		defer scannerWG.Done()
		defer close(jobs)
		err := forEachChangedLeaf(oldTrie, newTrie, func(accountHash common.Hash, oldBlob []byte) error {
			if err := runCtx.Err(); err != nil {
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
			job := archiveStorageSpoolJob{
				parentRoot:  parentRoot,
				root:        root,
				accountHash: accountHash,
				address:     address,
				origin:      oldOrigin,
				oldRoot:     accountStorageRoot(oldAccount),
				newRoot:     accountStorageRoot(newAccount),
			}
			select {
			case jobs <- job:
				return nil
			case <-runCtx.Done():
				return runCtx.Err()
			}
		})
		if err != nil {
			reportError(err)
		}
	}()

	go func() {
		scannerWG.Wait()
		workerWG.Wait()
		close(results)
	}()

	started := time.Now()
	nextProgress := uint64(archiveSpillLogEvery)
	accounts := make([]archiveSpoolAccount, 0)
	stats := archiveSpillStats{}
	for result := range results {
		stats.accounts += result.stats.accounts
		stats.storageSlots += result.stats.storageSlots
		stats.accountDataBytes += result.stats.accountDataBytes
		stats.storageDataBytes += result.stats.storageDataBytes
		if err := validateArchiveSpillStats(stats); err != nil {
			reportError(err)
			continue
		}
		accounts = append(accounts, result.account)
		if stats.storageSlots >= nextProgress {
			elapsed := time.Since(started)
			log.Info(
				"Spooling archive storage changes",
				"accounts", stats.accounts,
				"storageSlots", stats.storageSlots,
				"workers", workers,
				"slotsPerSec", fmt.Sprintf("%.2f", float64(stats.storageSlots)/elapsed.Seconds()),
				"spoolMiB", (stats.storageSlots*archiveSpoolSlotHeaderSize+stats.storageDataBytes)/(1024*1024),
				"elapsed", elapsed,
			)
			nextProgress = (stats.storageSlots/archiveSpillLogEvery + 1) * archiveSpillLogEvery
		}
	}
	select {
	case err := <-firstErr:
		return nil, archiveSpillStats{}, err
	default:
	}
	if err := ctx.Err(); err != nil {
		return nil, archiveSpillStats{}, err
	}
	if stats.accounts == 0 {
		return nil, archiveSpillStats{}, errors.New("state roots differ but no account changes were found")
	}
	return accounts, stats, nil
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

func encodeArchiveHistoryFromSpill(accounts []archiveSpoolAccount, stats archiveSpillStats) (*encodedArchiveHistory, error) {
	if err := validateArchiveSpillStats(stats); err != nil {
		return nil, err
	}
	if uint64(len(accounts)) != stats.accounts {
		return nil, fmt.Errorf("archive account spool size mismatch: accounts %d/%d", len(accounts), stats.accounts)
	}
	sort.Slice(accounts, func(i, j int) bool {
		return accounts[i].address.Cmp(accounts[j].address) < 0
	})
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

	spoolFiles := make(map[string]*os.File)
	defer func() {
		for _, file := range spoolFiles {
			_ = file.Close()
		}
	}()
	openSpool := func(path string) (*os.File, error) {
		if file := spoolFiles[path]; file != nil {
			return file, nil
		}
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		spoolFiles[path] = file
		return file, nil
	}
	spoolReader := bufio.NewReaderSize(bytes.NewReader(nil), archiveSpoolReadBufferSize)

	var accountDataOffset, storageOffset, storageDataOffset uint64
	for accountNumber, account := range accounts {
		if accountNumber > 0 && accounts[accountNumber-1].address == account.address {
			return nil, fmt.Errorf("duplicate archive account %s", account.address)
		}
		if len(account.origin) > math.MaxUint8 {
			return nil, fmt.Errorf("origin account for %s too large: %d bytes", account.address, len(account.origin))
		}
		slots := uint64(account.storage.slots)
		if storageOffset+slots > math.MaxUint32 {
			return nil, fmt.Errorf("too many storage slots in archive history: %d", storageOffset+slots)
		}
		entryStart := uint64(accountNumber) * accountIndexSize
		entry := accountIndex[entryStart : entryStart+accountIndexSize]
		copy(entry[:common.AddressLength], account.address.Bytes())
		entry[common.AddressLength] = uint8(len(account.origin))
		binary.BigEndian.PutUint32(entry[common.AddressLength+1:common.AddressLength+5], uint32(accountDataOffset))
		binary.BigEndian.PutUint32(entry[common.AddressLength+5:common.AddressLength+9], uint32(storageOffset))
		binary.BigEndian.PutUint32(entry[common.AddressLength+9:common.AddressLength+13], account.storage.slots)
		copy(accountData[accountDataOffset:], account.origin)
		accountDataOffset += uint64(len(account.origin))

		if slots != 0 {
			file, err := openSpool(account.storage.path)
			if err != nil {
				return nil, fmt.Errorf("open archive storage spool %s: %w", account.storage.path, err)
			}
			if account.storage.offset > math.MaxInt64 || account.storage.size > math.MaxInt64 {
				return nil, fmt.Errorf("archive storage spool range is too large for %s", account.address)
			}
			section := io.NewSectionReader(file, int64(account.storage.offset), int64(account.storage.size))
			spoolReader.Reset(section)
			var spoolBytesRead uint64
			for slotNumber := uint64(0); slotNumber < slots; slotNumber++ {
				var header [archiveSpoolSlotHeaderSize]byte
				if _, err := io.ReadFull(spoolReader, header[:]); err != nil {
					return nil, fmt.Errorf("read archive storage spool header for %s: %w", account.address, err)
				}
				spoolBytesRead += uint64(len(header))
				originSize := uint64(header[common.HashLength])
				if storageDataOffset+originSize > uint64(len(storageData)) {
					return nil, fmt.Errorf("archive storage spool data exceeds expected size for %s", account.address)
				}
				storageEntryStart := (storageOffset + slotNumber) * storageIndexSizeForArchiveHistory
				storageEntry := storageIndex[storageEntryStart : storageEntryStart+storageIndexSizeForArchiveHistory]
				copy(storageEntry[:common.HashLength], header[:common.HashLength])
				storageEntry[common.HashLength] = byte(originSize)
				binary.BigEndian.PutUint32(storageEntry[common.HashLength+1:common.HashLength+5], uint32(storageDataOffset))
				if _, err := io.ReadFull(spoolReader, storageData[storageDataOffset:storageDataOffset+originSize]); err != nil {
					return nil, fmt.Errorf("read archive storage spool data for %s: %w", account.address, err)
				}
				spoolBytesRead += originSize
				storageDataOffset += originSize
			}
			if spoolBytesRead != account.storage.size {
				return nil, fmt.Errorf(
					"archive storage spool size mismatch for %s: read %d/%d bytes",
					account.address, spoolBytesRead, account.storage.size,
				)
			}
		}
		storageOffset += slots
	}
	if uint64(len(accounts)) != stats.accounts || accountDataOffset != stats.accountDataBytes || storageOffset != stats.storageSlots {
		return nil, fmt.Errorf(
			"archive account spool size mismatch: accounts %d/%d accountData %d/%d storageSlots %d/%d",
			len(accounts), stats.accounts, accountDataOffset, stats.accountDataBytes, storageOffset, stats.storageSlots,
		)
	}
	if storageDataOffset != stats.storageDataBytes {
		return nil, fmt.Errorf(
			"archive storage spool data size mismatch: have %d want %d",
			storageDataOffset, stats.storageDataBytes,
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

func prepareArchiveSpillDirectory(config ArchiveHistoryConfig, dstChainData string) error {
	parent, prefix := archiveSpillDirectory(config, dstChainData)
	if err := os.MkdirAll(parent, 0755); err != nil {
		return fmt.Errorf("create archive spill parent: %w", err)
	}
	if err := removeStaleArchiveSpills(parent, prefix); err != nil {
		return fmt.Errorf("remove stale archive spill: %w", err)
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
	directory, err := os.MkdirTemp(parent, prefix)
	if err != nil {
		return nil, fmt.Errorf("create archive spill directory: %w", err)
	}
	removeSpool := true
	defer func() {
		if removeSpool {
			_ = os.RemoveAll(directory)
		}
	}()
	started := time.Now()
	log.Warn(
		"Using disk-backed archive trie diff",
		"directory", directory,
		"parentRoot", parentRoot,
		"root", root,
		"workers", config.SpillWorkers,
		"bufferMB", config.SpillCache,
	)

	accounts, stats, collectErr := collectArchiveHistoryOriginsToSpill(
		ctx,
		src,
		trieDB,
		parentRoot,
		root,
		directory,
		config.SpillWorkers,
		config.SpillCache,
	)
	spoolElapsed := time.Since(started)
	if collectErr == nil {
		log.Info(
			"Archive trie diff spooled to disk",
			"accounts", stats.accounts,
			"storageSlots", stats.storageSlots,
			"accountDataBytes", stats.accountDataBytes,
			"storageDataBytes", stats.storageDataBytes,
			"spoolBytes", stats.storageSlots*archiveSpoolSlotHeaderSize+stats.storageDataBytes,
			"workers", config.SpillWorkers,
			"elapsed", spoolElapsed,
		)
	}
	var encoded *encodedArchiveHistory
	encodeStarted := time.Now()
	if collectErr == nil {
		encoded, collectErr = encodeArchiveHistoryFromSpill(accounts, stats)
	}
	removeErr := os.RemoveAll(directory)
	if collectErr != nil {
		return nil, collectErr
	}
	if removeErr != nil {
		return nil, fmt.Errorf("remove archive spool directory: %w", removeErr)
	}
	removeSpool = false
	log.Info(
		"Disk-backed archive trie diff encoded",
		"accounts", encoded.accounts,
		"storageSlots", encoded.storageSlots,
		"accountIndexBytes", len(encoded.accountIndex),
		"storageIndexBytes", len(encoded.storageIndex),
		"accountDataBytes", len(encoded.accountData),
		"storageDataBytes", len(encoded.storageData),
		"spoolElapsed", spoolElapsed,
		"encodeElapsed", time.Since(encodeStarted),
		"elapsed", time.Since(started),
	)
	return encoded, nil
}
