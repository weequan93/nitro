// Copyright 2021-2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package arbos

import "github.com/ethereum/go-ethereum/params"

// useDiscountedCustomPriceComputeGas preserves the historical block-building
// behavior before ArbOS 60. Expanding the discount to dynamically configured
// custom-price transactions changes which transactions fit in a block, so it
// must only take effect at a deterministic ArbOS activation boundary.
func useDiscountedCustomPriceComputeGas(arbosVersion uint64, legacyMatch, upgradedMatch bool) bool {
	if arbosVersion < params.ArbosVersion_60 {
		return legacyMatch
	}
	return upgradedMatch
}
