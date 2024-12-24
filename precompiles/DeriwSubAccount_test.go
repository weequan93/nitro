// Copyright 2021-2022, Offchain Labs, Inc.
// For license information, see https://github.com/nitro/blob/master/LICENSE

package precompiles

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

func TestDeriwSubAccount(t *testing.T) {
	evm := newMockEVMForTesting()
	caller := common.BytesToAddress(crypto.Keccak256([]byte{})[:20])
	// tracer := util.NewTracingInfo(evm, testhelpers.RandomAddress(), types.ArbosAddress, util.TracingDuringEVM)
	// state, err := arbosState.OpenArbosState(evm.StateDB, burn.NewSystemBurner(tracer, false))
	// Require(t, err)
	// Require(t, state.DeriwGasless().Add(caller))

	addr1 := common.BytesToAddress(crypto.Keccak256([]byte{1})[:20])
	addr2 := common.BytesToAddress(crypto.Keccak256([]byte{2})[:20])
	addr3 := common.BytesToAddress(crypto.Keccak256([]byte{3})[:20])

	prec := &DeriwSubAccount{}
	// gasInfo := &ArbGasInfo{}
	callCtx := testContext(caller, evm)

	// the zero address is an owner by default
	Require(t, prec.RemoveSubAccountOwner(callCtx, evm, common.Address{}))

	Require(t, prec.AddSubAccountOwner(callCtx, evm, addr1))
	Require(t, prec.AddSubAccountOwner(callCtx, evm, addr2))
	Require(t, prec.AddSubAccountOwner(callCtx, evm, addr1))

	member, err := prec.IsSubAccountOwner(callCtx, evm, addr1)
	Require(t, err)
	if !member {
		Fail(t)
	}

	member, err = prec.IsSubAccountOwner(callCtx, evm, addr2)
	Require(t, err)
	if !member {
		Fail(t)
	}

	member, err = prec.IsSubAccountOwner(callCtx, evm, addr3)
	Require(t, err)
	if member {
		Fail(t)
	}

	Require(t, prec.RemoveSubAccountOwner(callCtx, evm, addr1))
	member, err = prec.IsSubAccountOwner(callCtx, evm, addr1)
	Require(t, err)
	if member {
		Fail(t)
	}
	member, err = prec.IsSubAccountOwner(callCtx, evm, addr2)
	Require(t, err)
	if !member {
		Fail(t)
	}

	Require(t, prec.AddSubAccountOwner(callCtx, evm, addr1))
	all, err := prec.GetAllSubAccountOwner(callCtx, evm)
	Require(t, err)
	if len(all) != 3 {
		Fail(t)
	}
	if all[0] == all[1] || all[1] == all[2] || all[0] == all[2] {
		Fail(t)
	}
	if all[0] != addr1 && all[1] != addr1 && all[2] != addr1 {
		Fail(t)
	}
	if all[0] != addr2 && all[1] != addr2 && all[2] != addr2 {
		Fail(t)
	}
	if all[0] != caller && all[1] != caller && all[2] != caller {
		Fail(t)
	}
}

func TestUsdtSetting(t *testing.T) {
	evm := newMockEVMForTesting()
	caller := common.BytesToAddress(crypto.Keccak256([]byte{})[:20])
	// tracer := util.NewTracingInfo(evm, testhelpers.RandomAddress(), types.ArbosAddress, util.TracingDuringEVM)
	// state, err := arbosState.OpenArbosState(evm.StateDB, burn.NewSystemBurner(tracer, false))
	// Require(t, err)
	// Require(t, state.DeriwGasless().Add(caller))

	usdt := common.BytesToAddress(crypto.Keccak256([]byte{1})[:20])
	fakeUsdt := common.BytesToAddress(crypto.Keccak256([]byte{2})[:20])

	prec := &DeriwSubAccount{}
	// gasInfo := &ArbGasInfo{}
	callCtx := testContext(caller, evm)

	// the zero address is an owner by default
	Require(t, prec.AddUsdtAddress(callCtx, evm, usdt))
	isUsdtAddress, err := prec.IsUsdtAddress(callCtx, evm, usdt)
	Require(t, err)
	if isUsdtAddress != true {
		Fail(t, "usdt address not equal")
	}

	isUsdtAddress, err = prec.IsUsdtAddress(callCtx, evm, fakeUsdt)
	if isUsdtAddress == true {
		Fail(t, "usdt address not equal")
	}

}

func TestDeriwAllowedAddress(t *testing.T) {
	evm := newMockEVMForTesting()
	caller := common.BytesToAddress(crypto.Keccak256([]byte{})[:20])

	addr1 := common.BytesToAddress(crypto.Keccak256([]byte{1})[:20])
	addr2 := common.BytesToAddress(crypto.Keccak256([]byte{2})[:20])
	addr3 := common.BytesToAddress(crypto.Keccak256([]byte{3})[:20])

	prec := &DeriwSubAccount{}
	// gasInfo := &ArbGasInfo{}
	callCtx := testContext(caller, evm)

	// the zero address is an owner by default
	Require(t, prec.RemoveAllowedAddress(callCtx, evm, common.Address{}))

	Require(t, prec.AddAllowedAddress(callCtx, evm, addr1))
	Require(t, prec.AddAllowedAddress(callCtx, evm, addr2))
	Require(t, prec.AddAllowedAddress(callCtx, evm, addr1))

	member, err := prec.IsAllowedAddress(callCtx, evm, addr1)
	Require(t, err)
	if !member {
		Fail(t)
	}

	member, err = prec.IsAllowedAddress(callCtx, evm, addr2)
	Require(t, err)
	if !member {
		Fail(t)
	}

	member, err = prec.IsAllowedAddress(callCtx, evm, addr3)
	Require(t, err)
	if member {
		Fail(t)
	}

	Require(t, prec.RemoveAllowedAddress(callCtx, evm, addr1))
	member, err = prec.IsAllowedAddress(callCtx, evm, addr1)
	Require(t, err)
	if member {
		Fail(t)
	}
	member, err = prec.IsAllowedAddress(callCtx, evm, addr2)
	Require(t, err)
	if !member {
		Fail(t)
	}

	Require(t, prec.AddSubAccountOwner(callCtx, evm, addr1))
	all, err := prec.GetAllAllowedAddress(callCtx, evm)
	Require(t, err)
	if len(all) != 3 {
		Fail(t)
	}
	if all[0] == all[1] || all[1] == all[2] || all[0] == all[2] {
		Fail(t)
	}
	if all[0] != addr1 && all[1] != addr1 && all[2] != addr1 {
		Fail(t)
	}
	if all[0] != addr2 && all[1] != addr2 && all[2] != addr2 {
		Fail(t)
	}
	if all[0] != caller && all[1] != caller && all[2] != caller {
		Fail(t)
	}
}
