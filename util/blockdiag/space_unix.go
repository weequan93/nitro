// Copyright 2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

//go:build unix

package blockdiag

import (
	"fmt"
	"syscall"
)

func spaceAvailable(path string) error {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return err
	}
	if uint64(stat.Bavail)*uint64(stat.Bsize) < 2<<30 {
		return fmt.Errorf("diagnostic capture requires 2 GiB free")
	}
	return nil
}
