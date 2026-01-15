//go:build erigon
// +build erigon

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	gethcommon "github.com/ethereum/go-ethereum/common"
	gethrawdb "github.com/ethereum/go-ethereum/core/rawdb"
	gethstate "github.com/ethereum/go-ethereum/core/state"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/ethereum/go-ethereum/triedb/hashdb"
	"github.com/ethereum/go-ethereum/triedb/pathdb"
	"github.com/holiman/uint256"

	ecommon "github.com/erigontech/erigon-lib/common"
	elog "github.com/erigontech/erigon-lib/log/v3"
	"github.com/erigontech/erigon/core/state"
	"github.com/erigontech/erigon/db/datadir"
	"github.com/erigontech/erigon/db/kv"
	"github.com/erigontech/erigon/db/kv/dbcfg"
	emdbx "github.com/erigontech/erigon/db/kv/mdbx"
	"github.com/erigontech/erigon/db/kv/order"
	"github.com/erigontech/erigon/db/kv/rawdbv3"
	"github.com/erigontech/erigon/db/kv/temporal"
	erigonrawdb "github.com/erigontech/erigon/db/rawdb"
	dbstate "github.com/erigontech/erigon/db/state"
	etrie "github.com/erigontech/erigon/execution/trie"
)

type compositeKey struct {
	addr []byte
	slot []byte
	line int
}

type addressKey struct {
	addr []byte
	line int
}

type gethAccountDump struct {
	nonce    uint64
	balance  string
	root     []byte
	codeHash []byte
}

type dumpCollector struct {
	targets      map[string]struct{}
	targetHashes map[string][]byte
	accountsDump map[string]gethAccountDump
}

func (d *dumpCollector) OnRoot(_ gethcommon.Hash) {}

func (d *dumpCollector) OnAccount(addr *gethcommon.Address, account gethstate.DumpAccount) {
	if addr != nil {
		raw := addr.Bytes()
		if _, ok := d.targets[string(raw)]; ok {
			d.accountsDump[string(raw)] = gethAccountDump{
				nonce:    account.Nonce,
				balance:  account.Balance,
				root:     account.Root,
				codeHash: account.CodeHash,
			}
		}
		return
	}
	if len(account.AddressHash) == 0 {
		return
	}
	if addrBytes, ok := d.targetHashes[string(account.AddressHash)]; ok {
		d.accountsDump[string(addrBytes)] = gethAccountDump{
			nonce:    account.Nonce,
			balance:  account.Balance,
			root:     account.Root,
			codeHash: account.CodeHash,
		}
	}
}

type gethStateReader struct {
	statedb *gethstate.StateDB
	trieDB  *triedb.Database
}

func newGethStateReader(db ethdb.Database, block uint64) (*gethStateReader, error) {
	hash := gethrawdb.ReadCanonicalHash(db, block)
	header := gethrawdb.ReadHeader(db, hash, block)
	if header == nil {
		return nil, fmt.Errorf("missing header %d", block)
	}

	scheme, err := gethrawdb.ParseStateScheme("", db)
	if err != nil {
		return nil, err
	}

	trieCfg := &triedb.Config{Preimages: false}
	if scheme == gethrawdb.HashScheme {
		trieCfg.HashDB = hashdb.Defaults
	} else {
		trieCfg.PathDB = pathdb.Defaults
	}

	stateDb := gethstate.NewDatabaseWithConfig(db, trieCfg)
	statedb, err := gethstate.New(header.Root, stateDb, nil)
	if err != nil {
		return nil, err
	}

	return &gethStateReader{
		statedb: statedb,
		trieDB:  stateDb.TrieDB(),
	}, nil
}

func (r *gethStateReader) Close() {
	if r.trieDB != nil {
		r.trieDB.Close()
	}
}

func openSourceChainDB(path string) (ethdb.Database, error) {
	switch gethrawdb.PreexistingDatabase(path) {
	case "pebble":
		return gethrawdb.NewPebbleDBDatabase(path, 0, 0, "mdbx-storage-diff", true, true, nil)
	case "leveldb":
		return gethrawdb.NewLevelDBDatabase(path, 0, 0, "mdbx-storage-diff", true)
	default:
		return nil, errors.New("no supported database found")
	}
}

