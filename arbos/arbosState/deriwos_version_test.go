// Copyright 2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package arbosState

import (
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/offchainlabs/nitro/arbos/burn"
	"github.com/offchainlabs/nitro/cmd/chaininfo"
)

func TestDeriwOSVersionDefaultsToLegacy(t *testing.T) {
	state, stateDB := NewArbosMemoryBackedArbOSState()
	if state.DeriwOSVersion() != DeriwOSVersion_Legacy {
		t.Fatalf("new state DeriwOS version = %v, want %v", state.DeriwOSVersion(), DeriwOSVersion_Legacy)
	}
	if got := DeriwOSVersion(stateDB); got != DeriwOSVersion_Legacy {
		t.Fatalf("stored DeriwOS version = %v, want %v", got, DeriwOSVersion_Legacy)
	}
}

func TestScheduleAndActivateDeriwOSUpgradeRecordsArbOSVersion(t *testing.T) {
	state, stateDB := NewArbosMemoryBackedArbOSState()
	activationTime := uint64(12345)
	if err := state.ScheduleDeriwOSUpgrade(DeriwOSVersion_ConsensusBlacklist, activationTime); err != nil {
		t.Fatal(err)
	}

	version, timestamp, arbosVersion, err := state.GetScheduledDeriwOSUpgrade()
	if err != nil {
		t.Fatal(err)
	}
	if version != DeriwOSVersion_ConsensusBlacklist || timestamp != activationTime || arbosVersion != state.ArbOSVersion() {
		t.Fatalf("scheduled upgrade = (%v, %v, ArbOS %v), want (%v, %v, ArbOS %v)", version, timestamp, arbosVersion, DeriwOSVersion_ConsensusBlacklist, activationTime, state.ArbOSVersion())
	}

	if err := state.UpgradeDeriwOSVersionIfNecessary(activationTime - 1); err != nil {
		t.Fatal(err)
	}
	if state.DeriwOSVersion() != DeriwOSVersion_Legacy {
		t.Fatalf("DeriwOS upgraded before activation: %v", state.DeriwOSVersion())
	}
	if err := state.UpgradeDeriwOSVersionIfNecessary(activationTime); err != nil {
		t.Fatal(err)
	}
	if state.DeriwOSVersion() != DeriwOSVersion_ConsensusBlacklist {
		t.Fatalf("DeriwOS version = %v, want %v", state.DeriwOSVersion(), DeriwOSVersion_ConsensusBlacklist)
	}

	reopened, err := OpenArbosState(stateDB, burn.NewSystemBurner(nil, false))
	if err != nil {
		t.Fatal(err)
	}
	if reopened.DeriwOSVersion() != DeriwOSVersion_ConsensusBlacklist {
		t.Fatalf("reopened DeriwOS version = %v, want %v", reopened.DeriwOSVersion(), DeriwOSVersion_ConsensusBlacklist)
	}
}

func TestScheduleAndActivateRouterOnlySendsOnConfiguredChains(t *testing.T) {
	for _, chainID := range []uint64{DeriwDevChainID, DeriwProdChainID} {
		t.Run(new(big.Int).SetUint64(chainID).String(), func(t *testing.T) {
			chainConfig := chaininfo.ArbitrumDevTestChainConfig()
			chainConfig.ChainID = new(big.Int).SetUint64(chainID)
			state, stateDB := NewArbosMemoryBackedArbOSStateWithConfig(chainConfig)
			activationTime := uint64(12345)
			if err := state.ScheduleDeriwOSUpgrade(DeriwOSVersion_RouterOnlySends, activationTime); err != nil {
				t.Fatal(err)
			}
			if err := state.UpgradeDeriwOSVersionIfNecessary(activationTime); err != nil {
				t.Fatal(err)
			}
			if state.DeriwOSVersion() != DeriwOSVersion_RouterOnlySends || DeriwOSVersion(stateDB) != DeriwOSVersion_RouterOnlySends {
				t.Fatalf("DeriwOS version = memory %v / storage %v, want %v", state.DeriwOSVersion(), DeriwOSVersion(stateDB), DeriwOSVersion_RouterOnlySends)
			}
		})
	}
}

func TestScheduleAndActivateDirectETHWithdrawals(t *testing.T) {
	chainConfig := chaininfo.ArbitrumDevTestChainConfig()
	chainConfig.ChainID = new(big.Int).SetUint64(DeriwDevChainID)
	state, stateDB := NewArbosMemoryBackedArbOSStateWithConfig(chainConfig)
	activationTime := uint64(12345)

	if err := state.UpgradeDeriwOSVersion(DeriwOSVersion_RouterOnlySends); err != nil {
		t.Fatal(err)
	}
	if err := state.ScheduleDeriwOSUpgrade(DeriwOSVersion_DirectETHWithdrawals, activationTime); err != nil {
		t.Fatal(err)
	}
	if err := state.UpgradeDeriwOSVersionIfNecessary(activationTime); err != nil {
		t.Fatal(err)
	}
	if state.DeriwOSVersion() != DeriwOSVersion_DirectETHWithdrawals ||
		DeriwOSVersion(stateDB) != DeriwOSVersion_DirectETHWithdrawals {
		t.Fatalf(
			"DeriwOS version = memory %v / storage %v, want %v",
			state.DeriwOSVersion(),
			DeriwOSVersion(stateDB),
			DeriwOSVersion_DirectETHWithdrawals,
		)
	}

	_, revision, configured, err := state.ActiveDeriwRouterConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !configured || revision != 1 {
		t.Fatalf("router config after DeriwOS 3 activation = configured %v revision %v, want true/1", configured, revision)
	}
}

