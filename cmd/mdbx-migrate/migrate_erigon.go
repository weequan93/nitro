//go:build erigon
// +build erigon

package main

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/ethereum/go-ethereum/ethdb"

	"github.com/offchainlabs/nitro/cmd/conf"
	"github.com/offchainlabs/nitro/execution/erigonexec"
	"github.com/offchainlabs/nitro/util/dbutil"
)

func migrate(opts Options) error {
	if opts.Mode == "full" {
		return migrateFull(opts)
	}

	srcArb, err := openSourceChainDB(filepath.Join(opts.Source, "arbitrumdata"))
	if err != nil {
		return ExitError{Code: ExitMigration, Err: fmt.Errorf("mdbx-migrate: open source arbitrumdata: %w", err)}
	}
	defer srcArb.Close()

	srcWasm, err := openSourceChainDB(filepath.Join(opts.Source, "wasm"))
	if err != nil {
		return ExitError{Code: ExitMigration, Err: fmt.Errorf("mdbx-migrate: open source wasm: %w", err)}
	}
	defer srcWasm.Close()

	dstArb, err := erigonexec.OpenArbDB(opts.Dest, mdbxOptionsFromConfig(opts.Mdbx))
	if err != nil {
		return ExitError{Code: ExitMigration, Err: fmt.Errorf("mdbx-migrate: open destination arbitrumdata: %w", err)}
	}
	defer dstArb.Close()
	if err := dbutil.PutUnfinishedConversionCanaryFile(filepath.Join(opts.Dest, "arbitrumdata")); err != nil {
		return ExitError{Code: ExitMigration, Err: fmt.Errorf("mdbx-migrate: write arbitrumdata canary: %w", err)}
	}

	dstWasm, err := erigonexec.OpenWasmDB(opts.Dest, mdbxOptionsFromConfig(opts.Mdbx))
	if err != nil {
		return ExitError{Code: ExitMigration, Err: fmt.Errorf("mdbx-migrate: open destination wasm: %w", err)}
	}
	defer dstWasm.Close()

	logKV("copy", "dataset", "arbitrumdata", "status", "start")
	arbStats, err := copyDatabase(context.Background(), srcArb, dstArb)
	if err != nil {
		return ExitError{Code: ExitMigration, Err: fmt.Errorf("mdbx-migrate: copy arbitrumdata: %w", err)}
	}
	if err := dbutil.DeleteUnfinishedConversionCanaryFile(filepath.Join(opts.Dest, "arbitrumdata")); err != nil {
		return ExitError{Code: ExitMigration, Err: fmt.Errorf("mdbx-migrate: clear arbitrumdata canary: %w", err)}
	}
	logKV("copy", "dataset", "arbitrumdata", "keys", arbStats.Keys, "bytes", arbStats.Bytes)
	logKV("finalize", "dataset", "arbitrumdata", "canary", "removed")

	if err := dbutil.PutUnfinishedConversionCanaryFile(filepath.Join(opts.Dest, "wasm")); err != nil {
		return ExitError{Code: ExitMigration, Err: fmt.Errorf("mdbx-migrate: write wasm canary: %w", err)}
	}
	logKV("copy", "dataset", "wasm", "status", "start")
	wasmStats, err := copyDatabase(context.Background(), srcWasm, dstWasm)
	if err != nil {
		return ExitError{Code: ExitMigration, Err: fmt.Errorf("mdbx-migrate: copy wasm: %w", err)}
	}
	if err := dbutil.DeleteUnfinishedConversionCanaryFile(filepath.Join(opts.Dest, "wasm")); err != nil {
		return ExitError{Code: ExitMigration, Err: fmt.Errorf("mdbx-migrate: clear wasm canary: %w", err)}
	}
	logKV("copy", "dataset", "wasm", "keys", wasmStats.Keys, "bytes", wasmStats.Bytes)
	logKV("finalize", "dataset", "wasm", "canary", "removed")

	return nil
}

func verify(opts Options) error {
	if opts.Verify == "none" {
		return nil
	}
	switch opts.Mode {
	case "state":
		return verifyState(opts)
	case "full":
		return verifyFull(opts)
	default:
		return ExitError{Code: ExitVerification, Err: fmt.Errorf("mdbx-migrate: unknown verify mode %q", opts.Mode)}
	}
}

