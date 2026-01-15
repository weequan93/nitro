//go:build erigon
// +build erigon

package main

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/c2h5oh/datasize"
	ecommon "github.com/erigontech/erigon-lib/common"
	elog "github.com/erigontech/erigon-lib/log/v3"
	"github.com/erigontech/erigon/db/kv"
	"github.com/erigontech/erigon/db/kv/dbcfg"
	emdbx "github.com/erigontech/erigon/db/kv/mdbx"
	"github.com/erigontech/erigon/db/kv/rawdbv3"
	"github.com/erigontech/erigon/db/kv/temporal"
	"github.com/erigontech/erigon/db/rawdb"
	dbstate "github.com/erigontech/erigon/db/state"
	"github.com/erigontech/mdbx-go/mdbx"
	"github.com/ethereum/go-ethereum/common"
	gethrawdb "github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/ethdb"
	"golang.org/x/sync/semaphore"

	"github.com/offchainlabs/nitro/cmd/conf"
)

func verifyFull(opts Options) error {
	if opts.Verify != "basic" && opts.Verify != "extended" && opts.Verify != "strict" {
		return ExitError{Code: ExitVerification, Err: fmt.Errorf("mdbx-migrate: invalid verify mode %q", opts.Verify)}
	}
	logKV("verify", "dataset", "l2chaindata", "mode", opts.Verify, "status", "start")

	srcChain, err := openSourceChainDB(filepath.Join(opts.Source, "l2chaindata"))
	if err != nil {
		return ExitError{Code: ExitVerification, Err: fmt.Errorf("mdbx-migrate: open source l2chaindata: %w", err)}
	}
	defer srcChain.Close()

	dstChain, err := openDestChainDB(filepath.Join(opts.Dest, "l2chaindata"), opts.Mdbx)
	if err != nil {
		return ExitError{Code: ExitVerification, Err: fmt.Errorf("mdbx-migrate: open destination l2chaindata: %w", err)}
	}
	defer dstChain.Close()

	head, err := verifyHeadAndRoot(srcChain, dstChain)
	if err != nil {
		return ExitError{Code: ExitVerification, Err: fmt.Errorf("mdbx-migrate: verify head/root: %w", err)}
	}
	logKV("verify", "dataset", "l2chaindata", "section", "head_root", "head", head)

	if opts.Verify == "extended" || opts.Verify == "strict" {
		dirs, err := buildExecDirs(opts.Dest)
		if err != nil {
			return ExitError{Code: ExitVerification, Err: fmt.Errorf("mdbx-migrate: init verify dirs: %w", err)}
		}
		logger := elog.New("component", "mdbx-migrate")
		agg, err := dbstate.New(dirs).Logger(logger).GenSaltIfNeed(true).Open(context.Background(), dstChain)
		if err != nil {
			return ExitError{Code: ExitVerification, Err: fmt.Errorf("mdbx-migrate: open state aggregator: %w", err)}
		}
		if err := agg.OpenFolder(); err != nil {
			agg.Close()
			return ExitError{Code: ExitVerification, Err: fmt.Errorf("mdbx-migrate: open state snapshots: %w", err)}
		}
		temporalDB, err := temporal.New(dstChain, agg)
		if err != nil {
			agg.Close()
			return ExitError{Code: ExitVerification, Err: fmt.Errorf("mdbx-migrate: open temporal db: %w", err)}
		}
		err = verifyArbosSlots(srcChain, temporalDB, head, opts.StartBlock, opts.VerifySamples, opts.Verify == "strict")
		agg.Close()
		if err != nil {
			return ExitError{Code: ExitVerification, Err: fmt.Errorf("mdbx-migrate: verify arbos slots: %w", err)}
		}
	}

	if err := verifyState(opts); err != nil {
		return err
	}

	return nil
}

func verifyHeadAndRoot(src ethdb.Database, dst kv.RoDB) (uint64, error) {
	srcHeadHash := gethrawdb.ReadHeadBlockHash(src)
	if srcHeadHash == (common.Hash{}) {
		return 0, fmt.Errorf("source head hash missing")
	}
	srcHeadNum := gethrawdb.ReadHeaderNumber(src, srcHeadHash)
	if srcHeadNum == nil {
		return 0, fmt.Errorf("source head number missing")
	}
	srcHeader := gethrawdb.ReadHeader(src, srcHeadHash, *srcHeadNum)
	if srcHeader == nil {
		return 0, fmt.Errorf("source head header missing")
	}

	tx, err := dst.BeginRo(context.Background())
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	dstHeadHash := rawdb.ReadHeadBlockHash(tx)
	if dstHeadHash == (ecommon.Hash{}) {
		return 0, fmt.Errorf("destination head hash missing")
	}
	dstHeadNum := rawdb.ReadHeaderNumber(tx, dstHeadHash)
	if dstHeadNum == nil {
		return 0, fmt.Errorf("destination head number missing")
	}
	dstHeader := rawdb.ReadHeader(tx, dstHeadHash, *dstHeadNum)
	if dstHeader == nil {
		return 0, fmt.Errorf("destination head header missing")
	}

	if *srcHeadNum != *dstHeadNum {
		return 0, fmt.Errorf("head number mismatch source=%d dest=%d", *srcHeadNum, *dstHeadNum)
	}
	if !bytesEqual(srcHeadHash.Bytes(), dstHeadHash.Bytes()) {
		return 0, fmt.Errorf("head hash mismatch source=%x dest=%x", srcHeadHash, dstHeadHash)
	}
	if !bytesEqual(srcHeader.Root.Bytes(), dstHeader.Root.Bytes()) {
		return 0, fmt.Errorf("state root mismatch source=%x dest=%x", srcHeader.Root, dstHeader.Root)
	}

	return *srcHeadNum, nil
}

