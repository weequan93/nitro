// Copyright 2021-2022, Offchain Labs, Inc.
// For license information, see https://github.com/nitro/blob/master/LICENSE

package precompiles

import (
	"errors"
	"github.com/ethereum/go-ethereum/common"
	"github.com/offchainlabs/nitro/arbutil"
	"math/big"
	"time"
)

// DeriwSubAccount precompile,public accessible contract, allow anyone to grant permission so that another public key can act on behalf on their permission
type DeriwSubAccountPublic struct {
	Address          addr // 0x7E9 2025
	OwnerActs        func(ctx, mech, bytes4, addr, []byte) error
	OwnerActsGasCost func(bytes4, addr, []byte) (uint64, error)
}

// AddChainOwner adds account as a chain owner
func (con DeriwSubAccountPublic) GrantAccountControl(c ctx, evm mech, signData []byte, signature []byte) error {

	var hasUsed, err = c.State.SubAccount().HasUsedHash(common.BytesToHash(signature))
	if err != nil {
		return err
	}

	if hasUsed == true {
		return errors.New("Signature already used")
	}

	typedData, address, validSignature, err := arbutil.ParseTypeDataNSignature(signData, signature)
	if err != nil {
		return err
	}
	if validSignature != true {
		return errors.New("failed to verify signature")
	}

	currentTimestamp := big.NewInt(time.Now().UnixMilli())
	timestamp := big.NewInt(0)
	timestamp.SetString((typedData.Message["Timestamp"]).(string), 10)
	if currentTimestamp.Cmp(timestamp) < 0 {
		// over current time
		return errors.New("timestamp is over current")
	}

	currentTimestamp = currentTimestamp.Sub(currentTimestamp, big.NewInt(60))
	if timestamp.Cmp(currentTimestamp) < 0 {
		// more than 60s old of signature
		return errors.New("timestamp is too old")
	}

	operation := typedData.Message["Operation"]
	if operation != "Grant" {
		return errors.New("operation not supported")
	}

	childAddr := typedData.Message["child"]
	childAddress := common.HexToAddress(childAddr.(string))
	if childAddress.Cmp(c.caller) != 0 {
		return errors.New("address validation fail ")
	}

	// update sub-account
	return c.State.SubAccount().BindRelation(*address, childAddress)
}

// RemoveGaslessOwner removes account from the list of chain owners
func (con DeriwSubAccountPublic) RevokeAccountControl(c ctx, evm mech, signData []byte, signature []byte) error {
	typedData, address, validSignature, err := arbutil.ParseTypeDataNSignature(signData, signature)
	if err != nil {
		return err
	}
	if validSignature != true {
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

	return c.State.SubAccount().ReadRelationFromChild(c.caller)
}
