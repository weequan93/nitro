//go:build erigon
// +build erigon

package main

import (
	"context"
	"fmt"
	"os"

	elog "github.com/erigontech/erigon-lib/log/v3"
	"github.com/erigontech/erigon/db/kv"
	"github.com/erigontech/erigon/db/kv/prune"
	"github.com/erigontech/erigon/db/snapshotsync/freezeblocks"
	"github.com/erigontech/erigon/db/wrap"
	"github.com/erigontech/erigon/eth/ethconfig"
	"github.com/erigontech/erigon/execution/chain"
	"github.com/erigontech/erigon/execution/stagedsync"
	"github.com/erigontech/erigon/execution/stagedsync/stages"
	"github.com/erigontech/erigon/turbo/services"
)

func newBlockReader(snapDir string, chainName string, logger elog.Logger) (services.FullBlockReader, error) {
	if err := os.MkdirAll(snapDir, 0o755); err != nil {
		return nil, fmt.Errorf("create snapshots dir: %w", err)
	}
	snapCfg := ethconfig.BlocksFreezing{ChainName: chainName}
	sn := freezeblocks.NewRoSnapshots(snapCfg, snapDir, logger)
	return freezeblocks.NewBlockReader(sn, nil), nil
}

func runSendersStage(ctx context.Context, db kv.RwDB, chainCfg *chain.Config, blockReader services.FullBlockReader, tmpDir string, toBlock uint64, logger elog.Logger) error {
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	syncCfg := ethconfig.Defaults.Sync
	sendCfg := stagedsync.StageSendersCfg(db, chainCfg, syncCfg, false, tmpDir, prune.Mode{}, blockReader, nil)
	stage := &stagedsync.Stage{
		ID:          stages.Senders,
		Description: "Recover transaction senders",
		Forward: func(badBlockUnwind bool, s *stagedsync.StageState, u stagedsync.Unwinder, txc wrap.TxContainer, log elog.Logger) error {
			return stagedsync.SpawnRecoverSendersStage(sendCfg, s, u, txc.Tx, toBlock, ctx, log)
		},
		Unwind: func(u *stagedsync.UnwindState, s *stagedsync.StageState, txc wrap.TxContainer, log elog.Logger) error {
			return nil
		},
		Prune: func(u *stagedsync.PruneState, tx kv.RwTx, log elog.Logger) error {
			return nil
		},
	}
	sync := stagedsync.New(syncCfg, []*stagedsync.Stage{stage}, stagedsync.DefaultUnwindOrder, stagedsync.DefaultPruneOrder, logger, stages.ModeApplyingBlocks)
	_, err := sync.Run(db, wrap.NewTxContainer(nil, nil), false, false)
	return err
}