func verifyArbosSlots(src ethdb.Database, dst kv.RoDB, head uint64, startBlock uint64, samples int, strict bool) error {
	if startBlock > head {
		return fmt.Errorf("start block %d exceeds head %d", startBlock, head)
	}
	srcReader := &gethSlotReader{db: src}

	tx, err := dst.BeginRo(context.Background())
	if err != nil {
		return err
	}
	defer tx.Rollback()

	ttx, ok := tx.(kv.TemporalTx)
	if !ok {
		return fmt.Errorf("destination tx is not temporal")
	}
	dstReader := &erigonSlotReader{
		tx:          ttx,
		txNumReader: rawdbv3.TxNums,
	}

	refs := []slotRef{
		{Name: "arbosVersion", Offset: 0},
		{Name: "networkFeeAccount", Offset: 3},
		{Name: "chainId", Offset: 4},
		{Name: "genesisBlockNum", Offset: 5},
		{Name: "infraFeeAccount", Offset: 6},
		{Name: "l1PricePerUnit", Subspace: []byte{0}, Offset: 7},
		{Name: "l1PerUnitReward", Subspace: []byte{0}, Offset: 3},
		{Name: "l1LastUpdateTime", Subspace: []byte{0}, Offset: 4},
		{Name: "l1FeesAvailable", Subspace: []byte{0}, Offset: 11},
		{Name: "l2BaseFee", Subspace: []byte{1}, Offset: 2},
		{Name: "l2MinBaseFee", Subspace: []byte{1}, Offset: 3},
		{Name: "l2GasBacklog", Subspace: []byte{1}, Offset: 4},
		{Name: "l2PricingInertia", Subspace: []byte{1}, Offset: 5},
	}

	blockSamples := sampleBlocksRange(startBlock, head, samples)
	logKV("verify",
		"dataset", "l2chaindata",
		"section", "arbos_slots",
		"samples", len(blockSamples),
		"start_block", startBlock,
		"end_block", head,
	)
	for _, blockNum := range blockSamples {
		left, err := readOffsets(srcReader, blockNum, refs)
		if err != nil {
			return fmt.Errorf("source read block %d: %w", blockNum, err)
		}
		right, err := readOffsets(dstReader, blockNum, refs)
		if err != nil {
			return fmt.Errorf("dest read block %d: %w", blockNum, err)
		}
		leftVersion := left["arbosVersion"].Big().Uint64()
		rightVersion := right["arbosVersion"].Big().Uint64()
		if leftVersion != rightVersion {
			return fmt.Errorf("arbos version mismatch at block %d: source=%d dest=%d", blockNum, leftVersion, rightVersion)
		}
		if leftVersion < 10 && !strict {
			delete(left, "l1FeesAvailable")
			delete(right, "l1FeesAvailable")
		}
		if err := compareSlotMaps(blockNum, left, right); err != nil {
			return err
		}
	}

	srcCfg, err := readBytes(srcReader, head, []byte{7})
	if err != nil {
		return fmt.Errorf("source chain config bytes: %w", err)
	}
	dstCfg, err := readBytes(dstReader, head, []byte{7})
	if err != nil {
		return fmt.Errorf("dest chain config bytes: %w", err)
	}
	if !bytesEqual(srcCfg, dstCfg) {
		return fmt.Errorf("chain config bytes mismatch")
	}
	logKV("verify", "dataset", "l2chaindata", "section", "chain_config", "status", "ok")

	return nil
}

func compareSlotMaps(blockNum uint64, left, right map[string]common.Hash) error {
	if len(left) != len(right) {
		return fmt.Errorf("slot map size mismatch at block %d", blockNum)
	}
	for name, lval := range left {
		rval, ok := right[name]
		if !ok {
			return fmt.Errorf("missing slot %q at block %d", name, blockNum)
		}
		if lval != rval {
			return fmt.Errorf("slot mismatch %q at block %d: source=%x dest=%x", name, blockNum, lval, rval)
		}
	}
	return nil
}

func sampleBlocksRange(start, end uint64, count int) []uint64 {
	if count <= 0 {
		return nil
	}
	if end <= start || count == 1 {
		return []uint64{end}
	}
	seen := make(map[uint64]struct{}, count)
	samples := make([]uint64, 0, count)
	for i := 0; i < count; i++ {
		n := start + (end-start)*uint64(i)/uint64(count-1)
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		samples = append(samples, n)
	}
	return samples
}

func openDestChainDB(path string, cfg conf.MdbxConfig) (kv.RwDB, error) {
	logger := elog.New("component", "mdbx-migrate")
	opts := emdbx.New(dbcfg.ChainDB, logger).Path(path)
	opts = applyMdbxConfig(opts, cfg)
	return opts.Open(context.Background())
}

func applyMdbxConfig(opts emdbx.MdbxOpts, cfg conf.MdbxConfig) emdbx.MdbxOpts {
	if cfg.PageSize > 0 {
		opts = opts.PageSize(datasize.ByteSize(cfg.PageSize))
	}
	if cfg.MapSize > 0 {
		opts = opts.MapSize(datasize.ByteSize(cfg.MapSize))
	}
	if cfg.GrowthStep > 0 {
		opts = opts.GrowthStep(datasize.ByteSize(cfg.GrowthStep))
	}
	if cfg.WriteMap {
		opts = opts.WriteMap(true)
	}
	if cfg.NoSync {
		opts = opts.Flags(func(f uint) uint { return f&^mdbx.Durable | mdbx.SafeNoSync })
	}
	if cfg.MaxReaders > 0 {
		opts = opts.RoTxsLimiter(semaphore.NewWeighted(int64(cfg.MaxReaders)))
	}
	return opts
}
