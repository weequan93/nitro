package arbutil

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ethereum/go-ethereum/common"
	ethmath "github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
)

const deriwSubAccountTestKey = "3f924b934c41a048183b48835acdb533b1d07045a38394b006b238a3fc07ea89"

func TestParseTypeDataNSignature(t *testing.T) {
	privKey, err := crypto.HexToECDSA(deriwSubAccountTestKey)
	require.NoError(t, err)

	typedData := deriwSubAccountTypedData(common.HexToAddress("0x00000000000000000000000000000000000007E9"))
	signData, err := json.Marshal(typedData)
	require.NoError(t, err)

	sighash, _, err := apitypes.TypedDataAndHash(typedData)
	require.NoError(t, err)

	signature, err := crypto.Sign(sighash, privKey)
	require.NoError(t, err)
	signature[64] += 27

	_, _, validSignature, err := ParseTypeDataNSignature(signData, signature)
	require.NoError(t, err)
	require.True(t, validSignature)
}

func deriwSubAccountTypedData(verifyingContract common.Address) apitypes.TypedData {
	messageTypes := []apitypes.Type{
		{Name: "Timestamp", Type: "string"},
		{Name: "Operation", Type: "string"},
		{Name: "Child", Type: "address"},
	}
	message := apitypes.TypedDataMessage{
		"Timestamp": "0",
		"Operation": "Grant",
		"Child":     common.HexToAddress("0x8f48163d1932dc2286cc7d1f260e09c6ed07a1e0").Hex(),
	}

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
			ChainId:           ethmath.NewHexOrDecimal256(412346),
			VerifyingContract: verifyingContract.Hex(),
		},
		Message: message,
	}
}
