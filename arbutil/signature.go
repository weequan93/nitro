package arbutil

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
)

func ParseTypeDataNSignature(signData []byte, signature []byte) (*apitypes.TypedData, *common.Address, bool, error) {
	typedData, address, _, valid, err := ParseTypeDataNSignatureWithDigest(signData, signature)
	return typedData, address, valid, err
}

// ParseTypeDataNSignatureWithDigest parses strictly encoded EIP-712 data,
// canonicalizes the signature recovery byte, and returns the digest that was
// actually verified. It never mutates the supplied signature.
func ParseTypeDataNSignatureWithDigest(signData []byte, signature []byte) (*apitypes.TypedData, *common.Address, common.Hash, bool, error) {
	typedData := apitypes.TypedData{}
	decoder := json.NewDecoder(bytes.NewReader(signData))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&typedData); err != nil {
		return nil, nil, common.Hash{}, false, err
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, nil, common.Hash{}, false, err
	}

	// EIP-712 typed data marshalling
	domainSeparator, err := typedData.HashStruct("EIP712Domain", typedData.Domain.Map())
	if err != nil {
		return nil, nil, common.Hash{}, false, err
	}
	typedDataHash, err := typedData.HashStruct(typedData.PrimaryType, typedData.Message)
	if err != nil {
		return nil, nil, common.Hash{}, false, err
	}

	// add magic string prefix
	rawData := []byte(fmt.Sprintf("\x19\x01%s%s", string(domainSeparator), string(typedDataHash)))
	sighash := crypto.Keccak256Hash(rawData)

	// Canonicalize the recovery id on a copy before recovery. Ethereum clients
	// commonly encode v as either 0/1 or 27/28; both forms represent the same
	// signature and must have the same authorization identity.
	// https://github.com/ethereum/go-ethereum/blob/55599ee95d4151a2502465e0afc7c47bd1acba77/internal/ethapi/api.go#L442
	if len(signature) != 65 {
		return nil, nil, common.Hash{}, false, fmt.Errorf("invalid signature length %d", len(signature))
	}
	canonicalSignature := bytes.Clone(signature)
	switch canonicalSignature[64] {
	case 27, 28:
		canonicalSignature[64] -= 27
	case 0, 1:
		// Already canonical.
	default:
		return nil, nil, common.Hash{}, false, fmt.Errorf("invalid recovery id %d", canonicalSignature[64])
	}

	// get the pubkey used to sign this signature
	sigPubkey, err := crypto.Ecrecover(sighash.Bytes(), canonicalSignature)
	if err != nil {
		return nil, nil, common.Hash{}, false, err
	}

	// get the address to confirm it's the same one in the auth token
	pubkey, err := crypto.UnmarshalPubkey(sigPubkey)
	if err != nil {
		return nil, nil, common.Hash{}, false, err
	}

	address := crypto.PubkeyToAddress(*pubkey)

	// verify the signature (not sure if this is actually required after ecrecover)
	signatureNoRecoverID := canonicalSignature[:len(canonicalSignature)-1]
	verified := crypto.VerifySignature(sigPubkey, sighash.Bytes(), signatureNoRecoverID)
	if !verified {
		return &typedData, &address, sighash, false, errors.New("failed to verify signature")
	}

	return &typedData, &address, sighash, true, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing interface{}
	err := decoder.Decode(&trailing)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("unexpected trailing JSON value")
	}
	return err
}
