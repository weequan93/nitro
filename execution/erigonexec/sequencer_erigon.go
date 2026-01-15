//go:build erigon
// +build erigon

package erigonexec

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/big"
	"runtime/debug"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	ecommon "github.com/erigontech/erigon-lib/common"
	estate "github.com/erigontech/erigon/core/state"
	"github.com/erigontech/erigon/core/vm/evmtypes"
	etypes "github.com/erigontech/erigon/execution/types"

	"github.com/ethereum/go-ethereum/arbitrum"
	"github.com/ethereum/go-ethereum/arbitrum_types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/misc/eip4844"
	gcore "github.com/ethereum/go-ethereum/core"
	gstate "github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/txpool"
	gtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto/kzg4844"
	"github.com/ethereum/go-ethereum/eth/filters"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/metrics"
	"github.com/ethereum/go-ethereum/params"

	"github.com/offchainlabs/nitro/arbos/arbosState"
	"github.com/offchainlabs/nitro/arbos/arbostypes"
	"github.com/offchainlabs/nitro/arbos/l1pricing"
	"github.com/offchainlabs/nitro/arbutil"
	"github.com/offchainlabs/nitro/execution"
	"github.com/offchainlabs/nitro/execution/gethexec"
	"github.com/offchainlabs/nitro/timeboost"
	"github.com/offchainlabs/nitro/util/arbmath"
	"github.com/offchainlabs/nitro/util/containers"
	"github.com/offchainlabs/nitro/util/headerreader"
	"github.com/offchainlabs/nitro/util/stopwaiter"
)

var (
	sequencerBacklogGauge                   = metrics.NewRegisteredGauge("arb/sequencer/backlog", nil)
	nonceCacheHitCounter                    = metrics.NewRegisteredCounter("arb/sequencer/noncecache/hit", nil)
	nonceCacheMissCounter                   = metrics.NewRegisteredCounter("arb/sequencer/noncecache/miss", nil)
	nonceCacheRejectedCounter               = metrics.NewRegisteredCounter("arb/sequencer/noncecache/rejected", nil)
	nonceCacheClearedCounter                = metrics.NewRegisteredCounter("arb/sequencer/noncecache/cleared", nil)
	nonceFailureCacheSizeGauge              = metrics.NewRegisteredGauge("arb/sequencer/noncefailurecache/size", nil)
	nonceFailureCacheOverflowCounter        = metrics.NewRegisteredGauge("arb/sequencer/noncefailurecache/overflow", nil)
	blockCreationTimer                      = metrics.NewRegisteredTimer("arb/sequencer/block/creation", nil)
	successfulBlocksCounter                 = metrics.NewRegisteredCounter("arb/sequencer/block/successful", nil)
	conditionalTxRejectedBySequencerCounter = metrics.NewRegisteredCounter("arb/sequencer/conditionaltx/rejected", nil)
	conditionalTxAcceptedBySequencerCounter = metrics.NewRegisteredCounter("arb/sequencer/conditionaltx/accepted", nil)
	l1GasPriceGauge                         = metrics.NewRegisteredGauge("arb/sequencer/l1gasprice", nil)
	callDataUnitsBacklogGauge               = metrics.NewRegisteredGauge("arb/sequencer/calldataunitsbacklog", nil)
	unusedL1GasChargeGauge                  = metrics.NewRegisteredGauge("arb/sequencer/unusedl1gascharge", nil)
	currentSurplusGauge                     = metrics.NewRegisteredGauge("arb/sequencer/currentsurplus", nil)
	expectedSurplusGauge                    = metrics.NewRegisteredGauge("arb/sequencer/expectedsurplus", nil)
)

type txQueueItem struct {
	tx              *gtypes.Transaction
	txSize          int
	options         *arbitrum_types.ConditionalOptions
	resultChan      chan<- error
	returnedResult  *atomic.Bool
	ctx             context.Context
	firstAppearance time.Time
	isTimeboosted   bool
}

func (i *txQueueItem) returnResult(err error) {
	if i.returnedResult.Swap(true) {
		log.Error("attempting to return result to already finished queue item", "err", err)
		return
	}
	i.resultChan <- err
	close(i.resultChan)
}

type nonceCache struct {
	cache      *containers.LruCache[common.Address, uint64]
	parentHash ecommon.Hash
	dirty      *etypes.Header
}

func newNonceCache(size int) *nonceCache {
	return &nonceCache{
		cache:      containers.NewLruCache[common.Address, uint64](size),
		parentHash: ecommon.Hash{},
		dirty:      nil,
	}
}

func (c *nonceCache) matches(header *etypes.Header) bool {
	if c.dirty != nil {
		return c.dirty == header
	}
	return c.parentHash == header.ParentHash
}

func (c *nonceCache) Reset(parentHash ecommon.Hash) {
	if c.cache.Len() > 0 {
		nonceCacheClearedCounter.Inc(1)
	}
	c.cache.Clear()
	c.parentHash = parentHash
	c.dirty = nil
}

func (c *nonceCache) BeginNewBlock() {
	if c.dirty != nil {
		c.Reset(ecommon.Hash{})
	}
}

func (c *nonceCache) Get(header *etypes.Header, ibs estate.IntraBlockStateArbitrum, addr common.Address) (uint64, error) {
	if !c.matches(header) {
		c.Reset(header.ParentHash)
	}
	nonce, ok := c.cache.Get(addr)
	if ok {
		nonceCacheHitCounter.Inc(1)
		return nonce, nil
	}
	nonceCacheMissCounter.Inc(1)
	nonce, err := ibs.GetNonce(ecommon.Address(addr))
	if err != nil {
		return 0, err
	}
	c.cache.Add(addr, nonce)
	return nonce, nil
}

func (c *nonceCache) Update(header *etypes.Header, addr common.Address, nonce uint64) {
	if !c.matches(header) {
		c.Reset(header.ParentHash)
	}
	c.dirty = header
	c.cache.Add(addr, nonce)
}

func (c *nonceCache) Finalize(block *etypes.Block) {
	if block == nil {
		return
	}
	if c.parentHash == block.ParentHash() {
		c.parentHash = block.Hash()
		c.dirty = nil
	} else {
		c.Reset(block.Hash())
	}
}