func openDestChainDB(path string) (kv.RwDB, error) {
	logger := elog.New("component", "mdbx-storage-diff")
	opts := emdbx.New(dbcfg.ChainDB, logger).Path(path)
	return opts.Open(context.Background())
}

func resolveChainPath(base string) string {
	if filepath.Base(base) == "l2chaindata" {
		return base
	}
	return filepath.Join(base, "l2chaindata")
}

func resolveChainRoot(base string) string {
	if filepath.Base(base) == "l2chaindata" {
		return filepath.Dir(base)
	}
	return base
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

func loadCompositeKeys(path string, includeAddrs bool) ([]compositeKey, []addressKey, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, 0, err
	}
	defer f.Close()

	var (
		keys          []compositeKey
		addrs         []addressKey
		skippedPrefix int
	)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024), 1024*1024)
	for lineNo := 1; scanner.Scan(); lineNo++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		line = strings.TrimPrefix(line, "0x")
		if strings.Contains(line, "...len=") {
			return nil, nil, skippedPrefix, fmt.Errorf("line %d looks truncated: %q", lineNo, line)
		}
		raw, err := hex.DecodeString(line)
		if err != nil {
			return nil, nil, skippedPrefix, fmt.Errorf("line %d: %w", lineNo, err)
		}
		switch len(raw) {
		case 20:
			if includeAddrs {
				addrs = append(addrs, addressKey{
					addr: append([]byte(nil), raw...),
					line: lineNo,
				})
				continue
			}
			skippedPrefix++
		case 52:
			keys = append(keys, compositeKey{
				addr: append([]byte(nil), raw[:20]...),
				slot: append([]byte(nil), raw[20:]...),
				line: lineNo,
			})
		default:
			return nil, nil, skippedPrefix, fmt.Errorf("line %d: unexpected key length %d", lineNo, len(raw))
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, skippedPrefix, err
	}
	return keys, addrs, skippedPrefix, nil
}

func readHeadNumber(tx kv.Getter) *uint64 {
	headHash := erigonrawdb.ReadHeadHeaderHash(tx)
	if headHash == (ecommon.Hash{}) {
		return nil
	}
	return erigonrawdb.ReadHeaderNumber(tx, headHash)
}

func computeStorageRootFromDomain(tx kv.TemporalTx, addr ecommon.Address, txNum uint64) (gethcommon.Hash, error) {
	to, ok := kv.NextSubtree(addr.Bytes())
	if !ok {
		to = nil
	}
	it, err := tx.RangeAsOf(kv.StorageDomain, addr.Bytes(), to, txNum, order.Asc, kv.Unlim)
	if err != nil {
		return gethcommon.Hash{}, err
	}
	defer it.Close()

	tr := etrie.New(ecommon.Hash{})
	for it.HasNext() {
		k, v, err := it.Next()
		if err != nil {
			return gethcommon.Hash{}, err
		}
		if len(v) == 0 {
			continue
		}
		if len(k) < 20 {
			return gethcommon.Hash{}, fmt.Errorf("short storage key: %d bytes", len(k))
		}
		slot := k[20:]
		slotHash, _ := ecommon.HashData(slot)
		tr.Update(slotHash.Bytes(), v)
	}
	root := tr.Hash()
	return gethcommon.BytesToHash(root.Bytes()), nil
}

