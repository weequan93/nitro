package main

import (
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestCheckpointRoundTripAndFinalize(t *testing.T) {
	dest := t.TempDir()
	ckpt := &checkpoint{
		Mode:               "full",
		Phase:              phaseExecutionDone,
		ChainID:            "42161",
		GenesisHash:        common.HexToHash("0x1234").Hex(),
		StartBlock:         0,
		EndBlock:           100,
		LastHeaderImported: 100,
		LastExecuted:       95,
	}
	require.NoError(t, writeCheckpoint(dest, ckpt))

	loaded, err := loadCheckpoint(dest)
	require.NoError(t, err)
	require.Equal(t, ckpt, loaded)

	require.NoError(t, finalizeCheckpoint(dest))
	_, err = os.Stat(checkpointPath(dest))
	require.Error(t, err)
	require.True(t, os.IsNotExist(err))
}

func TestValidateCheckpoint(t *testing.T) {
	opts := Options{
		Mode:       "full",
		StartBlock: 10,
		EndBlock:   200,
	}
	info := sourceChainInfo{
		ChainID:     "42161",
		GenesisHash: common.HexToHash("0xdeadbeef"),
	}
	ckpt := &checkpoint{
		Mode:       "full",
		Phase:      phaseSendersDone,
		ChainID:    "42161",
		GenesisHash: common.HexToHash("0xdeadbeef").Hex(),
		StartBlock:  10,
		EndBlock:    200,
	}
	require.NoError(t, validateCheckpoint(opts, info, ckpt))

	ckpt.Mode = "state"
	require.ErrorContains(t, validateCheckpoint(opts, info, ckpt), "mode mismatch")
	ckpt.Mode = "full"

	ckpt.StartBlock = 11
	require.ErrorContains(t, validateCheckpoint(opts, info, ckpt), "start block mismatch")
	ckpt.StartBlock = 10

	ckpt.EndBlock = 201
	require.ErrorContains(t, validateCheckpoint(opts, info, ckpt), "end block mismatch")
	ckpt.EndBlock = 200

	ckpt.ChainID = "1"
	require.ErrorContains(t, validateCheckpoint(opts, info, ckpt), "chain id mismatch")
	ckpt.ChainID = "42161"

	ckpt.GenesisHash = common.HexToHash("0x1234").Hex()
	require.ErrorContains(t, validateCheckpoint(opts, info, ckpt), "genesis hash mismatch")
}

func TestPhaseCompletion(t *testing.T) {
	ckpt := &checkpoint{Phase: phaseExecutionDone}
	require.True(t, isPhaseComplete(ckpt, phaseSendersDone))
	require.True(t, isPhaseComplete(ckpt, phaseExecutionDone))
	require.False(t, isPhaseComplete(ckpt, phaseVerifyDone))
}
