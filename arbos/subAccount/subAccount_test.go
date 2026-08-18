package subAccount

import (
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/offchainlabs/nitro/arbos/burn"
	"github.com/offchainlabs/nitro/arbos/storage"
	"github.com/offchainlabs/nitro/util/testhelpers"
	"github.com/stretchr/testify/require"
)

// Copyright 2021-2022, Offchain Labs, Inc.
// For license information, see https://github.com/nitro/blob/master/LICENSE

var usdtAddress = common.HexToAddress("0x9C26a80e21a762eb2809aFd7C123728bF9930Cf1")
var subAccountOwnerAddress = common.HexToAddress("0x94A6713cbF5F589aB51570D0b4cd219792421af2")
var allowedUsdtSpenderAddress = common.HexToAddress("0x6b20483C964B39da3607cE96BCf4b53794944490")
var emptyAddress = common.HexToAddress("")

func SubAccountForTest(t *testing.T) *SubAccountState {
	storage := storage.NewMemoryBacked(burn.NewSystemBurner(nil, false))
	err := InitializeSubAccountState(storage)
	Require(t, err)
	return OpenSubAccountState(storage)
}

func InitSubAccountData(t *testing.T, subAccountState *SubAccountState) {
	err := subAccountState.usdtAddress.Add(usdtAddress)
	Require(t, err)

	err = subAccountState.SubAccountOwner().Add(subAccountOwnerAddress)
	Require(t, err)

	err = subAccountState.AllowedAddress().Add(allowedUsdtSpenderAddress)
	Require(t, err)
}

type UsdtTestCase struct {
	ContractAddress common.Address
	TxData          []byte
	ExpectedResult  bool
}

// test permission and value set
func TestUsdtOperation(t *testing.T) {
	t.Parallel()

	subAccountState := SubAccountForTest(t)
	InitSubAccountData(t, subAccountState)

	// check initial state
	usdtAddr, err := subAccountState.UsdtAddress().IsMember(usdtAddress)
	if err != nil {
		Fail(t, "Fail to read usdt address")
	}

	if usdtAddr != true {
		Fail(t, "Initial state of usdt address is not same, expected = %s", usdtAddress.Hex())
	}

	// reset address
	err = subAccountState.UsdtAddress().Add(emptyAddress)
	if err != nil {
		Fail(t, "Fail to set empty usdt address")
	}

	// validate reset correctly
	usdtAddr, err = subAccountState.UsdtAddress().IsMember(emptyAddress)
	if err != nil {
		Fail(t, "Fail to read usdt address")
	}
	if usdtAddr != true {
		Fail(t, "Usdt address after reset is not zero, expected = %s", emptyAddress)
	}

	// set again address
	err = subAccountState.UsdtAddress().Add(usdtAddress)
	if err != nil {
		Fail(t, "Fail to set usdt address")
	}

	// validate reset correctly
	usdtAddr, err = subAccountState.UsdtAddress().IsMember(usdtAddress)
	if err != nil {
		Fail(t, "Fail to read usdt address")
	}
	if usdtAddr != true {
		Fail(t, "Set of usdt address fail, expected = %s", usdtAddress)
	}
}

func TestIsAllowedUsdtAddress(t *testing.T) {
	t.Parallel()

	subAccountState := SubAccountForTest(t)
	InitSubAccountData(t, subAccountState)

	anyFailOpSig1, _ := hex.DecodeString("095ea7b1")
	anyFailOpSig2, _ := hex.DecodeString("095ea1")
	anyFailOpSig3, _ := hex.DecodeString("095ea7b21122")
	fakeUsdtAddress := common.HexToAddress("0x6b20483C964B39da3607cE96BCf4b53794944490")
	validApproveTxData := usdtApproveCallData(allowedUsdtSpenderAddress)

	testCases := []UsdtTestCase{
		{ContractAddress: usdtAddress, TxData: []byte{}, ExpectedResult: false},
		{ContractAddress: usdtAddress, TxData: anyFailOpSig1, ExpectedResult: false},
		{ContractAddress: usdtAddress, TxData: anyFailOpSig2, ExpectedResult: false},
		{ContractAddress: usdtAddress, TxData: anyFailOpSig3, ExpectedResult: false},

		{ContractAddress: fakeUsdtAddress, TxData: []byte{}, ExpectedResult: false},
		{ContractAddress: fakeUsdtAddress, TxData: anyFailOpSig1, ExpectedResult: false},
		{ContractAddress: fakeUsdtAddress, TxData: anyFailOpSig2, ExpectedResult: false},
		{ContractAddress: fakeUsdtAddress, TxData: anyFailOpSig3, ExpectedResult: false},
		{ContractAddress: fakeUsdtAddress, TxData: validApproveTxData, ExpectedResult: false},

		{ContractAddress: usdtAddress, TxData: validApproveTxData, ExpectedResult: true},
	}

	for i, testCaseItem := range testCases {
		result, err := subAccountState.IsAllowedUsdtAddress(testCaseItem.ContractAddress, testCaseItem.TxData)
		if err != nil {
			Fail(t, "Fail to read is allowed sub-account address")
		}
		if result != testCaseItem.ExpectedResult {
			Fail(t, "Fail to match result IsAllowedUsdtAddress for test case %d, expected = %s, actual = %s", i, testCaseItem.ExpectedResult, result)
		}
	}
}

