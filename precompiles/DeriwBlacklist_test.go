// Copyright 2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package precompiles

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/offchainlabs/nitro/arbos/arbosState"
)

func TestDeriwBlacklistProtectedAddressCompatibility(t *testing.T) {
	t.Run("legacy permits historical behavior", func(t *testing.T) {
		evm := newMockEVMForTesting()
		ctx := testContext(common.HexToAddress("0x1001"), evm)
		precompile := &DeriwBlacklist{}
		if err := precompile.AddBlacklistTxFrom(ctx, evm, types.DeriwBlacklistAddress); err != nil {
			t.Fatalf("DeriwOS 0 changed historical add behavior: %v", err)
		}
	})

	t.Run("consensus version rejects protected address", func(t *testing.T) {
		evm := newMockEVMForTesting()
		ctx := testContext(common.HexToAddress("0x1001"), evm)
		if err := ctx.State.UpgradeDeriwOSVersion(arbosState.DeriwOSVersion_ConsensusBlacklist); err != nil {
			t.Fatal(err)
		}
		precompile := &DeriwBlacklist{}
		if err := precompile.AddBlacklistTxTo(ctx, evm, types.DeriwBlacklistAddress); err == nil {
			t.Fatal("DeriwOS 1 allowed recovery precompile to be quarantined")
		}
	})

	t.Run("scheduled consensus version rejects protected address", func(t *testing.T) {
		evm := newMockEVMForTesting()
		ctx := testContext(common.HexToAddress("0x1001"), evm)
		if err := ctx.State.ScheduleDeriwOSUpgrade(arbosState.DeriwOSVersion_ConsensusBlacklist, 1234); err != nil {
			t.Fatal(err)
		}
		precompile := &DeriwBlacklist{}
		if err := precompile.AddBlacklistTxFrom(ctx, evm, types.DeriwBlacklistAddress); err == nil {
			t.Fatal("pending DeriwOS 1 allowed recovery precompile to be quarantined")
		}
	})

	t.Run("scheduled consensus version rejects quarantined fee account", func(t *testing.T) {
		evm := newMockEVMForTesting()
		ctx := testContext(common.HexToAddress("0x1001"), evm)
		feeAccount := common.HexToAddress("0xf001")
		if err := ctx.State.Blacklist().TxFromAddrs().Add(feeAccount); err != nil {
			t.Fatal(err)
		}
		if err := ctx.State.ScheduleDeriwOSUpgrade(arbosState.DeriwOSVersion_ConsensusBlacklist, 1234); err != nil {
			t.Fatal(err)
		}
		owner := &ArbOwner{}
		if err := owner.SetNetworkFeeAccount(ctx, evm, feeAccount); err == nil {
			t.Fatal("pending DeriwOS 1 allowed a quarantined network fee account")
		}
		if err := owner.SetInfraFeeAccount(ctx, evm, feeAccount); err == nil {
			t.Fatal("pending DeriwOS 1 allowed a quarantined infrastructure fee account")
		}
	})
}

func TestDeriwBlacklistVersionQueriesReturnArbOSPair(t *testing.T) {
	evm := newMockEVMForTesting()
	ctx := testContext(common.HexToAddress("0x1001"), evm)
	activationTime := uint64(1234)
	if err := ctx.State.ScheduleDeriwOSUpgrade(arbosState.DeriwOSVersion_ConsensusBlacklist, activationTime); err != nil {
		t.Fatal(err)
	}
	public := &DeriwBlacklistPublic{}
	arbOSVersion, deriwOSVersion, err := public.GetDeriwOSVersion(ctx, evm)
	if err != nil {
		t.Fatal(err)
	}
	if arbOSVersion != ctx.State.ArbOSVersion() || deriwOSVersion != arbosState.DeriwOSVersion_Legacy {
		t.Fatalf("version pair = ArbOS %v / DeriwOS %v", arbOSVersion, deriwOSVersion)
	}
	target, timestamp, scheduledAtArbOS, err := public.GetScheduledDeriwOSUpgrade(ctx, evm)
	if err != nil {
		t.Fatal(err)
	}
	if target != arbosState.DeriwOSVersion_ConsensusBlacklist || timestamp != activationTime || scheduledAtArbOS != arbOSVersion {
		t.Fatalf("scheduled tuple = (%v, %v, %v), want (%v, %v, %v)", target, timestamp, scheduledAtArbOS, arbosState.DeriwOSVersion_ConsensusBlacklist, activationTime, arbOSVersion)
	}
}

func TestLegacyBlacklistSchedulerCannotScheduleChainOwnerGovernance(t *testing.T) {
	evm := newMockEVMForTesting()
	ctx := testContext(common.HexToAddress("0x1001"), evm)
	legacy := &DeriwBlacklist{}

	if err := legacy.ScheduleDeriwOSUpgrade(
		ctx,
		evm,
		arbosState.DeriwOSVersion_ChainOwnerUpgradeScheduling,
		1234,
	); err == nil {
		t.Fatal("legacy blacklist scheduler accepted DeriwOS 4")
	}
	version, timestamp, arbosVersion, err := ctx.State.GetScheduledDeriwOSUpgrade()
	if err != nil {
		t.Fatal(err)
	}
	if version != 0 || timestamp != 0 || arbosVersion != 0 {
		t.Fatalf("rejected legacy schedule changed state to (%v, %v, %v)", version, timestamp, arbosVersion)
	}
}
