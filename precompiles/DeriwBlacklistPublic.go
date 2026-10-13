package precompiles

import (
	"github.com/ethereum/go-ethereum/common"
)

// DeriwBlacklistPublic precompile provides  info about the current blacklist info.
// The calls to this precompile do not require the sender have a permission.
type DeriwBlacklistPublic struct {
	Address                        addr // 0x7E7 2023
	BlacklistOwnerRectified        func(ctx, mech, addr) error
	BlacklistOwnerRectifiedGasCost func(addr) (uint64, error)
}

// GetAllChainOwners retrieves the list of blacklist owners
func (con DeriwBlacklistPublic) GetAllBlacklistOwners(c ctx, evm mech) ([]common.Address, error) {
	return c.State.Blacklist().BlacklistOwner().AllMembers(65536)
}

// RectifyChainOwner checks if the account is a blacklist owner
func (con DeriwBlacklistPublic) RectifyBlacklistOwner(c ctx, evm mech, addr addr) error {
	err := c.State.Blacklist().BlacklistOwner().RectifyMapping(addr)
	if err != nil {
		return err
	}
	return con.BlacklistOwnerRectified(c, evm, addr)
}

// IsChainOwner checks if the user is a blacklist owner
func (con DeriwBlacklistPublic) IsBlacklistOwner(c ctx, evm mech, addr addr) (bool, error) {
	return c.State.Blacklist().BlacklistOwner().IsMember(addr)
}

func (con DeriwBlacklistPublic) GetBlacklistTxFrom(c ctx, evm mech) ([]common.Address, error) {
	return c.State.Blacklist().TxFromAddrs().AllMembers(65536)
}

func (con DeriwBlacklistPublic) GetBlacklistTxTo(c ctx, evm mech) ([]common.Address, error) {
	return c.State.Blacklist().TxToAddrs().AllMembers(65536)
}

func (con DeriwBlacklistPublic) IsBlacklistTxFrom(c ctx, evm mech, addr common.Address) (bool, error) {
	return c.State.Blacklist().TxFromAddrs().IsMember(addr)
}

func (con DeriwBlacklistPublic) IsBlacklistTxTo(c ctx, evm mech, addr common.Address) (bool, error) {
	return c.State.Blacklist().TxToAddrs().IsMember(addr)
}

// GetDeriwOSVersion returns the active ArbOS and DeriwOS versions as a pair.
func (con DeriwBlacklistPublic) GetDeriwOSVersion(c ctx, evm mech) (uint64, uint64, error) {
	return c.State.ArbOSVersion(), c.State.DeriwOSVersion(), nil
}

// GetScheduledDeriwOSUpgrade returns the target DeriwOS version, timestamp,
// and ArbOS version recorded when the upgrade was scheduled.
func (con DeriwBlacklistPublic) GetScheduledDeriwOSUpgrade(c ctx, evm mech) (uint64, uint64, uint64, error) {
	version, timestamp, arbosVersion, err := c.State.GetScheduledDeriwOSUpgrade()
	if err != nil {
		return 0, 0, 0, err
	}
	if c.State.DeriwOSVersion() >= version {
		return 0, 0, 0, nil
	}
	return version, timestamp, arbosVersion, nil
}

// GetBlacklistTxFromWithFlag lists addresses with exactly the requested flag.
func (con DeriwBlacklistPublic) GetBlacklistTxFromWithFlag(c ctx, evm mech, flag uint64) ([]common.Address, error) {
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
func (con DeriwBlacklistPublic) IsBlacklistTxFromWithFlag(c ctx, evm mech, addr common.Address, flag uint64) (bool, error) {
	if err := requireBlacklistBanTypes(c); err != nil {
		return false, err
	}
	addresses, err := c.State.Blacklist().TxFromAddrsWithFlag(flag)
	if err != nil {
		return false, err
	}
	return addresses.IsMember(addr)
}

// GetBlacklistTxToWithFlag lists addresses with exactly the requested flag.
func (con DeriwBlacklistPublic) GetBlacklistTxToWithFlag(c ctx, evm mech, flag uint64) ([]common.Address, error) {
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
func (con DeriwBlacklistPublic) IsBlacklistTxToWithFlag(c ctx, evm mech, addr common.Address, flag uint64) (bool, error) {
	if err := requireBlacklistBanTypes(c); err != nil {
		return false, err
	}
	addresses, err := c.State.Blacklist().TxToAddrsWithFlag(flag)
	if err != nil {
		return false, err
	}
	return addresses.IsMember(addr)
}

// GetBlacklistBanFlag returns 0 (unlisted), 1 (all), or 2 (ERC20/USDT transfer).
func (con DeriwBlacklistPublic) GetBlacklistBanFlag(c ctx, evm mech, addr common.Address) (uint64, error) {
	if err := requireBlacklistBanTypes(c); err != nil {
		return 0, err
	}
	return c.State.Blacklist().BanType(addr)
}
