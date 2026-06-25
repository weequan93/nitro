// Copyright 2023-2026, Offchain Labs, Inc.
// For license information, see https://github.com/offchainlabs/nitro/blob/master/LICENSE.md
package pathdbmigrate

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/triedb"
)

const (
	stateHistoryV1ForAccountHistory = uint8(1)
	historyMetaSize                 = 1 + 2*common.HashLength + 8
	accountIndexSize                = common.AddressLength + 13
)

type accountHistoryStats struct {
	blocks      uint64
	transitions uint64
}

func (m *Migrator) runAccountHistory(ctx context.Context) error {
	cfg := m.config.AccountHistory
	addresses, err := parseAccountHistoryAddresses(cfg.Addresses)
	if err != nil {
		return err
	}

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
		return fmt.Errorf("account-history.start-block %d is greater than end-block %d", start, end)
	}
	_, endRoot, err := canonicalHeaderAndRoot(src, end)
	if err != nil {
		return err
	}
	if dstRoot := pathAccountRoot(dst); dstRoot != endRoot {
		return fmt.Errorf(
			"destination pathdb root %s does not match account-history.end-block %d root %s; run this mode before syncing past end-block or choose the current destination root block",
			dstRoot,
			end,
			endRoot,
		)
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
			return fmt.Errorf("destination already has %d state history entries; rerun with --account-history.reset-history only on a disposable/copy DB", frozen)
		}
		log.Warn("Resetting existing destination state history", "entries", frozen)
		if err := freezer.Reset(); err != nil {
			return fmt.Errorf("reset destination state history: %w", err)
		}
	}
	resetStateHistoryIndexes(dst)

	srcTrieDB := triedb.NewDatabase(src, triedb.HashDefaults)
	defer srcTrieDB.Close()

	prevHeader, prevRoot, err := canonicalHeaderAndRoot(src, start)
	if err != nil {
		return err
	}
	stateID := uint64(0)
	rawdb.WriteStateID(dst, prevRoot, stateID)

	started := time.Now()
	stats := accountHistoryStats{}
	log.Info(
		"Writing targeted account history",
		"start", start,
		"end", end,
		"addresses", len(addresses),
		"root", prevRoot,
	)

	for block := start + 1; block <= end; block++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		header, root, err := canonicalHeaderAndRoot(src, block)
		if err != nil {
			return err
		}
		stats.blocks++

		if root == prevRoot {
			rawdb.WriteStateID(dst, root, stateID)
			prevHeader = header
			continue
		}
		stateID++
		origins, err := accountOrigins(srcTrieDB, prevRoot, addresses)
		if err != nil {
			return fmt.Errorf("block %d parent %d account origins: %w", block, prevHeader.Number.Uint64(), err)
		}
		meta := encodeAccountHistoryMeta(prevRoot, root, block)
		accountIndex, accountData, err := encodeAccountHistoryAccounts(origins, addresses)
		if err != nil {
			return fmt.Errorf("block %d encode account history: %w", block, err)
		}
		if err := rawdb.WriteStateHistory(freezer, stateID, meta, accountIndex, nil, accountData, nil); err != nil {
			return fmt.Errorf("write account history id %d block %d: %w", stateID, block, err)
		}
		rawdb.WriteStateID(dst, root, stateID)
		stats.transitions++

		if stats.transitions%10000 == 0 {
			log.Info(
				"Targeted account history progress",
				"block", block,
				"stateID", stateID,
				"transitions", stats.transitions,
				"elapsed", time.Since(started),
			)
		}
		prevHeader = header
		prevRoot = root
	}

	rawdb.WritePersistentStateID(dst, stateID)
	resetStateHistoryIndexes(dst)
	if err := dst.SyncKeyValue(); err != nil {
		return fmt.Errorf("sync destination db: %w", err)
	}
	log.Info(
		"Targeted account history finished",
		"blocks", stats.blocks,
		"transitions", stats.transitions,
		"stateID", stateID,
		"elapsed", time.Since(started),
	)
	return nil
}

func pathAccountRoot(db ethdb.KeyValueReader) common.Hash {
	blob := rawdb.ReadAccountTrieNode(db, nil)
	if len(blob) == 0 {
		return types.EmptyRootHash
	}
	return crypto.Keccak256Hash(blob)
}

