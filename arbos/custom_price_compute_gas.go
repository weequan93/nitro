// Copyright 2021-2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package arbos

import (
	"math/big"

	"github.com/ethereum/go-ethereum/params"
)

const (
	deriwChainID = uint64(2885)

	// deriwDynamicCustomPriceHistoryStart is the first canonical Deriw block
	// whose transaction set depends on the dynamic custom-price accounting
	// introduced before its intended ArbOS activation.
	deriwDynamicCustomPriceHistoryStart = uint64(131292081)

	// deriwLegacyCustomPriceRestoreBlock ends the historical compatibility
	// window. Starting with this block, Deriw returns to legacy custom-price
	// accounting while ArbOS is below version 60.
	deriwLegacyCustomPriceRestoreBlock = uint64(132676214)
)

// useDynamicCustomPriceComputeGas selects the custom-price accounting rules
// without consulting dynamic ArbOS state unless those rules are active.
//
// Deriw canonicalized blocks using the dynamic rules before ArbOS 60. Preserve
// that bounded historical interval so those blocks remain replayable, then
// restore the legacy rules at a deterministic block. ArbOS 60 permanently
// activates the dynamic rules on every chain.
func useDynamicCustomPriceComputeGas(chainID *big.Int, blockNumber, arbosVersion uint64) bool {
	if arbosVersion >= params.ArbosVersion_60 {
		return true
	}
	return chainID != nil && chainID.IsUint64() && chainID.Uint64() == deriwChainID &&
		blockNumber >= deriwDynamicCustomPriceHistoryStart &&
		blockNumber < deriwLegacyCustomPriceRestoreBlock
}
