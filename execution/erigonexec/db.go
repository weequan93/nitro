//go:build !erigon
// +build !erigon

package erigonexec

import "errors"

func OpenDatabases(chainDir string, opts MdbxOptions) (*Databases, error) {
	_ = chainDir
	_ = opts
	return nil, errors.New("erigon mdbx open not implemented")
}
