// Copyright 2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md
package precompiles

import (
	"bytes"
	"encoding/json"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"

	"github.com/offchainlabs/nitro/arbos"
	"github.com/offchainlabs/nitro/arbos/arbosState"
	"github.com/offchainlabs/nitro/cmd/chaininfo"
	"github.com/offchainlabs/nitro/solgen/go/precompilesgen"
)

func newDeriwBootstrapEVM(chainID uint64) (*vm.EVM, common.Address) {
	cfg := chaininfo.ArbitrumDevTestChainConfig()
	cfg.ChainID = new(big.Int).SetUint64(chainID)
	cfg.ArbitrumChainParams.InitialArbOSVersion = 60
	evm := newMockEVMForTestingWithConfigs(cfg, cfg)
	caller := common.HexToAddress("0x1001")
	state := arbosState.OpenSystemArbosStateOrPanic(evm.StateDB, nil, false)
	if err := state.ChainOwners().Add(caller); err != nil {
		panic(err)
	}
	evm.ProcessingHook = arbos.NewTxProcessor(evm, &core.Message{From: caller, TxRunContext: core.NewMessageReplayContext()})
	return evm, caller
}

func TestDeriwProductionBootstrapThroughExistingOwnerMethod(t *testing.T) {
	evm, caller := newDeriwBootstrapEVM(arbosState.DeriwProdChainID)
	evm.Context.Time = 100
	contracts := Precompiles()
	ownerABI, err := precompilesgen.ArbOwnerMetaData.GetAbi()
	Require(t, err)
	publicABI, err := precompilesgen.ArbOwnerPublicMetaData.GetAbi()
	Require(t, err)
	getVersions, err := publicABI.Pack("getDeriwOSVersion")
	Require(t, err)
	query := func(want uint64) {
		t.Helper()
		p := contracts[types.ArbOwnerPublicAddress]
		out, _, _, err := p.Call(getVersions, p.Address(), caller, big.NewInt(0), true, 1_000_000, evm)
		Require(t, err)
		values, err := publicABI.Unpack("getDeriwOSVersion", out)
		Require(t, err)
		if values[0].(uint64) != 60 || values[1].(uint64) != want {
			t.Fatalf("wrong version pair: %v", values)
		}
	}
	invokeOwner := func(input []byte) error {
		p := contracts[types.ArbOwnerAddress]
		_, _, _, err := p.Call(input, p.Address(), caller, big.NewInt(0), false, 1_000_000, evm)
		return err
	}
	schedule, err := ownerABI.Pack("scheduleDeriwOSUpgrade", uint64(1), uint64(300))
	Require(t, err)
	if err := invokeOwner(schedule); err == nil {
		t.Fatal("new scheduler available without bootstrap configuration")
	}
	// The old ABI already has setChainConfig(string). Preserve the complete
	// configuration and an unrelated opaque field while adding the fork time.
	raw, err := json.Marshal(evm.ChainConfig())
	Require(t, err)
	initialState := arbosState.OpenSystemArbosStateOrPanic(evm.StateDB, nil, false)
	Require(t, initialState.SetChainConfig(raw))
	var cfg map[string]json.RawMessage
	Require(t, json.Unmarshal(raw, &cfg))
	cfg["deriwOSBootstrapTime"] = json.RawMessage("200")
	cfg["unrelatedOpaqueField"] = json.RawMessage(`{"preserved":true}`)
	raw, err = json.Marshal(cfg)
	Require(t, err)
	setConfig, err := ownerABI.Pack("setChainConfig", string(raw))
	Require(t, err)
	// Preview follows the old ABI's existing compatibility checks, which must
	// accept the opaque field without changing any recognized fork settings.
	snapshot := evm.StateDB.Snapshot()
	evm.ProcessingHook = arbos.NewTxProcessor(evm, &core.Message{From: caller, TxRunContext: core.NewMessageEthcallContext()})
	Require(t, invokeOwner(setConfig))
	evm.StateDB.RevertToSnapshot(snapshot)
	evm.ProcessingHook = arbos.NewTxProcessor(evm, &core.Message{From: caller, TxRunContext: core.NewMessageReplayContext()})
	Require(t, invokeOwner(setConfig))
	state := arbosState.OpenSystemArbosStateOrPanic(evm.StateDB, nil, false)
	stored, err := state.ChainConfig()
	Require(t, err)
	if !bytes.Equal(raw, stored) {
		t.Fatal("existing owner method rewrote opaque configuration bytes")
	}
	// Unknown metadata leaves the legacy Ethereum chain configuration intact.
	var decodedConfig map[string]json.RawMessage
	Require(t, json.Unmarshal(stored, &decodedConfig))
	if !bytes.Equal(decodedConfig["chainId"], cfg["chainId"]) {
		t.Fatal("chain ID changed")
	}
	for _, timestamp := range []uint64{100, 199} {
		evm.Context.Time = timestamp
		p := contracts[types.ArbOwnerPublicAddress]
		if _, _, _, err := p.Call(getVersions, p.Address(), caller, big.NewInt(0), true, 100_000, evm); err == nil {
			t.Fatal("API activated before bootstrap time")
		}
		if err := invokeOwner(schedule); err == nil {
			t.Fatal("scheduler activated before bootstrap time")
		}
	}
	evm.Context.Time = 200
	query(0)
	Require(t, invokeOwner(schedule))
	state = arbosState.OpenSystemArbosStateOrPanic(evm.StateDB, nil, false)
	version, at, arbosVersion, err := state.GetScheduledDeriwOSUpgrade()
	Require(t, err)
	if version != 1 || at != 300 || arbosVersion != 60 {
		t.Fatalf("wrong custom schedule: %d %d %d", version, at, arbosVersion)
	}
	// Protection remains armed while version 0 is waiting for version 1.
	blacklistABI, err := precompilesgen.DeriwBlacklistMetaData.GetAbi()
	Require(t, err)
	protectedAdd, err := blacklistABI.Pack("addBlacklistTxTo", types.ArbOwnerAddress)
	Require(t, err)
	bp := contracts[types.DeriwBlacklistAddress]
	if _, _, _, err := bp.Call(protectedAdd, bp.Address(), caller, big.NewInt(0), false, 1_000_000, evm); err == nil {
		t.Fatal("scheduled upgrade lost protected-address safety")
	}
	Require(t, state.UpgradeDeriwOSVersionIfNecessary(299))
	query(0)
	Require(t, state.UpgradeDeriwOSVersionIfNecessary(300))
	evm.Context.Time = 300
	query(1)
	// Active DeriwOS histories remain readable if later configuration updates
	// omit the bootstrap metadata; the stored version is the durable marker.
	delete(cfg, "deriwOSBootstrapTime")
	raw, err = json.Marshal(cfg)
	Require(t, err)
	setConfig, err = ownerABI.Pack("setChainConfig", string(raw))
	Require(t, err)
	Require(t, invokeOwner(setConfig))
	query(1)
}

