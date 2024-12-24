// Copyright 2021-2022, Offchain Labs, Inc.
// For license information, see https://github.com/nitro/blob/master/LICENSE

package precompiles

import (
	"errors"

	"github.com/ethereum/go-ethereum/common"
)

// ArbOwner precompile provides owners with tools for managing the rollup.
// All calls to this precompile are authorized by the OwnerPrecompile wrapper,
// which ensures only a chain owner can access these methods. For methods that
// are safe for non-owners to call, see ArbOwnerOld
type DeriwGasless struct {
	Address          addr // 0x7E8 2024
	OwnerActs        func(ctx, mech, bytes4, addr, []byte) error
	OwnerActsGasCost func(bytes4, addr, []byte) (uint64, error)
}

// AddChainOwner adds account as a chain owner
func (con DeriwGasless) AddGaslessOwner(c ctx, evm mech, newOwner addr) error {
	return c.State.GaslessOwners().Add(newOwner)
}

// RemoveGaslessOwner removes account from the list of chain owners
func (con DeriwGasless) RemoveGaslessOwner(c ctx, evm mech, addr addr) error {
	member, _ := con.IsGaslessOwner(c, evm, addr)
	if !member {
		return errors.New("tried to remove non-owner")
	}
	return c.State.GaslessOwners().Remove(addr, c.State.ArbOSVersion())
}

// IsGaslessOwner checks if the account is a chain owner
func (con DeriwGasless) IsGaslessOwner(c ctx, evm mech, addr addr) (bool, error) {
	return c.State.GaslessOwners().IsMember(addr)
}

// GetAllGaslessOwners retrieves the list of chain owners
func (con DeriwGasless) GetAllGaslessOwners(c ctx, evm mech) ([]common.Address, error) {
	return c.State.GaslessOwners().AllMembers(65536)
}

func (con DeriwGasless) GetPricerTxFromAddrs(c ctx, evm mech) ([]common.Address, error) {
	return c.State.Pricer().TxFromAddrs().AllMembers(65536)
}

func (con DeriwGasless) GetPricerTxToAddrs(c ctx, evm mech) ([]common.Address, error) {
	return c.State.Pricer().TxToAddrs().AllMembers(65536)
}

func (con DeriwGasless) AddPricerTxFrom(c ctx, evm mech, addr common.Address) error {
	return c.State.Pricer().TxFromAddrs().Add(addr)
}

func (con DeriwGasless) AddPricerTxTo(c ctx, evm mech, addr common.Address) error {
	return c.State.Pricer().TxToAddrs().Add(addr)
}

func (con DeriwGasless) IsPricerTxFrom(c ctx, evm mech, addr common.Address) (bool, error) {
	return c.State.Pricer().TxFromAddrs().IsMember(addr)
}

func (con DeriwGasless) IsPricerTxTo(c ctx, evm mech, addr common.Address) (bool, error) {
	return c.State.Pricer().TxToAddrs().IsMember(addr)
}

func (con DeriwGasless) RemovePricerTxFrom(c ctx, evm mech, addr common.Address) error {
	member, _ := con.IsPricerTxFrom(c, evm, addr)
	if !member {
		return errors.New("tried to remove non-tx-from")
	}
	return c.State.Pricer().TxFromAddrs().Remove(addr, c.State.ArbOSVersion())
}

func (con DeriwGasless) RemovePricerTxTo(c ctx, evm mech, addr common.Address) error {
	member, _ := con.IsPricerTxTo(c, evm, addr)
	if !member {
		return errors.New("tried to remove non-tx-to")
	}
	return c.State.Pricer().TxToAddrs().Remove(addr, c.State.ArbOSVersion())
}
