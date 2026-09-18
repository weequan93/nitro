// Copyright 2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package gethexec

import (
	"github.com/ethereum/go-ethereum/arbitrum_types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"

	"github.com/offchainlabs/nitro/arbos"
	"github.com/offchainlabs/nitro/arbos/arbosState"
	"github.com/offchainlabs/nitro/arbos/arbostypes"
)

type legacyRecordingHooks struct {
	*arbos.NoopSequencingHooks
	recordingError error
}

func (h *legacyRecordingHooks) PreTxFilter(_ *params.ChainConfig, _ *types.Header, db *state.StateDB, _ *arbosState.ArbosState, tx *types.Transaction, _ *arbitrum_types.ConditionalOptions, sender common.Address, _ *arbos.L1Info) error {
	err := recordLegacyBlacklistPreimages(db, tx, sender)
	if err != nil && h.recordingError == nil {
		h.recordingError = err
	}
	return err
}

// Scheduled redeems bypass PreTxFilter. Read their legacy blacklist paths as
// soon as the scheduling transaction has executed, before the scheduler runs
// them. PostTxFilter also runs for redeems, so this covers nested scheduling.
// These reads collect trie witnesses only; membership never rejects a tx.
func (h *legacyRecordingHooks) PostTxFilter(_ *types.Header, db *state.StateDB, _ *arbosState.ArbosState, _ *types.Transaction, _ common.Address, _ uint64, result *core.ExecutionResult) error {
	for _, tx := range result.ScheduledTxes {
		retry, ok := tx.GetInner().(*types.ArbitrumRetryTx)
		if !ok {
			continue
		}
		if err := recordLegacyBlacklistPreimages(db, tx, retry.From); err != nil {
			if h.recordingError == nil {
				h.recordingError = err
			}
			return err
		}
	}
	return nil
}

// Mirror ProduceBlock's parsing and non-discarding scheduler. Supplement reads
// immediately before each input transaction so earlier blacklist updates in the
// same block are visible. PostTxFilter supplements scheduled redeem reads.
// This does not guarantee complete compatibility with every legacy read path.
func produceBlockWithLegacyPreimages(message *arbostypes.L1IncomingMessage, delayedMessagesRead uint64, lastBlockHeader *types.Header, db *state.StateDB, chainContext core.ChainContext, prefetch bool, runCtx *core.MessageRunContext, exposeMultiGas bool) (*types.Block, *state.StateDB, types.Receipts, error) {
	version := types.DeserializeHeaderExtraInformation(lastBlockHeader).ArbOSFormatVersion
	txs, err := arbos.ParseL2Transactions(message, chainContext.Config().ChainID, version)
	if err != nil {
		log.Warn("error parsing incoming message", "err", err)
		txs = types.Transactions{}
	}
	hooks := &legacyRecordingHooks{NoopSequencingHooks: arbos.NewNoopSequencingHooks(txs)}
	block, resultState, receipts, err := arbos.ProduceBlockAdvanced(message.Header, delayedMessagesRead, lastBlockHeader, db, chainContext, hooks, prefetch, runCtx, exposeMultiGas)
	// NoopSequencingHooks does not propagate individual transaction errors.
	// Never let a failed witness read be mistaken for a successfully recorded tx.
	if hooks.recordingError != nil {
		return nil, nil, nil, hooks.recordingError
	}
	return block, resultState, receipts, err
}
