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
