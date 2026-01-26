//go:build erigon
// +build erigon

package main

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/ethereum/go-ethereum/triedb/hashdb"
	"github.com/ethereum/go-ethereum/triedb/pathdb"

	ecommon "github.com/erigontech/erigon-lib/common"
	estate "github.com/erigontech/erigon/core/state"
	"github.com/erigontech/erigon/db/kv"
	"github.com/erigontech/erigon/db/kv/rawdbv3"
	"github.com/erigontech/erigon/db/kv/order"

	"github.com/offchainlabs/nitro/arbos/util"
)

type slotRef struct {
	Name     string
	Subspace []byte
	Offset   uint64
}

var arbosStorageAccount = common.HexToAddress("0xA4B05FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF")
var arbosL2GasBacklogSlot = slotForOffset(storageKeyForSubspace([]byte{1}), 4)

func storageKeyForSubspace(id []byte) []byte {
	if len(id) == 0 {
		return nil
	}
	return crypto.Keccak256(id)
}

func mapAddress(storageKey []byte, key common.Hash) common.Hash {
	keyBytes := key.Bytes()
	boundary := common.HashLength - 1
	prefix := crypto.Keccak256(storageKey, keyBytes[:boundary])
	mapped := append(prefix[:boundary], keyBytes[boundary])
	return common.BytesToHash(mapped)
}

func slotForOffset(storageKey []byte, offset uint64) common.Hash {
	return mapAddress(storageKey, util.UintToHash(offset))
}

type slotReader interface {
	StorageAt(blockNum uint64, addr common.Address, slot common.Hash) (common.Hash, error)
}

type gethSlotReader struct {
	db ethdb.Database
}

func (r *gethSlotReader) StorageAt(blockNum uint64, addr common.Address, slot common.Hash) (common.Hash, error) {
	hash := rawdb.ReadCanonicalHash(r.db, blockNum)
	header := rawdb.ReadHeader(r.db, hash, blockNum)
	if header == nil {
		return common.Hash{}, fmt.Errorf("missing header %d", blockNum)
	}

	scheme, err := rawdb.ParseStateScheme("", r.db)
	if err != nil {
		return common.Hash{}, err
	}

	trieCfg := &triedb.Config{Preimages: false}
	if scheme == rawdb.HashScheme {
		trieCfg.HashDB = hashdb.Defaults
	} else {
		trieCfg.PathDB = pathdb.Defaults
	}

	stateDb := state.NewDatabaseWithConfig(r.db, trieCfg)
	defer stateDb.TrieDB().Close()
	statedb, err := state.New(header.Root, stateDb, nil)
	if err != nil {
		return common.Hash{}, err
	}
	return statedb.GetState(addr, slot), nil
}

type erigonSlotReader struct {
	tx          kv.TemporalTx
	txNumReader rawdbv3.TxNumsReader
}

