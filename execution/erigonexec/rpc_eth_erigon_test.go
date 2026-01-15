//go:build erigon
// +build erigon

package erigonexec

import (
	"context"
	"math/big"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ethereum/go-ethereum/arbitrum_types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	gtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/offchainlabs/nitro/timeboost"
)

type fakePublisher struct {
	mu          sync.Mutex
	last        *gtypes.Transaction
	lastOptions *arbitrum_types.ConditionalOptions
}

func (f *fakePublisher) PublishAuctionResolutionTransaction(ctx context.Context, tx *gtypes.Transaction) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.last = tx
	return nil
}

func (f *fakePublisher) PublishExpressLaneTransaction(ctx context.Context, msg *timeboost.ExpressLaneSubmission) error {
	return nil
}

func (f *fakePublisher) PublishTransaction(ctx context.Context, tx *gtypes.Transaction, options *arbitrum_types.ConditionalOptions) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.last = tx
	f.lastOptions = options
	return nil
}

func (f *fakePublisher) PublishPriorityTransaction(ctx context.Context, tx *gtypes.Transaction, options *arbitrum_types.ConditionalOptions) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.last = tx
	f.lastOptions = options
	return nil
}

func (f *fakePublisher) CheckHealth(ctx context.Context) error { return nil }

func (f *fakePublisher) Initialize(ctx context.Context) error { return nil }

func (f *fakePublisher) Start(ctx context.Context) error { return nil }

func (f *fakePublisher) StopAndWait() {}

func (f *fakePublisher) Started() bool { return true }

func (f *fakePublisher) LastTx() *gtypes.Transaction {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.last
}

func (f *fakePublisher) LastOptions() *arbitrum_types.ConditionalOptions {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastOptions
}

func TestSendRawTransactionForwards(t *testing.T) {
	chain := setupTestChain(t)
	publisher := &fakePublisher{}

	api, err := NewEthAPI(chain.client, publisher)
	require.NoError(t, err)

	tx := gtypes.NewTx(&gtypes.LegacyTx{
		Nonce:    0,
		To:       &common.Address{},
		Value:    big.NewInt(1),
		Gas:      21_000,
		GasPrice: big.NewInt(1),
	})
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	signer := gtypes.NewEIP155Signer(big.NewInt(1))
	signedTx, err := gtypes.SignTx(tx, signer, key)
	require.NoError(t, err)
	raw, err := signedTx.MarshalBinary()
	require.NoError(t, err)

	hash, err := api.SendRawTransaction(context.Background(), raw)
	require.NoError(t, err)
	require.Equal(t, toErigonHash(signedTx.Hash()), hash)

	last := publisher.LastTx()
	require.NotNil(t, last)
	require.Equal(t, signedTx.Hash(), last.Hash())
	require.Nil(t, publisher.LastOptions())
}

func TestSendRawTransactionConditionalForwardsOptions(t *testing.T) {
	chain := setupTestChain(t)
	publisher := &fakePublisher{}

	api, err := NewEthAPI(chain.client, publisher)
	require.NoError(t, err)

	tx := gtypes.NewTx(&gtypes.LegacyTx{
		Nonce:    0,
		To:       &common.Address{},
		Value:    big.NewInt(2),
		Gas:      21_000,
		GasPrice: big.NewInt(1),
	})
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	signer := gtypes.NewEIP155Signer(big.NewInt(1))
	signedTx, err := gtypes.SignTx(tx, signer, key)
	require.NoError(t, err)
	raw, err := signedTx.MarshalBinary()
	require.NoError(t, err)

	ts := math.HexOrDecimal64(123)
	opts := &arbitrum_types.ConditionalOptions{
		TimestampMin: &ts,
	}
	hash, err := api.SendRawTransactionConditional(context.Background(), raw, opts)
	require.NoError(t, err)
	require.Equal(t, toErigonHash(signedTx.Hash()), hash)

	last := publisher.LastTx()
	require.NotNil(t, last)
	require.Equal(t, signedTx.Hash(), last.Hash())
	require.Equal(t, opts, publisher.LastOptions())
}
