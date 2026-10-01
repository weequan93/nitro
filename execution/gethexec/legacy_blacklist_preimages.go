// Copyright 2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package gethexec

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/offchainlabs/nitro/arbos/arbosState"
)

// Record the legacy top-level blacklist read set, without enforcing admission
// policy or modifying state. Use a separate system burner, not transaction gas.
func recordLegacyBlacklistPreimages(db *state.StateDB, tx *types.Transaction, sender common.Address) error {
	a, err := arbosState.OpenSystemArbosState(db, nil, true)
	if err != nil {
		return fmt.Errorf("opening legacy blacklist recording state: %w", err)
	}
	if tx.To() != nil {
		if _, err := a.Blacklist().TxToAddrs().IsMember(*tx.To()); err != nil {
			return fmt.Errorf("recording legacy recipient blacklist: %w", err)
		}
	}
	if _, err := a.Blacklist().TxFromAddrs().IsMember(sender); err != nil {
		return fmt.Errorf("recording legacy sender blacklist: %w", err)
	}
	return db.Error()
}
