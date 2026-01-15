//go:build erigon
// +build erigon

package erigonexec

import (
	"bytes"
	"context"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/pkg/errors"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/arbitrum_types"
	"github.com/ethereum/go-ethereum/common"
	gtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/metrics"

	"github.com/offchainlabs/nitro/execution/gethexec"
	"github.com/offchainlabs/nitro/solgen/go/express_lane_auctiongen"
	"github.com/offchainlabs/nitro/timeboost"
	"github.com/offchainlabs/nitro/util/containers"
	"github.com/offchainlabs/nitro/util/stopwaiter"
)

var (
	auctionResolutionLatency = metrics.NewRegisteredHistogram("arb/sequencer/timeboost/auctionresolution", nil, metrics.NewBoundedHistogramSample())
)

type transactionPublisher interface {
	PublishTimeboostedTransaction(context.Context, *gtypes.Transaction, *arbitrum_types.ConditionalOptions, chan error)
}

type msgAndResult struct {
	msg        *timeboost.ExpressLaneSubmission
	resultChan chan error
}

type expressLaneRoundInfo struct {
	sequence                     uint64
	msgAndResultBySequenceNumber map[uint64]*msgAndResult
}

type expressLaneService struct {
	stopwaiter.StopWaiter
	transactionPublisher transactionPublisher
	seqConfig            gethexec.SequencerConfigFetcher
	auctionContractAddr  common.Address
	client               *Client
	roundTimingInfo      timeboost.RoundTimingInfo
	earlySubmissionGrace time.Duration
	chainID              *big.Int
	auctionContract      *express_lane_auctiongen.ExpressLaneAuction
	redisCoordinator     *timeboost.RedisCoordinator
	roundControl         containers.SyncMap[uint64, common.Address]

	roundInfoMutex sync.Mutex
	roundInfo      *containers.LruCache[uint64, *expressLaneRoundInfo]
}

func newExpressLaneService(
	client *Client,
	transactionPublisher transactionPublisher,
	seqConfig gethexec.SequencerConfigFetcher,
	auctionContractAddr common.Address,
	earlySubmissionGrace time.Duration,
) (*expressLaneService, error) {
	if client == nil {
		return nil, errors.New("erigonexec: express lane service missing client")
	}
	if client.chainConfig == nil || client.chainConfig.ChainID == nil {
		return nil, errors.New("erigonexec: express lane service missing chain config")
	}
	ethAPI, err := NewEthAPI(client, nil)
	if err != nil {
		return nil, err
	}
	contractBackend := newContractAdapter(ethAPI.APIImpl)
	auctionContract, err := express_lane_auctiongen.NewExpressLaneAuction(auctionContractAddr, contractBackend)
	if err != nil {
		return nil, err
	}

	retries := 0
pending:
	rawRoundTimingInfo, err := auctionContract.RoundTimingInfo(&bind.CallOpts{})
	if err != nil {
		const maxRetries = 5
		if errors.Is(err, bind.ErrNoCode) && retries < maxRetries {
			wait := time.Millisecond * 250 * (1 << retries)
			log.Info("ExpressLaneAuction contract not ready, will retry after wait", "err", err, "auctionContractAddr", auctionContractAddr, "wait", wait, "maxRetries", maxRetries)
			retries++
			time.Sleep(wait)
			goto pending
		}
		return nil, err
	}
	roundTimingInfo, err := timeboost.NewRoundTimingInfo(rawRoundTimingInfo)
	if err != nil {
		return nil, err
	}

	var redisCoordinator *timeboost.RedisCoordinator
	if seqConfig().Dangerous.Timeboost.RedisUrl != "" {
		redisCoordinator, err = timeboost.NewRedisCoordinator(seqConfig().Dangerous.Timeboost.RedisUrl, roundTimingInfo.Round)
		if err != nil {
			return nil, fmt.Errorf("error initializing expressLaneService redis: %w", err)
		}
	}

	return &expressLaneService{
		transactionPublisher: transactionPublisher,
		seqConfig:            seqConfig,
		auctionContract:      auctionContract,
		client:               client,
		chainID:              new(big.Int).Set(client.chainConfig.ChainID),
		roundTimingInfo:      *roundTimingInfo,
		earlySubmissionGrace: earlySubmissionGrace,
		auctionContractAddr:  auctionContractAddr,
		redisCoordinator:     redisCoordinator,
		roundInfo:            containers.NewLruCache[uint64, *expressLaneRoundInfo](8),
	}, nil
}

