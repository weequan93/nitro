//go:build erigon
// +build erigon

package erigonexec

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"os"
	"runtime/debug"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	gcommon "github.com/ethereum/go-ethereum/common"
	gtypes "github.com/ethereum/go-ethereum/core/types"

	ecommon "github.com/erigontech/erigon-lib/common"
	ehexutil "github.com/erigontech/erigon-lib/common/hexutil"
	"github.com/erigontech/erigon/eth/filters"
	etypes "github.com/erigontech/erigon/execution/types"
	"github.com/erigontech/erigon/rpc"
	"github.com/erigontech/erigon/rpc/ethapi"
	"github.com/erigontech/erigon/rpc/jsonrpc"
)

// contractAdapter implements bind.ContractBackend using the Erigon RPC stack.
type contractAdapter struct {
	bind.ContractTransactor
	ethAPI *jsonrpc.APIImpl
}

func newContractAdapter(ethAPI *jsonrpc.APIImpl) *contractAdapter {
	return &contractAdapter{ethAPI: ethAPI}
}

func (a *contractAdapter) FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]gtypes.Log, error) {
	if a.ethAPI == nil {
		return nil, errors.New("erigonexec: contract adapter missing eth API")
	}
	crit := filters.FilterCriteria{
		FromBlock: q.FromBlock,
		ToBlock:   q.ToBlock,
		BlockHash: convertBlockHash(q.BlockHash),
		Addresses: convertAddresses(q.Addresses),
		Topics:    convertTopics(q.Topics),
	}
	logs, err := a.ethAPI.GetLogs(ctx, crit)
	if err != nil {
		return nil, err
	}
	out := make([]gtypes.Log, 0, len(logs))
	for _, logEntry := range logs {
		if logEntry == nil {
			continue
		}
		out = append(out, erigonLogToGeth(logEntry))
	}
	return out, nil
}

func (a *contractAdapter) SubscribeFilterLogs(ctx context.Context, q ethereum.FilterQuery, ch chan<- gtypes.Log) (ethereum.Subscription, error) {
	_ = ctx
	_ = q
	_ = ch
	fmt.Fprintf(os.Stderr, "contractAdapter doesn't implement SubscribeFilterLogs: Stack trace:\n%s\n", debug.Stack())
	return nil, errors.New("contractAdapter doesn't implement SubscribeFilterLogs - shouldn't be needed")
}

func (a *contractAdapter) CodeAt(ctx context.Context, contract gcommon.Address, blockNumber *big.Int) ([]byte, error) {
	if a.ethAPI == nil {
		return nil, errors.New("erigonexec: contract adapter missing eth API")
	}
	blockNrOrHash := rpc.BlockNumberOrHashWithNumber(rpc.LatestBlockNumber)
	if blockNumber != nil {
		blockNrOrHash = rpc.BlockNumberOrHashWithNumber(rpc.BlockNumber(blockNumber.Int64()))
	}
	code, err := a.ethAPI.GetCode(ctx, toErigonAddress(contract), blockNrOrHash)
	if err != nil {
		return nil, err
	}
	return []byte(code), nil
}

func (a *contractAdapter) CallContract(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	if a.ethAPI == nil {
		return nil, errors.New("erigonexec: contract adapter missing eth API")
	}
	args := ethapi.CallArgs{}
	if call.From != (gcommon.Address{}) {
		addr := toErigonAddress(call.From)
		args.From = &addr
	}
	if call.To != nil {
		addr := toErigonAddress(*call.To)
		args.To = &addr
	}
	if call.Gas != 0 {
		gas := ehexutil.Uint64(call.Gas)
		args.Gas = &gas
	}
	if call.GasPrice != nil {
		val := ehexutil.Big(*call.GasPrice)
		args.GasPrice = &val
	}
	if call.GasFeeCap != nil {
		val := ehexutil.Big(*call.GasFeeCap)
		args.MaxFeePerGas = &val
	}
	if call.GasTipCap != nil {
		val := ehexutil.Big(*call.GasTipCap)
		args.MaxPriorityFeePerGas = &val
	}
	if call.Value != nil {
		val := ehexutil.Big(*call.Value)
		args.Value = &val
	}
	if call.Data != nil {
		data := ehexutil.Bytes(call.Data)
		args.Data = &data
	}
	if len(call.AccessList) > 0 {
		accessList := convertAccessList(call.AccessList)
		args.AccessList = &accessList
	}
	skipL1 := true
	args.SkipL1Charging = &skipL1

	var block *rpc.BlockNumberOrHash
	if blockNumber != nil {
		blockNrOrHash := rpc.BlockNumberOrHashWithNumber(rpc.BlockNumber(blockNumber.Int64()))
		block = &blockNrOrHash
	}
	res, err := a.ethAPI.Call(ctx, args, block, nil)
	if err != nil {
		return nil, err
	}
	return []byte(res), nil
}

func convertBlockHash(hash *gcommon.Hash) *ecommon.Hash {
	if hash == nil {
		return nil
	}
	converted := ecommon.BytesToHash(hash.Bytes())
	return &converted
}

func convertAddresses(addrs []gcommon.Address) []ecommon.Address {
	if len(addrs) == 0 {
		return nil
	}
	out := make([]ecommon.Address, 0, len(addrs))
	for _, addr := range addrs {
		out = append(out, toErigonAddress(addr))
	}
	return out
}

func convertTopics(topics [][]gcommon.Hash) [][]ecommon.Hash {
	if len(topics) == 0 {
		return nil
	}
	out := make([][]ecommon.Hash, len(topics))
	for i, group := range topics {
		if group == nil {
			out[i] = nil
			continue
		}
		hashes := make([]ecommon.Hash, 0, len(group))
		for _, topic := range group {
			hashes = append(hashes, ecommon.BytesToHash(topic.Bytes()))
		}
		out[i] = hashes
	}
	return out
}

func convertAccessList(list gtypes.AccessList) etypes.AccessList {
	if len(list) == 0 {
		return nil
	}
	out := make(etypes.AccessList, 0, len(list))
	for _, item := range list {
		storageKeys := make([]ecommon.Hash, 0, len(item.StorageKeys))
		for _, key := range item.StorageKeys {
			storageKeys = append(storageKeys, ecommon.BytesToHash(key.Bytes()))
		}
		out = append(out, etypes.AccessTuple{
			Address:     toErigonAddress(item.Address),
			StorageKeys: storageKeys,
		})
	}
	return out
}

func toErigonAddress(addr gcommon.Address) ecommon.Address {
	return ecommon.BytesToAddress(addr.Bytes())
}

func erigonLogToGeth(log *etypes.RPCLog) gtypes.Log {
	entry := log.Log
	out := gtypes.Log{
		Address:     gcommon.BytesToAddress(entry.Address.Bytes()),
		Data:        entry.Data,
		BlockNumber: entry.BlockNumber,
		TxHash:      gcommon.BytesToHash(entry.TxHash.Bytes()),
		TxIndex:     entry.TxIndex,
		BlockHash:   gcommon.BytesToHash(entry.BlockHash.Bytes()),
		Index:       entry.Index,
		Removed:     entry.Removed,
	}
	if len(entry.Topics) > 0 {
		out.Topics = make([]gcommon.Hash, 0, len(entry.Topics))
		for _, topic := range entry.Topics {
			out.Topics = append(out.Topics, gcommon.BytesToHash(topic.Bytes()))
		}
	}
	return out
}
