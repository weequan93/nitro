// Copyright 2021-2024, Offchain Labs, Inc.
// For license information, see https://github.com/nitro/blob/master/LICENSE

package addressMap

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/params"

	"github.com/offchainlabs/nitro/arbos/burn"
	"github.com/offchainlabs/nitro/arbos/storage"
	"github.com/offchainlabs/nitro/util/testhelpers"
)

func TestEmptyAddressMap(t *testing.T) {
	sto := storage.NewMemoryBacked(burn.NewSystemBurner(nil, false))
	Require(t, Initialize(sto))
	amap := OpenAddressMap(sto)
	version := params.ArbosVersion_11

	if size(t, amap) != 0 {
		Fail(t)
	}
	if isMember(t, amap, common.Address{}) {
		Fail(t)
	}
	value, err := amap.GetMember(common.Address{})
	Require(t, err)
	if value != (common.Address{}) {
		Fail(t)
	}
	Require(t, amap.Remove(common.Address{}, version))
	if size(t, amap) != 0 {
		Fail(t)
	}
}

func TestAddressMapAddGetRemove(t *testing.T) {
	sto := storage.NewMemoryBacked(burn.NewSystemBurner(nil, false))
	Require(t, Initialize(sto))
	amap := OpenAddressMap(sto)
	version := params.ArbosVersion_11

	key1 := testhelpers.RandomAddress()
	key2 := testhelpers.RandomAddress()
	key3 := testhelpers.RandomAddress()
	value1 := testhelpers.RandomAddress()
	value2 := testhelpers.RandomAddress()
	value3 := testhelpers.RandomAddress()

	Require(t, amap.Add(key1, value1))
	Require(t, amap.Add(key2, value2))
	Require(t, amap.Add(key3, value3))

	if size(t, amap) != 3 {
		Fail(t)
	}
	if !isMember(t, amap, key1) || !isMember(t, amap, key2) || !isMember(t, amap, key3) {
		Fail(t)
	}
	if isMember(t, amap, testhelpers.RandomAddress()) {
		Fail(t)
	}

	value, err := amap.GetMember(key2)
	Require(t, err)
	if value != value2 {
		Fail(t)
	}

	Require(t, amap.Remove(key2, version))
	if isMember(t, amap, key2) {
		Fail(t)
	}
	if size(t, amap) != 2 {
		Fail(t)
	}
	value, err = amap.GetMember(key1)
	Require(t, err)
	if value != value1 {
		Fail(t)
	}
	value, err = amap.GetMember(key3)
	Require(t, err)
	if value != value3 {
		Fail(t)
	}

	Require(t, amap.Remove(key1, version))
	if isMember(t, amap, key1) {
		Fail(t)
	}
	value, err = amap.GetMember(key3)
	Require(t, err)
	if value != value3 {
		Fail(t)
	}

	if err := amap.Add(key3, value3); err == nil {
		Fail(t, "expected duplicate Add to fail")
	}
}

func TestAddressMapClear(t *testing.T) {
	sto := storage.NewMemoryBacked(burn.NewSystemBurner(nil, false))
	Require(t, Initialize(sto))
	amap := OpenAddressMap(sto)

	key1 := testhelpers.RandomAddress()
	key2 := testhelpers.RandomAddress()
	Require(t, amap.Add(key1, testhelpers.RandomAddress()))
	Require(t, amap.Add(key2, testhelpers.RandomAddress()))

	Require(t, amap.Clear())
	if size(t, amap) != 0 {
		Fail(t)
	}
	if isMember(t, amap, key1) || isMember(t, amap, key2) {
		Fail(t)
	}
}

func TestAddressMapClearBySize(t *testing.T) {
	sto := storage.NewMemoryBacked(burn.NewSystemBurner(nil, false))
	Require(t, Initialize(sto))
	amap := OpenAddressMap(sto)

	Require(t, amap.Add(testhelpers.RandomAddress(), testhelpers.RandomAddress()))
	Require(t, amap.Add(testhelpers.RandomAddress(), testhelpers.RandomAddress()))
	Require(t, amap.ClearBySize(size(t, amap)))

	if size(t, amap) != 0 {
		Fail(t)
	}
}

func isMember(t *testing.T, amap *AddressMap, address common.Address) bool {
	t.Helper()
	present, err := amap.IsMember(address)
	Require(t, err)
	return present
}

func size(t *testing.T, amap *AddressMap) uint64 {
	t.Helper()
	size, err := amap.Size()
	Require(t, err)
	return size
}

func Require(t *testing.T, err error, printables ...interface{}) {
	t.Helper()
	testhelpers.RequireImpl(t, err, printables...)
}

func Fail(t *testing.T, printables ...interface{}) {
	t.Helper()
	testhelpers.FailImpl(t, printables...)
}
