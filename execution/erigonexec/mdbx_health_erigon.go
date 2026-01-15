//go:build erigon
// +build erigon

package erigonexec

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	ecommon "github.com/erigontech/erigon-lib/common"
	estate "github.com/erigontech/erigon/core/state"
	"github.com/erigontech/erigon/db/kv"
	erawdb "github.com/erigontech/erigon/db/rawdb"
	"github.com/erigontech/erigon/db/kv/rawdbv3"
	dbstate "github.com/erigontech/erigon/db/state"
	"github.com/erigontech/erigon/execution/stagedsync/stages"

	"github.com/offchainlabs/nitro/util/dbutil"
)

func (c *Client) runStartupChecks(ctx context.Context) error {
	if c.cfg.ChainDir == "" {
		return errors.New("erigonexec: missing chain dir for startup checks")
	}
	if err := checkUnfinishedConversionCanary(c.cfg.ChainDir); err != nil {
		return err
	}
	if c.arbDB != nil {
		if err := dbutil.UnfinishedConversionCheck(c.arbDB); err != nil {
			return fmt.Errorf("erigonexec: arbitrumdata canary: %w", err)
		}
	}
	if c.wasmDB != nil {
		if err := dbutil.UnfinishedConversionCheck(c.wasmDB); err != nil {
			return fmt.Errorf("erigonexec: wasm canary: %w", err)
		}
	}
	header, err := c.readCurrentHeader()
	if err != nil {
		return err
	}
	if header.Root == (ecommon.Hash{}) {
		return errors.New("erigonexec: current header has empty state root")
	}
	if err := c.checkStateReadable(ctx); err != nil {
		return err
	}
	if err := c.checkHistoryPruning(ctx); err != nil {
		return err
	}
	if err := c.checkStageProgress(ctx, header.Number.Uint64()); err != nil {
		return err
	}
	// Stage checks may rewind head; re-read for subsequent checks/logging.
	header, err = c.readCurrentHeader()
	if err != nil {
		return err
	}
	if err := c.checkTxNumsConsistency(ctx, header.Number.Uint64()); err != nil {
		return err
	}
	if err := c.checkHistoryPresence(ctx, header.Number.Uint64()); err != nil {
		return err
	}
	c.logger.Info("erigonexec startup checks", "status", "ok", "head", header.Number.Uint64(), "root", header.Root)
	return nil
}

func checkUnfinishedConversionCanary(chainDir string) error {
	subDirs := []string{"l2chaindata", "arbitrumdata", "wasm"}
	for _, sub := range subDirs {
		dir := filepath.Join(chainDir, sub)
		if dbutil.HasUnfinishedConversionCanaryFile(dir) {
			return fmt.Errorf("erigonexec: unfinished conversion canary present in %s", dir)
		}
	}
	return nil
}

