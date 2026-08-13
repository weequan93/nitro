// Copyright 2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package arbosState

import (
	"fmt"

	"github.com/ethereum/go-ethereum/core/vm"

	"github.com/offchainlabs/nitro/arbos/blacklist"
	"github.com/offchainlabs/nitro/arbos/burn"
	"github.com/offchainlabs/nitro/arbos/l1pricing"
	"github.com/offchainlabs/nitro/arbos/storage"
)

const (
	// DeriwOSVersion_Legacy preserves the historical Deriw execution behavior.
	DeriwOSVersion_Legacy uint64 = 0
	// DeriwOSVersion_ConsensusBlacklist enables consensus checks for top-level
	// transaction sender, destination, and subaccount parent addresses.
	DeriwOSVersion_ConsensusBlacklist uint64 = 1

	MaxDeriwOSVersionSupported = DeriwOSVersion_ConsensusBlacklist
)

// DeriwOSVersion reads the independent Deriw consensus version from state.
// Unupgraded chains return DeriwOSVersion_Legacy because empty storage is zero.
func DeriwOSVersion(stateDB vm.StateDB) uint64 {
	backingStorage := storage.NewGeth(stateDB, burn.NewSystemBurner(nil, false))
	version, err := backingStorage.GetUint64ByUint64(uint64(deriwOSVersionOffset))
	if err != nil {
		panic("failed to get the DeriwOS version: " + err.Error())
	}
	return version
}

func (state *ArbosState) DeriwOSVersion() uint64 {
	return state.deriwOSVersion
}

// DeriwConsensusBlacklistActiveOrScheduled reports whether the DeriwOS 1
// safety rules must be enforced. Once the upgrade is scheduled, protected
// addresses and fee accounts are locked down so activation cannot deadlock.
func (state *ArbosState) DeriwConsensusBlacklistActiveOrScheduled() (bool, error) {
	if state.deriwOSVersion >= DeriwOSVersion_ConsensusBlacklist {
		return true, nil
	}
	upgradeTo, err := state.deriwOSUpgradeVersion.Get()
	if err != nil {
		return false, err
	}
	return upgradeTo >= DeriwOSVersion_ConsensusBlacklist, nil
}

func (state *ArbosState) UpgradeDeriwOSVersionIfNecessary(currentTimestamp uint64) error {
	upgradeTo, err := state.deriwOSUpgradeVersion.Get()
	state.Restrict(err)
	flagday, err := state.deriwOSUpgradeTimestamp.Get()
	state.Restrict(err)
	if state.deriwOSVersion < upgradeTo && currentTimestamp >= flagday {
		return state.UpgradeDeriwOSVersion(upgradeTo)
	}
	return nil
}

func (state *ArbosState) UpgradeDeriwOSVersion(upgradeTo uint64) error {
	if upgradeTo > MaxDeriwOSVersionSupported {
		return fmt.Errorf(
			"the chain is upgrading to unsupported DeriwOS version %v, %w",
			upgradeTo,
			ErrFatalNodeOutOfDate,
		)
	}
	for state.deriwOSVersion < upgradeTo {
		nextVersion := state.deriwOSVersion + 1
		switch nextVersion {
		case DeriwOSVersion_ConsensusBlacklist:
			if err := state.validateConsensusBlacklistActivation(); err != nil {
				return err
			}
		default:
			return fmt.Errorf("missing DeriwOS upgrade implementation for version %v", nextVersion)
		}
		state.deriwOSVersion = nextVersion
	}
	return state.backingStorage.SetUint64ByUint64(uint64(deriwOSVersionOffset), state.deriwOSVersion)
}

func (state *ArbosState) validateConsensusBlacklistActivation() error {
	networkFeeAccount, err := state.NetworkFeeAccount()
	if err != nil {
		return err
	}
	infraFeeAccount, err := state.InfraFeeAccount()
	if err != nil {
		return err
	}
	protected := blacklist.ProtectedSystemAddresses(networkFeeAccount, infraFeeAccount, l1pricing.BatchPosterAddress)
	for _, address := range protected {
		if state.Blacklist().IsQuarantinedFree(address) {
			return fmt.Errorf("cannot activate DeriwOS consensus blacklist while protected system address %v is quarantined", address)
		}
	}
	return nil
}

func (state *ArbosState) ScheduleDeriwOSUpgrade(newVersion uint64, timestamp uint64) error {
	if newVersion <= state.deriwOSVersion {
		return fmt.Errorf("DeriwOS upgrade version %v must be newer than current version %v", newVersion, state.deriwOSVersion)
	}
	if newVersion > MaxDeriwOSVersionSupported {
		return fmt.Errorf("cannot schedule unsupported DeriwOS version %v; this node supports up to version %v", newVersion, MaxDeriwOSVersionSupported)
	}
	if state.deriwOSVersion < DeriwOSVersion_ConsensusBlacklist && newVersion >= DeriwOSVersion_ConsensusBlacklist {
		if err := state.validateConsensusBlacklistActivation(); err != nil {
			return fmt.Errorf("cannot schedule DeriwOS consensus blacklist: %w", err)
		}
	}
	if err := state.deriwOSUpgradeVersion.Set(newVersion); err != nil {
		return err
	}
	if err := state.deriwOSUpgradeTimestamp.Set(timestamp); err != nil {
		return err
	}
	return state.deriwOSUpgradeArbOSVersion.Set(state.arbosVersion)
}

// GetScheduledDeriwOSUpgrade returns the target DeriwOS version, activation
// timestamp, and the ArbOS version recorded when the upgrade was scheduled.
func (state *ArbosState) GetScheduledDeriwOSUpgrade() (uint64, uint64, uint64, error) {
	version, err := state.deriwOSUpgradeVersion.Get()
	if err != nil {
		return 0, 0, 0, err
	}
	timestamp, err := state.deriwOSUpgradeTimestamp.Get()
	if err != nil {
		return 0, 0, 0, err
	}
	arbosVersion, err := state.deriwOSUpgradeArbOSVersion.Get()
	if err != nil {
		return 0, 0, 0, err
	}
	return version, timestamp, arbosVersion, nil
}
