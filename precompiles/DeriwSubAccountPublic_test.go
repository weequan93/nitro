// Copyright 2021-2022, Offchain Labs, Inc.
// For license information, see https://github.com/nitro/blob/master/LICENSE

package precompiles

import (
	"bytes"
	"encoding/json"
	"math/big"
	"strconv"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	commonmath "github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
	"github.com/stretchr/testify/require"
)

const deriwSubAccountTestTimestamp = uint64(1_800_000_000)

func TestDeriwSubAccountPublic(t *testing.T) {
	evm := newMockEVMForTesting()
	evm.Context.Time = deriwSubAccountTestTimestamp
	child := common.HexToAddress("0x8f48163d1932dc2286cc7d1f260e09c6ed07a1e0")
	prec := &DeriwSubAccountPublic{Address: types.DeriwSubAccountPublicAddress}
	callCtx := testContext(child, evm)

	grantData := marshalDeriwSubAccountTypedData(t, newDeriwSubAccountTypedData(
		evm.ChainConfig().ChainID,
		strconv.FormatUint(evm.Context.Time, 10),
		"Grant",
		&child,
	))
	grantSignature, parent := signDeriwSubAccountTypedData(t, grantData)
	require.NoError(t, prec.GrantAccountControl(callCtx, evm, grantData, grantSignature))

	parentAddress, err := prec.ReadAccountControl(callCtx, evm, child)
	require.NoError(t, err)
	require.Equal(t, parent, parentAddress)

	// The equivalent 0/1 recovery-byte representation is the same signed
	// authorization and must not bypass replay protection.
	alternateGrantSignature := bytes.Clone(grantSignature)
	alternateGrantSignature[crypto.RecoveryIDOffset] -= 27
	err = prec.GrantAccountControl(callCtx, evm, grantData, alternateGrantSignature)
	require.ErrorContains(t, err, "authorization already used")

	revokeData := marshalDeriwSubAccountTypedData(t, newDeriwSubAccountTypedData(
		evm.ChainConfig().ChainID,
		strconv.FormatUint(evm.Context.Time, 10),
		"Revoke",
		nil,
	))
	revokeSignature, _ := signDeriwSubAccountTypedData(t, revokeData)
	require.NoError(t, prec.RevokeAccountControl(callCtx, evm, revokeData, revokeSignature))

	parentAddress, err = prec.ReadAccountControl(callCtx, evm, child)
	require.NoError(t, err)
	require.Equal(t, common.Address{}, parentAddress)

	// A fresh timestamp creates a new grant authorization. Replaying the old,
	// already-consumed revoke must not revoke this later relationship.
	evm.Context.Time++
	secondGrantData := marshalDeriwSubAccountTypedData(t, newDeriwSubAccountTypedData(
		evm.ChainConfig().ChainID,
		strconv.FormatUint(evm.Context.Time, 10),
		"Grant",
		&child,
	))
	secondGrantSignature, _ := signDeriwSubAccountTypedData(t, secondGrantData)
	require.NoError(t, prec.GrantAccountControl(callCtx, evm, secondGrantData, secondGrantSignature))
	require.ErrorContains(t, prec.RevokeAccountControl(callCtx, evm, revokeData, revokeSignature), "authorization already used")

	parentAddress, err = prec.ReadAccountControl(callCtx, evm, child)
	require.NoError(t, err)
	require.Equal(t, parent, parentAddress)
}

func TestDeriwSubAccountPublicRejectsInvalidAuthorizationContext(t *testing.T) {
	tests := []struct {
		name          string
		timestamp     string
		mutate        func(*apitypes.TypedData, *big.Int)
		expectedError string
	}{
		{
			name: "wrong verifying contract",
			mutate: func(typedData *apitypes.TypedData, _ *big.Int) {
				typedData.Domain.VerifyingContract = common.HexToAddress("0x1234").Hex()
			},
			expectedError: "verifying contract",
		},
		{
			name:          "expired timestamp",
			timestamp:     strconv.FormatUint(deriwSubAccountTestTimestamp-deriwSubAccountMaxAgeSeconds-1, 10),
			expectedError: "expired",
		},
		{
			name:          "timestamp too far in future",
			timestamp:     strconv.FormatUint(deriwSubAccountTestTimestamp+deriwSubAccountFutureSkewSeconds+1, 10),
			expectedError: "future",
		},
		{
			name:          "noncanonical timestamp",
			timestamp:     "01800000000",
			expectedError: "invalid Timestamp",
		},
		{
			name: "extra signed field",
			mutate: func(typedData *apitypes.TypedData, _ *big.Int) {
				typedData.Types["Message"] = append(typedData.Types["Message"], apitypes.Type{Name: "Extra", Type: "string"})
				typedData.Message["Extra"] = "unexpected"
			},
			expectedError: "message schema",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evm := newMockEVMForTesting()
			evm.Context.Time = deriwSubAccountTestTimestamp
			child := common.HexToAddress("0x8f48163d1932dc2286cc7d1f260e09c6ed07a1e0")
			callCtx := testContext(child, evm)
			prec := &DeriwSubAccountPublic{Address: types.DeriwSubAccountPublicAddress}

			timestamp := test.timestamp
			if timestamp == "" {
				timestamp = strconv.FormatUint(evm.Context.Time, 10)
			}
			typedData := newDeriwSubAccountTypedData(evm.ChainConfig().ChainID, timestamp, "Grant", &child)
			if test.mutate != nil {
				test.mutate(&typedData, evm.ChainConfig().ChainID)
			}
			signData := marshalDeriwSubAccountTypedData(t, typedData)
			signature, _ := signDeriwSubAccountTypedData(t, signData)

			err := prec.GrantAccountControl(callCtx, evm, signData, signature)
			require.ErrorContains(t, err, test.expectedError)
		})
	}
}

