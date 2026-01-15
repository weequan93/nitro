package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/ethdb"

	"github.com/offchainlabs/nitro/cmd/conf"
	"github.com/offchainlabs/nitro/util/dbutil"
)

const (
	ExitPreflight    = 2
	ExitMigration    = 3
	ExitVerification = 4
)

type Options struct {
	Source        string
	Dest          string
	Mode          string
	Verify        string
	VerifySamples int
	Resume        bool
	StartBlock    uint64
	EndBlock      uint64
	Workers       int
	Mdbx          conf.MdbxConfig
}

type ExitError struct {
	Code int
	Err  error
}

func (e ExitError) Error() string {
	return e.Err.Error()
}

func run(opts Options) error {
	logKV("init",
		"source", opts.Source,
		"dest", opts.Dest,
		"mode", opts.Mode,
		"verify", opts.Verify,
		"verify_samples", opts.VerifySamples,
		"resume", opts.Resume,
		"start_block", opts.StartBlock,
		"end_block", opts.EndBlock,
		"workers", opts.Workers,
	)
	if err := preflight(opts); err != nil {
		return err
	}
	if err := migrate(opts); err != nil {
		return err
	}
	if opts.Verify != "none" {
		if err := verify(opts); err != nil {
			return err
		}
	}
	if opts.Mode == "full" {
		if err := finalizeCheckpoint(opts.Dest); err != nil {
			return ExitError{Code: ExitVerification, Err: fmt.Errorf("mdbx-migrate: finalize checkpoint: %w", err)}
		}
		if err := dbutil.DeleteUnfinishedConversionCanaryFile(filepath.Join(opts.Dest, "l2chaindata")); err != nil {
			return ExitError{Code: ExitVerification, Err: fmt.Errorf("mdbx-migrate: clear l2chaindata canary: %w", err)}
		}
		logKV("finalize", "dataset", "l2chaindata", "canary", "removed")
	}
	logKV("done", "status", "ok")
	return nil
}

func preflight(opts Options) error {
	source, err := filepath.Abs(opts.Source)
	if err != nil {
		return ExitError{Code: ExitPreflight, Err: fmt.Errorf("mdbx-migrate: resolve --source: %w", err)}
	}
	dest, err := filepath.Abs(opts.Dest)
	if err != nil {
		return ExitError{Code: ExitPreflight, Err: fmt.Errorf("mdbx-migrate: resolve --dest: %w", err)}
	}
	if source == dest {
		return ExitError{Code: ExitPreflight, Err: errors.New("mdbx-migrate: --source and --dest must differ")}
	}

	subDirs := []string{"l2chaindata", "arbitrumdata", "wasm"}
	for _, sub := range subDirs {
		dir := filepath.Join(source, sub)
		if _, err := os.Stat(dir); err != nil {
			return ExitError{Code: ExitPreflight, Err: fmt.Errorf("mdbx-migrate: missing source dir %s", dir)}
		}
		if fileExists(filepath.Join(dir, "mdbx.dat")) {
			return ExitError{Code: ExitPreflight, Err: fmt.Errorf("mdbx-migrate: source dir already contains mdbx (%s)", dir)}
		}
		if !fileExists(filepath.Join(dir, "CURRENT")) {
			return ExitError{Code: ExitPreflight, Err: fmt.Errorf("mdbx-migrate: source dir has no Pebble/LevelDB marker (%s)", dir)}
		}
	}

	if !opts.Resume {
		for _, sub := range subDirs {
			dir := filepath.Join(dest, sub)
			if fileExists(filepath.Join(dir, "CURRENT")) || fileExists(filepath.Join(dir, "mdbx.dat")) {
				return ExitError{Code: ExitPreflight, Err: fmt.Errorf("mdbx-migrate: destination already contains db files (%s)", dir)}
			}
			if dbutil.HasUnfinishedConversionCanaryFile(dir) {
				return ExitError{Code: ExitPreflight, Err: fmt.Errorf("mdbx-migrate: unfinished conversion canary present (%s)", dir)}
			}
		}
	}

	info, err := readSourceChainInfo(filepath.Join(source, "l2chaindata"))
	if err != nil {
		return ExitError{Code: ExitPreflight, Err: fmt.Errorf("mdbx-migrate: %w", err)}
	}
	logKV("preflight", "chain_id", info.ChainID, "genesis_hash", info.GenesisHash.Hex())

	return nil
}

type sourceChainInfo struct {
	ChainID     string
	GenesisHash common.Hash
}

func readSourceChainInfo(path string) (sourceChainInfo, error) {
	var info sourceChainInfo
	chainDB, err := openSourceChainDB(path)
	if err != nil {
		return info, fmt.Errorf("open l2chaindata: %w", err)
	}
	defer chainDB.Close()
	genesisHash := rawdb.ReadCanonicalHash(chainDB, 0)
	if genesisHash == (common.Hash{}) {
		return info, errors.New("missing genesis block in l2chaindata")
	}
	chainCfg := rawdb.ReadChainConfig(chainDB, genesisHash)
	if chainCfg == nil {
		return info, errors.New("missing chain config in l2chaindata")
	}
	chainID := "unknown"
	if chainCfg.ChainID != nil {
		chainID = chainCfg.ChainID.String()
	}
	info.ChainID = chainID
	info.GenesisHash = genesisHash
	return info, nil
}

func openSourceChainDB(path string) (ethdb.Database, error) {
	switch rawdb.PreexistingDatabase(path) {
	case "pebble":
		return rawdb.NewPebbleDBDatabase(path, 0, 0, "mdbx-migrate", true, true, nil)
	case "leveldb":
		return rawdb.NewLevelDBDatabase(path, 0, 0, "mdbx-migrate", true)
	default:
		return nil, errors.New("no supported database found")
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