func (c *nonceCache) Caching() bool {
	return c.cache != nil && c.cache.Size() > 0
}

func (c *nonceCache) Resize(newSize int) {
	c.cache.Resize(newSize)
}

type addressAndNonce struct {
	address common.Address
	nonce   uint64
}

type nonceError struct {
	sender     common.Address
	txNonce    uint64
	stateNonce uint64
}

func (e nonceError) Error() string {
	if e.txNonce < e.stateNonce {
		return fmt.Sprintf("%v: address %v, tx: %d state: %d", gcore.ErrNonceTooLow, e.sender, e.txNonce, e.stateNonce)
	}
	if e.txNonce > e.stateNonce {
		return fmt.Sprintf("%v: address %v, tx: %d state: %d", gcore.ErrNonceTooHigh, e.sender, e.txNonce, e.stateNonce)
	}
	return fmt.Sprintf("invalid nonce error for address %v nonce %v", e.sender, e.txNonce)
}

func (e nonceError) Unwrap() error {
	if e.txNonce < e.stateNonce {
		return gcore.ErrNonceTooLow
	}
	if e.txNonce > e.stateNonce {
		return gcore.ErrNonceTooHigh
	}
	return nil
}

func (e nonceError) Sender() common.Address   { return e.sender }
func (e nonceError) TxNonce() uint64          { return e.txNonce }
func (e nonceError) StateNonce() uint64       { return e.stateNonce }

func makeNonceError(sender common.Address, txNonce uint64, stateNonce uint64) error {
	if txNonce == stateNonce {
		return nil
	}
	return nonceError{
		sender:     sender,
		txNonce:    txNonce,
		stateNonce: stateNonce,
	}
}

type nonceFailure struct {
	queueItem txQueueItem
	nonceErr  error
	expiry    time.Time
	revived   bool
}

type nonceFailureCache struct {
	*containers.LruCache[addressAndNonce, *nonceFailure]
	getExpiry func() time.Duration
}

func (c nonceFailureCache) Contains(err nonceError) bool {
	key := addressAndNonce{err.Sender(), err.TxNonce()}
	return c.LruCache.Contains(key)
}

func (c nonceFailureCache) Add(err nonceError, queueItem txQueueItem) {
	expiry := queueItem.firstAppearance.Add(c.getExpiry())
	if c.Contains(err) || time.Now().After(expiry) {
		queueItem.returnResult(err)
		return
	}
	key := addressAndNonce{err.Sender(), err.TxNonce()}
	val := &nonceFailure{
		queueItem: queueItem,
		nonceErr:  err,
		expiry:    expiry,
		revived:   false,
	}
	evicted := c.LruCache.Add(key, val)
	if evicted {
		nonceFailureCacheOverflowCounter.Inc(1)
	}
}

type synchronizedTxQueue struct {
	queue containers.Queue[txQueueItem]
	mutex sync.RWMutex
}

func (q *synchronizedTxQueue) Push(item txQueueItem) {
	q.mutex.Lock()
	q.queue.Push(item)
	q.mutex.Unlock()
}

func (q *synchronizedTxQueue) Pop() txQueueItem {
	q.mutex.Lock()
	defer q.mutex.Unlock()
	return q.queue.Pop()
}

func (q *synchronizedTxQueue) Len() int {
	q.mutex.RLock()
	defer q.mutex.RUnlock()
	return q.queue.Len()
}

type Sequencer struct {
	stopwaiter.StopWaiter

	client                  *Client
	txQueue                 chan txQueueItem
	txPriorityQueue         chan txQueueItem
	txRetryQueue            synchronizedTxQueue
	l1Reader                *headerreader.HeaderReader
	config                  gethexec.SequencerConfigFetcher
	senderWhitelist         map[common.Address]struct{}
	prioritySenderWhitelist map[common.Address]struct{}
	nonceCache              *nonceCache
	nonceFailures           *nonceFailureCache
	expressLaneService      *expressLaneService
	onForwarderSet          chan struct{}

	L1BlockAndTimeMutex sync.Mutex
	l1BlockNumber       atomic.Uint64
	l1Timestamp         uint64

	activeMutex sync.Mutex
	pauseChan   chan struct{}
	forwarder   *gethexec.TxForwarder

	expectedSurplusMutex              sync.RWMutex
	expectedSurplus                   int64
	expectedSurplusUpdated            bool
	auctioneerAddr                    common.Address
	timeboostAuctionResolutionTxQueue chan txQueueItem
}

func NewSequencer(client *Client, l1Reader *headerreader.HeaderReader, configFetcher gethexec.SequencerConfigFetcher) (*Sequencer, error) {
	config := configFetcher()
	if err := config.Validate(); err != nil {
		return nil, err
	}
	senderWhitelist := make(map[common.Address]struct{})
	for _, address := range config.SenderWhitelist {
		if len(address) == 0 {
			continue
		}
		senderWhitelist[common.HexToAddress(address)] = struct{}{}
	}

	prioritySenderWhitelist := make(map[common.Address]struct{})
	for _, address := range config.PrioritySenderWhitelist {
		if len(address) == 0 {
			continue
		}
		prioritySenderWhitelist[common.HexToAddress(address)] = struct{}{}
	}

	s := &Sequencer{
		client:                          client,
		txQueue:                         make(chan txQueueItem, config.QueueSize),
		l1Reader:                        l1Reader,
		config:                          configFetcher,
		senderWhitelist:                 senderWhitelist,
		nonceCache:                      newNonceCache(config.NonceCacheSize),
		l1Timestamp:                     0,
		pauseChan:                       nil,
		onForwarderSet:                  make(chan struct{}, 1),
		timeboostAuctionResolutionTxQueue: make(chan txQueueItem, 10),
		txPriorityQueue:                 make(chan txQueueItem, config.QueueSize),
		prioritySenderWhitelist:         prioritySenderWhitelist,
	}
	s.nonceFailures = &nonceFailureCache{
		containers.NewLruCacheWithOnEvict(config.NonceCacheSize, s.onNonceFailureEvict),
		func() time.Duration { return configFetcher().NonceFailureCacheExpiry },
	}
	s.Pause()
	return s, nil
}

