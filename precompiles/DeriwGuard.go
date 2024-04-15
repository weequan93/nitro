// Copyright 2021-2022, Offchain Labs, Inc.
// For license information, see https://github.com/nitro/blob/master/LICENSE

package precompiles

import (
	"errors"

	"github.com/ethereum/go-ethereum/common"
)

// DeriwGuard precompile provides owners with tools for managing the pricer permission.
// All calls to this precompile are authorized by the respectiveliy deriw guard xx permission,
type DeriwGuard struct {
	Address          addr // 0x3E9
	OwnerActs        func(ctx, mech, bytes4, addr, []byte) error
	OwnerActsGasCost func(bytes4, addr, []byte) (uint64, error)
}

func (con DeriwGuard) GetPricerTxFromAddrs(c ctx, evm mech) ([]common.Address, error) {
	return c.State.Pricer().TxFromAddrs().AllMembers(65536)
}

func (con DeriwGuard) GetPricerTxToAddrs(c ctx, evm mech) ([]common.Address, error) {
	return c.State.Pricer().TxToAddrs().AllMembers(65536)
}

func (con DeriwGuard) AddPricerTxFrom(c ctx, evm mech, addr common.Address) error {
	return c.State.Pricer().TxFromAddrs().Add(addr)
}

func (con DeriwGuard) AddPricerTxTo(c ctx, evm mech, addr common.Address) error {
	return c.State.Pricer().TxToAddrs().Add(addr)
}

func (con DeriwGuard) IsPricerTxFrom(c ctx, evm mech, addr common.Address) (bool, error) {
	return c.State.Pricer().TxFromAddrs().IsMember(addr)
}

func (con DeriwGuard) IsPricerTxTo(c ctx, evm mech, addr common.Address) (bool, error) {
	return c.State.Pricer().TxToAddrs().IsMember(addr)
}

func (con DeriwGuard) RemovePricerTxFrom(c ctx, evm mech, addr common.Address) error {
	member, _ := con.IsPricerTxFrom(c, evm, addr)
	if !member {
		return errors.New("tried to remove non-tx-from")
	}
	return c.State.Pricer().TxFromAddrs().Remove(addr, c.State.ArbOSVersion())
}

func (con DeriwGuard) RemovePricerTxTo(c ctx, evm mech, addr common.Address) error {
	member, _ := con.IsPricerTxTo(c, evm, addr)
	if !member {
		return errors.New("tried to remove non-tx-to")
	}
	return c.State.Pricer().TxToAddrs().Remove(addr, c.State.ArbOSVersion())
}
