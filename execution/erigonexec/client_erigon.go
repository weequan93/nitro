//go:build erigon
// +build erigon

package erigonexec

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	ecommon "github.com/erigontech/erigon-lib/common"
	elog "github.com/erigontech/erigon-lib/log/v3"
	"github.com/erigontech/erigon/arb/ethdb/wasmdb"
	"github.com/erigontech/erigon/core/vm"
	"github.com/erigontech/erigon/db/kv"
	"github.com/erigontech/erigon/db/kv/dbcfg"
	"github.com/erigontech/erigon/db/kv/prune"
	"github.com/erigontech/erigon/db/kv/rawdbv3"
	"github.com/erigontech/erigon/db/kv/temporal"
	erawdb "github.com/erigontech/erigon/db/rawdb"
	"github.com/erigontech/erigon/db/snapshotsync/freezeblocks"
	dbstate "github.com/erigontech/erigon/db/state"
	"github.com/erigontech/erigon/db/wrap"
	"github.com/erigontech/erigon/eth/ethconfig"
	"github.com/erigontech/erigon/execution/chain"
	"github.com/erigontech/erigon/execution/consensus/ethash"
	"github.com/erigontech/erigon/execution/stagedsync"
	"github.com/erigontech/erigon/execution/stagedsync/stages"
	etypes "github.com/erigontech/erigon/execution/types"
	"github.com/erigontech/erigon/turbo/services"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/node"

	"github.com/offchainlabs/nitro/arbos/arbostypes"
	"github.com/offchainlabs/nitro/arbutil"
	"github.com/offchainlabs/nitro/execution"
	"github.com/offchainlabs/nitro/execution/erigonexec/kvdb"
	"github.com/offchainlabs/nitro/execution/gethexec"
)

var errForwardUnavailable = errors.New("erigonexec: tx publisher not configured")

type Client struct {
	cfg                Config
	logger             elog.Logger
	chainDB            kv.RwDB
	temporalAgg        *dbstate.Aggregator
	arbDB              ethdb.Database
	rawWasmDB          kv.RwDB
	wasmDB             ethdb.Database
	wasmIface          wasmdb.WasmIface
	blockReader        services.FullBlockReader
	chainConfig        *chain.Config
	genesisHash        ecommon.Hash
	blockMetadataLimit uint64
	consensus          execution.FullConsensusClient
	closeOnce          sync.Once
	blocksMu           sync.Mutex
	metricsMu          sync.Mutex
	metricsDone        chan struct{}
	metricsStop        context.CancelFunc
	lastPrefault       uint64
	prefaultInit       bool
	lastReadTxWaits    uint64
	lastTxnRestarts    uint64
	cachedL1PriceData  *L1PriceData
	pruneMode          prune.Mode
	pruneModeLabel     string
	txPublisherMu      sync.Mutex
	txPublisherCtl     TxPublisherController
	txPublisherActive  gethexec.TransactionPublisher
	txPublisherPaused  bool
}

