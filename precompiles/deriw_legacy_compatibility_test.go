// Copyright 2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package precompiles

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/offchainlabs/nitro/arbos/arbosState"
	"github.com/offchainlabs/nitro/arbos/storage"
	"github.com/offchainlabs/nitro/cmd/chaininfo"
	"github.com/offchainlabs/nitro/solgen/go/precompilesgen"
)

// These four baseline bodies match production commit 16c17ee3a9c4.
type deriwLegacyCompatLegacyBlacklist struct{ DeriwBlacklist }

func (con deriwLegacyCompatLegacyBlacklist) AddBlacklistTxFrom(c ctx, evm mech, target addr) error {
	return c.State.Blacklist().TxFromAddrs().Add(target)
}
func (con deriwLegacyCompatLegacyBlacklist) AddBlacklistTxTo(c ctx, evm mech, target addr) error {
	return c.State.Blacklist().TxToAddrs().Add(target)
}

type deriwLegacyCompatLegacyOwner struct{ ArbOwner }

func (con deriwLegacyCompatLegacyOwner) SetNetworkFeeAccount(c ctx, evm mech, target addr) error {
	return c.State.SetNetworkFeeAccount(target)
}
func (con deriwLegacyCompatLegacyOwner) SetInfraFeeAccount(c ctx, evm mech, target addr) error {
	return c.State.SetInfraFeeAccount(target)
}

func deriwLegacyCompatEVM(version uint64, caller addr) *vm.EVM {
	cfg := chaininfo.ArbitrumDevTestChainConfig()
	cfg.ChainID = big.NewInt(2886)
	cfg.ArbitrumChainParams.InitialArbOSVersion = version
	evm := newMockEVMForTestingWithConfigs(cfg, cfg)
	state := arbosState.OpenSystemArbosStateOrPanic(evm.StateDB, nil, false)
	if err := state.ChainOwners().Add(caller); err != nil {
		panic(err)
	}
	return evm
}

