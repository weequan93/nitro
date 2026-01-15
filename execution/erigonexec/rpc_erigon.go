//go:build erigon
// +build erigon

package erigonexec

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"runtime"
	"runtime/debug"

	"github.com/erigontech/erigon-lib/common"
	"github.com/erigontech/erigon-lib/common/hexutil"
	estate "github.com/erigontech/erigon/core/state"
	"github.com/erigontech/erigon/cmd/rpcdaemon/cli/httpcfg"
	"github.com/erigontech/erigon/db/kv"
	"github.com/erigontech/erigon/db/kv/kvcache"
	"github.com/erigontech/erigon/eth/ethconfig"
	tracersConfig "github.com/erigontech/erigon/eth/tracers/config"
	"github.com/erigontech/erigon/execution/consensus/ethash"
	"github.com/erigontech/erigon/rpc"
	"github.com/erigontech/erigon/rpc/ethapi"
	"github.com/erigontech/erigon/rpc/jsonrpc"
	"github.com/erigontech/erigon/rpc/jsonstream"
	"github.com/erigontech/erigon/rpc/rpccfg"
	"github.com/erigontech/erigon/rpc/rpchelper"

	"github.com/offchainlabs/nitro/execution"
)

type rpcDeps struct {
	base *jsonrpc.BaseAPI
	db   kv.TemporalRoDB
}

func newRPCDeps(ctx context.Context, exec execution.ExecutionClient) (*rpcDeps, *rpcBackend, error) {
	client, ok := exec.(*Client)
	if !ok || client == nil {
		return nil, nil, errors.New("erigonexec: execution client is not erigon")
	}
	if client.chainDB == nil {
		return nil, nil, errors.New("erigonexec: chain db not initialized")
	}
	temporalDB, ok := client.chainDB.(kv.TemporalRoDB)
	if !ok {
		return nil, nil, errors.New("erigonexec: chain db missing temporal support")
	}
	dirs, err := buildExecDirs(client.cfg.ChainDir)
	if err != nil {
		return nil, nil, err
	}
	backend := newRPCBackend(client)
	filters := rpchelper.New(ctx, rpchelper.DefaultFiltersConfig, backend, nil, nil, func() {}, client.logger)
	stateCache := kvcache.NewDummy()
	engine := ethash.NewFaker()
	base := jsonrpc.NewBaseApi(filters, stateCache, client.blockReader, true, rpccfg.DefaultEvmCallTimeout, engine, dirs, nil)
	return &rpcDeps{
		base: base,
		db:   temporalDB,
	}, backend, nil
}

type DebugAPI struct {
	impl   *jsonrpc.DebugAPIImpl
	client *Client
}

func NewDebugAPI(exec execution.ExecutionClient) (*DebugAPI, error) {
	deps, _, err := newRPCDeps(context.Background(), exec)
	if err != nil {
		return nil, err
	}
	client, _ := exec.(*Client)
	return &DebugAPI{
		impl:   jsonrpc.NewPrivateDebugAPI(deps.base, deps.db, ethconfig.Defaults.RPCGasCap),
		client: client,
	}, nil
}

func (api *DebugAPI) wrapHistoryErr(err error) error {
	if api == nil || api.client == nil {
		return err
	}
	return api.client.wrapHistoryError(err)
}

func (api *DebugAPI) StorageRangeAt(ctx context.Context, blockHash common.Hash, txIndex uint64, contractAddress common.Address, keyStart hexutil.Bytes, maxResult int) (jsonrpc.StorageRangeResult, error) {
	result, err := api.impl.StorageRangeAt(ctx, blockHash, txIndex, contractAddress, keyStart, maxResult)
	return result, api.wrapHistoryErr(err)
}

func (api *DebugAPI) AccountRange(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash, start interface{}, maxResults int, nocode, nostorage bool, incompletes *bool) (estate.IteratorDump, error) {
	result, err := api.impl.AccountRange(ctx, blockNrOrHash, start, maxResults, nocode, nostorage, incompletes)
	return result, api.wrapHistoryErr(err)
}

func (api *DebugAPI) GetModifiedAccountsByNumber(ctx context.Context, startNum rpc.BlockNumber, endNum *rpc.BlockNumber) ([]common.Address, error) {
	result, err := api.impl.GetModifiedAccountsByNumber(ctx, startNum, endNum)
	return result, api.wrapHistoryErr(err)
}

