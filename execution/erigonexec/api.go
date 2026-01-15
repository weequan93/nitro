//go:build !erigon
// +build !erigon

package erigonexec

import (
	"context"
	"errors"

	"github.com/ethereum/go-ethereum/rpc"

	"github.com/offchainlabs/nitro/execution"
	"github.com/offchainlabs/nitro/execution/gethexec"
)

// ArbAPI exposes minimal arb namespace methods for the erigon backend.
type ArbAPI struct {
	exec execution.ExecutionClient
}

func NewArbAPI(exec execution.ExecutionClient) *ArbAPI {
	return &ArbAPI{exec: exec}
}

func (a *ArbAPI) CheckPublisherHealth(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if a.exec == nil {
		return errors.New("execution client not configured")
	}
	_, err := a.exec.HeadMessageNumber()
	return err
}

func (a *ArbAPI) GetRawBlockMetadata(ctx context.Context, fromBlock, toBlock rpc.BlockNumber) ([]gethexec.NumberAndBlockMetadata, error) {
	_ = ctx
	_ = fromBlock
	_ = toBlock
	return nil, errors.New("arb_getRawBlockMetadata is not available")
}
