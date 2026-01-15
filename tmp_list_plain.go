package main

import (
    "context"
    "encoding/binary"
    "fmt"
    "log"
    "os"

    emdbx "github.com/erigontech/erigon/db/kv/mdbx"
    "github.com/erigontech/erigon/db/kv"
    "github.com/erigontech/erigon/execution/types/accounts"
)

func dumpAccountVals(tx kv.Tx) error {
    c, err := tx.Cursor(kv.TblAccountVals)
    if err != nil {
        return err
    }
    defer c.Close()

    count := 0
    for k, v, err := c.First(); k != nil; k, v, err = c.Next() {
        if err != nil {
            return err
        }
        var acc accounts.Account
        fmt.Printf("AccountKey %x klen=%d vlen=%d\n", k, len(k), len(v))
        dec := v
        if len(dec) >= 9 && dec[0] == 0xff && dec[1] == 0xff {
            dec = dec[8:]
        }
        if len(dec) < 16 {
            log.Printf("skip short record %x len=%d", k, len(v))
            continue
        }
        if err := accounts.DeserialiseV3(&acc, dec); err != nil {
            log.Printf("decode fail %x len=%d err=%v", k, len(v), err)
            continue
        }
        fmt.Printf("AccountVals %x nonce=%d bal=%s codehash=%x raw_len=%d\n", k, acc.Nonce, acc.Balance.String(), acc.CodeHash, len(v))
        count++
    }
    log.Printf("AccountVals total: %d\n", count)
    return nil
}

func dumpStorage(tx kv.Tx) error {
    c, err := tx.CursorDupSort(kv.TblStorageVals)
    if err != nil {
        return err
    }
    defer c.Close()

    count := 0
    for k, v, err := c.First(); k != nil && count < 50; k, v, err = c.Next() {
        if err != nil {
            return err
        }
        addr := k
        inc := uint64(0)
        if len(k) >= 28 {
            inc = binary.BigEndian.Uint64(k[20:28])
            addr = k[:20]
        }
        fmt.Printf("StorageVals addr=%x inc=%d key=%x val_len=%d\n", addr, inc, k[28:], len(v))
        count++
    }
    log.Printf("StorageVals sampled: %d\n", count)
    return nil
}

func main() {
    path := "/tmp/mdbx-debug-keep1/l2chaindata"
    if env := os.Getenv("DB_PATH"); env != "" {
        path = env
    }
    db := emdbx.MustOpen(path)
    defer db.Close()

    tx, err := db.BeginRo(context.Background())
    if err != nil {
        log.Fatalf("tx: %v", err)
    }
    defer tx.Rollback()

    if err := dumpAccountVals(tx); err != nil {
        log.Fatalf("dump accountvals: %v", err)
    }
    if err := dumpStorage(tx); err != nil {
        log.Fatalf("dump storage: %v", err)
    }
}