func New(ctx context.Context,
	stack *node.Node,
	l2BlockChain *core.BlockChain,
	l1client *ethclient.Client,
	cfg Config) (execution.FullExecutionClient, error) {
	if cfg.ChainDir == "" {
		return nil, errors.New("erigonexec: ChainDir is required")
	}
	logger := elog.New("component", "erigonexec")
	pruneMode, pruneModeLabel, err := parsePruneMode(cfg.PruneMode)
	if err != nil {
		return nil, fmt.Errorf("erigonexec: parse prune mode: %w", err)
	}

	chainPath := filepath.Join(cfg.ChainDir, "l2chaindata")
	chainDB, err := openMdbxDB(dbcfg.ChainDB, chainPath, cfg.Mdbx, logger, nil)
	if err != nil {
		return nil, fmt.Errorf("open mdbx chain db: %w", err)
	}

	dirs, err := buildExecDirs(cfg.ChainDir)
	if err != nil {
		chainDB.Close()
		return nil, fmt.Errorf("erigonexec: build dirs: %w", err)
	}
	agg, err := dbstate.New(dirs).Logger(logger).SanityOldNaming().GenSaltIfNeed(true).Open(context.Background(), chainDB)
	if err != nil {
		chainDB.Close()
		return nil, fmt.Errorf("erigonexec: open temporal aggregator: %w", err)
	}
	temporalDB, err := temporal.New(chainDB, agg)
	if err != nil {
		agg.Close()
		chainDB.Close()
		return nil, fmt.Errorf("erigonexec: open temporal db: %w", err)
	}
	chainDB = temporalDB

	arbDB, err := OpenArbDB(cfg.ChainDir, cfg.Mdbx)
	if err != nil {
		agg.Close()
		chainDB.Close()
		return nil, fmt.Errorf("open mdbx arb db: %w", err)
	}

	wasmPath := filepath.Join(cfg.ChainDir, "wasm")
	rawWasmDB, err := openMdbxDB(dbcfg.ArbWasmDB, wasmPath, cfg.Mdbx, logger, wasmTablesCfg())
	if err != nil {
		arbDB.Close()
		agg.Close()
		chainDB.Close()
		return nil, fmt.Errorf("open mdbx wasm db: %w", err)
	}
	wasmDB := kvdb.New(rawWasmDB, BucketArbWasm)
	wasmIface := wasmdb.WrapDatabaseWithWasm(rawWasmDB, []wasmdb.WasmTarget{wasmdb.LocalTarget()})

	chainCfg, genesisHash, err := readChainConfig(ctx, chainDB)
	if err != nil {
		wasmDB.Close()
		arbDB.Close()
		agg.Close()
		chainDB.Close()
		return nil, err
	}
	if cfg.ExpectedChainID != nil && chainCfg != nil && chainCfg.ChainID != nil {
		if chainCfg.ChainID.Cmp(cfg.ExpectedChainID) != 0 {
			wasmDB.Close()
			arbDB.Close()
			agg.Close()
			chainDB.Close()
			return nil, fmt.Errorf("erigonexec: chain id mismatch db=%v config=%v", chainCfg.ChainID, cfg.ExpectedChainID)
		}
	}

	chainName := cfg.ChainName
	if chainName == "" && chainCfg != nil {
		chainName = chainCfg.ChainName
	}
	blockReader, err := newBlockReader(cfg.ChainDir, chainName, logger)
	if err != nil {
		wasmDB.Close()
		arbDB.Close()
		agg.Close()
		chainDB.Close()
		return nil, err
	}

	return &Client{
		cfg:                cfg,
		logger:             logger,
		chainDB:            chainDB,
		temporalAgg:        agg,
		arbDB:              arbDB,
		rawWasmDB:          rawWasmDB,
		wasmDB:             wasmDB,
		wasmIface:          wasmIface,
		blockReader:        blockReader,
		chainConfig:        chainCfg,
		genesisHash:        genesisHash,
		blockMetadataLimit: cfg.BlockMetadataApiBlocksLimit,
		cachedL1PriceData:  NewL1PriceData(),
		pruneMode:          pruneMode,
		pruneModeLabel:     pruneModeLabel,
	}, nil
}

func (c *Client) Start(ctx context.Context) error {
	if err := c.runStartupChecks(ctx); err != nil {
		return err
	}
	c.startMdbxMetrics()
	return nil
}

func (c *Client) ArbDB() ethdb.Database {
	return c.arbDB
}

func (c *Client) wasmDBForCtx(ctx context.Context) wasmdb.WasmIface {
	if c.wasmIface != nil {
		return c.wasmIface
	}
	if c.rawWasmDB != nil {
		c.wasmIface = wasmdb.WrapDatabaseWithWasm(c.rawWasmDB, []wasmdb.WasmTarget{wasmdb.LocalTarget()})
		return c.wasmIface
	}
	return wasmdb.OpenArbitrumWasmDB(ctx, filepath.Join(c.cfg.ChainDir, "wasm"))
}

func (c *Client) StopAndWait() {
	c.closeOnce.Do(func() {
		c.stopMdbxMetrics()
		if c.wasmDB != nil {
			_ = c.wasmDB.Close()
		}
		if c.arbDB != nil {
			_ = c.arbDB.Close()
		}
		if c.temporalAgg != nil {
			c.temporalAgg.Close()
		}
		if c.chainDB != nil {
			c.chainDB.Close()
		}
	})
}

