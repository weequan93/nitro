package blacklist

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/offchainlabs/nitro/arbos/burn"
	"github.com/offchainlabs/nitro/arbos/storage"
	"github.com/offchainlabs/nitro/util/testhelpers"
	"github.com/stretchr/testify/require"
)

// Copyright 2021-2022, Offchain Labs, Inc.
// For license information, see https://github.com/nitro/blob/master/LICENSE

func BlacklistForTest(t *testing.T) *Blacklist {
	storage := storage.NewMemoryBacked(burn.NewSystemBurner(nil, false))
	err := InitializeBlacklist(storage)
	Require(t, err)
	return OpenBlacklist(storage)
}

func TestBlacklist(t *testing.T) {
	t.Parallel()

	blacklist := BlacklistForTest(t)

	// validate storing

	members, err := blacklist.TxToAddrs().AllMembers(100)
	if err != nil {
		Fail(t, "Fail to read tx to records")
	}
	if len(members) != 0 {
		Fail(t, "Record inside initial blacklist tx to should be zero")
	}

	members, err = blacklist.TxFromAddrs().AllMembers(100)
	if err != nil {
		t.Fatalf("Fail to read tx from records: %v", err)
	}
	if len(members) != 0 {
		Fail(t, "Record inside initial blacklist tx from should be zero")
	}

	validBlacklistFromAddress := common.HexToAddress("0x9C26a80e21a762eb2809aFd7C123728bF9930Cf1")
	invalidBlacklistFromAddress := common.HexToAddress("0x94A6713cbF5F589aB51570D0b4cd219792421af2")

	err = blacklist.TxFromAddrs().Add(validBlacklistFromAddress)
	if err != nil {
		t.Fatalf("Fail to add tx from records: %v", err)
	}
	isMember, err := blacklist.TxFromAddrs().IsMember(validBlacklistFromAddress)
	if err != nil {
		t.Fatalf("Fail to check tx from member: %v", err)
	}
	if isMember != true {
		t.Fatalf("Check tx from member result incorrect, expected %t, receive %t", true, isMember)
	}

	isMember, err = blacklist.TxFromAddrs().IsMember(invalidBlacklistFromAddress)
	if err != nil {
		t.Fatalf("Fail to check tx from member: %v", err)
	}
	if isMember == true {
		t.Fatalf("Check tx from member result incorrect, expected %t, receive %t", false, isMember)
	}

	validBlacklistToAddress := common.HexToAddress("0x6b20483C964B39da3607cE96BCf4b53794944490")
	invalidBlacklistToAddress := common.HexToAddress("0x1a76822BF95714D9c32a07477906fF0ddEaBc2f2")

	err = blacklist.TxToAddrs().Add(validBlacklistToAddress)
	if err != nil {
		t.Fatalf("Fail to add tx to records: %v", err)
	}
	isMember, err = blacklist.TxToAddrs().IsMember(validBlacklistToAddress)
	if err != nil {
		t.Fatalf("Fail to check tx to member: %v", err)
	}
	if isMember != true {
		t.Fatalf("Check tx to member result incorrect, expected %t, receive %t", true, isMember)
	}

	isMember, err = blacklist.TxToAddrs().IsMember(invalidBlacklistToAddress)
	if err != nil {
		t.Fatalf("Fail to check tx to member: %v", err)
	}
	if isMember == true {
		t.Fatalf("Check tx to member result incorrect, expected %t, receive %t", false, isMember)
	}

	// validate transaction
	isValid := blacklist.IsBlacklistAddrCheck(&invalidBlacklistFromAddress)
	if isValid == true {
		t.Fatalf("Check valid to address fail, expected %t, receive %t", false, isValid)
	}

	isValid = blacklist.IsBlacklistAddrCheck(&validBlacklistToAddress)
	if isValid != true {
		t.Fatalf("Check valid to address fail, expected %t, receive %t", true, isValid)
	}

	var inner types.TxData
	inner = &types.BlobTx{
		To: invalidBlacklistToAddress,
	}

	tx := types.NewTx(inner)
	isValid = blacklist.IsBlacklistTxCheck(nil, tx)
	if isValid == true {
		t.Fatalf("Check valid to address fail, expected %t, receive %t", false, isValid)
	}

	inner = &types.BlobTx{
		To: validBlacklistToAddress,
	}
	tx = types.NewTx(inner)
	isValid = blacklist.IsBlacklistTxCheck(nil, tx)
	if isValid != true {
		t.Fatalf("Check valid to address fail, expected %t, receive %t", true, isValid)
	}
}

func Require(t *testing.T, err error, printables ...interface{}) {
	t.Helper()
	testhelpers.RequireImpl(t, err, printables...)
}