func (c *Client) checkStateReadable(ctx context.Context) error {
	if c.chainDB == nil {
		return errors.New("erigonexec: chain db not initialized")
	}
	temporalDB, ok := c.chainDB.(kv.TemporalRoDB)
	if !ok {
		return errors.New("erigonexec: chain db missing temporal support")
	}
	tx, err := temporalDB.BeginTemporalRo(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	domains, err := dbstate.NewSharedDomains(tx, c.logger)
	if err != nil {
		return err
	}
	defer domains.Close()

	stateReader := estate.NewReaderV3(domains.AsGetter(tx))
	stateReader.SetTxNum(domains.TxNum())
	_, err = stateReader.ReadAccountData(ecommon.Address{})
	if err != nil {
		return fmt.Errorf("erigonexec: state read failed: %w", err)
	}
	return nil
}

func (c *Client) checkHistoryPruning(ctx context.Context) error {
	if c.chainDB == nil {
		return errors.New("erigonexec: chain db not initialized")
	}
	if c.historyPruned() {
		c.logger.Warn("erigonexec: history pruning enabled; debug/trace parity not guaranteed", "mode", c.pruneModeLabel)
	}
	return nil
}

func (c *Client) checkStageProgress(ctx context.Context, head uint64) error {
	if c.chainDB == nil {
		return errors.New("erigonexec: chain db not initialized")
	}
	var (
		minProgress = head
		behind      []string
	)
	err := c.chainDB.Update(ctx, func(tx kv.RwTx) error {
		required := []stages.SyncStage{
			stages.Headers,
			stages.BlockHashes,
			stages.Bodies,
			stages.Senders,
			stages.Execution,
			stages.TxLookup,
			stages.Finish,
		}
		for _, stage := range required {
			progress, err := stages.GetStageProgress(tx, stage)
			if err != nil {
				return err
			}
			if progress < head {
				behind = append(behind, fmt.Sprintf("%s:%d", stage, progress))
				if progress < minProgress {
					minProgress = progress
				}
			}
		}
		if len(behind) == 0 {
			return nil
		}
		if minProgress == 0 {
			return fmt.Errorf("erigonexec: stage behind head (head=%d) and cannot rewind to genesis", head)
		}
		if err := rawdbv3.TxNums.Truncate(tx, minProgress+1); err != nil {
			return fmt.Errorf("erigonexec: truncate tx nums to %d: %w", minProgress, err)
		}
		hash, err := erawdb.ReadCanonicalHash(tx, minProgress)
		if err != nil {
			return err
		}
		if hash == (ecommon.Hash{}) {
			return fmt.Errorf("erigonexec: canonical hash missing for head rewind block=%d", minProgress)
		}
		if err := erawdb.WriteHeadHeaderHash(tx, hash); err != nil {
			return err
		}
		erawdb.WriteHeadBlockHash(tx, hash)
		erawdb.WriteForkchoiceHead(tx, hash)
		return nil
	})
	if err != nil {
		return err
	}
	if len(behind) > 0 {
		c.logger.Warn("erigonexec: stage behind head; rewound head", "from", head, "to", minProgress, "stages", strings.Join(behind, ","))
	}
	return nil
}

func (c *Client) checkHistoryPresence(ctx context.Context, head uint64) error {
	if head == 0 {
		return nil
	}
	if c.chainDB == nil {
		return errors.New("erigonexec: chain db not initialized")
	}
	temporalDB, ok := c.chainDB.(kv.TemporalRoDB)
	if !ok {
		return errors.New("erigonexec: chain db missing temporal support")
	}
	tx, err := temporalDB.BeginTemporalRo(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	historyPruned := c.historyPruned()

	files := tx.Debug().DomainFiles(kv.AccountsDomain, kv.StorageDomain, kv.CodeDomain)
	if hasHistorySnapshots(files) {
		return nil
	}

	tables := []string{
		kv.TblAccountHistoryVals,
		kv.TblStorageHistoryVals,
		kv.TblCodeHistoryVals,
	}
	hasEntries, err := hasHistoryTableEntries(tx, tables)
	if err != nil {
		return err
	}
	if hasEntries {
		return nil
	}
	if historyPruned {
		c.logger.Warn("erigonexec: history data missing; historical RPCs may be limited", "mode", c.pruneModeLabel)
		return nil
	}
	return errors.New("erigonexec: history data missing (no history files or DB entries); full history is required for debug/trace parity")
}

func (c *Client) checkTxNumsConsistency(ctx context.Context, head uint64) error {
	if c.chainDB == nil {
		return errors.New("erigonexec: chain db not initialized")
	}
	var (
		lastBlock uint64
		truncate  bool
	)
	err := c.chainDB.Update(ctx, func(tx kv.RwTx) error {
		var err error
		lastBlock, _, err = rawdbv3.TxNums.Last(tx)
		if err != nil {
			return err
		}
		if lastBlock == 0 || lastBlock <= head {
			return nil
		}
		truncate = true
		if err := rawdbv3.TxNums.Truncate(tx, head+1); err != nil {
			return fmt.Errorf("erigonexec: truncate tx nums to %d: %w", head, err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if truncate {
		c.logger.Warn("erigonexec: tx nums ahead of head; truncated", "head", head, "txnums", lastBlock)
	}
	return nil
}

func hasHistorySnapshots(files kv.VisibleFiles) bool {
	if len(files) == 0 {
		return false
	}
	sep := string(filepath.Separator)
	marker := sep + "history" + sep
	for _, file := range files {
		if strings.Contains(file.Fullpath(), marker) {
			return true
		}
	}
	return false
}

func hasHistoryTableEntries(tx kv.Tx, tables []string) (bool, error) {
	for _, table := range tables {
		cursor, err := tx.Cursor(table)
		if err != nil {
			return false, err
		}
		key, _, err := cursor.First()
		cursor.Close()
		if err != nil {
			return false, err
		}
		if key != nil {
			return true, nil
		}
	}
	return false, nil
}