// test sub-account owner and value set
func TestSubAccountOwnerOperation(t *testing.T) {
	t.Parallel()

	subAccountState := SubAccountForTest(t)
	InitSubAccountData(t, subAccountState)

	// check initial state
	ownersAddr, err := subAccountState.SubAccountOwner().AllMembers(100)
	if err != nil {
		Fail(t, "SubAccount owner address length is not same, expected = %d, actual = %d", 2, len(ownersAddr))
	}
	if len(ownersAddr) != 1 {
		Fail(t, "SubAccount owner address length is not same")
	}

	// verify original value is there
	isOwner, err := subAccountState.SubAccountOwner().IsMember(subAccountOwnerAddress)
	if err != nil {
		Fail(t, "Fail to get sub-account owner address")
	}
	if isOwner != true {
		Fail(t, "Checking of sub-account owner fail, expected = %s, actual = %s", true, isOwner)
	}

	// validate can add more than 1 owner
	secondSubAccountOwnerAddress := common.HexToAddress("0x6b20483C964B39da3607cE96BCf4b53794944490")
	isOwner, err = subAccountState.SubAccountOwner().IsMember(secondSubAccountOwnerAddress)
	if err != nil {
		Fail(t, "Fail to get sub-account owner address")
	}
	if isOwner == true {
		Fail(t, "Checking of sub-account owner fail, expected = %s, actual = %s", false, isOwner)
	}

	err = subAccountState.SubAccountOwner().Add(secondSubAccountOwnerAddress)
	if err != nil {
		Fail(t, "Fail to add new sub-account owner address")
	}

	ownersAddr, err = subAccountState.SubAccountOwner().AllMembers(100)
	if err != nil {
		Fail(t, "Fail to get sub-account owner address")
	}
	if len(ownersAddr) != 2 {
		Fail(t, "SubAccount owner address length is not same, expected = %d, actual = %d", 2, len(ownersAddr))
	}

	isOwner, err = subAccountState.SubAccountOwner().IsMember(subAccountOwnerAddress)
	if err != nil {
		Fail(t, "Fail to get sub-account owner address")
	}
	if isOwner != true {
		Fail(t, "Checking of sub-account owner fail, expected = %s, actual = %s", true, isOwner)
	}

	isOwner, err = subAccountState.SubAccountOwner().IsMember(secondSubAccountOwnerAddress)
	if err != nil {
		Fail(t, "Fail to get sub-account owner address")
	}
	if isOwner != true {
		Fail(t, "Checking of sub-account owner fail, expected = %s, actual = %s", true, isOwner)
	}

	// remove secondSubAccountOwnerAddress
	err = subAccountState.SubAccountOwner().Remove(secondSubAccountOwnerAddress, 21)
	if err != nil {
		Fail(t, "Fail to remove sub-account owner address")
	}

	ownersAddr, err = subAccountState.SubAccountOwner().AllMembers(100)
	if err != nil {
		Fail(t, "SubAccount owner address length is not same, expected = %d, actual = %d", 2, len(ownersAddr))
	}
	if len(ownersAddr) != 1 {
		Fail(t, "SubAccount owner address length is not same")
	}

	isOwner, err = subAccountState.SubAccountOwner().IsMember(subAccountOwnerAddress)
	if err != nil {
		Fail(t, "Fail to get sub-account owner address")
	}
	if isOwner != true {
		Fail(t, "Checking of sub-account owner fail, expected = %s, actual = %s", true, isOwner)
	}

	isOwner, err = subAccountState.SubAccountOwner().IsMember(secondSubAccountOwnerAddress)
	if err != nil {
		Fail(t, "Fail to get sub-account owner address")
	}
	if isOwner == true {
		Fail(t, "Checking of sub-account owner fail, expected = %s, actual = %s", true, isOwner)
	}
}

