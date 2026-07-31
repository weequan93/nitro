// Copyright 2021-2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package arbos

import (
	"testing"

	"github.com/ethereum/go-ethereum/params"
	"github.com/stretchr/testify/require"
)

func TestCustomPriceComputeGasActivationBoundary(t *testing.T) {
	tests := []struct {
		name          string
		arbosVersion  uint64
		legacyMatch   bool
		upgradedMatch bool
		expected      bool
	}{
		{
			name:          "dynamic whitelist does not change pre-v60 blocks",
			arbosVersion:  params.ArbosVersion_60 - 1,
			legacyMatch:   false,
			upgradedMatch: true,
			expected:      false,
		},
		{
			name:          "legacy hard-coded address remains discounted pre-v60",
			arbosVersion:  params.ArbosVersion_60 - 1,
			legacyMatch:   true,
			upgradedMatch: true,
			expected:      true,
		},
		{
			name:          "dynamic whitelist activates at v60",
			arbosVersion:  params.ArbosVersion_60,
			legacyMatch:   false,
			upgradedMatch: true,
			expected:      true,
		},
		{
			name:          "ordinary transaction remains undiscounted",
			arbosVersion:  params.ArbosVersion_60,
			legacyMatch:   false,
			upgradedMatch: false,
			expected:      false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual := useDiscountedCustomPriceComputeGas(
				test.arbosVersion,
				test.legacyMatch,
				test.upgradedMatch,
			)
			require.Equal(t, test.expected, actual)
		})
	}
}
