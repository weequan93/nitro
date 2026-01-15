//go:build erigon
// +build erigon

package erigonexec

import (
	"context"
	"errors"

	ecommon "github.com/erigontech/erigon-lib/common"
	"github.com/erigontech/erigon-lib/common/hexutil"
	elog "github.com/erigontech/erigon-lib/log/v3"
	"github.com/erigontech/erigon/eth/ethconfig"
	"github.com/erigontech/erigon/rpc/jsonrpc"
	"github.com/ethereum/go-ethereum/arbitrum_types"
	gtypes "github.com/ethereum/go-ethereum/core/types"

	"github.com/offchainlabs/nitro/execution"
	"github.com/offchainlabs/nitro/execution/gethexec"
)

const (
	rpcReturnDataLimitDefault          = 100_000
	rpcMaxGetProofRewindBlockCount     = 100_000
	rpcSubscribeLogsChannelSizeDefault = 128
)

type EthAPI struct {
	*jsonrpc.APIImpl
	publisher gethexec.TransactionPublisher
}

func NewEthAPI(exec execution.ExecutionClient, publisher gethexec.TransactionPublisher) (*EthAPI, error) {
	deps, backend, err := newRPCDeps(context.Background(), exec)
	if err != nil {
		return nil, err
	}
	logger := elog.New("component", "erigonexec")
	if backend != nil && backend.client != nil && backend.client.logger != nil {
		logger = backend.client.logger
	}
	impl := jsonrpc.NewEthAPI(
		deps.base,
		deps.db,
		backend,
		nil,
		nil,
		ethconfig.Defaults.RPCGasCap,
		ethconfig.Defaults.RPCTxFeeCap,
		rpcReturnDataLimitDefault,
		false,
		rpcMaxGetProofRewindBlockCount,
		rpcSubscribeLogsChannelSizeDefault,
		logger,
	)
	return &EthAPI{
		APIImpl:   impl,
		publisher: publisher,
	}, nil
}

func NewNetAPI(exec execution.ExecutionClient) (*jsonrpc.NetAPIImpl, error) {
	_, backend, err := newRPCDeps(context.Background(), exec)
	if err != nil {
		return nil, err
	}
	return jsonrpc.NewNetAPIImpl(backend), nil
}

func NewWeb3API(exec execution.ExecutionClient) (*jsonrpc.Web3APIImpl, error) {
	_, backend, err := newRPCDeps(context.Background(), exec)
	if err != nil {
		return nil, err
	}
	return jsonrpc.NewWeb3APIImpl(backend), nil
}

func (api *EthAPI) SendRawTransaction(ctx context.Context, encodedTx hexutil.Bytes) (ecommon.Hash, error) {
	if api.publisher == nil {
		return ecommon.Hash{}, errors.New("transaction forwarding not configured")
	}
	var tx gtypes.Transaction
	if err := tx.UnmarshalBinary(encodedTx); err != nil {
		return ecommon.Hash{}, err
	}
	if err := api.checkRPCTransaction(ctx, &tx); err != nil {
		return ecommon.Hash{}, err
	}
	if err := api.publisher.PublishTransaction(ctx, &tx, nil); err != nil {
		return ecommon.Hash{}, err
	}
	return toErigonHash(tx.Hash()), nil
}

func (api *EthAPI) SendRawTransactionConditional(ctx context.Context, encodedTx hexutil.Bytes, options *arbitrum_types.ConditionalOptions) (ecommon.Hash, error) {
	if api.publisher == nil {
		return ecommon.Hash{}, errors.New("transaction forwarding not configured")
	}
	var tx gtypes.Transaction
	if err := tx.UnmarshalBinary(encodedTx); err != nil {
		return ecommon.Hash{}, err
	}
	if err := api.checkRPCTransaction(ctx, &tx); err != nil {
		return ecommon.Hash{}, err
	}
	if err := api.publisher.PublishTransaction(ctx, &tx, options); err != nil {
		return ecommon.Hash{}, err
	}
	return toErigonHash(tx.Hash()), nil
}

func (api *EthAPI) SendTransaction(ctx context.Context, txObject interface{}) (ecommon.Hash, error) {
	_ = ctx
	_ = txObject
	return ecommon.Hash{}, errors.New("eth_sendTransaction is not supported; use eth_sendRawTransaction")
}

func (api *EthAPI) checkRPCTransaction(ctx context.Context, tx *gtypes.Transaction) error {
	if api.APIImpl != nil {
		if tx.Type() == gtypes.DynamicFeeTxType || tx.Type() == gtypes.BlobTxType {
			baseFee, err := api.APIImpl.BaseFee(ctx)
			if err != nil {
				return err
			}
			if baseFee != nil && tx.GasFeeCap().Cmp(baseFee.ToInt()) < 0 {
				return errors.New("fee cap is lower than the base fee")
			}
		} else if err := jsonrpc.CheckTxFee(tx.GasPrice(), tx.Gas(), api.APIImpl.FeeCap); err != nil {
			return err
		}
		if !api.APIImpl.AllowUnprotectedTxs && !tx.Protected() {
			return errors.New("only replay-protected (EIP-155) transactions allowed over RPC")
		}
	}
	return nil
}
