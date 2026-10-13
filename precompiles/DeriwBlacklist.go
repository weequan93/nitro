// Copyright 2021-2022, Offchain Labs, Inc.
// For license information, see https://github.com/nitro/blob/master/LICENSE

package precompiles

import (
	"errors"

	"github.com/ethereum/go-ethereum/common"

	"github.com/offchainlabs/nitro/arbos/arbosState"
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
	if c.State.DeriwOSVersion() >= arbosState.DeriwOSVersion_BlacklistBanTypes {
		if err := c.State.Blacklist().SetBanType(addr, blacklist.BanFlagAll); err != nil {
			return err
		}
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
	if c.State.DeriwOSVersion() >= arbosState.DeriwOSVersion_BlacklistBanTypes {
		if err := c.State.Blacklist().SetBanType(addr, blacklist.BanFlagAll); err != nil {
			return err
		}
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
	if c.State.DeriwOSVersion() >= arbosState.DeriwOSVersion_BlacklistBanTypes {
		return c.State.Blacklist().ClearBanTypeIfUnlisted(addr)
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
	if c.State.DeriwOSVersion() >= arbosState.DeriwOSVersion_BlacklistBanTypes {
		return c.State.Blacklist().ClearBanTypeIfUnlisted(addr)
	}
	return nil
}

// ScheduleDeriwOSUpgrade is retained only so historical DeriwOS 1-3
// transactions replay identically. DeriwOS 4 and all future versions must be
// scheduled through the chain-owner-only ArbOwner precompile.
func (con DeriwBlacklist) ScheduleDeriwOSUpgrade(c ctx, evm mech, newVersion uint64, timestamp uint64) error {
	if newVersion >= arbosState.DeriwOSVersion_ChainOwnerUpgradeScheduling {
		return errors.New("DeriwOS 4 and later upgrades must be scheduled through ArbOwner")
	}
	return c.State.ScheduleDeriwOSUpgrade(newVersion, timestamp)
}

// GetBlacklistTxFromWithFlag lists addresses with exactly the requested flag.
func (con DeriwBlacklist) GetBlacklistTxFromWithFlag(c ctx, evm mech, flag uint64) ([]common.Address, error) {
	if err := requireBlacklistBanTypes(c); err != nil {
		return nil, err
	}
	addresses, err := c.State.Blacklist().TxFromAddrsWithFlag(flag)
	if err != nil {
		return nil, err
	}
	return addresses.AllMembers(65536)
}

// IsBlacklistTxFromWithFlag checks the address for the requested ban type.
func (con DeriwBlacklist) IsBlacklistTxFromWithFlag(c ctx, evm mech, addr common.Address, flag uint64) (bool, error) {
	if err := requireBlacklistBanTypes(c); err != nil {
		return false, err
	}
	addresses, err := c.State.Blacklist().TxFromAddrsWithFlag(flag)
	if err != nil {
		return false, err
	}
	return addresses.IsMember(addr)
}

// AddBlacklistTxFromWithFlag sets the address's ban type, replacing its previous type in both lists.
func (con DeriwBlacklist) AddBlacklistTxFromWithFlag(c ctx, evm mech, addr common.Address, flag uint64) error {
	if err := requireBlacklistBanTypes(c); err != nil {
		return err
	}
	if flag == blacklist.BanFlagAll {
		return con.AddBlacklistTxFrom(c, evm, addr)
	}
	addresses, err := c.State.Blacklist().TxFromAddrsWithFlag(flag)
	if err != nil {
		return err
	}
	return addresses.Add(addr)
}

// RemoveBlacklistTxFromWithFlag removes the address from this direction for the requested ban type.
func (con DeriwBlacklist) RemoveBlacklistTxFromWithFlag(c ctx, evm mech, addr common.Address, flag uint64) error {
	if err := requireBlacklistBanTypes(c); err != nil {
		return err
	}
	addresses, err := c.State.Blacklist().TxFromAddrsWithFlag(flag)
	if err != nil {
		return err
	}
	return addresses.Remove(addr, c.State.ArbOSVersion())
}

// GetBlacklistTxToWithFlag lists addresses with exactly the requested flag.
func (con DeriwBlacklist) GetBlacklistTxToWithFlag(c ctx, evm mech, flag uint64) ([]common.Address, error) {
	if err := requireBlacklistBanTypes(c); err != nil {
		return nil, err
	}
	addresses, err := c.State.Blacklist().TxToAddrsWithFlag(flag)
	if err != nil {
		return nil, err
	}
	return addresses.AllMembers(65536)
}

// IsBlacklistTxToWithFlag checks the address for the requested ban type.
func (con DeriwBlacklist) IsBlacklistTxToWithFlag(c ctx, evm mech, addr common.Address, flag uint64) (bool, error) {
	if err := requireBlacklistBanTypes(c); err != nil {
		return false, err
	}
	addresses, err := c.State.Blacklist().TxToAddrsWithFlag(flag)
	if err != nil {
		return false, err
	}
	return addresses.IsMember(addr)
}

// AddBlacklistTxToWithFlag sets the address's ban type, replacing its previous type in both lists.
func (con DeriwBlacklist) AddBlacklistTxToWithFlag(c ctx, evm mech, addr common.Address, flag uint64) error {
	if err := requireBlacklistBanTypes(c); err != nil {
		return err
	}
	if flag == blacklist.BanFlagAll {
		return con.AddBlacklistTxTo(c, evm, addr)
	}
	addresses, err := c.State.Blacklist().TxToAddrsWithFlag(flag)
	if err != nil {
		return err
	}
	return addresses.Add(addr)
}

// RemoveBlacklistTxToWithFlag removes the address from this direction for the requested ban type.
func (con DeriwBlacklist) RemoveBlacklistTxToWithFlag(c ctx, evm mech, addr common.Address, flag uint64) error {
	if err := requireBlacklistBanTypes(c); err != nil {
		return err
	}
	addresses, err := c.State.Blacklist().TxToAddrsWithFlag(flag)
	if err != nil {
		return err
	}
	return addresses.Remove(addr, c.State.ArbOSVersion())
}

func requireBlacklistBanTypes(c ctx) error {
	if c.State.DeriwOSVersion() < arbosState.DeriwOSVersion_BlacklistBanTypes {
		return errors.New("blacklist ban types require DeriwOS 6")
	}
	return nil
}

// GetBlacklistBanFlag returns 0 (unlisted), 1 (all), or 2 (ERC20/USDT transfer).
func (con DeriwBlacklist) GetBlacklistBanFlag(c ctx, evm mech, addr common.Address) (uint64, error) {
	if err := requireBlacklistBanTypes(c); err != nil {
		return 0, err
	}
	return c.State.Blacklist().BanType(addr)
}