func (c *Client) Maintenance() error {
	if c.arbDB != nil {
		if err := c.arbDB.Compact(nil, nil); err != nil {
			return err
		}
	}
	if c.wasmDB != nil {
		if err := c.wasmDB.Compact(nil, nil); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) ArbOSVersionForMessageNumber(messageNum arbutil.MessageIndex) (uint64, error) {
	header, err := c.readHeaderByNumber(c.MessageIndexToBlockNumber(messageNum))
	if err != nil {
		return 0, err
	}
	extra := etypes.DeserializeHeaderExtraInformation(header)
	return extra.ArbOSFormatVersion, nil
}

func (c *Client) DigestMessage(num arbutil.MessageIndex, msg *arbostypes.MessageWithMetadata, msgForPrefetch *arbostypes.MessageWithMetadata) (*execution.MessageResult, error) {
	c.blocksMu.Lock()
	defer c.blocksMu.Unlock()
	return c.digestMessageLocked(num, msg, msgForPrefetch)
}

func (c *Client) digestMessageLocked(num arbutil.MessageIndex, msg *arbostypes.MessageWithMetadata, msgForPrefetch *arbostypes.MessageWithMetadata) (*execution.MessageResult, error) {
	_ = msgForPrefetch
	header, err := c.readCurrentHeader()
	if err != nil {
		return nil, err
	}
	curMsg, err := c.BlockNumberToMessageIndex(header.Number.Uint64())
	if err != nil {
		return nil, err
	}
	if curMsg+1 != num {
		return nil, fmt.Errorf("wrong message number in digest got %d expected %d", num, curMsg+1)
	}
	block, receipts, err := c.buildBlockFromMessage(context.Background(), msg, header)
	if err != nil {
		return nil, err
	}
	if err := c.commitBlock(context.Background(), block); err != nil {
		return nil, err
	}
	c.cacheL1PriceDataOfMsg(num, receipts, block, false)
	extra := etypes.DeserializeHeaderExtraInformation(block.Header())
	return &execution.MessageResult{
		BlockHash: toGethHash(block.Hash()),
		SendRoot:  toGethHash(extra.SendRoot),
	}, nil
}

func (c *Client) Reorg(count arbutil.MessageIndex, newMessages []arbostypes.MessageWithMetadataAndBlockInfo, oldMessages []*arbostypes.MessageWithMetadata) ([]*execution.MessageResult, error) {
	_ = oldMessages
	if count == 0 {
		return nil, errors.New("erigonexec: cannot reorg out genesis")
	}
	c.blocksMu.Lock()
	defer c.blocksMu.Unlock()

	ctx := context.Background()
	head, err := c.readCurrentHeader()
	if err != nil {
		return nil, err
	}
	targetBlockNum := c.MessageIndexToBlockNumber(count - 1)
	if targetBlockNum > head.Number.Uint64() {
		return nil, fmt.Errorf("erigonexec: reorg target %d beyond head %d", targetBlockNum, head.Number.Uint64())
	}

	temporalDB, ok := c.chainDB.(kv.TemporalRwDB)
	if !ok {
		return nil, errors.New("erigonexec: chain db missing temporal write support")
	}
	tx, err := temporalDB.BeginTemporalRw(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	targetHeader := erawdb.ReadHeaderByNumber(tx, targetBlockNum)
	if targetHeader == nil {
		return nil, fmt.Errorf("erigonexec: reorg target header not found block=%d", targetBlockNum)
	}

	execProgress, err := stages.GetStageProgress(tx, stages.Execution)
	if err != nil {
		return nil, err
	}
	sendersProgress, err := stages.GetStageProgress(tx, stages.Senders)
	if err != nil {
		return nil, err
	}
	txLookupProgress, err := stages.GetStageProgress(tx, stages.TxLookup)
	if err != nil {
		return nil, err
	}
	finishProgress, err := stages.GetStageProgress(tx, stages.Finish)
	if err != nil {
		return nil, err
	}

	genesis, err := erawdb.ReadGenesis(tx)
	if err != nil {
		return nil, err
	}
	if genesis == nil {
		return nil, errors.New("erigonexec: genesis not found in chain db")
	}

	dirs, err := buildExecDirs(c.cfg.ChainDir)
	if err != nil {
		return nil, err
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
	txLookupCfg := stagedsync.StageTxLookupCfg(c.chainDB, c.pruneMode, dirs.Tmp, c.chainConfig.Bor, c.blockReader)
	finishCfg := stagedsync.StageFinishCfg(c.chainDB, dirs.Tmp, nil)

	if finishProgress > targetBlockNum {
		unwind := &stagedsync.UnwindState{ID: stages.Finish, UnwindPoint: targetBlockNum, CurrentBlockNumber: finishProgress}
		if err := stagedsync.UnwindFinish(unwind, tx, finishCfg, ctx); err != nil {
			return nil, err
		}
	}
	if txLookupProgress > targetBlockNum {
		stage := &stagedsync.StageState{ID: stages.TxLookup, BlockNumber: txLookupProgress}
		unwind := &stagedsync.UnwindState{ID: stages.TxLookup, UnwindPoint: targetBlockNum, CurrentBlockNumber: txLookupProgress}
		if err := stagedsync.UnwindTxLookup(unwind, stage, tx, txLookupCfg, ctx, c.logger); err != nil {
			return nil, err
		}
	}
	if execProgress > targetBlockNum {
		stage := &stagedsync.StageState{ID: stages.Execution, BlockNumber: execProgress}
		unwind := &stagedsync.UnwindState{ID: stages.Execution, UnwindPoint: targetBlockNum, CurrentBlockNumber: execProgress}
		txc := wrap.NewTxContainer(tx, nil)
		if err := stagedsync.UnwindExecutionStage(unwind, stage, txc, ctx, execCfg, c.logger); err != nil {
			return nil, err
		}
	}
	if sendersProgress > targetBlockNum {
		unwind := &stagedsync.UnwindState{ID: stages.Senders, UnwindPoint: targetBlockNum, CurrentBlockNumber: sendersProgress}
		if err := stagedsync.UnwindSendersStage(unwind, tx, sendersCfg, ctx); err != nil {
			return nil, err
		}
	}

	if err := erawdb.TruncateCanonicalHash(tx, targetBlockNum+1, false); err != nil {
		return nil, fmt.Errorf("truncate canonical hash: %w", err)
	}
	if err := erawdb.TruncateTd(tx, targetBlockNum+1); err != nil {
		return nil, fmt.Errorf("truncate td: %w", err)
	}
	if err := rawdbv3.TxNums.Truncate(tx, targetBlockNum+1); err != nil {
		return nil, fmt.Errorf("truncate tx nums: %w", err)
	}
	if err := erawdb.WriteHeadHeaderHash(tx, targetHeader.Hash()); err != nil {
		return nil, err
	}
	erawdb.WriteHeadBlockHash(tx, targetHeader.Hash())

	if err := stages.SaveStageProgress(tx, stages.Headers, targetBlockNum); err != nil {
		return nil, err
	}
	if err := stages.SaveStageProgress(tx, stages.BlockHashes, targetBlockNum); err != nil {
		return nil, err
	}
	if err := stages.SaveStageProgress(tx, stages.Bodies, targetBlockNum); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	results := make([]*execution.MessageResult, 0, len(newMessages))
	for i := range newMessages {
		var prefetch *arbostypes.MessageWithMetadata
		if i+1 < len(newMessages) {
			prefetch = &newMessages[i+1].MessageWithMeta
		}
		msgResult, err := c.digestMessageLocked(count+arbutil.MessageIndex(i), &newMessages[i].MessageWithMeta, prefetch)
		if err != nil {
			return nil, err
		}
		results = append(results, msgResult)
	}
	return results, nil
}

func (c *Client) HeadMessageNumber() (arbutil.MessageIndex, error) {
	header, err := c.readCurrentHeader()
	if err != nil {
		return 0, err
	}
	return c.BlockNumberToMessageIndex(header.Number.Uint64())
}

func (c *Client) HeadMessageNumberSync(t *testing.T) (arbutil.MessageIndex, error) {
	_ = t
	c.blocksMu.Lock()
	defer c.blocksMu.Unlock()
	return c.HeadMessageNumber()
}

func (c *Client) ResultAtPos(pos arbutil.MessageIndex) (*execution.MessageResult, error) {
	header, err := c.readHeaderByNumber(c.MessageIndexToBlockNumber(pos))
	if err != nil {
		return nil, err
	}
	extra := etypes.DeserializeHeaderExtraInformation(header)
	return &execution.MessageResult{
		BlockHash: toGethHash(header.Hash()),
		SendRoot:  toGethHash(extra.SendRoot),
	}, nil
}

func (c *Client) MessageIndexToBlockNumber(messageNum arbutil.MessageIndex) uint64 {
	return uint64(messageNum) + c.chainConfig.ArbitrumChainParams.GenesisBlockNum
}

func (c *Client) BlockNumberToMessageIndex(blockNum uint64) (arbutil.MessageIndex, error) {
	genesis := c.chainConfig.ArbitrumChainParams.GenesisBlockNum
	if blockNum < genesis {
		return 0, fmt.Errorf("blockNum %d < genesis %d", blockNum, genesis)
	}
	return arbutil.MessageIndex(blockNum - genesis), nil
}

func (c *Client) RecordBlockCreation(ctx context.Context, pos arbutil.MessageIndex, msg *arbostypes.MessageWithMetadata) (*execution.RecordResult, error) {
	return c.recordBlockCreation(ctx, pos, msg)
}

func (c *Client) MarkValid(pos arbutil.MessageIndex, resultHash common.Hash) {
	if c.chainDB == nil {
		return
	}
	blockNum := c.MessageIndexToBlockNumber(pos)
	err := c.chainDB.View(context.Background(), func(tx kv.Tx) error {
		hash, err := erawdb.ReadCanonicalHash(tx, blockNum)
		if err != nil {
			return err
		}
		if hash == (ecommon.Hash{}) {
			return nil
		}
		if toGethHash(hash) != resultHash {
			if c.logger != nil {
				c.logger.Warn("erigonexec: markvalid hash not canonical", "pos", pos, "result", resultHash, "canonical", toGethHash(hash))
			}
		}
		return nil
	})
	if err != nil && c.logger != nil {
		c.logger.Warn("erigonexec: markvalid failed", "pos", pos, "err", err)
	}
}

func (c *Client) PrepareForRecord(ctx context.Context, start, end arbutil.MessageIndex) error {
	return c.prepareForRecord(ctx, start, end)
}

func (c *Client) Pause() {
	if seq := c.currentSequencer(); seq != nil {
		seq.Pause()
		return
	}
	c.txPublisherMu.Lock()
	ctl := c.txPublisherCtl
	if ctl == nil || c.txPublisherPaused {
		c.txPublisherMu.Unlock()
		return
	}
	c.txPublisherPaused = true
	c.txPublisherMu.Unlock()
	if err := ctl.Swap(gethexec.NewTxDropper()); err != nil && c.logger != nil {
		c.logger.Warn("erigonexec: failed to pause tx publisher", "err", err)
	}
}

func (c *Client) Activate() {
	if seq := c.currentSequencer(); seq != nil {
		seq.Activate()
		return
	}
	c.txPublisherMu.Lock()
	ctl := c.txPublisherCtl
	active := c.txPublisherActive
	if ctl == nil || !c.txPublisherPaused {
		c.txPublisherMu.Unlock()
		return
	}
	c.txPublisherPaused = false
	c.txPublisherMu.Unlock()
	if active == nil {
		return
	}
	if err := ctl.Swap(active); err != nil && c.logger != nil {
		c.logger.Warn("erigonexec: failed to activate tx publisher", "err", err)
	}
}

func (c *Client) ForwardTo(url string) error {
	if url == "" {
		return errors.New("erigonexec: forwarding target is empty")
	}
	if seq := c.currentSequencer(); seq != nil {
		return seq.ForwardTo(url)
	}
	c.txPublisherMu.Lock()
	ctl := c.txPublisherCtl
	paused := c.txPublisherPaused
	c.txPublisherMu.Unlock()
	if ctl == nil {
		if c.logger != nil {
			c.logger.Warn("erigonexec: forwarding target update ignored; tx publisher not configured", "url", url)
		}
		return errForwardUnavailable
	}
	forwarder := gethexec.NewForwarder([]string{url}, &c.cfg.Forwarder)
	c.txPublisherMu.Lock()
	c.txPublisherActive = forwarder
	c.txPublisherMu.Unlock()
	if paused {
		return nil
	}
	if err := ctl.Swap(forwarder); err != nil {
		if c.logger != nil {
			c.logger.Warn("erigonexec: failed to swap tx publisher", "err", err, "url", url)
		}
		return err
	}
	return nil
}

func (c *Client) currentSequencer() *Sequencer {
	c.txPublisherMu.Lock()
	publisher := c.txPublisherActive
	c.txPublisherMu.Unlock()
	if publisher == nil {
		return nil
	}
	if seq, ok := publisher.(*Sequencer); ok {
		return seq
	}
	if ctl, ok := publisher.(interface {
		Current() gethexec.TransactionPublisher
	}); ok {
		if seq, ok := ctl.Current().(*Sequencer); ok {
			return seq
		}
	}
	return nil
}

func (c *Client) SequenceDelayedMessage(message *arbostypes.L1IncomingMessage, delayedSeqNum uint64) error {
	if c.consensus == nil {
		return errors.New("erigonexec: consensus client not configured")
	}
	c.blocksMu.Lock()
	defer c.blocksMu.Unlock()
	header, err := c.readCurrentHeader()
	if err != nil {
		return err
	}
	expectedDelayed := header.Nonce.Uint64()
	if expectedDelayed != delayedSeqNum {
		return fmt.Errorf("wrong delayed message sequenced got %d expected %d", delayedSeqNum, expectedDelayed)
	}
	pos, err := c.BlockNumberToMessageIndex(header.Number.Uint64() + 1)
	if err != nil {
		return err
	}
	messageWithMeta := arbostypes.MessageWithMetadata{
		Message:             message,
		DelayedMessagesRead: delayedSeqNum + 1,
	}
	block, receipts, err := c.buildBlockFromMessage(context.Background(), &messageWithMeta, header)
	if err != nil {
		return err
	}
	extra := etypes.DeserializeHeaderExtraInformation(block.Header())
	msgResult := execution.MessageResult{
		BlockHash: toGethHash(block.Hash()),
		SendRoot:  toGethHash(extra.SendRoot),
	}
	blockMeta := make(common.BlockMetadata, 1+(uint64(len(block.Transactions()))+7)/8)
	if err := c.consensus.WriteMessageFromSequencer(pos, messageWithMeta, msgResult, blockMeta); err != nil {
		return err
	}
	if err := c.commitBlock(context.Background(), block); err != nil {
		return err
	}
	c.cacheL1PriceDataOfMsg(pos, receipts, block, true)
	return nil
}

func (c *Client) NextDelayedMessageNumber() (uint64, error) {
	header, err := c.readCurrentHeader()
	if err != nil {
		return 0, err
	}
	return header.Nonce.Uint64(), nil
}

func (c *Client) MarkFeedStart(to arbutil.MessageIndex) {
	if c.cachedL1PriceData == nil {
		return
	}
	c.cachedL1PriceData.mutex.Lock()
	defer c.cachedL1PriceData.mutex.Unlock()

	if to < c.cachedL1PriceData.startOfL1PriceDataCache {
		c.logger.Debug("erigonexec: l1 price data cache does not include requested start", "start", to)
	} else if to >= c.cachedL1PriceData.endOfL1PriceDataCache {
		c.cachedL1PriceData.startOfL1PriceDataCache = 0
		c.cachedL1PriceData.endOfL1PriceDataCache = 0
		c.cachedL1PriceData.msgToL1PriceData = []L1PriceDataOfMsg{}
	} else {
		newStart := to - c.cachedL1PriceData.startOfL1PriceDataCache + 1
		c.cachedL1PriceData.msgToL1PriceData = c.cachedL1PriceData.msgToL1PriceData[newStart:]
		c.cachedL1PriceData.startOfL1PriceDataCache = to + 1
	}
}

func (c *Client) SetConsensusClient(consensus execution.FullConsensusClient) {
	c.consensus = consensus
}

func (c *Client) SetTxPublisher(publisher gethexec.TransactionPublisher) {
	c.txPublisherMu.Lock()
	defer c.txPublisherMu.Unlock()
	c.txPublisherActive = publisher
	if ctl, ok := publisher.(TxPublisherController); ok {
		c.txPublisherCtl = ctl
		if current := ctl.Current(); current != nil {
			c.txPublisherActive = current
		}
	} else {
		c.txPublisherCtl = nil
	}
}

func (c *Client) Synced() bool {
	header, err := c.readCurrentHeader()
	if err != nil {
		return false
	}
	var execProgress uint64
	err = c.chainDB.View(context.Background(), func(tx kv.Tx) error {
		var err error
		execProgress, err = stages.GetStageProgress(tx, stages.Execution)
		return err
	})
	if err != nil {
		return false
	}
	return execProgress >= header.Number.Uint64()
}

func (c *Client) FullSyncProgressMap() map[string]interface{} {
	progress := map[string]interface{}{}
	if c.chainDB == nil {
		return progress
	}
	_ = c.chainDB.View(context.Background(), func(tx kv.Tx) error {
		for _, stage := range stages.AllStages {
			value, err := stages.GetStageProgress(tx, stage)
			if err != nil {
				continue
			}
			progress[string(stage)] = value
		}
		return nil
	})
	return progress
}

func (c *Client) readCurrentHeader() (*etypes.Header, error) {
	if c.chainDB == nil {
		return nil, errors.New("erigonexec: chain db not initialized")
	}
	var header *etypes.Header
	if err := c.chainDB.View(context.Background(), func(tx kv.Tx) error {
		header = erawdb.ReadCurrentHeader(tx)
		if header == nil {
			return errors.New("erigonexec: current header not found")
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return header, nil
}

func (c *Client) readHeaderByNumber(blockNum uint64) (*etypes.Header, error) {
	if c.chainDB == nil {
		return nil, errors.New("erigonexec: chain db not initialized")
	}
	var header *etypes.Header
	if err := c.chainDB.View(context.Background(), func(tx kv.Tx) error {
		header = erawdb.ReadHeaderByNumber(tx, blockNum)
		if header == nil {
			return fmt.Errorf("erigonexec: header not found block=%d", blockNum)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return header, nil
}

func readChainConfig(ctx context.Context, db kv.RwDB) (*chain.Config, ecommon.Hash, error) {
	var genesis ecommon.Hash
	var cfg *chain.Config
	if err := db.View(ctx, func(tx kv.Tx) error {
		var err error
		genesis, err = erawdb.ReadCanonicalHash(tx, 0)
		if err != nil {
			return err
		}
		if genesis == (ecommon.Hash{}) {
			return errors.New("erigonexec: genesis hash missing")
		}
		cfg, err = erawdb.ReadChainConfig(tx, genesis)
		if err != nil {
			return err
		}
		if cfg == nil {
			return errors.New("erigonexec: chain config missing")
		}
		return nil
	}); err != nil {
		return nil, ecommon.Hash{}, err
	}
	return cfg, genesis, nil
}

func newBlockReader(chainDir string, chainName string, logger elog.Logger) (services.FullBlockReader, error) {
	snapDir := filepath.Join(chainDir, "snapshots")
	if err := os.MkdirAll(snapDir, 0o755); err != nil {
		return nil, fmt.Errorf("create snapshots dir: %w", err)
	}
	snapCfg := ethconfig.BlocksFreezing{ChainName: chainName}
	sn := freezeblocks.NewRoSnapshots(snapCfg, snapDir, logger)
	return freezeblocks.NewBlockReader(sn, nil), nil
}

func toGethHash(hash ecommon.Hash) common.Hash { return common.Hash(hash) }
