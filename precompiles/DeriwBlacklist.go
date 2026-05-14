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
type DeriwBlacklist struct {
	Address          addr // 0x7E8 2024
	OwnerActs        func(ctx, mech, bytes4, addr, []byte) error
	OwnerActsGasCost func(bytes4, addr, []byte) (uint64, error)
}

// AddBlacklistOwner adds account as a chain owner
func (con DeriwBlacklist) AddBlacklistOwner(c ctx, evm mech, newOwner addr) error {
	return c.State.Blacklist().BlacklistOwner().Add(newOwner)
}

// RemoveBlacklistOwner removes account from the list of chain owners
func (con DeriwBlacklist) RemoveBlacklistOwner(c ctx, evm mech, addr addr) error {
	member, _ := con.IsBlacklistOwner(c, evm, addr)
	if !member {
		return errors.New("tried to remove non-owner")
	}
	return c.State.Blacklist().BlacklistOwner().Remove(addr, c.State.ArbOSVersion())
}

// IsBlacklistOwner checks if the account is a chain owner
func (con DeriwBlacklist) IsBlacklistOwner(c ctx, evm mech, addr addr) (bool, error) {
	return c.State.Blacklist().BlacklistOwner().IsMember(addr)
}

// GetAllBlacklistOwners retrieves the list of chain owners
func (con DeriwBlacklist) GetAllBlacklistOwners(c ctx, evm mech) ([]common.Address, error) {
	return c.State.Blacklist().BlacklistOwner().AllMembers(65536)
}

func (con DeriwBlacklist) GetBlacklistTxFrom(c ctx, evm mech) ([]common.Address, error) {
	return c.State.Blacklist().TxFromAddrs().AllMembers(65536)
}

func (con DeriwBlacklist) GetBlacklistTxTo(c ctx, evm mech) ([]common.Address, error) {
	return c.State.Blacklist().TxToAddrs().AllMembers(65536)
}

func (con DeriwBlacklist) AddBlacklistTxFrom(c ctx, evm mech, addr common.Address) error {
	return c.State.Blacklist().TxFromAddrs().Add(addr)
}

func (con DeriwBlacklist) AddBlacklistTxTo(c ctx, evm mech, addr common.Address) error {
	return c.State.Blacklist().TxToAddrs().Add(addr)
}

func (con DeriwBlacklist) IsBlacklistTxFrom(c ctx, evm mech, addr common.Address) (bool, error) {
	return c.State.Blacklist().TxFromAddrs().IsMember(addr)
}

func (con DeriwBlacklist) IsBlacklistTxTo(c ctx, evm mech, addr common.Address) (bool, error) {
	return c.State.Blacklist().TxToAddrs().IsMember(addr)
}

func (con DeriwBlacklist) RemoveBlacklistTxFrom(c ctx, evm mech, addr common.Address) error {
	member, _ := con.IsBlacklistTxFrom(c, evm, addr)
	if !member {
		return errors.New("tried to remove non-tx-from")
	}
	return c.State.Blacklist().TxFromAddrs().Remove(addr, c.State.ArbOSVersion())
}

func (con DeriwBlacklist) RemoveBlacklistTxTo(c ctx, evm mech, addr common.Address) error {
	member, _ := con.IsBlacklistTxTo(c, evm, addr)
	if !member {
		return errors.New("tried to remove non-tx-to")
	}
	return c.State.Blacklist().TxToAddrs().Remove(addr, c.State.ArbOSVersion())
}
