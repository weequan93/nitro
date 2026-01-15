package erigonexec

import (
	"context"
	"math/big"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/arbitrum_types"
	"github.com/ethereum/go-ethereum/common"
	gtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"

	"github.com/offchainlabs/nitro/timeboost"
)

type countingPublisher struct {
	mu         sync.Mutex
	initCalls  int
	startCalls int
	stopCalls  int
	started    bool
	lastTx     *gtypes.Transaction
}

func (c *countingPublisher) PublishAuctionResolutionTransaction(ctx context.Context, tx *gtypes.Transaction) error {
	return c.PublishTransaction(ctx, tx, nil)
}

func (c *countingPublisher) PublishExpressLaneTransaction(ctx context.Context, msg *timeboost.ExpressLaneSubmission) error {
	return nil
}

func (c *countingPublisher) PublishTransaction(ctx context.Context, tx *gtypes.Transaction, options *arbitrum_types.ConditionalOptions) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastTx = tx
	return nil
}

func (c *countingPublisher) PublishPriorityTransaction(ctx context.Context, tx *gtypes.Transaction, options *arbitrum_types.ConditionalOptions) error {
	return c.PublishTransaction(ctx, tx, options)
}

func (c *countingPublisher) CheckHealth(ctx context.Context) error { return nil }

func (c *countingPublisher) Initialize(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.initCalls++
	return nil
}

func (c *countingPublisher) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.startCalls++
	c.started = true
	return nil
}

func (c *countingPublisher) StopAndWait() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stopCalls++
	c.started = false
}

func (c *countingPublisher) Started() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.started
}

func (c *countingPublisher) snapshot() (initCalls, startCalls, stopCalls int, lastTx *gtypes.Transaction) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.initCalls, c.startCalls, c.stopCalls, c.lastTx
}

func newTestTx() *gtypes.Transaction {
	return gtypes.NewTx(&gtypes.LegacyTx{
		Nonce:    1,
		To:       &common.Address{},
		Value:    big.NewInt(1),
		Gas:      21_000,
		GasPrice: big.NewInt(1),
	})
}

func TestDynamicTxPublisherSwapAfterStart(t *testing.T) {
	ctx := context.Background()
	first := &countingPublisher{}
	second := &countingPublisher{}

	dyn := NewDynamicTxPublisher(first)
	require.NoError(t, dyn.Initialize(ctx))
	require.NoError(t, dyn.Start(ctx))

	initCalls, startCalls, stopCalls, _ := first.snapshot()
	require.Equal(t, 1, initCalls)
	require.Equal(t, 1, startCalls)
	require.Equal(t, 0, stopCalls)

	require.NoError(t, dyn.Swap(second))

	initCalls, startCalls, stopCalls, _ = second.snapshot()
	require.Equal(t, 1, initCalls)
	require.Equal(t, 1, startCalls)
	require.Equal(t, 0, stopCalls)

	_, _, stopCalls, _ = first.snapshot()
	require.Equal(t, 1, stopCalls)

	tx := newTestTx()
	require.NoError(t, dyn.PublishTransaction(ctx, tx, nil))
	_, _, _, last := second.snapshot()
	require.Equal(t, tx.Hash(), last.Hash())

	dyn.StopAndWait()
	_, _, stopCalls, _ = second.snapshot()
	require.Equal(t, 1, stopCalls)
}

func TestDynamicTxPublisherSwapBeforeInit(t *testing.T) {
	ctx := context.Background()
	first := &countingPublisher{}
	second := &countingPublisher{}

	dyn := NewDynamicTxPublisher(first)
	require.NoError(t, dyn.Swap(second))

	initCalls, startCalls, stopCalls, _ := second.snapshot()
	require.Equal(t, 0, initCalls)
	require.Equal(t, 0, startCalls)
	require.Equal(t, 0, stopCalls)

	require.NoError(t, dyn.Initialize(ctx))
	require.NoError(t, dyn.Start(ctx))

	initCalls, startCalls, _, _ = second.snapshot()
	require.Equal(t, 1, initCalls)
	require.Equal(t, 1, startCalls)
}

func TestDynamicTxPublisherSwapNil(t *testing.T) {
	dyn := NewDynamicTxPublisher(&countingPublisher{})
	require.Error(t, dyn.Swap(nil))
}
