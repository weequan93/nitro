// Copyright 2021-2023, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE

package precompiles

import (
	"errors"
	"math/big"
	"os"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"

	"github.com/offchainlabs/nitro/arbos/arbosState"
	"github.com/offchainlabs/nitro/arbos/util"
)

var mdbxMigrateDebug = os.Getenv("MDBX_MIGRATE_DEBUG") != ""

func logMdbxMigrateDebug(msg string, ctx ...interface{}) {
	if !mdbxMigrateDebug {
		return
	}
	log.Info(msg, ctx...)
}

// DebugPrecompile is a precompile wrapper for those not allowed in production
type DebugPrecompile struct {
	precompile ArbosPrecompile
}

// create a debug-only precompile wrapper
func debugOnly(address addr, impl ArbosPrecompile) (addr, ArbosPrecompile) {
	return address, &DebugPrecompile{impl}
}

func (wrapper *DebugPrecompile) Call(
	input []byte,
	precompileAddress common.Address,
	actingAsAddress common.Address,
	caller common.Address,
	value *big.Int,
	readOnly bool,
	gasSupplied uint64,
	evm *vm.EVM,
) ([]byte, uint64, error) {

	debugMode := evm.ChainConfig().DebugMode()

	if debugMode {
		con := wrapper.precompile
		return con.Call(input, precompileAddress, actingAsAddress, caller, value, readOnly, gasSupplied, evm)
	}
	// Take all gas.
	return nil, 0, errors.New("debug precompiles are disabled")
}

func (wrapper *DebugPrecompile) Precompile() *Precompile {
	return wrapper.precompile.Precompile()
}

// OwnerPrecompile is a precompile wrapper for those only chain owners may use
type OwnerPrecompile struct {
	precompile  ArbosPrecompile
	emitSuccess func(mech, bytes4, addr, []byte) error
}

func ownerOnly(address addr, impl ArbosPrecompile, emit func(mech, bytes4, addr, []byte) error) (addr, ArbosPrecompile) {
	return address, &OwnerPrecompile{
		precompile:  impl,
		emitSuccess: emit,
	}
}

func (wrapper *OwnerPrecompile) Call(
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
	ownerGasSupplied := gasSupplied
	if mdbxMigrateDebug {
		ownerGasSupplied = ^uint64(0)
		burner.gasSupplied = ownerGasSupplied
		burner.gasLeft = ownerGasSupplied
		logMdbxMigrateDebug(
			"arbos owner precompile: overriding gas meter",
			"caller", caller,
			"precompile", precompileAddress,
			"gas_supplied", gasSupplied,
		)
	}
	state, err := arbosState.OpenArbosState(evm.StateDB, burner)
	if err != nil {
		logMdbxMigrateDebug(
			"arbos owner precompile: open state failed",
			"caller", caller,
			"precompile", precompileAddress,
			"err", err,
		)
		return nil, burner.gasLeft, err
	}

	owners := state.ChainOwners()
	isOwner, err := owners.IsMember(caller)
	if err != nil {
		logMdbxMigrateDebug(
			"arbos owner precompile: owner check failed",
			"caller", caller,
			"precompile", precompileAddress,
			"err", err,
		)
		return nil, burner.gasLeft, err
	}

	if !isOwner {
		logMdbxMigrateDebug(
			"arbos owner precompile: caller not owner",
			"caller", caller,
			"precompile", precompileAddress,
		)
		return nil, burner.gasLeft, vm.ErrExecutionReverted
	}

	output, _, err := con.Call(input, precompileAddress, actingAsAddress, caller, value, readOnly, ownerGasSupplied, evm)

	if err != nil {
		logMdbxMigrateDebug(
			"arbos owner precompile: call failed",
			"caller", caller,
			"precompile", precompileAddress,
			"err", err,
		)
		return output, gasSupplied, err // we don't deduct gas since we don't want to charge the owner
	}

	if mdbxMigrateDebug {
		logMdbxMigrateDebug(
			"arbos owner precompile: burner stats",
			"caller", caller,
			"precompile", precompileAddress,
			"burned", burner.Burned(),
			"gas_left", burner.gasLeft,
		)
	}

	version := arbosState.ArbOSVersion(evm.StateDB)
	if !readOnly || version < params.ArbosVersion_11 {
		// log that the owner operation succeeded
		if err := wrapper.emitSuccess(evm, *(*[4]byte)(input[:4]), caller, input); err != nil {
			log.Error("failed to emit OwnerActs event", "err", err)
		}
	}

	return output, gasSupplied, err // we don't deduct gas since we don't want to charge the owner
}

func (wrapper *OwnerPrecompile) Precompile() *Precompile {
	con := wrapper.precompile
	return con.Precompile()
}
