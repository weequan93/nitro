// Copyright 2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package arbtest

import (
	"context"
	"math/big"
	"sync/atomic"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/stretchr/testify/require"
)

type precheckerEVMCountingBackend struct {
	core.NodeInterfaceBackendAPI
	calls          atomic.Uint64
	filterOverride core.TxFilterer
}

func (b *precheckerEVMCountingBackend) TxFilter() core.TxFilterer {
	if b.filterOverride != nil {
		return b.filterOverride
	}
	return b.NodeInterfaceBackendAPI.TxFilter()
}

func (b *precheckerEVMCountingBackend) GetEVM(ctx context.Context, statedb *state.StateDB, header *types.Header, config *vm.Config, blockContext *vm.BlockContext) *vm.EVM {
	b.calls.Add(1)
	return b.NodeInterfaceBackendAPI.GetEVM(ctx, statedb, header, config, blockContext)
}

// Unknown filter implementations must retain their simulation and rejection
// behavior even if the built-in engine has no address checker.
type precheckerRejectingFilterer struct{}

func (precheckerRejectingFilterer) Setup(*state.StateDB) {}
func (precheckerRejectingFilterer) TouchAddresses(*state.StateDB, *types.Transaction, common.Address) {
}
func (precheckerRejectingFilterer) CheckFiltered(*state.StateDB) error {
	return state.ErrArbTxFilter
}

// A filter wrapper is installed on the forwarder even with no address checker.
// Observe EVM creation at the prechecker boundary, separately from the sequencer,
// and then add/remove a checker to prove the decision is not cached at startup.
func TestPrecheckerFilterSkipsInactiveSimulation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	builder, forwarder, cleanup := buildPrecheckerFilterNodes(t, ctx, false)
	defer cleanup()
	backend := &precheckerEVMCountingBackend{
		NodeInterfaceBackendAPI: forwarder.ExecNode.Backend.APIBackend(),
	}
	require.NotNil(t, backend.TxFilter(), "fixture requires an installed API filter wrapper")
	forwarder.ExecNode.TxPreChecker.SetAPIBackend(backend)

	builder.L2Info.GenerateAccount("User1")
	builder.L2Info.GenerateAccount("User2")
	builder.L2Info.GenerateAccount("FilteredUser")
	_, receipt := builder.L2.TransferBalance(t, "Owner", "User1", big.NewInt(1e18), builder.L2Info)
	waitForForwarderSync(t, ctx, forwarder, receipt.BlockNumber.Uint64())

	tx := builder.L2Info.PrepareTx("User1", "User2", builder.L2Info.TransferGas, big.NewInt(1e12), nil)
	require.NoError(t, forwarder.Client.SendTransaction(ctx, tx))
	require.Zero(t, backend.calls.Load(), "no address checker: submission performed an unnecessary EVM simulation")
	receipt, err := builder.L2.EnsureTxSucceeded(tx)
	require.NoError(t, err)
	waitForForwarderSync(t, ctx, forwarder, receipt.BlockNumber.Uint64())

	checker := newHashedChecker([]common.Address{builder.L2Info.GetAddress("FilteredUser")})
	forwarder.ExecNode.ExecEngine.SetAddressChecker(t, checker)
	tx = builder.L2Info.PrepareTx("User1", "FilteredUser", builder.L2Info.TransferGas, big.NewInt(1e12), nil)
	err = forwarder.Client.SendTransaction(ctx, tx)
	require.True(t, isFilteredError(err), "active checker did not reject the filtered recipient: %v", err)
	require.Positive(t, backend.calls.Load(), "active checker did not run the filtering simulation")
	// The rejected transaction did not consume the sender's nonce.
	builder.L2Info.GetInfoWithPrivKey("User1").Nonce.Store(tx.Nonce())

	forwarder.ExecNode.ExecEngine.SetAddressChecker(t, nil)
	previousCalls := backend.calls.Load()
	tx = builder.L2Info.PrepareTx("User1", "FilteredUser", builder.L2Info.TransferGas, big.NewInt(1e12), nil)
	require.NoError(t, forwarder.Client.SendTransaction(ctx, tx))
	require.Equal(t, previousCalls, backend.calls.Load(), "removing the checker left preflight simulation active")
	receipt, err = builder.L2.EnsureTxSucceeded(tx)
	require.NoError(t, err)
	waitForForwarderSync(t, ctx, forwarder, receipt.BlockNumber.Uint64())

	backend.filterOverride = precheckerRejectingFilterer{}
	previousCalls = backend.calls.Load()
	tx = builder.L2Info.PrepareTx("User1", "User2", builder.L2Info.TransferGas, big.NewInt(1e12), nil)
	err = forwarder.Client.SendTransaction(ctx, tx)
	require.True(t, isFilteredError(err), "custom filter implementation was bypassed: %v", err)
	require.Greater(t, backend.calls.Load(), previousCalls, "custom filter did not execute its simulation")
}
