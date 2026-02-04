//go:build erigon
// +build erigon

package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/c2h5oh/datasize"
	elog "github.com/erigontech/erigon-lib/log/v3"
	"github.com/erigontech/erigon/arb/ethdb/wasmdb"
	estate "github.com/erigontech/erigon/core/state"
	"github.com/erigontech/erigon/core/vm"
	"github.com/erigontech/erigon/db/datadir"
	"github.com/erigontech/erigon/db/kv"
	"github.com/erigontech/erigon/db/kv/prune"
	"github.com/erigontech/erigon/db/kv/rawdbv3"
	"github.com/erigontech/erigon/db/kv/temporal"
	erawdb "github.com/erigontech/erigon/db/rawdb"
	dbstate "github.com/erigontech/erigon/db/state"
	"github.com/erigontech/erigon/db/wrap"
	"github.com/erigontech/erigon/eth/ethconfig"
	"github.com/erigontech/erigon/execution/chain"
	"github.com/erigontech/erigon/execution/consensus/ethash"
	"github.com/erigontech/erigon/execution/exec3"
	"github.com/erigontech/erigon/execution/stagedsync"
	"github.com/erigontech/erigon/execution/stagedsync/stages"
	etypes "github.com/erigontech/erigon/execution/types"
	"github.com/erigontech/erigon/turbo/services"
	"github.com/erigontech/erigon/turbo/shards"
	gethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	gethrawdb "github.com/ethereum/go-ethereum/core/rawdb"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/rlp"
	gethtrie "github.com/ethereum/go-ethereum/trie"

	"github.com/offchainlabs/nitro/arbos"
	"github.com/offchainlabs/nitro/arbos/arbosState"
	"github.com/offchainlabs/nitro/arbos/arbostypes"
	"github.com/offchainlabs/nitro/statetransfer"
)

const execBatchSize = 64 * datasize.MB

var (
	mdbxMigrateSourceReceiptsDebug                                              = os.Getenv("MDBX_MIGRATE_DEBUG") != ""
	mdbxMigrateSourceReceiptsDebugBlock, mdbxMigrateSourceReceiptsDebugBlockSet = parseEnvUint("MDBX_MIGRATE_DEBUG_BLOCK")
)

var (
	rlpDelayedMessagePrefix        = []byte("e")
	legacyDelayedMessagePrefix     = []byte("d")
	sweepPlainAccountTombstonesEnv = os.Getenv("ERIGON_MDBX_MIGRATE_SWEEP_TOMBSTONES_INIT") != ""
)

func parseEnvUint(name string) (uint64, bool) {
	value := os.Getenv(name)
	if value == "" {
		return 0, false
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		logKV("source_receipts", "status", "invalid_env", "name", name, "value", value, "err", err)
		return 0, false
	}
	return parsed, true
}

func logSourceReceiptsDebug(src ethdb.Database) {
	if !mdbxMigrateSourceReceiptsDebug {
		return
	}
	if !mdbxMigrateSourceReceiptsDebugBlockSet {
		logKV("source_receipts", "status", "skip", "reason", "missing_MDBX_MIGRATE_DEBUG_BLOCK")
		return
	}

	blockNum := mdbxMigrateSourceReceiptsDebugBlock
	blockHash := gethrawdb.ReadCanonicalHash(src, blockNum)
	if blockHash == (gethcommon.Hash{}) {
		logKV("source_receipts", "status", "missing_hash", "block", blockNum)
		return
	}
	header := gethrawdb.ReadHeader(src, blockHash, blockNum)
	if header == nil {
		logKV("source_receipts", "status", "missing_header", "block", blockNum, "hash", blockHash)
		return
	}
	genesisHash := gethrawdb.ReadCanonicalHash(src, 0)
	if genesisHash == (gethcommon.Hash{}) {
		logKV("source_receipts", "status", "missing_genesis_hash", "block", blockNum)
		return
	}
	cfg := gethrawdb.ReadChainConfig(src, genesisHash)
	if cfg == nil {
		logKV("source_receipts", "status", "missing_chain_config", "block", blockNum, "genesis_hash", genesisHash)
		return
	}

	receiptsRLP := gethrawdb.ReadReceiptsRLP(src, blockHash, blockNum)
	receipts := gethrawdb.ReadReceipts(src, blockHash, blockNum, header.Time, cfg)
	receiptsNil := receipts == nil
	receiptCount := len(receipts)
	computedRoot := gethtypes.DeriveSha(gethtypes.Receipts(receipts), gethtrie.NewStackTrie(nil))

	logKV(
		"source_receipts",
		"block", blockNum,
		"hash", blockHash,
		"rlp_len", len(receiptsRLP),
		"receipts", receiptCount,
		"receipts_nil", receiptsNil,
		"header_receipt_root", header.ReceiptHash,
		"computed_root", computedRoot,
		"match", computedRoot == header.ReceiptHash,
	)
}

