//go:build erigon
// +build erigon

package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	ecommon "github.com/erigontech/erigon-lib/common"
	elog "github.com/erigontech/erigon-lib/log/v3"
	"github.com/erigontech/erigon/db/kv"
	"github.com/erigontech/erigon/db/kv/dbcfg"
	emdbx "github.com/erigontech/erigon/db/kv/mdbx"
	"github.com/erigontech/erigon/execution/types/accounts"
)

func resolveRoot(path string) (string, string) {
	if path == "" {
		return "", ""
	}
	clean := filepath.Clean(path)
	if filepath.Base(clean) == "l2chaindata" {
		root := filepath.Dir(clean)
		return root, clean
	}
	return clean, filepath.Join(clean, "l2chaindata")
}

func openChainDB(path string) (kv.RwDB, error) {
	logger := elog.New("component", "tmp-account-history")
	return emdbx.New(dbcfg.ChainDB, logger).Path(path).Open(context.Background())
}

func main() {
	dest := os.Getenv("DEST")
	if dest == "" {
		dest = os.Getenv("DB_PATH")
	}
	if dest == "" {
		dest = "/tmp/mdbx-check"
	}
	root, chainPath := resolveRoot(dest)
	if root == "" || chainPath == "" {
		log.Fatalf("invalid DEST/DB_PATH: %q", dest)
	}

	limit := 10
	if s := os.Getenv("LIMIT"); s != "" {
		v, err := strconv.Atoi(s)
		if err != nil || v <= 0 {
			log.Fatalf("invalid LIMIT %q", s)
		}
		limit = v
	}

	addrsEnv := os.Getenv("ADDRS")
	if addrsEnv == "" {
		log.Fatal("ADDRS env is required")
	}
	var addrs []ecommon.Address
	for _, a := range strings.Split(addrsEnv, ",") {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		addrs = append(addrs, ecommon.HexToAddress(a))
	}
	if len(addrs) == 0 {
		log.Fatal("no ADDRS provided")
	}

	db, err := openChainDB(chainPath)
	if err != nil {
		log.Fatalf("open chain DB: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := db.View(ctx, func(tx kv.Tx) error {
		fmt.Printf("root=%s chain_path=%s table=%s\n", root, chainPath, kv.TblAccountHistoryVals)
		c, err := tx.CursorDupSort(kv.TblAccountHistoryVals)
		if err != nil {
			return fmt.Errorf("cursor dupsort: %w", err)
		}
		defer c.Close()

		for _, addr := range addrs {
			addrKey := addr.Bytes()
			k, v, err := c.SeekExact(addrKey)
			if err != nil {
				return fmt.Errorf("seek addr %s: %w", addr.Hex(), err)
			}
			if k == nil {
				fmt.Printf("addr=%s history=none\n", addr.Hex())
				continue
			}
			fmt.Printf("addr=%s history:\n", addr.Hex())

			count := 0
			for {
				if v == nil {
					break
				}
				if len(v) < 8 {
					fmt.Printf("  txnum=? value_len=%d (invalid)\n", len(v))
				} else {
					txNum := binary.BigEndian.Uint64(v[:8])
					payload := v[8:]
					if len(payload) == 0 {
						fmt.Printf("  txnum=%d value=<created_marker>\n", txNum)
					} else {
						var acc accounts.Account
						if err := accounts.DeserialiseV3(&acc, payload); err != nil {
							fmt.Printf("  txnum=%d value=<decode_error %v>\n", txNum, err)
						} else {
							fmt.Printf("  txnum=%d nonce=%d balance=%s codehash=%s root=%s incarnation=%d\n",
								txNum, acc.Nonce, acc.Balance.String(), acc.CodeHash.Hex(), acc.Root.Hex(), acc.Incarnation)
						}
					}
				}
				count++
				if count >= limit {
					break
				}
				_, v, err = c.NextDup()
				if err != nil {
					return fmt.Errorf("nextdup addr %s: %w", addr.Hex(), err)
				}
			}
		}
		return nil
	}); err != nil {
		log.Fatalf("view account history: %v", err)
	}
}
