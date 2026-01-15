//go:build erigon
// +build erigon

package erigonexec

import (
	"context"
	"errors"
	"fmt"
	"time"

	ecommon "github.com/erigontech/erigon-lib/common"
	estate "github.com/erigontech/erigon/core/state"
	"github.com/erigontech/erigon/db/kv"
	"github.com/erigontech/erigon/db/kv/rawdbv3"
	erawdb "github.com/erigontech/erigon/db/rawdb"
	etypes "github.com/erigontech/erigon/execution/types"

	"github.com/ethereum/go-ethereum/arbitrum_types"
	"github.com/ethereum/go-ethereum/common"
	gcore "github.com/ethereum/go-ethereum/core"
	gtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/metrics"
	"github.com/ethereum/go-ethereum/params"

	"github.com/offchainlabs/nitro/arbos"
	"github.com/offchainlabs/nitro/arbos/arbosState"
	"github.com/offchainlabs/nitro/arbos/l1pricing"
	"github.com/offchainlabs/nitro/execution/gethexec"
	"github.com/offchainlabs/nitro/timeboost"
	"github.com/offchainlabs/nitro/util/arbmath"
)

var (
	conditionalTxRejectedByTxPreCheckerCurrentStateCounter = metrics.NewRegisteredCounter("arb/txprechecker/conditionaltx/currentstate/rejected", nil)
	conditionalTxAcceptedByTxPreCheckerCurrentStateCounter = metrics.NewRegisteredCounter("arb/txprechecker/conditionaltx/currentstate/accepted", nil)
	conditionalTxRejectedByTxPreCheckerOldStateCounter     = metrics.NewRegisteredCounter("arb/txprechecker/conditionaltx/oldstate/rejected", nil)
	conditionalTxAcceptedByTxPreCheckerOldStateCounter     = metrics.NewRegisteredCounter("arb/txprechecker/conditionaltx/oldstate/accepted", nil)
)

type TxPreChecker struct {
	publisher gethexec.TransactionPublisher
	client    *Client
	config    gethexec.TxPreCheckerConfigFetcher
}

func NewTxPreChecker(publisher gethexec.TransactionPublisher, client *Client, config gethexec.TxPreCheckerConfigFetcher) *TxPreChecker {
	return &TxPreChecker{
		publisher: publisher,
		client:    client,
		config:    config,
	}
}

