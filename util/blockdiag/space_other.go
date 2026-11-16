// Copyright 2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

//go:build !unix

package blockdiag

import "fmt"

func spaceAvailable(string) error {
	return fmt.Errorf("runtime artifact capture is unsupported on this platform")
}