func (api *DebugAPI) GetModifiedAccountsByHash(ctx context.Context, startHash common.Hash, endHash *common.Hash) ([]common.Address, error) {
	result, err := api.impl.GetModifiedAccountsByHash(ctx, startHash, endHash)
	return result, api.wrapHistoryErr(err)
}

func (api *DebugAPI) AccountAt(ctx context.Context, blockHash common.Hash, txIndex uint64, account common.Address) (*jsonrpc.AccountResult, error) {
	result, err := api.impl.AccountAt(ctx, blockHash, txIndex, account)
	return result, api.wrapHistoryErr(err)
}

func (api *DebugAPI) GetRawHeader(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (hexutil.Bytes, error) {
	result, err := api.impl.GetRawHeader(ctx, blockNrOrHash)
	return result, api.wrapHistoryErr(err)
}

func (api *DebugAPI) GetRawBlock(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (hexutil.Bytes, error) {
	result, err := api.impl.GetRawBlock(ctx, blockNrOrHash)
	return result, api.wrapHistoryErr(err)
}

func (api *DebugAPI) GetRawReceipts(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) ([]hexutil.Bytes, error) {
	result, err := api.impl.GetRawReceipts(ctx, blockNrOrHash)
	return result, api.wrapHistoryErr(err)
}

func (api *DebugAPI) GetBadBlocks(ctx context.Context) ([]map[string]interface{}, error) {
	result, err := api.impl.GetBadBlocks(ctx)
	return result, api.wrapHistoryErr(err)
}

func (api *DebugAPI) GetRawTransaction(ctx context.Context, hash common.Hash) (hexutil.Bytes, error) {
	result, err := api.impl.GetRawTransaction(ctx, hash)
	return result, api.wrapHistoryErr(err)
}

func (api *DebugAPI) FreeOSMemory() {
	api.impl.FreeOSMemory()
}

func (api *DebugAPI) SetGCPercent(v int) int {
	return api.impl.SetGCPercent(v)
}

func (api *DebugAPI) SetMemoryLimit(limit int64) int64 {
	return api.impl.SetMemoryLimit(limit)
}

func (api *DebugAPI) GcStats() *debug.GCStats {
	return api.impl.GcStats()
}

func (api *DebugAPI) MemStats() *runtime.MemStats {
	return api.impl.MemStats()
}

func (api *DebugAPI) TraceTransaction(ctx context.Context, hash common.Hash, config *tracersConfig.TraceConfig) (json.RawMessage, error) {
	result, err := traceToRaw(func(stream jsonstream.Stream) error {
		return api.impl.TraceTransaction(ctx, hash, config, stream)
	})
	return result, api.wrapHistoryErr(err)
}

func (api *DebugAPI) TraceBlockByHash(ctx context.Context, hash common.Hash, config *tracersConfig.TraceConfig) (json.RawMessage, error) {
	result, err := traceToRaw(func(stream jsonstream.Stream) error {
		return api.impl.TraceBlockByHash(ctx, hash, config, stream)
	})
	return result, api.wrapHistoryErr(err)
}

func (api *DebugAPI) TraceBlockByNumber(ctx context.Context, number rpc.BlockNumber, config *tracersConfig.TraceConfig) (json.RawMessage, error) {
	result, err := traceToRaw(func(stream jsonstream.Stream) error {
		return api.impl.TraceBlockByNumber(ctx, number, config, stream)
	})
	return result, api.wrapHistoryErr(err)
}

func (api *DebugAPI) TraceCall(ctx context.Context, args ethapi.CallArgs, blockNrOrHash rpc.BlockNumberOrHash, config *tracersConfig.TraceConfig) (json.RawMessage, error) {
	result, err := traceToRaw(func(stream jsonstream.Stream) error {
		return api.impl.TraceCall(ctx, args, blockNrOrHash, config, stream)
	})
	return result, api.wrapHistoryErr(err)
}

type TraceAPI struct {
	impl   *jsonrpc.TraceAPIImpl
	client *Client
}

func NewTraceAPI(exec execution.ExecutionClient) (*TraceAPI, error) {
	deps, _, err := newRPCDeps(context.Background(), exec)
	if err != nil {
		return nil, err
	}
	cfg := &httpcfg.HttpCfg{
		Gascap:            ethconfig.Defaults.RPCGasCap,
		MaxTraces:         200,
		TraceCompatibility: false,
		EvmCallTimeout:    rpccfg.DefaultEvmCallTimeout,
	}
	client, _ := exec.(*Client)
	return &TraceAPI{
		impl:   jsonrpc.NewTraceAPI(deps.base, deps.db, cfg),
		client: client,
	}, nil
}

func (api *TraceAPI) wrapHistoryErr(err error) error {
	if api == nil || api.client == nil {
		return err
	}
	return api.client.wrapHistoryError(err)
}

func (api *TraceAPI) ReplayBlockTransactions(ctx context.Context, blockNr rpc.BlockNumberOrHash, traceTypes []string, gasBailOut *bool, traceConfig *tracersConfig.TraceConfig) ([]*jsonrpc.TraceCallResult, error) {
	result, err := api.impl.ReplayBlockTransactions(ctx, blockNr, traceTypes, gasBailOut, traceConfig)
	return result, api.wrapHistoryErr(err)
}

func (api *TraceAPI) ReplayTransaction(ctx context.Context, txHash common.Hash, traceTypes []string, gasBailOut *bool, traceConfig *tracersConfig.TraceConfig) (*jsonrpc.TraceCallResult, error) {
	result, err := api.impl.ReplayTransaction(ctx, txHash, traceTypes, gasBailOut, traceConfig)
	return result, api.wrapHistoryErr(err)
}

func (api *TraceAPI) Call(ctx context.Context, call jsonrpc.TraceCallParam, types []string, blockNr *rpc.BlockNumberOrHash, traceConfig *tracersConfig.TraceConfig) (*jsonrpc.TraceCallResult, error) {
	result, err := api.impl.Call(ctx, call, types, blockNr, traceConfig)
	return result, api.wrapHistoryErr(err)
}

func (api *TraceAPI) CallMany(ctx context.Context, calls json.RawMessage, blockNr *rpc.BlockNumberOrHash, traceConfig *tracersConfig.TraceConfig) ([]*jsonrpc.TraceCallResult, error) {
	result, err := api.impl.CallMany(ctx, calls, blockNr, traceConfig)
	return result, api.wrapHistoryErr(err)
}

func (api *TraceAPI) RawTransaction(ctx context.Context, txHash common.Hash, traceTypes []string) ([]interface{}, error) {
	result, err := api.impl.RawTransaction(ctx, txHash, traceTypes)
	return result, api.wrapHistoryErr(err)
}

func (api *TraceAPI) Transaction(ctx context.Context, txHash common.Hash, gasBailOut *bool, traceConfig *tracersConfig.TraceConfig) (jsonrpc.ParityTraces, error) {
	result, err := api.impl.Transaction(ctx, txHash, gasBailOut, traceConfig)
	return result, api.wrapHistoryErr(err)
}

func (api *TraceAPI) Get(ctx context.Context, txHash common.Hash, txIndicies []hexutil.Uint64, gasBailOut *bool, traceConfig *tracersConfig.TraceConfig) (*jsonrpc.ParityTrace, error) {
	result, err := api.impl.Get(ctx, txHash, txIndicies, gasBailOut, traceConfig)
	return result, api.wrapHistoryErr(err)
}

func (api *TraceAPI) Block(ctx context.Context, blockNr rpc.BlockNumber, gasBailOut *bool, traceConfig *tracersConfig.TraceConfig) (jsonrpc.ParityTraces, error) {
	result, err := api.impl.Block(ctx, blockNr, gasBailOut, traceConfig)
	return result, api.wrapHistoryErr(err)
}

func (api *TraceAPI) Filter(ctx context.Context, req jsonrpc.TraceFilterRequest, gasBailOut *bool, traceConfig *tracersConfig.TraceConfig) (json.RawMessage, error) {
	result, err := traceToRaw(func(stream jsonstream.Stream) error {
		return api.impl.Filter(ctx, req, gasBailOut, traceConfig, stream)
	})
	return result, api.wrapHistoryErr(err)
}

func traceToRaw(write func(stream jsonstream.Stream) error) (json.RawMessage, error) {
	var buf bytes.Buffer
	stream := jsonstream.New(&buf)
	if err := write(stream); err != nil {
		return nil, err
	}
	if err := stream.Flush(); err != nil {
		return nil, err
	}
	return json.RawMessage(buf.Bytes()), nil
}
