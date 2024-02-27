package pricer

import (
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/offchainlabs/nitro/arbos/addressSet"
	"github.com/offchainlabs/nitro/arbos/storage"
)

type Pricer struct {
	storage     *storage.Storage
	txFromAddrs *addressSet.AddressSet
	txToAddrs   *addressSet.AddressSet
}

type SubspaceID []byte

var (
	txFromAddrsSubspace SubspaceID = []byte{0}
	txToAddrsSubspace   SubspaceID = []byte{1}
)

func InitializePricer(sto *storage.Storage) error {
	_ = addressSet.Initialize(sto.OpenSubStorage(txFromAddrsSubspace))
	return addressSet.Initialize(sto.OpenSubStorage(txToAddrsSubspace))
}

func OpenPricer(sto *storage.Storage) *Pricer {
	return &Pricer{
		sto,
		addressSet.OpenAddressSet(sto.OpenSubStorage(txFromAddrsSubspace)),
		addressSet.OpenAddressSet(sto.OpenSubStorage(txToAddrsSubspace)),
	}
}

func (pricer *Pricer) TxFromAddrs() *addressSet.AddressSet {
	return pricer.txFromAddrs
}

func (pricer *Pricer) TxToAddrs() *addressSet.AddressSet {
	return pricer.txToAddrs
}

func IsCustomPriceTx(pricer *Pricer, tx *types.Transaction) bool {
	if tx == nil || tx.To() == nil || pricer == nil {
		return false
	}
	ok, _ := pricer.TxToAddrs().IsMember(*tx.To())
	return ok
}
