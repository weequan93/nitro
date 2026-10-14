// Copyright 2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package arbosState

import (
	"encoding/json"

	"github.com/ethereum/go-ethereum/arbitrum/multigas"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"

	"github.com/offchainlabs/nitro/arbos/burn"
	"github.com/offchainlabs/nitro/arbos/util"
)

// deriwLegacyProductionState distinguishes production's pre-feature history
// from the dev/test histories that already deployed the DeriwOS APIs. These
// fixed-size identity reads must not change historical precompile gas costs.
func (state *ArbosState) deriwLegacyProductionState() bool {
	return state.deriwOSVersion == DeriwOSVersion_Legacy &&
		state.backingStorage.GetFree(util.UintToHash(uint64(chainIdOffset))) == util.UintToHash(DeriwProdChainID)
}

func (state *ArbosState) pendingDeriwOSUpgradeVersionFree() uint64 {
	return state.backingStorage.GetFree(util.UintToHash(uint64(deriwOSUpgradeVersionOffset))).Big().Uint64()
}

// DeriwBootstrapAPIsActive gates production's first custom precompile methods
// at one deterministic child-block timestamp. Governance records the optional
// deriwOSBootstrapTime field in the complete serialized chain configuration,
// using the existing ArbOwner.setChainConfig(string) method. The existing setter
// stores these bytes unchanged, including fields unknown to the old binary.
//
// Without the field, production at DeriwOS 0 retains unknown-selector behavior.
// All nodes and proving machines must be upgraded before the selected time.
// Already-active versions and existing dev/test histories keep their behavior.
// The burner is charged only for production's version-0 configuration probe;
// callers must include those charges in the successful precompile result.
func DeriwBootstrapAPIsActive(stateDB vm.StateDB, timestamp uint64, burner burn.Burner) (bool, error) {
	state, err := OpenSystemArbosState(stateDB, nil, true)
	if err != nil {
		return false, nil
	}
	if !state.deriwLegacyProductionState() {
		return true, nil
	}
	if state.arbosVersion < params.ArbosVersion_60 {
		return false, nil
	}
	// The configuration has variable length. Meter its lookup against the
	// actual call budget; a zero-gas call must not scan or parse it for free.
	state, err = OpenArbosState(stateDB, burner)
	if err != nil {
		return false, err
	}
	serialized, err := state.ChainConfig()
	if err != nil {
		return false, err
	}
	// JSON decoding is linear in the byte count. Charge that computation
	// before parsing, in addition to the storage reads above.
	if err := burner.Burn(multigas.ResourceKindComputation, uint64(len(serialized))*params.CopyGas); err != nil {
		return false, err
	}
	var config struct {
		BootstrapTime uint64 `json:"deriwOSBootstrapTime"`
	}
	if err := json.Unmarshal(serialized, &config); err != nil {
		return false, nil
	}
	return config.BootstrapTime != 0 && timestamp >= config.BootstrapTime, nil
}
