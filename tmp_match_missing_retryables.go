package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"

	"github.com/offchainlabs/nitro/arbos/burn"
	"github.com/offchainlabs/nitro/arbos/storage"
	"github.com/offchainlabs/nitro/arbos/util"
)

func openSourceChainDB(path string) (ethdb.Database, error) {
	switch rawdb.PreexistingDatabase(path) {
	case "pebble":
		return rawdb.NewPebbleDBDatabase(path, 0, 0, "retryable-scan", true, true, nil)
	case "leveldb":
		return rawdb.NewLevelDBDatabase(path, 0, 0, "retryable-scan", true)
	default:
		return nil, fmt.Errorf("no supported database found at %s", path)
	}
}

func loadMissing(path string) (map[string]struct{}, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	missing := make(map[string]struct{})
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		line = strings.TrimPrefix(line, "0x")
		if len(line) != 64 {
			continue
		}
		missing[strings.ToLower(line)] = struct{}{}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return missing, nil
}

func main() {
	source := os.Getenv("SOURCE")
	if source == "" {
		source = "/tmp/nitro-src/l2chaindata"
	}
	blockStr := os.Getenv("BLOCK")
	if blockStr == "" {
		blockStr = "32"
	}
	block, err := strconv.ParseUint(blockStr, 10, 64)
	if err != nil {
		log.Fatalf("invalid BLOCK %q: %v", blockStr, err)
	}
	missingPath := os.Getenv("MISSING")
	if missingPath == "" {
		missingPath = "tmp_missing_slots.log"
	}

	missing, err := loadMissing(missingPath)
	if err != nil {
		log.Fatalf("load missing: %v", err)
	}

	db, err := openSourceChainDB(source)
	if err != nil {
		log.Fatalf("open source: %v", err)
	}
	defer db.Close()

	hash := rawdb.ReadCanonicalHash(db, block)
	header := rawdb.ReadHeader(db, hash, block)
	if header == nil {
		log.Fatalf("header %d not found", block)
	}

	stateDb := state.NewDatabase(db)
	statedb, err := state.New(header.Root, stateDb, nil)
	if err != nil {
		log.Fatalf("new state: %v", err)
	}

	burner := burn.NewSystemBurner(nil, true)
	rootStorage := storage.NewGeth(statedb, burner)

	// ArbOS subspace IDs
	retryablesSubspace := []byte{2}
	timeoutQueueKey := []byte{0}
	calldataKey := []byte{1}
	chainConfigKey := []byte{7}

	retryablesStorage := rootStorage.OpenSubStorage(retryablesSubspace)
	queue := storage.OpenQueue(retryablesStorage.OpenCachedSubStorage(timeoutQueueKey))

	totalMatches := 0
	totalSlots := 0
	err = queue.ForEach(func(i uint64, id common.Hash) (bool, error) {
		sto := retryablesStorage.OpenSubStorage(id.Bytes())
		calldata := sto.OpenStorageBackedBytes(calldataKey)
		size, err := calldata.Size()
		if err != nil {
			return false, err
		}
		dataSlots := size/32 + 1 // includes final slot even if size is multiple of 32
		totalOffsets := dataSlots + 1 // include length slot
		for off := uint64(0); off < totalOffsets; off++ {
			slotKey := calldata.GetStorageSlot(util.UintToHash(off))
			slotHash := crypto.Keccak256Hash(slotKey.Bytes()).Hex()[2:]
			totalSlots++
			if _, ok := missing[slotHash]; ok {
				totalMatches++
				fmt.Printf("match retryable=%s off=%d slot=%s hash=%s\n", id.Hex(), off, slotKey.Hex(), slotHash)
			}
		}
		return false, nil
	})
	if err != nil {
		log.Fatalf("retryable scan: %v", err)
	}

	// Also check chain config bytes (arbos chainConfig subspace)
	chainCfg := rootStorage.OpenStorageBackedBytes(chainConfigKey)
	chainSize, err := chainCfg.Size()
	if err != nil {
		log.Fatalf("chain config size: %v", err)
	}
	chainDataSlots := chainSize/32 + 1
	chainOffsets := chainDataSlots + 1
	chainMatches := 0
	for off := uint64(0); off < chainOffsets; off++ {
		slotKey := chainCfg.GetStorageSlot(util.UintToHash(off))
		slotHash := crypto.Keccak256Hash(slotKey.Bytes()).Hex()[2:]
		totalSlots++
		if _, ok := missing[slotHash]; ok {
			chainMatches++
			totalMatches++
			fmt.Printf("match chainConfig off=%d slot=%s hash=%s\n", off, slotKey.Hex(), slotHash)
		}
	}

	fmt.Printf("checked retryables+chainConfig slots=%d matches=%d chainConfig_matches=%d missing_total=%d\n",
		totalSlots, totalMatches, chainMatches, len(missing))
}
