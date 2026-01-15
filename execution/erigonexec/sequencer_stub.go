//go:build !erigon
// +build !erigon

package erigonexec

import (
	"context"
	"time"

	"github.com/ethereum/go-ethereum/arbitrum"
	"github.com/ethereum/go-ethereum/arbitrum_types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"

	"github.com/offchainlabs/nitro/execution/gethexec"
	"github.com/offchainlabs/nitro/timeboost"
	"github.com/offchainlabs/nitro/util/headerreader"
)

type Sequencer struct{}

func NewSequencer(client *Client, l1Reader *headerreader.HeaderReader, configFetcher gethexec.SequencerConfigFetcher) (*Sequencer, error) {
	_ = client
	_ = l1Reader
	_ = configFetcher
	return nil, errNotImplemented
}

func (s *Sequencer) PublishTransaction(ctx context.Context, tx *types.Transaction, options *arbitrum_types.ConditionalOptions) error {
	_ = ctx
	_ = tx
	_ = options
	return errNotImplemented
}

func (s *Sequencer) PublishPriorityTransaction(ctx context.Context, tx *types.Transaction, options *arbitrum_types.ConditionalOptions) error {
	_ = ctx
	_ = tx
	_ = options
	return errNotImplemented
}

func (s *Sequencer) PublishExpressLaneTransaction(ctx context.Context, msg *timeboost.ExpressLaneSubmission) error {
	_ = ctx
	_ = msg
	return errNotImplemented
}

func (s *Sequencer) PublishAuctionResolutionTransaction(ctx context.Context, tx *types.Transaction) error {
	_ = ctx
	_ = tx
	return errNotImplemented
}

func (s *Sequencer) CheckHealth(ctx context.Context) error {
	_ = ctx
	return errNotImplemented
}

func (s *Sequencer) Initialize(ctx context.Context) error {
	_ = ctx
	return errNotImplemented
}

func (s *Sequencer) Start(ctx context.Context) error {
	_ = ctx
	return errNotImplemented
}

func (s *Sequencer) StopAndWait() {}

func (s *Sequencer) Started() bool {
	return false
}

func (s *Sequencer) InitializeExpressLaneService(_ *arbitrum.APIBackend, _ *filters.FilterSystem, _ common.Address, _ common.Address, _ time.Duration) error {
	return errNotImplemented
}

func (s *Sequencer) StartExpressLaneService(_ context.Context) {}
