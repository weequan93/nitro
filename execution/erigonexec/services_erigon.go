//go:build erigon
// +build erigon

package erigonexec

import (
	"context"
	"fmt"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/rpc"

	"github.com/offchainlabs/nitro/execution"
	"github.com/offchainlabs/nitro/execution/gethexec"
	"github.com/offchainlabs/nitro/solgen/go/precompilesgen"
	"github.com/offchainlabs/nitro/util/headerreader"
)

// BuildTxPublisher creates a tx publisher using the current execution config.
func BuildTxPublisher(
	ctx context.Context,
	cfgFetcher func() *gethexec.Config,
	client *Client,
	l1Client *ethclient.Client,
) (gethexec.TransactionPublisher, error) {
	if cfgFetcher == nil {
		return gethexec.NewTxDropper(), nil
	}
	cfg := cfgFetcher()
	if cfg == nil {
		return gethexec.NewTxDropper(), nil
	}
	if client == nil {
		return nil, fmt.Errorf("erigon tx publisher requires erigon execution client")
	}

	var publisher gethexec.TransactionPublisher
	if cfg.Sequencer.Enable {
		var parentChainReader *headerreader.HeaderReader
		if l1Client != nil {
			arbSys, _ := precompilesgen.NewArbSys(types.ArbSysAddress, l1Client)
			seqParentCfg := func() *headerreader.Config { return &cfgFetcher().ParentChainReader }
			var err error
			parentChainReader, err = headerreader.New(ctx, l1Client, seqParentCfg, arbSys)
			if err != nil {
				return nil, err
			}
		} else {
			log.Warn("sequencer enabled without l1 client")
		}

		seqCfgFetcher := func() *gethexec.SequencerConfig { return &cfgFetcher().Sequencer }
		sequencer, err := NewSequencer(client, parentChainReader, seqCfgFetcher)
		if err != nil {
			return nil, err
		}
		publisher = sequencer
	} else {
		forwardingTarget := cfg.ForwardingTarget
		if forwardingTarget == "null" {
			forwardingTarget = ""
		}
		if cfg.Forwarder.RedisUrl != "" {
			publisher = gethexec.NewRedisTxForwarder(forwardingTarget, &cfg.Forwarder)
		} else if forwardingTarget == "" {
			publisher = gethexec.NewTxDropper()
		} else {
			targets := append([]string{forwardingTarget}, cfg.SecondaryForwardingTarget...)
			publisher = gethexec.NewForwarder(targets, &cfg.Forwarder)
		}
	}

	dynamic := NewDynamicTxPublisher(publisher)
	txPrecheckCfg := func() *gethexec.TxPreCheckerConfig { return &cfgFetcher().TxPreChecker }
	return NewTxPreChecker(dynamic, client, txPrecheckCfg), nil
}

// SetupServices initializes the Erigon tx publisher and registers Erigon RPC APIs.
func SetupServices(
	ctx context.Context,
	stack *node.Node,
	execClient execution.ExecutionClient,
	client *Client,
	l1Client *ethclient.Client,
	execConfigFn func() *gethexec.Config,
) (gethexec.TransactionPublisher, error) {
	if client == nil {
		return nil, fmt.Errorf("missing erigon execution client")
	}

	basePublisher, err := BuildTxPublisher(ctx, execConfigFn, client, l1Client)
	if err != nil {
		return nil, err
	}
	client.SetTxPublisher(basePublisher)
	if err := basePublisher.Initialize(ctx); err != nil {
		return nil, err
	}

	ethAPI, err := NewEthAPI(execClient, basePublisher)
	if err != nil {
		return nil, err
	}
	netAPI, err := NewNetAPI(execClient)
	if err != nil {
		return nil, err
	}
	web3API, err := NewWeb3API(execClient)
	if err != nil {
		return nil, err
	}
	debugAPI, err := NewDebugAPI(execClient)
	if err != nil {
		return nil, err
	}
	traceAPI, err := NewTraceAPI(execClient)
	if err != nil {
		return nil, err
	}

	stack.RegisterAPIs([]rpc.API{
		{
			Namespace: "eth",
			Version:   "1.0",
			Service:   ethAPI,
			Public:    true,
		},
		{
			Namespace: "net",
			Version:   "1.0",
			Service:   netAPI,
			Public:    true,
		},
		{
			Namespace: "web3",
			Version:   "1.0",
			Service:   web3API,
			Public:    true,
		},
		{
			Namespace: "arb",
			Version:   "1.0",
			Service:   NewArbAPI(execClient),
			Public:    false,
		},
		{
			Namespace: "auctioneer",
			Version:   "1.0",
			Service:   gethexec.NewArbTimeboostAuctioneerAPI(basePublisher),
			Public:    false,
		},
		{
			Namespace: "timeboost",
			Version:   "1.0",
			Service:   gethexec.NewArbTimeboostAPI(basePublisher),
			Public:    false,
		},
		{
			Namespace: "debug",
			Version:   "1.0",
			Service:   debugAPI,
			Public:    true,
		},
		{
			Namespace: "trace",
			Version:   "1.0",
			Service:   traceAPI,
			Public:    true,
		},
		{
			Namespace: "txpool",
			Version:   "1.0",
			Service:   NewTxPoolAPI(),
			Public:    true,
		},
	})

	return basePublisher, nil
}
