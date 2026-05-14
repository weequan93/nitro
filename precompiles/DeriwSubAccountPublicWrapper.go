// Copyright 2021-2023, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE

package precompiles

import (
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/arbitrum/multigas"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
)

// OwnerPrecompile is a precompile wrapper for those only chain owners may use
type DeriwSubAccountPublicPrecompile struct {
	precompile ArbosPrecompile
}

func (wrapper *DeriwSubAccountPublicPrecompile) Address() common.Address {
	return wrapper.precompile.Address()
}

func (wrapper *DeriwSubAccountPublicPrecompile) Call(
	input []byte,
	actingAsAddress common.Address,
	caller common.Address,
	value *big.Int,
	readOnly bool,
	gasSupplied uint64,
	evm *vm.EVM,
) ([]byte, uint64, multigas.MultiGas, error) {
	burner := &Context{
		gasSupplied: gasSupplied,
		gasUsed:     multigas.ZeroGas(),
	}

	return nil, burner.GasLeft(), burner.gasUsed, errors.New("unauthorized caller to access-controlled method")
}

func (wrapper *DeriwSubAccountPublicPrecompile) Precompile() *Precompile {
	con := wrapper.precompile
	return con.Precompile()
}

func (wrapper *DeriwSubAccountPublicPrecompile) Name() string {
	return wrapper.precompile.Name()
}
