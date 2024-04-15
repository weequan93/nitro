// Copyright 2021-2022, Offchain Labs, Inc.
// For license information, see https://github.com/nitro/blob/master/LICENSE

package precompiles

import (
	"github.com/ethereum/go-ethereum/common"
)

// DeriwPricerPublic precompile provides non-permission with info about the current deriw guard pricer config.
// The calls to this precompile do not require the sender be a chain owner.
// For those that are, see DeriwGuard
type DeriwGuardPublic struct {
	Address addr // 3E8, 100
}

func (con DeriwGuardPublic) GetPricerTxFromAddrs(c ctx, evm mech) ([]common.Address, error) {
	return c.State.Pricer().TxFromAddrs().AllMembers(65536)
}

func (con DeriwGuardPublic) GetPricerTxToAddrs(c ctx, evm mech) ([]common.Address, error) {
	return c.State.Pricer().TxToAddrs().AllMembers(65536)
}

func (con DeriwGuardPublic) IsPricerTxFrom(c ctx, evm mech, addr common.Address) (bool, error) {
	return c.State.Pricer().TxFromAddrs().IsMember(addr)
}

func (con DeriwGuardPublic) IsPricerTxTo(c ctx, evm mech, addr common.Address) (bool, error) {
	return c.State.Pricer().TxToAddrs().IsMember(addr)
}
