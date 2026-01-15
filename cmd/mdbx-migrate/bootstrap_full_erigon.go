//go:build erigon
// +build erigon

package main

import (
	"context"
	"encoding/json"
	"fmt"

	ecommon "github.com/erigontech/erigon-lib/common"
	"github.com/erigontech/erigon/db/kv"
	erawdb "github.com/erigontech/erigon/db/rawdb"
	"github.com/erigontech/erigon/execution/chain"
	gcommon "github.com/ethereum/go-ethereum/common"
	gethrawdb "github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/ethdb"
)

func ensureDestChainConfig(ctx context.Context, src ethdb.Database, dst kv.RwDB) (*chain.Config, ecommon.Hash, bool, error) {
	genesisHash := gethrawdb.ReadCanonicalHash(src, 0)
	if genesisHash == (gcommon.Hash{}) {
		return nil, ecommon.Hash{}, false, fmt.Errorf("source genesis hash missing")
	}
	eGenesis := ecommon.BytesToHash(genesisHash.Bytes())

	var existing *chain.Config
	if err := dst.View(ctx, func(tx kv.Tx) error {
		cfg, err := erawdb.ReadChainConfig(tx, eGenesis)
		if err != nil {
			return err
		}
		existing = cfg
		return nil
	}); err != nil {
		return nil, eGenesis, false, fmt.Errorf("read destination chain config: %w", err)
	}
	if existing != nil {
		return existing, eGenesis, false, nil
	}

	gethCfg := gethrawdb.ReadChainConfig(src, genesisHash)
	if gethCfg == nil {
		return nil, eGenesis, false, fmt.Errorf("source chain config missing")
	}
	data, err := json.Marshal(gethCfg)
	if err != nil {
		return nil, eGenesis, false, fmt.Errorf("marshal source chain config: %w", err)
	}
	var cfg chain.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, eGenesis, false, fmt.Errorf("unmarshal chain config: %w", err)
	}

	if err := dst.Update(ctx, func(tx kv.RwTx) error {
		return erawdb.WriteChainConfig(tx, eGenesis, &cfg)
	}); err != nil {
		return nil, eGenesis, false, fmt.Errorf("write chain config: %w", err)
	}
	return &cfg, eGenesis, true, nil
}
