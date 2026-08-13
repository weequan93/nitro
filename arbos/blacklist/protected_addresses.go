// Copyright 2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package blacklist

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

var staticProtectedSystemAddresses = []common.Address{
	params.SystemAddress,
	params.BeaconRootsAddress,
	params.HistoryStorageAddress,
	params.WithdrawalQueueAddress,
	params.ConsolidationQueueAddress,
	types.ArbosAddress,
	types.ArbosStateAddress,
	types.FilteredTransactionsStateAddress,
	types.L1PricerFundsPoolAddress,
	types.ArbSysAddress,
	types.ArbInfoAddress,
	types.ArbAddressTableAddress,
	types.ArbBLSAddress,
	types.ArbFunctionTableAddress,
	types.ArbosTestAddress,
	types.ArbGasInfoAddress,
	types.ArbOwnerPublicAddress,
	types.ArbAggregatorAddress,
	types.ArbRetryableTxAddress,
	types.ArbStatisticsAddress,
	types.ArbOwnerAddress,
	types.ArbWasmAddress,
	types.ArbWasmCacheAddress,
	types.ArbNativeTokenManagerAddress,
	types.ArbFilteredTransactionsManagerAddress,
	types.NodeInterfaceAddress,
	types.NodeInterfaceDebugAddress,
	types.ArbDebugAddress,
	types.DeriwGaslessPublicAddress,
	types.DeriwGaslessAddress,
	types.DeriwSubAccountPublicAddress,
	types.DeriwSubAccountAddress,
	types.DeriwBlacklistPublicAddress,
	types.DeriwBlacklistAddress,
}

func ProtectedSystemAddresses(extra ...common.Address) []common.Address {
	addresses := make([]common.Address, 0, len(staticProtectedSystemAddresses)+len(extra))
	addresses = append(addresses, staticProtectedSystemAddresses...)
	for _, address := range extra {
		if address != (common.Address{}) {
			addresses = append(addresses, address)
		}
	}
	return addresses
}

func IsProtectedSystemAddress(address common.Address, extra ...common.Address) bool {
	for _, protected := range ProtectedSystemAddresses(extra...) {
		if address == protected {
			return true
		}
	}
	return false
}
