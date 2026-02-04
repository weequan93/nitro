package main

import (
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/holiman/uint256"

	"github.com/ethereum/go-ethereum/common"
	gethrawdb "github.com/ethereum/go-ethereum/core/rawdb"
	gethstate "github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/ethdb"

	ecommon "github.com/erigontech/erigon-lib/common"
	eaccounts "github.com/erigontech/erigon/execution/types/accounts"
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
	addrsEnv := os.Getenv("ADDRS")
	if addrsEnv == "" {
		log.Fatalf("set ADDRS env (comma-separated)")
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

	for _, a := range strings.Split(addrsEnv, ",") {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		addr := common.HexToAddress(a)
		nonce := statedb.GetNonce(addr)
		bal := statedb.GetBalance(addr)
		codeHash := statedb.GetCodeHash(addr)
		acc := eaccounts.NewAccount()
		acc.Nonce = nonce
		acc.Balance = *uint256.MustFromBig(bal.ToBig())
		acc.CodeHash = ecommon.BytesToHash(codeHash.Bytes())
		encStorageLen := acc.EncodingLengthForStorage()
		buf := make([]byte, encStorageLen)
		acc.EncodeForStorage(buf)
		encV3 := eaccounts.SerialiseV3(&acc)
		fmt.Printf("addr=%s nonce=%d balance=%s codehash=%s enc=%s\n",
			addr.Hex(),
			nonce,
			bal.String(),
			codeHash.Hex(),
			hex.EncodeToString(buf),
		)
		fmt.Printf("addr=%s v3=%s\n", addr.Hex(), hex.EncodeToString(encV3))
	}
}
