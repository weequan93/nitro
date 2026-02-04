package main

import (
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	gethrawdb "github.com/ethereum/go-ethereum/core/rawdb"
	gethstate "github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
)

func openSourceChainDB(path string) (ethdb.Database, error) {
	switch gethrawdb.PreexistingDatabase(path) {
	case "pebble":
		return gethrawdb.NewPebbleDBDatabase(path, 0, 0, "dump", true, true, nil)
	case "leveldb":
		return gethrawdb.NewLevelDBDatabase(path, 0, 0, "dump", true)
	default:
		return nil, fmt.Errorf("no supported database found at %s", path)
	}
}

func main() {
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "/tmp/nitro-src/l2chaindata"
	}
	blockStr := os.Getenv("BLOCK")
	if blockStr == "" {
		log.Fatalf("set BLOCK env")
	}
	var blockNum uint64
	if _, err := fmt.Sscan(blockStr, &blockNum); err != nil {
		log.Fatalf("invalid BLOCK: %v", err)
	}
	addrStr := os.Getenv("ADDR")
	if addrStr == "" {
		log.Fatalf("set ADDR env")
	}
	slotsStr := os.Getenv("SLOTS")
	slotsFile := os.Getenv("SLOTS_FILE")

	db, err := openSourceChainDB(dbPath)
	if err != nil {
		log.Fatalf("open source: %v", err)
	}
	defer db.Close()

	hash := gethrawdb.ReadCanonicalHash(db, blockNum)
	header := gethrawdb.ReadHeader(db, hash, blockNum)
	if header == nil {
		log.Fatalf("header %d not found", blockNum)
	}
	stateDb := gethstate.NewDatabase(db)
	statedb, err := gethstate.New(header.Root, stateDb, nil)
	if err != nil {
		log.Fatalf("new state: %v", err)
	}

	addr := common.HexToAddress(addrStr)
	fmt.Printf("addr=%s root=%s block=%d storage_root=%s\n", addr.Hex(), header.Root.Hex(), blockNum, statedb.GetStorageRoot(addr).Hex())
	var slots []string
	if slotsFile != "" {
		data, err := os.ReadFile(slotsFile)
		if err != nil {
			log.Fatalf("read SLOTS_FILE: %v", err)
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				slots = append(slots, line)
			}
		}
	} else {
		for _, s := range strings.Split(slotsStr, ",") {
			s = strings.TrimSpace(s)
			if s != "" {
				slots = append(slots, s)
			}
		}
	}

	if len(slots) == 0 {
		stRoot := statedb.GetStorageRoot(addr)
		stTrie, err := stateDb.OpenStorageTrie(header.Root, addr, stRoot, nil)
		if err != nil {
			log.Fatalf("open storage trie: %v", err)
		}
		type nodeIterator interface {
			NodeIterator(start []byte) (trie.NodeIterator, error)
		}
		nit, ok := stTrie.(nodeIterator)
		if !ok {
			log.Fatalf("storage trie has no NodeIterator")
		}
		rawIt, err := nit.NodeIterator(nil)
		if err != nil {
			log.Fatalf("storage trie iterator: %v", err)
		}
		it := trie.NewIterator(rawIt)
		var items []string
		for it.Next() {
			key := append([]byte(nil), it.Key...)
			val := append([]byte(nil), it.Value...)
			_, content, _, err := rlp.Split(val)
			if err != nil {
				log.Fatalf("rlp split: %v", err)
			}
			preimage := gethrawdb.ReadPreimage(db, common.BytesToHash(key))
			if len(preimage) > 0 {
				items = append(items, fmt.Sprintf("slot=0x%x preimage=0x%x val=0x%x", key, preimage, content))
			} else {
				items = append(items, fmt.Sprintf("slot=0x%x val=0x%x", key, content))
			}
		}
		if it.Err != nil {
			log.Fatalf("storage trie iterate: %v", it.Err)
		}
		sort.Strings(items)
		for _, it := range items {
			fmt.Println(it)
		}
		fmt.Printf("storage_items=%d\n", len(items))
		return
	}

	for _, s := range slots {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		s = strings.TrimPrefix(s, "0x")
		b, err := hex.DecodeString(s)
		if err != nil || len(b) != 32 {
			log.Fatalf("invalid slot %q (need 32-byte hex)", s)
		}
		var key common.Hash
		copy(key[:], b)
		val := statedb.GetState(addr, key)
		fmt.Printf("slot=0x%s val=%s\n", s, val.Hex())
	}
}
