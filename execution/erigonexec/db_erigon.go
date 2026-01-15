//go:build erigon
// +build erigon

package erigonexec

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/c2h5oh/datasize"
	elog "github.com/erigontech/erigon-lib/log/v3"
	"github.com/erigontech/erigon/db/kv"
	"github.com/erigontech/erigon/db/kv/dbcfg"
	emdbx "github.com/erigontech/erigon/db/kv/mdbx"
	mdbxgo "github.com/erigontech/mdbx-go/mdbx"
	"golang.org/x/sync/semaphore"
)

type ioCloser struct {
	kv.Closer
}

func (c ioCloser) Close() error {
	c.Closer.Close()
	return nil
}

func OpenDatabases(chainDir string, opts MdbxOptions) (*Databases, error) {
	logger := elog.New("component", "erigonexec")
	chainPath := filepath.Join(chainDir, "l2chaindata")
	arbPath := filepath.Join(chainDir, "arbitrumdata")
	wasmPath := filepath.Join(chainDir, "wasm")

	chainDB, err := openMdbxDB(dbcfg.ChainDB, chainPath, opts, logger, nil)
	if err != nil {
		return nil, fmt.Errorf("open mdbx chain db: %w", err)
	}

	arbDB, err := openMdbxDB(dbcfg.ArbitrumDB, arbPath, opts, logger, arbTablesCfg())
	if err != nil {
		chainDB.Close()
		return nil, fmt.Errorf("open mdbx arb db: %w", err)
	}

	wasmDB, err := openMdbxDB(dbcfg.ArbWasmDB, wasmPath, opts, logger, wasmTablesCfg())
	if err != nil {
		arbDB.Close()
		chainDB.Close()
		return nil, fmt.Errorf("open mdbx wasm db: %w", err)
	}

	return &Databases{
		ChainDB: ioCloser{chainDB},
		ArbDB:   ioCloser{arbDB},
		WasmDB:  ioCloser{wasmDB},
	}, nil
}

func openMdbxDB(label kv.Label, path string, cfg MdbxOptions, logger elog.Logger, tables kv.TableCfg) (kv.RwDB, error) {
	opts := emdbx.New(label, logger).Path(path)
	opts = applyMdbxOptions(opts, cfg)
	if tables != nil {
		opts = opts.WithTableCfg(func(_ kv.TableCfg) kv.TableCfg { return tables })
	}
	return opts.Open(context.Background())
}

func applyMdbxOptions(opts emdbx.MdbxOpts, cfg MdbxOptions) emdbx.MdbxOpts {
	if cfg.PageSize > 0 {
		opts = opts.PageSize(datasize.ByteSize(cfg.PageSize))
	}
	if cfg.MapSize > 0 {
		opts = opts.MapSize(datasize.ByteSize(cfg.MapSize))
	}
	if cfg.GrowthStep > 0 {
		opts = opts.GrowthStep(datasize.ByteSize(cfg.GrowthStep))
	}
	if cfg.WriteMap {
		opts = opts.WriteMap(true)
	}
	if cfg.NoSync {
		opts = opts.Flags(func(f uint) uint { return f&^mdbxgo.Durable | mdbxgo.SafeNoSync })
	}
	if cfg.MaxReaders > 0 {
		opts = opts.RoTxsLimiter(semaphore.NewWeighted(int64(cfg.MaxReaders)))
	}
	return opts
}

func arbTablesCfg() kv.TableCfg {
	cfg := kv.TableCfg{
		kv.Sequence: {},
	}
	for _, bucket := range arbBuckets() {
		cfg[bucket] = kv.TableCfgItem{}
	}
	return cfg
}

func wasmTablesCfg() kv.TableCfg {
	return kv.TableCfg{
		BucketArbWasm: {},
		kv.Sequence:   {},
	}
}
