// Copyright 2021-2022, Offchain Labs, Inc.
// For license information, see https://github.com/nitro/blob/master/LICENSE

package arbutil

import (
	"bytes"
	"encoding/hex"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/offchainlabs/nitro/arbos/pricer"
)

var (
	// web3.eth.abi.encodeFunctionSignature('handleOp((address,bytes,uint256,uint256,uint256,uint256,bytes))'
	ENTRYPOINT_HANDLE_OP_SIG, _ = hex.DecodeString("b4e9984f")
	ENTRYPOINT_CONTRACT         = common.HexToAddress("0xF91919279E393256Fa764739d6974045c19a4E01")
)

var GASLESS_CONTRACT = map[string]bool{
	"0xFCcF5bd58d78ABb2629bD96cb81C150c7121eDD6": true, // weth
	"0x60c03C6cA6eB207BD2Cb9d8499C4fE95Ad29D4E1": true, // usdt
	"0x71DB70d3e8012dab7326220cEa80434aBEcA0467": true, // uni
	"0xF91919279E393256Fa764739d6974045c19a4E01": true, // link
	"0xdBE75CB0f179169638Db1046D81eED9F6B68d5d4": true, // wbtc
	"0xA02f9a5ec6454A23023a2ff7ddbF9467c7E2b961": true, // OrderBook
	"0xaBd3751871c4194Db179F9000553033b521E2Fa5": true, // PositionRouter
	"0x666Fcd66349B97BDceDE8afd2EaE21cfeC4a052f": true, // pweth
	"0xDEe1fEE90894b068a62B8334e9648c9f5a8799F9": true, // pusdt
	"0x3Ea799F80199F0ABDA236239a96358016881fD67": true, // puni
	"0x14931A629399D71111E3Db03260aF4d75bb660d7": true, // plink
	"0x9EDF968C32E8a54aB2bb082fda94fDC23eBe95f8": true, // pwbtc
	"0xe04693604079aCf2e226a331Ea2bD268030978B3": true, // FastPriceFeed
}

func IsGaslessTx(tx *types.Transaction) bool {
	return tx != nil && tx.To() != nil && *tx.To() == ENTRYPOINT_CONTRACT &&
		tx.Data() != nil && len(tx.Data()) > 4 && bytes.Equal(tx.Data()[:4], ENTRYPOINT_HANDLE_OP_SIG)
}

func IsCustomPriceTx(tx *types.Transaction) bool {
	if tx != nil && tx.To() != nil {
		IsGaslessContract, err := GASLESS_CONTRACT[tx.To().String()]
		if err != true {
			return false
		}
		return IsGaslessContract
	}
	return false
}

func IsCustomPriceAddr(addr *common.Address) bool {
	IsGaslessContract, err := GASLESS_CONTRACT[addr.String()]
	if err != true {
		return false
	}
	return IsGaslessContract
}

func IsCustomPriceTxCheck(pricer *pricer.Pricer, tx *types.Transaction) bool {
	if tx != nil && tx.To() != nil {

		addr := common.HexToAddress(tx.To().String())
		IsGaslessContract, err := pricer.TxToAddrs().IsMember(addr)
		if err != nil {
			return false
		}
		return IsGaslessContract
	}
	return false
}

func IsCustomPriceTxCheckAddr(pricer *pricer.Pricer, addr *common.Address) bool {
	if addr == nil {
		return false
	}
	IsGaslessContract, err := pricer.TxToAddrs().IsMember(*addr)
	if err != nil {
		return false
	}
	return IsGaslessContract

}
