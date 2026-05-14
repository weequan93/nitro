// Copyright 2021-2022, Offchain Labs, Inc.
// For license information, see https://github.com/nitro/blob/master/LICENSE

package addressMap

// TODO lowercase this package name

import (
	"errors"
	"github.com/ethereum/go-ethereum/common"
	"github.com/offchainlabs/nitro/arbos/storage"
	"github.com/offchainlabs/nitro/arbos/util"
)

// AddressMap represents a set of addresses
// size is stored at position 0
// members of the set are stored sequentially from 1 onward
type AddressMap struct {
	backingStorage *storage.Storage
	backingValue   *storage.Storage
	size           storage.StorageBackedUint64
	byAddress      *storage.Storage
}

func Initialize(sto *storage.Storage) error {
	return sto.SetUint64ByUint64(0, 0)
}

func OpenAddressMap(sto *storage.Storage) *AddressMap {
	return &AddressMap{
		backingStorage: sto.OpenSubStorage([]byte{2}),
		backingValue:   sto.OpenSubStorage([]byte{1}),
		size:           sto.OpenStorageBackedUint64(0),
		byAddress:      sto.OpenSubStorage([]byte{3}),
	}
}

func OpenAddressSet(sto *storage.Storage) *AddressMap {
	return OpenAddressMap(sto)
}

func (as *AddressMap) Size() (uint64, error) {
	return as.size.Get()
}

func (as *AddressMap) IsMember(addr common.Address) (bool, error) {
	value, err := as.byAddress.Get(util.AddressToHash(addr))
	return value != (common.Hash{}), err
}

func (as *AddressMap) GetMember(addr common.Address) (common.Address, error) {
	addrAsHash := common.BytesToHash(addr.Bytes())
	slot, err := as.byAddress.GetUint64(addrAsHash)

	if slot == 0 || err != nil {
		return common.Address{}, err
	}

	sba := as.backingValue.OpenStorageBackedAddress(slot)
	value, _ := sba.Get()
	return value, nil
}

func (as *AddressMap) ClearBySize(size uint64) error {

	for i := uint64(1); i <= size; i++ {
		contents, _ := as.backingStorage.GetByUint64(i)
		_ = as.backingStorage.ClearByUint64(i)

		_ = as.backingValue.ClearByUint64(i)

		err := as.byAddress.Clear(contents)
		if err != nil {
			return err
		}
	}
	return as.size.Clear()
}

func (as *AddressMap) Clear() error {
	size, err := as.size.Get()
	if err != nil || size == 0 {
		return err
	}
	for i := uint64(1); i <= size; i++ {
		contents, _ := as.backingStorage.GetByUint64(i)
		_ = as.backingStorage.ClearByUint64(i)

		_ = as.backingValue.ClearByUint64(i)

		err = as.byAddress.Clear(contents)
		if err != nil {
			return err
		}
	}
	return as.size.Clear()
}

func (as *AddressMap) ClearList() error {
	size, err := as.size.Get()
	if err != nil || size == 0 {
		return err
	}
	for i := uint64(1); i <= size; i++ {
		err = as.backingStorage.ClearByUint64(i)
		if err != nil {
			return err
		}
		err = as.backingValue.ClearByUint64(i)
		if err != nil {
			return err
		}
	}
	return as.size.Clear()
}

func (as *AddressMap) AllMembers(maxNumToReturn uint64) ([]common.Address, error) {
	size, err := as.size.Get()
	if err != nil {
		return nil, err
	}
	if size > maxNumToReturn {
		size = maxNumToReturn
	}
	ret := make([]common.Address, size)
	for i := range ret {
		// #nosec G115
		sba := as.backingStorage.OpenStorageBackedAddress(uint64(i + 1))
		ret[i], err = sba.Get()
		if err != nil {
			return nil, err
		}
	}
	return ret, nil
}

func (as *AddressMap) RectifyMapping(addr common.Address) error {
	value, err := as.GetMember(addr)
	if err != nil {
		return err
	}
	isOwner, err := as.IsMember(addr)
	if !isOwner || err != nil {
		return errors.New("RectifyMapping: Address is not an owner")
	}

	addrAsHash := common.BytesToHash(addr.Bytes())
	slot, err := as.byAddress.GetUint64(addrAsHash)
	if err != nil {
		return err
	}
	atSlot, err := as.backingStorage.GetByUint64(slot)
	if err != nil {
		return err
	}
	size, err := as.size.Get()
	if err != nil {
		return err
	}
	if atSlot == addrAsHash && slot <= size {
		return errors.New("RectifyMapping: Owner address is correctly mapped")
	}

	err = as.byAddress.Clear(addrAsHash)
	if err != nil {
		return err
	}

	return as.Add(addr, value)
}

func (as *AddressMap) Add(addr common.Address, values ...common.Address) error {
	value := common.Address{}
	if len(values) > 0 {
		value = values[0]
	}

	present, err := as.IsMember(addr)
	if err != nil {
		return err
	}
	if present || err != nil {
		return err
	}

	size, err := as.size.Get()
	if err != nil {
		return err
	}
	slot := util.UintToHash(1 + size)
	addrAsHash := common.BytesToHash(addr.Bytes())
	err = as.byAddress.Set(addrAsHash, slot)
	if err != nil {
		return err
	}
	sba := as.backingStorage.OpenStorageBackedAddress(1 + size)

	err = sba.Set(addr)
	if err != nil {
		return err
	}

	sbv := as.backingValue.OpenStorageBackedAddress(1 + size)
	err = sbv.Set(value)
	if err != nil {
		return err
	}
	_, err = as.size.Increment()
	return err
}

func (as *AddressMap) Remove(addr common.Address, arbosVersion uint64) error {
	addrAsHash := common.BytesToHash(addr.Bytes())
	slot, err := as.byAddress.GetUint64(addrAsHash)
	if slot == 0 || err != nil {
		return err
	}
	err = as.byAddress.Clear(addrAsHash)
	if err != nil {
		return err
	}
	size, err := as.size.Get()
	if err != nil {
		return err
	}
	if slot < size {

		atSize, err := as.backingValue.GetByUint64(size)
		if err != nil {
			return err
		}
		err = as.backingValue.SetByUint64(slot, atSize)
		if err != nil {
			return err
		}

		atSize, err = as.backingStorage.GetByUint64(size)
		if err != nil {
			return err
		}
		err = as.backingStorage.SetByUint64(slot, atSize)
		if err != nil {
			return err
		}

		if arbosVersion >= 11 {
			err = as.byAddress.Set(atSize, util.UintToHash(slot))
			if err != nil {
				return err
			}
		}
	}
	err = as.backingStorage.ClearByUint64(size)
	if err != nil {
		return err
	}
	err = as.backingValue.ClearByUint64(size)
	if err != nil {
		return err
	}
	if size > 0 {
		_, err = as.size.Decrement()
	}
	return err
}
