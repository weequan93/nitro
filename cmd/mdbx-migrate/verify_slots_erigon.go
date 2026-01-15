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
	"github.com/erigontech/erigon/db/kv"
	"github.com/erigontech/erigon/db/kv/rawdbv3"
	"github.com/erigontech/erigon/rpc/rpchelper"

	"github.com/offchainlabs/nitro/arbos/util"
)

type slotRef struct {
	Name     string
	Subspace []byte
	Offset   uint64
}

var arbosStorageAccount = common.HexToAddress("0xA4B05FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF")

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
	reader, err := rpchelper.CreateHistoryStateReader(r.tx, blockNum+1, 0, r.txNumReader)
	if err != nil {
		return common.Hash{}, err
	}
	eaddr := ecommon.BytesToAddress(addr.Bytes())
	eslot := ecommon.BytesToHash(slot.Bytes())
	val, ok, err := reader.ReadAccountStorage(eaddr, eslot)
	if err != nil {
		return common.Hash{}, err
	}
	if !ok {
		return common.Hash{}, nil
	}
	return common.BytesToHash(val.Bytes()), nil
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
