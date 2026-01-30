//go:build erigon
// +build erigon

package main

import (
	"context"
	"fmt"
	"path/filepath"

	elog "github.com/erigontech/erigon-lib/log/v3"
	"github.com/erigontech/erigon/arb/ethdb/wasmdb"
	"github.com/erigontech/erigon/core/vm"
	"github.com/erigontech/erigon/db/kv"
	"github.com/erigontech/erigon/db/kv/prune"
	"github.com/erigontech/erigon/db/kv/temporal"
	erawdb "github.com/erigontech/erigon/db/rawdb"
	dbstate "github.com/erigontech/erigon/db/state"
	"github.com/erigontech/erigon/db/wrap"
	"github.com/erigontech/erigon/eth/ethconfig"
	"github.com/erigontech/erigon/execution/consensus/ethash"
	"github.com/erigontech/erigon/execution/exec3"
	"github.com/erigontech/erigon/execution/stagedsync"
	"github.com/erigontech/erigon/execution/stagedsync/stages"
	"github.com/erigontech/erigon/turbo/shards"
	gethcommon "github.com/ethereum/go-ethereum/common"
	gethrawdb "github.com/ethereum/go-ethereum/core/rawdb"

	"github.com/offchainlabs/nitro/arbos/arbostypes"
)

func verifyConsensus(opts Options) error {
	ctx := context.Background()
	logKV("verify", "dataset", "l2chaindata", "section", "consensus", "status", "start", "start_block", opts.StartBlock, "end_block", opts.EndBlock)

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

	logger := elog.New("component", "mdbx-migrate")

	chainCfg, _, wroteCfg, err := ensureDestChainConfig(ctx, srcChain, dstChain)
	if err != nil {
		return ExitError{Code: ExitVerification, Err: fmt.Errorf("mdbx-migrate: ensure chain config: %w", err)}
	}
	if wroteCfg {
		logKV("bootstrap", "status", "done")
	}

	var initMsg *arbostypes.ParsedInitMessage
	chainCfg, initMsg, err = resolveArbitrumInitMessage(filepath.Join(opts.Source, "arbitrumdata"), chainCfg)
	if err != nil {
		return ExitError{Code: ExitVerification, Err: fmt.Errorf("mdbx-migrate: resolve init message: %w", err)}
	}

	endBlock, err := resolveImportHead(srcChain, opts)
	if err != nil {
		return ExitError{Code: ExitVerification, Err: fmt.Errorf("mdbx-migrate: resolve head: %w", err)}
	}
	if opts.StartBlock > endBlock {
		return ExitError{Code: ExitVerification, Err: fmt.Errorf("mdbx-migrate: start block %d exceeds end block %d", opts.StartBlock, endBlock)}
	}

	blockReader, err := newBlockReader(filepath.Join(opts.Dest, "snapshots"), chainCfg.ChainName, logger)
	if err != nil {
		return ExitError{Code: ExitVerification, Err: fmt.Errorf("mdbx-migrate: init block reader: %w", err)}
	}

	dirs, err := buildExecDirs(opts.Dest)
	if err != nil {
		return ExitError{Code: ExitVerification, Err: fmt.Errorf("mdbx-migrate: init exec dirs: %w", err)}
	}
	agg, err := dbstate.New(dirs).Logger(logger).GenSaltIfNeed(true).Open(ctx, dstChain)
	if err != nil {
		return ExitError{Code: ExitVerification, Err: fmt.Errorf("mdbx-migrate: open state aggregator: %w", err)}
	}
	if err := agg.OpenFolder(); err != nil {
		agg.Close()
		return ExitError{Code: ExitVerification, Err: fmt.Errorf("mdbx-migrate: open state snapshots: %w", err)}
	}
	defer agg.Close()

	execDB, err := temporal.New(dstChain, agg)
	if err != nil {
		return ExitError{Code: ExitVerification, Err: fmt.Errorf("mdbx-migrate: open temporal db: %w", err)}
	}

	genesis, err := loadSourceGenesis(srcChain, chainCfg)
	if err != nil {
		return ExitError{Code: ExitVerification, Err: fmt.Errorf("mdbx-migrate: load genesis: %w", err)}
	}

	var expectedGenesisRoot *gethcommon.Hash
	genesisHash := gethrawdb.ReadCanonicalHash(srcChain, 0)
	if genesisHash == (gethcommon.Hash{}) {
		logKV("genesis_root", "status", "missing_hash")
	} else if header := gethrawdb.ReadHeader(srcChain, genesisHash, 0); header == nil {
		logKV("genesis_root", "status", "missing_header", "hash", genesisHash)
	} else {
		root := header.Root
		expectedGenesisRoot = &root
		logKV("genesis_root", "status", "loaded", "hash", genesisHash, "root", root)
	}

	logSourceReceiptsDebug(srcChain)

	if err := execDB.Update(ctx, func(tx kv.RwTx) error {
		return erawdb.WriteGenesisIfNotExist(tx, genesis)
	}); err != nil {
		return ExitError{Code: ExitVerification, Err: fmt.Errorf("mdbx-migrate: write genesis: %w", err)}
	}
	exec3.SetArbitrumInitMessage(initMsg)
	if err := ensureArbosInitialized(ctx, execDB, chainCfg, initMsg, genesis.Timestamp, expectedGenesisRoot, logger); err != nil {
		return ExitError{Code: ExitVerification, Err: fmt.Errorf("mdbx-migrate: init arbos: %w", err)}
	}
	if sweepPlainAccountTombstonesEnv {
		if err := sweepPlainAccountTombstones(ctx, execDB, logger); err != nil {
			return ExitError{Code: ExitVerification, Err: fmt.Errorf("mdbx-migrate: sweep tombstones: %w", err)}
		}
	}

	engine := ethash.NewFaker()
	defer engine.Close()

	syncCfg := ethconfig.Defaults.Sync
	if opts.Workers > 0 {
		syncCfg.ExecWorkerCount = opts.Workers
	}
	syncCfg.PersistReceiptsCacheV2 = true

	wasmCtx, cancelWasm := context.WithCancel(ctx)
	defer cancelWasm()
	wasmDB := wasmdb.OpenArbitrumWasmDB(wasmCtx, dirs.ArbitrumWasm)
	notifications := shards.NewNotifications(nil)

	execCfg := stagedsync.StageExecuteBlocksCfg(
		execDB,
		prune.DefaultMode,
		execBatchSize,
		chainCfg,
		engine,
		&vm.Config{},
		notifications,
		false,
		false,
		dirs,
		blockReader,
		nil,
		genesis,
		syncCfg,
		nil,
		wasmDB,
	)

	stage := &stagedsync.Stage{
		ID:          stages.Execution,
		Description: "Execute blocks (consensus verify)",
		Forward: func(badBlockUnwind bool, s *stagedsync.StageState, u stagedsync.Unwinder, txc wrap.TxContainer, log elog.Logger) error {
			return stagedsync.SpawnExecuteBlocksStage(s, u, txc, endBlock, ctx, execCfg, log)
		},
		Unwind: func(u *stagedsync.UnwindState, s *stagedsync.StageState, txc wrap.TxContainer, log elog.Logger) error {
			return stagedsync.UnwindExecutionStage(u, s, txc, ctx, execCfg, log)
		},
		Prune: func(p *stagedsync.PruneState, tx kv.RwTx, log elog.Logger) error {
			return stagedsync.PruneExecutionStage(p, tx, execCfg, ctx, log)
		},
	}

	sync := stagedsync.New(syncCfg, []*stagedsync.Stage{stage}, stagedsync.DefaultUnwindOrder, stagedsync.DefaultPruneOrder, logger, stages.ModeApplyingBlocks)

	var execProgress uint64
	var sendersProgress uint64
	if err := execDB.View(ctx, func(tx kv.Tx) error {
		var err error
		execProgress, err = stages.GetStageProgress(tx, stages.Execution)
		if err != nil {
			return err
		}
		sendersProgress, err = stages.GetStageProgress(tx, stages.Senders)
		if err != nil {
			return err
		}
		return nil
	}); err != nil {
		return ExitError{Code: ExitVerification, Err: fmt.Errorf("mdbx-migrate: read stage progress: %w", err)}
	}

	if sendersProgress < endBlock {
		return ExitError{Code: ExitVerification, Err: fmt.Errorf("mdbx-migrate: senders stage (%d) behind end block (%d)", sendersProgress, endBlock)}
	}

	if opts.StartBlock > 0 {
		if execProgress >= opts.StartBlock {
			logKV("verify", "dataset", "l2chaindata", "section", "consensus_unwind", "from", execProgress, "to", opts.StartBlock-1)
			if err := sync.UnwindTo(opts.StartBlock-1, stagedsync.ExecUnwind, nil); err != nil {
				return ExitError{Code: ExitVerification, Err: fmt.Errorf("mdbx-migrate: unwind execution: %w", err)}
			}
			if err := sync.RunUnwind(execDB, wrap.NewTxContainer(nil, nil)); err != nil {
				return ExitError{Code: ExitVerification, Err: fmt.Errorf("mdbx-migrate: unwind execution run: %w", err)}
			}
			execProgress = opts.StartBlock - 1
		} else if execProgress < opts.StartBlock-1 {
			logKV("verify", "dataset", "l2chaindata", "section", "consensus_range", "status", "expand", "reason", "exec_progress_behind_start", "exec_progress", execProgress, "start_block", opts.StartBlock)
		}
	}

	if execProgress >= endBlock {
		logKV("verify", "dataset", "l2chaindata", "section", "consensus", "status", "skip", "reason", "already_at_or_past_end", "exec_progress", execProgress, "end_block", endBlock)
		return nil
	}

	logKV("verify", "dataset", "l2chaindata", "section", "consensus_execute", "from", execProgress+1, "to", endBlock)
	if _, err := sync.Run(execDB, wrap.NewTxContainer(nil, nil), false, false); err != nil {
		return ExitError{Code: ExitVerification, Err: fmt.Errorf("mdbx-migrate: consensus execute: %w", err)}
	}

	logKV("verify", "dataset", "l2chaindata", "section", "consensus", "status", "ok", "head", endBlock)
	return nil
}
