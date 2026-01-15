//go:build erigon
// +build erigon

package erigonexec

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/rpc"

	"github.com/offchainlabs/nitro/execution"
	"github.com/offchainlabs/nitro/execution/gethexec"
	"github.com/offchainlabs/nitro/util/dbutil"
)

const blockMetadataKeyPrefix = byte('t')

// ArbAPI exposes minimal arb namespace methods for the erigon backend.
type ArbAPI struct {
	exec   execution.ExecutionClient
	client *Client
}

func NewArbAPI(exec execution.ExecutionClient) *ArbAPI {
	client, _ := exec.(*Client)
	return &ArbAPI{exec: exec, client: client}
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
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if a.exec == nil {
		return nil, errors.New("execution client not configured")
	}
	if a.client == nil || a.client.arbDB == nil {
		return nil, errors.New("arb db not configured")
	}
	headMsg, err := a.exec.HeadMessageNumber()
	if err != nil {
		return nil, err
	}
	current := rpc.BlockNumber(a.exec.MessageIndexToBlockNumber(headMsg))
	genesis := rpc.BlockNumber(a.client.chainConfig.ArbitrumChainParams.GenesisBlockNum)
	start := clipBlockNumber(fromBlock, current, genesis)
	end := clipBlockNumber(toBlock, current, genesis)
	if start > end {
		return nil, fmt.Errorf("invalid inputs, fromBlock: %d is greater than toBlock: %d", start, end)
	}
	requested := uint64(end-start) + 1
	if a.client.blockMetadataLimit > 0 && requested > a.client.blockMetadataLimit {
		return nil, fmt.Errorf("%w. Range requested- %d, Limit- %d", gethexec.ErrBlockMetadataApiBlocksLimitExceeded, requested, a.client.blockMetadataLimit)
	}
	result := make([]gethexec.NumberAndBlockMetadata, 0)
	for blockNum := uint64(start); blockNum <= uint64(end); blockNum++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		pos, err := a.exec.BlockNumberToMessageIndex(blockNum)
		if err != nil {
			return nil, err
		}
		data, err := a.client.arbDB.Get(blockMetadataKey(uint64(pos)))
		if err != nil {
			if dbutil.IsErrNotFound(err) {
				continue
			}
			return nil, err
		}
		result = append(result, gethexec.NumberAndBlockMetadata{
			BlockNumber: blockNum,
			RawMetadata: hexutil.Bytes(data),
		})
	}
	return result, nil
}

func clipBlockNumber(blockNum, current, genesis rpc.BlockNumber) rpc.BlockNumber {
	if blockNum == rpc.LatestBlockNumber || blockNum == rpc.PendingBlockNumber {
		blockNum = current
	}
	if blockNum > current {
		blockNum = current
	}
	if blockNum < genesis {
		blockNum = genesis
	}
	return blockNum
}

func blockMetadataKey(pos uint64) []byte {
	key := make([]byte, 9)
	key[0] = blockMetadataKeyPrefix
	binary.BigEndian.PutUint64(key[1:], pos)
	return key
}
