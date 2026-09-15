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
// consensus execution so delayed messages cannot bypass it. Only BanFlagAll
// is rejected here; other stored flags belong to separate policy handlers.
func preCheckBlacklist(state *arbosState.ArbosState, tx *types.Transaction, sender common.Address) error {
	parent, err := state.SubAccount().GetParentAddress(sender, tx.To(), tx.Data())
	if err != nil {
		return fmt.Errorf("failed to resolve parent account for %v: %w", sender, err)
	}
	// Consensus compares the effective sender with the signed sender. A legacy
	// self-bound relationship does not change that identity and must not disable
	// the owner's emergency removal path during admission either.
	undelegated := parent == nil || *parent == sender
	if state.DeriwOSVersion() >= arbosState.DeriwOSVersion_ConsensusBlacklist &&
		tx.To() != nil && *tx.To() == types.DeriwBlacklistAddress && tx.Value().Sign() == 0 &&
		undelegated && len(tx.SetCodeAuthorizations()) == 0 && blacklist.IsEmergencyRemovalInput(tx.Data()) {
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

	banFlag, err := blacklistAdmissionBanFlag(state, sender, true)
	if err != nil {
		return fmt.Errorf("failed to check blacklist sender %v: %w", sender, err)
	}
	if banFlag == blacklist.BanFlagAll {
		return fmt.Errorf("%w: sender %v", ErrTxBlacklist, sender)
	}

	if parent != nil {
		banFlag, err = blacklistAdmissionBanFlag(state, *parent, true)
		if err != nil {
			return fmt.Errorf("failed to check blacklist parent %v: %w", *parent, err)
		}
		if banFlag == blacklist.BanFlagAll {
			return fmt.Errorf("%w: delegated parent %v", ErrTxBlacklist, *parent)
		}
	}

	if tx.To() != nil {
		banFlag, err = blacklistAdmissionBanFlag(state, *tx.To(), false)
		if err != nil {
			return fmt.Errorf("failed to check blacklist recipient %v: %w", *tx.To(), err)
		}
		if banFlag == blacklist.BanFlagAll {
			return fmt.Errorf("%w: recipient %v", ErrTxBlacklist, *tx.To())
		}
	}

	return nil
}

// blacklistAdmissionBanFlag resolves the actual stored type from DeriwOS 6.
// Historical lists predate flags and retain their original role/union rules.
func blacklistAdmissionBanFlag(state *arbosState.ArbosState, address common.Address, senderRole bool) (uint64, error) {
	if state.DeriwOSVersion() >= arbosState.DeriwOSVersion_BlacklistBanTypes {
		return state.Blacklist().BanType(address)
	}
	var member bool
	var err error
	if state.DeriwOSVersion() >= arbosState.DeriwOSVersion_ConsensusBlacklist {
		fromMember, err := state.Blacklist().TxFromAddrs().IsMember(address)
		if err != nil {
			return 0, err
		}
		toMember, err := state.Blacklist().TxToAddrs().IsMember(address)
		if err != nil {
			return 0, err
		}
		member = fromMember || toMember
	} else if senderRole {
		member, err = state.Blacklist().TxFromAddrs().IsMember(address)
	} else {
		member, err = state.Blacklist().TxToAddrs().IsMember(address)
	}
	if err != nil {
		return 0, err
	}
	if member {
		return blacklist.BanFlagAll, nil
	}
	return 0, nil
}