func verifyState(opts Options) error {
	srcArb, err := openSourceChainDB(filepath.Join(opts.Source, "arbitrumdata"))
	if err != nil {
		return ExitError{Code: ExitVerification, Err: fmt.Errorf("mdbx-migrate: open source arbitrumdata: %w", err)}
	}
	defer srcArb.Close()

	srcWasm, err := openSourceChainDB(filepath.Join(opts.Source, "wasm"))
	if err != nil {
		return ExitError{Code: ExitVerification, Err: fmt.Errorf("mdbx-migrate: open source wasm: %w", err)}
	}
	defer srcWasm.Close()

	dstArb, err := erigonexec.OpenArbDB(opts.Dest, mdbxOptionsFromConfig(opts.Mdbx))
	if err != nil {
		return ExitError{Code: ExitVerification, Err: fmt.Errorf("mdbx-migrate: open destination arbitrumdata: %w", err)}
	}
	defer dstArb.Close()

	dstWasm, err := erigonexec.OpenWasmDB(opts.Dest, mdbxOptionsFromConfig(opts.Mdbx))
	if err != nil {
		return ExitError{Code: ExitVerification, Err: fmt.Errorf("mdbx-migrate: open destination wasm: %w", err)}
	}
	defer dstWasm.Close()

	logKV("verify", "dataset", "arbitrumdata", "mode", opts.Verify, "status", "start")
	arbStats, err := verifyDatabase(context.Background(), srcArb, dstArb, opts.Verify, "arbitrumdata")
	if err != nil {
		return ExitError{Code: ExitVerification, Err: fmt.Errorf("mdbx-migrate: verify arbitrumdata: %w", err)}
	}
	logKV("verify", "dataset", "arbitrumdata", "mode", opts.Verify, "keys", arbStats.Keys, "bytes", arbStats.Bytes)
	if err := verifyArbPrefixStats(context.Background(), srcArb, dstArb, opts.Verify); err != nil {
		return ExitError{Code: ExitVerification, Err: fmt.Errorf("mdbx-migrate: verify arbitrumdata prefixes: %w", err)}
	}

	logKV("verify", "dataset", "wasm", "mode", opts.Verify, "status", "start")
	wasmStats, err := verifyDatabase(context.Background(), srcWasm, dstWasm, opts.Verify, "wasm")
	if err != nil {
		return ExitError{Code: ExitVerification, Err: fmt.Errorf("mdbx-migrate: verify wasm: %w", err)}
	}
	logKV("verify", "dataset", "wasm", "mode", opts.Verify, "keys", wasmStats.Keys, "bytes", wasmStats.Bytes)

	return nil
}

type dbStats struct {
	Keys  uint64
	Bytes uint64
}

func verifyArbPrefixStats(ctx context.Context, src ethdb.Database, dst ethdb.Database, mode string) error {
	type prefixEntry struct {
		prefix  byte
		section string
	}
	entries := []prefixEntry{
		{prefix: 'm', section: "messages"},
		{prefix: 'r', section: "message_results"},
		{prefix: 'b', section: "block_hash_feed"},
		{prefix: 't', section: "block_metadata"},
		{prefix: 'x', section: "missing_block_metadata"},
		{prefix: 'd', section: "delayed_messages_legacy"},
		{prefix: 'e', section: "delayed_messages_rlp"},
		{prefix: 'p', section: "parent_chain_blocks"},
		{prefix: 's', section: "sequencer_batches"},
		{prefix: 'a', section: "delayed_sequenced"},
		{prefix: '_', section: "counters"},
	}
	var (
		tSrc, tDst dbStats
		xSrc, xDst dbStats
		gotT       bool
		gotX       bool
	)
	for _, entry := range entries {
		srcStats, dstStats, err := verifyPrefixStats(ctx, src, dst, []byte{entry.prefix}, mode, entry.section)
		if err != nil {
			return err
		}
		switch entry.prefix {
		case 't':
			tSrc, tDst = srcStats, dstStats
			gotT = true
		case 'x':
			xSrc, xDst = srcStats, dstStats
			gotX = true
		}
	}
	if gotT && gotX {
		status := "ok"
		if tSrc.Keys != tDst.Keys || tSrc.Bytes != tDst.Bytes || xSrc.Keys != xDst.Keys || xSrc.Bytes != xDst.Bytes {
			status = "mismatch"
		}
		logKV(
			"verify",
			"dataset", "arbitrumdata",
			"section", "metadata_summary",
			"t_keys_src", tSrc.Keys,
			"t_keys_dst", tDst.Keys,
			"t_bytes_src", tSrc.Bytes,
			"t_bytes_dst", tDst.Bytes,
			"x_keys_src", xSrc.Keys,
			"x_keys_dst", xDst.Keys,
			"x_bytes_src", xSrc.Bytes,
			"x_bytes_dst", xDst.Bytes,
			"status", status,
		)
	}
	return nil
}

func verifyPrefixStats(ctx context.Context, src ethdb.Database, dst ethdb.Database, prefix []byte, mode string, section string) (dbStats, dbStats, error) {
	srcStats, err := countPrefixStats(ctx, src, prefix)
	if err != nil {
		return dbStats{}, dbStats{}, fmt.Errorf("source %s prefix stats: %w", section, err)
	}
	dstStats, err := countPrefixStats(ctx, dst, prefix)
	if err != nil {
		return dbStats{}, dbStats{}, fmt.Errorf("dest %s prefix stats: %w", section, err)
	}
	status := "ok"
	if srcStats.Keys != dstStats.Keys || srcStats.Bytes != dstStats.Bytes {
		if mode == "basic" {
			status = "mismatch"
		} else {
			return dbStats{}, dbStats{}, fmt.Errorf("%s prefix mismatch src_keys=%d dst_keys=%d src_bytes=%d dst_bytes=%d", section, srcStats.Keys, dstStats.Keys, srcStats.Bytes, dstStats.Bytes)
		}
	}
	logKV(
		"verify",
		"dataset", "arbitrumdata",
		"section", section,
		"keys_src", srcStats.Keys,
		"keys_dst", dstStats.Keys,
		"bytes_src", srcStats.Bytes,
		"bytes_dst", dstStats.Bytes,
		"status", status,
	)
	return srcStats, dstStats, nil
}

