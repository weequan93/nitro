package main

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"sort"

	gethcommon "github.com/ethereum/go-ethereum/common"
	gethrawdb "github.com/ethereum/go-ethereum/core/rawdb"
	gethstate "github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/crypto"
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

	addr := gethcommon.HexToAddress(addrStr)

	type entry struct {
		raw, h1, h2 []byte
		full        []byte
		trimmed     []byte
		rlpTrimmed  []byte
		rlpFull     []byte
	}
	var entries []entry
	type leafEntry struct {
		key []byte
		val []byte
	}
	var leafEntries []leafEntry
	preimageMissing := 0
	preimagePresent := 0

	if err := gethstate.ForEachStorage(statedb, addr, func(key, value gethcommon.Hash) bool {
		slot := append([]byte(nil), key.Bytes()...)   // 32-byte slot
		full := append([]byte(nil), value.Bytes()...) // 32-byte value
		trimmed := gethcommon.TrimLeftZeroes(full)
		trimmedCopy := append([]byte(nil), trimmed...)
		rlpTrimmed, _ := rlp.EncodeToBytes(trimmedCopy)
		rlpFull, _ := rlp.EncodeToBytes(full)

		h1 := append([]byte(nil), crypto.Keccak256Hash(slot).Bytes()...)
		h2 := append([]byte(nil), crypto.Keccak256Hash(h1).Bytes()...)

		entries = append(entries, entry{
			raw:        slot,
			h1:         h1,
			h2:         h2,
			full:       full,
			trimmed:    trimmedCopy,
			rlpTrimmed: rlpTrimmed,
			rlpFull:    rlpFull,
		})
		return true
	}); err != nil {
		log.Fatalf("ForEachStorage: %v", err)
	}

	// Iterate the storage trie directly (hashed keys) to check preimages.
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
	for it.Next() {
		key := append([]byte(nil), it.Key...)
		val := append([]byte(nil), it.Value...)
		leafEntries = append(leafEntries, leafEntry{key: key, val: val})
		if len(gethrawdb.ReadPreimage(db, gethcommon.BytesToHash(key))) == 0 {
			preimageMissing++
		} else {
			preimagePresent++
		}
	}
	if it.Err != nil {
		log.Fatalf("storage trie iterate: %v", it.Err)
	}

	entriesByH1 := append([]entry(nil), entries...)
	entriesByH2 := append([]entry(nil), entries...)
	entriesByRaw := append([]entry(nil), entries...)
	sort.Slice(entriesByH1, func(i, j int) bool {
		return bytes.Compare(entriesByH1[i].h1, entriesByH1[j].h1) < 0
	})
	sort.Slice(entriesByH2, func(i, j int) bool {
		return bytes.Compare(entriesByH2[i].h2, entriesByH2[j].h2) < 0
	})
	sort.Slice(entriesByRaw, func(i, j int) bool {
		return bytes.Compare(entriesByRaw[i].raw, entriesByRaw[j].raw) < 0
	})

	hashStack := func(list []entry, keyFn func(entry) []byte, valFn func(entry) []byte) gethcommon.Hash {
		tr := trie.NewStackTrie(nil)
		for _, e := range list {
			tr.Update(keyFn(e), valFn(e))
		}
		return tr.Hash()
	}

	rootHashRlp := hashStack(entriesByH1, func(e entry) []byte { return e.h1 }, func(e entry) []byte { return e.rlpTrimmed })
	rootHashRawTrim := hashStack(entriesByH1, func(e entry) []byte { return e.h1 }, func(e entry) []byte { return e.trimmed })
	rootHashRawFull := hashStack(entriesByH1, func(e entry) []byte { return e.h1 }, func(e entry) []byte { return e.full })
	rootHashRlpFull := hashStack(entriesByH1, func(e entry) []byte { return e.h1 }, func(e entry) []byte { return e.rlpFull })
	rootDoubleHashRlp := hashStack(entriesByH2, func(e entry) []byte { return e.h2 }, func(e entry) []byte { return e.rlpTrimmed })
	rootDoubleHashRawTrim := hashStack(entriesByH2, func(e entry) []byte { return e.h2 }, func(e entry) []byte { return e.trimmed })
	rootRawKeyRlp := hashStack(entriesByRaw, func(e entry) []byte { return e.raw }, func(e entry) []byte { return e.rlpTrimmed })
	rootRawKeyRawTrim := hashStack(entriesByRaw, func(e entry) []byte { return e.raw }, func(e entry) []byte { return e.trimmed })

	leafEntriesSorted := append([]leafEntry(nil), leafEntries...)
	sort.Slice(leafEntriesSorted, func(i, j int) bool {
		return bytes.Compare(leafEntriesSorted[i].key, leafEntriesSorted[j].key) < 0
	})
	leafTrie := trie.NewStackTrie(nil)
	leafTrieRawContent := trie.NewStackTrie(nil)
	for _, e := range leafEntriesSorted {
		leafTrie.Update(e.key, e.val)
		// Also try using raw content (decoded from RLP) as value, to mirror domain behavior.
		_, content, _, err := rlp.Split(e.val)
		if err == nil && len(content) > 0 {
			leafTrieRawContent.Update(e.key, content)
		}
	}

	fmt.Printf("addr=%s block=%d items=%d\n", addr.Hex(), blockNum, len(entries))
	fmt.Printf("geth_storage_root=%s\n", statedb.GetStorageRoot(addr).Hex())
	fmt.Printf("root_hash_rlp(trimmed)=%s\n", rootHashRlp.Hex())
	fmt.Printf("root_hash_raw(trimmed)=%s\n", rootHashRawTrim.Hex())
	fmt.Printf("root_hash_raw(full32)=%s\n", rootHashRawFull.Hex())
	fmt.Printf("root_hash_rlp(full32)=%s\n", rootHashRlpFull.Hex())
	fmt.Printf("root_doublehash_rlp(trimmed)=%s\n", rootDoubleHashRlp.Hex())
	fmt.Printf("root_doublehash_raw(trimmed)=%s\n", rootDoubleHashRawTrim.Hex())
	fmt.Printf("root_rawkey_rlp(trimmed)=%s\n", rootRawKeyRlp.Hex())
	fmt.Printf("root_rawkey_raw(trimmed)=%s\n", rootRawKeyRawTrim.Hex())
	fmt.Printf("root_hashedkey_leafval=%s\n", leafTrie.Hash().Hex())
	fmt.Printf("root_hashedkey_rawcontent=%s\n", leafTrieRawContent.Hash().Hex())
	fmt.Printf("preimage_present=%d preimage_missing=%d\n", preimagePresent, preimageMissing)
}
