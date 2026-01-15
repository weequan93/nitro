//go:build erigon
// +build erigon

package erigonexec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/params"

	"github.com/offchainlabs/nitro/arbos"
	"github.com/offchainlabs/nitro/arbos/arbosState"
)

func (c *Client) ValidateChainConfig(ctx context.Context, chainConfig *params.ChainConfig) error {
	if c == nil {
		return errors.New("erigonexec: nil client")
	}
	if chainConfig == nil || chainConfig.ChainID == nil {
		return errors.New("erigonexec: missing chain config")
	}
	header, ibs, release, err := c.latestState(ctx)
	if err != nil {
		return err
	}
	defer release()

	stateAdapter := arbos.NewStateDBAdapter(ibs, nil)
	arbState, err := arbosState.OpenSystemArbosState(stateAdapter, nil, true)
	if err != nil {
		return err
	}
	chainID, err := arbState.ChainId()
	if err != nil {
		return err
	}
	if chainID.Cmp(chainConfig.ChainID) != 0 {
		return fmt.Errorf("attempted to launch node with chain ID %v on ArbOS state with chain ID %v", chainConfig.ChainID, chainID)
	}
	oldSerializedConfig, err := arbState.ChainConfig()
	if err != nil {
		return fmt.Errorf("failed to get old chain config from ArbOS state: %w", err)
	}
	if len(oldSerializedConfig) != 0 {
		var oldConfig params.ChainConfig
		if err := json.Unmarshal(oldSerializedConfig, &oldConfig); err != nil {
			return fmt.Errorf("failed to deserialize old chain config: %w", err)
		}
		if header == nil || header.Number == nil {
			return errors.New("failed to get current block")
		}
		if err := oldConfig.CheckCompatible(chainConfig, header.Number.Uint64(), header.Time); err != nil {
			return fmt.Errorf("invalid chain config, not compatible with previous: %w", err)
		}
	}
	if chainConfig.DebugMode() {
		if arbState.ArbOSVersion() > params.MaxDebugArbosVersionSupported {
			return fmt.Errorf("attempted to launch node in debug mode with ArbOS version %v on ArbOS state with version %v", params.MaxDebugArbosVersionSupported, arbState.ArbOSVersion())
		}
	} else if arbState.ArbOSVersion() > params.MaxArbosVersionSupported {
		return fmt.Errorf("attempted to launch node with ArbOS version %v on ArbOS state with version %v", params.MaxArbosVersionSupported, arbState.ArbOSVersion())
	}
	return nil
}

func (c *Client) ArbOSChainConfig(ctx context.Context) (*params.ChainConfig, error) {
	if c == nil {
		return nil, errors.New("erigonexec: nil client")
	}
	_, ibs, release, err := c.latestState(ctx)
	if err != nil {
		return nil, err
	}
	defer release()

	stateAdapter := arbos.NewStateDBAdapter(ibs, nil)
	arbState, err := arbosState.OpenSystemArbosState(stateAdapter, nil, true)
	if err != nil {
		return nil, err
	}
	rawCfg, err := arbState.ChainConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get chain config from ArbOS state: %w", err)
	}
	if len(rawCfg) == 0 {
		return nil, nil
	}
	var parsed params.ChainConfig
	if err := json.Unmarshal(rawCfg, &parsed); err != nil {
		return nil, fmt.Errorf("failed to deserialize chain config: %w", err)
	}
	return &parsed, nil
}
