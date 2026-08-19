// Copyright 2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package arbosState

import (
	"fmt"

	"github.com/ethereum/go-ethereum/core/vm"

	"github.com/offchainlabs/nitro/arbos/blacklist"
	"github.com/offchainlabs/nitro/arbos/burn"
	"github.com/offchainlabs/nitro/arbos/deriwpolicy"
	"github.com/offchainlabs/nitro/arbos/l1pricing"
	"github.com/offchainlabs/nitro/arbos/storage"
)

const (
	// DeriwOSVersion_Legacy preserves the historical Deriw execution behavior.
	DeriwOSVersion_Legacy uint64 = 0
	// DeriwOSVersion_ConsensusBlacklist enables consensus checks for top-level
	// transaction sender, destination, and subaccount parent addresses.
	DeriwOSVersion_ConsensusBlacklist uint64 = 1
	// DeriwOSVersion_RouterOnlySends restricts ArbSys L3-to-parent sends to the
	// approved chain-specific Deriw router and canonical ERC-20 routes.
	DeriwOSVersion_RouterOnlySends uint64 = 2
	// DeriwOSVersion_DirectETHWithdrawals restores direct ArbSys.withdrawEth
	// while preserving router-only enforcement for raw sendTxToL1 calls and
	// canonical ERC-20 gateway sends.
	DeriwOSVersion_DirectETHWithdrawals uint64 = 3
	// DeriwOSVersion_ChainOwnerUpgradeScheduling moves DeriwOS upgrade
	// scheduling to the chain-owner-only ArbOwner precompile. The legacy
	// blacklist endpoint cannot schedule this or any later version.
	DeriwOSVersion_ChainOwnerUpgradeScheduling uint64 = 4
	// DeriwOSVersion_SubAccountAuthorizationHardening activates strict
	// sub-account signatures, replay protection, timestamp validation, and
	// one-to-one parent/child relationship updates at one consensus boundary.
	DeriwOSVersion_SubAccountAuthorizationHardening uint64 = 5

	MaxDeriwOSVersionSupported = DeriwOSVersion_SubAccountAuthorizationHardening

	DeriwDevChainID  = deriwpolicy.DevChainID
	DeriwTestChainID = deriwpolicy.TestChainID
	DeriwProdChainID = deriwpolicy.ProdChainID
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
	if err := state.validateDeriwOSUpgrade(upgradeTo); err != nil {
		return err
	}
	for state.deriwOSVersion < upgradeTo {
		nextVersion := state.deriwOSVersion + 1
		switch nextVersion {
		case DeriwOSVersion_ConsensusBlacklist:
			// Activation requirements were checked before changing any version state.
		case DeriwOSVersion_RouterOnlySends:
			// Persist a chain-specific compiled default exactly once. If governance
			// already activated an on-chain configuration, it is preserved.
			if err := state.initializeDeriwRouterConfigForActivation(); err != nil {
				return err
			}
		case DeriwOSVersion_DirectETHWithdrawals:
			// No state migration is required. ArbSys uses this version to exempt
			// only its withdrawEth ABI entry point from route enforcement.
		case DeriwOSVersion_ChainOwnerUpgradeScheduling:
			// No state migration is required. Authorization is enforced by the
			// ArbOwner precompile wrapper and the legacy scheduler's version cap.
		case DeriwOSVersion_SubAccountAuthorizationHardening:
			// No state migration is required. The sub-account precompiles use this
			// version as the deterministic legacy-to-hardened execution boundary.
		default:
			return fmt.Errorf("missing DeriwOS upgrade implementation for version %v", nextVersion)
		}
		state.deriwOSVersion = nextVersion
	}
	return state.backingStorage.SetUint64ByUint64(uint64(deriwOSVersionOffset), state.deriwOSVersion)
}

func (state *ArbosState) validateDeriwOSUpgrade(upgradeTo uint64) error {
	if state.deriwOSVersion < DeriwOSVersion_ConsensusBlacklist && upgradeTo >= DeriwOSVersion_ConsensusBlacklist {
		if err := state.validateConsensusBlacklistActivation(); err != nil {
			return err
		}
	}
	if state.deriwOSVersion < DeriwOSVersion_RouterOnlySends && upgradeTo >= DeriwOSVersion_RouterOnlySends {
		if err := state.canInitializeDeriwRouterConfig(); err != nil {
			return fmt.Errorf("DeriwOS router-only sends have no valid active route: %w", err)
		}
	}
	return nil
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
	if err := state.validateDeriwOSUpgrade(newVersion); err != nil {
		return fmt.Errorf("cannot schedule DeriwOS upgrade %v: %w", newVersion, err)
	}
	if err := state.deriwOSUpgradeVersion.Set(newVersion); err != nil {
		return err
	}
	if err := state.deriwOSUpgradeTimestamp.Set(timestamp); err != nil {
		return err
	}
	return state.deriwOSUpgradeArbOSVersion.Set(state.arbosVersion)
}

// CancelScheduledDeriwOSUpgrade clears a pending DeriwOS upgrade. This state
// operation must only be exposed through a chain-owner-authorized precompile.
func (state *ArbosState) CancelScheduledDeriwOSUpgrade() error {
	if err := state.deriwOSUpgradeVersion.Set(0); err != nil {
		return err
	}
	if err := state.deriwOSUpgradeTimestamp.Set(0); err != nil {
		return err
	}
	return state.deriwOSUpgradeArbOSVersion.Set(0)
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