func (s *Sequencer) onNonceFailureEvict(_ addressAndNonce, failure *nonceFailure) {
	if failure.revived {
		return
	}
	queueItem := failure.queueItem
	err := queueItem.ctx.Err()
	if err != nil {
		queueItem.returnResult(err)
		return
	}
	_, forwarder := s.GetPauseAndForwarder()
	if forwarder != nil {
		s.LaunchUntrackedThread(func() {
			err = forwarder.PublishTransaction(queueItem.ctx, queueItem.tx, queueItem.options)
			queueItem.returnResult(err)
		})
	} else {
		queueItem.returnResult(failure.nonceErr)
	}
}

func ctxWithTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout == time.Duration(0) {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, timeout)
}

func (s *Sequencer) PublishTransaction(parentCtx context.Context, tx *gtypes.Transaction, options *arbitrum_types.ConditionalOptions) error {
	_, forwarder := s.GetPauseAndForwarder()
	if forwarder != nil {
		err := forwarder.PublishTransaction(parentCtx, tx, options)
		if !errors.Is(err, gethexec.ErrNoSequencer) {
			return err
		}
	}

	config := s.config()
	queueCtx, cancel := ctxWithTimeout(parentCtx, config.QueueTimeout+config.Dangerous.Timeboost.ExpressLaneAdvantage)
	defer cancel()
	resultChan := make(chan error, 1)
	if err := s.publishTransactionToQueue(queueCtx, tx, options, resultChan, false); err != nil {
		return err
	}
	return <-resultChan
}

func (s *Sequencer) PublishPriorityTransaction(parentCtx context.Context, tx *gtypes.Transaction, options *arbitrum_types.ConditionalOptions) error {
	_, forwarder := s.GetPauseAndForwarder()
	if forwarder != nil {
		err := forwarder.PublishPriorityTransaction(parentCtx, tx, options)
		if !errors.Is(err, gethexec.ErrNoSequencer) {
			return err
		}
	}

	config := s.config()
	queueCtx, cancel := ctxWithTimeout(parentCtx, config.QueueTimeout+config.Dangerous.Timeboost.ExpressLaneAdvantage)
	defer cancel()
	resultChan := make(chan error, 1)
	if err := s.publishPriorityTransactionToQueue(queueCtx, tx, options, resultChan, false); err != nil {
		return err
	}
	return <-resultChan
}

func (s *Sequencer) PublishAuctionResolutionTransaction(ctx context.Context, tx *gtypes.Transaction) error {
	if !s.config().Dangerous.Timeboost.Enable {
		return errors.New("timeboost not enabled")
	}
	forwarder, err := s.getForwarder(ctx)
	if err != nil {
		return err
	}
	if forwarder != nil {
		err := forwarder.PublishAuctionResolutionTransaction(ctx, tx)
		if !errors.Is(err, gethexec.ErrNoSequencer) {
			return err
		}
	}

	if s.expressLaneService == nil {
		return errors.New("express lane service not enabled")
	}

	arrivalTime := time.Now()
	auctioneerAddr := s.auctioneerAddr
	if auctioneerAddr == (common.Address{}) {
		return errors.New("invalid auctioneer address")
	}
	if tx.To() == nil {
		return errors.New("transaction has no recipient")
	}
	if *tx.To() != s.expressLaneService.auctionContractAddr {
		return errors.New("transaction recipient is not the auction contract")
	}
	signer := gtypes.LatestSignerForChainID(s.client.chainConfig.ChainID)
	sender, err := gtypes.Sender(signer, tx)
	if err != nil {
		return err
	}
	if sender != auctioneerAddr {
		return fmt.Errorf("sender %#x is not the auctioneer address %#x", sender, auctioneerAddr)
	}
	if !s.expressLaneService.roundTimingInfo.IsWithinAuctionCloseWindow(arrivalTime) {
		return fmt.Errorf("transaction arrival time not within auction closure window: %v", arrivalTime)
	}
	txBytes, err := tx.MarshalBinary()
	if err != nil {
		return err
	}
	log.Info("prioritizing auction resolution transaction from auctioneer", "txHash", tx.Hash().Hex())
	s.timeboostAuctionResolutionTxQueue <- txQueueItem{
		tx:              tx,
		txSize:          len(txBytes),
		options:         nil,
		resultChan:      make(chan error, 1),
		returnedResult:  &atomic.Bool{},
		ctx:             s.GetContext(),
		firstAppearance: time.Now(),
		isTimeboosted:   true,
	}
	return nil
}

func (s *Sequencer) PublishExpressLaneTransaction(ctx context.Context, msg *timeboost.ExpressLaneSubmission) error {
	if !s.config().Dangerous.Timeboost.Enable {
		return errors.New("timeboost not enabled")
	}
	forwarder, err := s.getForwarder(ctx)
	if err != nil {
		return err
	}
	if forwarder != nil {
		return forwarder.PublishExpressLaneTransaction(ctx, msg)
	}

	if s.expressLaneService == nil {
		return errors.New("express lane service not enabled")
	}
	if err := s.expressLaneService.validateExpressLaneTx(msg); err != nil {
		return err
	}

	forwarder, err = s.getForwarder(ctx)
	if err != nil {
		return err
	}
	if forwarder != nil {
		return forwarder.PublishExpressLaneTransaction(ctx, msg)
	}
	return s.expressLaneService.sequenceExpressLaneSubmission(ctx, msg)
}

func (s *Sequencer) PublishTimeboostedTransaction(queueCtx context.Context, tx *gtypes.Transaction, options *arbitrum_types.ConditionalOptions, resultChan chan error) {
	if err := s.publishTransactionToQueue(queueCtx, tx, options, resultChan, true); err != nil {
		resultChan <- err
	}
}

