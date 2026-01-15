package erigonexec

import (
	"context"
	"errors"
	"sync"

	"github.com/ethereum/go-ethereum/arbitrum_types"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/offchainlabs/nitro/execution/gethexec"
	"github.com/offchainlabs/nitro/timeboost"
)

// TxPublisherController allows swapping the underlying publisher at runtime.
type TxPublisherController interface {
	gethexec.TransactionPublisher
	Swap(gethexec.TransactionPublisher) error
	Current() gethexec.TransactionPublisher
}

// DynamicTxPublisher wraps a TransactionPublisher and allows swapping it at runtime.
type DynamicTxPublisher struct {
	mu          sync.Mutex
	current     gethexec.TransactionPublisher
	initCtx     context.Context
	startCtx    context.Context
	initialized bool
	started     bool
}

func NewDynamicTxPublisher(initial gethexec.TransactionPublisher) *DynamicTxPublisher {
	return &DynamicTxPublisher{current: initial}
}

func (d *DynamicTxPublisher) Current() gethexec.TransactionPublisher {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.current
}

func (d *DynamicTxPublisher) Swap(next gethexec.TransactionPublisher) error {
	if next == nil {
		return errors.New("erigonexec: transaction publisher is nil")
	}
	d.mu.Lock()
	initCtx := d.initCtx
	startCtx := d.startCtx
	initialized := d.initialized
	started := d.started
	d.mu.Unlock()

	if initialized {
		if err := next.Initialize(initCtx); err != nil {
			return err
		}
	}
	if started {
		if err := next.Start(startCtx); err != nil {
			return err
		}
	}

	d.mu.Lock()
	old := d.current
	d.current = next
	d.mu.Unlock()

	if old != nil && old != next && old.Started() {
		old.StopAndWait()
	}
	return nil
}

func (d *DynamicTxPublisher) PublishAuctionResolutionTransaction(ctx context.Context, tx *types.Transaction) error {
	cur := d.Current()
	if cur == nil {
		return errors.New("erigonexec: transaction publisher not configured")
	}
	return cur.PublishAuctionResolutionTransaction(ctx, tx)
}

func (d *DynamicTxPublisher) PublishExpressLaneTransaction(ctx context.Context, msg *timeboost.ExpressLaneSubmission) error {
	cur := d.Current()
	if cur == nil {
		return errors.New("erigonexec: transaction publisher not configured")
	}
	return cur.PublishExpressLaneTransaction(ctx, msg)
}

func (d *DynamicTxPublisher) PublishTransaction(ctx context.Context, tx *types.Transaction, options *arbitrum_types.ConditionalOptions) error {
	cur := d.Current()
	if cur == nil {
		return errors.New("erigonexec: transaction publisher not configured")
	}
	return cur.PublishTransaction(ctx, tx, options)
}

func (d *DynamicTxPublisher) PublishPriorityTransaction(ctx context.Context, tx *types.Transaction, options *arbitrum_types.ConditionalOptions) error {
	cur := d.Current()
	if cur == nil {
		return errors.New("erigonexec: transaction publisher not configured")
	}
	return cur.PublishPriorityTransaction(ctx, tx, options)
}

func (d *DynamicTxPublisher) CheckHealth(ctx context.Context) error {
	cur := d.Current()
	if cur == nil {
		return errors.New("erigonexec: transaction publisher not configured")
	}
	return cur.CheckHealth(ctx)
}

func (d *DynamicTxPublisher) Initialize(ctx context.Context) error {
	d.mu.Lock()
	d.initCtx = ctx
	d.initialized = true
	cur := d.current
	d.mu.Unlock()
	if cur == nil {
		return errors.New("erigonexec: transaction publisher not configured")
	}
	return cur.Initialize(ctx)
}

func (d *DynamicTxPublisher) Start(ctx context.Context) error {
	d.mu.Lock()
	d.startCtx = ctx
	d.started = true
	cur := d.current
	d.mu.Unlock()
	if cur == nil {
		return errors.New("erigonexec: transaction publisher not configured")
	}
	return cur.Start(ctx)
}

func (d *DynamicTxPublisher) StopAndWait() {
	d.mu.Lock()
	cur := d.current
	d.started = false
	d.mu.Unlock()
	if cur != nil {
		cur.StopAndWait()
	}
}

func (d *DynamicTxPublisher) Started() bool {
	cur := d.Current()
	if cur == nil {
		return false
	}
	return cur.Started()
}
