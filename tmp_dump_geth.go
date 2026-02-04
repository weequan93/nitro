package main

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	gethrawdb "github.com/ethereum/go-ethereum/core/rawdb"
	gethstate "github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/rlp"
	gethtrie "github.com/ethereum/go-ethereum/trie"
)

func openSourceChainDB(path string) (ethdb.Database, error) {
    switch gethrawdb.PreexistingDatabase(path) {
    case "pebble":
        return gethrawdb.NewPebbleDBDatabase(path, 0, 0, "dump", true, true, nil)
    case "leveldb":
        return gethrawdb.NewLevelDBDatabase(path, 0, 0, "dump", true)
    default:
        return nil, fmt.Errorf("no supported database found")
    }
}

func main() {
    dbPath := os.Getenv("DB_PATH")
    if dbPath == "" {
        dbPath = "/tmp/nitro-src/l2chaindata"
    }

    blockNum := uint64(1)
    if s := os.Getenv("BLOCK"); s != "" {
        if v, err := strconv.ParseUint(s, 10, 64); err == nil {
            blockNum = v
        } else {
            log.Fatalf("invalid BLOCK env %q: %v", s, err)
        }
    }

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

	addrsEnv := os.Getenv("ADDRS")
	if addrsEnv != "" {
		dumpStorage := os.Getenv("DUMP_STORAGE") == "1"
		for _, a := range strings.Split(addrsEnv, ",") {
			a = strings.TrimSpace(a)
			if a == "" {
				continue
			}
			addr := common.HexToAddress(a)
			exists := statedb.Exist(addr)
			bal := statedb.GetBalance(addr)
			nonce := statedb.GetNonce(addr)
			codeHash := statedb.GetCodeHash(addr)
			storageRoot := statedb.GetStorageRoot(addr)
			fmt.Printf("addr=%s exists=%v balance=%s nonce=%d codehash=%s storage_root=%s\n", addr.Hex(), exists, bal.String(), nonce, codeHash.Hex(), storageRoot.Hex())
			if dumpStorage {
				stRoot := statedb.GetStorageRoot(addr)
				stTrie, err := stateDb.OpenStorageTrie(header.Root, addr, stRoot, nil)
				if err != nil {
					log.Fatalf("open storage trie %s: %v", addr.Hex(), err)
				}
				type nodeIterator interface {
					NodeIterator(start []byte) (gethtrie.NodeIterator, error)
				}
				nit, ok := stTrie.(nodeIterator)
				if !ok {
					log.Fatalf("storage trie has no NodeIterator")
				}
				rawIt, err := nit.NodeIterator(nil)
				if err != nil {
					log.Fatalf("storage trie iterator: %v", err)
				}
				it := gethtrie.NewIterator(rawIt)
				items := 0
				type keyGetter interface {
					GetKey([]byte) []byte
				}
				kg, _ := stTrie.(keyGetter)
				for it.Next() {
					items++
					keyHash := append([]byte(nil), it.Key...)
					_, content, _, err := rlp.Split(it.Value)
					if err != nil {
						log.Fatalf("rlp split %s slot %x: %v", addr.Hex(), it.Key, err)
					}
					var preimage []byte
					if kg != nil {
						preimage = kg.GetKey(keyHash)
					}
					if len(preimage) > 0 {
						fmt.Printf("slot=%x slot_hash=0x%x value=0x%x\n", preimage, keyHash, content)
					} else {
						fmt.Printf("slot_hash=0x%x value=0x%x\n", keyHash, content)
					}
				}
				if it.Err != nil {
					log.Fatalf("storage iterate %s: %v", addr.Hex(), it.Err)
				}
				fmt.Printf("storage_root_trie=%s storage_items=%d\n", stTrie.Hash().Hex(), items)
			}
		}
		fmt.Printf("root=%s expected=%s block=%d\n", statedb.IntermediateRoot(false).Hex(), header.Root.Hex(), blockNum)
		return
	}

	dump := statedb.RawDump(nil)
    for key, info := range dump.Accounts {
        addrStr := key
        if info.Address != nil {
            addrStr = info.Address.Hex()
        }
        fmt.Printf("key=%s addr=%s balance=%s nonce=%d codehash=%s storage=%d\n", key, addrStr, info.Balance, info.Nonce, info.CodeHash, len(info.Storage))
    }
    fmt.Printf("total=%d root=%s expected=%s block=%d\n", len(dump.Accounts), dump.Root, header.Root.Hex(), blockNum)
}
