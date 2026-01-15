//go:build !erigon
// +build !erigon

package erigonexec

import (
	"github.com/offchainlabs/nitro/execution"
	"github.com/offchainlabs/nitro/execution/gethexec"
)

type DebugAPI struct{}
type TraceAPI struct{}
type EthAPI struct{}
type NetAPI struct{}
type Web3API struct{}

func NewDebugAPI(exec execution.ExecutionClient) (*DebugAPI, error) {
	_ = exec
	return nil, errNotImplemented
}

func NewTraceAPI(exec execution.ExecutionClient) (*TraceAPI, error) {
	_ = exec
	return nil, errNotImplemented
}

func NewEthAPI(exec execution.ExecutionClient, _ gethexec.TransactionPublisher) (*EthAPI, error) {
	_ = exec
	return nil, errNotImplemented
}

func NewNetAPI(exec execution.ExecutionClient) (*NetAPI, error) {
	_ = exec
	return nil, errNotImplemented
}

func NewWeb3API(exec execution.ExecutionClient) (*Web3API, error) {
	_ = exec
	return nil, errNotImplemented
}
