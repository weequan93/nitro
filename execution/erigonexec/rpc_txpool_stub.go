//go:build !erigon
// +build !erigon

package erigonexec

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// TxPoolAPI provides explicit txpool RPC stubs for non-erigon builds.
type TxPoolAPI struct{}

func NewTxPoolAPI() *TxPoolAPI {
	return &TxPoolAPI{}
}

func (api *TxPoolAPI) Status() (map[string]hexutil.Uint, error) {
	return nil, errNotImplemented
}

func (api *TxPoolAPI) Content() (map[string]map[string]map[string]interface{}, error) {
	return nil, errNotImplemented
}

func (api *TxPoolAPI) ContentFrom(addr common.Address) (map[string]map[string]interface{}, error) {
	_ = addr
	return nil, errNotImplemented
}

func (api *TxPoolAPI) Inspect() (map[string]map[string]map[string]string, error) {
	return nil, errNotImplemented
}