// sweepPlainAccountTombstones removes obvious tombstone/short account encodings
// from the plain AccountVals table before execution starts (AccountVals include
// an 8-byte prefix), and triggers the
// hashed-domain sweep performed in SharedDomains.NewSharedDomains. Leaving these
// markers in place causes the commitment trie to treat the account as present
// and can produce divergent state roots (e.g. block 13 during migration).
func sweepPlainAccountTombstones(ctx context.Context, db kv.RwDB, logger elog.Logger) error {
	return db.Update(ctx, func(tx kv.RwTx) error {
		const accountValsMinLen = 8 + 4
		c, err := tx.Cursor(kv.TblAccountVals)
		if err != nil {
			return err
		}
		defer c.Close()

		for k, v, err := c.First(); k != nil; k, v, err = c.Next() {
			if err != nil {
				return err
			}
			// AccountVals entries include an 8-byte prefix; shorter rows are tombstones/garbage.
			if len(v) > 0 && len(v) < accountValsMinLen {
				if err := tx.Delete(kv.TblAccountVals, k); err != nil {
					return err
				}
				continue
			}
		}

		if temporalTx, ok := tx.(kv.TemporalTx); ok {
			if _, err := dbstate.NewSharedDomains(temporalTx, logger); err != nil {
				logger.Warn("sweepPlainAccountTombstones: shared domains init failed", "err", err)
			}
		}

		return nil
	})
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

	for _, path := range paths {
		if err := os.MkdirAll(path, 0o755); err != nil {
			return datadir.Dirs{}, fmt.Errorf("create dir %s: %w", path, err)
		}
	}

	return dirs, nil
}

func loadSourceGenesis(src ethdb.Database, chainCfg *chain.Config) (*etypes.Genesis, error) {
	genesisHash := gethrawdb.ReadCanonicalHash(src, 0)
	if genesisHash == (gethcommon.Hash{}) {
		return nil, fmt.Errorf("missing genesis hash in source db")
	}
	cfg := gethrawdb.ReadChainConfig(src, genesisHash)
	if cfg == nil {
		return nil, fmt.Errorf("missing genesis config in source db")
	}
	block := gethrawdb.ReadBlock(src, genesisHash, 0)
	if block == nil {
		return nil, fmt.Errorf("missing genesis block in source db")
	}
	header := block.Header()

	genesis := &core.Genesis{
		Config:        cfg,
		Nonce:         header.Nonce.Uint64(),
		Timestamp:     header.Time,
		ExtraData:     header.Extra,
		GasLimit:      header.GasLimit,
		Difficulty:    header.Difficulty,
		Mixhash:       header.MixDigest,
		Coinbase:      header.Coinbase,
		Number:        header.Number.Uint64(),
		GasUsed:       header.GasUsed,
		ParentHash:    header.ParentHash,
		BaseFee:       header.BaseFee,
		BlobGasUsed:   header.BlobGasUsed,
		ExcessBlobGas: header.ExcessBlobGas,
	}

	stateSpec := gethrawdb.ReadGenesisStateSpec(src, genesisHash)
	if len(stateSpec) != 0 {
		var alloc gethtypes.GenesisAlloc
		if err := alloc.UnmarshalJSON(stateSpec); err != nil {
			return nil, fmt.Errorf("unmarshal genesis state: %w", err)
		}
		genesis.Alloc = alloc
	} else {
		genesis.Alloc = gethtypes.GenesisAlloc{}
	}

	data, err := json.Marshal(genesis)
	if err != nil {
		return nil, fmt.Errorf("marshal genesis: %w", err)
	}
	var out etypes.Genesis
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("unmarshal genesis: %w", err)
	}
	if out.Alloc == nil {
		out.Alloc = etypes.GenesisAlloc{}
	}
	if chainCfg != nil {
		out.Config = chainCfg
	}
	return &out, nil
}

