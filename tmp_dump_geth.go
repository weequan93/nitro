package main

import (
    "fmt"
    "log"
    "os"
    "strconv"

    gethrawdb "github.com/ethereum/go-ethereum/core/rawdb"
    gethstate "github.com/ethereum/go-ethereum/core/state"
    "github.com/ethereum/go-ethereum/ethdb"
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
    dbPath := "/tmp/nitro-copy/l2chaindata"

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
