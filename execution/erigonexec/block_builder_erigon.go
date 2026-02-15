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
	"strings"

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
	gethlog "github.com/ethereum/go-ethereum/log"

	"github.com/offchainlabs/nitro/arbos"
	"github.com/offchainlabs/nitro/arbos/arbosState"
	"github.com/offchainlabs/nitro/arbos/arbostypes"
	"github.com/offchainlabs/nitro/arbos/util"
)

const execBatchSize = 64 * datasize.MB

var (
	pathProbeAddr = func() ecommon.Address {
		raw := strings.TrimSpace(os.Getenv("ERIGON_PATH_PROBE_ADDR"))
		if raw == "" {
			raw = "0xA4b05FffffFffFFFFfFFfffFfffFFfffFfFfFFFf"
		}
		return ecommon.HexToAddress(raw)
	}()
	accountProbeAddr = func() ecommon.Address {
		raw := strings.TrimSpace(os.Getenv("ERIGON_ACCOUNT_PROBE_ADDR"))
		if raw == "" {
			raw = "0xA4b000000000000000000073657175656e636572"
		}
		return ecommon.HexToAddress(raw)
	}()
	pathProbeSlot = func() ecommon.Hash {
		raw := strings.TrimSpace(os.Getenv("ERIGON_PATH_PROBE_SLOT"))
		if raw == "" {
			raw = "0x3c79da47f96b0f39664f73c0a1f350580be90742947dddfa21ba64d578dfe623"
		}
		return ecommon.HexToHash(raw)
	}()
)

func logBuilderPathProbe(logger elog.Logger, blockNum, txNum uint64, txIndex int, txHash ecommon.Hash, stateReader *estate.ReaderV3) {
	if logger == nil || stateReader == nil {
		return
	}
	if !strings.EqualFold(os.Getenv("ERIGON_BAD_ROOT_DEBUG"), "true") || blockNum < 33 {
		return
	}

	probeVal, probeOK, err := stateReader.ReadAccountStorage(pathProbeAddr, pathProbeSlot)
	if err != nil {
		logger.Warn(
			"erigonexec: path probe read failed",
			"phase", "builder",
			"block", blockNum,
			"txnum", txNum,
			"tx_index", txIndex,
			"tx_hash", txHash,
			"probe_addr", pathProbeAddr,
			"probe_slot", pathProbeSlot,
			"err", err,
		)
		return
	}

	probeValHex := "0x"
	if probeOK {
		probeValHex = fmt.Sprintf("0x%x", probeVal.Bytes())
	}
	logger.Warn(
		"erigonexec: path probe",
		"phase", "builder",
		"block", blockNum,
		"txnum", txNum,
		"tx_index", txIndex,
		"tx_hash", txHash,
		"probe_addr", pathProbeAddr,
		"probe_slot", pathProbeSlot,
		"probe_ok", probeOK,
		"probe_val", probeValHex,
	)

	accountData, accErr := stateReader.ReadAccountDataForDebug(accountProbeAddr)
	if accErr != nil {
		logger.Warn(
			"erigonexec: account probe read failed",
			"phase", "builder",
			"block", blockNum,
			"txnum", txNum,
			"tx_index", txIndex,
			"tx_hash", txHash,
			"probe_addr", accountProbeAddr,
			"err", accErr,
		)
		return
	}
	exists := accountData != nil
	nonce := uint64(0)
	balance := "0"
	incarnation := uint64(0)
	codeHash := "0x"
	root := "0x"
	if accountData != nil {
		nonce = accountData.Nonce
		balance = accountData.Balance.ToBig().String()
		incarnation = accountData.Incarnation
		codeHash = accountData.CodeHash.Hex()
		root = accountData.Root.Hex()
	}
	logger.Warn(
		"erigonexec: account probe",
		"phase", "builder",
		"block", blockNum,
		"txnum", txNum,
		"tx_index", txIndex,
		"tx_hash", txHash,
		"probe_addr", accountProbeAddr,
		"exists", exists,
		"nonce", nonce,
		"balance", balance,
		"incarnation", incarnation,
		"code_hash", codeHash,
		"root", root,
	)
}