func TestDeriwSubAccountPublicDoesNotEnforceSignedChainID(t *testing.T) {
	evm := newMockEVMForTesting()
	evm.Context.Time = deriwSubAccountTestTimestamp
	child := common.HexToAddress("0x8f48163d1932dc2286cc7d1f260e09c6ed07a1e0")
	callCtx := testContext(child, evm)
	prec := &DeriwSubAccountPublic{Address: types.DeriwSubAccountPublicAddress}

	// Existing clients sign a domain chain ID that may differ from the Deriw
	// node's configured chain ID. That value is signed but is not enforced.
	signerChainID := new(big.Int).Add(evm.ChainConfig().ChainID, big.NewInt(1))
	signData := marshalDeriwSubAccountTypedData(t, newDeriwSubAccountTypedData(
		signerChainID,
		strconv.FormatUint(evm.Context.Time, 10),
		"Grant",
		&child,
	))
	signature, parent := signDeriwSubAccountTypedData(t, signData)

	require.NoError(t, prec.GrantAccountControl(callCtx, evm, signData, signature))
	actualParent, err := prec.ReadAccountControl(callCtx, evm, child)
	require.NoError(t, err)
	require.Equal(t, parent, actualParent)
}

func TestDeriwSubAccountPublicChecksBothLegacyRecoveryByteKeys(t *testing.T) {
	evm := newMockEVMForTesting()
	evm.Context.Time = deriwSubAccountTestTimestamp
	child := common.HexToAddress("0x8f48163d1932dc2286cc7d1f260e09c6ed07a1e0")
	callCtx := testContext(child, evm)
	prec := &DeriwSubAccountPublic{Address: types.DeriwSubAccountPublicAddress}

	signData := marshalDeriwSubAccountTypedData(t, newDeriwSubAccountTypedData(
		evm.ChainConfig().ChainID,
		strconv.FormatUint(evm.Context.Time, 10),
		"Grant",
		&child,
	))
	signature, _ := signDeriwSubAccountTypedData(t, signData)
	legacyKey, _, err := deriwSubAccountLegacySignatureKeys(signature)
	require.NoError(t, err)
	require.NoError(t, callCtx.State.SubAccount().SetUsedHash(legacyKey))

	alternateSignature := bytes.Clone(signature)
	alternateSignature[crypto.RecoveryIDOffset] -= 27
	err = prec.GrantAccountControl(callCtx, evm, signData, alternateSignature)
	require.ErrorContains(t, err, "authorization already used")
}

func newDeriwSubAccountTypedData(chainID *big.Int, timestamp string, operation string, child *common.Address) apitypes.TypedData {
	typedChainID := commonmath.HexOrDecimal256(*new(big.Int).Set(chainID))
	messageTypes := []apitypes.Type{
		{Name: "Timestamp", Type: "string"},
		{Name: "Operation", Type: "string"},
	}
	message := apitypes.TypedDataMessage{
		"Timestamp": timestamp,
		"Operation": operation,
	}
	if child != nil {
		messageTypes = append(messageTypes, apitypes.Type{Name: "Child", Type: "address"})
		message["Child"] = child.Hex()
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
			ChainId:           &typedChainID,
			VerifyingContract: types.DeriwSubAccountPublicAddress.Hex(),
		},
		Message: message,
	}
}

func marshalDeriwSubAccountTypedData(t *testing.T, typedData apitypes.TypedData) []byte {
	t.Helper()
	signData, err := json.Marshal(typedData)
	require.NoError(t, err)
	return signData
}

func signDeriwSubAccountTypedData(t *testing.T, signData []byte) ([]byte, common.Address) {
	t.Helper()

	privateKey, err := crypto.HexToECDSA("59c6995e998f97a5a0044966f094538e3878c9e59085909bc4fe6b3a7f7f6d3b")
	require.NoError(t, err)

	typedData := apitypes.TypedData{}
	require.NoError(t, json.Unmarshal(signData, &typedData))
	digest, _, err := apitypes.TypedDataAndHash(typedData)
	require.NoError(t, err)

	signature, err := crypto.Sign(digest, privateKey)
	require.NoError(t, err)
	signature[crypto.RecoveryIDOffset] += 27

	return signature, crypto.PubkeyToAddress(privateKey.PublicKey)
}
