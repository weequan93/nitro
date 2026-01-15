//go:build erigon
// +build erigon

package arbos

import (
	"encoding/json"
	"errors"

	echain "github.com/erigontech/erigon/execution/chain"

	"github.com/offchainlabs/nitro/arbos/arbostypes"
)

// BuildInitMessageFromChainConfig constructs a minimal ArbOS init message for local bootstrapping.
func BuildInitMessageFromChainConfig(chainConfig *echain.Config) (*arbostypes.ParsedInitMessage, error) {
	if chainConfig == nil {
		return nil, errors.New("arbos: missing chain config")
	}

	chainCfg, err := loadGethChainConfig(nil, chainConfig)
	if err != nil {
		return nil, err
	}

	serializedChainConfig, err := json.Marshal(chainCfg)
	if err != nil {
		return nil, err
	}

	return &arbostypes.ParsedInitMessage{
		ChainId:               chainCfg.ChainID,
		InitialL1BaseFee:      arbostypes.DefaultInitialL1BaseFee,
		ChainConfig:           chainCfg,
		SerializedChainConfig: serializedChainConfig,
	}, nil
}
