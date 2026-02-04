//go:build erigon
// +build erigon

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	ecommon "github.com/erigontech/erigon-lib/common"
	elog "github.com/erigontech/erigon-lib/log/v3"
	"github.com/erigontech/erigon/db/datadir"
	"github.com/erigontech/erigon/db/kv"
	"github.com/erigontech/erigon/db/kv/dbcfg"
	emdbx "github.com/erigontech/erigon/db/kv/mdbx"
	"github.com/erigontech/erigon/db/kv/rawdbv3"
	"github.com/erigontech/erigon/db/kv/temporal"
	dbstate "github.com/erigontech/erigon/db/state"
	"github.com/erigontech/erigon/execution/types/accounts"
)

func buildExecDirs(dest string) (datadir.Dirs, error) {
	dirs := datadir.Dirs{
		DataDir:          dest,
		RelativeDataDir:  dest,
		Chaindata:        filepath.Join(dest, "l2chaindata"),
		ArbitrumWasm:     filepath.Join(dest, "wasm"),
		Tmp:              filepath.Join(dest, "tmp"),
		Snap:             filepath.Join(dest, "snapshots"),
		SnapIdx:          filepath.Join(dest, "snapshots", "idx"),
		SnapHistory:      filepath.Join(dest, "snapshots", "history"),
		SnapDomain:       filepath.Join(dest, "snapshots", "domain"),
		SnapAccessors:    filepath.Join(dest, "snapshots", "accessor"),
		SnapCaplin:       filepath.Join(dest, "snapshots", "caplin"),
		Downloader:       filepath.Join(dest, "downloader"),
		TxPool:           filepath.Join(dest, "txpool"),
		Nodes:            filepath.Join(dest, "nodes"),
		CaplinBlobs:      filepath.Join(dest, "caplin", "blobs"),
		CaplinColumnData: filepath.Join(dest, "caplin", "column"),
		CaplinIndexing:   filepath.Join(dest, "caplin", "indexing"),
		CaplinLatest:     filepath.Join(dest, "caplin", "latest"),
		CaplinGenesis:    filepath.Join(dest, "caplin", "genesis-state"),
	}

	paths := []string{
		dirs.Chaindata,
		dirs.ArbitrumWasm,
		dirs.Tmp,
		dirs.Snap,
		dirs.SnapIdx,
		dirs.SnapHistory,
		dirs.SnapDomain,
		dirs.SnapAccessors,
		dirs.SnapCaplin,
		dirs.Downloader,
		dirs.TxPool,
		dirs.Nodes,
		dirs.CaplinBlobs,
		dirs.CaplinColumnData,
		dirs.CaplinIndexing,
		dirs.CaplinLatest,
		dirs.CaplinGenesis,
	}
	for _, path := range paths {
		if err := os.MkdirAll(path, 0o755); err != nil {
			return datadir.Dirs{}, fmt.Errorf("create dir %s: %w", path, err)
		}
	}
	return dirs, nil
}

func openChainDB(path string) (kv.RwDB, error) {
	logger := elog.New("component", "tmp-accounts-domain")
	return emdbx.New(dbcfg.ChainDB, logger).Path(path).Open(context.Background())
}

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

	block := uint64(0)
	blockSet := false
	if s := os.Getenv("BLOCK"); s != "" {
		v, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			log.Fatalf("invalid BLOCK %q: %v", s, err)
		}
		block = v
		blockSet = true
	}
	txNum := uint64(0)
	txNumSet := false
	if s := os.Getenv("TXNUM"); s != "" {
		v, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			log.Fatalf("invalid TXNUM %q: %v", s, err)
		}
		txNum = v
		txNumSet = true
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

	dirs, err := buildExecDirs(root)
	if err != nil {
		log.Fatalf("build exec dirs: %v", err)
	}
	logger := elog.New("component", "tmp-accounts-domain")
	agg, err := dbstate.New(dirs).Logger(logger).GenSaltIfNeed(true).Open(context.Background(), db)
	if err != nil {
		log.Fatalf("open state aggregator: %v", err)
	}
	if err := agg.OpenFolder(); err != nil {
		agg.Close()
		log.Fatalf("open state snapshots: %v", err)
	}
	defer agg.Close()

	execDB, err := temporal.New(db, agg)
	if err != nil {
		log.Fatalf("open temporal db: %v", err)
	}

	ctx := context.Background()
	if err := execDB.ViewTemporal(ctx, func(tx kv.TemporalTx) error {
		if !txNumSet {
			if !blockSet {
				return fmt.Errorf("BLOCK or TXNUM required")
			}
			var err error
			txNum, err = rawdbv3.TxNums.Max(tx, block)
			if err != nil {
				return fmt.Errorf("resolve txnum for block %d: %w", block, err)
			}
		}
		fmt.Printf("root=%s chain_path=%s block=%d txnum=%d\n", root, chainPath, block, txNum)
		for _, addr := range addrs {
			val, ok, err := tx.GetAsOf(kv.AccountsDomain, addr.Bytes(), txNum)
			if err != nil {
				return fmt.Errorf("GetAsOf addr %s: %w", addr.Hex(), err)
			}
			if !ok || len(val) == 0 {
				fmt.Printf("addr=%s exists=false\n", addr.Hex())
				continue
			}
			var acc accounts.Account
			if err := accounts.DeserialiseV3(&acc, val); err != nil {
				return fmt.Errorf("decode addr %s: %w", addr.Hex(), err)
			}
			fmt.Printf("addr=%s exists=true nonce=%d balance=%s codehash=%s root=%s incarnation=%d prev_inc=%d\n",
				addr.Hex(),
				acc.Nonce,
				acc.Balance.String(),
				acc.CodeHash.Hex(),
				acc.Root.Hex(),
				acc.Incarnation,
				acc.PrevIncarnation,
			)
			if os.Getenv("SHOW_DOMAIN_RAW") == "1" {
				fmt.Printf("addr=%s domain_val_hex=0x%x\n", addr.Hex(), val)
			}
		}
		return nil
	}); err != nil {
		log.Fatalf("view temporal: %v", err)
	}
}
