// Copyright 2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package arbosState

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	"github.com/offchainlabs/nitro/arbos/deriwpolicy"
	"github.com/offchainlabs/nitro/cmd/chaininfo"
)

func testDeriwRouterConfig(seed byte) deriwpolicy.RouterOnlySendConfig {
	return deriwpolicy.RouterOnlySendConfig{
		Router:                 common.BytesToAddress([]byte{seed, 1}),
		CanonicalGatewayRouter: common.BytesToAddress([]byte{seed, 2}),
		ApprovedTokenGateways: []common.Address{
			common.BytesToAddress([]byte{seed, 3}),
			common.BytesToAddress([]byte{seed, 4}),
		},
	}
}

func requireDeriwRouterConfigEqual(
	t *testing.T,
	got deriwpolicy.RouterOnlySendConfig,
	want deriwpolicy.RouterOnlySendConfig,
) {
	t.Helper()
	if got.Router != want.Router || got.CanonicalGatewayRouter != want.CanonicalGatewayRouter {
		t.Fatalf("route header = (%v, %v), want (%v, %v)", got.Router, got.CanonicalGatewayRouter, want.Router, want.CanonicalGatewayRouter)
	}
	if len(got.ApprovedTokenGateways) != len(want.ApprovedTokenGateways) {
		t.Fatalf("gateway count = %v, want %v", len(got.ApprovedTokenGateways), len(want.ApprovedTokenGateways))
	}
	for index := range want.ApprovedTokenGateways {
		if got.ApprovedTokenGateways[index] != want.ApprovedTokenGateways[index] {
			t.Fatalf("gateway[%v] = %v, want %v", index, got.ApprovedTokenGateways[index], want.ApprovedTokenGateways[index])
		}
	}
}

func TestDeriwRouterConfigBootstrapsAtVersionActivation(t *testing.T) {
	chainConfig := chaininfo.ArbitrumDevTestChainConfig()
	chainConfig.ChainID = new(big.Int).SetUint64(deriwpolicy.DevChainID)
	state, _ := NewArbosMemoryBackedArbOSStateWithConfig(chainConfig)

	if _, revision, configured, err := state.ActiveDeriwRouterConfig(); err != nil || configured || revision != 0 {
		t.Fatalf("route initialized before DeriwOS activation: configured=%v revision=%v err=%v", configured, revision, err)
	}
	if err := state.UpgradeDeriwOSVersion(DeriwOSVersion_RouterOnlySends); err != nil {
		t.Fatal(err)
	}

	got, revision, configured, err := state.ActiveDeriwRouterConfig()
	if err != nil || !configured || revision != 1 {
		t.Fatalf("bootstrap route status: configured=%v revision=%v err=%v", configured, revision, err)
	}
	want, _ := deriwpolicy.RouterOnlySendConfigForChainID(chainConfig.ChainID)
	requireDeriwRouterConfigEqual(t, got, want)
}

func TestDeriwRouterConfigScheduleActivateAndCancel(t *testing.T) {
	chainConfig := chaininfo.ArbitrumDevTestChainConfig()
	chainConfig.ChainID = new(big.Int).SetUint64(deriwpolicy.DevChainID)
	state, _ := NewArbosMemoryBackedArbOSStateWithConfig(chainConfig)
	if err := state.UpgradeDeriwOSVersion(DeriwOSVersion_RouterOnlySends); err != nil {
		t.Fatal(err)
	}

	now := uint64(1_000_000)
	activation := now + DeriwRouterConfigUpdateDelay
	replacement := testDeriwRouterConfig(0x20)
	if err := state.ScheduleDeriwRouterConfig(replacement, now, activation-1); err == nil {
		t.Fatal("accepted a route update before the minimum delay")
	}
	if err := state.ScheduleDeriwRouterConfig(replacement, now, activation); err != nil {
		t.Fatal(err)
	}

	pending, revision, pendingAt, configured, err := state.PendingDeriwRouterConfig()
	if err != nil || !configured || revision != 2 || pendingAt != activation {
		t.Fatalf("pending route status: configured=%v revision=%v activation=%v err=%v", configured, revision, pendingAt, err)
	}
	requireDeriwRouterConfigEqual(t, pending, replacement)

	if err := state.ActivateDeriwRouterConfigIfNecessary(activation - 1); err != nil {
		t.Fatal(err)
	}
	active, revision, configured, err := state.ActiveDeriwRouterConfig()
	if err != nil || !configured || revision != 1 || active.Router == replacement.Router {
		t.Fatalf("route activated early: configured=%v revision=%v route=%+v err=%v", configured, revision, active, err)
	}
	if err := state.ActivateDeriwRouterConfigIfNecessary(activation); err != nil {
		t.Fatal(err)
	}
	active, revision, configured, err = state.ActiveDeriwRouterConfig()
	if err != nil || !configured || revision != 2 {
		t.Fatalf("replacement route status: configured=%v revision=%v err=%v", configured, revision, err)
	}
	requireDeriwRouterConfigEqual(t, active, replacement)
	if _, _, _, configured, err := state.PendingDeriwRouterConfig(); err != nil || configured {
		t.Fatalf("pending route remained after activation: configured=%v err=%v", configured, err)
	}

	second := testDeriwRouterConfig(0x30)
	if err := state.ScheduleDeriwRouterConfig(second, activation, activation+DeriwRouterConfigUpdateDelay); err != nil {
		t.Fatal(err)
	}
	if err := state.CancelScheduledDeriwRouterConfig(); err != nil {
		t.Fatal(err)
	}
	if _, _, _, configured, err := state.PendingDeriwRouterConfig(); err != nil || configured {
		t.Fatalf("cancel did not clear pending route: configured=%v err=%v", configured, err)
	}
	active, revision, configured, err = state.ActiveDeriwRouterConfig()
	if err != nil || !configured || revision != 2 {
		t.Fatalf("cancel changed active route: configured=%v revision=%v err=%v", configured, revision, err)
	}
	requireDeriwRouterConfigEqual(t, active, replacement)
}

func TestUnconfiguredChainCanBeGovernanceConfiguredBeforeDeriwOSActivation(t *testing.T) {
	chainConfig := chaininfo.ArbitrumDevTestChainConfig()
	chainConfig.ChainID = new(big.Int).SetUint64(deriwpolicy.TestChainID)
	state, _ := NewArbosMemoryBackedArbOSStateWithConfig(chainConfig)
	routes := testDeriwRouterConfig(0x40)
	now := uint64(2_000_000)
	activation := now + DeriwRouterConfigUpdateDelay

	if err := state.ScheduleDeriwRouterConfig(routes, now, activation); err != nil {
		t.Fatal(err)
	}
	if err := state.ActivateDeriwRouterConfigIfNecessary(activation); err != nil {
		t.Fatal(err)
	}
	if err := state.UpgradeDeriwOSVersion(DeriwOSVersion_RouterOnlySends); err != nil {
		t.Fatalf("governance-configured chain could not activate router-only sends: %v", err)
	}
	active, revision, configured, err := state.ActiveDeriwRouterConfig()
	if err != nil || !configured || revision != 1 {
		t.Fatalf("configured route status: configured=%v revision=%v err=%v", configured, revision, err)
	}
	requireDeriwRouterConfigEqual(t, active, routes)
}
