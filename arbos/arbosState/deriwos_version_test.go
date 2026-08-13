// Copyright 2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package arbosState

import (
	"errors"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/offchainlabs/nitro/arbos/burn"
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
