package erigonexec

import (
	"math/big"

	"github.com/offchainlabs/nitro/cmd/conf"
	"github.com/offchainlabs/nitro/execution/gethexec"
)

// Config holds the inputs required to bootstrap the Erigon execution backend.
type Config struct {
	ChainDir        string
	ChainName       string
	Mdbx            MdbxOptions
	ExpectedChainID *big.Int
	PruneMode       string
	// BlockMetadataApiBlocksLimit limits arb_getRawBlockMetadata range (0 disables).
	BlockMetadataApiBlocksLimit uint64
	Forwarder                   gethexec.ForwarderConfig
}

func MdbxOptionsFromConfig(cfg conf.MdbxConfig) MdbxOptions {
	return MdbxOptions{
		PageSize:   cfg.PageSize,
		MapSize:    cfg.MapSize,
		GrowthStep: cfg.GrowthStep,
		WriteMap:   cfg.WriteMap,
		NoSync:     cfg.NoSync,
		MaxReaders: cfg.MaxReaders,
	}
}
