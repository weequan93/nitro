// Copyright 2021-2022, Offchain Labs, Inc.
// For license information, see https://github.com/nitro/blob/master/LICENSE

package precompiles

import (
	"errors"

	"github.com/ethereum/go-ethereum/common"

	"github.com/offchainlabs/nitro/arbos/blacklist"
	"github.com/offchainlabs/nitro/arbos/l1pricing"
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
	if err := rejectProtectedBlacklistAddress(c, addr); err != nil {
		return err
	}
	if err := c.State.Blacklist().TxFromAddrs().Add(addr); err != nil {
		return err
	}
	return nil
}

func (con DeriwBlacklist) AddBlacklistTxTo(c ctx, evm mech, addr common.Address) error {
	if err := rejectProtectedBlacklistAddress(c, addr); err != nil {
		return err
	}
	if err := c.State.Blacklist().TxToAddrs().Add(addr); err != nil {
		return err
	}
	return nil
}

func rejectProtectedBlacklistAddress(c ctx, address common.Address) error {
	enforceProtection, err := c.State.DeriwConsensusBlacklistActiveOrScheduled()
	if err != nil {
		return err
	}
	if !enforceProtection {
		return nil
	}
	networkFeeAccount, err := c.State.NetworkFeeAccount()
	if err != nil {
		return err
	}
	infraFeeAccount, err := c.State.InfraFeeAccount()
	if err != nil {
		return err
	}
	if blacklist.IsProtectedSystemAddress(address, networkFeeAccount, infraFeeAccount, l1pricing.BatchPosterAddress) {
		return errors.New("cannot quarantine a protected system address")
	}
	return nil
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
	if err := c.State.Blacklist().TxFromAddrs().Remove(addr, c.State.ArbOSVersion()); err != nil {
		return err
	}
	return nil
}

func (con DeriwBlacklist) RemoveBlacklistTxTo(c ctx, evm mech, addr common.Address) error {
	member, _ := con.IsBlacklistTxTo(c, evm, addr)
	if !member {
		return errors.New("tried to remove non-tx-to")
	}
	if err := c.State.Blacklist().TxToAddrs().Remove(addr, c.State.ArbOSVersion()); err != nil {
		return err
	}
	return nil
}

// ScheduleDeriwOSUpgrade schedules independent Deriw consensus semantics and
// records the active ArbOS version alongside the schedule.
func (con DeriwBlacklist) ScheduleDeriwOSUpgrade(c ctx, evm mech, newVersion uint64, timestamp uint64) error {
	return c.State.ScheduleDeriwOSUpgrade(newVersion, timestamp)
}
