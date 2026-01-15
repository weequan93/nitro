//go:build erigon
// +build erigon

package erigonexec

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"

	"github.com/c2h5oh/datasize"
	"github.com/holiman/uint256"

	ecommon "github.com/erigontech/erigon-lib/common"
	elog "github.com/erigontech/erigon-lib/log/v3"
	"github.com/erigontech/erigon/core"
	estate "github.com/erigontech/erigon/core/state"
	"github.com/erigontech/erigon/core/vm"
	"github.com/erigontech/erigon/core/vm/evmtypes"
	"github.com/erigontech/erigon/db/datadir"
	"github.com/erigontech/erigon/db/kv"
	"github.com/erigontech/erigon/db/kv/rawdbv3"
	erawdb "github.com/erigontech/erigon/db/rawdb"
	dbstate "github.com/erigontech/erigon/db/state"
	"github.com/erigontech/erigon/db/wrap"
	"github.com/erigontech/erigon/eth/ethconfig"
	"github.com/erigontech/erigon/execution/consensus/ethash"
	"github.com/erigontech/erigon/execution/stagedsync"
	"github.com/erigontech/erigon/execution/stagedsync/stages"
	etypes "github.com/erigontech/erigon/execution/types"

	"github.com/offchainlabs/nitro/arbos"
	"github.com/offchainlabs/nitro/arbos/arbosState"
	"github.com/offchainlabs/nitro/arbos/arbostypes"
	"github.com/offchainlabs/nitro/arbos/util"
)

const execBatchSize = 64 * datasize.MB

