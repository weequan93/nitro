package main

import (
	"fmt"
	"log"
	"os"
	"strconv"

	gethcommon "github.com/ethereum/go-ethereum/common"
	gethrawdb "github.com/ethereum/go-ethereum/core/rawdb"
	gethstate "github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/params"

	"github.com/offchainlabs/nitro/arbos/arbosState"
	"github.com/offchainlabs/nitro/arbos/burn"
	"github.com/offchainlabs/nitro/arbos/storage"
)

func openSourceChainDB(path string) (ethdb.Database, error) {
	switch gethrawdb.PreexistingDatabase(path) {
	case "pebble":
		return gethrawdb.NewPebbleDBDatabase(path, 0, 0, "retryable-prefix", true, true, nil)
	case "leveldb":
		return gethrawdb.NewLevelDBDatabase(path, 0, 0, "retryable-prefix", true)
	default:
		return nil, fmt.Errorf("no supported database found at %s", path)
	}
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

	db, err := openSourceChainDB(source)
	if err != nil {
		log.Fatalf("open source: %v", err)
	}
	defer db.Close()

	hash := gethrawdb.ReadCanonicalHash(db, block)
	header := gethrawdb.ReadHeader(db, hash, block)
	if header == nil {
		log.Fatalf("header %d not found", block)
	}

	stateDb := gethstate.NewDatabase(db)
	statedb, err := gethstate.New(header.Root, stateDb, nil)
	if err != nil {
		log.Fatalf("new state: %v", err)
	}

	burner := burn.NewSystemBurner(nil, true)
	arbos, err := arbosState.OpenArbosState(statedb, burner)
	if err != nil {
		log.Fatalf("open arbos: %v", err)
	}

	rootStorage := storage.NewGeth(statedb, burner)
	retryablesStorage := rootStorage.OpenSubStorage([]byte{2})
	chainConfigStorage := rootStorage.OpenSubStorage([]byte{7})

	chainSlot := chainConfigStorage.GetStorageSlot(gethcommon.Hash{})
	chainPrefix := chainSlot.Hex()[2:2+62]
	fmt.Printf("chain_config prefix=%s slot0=%s\n", chainPrefix, chainSlot.Hex())

	queue := arbos.RetryableState().TimeoutQueue
	err = queue.ForEach(func(i uint64, id gethcommon.Hash) (bool, error) {
		sto := retryablesStorage.OpenSubStorage(id.Bytes())
		calldataSto := sto.OpenSubStorage([]byte{1})
		zeroSlot := calldataSto.GetStorageSlot(gethcommon.Hash{})
		prefix := zeroSlot.Hex()[2:2+62]
		fmt.Printf("idx=%d ticket=%s prefix=%s arbos_version=%d\n", i, id.Hex(), prefix, arbos.ArbOSVersion())
		return false, nil
	})
	if err != nil {
		log.Fatalf("queue foreach: %v", err)
	}

	if arbos.ArbOSVersion() < params.ArbosVersion_Stylus {
		// noop, just to import params for build tag consistency
	}
}