func composeStorageDomainKey(addr ecommon.Address, slot ecommon.Hash) []byte {
	addrBytes := addr.Bytes()
	slotBytes := slot.Bytes()
	key := make([]byte, len(addrBytes)+len(slotBytes))
	copy(key, addrBytes)
	copy(key[len(addrBytes):], slotBytes)
	return key
}

func logHexPreview(v []byte, max int) string {
	if len(v) == 0 {
		return "0x"
	}
	if max <= 0 || len(v) <= max {
		return fmt.Sprintf("0x%x", v)
	}
	return fmt.Sprintf("0x%x...(+%d bytes)", v[:max], len(v)-max)
}

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

func deriveBlockStartTxNum(tx kv.Tx, prevHeader *etypes.Header) (uint64, error) {
	if tx == nil {
		return 0, errors.New("erigonexec: missing tx for txnum derivation")
	}
	if prevHeader == nil || prevHeader.Number == nil {
		return 0, errors.New("erigonexec: missing previous header for txnum derivation")
	}
	prevMaxTxNum, err := rawdbv3.TxNums.Max(tx, prevHeader.Number.Uint64())
	if err != nil {
		return 0, err
	}
	if prevMaxTxNum == ^uint64(0) {
		return 0, fmt.Errorf("erigonexec: previous block txnum overflow block=%d", prevHeader.Number.Uint64())
	}
	return prevMaxTxNum + 1, nil
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
	txNum, err := deriveBlockStartTxNum(tx, prevHeader)
	if err != nil {
		return nil, nil, err
	}
	stateWriter := estate.NewWriter(domains.AsPutDel(tx), nil, txNum)

	complete := make(etypes.Transactions, 0, len(allTxs))
	receipts := make(etypes.Receipts, 0, len(allTxs))
	var scheduled etypes.Transactions

	startTxNum := txNum
	rootDebug := strings.EqualFold(os.Getenv("ERIGON_BAD_ROOT_DEBUG"), "true")
	logRootDebug := rootDebug && c.logger != nil && header.Number != nil && header.Number.Uint64() >= 33

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
		logBuilderPathProbe(
			c.logger,
			header.Number.Uint64(),
			txNum,
			len(complete)-1,
			txToApply.Hash(),
			stateReader,
		)
		if execResult != nil && len(execResult.ScheduledTxes) > 0 {
			scheduled = append(scheduled, execResult.ScheduledTxes...)
		}
	}

	touchedAddrs := make(map[ecommon.Address]struct{}, len(complete)*2+1)
	touchedAddrs[header.Coinbase] = struct{}{}
	for _, txApplied := range complete {
		if sender, ok := txApplied.GetSender(); ok {
			touchedAddrs[sender] = struct{}{}
		}
		if to := txApplied.GetTo(); to != nil {
			touchedAddrs[*to] = struct{}{}
		}
	}

	// Flush block-level state object changes before computing the state root.
	// FinalizeTx runs per transaction, but CommitBlock writes the final block
	// write-set (including deferred account updates) into SharedDomains.
	if err := ibs.CommitBlock(rules, stateWriter); err != nil {
		return nil, nil, err
	}

	commitTxNum := txNum

	commitmentCtx := domains.GetCommitmentContext()
	restoredState := false
	restoredStateBlock := uint64(0)
	restoredStateTxNum := uint64(0)
	restoredStateRoot := ecommon.Hash{}
	if commitmentCtx != nil {
		restoredBlock, restoredTxNum, restoredRoot, restored, restoreErr := commitmentCtx.RestoreLatestCommitmentStateFromTx(tx)
		if restoreErr != nil {
			return nil, nil, fmt.Errorf("erigonexec: restore latest commitment state from tx: %w", restoreErr)
		}
		restoredState = restored
		restoredStateBlock = restoredBlock
		restoredStateTxNum = restoredTxNum
		if len(restoredRoot) > 0 {
			restoredStateRoot = ecommon.BytesToHash(restoredRoot)
		}
	}

	domsTxNumBefore := domains.TxNum()
	ctxTxNumBefore := uint64(0)
	ctxReadable := false
	if commitmentCtx != nil {
		ctxTxNumBefore, _, _, ctxReadable = commitmentCtx.DebugReadContext()
	}
	ctxAdjusted := false
	if commitmentCtx != nil && ctxReadable && ctxTxNumBefore != commitTxNum {
		commitmentCtx.SetTxNum(commitTxNum)
		ctxAdjusted = true
	}
	domsAdjusted := false
	if domsTxNumBefore != commitTxNum {
		domains.SetTxNum(commitTxNum)
		domsAdjusted = true
	}

	domains.SetBlockNum(header.Number.Uint64())
	root, err := domains.ComputeCommitment(ctx, true, header.Number.Uint64(), commitTxNum, "erigonexec")
	if domsAdjusted {
		domains.SetTxNum(domsTxNumBefore)
	}
	if ctxAdjusted && commitmentCtx != nil {
		commitmentCtx.SetTxNum(ctxTxNumBefore)
	}
	if err != nil {
		return nil, nil, err
	}
	header.Root = ecommon.BytesToHash(root)
	if logRootDebug {
		c.logger.Warn("erigonexec: build root debug",
			"block", header.Number.Uint64(),
			"start_txnum", startTxNum,
			"final_txnum", txNum,
			"commit_txnum", commitTxNum,
			"state_restored", restoredState,
			"state_restored_block", restoredStateBlock,
			"state_restored_txnum", restoredStateTxNum,
			"state_restored_root", restoredStateRoot,
			"ctx_txnum_before", ctxTxNumBefore,
			"ctx_adjusted", ctxAdjusted,
			"domains_txnum_before", domsTxNumBefore,
			"domains_adjusted", domsAdjusted,
			"root_final", header.Root,
			"txs", len(complete),
		)
		for delta := uint64(1); delta <= 3; delta++ {
			altTxNum := commitTxNum + delta
			altRoot, altErr := domains.ComputeCommitment(ctx, true, header.Number.Uint64(), altTxNum, "erigonexec")
			if altErr != nil {
				c.logger.Warn("erigonexec: build root alt failed",
					"block", header.Number.Uint64(),
					"txnum", altTxNum,
					"delta", delta,
					"err", altErr,
				)
				continue
			}
			c.logger.Warn("erigonexec: build root alt",
				"block", header.Number.Uint64(),
				"txnum", altTxNum,
				"delta", delta,
				"root", ecommon.BytesToHash(altRoot),
			)
		}
	}

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
	// Keep receipt persistence aligned with migration execution path.
	syncCfg.PersistReceiptsCacheV2 = true

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
	debugCommit := debugErigonCommitEnabled()
	rootDebug := strings.EqualFold(os.Getenv("ERIGON_BAD_ROOT_DEBUG"), "true")
	tx, err := temporalDB.BeginTemporalRw(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	blockNum := block.NumberU64()
	if rootDebug && c.logger != nil {
		c.logger.Warn("erigonexec: commit block header",
			"block", blockNum,
			"hash", block.Hash(),
			"parent_hash", block.ParentHash(),
			"state_root", block.Root(),
			"tx_root", block.TxHash(),
			"receipt_root", block.ReceiptHash(),
			"txs", len(block.Transactions()),
		)
	}
	var canonicalHashBefore ecommon.Hash
	var canonicalHeaderBefore *etypes.Header
	if rootDebug && c.logger != nil {
		var canonicalErr error
		canonicalHashBefore, canonicalErr = erawdb.ReadCanonicalHash(tx, blockNum)
		if canonicalErr != nil {
			c.logger.Warn("erigonexec: canonical before read failed", "block", blockNum, "err", canonicalErr)
		} else if canonicalHashBefore == (ecommon.Hash{}) {
			c.logger.Warn("erigonexec: canonical before missing", "block", blockNum)
		} else {
			canonicalHeaderBefore = erawdb.ReadHeader(tx, canonicalHashBefore, blockNum)
			if canonicalHeaderBefore == nil {
				c.logger.Warn(
					"erigonexec: canonical before header missing",
					"block", blockNum,
					"canonical_hash_before", canonicalHashBefore,
				)
			} else {
				c.logger.Warn(
					"erigonexec: canonical before",
					"block", blockNum,
					"canonical_hash_before", canonicalHashBefore,
					"canonical_parent_before", canonicalHeaderBefore.ParentHash,
					"canonical_state_root_before", canonicalHeaderBefore.Root,
					"canonical_tx_root_before", canonicalHeaderBefore.TxHash,
					"canonical_receipt_root_before", canonicalHeaderBefore.ReceiptHash,
					"incoming_hash", block.Hash(),
					"incoming_parent", block.ParentHash(),
					"incoming_state_root", block.Root(),
					"incoming_tx_root", block.TxHash(),
					"incoming_receipt_root", block.ReceiptHash(),
					"hash_changed", canonicalHashBefore != block.Hash(),
				)
			}
		}
	}
	var headBefore *etypes.Header
	var headHeaderHashBefore ecommon.Hash
	var headBlockHashBefore ecommon.Hash
	if debugCommit {
		headBefore = erawdb.ReadCurrentHeader(tx)
		headHeaderHashBefore = erawdb.ReadHeadHeaderHash(tx)
		headBlockHashBefore = erawdb.ReadHeadBlockHash(tx)
	}

	if err := erawdb.WriteHeader(tx, block.Header()); err != nil {
		return fmt.Errorf("write header: %w", err)
	}
	if err := erawdb.WriteCanonicalHash(tx, block.Hash(), blockNum); err != nil {
		return fmt.Errorf("write canonical hash: %w", err)
	}
	if rootDebug && c.logger != nil && canonicalHashBefore != (ecommon.Hash{}) && canonicalHashBefore != block.Hash() {
		c.logger.Warn(
			"erigonexec: canonical overwrite",
			"block", blockNum,
			"canonical_hash_before", canonicalHashBefore,
			"incoming_hash", block.Hash(),
			"canonical_state_root_before", func() interface{} {
				if canonicalHeaderBefore == nil {
					return nil
				}
				return canonicalHeaderBefore.Root
			}(),
			"incoming_state_root", block.Root(),
		)
	}
	if err := erawdb.WriteHeadHeaderHash(tx, block.Hash()); err != nil {
		return fmt.Errorf("write head header hash: %w", err)
	}
	erawdb.WriteHeadBlockHash(tx, block.Hash())
	bodyWritten, err := erawdb.WriteRawBodyIfNotExists(tx, block.Hash(), blockNum, block.RawBody())
	if err != nil {
		return fmt.Errorf("write body: %w", err)
	}
	if debugCommit {
		gethlog.Info("erigonexec: body exists", "block", blockNum, "already", !bodyWritten)
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
	if err := stages.SaveStageProgress(tx, stages.Senders, blockNum); err != nil {
		return fmt.Errorf("save senders progress: %w", err)
	}
	if debugCommit {
		logExecDebugState(tx, blockNum, "after_senders")
	}

	execState := &stagedsync.StageState{ID: stages.Execution, BlockNumber: blockNum - 1}
	txc := wrap.NewTxContainer(tx, nil)
	if err := stagedsync.ExecBlockV3(execState, noopUnwinder{}, txc, blockNum, ctx, execCfg, false, c.logger, false); err != nil {
		return fmt.Errorf("execution stage: %w", err)
	}
	if err := stages.SaveStageProgress(tx, stages.Execution, blockNum); err != nil {
		return fmt.Errorf("save execution progress: %w", err)
	}
	if debugCommit {
		logExecDebugState(tx, blockNum, "after_exec")
	}
	if debugCommit && c.logger != nil {
		headAfter := erawdb.ReadCurrentHeader(tx)
		headHeaderHashAfter := erawdb.ReadHeadHeaderHash(tx)
		headBlockHashAfter := erawdb.ReadHeadBlockHash(tx)
		var headBeforeNum interface{} = nil
		var headAfterNum interface{} = nil
		if headBefore != nil {
			headBeforeNum = headBefore.Number.Uint64()
		}
		if headAfter != nil {
			headAfterNum = headAfter.Number.Uint64()
		}
		c.logger.Info(
			"erigonexec: commit head (tx)",
			"block", blockNum,
			"block_hash", block.Hash(),
			"head_before", headBeforeNum,
			"head_after", headAfterNum,
			"head_hash_before", headHeaderHashBefore,
			"head_hash_after", headHeaderHashAfter,
			"head_block_hash_before", headBlockHashBefore,
			"head_block_hash_after", headBlockHashAfter,
		)
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	if rootDebug && c.logger != nil {
		temporalDB, ok := c.chainDB.(kv.TemporalRoDB)
		if !ok {
			c.logger.Warn("erigonexec: persisted root audit skipped (non-temporal db)", "block", blockNum)
		} else {
			roTx, err := temporalDB.BeginTemporalRo(ctx)
			if err != nil {
				c.logger.Warn("erigonexec: persisted root audit begin failed", "block", blockNum, "err", err)
				goto persistedAuditDone
			}
			defer roTx.Rollback()
			persistedHeader := erawdb.ReadHeader(roTx, block.Hash(), blockNum)
			if persistedHeader == nil {
				c.logger.Warn("erigonexec: persisted root audit header missing", "block", blockNum, "hash", block.Hash())
			} else {
				minTxNum, minErr := rawdbv3.TxNums.Min(roTx, blockNum)
				maxTxNum, maxErr := rawdbv3.TxNums.Max(roTx, blockNum)
				if minErr != nil || maxErr != nil {
					c.logger.Warn(
						"erigonexec: persisted root audit txnums read failed",
						"block", blockNum,
						"min_err", minErr,
						"max_err", maxErr,
					)
				} else {
					domsRo, domsErr := dbstate.NewSharedDomains(roTx, elog.New("component", "erigonexec"))
					if domsErr != nil {
						c.logger.Warn("erigonexec: persisted root audit domains open failed", "block", blockNum, "err", domsErr)
					} else {
						storageProbeKey := composeStorageDomainKey(pathProbeAddr, pathProbeSlot)
						accountProbeKey := accountProbeAddr.Bytes()
						defer domsRo.Close()
						domsRo.SetBlockNum(blockNum)
						for _, probeTxNum := range []uint64{minTxNum, maxTxNum, maxTxNum + 1} {
							probeRoot, probeErr := domsRo.ComputeCommitment(ctx, true, blockNum, probeTxNum, "erigonexec")
							if probeErr != nil {
								c.logger.Warn(
									"erigonexec: persisted root audit compute failed",
									"block", blockNum,
									"probe_txnum", probeTxNum,
									"err", probeErr,
								)
								continue
							}
							probeHash := ecommon.BytesToHash(probeRoot)
							storageAsOf, storageAsOfOK, storageAsOfErr := roTx.GetAsOf(kv.StorageDomain, storageProbeKey, probeTxNum)
							storageAsOfNext, storageAsOfNextOK, storageAsOfNextErr := roTx.GetAsOf(kv.StorageDomain, storageProbeKey, probeTxNum+1)
							accountAsOf, accountAsOfOK, accountAsOfErr := roTx.GetAsOf(kv.AccountsDomain, accountProbeKey, probeTxNum)
							accountAsOfNext, accountAsOfNextOK, accountAsOfNextErr := roTx.GetAsOf(kv.AccountsDomain, accountProbeKey, probeTxNum+1)
							storageLatest, storageLatestStep, storageLatestErr := domsRo.GetLatest(kv.StorageDomain, roTx, storageProbeKey)
							accountLatest, accountLatestStep, accountLatestErr := domsRo.GetLatest(kv.AccountsDomain, roTx, accountProbeKey)
							c.logger.Warn(
								"erigonexec: persisted root audit",
								"block", blockNum,
								"hash", block.Hash(),
								"probe_txnum", probeTxNum,
								"probe_root", probeHash,
								"header_root", persistedHeader.Root,
								"matches_header", probeHash == persistedHeader.Root,
								"txnums_min", minTxNum,
								"txnums_max", maxTxNum,
							)
							c.logger.Warn(
								"erigonexec: persisted probe values",
								"block", blockNum,
								"probe_txnum", probeTxNum,
								"probe_addr", pathProbeAddr,
								"probe_slot", pathProbeSlot,
								"storage_asof_ok", storageAsOfOK,
								"storage_asof_len", len(storageAsOf),
								"storage_asof_preview", logHexPreview(storageAsOf, 64),
								"storage_asof_err", storageAsOfErr,
								"storage_asof_next_ok", storageAsOfNextOK,
								"storage_asof_next_len", len(storageAsOfNext),
								"storage_asof_next_preview", logHexPreview(storageAsOfNext, 64),
								"storage_asof_next_err", storageAsOfNextErr,
								"storage_latest_len", len(storageLatest),
								"storage_latest_step", storageLatestStep,
								"storage_latest_preview", logHexPreview(storageLatest, 64),
								"storage_latest_err", storageLatestErr,
								"account_probe_addr", accountProbeAddr,
								"account_asof_ok", accountAsOfOK,
								"account_asof_len", len(accountAsOf),
								"account_asof_preview", logHexPreview(accountAsOf, 64),
								"account_asof_err", accountAsOfErr,
								"account_asof_next_ok", accountAsOfNextOK,
								"account_asof_next_len", len(accountAsOfNext),
								"account_asof_next_preview", logHexPreview(accountAsOfNext, 64),
								"account_asof_next_err", accountAsOfNextErr,
								"account_latest_len", len(accountLatest),
								"account_latest_step", accountLatestStep,
								"account_latest_preview", logHexPreview(accountLatest, 64),
								"account_latest_err", accountLatestErr,
							)
						}
					}
				}
			}
		}
	}
persistedAuditDone:
	if debugCommit && c.logger != nil {
		head, err := c.readCurrentHeader()
		if err != nil {
			c.logger.Warn("erigonexec: commit head (persisted) read failed", "err", err)
		} else {
			c.logger.Info("erigonexec: commit head (persisted)", "head", head.Number.Uint64(), "hash", head.Hash())
		}
	}
	return nil
}

func logExecDebugState(tx kv.TemporalTx, blockNum uint64, stage string) {
	var (
		headers  uint64
		bodies   uint64
		senders  uint64
		exec     uint64
		txlookup uint64
		finish   uint64
	)
	var err error
	if headers, err = stages.GetStageProgress(tx, stages.Headers); err != nil {
		gethlog.Warn("erigonexec: stage progress read failed", "stage", stage, "err", err)
		return
	}
	if bodies, err = stages.GetStageProgress(tx, stages.Bodies); err != nil {
		gethlog.Warn("erigonexec: stage progress read failed", "stage", stage, "err", err)
		return
	}
	if senders, err = stages.GetStageProgress(tx, stages.Senders); err != nil {
		gethlog.Warn("erigonexec: stage progress read failed", "stage", stage, "err", err)
		return
	}
	if exec, err = stages.GetStageProgress(tx, stages.Execution); err != nil {
		gethlog.Warn("erigonexec: stage progress read failed", "stage", stage, "err", err)
		return
	}
	if txlookup, err = stages.GetStageProgress(tx, stages.TxLookup); err != nil {
		gethlog.Warn("erigonexec: stage progress read failed", "stage", stage, "err", err)
		return
	}
	if finish, err = stages.GetStageProgress(tx, stages.Finish); err != nil {
		gethlog.Warn("erigonexec: stage progress read failed", "stage", stage, "err", err)
		return
	}

	lastBlock, lastTxNum, err := rawdbv3.TxNums.Last(tx)
	if err != nil {
		gethlog.Warn("erigonexec: txnums last failed", "stage", stage, "err", err)
		return
	}
	maxTxNum, err := rawdbv3.TxNums.Max(tx, blockNum)
	if err != nil {
		gethlog.Warn("erigonexec: txnums max failed", "stage", stage, "block", blockNum, "err", err)
		return
	}
	minTxNum, err := rawdbv3.TxNums.Min(tx, blockNum)
	if err != nil {
		gethlog.Warn("erigonexec: txnums min failed", "stage", stage, "block", blockNum, "err", err)
		return
	}

	doms, err := dbstate.NewSharedDomains(tx, elog.New("component", "erigonexec"))
	if err != nil {
		gethlog.Warn("erigonexec: domains open failed", "stage", stage, "err", err)
		return
	}
	defer doms.Close()

	gethlog.Info(
		"erigonexec: exec state",
		"stage", stage,
		"block", blockNum,
		"headers", headers,
		"bodies", bodies,
		"senders", senders,
		"execution", exec,
		"txlookup", txlookup,
		"finish", finish,
		"txnums_last_block", lastBlock,
		"txnums_last", lastTxNum,
		"txnums_min", minTxNum,
		"txnums_max", maxTxNum,
		"doms_block", doms.BlockNum(),
		"doms_txnum", doms.TxNum(),
	)
}
