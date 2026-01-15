//go:build erigon
// +build erigon

package erigonexec

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime/pprof"
	"runtime/trace"
	"time"

	"github.com/google/uuid"

	ecommon "github.com/erigontech/erigon-lib/common"
	elog "github.com/erigontech/erigon-lib/log/v3"
	"github.com/erigontech/erigon/execution/consensus/ethash"
	ecore "github.com/erigontech/erigon/core"
	estate "github.com/erigontech/erigon/core/state"
	"github.com/erigontech/erigon/core/vm"
	"github.com/erigontech/erigon/core/vm/evmtypes"
	"github.com/erigontech/erigon/db/kv"
	dbstate "github.com/erigontech/erigon/db/state"
	etypes "github.com/erigontech/erigon/execution/types"

	"github.com/ethereum/go-ethereum/arbitrum_types"
	"github.com/ethereum/go-ethereum/common"
	gcore "github.com/ethereum/go-ethereum/core"
	gstate "github.com/ethereum/go-ethereum/core/state"
	gtypes "github.com/ethereum/go-ethereum/core/types"
	gparams "github.com/ethereum/go-ethereum/params"

	"github.com/offchainlabs/nitro/arbos"
	"github.com/offchainlabs/nitro/arbos/arbosState"
	"github.com/offchainlabs/nitro/arbos/l2pricing"
	"github.com/offchainlabs/nitro/arbos/arbostypes"
	"github.com/offchainlabs/nitro/arbutil"
	"github.com/offchainlabs/nitro/execution"
	"github.com/offchainlabs/nitro/execution/gethexec"
	"github.com/offchainlabs/nitro/util/arbmath"
)

type sequencingHooks struct {
	TxErrors               []error
	DiscardInvalidTxsEarly bool
	PreTxFilter            func(*etypes.Header, estate.IntraBlockStateArbitrum, *arbosState.ArbosState, *gtypes.Transaction, *arbitrum_types.ConditionalOptions, common.Address, *l1Info) error
	PostTxFilter           func(*etypes.Header, estate.IntraBlockStateArbitrum, *arbosState.ArbosState, *gtypes.Transaction, common.Address, uint64, *evmtypes.ExecutionResult) error
	ConditionalOptionsForTx []*arbitrum_types.ConditionalOptions
}

func noopSequencingHooks() *sequencingHooks {
	return &sequencingHooks{
		TxErrors:               []error{},
		DiscardInvalidTxsEarly: false,
		PreTxFilter: func(*etypes.Header, estate.IntraBlockStateArbitrum, *arbosState.ArbosState, *gtypes.Transaction, *arbitrum_types.ConditionalOptions, common.Address, *l1Info) error {
			return nil
		},
		PostTxFilter: func(*etypes.Header, estate.IntraBlockStateArbitrum, *arbosState.ArbosState, *gtypes.Transaction, common.Address, uint64, *evmtypes.ExecutionResult) error {
			return nil
		},
		ConditionalOptionsForTx: nil,
	}
}

type sequencerTxItem struct {
	tx      etypes.Transaction
	gethTx  *gtypes.Transaction
	options *arbitrum_types.ConditionalOptions
	isUser  bool
}