func main() {
	var (
		source             string
		dest               string
		keysPath           string
		block              uint64
		compareAccounts    bool
		compareStorageRoot bool
		erigonLatestFallback bool
	)

	flag.StringVar(&source, "source", "", "path to source chain directory (root or l2chaindata)")
	flag.StringVar(&dest, "dest", "", "path to destination chain directory (root or l2chaindata)")
	flag.StringVar(&keysPath, "keys", "", "path to storage keys file")
	flag.Uint64Var(&block, "block", 0, "block number to compare")
	flag.BoolVar(&compareAccounts, "compare-accounts", false, "compare accounts/code for 20-byte keys")
	flag.BoolVar(&compareStorageRoot, "compare-storage-root", false, "compute storage root from storage domain and compare to geth")
	flag.BoolVar(&erigonLatestFallback, "erigon-latest-fallback", false, "use latest erigon state when history lookup is empty (only valid at head)")
	flag.Parse()

	if source == "" || dest == "" || keysPath == "" {
		fmt.Fprintln(os.Stderr, "mdbx-storage-diff: --source, --dest, and --keys are required")
		os.Exit(2)
	}

	keys, addrs, skippedPrefix, err := loadCompositeKeys(keysPath, compareAccounts || compareStorageRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mdbx-storage-diff: load keys: %v\n", err)
		os.Exit(2)
	}

	srcChain, err := openSourceChainDB(resolveChainPath(source))
	if err != nil {
		fmt.Fprintf(os.Stderr, "mdbx-storage-diff: open source: %v\n", err)
		os.Exit(2)
	}
	defer srcChain.Close()

	dstChain, err := openDestChainDB(resolveChainPath(dest))
	if err != nil {
		fmt.Fprintf(os.Stderr, "mdbx-storage-diff: open dest: %v\n", err)
		os.Exit(2)
	}
	defer dstChain.Close()

	gethReader, err := newGethStateReader(srcChain, block)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mdbx-storage-diff: open geth state: %v\n", err)
		os.Exit(2)
	}
	defer gethReader.Close()

	destRoot := resolveChainRoot(dest)
	dirs, err := buildExecDirs(destRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mdbx-storage-diff: init dirs: %v\n", err)
		os.Exit(2)
	}

	logger := elog.New("component", "mdbx-storage-diff")
	agg, err := dbstate.New(dirs).Logger(logger).GenSaltIfNeed(true).Open(context.Background(), dstChain)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mdbx-storage-diff: open state aggregator: %v\n", err)
		os.Exit(2)
	}
	if err := agg.OpenFolder(); err != nil {
		agg.Close()
		fmt.Fprintf(os.Stderr, "mdbx-storage-diff: open state snapshots: %v\n", err)
		os.Exit(2)
	}
	defer agg.Close()

	execDB, err := temporal.New(dstChain, agg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mdbx-storage-diff: open temporal db: %v\n", err)
		os.Exit(2)
	}

	tx, err := execDB.BeginTemporalRo(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "mdbx-storage-diff: begin dest tx: %v\n", err)
		os.Exit(2)
	}
	defer tx.Rollback()

	headNum := readHeadNumber(tx)
	if headNum == nil {
		fmt.Fprintln(os.Stderr, "mdbx-storage-diff: missing head header number in destination DB")
		os.Exit(2)
	}
	if erigonLatestFallback && block != *headNum {
		fmt.Fprintf(os.Stderr, "mdbx-storage-diff: erigon-latest-fallback ignored for block %d (head=%d)\n", block, *headNum)
		erigonLatestFallback = false
	}

	maxTxNum, err := rawdbv3.TxNums.Max(tx, block)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mdbx-storage-diff: read max txnum: %v\n", err)
		os.Exit(2)
	}
	txNum := maxTxNum

	historyReader := state.NewHistoryReaderV3()
	historyReader.SetTx(tx)
	historyReader.SetTxNum(txNum)
	stateReader := state.StateReader(historyReader)
	latestReader := state.StateReader(state.NewReaderV3(tx))

	fmt.Fprintf(os.Stderr, "mdbx-storage-diff: using history txnum=%d (max=%d) for block %d (head=%d)\n", txNum, maxTxNum, block, *headNum)

	if (compareAccounts || compareStorageRoot) && len(addrs) > 0 {
		targets := make(map[string]struct{}, len(addrs))
		targetHashes := make(map[string][]byte, len(addrs))
		for _, key := range addrs {
			targets[string(key.addr)] = struct{}{}
			hash := crypto.Keccak256(key.addr)
			targetHashes[string(hash)] = key.addr
		}
		accountsDump := make(map[string]gethAccountDump, len(addrs))
		collector := &dumpCollector{
			targets:      targets,
			targetHashes: targetHashes,
			accountsDump: accountsDump,
		}
		_ = gethReader.statedb.DumpToCollector(collector, &gethstate.DumpConfig{
			SkipCode:    true,
			SkipStorage: true,
		})

		for i, key := range addrs {
			gethAddr := gethcommon.BytesToAddress(key.addr)
			gethExists := gethReader.statedb.Exist(gethAddr)
			gethBalance := gethReader.statedb.GetBalance(gethAddr)
			gethNonce := gethReader.statedb.GetNonce(gethAddr)
			gethCodeHash := gethReader.statedb.GetCodeHash(gethAddr)
			gethCode := gethReader.statedb.GetCode(gethAddr)
			if gethBalance == nil {
				gethBalance = new(uint256.Int)
			}

			erigonAddr := ecommon.BytesToAddress(key.addr)
			erigonReader := stateReader
			erigonAcc, err := erigonReader.ReadAccountData(erigonAddr)
			if err != nil {
				fmt.Fprintf(os.Stderr, "mdbx-storage-diff: erigon account read line %d: %v\n", key.line, err)
				os.Exit(2)
			}
			usedLatest := false
			if erigonAcc == nil && erigonLatestFallback {
				latestAcc, latestErr := latestReader.ReadAccountData(erigonAddr)
				if latestErr != nil {
					fmt.Fprintf(os.Stderr, "mdbx-storage-diff: erigon latest account read line %d: %v\n", key.line, latestErr)
					os.Exit(2)
				}
				if latestAcc != nil {
					usedLatest = true
					erigonAcc = latestAcc
					erigonReader = latestReader
					fmt.Fprintf(os.Stderr, "mdbx-storage-diff: history missing account 0x%x at txnum=%d, using latest state\n", key.addr, txNum)
				} else {
					fmt.Fprintf(os.Stderr, "mdbx-storage-diff: history missing account 0x%x at txnum=%d; latest state missing too (head=%d)\n", key.addr, txNum, *headNum)
				}
			}

			erigonNonce := uint64(0)
			erigonBalance := new(uint256.Int)
			erigonCodeHash := gethcommon.Hash{}
			if erigonAcc != nil {
				erigonNonce = erigonAcc.Nonce
				erigonBalance = &erigonAcc.Balance
				erigonCodeHash = gethcommon.BytesToHash(erigonAcc.CodeHash.Bytes())
			}
			erigonExists := erigonAcc != nil
			if gethExists != erigonExists {
				fmt.Printf("account existence mismatch idx=%d line=%d addr=0x%x geth_exists=%t erigon_exists=%t\n",
					i, key.line, key.addr, gethExists, erigonExists)
				os.Exit(1)
			}

			codeHashMatch := codeHashEqual(gethCodeHash, erigonCodeHash)
			if dump, ok := accountsDump[string(key.addr)]; ok {
				if dump.nonce != gethNonce {
					fmt.Printf("account mismatch idx=%d line=%d addr=0x%x geth_nonce=%d dump_nonce=%d\n",
						i, key.line, key.addr, gethNonce, dump.nonce)
					os.Exit(1)
				}
				if dump.balance != gethBalance.String() {
					fmt.Printf("account mismatch idx=%d line=%d addr=0x%x geth_balance=%s dump_balance=%s\n",
						i, key.line, key.addr, gethBalance.String(), dump.balance)
					os.Exit(1)
				}
				if len(dump.codeHash) > 0 && !bytes.Equal(dump.codeHash, erigonCodeHash.Bytes()) {
					fmt.Printf("account codehash mismatch idx=%d line=%d addr=0x%x geth_codehash=0x%x erigon_codehash=%s\n",
						i, key.line, key.addr, dump.codeHash, erigonCodeHash.Hex())
					os.Exit(1)
				}
			}
			if !gethExists {
				if compareStorageRoot {
					if usedLatest {
						fmt.Fprintf(os.Stderr, "mdbx-storage-diff: skipping storage root compare for 0x%x (latest fallback)\n", key.addr)
						continue
					}
					erigonRootComputed, err := computeStorageRootFromDomain(tx, erigonAddr, txNum)
					if err != nil {
						fmt.Fprintf(os.Stderr, "mdbx-storage-diff: compute storage root line %d: %v\n", key.line, err)
						os.Exit(2)
					}
					if erigonRootComputed != gethtypes.EmptyRootHash {
						fmt.Printf("storage root mismatch idx=%d line=%d addr=0x%x geth_root=%s erigon_root=%s\n",
							i, key.line, key.addr, gethcommon.Hash{}.Hex(), erigonRootComputed.Hex())
						os.Exit(1)
					}
				}
				continue
			}
			if gethNonce != erigonNonce || gethBalance.Cmp(erigonBalance) != 0 || !codeHashMatch {
				fmt.Printf("account mismatch idx=%d line=%d addr=0x%x geth_nonce=%d erigon_nonce=%d geth_balance=%s erigon_balance=%s geth_code_hash=%s erigon_code_hash=%s\n",
					i, key.line, key.addr, gethNonce, erigonNonce, gethBalance.String(), erigonBalance.String(), gethCodeHash.Hex(), erigonCodeHash.Hex())
				os.Exit(1)
			}

			erigonCode, err := erigonReader.ReadAccountCode(erigonAddr)
			if err != nil {
				fmt.Fprintf(os.Stderr, "mdbx-storage-diff: erigon code read line %d: %v\n", key.line, err)
				os.Exit(2)
			}
			if !bytes.Equal(gethCode, erigonCode) {
				fmt.Printf("code mismatch idx=%d line=%d addr=0x%x geth_code_len=%d erigon_code_len=%d geth_code_hash=%s erigon_code_hash=%s\n",
					i, key.line, key.addr, len(gethCode), len(erigonCode), gethCodeHash.Hex(), erigonCodeHash.Hex())
				os.Exit(1)
			}

			if compareStorageRoot {
				if usedLatest {
					fmt.Fprintf(os.Stderr, "mdbx-storage-diff: skipping storage root compare for 0x%x (latest fallback)\n", key.addr)
					continue
				}
				gethRoot := gethReader.statedb.GetStorageRoot(gethAddr)
				erigonRootComputed, err := computeStorageRootFromDomain(tx, erigonAddr, txNum)
				if err != nil {
					fmt.Fprintf(os.Stderr, "mdbx-storage-diff: compute storage root line %d: %v\n", key.line, err)
					os.Exit(2)
				}
				if gethRoot != erigonRootComputed {
					fmt.Printf("storage root mismatch idx=%d line=%d addr=0x%x geth_root=%s erigon_root=%s\n",
						i, key.line, key.addr, gethRoot.Hex(), erigonRootComputed.Hex())
					os.Exit(1)
				}
			}
		}
		fmt.Printf("no mismatches across %d account keys\n", len(addrs))
	}

	for i, key := range keys {
		gethAddr := gethcommon.BytesToAddress(key.addr)
		gethSlot := gethcommon.BytesToHash(key.slot)
		gethVal := gethReader.statedb.GetState(gethAddr, gethSlot)

		erigonAddr := ecommon.BytesToAddress(key.addr)
		erigonSlot := ecommon.BytesToHash(key.slot)
		val, ok, err := stateReader.ReadAccountStorage(erigonAddr, erigonSlot)
		if err != nil {
			fmt.Fprintf(os.Stderr, "mdbx-storage-diff: erigon read line %d: %v\n", key.line, err)
			os.Exit(2)
		}
		var erigonVal gethcommon.Hash
		if ok {
			erigonVal = gethcommon.BytesToHash(val.Bytes())
		}

		if gethVal != erigonVal {
			composite := append(erigonAddr.Bytes(), erigonSlot.Bytes()...)
			latestVal, _, latestErr := tx.GetLatest(kv.StorageDomain, composite)
			latestHex := "<nil>"
			if len(latestVal) > 0 {
				latestHex = gethcommon.BytesToHash(latestVal).Hex()
			}
			asOfNext, asOfNextOk, asOfNextErr := tx.GetAsOf(kv.StorageDomain, composite, txNum+1)
			asOfNextHex := "<nil>"
			if asOfNextOk {
				asOfNextHex = gethcommon.BytesToHash(asOfNext).Hex()
			}
			fmt.Printf("mismatch idx=%d line=%d addr=0x%x slot=0x%x geth=%s erigon=%s erigon_ok=%t\n",
				i, key.line, key.addr, key.slot, gethVal.Hex(), erigonVal.Hex(), ok)
			fmt.Printf("dest latest=%s latest_err=%v\n", latestHex, latestErr)
			fmt.Printf("dest asof_txnum_plus_one=%s ok=%t err=%v\n", asOfNextHex, asOfNextOk, asOfNextErr)
			os.Exit(1)
		}
	}

	fmt.Printf("no mismatches across %d keys (skipped %d prefix keys)\n", len(keys), skippedPrefix)
}

func codeHashEqual(a, b gethcommon.Hash) bool {
	if a == b {
		return true
	}
	if a == (gethcommon.Hash{}) && b == gethtypes.EmptyCodeHash {
		return true
	}
	if b == (gethcommon.Hash{}) && a == gethtypes.EmptyCodeHash {
		return true
	}
	return false
}
