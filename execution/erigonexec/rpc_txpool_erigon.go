//go:build erigon
// +build erigon

package erigonexec

import (
	"errors"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

var errTxPoolUnsupported = errors.New("erigonexec: txpool API not supported")

// TxPoolAPI provides explicit txpool RPC stubs for the Erigon backend.
// These return a consistent "not supported" error instead of "method not found".
type TxPoolAPI struct{}

func NewTxPoolAPI() *TxPoolAPI {
	return &TxPoolAPI{}
}

func (api *TxPoolAPI) Status() (map[string]hexutil.Uint, error) {
	return nil, errTxPoolUnsupported
}

func (api *TxPoolAPI) Content() (map[string]map[string]map[string]interface{}, error) {
	return nil, errTxPoolUnsupported
}

func (api *TxPoolAPI) ContentFrom(addr common.Address) (map[string]map[string]interface{}, error) {
	_ = addr
	return nil, errTxPoolUnsupported
}

func (api *TxPoolAPI) Inspect() (map[string]map[string]map[string]string, error) {
	return nil, errTxPoolUnsupported
}