func TestDeriwLegacyProductionGasBoundaries(t *testing.T) {
	caller := common.HexToAddress("0x1001")
	target := common.HexToAddress("0x1002")
	for _, version := range []uint64{32, 60} {
		for _, method := range []string{"addBlacklistTxFrom", "addBlacklistTxTo", "setNetworkFeeAccount", "setInfraFeeAccount"} {
			t.Run(fmt.Sprintf("ArbOS%d/%s", version, method), func(t *testing.T) {
				fleet := Precompiles()
				var current ArbosPrecompile
				var legacyInner *Precompile
				var legacy ArbosPrecompile
				var input []byte
				var err error
				if method == "addBlacklistTxFrom" || method == "addBlacklistTxTo" {
					current = fleet[types.DeriwBlacklistAddress]
					impl := current.Precompile().implementer.Interface().(*DeriwBlacklist)
					_, legacyInner = MakePrecompile(precompilesgen.DeriwBlacklistMetaData, &deriwLegacyCompatLegacyBlacklist{*impl})
					_, legacy = deriwBlacklistOwnerOnly(current.Address(), legacyInner, current.(*DeriwBlacklistPrecompile).emitSuccess)
					abi, _ := precompilesgen.DeriwBlacklistMetaData.GetAbi()
					input, err = abi.Pack(method, target)
				} else {
					current = fleet[types.ArbOwnerAddress]
					impl := current.Precompile().implementer.Interface().(*ArbOwner)
					_, legacyInner = MakePrecompile(precompilesgen.ArbOwnerMetaData, &deriwLegacyCompatLegacyOwner{*impl})
					_, legacy = ownerOnly(current.Address(), legacyInner, current.(*OwnerPrecompile).emitSuccess)
					abi, _ := precompilesgen.ArbOwnerMetaData.GetAbi()
					input, err = abi.Pack(method, target)
				}
				if err != nil {
					t.Fatal(err)
				}
				probe := deriwLegacyCompatEVM(version, caller)
				_, left, _, err := legacyInner.Call(input, current.Address(), caller, big.NewInt(0), false, 1_000_000, probe)
				if err != nil {
					t.Fatal(err)
				}
				budget := uint64(1_000_000) - left
				oldEVM := deriwLegacyCompatEVM(version, caller)
				newEVM := deriwLegacyCompatEVM(version, caller)
				_, _, _, oldErr := legacy.Call(input, current.Address(), caller, big.NewInt(0), false, budget, oldEVM)
				_, _, _, newErr := current.Call(input, current.Address(), caller, big.NewInt(0), false, budget, newEVM)
				controlEVM := deriwLegacyCompatEVM(version, caller)
				_, _, _, controlErr := current.Call(input, current.Address(), caller, big.NewInt(0), false, budget+storage.StorageReadCost, controlEVM)
				if controlErr != nil {
					t.Fatalf("extra-read control unexpectedly failed: %v", controlErr)
				}
				t.Logf("ArbOS=%d DeriwOS=%d legacy_success_budget=%d legacy_err=%v candidate_err=%v candidate_plus_%d_gas_err=%v", version, arbosState.DeriwOSVersion(newEVM.StateDB), budget, oldErr, newErr, storage.StorageReadCost, controlErr)
				if oldErr != nil {
					t.Fatalf("baseline unexpectedly failed: %v", oldErr)
				}
				if newErr != nil {
					t.Errorf("legacy successful call becomes a failure before any DeriwOS upgrade: %v", newErr)
				}
				// Preserve failed legacy calls too: making a historical failure
				// succeed would be another consensus compatibility change.
				oldBelow := deriwLegacyCompatEVM(version, caller)
				newBelow := deriwLegacyCompatEVM(version, caller)
				_, _, _, oldBelowErr := legacy.Call(input, current.Address(), caller, big.NewInt(0), false, budget-1, oldBelow)
				_, _, _, newBelowErr := current.Call(input, current.Address(), caller, big.NewInt(0), false, budget-1, newBelow)
				if oldBelowErr == nil || newBelowErr == nil {
					t.Fatalf("call below legacy threshold changed: legacy=%v candidate=%v", oldBelowErr, newBelowErr)
				}
			})
		}
	}
}

func TestDeriwLegacyProductionUnknownSelectors(t *testing.T) {
	for _, version := range []uint64{32, 60} {
		for _, address := range []common.Address{types.DeriwBlacklistPublicAddress, types.ArbOwnerPublicAddress} {
			t.Run(fmt.Sprintf("ArbOS%d/%s", version, address.Hex()), func(t *testing.T) {
				caller := common.HexToAddress("0x1001")
				evm := deriwLegacyCompatEVM(version, caller)
				current := Precompiles()[address].Precompile()
				abi, _ := precompilesgen.DeriwBlacklistPublicMetaData.GetAbi()
				input, err := abi.Pack("getDeriwOSVersion")
				if err != nil {
					t.Fatal(err)
				}
				legacy := *current
				legacy.methods = make(map[[4]byte]*PrecompileMethod)
				for selector, method := range current.methods {
					legacy.methods[selector] = method
				}
				var selector [4]byte
				copy(selector[:], input[:4])
				delete(legacy.methods, selector)
				oldOut, oldLeft, _, oldErr := legacy.Call(input, address, caller, big.NewInt(0), true, 100_000, evm)
				out, left, _, newErr := current.Call(input, address, caller, big.NewInt(0), true, 100_000, evm)
				t.Logf("ArbOS=%d DeriwOS=%d address=%s selector=%x legacy=(out:%x,left:%d,err:%v) candidate=(out:%x,left:%d,err:%v)", version, arbosState.DeriwOSVersion(evm.StateDB), address.Hex(), selector, oldOut, oldLeft, oldErr, out, left, newErr)
				if oldErr == nil {
					t.Fatal("baseline should reject unknown selector")
				}
				if newErr == nil {
					t.Error("new selector already changes legacy consensus before DeriwOS activation")
				}
			})
		}
	}
}