func (es *expressLaneService) Start(ctxIn context.Context) {
	es.StopWaiter.Start(ctxIn, es)

	if es.redisCoordinator != nil {
		es.redisCoordinator.Start(ctxIn)
	}

	es.LaunchThread(func(ctx context.Context) {
		log.Info("Watching for new express lane rounds")
		waitTime := es.roundTimingInfo.TimeTilNextRound()
		select {
		case <-ctx.Done():
			return
		case <-time.After(waitTime):
		}

		ticker := time.NewTicker(es.roundTimingInfo.Round)
		defer ticker.Stop()
		for {
			var t time.Time
			select {
			case <-ctx.Done():
				return
			case t = <-ticker.C:
			}

			round := es.roundTimingInfo.RoundNumber()
			log.Info(
				"New express lane auction round",
				"round", round,
				"timestamp", t,
			)
			es.roundControl.Delete(round - 1)
		}
	})

	es.LaunchThread(func(ctx context.Context) {
		log.Info("Monitoring express lane auction contract")

		var fromBlock uint64
		maxBlockSpeed := es.seqConfig().MaxBlockSpeed
		header, err := es.client.readCurrentHeader()
		if err != nil {
			log.Error("ExpressLaneService could not get the latest header", "err", err)
		} else {
			maxBlocksPerRound := es.roundTimingInfo.Round / maxBlockSpeed
			fromBlock = header.Number.Uint64()
			if fromBlock > uint64(maxBlocksPerRound) {
				fromBlock -= uint64(maxBlocksPerRound)
			}
		}

		ticker := time.NewTicker(maxBlockSpeed)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				newMaxBlockSpeed := es.seqConfig().MaxBlockSpeed
				if newMaxBlockSpeed != maxBlockSpeed {
					maxBlockSpeed = newMaxBlockSpeed
					ticker.Reset(maxBlockSpeed)
				}
			}

			header, err := es.client.readCurrentHeader()
			if err != nil {
				log.Error("ExpressLaneService could not get the latest header", "err", err)
				continue
			}
			toBlock := header.Number.Uint64()
			if fromBlock > toBlock {
				continue
			}
			filterOpts := &bind.FilterOpts{
				Context: ctx,
				Start:   fromBlock,
				End:     &toBlock,
			}

			it, err := es.auctionContract.FilterAuctionResolved(filterOpts, nil, nil, nil)
			if err != nil {
				log.Error("Could not filter auction resolutions event", "error", err)
				continue
			}
			for it.Next() {
				timeSinceAuctionClose := es.roundTimingInfo.AuctionClosing - es.roundTimingInfo.TimeTilNextRound()
				auctionResolutionLatency.Update(timeSinceAuctionClose.Nanoseconds())
				log.Info(
					"AuctionResolved: New express lane controller assigned",
					"round", it.Event.Round,
					"controller", it.Event.FirstPriceExpressLaneController,
					"timeSinceAuctionClose", timeSinceAuctionClose,
				)
				es.roundControl.Store(it.Event.Round, it.Event.FirstPriceExpressLaneController)
			}
			fromBlock = toBlock + 1
		}
	})
}

func (es *expressLaneService) StopAndWait() {
	es.StopWaiter.StopAndWait()
	if es.redisCoordinator != nil {
		es.redisCoordinator.StopAndWait()
	}
}

func (es *expressLaneService) currentRoundHasController() bool {
	controller, ok := es.roundControl.Load(es.roundTimingInfo.RoundNumber())
	if !ok {
		return false
	}
	return controller != (common.Address{})
}

