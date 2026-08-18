// Copyright 2021-2022, Offchain Labs, Inc.
// For license information, see https://github.com/nitro/blob/master/LICENSE

package precompiles

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"
	"strconv"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
	"github.com/offchainlabs/nitro/arbutil"
)

const (
	deriwSubAccountDomainName         = "DeriwSubAccountSignature"
	deriwSubAccountDomainVersion      = "1"
	deriwSubAccountAuthorizationScope = "DeriwSubAccountAuthorization/v1"
	deriwSubAccountMaxAgeSeconds      = uint64(600)
	deriwSubAccountFutureSkewSeconds  = uint64(30)
)

var (
	deriwSubAccountDomainTypes = []apitypes.Type{
		{Name: "name", Type: "string"},
		{Name: "version", Type: "string"},
		{Name: "chainId", Type: "uint256"},
		{Name: "verifyingContract", Type: "address"},
	}
	deriwSubAccountGrantTypes = []apitypes.Type{
		{Name: "Timestamp", Type: "string"},
		{Name: "Operation", Type: "string"},
		{Name: "Child", Type: "address"},
	}
	deriwSubAccountRevokeTypes = []apitypes.Type{
		{Name: "Timestamp", Type: "string"},
		{Name: "Operation", Type: "string"},
	}
)

// DeriwSubAccount precompile,public accessible contract, allow anyone to grant permission so that another public key can act on behalf on their permission
type DeriwSubAccountPublic struct {
	Address addr // 0x7E9 2023
}

// AddChainOwner adds account as a chain owner
func (con DeriwSubAccountPublic) GrantAccountControl(c ctx, evm mech, signData []byte, signature []byte) error {
	typedData, parent, digest, validSignature, err := arbutil.ParseTypeDataNSignatureWithDigest(signData, signature)
	if err != nil {
		return err
	}
	if !validSignature {
		return errors.New("GrantAccountControl failed to verify signature")
	}

	timestamp, childAddress, err := validateDeriwSubAccountAuthorization(typedData, "Grant", evm)
	if err != nil {
		return err
	}

	if childAddress.Cmp(c.caller) != 0 {
		return errors.New("GrantAccountControl address validation fail ")
	}

	actionKey := deriwSubAccountAuthorizationKey(*parent, digest)
	used, err := deriwSubAccountAuthorizationUsed(c, actionKey, signature, true)
	if err != nil {
		return err
	}
	if used {
		return errors.New("GrantAccountControl authorization already used")
	}
	if err := c.State.SubAccount().SetUsedHash(actionKey); err != nil {
		return err
	}

	// update sub-account
	return c.State.SubAccount().BindRelation(*parent, childAddress, new(big.Int).SetUint64(timestamp))
}

// RemoveGaslessOwner removes account from the list of chain owners
func (con DeriwSubAccountPublic) RevokeAccountControl(c ctx, evm mech, signData []byte, signature []byte) error {
	typedData, parent, digest, validSignature, err := arbutil.ParseTypeDataNSignatureWithDigest(signData, signature)
	if err != nil {
		return err
	}
	if !validSignature {
		return errors.New("failed to verify signature")
	}

	if _, _, err := validateDeriwSubAccountAuthorization(typedData, "Revoke", evm); err != nil {
		return err
	}

	actionKey := deriwSubAccountAuthorizationKey(*parent, digest)
	used, err := deriwSubAccountAuthorizationUsed(c, actionKey, signature, false)
	if err != nil {
		return err
	}
	if used {
		return errors.New("RevokeAccountControl authorization already used")
	}
	if err := c.State.SubAccount().SetUsedHash(actionKey); err != nil {
		return err
	}

	// update sub-account
	return c.State.SubAccount().RevokeRelation(*parent)
}

