package arbos

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/offchainlabs/nitro/arbos/arbosState"
	"github.com/offchainlabs/nitro/arbos/burn"
)

func TestArbPricerTxFrom(t *testing.T) {
	version := uint64(10)
	evm := newMockEVMForTesting()
	burner := burn.NewSystemBurner(nil, false)
	arbosSt, err := arbosState.OpenArbosState(evm.StateDB, burner)
	Require(t, err)
	newAddr := common.BytesToAddress(crypto.Keccak256([]byte{0})[:20])
	price := arbosSt.Pricer()

	Require(t, price.TxFromAddrs().Add(newAddr))
	member, err := price.TxFromAddrs().IsMember(newAddr)
	Require(t, err)
	if !member {
		Fail(t)
	}
	all, err := price.TxFromAddrs().AllMembers(65536)
	Require(t, err)
	if len(all) != 1 {
		Fail(t)
	}
	Require(t, price.TxFromAddrs().Remove(newAddr, version))
	member, err = price.TxFromAddrs().IsMember(newAddr)
	Require(t, err)
	if member {
		Fail(t)
	}
}

func TestArbPricerTxTo(t *testing.T) {
	version := uint64(10)
	arbosSt, _ := arbosState.NewArbosMemoryBackedArbOSState()
	price := arbosSt.Pricer()
	newAddr := common.BytesToAddress(crypto.Keccak256([]byte{0})[:20])

	Require(t, price.TxToAddrs().Add(newAddr))
	member, err := price.TxToAddrs().IsMember(newAddr)
	Require(t, err)
	if !member {
		Fail(t)
	}
	all, err := price.TxToAddrs().AllMembers(65536)
	Require(t, err)
	if len(all) != 1 {
		Fail(t)
	}
	Require(t, price.TxToAddrs().Remove(newAddr, version))
	member, err = price.TxToAddrs().IsMember(newAddr)
	Require(t, err)
	if member {
		Fail(t)
	}
}