func TestScheduleCancelAndActivateChainOwnerUpgradeScheduling(t *testing.T) {
	chainConfig := chaininfo.ArbitrumDevTestChainConfig()
	chainConfig.ChainID = new(big.Int).SetUint64(DeriwDevChainID)
	state, stateDB := NewArbosMemoryBackedArbOSStateWithConfig(chainConfig)

	if err := state.UpgradeDeriwOSVersion(DeriwOSVersion_DirectETHWithdrawals); err != nil {
		t.Fatal(err)
	}
	if err := state.ScheduleDeriwOSUpgrade(DeriwOSVersion_ChainOwnerUpgradeScheduling, 12345); err != nil {
		t.Fatal(err)
	}
	if err := state.CancelScheduledDeriwOSUpgrade(); err != nil {
		t.Fatal(err)
	}
	version, timestamp, arbosVersion, err := state.GetScheduledDeriwOSUpgrade()
	if err != nil {
		t.Fatal(err)
	}
	if version != 0 || timestamp != 0 || arbosVersion != 0 {
		t.Fatalf("cancelled schedule = (%v, %v, %v), want all zero", version, timestamp, arbosVersion)
	}

	if err := state.ScheduleDeriwOSUpgrade(DeriwOSVersion_ChainOwnerUpgradeScheduling, 23456); err != nil {
		t.Fatal(err)
	}
	if err := state.UpgradeDeriwOSVersionIfNecessary(23456); err != nil {
		t.Fatal(err)
	}
	if state.DeriwOSVersion() != DeriwOSVersion_ChainOwnerUpgradeScheduling ||
		DeriwOSVersion(stateDB) != DeriwOSVersion_ChainOwnerUpgradeScheduling {
		t.Fatalf(
			"DeriwOS version = memory %v / storage %v, want %v",
			state.DeriwOSVersion(),
			DeriwOSVersion(stateDB),
			DeriwOSVersion_ChainOwnerUpgradeScheduling,
		)
	}
}

func TestScheduleAndActivateSubAccountAuthorizationHardening(t *testing.T) {
	chainConfig := chaininfo.ArbitrumDevTestChainConfig()
	chainConfig.ChainID = new(big.Int).SetUint64(DeriwDevChainID)
	state, stateDB := NewArbosMemoryBackedArbOSStateWithConfig(chainConfig)
	activationTime := uint64(34567)

	if err := state.UpgradeDeriwOSVersion(DeriwOSVersion_ChainOwnerUpgradeScheduling); err != nil {
		t.Fatal(err)
	}
	if err := state.ScheduleDeriwOSUpgrade(DeriwOSVersion_SubAccountAuthorizationHardening, activationTime); err != nil {
		t.Fatal(err)
	}
	if err := state.UpgradeDeriwOSVersionIfNecessary(activationTime - 1); err != nil {
		t.Fatal(err)
	}
	if state.DeriwOSVersion() != DeriwOSVersion_ChainOwnerUpgradeScheduling {
		t.Fatalf("DeriwOS upgraded before v5 activation: %v", state.DeriwOSVersion())
	}
	if err := state.UpgradeDeriwOSVersionIfNecessary(activationTime); err != nil {
		t.Fatal(err)
	}
	if state.DeriwOSVersion() != DeriwOSVersion_SubAccountAuthorizationHardening ||
		DeriwOSVersion(stateDB) != DeriwOSVersion_SubAccountAuthorizationHardening {
		t.Fatalf(
			"DeriwOS version = memory %v / storage %v, want %v",
			state.DeriwOSVersion(),
			DeriwOSVersion(stateDB),
			DeriwOSVersion_SubAccountAuthorizationHardening,
		)
	}
}