func (c *Client) sequenceTransactions(ctx context.Context, l1Header *arbostypes.L1IncomingMessageHeader, txes gtypes.Transactions, hooks *sequencingHooks, timeboostedTxs map[common.Hash]struct{}) (*etypes.Block, etypes.Receipts, []error, error) {
	if c.consensus == nil {
		return nil, nil, nil, errors.New("erigonexec: consensus client not configured")
	}
	if l1Header == nil {
		return nil, nil, nil, errors.New("erigonexec: missing l1 header for sequencing")
	}
	if hooks == nil {
		hooks = noopSequencingHooks()
	}
	c.blocksMu.Lock()
	defer c.blocksMu.Unlock()

	prevHeader, err := c.readCurrentHeader()
	if err != nil {
		return nil, nil, nil, err
	}
	delayedMessagesRead := prevHeader.Nonce.Uint64()

	block, receipts, txErrors, err := c.buildBlockFromTransactions(ctx, l1Header, txes, prevHeader, delayedMessagesRead, hooks)
	if err != nil {
		return nil, nil, txErrors, err
	}
	if block == nil {
		return nil, nil, txErrors, nil
	}

	msg, err := gethexec.MessageFromTxes(l1Header, txes, txErrors)
	if err != nil {
		return nil, receipts, txErrors, err
	}
	pos, err := c.BlockNumberToMessageIndex(prevHeader.Number.Uint64() + 1)
	if err != nil {
		return nil, receipts, txErrors, err
	}
	messageWithMeta := arbostypes.MessageWithMetadata{
		Message:             msg,
		DelayedMessagesRead: delayedMessagesRead,
	}
	extra := etypes.DeserializeHeaderExtraInformation(block.Header())
	msgResult := execution.MessageResult{
		BlockHash: toGethHash(block.Hash()),
		SendRoot:  toGethHash(extra.SendRoot),
	}
	blockMeta := blockMetadataFromErigonBlock(block, timeboostedTxs)
	if err := c.consensus.WriteMessageFromSequencer(pos, messageWithMeta, msgResult, blockMeta); err != nil {
		return nil, receipts, txErrors, err
	}
	if err := c.commitBlock(ctx, block); err != nil {
		return nil, receipts, txErrors, err
	}
	c.cacheL1PriceDataOfMsg(pos, receipts, block, false)
	return block, receipts, txErrors, nil
}

func (c *Client) sequenceTransactionsWithProfiling(ctx context.Context, l1Header *arbostypes.L1IncomingMessageHeader, txes gtypes.Transactions, hooks *sequencingHooks, timeboostedTxs map[common.Hash]struct{}) (*etypes.Block, etypes.Receipts, []error, error) {
	pprofBuf, traceBuf := bytes.NewBuffer(nil), bytes.NewBuffer(nil)
	if err := pprof.StartCPUProfile(pprofBuf); err != nil {
		if c.logger != nil {
			c.logger.Warn("erigonexec: starting CPU profiling failed", "err", err)
		}
	}
	if err := trace.Start(traceBuf); err != nil {
		if c.logger != nil {
			c.logger.Warn("erigonexec: starting trace failed", "err", err)
		}
	}
	start := time.Now()
	block, receipts, txErrors, err := c.sequenceTransactions(ctx, l1Header, txes, hooks, timeboostedTxs)
	elapsed := time.Since(start)
	pprof.StopCPUProfile()
	trace.Stop()
	if elapsed > 2*time.Second {
		writeProfilingArtifacts(pprofBuf, traceBuf, c.logger)
	}
	return block, receipts, txErrors, err
}

func writeProfilingArtifacts(pprofBuf, traceBuf *bytes.Buffer, logger elog.Logger) {
	id := uuid.NewString()
	pprofFile := filepath.Join(os.TempDir(), id+".pprof")
	if err := os.WriteFile(pprofFile, pprofBuf.Bytes(), 0o600); err != nil {
		if logger != nil {
			logger.Warn("erigonexec: create profiling file failed", "file", pprofFile, "err", err)
		}
		return
	}
	traceFile := filepath.Join(os.TempDir(), id+".trace")
	if err := os.WriteFile(traceFile, traceBuf.Bytes(), 0o600); err != nil {
		if logger != nil {
			logger.Warn("erigonexec: create trace file failed", "file", traceFile, "err", err)
		}
		return
	}
	if logger != nil {
		logger.Info("erigonexec: block sequencing created pprof/trace files", "pprof", pprofFile, "trace", traceFile)
	}
}

func blockMetadataFromErigonBlock(block *etypes.Block, timeboostedTxs map[common.Hash]struct{}) common.BlockMetadata {
	if block == nil {
		return nil
	}
	bits := make(common.BlockMetadata, 1+arbmath.DivCeil(uint64(len(block.Transactions())), 8))
	if len(timeboostedTxs) == 0 {
		return bits
	}
	for i, tx := range block.Transactions() {
		hash := toGethHash(tx.Hash())
		if _, ok := timeboostedTxs[hash]; ok {
			bits[1+i/8] |= 1 << (i % 8)
		}
	}
	return bits
}