func TestBindRelation(t *testing.T) {
	t.Parallel()

	subAccountState := SubAccountForTest(t)
	InitSubAccountData(t, subAccountState)

	parentAddress := common.HexToAddress("0x9C26a80e21a762eb2809aFd7C123728bF9930Cf1")
	childAddress := common.HexToAddress("0x94A6713cbF5F589aB51570D0b4cd219792421af2")

	child, err := subAccountState.ReadRelationFromParent(parentAddress)
	if err != nil {
		Fail(t, "Fail to read relation")
	}

	if child.Cmp(common.Address{}) != 0 {
		Fail(t, "Check relationship from parent fail, expected = %s, actual = %s", common.Address{}, child)
	}

	parent, err := subAccountState.ReadRelationFromChild(childAddress)
	if err != nil {
		Fail(t, "Fail to read relation")
	}

	if parent.Cmp(common.Address{}) != 0 {
		Fail(t, "Check relationship from child fail, expected = %s, actual = %s", common.Address{}, parent)
	}

	// check with existing relation
	err = subAccountState.BindRelation(parentAddress, childAddress, big.NewInt(0))
	if err != nil {
		Fail(t, "Fail to bind relation")
	}

	child, err = subAccountState.ReadRelationFromParent(parentAddress)
	if err != nil {
		Fail(t, "Fail to read relation")
	}

	if child.Cmp(childAddress) != 0 {
		Fail(t, "Check relationship from parent fail, expected = %s, actual = %s", childAddress, child)
	}

	parent, err = subAccountState.ReadRelationFromChild(childAddress)
	if err != nil {
		Fail(t, "Fail to read relation")
	}

	if parent.Cmp(parentAddress) != 0 {
		Fail(t, "Check relationship from child fail, expected = %s, actual = %s", parentAddress, parent)
	}

	parentAddr, err := subAccountState.GetParentAddress(child, &usdtAddress, usdtApproveCallData(allowedUsdtSpenderAddress))
	Require(t, err)
	if parentAddr == nil {
		Fail(t, "Expected parent address for allowed USDT approve")
	}
	if parentAddr.Cmp(parentAddress) != 0 {
		Fail(t, "Check relationship from child fail, expected = %s, actual = %s", parentAddress, parentAddr)
	}

	// revoke it
	err = subAccountState.RevokeRelation(parentAddress)
	if err != nil {
		Fail(t, "Fail to revoke relation")
	}
	child, err = subAccountState.ReadRelationFromParent(parentAddress)
	if err != nil {
		Fail(t, "Fail to read relation")
	}

	if child.Cmp(common.Address{}) != 0 {
		Fail(t, "Check relationship from parent fail, expected = %s, actual = %s", common.Address{}, child)
	}

	parent, err = subAccountState.ReadRelationFromChild(childAddress)
	if err != nil {
		Fail(t, "Fail to read relation")
	}

	if parent.Cmp(common.Address{}) != 0 {
		Fail(t, "Check relationship from child fail, expected = %s, actual = %s", common.Address{}, parent)
	}

}

func TestBindRelationRebindsChildOneToOne(t *testing.T) {
	subAccountState := SubAccountForTest(t)
	parentA := common.HexToAddress("0x1001")
	parentB := common.HexToAddress("0x1002")
	child := common.HexToAddress("0x2001")

	require.NoError(t, subAccountState.BindRelation(parentA, child, big.NewInt(0)))
	require.NoError(t, subAccountState.BindRelation(parentB, child, big.NewInt(0)))

	childOfA, err := subAccountState.ReadRelationFromParent(parentA)
	require.NoError(t, err)
	require.Equal(t, common.Address{}, childOfA)
	childOfB, err := subAccountState.ReadRelationFromParent(parentB)
	require.NoError(t, err)
	require.Equal(t, child, childOfB)
	parentOfChild, err := subAccountState.ReadRelationFromChild(child)
	require.NoError(t, err)
	require.Equal(t, parentB, parentOfChild)

	require.NoError(t, subAccountState.RevokeRelation(parentB))
	childOfB, err = subAccountState.ReadRelationFromParent(parentB)
	require.NoError(t, err)
	require.Equal(t, common.Address{}, childOfB)
	parentOfChild, err = subAccountState.ReadRelationFromChild(child)
	require.NoError(t, err)
	require.Equal(t, common.Address{}, parentOfChild)
}

func TestBindRelationRebindsParentOneToOne(t *testing.T) {
	subAccountState := SubAccountForTest(t)
	parent := common.HexToAddress("0x1001")
	childA := common.HexToAddress("0x2001")
	childB := common.HexToAddress("0x2002")

	require.NoError(t, subAccountState.BindRelation(parent, childA, big.NewInt(0)))
	require.NoError(t, subAccountState.BindRelation(parent, childB, big.NewInt(0)))

	parentOfA, err := subAccountState.ReadRelationFromChild(childA)
	require.NoError(t, err)
	require.Equal(t, common.Address{}, parentOfA)
	parentOfB, err := subAccountState.ReadRelationFromChild(childB)
	require.NoError(t, err)
	require.Equal(t, parent, parentOfB)
	childOfParent, err := subAccountState.ReadRelationFromParent(parent)
	require.NoError(t, err)
	require.Equal(t, childB, childOfParent)
}

