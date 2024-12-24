// Copyright 2021-2022, Offchain Labs, Inc.
// For license information, see https://github.com/nitro/blob/master/LICENSE

package precompiles

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

func TestGaslessOwnerPublic(t *testing.T) {
	evm := newMockEVMForTesting()
	caller := common.BytesToAddress(crypto.Keccak256([]byte{})[:20])
	// tracer := util.NewTracingInfo(evm, testhelpers.RandomAddress(), types.ArbosAddress, util.TracingDuringEVM)
	// state, err := arbosState.OpenArbosState(evm.StateDB, burn.NewSystemBurner(tracer, false))
	// Require(t, err)
	// Require(t, state.DeriwGasless().Add(caller))

	addr1 := common.BytesToAddress(crypto.Keccak256([]byte{1})[:20])
	addr2 := common.BytesToAddress(crypto.Keccak256([]byte{2})[:20])
	addr3 := common.BytesToAddress(crypto.Keccak256([]byte{3})[:20])

	prec := &DeriwGasless{}
	precPublic := &DeriwGaslessPublic{}
	// gasInfo := &ArbGasInfo{}
	callCtx := testContext(caller, evm)

	// the zero address is an owner by default
	Require(t, prec.RemoveGaslessOwner(callCtx, evm, common.Address{}))

	Require(t, prec.AddGaslessOwner(callCtx, evm, addr1))
	Require(t, prec.AddGaslessOwner(callCtx, evm, addr2))
	Require(t, prec.AddGaslessOwner(callCtx, evm, addr1))

	member, err := precPublic.IsGaslessOwner(callCtx, evm, addr1)
	Require(t, err)
	if !member {
		Fail(t)
	}

	member, err = precPublic.IsGaslessOwner(callCtx, evm, addr2)
	Require(t, err)
	if !member {
		Fail(t)
	}

	member, err = precPublic.IsGaslessOwner(callCtx, evm, addr3)
	Require(t, err)
	if member {
		Fail(t)
	}

	Require(t, prec.RemoveGaslessOwner(callCtx, evm, addr1))
	member, err = precPublic.IsGaslessOwner(callCtx, evm, addr1)
	Require(t, err)
	if member {
		Fail(t)
	}
	member, err = precPublic.IsGaslessOwner(callCtx, evm, addr2)
	Require(t, err)
	if !member {
		Fail(t)
	}

	Require(t, prec.AddGaslessOwner(callCtx, evm, addr1))
	all, err := precPublic.GetAllGaslessOwners(callCtx, evm)
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

func TestGaslessPricerPublic(t *testing.T) {
	version := uint64(10)
	evm := newMockEVMForTestingWithVersion(&version)
	caller := common.BytesToAddress(crypto.Keccak256([]byte{})[:20])
	newAddr := common.BytesToAddress(crypto.Keccak256([]byte{0})[:20])
	callCtx := testContext(caller, evm)
	prec := &DeriwGasless{}

	precPublic := &DeriwGaslessPublic{}

	Require(t, prec.AddPricerTxFrom(callCtx, evm, newAddr))
	member, err := precPublic.IsPricerTxFrom(callCtx, evm, newAddr)
	Require(t, err)
	if !member {
		Fail(t)
	}
	all, err := precPublic.GetPricerTxFromAddrs(callCtx, evm)
	Require(t, err)
	if len(all) != 1 {
		Fail(t)
	}
	Require(t, prec.RemovePricerTxFrom(callCtx, evm, newAddr))
	member, err = precPublic.IsPricerTxFrom(callCtx, evm, newAddr)
	Require(t, err)
	if member {
		Fail(t)
	}

	Require(t, prec.AddPricerTxTo(callCtx, evm, newAddr))
	member, err = precPublic.IsPricerTxTo(callCtx, evm, newAddr)
	Require(t, err)
	if !member {
		Fail(t)
	}
	all, err = precPublic.GetPricerTxToAddrs(callCtx, evm)
	Require(t, err)
	if len(all) != 1 {
		Fail(t)
	}
	Require(t, prec.RemovePricerTxTo(callCtx, evm, newAddr))
	member, err = precPublic.IsPricerTxTo(callCtx, evm, newAddr)
	Require(t, err)
	if member {
		Fail(t)
	}
}
