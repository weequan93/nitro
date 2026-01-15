//go:build !erigon
// +build !erigon

package erigonexec

import (
	"context"

	"github.com/ethereum/go-ethereum/arbitrum_types"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/offchainlabs/nitro/execution/gethexec"
	"github.com/offchainlabs/nitro/timeboost"
)

type TxPreChecker struct {
	current gethexec.TransactionPublisher
}

func NewTxPreChecker(publisher gethexec.TransactionPublisher, client *Client, config gethexec.TxPreCheckerConfigFetcher) *TxPreChecker {
	_ = client
	_ = config
	return &TxPreChecker{current: publisher}
}

func (c *TxPreChecker) PublishTransaction(ctx context.Context, tx *types.Transaction, options *arbitrum_types.ConditionalOptions) error {
	if c.current == nil {
		return errNotImplemented
	}
	return c.current.PublishTransaction(ctx, tx, options)
}

func (c *TxPreChecker) PublishPriorityTransaction(ctx context.Context, tx *types.Transaction, options *arbitrum_types.ConditionalOptions) error {
	if c.current == nil {
		return errNotImplemented
	}
	return c.current.PublishPriorityTransaction(ctx, tx, options)
}

func (c *TxPreChecker) PublishExpressLaneTransaction(ctx context.Context, msg *timeboost.ExpressLaneSubmission) error {
	if c.current == nil {
		return errNotImplemented
	}
	return c.current.PublishExpressLaneTransaction(ctx, msg)
}

func (c *TxPreChecker) PublishAuctionResolutionTransaction(ctx context.Context, tx *types.Transaction) error {
	if c.current == nil {
		return errNotImplemented
	}
	return c.current.PublishAuctionResolutionTransaction(ctx, tx)
}

func (c *TxPreChecker) Swap(next gethexec.TransactionPublisher) error {
	c.current = next
	return nil
}

func (c *TxPreChecker) Current() gethexec.TransactionPublisher {
	return c.current
}

func (c *TxPreChecker) CheckHealth(ctx context.Context) error {
	if c.current == nil {
		return errNotImplemented
	}
	return c.current.CheckHealth(ctx)
}

func (c *TxPreChecker) Initialize(ctx context.Context) error {
	if c.current == nil {
		return errNotImplemented
	}
	return c.current.Initialize(ctx)
}

func (c *TxPreChecker) Start(ctx context.Context) error {
	if c.current == nil {
		return errNotImplemented
	}
	return c.current.Start(ctx)
}

func (c *TxPreChecker) StopAndWait() {
	if c.current != nil {
		c.current.StopAndWait()
	}
}

func (c *TxPreChecker) Started() bool {
	if c.current == nil {
		return false
	}
	return c.current.Started()
}