func buildExecDirs(chainDir string) (datadir.Dirs, error) {
	dirs := datadir.Dirs{
		DataDir:          chainDir,
		RelativeDataDir:  chainDir,
		Chaindata:        filepath.Join(chainDir, "l2chaindata"),
		ArbitrumWasm:     filepath.Join(chainDir, "wasm"),
		Tmp:              filepath.Join(chainDir, "tmp"),
		Snap:             filepath.Join(chainDir, "snapshots"),
		SnapIdx:          filepath.Join(chainDir, "snapshots", "idx"),
		SnapHistory:      filepath.Join(chainDir, "snapshots", "history"),
		SnapDomain:       filepath.Join(chainDir, "snapshots", "domain"),
		SnapAccessors:    filepath.Join(chainDir, "snapshots", "accessor"),
		SnapCaplin:       filepath.Join(chainDir, "snapshots", "caplin"),
		Downloader:       filepath.Join(chainDir, "downloader"),
		TxPool:           filepath.Join(chainDir, "txpool"),
		Nodes:            filepath.Join(chainDir, "nodes"),
		CaplinBlobs:      filepath.Join(chainDir, "caplin", "blobs"),
		CaplinColumnData: filepath.Join(chainDir, "caplin", "column"),
		CaplinIndexing:   filepath.Join(chainDir, "caplin", "indexing"),
		CaplinLatest:     filepath.Join(chainDir, "caplin", "latest"),
		CaplinGenesis:    filepath.Join(chainDir, "caplin", "genesis-state"),
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

func buildInternalStartTx(chainID *big.Int, l1BaseFee *big.Int, l1BlockNum uint64, header *etypes.Header, prevHeader *etypes.Header) (etypes.Transaction, error) {
	if chainID == nil {
		return nil, errors.New("erigonexec: missing chain id for internal tx")
	}
	if l1BaseFee == nil {
		l1BaseFee = big.NewInt(0)
	}
	timePassed := header.Time
	if prevHeader != nil {
		if header.Time >= prevHeader.Time {
			timePassed = header.Time - prevHeader.Time
		} else {
			timePassed = 0
		}
	}
	if os.Getenv("MDBX_MIGRATE_DEBUG") != "" {
		prevTime := uint64(0)
		if prevHeader != nil {
			prevTime = prevHeader.Time
		}
		elog.Info("mdbx-migrate internal start tx",
			"l1_base_fee", l1BaseFee,
			"l1_block_number", l1BlockNum,
			"l2_block_number", header.Number,
			"header_base_fee", header.BaseFee,
			"header_time", header.Time,
			"prev_header_time", prevTime,
			"time_passed", timePassed,
		)
	}
	data, err := util.PackInternalTxDataStartBlock(l1BaseFee, l1BlockNum, header.Number.Uint64(), timePassed)
	if err != nil {
		return nil, err
	}
	chainIDU256, overflow := uint256.FromBig(chainID)
	if overflow {
		return nil, errors.New("erigonexec: chain id overflow for internal tx")
	}
	return &etypes.ArbitrumInternalTx{
		ChainId: chainIDU256,
		Data:    data,
	}, nil
}

func (c *Client) buildBlockFromMessage(ctx context.Context, msg *arbostypes.MessageWithMetadata, prevHeader *etypes.Header) (*etypes.Block, etypes.Receipts, error) {
	if msg == nil || msg.Message == nil {
		return nil, nil, errors.New("erigonexec: missing message")
	}
	if c.chainConfig == nil {
		return nil, nil, errors.New("erigonexec: missing chain config")
	}
	temporalDB, ok := c.chainDB.(kv.TemporalRoDB)
	if !ok {
		return nil, nil, errors.New("erigonexec: chain db missing temporal support")
	}
	tx, err := temporalDB.BeginTemporalRo(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()

	domains, err := dbstate.NewSharedDomains(tx, c.logger)
	if err != nil {
		return nil, nil, err
	}
	defer domains.Close()

	stateReader := estate.NewReaderV3(domains.AsGetter(tx))
	ibs := estate.New(stateReader)
	ibs.SetWasmDB(c.wasmDBForCtx(ctx))
	ibsArb := estate.NewArbitrum(ibs)

	l1 := l1Info{
		poster:        ecommon.Address(msg.Message.Header.Poster),
		l1BlockNumber: msg.Message.Header.BlockNumber,
		l1Timestamp:   msg.Message.Header.Timestamp,
	}
	header, _, err := buildArbitrumHeader(prevHeader, l1, c.chainConfig, ibsArb)
	if err != nil {
		return nil, nil, err
	}

	internalTx, err := buildInternalStartTx(c.chainConfig.ChainID, msg.Message.Header.L1BaseFee, msg.Message.Header.BlockNumber, header, prevHeader)
	if err != nil {
		return nil, nil, err
	}
	l2txs, err := parseL2TransactionsErigon(msg.Message, c.chainConfig.ChainID)
	if err != nil {
		c.logger.Warn("erigonexec: parse l2 transactions failed", "err", err)
		l2txs = nil
	}

	allTxs := make(etypes.Transactions, 0, len(l2txs)+1)
	allTxs = append(allTxs, internalTx)
	allTxs = append(allTxs, l2txs...)

	engine := ethash.NewFaker()
	defer engine.Close()

	getHeader := func(hash ecommon.Hash, number uint64) (*etypes.Header, error) {
		return c.blockReader.Header(ctx, tx, hash, number)
	}
	blockCtx := core.NewEVMBlockContext(header, core.GetHashFn(header, getHeader), engine, &header.Coinbase, c.chainConfig)
	rules := blockCtx.Rules(c.chainConfig)
	evm := vm.NewEVM(blockCtx, evmtypes.TxContext{}, ibs, c.chainConfig, vm.Config{})

	signer := *etypes.MakeSigner(c.chainConfig, header.Number.Uint64(), header.Time)
	gasPool := new(core.GasPool).AddGas(header.GasLimit)
	stateWriter := estate.NewWriter(domains.AsPutDel(tx), nil, domains.TxNum())

	complete := make(etypes.Transactions, 0, len(allTxs))
	receipts := make(etypes.Receipts, 0, len(allTxs))
	var scheduled etypes.Transactions

	txNum := domains.TxNum()

	for len(allTxs) > 0 || len(scheduled) > 0 {
		var txToApply etypes.Transaction
		if len(scheduled) > 0 {
			txToApply = scheduled[0]
			scheduled = scheduled[1:]
		} else {
			txToApply = allTxs[0]
			allTxs = allTxs[1:]
		}

		txNum++
		domains.SetTxNum(txNum)
		stateReader.SetTxNum(txNum)
		stateWriter.SetTxNum(txNum)

		ibs.SetTxContext(header.Number.Uint64(), len(complete))
		msgForHook, err := txToApply.AsMessage(signer, header.BaseFee, rules)
		if err != nil {
			return nil, nil, err
		}
		if evm.ProcessingHookSet.CompareAndSwap(false, true) {
			evm.ProcessingHook = arbos.NewTxProcessorIBS(evm, ibsArb, msgForHook)
		} else {
			evm.ProcessingHook.SetMessage(msgForHook, ibsArb)
		}

		receipt, execResult, err := core.ApplyArbTransactionVmenv(
			c.chainConfig,
			engine,
			gasPool,
			ibsArb,
			stateWriter,
			header,
			txToApply,
			&header.GasUsed,
			header.BlobGasUsed,
			vm.Config{},
			evm,
		)
		if err != nil {
			return nil, nil, err
		}
		receipts = append(receipts, receipt)
		complete = append(complete, txToApply)
		if execResult != nil && len(execResult.ScheduledTxes) > 0 {
			scheduled = append(scheduled, execResult.ScheduledTxes...)
		}
	}

	domains.SetBlockNum(header.Number.Uint64())
	domains.SetTxNum(txNum)
	root, err := domains.ComputeCommitment(ctx, true, header.Number.Uint64(), txNum, "erigonexec")
	if err != nil {
		return nil, nil, err
	}
	header.Root = ecommon.BytesToHash(root)

	stateAdapter := arbos.NewStateDBAdapter(ibsArb, evm.ChainRules())
	arbState, err := arbosState.OpenSystemArbosState(stateAdapter, nil, true)
	if err != nil {
		return nil, nil, err
	}
	if err := updateArbitrumHeaderInfo(header, c.chainConfig, arbState); err != nil {
		return nil, nil, err
	}
	binary.BigEndian.PutUint64(header.Nonce[:], msg.DelayedMessagesRead)

	block := etypes.NewBlock(header, complete, nil, receipts, nil)
	senders := make([]ecommon.Address, len(complete))
	for i, tx := range complete {
		if sender, ok := tx.GetSender(); ok {
			senders[i] = sender
			continue
		}
		sender, err := tx.Sender(signer)
		if err != nil {
			return nil, nil, err
		}
		tx.SetSender(sender)
		senders[i] = sender
	}
	if err := receipts.DeriveFields(block.Hash(), block.NumberU64(), complete, senders); err != nil {
		return nil, nil, err
	}
	return block, receipts, nil
}

func (c *Client) commitBlock(ctx context.Context, block *etypes.Block) error {
	if block == nil {
		return errors.New("erigonexec: missing block")
	}
	if c.chainConfig == nil {
		return errors.New("erigonexec: missing chain config")
	}
	dirs, err := buildExecDirs(c.cfg.ChainDir)
	if err != nil {
		return err
	}

	var genesis *etypes.Genesis
	if err := c.chainDB.View(ctx, func(tx kv.Tx) error {
		var readErr error
		genesis, readErr = erawdb.ReadGenesis(tx)
		return readErr
	}); err != nil {
		return err
	}
	if genesis == nil {
		return errors.New("erigonexec: genesis not found in chain db")
	}

	engine := ethash.NewFaker()
	defer engine.Close()

	syncCfg := ethconfig.Defaults.Sync
	syncCfg.ExecWorkerCount = 1

	execCfg := stagedsync.StageExecuteBlocksCfg(
		c.chainDB,
		c.pruneMode,
		execBatchSize,
		c.chainConfig,
		engine,
		&vm.Config{},
		nil,
		false,
		false,
		dirs,
		c.blockReader,
		nil,
		genesis,
		syncCfg,
		nil,
		c.wasmDBForCtx(ctx),
	)
	sendersCfg := stagedsync.StageSendersCfg(c.chainDB, c.chainConfig, syncCfg, false, dirs.Tmp, c.pruneMode, c.blockReader, nil)

	temporalDB, ok := c.chainDB.(kv.TemporalRwDB)
	if !ok {
		return errors.New("erigonexec: chain db missing temporal write support")
	}
	tx, err := temporalDB.BeginTemporalRw(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	blockNum := block.NumberU64()

	if err := erawdb.WriteHeader(tx, block.Header()); err != nil {
		return fmt.Errorf("write header: %w", err)
	}
	if err := erawdb.WriteCanonicalHash(tx, block.Hash(), blockNum); err != nil {
		return fmt.Errorf("write canonical hash: %w", err)
	}
	if err := erawdb.WriteHeadHeaderHash(tx, block.Hash()); err != nil {
		return fmt.Errorf("write head header hash: %w", err)
	}
	erawdb.WriteHeadBlockHash(tx, block.Hash())
	if _, err := erawdb.WriteRawBodyIfNotExists(tx, block.Hash(), blockNum, block.RawBody()); err != nil {
		return fmt.Errorf("write body: %w", err)
	}
	if err := erawdb.AppendCanonicalTxNums(tx, blockNum); err != nil {
		return fmt.Errorf("append tx nums: %w", err)
	}
	txNumMin, err := rawdbv3.TxNums.Min(tx, blockNum)
	if err != nil {
		return fmt.Errorf("read tx nums: %w", err)
	}
	erawdb.WriteTxLookupEntries(tx, block, txNumMin)
	if err := stages.SaveStageProgress(tx, stages.Headers, blockNum); err != nil {
		return err
	}
	if err := stages.SaveStageProgress(tx, stages.BlockHashes, blockNum); err != nil {
		return err
	}
	if err := stages.SaveStageProgress(tx, stages.Bodies, blockNum); err != nil {
		return err
	}
	if err := stages.SaveStageProgress(tx, stages.TxLookup, blockNum); err != nil {
		return err
	}
	if err := stages.SaveStageProgress(tx, stages.Finish, blockNum); err != nil {
		return err
	}

	senderState := &stagedsync.StageState{ID: stages.Senders, BlockNumber: blockNum - 1}
	if err := stagedsync.SpawnRecoverSendersStage(sendersCfg, senderState, nil, tx, blockNum, ctx, c.logger); err != nil {
		return fmt.Errorf("senders stage: %w", err)
	}

	execState := &stagedsync.StageState{ID: stages.Execution, BlockNumber: blockNum - 1}
	txc := wrap.NewTxContainer(tx, nil)
	if err := stagedsync.ExecBlockV3(execState, nil, txc, blockNum, ctx, execCfg, false, c.logger, false); err != nil {
		return fmt.Errorf("execution stage: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}