func (es *expressLaneService) sequenceExpressLaneSubmission(ctx context.Context, msg *timeboost.ExpressLaneSubmission) error {
	unlockByDefer := true
	es.roundInfoMutex.Lock()
	defer func() {
		if unlockByDefer {
			es.roundInfoMutex.Unlock()
		}
	}()

	controller, ok := es.roundControl.Load(msg.Round)
	if !ok {
		return timeboost.ErrNoOnchainController
	}
	sender, err := msg.Sender()
	if err != nil {
		return err
	}
	if sender != controller {
		return timeboost.ErrNotExpressLaneController
	}

	if !es.roundInfo.Contains(msg.Round) {
		es.roundInfo.Add(msg.Round, &expressLaneRoundInfo{
			0,
			make(map[uint64]*msgAndResult),
		})
	}
	roundInfo, _ := es.roundInfo.Get(msg.Round)

	prev, exists := roundInfo.msgAndResultBySequenceNumber[msg.SequenceNumber]
	if msg.SequenceNumber < roundInfo.sequence {
		if exists && bytes.Equal(prev.msg.Signature, msg.Signature) {
			return nil
		}
		return timeboost.ErrSequenceNumberTooLow
	}
	if exists {
		if bytes.Equal(prev.msg.Signature, msg.Signature) {
			return nil
		}
		return timeboost.ErrDuplicateSequenceNumber
	}

	seqConfig := es.seqConfig()
	if msg.SequenceNumber > roundInfo.sequence {
		if seqConfig.Dangerous.Timeboost.MaxQueuedTxCount != 0 &&
			len(roundInfo.msgAndResultBySequenceNumber)-int(roundInfo.sequence) >= seqConfig.Dangerous.Timeboost.MaxQueuedTxCount {
			return fmt.Errorf("reached limit for queuing of future sequence number transactions, please try again with the correct sequence number. Limit: %d, Current sequence number: %d", seqConfig.Dangerous.Timeboost.MaxQueuedTxCount, roundInfo.sequence)
		}
		if msg.SequenceNumber > roundInfo.sequence+seqConfig.Dangerous.Timeboost.MaxFutureSequenceDistance {
			return fmt.Errorf("message sequence number has reached max allowed limit. SequenceNumber: %d, Limit: %d", msg.SequenceNumber, roundInfo.sequence+seqConfig.Dangerous.Timeboost.MaxFutureSequenceDistance)
		}
		log.Info("Received express lane submission with future sequence number", "SequenceNumber", msg.SequenceNumber)
	}

	resultChan := make(chan error, 1)
	roundInfo.msgAndResultBySequenceNumber[msg.SequenceNumber] = &msgAndResult{msg, resultChan}

	if es.redisCoordinator != nil {
		es.LaunchThread(func(context.Context) {
			if err := es.redisCoordinator.AddAcceptedTx(msg); err != nil {
				log.Error("Error adding accepted ExpressLaneSubmission to redis. Loss of msg possible if sequencer switch happens", "seqNum", msg.SequenceNumber, "txHash", msg.Transaction.Hash(), "err", err)
			}
		})
	}

	now := time.Now()
	queueTimeout := seqConfig.QueueTimeout
	for es.roundTimingInfo.RoundNumber() == msg.Round {
		nextMsgAndResult, exists := roundInfo.msgAndResultBySequenceNumber[roundInfo.sequence]
		if !exists {
			break
		}
		queueCtx, _ := ctxWithTimeout(es.GetContext(), queueTimeout)
		if nextMsgAndResult.msg.SequenceNumber == msg.SequenceNumber {
			var cancel context.CancelFunc
			queueCtx, cancel = ctxWithTimeout(ctx, queueTimeout)
			defer cancel()
		}
		es.transactionPublisher.PublishTimeboostedTransaction(queueCtx, nextMsgAndResult.msg.Transaction, nextMsgAndResult.msg.Options, nextMsgAndResult.resultChan)
		roundInfo.sequence += 1
	}

	seqCount := roundInfo.sequence
	es.roundInfo.Add(msg.Round, roundInfo)
	unlockByDefer = false
	es.roundInfoMutex.Unlock()

	abortCtx, cancel := ctxWithTimeout(ctx, queueTimeout*2)
	defer cancel()
	select {
	case err = <-resultChan:
	case <-abortCtx.Done():
		if ctx.Err() == nil {
			log.Warn("Transaction sequencing hit abort deadline", "err", abortCtx.Err(), "submittedAt", now, "TxProcessingTimeout", queueTimeout*2, "txHash", msg.Transaction.Hash())
		}
		err = fmt.Errorf("Transaction sequencing hit timeout, result for the submitted transaction is not yet available: %w", abortCtx.Err())
	}

	if es.redisCoordinator != nil {
		es.LaunchThread(func(context.Context) {
			if redisErr := es.redisCoordinator.UpdateSequenceCount(msg.Round, seqCount); redisErr != nil {
				log.Error("Error updating round's sequence count in redis", "err", redisErr)
			}
		})
	}

	if err != nil {
		return fmt.Errorf("%w: Sequence number: %d (consumed), Transaction hash: %v, Error: %w", timeboost.ErrAcceptedTxFailed, msg.SequenceNumber, msg.Transaction.Hash(), err)
	}
	return nil
}