func loadInitMessage(srcArb ethdb.Database) (*arbostypes.ParsedInitMessage, error) {
	msg, err := readDelayedMessage(srcArb, rlpDelayedMessagePrefix)
	if err != nil || msg == nil {
		msg, err = readDelayedMessage(srcArb, legacyDelayedMessagePrefix)
		if err != nil {
			return nil, err
		}
	}
	if msg == nil {
		return nil, fmt.Errorf("init message not found in arbitrumdata")
	}
	initMsg, err := msg.ParseInitMessage()
	if err != nil {
		return nil, fmt.Errorf("parse init message: %w", err)
	}
	return initMsg, nil
}

func readDelayedMessage(db ethdb.Database, prefix []byte) (*arbostypes.L1IncomingMessage, error) {
	key := append([]byte{}, prefix...)
	key = append(key, uint64ToKey(0)...)
	data, err := db.Get(key)
	if err != nil {
		return nil, err
	}
	if len(data) < 32 {
		return nil, fmt.Errorf("delayed message entry too short")
	}
	var msg *arbostypes.L1IncomingMessage
	if err := rlp.DecodeBytes(data[32:], &msg); err != nil {
		return nil, err
	}
	return msg, nil
}

func uint64ToKey(x uint64) []byte {
	out := make([]byte, 8)
	binary.BigEndian.PutUint64(out, x)
	return out
}

func resolveArbitrumInitMessage(srcArbPath string, chainCfg *chain.Config) (*chain.Config, *arbostypes.ParsedInitMessage, error) {
	exec3.SetArbitrumInitMessage(nil)
	if gethrawdb.PreexistingDatabase(srcArbPath) == "" {
		return chainCfg, nil, nil
	}

	srcArb, err := openSourceChainDB(srcArbPath)
	if err != nil {
		return chainCfg, nil, fmt.Errorf("open source arbitrumdata: %w", err)
	}
	defer srcArb.Close()

	initMsg, err := loadInitMessage(srcArb)
	if err != nil {
		return chainCfg, nil, fmt.Errorf("load init message: %w", err)
	}
	if initMsg.ChainConfig == nil {
		logKV("init_message", "chain_id", initMsg.ChainId, "has_chain_config", false)
	} else {
		logKV("init_message",
			"chain_id", initMsg.ChainId,
			"has_chain_config", true,
			"arbos_enabled", initMsg.ChainConfig.IsArbitrum(),
			"init_arbos_version", initMsg.ChainConfig.ArbitrumChainParams.InitialArbOSVersion,
			"genesis_block", initMsg.ChainConfig.ArbitrumChainParams.GenesisBlockNum,
		)
	}
	exec3.SetArbitrumInitMessage(initMsg)

	if initMsg.ChainConfig != nil {
		data, err := json.Marshal(initMsg.ChainConfig)
		if err != nil {
			return chainCfg, initMsg, fmt.Errorf("marshal init chain config: %w", err)
		}
		var cfg chain.Config
		if err := json.Unmarshal(data, &cfg); err != nil {
			return chainCfg, initMsg, fmt.Errorf("unmarshal init chain config: %w", err)
		}
		if chainCfg == nil {
			chainCfg = &cfg
		} else if cfg.IsArbitrum() {
			chainCfg.ArbitrumChainParams = cfg.ArbitrumChainParams
			if chainCfg.ChainID == nil && cfg.ChainID != nil {
				chainCfg.ChainID = cfg.ChainID
			}
			if chainCfg.ChainName == "" && cfg.ChainName != "" {
				chainCfg.ChainName = cfg.ChainName
			}
		}
	}

	if chainCfg != nil {
		logKV("chain_config",
			"arbos_enabled", chainCfg.IsArbitrum(),
			"init_arbos_version", chainCfg.ArbitrumChainParams.InitialArbOSVersion,
			"genesis_block", chainCfg.ArbitrumChainParams.GenesisBlockNum,
			"chain_id", chainCfg.ChainID,
			"chain_name", chainCfg.ChainName,
		)
	} else {
		logKV("chain_config", "arbos_enabled", false, "init_arbos_version", 0, "genesis_block", 0)
	}

	return chainCfg, initMsg, nil
}