func (s *Sequencer) publishTransactionToQueue(queueCtx context.Context, tx *gtypes.Transaction, options *arbitrum_types.ConditionalOptions, resultChan chan error, isTimeboosted bool) error {
	config := s.config()
	if s.l1Reader != nil && config.ExpectedSurplusHardThreshold != "default" {
		threshold, err := strconv.ParseInt(config.ExpectedSurplusHardThreshold, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid expected-surplus-hard-threshold: %w", err)
		}
		s.expectedSurplusMutex.RLock()
		if s.expectedSurplusUpdated && s.expectedSurplus < threshold {
			s.expectedSurplusMutex.RUnlock()
			return errors.New("currently not accepting transactions due to expected surplus being below threshold")
		}
		s.expectedSurplusMutex.RUnlock()
	}

	sequencerBacklogGauge.Inc(1)
	defer sequencerBacklogGauge.Dec(1)

	if len(s.senderWhitelist) > 0 {
		signer := gtypes.LatestSignerForChainID(s.client.chainConfig.ChainID)
		sender, err := gtypes.Sender(signer, tx)
		if err != nil {
			return err
		}
		if _, authorized := s.senderWhitelist[sender]; !authorized {
			return errors.New("transaction sender is not on the whitelist")
		}
	}
	if tx.Type() >= gtypes.ArbitrumDepositTxType || tx.Type() == gtypes.BlobTxType {
		return gtypes.ErrTxTypeNotSupported
	}
	txBytes, err := tx.MarshalBinary()
	if err != nil {
		return err
	}

	if config.Dangerous.Timeboost.Enable && s.expressLaneService != nil {
		if !isTimeboosted && s.expressLaneService.currentRoundHasController() {
			time.Sleep(config.Dangerous.Timeboost.ExpressLaneAdvantage)
		}
	}

	queueItem := txQueueItem{
		tx:              tx,
		txSize:          len(txBytes),
		options:         options,
		resultChan:      resultChan,
		returnedResult:  &atomic.Bool{},
		ctx:             queueCtx,
		firstAppearance: time.Now(),
		isTimeboosted:   isTimeboosted,
	}
	select {
	case s.txQueue <- queueItem:
	case <-queueCtx.Done():
		return queueCtx.Err()
	}
	return nil
}

func (s *Sequencer) publishPriorityTransactionToQueue(queueCtx context.Context, tx *gtypes.Transaction, options *arbitrum_types.ConditionalOptions, resultChan chan error, isTimeboosted bool) error {
	config := s.config()
	if s.l1Reader != nil && config.ExpectedSurplusHardThreshold != "default" {
		threshold, err := strconv.ParseInt(config.ExpectedSurplusHardThreshold, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid expected-surplus-hard-threshold: %w", err)
		}
		s.expectedSurplusMutex.RLock()
		if s.expectedSurplusUpdated && s.expectedSurplus < threshold {
			s.expectedSurplusMutex.RUnlock()
			return errors.New("currently not accepting transactions due to expected surplus being below threshold")
		}
		s.expectedSurplusMutex.RUnlock()
	}

	sequencerBacklogGauge.Inc(1)
	defer sequencerBacklogGauge.Dec(1)

	if len(s.prioritySenderWhitelist) > 0 {
		signer := gtypes.LatestSignerForChainID(s.client.chainConfig.ChainID)
		sender, err := gtypes.Sender(signer, tx)
		if err != nil {
			return err
		}
		if _, authorized := s.prioritySenderWhitelist[sender]; !authorized {
			return errors.New("transaction sender is not on the priority whitelist")
		}
	}
	if tx.Type() >= gtypes.ArbitrumDepositTxType || tx.Type() == gtypes.BlobTxType {
		return gtypes.ErrTxTypeNotSupported
	}
	txBytes, err := tx.MarshalBinary()
	if err != nil {
		return err
	}

	if config.Dangerous.Timeboost.Enable && s.expressLaneService != nil {
		if !isTimeboosted && s.expressLaneService.currentRoundHasController() {
			time.Sleep(config.Dangerous.Timeboost.ExpressLaneAdvantage)
		}
	}

	queueItem := txQueueItem{
		tx:              tx,
		txSize:          len(txBytes),
		options:         options,
		resultChan:      resultChan,
		returnedResult:  &atomic.Bool{},
		ctx:             queueCtx,
		firstAppearance: time.Now(),
		isTimeboosted:   isTimeboosted,
	}
	select {
	case s.txPriorityQueue <- queueItem:
	case <-queueCtx.Done():
		return queueCtx.Err()
	}
	return nil
}

func (s *Sequencer) preTxFilter(header *etypes.Header, ibs estate.IntraBlockStateArbitrum, _ *arbosState.ArbosState, tx *gtypes.Transaction, options *arbitrum_types.ConditionalOptions, sender common.Address, l1Info *l1Info) error {
	if s.nonceCache.Caching() {
		stateNonce, err := s.nonceCache.Get(header, ibs, sender)
		if err != nil {
			return err
		}
		if err := makeNonceError(sender, tx.Nonce(), stateNonce); err != nil {
			nonceCacheRejectedCounter.Inc(1)
			return err
		}
	}
	if options != nil {
		if err := checkConditionalOptions(options, l1Info.l1BlockNumber, l1Info.l1Timestamp, ibs); err != nil {
			conditionalTxRejectedBySequencerCounter.Inc(1)
			return err
		}
		conditionalTxAcceptedBySequencerCounter.Inc(1)
	}
	return nil
}

func newRevertReasonFromErigon(result *evmtypes.ExecutionResult) error {
	if result == nil {
		return nil
	}
	var deployed *common.Address
	if result.TopLevelDeployed != nil {
		addr := common.Address(*result.TopLevelDeployed)
		deployed = &addr
	}
	gethResult := &gcore.ExecutionResult{
		UsedGas:         result.GasUsed,
		RefundedGas:     result.EvmRefund,
		Err:             result.Err,
		ReturnData:      result.ReturnData,
		ScheduledTxes:   nil,
		TopLevelDeployed: deployed,
	}
	return arbitrum.NewRevertReason(gethResult)
}

func (s *Sequencer) postTxFilter(header *etypes.Header, ibs estate.IntraBlockStateArbitrum, _ *arbosState.ArbosState, tx *gtypes.Transaction, sender common.Address, dataGas uint64, result *evmtypes.ExecutionResult) error {
	if ibs.IsTxFiltered() {
		return gstate.ErrArbTxFilter
	}
	if result != nil && result.Err != nil && result.GasUsed > dataGas && result.GasUsed-dataGas <= s.config().MaxRevertGasReject {
		return newRevertReasonFromErigon(result)
	}
	newNonce := tx.Nonce() + 1
	s.nonceCache.Update(header, sender, newNonce)
	newAddrAndNonce := addressAndNonce{sender, newNonce}
	nonceFailure, haveNonceFailure := s.nonceFailures.Get(newAddrAndNonce)
	if haveNonceFailure {
		nonceFailure.revived = true
		s.nonceFailures.Remove(newAddrAndNonce)
		err := nonceFailure.queueItem.ctx.Err()
		if err != nil {
			nonceFailure.queueItem.returnResult(err)
		} else {
			s.txRetryQueue.Push(nonceFailure.queueItem)
		}
	}
	return nil
}

