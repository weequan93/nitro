// Copyright 2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package blacklist

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
)

func TestEmergencyRemovalSelectorsMatchSolidity(t *testing.T) {
	for signature, selector := range map[string][4]byte{
		"removeBlacklistTxFrom(address)": RemoveBlacklistTxFromSelector,
		"removeBlacklistTxTo(address)":   RemoveBlacklistTxToSelector,
	} {
		hash := crypto.Keccak256([]byte(signature))
		if got := [4]byte(hash[:4]); got != selector {
			t.Fatalf("selector for %s = %x, want %x", signature, got, selector)
		}
	}
}

func TestProtectedSystemAddressesCoverExecutionAndRecovery(t *testing.T) {
	for _, address := range []common.Address{
		params.SystemAddress,
		params.BeaconRootsAddress,
		params.HistoryStorageAddress,
		params.WithdrawalQueueAddress,
		params.ConsolidationQueueAddress,
		types.ArbosAddress,
		types.ArbosStateAddress,
		types.L1PricerFundsPoolAddress,
		types.ArbOwnerAddress,
		types.NodeInterfaceAddress,
		types.DeriwBlacklistAddress,
		types.DeriwBlacklistPublicAddress,
	} {
		if !IsProtectedSystemAddress(address) {
			t.Fatalf("system address %v is not protected", address)
		}
	}
	dynamicFeeAccount := common.HexToAddress("0xf001")
	if !IsProtectedSystemAddress(dynamicFeeAccount, dynamicFeeAccount) {
		t.Fatal("dynamic fee account is not protected")
	}
}