func parseAccountHistoryAddresses(values []string) ([]common.Address, error) {
	seen := make(map[common.Address]struct{})
	var addresses []common.Address
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if !common.IsHexAddress(part) {
				return nil, fmt.Errorf("invalid address %q", part)
			}
			address := common.HexToAddress(part)
			if _, ok := seen[address]; ok {
				continue
			}
			seen[address] = struct{}{}
			addresses = append(addresses, address)
		}
	}
	if len(addresses) == 0 {
		return nil, errors.New("no account-history addresses provided")
	}
	return addresses, nil
}

func parseHistoryBlock(db ethdb.Database, spec string) (uint64, error) {
	if strings.EqualFold(spec, "latest") || spec == "" {
		header := rawdb.ReadHeadHeader(db)
		if header == nil {
			return 0, errors.New("missing head header")
		}
		return header.Number.Uint64(), nil
	}
	number, err := strconv.ParseUint(spec, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid block %q: use latest or a block number", spec)
	}
	if hash := rawdb.ReadCanonicalHash(db, number); hash == (common.Hash{}) {
		return 0, fmt.Errorf("missing canonical hash for block %d", number)
	}
	return number, nil
}

func canonicalHeaderAndRoot(db ethdb.Database, number uint64) (*types.Header, common.Hash, error) {
	hash := rawdb.ReadCanonicalHash(db, number)
	if hash == (common.Hash{}) {
		return nil, common.Hash{}, fmt.Errorf("missing canonical hash for block %d", number)
	}
	header := rawdb.ReadHeader(db, hash, number)
	if header == nil {
		return nil, common.Hash{}, fmt.Errorf("missing header for canonical block %d hash %s", number, hash)
	}
	return header, header.Root, nil
}

func accountOrigins(srcTrieDB *triedb.Database, root common.Hash, addresses []common.Address) (map[common.Address][]byte, error) {
	accountTrie, err := trie.NewStateTrie(trie.StateTrieID(root), srcTrieDB)
	if err != nil {
		return nil, fmt.Errorf("open account trie %s: %w", root, err)
	}
	origins := make(map[common.Address][]byte, len(addresses))
	for _, address := range addresses {
		account, err := accountTrie.GetAccount(address)
		if err != nil {
			return nil, fmt.Errorf("get account %s: %w", address, err)
		}
		if account == nil {
			origins[address] = nil
			continue
		}
		origins[address] = types.SlimAccountRLP(*account)
	}
	return origins, nil
}

func encodeAccountHistoryMeta(parent common.Hash, root common.Hash, block uint64) []byte {
	meta := make([]byte, historyMetaSize)
	meta[0] = stateHistoryV1ForAccountHistory
	copy(meta[1:1+common.HashLength], parent.Bytes())
	copy(meta[1+common.HashLength:1+2*common.HashLength], root.Bytes())
	binary.BigEndian.PutUint64(meta[1+2*common.HashLength:], block)
	return meta
}

func encodeAccountHistoryAccounts(origins map[common.Address][]byte, addresses []common.Address) ([]byte, []byte, error) {
	sorted := append([]common.Address(nil), addresses...)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j].Cmp(sorted[j-1]) < 0; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}

	var accountIndex []byte
	var accountData []byte
	for _, address := range sorted {
		blob, ok := origins[address]
		if !ok {
			return nil, nil, fmt.Errorf("missing origin for %s", address)
		}
		if len(blob) > 255 {
			return nil, nil, fmt.Errorf("origin account for %s too large: %d bytes", address, len(blob))
		}
		var index [accountIndexSize]byte
		copy(index[:common.AddressLength], address.Bytes())
		index[common.AddressLength] = uint8(len(blob))
		binary.BigEndian.PutUint32(index[common.AddressLength+1:common.AddressLength+5], uint32(len(accountData)))
		accountIndex = append(accountIndex, index[:]...)
		accountData = append(accountData, blob...)
	}
	return accountIndex, accountData, nil
}

func resetStateHistoryIndexes(db ethdb.Database) {
	rawdb.DeleteStateHistoryIndexMetadata(db)
	if deleter, ok := db.(ethdb.KeyValueRangeDeleter); ok {
		rawdb.DeleteStateHistoryIndexes(deleter)
	}
}