func (r *erigonSlotReader) StorageAt(blockNum uint64, addr common.Address, slot common.Hash) (common.Hash, error) {
	nextMinTxNum, err := r.txNumReader.Min(r.tx, blockNum+1)
	if err != nil {
		return common.Hash{}, err
	}
	txNum := nextMinTxNum
	reader := estate.NewHistoryReaderV3()
	reader.SetTx(r.tx)
	eaddr := ecommon.BytesToAddress(addr.Bytes())
	eslot := ecommon.BytesToHash(slot.Bytes())
	readAt := func(txNum uint64) (common.Hash, bool, error) {
		reader.SetTxNum(txNum)
		val, ok, err := reader.ReadAccountStorage(eaddr, eslot)
		if err != nil {
			return common.Hash{}, false, err
		}
		if !ok {
			return common.Hash{}, false, nil
		}
		return common.BytesToHash(val.Bytes()), true, nil
	}

	val, ok, err := readAt(txNum)
	if err != nil {
		return common.Hash{}, err
	}
	if arbosSlotsDebug && addr == arbosStorageAccount && slot == arbosL2GasBacklogSlot {
		blockMin, minErr := r.txNumReader.Min(r.tx, blockNum)
		if minErr != nil {
			return common.Hash{}, minErr
		}
		blockMax, maxErr := r.txNumReader.Max(r.tx, blockNum)
		if maxErr != nil {
			return common.Hash{}, maxErr
		}
		composite := make([]byte, 0, 20+32)
		composite = append(composite, eaddr.Bytes()...)
		composite = append(composite, eslot.Bytes()...)
		histVal, histOk, histErr := r.tx.HistorySeek(kv.StorageDomain, composite, txNum)
		if histErr != nil {
			return common.Hash{}, histErr
		}
		var histHash common.Hash
		if histOk && len(histVal) > 0 {
			histHash = common.BytesToHash(histVal)
		}
		latestVal, _, latestErr := r.tx.GetLatest(kv.StorageDomain, composite)
		if latestErr != nil {
			return common.Hash{}, latestErr
		}
		var latestHash common.Hash
		if len(latestVal) > 0 {
			latestHash = common.BytesToHash(latestVal)
		}
		histIt, histErr := r.tx.IndexRange(kv.StorageHistoryIdx, composite, -1, -1, order.Asc, 10)
		if histErr != nil {
			return common.Hash{}, histErr
		}
		var histTxs []uint64
		for histIt.HasNext() {
			htx, err := histIt.Next()
			if err != nil {
				histIt.Close()
				return common.Hash{}, err
			}
			histTxs = append(histTxs, htx)
		}
		histIt.Close()
		logKV(
			"verify",
			"section", "arbos_slots_dest_read",
			"block", blockNum,
			"block_min", blockMin,
			"block_max", blockMax,
			"next_min", nextMinTxNum,
			"txnum", txNum,
			"ok", ok,
			"val", val,
			"hist_ok", histOk,
			"hist_len", len(histVal),
			"hist_val", histHash,
			"latest", latestHash,
			"hist_txs", histTxs,
		)
		if blockNum <= 1 {
			var scan []uint64
			for delta := int64(-2); delta <= 2; delta++ {
				if delta == 0 {
					continue
				}
				if delta < 0 && txNum < uint64(-delta) {
					continue
				}
				scan = append(scan, uint64(int64(txNum)+delta))
			}
			for _, probe := range scan {
				pval, pok, perr := readAt(probe)
				if perr != nil {
					return common.Hash{}, perr
				}
				logKV(
					"verify",
					"section", "arbos_slots_dest_scan",
					"block", blockNum,
					"txnum", probe,
					"ok", pok,
					"val", pval,
				)
			}
		}
	}
	if !ok {
		return common.Hash{}, nil
	}
	return val, nil
}

func readOffsets(reader slotReader, blockNum uint64, refs []slotRef) (map[string]common.Hash, error) {
	out := make(map[string]common.Hash, len(refs))
	for _, ref := range refs {
		storageKey := storageKeyForSubspace(ref.Subspace)
		slot := slotForOffset(storageKey, ref.Offset)
		val, err := reader.StorageAt(blockNum, arbosStorageAccount, slot)
		if err != nil {
			return nil, err
		}
		out[ref.Name] = val
	}
	return out, nil
}

func readBytes(reader slotReader, blockNum uint64, subspace []byte) ([]byte, error) {
	storageKey := storageKeyForSubspace(subspace)
	sizeSlot := slotForOffset(storageKey, 0)
	sizeHash, err := reader.StorageAt(blockNum, arbosStorageAccount, sizeSlot)
	if err != nil {
		return nil, err
	}
	size := sizeHash.Big().Uint64()
	out := make([]byte, 0, size)
	bytesLeft := size
	offset := uint64(1)
	for bytesLeft >= 32 {
		slot := slotForOffset(storageKey, offset)
		val, err := reader.StorageAt(blockNum, arbosStorageAccount, slot)
		if err != nil {
			return nil, err
		}
		out = append(out, val.Bytes()...)
		bytesLeft -= 32
		offset++
	}
	if bytesLeft > 0 {
		slot := slotForOffset(storageKey, offset)
		val, err := reader.StorageAt(blockNum, arbosStorageAccount, slot)
		if err != nil {
			return nil, err
		}
		out = append(out, val.Bytes()[32-bytesLeft:]...)
	}
	return out, nil
}
