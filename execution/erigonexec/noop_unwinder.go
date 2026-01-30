//go:build erigon
// +build erigon

package erigonexec

import (
	"github.com/erigontech/erigon/db/kv"
	"github.com/erigontech/erigon/execution/stagedsync"
)

// noopUnwinder forces ExecV3 to flush state at the end without triggering unwinds.
type noopUnwinder struct{}

func (noopUnwinder) UnwindTo(uint64, stagedsync.UnwindReason, kv.Tx) error { return nil }
func (noopUnwinder) HasUnwindPoint() bool                                 { return false }
func (noopUnwinder) LogPrefix() string                                     { return "erigonexec" }