func (s *Sequencer) CheckHealth(ctx context.Context) error {
	pauseChan, forwarder := s.GetPauseAndForwarder()
	if forwarder != nil {
		return forwarder.CheckHealth(ctx)
	}
	if pauseChan != nil {
		return nil
	}
	if s.client == nil || s.client.consensus == nil {
		return errors.New("erigonexec: consensus client not configured")
	}
	return s.client.consensus.ExpectChosenSequencer()
}

func (s *Sequencer) ForwardTarget() string {
	s.activeMutex.Lock()
	defer s.activeMutex.Unlock()
	if s.forwarder == nil {
		return ""
	}
	return s.forwarder.PrimaryTarget()
}

func (s *Sequencer) ForwardTo(url string) error {
	s.activeMutex.Lock()
	defer s.activeMutex.Unlock()
	if s.forwarder != nil {
		if s.forwarder.PrimaryTarget() == url {
			log.Warn("attempted to update sequencer forward target with existing target", "url", url)
			return nil
		}
		s.forwarder.Disable()
	}
	s.forwarder = gethexec.NewForwarder([]string{url}, &s.config().Forwarder)
	err := s.forwarder.Initialize(s.GetContext())
	if err != nil {
		log.Error("failed to set forward agent", "err", err)
		s.forwarder = nil
	}
	if s.pauseChan != nil {
		close(s.pauseChan)
		s.pauseChan = nil
	}
	if err == nil {
		select {
		case s.onForwarderSet <- struct{}{}:
		default:
		}
	}
	return err
}

func (s *Sequencer) Activate() {
	s.activeMutex.Lock()
	defer s.activeMutex.Unlock()
	if s.forwarder != nil {
		s.forwarder.Disable()
		s.forwarder = nil
	}
	if s.pauseChan != nil {
		close(s.pauseChan)
		s.pauseChan = nil
	}
	if s.expressLaneService != nil {
		s.LaunchThread(func(context.Context) {
			s.expressLaneService.syncFromRedis()
			time.Sleep(time.Second)
			s.expressLaneService.syncFromRedis()
		})
	}
}

func (s *Sequencer) Pause() {
	s.activeMutex.Lock()
	defer s.activeMutex.Unlock()
	if s.forwarder != nil {
		s.forwarder.Disable()
		s.forwarder = nil
	}
	if s.pauseChan == nil {
		s.pauseChan = make(chan struct{})
	}
}

func (s *Sequencer) GetPauseAndForwarder() (chan struct{}, *gethexec.TxForwarder) {
	s.activeMutex.Lock()
	defer s.activeMutex.Unlock()
	return s.pauseChan, s.forwarder
}

func (s *Sequencer) getForwarder(ctx context.Context) (*gethexec.TxForwarder, error) {
	for {
		pause, forwarder := s.GetPauseAndForwarder()
		if pause == nil {
			return forwarder, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-pause:
		}
	}
}

func (s *Sequencer) handleInactive(ctx context.Context, queueItems []txQueueItem) bool {
	forwarder, err := s.getForwarder(ctx)
	if err != nil {
		return true
	}
	if forwarder == nil {
		return false
	}
	publishResults := make(chan *txQueueItem, len(queueItems))
	for _, item := range queueItems {
		item := item
		go func() {
			res := forwarder.PublishTransaction(item.ctx, item.tx, item.options)
			if errors.Is(res, gethexec.ErrNoSequencer) {
				publishResults <- &item
			} else {
				publishResults <- nil
				item.returnResult(res)
			}
		}()
	}
	for range queueItems {
		remainingItem := <-publishResults
		if remainingItem != nil {
			s.txRetryQueue.Push(*remainingItem)
		}
	}
	s.nonceFailures.Clear()
	return true
}

var sequencerInternalError = errors.New("sequencer internal error")

func (s *Sequencer) makeSequencingHooks(options []*arbitrum_types.ConditionalOptions) *sequencingHooks {
	return &sequencingHooks{
		PreTxFilter:            s.preTxFilter,
		PostTxFilter:           s.postTxFilter,
		DiscardInvalidTxsEarly: true,
		TxErrors:               []error{},
		ConditionalOptionsForTx: options,
	}
}

func (s *Sequencer) expireNonceFailures() *time.Timer {
	defer nonceFailureCacheSizeGauge.Update(int64(s.nonceFailures.Len()))
	for {
		_, failure, ok := s.nonceFailures.GetOldest()
		if !ok {
			return nil
		}
		untilExpiry := time.Until(failure.expiry)
		if untilExpiry > 0 {
			return time.NewTimer(untilExpiry)
		}
		s.nonceFailures.RemoveOldest()
	}
}

