// Copyright 2021-2022, Offchain Labs, Inc.
// For license information, see https://github.com/nitro/blob/master/LICENSE

package subAccount

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/offchainlabs/nitro/arbos/addressSet"
	"github.com/offchainlabs/nitro/arbos/storage"
	"github.com/offchainlabs/nitro/arbos/util"
	"time"
)

type SubAccountState struct {
	storage             *storage.Storage
	parentChildRelation *storage.Storage
	childParentRelation *storage.Storage
	relationTimer       *storage.Storage
	allowedAddress      *addressSet.AddressSet
	subAccountOwner     *addressSet.AddressSet
	hashSpend           *storage.Storage
}

type SubspaceID []byte

var (
	parentChildRelationOffset SubspaceID = []byte{0}
	childParentRelationOffset SubspaceID = []byte{1}
	allowedAddressOffSet      SubspaceID = []byte{2}
	subAccountOwnerOffSet     SubspaceID = []byte{3}
	relationTimerOffSet       SubspaceID = []byte{4}
	hashSpendOffset           SubspaceID = []byte{5}
)

func InitializeSubAccountState(sto *storage.Storage) error {
	sto.OpenCachedSubStorage(parentChildRelationOffset)
	sto.OpenCachedSubStorage(childParentRelationOffset)
	sto.OpenCachedSubStorage(relationTimerOffSet)
	sto.OpenCachedSubStorage(hashSpendOffset)
	_ = addressSet.Initialize(sto.OpenSubStorage(allowedAddressOffSet))
	return addressSet.Initialize(sto.OpenSubStorage(subAccountOwnerOffSet))
}

func OpenSubAccountState(sto *storage.Storage) *SubAccountState {

	return &SubAccountState{
		storage:             sto,
		parentChildRelation: sto.OpenCachedSubStorage(parentChildRelationOffset),
		childParentRelation: sto.OpenCachedSubStorage(childParentRelationOffset),
		allowedAddress:      addressSet.OpenAddressSet(sto.OpenSubStorage(allowedAddressOffSet)),
		subAccountOwner:     addressSet.OpenAddressSet(sto.OpenSubStorage(subAccountOwnerOffSet)),
		relationTimer:       sto.OpenCachedSubStorage(relationTimerOffSet),
		hashSpend:           sto.OpenCachedSubStorage(hashSpendOffset),
	}
}

func (subAccountState *SubAccountState) BindRelation(parentAccount common.Address, subAccount common.Address) (err error) {
	err = subAccountState.parentChildRelation.Set(common.BytesToHash(parentAccount.Bytes()), common.BytesToHash(subAccount.Bytes()))
	if err != nil {
		return err
	}

	err = subAccountState.childParentRelation.Set(common.BytesToHash(subAccount.Bytes()), common.BytesToHash(parentAccount.Bytes()))
	if err != nil {
		return err
	}

	timeNow := time.Now().Unix()

	subAccountState.relationTimer.Set(common.BytesToHash(subAccount.Bytes()), common.BytesToHash(util.Int64ToBytes(timeNow)))

	return nil
}

func (subAccountState *SubAccountState) RevokeRelation(parentAccount common.Address) (err error) {

	err = subAccountState.parentChildRelation.Clear(common.BytesToHash(parentAccount.Bytes()))
	if err != nil {
		return err
	}

	// get child account
	subAccount, err := subAccountState.ReadRelationFromParent(parentAccount)
	if err != nil {
		return err
	}
	err = subAccountState.childParentRelation.Clear(common.BytesToHash(subAccount.Bytes()))
	if err != nil {
		return err
	}

	subAccountState.relationTimer.Set(common.BytesToHash(subAccount.Bytes()), common.BytesToHash(util.Int64ToBytes(0)))

	return nil
}

func (subAccountState *SubAccountState) ReadRelationFromParent(parentAccount common.Address) (common.Address, error) {
	childAccount, err := subAccountState.parentChildRelation.Get(common.BytesToHash(parentAccount.Bytes()))
	return common.BytesToAddress(childAccount.Bytes()), err
}

func (subAccountState *SubAccountState) ReadRelationFromChild(subAccount common.Address) (common.Address, error) {
	parentAccount, err := subAccountState.childParentRelation.Get(common.BytesToHash(subAccount.Bytes()))
	return common.BytesToAddress(parentAccount.Bytes()), err
}

func (subAccountState *SubAccountState) AllowedAddress() *addressSet.AddressSet {
	return subAccountState.allowedAddress
}

func (subAccountState *SubAccountState) SubAccountOwner() *addressSet.AddressSet {
	return subAccountState.subAccountOwner
}

func (subAccountState *SubAccountState) HasUsedHash(hash common.Hash) (bool, error) {
	hashContent, err := subAccountState.hashSpend.Get(hash)
	hasUsed := false
	if hashContent.Cmp(common.Hash{}) == 0 {
		hasUsed = true
	}
	return hasUsed, err
}
