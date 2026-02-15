//go:build erigon
// +build erigon

package main

import (
	"bytes"
	"context"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	elog "github.com/erigontech/erigon-lib/log/v3"
	"github.com/erigontech/erigon/db/datadir"
	"github.com/erigontech/erigon/db/kv"
	"github.com/erigontech/erigon/db/kv/dbcfg"
	emdbx "github.com/erigontech/erigon/db/kv/mdbx"
	"github.com/erigontech/erigon/db/kv/rawdbv3"
	"github.com/erigontech/erigon/db/kv/temporal"
	erawdb "github.com/erigontech/erigon/db/rawdb"
	dbstate "github.com/erigontech/erigon/db/state"
	etypes "github.com/erigontech/erigon/execution/types"
	gethcommon "github.com/ethereum/go-ethereum/common"
	gethrawdb "github.com/ethereum/go-ethereum/core/rawdb"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethdb"
	gethtrie "github.com/ethereum/go-ethereum/trie"
)

func openSourceChainDB(path string) (ethdb.Database, error) {
	switch gethrawdb.PreexistingDatabase(path) {
	case "pebble":
		return gethrawdb.NewPebbleDBDatabase(path, 0, 0, "receipt-diff", true, true, nil)
	case "leveldb":
		return gethrawdb.NewLevelDBDatabase(path, 0, 0, "receipt-diff", true)
	default:
		return nil, fmt.Errorf("no supported database found at %s", path)
	}
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

func openDestChainDB(path string) (kv.RwDB, error) {
	logger := elog.New("component", "tmp-diff-receipts-block")
	return emdbx.New(dbcfg.ChainDB, logger).Path(path).Open(context.Background())
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
	for _, p := range paths {
		if err := os.MkdirAll(p, 0o755); err != nil {
			return datadir.Dirs{}, err
		}
	}
	return dirs, nil
}

func eqBig(a, b *big.Int) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.Cmp(b) == 0
}

func bigStr(v *big.Int) string {
	if v == nil {
		return "<nil>"
	}
	return v.String()
}

func compareLogs(src []*gethtypes.Log, dst etypes.Logs) []string {
	issues := make([]string, 0)
	if len(src) != len(dst) {
		issues = append(issues, fmt.Sprintf("logs_len src=%d dst=%d", len(src), len(dst)))
		return issues
	}
	for i := 0; i < len(src); i++ {
		s := src[i]
		d := dst[i]
		if s == nil || d == nil {
			if s != nil || d != nil {
				issues = append(issues, fmt.Sprintf("log[%d] nil mismatch", i))
			}
			continue
		}
		if s.Address != gethcommon.Address(d.Address) {
			issues = append(issues, fmt.Sprintf("log[%d].address src=%s dst=%s", i, s.Address.Hex(), d.Address.Hex()))
		}
		if len(s.Topics) != len(d.Topics) {
			issues = append(issues, fmt.Sprintf("log[%d].topics_len src=%d dst=%d", i, len(s.Topics), len(d.Topics)))
			continue
		}
		for t := 0; t < len(s.Topics); t++ {
			if s.Topics[t] != gethcommon.Hash(d.Topics[t]) {
				issues = append(issues, fmt.Sprintf("log[%d].topic[%d] src=%s dst=%s", i, t, s.Topics[t].Hex(), d.Topics[t].Hex()))
			}
		}
		if !bytes.Equal(s.Data, d.Data) {
			issues = append(issues, fmt.Sprintf("log[%d].data src=0x%x dst=0x%x", i, s.Data, d.Data))
		}
	}
	return issues
}

