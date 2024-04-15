package deriwguard

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/offchainlabs/nitro/arbos/addressSet"
	"github.com/offchainlabs/nitro/arbos/storage"
)

type DeriwGuard struct {
	storage     *storage.Storage
	pricerAddrs *addressSet.AddressSet
}

type SubspaceID []byte

var (
	pricerAddrsSubspace SubspaceID = []byte{0}
)

func InitializeDeriwGuard(sto *storage.Storage) error {
	return addressSet.Initialize(sto.OpenSubStorage(pricerAddrsSubspace))
}

func OpenDeriwGuard(sto *storage.Storage) *DeriwGuard {
	return &DeriwGuard{
		sto,
		addressSet.OpenAddressSet(sto.OpenSubStorage(pricerAddrsSubspace)),
	}
}

func (deriwGaurd *DeriwGuard) PricerAddrs() *addressSet.AddressSet {
	return deriwGaurd.pricerAddrs
}

func IsPricerAddrs(deriwGaurd *DeriwGuard, addr *common.Address) bool {
	if addr == nil && deriwGaurd == nil {
		return false
	}
	ok, _ := deriwGaurd.PricerAddrs().IsMember(*addr)
	return ok
}
