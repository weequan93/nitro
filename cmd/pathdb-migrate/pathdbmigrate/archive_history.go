// Copyright 2023-2026, Offchain Labs, Inc.
// For license information, see https://github.com/offchainlabs/nitro/blob/master/LICENSE.md
package pathdbmigrate

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/triedb"
)

const storageIndexSizeForArchiveHistory = common.HashLength + 5

var errArchiveStateUnavailable = errors.New("archive source state trie unavailable")

type archiveHistoryStats struct {
	blocks           uint64
	availableBlocks  uint64
	skippedBlocks    uint64
	transitions      uint64
	accounts         uint64
	storageSlots     uint64
	missingPreimages uint64
}

func (m *Migrator) runArchiveHistory(ctx context.Context) error {
	cfg := m.config.ArchiveHistory

	src, err := openChainDB(&m.config.Src, "src", true, false)
	if err != nil {
		return err
	}
	defer src.Close()
	if err := ensureHashSource(src); err != nil {
		return err
	}

	dst, err := openChainDB(&m.config.Dst, "dst", false, false)
	if err != nil {
		return err
	}
	defer dst.Close()
	if scheme := rawdb.ReadStateScheme(dst); scheme != rawdb.PathScheme {
		return fmt.Errorf("destination state scheme must be path, got %q", scheme)
	}

	start, err := parseHistoryBlock(src, cfg.StartBlock)
	if err != nil {
		return fmt.Errorf("start block: %w", err)
	}
	end, err := parseHistoryBlock(src, cfg.EndBlock)
	if err != nil {
		return fmt.Errorf("end block: %w", err)
	}
	if start > end {
		return fmt.Errorf("archive-history.start-block %d is greater than end-block %d", start, end)
	}
	_, endRoot, err := canonicalHeaderAndRoot(src, end)
	if err != nil {
		return err
	}
	if dstRoot := pathAccountRoot(dst); dstRoot != endRoot {
		return fmt.Errorf(
			"destination pathdb root %s does not match archive-history.end-block %d root %s; run this mode before syncing past end-block or choose the current destination root block",
			dstRoot,
			end,
			endRoot,
		)
	}
	_, initialRoot, err := canonicalHeaderAndRoot(src, start)
	if err != nil {
		return err
	}
	if !archiveTrieRootAvailable(src, endRoot) {
		return fmt.Errorf("archive-history target block %d root %s is unavailable in source hashdb and cannot be skipped", end, endRoot)
	}
	if !cfg.SkipMissingStates && !archiveTrieRootAvailable(src, initialRoot) {
		return fmt.Errorf("archive-history start block %d root %s is unavailable in source hashdb", start, initialRoot)
	}

	ancientDir, err := dst.AncientDatadir()
	if err != nil {
		return fmt.Errorf("destination ancient dir: %w", err)
	}
	freezer, err := rawdb.NewStateFreezer(ancientDir, false, false)
	if err != nil {
		return fmt.Errorf("open destination state freezer: %w", err)
	}
	defer freezer.Close()
	frozen, err := freezer.Ancients()
	if err != nil {
		return fmt.Errorf("read destination state history size: %w", err)
	}
	if frozen != 0 {
		if !cfg.ResetHistory {
			return fmt.Errorf("destination already has %d state history entries; rerun with --archive-history.reset-history only on a disposable/copy DB", frozen)
		}
		log.Warn("Resetting existing destination state history", "entries", frozen)
		if err := freezer.Reset(); err != nil {
			return fmt.Errorf("reset destination state history: %w", err)
		}
	}
	resetStateHistoryIndexes(dst)

	srcTrieDB := triedb.NewDatabase(src, triedb.HashDefaults)
	defer srcTrieDB.Close()

	started := time.Now()
	stats := archiveHistoryStats{}
	totalBlocks := end - start
	stateID := uint64(0)
	prevRoot := common.Hash{}
	anchorBlock := start
	haveAnchor := archiveTrieRootAvailable(src, initialRoot)
	if haveAnchor {
		prevRoot = initialRoot
		stats.availableBlocks++
		rawdb.WriteStateID(dst, prevRoot, stateID)
	} else {
		stats.skippedBlocks++
		log.Warn("Skipping unavailable initial archive state", "block", start, "root", initialRoot)
	}
	log.Info(
		"Writing full archive state history",
		"start", start,
		"end", end,
		"target", end,
		"targetRoot", endRoot,
		"initialRoot", initialRoot,
		"skipMissingStates", cfg.SkipMissingStates,
		"progressEvery", cfg.ProgressEvery,
		"storageHistoryVersion", 0,
	)

	for block := start + 1; block <= end; block++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		_, root, err := canonicalHeaderAndRoot(src, block)
		if err != nil {
			return err
		}
		stats.blocks++
		if !haveAnchor {
			if !archiveTrieRootAvailable(src, root) {
				if block == end {
					return fmt.Errorf("archive-history target block %d root %s is unavailable in source hashdb and cannot be skipped", block, root)
				}
				recordSkippedArchiveState(block, root, anchorBlock, prevRoot, "state root is unavailable", cfg.ProgressEvery, &stats)
				continue
			}
			haveAnchor = true
			prevRoot = root
			anchorBlock = block
			stats.availableBlocks++
			rawdb.WriteStateID(dst, prevRoot, stateID)
			log.Info("Selected first available archive state", "block", block, "root", root, "skippedBlocks", stats.skippedBlocks)
			continue
		}

		if root == prevRoot {
			stats.availableBlocks++
			anchorBlock = block
			rawdb.WriteStateID(dst, root, stateID)
			if shouldLogArchiveProgress(stats.blocks, cfg.ProgressEvery, block, end) {
				logArchiveProgress("Full archive history progress", block, end, root, stateID, totalBlocks, started, stats)
			}
			continue
		}
		if !archiveTrieRootAvailable(src, root) {
			if !cfg.SkipMissingStates || block == end {
				return fmt.Errorf("block %d archive state root %s is unavailable in source hashdb", block, root)
			}
			recordSkippedArchiveState(block, root, anchorBlock, prevRoot, "state root is unavailable", cfg.ProgressEvery, &stats)
			continue
		}

		accounts, storages, changedAccounts, changedSlots, err := archiveHistoryOrigins(src, srcTrieDB, prevRoot, root)
		if err != nil {
			if cfg.SkipMissingStates && block != end && isMissingArchiveState(err) {
				recordSkippedArchiveState(block, root, anchorBlock, prevRoot, err.Error(), cfg.ProgressEvery, &stats)
				continue
			}
			return fmt.Errorf("block %d archive history origins: %w", block, err)
		}
		if len(accounts) == 0 {
			return fmt.Errorf("block %d root changed from %s to %s but no account changes were found", block, prevRoot, root)
		}
		stateID++
		meta := encodeArchiveHistoryMeta(prevRoot, root, block)
		accountIndex, storageIndex, accountData, storageData, err := encodeArchiveHistory(accounts, storages)
		if err != nil {
			return fmt.Errorf("block %d encode archive history: %w", block, err)
		}
		if err := rawdb.WriteStateHistory(freezer, stateID, meta, accountIndex, storageIndex, accountData, storageData); err != nil {
			return fmt.Errorf("write archive history id %d block %d: %w", stateID, block, err)
		}
		rawdb.WriteStateID(dst, root, stateID)

		stats.transitions++
		stats.availableBlocks++
		stats.accounts += changedAccounts
		stats.storageSlots += changedSlots
		if shouldLogArchiveProgress(stats.blocks, cfg.ProgressEvery, block, end) {
			logArchiveProgress("Full archive history progress", block, end, root, stateID, totalBlocks, started, stats)
		}
		prevRoot = root
		anchorBlock = block
	}

	rawdb.WritePersistentStateID(dst, stateID)
	resetStateHistoryIndexes(dst)
	if err := dst.SyncKeyValue(); err != nil {
		return fmt.Errorf("sync destination db: %w", err)
	}
	if stats.skippedBlocks != 0 {
		log.Warn(
			"Archive state history is incomplete because source states were unavailable",
			"availableBlocks", stats.availableBlocks,
			"skippedBlocks", stats.skippedBlocks,
			"coverage", archiveCoverage(stats),
		)
	}
	log.Info(
		"Full archive state history finished",
		"start", start,
		"end", end,
		"target", end,
		"targetRoot", endRoot,
		"blocks", stats.blocks,
		"availableBlocks", stats.availableBlocks,
		"skippedBlocks", stats.skippedBlocks,
		"coverage", archiveCoverage(stats),
		"partial", stats.skippedBlocks != 0,
		"transitions", stats.transitions,
		"stateID", stateID,
		"accounts", stats.accounts,
		"storageSlots", stats.storageSlots,
		"missingPreimages", stats.missingPreimages,
		"elapsed", time.Since(started),
	)
	return nil
}

