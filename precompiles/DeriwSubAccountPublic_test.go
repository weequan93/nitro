// Copyright 2021-2022, Offchain Labs, Inc.
// For license information, see https://github.com/nitro/blob/master/LICENSE

package precompiles

import (
	"crypto/ecdsa"
	"encoding/json"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethmath "github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
)

func TestDeriwSubAccountPublic(t *testing.T) {
	evm := newMockEVMForTesting()
	caller := common.HexToAddress("0x8f48163d1932dc2286cc7d1f260e09c6ed07a1e0")
	privKey, err := crypto.HexToECDSA(deriwSubAccountTestKey)
	Require(t, err)
	parent := crypto.PubkeyToAddress(privKey.PublicKey)

	prec := &DeriwSubAccountPublic{}
	// gasInfo := &ArbGasInfo{}
	callCtx := testContext(caller, evm)

	signData, signature := signDeriwSubAccount(t, evm, privKey, "Grant", &caller)
	Require(t, prec.GrantAccountControl(callCtx, evm, signData, signature))

	parentAddress, err := prec.ReadAccountControl(callCtx, evm, caller)
	Require(t, err)
	if parentAddress.Cmp(parent) != 0 {
		Fail(t)
	}

	signData, signature = signDeriwSubAccount(t, evm, privKey, "Revoke", nil)
	Require(t, prec.RevokeAccountControl(callCtx, evm, signData, signature))

	parentAddress, err = prec.ReadAccountControl(callCtx, evm, caller)
	Require(t, err)
	if parentAddress.Cmp(common.Address{}) != 0 {
		Fail(t)
	}

}

const deriwSubAccountTestKey = "3f924b934c41a048183b48835acdb533b1d07045a38394b006b238a3fc07ea89"

func signDeriwSubAccount(t *testing.T, evm mech, privKey *ecdsa.PrivateKey, operation string, child *common.Address) ([]byte, []byte) {
	t.Helper()
	typedData := deriwSubAccountTypedData(evm, operation, child)
	signData, err := json.Marshal(typedData)
	Require(t, err)

	sighash, _, err := apitypes.TypedDataAndHash(typedData)
	Require(t, err)

	signature, err := crypto.Sign(sighash, privKey)
	Require(t, err)
	signature[64] += 27
	return signData, signature
}

func deriwSubAccountTypedData(evm mech, operation string, child *common.Address) apitypes.TypedData {
	messageTypes := []apitypes.Type{
		{Name: "Timestamp", Type: "string"},
		{Name: "Operation", Type: "string"},
	}
	message := apitypes.TypedDataMessage{
		"Timestamp": "0",
		"Operation": operation,
	}
	if child != nil {
		messageTypes = append(messageTypes, apitypes.Type{Name: "Child", Type: "address"})
		message["Child"] = child.Hex()
	}

	chainID := evm.ChainConfig().ChainID
	return apitypes.TypedData{
		Types: apitypes.Types{
			"EIP712Domain": {
				{Name: "name", Type: "string"},
				{Name: "version", Type: "string"},
				{Name: "chainId", Type: "uint256"},
				{Name: "verifyingContract", Type: "address"},
			},
			"Message": messageTypes,
		},
		PrimaryType: "Message",
		Domain: apitypes.TypedDataDomain{
			Name:              "DeriwSubAccountSignature",
			Version:           "1",
			ChainId:           ethmath.NewHexOrDecimal256(chainID.Int64()),
			VerifyingContract: common.HexToAddress("0x00000000000000000000000000000000000007E9").Hex(),
		},
		Message: message,
	}
}
