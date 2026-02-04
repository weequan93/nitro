//go:build erigon
// +build erigon

package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	gethcommon "github.com/ethereum/go-ethereum/common"
	gethcrypto "github.com/ethereum/go-ethereum/crypto"
	gethrawdb "github.com/ethereum/go-ethereum/core/rawdb"
	gethstate "github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/trie"

	elog "github.com/erigontech/erigon-lib/log/v3"
	"github.com/erigontech/erigon/db/datadir"
	"github.com/erigontech/erigon/db/kv"
	"github.com/erigontech/erigon/db/kv/dbcfg"
	emdbx "github.com/erigontech/erigon/db/kv/mdbx"
	"github.com/erigontech/erigon/db/kv/order"
	"github.com/erigontech/erigon/db/kv/rawdbv3"
	"github.com/erigontech/erigon/db/kv/temporal"
	dbstate "github.com/erigontech/erigon/db/state"
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
	logger := elog.New("component", "tmp-diff-storage")
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

func main() {
	source := os.Getenv("SOURCE")
	if source == "" {
		source = "/tmp/nitro-src/l2chaindata"
	}
	dest := os.Getenv("DEST")
	if dest == "" {
		dest = "/tmp/mdbx-check"
	}
	addrStr := os.Getenv("ADDR")
	if addrStr == "" {
		log.Fatal("ADDR env required")
	}

	blockStr := os.Getenv("BLOCK")
	if blockStr == "" {
		log.Fatal("BLOCK env required")
	}
	block, err := strconv.ParseUint(blockStr, 10, 64)
	if err != nil {
		log.Fatalf("invalid BLOCK %q: %v", blockStr, err)
	}

	addr := gethcommon.HexToAddress(addrStr)

	// Source geth storage trie hashed keys
	srcDB, err := openSourceChainDB(source)
	if err != nil {
		log.Fatalf("open source: %v", err)
	}
	defer srcDB.Close()

	hash := gethrawdb.ReadCanonicalHash(srcDB, block)
	header := gethrawdb.ReadHeader(srcDB, hash, block)
	if header == nil {
		log.Fatalf("header %d not found", block)
	}
	stateDb := gethstate.NewDatabase(srcDB)
	statedb, err := gethstate.New(header.Root, stateDb, nil)
	if err != nil {
		log.Fatalf("new state: %v", err)
	}
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
	srcKeys := make(map[string]struct{})
	for it.Next() {
		key := append([]byte(nil), it.Key...)
		srcKeys[hex.EncodeToString(key)] = struct{}{}
	}
	if it.Err != nil {
		log.Fatalf("storage trie iterate: %v", it.Err)
	}

	// Destination domain hashed keys
	destRoot, chainPath := resolveDestRoot(dest)
	db, err := openDestChainDB(chainPath)
	if err != nil {
		log.Fatalf("open dest: %v", err)
	}
	defer db.Close()

	dirs, err := buildExecDirs(destRoot)
	if err != nil {
		log.Fatalf("build exec dirs: %v", err)
	}
	logger := elog.New("component", "tmp-diff-storage")
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

	destKeys := make(map[string]struct{})
	slotByHash := make(map[string]string)
	if err := execDB.ViewTemporal(context.Background(), func(tx kv.TemporalTx) error {
		txNum, err := rawdbv3.TxNums.Max(tx, block)
		if err != nil {
			return fmt.Errorf("resolve txnum: %w", err)
		}
		to, ok := kv.NextSubtree(addr.Bytes())
		if !ok {
			to = nil
		}
		it, err := tx.RangeAsOf(kv.StorageDomain, addr.Bytes(), to, txNum, order.Asc, kv.Unlim)
		if err != nil {
			return err
		}
		defer it.Close()
		for it.HasNext() {
			k, v, err := it.Next()
			if err != nil {
				return err
			}
			if len(v) == 0 || len(k) < 20 {
				continue
			}
			slot := k[20:]
			// StorageDomain keys are addr||plain_slot. Compare by hashed slot like geth storage trie.
			h := hex.EncodeToString(gethcrypto.Keccak256Hash(slot).Bytes())
			destKeys[h] = struct{}{}
			if _, ok := slotByHash[h]; !ok {
				slotByHash[h] = hex.EncodeToString(slot)
			}
		}
		return nil
	}); err != nil {
		log.Fatalf("dest view temporal: %v", err)
	}

	onlyDest := make([]string, 0)
	for h := range destKeys {
		if _, ok := srcKeys[h]; !ok {
			onlyDest = append(onlyDest, h)
		}
	}
	onlySrc := make([]string, 0)
	for h := range srcKeys {
		if _, ok := destKeys[h]; !ok {
			onlySrc = append(onlySrc, h)
		}
	}

	fmt.Printf("addr=%s block=%d src=%d dest=%d only_dest=%d only_src=%d\n", strings.ToLower(addr.Hex()), block, len(srcKeys), len(destKeys), len(onlyDest), len(onlySrc))
	if len(onlyDest) > 0 {
		fmt.Println("extra_in_dest:")
		for _, h := range onlyDest {
			if slot, ok := slotByHash[h]; ok {
				fmt.Printf("hash=%s slot=%s\n", h, slot)
			} else {
				fmt.Printf("hash=%s slot=<unknown>\n", h)
			}
		}
	}
	if len(onlySrc) > 0 {
		fmt.Println("missing_in_dest:")
		for _, h := range onlySrc {
			fmt.Printf("hash=%s\n", h)
		}
	}
}
