//go:build erigon
// +build erigon

package erigonexec

import (
	"bytes"

	ecommon "github.com/erigontech/erigon-lib/common"
	estate "github.com/erigontech/erigon/core/state"
	"github.com/holiman/uint256"

	"github.com/ethereum/go-ethereum/arbitrum_types"
)

func checkConditionalOptions(options *arbitrum_types.ConditionalOptions, l1BlockNumber uint64, l2Timestamp uint64, ibs estate.IntraBlockStateArbitrum) error {
	if options == nil {
		return nil
	}
	if options.BlockNumberMin != nil && l1BlockNumber < uint64(*options.BlockNumberMin) {
		return arbitrum_types.NewRejectedError("BlockNumberMin condition not met")
	}
	if options.BlockNumberMax != nil && l1BlockNumber > uint64(*options.BlockNumberMax) {
		return arbitrum_types.NewRejectedError("BlockNumberMax condition not met")
	}
	if options.TimestampMin != nil && l2Timestamp < uint64(*options.TimestampMin) {
		return arbitrum_types.NewRejectedError("TimestampMin condition not met")
	}
	if options.TimestampMax != nil && l2Timestamp > uint64(*options.TimestampMax) {
		return arbitrum_types.NewRejectedError("TimestampMax condition not met")
	}
	for address, rootHashOrSlots := range options.KnownAccounts {
		if rootHashOrSlots.RootHash != nil {
			storageRoot := ibs.GetStorageRoot(ecommon.Address(address))
			if toGethHash(storageRoot) != *rootHashOrSlots.RootHash {
				return arbitrum_types.NewRejectedError("Storage root hash condition not met")
			}
			continue
		}
		if len(rootHashOrSlots.SlotValue) == 0 {
			continue
		}
		for slot, value := range rootHashOrSlots.SlotValue {
			var out uint256.Int
			if err := ibs.GetState(ecommon.Address(address), ecommon.Hash(slot), &out); err != nil {
				return err
			}
			if !bytes.Equal(out.Bytes(), value.Bytes()) {
				return arbitrum_types.NewRejectedError("Storage slot value condition not met")
			}
		}
	}
	return nil
}
