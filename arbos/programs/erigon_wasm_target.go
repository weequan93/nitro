//go:build erigon
// +build erigon

package programs

import "github.com/erigontech/erigon/arb/ethdb"

func init() {
	ethdb.SetWasmTarget = SetTarget
}