func TestDeriwBootstrapKeepsDeployedDevAndTestLegacyBehavior(t *testing.T) {
	for _, chainID := range []uint64{arbosState.DeriwDevChainID, arbosState.DeriwTestChainID} {
		evm, caller := newDeriwBootstrapEVM(chainID)
		publicABI, err := precompilesgen.DeriwBlacklistPublicMetaData.GetAbi()
		Require(t, err)
		input, err := publicABI.Pack("getDeriwOSVersion")
		Require(t, err)
		p := Precompiles()[types.DeriwBlacklistPublicAddress]
		if _, _, _, err := p.Call(input, p.Address(), caller, big.NewInt(0), true, 100_000, evm); err != nil {
			t.Fatalf("changed deployed API behavior on chain %d: %v", chainID, err)
		}
		// Dev/test's already-deployed version-0 implementation meters the pending
		// slot. Preserve that boundary rather than rewriting their old history.
		blacklistABI, err := precompilesgen.DeriwBlacklistMetaData.GetAbi()
		Require(t, err)
		input, err = blacklistABI.Pack("addBlacklistTxFrom", common.HexToAddress("0x1002"))
		Require(t, err)
		p = Precompiles()[types.DeriwBlacklistAddress]
		if _, _, _, err := p.Call(input, p.Address(), caller, big.NewInt(0), false, 63203, evm); err == nil {
			t.Fatalf("rewrote deployed version-0 gas boundary on chain %d", chainID)
		}
		evm, caller = newDeriwBootstrapEVM(chainID)
		if _, _, _, err := p.Call(input, p.Address(), caller, big.NewInt(0), false, 64003, evm); err != nil {
			t.Fatalf("deployed gas budget no longer succeeds on chain %d: %v", chainID, err)
		}
	}
}

// Counting reads makes the gas-zero regression independent of timing or the
// host's speed: a budgetless call must not traverse a large configuration.
type deriwBootstrapCountingState struct {
	vm.StateDB
	reads int
}

func (db *deriwBootstrapCountingState) GetState(address common.Address, key common.Hash) common.Hash {
	db.reads++
	return db.StateDB.GetState(address, key)
}
func TestDeriwProductionBootstrapMetersConfigurationLookup(t *testing.T) {
	evm, caller := newDeriwBootstrapEVM(arbosState.DeriwProdChainID)
	state := arbosState.OpenSystemArbosStateOrPanic(evm.StateDB, nil, false)
	config := `{"chainId":2886,"padding":"` + strings.Repeat("x", 32000) + `","deriwOSBootstrapTime":100}`
	Require(t, state.SetChainConfig([]byte(config)))
	evm.Context.Time = 200
	counted := &deriwBootstrapCountingState{StateDB: evm.StateDB}
	evm.StateDB = counted
	abi, err := precompilesgen.ArbOwnerPublicMetaData.GetAbi()
	Require(t, err)
	input, err := abi.Pack("getDeriwOSVersion")
	Require(t, err)
	p := Precompiles()[types.ArbOwnerPublicAddress]
	if _, _, _, err := p.Call(input, p.Address(), caller, big.NewInt(0), true, 0, evm); err == nil {
		t.Fatal("budgetless configuration lookup succeeded")
	}
	if counted.reads > 6 {
		t.Fatalf("zero-gas call traversed configuration: %d reads", counted.reads)
	}
	// A sufficient budget succeeds and includes the configuration reads in the
	// reported gas, rather than merely doing metered work with an ignored bill.
	counted.reads = 0
	const budget = uint64(2_000_000)
	out, left, used, err := p.Call(input, p.Address(), caller, big.NewInt(0), true, budget, evm)
	Require(t, err)
	if len(out) != 64 {
		t.Fatalf("wrong version result: %x", out)
	}
	if used.SingleGas() != budget-left || used.SingleGas() < 700_000 {
		t.Fatalf("configuration reads were not included in gas: used=%d left=%d", used.SingleGas(), left)
	}
}

