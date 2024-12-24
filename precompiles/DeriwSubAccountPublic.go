// Copyright 2021-2022, Offchain Labs, Inc.
// For license information, see https://github.com/nitro/blob/master/LICENSE

package precompiles

import (
	"bytes"
	"errors"
	"github.com/ethereum/go-ethereum/common"
	"github.com/offchainlabs/nitro/arbutil"
	"math/big"
)

// DeriwSubAccount precompile,public accessible contract, allow anyone to grant permission so that another public key can act on behalf on their permission
type DeriwSubAccountPublic struct {
	Address addr // 0x7E9 2023
}

// AddChainOwner adds account as a chain owner
func (con DeriwSubAccountPublic) GrantAccountControl(c ctx, evm mech, signData []byte, signature []byte) error {
	signatureUse := bytes.Clone(signature)
	var hasUsed, err = c.State.SubAccount().HasUsedHash(common.BytesToHash(signatureUse))
	if err != nil {
		return err
	}

	err = c.State.SubAccount().SetUsedHash(common.BytesToHash(signatureUse))
	if err != nil {
		return err
	}

	if hasUsed {
		return errors.New("GrantAccountControl Signature already used")
	}

	typedData, address, validSignature, err := arbutil.ParseTypeDataNSignature(signData, signatureUse)
	if err != nil {
		return err
	}

	if !validSignature {
		return errors.New("GrantAccountControl failed to verify signature")
	}

	//currentTimestamp := big.NewInt(time.Now().Unix())
	timestamp := big.NewInt(0)

	if signedTimestamp, ok := (typedData.Message["Timestamp"]).(string); ok {
		timestamp.SetString(signedTimestamp, 10)
	} else {
		return errors.New("GrantAccountControl timestamp casting error")
	}

	//if currentTimestamp.Cmp(timestamp) < 0 {
	//	// over current time
	//	return errors.New("GrantAccountControl timestamp is over current")
	//}

	//currentTimestamp = currentTimestamp.Sub(currentTimestamp, big.NewInt(600))
	//if timestamp.Cmp(currentTimestamp) < 0 {
	//	// more than 60s old of signature
	//	log.Info("GrantAccountControl timestamp is less than current", "currentTimestamp", currentTimestamp, "timestamp", timestamp)
	//	return errors.New("GrantAccountControl timestamp is too old")
	//}

	operation := typedData.Message["Operation"]

	if operation != "Grant" {
		return errors.New("GrantAccountControl operation not supported")
	}

	childAddr := typedData.Message["Child"]

	var childAddress common.Address
	if childAddrString, ok := childAddr.(string); ok {
		childAddress = common.HexToAddress(childAddrString)
	} else {
		return errors.New("Cast child address failed")
	}

	if childAddress.Cmp(c.caller) != 0 {
		return errors.New("GrantAccountControl address validation fail ")
	}

	// update sub-account
	return c.State.SubAccount().BindRelation(*address, childAddress, timestamp)
}

// RemoveGaslessOwner removes account from the list of chain owners
func (con DeriwSubAccountPublic) RevokeAccountControl(c ctx, evm mech, signData []byte, signature []byte) error {

	signatureUse := bytes.Clone(signature)

	typedData, address, validSignature, err := arbutil.ParseTypeDataNSignature(signData, signatureUse)
	if err != nil {
		return err
	}
	if !validSignature {
		return errors.New("failed to verify signature")
	}

	operation := typedData.Message["Operation"]
	if operation != "Revoke" {
		return errors.New("operation not supported")
	}
	// update sub-account
	return c.State.SubAccount().RevokeRelation(*address)
}

func (con DeriwSubAccountPublic) ReadAccountControl(c ctx, evm mech, addr addr) (common.Address, error) {
	return c.State.SubAccount().ReadRelationFromChild(addr)
}

//func (con DeriwSubAccountPublic) IsValidAccountSession(c ctx, evm mech, addr addr) (bool, *big.Int, *big.Int, error) {
//	return c.State.SubAccount().IsValidSession(addr)
//}
