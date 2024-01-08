// Copyright 2021-2022, Offchain Labs, Inc.
// For license information, see https://github.com/nitro/blob/master/LICENSE

package arbutil

import (
	"bytes"
	"encoding/hex"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

var (
	// web3.eth.abi.encodeFunctionSignature('handleOp((address,bytes,uint256,uint256,uint256,uint256,bytes))'
	ENTRYPOINT_HANDLE_OP_SIG, _ = hex.DecodeString("b4e9984f")
	ENTRYPOINT_CONTRACT         = common.HexToAddress("0x60c03C6cA6eB207BD2Cb9d8499C4fE95Ad29D4E1")
	COUNTER_CONTRACT            = common.HexToAddress("0xd5E9937897BD904Eb7736d7049Fd474a1141B244")
)

func IsGaslessTx(tx *types.Transaction) bool {
	return tx != nil && tx.To() != nil && *tx.To() == ENTRYPOINT_CONTRACT &&
		tx.Data() != nil && len(tx.Data()) > 4 && bytes.Equal(tx.Data()[:4], ENTRYPOINT_HANDLE_OP_SIG)
}

func IsCustomPriceTx(tx *types.Transaction) bool {
	return tx != nil && tx.To() != nil && *tx.To() == COUNTER_CONTRACT
}