func main() {
	source := os.Getenv("SOURCE")
	if source == "" {
		fmt.Println("SOURCE env required (path to source l2chaindata)")
		os.Exit(2)
	}
	dest := os.Getenv("DEST")
	if dest == "" {
		fmt.Println("DEST env required (path to migrated chain root or l2chaindata)")
		os.Exit(2)
	}
	blockStr := os.Getenv("BLOCK")
	if blockStr == "" {
		blockStr = "33"
	}
	blockNum, err := strconv.ParseUint(blockStr, 10, 64)
	if err != nil {
		fmt.Printf("invalid BLOCK: %v\n", err)
		os.Exit(2)
	}

	srcDB, err := openSourceChainDB(source)
	if err != nil {
		fmt.Printf("open source db: %v\n", err)
		os.Exit(1)
	}
	defer srcDB.Close()

	srcHash := gethrawdb.ReadCanonicalHash(srcDB, blockNum)
	srcHeader := gethrawdb.ReadHeader(srcDB, srcHash, blockNum)
	srcBody := gethrawdb.ReadBody(srcDB, srcHash, blockNum)
	genesisHash := gethrawdb.ReadCanonicalHash(srcDB, 0)
	srcCfg := gethrawdb.ReadChainConfig(srcDB, genesisHash)
	srcReceipts := gethrawdb.ReadReceipts(srcDB, srcHash, blockNum, srcHeader.Time, srcCfg)
	srcRoot := gethtypes.DeriveSha(gethtypes.Receipts(srcReceipts), gethtrie.NewStackTrie(nil))
	srcRootNoInternal := gethcommon.Hash{}
	if len(srcReceipts) > 1 {
		srcRootNoInternal = gethtypes.DeriveSha(gethtypes.Receipts(srcReceipts[1:]), gethtrie.NewStackTrie(nil))
	}
	srcConverted := make(etypes.Receipts, 0, len(srcReceipts))
	srcRootErigon := ""
	srcRootErigonErr := ""
	for i := range srcReceipts {
		rb, err := srcReceipts[i].MarshalBinary()
		if err != nil {
			srcRootErigonErr = fmt.Sprintf("marshal source receipt[%d]: %v", i, err)
			break
		}
		var er etypes.Receipt
		if err := er.UnmarshalBinary(rb); err != nil {
			srcRootErigonErr = fmt.Sprintf("decode source receipt[%d] as erigon: %v", i, err)
			break
		}
		srcConverted = append(srcConverted, &er)
	}
	if srcRootErigonErr == "" {
		srcRootErigon = gethcommon.Hash(etypes.DeriveSha(srcConverted)).Hex()
	} else {
		srcRootErigon = "<n/a>"
	}

	destRoot, destChainPath := resolveDestRoot(dest)
	db, err := openDestChainDB(destChainPath)
	if err != nil {
		fmt.Printf("open dest db: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	dirs, err := buildExecDirs(destRoot)
	if err != nil {
		fmt.Printf("build dirs: %v\n", err)
		os.Exit(1)
	}
	agg, err := dbstate.New(dirs).Logger(elog.New("component", "tmp-diff-receipts-block")).GenSaltIfNeed(true).Open(context.Background(), db)
	if err != nil {
		fmt.Printf("open aggregator: %v\n", err)
		os.Exit(1)
	}
	if err := agg.OpenFolder(); err != nil {
		agg.Close()
		fmt.Printf("open snapshots: %v\n", err)
		os.Exit(1)
	}
	defer agg.Close()

	execDB, err := temporal.New(db, agg)
	if err != nil {
		fmt.Printf("open temporal db: %v\n", err)
		os.Exit(1)
	}

	var (
		dstHash       gethcommon.Hash
		dstHeader     *etypes.Header
		dstBlock      *etypes.Block
		dstReceipts   etypes.Receipts
		dstRootErigon gethcommon.Hash
		dstRootGeth   gethcommon.Hash
	)
	if err := execDB.ViewTemporal(context.Background(), func(tx kv.TemporalTx) error {
		h, err := erawdb.ReadCanonicalHash(tx, blockNum)
		if err != nil {
			return err
		}
		dstHash = gethcommon.Hash(h)
		dstHeader = erawdb.ReadHeader(tx, h, blockNum)
		dstBlock = erawdb.ReadBlock(tx, h, blockNum)
		if dstBlock == nil {
			return fmt.Errorf("dest block %d not found", blockNum)
		}
		dstReceipts, err = erawdb.ReadReceiptsCacheV2(tx, dstBlock, rawdbv3.TxNums)
		if err != nil {
			return err
		}
		dstRootErigon = gethcommon.Hash(etypes.DeriveSha(dstReceipts))

		converted := make(gethtypes.Receipts, 0, len(dstReceipts))
		for i := range dstReceipts {
			rb, err := dstReceipts[i].MarshalBinary()
			if err != nil {
				return fmt.Errorf("marshal dest receipt[%d]: %w", i, err)
			}
			var gr gethtypes.Receipt
			if err := gr.UnmarshalBinary(rb); err != nil {
				return fmt.Errorf("decode dest receipt[%d] as geth: %w", i, err)
			}
			converted = append(converted, &gr)
		}
		dstRootGeth = gethtypes.DeriveSha(converted, gethtrie.NewStackTrie(nil))
		return nil
	}); err != nil {
		fmt.Printf("read dest temporal: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("block=%d\n", blockNum)
	fmt.Printf("source: hash=%s state_root=%s tx_root=%s receipt_root_header=%s receipt_root_geth=%s receipt_root_no_internal=%s receipt_root_erigon=%s txs=%d receipts=%d\n",
		srcHash.Hex(), srcHeader.Root.Hex(), srcHeader.TxHash.Hex(), srcHeader.ReceiptHash.Hex(), srcRoot.Hex(), srcRootNoInternal.Hex(), srcRootErigon, len(srcBody.Transactions), len(srcReceipts))
	if srcRootErigonErr != "" {
		fmt.Printf("source: erigon decode issue: %s\n", srcRootErigonErr)
	}
	fmt.Printf("dest:   hash=%s state_root=%s tx_root=%s receipt_root_header=%s receipt_root_erigon=%s receipt_root_geth=%s txs=%d receipts=%d\n",
		dstHash.Hex(), gethcommon.Hash(dstHeader.Root).Hex(), gethcommon.Hash(dstHeader.TxHash).Hex(), gethcommon.Hash(dstHeader.ReceiptHash).Hex(), dstRootErigon.Hex(), dstRootGeth.Hex(), len(dstBlock.Transactions()), len(dstReceipts))

	minLen := len(srcReceipts)
	if len(dstReceipts) < minLen {
		minLen = len(dstReceipts)
	}
	mismatchCount := 0
	for i := 0; i < minLen; i++ {
		sr := srcReceipts[i]
		dr := dstReceipts[i]
		sb, err := sr.MarshalBinary()
		if err != nil {
			fmt.Printf("src receipt[%d] marshal error: %v\n", i, err)
			continue
		}
		db, err := dr.MarshalBinary()
		if err != nil {
			fmt.Printf("dest receipt[%d] marshal error: %v\n", i, err)
			continue
		}
		if bytes.Equal(sb, db) {
			continue
		}
		mismatchCount++
		fmt.Printf("receipt[%d] mismatch\n", i)
		fmt.Printf("  src tx_hash=%s type=%d status=%d cum_gas=%d gas_used=%d eff_price=%s bloom=%x logs=%d\n",
			sr.TxHash.Hex(), sr.Type, sr.Status, sr.CumulativeGasUsed, sr.GasUsed, bigStr(sr.EffectiveGasPrice), sr.Bloom, len(sr.Logs))
		fmt.Printf("  dst tx_hash=%s type=%d status=%d cum_gas=%d gas_used=%d eff_price=%s bloom=%x logs=%d gas_used_for_l1=%d\n",
			dr.TxHash.Hex(), dr.Type, dr.Status, dr.CumulativeGasUsed, dr.GasUsed, bigStr(dr.EffectiveGasPrice), dr.Bloom, len(dr.Logs), dr.GasUsedForL1)
		if sr.Type != dr.Type {
			fmt.Printf("    field mismatch: type src=%d dst=%d\n", sr.Type, dr.Type)
		}
		if sr.Status != dr.Status {
			fmt.Printf("    field mismatch: status src=%d dst=%d\n", sr.Status, dr.Status)
		}
		if sr.CumulativeGasUsed != dr.CumulativeGasUsed {
			fmt.Printf("    field mismatch: cumulativeGasUsed src=%d dst=%d\n", sr.CumulativeGasUsed, dr.CumulativeGasUsed)
		}
		if sr.GasUsed != dr.GasUsed {
			fmt.Printf("    field mismatch: gasUsed src=%d dst=%d\n", sr.GasUsed, dr.GasUsed)
		}
		if !eqBig(sr.EffectiveGasPrice, dr.EffectiveGasPrice) {
			fmt.Printf("    field mismatch: effectiveGasPrice src=%s dst=%s\n", bigStr(sr.EffectiveGasPrice), bigStr(dr.EffectiveGasPrice))
		}
		if sr.Bloom != gethtypes.Bloom(dr.Bloom) {
			fmt.Printf("    field mismatch: bloom differs\n")
		}
		logDiffs := compareLogs(sr.Logs, dr.Logs)
		for _, d := range logDiffs {
			fmt.Printf("    field mismatch: %s\n", d)
		}
		if !bytes.Equal(sb, db) {
			fmt.Printf("    consensus_rlp src=%s\n", strings.ToLower(fmt.Sprintf("0x%x", sb)))
			fmt.Printf("    consensus_rlp dst=%s\n", strings.ToLower(fmt.Sprintf("0x%x", db)))
		}
	}
	if len(srcReceipts) != len(dstReceipts) {
		fmt.Printf("receipt count mismatch src=%d dst=%d\n", len(srcReceipts), len(dstReceipts))
	}
	if mismatchCount == 0 && len(srcReceipts) == len(dstReceipts) {
		fmt.Println("receipt payloads match for all indices")
	}
}
