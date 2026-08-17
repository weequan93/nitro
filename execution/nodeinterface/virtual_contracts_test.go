// Copyright 2021-2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package nodeinterface

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/stretchr/testify/require"

	"github.com/offchainlabs/nitro/arbos/arbosState"
)

func TestRPCGaslessEstimateGasHookUsesTargetAllowlist(t *testing.T) {
	arbState, statedb := arbosState.NewArbosMemoryBackedArbOSState()
	allowlistedTarget := common.HexToAddress("0x1000000000000000000000000000000000000000")
	allowlistedSender := common.HexToAddress("0x2000000000000000000000000000000000000000")
	unlistedTarget := common.HexToAddress("0x3000000000000000000000000000000000000000")

	require.NoError(t, arbState.Pricer().TxToAddrs().Add(allowlistedTarget))
	require.NoError(t, arbState.Pricer().TxFromAddrs().Add(allowlistedSender))

	gasless, err := core.RPCGaslessEstimateGasHook(statedb, &allowlistedTarget)
	require.NoError(t, err)
	require.True(t, gasless)

	gasless, err = core.RPCGaslessEstimateGasHook(statedb, &allowlistedSender)
	require.NoError(t, err)
	require.False(t, gasless, "sender allowlisting must not make the target gasless")

	gasless, err = core.RPCGaslessEstimateGasHook(statedb, &unlistedTarget)
	require.NoError(t, err)
	require.False(t, gasless)

	gasless, err = core.RPCGaslessEstimateGasHook(statedb, nil)
	require.NoError(t, err)
	require.False(t, gasless)
}