func (es *expressLaneService) validateExpressLaneTx(msg *timeboost.ExpressLaneSubmission) error {
	if msg == nil || msg.Transaction == nil || msg.Signature == nil {
		return timeboost.ErrMalformedData
	}
	if msg.ChainId.Cmp(es.chainID) != 0 {
		return errors.Wrapf(timeboost.ErrWrongChainId, "express lane tx chain ID %d does not match current chain ID %d", msg.ChainId, es.chainID)
	}
	if msg.AuctionContractAddress != es.auctionContractAddr {
		return errors.Wrapf(timeboost.ErrWrongAuctionContract, "msg auction contract address %s does not match sequencer auction contract address %s", msg.AuctionContractAddress, es.auctionContractAddr)
	}

	currentRound := es.roundTimingInfo.RoundNumber()
	if msg.Round != currentRound {
		timeTilNextRound := es.roundTimingInfo.TimeTilNextRound()
		if msg.Round == currentRound+1 && timeTilNextRound <= es.earlySubmissionGrace {
			time.Sleep(timeTilNextRound)
		} else {
			return errors.Wrapf(timeboost.ErrBadRoundNumber, "express lane tx round %d does not match current round %d", msg.Round, currentRound)
		}
	}

	controller, ok := es.roundControl.Load(msg.Round)
	if !ok {
		return timeboost.ErrNoOnchainController
	}
	sender, err := msg.Sender()
	if err != nil {
		return err
	}
	if sender != controller {
		return timeboost.ErrNotExpressLaneController
	}
	return nil
}

func (es *expressLaneService) syncFromRedis() {
	if es.redisCoordinator == nil {
		return
	}

	currentRound := es.roundTimingInfo.RoundNumber()
	redisSeqCount, err := es.redisCoordinator.GetSequenceCount(currentRound)
	if err != nil {
		log.Error("error fetching current round's global sequence count from redis", "err", err)
	}

	es.roundInfoMutex.Lock()
	roundInfo, exists := es.roundInfo.Get(currentRound)
	if !exists {
		roundInfo = &expressLaneRoundInfo{0, make(map[uint64]*msgAndResult)}
	}
	if redisSeqCount > roundInfo.sequence {
		roundInfo.sequence = redisSeqCount
	}
	es.roundInfo.Add(currentRound, roundInfo)
	sequenceCount := roundInfo.sequence
	es.roundInfoMutex.Unlock()

	pendingMsgs := es.redisCoordinator.GetAcceptedTxs(currentRound, sequenceCount, sequenceCount+es.seqConfig().Dangerous.Timeboost.MaxFutureSequenceDistance)
	log.Info("Attempting to sequence pending expressLane transactions from redis", "count", len(pendingMsgs))
	for _, msg := range pendingMsgs {
		es.LaunchThread(func(ctx context.Context) {
			if err := es.sequenceExpressLaneSubmission(ctx, msg); err != nil {
				log.Error("Untracked expressLaneSubmission returned an error", "round", msg.Round, "seqNum", msg.SequenceNumber, "txHash", msg.Transaction.Hash(), "err", err)
			}
		})
	}
}