func runExecutionStage(ctx context.Context, srcChain ethdb.Database, dst kv.RwDB, chainCfg *chain.Config, initMsg *arbostypes.ParsedInitMessage, blockReader services.FullBlockReader, dest string, toBlock uint64, workers int, logger elog.Logger) error {
	dirs, err := buildExecDirs(dest)
	if err != nil {
		return err
	}

	agg, err := dbstate.New(dirs).Logger(logger).GenSaltIfNeed(true).Open(ctx, dst)
	if err != nil {
		return fmt.Errorf("open state aggregator: %w", err)
	}
	if err := agg.OpenFolder(); err != nil {
		agg.Close()
		return fmt.Errorf("open state snapshots: %w", err)
	}
	defer agg.Close()

	execDB, err := temporal.New(dst, agg)
	if err != nil {
		return fmt.Errorf("open temporal db: %w", err)
	}

	genesis, err := loadSourceGenesis(srcChain, chainCfg)
	if err != nil {
		return fmt.Errorf("load genesis: %w", err)
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
		return fmt.Errorf("write genesis: %w", err)
	}
	exec3.SetArbitrumInitMessage(initMsg)
	if err := ensureArbosInitialized(ctx, execDB, chainCfg, initMsg, genesis.Timestamp, expectedGenesisRoot, logger, srcChain, toBlock); err != nil {
		return fmt.Errorf("init arbos: %w", err)
	}
	if sweepPlainAccountTombstonesEnv {
		if err := sweepPlainAccountTombstones(ctx, execDB, logger); err != nil {
			return fmt.Errorf("sweep tombstones: %w", err)
		}
	}

	engine := ethash.NewFaker()
	defer engine.Close()

	syncCfg := ethconfig.Defaults.Sync
	if workers > 0 {
		syncCfg.ExecWorkerCount = workers
	}
	// Persist receipts so execution can verify receipt roots during migration.
	syncCfg.PersistReceiptsCacheV2 = true

	// Scope the wasm DB lifetime to this stage to avoid locking during copy.
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
		Description: "Execute blocks",
		Forward: func(badBlockUnwind bool, s *stagedsync.StageState, u stagedsync.Unwinder, txc wrap.TxContainer, log elog.Logger) error {
			return stagedsync.SpawnExecuteBlocksStage(s, u, txc, toBlock, ctx, execCfg, log)
		},
		Unwind: func(u *stagedsync.UnwindState, s *stagedsync.StageState, txc wrap.TxContainer, log elog.Logger) error {
			return stagedsync.UnwindExecutionStage(u, s, txc, ctx, execCfg, log)
		},
		Prune: func(p *stagedsync.PruneState, tx kv.RwTx, log elog.Logger) error {
			return stagedsync.PruneExecutionStage(p, tx, execCfg, ctx, log)
		},
	}

	sync := stagedsync.New(syncCfg, []*stagedsync.Stage{stage}, stagedsync.DefaultUnwindOrder, stagedsync.DefaultPruneOrder, logger, stages.ModeApplyingBlocks)
	_, err = sync.Run(execDB, wrap.NewTxContainer(nil, nil), false, false)
	return err
}