func Fail(t *testing.T, printables ...interface{}) {
	t.Helper()
	testhelpers.FailImpl(t, printables...)
}

func TestBanTypeStorageReopenAndReplacement(t *testing.T) {
	blacklist := BlacklistForTest(t)
	address := common.HexToAddress("0x1234")
	untouched := common.HexToAddress("0x5678")
	require.NoError(t, blacklist.TxFromAddrs().Add(address))
	require.NoError(t, blacklist.TxToAddrs().Add(address))
	require.NoError(t, blacklist.TxFromAddrs().Add(untouched))
	require.NoError(t, blacklist.SetBanType(address, BanFlagERC20Transfer))
	reopened := OpenBlacklist(blacklist.storage)
	flag, err := reopened.BanType(address)
	require.NoError(t, err)
	require.Equal(t, BanFlagERC20Transfer, flag)
	flag, err = reopened.BanType(untouched)
	require.NoError(t, err)
	require.Equal(t, BanFlagAll, flag)
	require.Equal(t, BanFlagERC20Transfer, reopened.BanTypeFree(address))
	require.Equal(t, BanFlagAll, reopened.BanTypeFree(untouched))
	require.False(t, reopened.IsBlacklistAddrCheck(&address))
	tx := types.NewTx(&types.LegacyTx{To: &address})
	require.False(t, reopened.IsBlacklistTxCheck(&address, tx))
	require.NoError(t, reopened.SetBanType(address, BanFlagAll))
	require.Equal(t, BanFlagAll, reopened.BanTypeFree(address))
	require.True(t, reopened.IsBlacklistTxCheck(&address, tx))
	for _, open := range []func(uint64) (*FlaggedAddressSet, error){reopened.TxFromAddrsWithFlag, reopened.TxToAddrsWithFlag} {
		list, err := open(BanFlagERC20Transfer)
		require.NoError(t, err)
		members, err := list.AllMembers(100)
		require.NoError(t, err)
		require.Empty(t, members)
	}
}

func TestOnlyExactStoredBanFlagAllRejects(t *testing.T) {
	for _, direction := range []string{"from", "to", "both"} {
		t.Run(direction, func(t *testing.T) {
			list := BlacklistForTest(t)
			address := common.HexToAddress("0x1234")
			if direction != "to" {
				require.NoError(t, list.TxFromAddrs().Add(address))
			}
			if direction != "from" {
				require.NoError(t, list.TxToAddrs().Add(address))
			}
			key := common.BytesToHash(address.Bytes())
			tx := types.NewTx(&types.LegacyTx{To: &address})
			// Seed future values directly to prove they cannot accidentally
			// become ban-all, including values with bit 1 set.
			for _, flag := range []uint64{BanFlagAll, BanFlagERC20Transfer, 3, 255, ^uint64(0)} {
				require.NoError(t, list.banFlags().SetUint64(key, flag))
				got, err := list.BanType(address)
				require.NoError(t, err)
				require.Equal(t, flag, got)
				require.Equal(t, flag, list.BanTypeFree(address))
				require.Equal(t, flag == BanFlagAll, list.IsBlacklistAddrCheck(&address))
				require.Equal(t, flag == BanFlagAll, list.IsBlacklistTxCheck(&address, tx))
			}
			require.NoError(t, list.SetBanType(address, BanFlagAll))
			stored, err := list.banFlags().GetUint64(key)
			require.NoError(t, err)
			require.Equal(t, BanFlagAll, stored, "new ban-all entries must store flag 1 explicitly")
		})
	}
}

func TestBanFlagRequiresMembershipAndClearsAfterFinalRemoval(t *testing.T) {
	list := BlacklistForTest(t)
	address := common.HexToAddress("0x1234")
	key := common.BytesToHash(address.Bytes())
	require.NoError(t, list.SetBanType(address, BanFlagAll))
	require.Zero(t, list.BanTypeFree(address), "a flag without list membership must not block")
	from, err := list.TxFromAddrsWithFlag(BanFlagERC20Transfer)
	require.NoError(t, err)
	to, err := list.TxToAddrsWithFlag(BanFlagERC20Transfer)
	require.NoError(t, err)
	require.NoError(t, from.Add(address))
	require.NoError(t, to.Add(address))
	require.NoError(t, from.Remove(address, 60))
	stored, err := list.banFlags().GetUint64(key)
	require.NoError(t, err)
	require.Equal(t, BanFlagERC20Transfer, stored)
	require.NoError(t, to.Remove(address, 60))
	stored, err = list.banFlags().GetUint64(key)
	require.NoError(t, err)
	require.Zero(t, stored)
	// A newly added legacy entry must not inherit the removed transfer type.
	require.NoError(t, list.TxFromAddrs().Add(address))
	require.Equal(t, BanFlagAll, list.BanTypeFree(address))
}