// Exercise the complete introduced API surface through actual dispatch. A
// missing gate must fail even if a handler would independently reject its data.
func TestDeriwProductionBootstrapAllIntroducedMethods(t *testing.T) {
	ownerABI, err := precompilesgen.ArbOwnerMetaData.GetAbi()
	Require(t, err)
	publicABI, err := precompilesgen.ArbOwnerPublicMetaData.GetAbi()
	Require(t, err)
	blacklistABI, err := precompilesgen.DeriwBlacklistMetaData.GetAbi()
	Require(t, err)
	blacklistPublicABI, err := precompilesgen.DeriwBlacklistPublicMetaData.GetAbi()
	Require(t, err)
	router := common.HexToAddress("0x1002")
	canonical := common.HexToAddress("0x1003")
	gateway := common.HexToAddress("0x1004")
	cases := []struct {
		address     common.Address
		contractABI *abi.ABI
		method      string
		args        []interface{}
	}{
		{types.ArbOwnerPublicAddress, publicABI, "getDeriwRouterConfig", nil},
		{types.ArbOwnerPublicAddress, publicABI, "getScheduledDeriwRouterConfig", nil},
		{types.ArbOwnerPublicAddress, publicABI, "getDeriwOSVersion", nil},
		{types.ArbOwnerPublicAddress, publicABI, "getScheduledDeriwOSUpgrade", nil},
		{types.DeriwBlacklistPublicAddress, blacklistPublicABI, "getDeriwOSVersion", nil},
		{types.DeriwBlacklistPublicAddress, blacklistPublicABI, "getScheduledDeriwOSUpgrade", nil},
		{types.ArbOwnerAddress, ownerABI, "scheduleDeriwRouterConfig", []interface{}{router, canonical, []common.Address{gateway}, uint64(200 + arbosState.DeriwRouterConfigUpdateDelay)}},
		{types.ArbOwnerAddress, ownerABI, "cancelScheduledDeriwRouterConfig", nil},
		{types.ArbOwnerAddress, ownerABI, "scheduleDeriwOSUpgrade", []interface{}{uint64(1), uint64(300)}},
		{types.ArbOwnerAddress, ownerABI, "cancelScheduledDeriwOSUpgrade", nil},
		{types.DeriwBlacklistAddress, blacklistABI, "scheduleDeriwOSUpgrade", []interface{}{uint64(1), uint64(300)}},
	}
	for _, test := range cases {
		t.Run(test.address.Hex()+"/"+test.method, func(t *testing.T) {
			for _, timestamp := range []uint64{199, 200} {
				evm, caller := newDeriwBootstrapEVM(arbosState.DeriwProdChainID)
				evm.Context.Time = timestamp
				for _, address := range []common.Address{router, canonical, gateway} {
					evm.StateDB.SetCode(address, []byte{byte(vm.INVALID)}, tracing.CodeChangeUnspecified)
				}
				state := arbosState.OpenSystemArbosStateOrPanic(evm.StateDB, nil, false)
				raw, err := json.Marshal(evm.ChainConfig())
				Require(t, err)
				var config map[string]json.RawMessage
				Require(t, json.Unmarshal(raw, &config))
				config["deriwOSBootstrapTime"] = json.RawMessage("200")
				raw, err = json.Marshal(config)
				Require(t, err)
				Require(t, state.SetChainConfig(raw))
				input, err := test.contractABI.Pack(test.method, test.args...)
				Require(t, err)
				const budget = uint64(1_000_000)
				p := Precompiles()[test.address].Precompile()
				out, left, used, err := p.Call(input, test.address, caller, big.NewInt(0), false, budget, evm)
				if timestamp < 200 {
					if err != vm.ErrExecutionReverted || len(out) != 0 || left != 0 || used.SingleGas() != budget {
						t.Fatalf("before boundary changed unknown-selector result: out=%x left=%d used=%d err=%v", out, left, used.SingleGas(), err)
					}
				} else if err != nil {
					t.Fatalf("method unavailable at activation boundary: %v", err)
				}
			}
		})
	}
}