func validateDeriwSubAccountAuthorization(typedData *apitypes.TypedData, expectedOperation string, evm mech) (uint64, common.Address, error) {
	if typedData.PrimaryType != "Message" {
		return 0, common.Address{}, fmt.Errorf("invalid primary type %q", typedData.PrimaryType)
	}
	if len(typedData.Types) != 2 ||
		!equalDeriwSubAccountTypes(typedData.Types["EIP712Domain"], deriwSubAccountDomainTypes) {
		return 0, common.Address{}, errors.New("invalid EIP-712 domain schema")
	}

	expectedMessageTypes := deriwSubAccountGrantTypes
	if expectedOperation == "Revoke" {
		expectedMessageTypes = deriwSubAccountRevokeTypes
	}
	if !equalDeriwSubAccountTypes(typedData.Types["Message"], expectedMessageTypes) {
		return 0, common.Address{}, fmt.Errorf("invalid %s message schema", expectedOperation)
	}

	domain := typedData.Domain
	if domain.Name != deriwSubAccountDomainName || domain.Version != deriwSubAccountDomainVersion {
		return 0, common.Address{}, errors.New("invalid EIP-712 domain name or version")
	}
	// The chain ID remains part of the EIP-712 payload for compatibility with
	// existing clients, but it is intentionally not compared with the node's
	// configured chain ID. The verifying contract and short-lived timestamp
	// remain enforced independently.
	if !common.IsHexAddress(domain.VerifyingContract) ||
		common.HexToAddress(domain.VerifyingContract) != types.DeriwSubAccountPublicAddress || domain.Salt != "" {
		return 0, common.Address{}, errors.New("invalid EIP-712 verifying contract")
	}

	expectedMessageSize := 2
	if expectedOperation == "Grant" {
		expectedMessageSize = 3
	}
	if len(typedData.Message) != expectedMessageSize {
		return 0, common.Address{}, fmt.Errorf("invalid %s message fields", expectedOperation)
	}
	operation, ok := typedData.Message["Operation"].(string)
	if !ok || operation != expectedOperation {
		return 0, common.Address{}, fmt.Errorf("operation %q not supported", operation)
	}
	timestampString, ok := typedData.Message["Timestamp"].(string)
	if !ok {
		return 0, common.Address{}, errors.New("Timestamp must be a decimal string")
	}
	timestamp, err := strconv.ParseUint(timestampString, 10, 64)
	if err != nil || strconv.FormatUint(timestamp, 10) != timestampString {
		return 0, common.Address{}, errors.New("invalid Timestamp")
	}
	if timestamp > evm.Context.Time {
		if timestamp-evm.Context.Time > deriwSubAccountFutureSkewSeconds {
			return 0, common.Address{}, errors.New("authorization Timestamp is too far in the future")
		}
	} else if evm.Context.Time-timestamp > deriwSubAccountMaxAgeSeconds {
		return 0, common.Address{}, errors.New("authorization has expired")
	}

	childAddress := common.Address{}
	if expectedOperation == "Grant" {
		childString, ok := typedData.Message["Child"].(string)
		if !ok || !common.IsHexAddress(childString) {
			return 0, common.Address{}, errors.New("invalid Child address")
		}
		childAddress = common.HexToAddress(childString)
	}
	return timestamp, childAddress, nil
}

func equalDeriwSubAccountTypes(actual []apitypes.Type, expected []apitypes.Type) bool {
	if len(actual) != len(expected) {
		return false
	}
	for index := range expected {
		if actual[index] != expected[index] {
			return false
		}
	}
	return true
}

func deriwSubAccountAuthorizationKey(parent common.Address, digest common.Hash) common.Hash {
	return crypto.Keccak256Hash(
		[]byte(deriwSubAccountAuthorizationScope),
		parent.Bytes(),
		digest.Bytes(),
	)
}

func deriwSubAccountAuthorizationUsed(c ctx, actionKey common.Hash, signature []byte, checkLegacyGrantKeys bool) (bool, error) {
	used, err := c.State.SubAccount().HasUsedHash(actionKey)
	if err != nil || used || !checkLegacyGrantKeys {
		return used, err
	}

	legacyKey, alternateLegacyKey, err := deriwSubAccountLegacySignatureKeys(signature)
	if err != nil {
		return false, err
	}
	used, err = c.State.SubAccount().HasUsedHash(legacyKey)
	if err != nil || used {
		return used, err
	}
	return c.State.SubAccount().HasUsedHash(alternateLegacyKey)
}

func deriwSubAccountLegacySignatureKeys(signature []byte) (common.Hash, common.Hash, error) {
	if len(signature) != crypto.SignatureLength {
		return common.Hash{}, common.Hash{}, fmt.Errorf("invalid signature length %d", len(signature))
	}
	alternate := bytes.Clone(signature)
	switch alternate[crypto.RecoveryIDOffset] {
	case 0, 1:
		alternate[crypto.RecoveryIDOffset] += 27
	case 27, 28:
		alternate[crypto.RecoveryIDOffset] -= 27
	default:
		return common.Hash{}, common.Hash{}, fmt.Errorf("invalid recovery id %d", alternate[crypto.RecoveryIDOffset])
	}
	return common.BytesToHash(signature), common.BytesToHash(alternate), nil
}

func (con DeriwSubAccountPublic) ReadAccountControl(c ctx, evm mech, addr addr) (common.Address, error) {
	return c.State.SubAccount().ReadRelationFromChild(addr)
}

func (con DeriwSubAccountPublic) ReadAccountGranted(c ctx, evm mech, addr addr) (common.Address, error) {
	return c.State.SubAccount().ReadAccountGranted(addr)
}

//func (con DeriwSubAccountPublic) IsValidAccountSession(c ctx, evm mech, addr addr) (bool, *big.Int, *big.Int, error) {
//	return c.State.SubAccount().IsValidSession(addr)
//}