func (c *Client) buildBlockFromTransactions(ctx context.Context, l1Header *arbostypes.L1IncomingMessageHeader, txes gtypes.Transactions, prevHeader *etypes.Header, delayedMessagesRead uint64, hooks *sequencingHooks) (*etypes.Block, etypes.Receipts, []error, error) {
	if l1Header == nil {
		return nil, nil, nil, errors.New("erigonexec: missing l1 header")
	}
	if c.chainConfig == nil {
		return nil, nil, nil, errors.New("erigonexec: missing chain config")
	}
	if hooks == nil {
		hooks = noopSequencingHooks()
	}
	temporalDB, ok := c.chainDB.(kv.TemporalRoDB)
	if !ok {
		return nil, nil, nil, errors.New("erigonexec: chain db missing temporal support")
	}
	tx, err := temporalDB.BeginTemporalRo(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	defer tx.Rollback()

	domains, err := dbstate.NewSharedDomains(tx, c.logger)
	if err != nil {
		return nil, nil, nil, err
	}
	defer domains.Close()

	stateReader := estate.NewReaderV3(domains.AsGetter(tx))
	ibs := estate.New(stateReader)
	ibs.SetWasmDB(c.wasmDBForCtx(ctx))
	ibsArb := estate.NewArbitrum(ibs)

	l1 := l1Info{
		poster:        ecommon.Address(l1Header.Poster),
		l1BlockNumber: l1Header.BlockNumber,
		l1Timestamp:   l1Header.Timestamp,
	}
	header, arbState, err := buildArbitrumHeader(prevHeader, l1, c.chainConfig, ibsArb)
	if err != nil {
		return nil, nil, nil, err
	}

	internalTx, err := buildInternalStartTx(c.chainConfig.ChainID, l1Header.L1BaseFee, l1Header.BlockNumber, header, prevHeader)
	if err != nil {
		return nil, nil, nil, err
	}
	converted, err := convertGethTransactions(txes)
	if err != nil {
		return nil, nil, nil, err
	}

	items := make([]sequencerTxItem, 0, len(converted)+1)
	items = append(items, sequencerTxItem{tx: internalTx, isUser: false})
	optIdx := 0
	for i, gtx := range txes {
		var opt *arbitrum_types.ConditionalOptions
		if hooks != nil && optIdx < len(hooks.ConditionalOptionsForTx) {
			opt = hooks.ConditionalOptionsForTx[optIdx]
			optIdx++
		}
		items = append(items, sequencerTxItem{
			tx:      converted[i],
			gethTx:  gtx,
			options: opt,
			isUser:  true,
		})
	}

	engine := ethash.NewFaker()
	defer engine.Close()

	getHeader := func(hash ecommon.Hash, number uint64) (*etypes.Header, error) {
		return c.blockReader.Header(ctx, tx, hash, number)
	}
	blockCtx := ecore.NewEVMBlockContext(header, ecore.GetHashFn(header, getHeader), engine, &header.Coinbase, c.chainConfig)
	rules := blockCtx.Rules(c.chainConfig)
	evm := vm.NewEVM(blockCtx, evmtypes.TxContext{}, ibs, c.chainConfig, vm.Config{})
	signer := *etypes.MakeSigner(c.chainConfig, header.Number.Uint64(), header.Time)
	gethSigner := gtypes.LatestSignerForChainID(c.chainConfig.ChainID)

	blockGasLeft, err := arbState.L2PricingState().PerBlockGasLimit()
	if err != nil {
		return nil, nil, nil, err
	}

	complete := make(etypes.Transactions, 0, len(items))
	receipts := make(etypes.Receipts, 0, len(items))
	var scheduled []sequencerTxItem

	txNum := domains.TxNum()
	stateWriter := estate.NewWriter(domains.AsPutDel(tx), nil, txNum)
	userTxsProcessed := 0

	for len(items) > 0 || len(scheduled) > 0 {
		var item sequencerTxItem
		if len(scheduled) > 0 {
			item = scheduled[0]
			scheduled = scheduled[1:]
		} else {
			item = items[0]
			items = items[1:]
		}

		isUserTx := item.isUser
		activeHooks := hooks
		if !isUserTx || activeHooks == nil {
			activeHooks = noopSequencingHooks()
		}

		var (
			sender   common.Address
			dataGas  uint64
			preErr   error
			gethTx   = item.gethTx
			arbosVer uint64
		)

		if isUserTx {
			if blockGasLeft < gparams.TxGas {
				preErr = gcore.ErrGasLimitReached
			}
			if preErr == nil {
				sender, preErr = gtypes.Sender(gethSigner, gethTx)
			}
			if preErr == nil && activeHooks.PreTxFilter != nil {
				preErr = activeHooks.PreTxFilter(header, ibsArb, arbState, gethTx, item.options, sender, &l1)
			}
			if preErr == nil && header.BaseFee != nil && header.BaseFee.Sign() > 0 {
				dataGas = math.MaxUint64
				brotliCompressionLevel, err := arbState.BrotliCompressionLevel()
				if err != nil {
					preErr = err
				} else {
					posterCost, _ := arbState.L1PricingState().GetPosterInfo(gethTx, l1Header.Poster, brotliCompressionLevel)
					posterCostInL2Gas := arbmath.BigDiv(posterCost, header.BaseFee)
					if posterCostInL2Gas.IsUint64() {
						dataGas = posterCostInL2Gas.Uint64()
					}
				}
			}
			if preErr == nil {
				txGas := gethTx.Gas()
				if dataGas > txGas {
					dataGas = txGas
				}
				computeGas := txGas - dataGas
				if gethTx.To() != nil && arbutil.IsCustomPriceAddr(gethTx.To()) {
					computeGas = gparams.TxGas
				}
				if computeGas < gparams.TxGas {
					if activeHooks.DiscardInvalidTxsEarly {
						preErr = gcore.ErrIntrinsicGas
					} else {
						computeGas = gparams.TxGas
					}
				}
				if preErr == nil && computeGas > blockGasLeft && isUserTx && userTxsProcessed > 0 {
					preErr = gcore.ErrGasLimitReached
				}
			}
		}

		if isUserTx {
			activeHooks.TxErrors = append(activeHooks.TxErrors, preErr)
		}
		if preErr != nil {
			if !activeHooks.DiscardInvalidTxsEarly {
				blockGasLeft = arbmath.SaturatingUSub(blockGasLeft, gparams.TxGas)
				if isUserTx {
					userTxsProcessed++
				}
			}
			continue
		}

		txNum++
		domains.SetTxNum(txNum)
		stateReader.SetTxNum(txNum)
		stateWriter.SetTxNum(txNum)
		ibs.SetTxContext(header.Number.Uint64(), len(receipts))

		if isUserTx {
			item.tx.SetSender(ecommon.Address(sender))
		}
		msgForHook, err := item.tx.AsMessage(signer, header.BaseFee, rules)
		if err != nil {
			if isUserTx {
				activeHooks.TxErrors[len(activeHooks.TxErrors)-1] = err
			}
			continue
		}
		if evm.ProcessingHookSet.CompareAndSwap(false, true) {
			evm.ProcessingHook = arbos.NewTxProcessorIBS(evm, ibsArb, msgForHook)
		} else {
			evm.ProcessingHook.SetMessage(msgForHook, ibsArb)
		}

		preHeaderGasUsed := header.GasUsed
		var preBlobGasUsed *uint64
		if header.BlobGasUsed != nil {
			val := *header.BlobGasUsed
			preBlobGasUsed = &val
		}

		snapshot := ibs.Snapshot()
		gasPool := ecore.NewGasPool(l2pricing.GethBlockGasLimit, 0)
		receipt, execResult, err := ecore.ApplyArbTransactionVmenv(
			c.chainConfig,
			engine,
			gasPool,
			ibsArb,
			stateWriter,
			header,
			item.tx,
			&header.GasUsed,
			header.BlobGasUsed,
			vm.Config{},
			evm,
		)
		if err != nil {
			ibs.RevertToSnapshot(snapshot, err)
			header.GasUsed = preHeaderGasUsed
			if preBlobGasUsed != nil {
				*header.BlobGasUsed = *preBlobGasUsed
			}
			if isUserTx {
				activeHooks.TxErrors[len(activeHooks.TxErrors)-1] = err
			}
			if !activeHooks.DiscardInvalidTxsEarly {
				blockGasLeft = arbmath.SaturatingUSub(blockGasLeft, gparams.TxGas)
				if isUserTx {
					userTxsProcessed++
				}
			}
			continue
		}

		if item.tx.Type() == etypes.ArbitrumInternalTxType {
			stateAdapter := arbos.NewStateDBAdapter(ibsArb, evm.ChainRules())
			arbState, err = arbosState.OpenSystemArbosState(stateAdapter, nil, true)
			if err != nil {
				return nil, nil, nil, err
			}
			extraInfo := etypes.DeserializeHeaderExtraInformation(header)
			extraInfo.ArbOSFormatVersion = arbState.ArbOSVersion()
			extraInfo.UpdateHeaderWithInfo(header)
			if execResult != nil && execResult.Err != nil {
				return nil, nil, nil, fmt.Errorf("erigonexec: internal tx failed: %w", execResult.Err)
			}
		}

		if isUserTx && activeHooks.PostTxFilter != nil {
			postErr := activeHooks.PostTxFilter(header, ibsArb, arbState, gethTx, sender, dataGas, execResult)
			if postErr != nil {
				ibs.RevertToSnapshot(snapshot, postErr)
				header.GasUsed = preHeaderGasUsed
				if preBlobGasUsed != nil {
					*header.BlobGasUsed = *preBlobGasUsed
				}
				activeHooks.TxErrors[len(activeHooks.TxErrors)-1] = postErr
				return nil, nil, hooks.TxErrors, postErr
			}
		}

		if execResult != nil {
			scheduledTxes := execResult.ScheduledTxes
			for _, scheduledTx := range scheduledTxes {
				scheduled = append(scheduled, sequencerTxItem{tx: scheduledTx, isUser: false})
			}
		}

		if preHeaderGasUsed > header.GasUsed {
			return nil, nil, nil, fmt.Errorf("erigonexec: ApplyArbTransactionVmenv used -%v gas", preHeaderGasUsed-header.GasUsed)
		}
		txGasUsed := header.GasUsed - preHeaderGasUsed
		arbosVer = etypes.DeserializeHeaderExtraInformation(header).ArbOSFormatVersion
		if arbosVer >= gparams.ArbosVersion_FixRedeemGas {
			if execResult != nil {
				for _, scheduledTx := range execResult.ScheduledTxes {
					if retry, ok := scheduledTx.Unwrap().(*etypes.ArbitrumRetryTx); ok {
						txGasUsed = arbmath.SaturatingUSub(txGasUsed, retry.Gas)
					}
				}
			}
		}

		if txGasUsed > item.tx.GetGasLimit() {
			return nil, nil, nil, fmt.Errorf("erigonexec: ApplyArbTransactionVmenv used %v more gas than it should have", txGasUsed-item.tx.GetGasLimit())
		}
		computeUsed := txGasUsed - dataGas
		if txGasUsed < dataGas {
			computeUsed = gparams.TxGas
		} else if computeUsed < gparams.TxGas {
			computeUsed = gparams.TxGas
		}
		blockGasLeft = arbmath.SaturatingUSub(blockGasLeft, computeUsed)

		complete = append(complete, item.tx)
		receipts = append(receipts, receipt)
		if isUserTx {
			userTxsProcessed++
		}
	}

	if ibsArb.IsTxFiltered() {
		return nil, nil, nil, gstate.ErrArbTxFilter
	}

	domains.SetBlockNum(header.Number.Uint64())
	domains.SetTxNum(txNum)
	root, err := domains.ComputeCommitment(ctx, true, header.Number.Uint64(), txNum, "erigonexec")
	if err != nil {
		return nil, nil, nil, err
	}
	header.Root = ecommon.BytesToHash(root)

	stateAdapter := arbos.NewStateDBAdapter(ibsArb, evm.ChainRules())
	arbState, err = arbosState.OpenSystemArbosState(stateAdapter, nil, true)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := updateArbitrumHeaderInfo(header, c.chainConfig, arbState); err != nil {
		return nil, nil, nil, err
	}
	binary.BigEndian.PutUint64(header.Nonce[:], delayedMessagesRead)

	block := etypes.NewBlock(header, complete, nil, receipts, nil)
	senders := make([]ecommon.Address, len(complete))
	for i, tx := range complete {
		if sender, ok := tx.GetSender(); ok {
			senders[i] = sender
			continue
		}
		sender, err := tx.Sender(signer)
		if err != nil {
			return nil, nil, nil, err
		}
		tx.SetSender(sender)
		senders[i] = sender
	}
	if err := receipts.DeriveFields(block.Hash(), block.NumberU64(), complete, senders); err != nil {
		return nil, nil, nil, err
	}

	allErrored := true
	for _, err := range hooks.TxErrors {
		if err == nil {
			allErrored = false
			break
		}
	}
	if allErrored {
		return nil, nil, hooks.TxErrors, nil
	}
	return block, receipts, hooks.TxErrors, nil
}