func archiveHistoryOrigins(src ethdb.KeyValueReader, trieDB *triedb.Database, parentRoot common.Hash, root common.Hash) (map[common.Address][]byte, map[common.Address]map[common.Hash][]byte, uint64, uint64, error) {
	if !archiveTrieRootAvailable(src, parentRoot) {
		return nil, nil, 0, 0, fmt.Errorf("%w: parent account root %s", errArchiveStateUnavailable, parentRoot)
	}
	if !archiveTrieRootAvailable(src, root) {
		return nil, nil, 0, 0, fmt.Errorf("%w: account root %s", errArchiveStateUnavailable, root)
	}
	oldTrie, err := trie.New(trie.TrieID(parentRoot), trieDB)
	if err != nil {
		return nil, nil, 0, 0, fmt.Errorf("open parent account trie %s: %w", parentRoot, err)
	}
	newTrie, err := trie.New(trie.TrieID(root), trieDB)
	if err != nil {
		return nil, nil, 0, 0, fmt.Errorf("open account trie %s: %w", root, err)
	}
	accountHashes, oldAccountBlobs, err := changedLeafOrigins(oldTrie, newTrie)
	if err != nil {
		return nil, nil, 0, 0, fmt.Errorf("diff account tries: %w", err)
	}

	accounts := make(map[common.Address][]byte, len(accountHashes))
	storages := make(map[common.Address]map[common.Hash][]byte)
	var storageSlots uint64
	for _, accountHash := range sortedHashes(accountHashes) {
		preimage := rawdb.ReadPreimage(src, accountHash)
		if len(preimage) != common.AddressLength {
			return nil, nil, 0, 0, fmt.Errorf("missing account preimage for hash %s; full archive history needs account preimages", accountHash)
		}
		address := common.BytesToAddress(preimage)

		oldAccount, oldOrigin, err := decodeArchiveAccountOrigin(accountHash, oldAccountBlobs[accountHash])
		if err != nil {
			return nil, nil, 0, 0, err
		}
		newAccount, err := readArchiveAccount(newTrie, accountHash)
		if err != nil {
			return nil, nil, 0, 0, err
		}
		accounts[address] = oldOrigin

		oldStorageRoot := accountStorageRoot(oldAccount)
		newStorageRoot := accountStorageRoot(newAccount)
		if oldStorageRoot == newStorageRoot {
			continue
		}
		slotOrigins, err := archiveStorageOrigins(src, trieDB, parentRoot, root, accountHash, oldStorageRoot, newStorageRoot)
		if err != nil {
			return nil, nil, 0, 0, fmt.Errorf("account %s storage history: %w", address, err)
		}
		if len(slotOrigins) != 0 {
			storages[address] = slotOrigins
			storageSlots += uint64(len(slotOrigins))
		}
	}
	return accounts, storages, uint64(len(accounts)), storageSlots, nil
}

