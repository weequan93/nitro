//go:build !erigon
// +build !erigon

package erigonexec

import (
	"errors"

	"github.com/ethereum/go-ethereum/ethdb"
)

func OpenArbDB(chainDir string, opts MdbxOptions) (ethdb.Database, error) {
	_ = chainDir
	_ = opts
	return nil, errors.New("erigon build tag required for MDBX")
}

func OpenWasmDB(chainDir string, opts MdbxOptions) (ethdb.Database, error) {
	_ = chainDir
	_ = opts
	return nil, errors.New("erigon build tag required for MDBX")
}
