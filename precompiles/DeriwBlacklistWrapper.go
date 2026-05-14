// Copyright 2021-2023, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE

package precompiles

import (
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/arbitrum/multigas"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"

	"github.com/offchainlabs/nitro/arbos/arbosState"
	"github.com/offchainlabs/nitro/arbos/util"
)

// OwnerPrecompile is a precompile wrapper for those only chain owners may use
type DeriwBlacklistPrecompile struct {
	precompile  ArbosPrecompile
	emitSuccess func(mech, bytes4, addr, []byte) error
}

func deriwBlacklistOwnerOnly(address addr, impl ArbosPrecompile, emit func(mech, bytes4, addr, []byte) error) (addr, ArbosPrecompile) {
	return address, &DeriwBlacklistPrecompile{
		precompile:  impl,
		emitSuccess: emit,
	}
}

func (wrapper *DeriwBlacklistPrecompile) Address() common.Address {
	return wrapper.precompile.Address()
}

func (wrapper *DeriwBlacklistPrecompile) Call(
	input []byte,
	actingAsAddress common.Address,
	caller common.Address,
	value *big.Int,
	readOnly bool,
	gasSupplied uint64,
	evm *vm.EVM,
) ([]byte, uint64, multigas.MultiGas, error) {
	con := wrapper.precompile

	burner := &Context{
		gasSupplied: gasSupplied,
		gasUsed:     multigas.ZeroGas(),
		tracingInfo: util.NewTracingInfo(evm, caller, wrapper.precompile.Address(), util.TracingDuringEVM),
	}
	state, err := arbosState.OpenArbosState(evm.StateDB, burner)
	if err != nil {
		return nil, burner.GasLeft(), burner.gasUsed, err
	}

	owners := state.Blacklist().BlacklistOwner()
	isOwner, err := owners.IsMember(caller)
	if err != nil {
		return nil, burner.GasLeft(), burner.gasUsed, err
	}

	chainOwners := state.ChainOwners()
	isChainOwner, err := chainOwners.IsMember(caller)
	if err != nil {
		return nil, burner.GasLeft(), burner.gasUsed, err
	}

	if !isOwner && !isChainOwner {
		return nil, burner.GasLeft(), burner.gasUsed, errors.New("unauthorized caller to access-controlled method")
	}

	output, _, _, err := con.Call(input, actingAsAddress, caller, value, readOnly, gasSupplied, evm)

	if err != nil {
		return output, gasSupplied, multigas.ZeroGas(), err // we don't deduct gas since we don't want to charge the owner
	}

	version := arbosState.ArbOSVersion(evm.StateDB)
	if !readOnly || version < params.ArbosVersion_11 {
		// log that the owner operation succeeded
		if err := wrapper.emitSuccess(evm, *(*[4]byte)(input[:4]), caller, input); err != nil {
			log.Error("failed to emit OwnerActs event", "err", err)
		}
	}

	return output, gasSupplied, multigas.ZeroGas(), err // we don't deduct gas since we don't want to charge the owner
}

func (wrapper *DeriwBlacklistPrecompile) Precompile() *Precompile {
	con := wrapper.precompile
	return con.Precompile()
}

func (wrapper *DeriwBlacklistPrecompile) Name() string {
	return wrapper.precompile.Name()
}