func runTxLookupStage(ctx context.Context, dst kv.RwDB, chainCfg *chain.Config, blockReader services.FullBlockReader, tmpDir string, toBlock uint64, logger elog.Logger) error {
	txCfg := stagedsync.StageTxLookupCfg(dst, prune.DefaultMode, tmpDir, chainCfg.Bor, blockReader)
	finishCfg := stagedsync.StageFinishCfg(dst, tmpDir, nil)

	txStage := &stagedsync.Stage{
		ID:          stages.TxLookup,
		Description: "Generate txn lookup index",
		Forward: func(badBlockUnwind bool, s *stagedsync.StageState, u stagedsync.Unwinder, txc wrap.TxContainer, log elog.Logger) error {
			return stagedsync.SpawnTxLookup(s, txc.Tx, toBlock, txCfg, ctx, log)
		},
		Unwind: func(u *stagedsync.UnwindState, s *stagedsync.StageState, txc wrap.TxContainer, log elog.Logger) error {
			return stagedsync.UnwindTxLookup(u, s, txc.Tx, txCfg, ctx, log)
		},
		Prune: func(p *stagedsync.PruneState, tx kv.RwTx, log elog.Logger) error {
			return stagedsync.PruneTxLookup(p, tx, txCfg, ctx, log)
		},
	}

	finishStage := &stagedsync.Stage{
		ID:          stages.Finish,
		Description: "Final: update current block for the RPC API",
		Forward: func(badBlockUnwind bool, s *stagedsync.StageState, _ stagedsync.Unwinder, txc wrap.TxContainer, log elog.Logger) error {
			return stagedsync.FinishForward(s, txc.Tx, finishCfg)
		},
		Unwind: func(u *stagedsync.UnwindState, s *stagedsync.StageState, txc wrap.TxContainer, log elog.Logger) error {
			return stagedsync.UnwindFinish(u, txc.Tx, finishCfg, ctx)
		},
		Prune: func(p *stagedsync.PruneState, tx kv.RwTx, log elog.Logger) error {
			return stagedsync.PruneFinish(p, tx, finishCfg, ctx)
		},
	}

	syncCfg := ethconfig.Defaults.Sync
	sync := stagedsync.New(syncCfg, []*stagedsync.Stage{txStage, finishStage}, stagedsync.DefaultUnwindOrder, stagedsync.DefaultPruneOrder, logger, stages.ModeApplyingBlocks)
	_, err := sync.Run(dst, wrap.NewTxContainer(nil, nil), false, false)
	return err
}