func (c *TxPreChecker) preCheckTx(ctx context.Context, tx *gtypes.Transaction, options *arbitrum_types.ConditionalOptions) error {
	cfg := c.config()
	if cfg.Strictness < gethexec.TxPreCheckerStrictnessAlwaysCompatible {
		return nil
	}
	if tx.Gas() < params.TxGas {
		return gcore.ErrIntrinsicGas
	}
	if tx.Type() >= gtypes.ArbitrumDepositTxType || tx.Type() == gtypes.BlobTxType {
		return gtypes.ErrTxTypeNotSupported
	}
	if c.client == nil || c.client.chainDB == nil {
		return fmt.Errorf("erigonexec: tx prechecker missing execution client")
	}
	temporalDB, ok := c.client.chainDB.(kv.TemporalRoDB)
	if !ok {
		return fmt.Errorf("erigonexec: chain db missing temporal support")
	}
	dbtx, err := temporalDB.BeginTemporalRo(ctx)
	if err != nil {
		return err
	}
	defer dbtx.Rollback()

	header := erawdb.ReadCurrentHeader(dbtx)
	if header == nil {
		return fmt.Errorf("erigonexec: tx prechecker missing latest header")
	}
	maxTxNum, err := rawdbv3.TxNums.Max(dbtx, header.Number.Uint64())
	if err != nil {
		return err
	}
	stateReader := estate.NewHistoryReaderV3()
	stateReader.SetTx(dbtx)
	stateReader.SetTxNum(maxTxNum)
	ibs := estate.New(stateReader)
	ibsArb := estate.NewArbitrum(ibs)

	stateAdapter := arbos.NewStateDBAdapter(ibsArb, nil)
	arbState, err := arbosState.OpenSystemArbosState(stateAdapter, nil, true)
	if err != nil {
		return err
	}

	baseFee := header.BaseFee
	if baseFee == nil {
		baseFee = common.Big0
	}
	if cfg.Strictness < gethexec.TxPreCheckerStrictnessLikelyCompatible {
		baseFee, err = arbState.L2PricingState().MinBaseFeeWei()
		if err != nil {
			return err
		}
	}

	signer := gtypes.LatestSignerForChainID(c.client.chainConfig.ChainID)
	sender, err := gtypes.Sender(signer, tx)
	if err != nil {
		return err
	}
	isGasless := arbState.Pricer().IsCustomPriceTxCheck(tx)
	if arbmath.BigLessThan(tx.GasFeeCap(), baseFee) && !isGasless {
		return fmt.Errorf("PreCheckTx() %w: address %v, maxFeePerGas: %s baseFee: %s", gcore.ErrFeeCapTooLow, sender, tx.GasFeeCap(), baseFee)
	}

	stateNonce, err := ibsArb.GetNonce(ecommon.Address(sender))
	if err != nil {
		return err
	}
	if tx.Nonce() < stateNonce {
		return gethexec.MakeNonceError(sender, tx.Nonce(), stateNonce)
	}

	extraInfo := etypes.DeserializeHeaderExtraInformation(header)
	intrinsic, err := gcore.IntrinsicGas(
		tx.Data(),
		tx.AccessList(),
		tx.To() == nil,
		c.client.chainConfig.IsHomestead(header.Number.Uint64()),
		c.client.chainConfig.IsIstanbul(header.Number.Uint64()),
		c.client.chainConfig.IsShanghai(header.Time, extraInfo.ArbOSFormatVersion),
	)
	if err != nil {
		return err
	}
	if tx.Gas() < intrinsic {
		return gcore.ErrIntrinsicGas
	}
	if cfg.Strictness < gethexec.TxPreCheckerStrictnessLikelyCompatible {
		return nil
	}

	if options != nil {
		if err := checkConditionalOptions(options, extraInfo.L1BlockNumber, header.Time, ibsArb); err != nil {
			conditionalTxRejectedByTxPreCheckerCurrentStateCounter.Inc(1)
			return err
		}
		conditionalTxAcceptedByTxPreCheckerCurrentStateCounter.Inc(1)
		if cfg.RequiredStateAge > 0 {
			now := time.Now().Unix()
			oldHeader := header
			blocksTraversed := uint(0)
			for now-int64(oldHeader.Time) < cfg.RequiredStateAge &&
				(cfg.RequiredStateMaxBlocks <= 0 || blocksTraversed < cfg.RequiredStateMaxBlocks) &&
				oldHeader.Number.Uint64() > 0 {
				prevHeader := erawdb.ReadHeaderByNumber(dbtx, oldHeader.Number.Uint64()-1)
				if prevHeader == nil {
					break
				}
				oldHeader = prevHeader
				blocksTraversed++
			}
			if oldHeader.Hash() != header.Hash() {
				maxTxNum, err := rawdbv3.TxNums.Max(dbtx, oldHeader.Number.Uint64())
				if err != nil {
					return err
				}
				oldStateReader := estate.NewHistoryReaderV3()
				oldStateReader.SetTx(dbtx)
				oldStateReader.SetTxNum(maxTxNum)
				oldIbs := estate.New(oldStateReader)
				oldIbsArb := estate.NewArbitrum(oldIbs)
				oldExtra := etypes.DeserializeHeaderExtraInformation(oldHeader)
				if err := checkConditionalOptions(options, oldExtra.L1BlockNumber, oldHeader.Time, oldIbsArb); err != nil {
					conditionalTxRejectedByTxPreCheckerOldStateCounter.Inc(1)
					return arbitrum_types.WrapOptionsCheckError(err, "conditions check failed for old state")
				}
				conditionalTxAcceptedByTxPreCheckerOldStateCounter.Inc(1)
			}
		}
	}

	balance, err := ibsArb.GetBalance(ecommon.Address(sender))
	if err != nil {
		return err
	}
	if arbmath.BigLessThan(balance.ToBig(), tx.Cost()) && !isGasless {
		return fmt.Errorf("%w: address %v have %v want %v", gcore.ErrInsufficientFunds, sender, balance, tx.Cost())
	}
	if cfg.Strictness >= gethexec.TxPreCheckerStrictnessFullValidation && tx.Nonce() > stateNonce {
		return gethexec.MakeNonceError(sender, tx.Nonce(), stateNonce)
	}
	brotliCompressionLevel, err := arbState.BrotliCompressionLevel()
	if err != nil {
		return fmt.Errorf("failed to get brotli compression level: %w", err)
	}
	if baseFee.Sign() > 0 {
		dataCost, _ := arbState.L1PricingState().GetPosterInfo(tx, l1pricing.BatchPosterAddress, brotliCompressionLevel)
		dataGas := arbmath.BigDiv(dataCost, baseFee)
		if tx.Gas() < intrinsic+dataGas.Uint64() {
			return gcore.ErrIntrinsicGas
		}
	}
	return nil
}

