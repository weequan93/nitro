//go:build erigon
// +build erigon

package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	elog "github.com/erigontech/erigon-lib/log/v3"

	"github.com/offchainlabs/nitro/arbos/arbostypes"
	"github.com/offchainlabs/nitro/execution/erigonexec"
	"github.com/offchainlabs/nitro/util/dbutil"
)

func migrateFull(opts Options) error {
	info, err := readSourceChainInfo(filepath.Join(opts.Source, "l2chaindata"))
	if err != nil {
		return ExitError{Code: ExitMigration, Err: fmt.Errorf("mdbx-migrate: preflight: %w", err)}
	}

	if !opts.Resume {
		exists, err := checkpointExists(opts.Dest)
		if err != nil {
			return ExitError{Code: ExitMigration, Err: fmt.Errorf("mdbx-migrate: checkpoint stat: %w", err)}
		}
		if exists {
			return ExitError{Code: ExitMigration, Err: errors.New("mdbx-migrate: checkpoint exists; use --resume or remove the checkpoint to start fresh")}
		}
	}
	var ckpt *checkpoint
	if opts.Resume {
		ckpt, err := loadCheckpoint(opts.Dest)
		if err != nil {
			return ExitError{Code: ExitMigration, Err: fmt.Errorf("mdbx-migrate: resume checkpoint: %w", err)}
		}
		if err := validateCheckpoint(opts, info, ckpt); err != nil {
			return ExitError{Code: ExitMigration, Err: fmt.Errorf("mdbx-migrate: resume checkpoint invalid: %w", err)}
		}
		logKV("resume",
			"checkpoint_phase", ckpt.Phase,
			"last_header_imported", ckpt.LastHeaderImported,
			"last_executed", ckpt.LastExecuted,
		)
	}

	srcChain, err := openSourceChainDB(filepath.Join(opts.Source, "l2chaindata"))
	if err != nil {
		return ExitError{Code: ExitMigration, Err: fmt.Errorf("mdbx-migrate: open source l2chaindata: %w", err)}
	}
	defer srcChain.Close()

	dstChain, err := openDestChainDB(filepath.Join(opts.Dest, "l2chaindata"), opts.Mdbx)
	if err != nil {
		return ExitError{Code: ExitMigration, Err: fmt.Errorf("mdbx-migrate: open destination l2chaindata: %w", err)}
	}
	defer dstChain.Close()
	if err := dbutil.PutUnfinishedConversionCanaryFile(filepath.Join(opts.Dest, "l2chaindata")); err != nil {
		return ExitError{Code: ExitMigration, Err: fmt.Errorf("mdbx-migrate: write l2chaindata canary: %w", err)}
	}

	logger := elog.New("component", "mdbx-migrate")
	chainCfg, _, wroteCfg, err := ensureDestChainConfig(context.Background(), srcChain, dstChain)
	if err != nil {
		return ExitError{Code: ExitMigration, Err: fmt.Errorf("mdbx-migrate: bootstrap chain config: %w", err)}
	}
	if wroteCfg {
		logKV("bootstrap", "status", "done")
	} else {
		logKV("bootstrap", "status", "skip", "reason", "exists")
	}

	var initMsg *arbostypes.ParsedInitMessage
	chainCfg, initMsg, err = resolveArbitrumInitMessage(filepath.Join(opts.Source, "arbitrumdata"), chainCfg)
	if err != nil {
		return ExitError{Code: ExitMigration, Err: fmt.Errorf("mdbx-migrate: resolve init message: %w", err)}
	}

	importHead, err := resolveImportHead(srcChain, opts)
	if err != nil {
		return ExitError{Code: ExitMigration, Err: fmt.Errorf("mdbx-migrate: resolve head: %w", err)}
	}

	if isPhaseComplete(ckpt, phaseHeadersImported) {
		logKV("import", "status", "skip", "reason", "checkpoint")
	} else {
		stats, headHash, err := importHeadersAndBodies(context.Background(), srcChain, dstChain, opts.StartBlock, importHead)
		if err != nil {
			return ExitError{Code: ExitMigration, Err: fmt.Errorf("mdbx-migrate: import headers/bodies: %w", err)}
		}
		if err := finalizeImportProgress(context.Background(), dstChain, importHead, headHash); err != nil {
			return ExitError{Code: ExitMigration, Err: fmt.Errorf("mdbx-migrate: import finalize: %w", err)}
		}
		logKV("import", "status", "done", "blocks", stats.Blocks, "head", importHead)
		ckpt = &checkpoint{
			Mode:               opts.Mode,
			Phase:              phaseHeadersImported,
			ChainID:            info.ChainID,
			GenesisHash:        info.GenesisHash.Hex(),
			StartBlock:         opts.StartBlock,
			EndBlock:           opts.EndBlock,
			LastHeaderImported: importHead,
		}
		if err := writeCheckpoint(opts.Dest, ckpt); err != nil {
			return ExitError{Code: ExitMigration, Err: fmt.Errorf("mdbx-migrate: write checkpoint: %w", err)}
		}
	}
	if err := appendCanonicalTxNums(context.Background(), dstChain, opts.StartBlock); err != nil {
		return ExitError{Code: ExitMigration, Err: fmt.Errorf("mdbx-migrate: append txnums: %w", err)}
	}

	blockReader, err := newBlockReader(filepath.Join(opts.Dest, "snapshots"), chainCfg.ChainName, logger)
	if err != nil {
		return ExitError{Code: ExitMigration, Err: fmt.Errorf("mdbx-migrate: init block reader: %w", err)}
	}

	if isPhaseComplete(ckpt, phaseSendersDone) {
		logKV("senders", "status", "skip", "reason", "checkpoint")
	} else {
		logKV("senders", "status", "start", "to", importHead)
		if err := runSendersStage(context.Background(), dstChain, chainCfg, blockReader, filepath.Join(opts.Dest, "tmp"), importHead, logger); err != nil {
			return ExitError{Code: ExitMigration, Err: fmt.Errorf("mdbx-migrate: senders stage: %w", err)}
		}
		logKV("senders", "status", "done", "head", importHead)
		if ckpt == nil {
			ckpt = &checkpoint{}
		}
		ckpt.Mode = opts.Mode
		ckpt.Phase = phaseSendersDone
		ckpt.ChainID = info.ChainID
		ckpt.GenesisHash = info.GenesisHash.Hex()
		ckpt.StartBlock = opts.StartBlock
		ckpt.EndBlock = opts.EndBlock
		ckpt.LastHeaderImported = importHead
		if err := writeCheckpoint(opts.Dest, ckpt); err != nil {
			return ExitError{Code: ExitMigration, Err: fmt.Errorf("mdbx-migrate: write checkpoint: %w", err)}
		}
	}

	if isPhaseComplete(ckpt, phaseExecutionDone) {
		logKV("execution", "status", "skip", "reason", "checkpoint")
	} else {
		logKV("execution", "status", "start", "to", importHead)
		if err := runExecutionStage(context.Background(), srcChain, dstChain, chainCfg, initMsg, blockReader, opts.Dest, importHead, opts.Workers, logger); err != nil {
			return ExitError{Code: ExitMigration, Err: fmt.Errorf("mdbx-migrate: execution stage: %w", err)}
		}
		logKV("execution", "status", "done", "head", importHead)
		if ckpt == nil {
			ckpt = &checkpoint{}
		}
		ckpt.Mode = opts.Mode
		ckpt.Phase = phaseExecutionDone
		ckpt.ChainID = info.ChainID
		ckpt.GenesisHash = info.GenesisHash.Hex()
		ckpt.StartBlock = opts.StartBlock
		ckpt.EndBlock = opts.EndBlock
		ckpt.LastHeaderImported = importHead
		ckpt.LastExecuted = importHead
		if err := writeCheckpoint(opts.Dest, ckpt); err != nil {
			return ExitError{Code: ExitMigration, Err: fmt.Errorf("mdbx-migrate: write checkpoint: %w", err)}
		}
	}

	if isPhaseComplete(ckpt, phaseTxLookupDone) {
		logKV("txlookup", "status", "skip", "reason", "checkpoint")
	} else {
		logKV("txlookup", "status", "start", "to", importHead)
		if err := runTxLookupStage(context.Background(), dstChain, chainCfg, blockReader, filepath.Join(opts.Dest, "tmp"), importHead, logger); err != nil {
			return ExitError{Code: ExitMigration, Err: fmt.Errorf("mdbx-migrate: txlookup stage: %w", err)}
		}
		logKV("txlookup", "status", "done", "head", importHead)
		if ckpt == nil {
			ckpt = &checkpoint{}
		}
		ckpt.Mode = opts.Mode
		ckpt.Phase = phaseTxLookupDone
		ckpt.ChainID = info.ChainID
		ckpt.GenesisHash = info.GenesisHash.Hex()
		ckpt.StartBlock = opts.StartBlock
		ckpt.EndBlock = opts.EndBlock
		ckpt.LastHeaderImported = importHead
		ckpt.LastExecuted = importHead
		if err := writeCheckpoint(opts.Dest, ckpt); err != nil {
			return ExitError{Code: ExitMigration, Err: fmt.Errorf("mdbx-migrate: write checkpoint: %w", err)}
		}
	}

	if isPhaseComplete(ckpt, phaseCopyDone) {
		logKV("copy", "status", "skip", "reason", "checkpoint")
	} else {
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

		dstWasm, err := erigonexec.OpenWasmDB(opts.Dest, mdbxOptionsFromConfig(opts.Mdbx))
		if err != nil {
			return ExitError{Code: ExitMigration, Err: fmt.Errorf("mdbx-migrate: open destination wasm: %w", err)}
		}
		defer dstWasm.Close()

		if err := dbutil.PutUnfinishedConversionCanaryFile(filepath.Join(opts.Dest, "arbitrumdata")); err != nil {
			return ExitError{Code: ExitMigration, Err: fmt.Errorf("mdbx-migrate: write arbitrumdata canary: %w", err)}
		}
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

		if ckpt == nil {
			ckpt = &checkpoint{}
		}
		ckpt.Mode = opts.Mode
		ckpt.Phase = phaseCopyDone
		ckpt.ChainID = info.ChainID
		ckpt.GenesisHash = info.GenesisHash.Hex()
		ckpt.StartBlock = opts.StartBlock
		ckpt.EndBlock = opts.EndBlock
		ckpt.LastHeaderImported = importHead
		ckpt.LastExecuted = importHead
		if err := writeCheckpoint(opts.Dest, ckpt); err != nil {
			return ExitError{Code: ExitMigration, Err: fmt.Errorf("mdbx-migrate: write checkpoint: %w", err)}
		}
	}

	return nil
}
