// Copyright 2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package gethexec

import (
	"context"
	"errors"
	"math/big"
	"reflect"
	"testing"

	"github.com/holiman/uint256"

	"github.com/ethereum/go-ethereum/arbitrum_types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"

	"github.com/offchainlabs/nitro/arbos"
	"github.com/offchainlabs/nitro/arbos/arbosState"
	"github.com/offchainlabs/nitro/arbos/arbostypes"
	"github.com/offchainlabs/nitro/arbos/burn"
	"github.com/offchainlabs/nitro/arbos/l1pricing"
	"github.com/offchainlabs/nitro/cmd/chaininfo"
	"github.com/offchainlabs/nitro/util/blockdiag"
)

type diagnosticTestChain struct {
	core.ChainContext
	cfg      *params.ChainConfig
	previous *types.Header
}

func (c *diagnosticTestChain) Config() *params.ChainConfig { return c.cfg }
func (c *diagnosticTestChain) GetHeader(hash common.Hash, number uint64) *types.Header {
	if c.previous.Hash() == hash && c.previous.Number.Uint64() == number {
		return c.previous
	}
	return nil
}

var diagnosticFilterError = errors.New("diagnostic test filter")

type diagnosticTestHooks struct {
	*arbos.NoopSequencingHooks
	reject string
	errors []error
}

func (h *diagnosticTestHooks) CanDiscardTx() bool          { return true }
func (h *diagnosticTestHooks) SupportsGroupRollback() bool { return true }
func (h *diagnosticTestHooks) TxFailed(err error)          { h.errors = append(h.errors, err) }
func (h *diagnosticTestHooks) PreTxFilter(_ *params.ChainConfig, _ *types.Header, _ *state.StateDB, _ *arbosState.ArbosState, _ *types.Transaction, _ *arbitrum_types.ConditionalOptions, _ common.Address, _ *arbos.L1Info) error {
	if h.reject == "pre" {
		return diagnosticFilterError
	}
	return nil
}
func (h *diagnosticTestHooks) PostTxFilter(_ *types.Header, _ *state.StateDB, _ *arbosState.ArbosState, tx *types.Transaction, _ common.Address, _ uint64, _ *core.ExecutionResult) error {
	if h.reject == "post" && tx.Type() != types.ArbitrumInternalTxType {
		return diagnosticFilterError
	}
	return nil
}
func (h *diagnosticTestHooks) BlockFilter(_ *types.Header, _ *state.StateDB, _ types.Transactions, _ types.Receipts) error {
	if h.reject == "block" {
		return diagnosticFilterError
	}
	return nil
}

func TestBlockDiagnosticsExecutionEquivalence(t *testing.T) {
	cfg := chaininfo.ArbitrumDevTestChainConfig()
	db := state.NewDatabaseForTesting()
	defer db.TrieDB().Close()
	initial, err := state.New(types.EmptyRootHash, db)
	if err != nil {
		t.Fatal(err)
	}
	_, err = arbosState.InitializeArbosState(initial, burn.NewSystemBurner(nil, false), cfg, nil, arbostypes.TestInitMessage)
	if err != nil {
		t.Fatal(err)
	}
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	sender := crypto.PubkeyToAddress(key.PublicKey)
	contract := common.HexToAddress("0x12345")
	initial.SetBalance(sender, uint256.NewInt(1_000_000_000_000_000_000), tracing.BalanceChangeUnspecified)
	// The transaction performs real EVM computation and storage writes/reads.
	initial.SetCode(contract, common.FromHex("0x60016000556000545000"), tracing.CodeChangeUnspecified)
	root, err := initial.Commit(0, false, false)
	if err != nil {
		t.Fatal(err)
	}
	previous := arbosState.MakeGenesisBlock(common.Hash{}, 0, 1, root, cfg).Header()
	chain := &diagnosticTestChain{cfg: cfg, previous: previous}
	tx, err := types.SignNewTx(key, types.LatestSignerForChainID(cfg.ChainID), &types.LegacyTx{
		To: &contract, Gas: 1_000_000, GasPrice: big.NewInt(1_000_000_000_000),
	})
	if err != nil {
		t.Fatal(err)
	}
	l1 := &arbostypes.L1IncomingMessageHeader{Kind: arbostypes.L1MessageType_L2Message,
		Poster: l1pricing.BatchPosterAddress, BlockNumber: 1, Timestamp: 2, L1BaseFee: big.NewInt(1_000_000_000)}
	for _, reject := range []string{"", "pre", "post", "block"} {
		t.Run("filter_"+reject, func(t *testing.T) {
			var blocks [2]*types.Block
			var roots [2]common.Hash
			var receipts [2]types.Receipts
			var outcomes [2]error
			var failed [2][]error
			for i := range blocks {
				var diag *blockdiag.Trace
				if i == 1 {
					config := blockdiag.DefaultConfig
					config.Enable = true
					manager, err := blockdiag.New(config, nil)
					if err != nil {
						t.Fatal(err)
					}
					diag = manager.Begin()
				}
				sdb, err := diag.NewState(root, db)
				if err != nil {
					t.Fatal(err)
				}
				hooks := &diagnosticTestHooks{NoopSequencingHooks: arbos.NewNoopSequencingHooks(types.Transactions{tx}), reject: reject}
				diag.Run(context.Background(), func() {
					var result *state.StateDB
					blocks[i], result, receipts[i], outcomes[i] = arbos.ProduceBlockAdvanced(l1, 1, previous, sdb, chain, hooks, false, core.NewMessageSequencingContext(nil), false)
					if result != nil {
						sdb = result
					}
				})
				roots[i] = sdb.IntermediateRoot(false)
				failed[i] = hooks.errors
				if reject == "" && (outcomes[i] != nil || len(receipts[i]) != 2 || receipts[i][1].Status != 1) {
					t.Fatalf("fixture failed to execute contract: err=%v receipts=%+v", outcomes[i], receipts[i])
				}
				if (reject == "pre" || reject == "post") && (len(failed[i]) != 1 || failed[i][0] != diagnosticFilterError || sdb.GetState(contract, common.Hash{}) != (common.Hash{})) {
					t.Fatal("fixture did not reject/rollback transaction", failed[i])
				}
			}
			if roots[0] != roots[1] || outcomes[0] != outcomes[1] || !reflect.DeepEqual(receipts[0], receipts[1]) || !reflect.DeepEqual(failed[0], failed[1]) {
				t.Fatal("diagnostics changed state, receipts, filters or errors")
			}
			if blocks[0] != nil && (blocks[1] == nil || blocks[0].Hash() != blocks[1].Hash()) {
				t.Fatal("diagnostics changed block hash")
			}
		})
	}
}
