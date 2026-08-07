// Copyright 2021-2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package gethexec

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	"github.com/offchainlabs/nitro/arbos/arbosState"
	"github.com/offchainlabs/nitro/cmd/chaininfo"
)

func blacklistTestTx(target common.Address) *types.Transaction {
	return types.NewTx(&types.LegacyTx{
		To:       &target,
		Gas:      100_000,
		GasPrice: big.NewInt(0),
	})
}

func TestPreCheckBlacklist(t *testing.T) {
	child := common.HexToAddress("0x08F575626819336B0b12834c1059eE400EAe378c")
	parent := common.HexToAddress("0xe76a03e00B10528E079070d39B52ec5788f6f3A8")
	target := common.HexToAddress("0x8fb358679749FD952Ea5f090b0eA3675722B08F5")

	t.Run("allows unlisted transaction", func(t *testing.T) {
		state, _ := arbosState.NewArbosMemoryBackedArbOSState()
		require.NoError(t, preCheckBlacklist(state, blacklistTestTx(target), child))
	})

	t.Run("rejects signed sender", func(t *testing.T) {
		state, _ := arbosState.NewArbosMemoryBackedArbOSState()
		require.NoError(t, state.Blacklist().TxFromAddrs().Add(child))

		err := preCheckBlacklist(state, blacklistTestTx(target), child)
		require.ErrorIs(t, err, ErrTxBlacklist)
	})

	t.Run("rejects delegated parent", func(t *testing.T) {
		state, _ := arbosState.NewArbosMemoryBackedArbOSState()
		require.NoError(t, state.SubAccount().AllowedAddress().Add(target))
		require.NoError(t, state.SubAccount().BindRelation(parent, child, big.NewInt(0)))
		require.NoError(t, state.Blacklist().TxFromAddrs().Add(parent))

		err := preCheckBlacklist(state, blacklistTestTx(target), child)
		require.ErrorIs(t, err, ErrTxBlacklist)
		require.Contains(t, err.Error(), "delegated parent")
	})

	t.Run("does not treat parent as sender for disallowed target", func(t *testing.T) {
		state, _ := arbosState.NewArbosMemoryBackedArbOSState()
		require.NoError(t, state.SubAccount().BindRelation(parent, child, big.NewInt(0)))
		require.NoError(t, state.Blacklist().TxFromAddrs().Add(parent))

		require.NoError(t, preCheckBlacklist(state, blacklistTestTx(target), child))
	})

	t.Run("rejects recipient", func(t *testing.T) {
		state, _ := arbosState.NewArbosMemoryBackedArbOSState()
		require.NoError(t, state.Blacklist().TxToAddrs().Add(target))

		err := preCheckBlacklist(state, blacklistTestTx(target), child)
		require.ErrorIs(t, err, ErrTxBlacklist)
	})
}

func TestPreCheckTxRejectsDelegatedParentWhenStrictnessDisabled(t *testing.T) {
	state, statedb := arbosState.NewArbosMemoryBackedArbOSState()
	parent := common.HexToAddress("0xe76a03e00B10528E079070d39B52ec5788f6f3A8")
	target := common.HexToAddress("0x8fb358679749FD952Ea5f090b0eA3675722B08F5")

	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	child := crypto.PubkeyToAddress(key.PublicKey)
	require.NoError(t, state.SubAccount().AllowedAddress().Add(target))
	require.NoError(t, state.SubAccount().BindRelation(parent, child, big.NewInt(0)))
	require.NoError(t, state.Blacklist().TxFromAddrs().Add(parent))

	chainConfig := chaininfo.ArbitrumDevTestChainConfig()
	header := &types.Header{Number: big.NewInt(1), Time: 1, BaseFee: big.NewInt(0)}
	signer := types.MakeSigner(chainConfig, header.Number, header.Time, state.ArbOSVersion())
	tx, err := types.SignTx(blacklistTestTx(target), signer, key)
	require.NoError(t, err)

	err = PreCheckTx(
		nil,
		chainConfig,
		header,
		statedb,
		state,
		tx,
		nil,
		&TxPreCheckerConfig{Strictness: TxPreCheckerStrictnessNone},
	)
	require.ErrorIs(t, err, ErrTxBlacklist)
}

func TestSequencerPreTxFilterRejectsDelegatedParent(t *testing.T) {
	state, _ := arbosState.NewArbosMemoryBackedArbOSState()
	child := common.HexToAddress("0x08F575626819336B0b12834c1059eE400EAe378c")
	parent := common.HexToAddress("0xe76a03e00B10528E079070d39B52ec5788f6f3A8")
	target := common.HexToAddress("0x8fb358679749FD952Ea5f090b0eA3675722B08F5")
	require.NoError(t, state.SubAccount().AllowedAddress().Add(target))
	require.NoError(t, state.SubAccount().BindRelation(parent, child, big.NewInt(0)))
	require.NoError(t, state.Blacklist().TxFromAddrs().Add(parent))

	err := (&Sequencer{}).preTxFilter(nil, nil, nil, state, blacklistTestTx(target), nil, child, nil)
	require.ErrorIs(t, err, ErrTxBlacklist)
}
