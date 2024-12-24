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
type DeriwSubAccount struct {
	Address          addr // 0x7E8 2024
	OwnerActs        func(ctx, mech, bytes4, addr, []byte) error
	OwnerActsGasCost func(bytes4, addr, []byte) (uint64, error)
}

// AddChainOwner adds account as a chain owner
func (con DeriwSubAccount) AddSubAccountOwner(c ctx, evm mech, newOwner addr) error {
	return c.State.SubAccount().SubAccountOwner().Add(newOwner)
}

// RemoveGaslessOwner removes account from the list of chain owners
func (con DeriwSubAccount) RemoveSubAccountOwner(c ctx, evm mech, addr addr) error {
	member, _ := con.IsSubAccountOwner(c, evm, addr)
	if !member {
		return errors.New("tried to remove non-owner")
	}
	return c.State.SubAccount().SubAccountOwner().Remove(addr, c.State.ArbOSVersion())
}

// IsGaslessOwner checks if the account is a chain owner
func (con DeriwSubAccount) IsSubAccountOwner(c ctx, evm mech, addr addr) (bool, error) {
	return c.State.SubAccount().SubAccountOwner().IsMember(addr)
}

// AddChainOwner adds account as a chain owner
func (con DeriwSubAccount) AddAllowedAddress(c ctx, evm mech, newAddress addr) error {
	return c.State.SubAccount().AllowedAddress().Add(newAddress)
}

// RemoveGaslessOwner removes account from the list of chain owners
func (con DeriwSubAccount) RemoveAllowedAddress(c ctx, evm mech, addr addr) error {
	member, _ := con.IsSubAccountOwner(c, evm, addr)
	if !member {
		return errors.New("tried to remove non-allowed address")
	}
	return c.State.SubAccount().AllowedAddress().Remove(addr, c.State.ArbOSVersion())
}

// IsGaslessOwner checks if the account is a chain owner
func (con DeriwSubAccount) IsAllowedAddress(c ctx, evm mech, addr addr) (bool, error) {
	return c.State.SubAccount().AllowedAddress().IsMember(addr)
}

func (con DeriwSubAccount) GetAllAllowedOwner(c ctx, evm mech) ([]common.Address, error) {
	return c.State.SubAccount().AllowedAddress().AllMembers(65536)
}

func (con DeriwSubAccount) GetAllSubAccountOwner(c ctx, evm mech) ([]common.Address, error) {
	return c.State.SubAccount().SubAccountOwner().AllMembers(65536)
}
