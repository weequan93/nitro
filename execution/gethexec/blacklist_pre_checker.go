// Copyright 2021-2022, Offchain Labs, Inc.
// For license information, see https://github.com/nitro/blob/master/LICENSE

package gethexec

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/offchainlabs/nitro/arbos/arbosState"
)

// preCheckBlacklist enforces the chain blacklist as a transaction admission
// policy. In addition to the signed transaction sender, it checks the parent
// account that a permitted sub-account transaction will execute as.
//
// This check belongs to the transaction publisher/sequencer path rather than
// consensus execution. Transactions rejected here are never included in a
// block, so non-sequencing nodes do not need this policy to replay the chain.
func preCheckBlacklist(state *arbosState.ArbosState, tx *types.Transaction, sender common.Address) error {
	blocked, err := state.Blacklist().TxFromAddrs().IsMember(sender)
	if err != nil {
		return fmt.Errorf("failed to check blacklist sender %v: %w", sender, err)
	}
	if blocked {
		return fmt.Errorf("%w: sender %v", core.ErrTxBlacklist, sender)
	}

	parent, err := state.SubAccount().GetParentAddress(sender, tx.To(), tx.Data())
	if err != nil {
		return fmt.Errorf("failed to resolve parent account for %v: %w", sender, err)
	}
	if parent != nil {
		blocked, err = state.Blacklist().TxFromAddrs().IsMember(*parent)
		if err != nil {
			return fmt.Errorf("failed to check blacklist parent %v: %w", *parent, err)
		}
		if blocked {
			return fmt.Errorf("%w: delegated parent %v", core.ErrTxBlacklist, *parent)
		}
	}

	if tx.To() != nil {
		blocked, err = state.Blacklist().TxToAddrs().IsMember(*tx.To())
		if err != nil {
			return fmt.Errorf("failed to check blacklist recipient %v: %w", *tx.To(), err)
		}
		if blocked {
			return fmt.Errorf("%w: recipient %v", core.ErrTxBlacklist, *tx.To())
		}
	}

	return nil
}