func TestBindRelationRepairsLegacyInconsistentRebind(t *testing.T) {
	subAccountState := SubAccountForTest(t)
	parentA := common.HexToAddress("0x1001")
	parentB := common.HexToAddress("0x1002")
	child := common.HexToAddress("0x2001")

	// Reproduce the state created by the old implementation: both parents
	// point to the child while the reverse map points only to parent A.
	require.NoError(t, subAccountState.parentChildRelation.Add(parentA, child))
	require.NoError(t, subAccountState.parentChildRelation.Add(parentB, child))
	require.NoError(t, subAccountState.childParentRelation.Add(child, parentA))

	require.NoError(t, subAccountState.BindRelation(parentB, child, big.NewInt(0)))

	childOfA, err := subAccountState.ReadRelationFromParent(parentA)
	require.NoError(t, err)
	require.Equal(t, common.Address{}, childOfA)
	childOfB, err := subAccountState.ReadRelationFromParent(parentB)
	require.NoError(t, err)
	require.Equal(t, child, childOfB)
	parentOfChild, err := subAccountState.ReadRelationFromChild(child)
	require.NoError(t, err)
	require.Equal(t, parentB, parentOfChild)
}

func TestRevokeRelationPreservesMismatchedReverseOwner(t *testing.T) {
	subAccountState := SubAccountForTest(t)
	parentA := common.HexToAddress("0x1001")
	parentB := common.HexToAddress("0x1002")
	child := common.HexToAddress("0x2001")

	// Reproduce a legacy inconsistent state, then make sure parent B cannot
	// delete the reverse relationship that belongs to parent A.
	require.NoError(t, subAccountState.parentChildRelation.Add(parentA, child))
	require.NoError(t, subAccountState.parentChildRelation.Add(parentB, child))
	require.NoError(t, subAccountState.childParentRelation.Add(child, parentA))

	require.NoError(t, subAccountState.RevokeRelation(parentB))

	childOfA, err := subAccountState.ReadRelationFromParent(parentA)
	require.NoError(t, err)
	require.Equal(t, child, childOfA)
	childOfB, err := subAccountState.ReadRelationFromParent(parentB)
	require.NoError(t, err)
	require.Equal(t, common.Address{}, childOfB)
	parentOfChild, err := subAccountState.ReadRelationFromChild(child)
	require.NoError(t, err)
	require.Equal(t, parentA, parentOfChild)
}

func TestResetAllRelationshipByPositionRemovesMatchingCounterpart(t *testing.T) {
	for _, removeAddress := range []string{"parent", "child"} {
		t.Run(removeAddress, func(t *testing.T) {
			subAccountState := SubAccountForTest(t)
			parent := common.HexToAddress("0x1001")
			child := common.HexToAddress("0x2001")
			require.NoError(t, subAccountState.BindRelation(parent, child, big.NewInt(0)))

			address := parent
			if removeAddress == "child" {
				address = child
			}
			require.NoError(t, subAccountState.ResetAllRelationshipByPosition(address))

			childOfParent, err := subAccountState.ReadRelationFromParent(parent)
			require.NoError(t, err)
			require.Equal(t, common.Address{}, childOfParent)
			parentOfChild, err := subAccountState.ReadRelationFromChild(child)
			require.NoError(t, err)
			require.Equal(t, common.Address{}, parentOfChild)
		})
	}
}

func TestSession(t *testing.T) {
	// BindRelation
	// IsValidSession
}

func TestHasUsedHash(t *testing.T) {
	t.Parallel()

	subAccountState := SubAccountForTest(t)
	InitSubAccountData(t, subAccountState)

	key := common.HexToHash("0x12345")

	isSpend, err := subAccountState.HasUsedHash(key)
	if err != nil {
		Fail(t, "Fail check has used hash", err)
	}

	if isSpend == true {
		Fail(t, "Check for has usd hash fail, expected = %v, actual = %v. ", false, isSpend)
	}

	err = subAccountState.SetUsedHash(key)
	if err != nil {
		Fail(t, "Fail set has used hash", err)
	}

	isSpend, err = subAccountState.HasUsedHash(key)
	if err != nil {
		Fail(t, "Fail check has used hash", err)
	}

	if isSpend == false {
		Fail(t, "Check for has usd hash fail, expected = %v, actual = %v. ", true, isSpend)
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

func usdtApproveCallData(spender common.Address) []byte {
	txData := append([]byte{}, ERC20_APPROVE_OP_SIG...)
	txData = append(txData, common.LeftPadBytes(spender.Bytes(), 32)...)
	txData = append(txData, common.LeftPadBytes([]byte{1}, 32)...)
	return txData
}
