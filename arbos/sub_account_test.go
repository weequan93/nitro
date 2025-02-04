package arbos

//nolint
//
//import (
//	"github.com/ethereum/go-ethereum/common"
//	"github.com/ethereum/go-ethereum/core/types"
//	"github.com/ethereum/go-ethereum/crypto"
//	"github.com/offchainlabs/nitro/arbos/arbosState"
//	"github.com/offchainlabs/nitro/arbos/burn"
//	"github.com/offchainlabs/nitro/arbos/pricer"
//	"math/big"
//	"testing"
//)
//
//func TestGrantAccountControl(t *testing.T) {
//	version := uint64(10)
//	arbosSt, _ := arbosState.NewArbosMemoryBackedArbOSState()
//	price := arbosSt.Pricer()
//	newAddr := common.BytesToAddress(crypto.Keccak256([]byte{0})[:20])
//
//	Require(t, price.TxToAddrs().Add(newAddr))
//	member, err := price.TxToAddrs().IsMember(newAddr)
//	Require(t, err)
//	if !member {
//		Fail(t)
//	}
//	all, err := price.TxToAddrs().AllMembers(65536)
//	Require(t, err)
//	if len(all) != 1 {
//		Fail(t)
//	}
//	Require(t, price.TxToAddrs().Remove(newAddr, version))
//	member, err = price.TxToAddrs().IsMember(newAddr)
//	Require(t, err)
//	if member {
//		Fail(t)
//	}
//}
//
//func TestRevokeAccountControl(t *testing.T) {
//	addr := common.HexToAddress("0x60c03C6cA6eB207BD2Cb9d8499C4fE95Ad29D4E1")
//
//	inner := types.LegacyTx{
//		Nonce:    1,
//		GasPrice: big.NewInt(0),
//		Gas:      1000000,
//		To:       &addr,
//		Value:    big.NewInt(1),
//	}
//
//	tx := types.NewTx(&inner)
//
//	evm := newMockEVMForTesting()
//	burner := burn.NewSystemBurner(nil, false)
//	price := arbosState.OpenArbosPricer(evm.StateDB, burner, false)
//	err := price.TxToAddrs().Add(addr)
//	Require(t, err)
//
//	if !pricer.IsCustomPriceTx(price, tx) {
//		t.Fail()
//	}
//}
