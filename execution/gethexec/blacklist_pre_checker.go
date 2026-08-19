// Copyright 2021-2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package gethexec

import (
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/offchainlabs/nitro/arbos/arbosState"
	"github.com/offchainlabs/nitro/arbos/blacklist"
)

// ErrTxBlacklist is returned when transaction admission is denied by the Deriw blacklist.
var ErrTxBlacklist = errors.New("sender / receiver blacklisted")

// preCheckBlacklist enforces the chain blacklist as a transaction admission
// policy. In addition to the signed transaction sender, it checks the parent
// account that a permitted sub-account transaction will execute as.
//
// This admission check belongs to the transaction publisher/sequencer path.
// DeriwOS 1 independently enforces the same top-level address roles during
// consensus execution so delayed messages cannot bypass it.
func preCheckBlacklist(state *arbosState.ArbosState, tx *types.Transaction, sender common.Address) error {
	parent, err := state.SubAccount().GetParentAddress(sender, tx.To(), tx.Data())
	if err != nil {
		return fmt.Errorf("failed to resolve parent account for %v: %w", sender, err)
	}
	if state.DeriwOSVersion() >= arbosState.DeriwOSVersion_ConsensusBlacklist &&
		tx.To() != nil && *tx.To() == types.DeriwBlacklistAddress && tx.Value().Sign() == 0 &&
		parent == nil && len(tx.SetCodeAuthorizations()) == 0 && blacklist.IsEmergencyRemovalInput(tx.Data()) {
		blacklistOwner, err := state.Blacklist().BlacklistOwner().IsMember(sender)
		if err != nil {
			return err
		}
		chainOwner, err := state.ChainOwners().IsMember(sender)
		if err != nil {
			return err
		}
		if blacklistOwner || chainOwner {
			return nil
		}
	}

	blocked, err := blacklistAdmissionMember(state, sender, true)
	if err != nil {
		return fmt.Errorf("failed to check blacklist sender %v: %w", sender, err)
	}
	if blocked {
		return fmt.Errorf("%w: sender %v", ErrTxBlacklist, sender)
	}

	if parent != nil {
		blocked, err = blacklistAdmissionMember(state, *parent, true)
		if err != nil {
			return fmt.Errorf("failed to check blacklist parent %v: %w", *parent, err)
		}
		if blocked {
			return fmt.Errorf("%w: delegated parent %v", ErrTxBlacklist, *parent)
		}
	}

	if tx.To() != nil {
		blocked, err = blacklistAdmissionMember(state, *tx.To(), false)
		if err != nil {
			return fmt.Errorf("failed to check blacklist recipient %v: %w", *tx.To(), err)
		}
		if blocked {
			return fmt.Errorf("%w: recipient %v", ErrTxBlacklist, *tx.To())
		}
	}

	return nil
}

func blacklistAdmissionMember(state *arbosState.ArbosState, address common.Address, senderRole bool) (bool, error) {
	if state.DeriwOSVersion() >= arbosState.DeriwOSVersion_ConsensusBlacklist {
		fromMember, err := state.Blacklist().TxFromAddrs().IsMember(address)
		if err != nil {
			return false, err
		}
		toMember, err := state.Blacklist().TxToAddrs().IsMember(address)
		return fromMember || toMember, err
	}
	if senderRole {
		return state.Blacklist().TxFromAddrs().IsMember(address)
	}
	return state.Blacklist().TxToAddrs().IsMember(address)
}