func TestRouterOnlySendsRejectsUnconfiguredChainsWithoutStateChange(t *testing.T) {
	for _, chainID := range []uint64{DeriwTestChainID, 1337} {
		t.Run(new(big.Int).SetUint64(chainID).String(), func(t *testing.T) {
			chainConfig := chaininfo.ArbitrumDevTestChainConfig()
			chainConfig.ChainID = new(big.Int).SetUint64(chainID)
			state, stateDB := NewArbosMemoryBackedArbOSStateWithConfig(chainConfig)
			if err := state.UpgradeDeriwOSVersion(DeriwOSVersion_RouterOnlySends); err == nil {
				t.Fatal("activated router-only sends without a route configuration")
			}
			if state.DeriwOSVersion() != DeriwOSVersion_Legacy || DeriwOSVersion(stateDB) != DeriwOSVersion_Legacy {
				t.Fatalf("failed upgrade changed DeriwOS version to memory %v / storage %v", state.DeriwOSVersion(), DeriwOSVersion(stateDB))
			}
			if err := state.ScheduleDeriwOSUpgrade(DeriwOSVersion_RouterOnlySends, 12345); err == nil {
				t.Fatal("scheduled router-only sends without a route configuration")
			}
			version, timestamp, arbosVersion, err := state.GetScheduledDeriwOSUpgrade()
			if err != nil {
				t.Fatal(err)
			}
			if version != 0 || timestamp != 0 || arbosVersion != 0 {
				t.Fatalf("failed schedule changed state to (%v, %v, %v)", version, timestamp, arbosVersion)
			}
		})
	}
}

func TestDeriwOSUpgradeRejectsUnsupportedVersionWithoutStateChange(t *testing.T) {
	state, stateDB := NewArbosMemoryBackedArbOSState()
	err := state.UpgradeDeriwOSVersion(MaxDeriwOSVersionSupported + 1)
	if !errors.Is(err, ErrFatalNodeOutOfDate) {
		t.Fatalf("error = %v, want ErrFatalNodeOutOfDate", err)
	}
	if state.DeriwOSVersion() != DeriwOSVersion_Legacy || DeriwOSVersion(stateDB) != DeriwOSVersion_Legacy {
		t.Fatal("unsupported upgrade changed DeriwOS state")
	}
}

func TestScheduleDeriwOSUpgradeRejectsUnsupportedVersionWithoutStateChange(t *testing.T) {
	state, _ := NewArbosMemoryBackedArbOSState()
	if err := state.ScheduleDeriwOSUpgrade(MaxDeriwOSVersionSupported+1, 12345); err == nil {
		t.Fatal("scheduled an unsupported DeriwOS version")
	}
	version, timestamp, arbosVersion, err := state.GetScheduledDeriwOSUpgrade()
	if err != nil {
		t.Fatal(err)
	}
	if version != 0 || timestamp != 0 || arbosVersion != 0 {
		t.Fatalf("failed schedule changed state to (%v, %v, %v)", version, timestamp, arbosVersion)
	}
}

func TestDeriwOSUpgradeRejectsQuarantinedSystemAddress(t *testing.T) {
	state, stateDB := NewArbosMemoryBackedArbOSState()
	if err := state.Blacklist().TxToAddrs().Add(types.DeriwBlacklistAddress); err != nil {
		t.Fatal(err)
	}
	if err := state.UpgradeDeriwOSVersion(DeriwOSVersion_ConsensusBlacklist); err == nil {
		t.Fatal("activated consensus blacklist with recovery precompile quarantined")
	}
	if state.DeriwOSVersion() != DeriwOSVersion_Legacy || DeriwOSVersion(stateDB) != DeriwOSVersion_Legacy {
		t.Fatal("failed activation changed DeriwOS version")
	}
}

func TestScheduleDeriwOSUpgradeRejectsQuarantinedSystemAddress(t *testing.T) {
	state, _ := NewArbosMemoryBackedArbOSState()
	if err := state.Blacklist().TxToAddrs().Add(types.DeriwBlacklistAddress); err != nil {
		t.Fatal(err)
	}
	if err := state.ScheduleDeriwOSUpgrade(DeriwOSVersion_ConsensusBlacklist, 12345); err == nil {
		t.Fatal("scheduled consensus blacklist with recovery precompile quarantined")
	}
	version, timestamp, arbosVersion, err := state.GetScheduledDeriwOSUpgrade()
	if err != nil {
		t.Fatal(err)
	}
	if version != 0 || timestamp != 0 || arbosVersion != 0 {
		t.Fatalf("failed schedule changed state to (%v, %v, %v)", version, timestamp, arbosVersion)
	}
}

func TestDeriwOSUpgradeRejectsQuarantinedDynamicFeeAccount(t *testing.T) {
	state, stateDB := NewArbosMemoryBackedArbOSState()
	feeAccount := common.HexToAddress("0xf001")
	if err := state.SetNetworkFeeAccount(feeAccount); err != nil {
		t.Fatal(err)
	}
	if err := state.Blacklist().TxFromAddrs().Add(feeAccount); err != nil {
		t.Fatal(err)
	}
	if err := state.UpgradeDeriwOSVersion(DeriwOSVersion_ConsensusBlacklist); err == nil {
		t.Fatal("activated consensus blacklist with network fee account quarantined")
	}
	if state.DeriwOSVersion() != DeriwOSVersion_Legacy || DeriwOSVersion(stateDB) != DeriwOSVersion_Legacy {
		t.Fatal("failed dynamic-account activation changed DeriwOS version")
	}
}
