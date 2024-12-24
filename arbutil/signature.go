package arbutil

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
)

func ParseTypeDataNSignature(signData []byte, signature []byte) (*apitypes.TypedData, *common.Address, bool, error) {
	typedData := apitypes.TypedData{}
	if err := json.Unmarshal(signData, &typedData); err != nil {
		return nil, nil, false, err
	}

	// EIP-712 typed data marshalling
	domainSeparator, err := typedData.HashStruct("EIP712Domain", typedData.Domain.Map())
	if err != nil {
		return nil, nil, false, err
	}
	typedDataHash, err := typedData.HashStruct(typedData.PrimaryType, typedData.Message)
	if err != nil {
		return nil, nil, false, err
	}

	// add magic string prefix
	rawData := []byte(fmt.Sprintf("\x19\x01%s%s", string(domainSeparator), string(typedDataHash)))
	sighash := crypto.Keccak256(rawData)

	// update the recovery id
	// https://github.com/ethereum/go-ethereum/blob/55599ee95d4151a2502465e0afc7c47bd1acba77/internal/ethapi/api.go#L442
	if signature[64] > 0 {
		signature[64] -= 27
	}

	// get the pubkey used to sign this signature
	sigPubkey, err := crypto.Ecrecover(sighash, signature)
	if err != nil {
		return nil, nil, false, err
	}

	// get the address to confirm it's the same one in the auth token
	pubkey, err := crypto.UnmarshalPubkey(sigPubkey)
	if err != nil {
		return nil, nil, false, err
	}

	address := crypto.PubkeyToAddress(*pubkey)

	// verify the signature (not sure if this is actually required after ecrecover)
	signatureNoRecoverID := signature[:len(signature)-1]
	verified := crypto.VerifySignature(sigPubkey, sighash, signatureNoRecoverID)
	if !verified {
		return &typedData, &address, false, errors.New("failed to verify signature")
	}

	return &typedData, &address, true, nil
}
