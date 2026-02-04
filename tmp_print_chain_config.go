package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/ethereum/go-ethereum/common"
	gethrawdb "github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/ethdb"
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
	db, err := openSourceChainDB(dbPath)
	if err != nil {
		log.Fatalf("open source: %v", err)
	}
	defer db.Close()

	genesisHash := gethrawdb.ReadCanonicalHash(db, 0)
	if genesisHash == (common.Hash{}) {
		log.Fatalf("genesis hash not found in %s", dbPath)
	}
	cfg := gethrawdb.ReadChainConfig(db, genesisHash)
	if cfg == nil {
		log.Fatalf("chain config not found for genesis %x", genesisHash)
	}

	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		log.Fatalf("marshal: %v", err)
	}
	fmt.Printf("%s\n", out)
}