func archiveStorageOrigins(src ethdb.KeyValueReader, trieDB *triedb.Database, parentRoot common.Hash, root common.Hash, accountHash common.Hash, oldRoot common.Hash, newRoot common.Hash) (map[common.Hash][]byte, error) {
	if !archiveTrieRootAvailable(src, oldRoot) {
		return nil, fmt.Errorf("%w: parent storage root %s", errArchiveStateUnavailable, oldRoot)
	}
	if !archiveTrieRootAvailable(src, newRoot) {
		return nil, fmt.Errorf("%w: storage root %s", errArchiveStateUnavailable, newRoot)
	}
	oldTrie, err := trie.New(trie.StorageTrieID(parentRoot, accountHash, oldRoot), trieDB)
	if err != nil {
		return nil, fmt.Errorf("open parent storage trie %s: %w", oldRoot, err)
	}
	newTrie, err := trie.New(trie.StorageTrieID(root, accountHash, newRoot), trieDB)
	if err != nil {
		return nil, fmt.Errorf("open storage trie %s: %w", newRoot, err)
	}
	slotHashes, oldSlotBlobs, err := changedLeafOrigins(oldTrie, newTrie)
	if err != nil {
		return nil, fmt.Errorf("diff storage tries: %w", err)
	}
	origins := make(map[common.Hash][]byte, len(slotHashes))
	for slotHash := range slotHashes {
		origins[slotHash] = oldSlotBlobs[slotHash]
	}
	return origins, nil
}

