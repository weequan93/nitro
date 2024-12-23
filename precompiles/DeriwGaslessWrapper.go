// Copyright 2021-2023, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE

package precompiles

import (
	"errors"
	"math/big"

	"github.com/offchainlabs/nitro/arbos/arbosState"
	"github.com/offchainlabs/nitro/arbos/util"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/log"
)

// OwnerPrecompile is a precompile wrapper for those only chain owners may use
type DeriwGaslessPrecompile struct {
	precompile  ArbosPrecompile
	emitSuccess func(mech, bytes4, addr, []byte) error
}

func deriwGaslessOwnerOnly(address addr, impl ArbosPrecompile, emit func(mech, bytes4, addr, []byte) error) (addr, ArbosPrecompile) {
	return address, &DeriwGaslessPrecompile{
		precompile:  impl,
		emitSuccess: emit,
	}
}

func (wrapper *DeriwGaslessPrecompile) Call(
	input []byte,
	precompileAddress common.Address,
	actingAsAddress common.Address,
	caller common.Address,
	value *big.Int,
	readOnly bool,
	gasSupplied uint64,
	evm *vm.EVM,
) ([]byte, uint64, error) {
	con := wrapper.precompile

	burner := &Context{
		gasSupplied: gasSupplied,
		gasLeft:     gasSupplied,
		tracingInfo: util.NewTracingInfo(evm, caller, precompileAddress, util.TracingDuringEVM),
	}
	state, err := arbosState.OpenArbosState(evm.StateDB, burner)
	if err != nil {
		return nil, burner.gasLeft, err
	}

	owners := state.GaslessOwners()
	isOwner, err := owners.IsMember(caller)
	if err != nil {
		return nil, burner.gasLeft, err
	}

	chainOwners := state.ChainOwners()
	isChainOwner, err := chainOwners.IsMember(caller)
	if err != nil {
		return nil, burner.gasLeft, err
	}

	if !isOwner && !isChainOwner {
		return nil, burner.gasLeft, errors.New("unauthorized caller to access-controlled method")
	}

	output, _, err := con.Call(input, precompileAddress, actingAsAddress, caller, value, readOnly, gasSupplied, evm)

	if err != nil {
		return output, gasSupplied, err // we don't deduct gas since we don't want to charge the owner
	}

	version := arbosState.ArbOSVersion(evm.StateDB)
	if !readOnly || version < 11 {
		// log that the owner operation succeeded
		if err := wrapper.emitSuccess(evm, *(*[4]byte)(input[:4]), caller, input); err != nil {
			log.Error("failed to emit OwnerActs event", "err", err)
		}
	}

	return output, gasSupplied, err // we don't deduct gas since we don't want to charge the owner
}

func (wrapper *DeriwGaslessPrecompile) Precompile() *Precompile {
	con := wrapper.precompile
	return con.Precompile()
}