func (s *Sequencer) precheckNonces(queueItems []txQueueItem, totalBlockSize int) []txQueueItem {
	header, ibsArb, release, err := s.client.latestState(s.GetContext())
	if err != nil {
		log.Error("failed to get current state to pre-check nonces", "err", err)
		return queueItems
	}
	defer release()

	signer := gtypes.LatestSignerForChainID(s.client.chainConfig.ChainID)
	outputQueueItems := make([]txQueueItem, 0, len(queueItems))
	var nextQueueItem *txQueueItem
	var queueItemsIdx int
	pendingNonces := make(map[common.Address]uint64)
	for {
		var queueItem txQueueItem
		if nextQueueItem != nil {
			queueItem = *nextQueueItem
			nextQueueItem = nil
		} else if queueItemsIdx < len(queueItems) {
			queueItem = queueItems[queueItemsIdx]
			queueItemsIdx++
		} else {
			break
		}
		tx := queueItem.tx
		sender, err := gtypes.Sender(signer, tx)
		if err != nil {
			queueItem.returnResult(err)
			continue
		}
		stateNonce, err := s.nonceCache.Get(header, ibsArb, sender)
		if err != nil {
			queueItem.returnResult(err)
			continue
		}
		pendingNonce, pending := pendingNonces[sender]
		if !pending {
			pendingNonce = stateNonce
		}
		txNonce := tx.Nonce()
		if txNonce == pendingNonce {
			pendingNonces[sender] = txNonce + 1
			nextKey := addressAndNonce{sender, txNonce + 1}
			revivingFailure, exists := s.nonceFailures.Get(nextKey)
			if exists {
				revivingFailure.revived = true
				s.nonceFailures.Remove(nextKey)
				err := revivingFailure.queueItem.ctx.Err()
				if err != nil {
					revivingFailure.queueItem.returnResult(err)
				} else {
					if arbmath.SaturatingAdd(totalBlockSize, revivingFailure.queueItem.txSize) > s.config().MaxTxDataSize {
						s.txRetryQueue.Push(revivingFailure.queueItem)
					} else {
						nextQueueItem = &revivingFailure.queueItem
						totalBlockSize += revivingFailure.queueItem.txSize
					}
				}
			}
		} else if txNonce < stateNonce || txNonce > pendingNonce {
			err := makeNonceError(sender, txNonce, stateNonce)
			if errors.Is(err, gcore.ErrNonceTooHigh) {
				var nonceErr nonceError
				if !errors.As(err, &nonceErr) {
					log.Warn("unreachable nonce error is not nonceError")
					continue
				}
				s.nonceFailures.Add(nonceErr, queueItem)
				continue
			} else if err != nil {
				nonceCacheRejectedCounter.Inc(1)
				queueItem.returnResult(err)
				continue
			} else {
				log.Warn("unreachable nonce err == nil condition hit in precheckNonces")
			}
		}
		outputQueueItems = append(outputQueueItems, queueItem)
	}
	nonceFailureCacheSizeGauge.Update(int64(s.nonceFailures.Len()))
	return outputQueueItems
}

func (s *Sequencer) createBlock(ctx context.Context) (returnValue bool) {
	var queueItems []txQueueItem
	var totalBlockSize int

	defer func() {
		panicErr := recover()
		if panicErr != nil {
			log.Error("sequencer block creation panicked", "panic", panicErr, "backtrace", string(debug.Stack()))
			for _, item := range queueItems {
				if !item.returnedResult.Load() {
					item.returnResult(sequencerInternalError)
				}
			}
			returnValue = true
		}
	}()
	defer nonceFailureCacheSizeGauge.Update(int64(s.nonceFailures.Len()))

	config := s.config()
	s.nonceFailures.Resize(config.NonceFailureCacheSize)
	nextNonceExpiryTimer := s.expireNonceFailures()
	defer func() {
		if nextNonceExpiryTimer != nil {
			nextNonceExpiryTimer.Stop()
		}
	}()

	for {
		var queueItem txQueueItem

		if s.txRetryQueue.Len() > 0 {
			select {
			case queueItem = <-s.timeboostAuctionResolutionTxQueue:
				log.Debug("popped auction resolution tx", "txHash", queueItem.tx.Hash())
			default:
				queueItem = s.txRetryQueue.Pop()
			}
		} else if len(queueItems) == 0 {
			var nextNonceExpiryChan <-chan time.Time
			if nextNonceExpiryTimer != nil {
				nextNonceExpiryChan = nextNonceExpiryTimer.C
			}
			select {
			case queueItem = <-s.timeboostAuctionResolutionTxQueue:
				log.Debug("popped auction resolution tx", "txHash", queueItem.tx.Hash())
			default:
				select {
				case queueItem = <-s.txPriorityQueue:
				case queueItem = <-s.txQueue:
				case queueItem = <-s.timeboostAuctionResolutionTxQueue:
					log.Debug("popped auction resolution tx", "txHash", queueItem.tx.Hash())
				case <-nextNonceExpiryChan:
					nextNonceExpiryTimer = s.expireNonceFailures()
					continue
				case <-s.onForwarderSet:
					_, forwarder := s.GetPauseAndForwarder()
					if forwarder != nil {
						s.nonceFailures.Clear()
					}
					continue
				case <-ctx.Done():
					return false
				}
			}
		} else {
			done := false
			select {
			case queueItem = <-s.timeboostAuctionResolutionTxQueue:
				log.Debug("popped auction resolution tx", "txHash", queueItem.tx.Hash())
			default:
				select {
				case queueItem = <-s.txPriorityQueue:
				case queueItem = <-s.txQueue:
				case queueItem = <-s.timeboostAuctionResolutionTxQueue:
					log.Debug("popped auction resolution tx", "txHash", queueItem.tx.Hash())
				default:
					done = true
				}
			}
			if done {
				break
			}
		}
		err := queueItem.ctx.Err()
		if err != nil {
			queueItem.returnResult(err)
			continue
		}
		if queueItem.txSize > config.MaxTxDataSize {
			queueItem.returnResult(txpool.ErrOversizedData)
			continue
		}
		if totalBlockSize+queueItem.txSize > config.MaxTxDataSize {
			s.txRetryQueue.Push(queueItem)
			break
		}
		totalBlockSize += queueItem.txSize
		queueItems = append(queueItems, queueItem)
	}

	s.nonceCache.Resize(config.NonceCacheSize)
	s.nonceCache.BeginNewBlock()
	queueItems = s.precheckNonces(queueItems, totalBlockSize)
	txes := make([]*gtypes.Transaction, len(queueItems))
	options := make([]*arbitrum_types.ConditionalOptions, len(queueItems))
	timeboostedTxs := make(map[common.Hash]struct{})
	totalBlockSize = 0
	for i, queueItem := range queueItems {
		txes[i] = queueItem.tx
		options[i] = queueItem.options
		totalBlockSize = arbmath.SaturatingAdd(totalBlockSize, queueItem.txSize)
		if queueItem.isTimeboosted {
			timeboostedTxs[queueItem.tx.Hash()] = struct{}{}
		}
	}

	if totalBlockSize > config.MaxTxDataSize {
		for _, queueItem := range queueItems {
			s.txRetryQueue.Push(queueItem)
		}
		log.Error(
			"put too many transactions in a block",
			"numTxes", len(queueItems),
			"totalBlockSize", totalBlockSize,
			"maxTxDataSize", config.MaxTxDataSize,
		)
		return false
	}

	if s.handleInactive(ctx, queueItems) {
		return false
	}

	timestamp := time.Now().Unix()
	s.L1BlockAndTimeMutex.Lock()
	l1Block := s.l1BlockNumber.Load()
	l1Timestamp := s.l1Timestamp
	s.L1BlockAndTimeMutex.Unlock()

	if s.l1Reader != nil && (l1Block == 0 || math.Abs(float64(l1Timestamp)-float64(timestamp)) > config.MaxAcceptableTimestampDelta.Seconds()) {
		for _, queueItem := range queueItems {
			s.txRetryQueue.Push(queueItem)
		}
		log.Error(
			"cannot sequence: unknown L1 block or L1 timestamp too far from local clock time",
			"l1Block", l1Block,
			"l1Timestamp", time.Unix(int64(l1Timestamp), 0),
			"localTimestamp", time.Unix(timestamp, 0),
		)
		return true
	}

	header := &arbostypes.L1IncomingMessageHeader{
		Kind:        arbostypes.L1MessageType_L2Message,
		Poster:      l1pricing.BatchPosterAddress,
		BlockNumber: l1Block,
		Timestamp:   arbmath.SaturatingUCast[uint64](timestamp),
		RequestId:   nil,
		L1BaseFee:   nil,
	}

	start := time.Now()
	var (
		block    *etypes.Block
		txErrors []error
		err      error
	)
	hooks := s.makeSequencingHooks(options)
	if config.EnableProfiling {
		block, _, txErrors, err = s.client.sequenceTransactionsWithProfiling(ctx, header, txes, hooks, timeboostedTxs)
	} else {
		block, _, txErrors, err = s.client.sequenceTransactions(ctx, header, txes, hooks, timeboostedTxs)
	}
	elapsed := time.Since(start)
	blockCreationTimer.Update(elapsed)
	if elapsed >= 5*time.Second {
		var blockNum *big.Int
		if block != nil {
			blockNum = block.Number()
		}
		log.Warn("took over 5 seconds to sequence a block", "elapsed", elapsed, "numTxes", len(txes), "success", block != nil, "l2Block", blockNum)
	}
	if err == nil && len(txErrors) != len(txes) {
		err = fmt.Errorf("unexpected number of error results: %v vs number of txes %v", len(txErrors), len(txes))
	}
	if errors.Is(err, execution.ErrRetrySequencer) {
		log.Warn("error sequencing transactions", "err", err)
		if s.handleInactive(ctx, queueItems) {
			return false
		}
		for _, item := range queueItems {
			s.txRetryQueue.Push(item)
		}
		return false
	}
	if err != nil {
		if errors.Is(err, context.Canceled) {
			for _, item := range queueItems {
				s.txRetryQueue.Push(item)
			}
			return true
		}
		log.Error("error sequencing transactions", "err", err)
		for _, queueItem := range queueItems {
			queueItem.returnResult(err)
		}
		return false
	}

	if block != nil {
		successfulBlocksCounter.Inc(1)
		s.nonceCache.Finalize(block)
	}

	madeBlock := false
	for i, err := range txErrors {
		if err == nil {
			madeBlock = true
		}
		queueItem := queueItems[i]
		if errors.Is(err, gcore.ErrGasLimitReached) {
			if madeBlock {
				s.txRetryQueue.Push(queueItem)
				continue
			}
		}
		if errors.Is(err, gcore.ErrIntrinsicGas) {
			err = gcore.ErrIntrinsicGas
		}
		var nonceErr nonceError
		if errors.As(err, &nonceErr) && nonceErr.TxNonce() > nonceErr.StateNonce() {
			s.nonceFailures.Add(nonceErr, queueItem)
			continue
		}
		queueItem.returnResult(err)
	}
	return madeBlock
}