func changedLeafOrigins(oldTrie *trie.Trie, newTrie *trie.Trie) (map[common.Hash]struct{}, map[common.Hash][]byte, error) {
	changed := make(map[common.Hash]struct{})
	oldOrigins := make(map[common.Hash][]byte)

	oldToNew, err := changedLeaves(oldTrie, newTrie)
	if err != nil {
		return nil, nil, err
	}
	for key := range oldToNew {
		changed[key] = struct{}{}
	}

	newToOld, err := changedLeaves(newTrie, oldTrie)
	if err != nil {
		return nil, nil, err
	}
	for key, oldBlob := range newToOld {
		changed[key] = struct{}{}
		oldOrigins[key] = oldBlob
	}
	return changed, oldOrigins, nil
}

func changedLeaves(base *trie.Trie, target *trie.Trie) (map[common.Hash][]byte, error) {
	baseIt, err := base.NodeIterator(nil)
	if err != nil {
		return nil, err
	}
	targetIt, err := target.NodeIterator(nil)
	if err != nil {
		return nil, err
	}
	diff, _ := trie.NewDifferenceIterator(baseIt, targetIt)
	leaves := make(map[common.Hash][]byte)
	for diff.Next(true) {
		if diff.Leaf() {
			leaves[common.BytesToHash(diff.LeafKey())] = common.CopyBytes(diff.LeafBlob())
		}
	}
	if err := diff.Error(); err != nil {
		return nil, err
	}
	return leaves, nil
}

func decodeArchiveAccountOrigin(accountHash common.Hash, blob []byte) (*types.StateAccount, []byte, error) {
	if len(blob) == 0 {
		return nil, nil, nil
	}
	var account types.StateAccount
	if err := rlp.DecodeBytes(blob, &account); err != nil {
		return nil, nil, fmt.Errorf("decode parent account %s: %w", accountHash, err)
	}
	return &account, types.SlimAccountRLP(account), nil
}

func readArchiveAccount(accountTrie *trie.Trie, accountHash common.Hash) (*types.StateAccount, error) {
	blob, err := accountTrie.Get(accountHash.Bytes())
	if err != nil {
		return nil, fmt.Errorf("get account %s: %w", accountHash, err)
	}
	if len(blob) == 0 {
		return nil, nil
	}
	var account types.StateAccount
	if err := rlp.DecodeBytes(blob, &account); err != nil {
		return nil, fmt.Errorf("decode account %s: %w", accountHash, err)
	}
	return &account, nil
}

func accountStorageRoot(account *types.StateAccount) common.Hash {
	if account == nil {
		return types.EmptyRootHash
	}
	return account.Root
}

func encodeArchiveHistoryMeta(parent common.Hash, root common.Hash, block uint64) []byte {
	meta := make([]byte, historyMetaSize)
	meta[0] = 0 // state history v0: storage slot identifiers are hashed keys.
	copy(meta[1:1+common.HashLength], parent.Bytes())
	copy(meta[1+common.HashLength:1+2*common.HashLength], root.Bytes())
	binary.BigEndian.PutUint64(meta[1+2*common.HashLength:], block)
	return meta
}

func encodeArchiveHistory(accounts map[common.Address][]byte, storages map[common.Address]map[common.Hash][]byte) ([]byte, []byte, []byte, []byte, error) {
	addresses := make([]common.Address, 0, len(accounts))
	for address := range accounts {
		addresses = append(addresses, address)
	}
	sort.Slice(addresses, func(i, j int) bool {
		return addresses[i].Cmp(addresses[j]) < 0
	})

	var (
		accountIndex  []byte
		storageIndex  []byte
		accountData   []byte
		storageData   []byte
		storageOffset uint32
	)
	for _, address := range addresses {
		accountBlob := accounts[address]
		if len(accountBlob) > 255 {
			return nil, nil, nil, nil, fmt.Errorf("origin account for %s too large: %d bytes", address, len(accountBlob))
		}
		if uint64(len(accountData)) > math.MaxUint32 {
			return nil, nil, nil, nil, fmt.Errorf("account data too large: %d bytes", len(accountData))
		}
		slots := storages[address]
		slotKeys := make([]common.Hash, 0, len(slots))
		for slot := range slots {
			slotKeys = append(slotKeys, slot)
		}
		sort.Slice(slotKeys, func(i, j int) bool {
			return slotKeys[i].Cmp(slotKeys[j]) < 0
		})
		if uint64(len(slotKeys)) > math.MaxUint32 {
			return nil, nil, nil, nil, fmt.Errorf("too many storage slots for %s: %d", address, len(slotKeys))
		}

		var accountEntry [accountIndexSize]byte
		copy(accountEntry[:common.AddressLength], address.Bytes())
		accountEntry[common.AddressLength] = uint8(len(accountBlob))
		binary.BigEndian.PutUint32(accountEntry[common.AddressLength+1:common.AddressLength+5], uint32(len(accountData)))
		binary.BigEndian.PutUint32(accountEntry[common.AddressLength+5:common.AddressLength+9], storageOffset)
		binary.BigEndian.PutUint32(accountEntry[common.AddressLength+9:common.AddressLength+13], uint32(len(slotKeys)))
		accountIndex = append(accountIndex, accountEntry[:]...)
		accountData = append(accountData, accountBlob...)

		for _, slot := range slotKeys {
			slotBlob := slots[slot]
			if len(slotBlob) > 255 {
				return nil, nil, nil, nil, fmt.Errorf("origin storage slot %s for %s too large: %d bytes", slot, address, len(slotBlob))
			}
			if uint64(len(storageData)) > math.MaxUint32 {
				return nil, nil, nil, nil, fmt.Errorf("storage data too large: %d bytes", len(storageData))
			}
			var storageEntry [storageIndexSizeForArchiveHistory]byte
			copy(storageEntry[:common.HashLength], slot.Bytes())
			storageEntry[common.HashLength] = uint8(len(slotBlob))
			binary.BigEndian.PutUint32(storageEntry[common.HashLength+1:common.HashLength+5], uint32(len(storageData)))
			storageIndex = append(storageIndex, storageEntry[:]...)
			storageData = append(storageData, slotBlob...)
		}
		storageOffset += uint32(len(slotKeys))
	}
	return accountIndex, storageIndex, accountData, storageData, nil
}

