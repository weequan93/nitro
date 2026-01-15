//go:build !erigon
// +build !erigon

package erigonexec

import (
	"context"

	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/node"

	"github.com/offchainlabs/nitro/execution"
	"github.com/offchainlabs/nitro/execution/gethexec"
)

func BuildTxPublisher(
	ctx context.Context,
	cfgFetcher func() *gethexec.Config,
	client *Client,
	l1Client *ethclient.Client,
) (gethexec.TransactionPublisher, error) {
	_ = ctx
	_ = cfgFetcher
	_ = client
	_ = l1Client
	return gethexec.NewTxDropper(), errNotImplemented
}

func SetupServices(
	ctx context.Context,
	stack *node.Node,
	execClient execution.ExecutionClient,
	client *Client,
	l1Client *ethclient.Client,
	execConfigFn func() *gethexec.Config,
) (gethexec.TransactionPublisher, error) {
	_ = ctx
	_ = stack
	_ = execClient
	_ = client
	_ = l1Client
	_ = execConfigFn
	return gethexec.NewTxDropper(), errNotImplemented
}