func (s *Sequencer) updateLatestParentChainBlock(header *gtypes.Header) {
	s.L1BlockAndTimeMutex.Lock()
	defer s.L1BlockAndTimeMutex.Unlock()

	l1BlockNumber := arbutil.ParentHeaderToL1BlockNumber(header)
	if header.Time > s.l1Timestamp || (header.Time == s.l1Timestamp && l1BlockNumber > s.l1BlockNumber.Load()) {
		s.l1Timestamp = header.Time
		s.l1BlockNumber.Store(l1BlockNumber)
	}
}

func (s *Sequencer) Initialize(ctx context.Context) error {
	if s.l1Reader == nil {
		return nil
	}
	header, err := s.l1Reader.LastHeader(ctx)
	if err != nil {
		return err
	}
	s.updateLatestParentChainBlock(header)
	return nil
}

func (s *Sequencer) InitializeExpressLaneService(_ *arbitrum.APIBackend, _ *filters.FilterSystem, auctionContractAddr common.Address, auctioneerAddr common.Address, earlySubmissionGrace time.Duration) error {
	els, err := newExpressLaneService(s.client, s, s.config, auctionContractAddr, earlySubmissionGrace)
	if err != nil {
		return fmt.Errorf("failed to create express lane service. auctionContractAddr: %v err: %w", auctionContractAddr, err)
	}
	s.auctioneerAddr = auctioneerAddr
	s.expressLaneService = els
	return nil
}

func (s *Sequencer) StartExpressLaneService(ctx context.Context) {
	if s.expressLaneService != nil {
		s.expressLaneService.Start(ctx)
	}
}

var (
	usableBytesInBlob    = big.NewInt(int64(len(kzg4844.Blob{}) * 31 / 32))
	blobTxBlobGasPerBlob = big.NewInt(params.BlobTxBlobGasPerBlob)
)