func sortedHashes(values map[common.Hash]struct{}) []common.Hash {
	hashes := make([]common.Hash, 0, len(values))
	for hash := range values {
		hashes = append(hashes, hash)
	}
	sort.Slice(hashes, func(i, j int) bool {
		return hashes[i].Cmp(hashes[j]) < 0
	})
	return hashes
}

func shouldLogArchiveProgress(processed uint64, progressEvery uint64, block uint64, end uint64) bool {
	if block == end {
		return true
	}
	return processed%progressEvery == 0
}

func logArchiveProgress(msg string, block uint64, end uint64, root common.Hash, stateID uint64, totalBlocks uint64, started time.Time, stats archiveHistoryStats) {
	elapsed := time.Since(started)
	rate := float64(0)
	if elapsed > 0 {
		rate = float64(stats.blocks) / elapsed.Seconds()
	}
	remaining := time.Duration(0)
	if rate > 0 && end >= block {
		remaining = time.Duration(float64(end-block)/rate) * time.Second
	}
	percent := float64(100)
	if totalBlocks != 0 {
		percent = float64(stats.blocks) * 100 / float64(totalBlocks)
	}
	log.Info(
		msg,
		"block", block,
		"target", end,
		"percent", fmt.Sprintf("%.2f", percent),
		"root", root,
		"stateID", stateID,
		"availableBlocks", stats.availableBlocks,
		"skippedBlocks", stats.skippedBlocks,
		"coverage", archiveCoverage(stats),
		"transitions", stats.transitions,
		"accounts", stats.accounts,
		"storageSlots", stats.storageSlots,
		"missingPreimages", stats.missingPreimages,
		"blocksPerSec", fmt.Sprintf("%.2f", rate),
		"elapsed", elapsed,
		"eta", remaining,
	)
}

func archiveTrieRootAvailable(db ethdb.KeyValueReader, root common.Hash) bool {
	return root == types.EmptyRootHash || rawdb.HasLegacyTrieNode(db, root)
}

func isMissingArchiveState(err error) bool {
	if errors.Is(err, errArchiveStateUnavailable) {
		return true
	}
	var missing *trie.MissingNodeError
	return errors.As(err, &missing)
}

func recordSkippedArchiveState(block uint64, root common.Hash, anchorBlock uint64, anchorRoot common.Hash, reason string, progressEvery uint64, stats *archiveHistoryStats) {
	stats.skippedBlocks++
	if stats.skippedBlocks <= 10 || stats.blocks%progressEvery == 0 {
		log.Warn(
			"Skipping unavailable archive state",
			"block", block,
			"root", root,
			"anchorBlock", anchorBlock,
			"anchorRoot", anchorRoot,
			"skippedBlocks", stats.skippedBlocks,
			"reason", reason,
		)
	}
}

func archiveCoverage(stats archiveHistoryStats) string {
	total := stats.availableBlocks + stats.skippedBlocks
	if total == 0 {
		return "100.00%"
	}
	return fmt.Sprintf("%.2f%%", float64(stats.availableBlocks)*100/float64(total))
}
