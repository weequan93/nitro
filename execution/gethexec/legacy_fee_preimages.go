// Copyright 2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package gethexec

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/offchainlabs/nitro/arbos/arbosState"
)

// recordLegacyFeeAccountPreimages includes initial-state recipients that legacy
// execution may load even for a zero fee. GetBalance records their trie paths
// without the account creation/touch side effects of AddBalance(0).
// This does not cover recipient changes within a block or pre-ArbOS-2 coinbase
// poster destinations. Compatibility must still be checked against the old WASM.
func recordLegacyFeeAccountPreimages(recordingdb *state.StateDB, initialArbosState *arbosState.ArbosState) error {
	infra, err := initialArbosState.InfraFeeAccount()
	if err != nil {
		return fmt.Errorf("recording legacy infra fee account: %w", err)
	}
	network, err := initialArbosState.NetworkFeeAccount()
	if err != nil {
		return fmt.Errorf("recording legacy network fee account: %w", err)
	}
	for _, account := range []common.Address{infra, network, types.L1PricerFundsPoolAddress} {
		recordingdb.GetBalance(account)
	}
	if err := recordingdb.Error(); err != nil {
		return fmt.Errorf("recording legacy fee account preimages: %w", err)
	}
	return nil
}