func (s *Sequencer) updateExpectedSurplus(ctx context.Context) (int64, error) {
	header, err := s.l1Reader.LastHeader(ctx)
	if err != nil {
		return 0, fmt.Errorf("error encountered getting latest header from l1reader while updating expectedSurplus: %w", err)
	}
	l1GasPrice := header.BaseFee.Uint64()
	if header.BlobGasUsed != nil {
		if header.ExcessBlobGas != nil {
			blobFeePerByte := eip4844.CalcBlobFee(eip4844.CalcExcessBlobGas(*header.ExcessBlobGas, *header.BlobGasUsed))
			blobFeePerByte.Mul(blobFeePerByte, blobTxBlobGasPerBlob)
			blobFeePerByte.Div(blobFeePerByte, usableBytesInBlob)
			if l1GasPrice > blobFeePerByte.Uint64()/16 {
				l1GasPrice = blobFeePerByte.Uint64() / 16
			}
		}
	}
	surplus, err := s.client.getL1PricingSurplus(ctx)
	if err != nil {
		return 0, fmt.Errorf("error encountered getting l1 pricing surplus while updating expectedSurplus: %w", err)
	}
	backlogL1GasCharged := int64(s.client.backlogL1GasCharged())
	backlogCallDataUnits := int64(s.client.backlogCallDataUnits())
	expectedSurplus := int64(surplus) + backlogL1GasCharged - backlogCallDataUnits*int64(l1GasPrice)
	l1GasPriceGauge.Update(int64(l1GasPrice))
	callDataUnitsBacklogGauge.Update(backlogCallDataUnits)
	unusedL1GasChargeGauge.Update(backlogL1GasCharged)
	currentSurplusGauge.Update(surplus)
	expectedSurplusGauge.Update(expectedSurplus)
	config := s.config()
	if config.ExpectedSurplusSoftThreshold != "default" {
		threshold, err := strconv.ParseInt(config.ExpectedSurplusSoftThreshold, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid expected-surplus-soft-threshold: %w", err)
		}
		if expectedSurplus < threshold {
			log.Warn("expected surplus is below soft threshold", "value", expectedSurplus, "threshold", config.ExpectedSurplusSoftThreshold)
		}
	}
	return expectedSurplus, nil
}

func (s *Sequencer) Start(ctxIn context.Context) error {
	s.StopWaiter.Start(ctxIn, s)
	config := s.config()
	if (config.ExpectedSurplusHardThreshold != "default" || config.ExpectedSurplusSoftThreshold != "default") && s.l1Reader == nil {
		return errors.New("expected surplus soft/hard thresholds are enabled but l1Reader is nil")
	}

	if s.l1Reader != nil {
		if !s.l1Reader.Started() {
			s.l1Reader.Start(ctxIn)
		}
		initialBlockNr := s.l1BlockNumber.Load()
		if initialBlockNr == 0 {
			return errors.New("sequencer not initialized")
		}

		expectedSurplus, err := s.updateExpectedSurplus(ctxIn)
		if err != nil {
			if config.ExpectedSurplusHardThreshold != "default" {
				return fmt.Errorf("expected-surplus-hard-threshold is enabled but error fetching initial expected surplus value: %w", err)
			}
			log.Error("expected-surplus-soft-threshold is enabled but error fetching initial expected surplus value", "err", err)
		} else {
			s.expectedSurplus = expectedSurplus
			s.expectedSurplusUpdated = true
		}
		s.CallIteratively(func(ctx context.Context) time.Duration {
			expectedSurplus, err := s.updateExpectedSurplus(ctxIn)
			s.expectedSurplusMutex.Lock()
			defer s.expectedSurplusMutex.Unlock()
			if err != nil {
				s.expectedSurplusUpdated = false
				log.Error("expected surplus soft/hard thresholds are enabled but unable to fetch latest expected surplus, retrying", "err", err)
				return 0
			}
			s.expectedSurplusUpdated = true
			s.expectedSurplus = expectedSurplus
			return 5 * time.Second
		})

		headerChan, cancel := s.l1Reader.Subscribe(false)
		s.LaunchThread(func(ctx context.Context) {
			defer cancel()
			for {
				select {
				case header, ok := <-headerChan:
					if !ok {
						return
					}
					s.updateLatestParentChainBlock(header)
				case <-ctx.Done():
					return
				}
			}
		})
	}

	s.CallIteratively(func(ctx context.Context) time.Duration {
		nextBlock := time.Now().Add(s.config().MaxBlockSpeed)
		if s.createBlock(ctx) {
			return time.Until(nextBlock)
		}
		return 0
	})

	return nil
}

func (s *Sequencer) StopAndWait() {
	s.StopWaiter.StopAndWait()
	if s.l1Reader != nil && s.l1Reader.Started() {
		s.l1Reader.StopAndWait()
	}
	if s.config().Dangerous.Timeboost.Enable && s.expressLaneService != nil {
		s.expressLaneService.StopAndWait()
	}
	if s.txRetryQueue.Len() == 0 &&
		len(s.txQueue) == 0 &&
		len(s.txPriorityQueue) == 0 &&
		s.nonceFailures.Len() == 0 &&
		len(s.timeboostAuctionResolutionTxQueue) == 0 {
		return
	}
	log.Warn("Sequencer has queued items while shutting down",
		"txQueue", len(s.txQueue),
		"txPriorityQueue", len(s.txPriorityQueue),
		"retryQueue", s.txRetryQueue.Len(),
		"nonceFailures", s.nonceFailures.Len(),
		"timeboostAuctionResolutionTxQueue", len(s.timeboostAuctionResolutionTxQueue))
	_, forwarder := s.GetPauseAndForwarder()
	if forwarder != nil {
		var wg sync.WaitGroup
	emptyqueues:
		for {
			var item txQueueItem
			source := ""
			if s.txRetryQueue.Len() > 0 {
				item = s.txRetryQueue.Pop()
				source = "retryQueue"
			} else if s.nonceFailures.Len() > 0 {
				_, failure, _ := s.nonceFailures.GetOldest()
				failure.revived = true
				item = failure.queueItem
				source = "nonceFailures"
				s.nonceFailures.RemoveOldest()
			} else {
				select {
				case item = <-s.txPriorityQueue:
					source = "txQueue"
				case item = <-s.txQueue:
					source = "txQueue"
				case item = <-s.timeboostAuctionResolutionTxQueue:
					source = "timeboostAuctionResolutionTxQueue"
				default:
					break emptyqueues
				}
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				err := forwarder.PublishTransaction(item.ctx, item.tx, item.options)
				if err != nil {
					log.Warn("failed to forward transaction while shutting down", "source", source, "err", err)
				}
			}()
		}
		wg.Wait()
	}
}