func (c *TxPreChecker) PublishTransaction(ctx context.Context, tx *gtypes.Transaction, options *arbitrum_types.ConditionalOptions) error {
	if err := c.preCheckTx(ctx, tx, options); err != nil {
		return err
	}
	if c.publisher == nil {
		return errors.New("erigonexec: tx prechecker missing publisher")
	}
	return c.publisher.PublishTransaction(ctx, tx, options)
}

func (c *TxPreChecker) PublishPriorityTransaction(ctx context.Context, tx *gtypes.Transaction, options *arbitrum_types.ConditionalOptions) error {
	if err := c.preCheckTx(ctx, tx, options); err != nil {
		return err
	}
	if c.publisher == nil {
		return errors.New("erigonexec: tx prechecker missing publisher")
	}
	return c.publisher.PublishPriorityTransaction(ctx, tx, options)
}

func (c *TxPreChecker) PublishExpressLaneTransaction(ctx context.Context, msg *timeboost.ExpressLaneSubmission) error {
	if c.publisher == nil {
		return errors.New("erigonexec: tx prechecker missing publisher")
	}
	return c.publisher.PublishExpressLaneTransaction(ctx, msg)
}

func (c *TxPreChecker) PublishAuctionResolutionTransaction(ctx context.Context, tx *gtypes.Transaction) error {
	if c.publisher == nil {
		return errors.New("erigonexec: tx prechecker missing publisher")
	}
	return c.publisher.PublishAuctionResolutionTransaction(ctx, tx)
}

func (c *TxPreChecker) Swap(next gethexec.TransactionPublisher) error {
	if ctl, ok := c.publisher.(TxPublisherController); ok {
		return ctl.Swap(next)
	}
	return errors.New("erigonexec: tx prechecker underlying publisher does not support swap")
}

func (c *TxPreChecker) Current() gethexec.TransactionPublisher {
	if ctl, ok := c.publisher.(TxPublisherController); ok {
		return ctl.Current()
	}
	return c.publisher
}

func (c *TxPreChecker) CheckHealth(ctx context.Context) error {
	if c.publisher == nil {
		return errors.New("erigonexec: tx prechecker missing publisher")
	}
	return c.publisher.CheckHealth(ctx)
}

func (c *TxPreChecker) Initialize(ctx context.Context) error {
	if c.publisher == nil {
		return errors.New("erigonexec: tx prechecker missing publisher")
	}
	return c.publisher.Initialize(ctx)
}

func (c *TxPreChecker) Start(ctx context.Context) error {
	if c.publisher == nil {
		return errors.New("erigonexec: tx prechecker missing publisher")
	}
	return c.publisher.Start(ctx)
}

func (c *TxPreChecker) StopAndWait() {
	if c.publisher != nil {
		c.publisher.StopAndWait()
	}
}

func (c *TxPreChecker) Started() bool {
	if c.publisher == nil {
		return false
	}
	return c.publisher.Started()
}