func ensureArbosInitialized(ctx context.Context, db kv.RwDB, chainCfg *chain.Config, initMsg *arbostypes.ParsedInitMessage, timestamp uint64, expectedGenesisRoot *gethcommon.Hash, logger elog.Logger, srcChain ethdb.Database, addrTableBlock uint64) error {
	chainID := "<nil>"
	if chainCfg != nil && chainCfg.ChainID != nil {
		chainID = chainCfg.ChainID.String()
	}
	if chainCfg == nil {
		logKV("arbos_init", "status", "skip", "reason", "nil_chain_config")
		return nil
	}
	if !chainCfg.IsArbitrum() {
		logKV("arbos_init", "status", "skip", "reason", "arbos_disabled", "chain_id", chainID)
		return nil
	}
	logKV(
		"arbos_init",
		"status", "prepare",
		"chain_id", chainID,
		"arbos_enabled", chainCfg.ArbitrumChainParams.EnableArbOS,
		"init_arbos_version", chainCfg.ArbitrumChainParams.InitialArbOSVersion,
		"genesis_block", chainCfg.ArbitrumChainParams.GenesisBlockNum,
		"init_msg_present", initMsg != nil,
	)
	if initMsg == nil {
		logKV("arbos_init", "status", "build_init_message", "chain_id", chainID)
		var err error
		initMsg, err = arbos.BuildInitMessageFromChainConfig(chainCfg)
		if err != nil {
			return fmt.Errorf("build init message: %w", err)
		}
	}

	temporalDB, ok := db.(kv.TemporalRwDB)
	if !ok {
		logKV("arbos_init", "status", "error", "err", "missing temporal db support for arbos init", "chain_id", chainID)
		return errors.New("missing temporal db support for arbos init")
	}

	return temporalDB.UpdateTemporal(ctx, func(tx kv.TemporalRwTx) error {
		domains, err := dbstate.NewSharedDomains(tx, logger)
		if err != nil {
			return err
		}
		defer domains.Close()

		genesisBlockNum := chainCfg.ArbitrumChainParams.GenesisBlockNum
		txNum, err := rawdbv3.TxNums.Max(tx, genesisBlockNum)
		if err != nil {
			return err
		}
		if txNum == 0 && genesisBlockNum != 0 {
			minTxNum, err := rawdbv3.TxNums.Min(tx, genesisBlockNum)
			if err != nil {
				return err
			}
			txNum = minTxNum
		}
		domains.SetTxNum(txNum)
		domains.SetBlockNum(genesisBlockNum)

		reader := estate.NewReaderV3(domains.AsGetter(tx))
		reader.SetTxNum(txNum)
		ibs := estate.New(reader)
		ibsArb := estate.NewArbitrum(ibs)

		initData := statetransfer.ArbosInitializationInfo{NextBlockNumber: genesisBlockNum}
		if initMsg != nil && initMsg.ChainConfig != nil {
			initData.ChainOwner = initMsg.ChainConfig.ArbitrumChainParams.InitialChainOwner
		}
		if initData.ChainOwner == (gethcommon.Address{}) && chainCfg != nil {
			initData.ChainOwner = gethcommon.BytesToAddress(chainCfg.ArbitrumChainParams.InitialChainOwner.Bytes())
		}
		if err := maybeFillAccountsFromSource(&initData, srcChain, genesisBlockNum); err != nil {
			return err
		}
		if err := maybeFillAddressTableFromSource(&initData, srcChain, addrTableBlock); err != nil {
			return err
		}
		initReader := statetransfer.NewMemoryInitDataReader(&initData)
		root, err := arbos.InitializeArbosInDatabase(ibsArb, domains, domains.AsPutDel(tx), initReader, chainCfg, initMsg, timestamp, 100000)
		if errors.Is(err, arbosState.ErrAlreadyInitialized) {
			logKV("arbos_init", "status", "already_initialized", "chain_id", chainID, "genesis_block", genesisBlockNum)
			return nil
		}
		if err != nil {
			logKV("arbos_init", "status", "error", "err", err, "chain_id", chainID, "genesis_block", genesisBlockNum)
			return err
		}
		if expectedGenesisRoot != nil {
			match := bytes.Equal(root.Bytes(), expectedGenesisRoot.Bytes())
			logKV(
				"arbos_init_compare",
				"expected_root", *expectedGenesisRoot,
				"computed_root", root,
				"match", match,
			)
		}
		if err := domains.Flush(ctx, tx); err != nil {
			logKV("arbos_init", "status", "error", "err", err, "chain_id", chainID, "genesis_block", genesisBlockNum)
			return err
		}
		logger.Info("ArbOS initialized", "stateRoot", root)
		logKV("arbos_init", "status", "initialized", "state_root", root, "chain_id", chainID, "genesis_block", genesisBlockNum)
		return nil
	})
}
