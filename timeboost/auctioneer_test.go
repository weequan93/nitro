package timeboost

import (
	"context"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/offchainlabs/nitro/cmd/genericconf"
	"github.com/offchainlabs/nitro/pubsub"
	"github.com/offchainlabs/nitro/util/redisutil"
	"github.com/stretchr/testify/require"
)

func TestBidValidatorAuctioneerRedisStream(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	testSetup := setupAuctionTest(t, ctx)
	redisURL := redisutil.CreateTestRedis(ctx, t)

	// Set up multiple bid validators that will receive bids via RPC using a bidder client.
	// They inject their validated bids into a Redis stream that a single auctioneer instance
	// will then consume.
	numBidValidators := 3
	bidValidators := make([]*BidValidator, numBidValidators)
	for i := 0; i < numBidValidators; i++ {
		randHttp := getRandomPort(t)
		stackConf := node.Config{
			DataDir:             "", // ephemeral.
			HTTPPort:            randHttp,
			HTTPModules:         []string{AuctioneerNamespace},
			HTTPHost:            "localhost",
			HTTPVirtualHosts:    []string{"localhost"},
			HTTPTimeouts:        rpc.DefaultHTTPTimeouts,
			WSPort:              getRandomPort(t),
			WSModules:           []string{AuctioneerNamespace},
			WSHost:              "localhost",
			GraphQLVirtualHosts: []string{"localhost"},
			P2P: p2p.Config{
				ListenAddr:  "",
				NoDial:      true,
				NoDiscovery: true,
			},
		}
		stack, err := node.New(&stackConf)
		require.NoError(t, err)
		cfg := &BidValidatorConfig{
			SequencerEndpoint:      testSetup.endpoint,
			AuctionContractAddress: testSetup.expressLaneAuctionAddr.Hex(),
			RedisURL:               redisURL,
			ProducerConfig:         pubsub.TestProducerConfig,
		}
		fetcher := func() *BidValidatorConfig {
			return cfg
		}
		bidValidator, err := NewBidValidator(
			ctx,
			stack,
			fetcher,
		)
		require.NoError(t, err)
		require.NoError(t, bidValidator.Initialize(ctx))
		require.NoError(t, stack.Start())
		bidValidator.Start(ctx)
		bidValidators[i] = bidValidator
	}
	t.Log("Started multiple bid validators")

	// Set up a single auctioneer instance that can consume messages produced
	// by the bid validators from a redis stream.
	cfg := &AuctioneerServerConfig{
		SequencerEndpoint:      testSetup.endpoint,
		AuctionContractAddress: testSetup.expressLaneAuctionAddr.Hex(),
		RedisURL:               redisURL,
		ConsumerConfig:         pubsub.TestConsumerConfig,
		Wallet: genericconf.WalletConfig{
			PrivateKey: fmt.Sprintf("%x", testSetup.accounts[0].privKey.D.Bytes()),
		},
	}
	fetcher := func() *AuctioneerServerConfig {
		return cfg
	}
	am, err := NewAuctioneerServer(
		ctx,
		fetcher,
	)
	require.NoError(t, err)
	am.Start(ctx)
	t.Log("Started auctioneer")

	// Now, we set up bidder clients for Alice, Bob, and Charlie.
	aliceAddr := testSetup.accounts[1].txOpts.From
	bobAddr := testSetup.accounts[2].txOpts.From
	charlieAddr := testSetup.accounts[3].txOpts.From
	alice := setupBidderClient(t, ctx, "alice", testSetup.accounts[1], testSetup, bidValidators[0].stack.HTTPEndpoint())
	bob := setupBidderClient(t, ctx, "bob", testSetup.accounts[2], testSetup, bidValidators[1].stack.HTTPEndpoint())
	charlie := setupBidderClient(t, ctx, "charlie", testSetup.accounts[3], testSetup, bidValidators[2].stack.HTTPEndpoint())
	require.NoError(t, alice.Deposit(ctx, big.NewInt(20)))
	require.NoError(t, bob.Deposit(ctx, big.NewInt(20)))
	require.NoError(t, charlie.Deposit(ctx, big.NewInt(20)))

	info, err := alice.auctionContract.RoundTimingInfo(&bind.CallOpts{})
	require.NoError(t, err)
	timeToWait := time.Until(time.Unix(int64(info.OffsetTimestamp), 0))
	t.Logf("Waiting for %v to start the bidding round, %v", timeToWait, time.Now())
	<-time.After(timeToWait)
	time.Sleep(time.Millisecond * 250) // Add 1/4 of a second of wait so that we are definitely within a round.

	// Alice, Bob, and Charlie will submit bids to the three different bid validators instances.
	start := time.Now()
	for i := 1; i <= 5; i++ {
		alice.Bid(ctx, big.NewInt(int64(i)), aliceAddr)
		bob.Bid(ctx, big.NewInt(int64(i)+1), bobAddr)         // Bob bids 1 wei higher than Alice.
		charlie.Bid(ctx, big.NewInt(int64(i)+2), charlieAddr) // Charlie bids 2 wei higher than the Bob.
	}

	// We expect that a final submission from each fails, as the bid limit is exceeded.
	_, err = alice.Bid(ctx, big.NewInt(6), aliceAddr)
	require.ErrorContains(t, err, ErrTooManyBids.Error())
	_, err = bob.Bid(ctx, big.NewInt(7), bobAddr) // Bob bids 1 wei higher than Alice.
	require.ErrorContains(t, err, ErrTooManyBids.Error())
	_, err = charlie.Bid(ctx, big.NewInt(8), charlieAddr) // Charlie bids 2 wei higher than the Bob.
	require.ErrorContains(t, err, ErrTooManyBids.Error())

	t.Log("Submitted bids", time.Now(), time.Since(start))
	time.Sleep(time.Second * 15)

	// We verify that the auctioneer has consumed all validated bids from the single Redis stream.
	// We also verify the top two bids are those we expect.
	require.Equal(t, 3, len(am.bidCache.bidsByExpressLaneControllerAddr))
	result := am.bidCache.topTwoBids()
	require.Equal(t, result.firstPlace.Amount, big.NewInt(7))
	require.Equal(t, result.firstPlace.Bidder, charlieAddr)
	require.Equal(t, result.secondPlace.Amount, big.NewInt(6))
	require.Equal(t, result.secondPlace.Bidder, bobAddr)
}
