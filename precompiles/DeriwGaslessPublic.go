package precompiles

import (
	"github.com/ethereum/go-ethereum/common"
)

// DeriwGaslessPublic precompile provides  info about the current gasless info.
// The calls to this precompile do not require the sender have a permission.
type DeriwGaslessPublic struct {
	Address                      addr // 0x7E7 2023
	GaslessOwnerRectified        func(ctx, mech, addr) error
	GaslessOwnerRectifiedGasCost func(addr) (uint64, error)
}

// GetAllChainOwners retrieves the list of gasless owners
func (con DeriwGaslessPublic) GetAllGaslessOwners(c ctx, evm mech) ([]common.Address, error) {
	return c.State.GaslessOwners().AllMembers(65536)
}

// RectifyChainOwner checks if the account is a gasless owner
func (con DeriwGaslessPublic) RectifyGaslessOwner(c ctx, evm mech, addr addr) error {
	err := c.State.GaslessOwners().RectifyMapping(addr)
	if err != nil {
		return err
	}
	return con.GaslessOwnerRectified(c, evm, addr)
}

// IsChainOwner checks if the user is a gasless owner
func (con DeriwGaslessPublic) IsGaslessOwner(c ctx, evm mech, addr addr) (bool, error) {
	return c.State.GaslessOwners().IsMember(addr)
}

func (con DeriwGaslessPublic) GetPricerTxFromAddrs(c ctx, evm mech) ([]common.Address, error) {
	return c.State.Pricer().TxFromAddrs().AllMembers(65536)
}

func (con DeriwGaslessPublic) GetPricerTxToAddrs(c ctx, evm mech) ([]common.Address, error) {
	return c.State.Pricer().TxToAddrs().AllMembers(65536)
}

func (con DeriwGaslessPublic) IsPricerTxFrom(c ctx, evm mech, addr common.Address) (bool, error) {
	return c.State.Pricer().TxFromAddrs().IsMember(addr)
}

func (con DeriwGaslessPublic) IsPricerTxTo(c ctx, evm mech, addr common.Address) (bool, error) {
	return c.State.Pricer().TxToAddrs().IsMember(addr)
}
