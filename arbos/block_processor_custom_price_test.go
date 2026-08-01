// Copyright 2021-2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package arbos

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/params"
	"github.com/stretchr/testify/require"
)

func TestCustomPriceComputeGasActivationBoundary(t *testing.T) {
	tests := []struct {
		name         string
		chainID      *big.Int
		blockNumber  uint64
		arbosVersion uint64
		expected     bool
	}{
		{
			name:         "Deriw uses legacy rules before canonical compatibility window",
			chainID:      new(big.Int).SetUint64(deriwChainID),
			blockNumber:  deriwDynamicCustomPriceHistoryStart - 1,
			arbosVersion: params.ArbosVersion_60 - 1,
			expected:     false,
		},
		{
			name:         "Deriw activates dynamic rules at canonical compatibility boundary",
			chainID:      new(big.Int).SetUint64(deriwChainID),
			blockNumber:  deriwDynamicCustomPriceHistoryStart,
			arbosVersion: params.ArbosVersion_60 - 1,
			expected:     true,
		},
		{
			name:         "Deriw preserves dynamic rules through compatibility window",
			chainID:      new(big.Int).SetUint64(deriwChainID),
			blockNumber:  deriwLegacyCustomPriceRestoreBlock - 1,
			arbosVersion: params.ArbosVersion_60 - 1,
			expected:     true,
		},
		{
			name:         "Deriw restores legacy rules at release boundary",
			chainID:      new(big.Int).SetUint64(deriwChainID),
			blockNumber:  deriwLegacyCustomPriceRestoreBlock,
			arbosVersion: params.ArbosVersion_60 - 1,
			expected:     false,
		},
		{
			name:         "other chains retain legacy rules before v60",
			chainID:      new(big.Int).SetUint64(deriwChainID + 1),
			blockNumber:  deriwDynamicCustomPriceHistoryStart,
			arbosVersion: params.ArbosVersion_60 - 1,
			expected:     false,
		},
		{
			name:         "missing chain ID retains legacy rules before v60",
			chainID:      nil,
			blockNumber:  deriwDynamicCustomPriceHistoryStart,
			arbosVersion: params.ArbosVersion_60 - 1,
			expected:     false,
		},
		{
			name:         "oversized chain ID with Deriw low bits retains legacy rules",
			chainID:      new(big.Int).Add(new(big.Int).Lsh(big.NewInt(1), 64), new(big.Int).SetUint64(deriwChainID)),
			blockNumber:  deriwDynamicCustomPriceHistoryStart,
			arbosVersion: params.ArbosVersion_60 - 1,
			expected:     false,
		},
		{
			name:         "ArbOS v60 activates dynamic rules before Deriw window",
			chainID:      new(big.Int).SetUint64(deriwChainID),
			blockNumber:  deriwDynamicCustomPriceHistoryStart - 1,
			arbosVersion: params.ArbosVersion_60,
			expected:     true,
		},
		{
			name:         "ArbOS v60 keeps dynamic rules after Deriw release boundary",
			chainID:      new(big.Int).SetUint64(deriwChainID),
			blockNumber:  deriwLegacyCustomPriceRestoreBlock,
			arbosVersion: params.ArbosVersion_60,
			expected:     true,
		},
		{
			name:         "ArbOS v60 activates dynamic rules on other chains",
			chainID:      new(big.Int).SetUint64(deriwChainID + 1),
			blockNumber:  0,
			arbosVersion: params.ArbosVersion_60,
			expected:     true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual := useDynamicCustomPriceComputeGas(
				test.chainID,
				test.blockNumber,
				test.arbosVersion,
			)
			require.Equal(t, test.expected, actual)
		})
	}
}
