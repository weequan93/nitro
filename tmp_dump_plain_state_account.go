//go:build erigon
// +build erigon

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
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
	logger := elog.New("component", "tmp-plain-state")
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
		fmt.Printf("root=%s chain_path=%s\n", root, chainPath)
		for _, addr := range addrs {
			val, err := tx.GetOne(kv.PlainState, addr.Bytes())
			if err != nil {
				return fmt.Errorf("GetOne addr %s: %w", addr.Hex(), err)
			}
			if len(val) == 0 {
				fmt.Printf("addr=%s exists=false\n", addr.Hex())
				continue
			}
			var acc accounts.Account
			if err := accounts.DeserialiseV3(&acc, val); err != nil {
				return fmt.Errorf("decode addr %s: %w", addr.Hex(), err)
			}
			fmt.Printf(
				"addr=%s exists=true nonce=%d balance=%s codehash=%s root=%s incarnation=%d\n",
				addr.Hex(),
				acc.Nonce,
				acc.Balance.String(),
				acc.CodeHash.Hex(),
				acc.Root.Hex(),
				acc.Incarnation,
			)
			if os.Getenv("SHOW_DOMAIN_RAW") == "1" {
				fmt.Printf("addr=%s plainstate_val_hex=0x%x\n", addr.Hex(), val)
			}
		}
		return nil
	}); err != nil {
		log.Fatalf("view plain state: %v", err)
	}
}