func countPrefixStats(ctx context.Context, db ethdb.Database, prefix []byte) (dbStats, error) {
	var stats dbStats
	it := db.NewIterator(prefix, nil)
	defer it.Release()
	for it.Next() && ctx.Err() == nil {
		stats.Keys++
		stats.Bytes += uint64(len(it.Key()) + len(it.Value()))
	}
	if err := it.Error(); err != nil {
		return stats, err
	}
	return stats, ctx.Err()
}

func countDatabaseStats(ctx context.Context, db ethdb.Database) (dbStats, error) {
	var stats dbStats
	it := db.NewIterator(nil, nil)
	defer it.Release()
	for it.Next() && ctx.Err() == nil {
		stats.Keys++
		stats.Bytes += uint64(len(it.Key()) + len(it.Value()))
	}
	if err := it.Error(); err != nil {
		return stats, err
	}
	return stats, ctx.Err()
}

func copyDatabase(ctx context.Context, src ethdb.Database, dst ethdb.Database) (dbStats, error) {
	var stats dbStats
	if err := dbutil.PutUnfinishedConversionCanary(dst); err != nil {
		return stats, fmt.Errorf("write conversion canary: %w", err)
	}
	success := false
	defer func() {
		if !success {
			_ = dbutil.DeleteUnfinishedConversionCanary(dst)
		}
	}()

	it := src.NewIterator(nil, nil)
	defer it.Release()
	batch := dst.NewBatch()
	for it.Next() && ctx.Err() == nil {
		if err := batch.Put(it.Key(), it.Value()); err != nil {
			return stats, err
		}
		if batch.ValueSize() >= ethdb.IdealBatchSize {
			if err := batch.Write(); err != nil {
				return stats, err
			}
			batch.Reset()
		}
		stats.Keys++
		stats.Bytes += uint64(len(it.Key()) + len(it.Value()))
	}
	if err := it.Error(); err != nil {
		return stats, err
	}
	if err := ctx.Err(); err != nil {
		return stats, err
	}
	if batch.ValueSize() > 0 {
		if err := batch.Write(); err != nil {
			return stats, err
		}
	}
	if err := dbutil.DeleteUnfinishedConversionCanary(dst); err != nil {
		return stats, err
	}
	success = true
	return stats, nil
}

func verifyDatabase(ctx context.Context, src ethdb.Database, dst ethdb.Database, mode string, dataset string) (dbStats, error) {
	var stats dbStats
	start := time.Now()
	it := src.NewIterator(nil, nil)
	defer it.Release()
	var dstStats dbStats
	for it.Next() && ctx.Err() == nil {
		stats.Keys++
		stats.Bytes += uint64(len(it.Key()) + len(it.Value()))
		switch mode {
		case "basic":
			has, err := dst.Has(it.Key())
			if err != nil {
				return stats, err
			}
			if !has {
				return stats, fmt.Errorf("missing key %x", it.Key())
			}
		case "extended", "strict":
			value, err := dst.Get(it.Key())
			if err != nil {
				return stats, err
			}
			if !bytesEqual(value, it.Value()) {
				return stats, fmt.Errorf("value mismatch for key %x", it.Key())
			}
		default:
			return stats, fmt.Errorf("invalid verify mode %q", mode)
		}
	}
	if err := it.Error(); err != nil {
		return stats, err
	}
	if err := ctx.Err(); err != nil {
		return stats, err
	}
	if mode == "extended" || mode == "strict" {
		var err error
		dstStats, err = countDatabaseStats(ctx, dst)
		if err != nil {
			return stats, err
		}
		if stats.Keys != dstStats.Keys || stats.Bytes != dstStats.Bytes {
			return stats, fmt.Errorf("destination has extra keys: src_keys=%d dst_keys=%d src_bytes=%d dst_bytes=%d", stats.Keys, dstStats.Keys, stats.Bytes, dstStats.Bytes)
		}
	}
	if mode == "extended" || mode == "strict" {
		logKV(
			"verify",
			"dataset", dataset,
			"section", "db_summary",
			"keys_src", stats.Keys,
			"keys_dst", dstStats.Keys,
			"bytes_src", stats.Bytes,
			"bytes_dst", dstStats.Bytes,
			"elapsed_ms", time.Since(start).Milliseconds(),
			"status", "ok",
		)
	}
	return stats, ctx.Err()
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func mdbxOptionsFromConfig(cfg conf.MdbxConfig) erigonexec.MdbxOptions {
	return erigonexec.MdbxOptions{
		PageSize:   cfg.PageSize,
		MapSize:    cfg.MapSize,
		GrowthStep: cfg.GrowthStep,
		WriteMap:   cfg.WriteMap,
		NoSync:     cfg.NoSync,
		MaxReaders: cfg.MaxReaders,
	}
}
