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

	"github.com/ethereum/go-ethereum/rlp"
	ecommon "github.com/erigontech/erigon-lib/common"
	elog "github.com/erigontech/erigon-lib/log/v3"
	"github.com/erigontech/erigon/db/datadir"
	"github.com/erigontech/erigon/db/kv"
	"github.com/erigontech/erigon/db/kv/dbcfg"
	emdbx "github.com/erigontech/erigon/db/kv/mdbx"
	"github.com/erigontech/erigon/db/kv/order"
	"github.com/erigontech/erigon/db/kv/rawdbv3"
	"github.com/erigontech/erigon/db/kv/temporal"
	dbstate "github.com/erigontech/erigon/db/state"
	etrie "github.com/erigontech/erigon/execution/trie"
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

func openDestChainDB(path string) (kv.RwDB, error) {
	logger := elog.New("component", "tmp-storage-domain")
	return emdbx.New(dbcfg.ChainDB, logger).Path(path).Open(context.Background())
}

func resolveDestRoot(path string) (string, string) {
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

func computeStorageRootFromDomain(tx kv.TemporalTx, addr ecommon.Address, txNum uint64, limit int) (rawRoot ecommon.Hash, rlpRoot ecommon.Hash, items int, err error) {
	to, ok := kv.NextSubtree(addr.Bytes())
	if !ok {
		to = nil
	}
	if limit > 0 {
		fmt.Printf("addr=%s\n", addr.Hex())
	}
	it, err := tx.RangeAsOf(kv.StorageDomain, addr.Bytes(), to, txNum, order.Asc, kv.Unlim)
	if err != nil {
		return ecommon.Hash{}, ecommon.Hash{}, 0, err
	}
	defer it.Close()

	trRaw := etrie.New(ecommon.Hash{})
	trRlp := etrie.New(ecommon.Hash{})
	items = 0
	for it.HasNext() {
		k, v, err := it.Next()
		if err != nil {
			return ecommon.Hash{}, ecommon.Hash{}, items, err
		}
		if len(v) == 0 {
			continue
		}
		if len(k) < 20 {
			return ecommon.Hash{}, ecommon.Hash{}, items, fmt.Errorf("short storage key: %d bytes", len(k))
		}
		slot := k[20:]
		slotHash, _ := ecommon.HashData(slot)
		trRaw.Update(slotHash.Bytes(), append([]byte(nil), v...))
		enc, encErr := rlp.EncodeToBytes(v)
		if encErr != nil {
			return ecommon.Hash{}, ecommon.Hash{}, items, fmt.Errorf("rlp encode value: %w", encErr)
		}
		trRlp.Update(slotHash.Bytes(), enc)
		if limit > 0 && items < limit {
			fmt.Printf("slot=%x slot_hash=%x value=%x\n", slot, slotHash.Bytes(), v)
		}
		items++
	}
	return trRaw.Hash(), trRlp.Hash(), items, nil
}

func main() {
	dest := os.Getenv("DEST")
	if dest == "" {
		dest = os.Getenv("DB_PATH")
	}
	if dest == "" {
		dest = "/tmp/mdbx-check"
	}
	root, chainPath := resolveDestRoot(dest)
	if root == "" || chainPath == "" {
		log.Fatalf("invalid DEST/DB_PATH: %q", dest)
	}

	block := uint64(0)
	if s := os.Getenv("BLOCK"); s != "" {
		v, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			log.Fatalf("invalid BLOCK %q: %v", s, err)
		}
		block = v
	}
	txNum := uint64(0)
	if s := os.Getenv("TXNUM"); s != "" {
		v, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			log.Fatalf("invalid TXNUM %q: %v", s, err)
		}
		txNum = v
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

	limit := 0
	if s := os.Getenv("LIMIT"); s != "" {
		v, err := strconv.Atoi(s)
		if err != nil {
			log.Fatalf("invalid LIMIT %q: %v", s, err)
		}
		limit = v
	}

	db, err := openDestChainDB(chainPath)
	if err != nil {
		log.Fatalf("open dest chain DB: %v", err)
	}
	defer db.Close()

	dirs, err := buildExecDirs(root)
	if err != nil {
		log.Fatalf("build exec dirs: %v", err)
	}
	logger := elog.New("component", "tmp-storage-domain")
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
		if txNum == 0 {
			if block == 0 {
				return fmt.Errorf("BLOCK or TXNUM required")
			}
			var err error
			txNum, err = rawdbv3.TxNums.Max(tx, block)
			if err != nil {
				return fmt.Errorf("resolve txnum for block %d: %w", block, err)
			}
		}
		fmt.Printf("dest_root=%s chain_path=%s block=%d txnum=%d\n", root, chainPath, block, txNum)
		for _, addr := range addrs {
			rawRoot, rlpRoot, items, err := computeStorageRootFromDomain(tx, addr, txNum, limit)
			if err != nil {
				return fmt.Errorf("addr %s: %w", addr.Hex(), err)
			}
			fmt.Printf("addr=%s domain_root_raw=%s domain_root_rlp=%s items=%d\n", addr.Hex(), rawRoot.Hex(), rlpRoot.Hex(), items)
		}
		return nil
	}); err != nil {
		log.Fatalf("view temporal: %v", err)
	}
}
